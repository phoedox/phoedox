package plugins

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// MaxLogoBytes limits a plugin logo (manifest logo and logoDark).
const MaxLogoBytes = 64 << 10

// Elements a logo must not contain: they run code, load other documents or
// embed HTML.
var badSVGElements = map[string]bool{
	"script": true, "foreignobject": true, "iframe": true, "embed": true, "object": true,
	"audio": true, "video": true, "image": true, "animate": true, "set": true, "animatetransform": true,
	"animatemotion": true, "handler": true, "listener": true,
}

var (
	cssURLRe   = regexp.MustCompile(`(?i)url\(\s*['"]?\s*([^)'"\s]*)`)
	cssBadRe   = regexp.MustCompile(`(?i)@import|javascript:|expression\(|behavior:|-moz-binding`)
	errNotLogo = errors.New("logo: not an SVG image")
)

// CheckSVG (a copy of phoedox-sdk pdk.CheckSVG) checks that a logo is a self-contained, static SVG: an <svg>
// root, no scripts, event handlers, animation, embedded HTML or images, and
// no references outside the file (only href="#…" and url(#…)). Store,
// instance and pack all run it; the logo is shown with <img>, which runs no
// script anyway, so this keeps the file harmless when opened directly too.
func CheckSVG(data []byte) error {
	if len(data) > MaxLogoBytes {
		return fmt.Errorf("logo: larger than %d KB", MaxLogoBytes>>10)
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	dec.Entity = map[string]string{}
	depth, root := 0, false
	inStyle := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("logo: %w", err)
		}
		switch t := tok.(type) {
		case xml.Directive:
			// A plain DOCTYPE (editors write one) is fine; an internal
			// subset could define entities.
			d := strings.ToUpper(string(t))
			if !strings.HasPrefix(d, "DOCTYPE") || strings.ContainsAny(d, "[<") || strings.Contains(d, "ENTITY") {
				return errors.New("logo: entities and internal DTDs are not allowed")
			}
		case xml.ProcInst:
			if t.Target != "xml" {
				return fmt.Errorf("logo: processing instruction %q is not allowed", t.Target)
			}
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			if depth == 0 {
				if name != "svg" || root {
					return errNotLogo
				}
				root = true
			}
			depth++
			if badSVGElements[name] {
				return fmt.Errorf("logo: <%s> is not allowed", t.Name.Local)
			}
			inStyle = name == "style"
			for _, a := range t.Attr {
				an := strings.ToLower(a.Name.Local)
				switch {
				case strings.HasPrefix(an, "on"):
					return fmt.Errorf("logo: event handler %s is not allowed", a.Name.Local)
				case an == "href" || an == "src":
					if !strings.HasPrefix(strings.TrimSpace(a.Value), "#") {
						return fmt.Errorf("logo: <%s> refers to %q; only #ids inside the file are allowed", t.Name.Local, a.Value)
					}
				}
				if err := checkCSS(a.Value); err != nil {
					return err
				}
			}
		case xml.EndElement:
			depth--
			inStyle = false
		case xml.CharData:
			if inStyle {
				if err := checkCSS(string(t)); err != nil {
					return err
				}
			}
		}
	}
	if !root {
		return errNotLogo
	}
	return nil
}

func checkCSS(s string) error {
	if cssBadRe.MatchString(s) {
		return errors.New("logo: style with imports or scripts is not allowed")
	}
	for _, m := range cssURLRe.FindAllStringSubmatch(s, -1) {
		if !strings.HasPrefix(m[1], "#") {
			return fmt.Errorf("logo: url(%s) points outside the file", m[1])
		}
	}
	return nil
}
