package server

import (
	"context"
	"crypto/sha256"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/secret"
	"github.com/phoedox/phoedox/internal/store"
)

func pow(prefix string, bits int) string {
	for i := 0; ; i++ {
		n := strconv.Itoa(i)
		h := sha256.Sum256([]byte(prefix + n))
		ok := true
		for b := 0; b < bits; b++ {
			if h[b/8]&(0x80>>(b%8)) != 0 {
				ok = false
				break
			}
		}
		if ok {
			return n
		}
	}
}

func TestBotCheckSignin(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, store.SQLite, filepath.Join(t.TempDir(), "bot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	au := &auth.Auth{DB: db}
	if _, err := au.CreateUser(ctx, "admin@coph.dev", "Admin", "sehr-geheimes-passwort", true); err != nil {
		t.Fatal(err)
	}
	svc := core.New(db)
	svc.Box, _ = secret.New(make([]byte, 32))
	svc.Bot = svc.NewBotCheck()
	c := &client{t: t, h: New(Deps{Core: svc, Auth: au})}
	good := map[string]string{"email": "admin@coph.dev", "password": "sehr-geheimes-passwort"}
	for i := 0; i < 3; i++ {
		if code, _ := c.do("POST", "/api/v1/auth/login", map[string]string{"email": "admin@coph.dev", "password": "falsch-falsch"}, false); code != 401 {
			t.Fatalf("bad login %d: %d", i, code)
		}
	}
	if code, body := c.do("POST", "/api/v1/auth/login", good, false); code != 400 || body["code"] != "bot_required" {
		t.Fatalf("after failures the check is required: %d %v", code, body)
	}
	code, ch := c.do("POST", "/api/v1/bot/challenge", map[string]string{"place": "signin"}, false)
	if code != 200 || ch["challenge"] == nil {
		t.Fatalf("challenge: %d %v", code, ch)
	}
	nonce := pow(ch["prefix"].(string), int(ch["difficulty"].(float64)))
	signals := map[string]any{"elapsedMs": 4000, "pointerMoves": 30, "keys": 10, "keyVariance": 500, "languages": 2}
	code, res := c.do("POST", "/api/v1/bot/verify", map[string]any{"challenge": ch["challenge"], "nonce": nonce, "signals": signals}, false)
	if code != 200 || res["token"] == nil {
		t.Fatalf("verify: %d %v", code, res)
	}
	with := map[string]string{"email": good["email"], "password": good["password"], "botToken": res["token"].(string)}
	if code, body := c.do("POST", "/api/v1/auth/login", with, false); code != 200 {
		t.Fatalf("login with token: %d %v", code, body)
	}
	// Admin settings: the default protects every place with "normal".
	code, st := c.do("GET", "/api/v1/admin/bot-protection", nil, false)
	if code != 200 {
		t.Fatalf("settings: %d %v", code, st)
	}
	cfg := st["config"].(map[string]any)
	if cfg["places"].(map[string]any)["signup"] != "normal" {
		t.Fatalf("default places: %v", cfg["places"])
	}
}
