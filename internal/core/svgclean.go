package core

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"strings"
)

// SVG pictures (logos, diagrams) are cleaned before they are stored: only
// drawing elements and attributes from an allow-list stay; scripts, event
// handlers, foreign content, styles that load things and links outside the
// picture are removed. Files are also served with a sandboxing CSP.

const (
	svgNS   = "http://www.w3.org/2000/svg"
	xlinkNS = "http://www.w3.org/1999/xlink"
	xmlNS   = "http://www.w3.org/XML/1998/namespace"
)

var svgElements = map[string]bool{
	"svg": true, "g": true, "path": true, "rect": true, "circle": true, "ellipse": true, "line": true, "polyline": true, "polygon": true,
	"text": true, "tspan": true, "textPath": true, "defs": true, "linearGradient": true, "radialGradient": true, "stop": true, "clipPath": true,
	"mask": true, "pattern": true, "symbol": true, "use": true, "title": true, "desc": true, "image": true, "marker": true,
	"filter": true, "feGaussianBlur": true, "feOffset": true, "feBlend": true, "feColorMatrix": true, "feFlood": true, "feComposite": true,
	"feMerge": true, "feMergeNode": true, "feDropShadow": true, "feMorphology": true,
}

// svgUnwrap are left out while their content stays (links, switches).
var svgUnwrap = map[string]bool{"a": true, "switch": true}

var svgAttrs = map[string]bool{}

func init() {
	for _, a := range strings.Fields(`id class style transform d x y x1 y1 x2 y2 cx cy r rx ry width height points viewBox preserveAspectRatio
		fill fill-opacity fill-rule stroke stroke-width stroke-linecap stroke-linejoin stroke-miterlimit stroke-dasharray stroke-dashoffset stroke-opacity
		opacity color display visibility clip-path clip-rule mask filter font-family font-size font-weight font-style text-anchor dominant-baseline
		letter-spacing word-spacing text-decoration offset stop-color stop-opacity gradientUnits gradientTransform spreadMethod fx fy fr
		patternUnits patternContentUnits patternTransform clipPathUnits maskUnits maskContentUnits markerWidth markerHeight markerUnits refX refY orient
		marker-start marker-mid marker-end dx dy rotate textLength lengthAdjust startOffset href
		stdDeviation in in2 result mode type values operator k1 k2 k3 k4 flood-color flood-opacity radius version vector-effect paint-order
		filterUnits primitiveUnits shape-rendering image-rendering color-interpolation-filters`) {
		svgAttrs[a] = true
	}
}

var (
	cssURL      = regexp.MustCompile(`(?i)url\(\s*['"]?\s*([^'")\s]*)`)
	cssBad      = regexp.MustCompile(`(?i)@import|expression\s*\(|javascript:|behavior\s*:|-moz-binding`)
	dataImageRe = regexp.MustCompile(`^data:image/(png|jpeg|gif|webp);base64,[A-Za-z0-9+/=\s]+$`)
)

// safeRef: a link inside the picture (#id) or an embedded raster image.
func safeRef(v string, image bool) bool {
	v = strings.TrimSpace(v)
	return strings.HasPrefix(v, "#") || image && dataImageRe.MatchString(v)
}

func safeStyle(v string) bool {
	if cssBad.MatchString(v) {
		return false
	}
	for _, m := range cssURL.FindAllStringSubmatch(v, -1) {
		if !strings.HasPrefix(m[1], "#") {
			return false
		}
	}
	return true
}

var errSVG = errors.New("not a readable SVG picture")

// cleanSVG returns the picture with only allowed elements and attributes.
func cleanSVG(data []byte) ([]byte, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	var out bytes.Buffer
	skip := 0 // depth inside a removed element
	depth, root := 0, false
	var dropped []bool // per open element: tag left out but content kept
	esc := func(s string) string {
		var b bytes.Buffer
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errSVG
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if skip > 0 {
				skip++
				continue
			}
			if t.Name.Space == "" {
				t.Name.Space = svgNS // hand-written files without xmlns
			}
			if depth == 1 && (t.Name.Space != svgNS || t.Name.Local != "svg") {
				return nil, errSVG
			}
			if t.Name.Space == svgNS && svgUnwrap[t.Name.Local] {
				dropped = append(dropped, true)
				continue
			}
			if t.Name.Space != svgNS || !svgElements[t.Name.Local] {
				skip = 1
				continue
			}
			dropped = append(dropped, false)
			if depth == 1 {
				root = true
			}
			out.WriteString("<" + t.Name.Local)
			if depth == 1 {
				out.WriteString(` xmlns="` + svgNS + `" xmlns:xlink="` + xlinkNS + `"`)
			}
			for _, a := range t.Attr {
				name := a.Name.Local
				switch a.Name.Space {
				case "":
				case xlinkNS:
					if name != "href" {
						continue
					}
					name = "xlink:href"
				case xmlNS:
					if name != "space" && name != "lang" {
						continue
					}
					name = "xml:" + name
				default:
					continue // xmlns declarations and foreign attributes
				}
				local := a.Name.Local
				if a.Name.Space == "" && !svgAttrs[local] {
					continue
				}
				if local == "href" && !safeRef(a.Value, t.Name.Local == "image") {
					continue
				}
				if (local == "style" || strings.Contains(a.Value, "url(")) && !safeStyle(a.Value) {
					continue
				}
				out.WriteString(" " + name + `="` + strings.ReplaceAll(esc(a.Value), `"`, "&#34;") + `"`)
			}
			out.WriteString(">")
		case xml.EndElement:
			depth--
			if skip > 0 {
				skip--
				continue
			}
			drop := dropped[len(dropped)-1]
			dropped = dropped[:len(dropped)-1]
			if !drop {
				out.WriteString("</" + t.Name.Local + ">")
			}
		case xml.CharData:
			if skip == 0 && depth > 0 {
				out.WriteString(esc(string(t)))
			}
		}
	}
	if !root {
		return nil, errSVG
	}
	return out.Bytes(), nil
}

// looksSVG: the bytes start like an SVG document.
func looksSVG(b []byte) bool {
	head := strings.ToLower(string(b[:min(len(b), 2048)]))
	return strings.Contains(head, "<svg")
}
