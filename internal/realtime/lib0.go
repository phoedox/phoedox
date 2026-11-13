package realtime

import (
	"errors"
	"unicode/utf8"
)

// lib0 encoding as used by Yjs and y-protocols: unsigned LEB128 integers,
// byte arrays and strings prefixed with their length.

var errShort = errors.New("realtime: message too short")

type decoder struct {
	b   []byte
	pos int
}

func (d *decoder) uint() (uint64, error) {
	var n uint64
	var shift uint
	for {
		if d.pos >= len(d.b) {
			return 0, errShort
		}
		c := d.b[d.pos]
		d.pos++
		n |= uint64(c&0x7f) << shift
		if c < 0x80 {
			return n, nil
		}
		shift += 7
		if shift > 63 {
			return 0, errors.New("realtime: varint too long")
		}
	}
}

func (d *decoder) bytes() ([]byte, error) {
	n, err := d.uint()
	if err != nil {
		return nil, err
	}
	if uint64(len(d.b)-d.pos) < n {
		return nil, errShort
	}
	out := d.b[d.pos : d.pos+int(n)]
	d.pos += int(n)
	return out, nil
}

func (d *decoder) string() (string, error) {
	b, err := d.bytes()
	if err != nil {
		return "", err
	}
	if !utf8.Valid(b) {
		return "", errors.New("realtime: invalid utf-8")
	}
	return string(b), nil
}

type encoder struct{ b []byte }

func (e *encoder) uint(n uint64) *encoder {
	for n >= 0x80 {
		e.b = append(e.b, byte(n)|0x80)
		n >>= 7
	}
	e.b = append(e.b, byte(n))
	return e
}

func (e *encoder) bytes(b []byte) *encoder {
	e.uint(uint64(len(b)))
	e.b = append(e.b, b...)
	return e
}

func (e *encoder) string(s string) *encoder { return e.bytes([]byte(s)) }

// Message types of y-websocket plus Phoedox control messages.
const (
	msgSync      = 0
	msgAwareness = 1
	msgControl   = 100 // server → client: JSON control message
	msgSnapshot  = 101 // client → server: compacted state (upto, update)

	syncStep1  = 0
	syncStep2  = 1
	syncUpdate = 2
)

// emptyUpdate is Y.encodeStateAsUpdate of an empty document; emptyState
// is an empty state vector.
var (
	emptyUpdate = []byte{0, 0}
	emptyState  = []byte{0}
)

func syncMsg(kind uint64, payload []byte) []byte {
	return (&encoder{}).uint(msgSync).uint(kind).bytes(payload).b
}

// awarenessEntry is one client's state in an awareness update.
type awarenessEntry struct {
	ClientID uint64
	Clock    uint64
	State    string // JSON, "null" when the client left
}

func decodeAwareness(b []byte) ([]awarenessEntry, error) {
	d := &decoder{b: b}
	n, err := d.uint()
	if err != nil {
		return nil, err
	}
	if n > 10000 {
		return nil, errors.New("realtime: too many awareness entries")
	}
	out := make([]awarenessEntry, 0, n)
	for i := uint64(0); i < n; i++ {
		var e awarenessEntry
		if e.ClientID, err = d.uint(); err != nil {
			return nil, err
		}
		if e.Clock, err = d.uint(); err != nil {
			return nil, err
		}
		if e.State, err = d.string(); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func encodeAwareness(entries []awarenessEntry) []byte {
	e := &encoder{}
	e.uint(uint64(len(entries)))
	for _, a := range entries {
		e.uint(a.ClientID).uint(a.Clock).string(a.State)
	}
	return e.b
}
