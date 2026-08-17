package services

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"projectview/internal/logger"
	"projectview/internal/repo"
)

// DigestScheduler sends periodic summaries of unread notifications.
//
// A digest exists so that someone can turn immediate e-mail off without going
// blind: the notifications still accumulate in the app, and once a day (or
// week) they arrive as one message instead of forty. It is also where mail
// suppressed by quiet hours resurfaces.
type DigestScheduler struct {
	Guard *SweepGuard

	preferences *repo.Preferences
	mailer      *Mailer
}

func NewDigestScheduler(preferences *repo.Preferences, mailer *Mailer) *DigestScheduler {
	return &DigestScheduler{preferences: preferences, mailer: mailer}
}

// Start runs the sweep every hour. Hourly rather than at a fixed time because
// each person picks the hour their digest arrives, and the query selects whose
// hour it currently is.
func (s *DigestScheduler) Start() {
	c := cron.New()
	if _, err := c.AddFunc("5 * * * *", func() {
		s.Guard.Do(context.Background(), "Digests", func(ctx context.Context) {
			if err := s.Run(ctx); err != nil {
				logger.Error("digest sweep failed: %v", err)
			}
		})
	}); err != nil {
		logger.Error("invalid digest cron expression: %v", err)
		return
	}
	c.Start()
	logger.Info("Notification digest scheduler started")
}

func (s *DigestScheduler) Run(ctx context.Context) error {
	recipients, err := s.preferences.DueForDigest(ctx, time.Now())
	if err != nil {
		return err
	}

	sent := 0
	for _, recipient := range recipients {
		if len(recipient.Pending) == 0 {
			continue
		}

		subject := fmt.Sprintf("%d update(s) waiting in ProjectView", len(recipient.Pending))
		if err := s.mailer.Send(recipient.Email, subject, digestBody(recipient)); err != nil {
			logger.Error("digest to %s failed: %v", recipient.Email, err)
			continue
		}

		// Recorded only after a successful send, so a mail failure means the
		// next sweep retries rather than silently skipping the window.
		if err := s.preferences.MarkDigestSent(ctx, recipient.UserID); err != nil {
			logger.Error("could not record digest for %s: %v", recipient.UserID, err)
		}
		sent++
	}

	if sent > 0 {
		logger.Info("Sent %d notification digest(s)", sent)
	}
	return nil
}

// digestBody renders the summary. Every value is escaped: notification titles
// carry task names, which are user input.
func digestBody(recipient repo.DigestRecipient) string {
	var b strings.Builder

	fmt.Fprintf(&b, "<p>Hello %s,</p>", html.EscapeString(recipient.Name))
	fmt.Fprintf(&b, "<p>You have %d unread update(s):</p><ul>", len(recipient.Pending))

	// Long lists are trimmed: a digest that scrolls forever is as unread as
	// the notifications it replaced.
	const maxItems = 20
	for i, notification := range recipient.Pending {
		if i == maxItems {
			fmt.Fprintf(&b, "<li>… and %d more.</li>", len(recipient.Pending)-maxItems)
			break
		}
		fmt.Fprintf(&b, "<li><strong>%s</strong>", html.EscapeString(notification.Title))
		if notification.Body != "" {
			fmt.Fprintf(&b, " — %s", html.EscapeString(notification.Body))
		}
		b.WriteString("</li>")
	}

	b.WriteString("</ul><p>Open ProjectView to see them in context.</p>")
	return b.String()
}
