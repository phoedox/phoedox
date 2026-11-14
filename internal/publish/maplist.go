package publish

import (
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"
)

// mapList renders a ```map block for published pages: no tiles and no
// scripts, only a list of the markers (linked to OpenStreetMap), tracks and
// GPX files. Entry markers (@slug) need the app and are left out.
func mapList(src string) string {
	var items []string
	for _, line := range strings.Split(src, "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) < 2 {
			continue
		}
		switch strings.ToLower(f[0]) {
		case "marker":
			lat, lng, rest, ok := latLng(f[1:])
			if !ok {
				continue
			}
			var label []string
			for _, w := range rest {
				if !strings.HasPrefix(w, "@") {
					label = append(label, w)
				}
			}
			name := strings.Join(label, " ")
			coord := fmt.Sprintf("%.5f, %.5f", lat, lng)
			if name == "" {
				name = coord
			}
			link := fmt.Sprintf("https://www.openstreetmap.org/?mlat=%f&mlon=%f#map=15/%f/%f", lat, lng, lat, lng)
			items = append(items, `<li><a href="`+html.EscapeString(link)+`" rel="noopener external">`+html.EscapeString(name)+`</a> <small>`+coord+`</small></li>`)
		case "track":
			label := strings.Join(f[1:], " ")
			if i := strings.Index(label, ":"); i >= 0 {
				label = strings.TrimSpace(label[:i])
			} else {
				label = ""
			}
			if label != "" {
				items = append(items, `<li>`+html.EscapeString(label)+`</li>`)
			}
		case "gpx":
			id := f[1]
			if strings.Trim(id, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789") != "" {
				continue
			}
			name := strings.Join(f[2:], " ")
			if name == "" {
				name = "GPX"
			}
			items = append(items, `<li><a href="/api/v1/files/`+url.PathEscape(id)+`">`+html.EscapeString(name)+`</a></li>`)
		}
	}
	if len(items) == 0 {
		return ""
	}
	return `<ul class="map-list">` + strings.Join(items, "") + `</ul>`
}

// latLng reads "52.52,13.40", "52.52, 13.40" or "52.52 13.40" from the start.
func latLng(f []string) (float64, float64, []string, bool) {
	joined := strings.Join(f, " ")
	parts := strings.FieldsFunc(joined, func(r rune) bool { return r == ',' || r == ' ' })
	if len(parts) < 2 {
		return 0, 0, nil, false
	}
	lat, err1 := strconv.ParseFloat(parts[0], 64)
	lng, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return 0, 0, nil, false
	}
	return lat, lng, parts[2:], true
}
