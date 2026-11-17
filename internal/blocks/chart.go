package blocks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Charts: ```chart blocks in pages and chart elements on whiteboards share
// one JSON format, drawn by the web (web/src/chart.ts) and the app. The
// server checks it like the web does, so AI clients learn about mistakes
// when they write (validate_blocks, blockProblems) instead of from people.

const maxChartJSON = 128 << 10

// ChartPoint is one point of a series.
type ChartPoint struct {
	X     any      `json:"x"`
	Y     *float64 `json:"y"`
	Label string   `json:"label,omitempty"`
	Title string   `json:"title,omitempty"`
	Text  string   `json:"text,omitempty"`
	Color string   `json:"color,omitempty"`
}

// ChartSeries is a line (or bars) of points.
type ChartSeries struct {
	Name   string       `json:"name,omitempty"`
	Color  string       `json:"color,omitempty"`
	Curve  string       `json:"curve,omitempty"`
	Dashed bool         `json:"dashed,omitempty"`
	Points []ChartPoint `json:"points"`
}

// ChartSpec is the content of a chart block.
type ChartSpec struct {
	Type  string `json:"type,omitempty"`
	Title string `json:"title,omitempty"`
	X     *struct {
		Type     string `json:"type,omitempty"`
		Segments []struct {
			From any `json:"from"`
			To   any `json:"to"`
		} `json:"segments,omitempty"`
	} `json:"x,omitempty"`
	Bands []struct {
		From  any    `json:"from"`
		To    any    `json:"to"`
		Label string `json:"label,omitempty"`
		Color string `json:"color,omitempty"`
	} `json:"bands,omitempty"`
	Series []ChartSeries `json:"series,omitempty"`
	Points []ChartPoint  `json:"points,omitempty"`
	Notes  []struct {
		X     any    `json:"x"`
		Color string `json:"color,omitempty"`
	} `json:"notes,omitempty"`
}

var chartDateRe = regexp.MustCompile(`^(\d{1,4})(?:-(\d{2})(?:-(\d{2}))?)?$`)

func (c *ChartSpec) series() []ChartSeries {
	if len(c.Series) > 0 {
		return c.Series
	}
	if len(c.Points) > 0 {
		return []ChartSeries{{Points: c.Points}}
	}
	return nil
}

func (c *ChartSpec) xs() []any {
	var out []any
	for _, s := range c.series() {
		for _, p := range s.Points {
			out = append(out, p.X)
		}
	}
	for _, b := range c.Bands {
		out = append(out, b.From, b.To)
	}
	for _, n := range c.Notes {
		out = append(out, n.X)
	}
	return out
}

// xKind follows web/src/chart.ts: time for dates, number for numbers, else category.
func (c *ChartSpec) xKind() string {
	if c.X != nil && c.X.Type != "" {
		return c.X.Type
	}
	xs := c.xs()
	allNum, allDate := true, true
	for _, v := range xs {
		switch t := v.(type) {
		case float64:
			allDate = false
		case string:
			allNum = false
			if !chartDateRe.MatchString(strings.TrimSpace(t)) {
				allDate = false
			}
		default:
			allNum, allDate = false, false
		}
	}
	switch {
	case allNum:
		return "number"
	case c.Type == "bar":
		return "category"
	case allDate:
		return "time"
	}
	return "category"
}

// chartX is a value on a time (Unix seconds) or number axis.
func chartX(v any, kind string) (float64, bool) {
	switch kind {
	case "number":
		switch t := v.(type) {
		case float64:
			return t, true
		case string:
			f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
			return f, err == nil
		}
	case "time":
		switch t := v.(type) {
		case float64:
			return float64(time.Date(int(t), 1, 1, 0, 0, 0, 0, time.UTC).Unix()), true
		case string:
			m := chartDateRe.FindStringSubmatch(strings.TrimSpace(t))
			if m == nil {
				return 0, false
			}
			y, _ := strconv.Atoi(m[1])
			mo, d := 1, 1
			if m[2] != "" {
				mo, _ = strconv.Atoi(m[2])
			}
			if m[3] != "" {
				d, _ = strconv.Atoi(m[3])
			}
			if mo < 1 || mo > 12 || d < 1 || d > 31 {
				return 0, false
			}
			return float64(time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC).Unix()), true
		}
	}
	return 0, false
}

var (
	chartTypes  = map[string]bool{"": true, "line": true, "area": true, "bar": true, "scatter": true}
	chartXTypes = map[string]bool{"": true, "time": true, "number": true, "category": true}
	chartCurves = map[string]bool{"": true, "smooth": true, "straight": true, "step": true}
)

// ParseChart reads and checks a chart; problems carry the line of the
// mistake inside the JSON (1-based) where it is known.
func ParseChart(src []byte) (*ChartSpec, []Problem) {
	bad := func(line int, f string, a ...any) []Problem {
		return []Problem{{Kind: "chart", Line: line, Severity: "error", Message: fmt.Sprintf(f, a...)}}
	}
	if len(src) > maxChartJSON {
		return nil, bad(1, "the chart is larger than %d KB", maxChartJSON>>10)
	}
	var c ChartSpec
	dec := json.NewDecoder(bytes.NewReader(src))
	if err := dec.Decode(&c); err != nil {
		line := 1
		var se *json.SyntaxError
		var te *json.UnmarshalTypeError
		switch {
		case errors.As(err, &se):
			line = 1 + bytes.Count(src[:min(int(se.Offset), len(src))], []byte("\n"))
			return nil, bad(line, "not valid JSON: %v", err)
		case errors.As(err, &te):
			line = 1 + bytes.Count(src[:min(int(te.Offset), len(src))], []byte("\n"))
			return nil, bad(line, "%s must be %s, not %s", te.Field, te.Type, te.Value)
		}
		return nil, bad(line, "not valid JSON: %v", err)
	}
	var out []Problem
	add := func(f string, a ...any) {
		out = append(out, Problem{Kind: "chart", Line: 1, Severity: "error", Message: fmt.Sprintf(f, a...)})
	}
	if !chartTypes[c.Type] {
		add("unknown type %q (line, area, bar, scatter)", c.Type)
	}
	if c.X != nil && !chartXTypes[c.X.Type] {
		add("unknown x.type %q (time, number, category)", c.X.Type)
	}
	series := c.series()
	n := 0
	for _, s := range series {
		n += len(s.Points)
	}
	if n == 0 {
		add("no points: give points, or series with points")
	}
	color := func(col, where string) {
		if col != "" && !Colors[col] {
			add("%s: unknown colour %q (gray, red, orange, yellow, green, blue, purple, pink)", where, col)
		}
	}
	for i, s := range series {
		color(s.Color, fmt.Sprintf("series %d", i+1))
		if !chartCurves[s.Curve] {
			add("series %d: unknown curve %q (smooth, straight, step)", i+1, s.Curve)
		}
		for j, p := range s.Points {
			if p.X == nil || p.X == "" {
				add("series %d, point %d: x is missing", i+1, j+1)
			}
			color(p.Color, fmt.Sprintf("series %d, point %d", i+1, j+1))
		}
	}
	for i, b := range c.Bands {
		color(b.Color, fmt.Sprintf("band %d", i+1))
	}
	for i, nt := range c.Notes {
		color(nt.Color, fmt.Sprintf("note %d", i+1))
	}
	if len(out) > 0 {
		return nil, out
	}
	if kind := c.xKind(); kind != "category" {
		for _, v := range c.xs() {
			if _, ok := chartX(v, kind); !ok {
				add("%v does not fit the %s axis (dates like 1848-03-18, or numbers)", v, kind)
				break
			}
		}
		if c.X != nil {
			for _, sg := range c.X.Segments {
				a, ok1 := chartX(sg.From, kind)
				b, ok2 := chartX(sg.To, kind)
				if !ok1 || !ok2 || b <= a {
					add("segment %v–%v: the end must come after the start", sg.From, sg.To)
				}
			}
		}
	}
	if len(out) > 0 {
		return nil, out
	}
	return &c, nil
}

// ChartText is what search and mentions see of a chart.
func ChartText(c *ChartSpec) string {
	parts := []string{c.Title}
	for _, b := range c.Bands {
		parts = append(parts, b.Label)
	}
	for _, s := range c.series() {
		parts = append(parts, s.Name)
		for _, p := range s.Points {
			parts = append(parts, p.Title, p.Text)
		}
	}
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func init() {
	Register(Def{Kind: "chart", Board: true,
		Check: func(src []byte) []Problem {
			_, ps := ParseChart(src)
			return ps
		},
		Text: func(src []byte) string {
			c, ps := ParseChart(src)
			if len(ps) > 0 {
				return ""
			}
			return ChartText(c)
		},
	})
}
