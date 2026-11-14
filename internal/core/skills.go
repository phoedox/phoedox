package core

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// AI skills: editors mark a page as a skill with a name and a description of
// when to use it. AI clients list the skills over MCP and load the matching
// one; the skills can also be downloaded as SKILL.md folders. Only people
// (not AI clients) can mark pages, so an AI cannot turn text into
// instructions for itself (ADR-0049).

var skillNameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// SkillInput names a skill and says when to use it.
type SkillInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// SkillInfo is a skill with its page and project.
type SkillInfo struct {
	store.Skill
	Project  string `json:"project"`
	Document string `json:"document"` // page slug
	Title    string `json:"title"`
}

// SkillFile is a file attached to a skill's page.
type SkillFile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
}

// SkillContent is a skill with its instructions and attached files.
type SkillContent struct {
	SkillInfo
	Instructions string      `json:"instructions"`
	Files        []SkillFile `json:"files"`
}

func validSkill(in SkillInput) (SkillInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if len(in.Name) > 64 || !skillNameRe.MatchString(in.Name) || strings.Contains(in.Name, "anthropic") || strings.Contains(in.Name, "claude") {
		return in, invalid("validation.skill_name", map[string]any{"max": 64}, "skill name: lowercase letters, digits and hyphens, at most 64")
	}
	if in.Description == "" || len([]rune(in.Description)) > 1024 || strings.ContainsAny(in.Description, "<>") {
		return in, invalid("validation.skill_description", map[string]any{"max": 1024}, "skill description: 1 to 1024 characters, no angle brackets")
	}
	return in, nil
}

// PageSkill returns the skill of a page, or nil if the page is none (readers).
func (s *Service) PageSkill(ctx context.Context, a Actor, ref, docRef string) (*store.Skill, error) {
	_, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return nil, err
	}
	sk, err := s.DB.GetSkill(ctx, doc.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	return &sk, err
}

// SetPageSkill marks a page as a skill or changes it (editors, not AI clients).
func (s *Service) SetPageSkill(ctx context.Context, a Actor, ref, docRef string, in SkillInput) (*store.Skill, error) {
	if a.Via == "mcp" || a.Plugin != "" {
		return nil, ErrForbidden
	}
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if doc.Type != store.DocMarkdown {
		return nil, invalid("validation.skill_type", nil, "only text pages can be skills")
	}
	if in, err = validSkill(in); err != nil {
		return nil, err
	}
	sk := store.Skill{DocumentID: doc.ID, ProjectID: p.ID, Name: in.Name, Description: in.Description, UpdatedBy: a.String(), UpdatedAt: store.Now()}
	if err := s.DB.SaveSkill(ctx, sk); errors.Is(err, store.ErrConflict) {
		return nil, invalid("validation.skill_name_taken", map[string]any{"name": in.Name}, "another page already uses this skill name")
	} else if err != nil {
		return nil, err
	}
	s.event(ctx, a, "document.skill", doc.ID, map[string]any{"name": sk.Name})
	return &sk, nil
}

// RemovePageSkill turns a skill back into an ordinary page (editors, not AI clients).
func (s *Service) RemovePageSkill(ctx context.Context, a Actor, ref, docRef string) error {
	if a.Via == "mcp" || a.Plugin != "" {
		return ErrForbidden
	}
	_, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return err
	}
	if err := s.DB.DeleteSkill(ctx, doc.ID); err != nil {
		return err
	}
	s.event(ctx, a, "document.skill_removed", doc.ID, nil)
	return nil
}

// Skills lists the skills the actor can read, of one project or (ref "")
// of all their projects.
func (s *Service) Skills(ctx context.Context, a Actor, ref string) ([]SkillInfo, error) {
	var ps []store.Project
	if ref == "" {
		all, err := s.Projects(ctx, a)
		if err != nil {
			return nil, err
		}
		ps = all
	} else {
		p, err := s.project(ctx, a, ref, store.RoleDoc)
		if err != nil {
			return nil, err
		}
		ps = []store.Project{p}
	}
	out := []SkillInfo{}
	for _, p := range ps {
		sks, err := s.DB.Skills(ctx, p.ID)
		if err != nil || len(sks) == 0 {
			if err != nil {
				return nil, err
			}
			continue
		}
		allowed, err := s.docFilter(ctx, a, p)
		if err != nil {
			return nil, err
		}
		for _, sk := range sks {
			doc, err := s.DB.GetDocument(ctx, p.ID, sk.DocumentID)
			if err != nil || !allowed(doc.ID, doc.Restricted) {
				continue
			}
			out = append(out, SkillInfo{Skill: sk, Project: p.Slug, Document: doc.Slug, Title: doc.Title})
		}
	}
	return out, nil
}

// Skill loads one skill by name (or by its page) with its instructions.
func (s *Service) Skill(ctx context.Context, a Actor, ref, name string) (SkillContent, error) {
	p, err := s.project(ctx, a, ref, store.RoleDoc)
	if err != nil {
		return SkillContent{}, err
	}
	sk, err := s.DB.SkillByName(ctx, p.ID, name)
	docRef := sk.DocumentID
	if errors.Is(err, store.ErrNotFound) {
		docRef = name // a page slug or id works too
	} else if err != nil {
		return SkillContent{}, err
	}
	_, doc, err := s.docAccess(ctx, a, p.Slug, docRef, store.RoleReader)
	if err != nil {
		return SkillContent{}, err
	}
	if sk, err = s.DB.GetSkill(ctx, doc.ID); err != nil {
		return SkillContent{}, err
	}
	full, err := s.DB.GetDocument(ctx, p.ID, doc.ID)
	if err != nil {
		return SkillContent{}, err
	}
	out := SkillContent{SkillInfo: SkillInfo{Skill: sk, Project: p.Slug, Document: doc.Slug, Title: doc.Title}, Instructions: markdownOf(full), Files: []SkillFile{}}
	if fs, err := s.Files(ctx, a, p.Slug, FileFilter{Document: doc.ID}); err == nil {
		for _, f := range fs {
			out.Files = append(out.Files, SkillFile{ID: f.ID, Name: f.Name, Mime: f.Mime, Size: f.Size})
		}
	}
	return out, nil
}

// SkillFileLimit caps the attachments copied into a skill download.
const SkillFileLimit = 25 << 20

// SkillMarkdown is the SKILL.md of a skill: front matter, then the page.
func SkillMarkdown(c SkillContent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\nname: %s\ndescription: %s\n---\n\n", c.Name, yamlString(c.Description))
	b.WriteString(strings.TrimSpace(c.Instructions))
	b.WriteString("\n")
	if len(c.Files) > 0 {
		b.WriteString("\n## Files\n\n")
		for _, f := range c.Files {
			fmt.Fprintf(&b, "- [%s](files/%s)\n", f.Name, skillFileName(f))
		}
	}
	return b.String()
}

func yamlString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ", "\r", "").Replace(s) + `"`
}

func skillFileName(f SkillFile) string {
	n := path.Base(strings.ReplaceAll(f.Name, `\`, "/"))
	if n == "." || n == "/" || n == ".." || n == "" {
		n = f.ID
	}
	return n
}

// WriteSkills writes skills as a zip: one folder per skill with SKILL.md and
// its attached files (up to SkillFileLimit in total). names empty = all
// skills of the project the actor can read.
func (s *Service) WriteSkills(ctx context.Context, a Actor, ref string, names []string, w io.Writer) error {
	if len(names) == 0 {
		list, err := s.Skills(ctx, a, ref)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			return ErrNotFound
		}
		for _, sk := range list {
			names = append(names, sk.Name)
		}
	}
	contents := make([]SkillContent, 0, len(names))
	for _, n := range names {
		c, err := s.Skill(ctx, a, ref, n)
		if err != nil {
			return err
		}
		contents = append(contents, c)
	}
	z := zip.NewWriter(w)
	budget := int64(SkillFileLimit)
	for _, c := range contents {
		kept := c.Files[:0:0]
		seen := map[string]bool{}
		for _, f := range c.Files {
			if f.Size > budget {
				continue
			}
			if seen[skillFileName(f)] {
				f.Name = f.ID + "-" + skillFileName(f)
			}
			seen[skillFileName(f)] = true
			fc, err := s.OpenFile(ctx, a, f.ID, 0, false)
			if err != nil {
				continue
			}
			fw, err := z.Create(c.Name + "/files/" + skillFileName(f))
			if err == nil {
				_, err = io.Copy(fw, io.LimitReader(fc.Reader, budget))
			}
			_ = fc.Reader.Close()
			if err != nil {
				return err
			}
			budget -= f.Size
			kept = append(kept, f)
		}
		c.Files = kept
		fw, err := z.Create(c.Name + "/SKILL.md")
		if err != nil {
			return err
		}
		if _, err := io.WriteString(fw, SkillMarkdown(c)); err != nil {
			return err
		}
	}
	return z.Close()
}
