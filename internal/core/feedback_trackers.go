package core

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/internal/tracker"
	"github.com/phoedox/phoedox/resources"
)

// Forwarding feedback topics to external trackers (GitHub, Gitea/Forgejo,
// GitLab, Jira). A forwarded topic gets an issue there; reports that arrive
// later are added as "+n" comments, and a closed issue counts like a done
// ticket: when all tickets and issues of a topic are done, the topic is done
// and the reporters are told. Reporters stay private: issues carry titles
// and texts of reports (secrets removed), never who sent them.

// TrackerInput creates or changes a tracker (project owners).
type TrackerInput struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind" jsonschema:"github, gitea, gitlab or jira"`
	URL         string   `json:"url" jsonschema:"repository address (https://github.com/owner/repo, GitLab with groups) or the Jira site (https://example.atlassian.net)"`
	JiraProject string   `json:"jiraProject,omitempty"`
	IssueType   string   `json:"issueType,omitempty"`
	Username    string   `json:"username,omitempty" jsonschema:"Jira account e-mail for API tokens"`
	TokenVar    string   `json:"tokenVar" jsonschema:"secret of the project environment holding the access token"`
	Labels      []string `json:"labels,omitempty"`
}

// TrackerRef names a tracker moderators can forward to.
type TrackerRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// FeedbackTrackers lists a project's trackers (editors and owners).
func (s *Service) FeedbackTrackers(ctx context.Context, a Actor, ref string) ([]store.FeedbackTracker, error) {
	p, err := s.feedbackProject(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	return s.DB.FeedbackTrackers(ctx, p.ID)
}

// SaveFeedbackTracker creates (id "") or changes a tracker and checks that
// address and token work.
func (s *Service) SaveFeedbackTracker(ctx context.Context, a Actor, ref, id string, in TrackerInput) (store.FeedbackTracker, error) {
	p, err := s.feedbackProject(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return store.FeedbackTracker{}, err
	}
	t := store.FeedbackTracker{ID: newID(), ProjectID: p.ID, CreatedBy: a.User.ID, CreatedAt: store.Now()}
	if id != "" {
		if t, err = s.DB.FeedbackTracker(ctx, p.ID, id); err != nil {
			return t, err
		}
	}
	t.Kind = strings.TrimSpace(in.Kind)
	t.URL = strings.TrimRight(strings.TrimSpace(in.URL), "/")
	t.JiraProject = strings.ToUpper(strings.TrimSpace(in.JiraProject))
	t.IssueType = excerpt(strings.TrimSpace(in.IssueType), 60)
	t.Username = excerpt(strings.TrimSpace(in.Username), 200)
	t.TokenVar = strings.TrimSpace(in.TokenVar)
	t.Labels = cleanLabels(in.Labels)
	if !slices.Contains(tracker.Kinds, t.Kind) {
		return t, invalid("validation.tracker_kind", nil, "kind must be github, gitea, gitlab or jira")
	}
	if t.Kind != tracker.Jira {
		t.JiraProject, t.IssueType, t.Username = "", "", ""
	}
	if !strings.HasPrefix(t.URL, "https://") {
		return t, invalid("validation.tracker_url", nil, "use the https address of the repository or the Jira site")
	}
	if t.TokenVar == "" {
		return t, invalid("validation.tracker_token", nil, "choose the secret that holds the access token")
	}
	if _, secrets, err := s.envSecretNames(ctx, p.ID); err != nil || !secrets[t.TokenVar] {
		return t, invalid("validation.git_token_var", map[string]any{"name": t.TokenVar}, "%s is not a secret of the project environment", t.TokenVar)
	}
	c, err := s.trackerClient(ctx, t)
	if err != nil {
		return t, err
	}
	if t.Name = excerpt(strings.TrimSpace(in.Name), 80); t.Name == "" {
		t.Name = c.Name()
	}
	if err := c.Check(ctx); err != nil {
		return t, trackerFailed(err)
	}
	if err := s.DB.SaveFeedbackTracker(ctx, t); err != nil {
		return t, err
	}
	s.event(ctx, a, "feedback.tracker", t.ID, map[string]string{"kind": t.Kind, "name": t.Name})
	return t, nil
}

// DeleteFeedbackTracker removes a tracker; issues stay in the tracker.
func (s *Service) DeleteFeedbackTracker(ctx context.Context, a Actor, ref, id string) error {
	p, err := s.feedbackProject(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	if err := s.DB.DeleteFeedbackTracker(ctx, p.ID, id); err != nil {
		return err
	}
	s.event(ctx, a, "feedback.tracker_delete", id, nil)
	return nil
}

// trackerClient builds a client with the token from the project environment.
func (s *Service) trackerClient(ctx context.Context, t store.FeedbackTracker) (*tracker.Client, error) {
	values, _, err := s.envValues(ctx, t.ProjectID)
	if err != nil {
		return nil, err
	}
	token := values[t.TokenVar]
	if token == "" {
		return nil, invalid("validation.git_token_var", map[string]any{"name": t.TokenVar}, "the secret %s is not set", t.TokenVar)
	}
	c, err := tracker.New(tracker.Config{Kind: t.Kind, URL: t.URL, Project: t.JiraProject, IssueType: t.IssueType, User: t.Username, Token: token, Labels: t.Labels, APIURL: s.trackerAPI[t.URL]},
		s.RequestPolicy.Client())
	if err != nil {
		return nil, invalid("validation.tracker_url", nil, "use the https address of the repository or the Jira site")
	}
	return c, nil
}

// trackerFailed turns a tracker's answer into a message people understand.
func trackerFailed(err error) error {
	var he *tracker.HTTPError
	if errors.As(err, &he) {
		switch he.Status {
		case 401, 403:
			return invalid("validation.tracker_denied", nil, "the tracker refused the token")
		case 404:
			return invalid("validation.tracker_not_found", nil, "the tracker does not know this repository or project")
		}
		return invalid("validation.tracker_failed", map[string]any{"status": he.Status}, "the tracker answered with HTTP %d", he.Status)
	}
	return invalid("validation.tracker_unreachable", nil, "the tracker cannot be reached")
}

type trackerExcerpt struct {
	Type, Title, Body, Build string
}

func (s *Service) trackerExcerpts(reports []store.FeedbackReport) []trackerExcerpt {
	out := []trackerExcerpt{}
	for _, r := range reports {
		body := strings.Join(strings.Fields(scrubSecrets(r.Body)), " ")
		out = append(out, trackerExcerpt{Type: r.Type, Title: excerpt(scrubSecrets(r.Title), 200), Body: excerpt(body, 600), Build: excerpt(r.Context["build"], 60)})
	}
	return out
}

func (s *Service) topicLinks(p store.Project, portal store.FeedbackPortal, t store.FeedbackTopic) (string, string) {
	link := s.PublicURL + "/p/" + p.Slug + "/feedback?topic=" + t.ID
	public := ""
	if t.Public && portalSettings(portal).PublicView {
		public = s.PublicURL + "/feedback/" + portal.Handle + "?topic=" + t.ID
	}
	return link, public
}

func contentLang(p store.Project) string {
	if p.MainLocale != "" {
		return p.MainLocale
	}
	return "en"
}

// ForwardFeedbackTopic opens an issue for a topic in one of the project's
// trackers (moderators).
func (s *Service) ForwardFeedbackTopic(ctx context.Context, a Actor, ref, topicID, trackerRef string) (TopicDetail, error) {
	p, portal, t, err := s.moderatedTopic(ctx, a, ref, topicID)
	if err != nil {
		return TopicDetail{}, err
	}
	trackers, err := s.DB.FeedbackTrackers(ctx, p.ID)
	if err != nil {
		return TopicDetail{}, err
	}
	i := slices.IndexFunc(trackers, func(k store.FeedbackTracker) bool {
		return k.ID == trackerRef || strings.EqualFold(k.Name, trackerRef)
	})
	if i < 0 {
		return TopicDetail{}, ErrNotFound
	}
	tr := trackers[i]
	existing, _ := s.DB.TopicForwards(ctx, t.ID)
	if slices.ContainsFunc(existing, func(f store.FeedbackForward) bool { return f.TrackerID == tr.ID }) {
		return TopicDetail{}, invalid("validation.tracker_forwarded", map[string]any{"tracker": tr.Name}, "the topic already has an issue in %s", tr.Name)
	}
	c, err := s.trackerClient(ctx, tr)
	if err != nil {
		return TopicDetail{}, err
	}
	reports, err := s.DB.FeedbackReports(ctx, t.ID)
	if err != nil {
		return TopicDetail{}, err
	}
	link, public := s.topicLinks(p, portal, t)
	shown := reports
	if len(shown) > 5 {
		shown = shown[:5]
	}
	body, err := resources.TrackerText("issue", contentLang(p), map[string]any{
		"Summary": scrubSecrets(t.Summary), "Reports": t.ReportCount, "Portal": portal.Name, "Category": t.Category, "Tags": strings.Join(t.Tags, ", "),
		"Priority": t.Priority, "Excerpts": s.trackerExcerpts(shown), "Link": link, "PublicLink": public,
	})
	if err != nil {
		return TopicDetail{}, err
	}
	is, err := c.Create(ctx, excerpt(scrubSecrets(t.Title), 200), body)
	if err != nil {
		return TopicDetail{}, trackerFailed(err)
	}
	f := store.FeedbackForward{ID: newID(), TopicID: t.ID, TrackerID: tr.ID, Ref: is.Ref, URL: is.URL, ReportsSent: t.ReportCount, CreatedBy: a.User.ID, CreatedAt: store.Now()}
	if err := s.DB.AddFeedbackForward(ctx, f); err != nil {
		return TopicDetail{}, err
	}
	s.event(ctx, a, "feedback.forward", t.ID, map[string]string{"tracker": tr.Name, "issue": is.URL})
	// A topic that was done because its tickets were done is open again.
	s.syncTopicDone(ctx, t.ID)
	return s.FeedbackTopic(ctx, a, ref, topicID)
}

// RunFeedbackForwards adds "+n new reports" comments to forwarded issues and
// notices closed ones (every ten minutes, each issue at most every 30).
func (s *Service) RunFeedbackForwards(ctx context.Context, now time.Time) {
	tenants, err := s.DB.Tenants(store.System(ctx))
	if err != nil {
		return
	}
	for _, tn := range tenants {
		tctx := store.WithTenant(ctx, tn.ID)
		forwards, err := s.DB.OpenFeedbackForwards(tctx)
		if err != nil {
			continue
		}
		for _, f := range forwards {
			if f.CheckedAt != nil && now.Sub(f.CheckedAt.Time) < 30*time.Minute {
				continue
			}
			s.syncForward(tctx, f)
		}
	}
}

func (s *Service) syncForward(ctx context.Context, f store.FeedbackForward) {
	tr, err := s.DB.FeedbackTracker(ctx, "", f.TrackerID)
	if err != nil {
		return
	}
	t, err := s.DB.FeedbackTopic(ctx, "", f.TopicID)
	if err != nil {
		return
	}
	fail := func(err error) {
		slog.Warn("feedback forward", "issue", f.URL, "err", err)
		_ = s.DB.UpdateFeedbackForward(ctx, f.ID, f.ReportsSent, f.Closed, excerpt(err.Error(), 300))
	}
	c, err := s.trackerClient(ctx, tr)
	if err != nil {
		fail(err)
		return
	}
	sent := f.ReportsSent
	if t.ReportCount > sent {
		p, perr := s.DB.ProjectFor(ctx, t.ProjectID, "")
		portal, rerr := s.DB.FeedbackPortal(ctx, "", t.PortalID)
		reports, err := s.DB.FeedbackReports(ctx, t.ID)
		if perr != nil || rerr != nil || err != nil {
			return
		}
		fresh := reports
		if n := t.ReportCount - sent; n < len(fresh) {
			fresh = fresh[len(fresh)-n:]
		}
		if len(fresh) > 5 {
			fresh = fresh[len(fresh)-5:]
		}
		link, _ := s.topicLinks(p, portal, t)
		body, err := resources.TrackerText("update", contentLang(p), map[string]any{"New": t.ReportCount - sent, "Total": t.ReportCount, "Excerpts": s.trackerExcerpts(fresh), "Link": link})
		if err == nil {
			err = c.Comment(ctx, f.Ref, body)
		}
		if err != nil {
			fail(err)
			return
		}
		sent = t.ReportCount
	}
	closed, err := c.Closed(ctx, f.Ref)
	if err != nil {
		fail(err)
		return
	}
	_ = s.DB.UpdateFeedbackForward(ctx, f.ID, sent, closed, "")
	if closed != f.Closed {
		s.syncTopicDone(ctx, t.ID)
	}
}

// RefreshFeedbackForwards checks a topic's issues now (moderators).
func (s *Service) RefreshFeedbackForwards(ctx context.Context, a Actor, ref, topicID string) (TopicDetail, error) {
	_, _, t, err := s.moderatedTopic(ctx, a, ref, topicID)
	if err != nil {
		return TopicDetail{}, err
	}
	forwards, _ := s.DB.TopicForwards(ctx, t.ID)
	for _, f := range forwards {
		s.syncForward(ctx, f)
	}
	return s.FeedbackTopic(ctx, a, ref, topicID)
}

// forwardsDone counts a topic's issues and the closed ones.
func (s *Service) forwardsDone(ctx context.Context, topicID string) (total, done int) {
	forwards, _ := s.DB.TopicForwards(ctx, topicID)
	for _, f := range forwards {
		total++
		if f.Closed {
			done++
		}
	}
	return total, done
}
