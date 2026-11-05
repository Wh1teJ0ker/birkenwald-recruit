package api

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/Wh1teJ0ker/birkenwald-recruit/internal/platform"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	verificationCodeTTL    = 10 * time.Minute
	verificationMaxTries   = 5
	verificationCodeLength = 6
)

type registerInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Code     string `json:"code"`
}

func normalizeEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > 254 {
		return "", false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", false
	}
	return email, true
}

func validVerificationCode(code string) bool {
	if len(code) != verificationCodeLength {
		return false
	}
	for _, digit := range code {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func newVerificationCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *Server) sendVerificationCode(c *gin.Context) {
	if !sameOrigin(c) {
		fail(c, 403, "invalid origin")
		return
	}
	enabled, err := registrationEnabled(c.Request.Context(), s.DB)
	if err != nil {
		fail(c, 500, "send verification code failed")
		return
	}
	if !enabled {
		fail(c, 403, "registration is closed")
		return
	}
	var in struct {
		Email string `json:"email"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "invalid JSON")
		return
	}
	email, ok := normalizeEmail(in.Email)
	if !ok {
		fail(c, 422, "valid email required")
		return
	}
	ctx := c.Request.Context()
	mailCfg, err := s.mailConfig(ctx)
	if err != nil {
		fail(c, 500, "send verification code failed")
		return
	}
	if !mailCfg.EmailDevMode && mailCfg.SMTPHost == "" {
		fail(c, 503, "email service unavailable")
		return
	}
	var exists bool
	if err := s.DB.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM users WHERE email=$1)", email).Scan(&exists); err != nil {
		fail(c, 500, "send verification code failed")
		return
	}
	if exists {
		fail(c, 409, "email already registered")
		return
	}
	var recent bool
	if err := s.DB.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM email_verification_codes WHERE email=$1 AND created_at > now() - interval '60 seconds')", email).Scan(&recent); err != nil {
		fail(c, 500, "send verification code failed")
		return
	}
	if recent {
		fail(c, 429, "verification code requested too recently")
		return
	}
	code, err := newVerificationCode()
	if err != nil {
		fail(c, 500, "send verification code failed")
		return
	}
	hash, err := platform.HashPassword(code)
	if err != nil {
		fail(c, 500, "send verification code failed")
		return
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		fail(c, 500, "send verification code failed")
		return
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM email_verification_codes WHERE email=$1 AND created_at > now() - interval '60 seconds')", email).Scan(&recent); err != nil {
		fail(c, 500, "send verification code failed")
		return
	}
	if recent {
		fail(c, 429, "verification code requested too recently")
		return
	}
	if _, err = tx.Exec(ctx, "UPDATE email_verification_codes SET consumed_at=now() WHERE email=$1 AND purpose='register' AND consumed_at IS NULL", email); err != nil {
		fail(c, 500, "send verification code failed")
		return
	}
	var codeID int64
	err = tx.QueryRow(ctx, `INSERT INTO email_verification_codes(email, purpose, code_hash, expires_at)
        VALUES ($1, 'register', $2, now() + interval '10 minutes') RETURNING id`, email, hash).Scan(&codeID)
	if isUniqueViolation(err) {
		fail(c, 429, "verification code requested too recently")
		return
	}
	if err != nil {
		fail(c, 500, "send verification code failed")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		fail(c, 500, "send verification code failed")
		return
	}
	if err = platform.SendVerificationEmail(mailCfg, email, code); err != nil {
		_, _ = s.DB.Exec(ctx, "DELETE FROM email_verification_codes WHERE id=$1", codeID)
		slog.Error("send verification email", "email", email, "error", err)
		fail(c, 502, "failed to send verification code")
		return
	}
	payload := gin.H{"ok": true, "expires_in": int(verificationCodeTTL.Seconds())}
	if mailCfg.EmailDevMode {
		payload["dev_code"] = code
	}
	c.JSON(http.StatusAccepted, payload)
}

func (s *Server) register(c *gin.Context) {
	if !sameOrigin(c) {
		fail(c, 403, "invalid origin")
		return
	}
	enabled, err := registrationEnabled(c.Request.Context(), s.DB)
	if err != nil {
		fail(c, 500, "registration failed")
		return
	}
	if !enabled {
		fail(c, 403, "registration is closed")
		return
	}
	var in registerInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, 400, "invalid JSON")
		return
	}
	email, ok := normalizeEmail(in.Email)
	if !ok {
		fail(c, 422, "valid email required")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 100 || len(in.Password) < 12 {
		fail(c, 422, "valid email, name and password of at least 12 characters required")
		return
	}
	if !validVerificationCode(in.Code) {
		fail(c, 422, "6-digit verification code required")
		return
	}
	ctx := c.Request.Context()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		fail(c, 500, "registration failed")
		return
	}
	defer tx.Rollback(ctx)
	var codeID int64
	var codeHash string
	var attempts int
	var expiresAt, now time.Time
	err = tx.QueryRow(ctx, `SELECT id, code_hash, attempts, expires_at, now()
        FROM email_verification_codes
        WHERE email=$1 AND purpose='register' AND consumed_at IS NULL
        ORDER BY id DESC LIMIT 1 FOR UPDATE`, email).Scan(&codeID, &codeHash, &attempts, &expiresAt, &now)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 422, "verification code required")
		return
	}
	if err != nil {
		fail(c, 500, "registration failed")
		return
	}
	if !now.Before(expiresAt) {
		if _, err = tx.Exec(ctx, "UPDATE email_verification_codes SET consumed_at=now() WHERE id=$1", codeID); err != nil {
			fail(c, 500, "registration failed")
			return
		}
		if err = tx.Commit(ctx); err != nil {
			fail(c, 500, "registration failed")
			return
		}
		fail(c, 422, "verification code expired")
		return
	}
	if !platform.CheckPassword(codeHash, in.Code) {
		attempts++
		if attempts >= verificationMaxTries {
			_, err = tx.Exec(ctx, "UPDATE email_verification_codes SET attempts=$2, consumed_at=now() WHERE id=$1", codeID, attempts)
		} else {
			_, err = tx.Exec(ctx, "UPDATE email_verification_codes SET attempts=$2 WHERE id=$1", codeID, attempts)
		}
		if err != nil {
			fail(c, 500, "registration failed")
			return
		}
		if err = tx.Commit(ctx); err != nil {
			fail(c, 500, "registration failed")
			return
		}
		fail(c, 422, "verification code invalid")
		return
	}
	passwordHash, err := platform.HashPassword(in.Password)
	if err != nil {
		fail(c, 500, "registration failed")
		return
	}
	var userID int64
	err = tx.QueryRow(ctx, `INSERT INTO users(email, display_name, password_hash, role)
        VALUES ($1, $2, $3, 'student')
        ON CONFLICT (email) DO NOTHING
        RETURNING id`, email, name, passwordHash).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 409, "email already registered")
		return
	}
	if err != nil {
		fail(c, 500, "registration failed")
		return
	}
	if _, err = tx.Exec(ctx, "UPDATE email_verification_codes SET consumed_at=now() WHERE id=$1", codeID); err != nil {
		fail(c, 500, "registration failed")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		fail(c, 500, "registration failed")
		return
	}
	if err = s.setSession(c, userID); err != nil {
		fail(c, 500, "session failed")
		return
	}
	c.JSON(201, gin.H{"id": userID, "role": "student"})
}
