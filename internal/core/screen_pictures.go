package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/png" // pictures of HTML screens
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// Pictures of HTML prototype screens. The server cannot draw HTML, so the
// browser of someone who may edit the board draws each HTML screen and
// uploads the picture; server-side screenshots (whiteboard_screenshot) use
// it. A picture belongs to the screen's content and size and to a theme, so
// a changed screen needs a new one.

const (
	purposeScreenPicture = "screen_picture"
	maxScreenPicture     = 4 << 20
)

func shortHash(s string, n int) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:n]
}

// screenPictureName is the file name of the picture of a screen as it is
// now: screen-<frame hash>-<content hash>-<light|dark>.png. The browser
// computes the same name (web/src/wb-html.ts).
func screenPictureName(e WBElement, dark bool) string {
	theme := "light"
	if dark {
		theme = "dark"
	}
	return screenPicturePrefix(e.ID) + shortHash(e.Screen.HTML+"|"+fnum(e.W)+"x"+fnum(e.H), 16) + "-" + theme + ".png"
}

func screenPicturePrefix(frameID string) string { return "screen-" + shortHash(frameID, 8) + "-" }

func fnum(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// SetScreenPicture stores the picture of an HTML screen of a whiteboard as
// the browser drew it (people who may edit the board).
func (s *Service) SetScreenPicture(ctx context.Context, a Actor, ref, docRef, frameID, theme string, png []byte) error {
	if err := s.fullUser(a); err != nil {
		return err
	}
	p, doc, wb, err := s.whiteboardDoc(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return err
	}
	var frame *WBElement
	for i := range wb.Elements {
		if e := &wb.Elements[i]; e.ID == frameID && e.Screen != nil && e.Screen.HTML != "" {
			frame = e
		}
	}
	if frame == nil {
		return ErrNotFound
	}
	if theme != "light" && theme != "dark" {
		return invalid("validation.screen_picture", nil, "theme is light or dark")
	}
	if len(png) == 0 || len(png) > maxScreenPicture || !bytes.HasPrefix(png, []byte("\x89PNG")) {
		return invalid("validation.screen_picture", nil, "a screen picture is a PNG up to 4 MB")
	}
	if s.Blobs == nil {
		return errNoBlobs
	}
	return s.storeScreenPicture(ctx, a, p, doc, *frame, theme, png)
}

// storeScreenPicture keeps the picture of a screen and drops older ones.
func (s *Service) storeScreenPicture(ctx context.Context, a Actor, p store.Project, doc store.Document, frame WBElement, theme string, png []byte) error {
	frameID := frame.ID
	name := screenPictureName(frame, theme == "dark")
	old, _ := s.DB.ListFiles(ctx, p.ID, store.FileFilter{DocumentID: doc.ID, Purpose: purposeScreenPicture})
	for _, o := range old {
		if o.Name == name {
			return nil // already there
		}
	}
	sp, err := s.spool(bytes.NewReader(png))
	if err != nil {
		return err
	}
	defer os.Remove(sp.path)
	if _, err := s.ingestAs(ctx, a, p, sp, name, doc.ID, "", nil, purposeScreenPicture); err != nil {
		return err
	}
	// Older pictures of this screen in this theme are gone with the change.
	pre, suf := screenPicturePrefix(frameID), "-"+theme+".png"
	for _, o := range old {
		if strings.HasPrefix(o.Name, pre) && strings.HasSuffix(o.Name, suf) {
			_ = s.DB.DeleteFile(ctx, o.ID)
		}
	}
	return nil
}

// ScreenPictures lists the pictures of a board's HTML screens that are up
// to date (file names), so the browser draws only the missing ones.
func (s *Service) ScreenPictures(ctx context.Context, a Actor, ref, docRef string) ([]string, error) {
	p, doc, wb, err := s.whiteboardDoc(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return nil, err
	}
	have := s.screenPictureFiles(ctx, p.ID, doc.ID)
	out := []string{}
	for _, e := range wb.Elements {
		if e.Screen == nil || e.Screen.HTML == "" {
			continue
		}
		for _, dark := range []bool{false, true} {
			if _, ok := have[screenPictureName(e, dark)]; ok {
				out = append(out, screenPictureName(e, dark))
			}
		}
	}
	return out, nil
}

func (s *Service) screenPictureFiles(ctx context.Context, projectID, docID string) map[string]store.File {
	fs, _ := s.DB.ListFiles(ctx, projectID, store.FileFilter{DocumentID: docID, Purpose: purposeScreenPicture})
	out := make(map[string]store.File, len(fs))
	for _, f := range fs {
		out[f.Name] = f
	}
	return out
}

// screenPictureLoader opens the current picture of an HTML screen (false:
// none yet).
func (s *Service) screenPictureLoader(ctx context.Context, projectID, docID string) func(e WBElement, dark bool) (image.Image, bool) {
	var have map[string]store.File
	return func(e WBElement, dark bool) (image.Image, bool) {
		if e.Screen == nil || e.Screen.HTML == "" || s.Blobs == nil {
			return nil, false
		}
		if have == nil {
			have = s.screenPictureFiles(ctx, projectID, docID)
		}
		// The other theme's picture is better than text.
		f, ok := have[screenPictureName(e, dark)]
		if !ok {
			if f, ok = have[screenPictureName(e, !dark)]; !ok {
				return nil, false
			}
		}
		fc, err := s.openBlob(ctx, f)
		if err != nil {
			return nil, false
		}
		defer fc.Reader.Close()
		img, _, err := image.Decode(io.LimitReader(fc.Reader, maxScreenPicture+1))
		if err != nil {
			return nil, false
		}
		return img, true
	}
}

// missingScreenPictures names the HTML screens without a current picture.
func (s *Service) missingScreenPictures(ctx context.Context, projectID, docID string, wb *Whiteboard, frame string, dark bool) []string {
	have := s.screenPictureFiles(ctx, projectID, docID)
	var out []string
	for _, e := range wb.Elements {
		if e.Screen == nil || e.Screen.HTML == "" || (frame != "" && frame != e.ID && !strings.EqualFold(frame, e.Text)) {
			continue
		}
		_, ok := have[screenPictureName(e, dark)]
		_, other := have[screenPictureName(e, !dark)]
		if !ok && !other {
			out = append(out, fmt.Sprintf("%q", e.Text))
		}
	}
	return out
}
