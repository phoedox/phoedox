package core

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"regexp"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// Reports by e-mail: a portal gets a secret inbound address
// (<instance>/api/v1/feedback/inbound/<key>). The mail provider or the
// owner's mail server posts each message there as raw MIME (Mailgun
// "body-mime", Postmark "RawEmail", Cloudflare Email Workers, a Postfix
// pipe with curl). The subject becomes the title, the text the report,
// attachments follow the portal's rules, and answers go back to the
// sender. Automatic mails (out of office, bounces, lists) are ignored.

// ErrIgnoredMail is returned for automatic mails that make no report.
var ErrIgnoredMail = errors.New("feedback: automatic mail ignored")

const maxMailBytes = 30 << 20

type parsedMail struct {
	From, Name, Subject, Text string
	Lang                      string
	Files                     []ReportFile
}

var (
	mailTagRe  = regexp.MustCompile(`(?s)<(script|style)[^>]*>.*?</(script|style)>|<[^>]+>`)
	blankRe    = regexp.MustCompile(`\n{3,}`)
	replyCutRe = regexp.MustCompile(`(?m)^(-- ?$|_{5,}|-----Original Message-----|On .{4,200} wrote:$|Am .{4,200} schrieb .{1,200}:$)`)
)

// automatic tells mails that must never become reports (loops).
func automatic(h mail.Header) bool {
	if v := strings.ToLower(h.Get("Auto-Submitted")); v != "" && v != "no" {
		return true
	}
	switch strings.ToLower(h.Get("Precedence")) {
	case "bulk", "junk", "list", "auto_reply":
		return true
	}
	if h.Get("List-Id") != "" || h.Get("X-Autoreply") != "" || h.Get("X-Autorespond") != "" {
		return true
	}
	from := strings.ToLower(h.Get("From"))
	return strings.Contains(from, "mailer-daemon") || strings.Contains(from, "postmaster@")
}

func decodePart(enc string, r io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, &newlineSkipper{r: r})
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	}
	return r
}

// newlineSkipper drops line breaks so base64 bodies decode.
type newlineSkipper struct{ r io.Reader }

func (n *newlineSkipper) Read(p []byte) (int, error) {
	buf := make([]byte, len(p))
	k, err := n.r.Read(buf)
	j := 0
	for _, b := range buf[:k] {
		if b != '\r' && b != '\n' && b != ' ' && b != '\t' {
			p[j] = b
			j++
		}
	}
	if j == 0 && err == nil && k > 0 {
		return n.Read(p)
	}
	return j, err
}

func htmlToText(s string) string {
	s = strings.NewReplacer("<br>", "\n", "<br/>", "\n", "<br />", "\n", "</p>", "\n\n", "</div>", "\n", "</li>", "\n").Replace(s)
	return html.UnescapeString(mailTagRe.ReplaceAllString(s, ""))
}

// walk collects text and attachments of a MIME part.
func (m *parsedMail) walk(ctype string, header map[string][]string, body io.Reader, depth int) {
	get := func(k string) string {
		if v := header[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	mt, params, err := mime.ParseMediaType(ctype)
	if err != nil {
		mt = "text/plain"
	}
	disp, dparams, _ := mime.ParseMediaType(get("Content-Disposition"))
	name := dparams["filename"]
	if name == "" {
		name = params["name"]
	}
	if dec, err := new(mime.WordDecoder).DecodeHeader(name); err == nil {
		name = dec
	}
	if strings.HasPrefix(mt, "multipart/") && depth < 6 {
		mr := multipart.NewReader(body, params["boundary"])
		var plain, rich string
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			sub := &parsedMail{}
			sub.walk(p.Header.Get("Content-Type"), p.Header, p, depth+1)
			m.Files = append(m.Files, sub.Files...)
			if sub.Text != "" {
				if strings.HasPrefix(strings.ToLower(p.Header.Get("Content-Type")), "text/html") {
					rich += sub.Text
				} else {
					plain += sub.Text
				}
			}
		}
		// multipart/alternative: the plain text if there is one.
		if plain != "" {
			m.Text += plain
		} else {
			m.Text += rich
		}
		return
	}
	data, _ := io.ReadAll(io.LimitReader(decodePart(get("Content-Transfer-Encoding"), body), maxMailBytes))
	if disp == "attachment" || (name != "" && !strings.HasPrefix(mt, "text/")) {
		if name == "" {
			name = "attachment"
		}
		if len(m.Files) < 20 {
			m.Files = append(m.Files, ReportFile{Name: excerpt(name, 200), R: bytes.NewReader(data)})
		}
		return
	}
	switch mt {
	case "text/plain":
		m.Text += string(data)
	case "text/html":
		m.Text += htmlToText(string(data))
	}
}

func parseMail(r io.Reader) (*parsedMail, bool, error) {
	msg, err := mail.ReadMessage(io.LimitReader(r, maxMailBytes))
	if err != nil {
		return nil, false, invalid("validation.feedback_mail", nil, "not an e-mail message")
	}
	if automatic(msg.Header) {
		return nil, true, nil
	}
	m := &parsedMail{}
	if addr, err := mail.ParseAddress(msg.Header.Get("From")); err == nil {
		m.From, m.Name = strings.ToLower(addr.Address), addr.Name
	}
	m.Subject, _ = new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	m.Lang = strings.ToLower(strings.TrimSpace(msg.Header.Get("Content-Language")))
	ctype := msg.Header.Get("Content-Type")
	if ctype == "" {
		ctype = "text/plain"
	}
	m.walk(ctype, msg.Header, msg.Body, 0)
	text := strings.ReplaceAll(m.Text, "\r\n", "\n")
	if loc := replyCutRe.FindStringIndex(text); loc != nil && loc[0] > 0 {
		text = text[:loc[0]] // quoted replies and signatures
	}
	m.Text = strings.TrimSpace(blankRe.ReplaceAllString(text, "\n\n"))
	m.Subject = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(m.Subject), " "))
	for _, p := range []string{"re:", "aw:", "fw:", "fwd:", "wg:"} {
		if strings.HasPrefix(strings.ToLower(m.Subject), p) {
			m.Subject = strings.TrimSpace(m.Subject[len(p):])
		}
	}
	return m, false, nil
}

// SubmitFeedbackMail turns an inbound e-mail into a report of the portal
// the key belongs to.
func (s *Service) SubmitFeedbackMail(ctx context.Context, key string, raw io.Reader) (ReportReceipt, error) {
	handle, tenant, err := s.DB.FeedbackPortalByEmailKey(ctx, hashSecret(strings.TrimSpace(key)))
	if err != nil {
		return ReportReceipt{}, ErrNotFound
	}
	ctx = store.WithTenant(ctx, tenant)
	m, ignored, err := parseMail(raw)
	if err != nil {
		return ReportReceipt{}, err
	}
	if ignored {
		return ReportReceipt{}, ErrIgnoredMail
	}
	portal, err := s.DB.FeedbackPortalByHandle(ctx, handle)
	if err != nil {
		return ReportReceipt{}, err
	}
	ps := portalSettings(portal)
	title := m.Subject
	if title == "" {
		title, _, _ = strings.Cut(m.Text, "\n")
	}
	if title == "" {
		title = "(no subject)"
	}
	lang := ""
	if len(m.Lang) >= 2 {
		lang = m.Lang[:2]
	}
	in := ReportInput{Type: ps.Types[0], Title: excerpt(title, 200), Body: excerpt(m.Text, 20_000), Email: m.From, Lang: lang,
		Context: map[string]string{"via": "e-mail"}}
	if m.Name != "" {
		in.Context["sender_name"] = excerpt(m.Name, 100)
	}
	rec, err := s.SubmitFeedback(ctx, nil, handle, in, m.Files, Intake{Via: "email"})
	var inv *InvalidError
	if err != nil && len(m.Files) > 0 && errors.As(err, &inv) && fileProblem(inv.Key) {
		// Attachments the portal does not take: the report without them.
		in.Context["attachments_dropped"] = "yes"
		return s.SubmitFeedback(ctx, nil, handle, in, nil, Intake{Via: "email"})
	}
	return rec, err
}

func fileProblem(key string) bool {
	switch key {
	case "validation.feedback_file_text", "validation.feedback_file_type", "validation.feedback_files", "validation.file_empty",
		"validation.file_too_large", "validation.feedback_storage_full":
		return true
	}
	return false
}
