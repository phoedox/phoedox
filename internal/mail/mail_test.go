package mail

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	b := string(Render("Phoedox <noreply@example.org>", Message{To: "a@b.de", Subject: "Einladung für Jörg", Body: "Hallo\nWelt"}))
	for _, want := range []string{"From: Phoedox <noreply@example.org>\r\n", "To: a@b.de\r\n", "Subject: =?utf-8?q?", "@example.org>\r\n", "charset=utf-8", "\r\n\r\nHallo\r\nWelt\r\n"} {
		if !strings.Contains(b, want) {
			t.Fatalf("missing %q in\n%s", want, b)
		}
	}
	if addressOf("Phoedox <x@y.z>") != "x@y.z" || addressOf(" x@y.z ") != "x@y.z" {
		t.Fatal("addressOf")
	}
}

// fakeSMTP accepts one message and returns what it received.
func fakeSMTP(t *testing.T) (addr string, got chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	got = make(chan string, 1)
	go func() {
		defer ln.Close()
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		w := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
		w("220 fake ESMTP")
		var data strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if inData {
				if line == ".\r\n" {
					inData = false
					w("250 queued")
					continue
				}
				data.WriteString(line)
				continue
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				w("250-fake")
				w("250 AUTH PLAIN")
			case strings.HasPrefix(cmd, "AUTH PLAIN"):
				w("235 ok")
			case strings.HasPrefix(cmd, "MAIL FROM:<NOREPLY@EXAMPLE.ORG>"), strings.HasPrefix(cmd, "RCPT TO:<A@B.DE>"):
				w("250 ok")
			case cmd == "DATA":
				inData = true
				w("354 go")
			case cmd == "QUIT":
				w("221 bye")
				got <- data.String()
				return
			default:
				w("500 unexpected " + cmd)
			}
		}
	}()
	return ln.Addr().String(), got
}

func TestSMTPSend(t *testing.T) {
	addr, got := fakeSMTP(t)
	host, port, _ := net.SplitHostPort(addr)
	p, _ := strconv.Atoi(port)
	// PLAIN auth over an unencrypted connection is only allowed to localhost.
	s := &SMTP{Host: host, Port: p, User: "u", Password: "p", From: "Phoedox <noreply@example.org>", TLS: None}
	if err := s.Send(context.Background(), Message{To: "a@b.de", Subject: "Hallo", Body: "Text"}); err != nil {
		t.Fatal(err)
	}
	if body := <-got; !strings.Contains(body, "Subject: =?utf-8?q?Hallo?=") && !strings.Contains(body, "Subject: Hallo") || !strings.Contains(body, "\r\nText\r\n") {
		t.Fatalf("data: %q", body)
	}
	if err := s.Send(context.Background(), Message{To: "a@b.de\r\nBcc: x@y.z", Subject: "x"}); err == nil {
		t.Fatal("header injection accepted")
	}
}
