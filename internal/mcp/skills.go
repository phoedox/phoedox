package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.yaml.in/yaml/v3"

	"github.com/phoedox/phoedox/resources"
)

// Skills over MCP (extension io.modelcontextprotocol/skills, SEP-2640): the
// Phoedox skill (resources/skills) is served as skill://phoedox/… resources
// and listed by skills/list, so clients that support the extension load it
// with the connector – no separate download of /phoedox-skill.zip.

const skillsExtension = "io.modelcontextprotocol/skills"

// skillsTTL is how long clients may cache the listing (the skill only
// changes with a new Phoedox version).
const skillsTTL = 300000

type skillResource struct {
	URI    string `json:"uri"`
	Digest string `json:"digest"`
	Size   int    `json:"size"`
}

type skillEntry struct {
	URI         string          `json:"uri"`
	Frontmatter map[string]any  `json:"frontmatter"`
	Resources   []skillResource `json:"resources"`
}

type skillsListParams struct {
	sdk.ParamsBase
	Cursor string `json:"cursor,omitempty"`
}

type skillsListResult struct {
	sdk.ResultBase
	Skills     []skillEntry `json:"skills"`
	NextCursor string       `json:"nextCursor,omitempty"`
	ResultType string       `json:"resultType"`
	TTLMs      int          `json:"ttlMs"`
	CacheScope string       `json:"cacheScope"`
}

type skillsGetParams struct {
	sdk.ParamsBase
	URI string `json:"uri"`
}

// skillsGetResult carries the entry both inline and as "skill", so clients
// reading either shape find it.
type skillsGetResult struct {
	sdk.ResultBase
	skillEntry
	Skill      skillEntry `json:"skill"`
	ResultType string     `json:"resultType"`
	TTLMs      int        `json:"ttlMs"`
	CacheScope string     `json:"cacheScope"`
}

// servedSkills reads the embedded skills: one entry per SKILL.md, with every
// file of its folder.
func servedSkills() ([]skillEntry, map[string][]byte, error) {
	files := map[string][]byte{}
	byDir := map[string][]string{}
	for _, f := range resources.SkillFiles() {
		dir, _, _ := strings.Cut(f.Path, "/")
		uri := "skill://" + f.Path
		files[uri] = f.Content
		byDir[dir] = append(byDir[dir], uri)
	}
	var out []skillEntry
	for dir, uris := range byDir {
		main := "skill://" + dir + "/SKILL.md"
		fm, err := frontmatter(files[main])
		if err != nil {
			return nil, nil, fmt.Errorf("skill %s: %w", dir, err)
		}
		if fm["name"] != dir {
			return nil, nil, fmt.Errorf("skill %s: name %v must equal the folder", dir, fm["name"])
		}
		sort.Strings(uris)
		e := skillEntry{URI: main, Frontmatter: fm}
		for _, u := range uris {
			sum := sha256.Sum256(files[u])
			e.Resources = append(e.Resources, skillResource{URI: u, Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: len(files[u])})
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URI < out[j].URI })
	return out, files, nil
}

// frontmatter reads the YAML block at the top of a SKILL.md.
func frontmatter(md []byte) (map[string]any, error) {
	rest, ok := bytes.CutPrefix(md, []byte("---\n"))
	if !ok {
		return nil, fmt.Errorf("SKILL.md must start with frontmatter")
	}
	head, _, ok := bytes.Cut(rest, []byte("\n---"))
	if !ok {
		return nil, fmt.Errorf("unterminated frontmatter")
	}
	fm := map[string]any{}
	if err := yaml.Unmarshal(head, &fm); err != nil {
		return nil, err
	}
	if fm["name"] == nil || fm["description"] == nil {
		return nil, fmt.Errorf("frontmatter needs name and description")
	}
	return fm, nil
}

// skillsCapabilities declares the extension (and resources, which it needs).
func skillsCapabilities() *sdk.ServerCapabilities {
	return &sdk.ServerCapabilities{
		Extensions: map[string]any{skillsExtension: map[string]any{}},
		Resources:  &sdk.ResourceCapabilities{ListChanged: true},
		Tools:      &sdk.ToolCapabilities{ListChanged: true},
	}
}

// registerSkills serves the skill files as resources and answers
// skills/list and skills/get.
func registerSkills(s *sdk.Server) {
	entries, files, err := servedSkills()
	if err != nil {
		panic(err) // embedded at build time; a test checks it
	}
	for uri, content := range files {
		c := string(content)
		mime := "text/markdown"
		if path.Ext(uri) != ".md" {
			mime = "text/plain"
		}
		name := strings.TrimPrefix(uri, "skill://")
		s.AddResource(&sdk.Resource{URI: uri, Name: name, Title: "Phoedox skill: " + name, MIMEType: mime, Size: int64(len(content))},
			func(ctx context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
				return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: uri, MIMEType: mime, Text: c}}}, nil
			})
	}
	_ = sdk.AddReceivingCustomMethod(s, "skills/list", func(ctx context.Context, _ *sdk.ServerSession, p *skillsListParams) (*skillsListResult, error) {
		return &skillsListResult{Skills: entries, ResultType: "complete", TTLMs: skillsTTL, CacheScope: "public"}, nil
	})
	_ = sdk.AddReceivingCustomMethod(s, "skills/get", func(ctx context.Context, _ *sdk.ServerSession, p *skillsGetParams) (*skillsGetResult, error) {
		for _, e := range entries {
			if p != nil && e.URI == p.URI {
				return &skillsGetResult{skillEntry: e, Skill: e, ResultType: "complete", TTLMs: skillsTTL, CacheScope: "public"}, nil
			}
		}
		u := ""
		if p != nil {
			u = p.URI
		}
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "No skill is served at " + u}
	})
}
