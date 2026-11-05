package platform

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"
)

//go:embed migrations/001_init.up.sql
var migrations embed.FS

const Version = "0.1.0"

type Config struct {
	DatabaseURL   string
	HTTPAddr      string
	UploadDir     string
	WebDist       string
	AdminEmail    string
	AdminPassword string
	CookieSecure  bool
	PluginTimeout time.Duration
}

func LoadConfig() (Config, error) {
	c := Config{
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		HTTPAddr:      getenv("HTTP_ADDR", ":8080"),
		UploadDir:     getenv("UPLOAD_DIR", "./data/uploads"),
		WebDist:       getenv("WEB_DIST", "./frontend/dist"),
		AdminEmail:    strings.ToLower(strings.TrimSpace(os.Getenv("ADMIN_EMAIL"))),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
		CookieSecure:  os.Getenv("COOKIE_SECURE") == "true",
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	var err error
	c.PluginTimeout, err = time.ParseDuration(getenv("PLUGIN_TIMEOUT", "45s"))
	if err != nil || c.PluginTimeout <= 0 {
		return c, errors.New("invalid PLUGIN_TIMEOUT")
	}
	c.UploadDir, err = filepath.Abs(c.UploadDir)
	if err != nil {
		return c, err
	}
	c.WebDist, err = filepath.Abs(c.WebDist)
	if err != nil {
		return c, err
	}
	return c, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func OpenDB(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 20
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// A transaction-level advisory lock prevents two API/worker processes migrating together.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(88012001)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	var applied bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = 1)").Scan(&applied); err != nil {
		return err
	}
	if !applied {
		sql, err := migrations.ReadFile("migrations/001_init.up.sql")
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(sql), pgx.QueryExecModeSimpleProtocol); err != nil {
			return fmt.Errorf("migration 1: %w", err)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES (1)"); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func SeedAdmin(ctx context.Context, pool *pgxpool.Pool, email, password string) error {
	if email == "" && password == "" {
		return nil
	}
	if email == "" || password == "" {
		return errors.New("ADMIN_EMAIL and ADMIN_PASSWORD must both be set")
	}
	if len(password) < 12 {
		return errors.New("ADMIN_PASSWORD must be at least 12 characters")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	var role string
	err = pool.QueryRow(ctx, `INSERT INTO users(email, display_name, password_hash, role)
        VALUES ($1, 'Administrator', $2, 'admin')
        ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
        RETURNING role`, email, hash).Scan(&role)
	if err != nil {
		return err
	}
	if role != "admin" {
		return errors.New("ADMIN_EMAIL belongs to a student account")
	}
	return nil
}

func RandomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func TokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return base64.RawStdEncoding.EncodeToString(salt) + ":" + base64.RawStdEncoding.EncodeToString(key), nil
}

func CheckPassword(encoded, password string) bool {
	parts := strings.Split(encoded, ":")
	if len(parts) != 2 {
		return false
	}
	salt, e1 := base64.RawStdEncoding.DecodeString(parts[0])
	expected, e2 := base64.RawStdEncoding.DecodeString(parts[1])
	if e1 != nil || e2 != nil || len(salt) != 16 || len(expected) != 32 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

type SavedFile struct {
	Key    string
	Name   string
	SHA256 string
	Size   int64
	Path   string
}

func SaveUpload(dir, kind string, header *multipart.FileHeader) (SavedFile, error) {
	var out SavedFile
	var limit int64
	switch kind {
	case "resume":
		limit = 10 << 20
	case "package":
		limit = 50 << 20
	default:
		return out, errors.New("kind must be resume or package")
	}
	if header.Size <= 0 || header.Size > limit {
		return out, errors.New("file is empty or too large")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return out, err
	}
	src, err := header.Open()
	if err != nil {
		return out, err
	}
	defer src.Close()
	token, err := RandomToken()
	if err != nil {
		return out, err
	}
	key := hex.EncodeToString(TokenHash(token))
	dst, err := os.OpenFile(filepath.Join(dir, key), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return out, err
	}
	cleanup := true
	defer func() {
		dst.Close()
		if cleanup {
			os.Remove(dst.Name())
		}
	}()
	digest := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, digest), io.LimitReader(src, limit+1))
	if err != nil {
		return out, err
	}
	if n == 0 || n > limit {
		return out, errors.New("file is empty or too large")
	}
	if err := dst.Close(); err != nil {
		return out, err
	}
	f, err := os.Open(dst.Name())
	if err != nil {
		return out, err
	}
	magic := make([]byte, 8)
	count, _ := f.Read(magic)
	f.Close()
	if kind == "resume" {
		if count < 5 || string(magic[:5]) != "%PDF-" {
			return out, errors.New("resume must be a PDF")
		}
	} else {
        if count < 4 || string(magic[:4]) != "PK\x03\x04" {
			return out, errors.New("package must be a ZIP")
		}
		if err := validateZIP(dst.Name()); err != nil {
			return out, err
		}
	}
	name := filepath.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	if name == "" || name == "." {
		name = kind
	}
	out = SavedFile{Key: key, Name: name, SHA256: hex.EncodeToString(digest.Sum(nil)), Size: n, Path: dst.Name()}
	cleanup = false
	return out, nil
}

func validateZIP(filename string) error {
	r, err := zip.OpenReader(filename)
	if err != nil {
		return errors.New("invalid ZIP package")
	}
	defer r.Close()
	if len(r.File) == 0 || len(r.File) > 1000 {
		return errors.New("ZIP entry count out of range")
	}
	var total uint64
	for _, f := range r.File {
		clean := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(f.Name, "\\") {
			return errors.New("ZIP contains unsafe path")
		}
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return errors.New("ZIP symlinks are not allowed")
		}
		total += f.UncompressedSize64
		if total > 200<<20 {
			return errors.New("ZIP expands beyond limit")
		}
		if f.UncompressedSize64 > 1<<20 && f.UncompressedSize64 > 100*(f.CompressedSize64+1) {
			return errors.New("ZIP compression ratio exceeds limit")
		}
	}
	return nil
}
