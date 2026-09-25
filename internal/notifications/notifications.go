package notifications

// Channel constants. Phase 6 adds whatsapp and push to the Phase 1 set.
// WhatsApp requires explicit user consent before any message is sent.
const (
	ChannelInApp    = "inapp"
	ChannelEmail    = "email"
	ChannelWhatsApp = "whatsapp"
	ChannelPush     = "push"
)

// EmailSender is the abstraction boundary. Phase 1 logs; production injects SMTP/SES.
type EmailSender interface {
	Send(to, subject, body string) error
}

// LogSender writes to server logs only. No credentials, no PII beyond address.
type LogSender struct{}

func (LogSender) Send(to, subject, body string) error { return nil }
