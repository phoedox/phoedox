package scan

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// fakeClamd answers like clamd: FOUND if the stream contains "EICAR".
func fakeClamd(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				cmd := make([]byte, 10)
				if _, err := io.ReadFull(c, cmd); err != nil || string(cmd) != "zINSTREAM\x00" {
					return
				}
				var data bytes.Buffer
				for {
					var n uint32
					if binary.Read(c, binary.BigEndian, &n) != nil {
						return
					}
					if n == 0 {
						break
					}
					_, _ = io.CopyN(&data, c, int64(n))
				}
				if bytes.Contains(data.Bytes(), []byte("EICAR")) {
					c.Write([]byte("stream: Eicar-Test-Signature FOUND\x00"))
				} else {
					c.Write([]byte("stream: OK\x00"))
				}
			}(c)
		}
	}()
	return l.Addr().String()
}

func TestClamd(t *testing.T) {
	ctx := context.Background()
	c := Clamd{Addr: fakeClamd(t)}
	dir := t.TempDir()
	clean, bad := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	_ = os.WriteFile(clean, bytes.Repeat([]byte("x"), 200<<10), 0o600)
	_ = os.WriteFile(bad, []byte("X5O!P%@AP[4\\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*"), 0o600)
	if err := c.Scan(ctx, clean); err != nil {
		t.Fatalf("clean: %v", err)
	}
	var inf ErrInfected
	if err := c.Scan(ctx, bad); !errors.As(err, &inf) || inf.Signature != "Eicar-Test-Signature" {
		t.Fatalf("infected: %v", err)
	}
	if err := (Clamd{Addr: "127.0.0.1:1"}).Scan(ctx, clean); err == nil {
		t.Fatal("unreachable clamd must fail")
	}
}
