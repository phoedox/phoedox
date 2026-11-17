package core

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/store"
)

func TestAppDataLikeArtifacts(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, stranger := setup(t)
	dir := t.TempDir()
	s.Blobs = blob.Local{Dir: filepath.Join(dir, "files")}
	s.TempDir = filepath.Join(dir, "tmp")
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Haus"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleReader)
	res, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocApp, Title: "Poll", App: &App{Level: 2, Devices: []string{"camera", "location", "camera"},
		Files: map[string]string{"index.html": "<h1>Poll</h1>"}}})
	if err != nil {
		t.Fatal(err)
	}
	doc := res.Document
	if _, err := s.CreateDocument(ctx, owner, p.Slug, DocumentInput{Type: store.DocApp, Title: "x", App: &App{Devices: []string{"nose"}}}); invalidKey(err) != "validation.app_device" {
		t.Fatalf("unknown device: %v", err)
	}

	// Nothing before the person allows the level.
	if _, err := s.AppMe(ctx, reader, p.Slug, doc.Slug); invalidKey(err) != "validation.app_not_allowed" {
		t.Fatalf("me before grant: %v", err)
	}
	for _, a := range []Actor{owner, reader} {
		if _, err := s.GrantApp(ctx, a, p.Slug, doc.Slug, 2); err != nil {
			t.Fatal(err)
		}
	}
	me, err := s.AppMe(ctx, reader, p.Slug, doc.Slug)
	if err != nil || me.Name != reader.User.Name || me.Editor || !strings.HasPrefix(me.ID, "u") || me.ID == reader.User.ID {
		t.Fatalf("me: %+v %v", me, err)
	}
	if _, err := s.AppMe(ctx, stranger, p.Slug, doc.Slug); err == nil {
		t.Fatal("stranger")
	}

	// Collections: shared records, live events, queries, merge, personal ones.
	ch, stop, err := s.WatchApp(ctx, owner, p.Slug, doc.Slug)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	for i, v := range []string{`{"choice":"pizza","n":3}`, `{"choice":"pasta","n":1}`, `{"choice":"pizza","n":2}`} {
		a := owner
		if i == 1 {
			a = reader
		}
		if _, err := s.AppAddRecord(ctx, a, p.Slug, doc.Slug, "votes", false, json.RawMessage(v)); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case c := <-ch:
		if c.Collection != "votes" || c.Kind != "add" {
			t.Fatalf("event: %+v", c)
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
	got, err := s.AppQueryRecords(ctx, reader, p.Slug, doc.Slug, "votes", false, AppQuery{Where: []AppCond{{Field: "choice", Op: "=", Value: json.RawMessage(`"pizza"`)}}, Order: "n"})
	if err != nil || len(got) != 2 || !strings.Contains(string(got[0].Data), `"n":2`) || got[0].By.Name != owner.User.Name || got[0].Mine {
		t.Fatalf("query: %v %+v", err, got)
	}
	mine, _ := s.AppQueryRecords(ctx, reader, p.Slug, doc.Slug, "votes", false, AppQuery{Mine: true})
	if len(mine) != 1 || !mine[0].Mine {
		t.Fatalf("mine: %+v", mine)
	}
	up, err := s.AppPutRecord(ctx, reader, p.Slug, doc.Slug, got[0].ID, json.RawMessage(`{"n":null,"note":"hot"}`), true)
	if err != nil || strings.Contains(string(up.Data), `"n"`) || !strings.Contains(string(up.Data), `"note":"hot"`) || !strings.Contains(string(up.Data), `"choice":"pizza"`) {
		t.Fatalf("merge: %v %s", err, up.Data)
	}
	if _, err := s.AppAddRecord(ctx, reader, p.Slug, doc.Slug, "votes", false, json.RawMessage(`[1,2]`)); invalidKey(err) != "validation.app_record" {
		t.Fatalf("not an object: %v", err)
	}
	priv, _ := s.AppAddRecord(ctx, reader, p.Slug, doc.Slug, "notes", true, json.RawMessage(`{"t":"mine"}`))
	if l, _ := s.AppQueryRecords(ctx, owner, p.Slug, doc.Slug, "notes", true, AppQuery{}); len(l) != 0 {
		t.Fatal("personal records are the owner's")
	}
	if err := s.AppDeleteRecord(ctx, owner, p.Slug, doc.Slug, priv.ID); err == nil {
		t.Fatal("deleted someone else's personal record")
	}
	if err := s.AppDeleteRecord(ctx, reader, p.Slug, doc.Slug, priv.ID); err != nil {
		t.Fatal(err)
	}

	// Files people add: in the app's storage; who added it or editors delete.
	f, err := s.AppUpload(ctx, reader, p.Slug, doc.Slug, "photo.png", bytes.NewReader(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 100)...)))
	if err != nil || f.Name != "photo.png" || !f.Mine {
		t.Fatalf("upload: %v %+v", err, f)
	}
	if l, _ := s.AppUploads(ctx, owner, p.Slug, doc.Slug); len(l) != 1 || l[0].Mine {
		t.Fatalf("uploads: %+v", l)
	}
	fc, err := s.AppUploadContent(ctx, owner, p.Slug, doc.Slug, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	fc.Reader.Close()

	// Device features: only those of the manifest, each answered on its own.
	fr, _ := s.OpenApp(ctx, reader, p.Slug, doc.Slug)
	if strings.Join(fr.Devices, ",") != "camera,location" || len(fr.DeviceGrants) != 0 {
		t.Fatalf("devices: %+v %+v", fr.Devices, fr.DeviceGrants)
	}
	if _, err := s.SetAppDevice(ctx, reader, p.Slug, doc.Slug, "microphone", true, false); invalidKey(err) != "validation.app_device_not_allowed" {
		t.Fatalf("undeclared device: %v", err)
	}
	g, err := s.SetAppDevice(ctx, reader, p.Slug, doc.Slug, "camera", true, false)
	if err != nil || !g["camera"] || len(g) != 1 {
		t.Fatalf("camera: %v %+v", err, g)
	}
	g, _ = s.SetAppDevice(ctx, reader, p.Slug, doc.Slug, "location", false, false)
	if !g["camera"] || g["location"] {
		t.Fatalf("location refused: %+v", g)
	}
	g, _ = s.SetAppDevice(ctx, reader, p.Slug, doc.Slug, "camera", false, true)
	if _, asked := g["camera"]; asked {
		t.Fatalf("forget: %+v", g)
	}

	// Apps that edit themselves: only with selfEdit, only editors.
	if _, err := s.AppSelfSave(ctx, owner, p.Slug, doc.Slug, map[string]*string{"index.html": ptr("<h1>x</h1>")}); invalidKey(err) != "validation.app_self_edit" {
		t.Fatalf("self edit off: %v", err)
	}
	_, _ = s.EditApp(ctx, owner, p.Slug, doc.Slug, nil, nil, &App{Level: 2, SelfEdit: true, Devices: []string{"camera"}})
	if _, err := s.AppSelfSave(ctx, reader, p.Slug, doc.Slug, map[string]*string{"index.html": ptr("<h1>x</h1>")}); err == nil {
		t.Fatal("readers must not save the app")
	}
	if r, err := s.AppSelfSave(ctx, owner, p.Slug, doc.Slug, map[string]*string{"index.html": ptr("<h1>Saved</h1>")}); err != nil || !r.Applied {
		t.Fatalf("self save: %v %+v", err, r)
	}

	// Assets: served on the frame, copied with the app into another project.
	if _, err := s.AddAppAsset(ctx, reader, p.Slug, doc.Slug, "logo.svg", strings.NewReader("<svg/>")); err == nil {
		t.Fatal("readers must not add assets")
	}
	if _, err := s.AddAppAsset(ctx, owner, p.Slug, doc.Slug, "logo.png", bytes.NewReader([]byte("\x89PNG\r\n\x1a\nlogo"))); err != nil {
		t.Fatal(err)
	}
	fr, _ = s.OpenApp(ctx, owner, p.Slug, doc.Slug)
	tok := strings.TrimSuffix(strings.TrimPrefix(fr.URL, "/app-frame/"), "/")
	asset, err := s.AppFrameFile(ctx, tok, "assets/logo.png", "", "")
	if err != nil || !bytes.HasSuffix(asset.Body, []byte("logo")) {
		t.Fatalf("asset: %v %q", err, asset.Body)
	}
	other, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Other"})
	cp, err := s.CopyApp(ctx, owner, p.Slug, doc.Slug, AppCopyInput{Project: other.Slug})
	if err != nil || cp.Document == nil || cp.Document.ProjectID != other.ID {
		t.Fatalf("copy: %v %+v", err, cp)
	}
	if l, _ := s.AppAssets(ctx, owner, other.Slug, cp.Document.Slug); len(l) != 1 || l[0].Path != "assets/logo.png" {
		t.Fatalf("copied assets: %+v", l)
	}
	if _, err := s.CopyApp(ctx, reader, p.Slug, doc.Slug, AppCopyInput{Project: other.Slug}); err == nil {
		t.Fatal("readers of the source may only copy into projects they write to")
	}

	// Preview pictures: editors store them; the AI reads the latest one.
	if err := s.SetAppPreview(ctx, owner, p.Slug, doc.Slug, []byte("not a png")); invalidKey(err) != "validation.app_preview" {
		t.Fatalf("bad preview: %v", err)
	}
	if _, err := s.AppPicture(ctx, owner, p.Slug, doc.Slug); invalidKey(err) != "validation.app_no_preview" {
		t.Fatalf("no picture yet: %v", err)
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 50)...)
	if err := s.SetAppPreview(ctx, owner, p.Slug, doc.Slug, png); err != nil {
		t.Fatal(err)
	}
	pic, err := s.AppPicture(ctx, reader, p.Slug, doc.Slug)
	if err != nil || !bytes.Equal(pic.PNG, png) || pic.Version != pic.Current {
		t.Fatalf("picture: %v %+v", err, pic.Version)
	}
	gal, err := s.AppGallery(ctx, reader, p.Slug)
	if err != nil || len(gal) != 1 || !gal[0].Preview || gal[0].Stale {
		t.Fatalf("gallery: %v %+v", err, gal)
	}
	// Previews are not attachments people see in the file list.
	if fs, _ := s.Files(ctx, owner, p.Slug, FileFilter{Document: doc.Slug}); len(fs) != 2 {
		t.Fatalf("files of the app (upload + asset): %d", len(fs))
	}

	// A public app: link preview, its picture, and a copy for someone else.
	sh, err := s.PublishApp(ctx, owner, p.Slug, doc.Slug, AppShareInput{Mode: "static", Schedule: "manual", Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	meta, err := s.SharedAppInfo(ctx, sh.Share.Token)
	if err != nil || meta.Title != "Poll" || !meta.Preview {
		t.Fatalf("meta: %v %+v", err, meta)
	}
	if fc, err := s.SharedAppPreview(ctx, sh.Share.Token); err != nil {
		t.Fatal(err)
	} else {
		fc.Reader.Close()
	}
	mine2, _ := s.CreateProject(ctx, stranger, ProjectInput{Name: "Mine"})
	sc, err := s.CopySharedApp(ctx, stranger, sh.Share.Token, mine2.Slug, "")
	if err != nil || sc.Document == nil || sc.Document.Title != "Poll" {
		t.Fatalf("copy shared: %v %+v", err, sc)
	}
	if l, _ := s.AppAssets(ctx, stranger, mine2.Slug, sc.Document.Slug); len(l) != 1 {
		t.Fatalf("assets of the shared copy: %+v", l)
	}
	if _, err := s.CopySharedApp(ctx, stranger, sh.Share.Token, p.Slug, ""); err == nil {
		t.Fatal("copied into a project the person cannot write to")
	}

	// Storage limit counts records and uploads.
	_, _ = s.EditApp(ctx, owner, p.Slug, doc.Slug, nil, nil, &App{Level: 2, StorageMB: 1, SelfEdit: true})
	if _, err := s.AppUpload(ctx, owner, p.Slug, doc.Slug, "big.bin", io.LimitReader(zeros{}, 2<<20)); invalidKey(err) != "validation.app_storage_full" {
		t.Fatalf("full: %v", err)
	}
}

type zeros struct{}

func (zeros) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = 0
	}
	return len(b), nil
}

func ptr(s string) *string { return &s }
