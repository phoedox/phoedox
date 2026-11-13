package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/phoedox/phoedox/internal/graph"
	"github.com/phoedox/phoedox/internal/store"
)

type wsClient struct {
	t    *testing.T
	conn *websocket.Conn
}

func (c *wsClient) read() []byte {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, b, err := c.conn.Read(ctx)
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	return b
}

func (c *wsClient) write(b []byte) {
	c.t.Helper()
	if err := c.conn.Write(context.Background(), websocket.MessageBinary, b); err != nil {
		c.t.Fatal(err)
	}
}

// kind describes a server message for assertions.
func kind(b []byte) string {
	d := &decoder{b: b}
	typ, _ := d.uint()
	switch typ {
	case msgSync:
		k, _ := d.uint()
		payload, _ := d.bytes()
		return []string{"step1", "step2", "update"}[k] + ":" + string(payload)
	case msgAwareness:
		payload, _ := d.bytes()
		es, _ := decodeAwareness(payload)
		var parts []string
		for _, e := range es {
			parts = append(parts, e.State)
		}
		return "awareness:" + strings.Join(parts, ",")
	case msgControl:
		s, _ := d.string()
		var c Control
		_ = json.Unmarshal([]byte(s), &c)
		return "control:" + c.Type
	}
	return "?"
}

func update(s string) []byte { return syncMsg(syncUpdate, []byte(s)) }

func TestHub(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "rt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	u, _ := db.CreateUser(ctx, store.User{ID: string(graph.NewID()), Email: "a@b.de", Name: "A"})
	p, _ := db.CreateProject(ctx, store.Project{ID: string(graph.NewID()), Slug: "p", Name: "P", Short: "P", PrimaryColor: "#F26A1B", AIWriteMode: "suggest", OwnerID: u.ID})
	doc, _ := db.CreateDocument(ctx, store.Document{ID: string(graph.NewID()), ProjectID: p.ID, Type: store.DocMarkdown, Slug: "d", Title: "D", Content: store.JSON(`{}`), CreatedBy: u.ID}, "t")

	hub := NewHub(db)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hub.Serve(w, r, doc.ID, r.URL.Query().Get("name")) }))
	defer srv.Close()
	dial := func(name string) *wsClient {
		t.Helper()
		c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"?name="+name, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.CloseNow() })
		return &wsClient{t: t, conn: c}
	}
	expect := func(c *wsClient, want ...string) {
		t.Helper()
		for _, w := range want {
			got := kind(c.read())
			if !strings.HasPrefix(got, w) {
				t.Fatalf("want %q, got %q", w, got)
			}
		}
	}

	// first editor: asked for its state, told to bootstrap, then synced
	a := dial("A")
	expect(a, "step1", "control:bootstrap", "step2")
	a.write(update("UPD1"))
	// second editor: gets the log, no bootstrap
	time.Sleep(50 * time.Millisecond)
	b := dial("B")
	expect(b, "step1", "update:UPD1", "step2")
	a.write(update("UPD2"))
	expect(b, "update:UPD2")
	if ups, _ := db.DocUpdates(ctx, doc.ID); len(ups) != 2 {
		t.Fatalf("stored: %d", len(ups))
	}

	// awareness relay, replay for newcomers, cleanup on leave
	aw := (&encoder{}).uint(msgAwareness).bytes(encodeAwareness([]awarenessEntry{{ClientID: 7, Clock: 1, State: `{"name":"A"}`}})).b
	a.write(aw)
	expect(b, `awareness:{"name":"A"}`)
	c := dial("C")
	expect(c, "step1", "update:UPD1", "update:UPD2", "step2", `awareness:{"name":"A"}`)
	a.conn.Close(websocket.StatusNormalClosure, "")
	expect(b, "awareness:null")
	expect(c, "awareness:null")

	// reset: log cleared, editors told to reload
	hub.Reset(ctx, doc.ID, "MCP")
	expect(b, "control:reset")
	expect(c, "control:reset")
	if ups, _ := db.DocUpdates(ctx, doc.ID); len(ups) != 0 {
		t.Fatalf("reset must clear the log: %d", len(ups))
	}

	// a new editor after the reset bootstraps again
	d := dial("D")
	expect(d, "step1", "control:bootstrap", "step2")

	// compaction: after many updates the sender is asked for a snapshot
	var compact []byte
	for i := 0; i <= compactCount; i++ {
		d.write(update("xxxx"))
	}
	for compact == nil {
		msg := d.read()
		if strings.HasPrefix(kind(msg), "control:compact") {
			compact = msg
		}
	}
	dec := &decoder{b: compact}
	_, _ = dec.uint()
	js, _ := dec.string()
	var ctl Control
	_ = json.Unmarshal([]byte(js), &ctl)
	d.write((&encoder{}).uint(msgSnapshot).uint(uint64(ctl.Upto)).bytes([]byte("SNAPSHOT")).b)
	time.Sleep(100 * time.Millisecond)
	ups, _ := db.DocUpdates(ctx, doc.ID)
	if len(ups) == 0 || !bytes.Equal(ups[0].Data, []byte("SNAPSHOT")) || ups[0].Seq != ctl.Upto || len(ups) > compactCount/2 {
		t.Fatalf("compaction: %d updates, first %q", len(ups), ups[0].Data)
	}
	// a stray snapshot from someone not asked is ignored
	b2 := dial("E")
	expect(b2, "step1")
	b2.write((&encoder{}).uint(msgSnapshot).uint(1).bytes([]byte("EVIL")).b)
	time.Sleep(50 * time.Millisecond)
	if ups, _ := db.DocUpdates(ctx, doc.ID); bytes.Equal(ups[0].Data, []byte("EVIL")) {
		t.Fatal("unrequested snapshot accepted")
	}
}

func TestLib0(t *testing.T) {
	e := (&encoder{}).uint(0).uint(300).bytes([]byte("hé")).string("x")
	d := &decoder{b: e.b}
	if n, _ := d.uint(); n != 0 {
		t.Fatal(n)
	}
	if n, _ := d.uint(); n != 300 {
		t.Fatal(n)
	}
	if b, _ := d.bytes(); string(b) != "hé" {
		t.Fatal(string(b))
	}
	if s, _ := d.string(); s != "x" {
		t.Fatal(s)
	}
	if _, err := (&decoder{b: []byte{0x80}}).uint(); err == nil {
		t.Fatal("truncated varint accepted")
	}
	if _, err := (&decoder{b: []byte{5, 1}}).bytes(); err == nil {
		t.Fatal("short bytes accepted")
	}
}
