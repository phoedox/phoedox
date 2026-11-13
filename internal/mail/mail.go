// Package mail sends transactional emails (invitations, password resets).
// Without SMTP configuration it is disabled and callers fall back to links
// that an admin copies by hand.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/phoedox/phoedox/internal/graph"
)

// Message is a plain-text email.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Sender delivers messages.
type Sender interface {
	Enabled() bool
	Send(ctx context.Context, m Message) error
}

// Disabled is used when no SMTP server is configured.
type Disabled struct{}

func (Disabled) Enabled() bool                         { return false }
func (Disabled) Send(context.Context, Message) error { return errors.New("mail: not configured") }

// TLSMode selects how the SMTP connection is secured.
type TLSMode string

const (
	StartTLS TLSMode = "starttls" // port 587 (default)
	Implicit TLSMode = "tls"      // port 465
	None     TLSMode = "none"     // only for local relays
)

// SMTP sends through an SMTP server.
type SMTP struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string // "Phoedox <noreply@example.org>"
	TLS      TLSMode
	Timeout  time.Duration
}

func (s *SMTP) Enabled() bool { return true }

// Send delivers one message. The connection is opened per message; volume
// is low (invitations, resets).
func (s *SMTP) Send(ctx context.Context, m Message) error {
	if strings.ContainsAny(m.To, "\r\n") || strings.ContainsAny(m.Subject, "\r\n") {
		return errors.New("mail: invalid header value")
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	dialer := &net.Dialer{Timeout: timeout}
	var conn net.Conn
	var err error
	if s.TLS == Implicit {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("mail: connect: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("mail: hello: %w", err)
	}
	defer c.Close()
	if s.TLS == StartTLS || s.TLS == "" {
		if err := c.StartTLS(&tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("mail: starttls: %w", err)
		}
	}
	if s.User != "" {
		if err := c.Auth(smtp.PlainAuth("", s.User, s.Password, s.Host)); err != nil {
			return fmt.Errorf("mail: auth: %w", err)
		}
	}
	from := addressOf(s.From)
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("mail: from: %w", err)
	}
	if err := c.Rcpt(m.To); err != nil {
		return fmt.Errorf("mail: rcpt: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(Render(s.From, m)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// Render builds the RFC 5322 message with UTF-8 subject and body.
func Render(from string, m Message) []byte {
	var b strings.Builder
	domain := "phoedox"
	if i := strings.LastIndex(addressOf(from), "@"); i >= 0 {
		domain = addressOf(from)[i+1:]
	}
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", m.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", m.Subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@%s>\r\n", strings.ToLower(string(graph.NewID())), domain)
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\nAuto-Submitted: auto-generated\r\n\r\n")
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(m.Body, "\r\n", "\n"), "\n", "\r\n"))
	b.WriteString("\r\n")
	return []byte(b.String())
}

// addressOf extracts the address from "Name <addr>".
func addressOf(s string) string {
	if i, j := strings.LastIndex(s, "<"), strings.LastIndex(s, ">"); i >= 0 && j > i {
		return s[i+1 : j]
	}
	return strings.TrimSpace(s)
}

// Capture records messages instead of sending them (tests, dry runs).
type Capture struct {
	mu   sync.Mutex
	Sent []Message
}

func (c *Capture) Enabled() bool { return true }

func (c *Capture) Send(_ context.Context, m Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Sent = append(c.Sent, m)
	return nil
}

// Last returns the most recent message.
func (c *Capture) Last() (Message, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.Sent) == 0 {
		return Message{}, false
	}
	return c.Sent[len(c.Sent)-1], true
}
