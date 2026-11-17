// Package tracker creates and follows issues in external trackers: GitHub,
// Gitea/Forgejo, GitLab and Jira. Feedback topics are forwarded there; new
// reports become "+n" comments, and a closed issue counts as a done ticket.
// Plain HTTP, no SDKs.
package tracker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Kinds of trackers.
const (
	GitHub = "github"
	Gitea  = "gitea"
	GitLab = "gitlab"
	Jira   = "jira"
)

// Kinds lists the supported trackers.
var Kinds = []string{GitHub, Gitea, GitLab, Jira}

// Config describes one tracker.
type Config struct {
	Kind string
	// URL is the web address of the repository (GitHub, Gitea, GitLab:
	// https://host/owner/name, GitLab also with sub-groups) or of the Jira
	// site (https://example.atlassian.net).
	URL string
	// Project is the Jira project key (e.g. GAME); IssueType the Jira issue
	// type (default Bug).
	Project   string
	IssueType string
	// User is the Jira account e-mail for API tokens (basic auth); empty
	// sends the token as a bearer token (Jira Data Center).
	User   string
	Token  string
	Labels []string
	// APIURL overrides the API base (tests).
	APIURL string
}

// Issue is a created issue.
type Issue struct {
	Ref string `json:"ref"` // number (forges) or key (Jira)
	URL string `json:"url"`
}

// ErrConfig is returned for a tracker address that cannot be used.
var ErrConfig = errors.New("tracker: invalid address")

var (
	partRe    = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
	jiraKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,30}$`)
	refRe     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
)

// Client talks to one tracker.
type Client struct {
	cfg  Config
	api  string
	repo string // owner/name (forges) or URL-escaped path (GitLab)
	web  string
	http *http.Client
}

// New checks a configuration and returns a client. hc may be nil.
func New(cfg Config, hc *http.Client) (*Client, error) {
	if hc == nil {
		hc = http.DefaultClient
	}
	u, err := url.Parse(strings.TrimSpace(cfg.URL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" {
		return nil, ErrConfig
	}
	base := u.Scheme + "://" + u.Host
	path := strings.Trim(strings.TrimSuffix(u.Path, ".git"), "/")
	c := &Client{cfg: cfg, http: hc}
	switch cfg.Kind {
	case GitHub, Gitea:
		parts := strings.Split(path, "/")
		if len(parts) < 2 || !partRe.MatchString(parts[len(parts)-1]) || !partRe.MatchString(parts[len(parts)-2]) {
			return nil, ErrConfig
		}
		owner, name := parts[len(parts)-2], parts[len(parts)-1]
		prefix := strings.Join(parts[:len(parts)-2], "/")
		c.repo = url.PathEscape(owner) + "/" + url.PathEscape(name)
		if cfg.Kind == GitHub {
			if prefix != "" {
				return nil, ErrConfig
			}
			c.api, c.web = "https://api.github.com", "https://github.com/"+owner+"/"+name
		} else {
			if prefix != "" {
				base += "/" + prefix
			}
			c.api, c.web = base+"/api/v1", base+"/"+owner+"/"+name
		}
	case GitLab:
		parts := strings.Split(path, "/")
		if len(parts) < 2 {
			return nil, ErrConfig
		}
		for _, p := range parts {
			if !partRe.MatchString(p) {
				return nil, ErrConfig
			}
		}
		c.repo = url.PathEscape(path)
		c.api, c.web = base+"/api/v4", base+"/"+path
	case Jira:
		if !jiraKeyRe.MatchString(cfg.Project) {
			return nil, ErrConfig
		}
		if path != "" {
			base += "/" + path
		}
		c.api, c.web = base+"/rest/api/2", base
		if c.cfg.IssueType == "" {
			c.cfg.IssueType = "Bug"
		}
	default:
		return nil, ErrConfig
	}
	if cfg.APIURL != "" {
		c.api = strings.TrimRight(cfg.APIURL, "/")
	}
	return c, nil
}

// Web is the tracker's web address (repository or Jira site).
func (c *Client) Web() string { return c.web }

// do sends a JSON request and decodes the answer into out (may be nil).
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.api+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Phoedox")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if t := c.cfg.Token; t != "" {
		switch c.cfg.Kind {
		case GitHub:
			req.Header.Set("Authorization", "Bearer "+t)
		case Gitea:
			req.Header.Set("Authorization", "token "+t)
		case GitLab:
			req.Header.Set("PRIVATE-TOKEN", t)
		case Jira:
			if c.cfg.User != "" {
				req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.cfg.User+":"+t)))
			} else {
				req.Header.Set("Authorization", "Bearer "+t)
			}
		}
	}
	if c.cfg.Kind == GitHub {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return &HTTPError{Status: res.StatusCode}
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// HTTPError is an answer outside 2xx.
type HTTPError struct{ Status int }

func (e *HTTPError) Error() string { return "tracker: HTTP " + strconv.Itoa(e.Status) }

// Check reads the repository or Jira project to test address and token.
func (c *Client) Check(ctx context.Context) error {
	switch c.cfg.Kind {
	case GitHub, Gitea:
		return c.do(ctx, http.MethodGet, "/repos/"+c.repo, nil, nil)
	case GitLab:
		return c.do(ctx, http.MethodGet, "/projects/"+c.repo, nil, nil)
	default:
		return c.do(ctx, http.MethodGet, "/project/"+url.PathEscape(c.cfg.Project), nil, nil)
	}
}

// Create opens an issue.
func (c *Client) Create(ctx context.Context, title, body string) (Issue, error) {
	title = strings.TrimSpace(title)
	switch c.cfg.Kind {
	case GitHub, Gitea:
		in := map[string]any{"title": title, "body": body}
		if c.cfg.Kind == GitHub && len(c.cfg.Labels) > 0 {
			in["labels"] = c.cfg.Labels // Gitea wants label ids
		}
		var out struct {
			Number  int    `json:"number"`
			HTMLURL string `json:"html_url"`
		}
		if err := c.do(ctx, http.MethodPost, "/repos/"+c.repo+"/issues", in, &out); err != nil {
			return Issue{}, err
		}
		return Issue{Ref: strconv.Itoa(out.Number), URL: out.HTMLURL}, nil
	case GitLab:
		in := map[string]any{"title": title, "description": body}
		if len(c.cfg.Labels) > 0 {
			in["labels"] = strings.Join(c.cfg.Labels, ",")
		}
		var out struct {
			IID    int    `json:"iid"`
			WebURL string `json:"web_url"`
		}
		if err := c.do(ctx, http.MethodPost, "/projects/"+c.repo+"/issues", in, &out); err != nil {
			return Issue{}, err
		}
		return Issue{Ref: strconv.Itoa(out.IID), URL: out.WebURL}, nil
	default:
		fields := map[string]any{"project": map[string]string{"key": c.cfg.Project}, "summary": title, "description": body,
			"issuetype": map[string]string{"name": c.cfg.IssueType}}
		if len(c.cfg.Labels) > 0 {
			labels := make([]string, len(c.cfg.Labels))
			for i, l := range c.cfg.Labels {
				labels[i] = strings.ReplaceAll(l, " ", "-") // Jira labels have no spaces
			}
			fields["labels"] = labels
		}
		var out struct {
			Key string `json:"key"`
		}
		if err := c.do(ctx, http.MethodPost, "/issue", map[string]any{"fields": fields}, &out); err != nil {
			return Issue{}, err
		}
		return Issue{Ref: out.Key, URL: c.web + "/browse/" + out.Key}, nil
	}
}

// Comment adds a comment to an issue.
func (c *Client) Comment(ctx context.Context, ref, body string) error {
	if !refRe.MatchString(ref) {
		return ErrConfig
	}
	switch c.cfg.Kind {
	case GitHub, Gitea:
		return c.do(ctx, http.MethodPost, "/repos/"+c.repo+"/issues/"+ref+"/comments", map[string]string{"body": body}, nil)
	case GitLab:
		return c.do(ctx, http.MethodPost, "/projects/"+c.repo+"/issues/"+ref+"/notes", map[string]string{"body": body}, nil)
	default:
		return c.do(ctx, http.MethodPost, "/issue/"+ref+"/comment", map[string]string{"body": body}, nil)
	}
}

// Closed tells whether an issue is closed (Jira: in a "done" status).
func (c *Client) Closed(ctx context.Context, ref string) (bool, error) {
	if !refRe.MatchString(ref) {
		return false, ErrConfig
	}
	switch c.cfg.Kind {
	case GitHub, Gitea, GitLab:
		path := "/repos/" + c.repo + "/issues/" + ref
		if c.cfg.Kind == GitLab {
			path = "/projects/" + c.repo + "/issues/" + ref
		}
		var out struct {
			State string `json:"state"`
		}
		if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
			return false, err
		}
		return out.State == "closed", nil
	default:
		var out struct {
			Fields struct {
				Status struct {
					StatusCategory struct {
						Key string `json:"key"`
					} `json:"statusCategory"`
				} `json:"status"`
			} `json:"fields"`
		}
		if err := c.do(ctx, http.MethodGet, "/issue/"+ref+"?fields=status", nil, &out); err != nil {
			return false, err
		}
		return out.Fields.Status.StatusCategory.Key == "done", nil
	}
}

// Name is a short display name: owner/name or the Jira project key.
func (c *Client) Name() string {
	switch c.cfg.Kind {
	case Jira:
		return c.cfg.Project
	default:
		u, _ := url.PathUnescape(c.repo)
		return u
	}
}

// String describes the client (for logs).
func (c *Client) String() string { return fmt.Sprintf("%s %s", c.cfg.Kind, c.Name()) }
