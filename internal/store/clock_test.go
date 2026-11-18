package store

import (
	"testing"
	"time"
)

func TestSetClock(t *testing.T) {
	defer func() { clockShift = 0 }()
	at := time.Date(2026, 6, 15, 8, 0, 0, 0, time.UTC)
	SetClock(at)
	if d := Now().Sub(at); d < 0 || d > time.Second {
		t.Fatalf("now %v, want about %v", Now(), at)
	}
}
