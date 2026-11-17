package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/store"
)

// Apps like artifacts (Jan, 2026-11-17): who is viewing, collections with
// queries and live updates, files people add, consent per device feature
// (each one separately), sources from outside services and live values,
// apps that save themselves, assets, copies across projects and from share
// links, preview pictures and a gallery.

var appDevices = []string{"camera", "microphone", "location", "screen", "motion"}

const (
	purposeAppUpload  = "app_upload"
	purposeAppAsset   = "app_asset"
	purposeAppPreview = "app_preview"

	maxAppRecord      = 256 << 10
	maxAppCollection  = 10_000
	maxAppQueryResult = 1000
	maxAppAsset       = 25 << 20
	maxAppPreview     = 2 << 20
)

var (
	appCollectionRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,60}$`)
	appAssetNameRe  = regexp.MustCompile(`^[A-Za-z0-9_.-][A-Za-z0-9_. -]{0,99}$`)
)

// --- who is viewing ------------------------------------------------------------

// AppViewer is the person using an app, as the app sees them. The id is the
// same for this person in this app (and different in other apps).
type AppViewer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Editor bool   `json:"editor"`
}

func appViewerID(docID, userID string) string {
	if userID == "" {
		return ""
	}
	h := sha256.Sum256([]byte("phoedox-app-viewer:" + docID + ":" + userID))
	return "u" + hex.EncodeToString(h[:8])
}

// AppMe tells an app who is using it (level 1: the person allowed reading).
func (s *Service) AppMe(ctx context.Context, a Actor, ref, docRef string) (AppViewer, error) {
	p, doc, _, granted, err := s.appFor(ctx, a, ref, docRef)
	if err != nil {
		return AppViewer{}, err
	}
	if err := needLevel(granted, 1); err != nil {
		return AppViewer{}, err
	}
	return AppViewer{ID: appViewerID(doc.ID, a.User.ID), Name: a.User.Name, Editor: rank[p.Role] >= rank[store.RoleEditor] && !s.readOnly(a, p)}, nil
}

// --- collections -----------------------------------------------------------------

// AppChange tells open frames of an app that a record changed.
type AppChange struct {
	Collection string `json:"collection"`
	ID         string `json:"id"`
	Kind       string `json:"kind"` // add | update | delete
	By         string `json:"by,omitempty"`
	owner      string // personal records go only to their owner
}

// AppRecordView is a record as an app sees it.
type AppRecordView struct {
	ID        string          `json:"id"`
	Data      json.RawMessage `json:"data"`
	By        AppViewer       `json:"by"`
	Mine      bool            `json:"mine"`
	CreatedAt string          `json:"createdAt"`
	UpdatedAt string          `json:"updatedAt"`
}

// AppQuery selects records of a collection.
type AppQuery struct {
	Where  []AppCond `json:"where,omitempty"`
	Order  string    `json:"order,omitempty"` // a field, or createdAt / updatedAt
	Desc   bool      `json:"desc,omitempty"`
	Limit  int       `json:"limit,omitempty"`
	Offset int       `json:"offset,omitempty"`
	Mine   bool      `json:"mine,omitempty"` // only records the viewer wrote
}

// AppCond is one condition: field op value (op: = != < <= > >= contains in).
type AppCond struct {
	Field string          `json:"field"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value"`
}

func (s *Service) appData(ctx context.Context, a Actor, ref, docRef string, write bool) (store.Project, store.Document, App, error) {
	if write {
		if err := s.fullUser(a); err != nil {
			return store.Project{}, store.Document{}, App{}, err
		}
	}
	p, doc, app, granted, err := s.appFor(ctx, a, ref, docRef)
	if err != nil {
		return p, doc, app, err
	}
	return p, doc, app, needLevel(granted, 2)
}

func appCollection(name string) error {
	if !appCollectionRe.MatchString(name) {
		return invalid("validation.app_collection", nil, "collection names are 1–60 letters, digits and _.-")
	}
	return nil
}

func (s *Service) recordViews(ctx context.Context, a Actor, docID string, list []store.AppRecord) []AppRecordView {
	ids := []string{}
	for _, r := range list {
		if !slices.Contains(ids, r.CreatedBy) {
			ids = append(ids, r.CreatedBy)
		}
	}
	names, _ := s.DB.UserNames(ctx, ids)
	out := make([]AppRecordView, 0, len(list))
	for _, r := range list {
		out = append(out, AppRecordView{ID: r.ID, Data: json.RawMessage(r.Data), By: AppViewer{ID: appViewerID(docID, r.CreatedBy), Name: names[r.CreatedBy]},
			Mine: r.CreatedBy == a.User.ID, CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339Nano)})
	}
	return out
}

func fieldOf(data map[string]any, f string) any {
	var cur any = data
	for _, part := range strings.Split(f, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

func compareAny(x, y any) (int, bool) {
	switch a := x.(type) {
	case float64:
		if b, ok := y.(float64); ok {
			switch {
			case a < b:
				return -1, true
			case a > b:
				return 1, true
			}
			return 0, true
		}
	case string:
		if b, ok := y.(string); ok {
			return strings.Compare(a, b), true
		}
	case bool:
		if b, ok := y.(bool); ok {
			if a == b {
				return 0, true
			}
			if !a {
				return -1, true
			}
			return 1, true
		}
	}
	if x == nil && y == nil {
		return 0, true
	}
	return 0, false
}

func matches(data map[string]any, c AppCond) bool {
	var want any
	_ = json.Unmarshal(c.Value, &want)
	got := fieldOf(data, c.Field)
	switch c.Op {
	case "=", "==", "":
		n, ok := compareAny(got, want)
		return ok && n == 0
	case "!=":
		n, ok := compareAny(got, want)
		return !ok || n != 0
	case "<", "<=", ">", ">=":
		n, ok := compareAny(got, want)
		if !ok {
			return false
		}
		return map[string]bool{"<": n < 0, "<=": n <= 0, ">": n > 0, ">=": n >= 0}[c.Op]
	case "contains":
		switch g := got.(type) {
		case string:
			w, _ := want.(string)
			return strings.Contains(strings.ToLower(g), strings.ToLower(w))
		case []any:
			for _, it := range g {
				if n, ok := compareAny(it, want); ok && n == 0 {
					return true
				}
			}
		}
		return false
	case "in":
		list, _ := want.([]any)
		for _, it := range list {
			if n, ok := compareAny(got, it); ok && n == 0 {
				return true
			}
		}
		return false
	}
	return false
}

// AppQueryRecords reads records of a collection: filtered, ordered, paged.
func (s *Service) AppQueryRecords(ctx context.Context, a Actor, ref, docRef, collection string, personal bool, q AppQuery) ([]AppRecordView, error) {
	_, doc, _, err := s.appData(ctx, a, ref, docRef, false)
	if err != nil {
		return nil, err
	}
	if err := appCollection(collection); err != nil {
		return nil, err
	}
	for _, c := range q.Where {
		if !slices.Contains([]string{"", "=", "==", "!=", "<", "<=", ">", ">=", "contains", "in"}, c.Op) || c.Field == "" || len(c.Field) > 100 {
			return nil, invalid("validation.app_query", nil, "conditions are field op value with op = != < <= > >= contains in")
		}
	}
	all, err := s.DB.AppRecords(ctx, doc.ID, collection, appOwner(a, personal), maxAppCollection)
	if err != nil {
		return nil, err
	}
	type row struct {
		r    store.AppRecord
		data map[string]any
	}
	rows := make([]row, 0, len(all))
	for _, r := range all {
		if q.Mine && r.CreatedBy != a.User.ID {
			continue
		}
		var data map[string]any
		_ = json.Unmarshal([]byte(r.Data), &data)
		ok := true
		for _, c := range q.Where {
			if !matches(data, c) {
				ok = false
				break
			}
		}
		if ok {
			rows = append(rows, row{r, data})
		}
	}
	if q.Order != "" {
		sort.SliceStable(rows, func(i, j int) bool {
			var n int
			switch q.Order {
			case "createdAt":
				n = rows[i].r.CreatedAt.Compare(rows[j].r.CreatedAt.Time)
			case "updatedAt":
				n = rows[i].r.UpdatedAt.Compare(rows[j].r.UpdatedAt.Time)
			default:
				n, _ = compareAny(fieldOf(rows[i].data, q.Order), fieldOf(rows[j].data, q.Order))
			}
			if q.Desc {
				return n > 0
			}
			return n < 0
		})
	}
	limit := q.Limit
	if limit <= 0 || limit > maxAppQueryResult {
		limit = maxAppQueryResult
	}
	start := min(max(q.Offset, 0), len(rows))
	end := min(start+limit, len(rows))
	page := make([]store.AppRecord, 0, end-start)
	for _, r := range rows[start:end] {
		page = append(page, r.r)
	}
	return s.recordViews(ctx, a, doc.ID, page), nil
}

func checkRecord(data json.RawMessage) error {
	var m map[string]any
	if len(data) > maxAppRecord || json.Unmarshal(data, &m) != nil || m == nil {
		return invalid("validation.app_record", nil, "a record is a JSON object up to 256 KB")
	}
	return nil
}

func (s *Service) appRoom(ctx context.Context, p store.Project, doc store.Document, app App, add int64) error {
	used, err := s.appUsed(ctx, p, doc)
	if err != nil {
		return err
	}
	if used+add > int64(app.StorageMB)<<20 {
		return invalid("validation.app_storage_full", map[string]any{"mb": app.StorageMB}, "the app's storage is full (%d MB)", app.StorageMB)
	}
	return s.checkQuota(ctx, p, add)
}

// appUsed is what an app stores: values, records and files people added.
func (s *Service) appUsed(ctx context.Context, p store.Project, doc store.Document) (int64, error) {
	used, err := s.DB.AppStorageSize(ctx, doc.ID)
	if err != nil {
		return 0, err
	}
	ups, _ := s.DB.ListFiles(ctx, p.ID, store.FileFilter{DocumentID: doc.ID, Purpose: purposeAppUpload})
	for _, f := range ups {
		used += f.Size
	}
	return used, nil
}

func (s *Service) appChanged(docID string, c AppChange) {
	s.appFeed.publishTo(docID, c)
}

// AppAddRecord adds a record to a collection.
func (s *Service) AppAddRecord(ctx context.Context, a Actor, ref, docRef, collection string, personal bool, data json.RawMessage) (AppRecordView, error) {
	p, doc, app, err := s.appData(ctx, a, ref, docRef, true)
	if err != nil {
		return AppRecordView{}, err
	}
	if err := appCollection(collection); err != nil {
		return AppRecordView{}, err
	}
	if err := checkRecord(data); err != nil {
		return AppRecordView{}, err
	}
	if n, _ := s.DB.AppCollections(ctx, doc.ID, appOwner(a, personal)); n[collection] >= maxAppCollection {
		return AppRecordView{}, invalid("validation.app_collection_full", map[string]any{"max": maxAppCollection}, "a collection holds at most %d records", maxAppCollection)
	}
	if err := s.appRoom(ctx, p, doc, app, int64(len(data))); err != nil {
		return AppRecordView{}, err
	}
	r, err := s.DB.PutAppRecord(ctx, p.ID, store.AppRecord{ID: newID(), DocumentID: doc.ID, Collection: collection, Owner: appOwner(a, personal), Data: string(data), CreatedBy: a.User.ID})
	if err != nil {
		return AppRecordView{}, err
	}
	s.appChanged(doc.ID, AppChange{Collection: collection, ID: r.ID, Kind: "add", By: appViewerID(doc.ID, a.User.ID), owner: r.Owner})
	return s.recordViews(ctx, a, doc.ID, []store.AppRecord{r})[0], nil
}

// AppPutRecord replaces (merge = false) or merges fields into a record.
// Shared records can be changed by everyone who uses the app, personal
// ones only by their owner.
func (s *Service) AppPutRecord(ctx context.Context, a Actor, ref, docRef, id string, data json.RawMessage, merge bool) (AppRecordView, error) {
	p, doc, app, err := s.appData(ctx, a, ref, docRef, true)
	if err != nil {
		return AppRecordView{}, err
	}
	r, err := s.DB.AppRecord(ctx, doc.ID, id)
	if err != nil || (r.Owner != "" && r.Owner != a.User.ID) {
		return AppRecordView{}, ErrNotFound
	}
	if err := checkRecord(data); err != nil {
		return AppRecordView{}, err
	}
	if merge {
		var cur, patch map[string]any
		_ = json.Unmarshal([]byte(r.Data), &cur)
		_ = json.Unmarshal(data, &patch)
		if cur == nil {
			cur = map[string]any{}
		}
		for k, v := range patch {
			if v == nil {
				delete(cur, k)
			} else {
				cur[k] = v
			}
		}
		data, _ = json.Marshal(cur)
		if err := checkRecord(data); err != nil {
			return AppRecordView{}, err
		}
	}
	if err := s.appRoom(ctx, p, doc, app, int64(len(data)-r.Size)); err != nil {
		return AppRecordView{}, err
	}
	r.Data = string(data)
	if r, err = s.DB.PutAppRecord(ctx, p.ID, r); err != nil {
		return AppRecordView{}, err
	}
	s.appChanged(doc.ID, AppChange{Collection: r.Collection, ID: r.ID, Kind: "update", By: appViewerID(doc.ID, a.User.ID), owner: r.Owner})
	return s.recordViews(ctx, a, doc.ID, []store.AppRecord{r})[0], nil
}

// AppDeleteRecord removes a record.
func (s *Service) AppDeleteRecord(ctx context.Context, a Actor, ref, docRef, id string) error {
	_, doc, _, err := s.appData(ctx, a, ref, docRef, true)
	if err != nil {
		return err
	}
	r, err := s.DB.AppRecord(ctx, doc.ID, id)
	if err != nil || (r.Owner != "" && r.Owner != a.User.ID) {
		return ErrNotFound
	}
	if err := s.DB.DeleteAppRecord(ctx, doc.ID, id); err != nil {
		return err
	}
	s.appChanged(doc.ID, AppChange{Collection: r.Collection, ID: r.ID, Kind: "delete", By: appViewerID(doc.ID, a.User.ID), owner: r.Owner})
	return nil
}

// WatchApp streams changes of an app's records to someone using it.
func (s *Service) WatchApp(ctx context.Context, a Actor, ref, docRef string) (<-chan AppChange, func(), error) {
	_, doc, _, err := s.appData(ctx, a, ref, docRef, false)
	if err != nil {
		return nil, nil, err
	}
	in, stop := s.appFeed.subscribe(doc.ID)
	out := make(chan AppChange, 16)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case c, ok := <-in:
				if !ok {
					return
				}
				if c.owner != "" && c.owner != a.User.ID {
					continue
				}
				select {
				case out <- c:
				default:
				}
			}
		}
	}()
	return out, stop, nil
}

// --- files people add ----------------------------------------------------------

// AppFileView is a file added in an app.
type AppFileView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Mime      string    `json:"mime"`
	Size      int64     `json:"size"`
	By        AppViewer `json:"by"`
	Mine      bool      `json:"mine"`
	CreatedAt string    `json:"createdAt"`
}

// AppUpload stores a file someone adds in an app (level 2): a project file
// of the app, counted against the app's storage.
func (s *Service) AppUpload(ctx context.Context, a Actor, ref, docRef, name string, r io.Reader) (AppFileView, error) {
	p, doc, app, err := s.appData(ctx, a, ref, docRef, true)
	if err != nil {
		return AppFileView{}, err
	}
	if s.Blobs == nil {
		return AppFileView{}, errNoBlobs
	}
	sp, err := s.spool(r)
	if err != nil {
		return AppFileView{}, err
	}
	defer os.Remove(sp.path)
	used, err := s.appUsed(ctx, p, doc)
	if err != nil {
		return AppFileView{}, err
	}
	if used+sp.size > int64(app.StorageMB)<<20 {
		return AppFileView{}, invalid("validation.app_storage_full", map[string]any{"mb": app.StorageMB}, "the app's storage is full (%d MB)", app.StorageMB)
	}
	f, err := s.ingestAs(ctx, a, p, sp, cleanName(name), doc.ID, "", nil, purposeAppUpload)
	if err != nil {
		return AppFileView{}, err
	}
	return s.fileViews(ctx, a, doc.ID, []store.File{f})[0], nil
}

func (s *Service) fileViews(ctx context.Context, a Actor, docID string, fs []store.File) []AppFileView {
	ids := []string{}
	for _, f := range fs {
		ids = append(ids, f.CreatedBy)
	}
	names, _ := s.DB.UserNames(ctx, ids)
	out := make([]AppFileView, 0, len(fs))
	for _, f := range fs {
		out = append(out, AppFileView{ID: f.ID, Name: f.Name, Mime: f.Mime, Size: f.Size, By: AppViewer{ID: appViewerID(docID, f.CreatedBy), Name: names[f.CreatedBy]},
			Mine: f.CreatedBy == a.User.ID, CreatedAt: f.CreatedAt.UTC().Format(time.RFC3339)})
	}
	return out
}

// AppUploads lists the files people added in an app.
func (s *Service) AppUploads(ctx context.Context, a Actor, ref, docRef string) ([]AppFileView, error) {
	p, doc, _, err := s.appData(ctx, a, ref, docRef, false)
	if err != nil {
		return nil, err
	}
	fs, err := s.DB.ListFiles(ctx, p.ID, store.FileFilter{DocumentID: doc.ID, Purpose: purposeAppUpload})
	if err != nil {
		return nil, err
	}
	return s.fileViews(ctx, a, doc.ID, fs), nil
}

// AppUploadContent opens a file added in the app for someone using it.
func (s *Service) AppUploadContent(ctx context.Context, a Actor, ref, docRef, id string) (FileContent, error) {
	_, doc, _, err := s.appData(ctx, a, ref, docRef, false)
	if err != nil {
		return FileContent{}, err
	}
	f, err := s.DB.GetFile(ctx, id)
	if err != nil || f.DocumentID != doc.ID || f.Purpose != purposeAppUpload {
		return FileContent{}, ErrNotFound
	}
	return s.openBlob(ctx, f)
}

func (s *Service) openBlob(ctx context.Context, f store.File) (FileContent, error) {
	if s.Blobs == nil {
		return FileContent{}, errNoBlobs
	}
	rc, err := s.Blobs.Open(ctx, f.SHA256)
	if err != nil {
		return FileContent{}, ErrNotFound
	}
	return FileContent{File: f, Mime: f.Mime, SHA256: f.SHA256, Reader: rc}, nil
}

// AppDeleteUpload removes a file added in the app (who added it, or editors).
func (s *Service) AppDeleteUpload(ctx context.Context, a Actor, ref, docRef, id string) error {
	p, doc, _, err := s.appData(ctx, a, ref, docRef, true)
	if err != nil {
		return err
	}
	f, err := s.DB.GetFile(ctx, id)
	if err != nil || f.DocumentID != doc.ID || f.Purpose != purposeAppUpload {
		return ErrNotFound
	}
	if f.CreatedBy != a.User.ID && rank[p.Role] < rank[store.RoleEditor] {
		return ErrForbidden
	}
	if err := s.DB.DeleteFile(ctx, id); err != nil {
		return err
	}
	s.event(ctx, a, "file.delete", id, map[string]string{"app": doc.ID})
	return nil
}

// --- device features -------------------------------------------------------------

// appDeviceState is what an app may ask for here and what the person answered.
func (s *Service) appDeviceState(ctx context.Context, a Actor, docID string, app App, pol AppPolicy) ([]string, map[string]bool) {
	if pol.NoDevices || len(app.Devices) == 0 {
		return []string{}, map[string]bool{}
	}
	g, err := s.DB.AppDeviceGrants(ctx, docID, a.User.ID)
	if err != nil {
		g = map[string]bool{}
	}
	out := map[string]bool{}
	for _, d := range app.Devices {
		if v, ok := g[d]; ok {
			out[d] = v
		}
	}
	return slices.Clone(app.Devices), out
}

// SetAppDevice stores a person's answer for one device feature of an app
// (forget: ask again). Each feature is asked and answered on its own.
func (s *Service) SetAppDevice(ctx context.Context, a Actor, ref, docRef, device string, allowed, forget bool) (map[string]bool, error) {
	if err := s.fullUser(a); err != nil {
		return nil, err
	}
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return nil, err
	}
	if doc.Type != store.DocApp {
		return nil, ErrNotFound
	}
	app := appOf(doc)
	pol := s.effectiveAppPolicy(ctx, p)
	if pol.Off || pol.NoDevices || !slices.Contains(app.Devices, device) {
		return nil, invalid("validation.app_device_not_allowed", map[string]any{"device": device}, "this app may not use %s here", device)
	}
	if err := s.DB.SetAppDeviceGrant(ctx, doc.ID, a.User.ID, device, allowed, forget); err != nil {
		return nil, err
	}
	s.event(ctx, a, "app.device", doc.ID, map[string]any{"device": device, "allowed": allowed && !forget, "forget": forget})
	_, g := s.appDeviceState(ctx, a, doc.ID, app, pol)
	return g, nil
}

// --- sources from outside services --------------------------------------------

// readRequestSource runs a request block of a page (ref "page#n", n from
// 1) for an app: read-only methods only, with the project's environment
// and network rules, like running the block on its page.
func (s *Service) readRequestSource(ctx context.Context, a Actor, p store.Project, ref string) (any, error) {
	page, num, ok := strings.Cut(ref, "#")
	n, err := strconv.Atoi(num)
	if !ok || err != nil || n < 1 {
		return nil, invalid("validation.app_source_request", nil, "a request source is page#n (the n-th request block of the page)")
	}
	_, doc, err := s.docAccess(ctx, a, p.Slug, page, store.RoleReader)
	if err != nil {
		return nil, err
	}
	req, err := blockRequest(doc, n-1)
	if err != nil {
		return nil, err
	}
	if !requests.Safe(req.Method) {
		return nil, invalid("validation.app_source_request", nil, "apps only read: the request must use GET or HEAD")
	}
	res, host, err := s.execRequest(ctx, p.ID, req)
	if err != nil {
		return nil, err
	}
	s.event(ctx, a, "request.run", doc.ID, map[string]any{"index": n - 1, "method": req.Method, "host": host, "status": res.Status, "via": "app"})
	// JSON answers arrive parsed, others as text.
	var parsed any
	if json.Unmarshal([]byte(res.Body), &parsed) == nil {
		return map[string]any{"status": res.Status, "json": parsed}, nil
	}
	return map[string]any{"status": res.Status, "text": res.Body}, nil
}

// --- apps that save themselves -----------------------------------------------------

// AppSelfSave saves files an app changed of itself (apps with selfEdit, by
// people who may edit it; the page around it asked them to confirm).
func (s *Service) AppSelfSave(ctx context.Context, a Actor, ref, docRef string, files map[string]*string) (DocResult, error) {
	if err := s.fullUser(a); err != nil {
		return DocResult{}, err
	}
	_, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return DocResult{}, err
	}
	if doc.Type != store.DocApp || !appOf(doc).SelfEdit {
		return DocResult{}, invalid("validation.app_self_edit", nil, "this app may not change itself")
	}
	if len(files) == 0 || len(files) > maxAppFiles {
		return DocResult{}, invalid("validation.app_files", map[string]any{"max": maxAppFiles}, "an app has at most %d files", maxAppFiles)
	}
	return s.EditApp(ctx, a, ref, doc.ID, files, nil, nil)
}

// AppOwnFiles returns an app's source files (for apps that edit themselves).
func (s *Service) AppOwnFiles(ctx context.Context, a Actor, ref, docRef string) (map[string]string, error) {
	_, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return nil, err
	}
	if doc.Type != store.DocApp || !appOf(doc).SelfEdit {
		return nil, invalid("validation.app_self_edit", nil, "this app may not change itself")
	}
	return appOf(doc).Files, nil
}

// --- assets ----------------------------------------------------------------------

// AppAsset is a binary file of an app, served as assets/<name>.
type AppAsset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
	Path string `json:"path"` // what the app's code uses
}

func assetView(f store.File) AppAsset {
	return AppAsset{ID: f.ID, Name: f.Name, Mime: f.Mime, Size: f.Size, Path: "assets/" + f.Name}
}

func (s *Service) appEditor(ctx context.Context, a Actor, ref, docRef string) (store.Project, store.Document, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return p, doc, err
	}
	if doc.Type != store.DocApp {
		return p, doc, invalid("validation.not_app", nil, "%s is not an app", docRef)
	}
	return p, doc, nil
}

func (s *Service) appAssets(ctx context.Context, p store.Project, docID string) ([]store.File, error) {
	return s.DB.ListFiles(ctx, p.ID, store.FileFilter{DocumentID: docID, Purpose: purposeAppAsset})
}

// AppAssets lists an app's assets.
func (s *Service) AppAssets(ctx context.Context, a Actor, ref, docRef string) ([]AppAsset, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return nil, err
	}
	fs, err := s.appAssets(ctx, p, doc.ID)
	out := make([]AppAsset, 0, len(fs))
	for _, f := range fs {
		out = append(out, assetView(f))
	}
	return out, err
}

// AddAppAsset stores an asset of an app (editors; also AI clients). An
// asset with the same name is replaced.
func (s *Service) AddAppAsset(ctx context.Context, a Actor, ref, docRef, name string, r io.Reader) (AppAsset, error) {
	p, doc, err := s.appEditor(ctx, a, ref, docRef)
	if err != nil {
		return AppAsset{}, err
	}
	if s.readOnly(a, p) {
		return AppAsset{}, ErrForbidden
	}
	name = strings.TrimSpace(path.Base(name))
	if !appAssetNameRe.MatchString(name) {
		return AppAsset{}, invalid("validation.app_asset_name", nil, "asset names are 1–100 letters, digits, spaces and _.-")
	}
	if s.Blobs == nil {
		return AppAsset{}, errNoBlobs
	}
	sp, err := s.spool(io.LimitReader(r, maxAppAsset+1))
	if err != nil {
		return AppAsset{}, err
	}
	defer os.Remove(sp.path)
	if sp.size > maxAppAsset {
		return AppAsset{}, errTooLarge(maxAppAsset)
	}
	old, _ := s.appAssets(ctx, p, doc.ID)
	f, err := s.ingestAs(ctx, a, p, sp, name, doc.ID, "", nil, purposeAppAsset)
	if err != nil {
		return AppAsset{}, err
	}
	for _, o := range old {
		if o.Name == name {
			_ = s.DB.DeleteFile(ctx, o.ID)
		}
	}
	return assetView(f), nil
}

// DeleteAppAsset removes an asset (editors).
func (s *Service) DeleteAppAsset(ctx context.Context, a Actor, ref, docRef, name string) error {
	p, doc, err := s.appEditor(ctx, a, ref, docRef)
	if err != nil {
		return err
	}
	fs, err := s.appAssets(ctx, p, doc.ID)
	if err != nil {
		return err
	}
	for _, f := range fs {
		if f.Name == name || f.ID == name {
			s.event(ctx, a, "file.delete", f.ID, map[string]string{"app": doc.ID})
			return s.DB.DeleteFile(ctx, f.ID)
		}
	}
	return ErrNotFound
}

// copyAssets gives the app to the assets of another app (the same stored
// content, no second copy), replacing assets of the same name.
func (s *Service) copyAssets(ctx context.Context, a Actor, to store.Project, toDoc string, from []store.File, names []string) ([]AppAsset, error) {
	have, _ := s.appAssets(ctx, to, toDoc)
	out := []AppAsset{}
	for _, f := range from {
		if len(names) > 0 && !slices.Contains(names, f.Name) {
			continue
		}
		if err := s.checkQuota(ctx, to, f.Size); err != nil {
			return out, err
		}
		nf, err := s.DB.CreateFile(ctx, store.File{ID: newID(), ProjectID: to.ID, SHA256: f.SHA256, Name: f.Name, Mime: f.Mime, Size: f.Size,
			DocumentID: toDoc, CreatedBy: a.User.ID, Purpose: purposeAppAsset})
		if err != nil {
			return out, err
		}
		for _, h := range have {
			if h.Name == f.Name {
				_ = s.DB.DeleteFile(ctx, h.ID)
			}
		}
		out = append(out, assetView(nf))
	}
	return out, nil
}

// CopyAppAssets copies assets of another app (in any project the person
// can read) into an app (editors); names empty = all.
func (s *Service) CopyAppAssets(ctx context.Context, a Actor, ref, docRef, fromProject, fromDoc string, names []string) ([]AppAsset, error) {
	p, doc, err := s.appEditor(ctx, a, ref, docRef)
	if err != nil {
		return nil, err
	}
	if fromProject == "" {
		fromProject = p.Slug
	}
	fp, fd, err := s.docAccess(ctx, a, fromProject, fromDoc, store.RoleReader)
	if err != nil {
		return nil, err
	}
	if fd.Type != store.DocApp {
		return nil, invalid("validation.not_app", nil, "%s is not an app", fromDoc)
	}
	from, err := s.appAssets(ctx, fp, fd.ID)
	if err != nil {
		return nil, err
	}
	out, err := s.copyAssets(ctx, a, p, doc.ID, from, names)
	if err == nil {
		s.event(ctx, a, "app.assets_copy", doc.ID, map[string]any{"from": fd.ID, "count": len(out)})
	}
	return out, err
}

// appAssetFile serves an asset on the app's frame.
func (s *Service) appAssetFile(ctx context.Context, projectID, docID, name string, hosts []string) (AppFile, error) {
	if docID == "" {
		return AppFile{}, ErrNotFound
	}
	fs, err := s.DB.ListFiles(ctx, projectID, store.FileFilter{DocumentID: docID, Purpose: purposeAppAsset})
	if err != nil {
		return AppFile{}, err
	}
	for _, f := range fs {
		if f.Name != name {
			continue
		}
		fc, err := s.openBlob(ctx, f)
		if err != nil {
			return AppFile{}, err
		}
		defer fc.Reader.Close()
		b, err := io.ReadAll(io.LimitReader(fc.Reader, maxAppAsset+1))
		if err != nil {
			return AppFile{}, err
		}
		mt := f.Mime
		if mt == "" || strings.HasPrefix(mt, "text/html") {
			mt = "application/octet-stream" // never a page on the app's origin
		}
		return AppFile{Body: b, Type: mt, Hosts: hosts}, nil
	}
	return AppFile{}, ErrNotFound
}

// --- copies --------------------------------------------------------------------

// copyAppInto creates an app from files and a manifest in a project where
// the person may write, with the assets of the original.
func (s *Service) copyAppInto(ctx context.Context, a Actor, target string, title, category string, app App, assets []store.File) (DocResult, error) {
	app.Build, app.Origin = nil, nil
	res, err := s.CreateDocument(ctx, a, target, DocumentInput{Type: store.DocApp, Title: title, Category: category, App: &app})
	for n := 2; errorsIsConflict(err) && n <= 20; n++ {
		res, err = s.CreateDocument(ctx, a, target, DocumentInput{Type: store.DocApp, Title: fmt.Sprintf("%s %d", title, n), Category: category, App: &app})
	}
	if err != nil || res.Document == nil || len(assets) == 0 {
		return res, err
	}
	p, err := s.project(ctx, a, target, store.RoleEditor)
	if err != nil {
		return res, err
	}
	_, err = s.copyAssets(ctx, a, p, res.Document.ID, assets, nil)
	return res, err
}

// CopySharedApp makes an own app from a public app (a signed-in person,
// into a project where they may write): its files, manifest and assets –
// not its data, sources or grants.
func (s *Service) CopySharedApp(ctx context.Context, a Actor, token, target, title string) (DocResult, error) {
	if err := s.fullUser(a); err != nil {
		return DocResult{}, err
	}
	sctx, _, p, doc, app, _, err := s.sharedApp(ctx, token)
	if err != nil {
		return DocResult{}, err
	}
	assets, _ := s.appAssets(sctx, p, doc.ID)
	app.Sources, app.Level = nil, min(app.Level, 2)
	if strings.TrimSpace(title) == "" {
		title = doc.Title
	}
	res, err := s.copyAppInto(ctx, a, target, title, "", app, assets)
	if err == nil && res.Document != nil {
		s.event(ctx, a, "app.copy", res.Document.ID, map[string]string{"fromShare": doc.ID})
	}
	return res, err
}

// --- previews and the gallery ---------------------------------------------------

// SetAppPreview stores a picture of an app as it renders (made in the
// browser of someone who may edit it), replacing the previous one.
func (s *Service) SetAppPreview(ctx context.Context, a Actor, ref, docRef string, png []byte) error {
	if err := s.fullUser(a); err != nil {
		return err
	}
	p, doc, err := s.appEditor(ctx, a, ref, docRef)
	if err != nil {
		return err
	}
	if len(png) == 0 || len(png) > maxAppPreview || !bytes.HasPrefix(png, []byte("\x89PNG")) {
		return invalid("validation.app_preview", nil, "a preview is a PNG up to 2 MB")
	}
	if s.Blobs == nil {
		return errNoBlobs
	}
	old, _ := s.DB.ListFiles(ctx, p.ID, store.FileFilter{DocumentID: doc.ID, Purpose: purposeAppPreview})
	sp, err := s.spool(bytes.NewReader(png))
	if err != nil {
		return err
	}
	defer os.Remove(sp.path)
	if _, err := s.ingestAs(ctx, a, p, sp, fmt.Sprintf("preview-v%d.png", doc.Version), doc.ID, "", nil, purposeAppPreview); err != nil {
		return err
	}
	for _, o := range old {
		_ = s.DB.DeleteFile(ctx, o.ID)
	}
	return nil
}

func (s *Service) appPreviewFile(ctx context.Context, projectID, docID string) (store.File, bool) {
	fs, err := s.DB.ListFiles(ctx, projectID, store.FileFilter{DocumentID: docID, Purpose: purposeAppPreview})
	if err != nil || len(fs) == 0 {
		return store.File{}, false
	}
	return fs[0], true
}

// AppPreview opens an app's preview picture (people who can read it).
func (s *Service) AppPreview(ctx context.Context, a Actor, ref, docRef string) (FileContent, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return FileContent{}, err
	}
	f, ok := s.appPreviewFile(ctx, p.ID, doc.ID)
	if !ok {
		return FileContent{}, ErrNotFound
	}
	return s.openBlob(ctx, f)
}

// SharedAppPreview is the preview picture of a public app (for links).
func (s *Service) SharedAppPreview(ctx context.Context, token string) (FileContent, error) {
	sctx, _, p, doc, _, _, err := s.sharedApp(ctx, token)
	if err != nil {
		return FileContent{}, err
	}
	f, ok := s.appPreviewFile(sctx, p.ID, doc.ID)
	if !ok {
		return FileContent{}, ErrNotFound
	}
	return s.openBlob(sctx, f)
}

// SharedAppMeta is what a link to a public app shows (title, picture).
type SharedAppMeta struct {
	Title   string
	Project string
	Preview bool
}

// SharedAppInfo describes a public app for link previews.
func (s *Service) SharedAppInfo(ctx context.Context, token string) (SharedAppMeta, error) {
	sctx, _, p, doc, _, _, err := s.sharedApp(ctx, token)
	if err != nil {
		return SharedAppMeta{}, err
	}
	_, ok := s.appPreviewFile(sctx, p.ID, doc.ID)
	return SharedAppMeta{Title: doc.Title, Project: p.Name, Preview: ok}, nil
}

// AppCard is an app in the gallery.
type AppCard struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Category  string `json:"category,omitempty"`
	UpdatedAt string `json:"updatedAt"`
	Preview   bool   `json:"preview"`
	PreviewAt string `json:"previewAt,omitempty"`
	// Stale: the picture shows an older version.
	Stale  bool `json:"stale,omitempty"`
	Shared bool `json:"shared"`
	Level  int  `json:"level"`
}

// AppGallery lists a project's apps with their preview pictures.
func (s *Service) AppGallery(ctx context.Context, a Actor, ref string) ([]AppCard, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return nil, err
	}
	docs, err := s.Documents(ctx, a, ref, store.DocFilter{Type: store.DocApp})
	if err != nil {
		return nil, err
	}
	out := []AppCard{}
	for _, d := range docs {
		c := AppCard{ID: d.ID, Slug: d.Slug, Title: d.Title, Category: d.Category, UpdatedAt: d.UpdatedAt.UTC().Format(time.RFC3339)}
		if f, ok := s.appPreviewFile(ctx, p.ID, d.ID); ok {
			c.Preview, c.PreviewAt = true, f.CreatedAt.UTC().Format(time.RFC3339)
			c.Stale = f.Name != fmt.Sprintf("preview-v%d.png", d.Version)
		}
		if _, err := s.DB.AppShare(ctx, d.ID); err == nil {
			c.Shared = true
		}
		if full, err := s.DB.GetDocument(ctx, p.ID, d.ID); err == nil {
			c.Level = appOf(full).Level
		}
		out = append(out, c)
	}
	return out, nil
}

// AppScreenshot is the latest picture of an app, for AI clients.
type AppScreenshot struct {
	PNG     []byte
	At      time.Time
	Version int // the app version shown
	Current int // the app's version now
}

// AppPicture reads the latest preview picture of an app.
func (s *Service) AppPicture(ctx context.Context, a Actor, ref, docRef string) (AppScreenshot, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return AppScreenshot{}, err
	}
	if doc.Type != store.DocApp {
		return AppScreenshot{}, invalid("validation.not_app", nil, "%s is not an app", docRef)
	}
	f, ok := s.appPreviewFile(ctx, p.ID, doc.ID)
	if !ok {
		return AppScreenshot{}, invalid("validation.app_no_preview", nil, "there is no picture of this app yet: it is made when someone who may edit it has it open in the browser")
	}
	fc, err := s.openBlob(ctx, f)
	if err != nil {
		return AppScreenshot{}, err
	}
	defer fc.Reader.Close()
	b, err := io.ReadAll(io.LimitReader(fc.Reader, maxAppPreview+1))
	if err != nil {
		return AppScreenshot{}, err
	}
	v := 0
	_, _ = fmt.Sscanf(f.Name, "preview-v%d.png", &v)
	return AppScreenshot{PNG: b, At: f.CreatedAt.Time, Version: v, Current: doc.Version}, nil
}

func errorsIsConflict(err error) bool { return errors.Is(err, store.ErrConflict) }
