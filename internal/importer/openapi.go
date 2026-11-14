package importer

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// API is an OpenAPI description reduced to what pages need.
type API struct {
	Title   string
	Version string
	Server  string
	Pages   []Page
}

// ErrNotOpenAPI means the file has no paths.
var ErrNotOpenAPI = errors.New("importer: not an OpenAPI or Swagger description")

var methods = []string{"get", "post", "put", "patch", "delete", "head", "options"}

// OpenAPI reads an OpenAPI 3 or Swagger 2 description (JSON or YAML) and
// makes one page per tag with a request block per operation. The base URL
// and token stay variables ({{base}}, {{token}}) of the project environment.
func OpenAPI(data []byte) (API, error) {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return API{}, ErrNotOpenAPI
		}
	}
	paths, _ := doc["paths"].(map[string]any)
	if len(paths) == 0 {
		return API{}, ErrNotOpenAPI
	}
	info, _ := doc["info"].(map[string]any)
	api := API{Title: str(info["title"]), Version: str(info["version"])}
	if api.Title == "" {
		api.Title = "API"
	}
	if servers, ok := doc["servers"].([]any); ok && len(servers) > 0 {
		if s, ok := servers[0].(map[string]any); ok {
			api.Server = str(s["url"])
		}
	} else if host := str(doc["host"]); host != "" {
		scheme := "https"
		if ss, ok := doc["schemes"].([]any); ok && len(ss) > 0 {
			scheme = str(ss[0])
		}
		api.Server = scheme + "://" + host + str(doc["basePath"])
	}
	bearer := hasBearer(doc)

	type op struct{ method, path, md string }
	byTag := map[string][]op{}
	keys := make([]string, 0, len(paths))
	for p := range paths {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	for _, p := range keys {
		item, _ := paths[p].(map[string]any)
		for _, m := range methods {
			o, ok := item[m].(map[string]any)
			if !ok {
				continue
			}
			tag := "API"
			if tags, ok := o["tags"].([]any); ok && len(tags) > 0 && str(tags[0]) != "" {
				tag = str(tags[0])
			}
			var b strings.Builder
			fmt.Fprintf(&b, "## %s %s\n\n", strings.ToUpper(m), p)
			if s := str(o["summary"]); s != "" {
				fmt.Fprintf(&b, "%s\n\n", s)
			}
			if d := str(o["description"]); d != "" && d != str(o["summary"]) {
				fmt.Fprintf(&b, "%s\n\n", d)
			}
			b.WriteString("```request\n")
			fmt.Fprintf(&b, "%s {{base}}%s\n", strings.ToUpper(m), p)
			if bearer {
				b.WriteString("Authorization: Bearer {{token}}\n")
			}
			if body := exampleBody(o); body != "" {
				b.WriteString("Content-Type: application/json\n\n" + body + "\n")
			}
			b.WriteString("\nexpect status < 400\n```\n")
			byTag[tag] = append(byTag[tag], op{m, p, b.String()})
		}
	}
	tags := make([]string, 0, len(byTag))
	for t := range byTag {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	for _, t := range tags {
		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\n", t)
		for _, o := range byTag[t] {
			b.WriteString(o.md + "\n")
		}
		api.Pages = append(api.Pages, Page{Title: t, Key: t, Markdown: b.String()})
	}
	return api, nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func hasBearer(doc map[string]any) bool {
	comps, _ := doc["components"].(map[string]any)
	schemes, _ := comps["securitySchemes"].(map[string]any)
	if schemes == nil {
		schemes, _ = doc["securityDefinitions"].(map[string]any)
	}
	for _, s := range schemes {
		m, _ := s.(map[string]any)
		if strings.EqualFold(str(m["scheme"]), "bearer") || str(m["type"]) == "oauth2" || (str(m["type"]) == "apiKey" && strings.EqualFold(str(m["name"]), "authorization")) {
			return true
		}
	}
	return false
}

// exampleBody returns a JSON example of the request body, if the spec has one.
func exampleBody(o map[string]any) string {
	rb, _ := o["requestBody"].(map[string]any)
	content, _ := rb["content"].(map[string]any)
	media, _ := content["application/json"].(map[string]any)
	ex := media["example"]
	if ex == nil {
		if exs, ok := media["examples"].(map[string]any); ok {
			for _, e := range exs {
				if m, ok := e.(map[string]any); ok {
					ex = m["value"]
					break
				}
			}
		}
	}
	if ex == nil {
		return ""
	}
	b, err := json.MarshalIndent(ex, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}
