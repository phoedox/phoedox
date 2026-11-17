package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/mail"
	"strings"
	"unicode/utf8"

	phxmail "github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/store"
)

// Public view of a feedback portal (feedback-concept): moderators release
// topics; visitors see them with state and answer, vote, follow them by
// e-mail or account and comment – comments wait for a moderator.

// PublicTopic is a released topic as visitors see it.
type PublicTopic struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	Type        string     `json:"type"`
	Category    string     `json:"category,omitempty"`
	State       string     `json:"state"`
	Answer      string     `json:"answer,omitempty"`
	FixedIn     string     `json:"fixedIn,omitempty"`
	Votes       int        `json:"votes"`
	ReportCount int        `json:"reportCount"`
	CreatedAt   store.Time `json:"createdAt"`
	UpdatedAt   store.Time `json:"updatedAt"`
	Voted       bool       `json:"voted"`
	Subscribed  bool       `json:"subscribed,omitempty"`
	// Comments are the approved comments (detail only).
	Comments []store.FeedbackComment `json:"comments,omitempty"`
}

func publicTopic(t store.FeedbackTopic) PublicTopic {
	return PublicTopic{ID: t.ID, Title: t.Title, Summary: t.Summary, Type: t.Type, Category: t.Category, State: t.State, Answer: t.Answer, FixedIn: t.FixedIn,
		Votes: t.Votes, ReportCount: t.ReportCount, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
}

// publicPortal loads a portal whose public view is on.
func (s *Service) publicPortal(ctx context.Context, handle string) (store.FeedbackPortal, PortalSettings, error) {
	portal, err := s.DB.FeedbackPortalByHandle(ctx, handle)
	if err != nil || !portal.Enabled {
		return portal, PortalSettings{}, ErrNotFound
	}
	ps := portalSettings(portal)
	if !ps.PublicView {
		return portal, ps, ErrNotFound
	}
	return portal, ps, nil
}

// voterOf is who votes: the account, or a hash of the visitor's key (kept
// in their browser) for portals that accept anonymous visitors.
func voterOf(a *Actor, portal store.FeedbackPortal, ps PortalSettings, key string) (string, error) {
	if a != nil && a.User.ID != "" {
		return "user:" + a.User.ID, nil
	}
	key = strings.TrimSpace(key)
	if !ps.Anonymous || len(key) < 16 || len(key) > 100 {
		return "", invalid("validation.feedback_vote_signin", nil, "sign in to vote")
	}
	sum := sha256.Sum256([]byte(portal.ID + "\x00" + key))
	return "anon:" + hex.EncodeToString(sum[:16]), nil
}

// PublicFeedbackTopics lists the released topics of a portal.
func (s *Service) PublicFeedbackTopics(ctx context.Context, a *Actor, handle, voterKey, sort, query, typ string) ([]PublicTopic, error) {
	portal, ps, err := s.publicPortal(ctx, handle)
	if err != nil {
		return nil, err
	}
	topics, err := s.DB.PublicTopics(ctx, portal.ID, sort, query, typ, 200)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(topics))
	for i, t := range topics {
		ids[i] = t.ID
	}
	voted := map[string]bool{}
	if voter, err := voterOf(a, portal, ps, voterKey); err == nil {
		voted, _ = s.DB.VotedTopics(ctx, voter, ids)
	}
	out := make([]PublicTopic, len(topics))
	for i, t := range topics {
		out[i] = publicTopic(t)
		out[i].Voted = voted[t.ID]
	}
	return out, nil
}

func (s *Service) publicTopic(ctx context.Context, handle, id string) (store.FeedbackPortal, PortalSettings, store.FeedbackTopic, error) {
	portal, ps, err := s.publicPortal(ctx, handle)
	if err != nil {
		return portal, ps, store.FeedbackTopic{}, err
	}
	t, err := s.DB.FeedbackTopic(ctx, portal.ProjectID, id)
	if err != nil || t.PortalID != portal.ID || !t.Public || t.Spam {
		return portal, ps, t, ErrNotFound
	}
	return portal, ps, t, nil
}

// PublicFeedbackTopic is one released topic with its approved comments.
func (s *Service) PublicFeedbackTopic(ctx context.Context, a *Actor, handle, id, voterKey string) (PublicTopic, error) {
	portal, ps, t, err := s.publicTopic(ctx, handle, id)
	if err != nil {
		return PublicTopic{}, err
	}
	out := publicTopic(t)
	if voter, err := voterOf(a, portal, ps, voterKey); err == nil {
		v, _ := s.DB.VotedTopics(ctx, voter, []string{t.ID})
		out.Voted = v[t.ID]
	}
	if a != nil {
		out.Subscribed = s.DB.IsSubscribed(ctx, t.ID, a.User.ID)
	}
	if ps.PublicComments {
		out.Comments, _ = s.DB.FeedbackComments(ctx, t.ID, "approved")
		for i := range out.Comments {
			out.Comments[i].Status = ""
		}
	}
	return out, nil
}

// VoteResult is a vote after toggling.
type VoteResult struct {
	Voted bool `json:"voted"`
	Votes int  `json:"votes"`
}

// VoteFeedbackTopic adds or removes the visitor's vote.
func (s *Service) VoteFeedbackTopic(ctx context.Context, a *Actor, handle, id, voterKey string) (VoteResult, error) {
	portal, ps, t, err := s.publicTopic(ctx, handle, id)
	if err != nil {
		return VoteResult{}, err
	}
	voter, err := voterOf(a, portal, ps, voterKey)
	if err != nil {
		return VoteResult{}, err
	}
	on, n, err := s.DB.ToggleVote(ctx, t.ID, voter)
	return VoteResult{Voted: on, Votes: n}, err
}

// FollowInput follows a topic: signed-in people by account, others by e-mail.
type FollowInput struct {
	Email string `json:"email,omitempty"`
	Lang  string `json:"lang,omitempty"`
}

func newFollowToken() (string, string) {
	tok := "phxf_" + strings.ToLower(newID()) + strings.ToLower(newID())
	sum := sha256.Sum256([]byte(tok))
	return tok, hex.EncodeToString(sum[:])
}

// FollowFeedbackTopic subscribes to news of a topic.
func (s *Service) FollowFeedbackTopic(ctx context.Context, a *Actor, handle, id string, in FollowInput) error {
	portal, ps, t, err := s.publicTopic(ctx, handle, id)
	if err != nil {
		return err
	}
	lang := in.Lang
	if lang != "de" {
		lang = "en"
	}
	tok, hash := newFollowToken()
	sub := store.FeedbackSubscription{ID: newID(), TopicID: t.ID, Lang: lang, TokenHash: hash, CreatedAt: store.Now()}
	if a != nil && a.User.ID != "" {
		sub.UserID = a.User.ID
	} else {
		addr, err := mail.ParseAddress(strings.TrimSpace(in.Email))
		if err != nil || !ps.Anonymous {
			return invalid("validation.feedback_follow_email", nil, "enter your e-mail address")
		}
		sub.Email = addr.Address
	}
	added, err := s.DB.AddFeedbackSubscription(ctx, sub)
	if err != nil || !added {
		return err
	}
	if sub.Email != "" {
		s.mailFollower(ctx, portal, t, sub, tok, true)
	}
	return nil
}

// UnfollowFeedbackTopic ends a subscription: by the link in the e-mails
// (token) or by account.
func (s *Service) UnfollowFeedbackTopic(ctx context.Context, a *Actor, token, handle, id string) error {
	if token != "" {
		sum := sha256.Sum256([]byte(token))
		ok, err := s.DB.DeleteFeedbackSubscription(ctx, hex.EncodeToString(sum[:]), "", "")
		if err == nil && !ok {
			return ErrNotFound
		}
		return err
	}
	if a == nil {
		return ErrForbidden
	}
	_, _, t, err := s.publicTopic(ctx, handle, id)
	if err != nil {
		return err
	}
	_, err = s.DB.DeleteFeedbackSubscription(ctx, "", t.ID, a.User.ID)
	return err
}

func (s *Service) topicLink(portal store.FeedbackPortal, t store.FeedbackTopic) string {
	return s.PublicURL + "/feedback/" + portal.Handle + "?topic=" + t.ID
}

func (s *Service) mailFollower(ctx context.Context, portal store.FeedbackPortal, t store.FeedbackTopic, sub store.FeedbackSubscription, token string, isNew bool) {
	if s.Mail == nil || !s.Mail.Enabled() || s.PublicURL == "" || sub.Email == "" {
		return
	}
	unsub := s.PublicURL + "/feedback/unfollow/" + token
	if token == "" {
		unsub = s.topicLink(portal, t)
	}
	subject, body, err := renderMail("feedback_follow", sub.Lang, map[string]any{"Portal": portal.Name, "Title": t.Title, "State": t.State, "Answer": t.Answer,
		"FixedIn": t.FixedIn, "New": isNew, "Link": s.topicLink(portal, t), "Unsubscribe": unsub})
	if err == nil {
		_ = s.Mail.Send(ctx, phxmail.Message{To: sub.Email, Subject: subject, Body: body, Lang: sub.Lang})
	}
}

// tellFollowers tells the people following a topic about news. E-mail
// followers get a fresh unsubscribe link each time.
func (s *Service) tellFollowers(ctx context.Context, p store.Project, portal store.FeedbackPortal, t store.FeedbackTopic) {
	subs, err := s.DB.FeedbackSubscriptions(ctx, t.ID)
	if err != nil {
		return
	}
	for _, sub := range subs {
		if sub.UserID != "" {
			if u, err := s.DB.UserByID(ctx, sub.UserID); err == nil {
				s.notify(ctx, u, Note{Kind: "feedback.update", ProjectID: p.ID, Project: portal.Name, Subject: t.ID, Title: t.Title, Body: t.Answer,
					Link: "/feedback/" + portal.Handle + "?topic=" + t.ID})
			}
			continue
		}
		// The stored hash cannot be turned back into the link: issue a new one.
		tok, hash := newFollowToken()
		if _, err := s.DB.DeleteFeedbackSubscription(ctx, sub.TokenHash, "", ""); err == nil {
			sub.ID, sub.TokenHash = newID(), hash
			if added, err := s.DB.AddFeedbackSubscription(ctx, sub); err == nil && added {
				s.mailFollower(ctx, portal, t, sub, tok, false)
			}
		}
	}
}

// TopicCommentInput is a visitor's comment.
type TopicCommentInput struct {
	Name string `json:"name,omitempty"`
	Body string `json:"body"`
}

// CommentFeedbackTopic adds a comment; it shows after a moderator approved it.
func (s *Service) CommentFeedbackTopic(ctx context.Context, a *Actor, handle, id string, in TopicCommentInput) (store.FeedbackComment, error) {
	portal, ps, t, err := s.publicTopic(ctx, handle, id)
	if err != nil {
		return store.FeedbackComment{}, err
	}
	if !ps.PublicComments {
		return store.FeedbackComment{}, ErrForbidden
	}
	body := strings.TrimSpace(scrubSecrets(in.Body))
	if body == "" || utf8.RuneCountInString(body) > 4000 {
		return store.FeedbackComment{}, invalid("validation.feedback_comment", nil, "write a comment (at most 4000 characters)")
	}
	c := store.FeedbackComment{ID: newID(), TopicID: t.ID, Body: body, Status: "pending", CreatedAt: store.Now()}
	switch {
	case a != nil && a.User.ID != "":
		c.UserID, c.AuthorName = a.User.ID, a.User.Name
	case ps.Anonymous:
		c.AuthorName = excerpt(strings.TrimSpace(in.Name), 60)
		if c.AuthorName == "" {
			c.AuthorName = "–"
		}
	default:
		return store.FeedbackComment{}, invalid("validation.feedback_vote_signin", nil, "sign in to comment")
	}
	if err := s.DB.AddFeedbackComment(ctx, c); err != nil {
		return c, err
	}
	s.notifyModerators(ctx, portal, Note{Kind: "feedback.comment", ProjectID: portal.ProjectID, Project: portal.Name, Subject: t.ID, Title: t.Title, Body: excerpt(body, 300),
		Link: "/p/" + s.projectSlug(ctx, portal.ProjectID) + "/feedback?topic=" + t.ID})
	return c, nil
}

func (s *Service) projectSlug(ctx context.Context, id string) string {
	if p, err := s.DB.ProjectFor(ctx, id, ""); err == nil {
		return p.Slug
	}
	return id
}

// DecideFeedbackComment approves or rejects a comment (moderators).
func (s *Service) DecideFeedbackComment(ctx context.Context, a Actor, ref, topicID, commentID string, approve bool) (TopicDetail, error) {
	_, _, t, err := s.moderatedTopic(ctx, a, ref, topicID)
	if err != nil {
		return TopicDetail{}, err
	}
	c, err := s.DB.FeedbackComment(ctx, commentID)
	if err != nil || c.TopicID != t.ID {
		return TopicDetail{}, ErrNotFound
	}
	status := "rejected"
	if approve {
		status = "approved"
	}
	if err := s.DB.DecideFeedbackComment(ctx, c.ID, status, a.String()); err != nil {
		return TopicDetail{}, err
	}
	s.event(ctx, a, "feedback.comment_"+status, t.ID, map[string]any{"comment": c.ID})
	return s.FeedbackTopic(ctx, a, ref, topicID)
}

// TeamCommentFeedbackTopic posts a public comment of the team (moderators;
// shown at once).
func (s *Service) TeamCommentFeedbackTopic(ctx context.Context, a Actor, ref, topicID string, in TopicCommentInput) (TopicDetail, error) {
	_, _, t, err := s.moderatedTopic(ctx, a, ref, topicID)
	if err != nil {
		return TopicDetail{}, err
	}
	body := strings.TrimSpace(in.Body)
	if body == "" || utf8.RuneCountInString(body) > 4000 {
		return TopicDetail{}, invalid("validation.feedback_comment", nil, "write a comment (at most 4000 characters)")
	}
	c := store.FeedbackComment{ID: newID(), TopicID: t.ID, UserID: a.User.ID, AuthorName: a.User.Name, Body: body, Status: "approved", Team: true, CreatedAt: store.Now()}
	if err := s.DB.AddFeedbackComment(ctx, c); err != nil {
		return TopicDetail{}, err
	}
	s.event(ctx, a, "feedback.comment_team", t.ID, map[string]any{"comment": c.ID})
	return s.FeedbackTopic(ctx, a, ref, topicID)
}
