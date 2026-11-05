package platform

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// SendVerificationEmail delivers a one-time registration code. In development
// mode the code is logged and no SMTP connection is made.
func SendVerificationEmail(cfg Config, to, code string) error {
	if cfg.EmailDevMode {
		slog.Info("email verification code (dev mode)", "email", to, "code", code)
		return nil
	}
	if cfg.SMTPHost == "" {
		return errors.New("SMTP is not configured")
	}
	toAddr, err := mail.ParseAddress(to)
	if err != nil {
		return errors.New("invalid recipient address")
	}
	fromAddr, err := mail.ParseAddress(cfg.SMTPFrom)
	if err != nil {
		return errors.New("invalid SMTP_FROM")
	}
	message := verificationMessage(fromAddr, toAddr, code)
	address := net.JoinHostPort(cfg.SMTPHost, strconv.Itoa(cfg.SMTPPort))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	tlsConfig := &tls.Config{ServerName: cfg.SMTPHost, MinVersion: tls.VersionTLS12}
	var conn net.Conn
	if cfg.SMTPPort == 465 {
		conn, err = tls.DialWithDialer(dialer, "tcp", address, tlsConfig)
	} else {
		conn, err = dialer.Dial("tcp", address)
	}
	if err != nil {
		return fmt.Errorf("connect SMTP: %w", err)
	}
	defer conn.Close()
	if err = conn.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, cfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("SMTP client: %w", err)
	}
	defer client.Close()
	if cfg.SMTPPort != 465 {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err = client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("SMTP STARTTLS: %w", err)
			}
		}
	}
	if cfg.SMTPUsername != "" {
		if err = client.Auth(smtp.PlainAuth("", cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPHost)); err != nil {
			return fmt.Errorf("SMTP auth: %w", err)
		}
	}
	if err = client.Mail(fromAddr.Address); err != nil {
		return fmt.Errorf("SMTP MAIL: %w", err)
	}
	if err = client.Rcpt(toAddr.Address); err != nil {
		return fmt.Errorf("SMTP RCPT: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA: %w", err)
	}
	if _, err = writer.Write([]byte(message)); err != nil {
		return fmt.Errorf("SMTP body: %w", err)
	}
	if err = writer.Close(); err != nil {
		return fmt.Errorf("SMTP body: %w", err)
	}
	if err = client.Quit(); err != nil {
		return fmt.Errorf("SMTP QUIT: %w", err)
	}
	return nil
}

func verificationMessage(from, to *mail.Address, code string) string {
	body := "Birkenwald 招新系统\r\n\r\n" +
		"你的邮箱验证码：" + code + "\r\n" +
		"10 分钟内有效，请勿转发给他人。\r\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	headers := []string{
		"From: " + from.String(),
		"To: " + to.String(),
		"Subject: " + mime.QEncoding.Encode("UTF-8", "Birkenwald 招新系统验证码"),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: base64",
	}
	return strings.Join(headers, "\r\n") + "\r\n\r\n" + encoded + "\r\n"
}
