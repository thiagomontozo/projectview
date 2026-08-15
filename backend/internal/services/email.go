// Package services contains cross-cutting application services: outbound
// email, in-app/notification delivery, and the deadline alert scheduler.
package services

import (
	"fmt"
	"net/smtp"
	"strings"

	"projectview/internal/config"
	"projectview/internal/logger"
)

type Mailer struct {
	cfg *config.Config
}

func NewMailer(cfg *config.Config) *Mailer {
	return &Mailer{cfg: cfg}
}

// Send delivers an internal e-mail. When SMTP is disabled it just logs the
// message so the rest of the app keeps working in environments without
// mail access (e.g. local development).
func (m *Mailer) Send(to, subject, htmlBody string) error {
	// Named so it does not shadow net/smtp, which this function also uses.
	settings := m.cfg.SMTP()

	if !settings.Enabled {
		logger.Info("[email disabled] Would send %q to %s", subject, to)
		return nil
	}

	from := settings.FromAddress
	fromAddr := from
	if idx := strings.LastIndex(from, "<"); idx != -1 {
		fromAddr = strings.TrimSuffix(from[idx+1:], ">")
	}

	addr := fmt.Sprintf("%s:%d", settings.Host, settings.Port)

	msg := buildMessage(from, to, subject, htmlBody)

	var auth smtp.Auth
	if settings.User != "" {
		auth = smtp.PlainAuth("", settings.User, settings.Password, settings.Host)
	}

	if err := smtp.SendMail(addr, auth, fromAddr, []string{to}, msg); err != nil {
		logger.Error("failed to send email to %s: %v", to, err)
		return err
	}
	logger.Info("Email sent to %s: %s", to, subject)
	return nil
}

func buildMessage(from, to, subject, htmlBody string) []byte {
	headers := map[string]string{
		"From":         from,
		"To":           to,
		"Subject":      subject,
		"MIME-Version": "1.0",
		"Content-Type": "text/html; charset=\"UTF-8\"",
	}
	var b strings.Builder
	for k, v := range headers {
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")
	b.WriteString(htmlBody)
	return []byte(b.String())
}
