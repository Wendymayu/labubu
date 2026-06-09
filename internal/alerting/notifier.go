package alerting

import (
	"fmt"
	"net/smtp"
	"strings"
	"time"
)

// --- EmailNotifier ---

// EmailNotifier sends email notifications via SMTP.
type EmailNotifier struct {
	cfg NotifierConfig
}

// NewEmailNotifier creates a new EmailNotifier from config.
func NewEmailNotifier(cfg NotifierConfig) *EmailNotifier {
	return &EmailNotifier{cfg: cfg}
}

// Type returns "email".
func (e *EmailNotifier) Type() string {
	return "email"
}

// Fire sends a firing notification.
func (e *EmailNotifier) Fire(rule Rule, state AlertState, trace TraceData) error {
	msg := formatFireEmail(rule, state, trace)
	return e.send(msg)
}

// Resolve sends a resolved notification.
func (e *EmailNotifier) Resolve(rule Rule, state AlertState, trace TraceData) error {
	msg := formatResolveEmail(rule, state, trace)
	return e.send(msg)
}

func (e *EmailNotifier) send(msg EmailMessage) error {
	addr := fmt.Sprintf("%s:%d", e.cfg.SMTPHost, e.cfg.SMTPPort)
	var auth smtp.Auth
	if e.cfg.Username != "" && e.cfg.Password != "" {
		auth = smtp.PlainAuth("", e.cfg.Username, e.cfg.Password, e.cfg.SMTPHost)
	}

	header := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n",
		e.cfg.Username,
		strings.Join(e.cfg.Recipients, ", "),
		msg.Subject,
	)
	body := header + msg.Body

	return smtp.SendMail(addr, auth, e.cfg.Username, e.cfg.Recipients, []byte(body))
}

// EmailMessage holds the subject and body for an email.
type EmailMessage struct {
	Subject string
	Body    string
}

func formatFireEmail(rule Rule, state AlertState, trace TraceData) EmailMessage {
	return EmailMessage{
		Subject: fmt.Sprintf(`[LABUBU ALERT] Rule "%s" is firing`, rule.Name),
		Body: fmt.Sprintf(`Rule: %s
Trace: %s
Model: %s
Total Tokens: %s
Threshold: check conditions in rule configuration
Triggered at: %s

View details: http://localhost:8080/traces/%s
`,
			rule.Name,
			state.TraceIDHex,
			trace.Model,
			formatNumber(uint64(trace.TotalTokens)),
			state.TriggeredAt.Format("2006-01-02 15:04:05"),
			state.TraceIDHex,
		),
	}
}

func formatResolveEmail(rule Rule, state AlertState, trace TraceData) EmailMessage {
	resolvedAt := time.Now()
	if state.ResolvedAt != nil {
		resolvedAt = *state.ResolvedAt
	}
	return EmailMessage{
		Subject: fmt.Sprintf(`[LABUBU RESOLVED] Rule "%s" recovered`, rule.Name),
		Body: fmt.Sprintf(`Rule: %s
Trace: %s
Resolved at: %s
`,
			rule.Name,
			state.TraceIDHex,
			resolvedAt.Format("2006-01-02 15:04:05"),
		),
	}
}

func formatNumber(n uint64) string {
	s := fmt.Sprintf("%d", n)
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}
