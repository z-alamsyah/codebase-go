// Package mailer contains email senders. LogMailer only simulates sending by
// writing a log line; replace it with an SMTP or email API client.
package mailer

import (
	"context"
	"log/slog"
)

type LogMailer struct {
	log *slog.Logger
}

func NewLogMailer(log *slog.Logger) *LogMailer {
	return &LogMailer{log: log}
}

func (m *LogMailer) SendWelcomeEmail(ctx context.Context, to, name string) error {
	m.log.InfoContext(ctx, "welcome email sent (simulated)", slog.String("to", to), slog.String("name", name))
	return nil
}
