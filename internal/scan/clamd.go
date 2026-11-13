// Package scan checks uploads for malware with ClamAV (clamd, INSTREAM).
package scan

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

// ErrInfected is returned with the signature name for infected content.
type ErrInfected struct{ Signature string }

func (e ErrInfected) Error() string { return "infected: " + e.Signature }

// Scanner checks a file.
type Scanner interface {
	Scan(ctx context.Context, path string) error
}

// Clamd talks to clamd over TCP ("host:port") or a Unix socket ("unix:/path").
type Clamd struct {
	Addr    string
	Timeout time.Duration
}

func (c Clamd) dial(ctx context.Context) (net.Conn, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	if p, ok := strings.CutPrefix(c.Addr, "unix:"); ok {
		return d.DialContext(ctx, "unix", p)
	}
	return d.DialContext(ctx, "tcp", c.Addr)
}

// Scan streams the file to clamd. It returns ErrInfected or another error
// if the scan could not be completed (callers should then refuse the file).
func (c Clamd) Scan(ctx context.Context, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	conn, err := c.dial(ctx)
	if err != nil {
		return fmt.Errorf("clamd: %w", err)
	}
	defer conn.Close()
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write([]byte("zINSTREAM\x00")); err != nil {
		return fmt.Errorf("clamd: %w", err)
	}
	buf := make([]byte, 64<<10)
	size := make([]byte, 4)
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			binary.BigEndian.PutUint32(size, uint32(n))
			if _, err := conn.Write(size); err != nil {
				return fmt.Errorf("clamd: %w", err)
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return fmt.Errorf("clamd: %w", err)
			}
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return fmt.Errorf("clamd: %w", err)
	}
	reply, err := bufio.NewReader(conn).ReadString(0)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("clamd: %w", err)
	}
	return parseReply(strings.TrimRight(reply, "\x00\n"))
}

// parseReply reads "stream: OK" or "stream: Eicar-Signature FOUND".
func parseReply(r string) error {
	_, res, _ := strings.Cut(r, ": ")
	switch {
	case res == "OK":
		return nil
	case strings.HasSuffix(res, " FOUND"):
		return ErrInfected{Signature: strings.TrimSuffix(res, " FOUND")}
	}
	return fmt.Errorf("clamd: unexpected reply %q", r)
}
