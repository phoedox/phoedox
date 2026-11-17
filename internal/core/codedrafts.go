package core

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Documentation changes tied to code (Phoedox replaces docs/ in code
// repositories): a draft of a page, floor plan or decision can belong to a
// pull request or branch of the project's code connection. The pull request
// gets a "Docs changes" comment with links; when it is merged the drafts are
// published, when it is closed without merging they are discarded. Phoedox
// learns about it from the repository's webhook or, without one, by asking
// the forge from time to time.

// DraftCodeInput ties a draft to a pull request or a branch.
type DraftCodeInput struct {
	// Repo is the repository (id or owner/name); empty = the project's only
	// repository.
	Repo   string `json:"repo,omitempty"`
	PR     int    `json:"pr,omitempty"`
	Branch string `json:"branch,omitempty"`
}

var prRefRe = regexp.MustCompile(`^(?:([\w.-]+/[\w.-]+))?(?:#(\d+)|@(\S+))$`)

// ParsePullRequest reads "owner/repo#12", "#12", "12", "owner/repo@branch"
// or "@branch" (also a pull request URL).
func ParsePullRequest(s string) (DraftCodeInput, bool) {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return DraftCodeInput{PR: n}, true
	}
	if i := strings.Index(s, "://"); i > 0 {
		// https://github.com/owner/repo/pull/12, …/pulls/12 (Gitea)
		parts := strings.Split(strings.Trim(s[i+3:], "/"), "/")
		if len(parts) >= 5 && (parts[3] == "pull" || parts[3] == "pulls") {
			if n, err := strconv.Atoi(parts[4]); err == nil && n > 0 {
				return DraftCodeInput{Repo: parts[1] + "/" + parts[2], PR: n}, true
			}
		}
		return DraftCodeInput{}, false
	}
	m := prRefRe.FindStringSubmatch(s)
	if m == nil {
		return DraftCodeInput{}, false
	}
	in := DraftCodeInput{Repo: m[1], Branch: m[3]}
	in.PR, _ = strconv.Atoi(m[2])
	return in, in.PR > 0 || in.Branch != ""
}

func (s *Service) codeRepoFor(ctx context.Context, p store.Project, ref string) (store.CodeRepo, error) {
	if strings.TrimSpace(ref) != "" {
		return s.findRepo(ctx, p, ref)
	}
	repos, err := s.DB.CodeRepos(ctx, p.ID)
	if err != nil {
		return store.CodeRepo{}, err
	}
	if len(repos) != 1 {
		return store.CodeRepo{}, invalid("validation.code_repo_needed", nil, "name the repository (owner/name)")
	}
	return repos[0], nil
}

// LinkDraftCode ties a document's draft to a pull request or branch (and
// opens the draft if there is none). Later writes to the document go into
// that draft until the pull request is merged or closed.
func (s *Service) LinkDraftCode(ctx context.Context, a Actor, ref, docRef string, in DraftCodeInput) (DraftInfo, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return DraftInfo{}, err
	}
	if !hasDrafts(doc.Type) {
		return DraftInfo{}, errDraftOnly()
	}
	in.Branch = strings.TrimSpace(in.Branch)
	if in.PR <= 0 && in.Branch == "" {
		return DraftInfo{}, invalid("validation.code_pr_needed", nil, "give a pull request number or a branch")
	}
	repo, err := s.codeRepoFor(ctx, p, in.Repo)
	if err != nil {
		return DraftInfo{}, err
	}
	d, err := s.startDraft(ctx, a, doc)
	if err != nil {
		return DraftInfo{}, err
	}
	if d.CodeRepo != "" && (d.CodeRepo != repo.ID || (d.CodePR != 0 && in.PR != 0 && d.CodePR != in.PR)) {
		return DraftInfo{}, invalid("validation.draft_other_pr", map[string]any{"pr": d.CodePR}, "the draft already belongs to another pull request")
	}
	d.CodeRepo = repo.ID
	if in.PR > 0 {
		d.CodePR = in.PR
	}
	if in.Branch != "" {
		d.CodeBranch = in.Branch
	}
	d.UpdatedBy, d.UpdatedAt = a.String(), store.Now()
	if err := s.DB.SaveDraft(ctx, d); err != nil {
		return DraftInfo{}, err
	}
	s.event(ctx, a, "document.draft_code", doc.ID, map[string]any{"repo": repo.Owner + "/" + repo.Name, "pr": d.CodePR, "branch": d.CodeBranch})
	if d.CodePR > 0 {
		s.prComment(ctx, p, repo, d.CodePR)
	}
	return s.draftInfo(ctx, a, p, doc, d), nil
}

// UnlinkDraftCode releases a draft from its pull request (it stays open).
func (s *Service) UnlinkDraftCode(ctx context.Context, a Actor, ref, docRef string) (DraftInfo, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return DraftInfo{}, err
	}
	d, err := s.DB.GetDraft(ctx, doc.ID)
	if err != nil {
		return DraftInfo{}, err
	}
	repoID, pr := d.CodeRepo, d.CodePR
	d.CodeRepo, d.CodePR, d.CodeBranch = "", 0, ""
	if err := s.DB.SaveDraft(ctx, d); err != nil {
		return DraftInfo{}, err
	}
	s.event(ctx, a, "document.draft_code", doc.ID, map[string]any{"unlinked": true})
	if repo, err := s.DB.GetCodeRepo(ctx, p.ID, repoID); err == nil && pr > 0 {
		s.prComment(ctx, p, repo, pr)
	}
	return s.draftInfo(ctx, a, p, doc, d), nil
}

// PRDocChange is a document changed in a pull request.
type PRDocChange struct {
	Document   string `json:"document"` // slug
	Title      string `json:"title"`
	DraftTitle string `json:"draftTitle,omitempty"`
	Type       string `json:"type"`
	Number     int    `json:"number,omitempty"`
	URL        string `json:"url"`
	Version    int    `json:"version"` // published version the draft started from
	Outdated   bool   `json:"outdated,omitempty"`
	Added      int    `json:"added"`
	Removed    int    `json:"removed"`
	// Diff is a unified line diff (shortened for long changes).
	Diff      string     `json:"diff"`
	UpdatedAt store.Time `json:"updatedAt"`
}

// PRDocChanges is what changed in the docs for a pull request.
type PRDocChanges struct {
	Repo    string        `json:"repo"`
	PR      int           `json:"pr,omitempty"`
	Branch  string        `json:"branch,omitempty"`
	URL     string        `json:"url,omitempty"`
	Changes []PRDocChange `json:"changes"`
}

// PullRequestDocs lists the documentation changes of a pull request or
// branch with a diff each (editors).
func (s *Service) PullRequestDocs(ctx context.Context, a Actor, ref string, in DraftCodeInput) (PRDocChanges, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return PRDocChanges{}, err
	}
	repo, err := s.codeRepoFor(ctx, p, in.Repo)
	if err != nil {
		return PRDocChanges{}, err
	}
	out := PRDocChanges{Repo: repo.Owner + "/" + repo.Name, PR: in.PR, Branch: in.Branch, Changes: []PRDocChange{}}
	if in.PR > 0 {
		out.URL = prURL(repo, in.PR)
	}
	list, err := s.DB.CodeDrafts(ctx, repo.ID, in.PR, in.Branch)
	if err != nil {
		return out, err
	}
	allowed, err := s.docFilter(ctx, a, p)
	if err != nil {
		return out, err
	}
	for _, d := range list {
		doc, err := s.DB.GetDocument(ctx, p.ID, d.DocumentID)
		if err != nil || !allowed(doc.ID, doc.Restricted) {
			continue
		}
		before, after := draftBody(doc), d.Markdown
		if doc.Type == store.DocADR {
			before, after = adrDiffText(doc.Content), adrDiffText([]byte(d.Markdown))
		}
		if d.Title != doc.Title {
			before, after = "# "+doc.Title+"\n"+before, "# "+d.Title+"\n"+after
		}
		add, rem, diff := lineDiff(before, after, 400)
		c := PRDocChange{Document: doc.Slug, Title: doc.Title, Type: doc.Type, Number: doc.Number, URL: s.PublicURL + "/p/" + p.Slug + "/d/" + doc.Slug,
			Version: d.BaseVersion, Outdated: doc.Version != d.BaseVersion, Added: add, Removed: rem, Diff: diff, UpdatedAt: d.UpdatedAt}
		if d.Title != doc.Title {
			c.DraftTitle = d.Title
		}
		out.Changes = append(out.Changes, c)
	}
	return out, nil
}

// adrDiffText renders a decision's fields as text for diffs.
func adrDiffText(raw []byte) string {
	var c ADR
	_ = json.Unmarshal(raw, &c)
	var b strings.Builder
	fmt.Fprintf(&b, "Status: %s\n", c.Status)
	for _, part := range [][2]string{{"Context", c.Context}, {"Decision", c.Decision}, {"Consequences", c.Consequences}, {"Alternatives", c.Alternatives}} {
		if strings.TrimSpace(part[1]) != "" {
			fmt.Fprintf(&b, "\n## %s\n%s\n", part[0], strings.TrimSpace(part[1]))
		}
	}
	return b.String()
}

// lineDiff is a small unified line diff (LCS); long texts are compared
// without context lines being shortened only in the output.
func lineDiff(a, b string, maxLines int) (added, removed int, out string) {
	x, y := strings.Split(strings.TrimRight(a, "\n"), "\n"), strings.Split(strings.TrimRight(b, "\n"), "\n")
	if a == "" {
		x = nil
	}
	if b == "" {
		y = nil
	}
	const limit = 3000
	if len(x) > limit || len(y) > limit {
		return len(y), len(x), "(too long for a line diff)"
	}
	n, m := len(x), len(y)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	type line struct {
		op   byte
		text string
	}
	var lines []line
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && x[i] == y[j]:
			lines = append(lines, line{' ', x[i]})
			i, j = i+1, j+1
		case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]):
			lines = append(lines, line{'-', x[i]})
			removed++
			i++
		default:
			lines = append(lines, line{'+', y[j]})
			added++
			j++
		}
	}
	// Keep two lines of context around changes.
	keep := make([]bool, len(lines))
	for k, l := range lines {
		if l.op != ' ' {
			for c := max(0, k-2); c <= min(len(lines)-1, k+2); c++ {
				keep[c] = true
			}
		}
	}
	var buf strings.Builder
	shown, gap := 0, false
	for k, l := range lines {
		if !keep[k] {
			gap = true
			continue
		}
		if shown >= maxLines {
			buf.WriteString("…\n")
			break
		}
		if gap && shown > 0 {
			buf.WriteString("@@\n")
		}
		gap = false
		buf.WriteByte(l.op)
		buf.WriteString(l.text)
		buf.WriteByte('\n')
		shown++
	}
	return added, removed, buf.String()
}

func prURL(r store.CodeRepo, pr int) string {
	if r.Provider == "github" {
		return fmt.Sprintf("%s/pull/%d", strings.TrimRight(r.WebURL, "/"), pr)
	}
	return fmt.Sprintf("%s/pulls/%d", strings.TrimRight(r.WebURL, "/"), pr)
}

// codeActor is who publishes or discards drafts of a pull request.
func codeActor(r store.CodeRepo, pr int) Actor {
	return Actor{User: store.User{Name: fmt.Sprintf("%s/%s#%d", r.Owner, r.Name, pr)}, Via: "code", TokenID: fmt.Sprintf("%s#%d", r.ID, pr)}
}

// codeDraftsDone publishes (merged) or discards (closed) the drafts of a
// pull request or branch.
func (s *Service) codeDraftsDone(ctx context.Context, r store.CodeRepo, pr int, branch string, merged bool) (int, error) {
	list, err := s.DB.CodeDrafts(ctx, r.ID, pr, branch)
	if err != nil || len(list) == 0 {
		return 0, err
	}
	p, err := s.DB.ProjectFor(ctx, r.ProjectID, "")
	if err != nil {
		return 0, err
	}
	a := codeActor(r, pr)
	n := 0
	for _, d := range list {
		doc, err := s.DB.GetDocument(ctx, p.ID, d.DocumentID)
		if err != nil {
			continue
		}
		if merged {
			if _, err := s.publishDraft(ctx, a, p, doc, d.Draft); err != nil {
				slog.Warn("publish draft of a merged pull request", "doc", doc.ID, "err", err)
				continue
			}
			s.notifyParticipants(ctx, a, p, doc, d.Draft, "draft.approved", "")
		} else {
			if err := s.DB.DeleteDraft(ctx, doc.ID); err != nil {
				continue
			}
			s.event(ctx, a, "document.draft_cancel", doc.ID, map[string]any{"participants": d.Participants, "pr": pr, "closed": true})
			s.draftEnded(ctx, a, doc.ID, "cancelled")
		}
		n++
	}
	return n, nil
}

// --- webhook ---------------------------------------------------------------

// CodeWebhook is how a repository tells Phoedox about pull requests.
type CodeWebhook struct {
	URL    string   `json:"url"`
	Secret string   `json:"secret"`
	Events []string `json:"events"`
}

// CodeRepoWebhook returns (and on first use or with rotate, creates) the
// webhook address and secret of a repository (project owners).
func (s *Service) CodeRepoWebhook(ctx context.Context, a Actor, ref, repoID string, rotate bool) (CodeWebhook, error) {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return CodeWebhook{}, err
	}
	r, err := s.findRepo(ctx, p, repoID)
	if err != nil {
		return CodeWebhook{}, err
	}
	if s.Box == nil {
		return CodeWebhook{}, errors.New("no secret box")
	}
	secret := ""
	if r.WebhookSecret != "" && !rotate {
		b, err := s.Box.Open(r.WebhookSecret, "codehook:"+r.ID)
		if err == nil {
			secret = string(b)
		}
	}
	if secret == "" {
		buf := make([]byte, 24)
		_, _ = rand.Read(buf)
		secret = hex.EncodeToString(buf)
		sealed, err := s.Box.Seal([]byte(secret), "codehook:"+r.ID)
		if err != nil {
			return CodeWebhook{}, err
		}
		if err := s.DB.SetCodeRepoWebhookSecret(ctx, p.ID, r.ID, sealed); err != nil {
			return CodeWebhook{}, err
		}
		s.event(ctx, a, "code.webhook_secret", r.ID, map[string]any{"repo": r.Owner + "/" + r.Name, "rotated": rotate})
	}
	return CodeWebhook{URL: s.PublicURL + "/api/v1/hooks/code/" + r.ID, Secret: secret, Events: []string{"pull_request"}}, nil
}

// HandleCodeWebhook takes a pull request event of GitHub or Gitea for a
// repository (signature checked against the repository's secret).
func (s *Service) HandleCodeWebhook(ctx context.Context, repoID, event, signature string, body []byte) error {
	r, err := s.DB.CodeRepoByID(ctx, repoID)
	if err != nil || r.WebhookSecret == "" || s.Box == nil {
		return ErrNotFound
	}
	secret, err := s.Box.Open(r.WebhookSecret, "codehook:"+r.ID)
	if err != nil {
		return ErrForbidden
	}
	m := hmac.New(sha256.New, secret)
	m.Write(body)
	want := hex.EncodeToString(m.Sum(nil))
	if !hmac.Equal([]byte(strings.TrimPrefix(strings.ToLower(signature), "sha256=")), []byte(want)) {
		return ErrForbidden
	}
	if event != "pull_request" {
		return nil // ping and other events
	}
	var ev struct {
		Action      string `json:"action"`
		Number      int    `json:"number"`
		PullRequest struct {
			Number int  `json:"number"`
			Merged bool `json:"merged"`
			Head   struct {
				Ref string `json:"ref"`
			} `json:"head"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return invalid("validation.code_webhook", nil, "not a pull request event")
	}
	pr := ev.Number
	if pr == 0 {
		pr = ev.PullRequest.Number
	}
	branch := ev.PullRequest.Head.Ref
	switch ev.Action {
	case "opened", "reopened", "synchronize", "synchronized", "edited":
		// Drafts tied to the branch learn their pull request.
		if branch == "" || pr == 0 {
			return nil
		}
		list, err := s.DB.CodeDrafts(ctx, r.ID, 0, branch)
		if err != nil {
			return err
		}
		changed := false
		for _, d := range list {
			if d.CodePR == 0 {
				d.CodePR = pr
				changed = true
				_ = s.DB.SaveDraft(ctx, d.Draft)
			}
		}
		if changed {
			if p, err := s.DB.ProjectFor(ctx, r.ProjectID, ""); err == nil {
				s.prComment(ctx, p, r, pr)
			}
		}
	case "closed":
		_, err := s.codeDraftsDone(ctx, r, pr, branch, ev.PullRequest.Merged)
		return err
	}
	return nil
}

// checkCodeDrafts asks the forge about pull requests with open drafts
// (without a webhook, or when one was missed).
func (s *Service) checkCodeDrafts(ctx context.Context, r store.CodeRepo) {
	list, err := s.DB.CodeDrafts(ctx, r.ID, 0, "")
	if err != nil || len(list) == 0 {
		return
	}
	f, err := s.forge(ctx, r)
	if err != nil {
		return
	}
	seen := map[int]bool{}
	for _, d := range list {
		if d.CodePR == 0 || seen[d.CodePR] {
			continue
		}
		seen[d.CodePR] = true
		var pr struct {
			State  string `json:"state"`
			Merged bool   `json:"merged"`
			Head   struct {
				Ref string `json:"ref"`
			} `json:"head"`
		}
		if err := f.get(ctx, f.repoPath(fmt.Sprintf("/pulls/%d", d.CodePR)), &pr); err != nil {
			continue
		}
		if pr.State == "closed" {
			if _, err := s.codeDraftsDone(ctx, r, d.CodePR, "", pr.Merged); err != nil {
				slog.Warn("pull request drafts", "repo", r.Owner+"/"+r.Name, "pr", d.CodePR, "err", err)
			}
		}
	}
}

// --- pull request comment ---------------------------------------------------

const prCommentMarker = "<!-- phoedox-docs -->"

// prComment writes or updates the "Docs changes" comment of a pull request
// (needs the repository's token; failures are only logged).
func (s *Service) prComment(ctx context.Context, p store.Project, r store.CodeRepo, pr int) {
	if r.TokenVar == "" || pr <= 0 {
		return
	}
	f, err := s.forge(ctx, r)
	if err != nil {
		return
	}
	list, err := s.DB.CodeDrafts(ctx, r.ID, pr, "")
	if err != nil {
		return
	}
	var b strings.Builder
	b.WriteString(prCommentMarker + "\n### Docs changes\n\n")
	if len(list) == 0 {
		b.WriteString("No documentation changes in Phoedox are tied to this pull request any more.\n")
	} else {
		fmt.Fprintf(&b, "%d page(s) in Phoedox change with this pull request:\n\n", len(list))
		for _, d := range list {
			doc, err := s.DB.GetDocument(ctx, p.ID, d.DocumentID)
			if err != nil {
				continue
			}
			label := doc.Title
			if doc.Type == store.DocADR {
				label = DecisionRef(doc.Number) + " " + label
			}
			fmt.Fprintf(&b, "- [%s](%s/p/%s/d/%s) (draft)\n", label, s.PublicURL, p.Slug, doc.Slug)
		}
		b.WriteString("\nThey are published when this pull request is merged and discarded when it is closed without merging.\n")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var comments []struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
	}
	_ = f.get(ctx, f.repoPath(fmt.Sprintf("/issues/%d/comments?per_page=100", pr)), &comments)
	payload := map[string]string{"body": b.String()}
	for _, c := range comments {
		if strings.Contains(c.Body, prCommentMarker) {
			if err := f.send(ctx, http.MethodPatch, f.repoPath(fmt.Sprintf("/issues/comments/%d", c.ID)), payload); err != nil {
				slog.Warn("pull request comment", "repo", r.Owner+"/"+r.Name, "pr", pr, "err", err)
			}
			return
		}
	}
	if len(list) == 0 {
		return
	}
	if err := f.send(ctx, http.MethodPost, f.repoPath(fmt.Sprintf("/issues/%d/comments", pr)), payload); err != nil {
		slog.Warn("pull request comment", "repo", r.Owner+"/"+r.Name, "pr", pr, "err", err)
	}
}

// send writes to the forge API (POST/PATCH with a JSON body).
func (f forge) send(ctx context.Context, method, path string, body any) error {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, method, f.repo.APIURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Phoedox")
	if f.repo.Provider == "github" {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("Authorization", "Bearer "+f.token)
	} else {
		req.Header.Set("Authorization", "token "+f.token)
	}
	res, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return nil
}
