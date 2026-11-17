package server

import (
	"encoding/json"
	"github.com/phoedox/phoedox/internal/aichat"
	"github.com/phoedox/phoedox/internal/backup"
	"github.com/phoedox/phoedox/internal/botcheck"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/google/jsonschema-go/jsonschema"

	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/plugins"
	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/internal/usercontent"
)

// op documents one route. In and Out are example values whose Go types are
// turned into JSON schemas; Raw marks a raw (binary) request body.
type op struct {
	Tag, Summary string
	In, Out      any
	Query        []string
	Raw          bool
	Public       bool // no session or token needed
}

type ok struct {
	OK bool `json:"ok"`
}
type emailRole struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}
type memberIn struct {
	Email string `json:"email"`
	Role  string `json:"role"`
	Until string `json:"until,omitempty"`
}
type nameIn struct {
	Name string `json:"name"`
}
type emailIn struct {
	Email string `json:"email"`
}
type roleIn struct {
	Role string `json:"role"`
}
type loginIn struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
type restrictIn struct {
	Restricted bool `json:"restricted"`
}
type quotaIn struct {
	QuotaMB int `json:"quotaMb"`
}
type tokenIn struct {
	Name     string   `json:"name"`
	Access   string   `json:"access,omitempty"`
	Projects []string `json:"projects,omitempty"`
}
type webhookList struct {
	Webhooks []store.Webhook `json:"webhooks"`
	Events   []string        `json:"events"`
}

type pluginList struct {
	Plugins  []core.PluginInfo `json:"plugins"`
	Store    bool              `json:"store"`
	Enabled  bool              `json:"enabled"`
	Unsigned bool              `json:"unsigned"`
}
type enabledIn struct {
	Enabled bool `json:"enabled"`
}
type licenseCodeIn struct {
	Code string `json:"code"`
}
type storeInstallIn struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// apiDocs describes every route of /api/v1; a test makes sure none is missing.
var apiDocs = map[string]op{
	"GET /admin/backups":                 {Tag: "admin", Summary: "Backups, schedule and target.", Out: backup.Status{}},
	"POST /admin/backups":                {Tag: "admin", Summary: "Make a backup now (201 done, 202 still running)."},
	"POST /admin/backups/upload":         {Tag: "admin", Summary: "Upload a backup file (e.g. from another instance).", Raw: true, Out: backup.Info{}},
	"GET /admin/backups/{name}":          {Tag: "admin", Summary: "Download a backup."},
	"DELETE /admin/backups/{name}":       {Tag: "admin", Summary: "Delete a backup.", Out: ok{}},
	"POST /admin/backups/{name}/restore": {Tag: "admin", Summary: "Replace all data with a backup (confirm = name); a backup of the current state is made first.", Out: backup.Manifest{}},
	"POST /auth/login":                   {Tag: "auth", Summary: "Sign in with email and password (sets the session cookie; may ask for a second factor).", In: loginIn{}, Public: true},
	"POST /auth/logout":                  {Tag: "auth", Summary: "Sign out.", Public: true},
	"POST /auth/login/totp":              {Tag: "auth", Summary: "Second step: TOTP code.", Public: true},
	"POST /auth/login/recovery":          {Tag: "auth", Summary: "Second step: recovery code.", Public: true},
	"POST /auth/login/passkey/begin":     {Tag: "auth", Summary: "Start signing in with a passkey.", Public: true},
	"POST /auth/login/passkey/finish":    {Tag: "auth", Summary: "Finish signing in with a passkey.", Public: true},
	"POST /auth/passkey/begin":           {Tag: "auth", Summary: "Second step with a passkey: start.", Public: true},
	"POST /auth/passkey/finish":          {Tag: "auth", Summary: "Second step with a passkey: finish.", Public: true},
	"POST /auth/reset-request":           {Tag: "auth", Summary: "Send a password reset link.", In: emailIn{}, Public: true},
	"POST /auth/signup":                  {Tag: "auth", Summary: "Self sign-up when the instance allows it: sends a confirmation link (202 whether or not the address is known).", In: auth.SignupInput{}, Public: true},
	"GET /identity/request":              {Tag: "auth", Summary: "Sign in with Phoedox: name of a registered service for its redirect address (ADR-0058).", Public: true},
	"POST /identity/approve":             {Tag: "auth", Summary: "Sign in with Phoedox: let a registered service learn who you are; returns the redirect with a one-time code."},
	"POST /identity/token":               {Tag: "auth", Summary: "Sign in with Phoedox: a service redeems the code with its PKCE verifier and gets id, name and e-mail (no access to content).", Public: true},
	"POST /app/token":                    {Tag: "auth", Summary: "Mobile app sign-in (ADR-0065): exchange the code from /identify?client_id=phoedox-app&redirect_uri=phoedox://signin and the PKCE verifier for a personal API token named after the device.", Public: true},
	"GET /auth/links/{token}":            {Tag: "auth", Summary: "Read an invitation or reset link.", Public: true},
	"POST /auth/links/{token}":           {Tag: "auth", Summary: "Set the password with an invitation or reset link.", Public: true},
	"GET /auth/oidc/start":               {Tag: "auth", Summary: "Start single sign-on (redirect).", Public: true},
	"GET /auth/oidc/callback":            {Tag: "auth", Summary: "Single sign-on callback (redirect).", Public: true},
	"GET /phoedox-skill.zip":             {Tag: "auth", Summary: "The Phoedox skill for AI clients (SKILL.md and references) as a ZIP.", Public: true},
	"GET /auth/ha/start":                 {Tag: "auth", Summary: "Start sign-in with Home Assistant (redirect).", Public: true},
	"GET /auth/ha/callback":              {Tag: "auth", Summary: "Sign-in with Home Assistant callback (redirect).", Public: true},
	"GET /instance":                      {Tag: "auth", Summary: "Version and sign-in options of this instance.", Public: true},
	"GET /openapi.json":                  {Tag: "auth", Summary: "This description (OpenAPI 3.1).", Public: true},

	"GET /me":                                        {Tag: "account", Summary: "The signed-in user.", Out: store.User{}},
	"PATCH /me":                                      {Tag: "account", Summary: "Change name or language."},
	"POST /me/password":                              {Tag: "account", Summary: "Change the password."},
	"GET /me/export":                                 {Tag: "account", Summary: "State of the personal data export (mail needed, latest export, next possible)."},
	"POST /me/export":                                {Tag: "account", Summary: "Export my data: a ZIP is built in the background and the link is mailed (once per 24 h)."},
	"GET /me/exports/{id}/download":                  {Tag: "account", Summary: "Download a finished export (own account, 7 days)."},
	"GET /me/deletion":                               {Tag: "account", Summary: "What deleting the account would do and how to confirm it."},
	"POST /me/delete":                                {Tag: "account", Summary: "Delete the own account (e-mail plus password and/or authenticator code)."},
	"GET /me/security":                               {Tag: "account", Summary: "Second factors and passkeys."},
	"POST /me/totp":                                  {Tag: "account", Summary: "Start setting up TOTP."},
	"POST /me/totp/confirm":                          {Tag: "account", Summary: "Confirm TOTP with a code."},
	"POST /me/totp/disable":                          {Tag: "account", Summary: "Turn TOTP off."},
	"POST /me/recovery-codes":                        {Tag: "account", Summary: "New recovery codes."},
	"POST /me/passkeys/begin":                        {Tag: "account", Summary: "Start adding a passkey."},
	"POST /me/passkeys/finish":                       {Tag: "account", Summary: "Finish adding a passkey."},
	"PATCH /me/passkeys/{id}":                        {Tag: "account", Summary: "Rename a passkey.", In: nameIn{}},
	"DELETE /me/passkeys/{id}":                       {Tag: "account", Summary: "Remove a passkey."},
	"GET /tokens":                                    {Tag: "account", Summary: "Personal API tokens.", Out: []store.Token{}},
	"POST /tokens":                                   {Tag: "account", Summary: "Create an API token (read/write, all or some projects); the token is shown once.", In: tokenIn{}},
	"DELETE /tokens/{id}":                            {Tag: "account", Summary: "Revoke an API token."},
	"GET /oauth/grants":                              {Tag: "account", Summary: "Apps connected with OAuth (MCP).", Out: []store.OAuthGrant{}},
	"DELETE /oauth/grants/{id}":                      {Tag: "account", Summary: "Disconnect an app."},
	"GET /oauth/request":                             {Tag: "account", Summary: "Consent page: the app asking for access."},
	"POST /oauth/authorize":                          {Tag: "account", Summary: "Consent page: allow or deny."},
	"GET /projects/{p}/entities/{e}/timeline":        {Tag: "graph", Summary: "Timeline of an entry: changes, check results, switching actions, agent jobs, reviews, mentioning comments and cards (limit, default 100).", Out: []core.TimelineItem{}},
	"GET /projects/{p}/discovered":                   {Tag: "graph", Summary: "Devices agents saw in the network (all=1 includes ignored ones), with the entry they match.", Out: []core.DiscoveredView{}},
	"POST /projects/{p}/discovered/{id}/adopt":       {Tag: "graph", Summary: "Add a discovered device as an entry (kind, name).", In: core.AdoptInput{}, Out: core.EntityResult{}},
	"POST /projects/{p}/discovered/{id}/dismiss":     {Tag: "graph", Summary: "Ignore a discovered device ({dismissed: true}) or show it again."},
	"GET /projects/{p}/documents/{d}/whiteboard.svg": {Tag: "documents", Summary: "A whiteboard drawn read-only as SVG (uses the app's CSS variables for colours)."},
	"GET /projects/{p}/tasks/{id}/time":              {Tag: "tasks", Summary: "Time logged on a card.", Out: []store.TimeEntry{}},
	"POST /projects/{p}/tasks/{id}/time":             {Tag: "tasks", Summary: "Log time on a card (duration like 1:30, 45m or 1.5h; day; note; billable).", In: core.TimeInput{}, Out: store.TimeEntry{}},
	"DELETE /projects/{p}/time/{id}":                 {Tag: "tasks", Summary: "Delete a time entry (its author or owners)."},
	"GET /projects/{p}/time":                         {Tag: "tasks", Summary: "Time of the project (from, to, user, board).", Out: []store.TimeEntry{}, Query: []string{"from", "to", "user", "board"}},
	"GET /projects/{p}/time.csv":                     {Tag: "tasks", Summary: "Time report as CSV (owners; from, to, user, board).", Query: []string{"from", "to", "user", "board"}},
	"DELETE /projects/{p}":                           {Tag: "projects", Summary: "Move a project to the trash (owners; confirm = the project's address, plus password or authenticator code)."},
	"GET /trashed-projects":                          {Tag: "projects", Summary: "Deleted projects the user can restore, with the date they are deleted for good.", Out: []core.TrashedProjectInfo{}},
	"POST /trashed-projects/{p}/restore":             {Tag: "projects", Summary: "Restore a deleted project (owners, system admins).", Out: store.Project{}},
	"GET /admin/settings":                            {Tag: "admin", Summary: "Instance settings.", Out: core.InstanceSettings{}},
	"PUT /admin/settings":                            {Tag: "admin", Summary: "Change instance settings (AI switch, storage quota).", In: core.SettingsPatch{}, Out: core.InstanceSettings{}},
	"GET /admin/users":                               {Tag: "admin", Summary: "All users.", Out: []store.User{}},
	"POST /admin/users":                              {Tag: "admin", Summary: "Invite a user."},
	"PATCH /admin/users/{id}":                        {Tag: "admin", Summary: "Change, disable or make admin."},
	"POST /admin/users/{id}/link":                    {Tag: "admin", Summary: "New invitation or reset link."},
	"POST /admin/users/{id}/reset-2fa":               {Tag: "admin", Summary: "Remove a user's second factors."},

	"GET /orgs":                                {Tag: "organizations", Summary: "Your organizations (all for system admins).", Out: []store.Org{}},
	"POST /orgs":                               {Tag: "organizations", Summary: "Create an organization (system admins).", In: core.OrgInput{}, Out: store.Org{}},
	"GET /orgs/{o}":                            {Tag: "organizations", Summary: "Organization with members and groups.", Out: core.OrgDetail{}},
	"PATCH /orgs/{o}":                          {Tag: "organizations", Summary: "Change name, AI switch, quota (org admins) or usage (system admins).", In: core.OrgInput{}, Out: store.Org{}},
	"PUT /orgs/{o}/members":                    {Tag: "organizations", Summary: "Add a member or change their role.", In: emailRole{}, Out: ok{}},
	"DELETE /orgs/{o}/members/{user}":          {Tag: "organizations", Summary: "Remove a member.", Out: ok{}},
	"POST /orgs/{o}/groups":                    {Tag: "organizations", Summary: "Create a group (optionally filled from an OIDC group).", In: core.GroupInput{}, Out: store.Group{}},
	"PATCH /orgs/{o}/groups/{g}":               {Tag: "organizations", Summary: "Change a group.", In: core.GroupInput{}, Out: store.Group{}},
	"DELETE /orgs/{o}/groups/{g}":              {Tag: "organizations", Summary: "Delete a group.", Out: ok{}},
	"POST /orgs/{o}/groups/{g}/members":        {Tag: "organizations", Summary: "Add a member to a group.", In: emailIn{}, Out: ok{}},
	"POST /orgs/{o}/groups/{g}/members/remove": {Tag: "organizations", Summary: "Remove a member from a group.", In: emailIn{}, Out: ok{}},

	"GET /projects":                                                   {Tag: "projects", Summary: "Projects you can see, with your role.", Out: []store.Project{}},
	"POST /projects":                                                  {Tag: "projects", Summary: "Create a project (personal or in an organization).", In: core.ProjectInput{}, Out: store.Project{}},
	"GET /projects/{p}":                                               {Tag: "projects", Summary: "A project.", Out: store.Project{}},
	"PATCH /projects/{p}":                                             {Tag: "projects", Summary: "Change settings (owners).", In: core.ProjectInput{}, Out: store.Project{}},
	"GET /projects/{p}/members":                                       {Tag: "projects", Summary: "Members (guests have an end date).", Out: []store.Member{}},
	"PUT /projects/{p}/members":                                       {Tag: "projects", Summary: "Add a member or guest (until = end date).", In: memberIn{}},
	"DELETE /projects/{p}/members/{user}":                             {Tag: "projects", Summary: "Remove a member.", Out: ok{}},
	"GET /projects/{p}/groups":                                        {Tag: "projects", Summary: "Groups with access.", Out: []store.ProjectGroup{}},
	"PUT /projects/{p}/groups/{g}":                                    {Tag: "projects", Summary: "Give a group a role.", In: roleIn{}, Out: ok{}},
	"DELETE /projects/{p}/groups/{g}":                                 {Tag: "projects", Summary: "Remove a group's access.", Out: ok{}},
	"GET /projects/{p}/storage":                                       {Tag: "projects", Summary: "Storage use and quota.", Out: core.Storage{}},
	"PUT /projects/{p}/storage":                                       {Tag: "projects", Summary: "Set the project's own quota (owners).", In: quotaIn{}, Out: core.Storage{}},
	"GET /projects/{p}/env":                                           {Tag: "projects", Summary: "Environment variables for request blocks (secrets hidden).", Out: []store.EnvVar{}},
	"PUT /projects/{p}/env/{name}":                                    {Tag: "projects", Summary: "Set a variable or secret."},
	"DELETE /projects/{p}/env/{name}":                                 {Tag: "projects", Summary: "Delete a variable."},
	"GET /projects/{p}/trash":                                         {Tag: "projects", Summary: "Deleted pages, entries and files.", Out: []store.TrashItem{}},
	"PUT /projects/{p}/category-names":                                {Tag: "projects", Summary: "Name a folder (category path) in one of the project's further languages; an empty name removes it.", Out: store.Project{}},
	"POST /projects/{p}/category-names/rename":                        {Tag: "projects", Summary: "Move the folder names of a renamed folder (from, to) to the new path."},
	"POST /projects/{p}/find":                                         {Tag: "documents", Summary: "Find text or a regular expression in one, several or all documents (pages, decision texts, link descriptions, optionally titles; main language or lang); with replace a preview, with apply the replacement (only/versions from the preview).", In: core.FindInput{}, Out: core.FindResult{}},
	"GET /projects/{p}/usage":                                         {Tag: "projects", Summary: "AI use (by person and model) and storage of the project this month, with links to higher usage pages (owners).", Out: core.UsageReport{}},
	"GET /orgs/{o}/usage":                                             {Tag: "organizations", Summary: "AI budgets and use (by project, person, model) and storage of an organization this month (its admins).", Out: core.UsageReport{}},
	"GET /admin/usage":                                                {Tag: "admin", Summary: "AI cost, charges and cap (by organization, model, top people) and storage of the instance this month.", Out: core.UsageReport{}},
	"GET /admin/orgs/{o}/prices":                                      {Tag: "admin", Summary: "An organization's own token prices: the one in effect and the history, newest first (system admins).", Out: core.OrgPrices{}},
	"POST /admin/orgs/{o}/prices":                                     {Tag: "admin", Summary: "Give an organization own token prices (percent on all in-app prices, own prices per model) from a date, now, the next month or back to the month start; past starts recalculate the calls since then.", In: core.OrgPriceInput{}, Out: core.OrgPrices{}},
	"POST /admin/orgs/{o}/prices/preview":                             {Tag: "admin", Summary: "What a price change would do to this month: charged amount and budgets before and after.", In: core.OrgPriceInput{}, Out: core.OrgPricePreview{}},
	"DELETE /admin/orgs/{o}/prices/{id}":                              {Tag: "admin", Summary: "Remove a planned price change that has not taken effect yet.", Out: core.OrgPrices{}},
	"GET /orgs/{o}/ai/members":                                        {Tag: "organizations", Summary: "AI limit and use of each member this month, with requests for more (organization admins; percentages when prices are hidden).", Out: core.MemberBudgets{}},
	"PUT /orgs/{o}/ai/members/{user}":                                 {Tag: "organizations", Summary: "Set a member's own AI limit (cents, -1 = the organization's default; or percent of the default when prices are hidden).", In: core.MemberLimitInput{}, Out: core.MemberBudgets{}},
	"POST /orgs/{o}/ai/requests":                                      {Tag: "organizations", Summary: "Ask the organization's admins for more AI usage (+25/50/100/200 % for this month or permanently), when the organization allows it.", In: core.BudgetRequestInput{}, Out: store.BudgetRequest{}},
	"POST /orgs/{o}/ai/requests/{id}":                                 {Tag: "organizations", Summary: "Approve (possibly with another percentage or scope) or reject a request for more AI usage (organization admins).", In: core.BudgetDecision{}, Out: core.MemberBudgets{}},
	"POST /projects/{p}/apps/preview":                                 {Tag: "apps", Query: []string{"doc"}, Summary: "Compile an app's unsaved files for the editor's live preview (editors; doc = the app being edited, whose grants apply); answers with a frame address valid for 30 minutes and the compile errors.", In: core.App{}, Out: core.AppFrame{}},
	"POST /projects/{p}/apps/{doc}/open":                              {Tag: "apps", Summary: "The frame address of an app (sealed, 12 hours), the bridge level it asks for, what the viewer allowed (-1 = not asked yet), its sources and compile errors.", Out: core.AppFrame{}},
	"PUT /projects/{p}/apps/{doc}/grant":                              {Tag: "apps", Summary: "Allow an app a bridge level (at most what it asks for): 0 none, 1 read its sources, 2 own storage, 3 propose changes."},
	"POST /projects/{p}/apps/{doc}/read":                              {Tag: "apps", Summary: "Read a source of the app's manifest (level 1) with the viewer's rights: a saved query, a board's cards, checks or entries of a kind."},
	"GET /projects/{p}/apps/{doc}/storage":                            {Tag: "apps", Summary: "Keys of the app's own storage (level 2); personal=1 for the viewer's own values instead of the shared ones.", Query: []string{"personal"}},
	"GET /projects/{p}/apps/{doc}/storage/{key}":                      {Tag: "apps", Summary: "A value (JSON) of the app's own storage; null if missing.", Query: []string{"personal"}},
	"PUT /projects/{p}/apps/{doc}/storage/{key}":                      {Tag: "apps", Summary: "Store a value (JSON, up to 1 MB) in the app's own storage; counts against the app's limit and the project storage.", Query: []string{"personal"}},
	"DELETE /projects/{p}/apps/{doc}/storage/{key}":                   {Tag: "apps", Summary: "Remove a value from the app's own storage.", Query: []string{"personal"}},
	"POST /projects/{p}/apps/{doc}/console":                           {Tag: "apps", Summary: "Report console errors and warnings of an app (kept briefly, so AI clients can fix them with get_app)."},
	"GET /admin/user-content":                                         {Tag: "admin", Summary: "The user content domain apps run on (one subdomain per app), its DNS provider without secrets, and the state of the wildcard certificate (system admins).", Out: core.UserContentView{}},
	"PUT /admin/user-content":                                         {Tag: "admin", Summary: "Set the user content domain: domain, DNS provider (hetzner, netcup, cloudflare) with its credentials (empty keeps the stored ones), ACME e-mail; Phoedox gets the wildcard certificate through DNS-01. An empty domain switches it off.", In: usercontent.Config{}, Out: core.UserContentView{}},
	"GET /projects/{p}/autolinks":                                     {Tag: "projects", Summary: "Auto-links of a project: own rules (pattern → address or ext ref) and the built-in references (decision numbers, ext refs) with their pages; pages link them when shown.", Out: core.AutoLinks{}},
	"PUT /projects/{p}/autolinks":                                     {Tag: "projects", Summary: "Replace the project's own auto-link rules (owners): {rules: [{pattern, to url|ref, url, note}]}."},
	"GET /projects/{p}/snapshots":                                     {Tag: "history", Summary: "Documentation snapshots of a project, newest first: named points in its history (by hand or from release tags of the code connection).", Out: []store.Snapshot{}},
	"POST /projects/{p}/snapshots":                                    {Tag: "history", Summary: "Name a point in the project history (editors): name such as 0.27.4, optional note and at (RFC 3339, default now).", In: core.SnapshotInput{}, Out: store.Snapshot{}},
	"GET /projects/{p}/snapshots/{name}":                              {Tag: "history", Summary: "The documents as they were at a snapshot, with the version of each (read one with the document's version endpoint).", Out: core.SnapshotView{}},
	"DELETE /projects/{p}/snapshots/{name}":                           {Tag: "history", Summary: "Remove a snapshot's name (owners); the history itself stays."},
	"GET /projects/{p}/snapshots/{name}/compare":                      {Tag: "history", Summary: "What changed between a snapshot and another one (to) or now (no to): changed, created and deleted documents with their versions.", Query: []string{"to"}, Out: core.SnapshotDiff{}},
	"GET /projects/{p}/glossary":                                      {Tag: "projects", Summary: "The project's glossary and style guide: terms with translations per language, terms kept untranslated, spellings to avoid.", Out: []core.GlossaryEntry{}},
	"PUT /projects/{p}/glossary":                                      {Tag: "projects", Summary: "Replace the glossary (editors): {entries: [{term, translations, keep, avoid, note}]}.", Out: []core.GlossaryEntry{}},
	"GET /projects/{p}/documents/{d}/glossary-check":                  {Tag: "documents", Summary: "Deviations of a page (lang: one of its translations) from the glossary: terms not used in a translation, spellings to avoid.", Query: []string{"lang"}, Out: []core.GlossaryProblem{}},
	"GET /projects/{p}/code/dbmodel":                                  {Tag: "code", Summary: "The SQL of a connected repository's migrations in a folder, in file order and without down parts (goose, dbmate; *.down.sql skipped), for dbmodel blocks (\"from owner/repo path\"); cached 10 minutes.", Query: []string{"repo", "path"}},
	"POST /projects/{p}/apps/{doc}/ai":                                {Tag: "apps", Summary: "An app asks the AI (apps with ai: true): {prompt, json}; runs on the viewer's chat budget and answers with text (and json when asked and valid).", Out: core.AppAIAnswer{}},
	"GET /projects/{p}/apps/{doc}/share":                              {Tag: "apps", Summary: "Whether an app is public, its address, schedule and AI limit; preview=1 adds what a snapshot of its sources would hold now (owners).", Query: []string{"preview"}, Out: core.AppShareView{}},
	"PUT /projects/{p}/apps/{doc}/share":                              {Tag: "apps", Summary: "Release an app to people without an account (owners): mode static (level 0) or snapshot (level 1 answered from a snapshot taken now with your rights), schedule manual/hourly/daily/weekly, aiLimit calls a month on your budget; confirm must be true.", In: core.AppShareInput{}, Out: core.AppShareView{}},
	"POST /projects/{p}/apps/{doc}/share/refresh":                     {Tag: "apps", Summary: "Take a new snapshot of a public app now (owners).", Out: core.AppShareView{}},
	"DELETE /projects/{p}/apps/{doc}/share":                           {Tag: "apps", Summary: "Withdraw the public release of an app (owners)."},
	"GET /shared-apps/{token}":                                        {Tag: "apps", Summary: "A public app for its page: frame address, level (0 or 1), sources, time of the snapshot, whether AI is offered.", Public: true, Out: core.SharedApp{}},
	"POST /shared-apps/{token}/read":                                  {Tag: "apps", Summary: "A public app reads a source from its snapshot: {source}.", Public: true},
	"POST /shared-apps/{token}/ai":                                    {Tag: "apps", Summary: "A public app asks the AI on the publisher's budget within the monthly limit: {prompt, json, botToken} (bot check of the apps place).", Public: true, In: sharedAppAIIn{}, Out: core.AppAIAnswer{}},
	"GET /agent/ws":                                                   {Tag: "agents", Summary: "WebSocket for agents (Bearer agent token, checked before the upgrade): the server pushes {type: jobs} and pings; the agent sends {type: result} and {type: report}. Long-poll on /agent/jobs stays as fallback."},
	"POST /hooks/code/{id}":                                           {Tag: "code", Summary: "Webhook for pull request events of GitHub or Gitea (X-Hub-Signature-256 / X-Gitea-Signature with the repository's secret): merged pull requests publish their documentation drafts, closed ones discard them."},
	"PUT /projects/{p}/documents/{d}/draft/code":                      {Tag: "drafts", Summary: "Tie the document's draft (opened if needed) to a pull request or branch: repo (id or owner/name, empty = the only one), pr, branch. Published on merge, discarded on close.", In: core.DraftCodeInput{}, Out: core.DraftInfo{}},
	"DELETE /projects/{p}/documents/{d}/draft/code":                   {Tag: "drafts", Summary: "Release the draft from its pull request (the draft stays open).", Out: core.DraftInfo{}},
	"GET /projects/{p}/code/docs-changes":                             {Tag: "code", Summary: "Documentation changes tied to a pull request or branch (?ref=owner/repo#12, or repo, pr, branch) with a line diff each.", Out: core.PRDocChanges{}},
	"GET /projects/{p}/code/repos/{id}/webhook":                       {Tag: "code", Summary: "Webhook address and secret for pull request events of a repository (owners; created on first use).", Out: core.CodeWebhook{}},
	"POST /projects/{p}/code/repos/{id}/webhook":                      {Tag: "code", Summary: "Create a new webhook secret for a repository (owners).", Out: core.CodeWebhook{}},
	"POST /themes/assets":                                             {Tag: "themes", Summary: "Upload a theme font (kind font, woff2, up to 2 MB) or logo (kind logo, PNG/JPEG/WebP, up to 512 KB) as multipart file; the answer's url goes into the theme (fonts need a licence note).", Out: core.ThemeAssetInfo{}},
	"GET /theme-assets/{file}":                                        {Tag: "themes", Summary: "A theme font or logo (public, cached for good)."},
	"GET /store/items":                                                {Tag: "store", Summary: "Themes (type=theme) or app templates (type=app, needs project) from the store with what is installed and update notices; access in the project: yes, request or no.", Out: core.StoreItems{}},
	"GET /store/items/{id}/versions":                                  {Tag: "store", Summary: "Versions of a store theme or app template that work with this Phoedox version, newest first (for rollbacks).", Out: []core.CatalogEntry{}},
	"POST /store/themes":                                              {Tag: "store", Summary: "Install a store theme as a theme of a person, project, organization or the instance; with theme: update or roll back a theme made from it.", In: core.StoreThemeInput{}, Out: core.ThemeView{}},
	"POST /projects/{p}/store/apps":                                   {Tag: "store", Summary: "Create an app from a store template (editors, if the organization's store access allows); with document: update or roll back an app made from it, its storage stays.", In: core.StoreAppInput{}, Out: core.DocResult{}},
	"GET /projects/{p}/store/requests":                                {Tag: "store", Summary: "The person's requests for store items in this project.", Out: []store.StoreRequest{}},
	"POST /projects/{p}/store/requests":                               {Tag: "store", Summary: "Ask the organization's admins to install a theme or app template into the project (when store access says request).", In: core.StoreRequestInput{}, Out: store.StoreRequest{}},
	"GET /orgs/{o}/providers":                                         {Tag: "organizations", Summary: "Service providers working in this organization (admins) and customers it works for (ADR-0073).", Out: core.ProviderLinks{}},
	"POST /orgs/{o}/providers":                                        {Tag: "organizations", Summary: "Grant a provider organization a role (reader, editor, admin) in all or chosen projects, optionally until a day (customer admins).", In: core.ProviderInput{}, Out: store.ProviderLink{}},
	"PATCH /orgs/{o}/providers/{id}":                                  {Tag: "organizations", Summary: "Let the provider's admins create projects in this organization, or stop it (customer admins; admin role for all projects only).", In: providerPatch{}, Out: store.ProviderLink{}},
	"DELETE /orgs/{o}/providers/{id}":                                 {Tag: "organizations", Summary: "Revoke a provider's access (customer admins, or the provider's admins giving it up)."},
	"GET /orgs/{o}/customers":                                         {Tag: "organizations", Summary: "Overview across the customers of a provider organization: open cards, failing checks, maintenance due and time logged this month per project.", Out: []core.CustomerOverview{}},
	"GET /orgs/{o}/customers/time.csv":                                {Tag: "organizations", Summary: "Time the provider's people logged at all customers in a month (?month=YYYY-MM) as CSV (provider admins)."},
	"GET /orgs/{o}/templates":                                         {Tag: "organizations", Summary: "Projects the organization offers as templates (ids); new projects start from them with template project:<id>.", Out: []string{}},
	"PUT /orgs/{o}/templates":                                         {Tag: "organizations", Summary: "Choose the template projects ({projects: [slug]}, admins): pages, boards (without cards), apps and the data model are copied into new projects, also at customers of a service provider."},
	"GET /projects/{p}/app-templates":                                 {Tag: "apps", Summary: "Apps of the project offered as templates (New → App).", Out: []core.AppTemplate{}},
	"PUT /projects/{p}/apps/{doc}/template":                           {Tag: "apps", Summary: "Mark an app as template of its project ({on: true}) or not (editors).", Out: []core.AppTemplate{}},
	"POST /projects/{p}/apps/{doc}/copy":                              {Tag: "apps", Summary: "Create a new app from an app's files and manifest ({title, category}); storage and grants stay with the original.", In: core.AppCopyInput{}, Out: core.DocResult{}},
	"GET /me/ai-credits":                                              {Tag: "ai", Summary: "Your AI credits: balance (cents), usable lots with expiry, history and offers to buy.", Out: core.CreditInfo{}},
	"POST /me/ai-credits/buy":                                         {Tag: "ai", Summary: "Buy an offer of AI credits for yourself: {offer} → checkout.", In: buyCreditsIn{}, Out: core.PurchaseCheckout{}},
	"GET /orgs/{o}/ai-credits":                                        {Tag: "ai", Summary: "An organization's AI credits (its admins).", Out: core.CreditInfo{}},
	"POST /orgs/{o}/ai-credits/buy":                                   {Tag: "ai", Summary: "Buy an offer of AI credits for the organization (its admins): {offer} → checkout.", In: buyCreditsIn{}, Out: core.PurchaseCheckout{}},
	"GET /admin/search/embeddings":                                    {Tag: "admin", Summary: "Semantic search: switched on, provider and embedding model, how many index entries have an embedding, the last error.", Out: core.EmbeddingStatus{}},
	"PUT /admin/search/embeddings":                                    {Tag: "admin", Summary: "Switch semantic search on or off and choose an OpenAI-compatible instance provider and embedding model (checked with a test call).", In: core.EmbeddingSettings{}, Out: core.EmbeddingStatus{}},
	"GET /admin/ai-credits":                                           {Tag: "admin", Summary: "Offers of AI credits and the latest grants (system admins).", Out: core.CreditAdmin{}},
	"PUT /admin/ai-credits/offers":                                    {Tag: "admin", Summary: "Replace the offers of AI credits: id, name, price, credit (cents), validMonths (0 = never), for (user, org, both).", In: []core.CreditOffer{}, Out: []core.CreditOffer{}},
	"POST /admin/ai-credits/grants":                                   {Tag: "admin", Summary: "Hand out AI credits to everyone or to people/organizations matching a filter, with an expiry date or unlimited; dryRun only counts.", In: core.CreditGrantInput{}, Out: core.CreditGrantResult{}},
	"GET /admin/dpa":                                                  {Tag: "admin", Summary: "Published versions of the data processing agreement and the placeholder shown until one exists (system admins).", Out: core.DPAAdmin{}},
	"POST /admin/dpa":                                                 {Tag: "admin", Summary: "Publish a new version of the data processing agreement (English, German translation); organizations accept it again.", In: core.DPAVersionInput{}, Out: store.DPAVersion{}},
	"GET /orgs/{o}/dpa":                                               {Tag: "organizations", Summary: "The current data processing agreement (?lang=de|en), whether the organization accepted it and the history of acceptances.", Out: core.DPAInfo{}},
	"POST /orgs/{o}/dpa/accept":                                       {Tag: "organizations", Summary: "Accept the current agreement on behalf of the organization (its admins, in person): versionId, signerName, signerRole, company, confirm.", In: core.DPAAcceptInput{}, Out: store.DPAAcceptance{}},
	"GET /orgs/{o}/dpa/acceptances/{id}/pdf":                          {Tag: "organizations", Summary: "An accepted agreement as PDF: the exact text and the record of the acceptance."},
	"GET /admin/payments":                                             {Tag: "payments", Summary: "Payment settings (system admins): provider (off, stripe or paddle), Stripe mode classic (Phoedox is the merchant, optional Stripe Tax) or managed (Stripe Managed Payments, Stripe is the merchant of record), whether keys are set, tax code (Stripe) or tax category (Paddle), currency, Paddle client token and pay page, webhook address and events.", Out: core.PaymentSettings{}},
	"PUT /admin/payments":                                             {Tag: "payments", Summary: "Change the payment settings; secretKey and webhookSecret are write-only and belong to the provider given (omit to keep, \"\" to remove); clientToken is Paddle's public client-side token.", In: core.PaymentSettingsInput{}, Out: core.PaymentSettings{}},
	"POST /admin/payments/check":                                      {Tag: "payments", Summary: "Check the key with the provider: account, country, live or test, whether Managed Payments is available there, warnings.", Out: core.PaymentCheck{}},
	"POST /admin/payments/test":                                       {Tag: "payments", Summary: "Open a checkout for a small test item to try the setup end to end.", Out: core.PurchaseCheckout{}},
	"GET /admin/payments/purchases":                                   {Tag: "payments", Summary: "The latest purchases of the instance (system admins).", Out: []store.Purchase{}},
	"POST /admin/payments/purchases/{id}/refund":                      {Tag: "payments", Summary: "Refund a paid one-time purchase in full (system admins)."},
	"GET /me/purchases":                                               {Tag: "payments", Summary: "Your purchases.", Out: []store.Purchase{}},
	"POST /me/payments/portal":                                        {Tag: "payments", Summary: "The provider's page to manage your payment methods, invoices and subscriptions: {returnPath} → {url}.", In: portalIn{}, Out: map[string]string{}},
	"POST /payments/webhook/{provider}":                               {Tag: "payments", Summary: "Events of the payment provider (stripe: Stripe-Signature, paddle: Paddle-Signature verified; repeated deliveries change nothing).", Public: true},
	"GET /payments/pay/{ref}":                                         {Tag: "payments", Summary: "Public values the /pay page needs to open Paddle's checkout for a transaction (txn_…): client token, sandbox, return addresses.", Out: core.PayInfo{}, Public: true},
	"GET /admin/apps":                                                 {Tag: "apps", Summary: "The instance's app switches: off, maxLevel (0–3), noAi, hosts apps may connect to, orgHosts (organizations may add hosts) (system admins).", Out: core.AppPolicy{}},
	"PUT /admin/apps":                                                 {Tag: "apps", Summary: "Change the instance's app switches (system admins).", In: core.AppPolicy{}, Out: core.AppPolicy{}},
	"GET /orgs/{o}/apps":                                              {Tag: "apps", Summary: "An organization's app switches (admins); the stricter of instance and organization applies.", Out: core.OrgAppPolicy{}},
	"PUT /orgs/{o}/apps":                                              {Tag: "apps", Summary: "Change an organization's app switches (admins).", In: core.AppPolicy{}, Out: core.OrgAppPolicy{}},
	"GET /orgs/{o}/store-access":                                      {Tag: "organizations", Summary: "Who may install themes and app templates from the store (all, groups or none) and whether people can send requests (admins).", Out: core.StoreAccess{}},
	"PUT /orgs/{o}/store-access":                                      {Tag: "organizations", Summary: "Change the organization's store access (admins).", In: core.StoreAccess{}, Out: core.StoreAccess{}},
	"GET /orgs/{o}/store/requests":                                    {Tag: "organizations", Summary: "Requests for store items (admins).", Out: []store.StoreRequest{}},
	"POST /orgs/{o}/store/requests/{id}":                              {Tag: "organizations", Summary: "Approve (installs the item into the project) or reject a request (admins).", In: core.StoreDecision{}, Out: store.StoreRequest{}},
	"POST /themes/generate":                                           {Tag: "themes", Summary: "Let the AI propose a theme from a description and/or the colours of a website (on the person's own AI budget); the proposal is checked and made readable, not saved.", In: core.ThemeIdea{}, Out: core.ThemeProposal{}},
	"GET /projects/{p}/background-ai":                                 {Tag: "projects", Summary: "The project's background AI this month (ADR-0077): the organization's general and project limits, the owner's limit, the lowest one, what was used, and whether jobs may use the AI now.", Out: core.BackgroundAIStatus{}},
	"PUT /projects/{p}/background-ai":                                 {Tag: "projects", Summary: "Set the owner's limit for the project's background AI in cents per month (owners; null clears it, 0 switches it off).", Out: core.BackgroundAIStatus{}},
	"GET /orgs/{o}/background-ai":                                     {Tag: "organizations", Summary: "The organization's background AI limits: general limit for every project and limits per project (organization admins)."},
	"PUT /orgs/{o}/background-ai":                                     {Tag: "organizations", Summary: "Set the general background AI limit and limits per project (project slug → cents; null removes one).", In: core.OrgBackgroundAIInput{}},
	"GET /themes":                                                     {Tag: "themes", Summary: "Themes the person can choose (own, organizations', instance's and, with project, the project's), the person's choice and the effective theme with where it comes from.", Query: []string{"project"}, Out: core.ThemeCatalog{}},
	"GET /themes/{id}":                                                {Tag: "themes", Summary: "A theme: tokens per mode, fonts, shape, own CSS and whether that CSS is released for the running version.", Out: core.ThemeView{}},
	"POST /themes/check":                                              {Tag: "themes", Summary: "Check a theme while editing: contrast problems per mode (WCAG AA) and the first other problem, without saving.", In: core.ThemeInput{}, Out: core.ThemeCheck{}},
	"POST /themes":                                                    {Tag: "themes", Summary: "Create a theme for the person (scope user), a project (owners), an organization (admins) or the instance (system admins).", In: core.ThemeInput{}, Out: core.ThemeView{}},
	"PUT /themes/{id}":                                                {Tag: "themes", Summary: "Change a theme; saving releases its CSS for the running version.", In: core.ThemeInput{}, Out: core.ThemeView{}},
	"DELETE /themes/{id}":                                             {Tag: "themes", Summary: "Delete a theme; whoever used it falls back to the next theme."},
	"POST /themes/{id}/release":                                       {Tag: "themes", Summary: "Release the theme's own CSS for the running Phoedox version after checking it.", Out: core.ThemeView{}},
	"PUT /me/theme":                                                   {Tag: "themes", Summary: "The person's theme: theme id, always use it (also in projects with their own theme), contrast (\"\", high = built-in high-contrast theme, variant = the theme's high-contrast variant).", In: core.MyThemeInput{}},
	"PUT /projects/{p}/theme":                                         {Tag: "themes", Summary: "Choose the project's theme (owners; empty = none)."},
	"GET /orgs/{o}/theme":                                             {Tag: "themes", Summary: "The organization's default theme and whether it is forced.", Out: core.OrgThemeInput{}},
	"PUT /orgs/{o}/theme":                                             {Tag: "themes", Summary: "Set the organization's default theme; forced = members cannot choose another one (high contrast stays possible).", In: core.OrgThemeInput{}, Out: core.OrgThemeInput{}},
	"PUT /admin/theme":                                                {Tag: "admin", Summary: "Set the instance's default theme (an instance theme; empty = Phoedox)."},
	"GET /admin/bot-protection":                                       {Tag: "admin", Summary: "Bot protection: providers (without secrets), policies, which policy each place uses, and outcomes of the last 7 days with blocked attempts (system admins).", Out: core.BotSettings{}},
	"PUT /admin/bot-protection":                                       {Tag: "admin", Summary: "Save the bot protection configuration; empty provider secrets keep the stored ones. Third-party providers need the DPA confirmation.", In: botcheck.Config{}, Out: core.BotSettings{}},
	"POST /bot/challenge":                                             {Tag: "auth", Summary: "Start the bot check of a place (signup, signin, reset, public_forms, feedback, apps): proof-of-work prefix and difficulty, or the provider and site key; off when the place is not protected.", Public: true, Out: botcheck.Challenge{}},
	"POST /bot/verify":                                                {Tag: "auth", Summary: "Answer the invisible checks (proof-of-work nonce and behaviour signals): a pass token, a visible puzzle, or blocked.", Public: true, In: botcheck.VerifyInput{}, Out: botcheck.Result{}},
	"POST /bot/solve":                                                 {Tag: "auth", Summary: "Answer a visible puzzle (slider position or rotation); three wrong answers block.", Public: true, In: botcheck.SolveInput{}, Out: botcheck.Result{}},
	"POST /bot/email":                                                 {Tag: "auth", Summary: "Accessible fallback: send a one-time code to an e-mail address.", Public: true, In: botcheck.EmailInput{}, Out: botcheck.Result{}},
	"POST /bot/email/verify":                                          {Tag: "auth", Summary: "Check the e-mail code; a pass token on success.", Public: true, In: botcheck.EmailInput{}, Out: botcheck.Result{}},
	"POST /bot/provider":                                              {Tag: "auth", Summary: "Check the answer of a third-party widget (Turnstile, hCaptcha, reCAPTCHA) with the operator's secret key; a pass token on success.", Public: true, In: botcheck.ProviderInput{}, Out: botcheck.Result{}},
	"GET /feedback/{handle}":                                          {Tag: "feedback", Summary: "A feedback portal for its public page: intro, report types, typed fields, attachment rules, who may report, theme (lang picks the language).", Public: true, Query: []string{"lang"}, Out: core.PublicPortal{}},
	"POST /feedback/{handle}/reports":                                 {Tag: "feedback", Summary: "Send a report (JSON, or multipart with the JSON in \"report\" and files in \"file\"). Signed in, anonymous with a bot-check token (botToken), or with the header X-Phoedox-Feedback-Key (public key: with bot check; server key: trusted backends). Answers with the status token once.", Public: true, In: core.ReportInput{}, Out: core.ReportReceipt{}},
	"GET /feedback/{handle}/login":                                    {Tag: "feedback", Summary: "Sign in as a reporter with the portal's own login (OpenID Connect of the product): redirects to the identity provider.", Public: true},
	"GET /feedback/login/callback":                                    {Tag: "feedback", Summary: "Return address for the identity provider (register it there); sets the reporter's sign-in for the portal and goes back to it.", Public: true},
	"DELETE /feedback/{handle}/login":                                 {Tag: "feedback", Summary: "Sign out of the portal's own login.", Public: true},
	"POST /feedback/status/{token}/reply":                             {Tag: "feedback", Summary: "The reporter answers the moderators' question: {body} (status link; moderators are notified).", Public: true, In: feedbackReplyIn{}, Out: core.ReportStatus{}},
	"GET /feedback/{handle}/topics":                                   {Tag: "feedback", Summary: "Released topics of a portal with public view (?sort=votes|new, q, type); header X-Phoedox-Voter (anonymous visitors' key) marks own votes.", Out: []core.PublicTopic{}, Public: true},
	"GET /feedback/{handle}/topics/{id}":                              {Tag: "feedback", Summary: "A released topic with its approved comments.", Out: core.PublicTopic{}, Public: true},
	"POST /feedback/{handle}/topics/{id}/vote":                        {Tag: "feedback", Summary: "Vote for a topic or take the vote back (account, or the X-Phoedox-Voter key on portals for anonymous visitors).", Out: core.VoteResult{}, Public: true},
	"POST /feedback/{handle}/topics/{id}/follow":                      {Tag: "feedback", Summary: "Follow a topic: by account, or {email} for anonymous visitors (bot check); news comes as notification or e-mail.", In: core.FollowInput{}, Public: true},
	"DELETE /feedback/{handle}/topics/{id}/follow":                    {Tag: "feedback", Summary: "Stop following a topic (account).", Public: true},
	"DELETE /feedback/unfollow/{token}":                               {Tag: "feedback", Summary: "Stop following with the link from the e-mails.", Public: true},
	"POST /feedback/{handle}/topics/{id}/comments":                    {Tag: "feedback", Summary: "Comment on a released topic (shown after a moderator approved it).", In: core.TopicCommentInput{}, Out: store.FeedbackComment{}, Public: true},
	"POST /projects/{p}/feedback/topics/{id}/comments":                {Tag: "feedback", Summary: "Comment publicly as the team (moderators; shown at once).", In: core.TopicCommentInput{}, Out: core.TopicDetail{}},
	"POST /projects/{p}/feedback/topics/{id}/comments/{cid}/{action}": {Tag: "feedback", Summary: "Approve or reject a comment (action approve|reject; moderators).", Out: core.TopicDetail{}},
	"GET /feedback/status/{token}":                                    {Tag: "feedback", Summary: "The state of a report by its status link (state, answer, number of reports on the topic).", Public: true, Out: core.ReportStatus{}},
	"DELETE /feedback/status/{token}":                                 {Tag: "feedback", Summary: "Delete my report: attachments, e-mail and context are removed; the text stays anonymized.", Public: true},
	"GET /feedback/status/{token}/export":                             {Tag: "feedback", Summary: "Everything stored about a report, as JSON.", Public: true, Out: core.FeedbackExport{}},
	"GET /me/feedback":                                                {Tag: "feedback", Summary: "My reports to feedback portals with their state.", Out: []core.ReportStatus{}},
	"GET /projects/{p}/feedback/portals":                              {Tag: "feedback", Summary: "The project's feedback portals (moderators see the ones they moderate).", Out: []core.PortalView{}},
	"POST /projects/{p}/feedback/portals":                             {Tag: "feedback", Summary: "Create a portal: handle (address /feedback/<handle>), name and settings (owners). A new server key is shown once.", In: core.PortalInput{}, Out: core.PortalView{}},
	"PUT /projects/{p}/feedback/portals/{id}":                         {Tag: "feedback", Summary: "Change a portal (owners).", In: core.PortalInput{}, Out: core.PortalView{}},
	"DELETE /projects/{p}/feedback/portals/{id}":                      {Tag: "feedback", Summary: "Delete a portal with its topics and reports (owners)."},
	"POST /projects/{p}/feedback/portals/{id}/keys":                   {Tag: "feedback", Summary: "Replace the public or the server key (kind); a new server key is shown once.", Out: core.PortalView{}},
	"PUT /projects/{p}/feedback/portals/{id}/moderators":              {Tag: "feedback", Summary: "Set the portal's extra moderators by e-mail; they see only this portal's topics.", Out: core.PortalView{}},
	"GET /projects/{p}/feedback/topics":                               {Tag: "feedback", Summary: "Topics for moderators (portal, state, category, priority, q), highest priority first.", Query: []string{"portal", "state", "category", "priority", "q"}, Out: core.TopicList{}},
	"GET /projects/{p}/feedback/topics/{id}":                          {Tag: "feedback", Summary: "A topic with its reports, attachments and tickets.", Out: core.TopicDetail{}},
	"PATCH /projects/{p}/feedback/topics/{id}":                        {Tag: "feedback", Summary: "Change title, summary, category, tags, priority, state, the answer to reporters or the version that fixed it (reporters are told), and the linked entries.", In: core.TopicPatch{}, Out: core.TopicDetail{}},
	"POST /projects/{p}/feedback/topics/{id}/answer-draft":            {Tag: "feedback", Summary: "The AI drafts an answer to the reporters (moderators, background AI budget); nothing is sent: {lang}.", In: answerDraftIn{}, Out: core.FeedbackAnswerDraft{}},
	"GET /projects/{p}/feedback/entities/{e}":                         {Tag: "feedback", Summary: "Feedback topics linked to an entry (for moderators; others get an empty list).", Out: []core.EntityFeedback{}},
	"POST /projects/{p}/tasks/{id}/feedback-seen":                     {Tag: "tasks", Summary: "Reset the \"+n new reports\" of a card that is a ticket of a feedback topic."},
	"POST /projects/{p}/feedback/topics/{id}/merge":                   {Tag: "feedback", Summary: "Merge another topic (from) into this one.", Out: core.TopicDetail{}},
	"POST /projects/{p}/feedback/topics/{id}/tickets":                 {Tag: "feedback", Summary: "Put the topic on a board as a card (ticket) linked to it; a topic can have several. When all tickets are done the topic is done.", In: core.TicketInput{}, Out: core.TopicDetail{}},
	"POST /projects/{p}/feedback/topics/{id}/forward": {Tag: "feedback", Summary: "Open an issue for the topic in one of the project's external trackers (GitHub, Gitea, GitLab, Jira); new reports become +n comments there, a closed issue counts as a done ticket.", In: struct {
		Tracker string `json:"tracker"`
	}{}, Out: core.TopicDetail{}},
	"POST /projects/{p}/feedback/topics/{id}/forwards/refresh": {Tag: "feedback", Summary: "Check the topic's issues in external trackers now (new reports, closed issues).", Out: core.TopicDetail{}},
	"GET /projects/{p}/feedback/trackers":                      {Tag: "feedback", Summary: "External trackers feedback topics can be forwarded to.", Out: []store.FeedbackTracker{}},
	"POST /projects/{p}/feedback/trackers":                     {Tag: "feedback", Summary: "Connect an external tracker (owners); the token is a secret of the project environment. Address and token are checked.", In: core.TrackerInput{}, Out: store.FeedbackTracker{}},
	"PUT /projects/{p}/feedback/trackers/{id}":                 {Tag: "feedback", Summary: "Change an external tracker (owners).", In: core.TrackerInput{}, Out: store.FeedbackTracker{}},
	"DELETE /projects/{p}/feedback/trackers/{id}":              {Tag: "feedback", Summary: "Remove an external tracker; its issues stay in the tracker."},
	"POST /projects/{p}/feedback/reports/{id}/move":            {Tag: "feedback", Summary: "Move a report into another topic, or into a new one (topic empty).", Out: core.TopicDetail{}},
	"GET /projects/{p}/feedback/attachments/{id}":              {Tag: "feedback", Summary: "An attachment of a report (moderators)."},
	"PUT /projects/{p}/feedback/quota":                         {Tag: "feedback", Summary: "Set the project's feedback storage in MB (null = instance default; system admins)."},
	"GET /me/usage":                                            {Tag: "account", Summary: "The signed-in person's own AI use this month by project and model, with their budgets.", Out: core.UsageReport{}},
	"GET /projects/{p}/dbschema":                               {Tag: "documents", Summary: "All dbmodel blocks of the project's pages: together the project's database schema (a block can show tables of other pages).", Out: []core.DBSchemaBlock{}},
	"POST /projects/{p}/validate-blocks":                       {Tag: "documents", Summary: "Check mermaid and dbmodel blocks of a page (document) or of Markdown (markdown).", Out: []core.BlockProblem{}},
	"GET /projects/{p}/history/points":                         {Tag: "projects", Summary: "The newest saved changes of the project's documents (points to look at or roll back to); doc (one document), before, limit.", Out: []core.ProjectPoint{}},
	"GET /projects/{p}/history/at":                             {Tag: "projects", Summary: "What differs between a point in time (at, RFC 3339) and now: changed, deleted and new documents.", Out: core.ProjectAt{}},
	"POST /projects/{p}/history/rollback":                      {Tag: "projects", Summary: "Owners: bring every document back to its state at a point in time (new versions; deleted pages return, new ones go to the trash).", Out: core.RollbackResult{}},
	"POST /projects/{p}/trash/{id}/restore":                    {Tag: "projects", Summary: "Restore from the trash.", Out: store.TrashItem{}},
	"DELETE /projects/{p}/trash/{id}":                          {Tag: "projects", Summary: "Delete for good (owners)."},
	"GET /projects/{p}/favorites":                              {Tag: "projects", Summary: "Project and personal favorites.", Out: []store.Favorite{}},
	"PUT /projects/{p}/favorites":                              {Tag: "projects", Summary: "Reorder favorites.", Query: []string{"scope"}},
	"PUT /projects/{p}/favorites/{d}":                          {Tag: "projects", Summary: "Add a favorite.", Query: []string{"scope"}},
	"DELETE /projects/{p}/favorites/{d}":                       {Tag: "projects", Summary: "Remove a favorite.", Query: []string{"scope"}},
	"GET /projects/{p}/suggestions":                            {Tag: "projects", Summary: "AI suggestions waiting for a decision.", Out: []store.Suggestion{}, Query: []string{"status"}},
	"POST /suggestions/{id}/accept":                            {Tag: "projects", Summary: "Accept (apply) a suggestion."},
	"POST /suggestions/{id}/reject":                            {Tag: "projects", Summary: "Reject a suggestion."},
	"GET /projects/{p}/share-links":                            {Tag: "sharing", Summary: "Active read-only links.", Out: []store.ShareLink{}},
	"POST /projects/{p}/share-links":                           {Tag: "sharing", Summary: "Create a read-only link (project or one page); the token is shown once.", In: core.ShareLinkInput{}, Out: core.NewShareLink{}},
	"DELETE /projects/{p}/share-links/{id}":                    {Tag: "sharing", Summary: "Revoke a link.", Out: ok{}},
	"GET /shared/{token}":                                      {Tag: "sharing", Summary: "Open a share link (no account).", Out: core.SharedView{}, Public: true},
	"GET /shared/{token}/documents/{d}":                        {Tag: "sharing", Summary: "A page of a share link.", Out: store.Document{}, Public: true},
	"GET /shared/{token}/files/{id}":                           {Tag: "sharing", Summary: "A file of a shared page.", Public: true, Query: []string{"preview"}},
	"GET /shared/{token}/files/{id}/{name}":                    {Tag: "sharing", Summary: "A file of a shared page (with file name).", Public: true, Query: []string{"preview"}},
	"GET /admin/plugins":                                       {Tag: "plugins", Summary: "Installed plugins with permissions, license and problems.", Out: pluginList{}},
	"POST /admin/plugins":                                      {Tag: "plugins", Summary: "Install or update a plugin package (.phxp body; signature in the Phoedox-Signature header).", Raw: true, Out: core.PluginInfo{}},
	"GET /admin/plugins/catalog":                               {Tag: "plugins", Summary: "Plugins the store offers for this server.", Out: []core.CatalogEntry{}},
	"POST /admin/plugins/install":                              {Tag: "plugins", Summary: "Install a plugin version from the store.", In: storeInstallIn{}, Out: core.PluginInfo{}},
	"PATCH /admin/plugins/{id}":                                {Tag: "plugins", Summary: "Switch a plugin on or off for the instance.", In: enabledIn{}, Out: core.PluginInfo{}},
	"DELETE /admin/plugins/{id}":                               {Tag: "plugins", Summary: "Uninstall a plugin with its data.", Out: ok{}},
	"GET /admin/plugins/{id}/logs":                             {Tag: "plugins", Summary: "Recent log lines of a plugin.", Out: []plugins.LogLine{}},
	"PUT /admin/plugins/{id}/license":                          {Tag: "plugins", Summary: "Store a signed license file.", Raw: true, Out: core.PluginInfo{}},
	"POST /admin/plugins/{id}/license/redeem":                  {Tag: "plugins", Summary: "Redeem a license code at the store.", In: licenseCodeIn{}, Out: core.PluginInfo{}},
	"GET /plugins/{id}/{v}/files/*":                            {Tag: "plugins", Summary: "A frontend file of an enabled plugin (cacheable, the version is in the path)."},
	"GET /projects/{p}/plugins":                                {Tag: "plugins", Summary: "Plugins of a project (owners see all installed ones with settings).", Out: []core.ProjectPluginInfo{}},
	"PUT /projects/{p}/plugins/{id}":                           {Tag: "plugins", Summary: "Enable or disable a plugin for the project (owners).", In: enabledIn{}, Out: ok{}},
	"PUT /projects/{p}/plugins/{id}/secrets":                   {Tag: "plugins", Summary: "Set secrets the plugin asks for; empty values remove them. Values are never returned.", In: map[string]string{}, Out: ok{}},
	"POST /projects/{p}/plugins/{id}/tools/{tool}":             {Tag: "plugins", Summary: "Run a tool of a plugin with the caller's rights (JSON arguments as body).", Raw: true},
	"GET /projects/{p}/webhooks":                               {Tag: "webhooks", Summary: "Webhooks and the event types they can receive.", Out: webhookList{}},
	"POST /projects/{p}/webhooks":                              {Tag: "webhooks", Summary: "Add a webhook; the signing secret is shown once.", In: core.WebhookInput{}, Out: core.NewWebhook{}},
	"PATCH /projects/{p}/webhooks/{id}":                        {Tag: "webhooks", Summary: "Change or switch a webhook on/off.", In: core.WebhookInput{}, Out: store.Webhook{}},
	"DELETE /projects/{p}/webhooks/{id}":                       {Tag: "webhooks", Summary: "Delete a webhook.", Out: ok{}},
	"GET /projects/{p}/webhooks/{id}/deliveries":               {Tag: "webhooks", Summary: "The latest deliveries.", Out: []store.Delivery{}},
	"POST /projects/{p}/webhooks/{id}/test":                    {Tag: "webhooks", Summary: "Send a ping now.", Out: store.Delivery{}},

	"GET /projects/{p}/documents":                               {Tag: "documents", Summary: "Pages, decisions and links (without content).", Out: []store.Document{}, Query: []string{"type", "category"}},
	"POST /projects/{p}/documents":                              {Tag: "documents", Summary: "Create a page, decision (ADR) or link.", In: core.DocumentInput{}, Out: core.DocResult{}},
	"GET /projects/{p}/documents/{d}":                           {Tag: "documents", Summary: "A document with content (slug, id or ADR-0001); lang picks a translation in multilingual projects.", Out: store.Document{}, Query: []string{"lang"}},
	"GET /projects/{p}/documents/{d}/translations/{lang}":       {Tag: "documents", Summary: "One translation with its text and whether it is outdated.", Out: core.TranslationInfo{}},
	"PUT /projects/{p}/documents/{d}/translations/{lang}":       {Tag: "documents", Summary: "Create or replace a translation; it refers to the current main version.", In: core.TranslationInput{}, Out: core.TranslationInfo{}},
	"DELETE /projects/{p}/documents/{d}/translations/{lang}":    {Tag: "documents", Summary: "Delete a translation."},
	"PATCH /projects/{p}/documents/{d}":                         {Tag: "documents", Summary: "Change a document (new version).", In: core.DocumentPatch{}, Out: core.DocResult{}},
	"GET /projects/{p}/tasks/{id}/code":                         {Tag: "tasks", Summary: "Commits, releases and pull requests linked to a card (ADR-0063).", Out: []store.TaskCodeLink{}},
	"POST /projects/{p}/tasks/{id}/code":                        {Tag: "tasks", Summary: "Link a commit (SHA), release (tag) or pull request (number) of a connected repository to a card (editors).", In: core.CodeLinkInput{}, Out: []store.TaskCodeLink{}},
	"DELETE /projects/{p}/tasks/{id}/code/{link}":               {Tag: "tasks", Summary: "Remove a code link from a card."},
	"GET /projects/{p}/code/repos":                              {Tag: "projects", Summary: "Connected GitHub or Gitea/Forgejo repositories of the project.", Out: []store.CodeRepo{}},
	"POST /projects/{p}/code/repos":                             {Tag: "projects", Summary: "Connect a repository by its https address; tokenVar names a secret of the project environment (owners).", In: core.CodeRepoInput{}, Out: store.CodeRepo{}},
	"DELETE /projects/{p}/code/repos/{id}":                      {Tag: "projects", Summary: "Disconnect a repository and remove its links (owners)."},
	"GET /projects/{p}/code/repos/{id}/commits":                 {Tag: "projects", Summary: "Recent commits of a repository; q is a SHA or words of the message (editors).", Out: []core.CodeItem{}},
	"GET /projects/{p}/code/repos/{id}/releases":                {Tag: "projects", Summary: "Latest releases of a repository (editors).", Out: []core.CodeItem{}},
	"DELETE /projects/{p}/documents/{d}":                        {Tag: "documents", Summary: "Move to the trash.", Out: core.DocResult{}},
	"GET /projects/{p}/ai/status":                               {Tag: "ai", Summary: "Whether the person can use the AI chat in this project: switches, budgets and usage of this month (ADR-0066).", Out: core.ChatStatus{}},
	"GET /projects/{p}/ai/chats":                                {Tag: "ai", Summary: "The person's AI chats in this project, newest first.", Out: []store.AIChat{}},
	"POST /projects/{p}/ai/chats":                               {Tag: "ai", Summary: "Start an AI chat, optionally about a page ({\"document\": slug}).", Out: store.AIChat{}},
	"GET /projects/{p}/ai/chats/{c}":                            {Tag: "ai", Summary: "An AI chat with its messages and card proposals.", Out: aichat.View{}},
	"DELETE /projects/{p}/ai/chats/{c}":                         {Tag: "ai", Summary: "Delete an AI chat (its costs stay in the usage ledger)."},
	"POST /projects/{p}/ai/chats/{c}/messages":                  {Tag: "ai", Summary: "Send a message ({text, document, language}); the answer streams as server-sent events: text, tool, tool_done, proposal, notice, error, done. Web sessions only."},
	"POST /projects/{p}/ai/chats/{c}/proposals/{tool}":          {Tag: "ai", Summary: "Create the chosen cards of a card proposal ({accept, cards: [index]}) or reject it; the person creates them.", Out: []store.Task{}},
	"GET /projects/{p}/documents/{d}/tasks":                     {Tag: "documents", Summary: "Task cards that link this page or decision (ADR-0064).", Out: []store.Task{}},
	"POST /projects/{p}/documents/{d}/decide":                   {Tag: "documents", Summary: "Answer a proposed decision with options (ADR-0062): choice is an option id (accepts the decision) or none (nothing fits, stays proposed), with an optional comment. Editors; not for AI clients.", In: core.DecideInput{}, Out: core.DocResult{}},
	"GET /projects/{p}/drafts":                                  {Tag: "documents", Summary: "Open drafts of the project (editors; others get an empty list).", Out: []core.DraftInfo{}},
	"GET /projects/{p}/documents/{d}/draft":                     {Tag: "documents", Summary: "The open draft of a page (editors).", Out: core.DraftInfo{}},
	"POST /projects/{p}/documents/{d}/draft":                    {Tag: "documents", Summary: "Start or join the draft of a Markdown page (edit session).", Out: core.DraftInfo{}},
	"PUT /projects/{p}/documents/{d}/draft":                     {Tag: "documents", Summary: "Save the draft (Markdown, title, category); the published page stays unchanged.", In: core.DraftPatch{}, Out: core.DraftInfo{}},
	"DELETE /projects/{p}/documents/{d}/draft":                  {Tag: "documents", Summary: "Cancel the draft: discards the changes of all participants and ends the session.", Out: ok{}},
	"PUT /projects/{p}/documents/{d}/whiteboard":                {Tag: "documents", Summary: "Save a whiteboard from the live editor (published at once; saves of the same person within 10 minutes update one version).", In: core.Whiteboard{}, Out: map[string]any{}},
	"GET /projects/{p}/embed-policy":                            {Tag: "documents", Summary: "Which websites whiteboards of the project may embed live (instance and organization switches): web, sites (empty = any that allows framing).", Out: core.EmbedPolicy{}},
	"POST /projects/{p}/whiteboards/code-status":                {Tag: "documents", Summary: "Live state (open, draft, merged, closed) and title of pull requests and issues on whiteboards, through the project's code connections: {urls}.", In: codeStatusIn{}, Out: map[string]core.CodeStatus{}},
	"POST /projects/{p}/whiteboards/mermaid":                    {Tag: "documents", Summary: "Turn a Mermaid flowchart into shapes and bound arrows: {source, document (a whiteboard; empty creates one with title)}.", In: mermaidIn{}, Out: core.DocResult{}},
	"POST /projects/{p}/documents/{d}/whiteboard/neighbourhood": {Tag: "documents", Summary: "Draw an entry and the entries it is related to as shapes with arrows labelled by the relation: {entity}.", In: neighbourhoodIn{}, Out: core.DocResult{}},
	"GET /projects/{p}/embed-preview":                           {Tag: "documents", Summary: "Title, description and site of an address for a whiteboard embed (?url=…).", Out: map[string]any{}},
	"POST /projects/{p}/documents/{d}/draft/review":             {Tag: "documents", Summary: "Ask the owners to approve the draft (approval workflow).", In: noteIn{}, Out: core.DraftInfo{}},
	"POST /projects/{p}/documents/{d}/draft/reject":             {Tag: "documents", Summary: "Send the draft back with a note (owners).", In: noteIn{}, Out: core.DraftInfo{}},
	"GET /projects/{p}/documents/{d}/rules":                     {Tag: "documents", Summary: "Review date and required reading of a page.", Out: core.PageRuleInfo{}},
	"PUT /projects/{p}/documents/{d}/rules":                     {Tag: "documents", Summary: "Set the review interval and required reading (editors).", In: core.PageRulesInput{}, Out: core.PageRuleInfo{}},
	"POST /projects/{p}/documents/{d}/reviewed":                 {Tag: "documents", Summary: "Mark a page as reviewed (editors).", Out: core.PageRuleInfo{}},
	"POST /projects/{p}/documents/{d}/read":                     {Tag: "documents", Summary: "Confirm reading the current version of a page.", Out: core.PageRuleInfo{}},
	"GET /projects/{p}/entities/{e}/meta":                       {Tag: "graph", Summary: "Responsible person, review date and broken documentation rules of an entry.", Out: core.EntityMetaInfo{}},
	"PUT /projects/{p}/entities/{e}/meta":                       {Tag: "graph", Summary: "Set the responsible person (member) and review interval of an entry (editors).", In: core.EntityMetaInput{}, Out: core.EntityMetaInfo{}},
	"POST /projects/{p}/entities/{e}/reviewed":                  {Tag: "graph", Summary: "Mark an entry as reviewed (editors).", Out: core.EntityMetaInfo{}},
	"PUT /projects/{p}/kinds/{k}/rules":                         {Tag: "graph", Summary: "Documentation rules of a kind: location, page, owner, fields, review interval (owners).", In: core.KindRules{}, Out: core.Kind{}},
	"GET /projects/{p}/locations":                               {Tag: "graph", Summary: "Location hierarchy site → building → floor → room with entry counts.", Out: []core.LocationNode{}},
	"POST /projects/{p}/query":                                  {Tag: "graph", Summary: "Run a query over the knowledge graph: one kind, field and relation conditions.", In: core.GraphQuery{}, Out: core.QueryResult{}},
	"GET /projects/{p}/documents/{d}/skill":                     {Tag: "documents", Summary: "Whether a page is an AI skill (skill is null if not)."},
	"PUT /projects/{p}/documents/{d}/skill":                     {Tag: "documents", Summary: "Mark a text page as an AI skill or change its name and description (editors; not AI clients).", In: core.SkillInput{}},
	"DELETE /projects/{p}/documents/{d}/skill":                  {Tag: "documents", Summary: "Turn an AI skill back into an ordinary page (editors; not AI clients)."},
	"GET /projects/{p}/skills":                                  {Tag: "documents", Summary: "AI skills of a project.", Out: []core.SkillInfo{}},
	"GET /projects/{p}/skills.zip":                              {Tag: "documents", Summary: "All AI skills of a project as a ZIP of SKILL.md folders with attached files."},
	"GET /projects/{p}/skills/{name}":                           {Tag: "documents", Summary: "One AI skill with its instructions and attached files.", Out: core.SkillContent{}},
	"GET /projects/{p}/skills/{name}/download":                  {Tag: "documents", Summary: "One AI skill as a ZIP with SKILL.md and attached files."},
	"GET /me/home":   {Tag: "projects", Summary: "Numbers for the projects page: per project pages, open tasks, failing checks, pending suggestions and last activity; pinned projects; due tasks.", Out: core.HomeInfo{}},
	"PUT /me/pinned": {Tag: "projects", Summary: "Set the projects pinned on the projects page ({\"projects\": [ids]})."},
	"GET /me/skills": {Tag: "documents", Summary: "AI skills of all your projects.", Out: []core.SkillInfo{}},
	"GET /projects/{p}/entities/{e}/readings":                {Tag: "graph", Summary: "Measured values of an entry over the last hours (default 24, at most 90 days), condensed to at most 240 points per metric.", Out: []core.Series{}, Query: []string{"metric", "hours"}},
	"POST /projects/{p}/readings":                            {Tag: "graph", Summary: "Record measured values of entries (editors; at most 500 per call, 30 s apart per metric).", In: []core.ReadingInput{}, Out: core.RecordResult{}},
	"GET /projects/{p}/entities/{e}/device":                  {Tag: "graph", Summary: "Control tools that act on an entry, its AI control rule and (owners) who it is shared with.", Out: core.DeviceInfo{}},
	"PUT /projects/{p}/entities/{e}/device":                  {Tag: "graph", Summary: "Allow AI clients to switch a device without confirmation (aiControl direct) or not (suggest); owners."},
	"GET /projects/{p}/entities/{e}/device/log":              {Tag: "graph", Summary: "Who switched a device and who changed its rules.", Out: []core.AuditEvent{}},
	"POST /projects/{p}/entities/{e}/device/control":         {Tag: "graph", Summary: "Run a control tool on a device (editors and people it is shared with).", In: core.ControlInput{}},
	"POST /projects/{p}/entities/{e}/device/grants":          {Tag: "graph", Summary: "Let one person switch a device, optionally for a number of days (owners).", In: core.DeviceGrantInput{}, Out: core.DeviceInfo{}},
	"DELETE /projects/{p}/entities/{e}/device/grants/{id}":   {Tag: "graph", Summary: "Stop sharing a device with a person (owners).", Out: core.DeviceInfo{}},
	"GET /me/devices":                                        {Tag: "graph", Summary: "Devices other people shared with you.", Out: []core.SharedDevice{}},
	"POST /me/devices/{grant}/control":                       {Tag: "graph", Summary: "Switch a device shared with you.", In: core.ControlInput{}},
	"GET /projects/{p}/queries":                              {Tag: "graph", Summary: "Saved queries of a project.", Out: []core.SavedQueryInfo{}},
	"POST /projects/{p}/queries":                             {Tag: "graph", Summary: "Save a query under a name (editors).", In: core.SaveQueryInput{}, Out: core.SavedQueryInfo{}},
	"PUT /projects/{p}/queries/{q}":                          {Tag: "graph", Summary: "Change a saved query (editors).", In: core.SaveQueryInput{}, Out: core.SavedQueryInfo{}},
	"DELETE /projects/{p}/queries/{q}":                       {Tag: "graph", Summary: "Delete a saved query (editors)."},
	"GET /projects/{p}/queries/{q}/run":                      {Tag: "graph", Summary: "Run a saved query by slug or id.", Out: core.QueryResult{}},
	"POST /projects/{p}/entities/bulk":                       {Tag: "graph", Summary: "Create, change or delete many entries at once (table view, CSV import; editors).", In: core.BulkInput{}, Out: core.BulkResult{}},
	"GET /projects/{p}/household":                            {Tag: "graph", Summary: "Household overview: inventory value, warranties and yearly power cost of entries.", Out: core.Household{}},
	"PUT /projects/{p}/household/price":                      {Tag: "graph", Summary: "Set the electricity price per kWh (editors).", In: powerPriceIn{}, Out: powerPriceIn{}},
	"GET /speedtest/ping":                                    {Tag: "me", Summary: "Speed test: round trip (no content)."},
	"GET /speedtest/down":                                    {Tag: "me", Summary: "Speed test: random bytes to download (?bytes, at most 100 MB)."},
	"POST /speedtest/up":                                     {Tag: "me", Summary: "Speed test: upload bytes; answers how many arrived.", Out: speedUpOut{}},
	"GET /projects/{p}/ipam":                                 {Tag: "graph", Summary: "IP address management: subnets with used and free addresses, conflicts, addresses outside any subnet.", Out: core.IPAMReport{}},
	"GET /agent/terminal/{id}":                               {Tag: "agents", Summary: "WebSocket for the agent side of a web terminal (agent token).", Public: true},
	"GET /agent/speedtest/ping":                              {Tag: "agents", Summary: "Speed test round trip for agents (agent token).", Public: true},
	"GET /agent/speedtest/down":                              {Tag: "agents", Summary: "Speed test download for agents (agent token).", Public: true},
	"POST /agent/speedtest/up":                               {Tag: "agents", Summary: "Speed test upload for agents (agent token).", Public: true, Out: speedUpOut{}},
	"GET /projects/{p}/terminal/{id}":                        {Tag: "agents", Summary: "WebSocket for the browser side of a web terminal (the owner who created the signed terminal job)."},
	"POST /qr":                                               {Tag: "me", Summary: "Render a QR code as SVG (not stored or logged), e.g. for a guest Wi-Fi card.", In: qrIn{}, Out: qrOut{}},
	"GET /projects/{p}/approval":                             {Tag: "projects", Summary: "Whether page changes need an owner's approval.", Out: approvalIn{}},
	"PUT /projects/{p}/approval":                             {Tag: "projects", Summary: "Switch the approval workflow (owners).", In: approvalIn{}, Out: approvalIn{}},
	"GET /projects/{p}/maintenance":                          {Tag: "tasks", Summary: "Maintenance plans of a project (?entity= for one entry).", Out: []store.MaintenancePlan{}},
	"POST /projects/{p}/maintenance":                         {Tag: "tasks", Summary: "Create a maintenance plan (editors).", In: core.MaintenanceInput{}, Out: store.MaintenancePlan{}},
	"PUT /projects/{p}/maintenance/{id}":                     {Tag: "tasks", Summary: "Change a maintenance plan (editors).", In: core.MaintenanceInput{}, Out: store.MaintenancePlan{}},
	"DELETE /projects/{p}/maintenance/{id}":                  {Tag: "tasks", Summary: "Delete a maintenance plan; its cards stay (editors)."},
	"POST /projects/{p}/documents/{d}/draft/publish":         {Tag: "documents", Summary: "Publish the draft as a new version and end the session for everyone.", Out: core.DocResult{}},
	"GET /projects/{p}/documents/{d}/versions":               {Tag: "documents", Summary: "Version history.", Out: []core.VersionInfo{}},
	"GET /projects/{p}/documents/{d}/versions/{n}":           {Tag: "documents", Summary: "An older version.", Out: core.VersionInfo{}},
	"POST /projects/{p}/documents/{d}/versions/{n}/restore":  {Tag: "documents", Summary: "Restore an older version (as a new one).", Out: core.DocResult{}},
	"POST /projects/{p}/documents/{d}/requests/{n}/run":      {Tag: "documents", Summary: "Run a request block of the page.", Out: requests.Result{}},
	"GET /projects/{p}/documents/{d}/comments":               {Tag: "comments", Summary: "Comment threads of a page, open ones first.", Out: []core.Thread{}},
	"POST /projects/{p}/documents/{d}/comments":              {Tag: "comments", Summary: "Start a thread (optionally on a block: anchor) or answer one (parent).", In: core.CommentInput{}, Out: store.Comment{}},
	"PATCH /projects/{p}/comments/{id}":                      {Tag: "comments", Summary: "Edit a comment (author) or mark a thread done or open.", In: core.CommentPatch{}, Out: store.Comment{}},
	"DELETE /projects/{p}/comments/{id}":                     {Tag: "comments", Summary: "Delete a comment (author or owner); the first one takes its thread."},
	"GET /projects/{p}/comments/open":                        {Tag: "comments", Summary: "Number of open threads per page id."},
	"GET /me/tasks":                                          {Tag: "tasks", Summary: "Open tasks assigned to the signed-in person in all projects, due ones first.", Out: []core.MyTask{}},
	"GET /projects/{p}/live":                                 {Tag: "projects", Summary: "Server-sent events (change: kind, subject, actor) when anything in the project changes; lists reload."},
	"GET /projects/{p}/boards/{d}/events":                    {Tag: "tasks", Summary: "Server-sent events (change) when cards or columns of a board change."},
	"GET /me/notifications":                                  {Tag: "notifications", Summary: "Newest notifications of the signed-in person (?unread=1, ?limit=).", Out: core.NotificationList{}},
	"POST /me/notifications/read":                            {Tag: "notifications", Summary: "Mark notifications as read ({ids}; none = all)."},
	"GET /me/notification-settings":                          {Tag: "notifications", Summary: "Mail mode, ntfy address, muted kinds and web push key.", Out: core.NotificationSettingsView{}},
	"PUT /me/notification-settings":                          {Tag: "notifications", Summary: "Change mail mode (instant, daily, off), ntfy address and muted kinds.", In: store.NotificationSettings{}, Out: core.NotificationSettingsView{}},
	"POST /me/push-subscriptions":                            {Tag: "notifications", Summary: "Register this browser for web push.", In: core.PushSubscriptionInput{}},
	"POST /me/push-subscriptions/remove":                     {Tag: "notifications", Summary: "Unregister a browser ({endpoint})."},
	"POST /me/notifications/test":                            {Tag: "notifications", Summary: "Send a test notification on all channels."},
	"GET /projects/{p}/status":                               {Tag: "monitoring", Summary: "State of entries (?e=<slug>, repeatable) and checks (?check=<name> or ?check=*) for dashboards.", Out: core.StatusBoard{}},
	"GET /orgs/{o}/domain":                                   {Tag: "orgs", Summary: "The organization's own app domain and the DNS TXT record proving it (org admins); access tells whether custom domains are paid and why the org may use them."},
	"GET /admin/ai/providers":                                {Tag: "admin", Summary: "Model providers of the instance, including the built-in one from PHOEDOX_AI_* (ADR-0067). API keys are never returned.", Out: []store.AIProvider{}},
	"POST /admin/ai/providers":                               {Tag: "admin", Summary: "Add a model provider: Anthropic or an OpenAI-compatible API (OpenAI, Mistral, Kimi, Ollama, gateways), with models and prices.", In: core.AIProviderInput{}, Out: store.AIProvider{}},
	"PUT /admin/ai/providers/{id}":                           {Tag: "admin", Summary: "Change a model provider (apiKey omitted keeps the key, empty removes it).", In: core.AIProviderInput{}, Out: store.AIProvider{}},
	"DELETE /admin/ai/providers/{id}":                        {Tag: "admin", Summary: "Remove a model provider."},
	"POST /admin/ai/providers/{id}/models":                   {Tag: "admin", Summary: "Ask the provider which models it offers.", Out: []string{}},
	"GET /orgs/{o}/ai/providers":                             {Tag: "orgs", Summary: "The organization's own model providers (own keys). Org admins.", Out: []store.AIProvider{}},
	"POST /orgs/{o}/ai/providers":                            {Tag: "orgs", Summary: "Add an own model provider of the organization. Its address must be public on hosted instances.", In: core.AIProviderInput{}, Out: store.AIProvider{}},
	"PUT /orgs/{o}/ai/providers/{id}":                        {Tag: "orgs", Summary: "Change an own model provider.", In: core.AIProviderInput{}, Out: store.AIProvider{}},
	"DELETE /orgs/{o}/ai/providers/{id}":                     {Tag: "orgs", Summary: "Remove an own model provider."},
	"POST /orgs/{o}/ai/providers/{id}/models":                {Tag: "orgs", Summary: "Ask the provider which models it offers.", Out: []string{}},
	"GET /projects/{p}/ai/models":                            {Tag: "ai", Summary: "Models people can choose in this project's AI chat, and the default.", Out: core.ChatModels{}},
	"GET /orgs/{o}/ai-chat":                                  {Tag: "orgs", Summary: "AI chat of the organization: switch, budgets, limit from system admins and usage of this month (ADR-0066). Org admins.", Out: core.OrgAIChat{}},
	"GET /orgs/{o}/features":                                 {Tag: "orgs", Summary: "Paid features of this instance and the organization's access (free, grandfathered, plan, grant or none; ADR-0061). Org admins.", Out: []core.FeatureAccess{}},
	"GET /me/features":                                       {Tag: "account", Summary: "Paid features of this instance and your access for personal projects (ADR-0061).", Out: []core.FeatureAccess{}},
	"PUT /orgs/{o}/domain":                                   {Tag: "orgs", Summary: "Choose an app domain ({domain}; empty removes it). It is used once verified."},
	"POST /orgs/{o}/domain/verify":                           {Tag: "orgs", Summary: "Look up the TXT record _phoedox.<domain> and activate the domain."},
	"GET /projects/{p}/export":                               {Tag: "projects", Summary: "The project as a ZIP (settings, pages with translations, entries, relations, kinds, boards, files; owners, read-only tokens work). ?format=git: a Git repository (as ZIP) with one commit per document version plus the full export."},
	"GET /projects/{p}/export/link":                          {Tag: "projects", Summary: "A download link of the export valid for 15 minutes (?format=zip|git; owners, also for AI clients).", Out: core.ExportLink{}},
	"GET /exports/{token}":                                   {Tag: "projects", Summary: "Download behind an export link (no session needed while the link is valid)."},
	"GET /projects/{p}/offline":                              {Tag: "projects", Summary: "Emergency copy: the project as static HTML pages in a ZIP that open without a server (what you may read)."},
	"POST /projects/{p}/answer":                              {Tag: "projects", Summary: "Short spoken answer to a question (voice assistants such as Home Assistant Assist): location and values of entries, else the best search hit.", Out: core.AnswerResult{}},
	"GET /projects/{p}/rules":                                {Tag: "projects", Summary: "Automation rules of a project (editors).", Out: []store.Rule{}},
	"POST /projects/{p}/rules":                               {Tag: "projects", Summary: "Create an automation rule: trigger (check.fail, check.recover, entity.create, date) and action (task, notify, http). Owners.", In: core.RuleInput{}, Out: store.Rule{}},
	"PUT /projects/{p}/rules/{id}":                           {Tag: "projects", Summary: "Change an automation rule (owners).", In: core.RuleInput{}, Out: store.Rule{}},
	"DELETE /projects/{p}/rules/{id}":                        {Tag: "projects", Summary: "Delete an automation rule (owners)."},
	"POST /projects/{p}/rules/{id}/test":                     {Tag: "projects", Summary: "Run a rule's action once with sample values (owners)."},
	"POST /projects/import":                                  {Tag: "projects", Summary: "Create a project from an export ZIP (request body).", Out: core.ImportResultProject{}},
	"POST /projects/import-remote/list":                      {Tag: "projects", Summary: "Projects another instance offers for the given API token ({url, token}).", In: core.RemoteSource{}, Out: []core.RemoteProject{}},
	"POST /projects/import-remote":                           {Tag: "projects", Summary: "Move a project from another instance ({url, token, project}).", In: core.RemoteSource{}, Out: core.ImportResultProject{}},
	"GET /projects/{p}/git":                                  {Tag: "projects", Summary: "Git sync of the project.", Out: store.ProjectGit{}},
	"PUT /projects/{p}/git":                                  {Tag: "projects", Summary: "Set up Git sync (https URL, branch, folder, name of the token secret).", In: core.GitInput{}, Out: store.ProjectGit{}},
	"POST /projects/{p}/git/sync":                            {Tag: "projects", Summary: "Sync to Git now.", Out: store.ProjectGit{}},
	"GET /projects/{p}/checks":                               {Tag: "monitoring", Summary: "Checks of a project (?document= narrows to one page).", Out: []core.CheckView{}},
	"POST /projects/{p}/checks":                              {Tag: "monitoring", Summary: "Monitor a reading request block on a schedule.", In: core.CheckInput{}, Out: core.CheckView{}},
	"PATCH /projects/{p}/checks/{id}":                        {Tag: "monitoring", Summary: "Change name, interval, entry or switch of a check.", In: core.CheckInput{}, Out: core.CheckView{}},
	"DELETE /projects/{p}/checks/{id}":                       {Tag: "monitoring", Summary: "Remove a check."},
	"POST /projects/{p}/checks/{id}/run":                     {Tag: "monitoring", Summary: "Run a check now.", Out: core.CheckView{}},
	"GET /projects/{p}/documents/{d}/sharing":                {Tag: "sharing", Summary: "Restriction and people/groups a page is shared with (owners).", Out: core.DocSharing{}},
	"PUT /projects/{p}/documents/{d}/sharing":                {Tag: "sharing", Summary: "Restrict a page to owners and shares.", In: restrictIn{}, Out: core.DocSharing{}},
	"POST /projects/{p}/documents/{d}/sharing/grants":        {Tag: "sharing", Summary: "Share a page with a person or group.", In: core.GrantInput{}, Out: core.DocSharing{}},
	"DELETE /projects/{p}/documents/{d}/sharing/grants/{id}": {Tag: "sharing", Summary: "Stop sharing.", Out: core.DocSharing{}},
	"GET /publications/{id}/releases":                        {Tag: "publications", Summary: "Fixed states of a publication, newest first.", Out: []store.PublicationRelease{}},
	"POST /publications/{id}/releases":                       {Tag: "publications", Summary: "Freeze the current state as the next release (/v/<n>/).", Out: store.PublicationRelease{}},
	"GET /templates":                                         {Tag: "projects", Summary: "Project templates (?lang=de|en): name, description, icon, tabs.", Out: []core.Template{}},
	"GET /admin/events":                                      {Tag: "admin", Summary: "Audit log of the instance (?via=web|mcp|token|plugin|agent|cli, user, kind prefix, days, before, limit).", Out: []core.AuditEvent{}},
	"POST /projects/{p}/import/markdown":                     {Tag: "projects", Summary: "Import a ZIP of Markdown files (also Obsidian vaults): folders become categories, [[links]] links.", Raw: true, Out: core.ImportResult{}},
	"POST /projects/{p}/import/openapi":                      {Tag: "projects", Summary: "Import an OpenAPI 3 / Swagger 2 file (JSON or YAML): a service entry and a page per tag with request blocks.", Raw: true, Out: core.ImportResult{}},
	"GET /projects/{p}/duplicates":                           {Tag: "graph", Summary: "Entries that look like the same thing (same MAC, serial number or hostname).", Out: []core.Duplicate{}},
	"POST /projects/{p}/entities-merge":                      {Tag: "graph", Summary: "Merge entry remove into keep: relations, page links and attachments move over.", Out: store.Entity{}},
	"GET /projects/{p}/topology":                             {Tag: "graph", Summary: "Network plan: stations connected by uplink, wired_to and runs_on, with status.", Out: core.Topology{}},
	"GET /projects/{p}/events":                               {Tag: "projects", Summary: "Audit log of a project (owners); same filters as /admin/events.", Out: []core.AuditEvent{}},
	"GET /projects/{p}/entities/{e}/tree":                    {Tag: "graph", Summary: "Software tree of a host: software, VMs, stacks and containers that are installed on, run on or are part of it.", Out: core.TreeNode{}},
	"GET /projects/{p}/entities/{e}/diagnose":                {Tag: "graph", Summary: "Network path of a device or of the devices in a room, with a status per station.", Out: core.Diagnosis{}},
	"GET /agent/jobs":                                        {Tag: "agents", Summary: "Approved, signed jobs for the agent; waits up to ?wait= seconds (max 25)."},
	"GET /agent/releases/{tag}/{asset}":                      {Tag: "agents", Summary: "An agent binary for self-updates (agent token), verified against the release's SHA256SUMS."},
	"POST /agent/jobs/{id}/result":                           {Tag: "agents", Summary: "The agent reports how a job went."},
	"GET /me/signing-keys":                                   {Tag: "agents", Summary: "Your public keys for signing agent jobs.", Out: []store.SigningKey{}},
	"POST /me/signing-keys":                                  {Tag: "agents", Summary: "Register a public key created in the browser (the private key never leaves it).", In: core.SigningKeyInput{}, Out: store.SigningKey{}},
	"DELETE /me/signing-keys/{id}":                           {Tag: "agents", Summary: "Remove a signing key."},
	"GET /projects/{p}/entities/{e}/agent-actions":           {Tag: "agents", Summary: "Agent actions available for an entry, with recent jobs.", Out: core.EntityActions{}},
	"POST /projects/{p}/entities/{e}/agent-jobs":             {Tag: "agents", Summary: "Prepare a job for one of the entry's actions; it runs once signed.", Out: store.AgentJob{}},
	"POST /projects/{p}/agent-jobs/{id}/sign":                {Tag: "agents", Summary: "Approve a job with an ed25519 signature over its payload.", Out: store.AgentJob{}},
	"POST /agent/enroll":                                     {Tag: "agents", Summary: "Exchange an enrollment code for an agent token (phoedox-agent enroll).", Out: core.Enrolled{}},
	"POST /agent/report":                                     {Tag: "agents", Summary: "Report a host snapshot with the agent token (read-only, ADR-0033).", In: core.Report{}},
	"GET /projects/{p}/agents":                               {Tag: "agents", Summary: "Agents of the project (owners).", Out: []store.Agent{}},
	"POST /projects/{p}/agents/enrollments":                  {Tag: "agents", Summary: "Create an enrollment code for up to 20 hosts, valid 24 hours.", Out: core.Enrollment{}},
	"DELETE /projects/{p}/agents/{id}":                       {Tag: "agents", Summary: "Revoke an agent."},
	"GET /publications":                                      {Tag: "publications", Summary: "Publications that include a project you own.", Out: []core.PublicationInfo{}},
	"POST /publications":                                     {Tag: "publications", Summary: "Publish projects you own as a site (/pub/<slug> or an own domain).", In: core.PublicationInput{}, Out: core.PublicationInfo{}},
	"PATCH /publications/{id}":                               {Tag: "publications", Summary: "Change a publication.", In: core.PublicationInput{}, Out: core.PublicationInfo{}},
	"DELETE /publications/{id}":                              {Tag: "publications", Summary: "Delete a publication (the projects stay)."},
	"PUT /me/avatar":                                         {Tag: "account", Summary: "Set the profile picture (raw PNG, JPEG or WebP body, ≤ 512 KB, ≤ 1024 px)."},
	"DELETE /me/avatar":                                      {Tag: "account", Summary: "Remove the profile picture."},
	"GET /images/user/{id}":                                  {Tag: "account", Summary: "A profile picture (?v=version for caching)."},
	"GET /projects/{p}/image":                                {Tag: "projects", Summary: "The project image."},
	"PUT /projects/{p}/image":                                {Tag: "projects", Summary: "Set the project image (raw body like the profile picture; owners)."},
	"DELETE /projects/{p}/image":                             {Tag: "projects", Summary: "Remove the project image."},
	"GET /projects/{p}/tasks":                                {Tag: "tasks", Summary: "Tasks of the project, a board (board), a column or an assignee (me, user id or e-mail); open=true hides done ones.", Out: []store.Task{}, Query: []string{"board", "column", "assignee", "open"}},
	"POST /projects/{p}/boards/{d}/tasks":                    {Tag: "tasks", Summary: "Add a task to a board (column id or name; default first column).", In: core.TaskInput{}, Out: store.Task{}},
	"PATCH /projects/{p}/tasks/{id}":                         {Tag: "tasks", Summary: "Change or move a task (column, position, board).", In: core.TaskPatch{}, Out: store.Task{}},
	"DELETE /projects/{p}/tasks/{id}":                        {Tag: "tasks", Summary: "Delete a task."},
	"GET /projects/{p}/decisions":                            {Tag: "documents", Summary: "Decision log (ADR numbers, status, supersedes).", Out: []core.DecisionSummary{}, Query: []string{"status", "q"}},
	"GET /search":                                            {Tag: "documents", Summary: "Full-text search over pages, entries and files.", Out: []store.Hit{}, Query: []string{"q", "project", "limit"}},

	"GET /projects/{p}/model":                 {Tag: "graph", Summary: "Data model: kinds, attributes, allowed relations.", Out: core.Model{}},
	"PUT /projects/{p}/kinds/{k}":             {Tag: "graph", Summary: "Define an own kind or extend a core kind.", In: core.KindInput{}, Out: core.KindResult{}},
	"DELETE /projects/{p}/kinds/{k}":          {Tag: "graph", Summary: "Delete an own kind (unused) or a core kind's extension.", Out: ok{}},
	"GET /projects/{p}/graph":                 {Tag: "graph", Summary: "All entries and relations.", Out: core.Graph{}},
	"GET /projects/{p}/entities":              {Tag: "graph", Summary: "Entries, optionally of one kind.", Out: []store.Entity{}, Query: []string{"kind"}},
	"POST /projects/{p}/entities":             {Tag: "graph", Summary: "Create or update an entry (by slug).", In: core.EntityInput{}, Out: core.EntityResult{}},
	"GET /projects/{p}/entities/{e}":          {Tag: "graph", Summary: "An entry with relations and mentioning pages.", Out: core.EntityDetail{}},
	"DELETE /projects/{p}/entities/{e}":       {Tag: "graph", Summary: "Move an entry with its relations to the trash.", Out: core.EntityResult{}},
	"GET /projects/{p}/entities/{e}/traverse": {Tag: "graph", Summary: "Follow relations.", Out: []store.Hop{}, Query: []string{"rel", "dir", "depth"}},
	"POST /projects/{p}/edges":                {Tag: "graph", Summary: "Create a relation (only allowed ones).", In: core.EdgeInput{}, Out: core.EntityResult{}},
	"POST /projects/{p}/edges/remove":         {Tag: "graph", Summary: "Remove a relation.", In: core.EdgeInput{}, Out: core.EntityResult{}},

	"GET /projects/{p}/files":                            {Tag: "files", Summary: "Files of a project, a page or an entry.", Out: []store.File{}, Query: []string{"document", "entity"}},
	"POST /projects/{p}/files":                           {Tag: "files", Summary: "Upload a file (raw body).", Raw: true, Out: store.File{}, Query: []string{"name", "document", "entity"}},
	"PATCH /projects/{p}/files/{id}":                     {Tag: "files", Summary: "Rename a file.", In: nameIn{}, Out: store.File{}},
	"DELETE /projects/{p}/files/{id}":                    {Tag: "files", Summary: "Move a file to the trash.", Out: ok{}},
	"GET /projects/{p}/files/{id}/versions":              {Tag: "files", Summary: "Versions of a file.", Out: []store.FileVersion{}},
	"POST /projects/{p}/files/{id}/versions":             {Tag: "files", Summary: "Upload a new version (raw body).", Raw: true, Out: store.File{}},
	"POST /projects/{p}/files/{id}/versions/{n}/restore": {Tag: "files", Summary: "Make an older version current.", Out: store.File{}},
	"GET /admin/plugins/collections":                     {Tag: "plugins", Summary: "Curated plugin collections from the store (admins).", Out: []core.PluginCollection{}},
	"POST /secret-links":                                 {Tag: "security", Summary: "Store an end-to-end encrypted one-time secret (ciphertext only; the key stays in the link's #fragment).", In: core.SecretLinkInput{}, Out: core.SecretLinkCreated{}},
	"POST /secret-links/{id}/open":                       {Tag: "security", Summary: "Open a one-time secret: returns the ciphertext once and deletes it (no sign-in)."},
	"GET /admin/audit/verify":                            {Tag: "admin", Summary: "Check the audit log's hash chain of this tenant.", Out: store.ChainReport{}},
	"GET /me/recent":                                     {Tag: "account", Summary: "Recently opened pages and entries (?limit, newest first).", Out: []core.RecentItem{}},
	"POST /me/recent":                                    {Tag: "account", Summary: "Remember that a page or entry was opened (project, kind document|entity, ref)."},
	"DELETE /me/recent":                                  {Tag: "account", Summary: "Forget one entry (?ref=id) or the whole history."},
	"GET /me/shortcuts":                                  {Tag: "account", Summary: "Personal keyboard shortcuts (action → combination)."},
	"PUT /me/shortcuts":                                  {Tag: "account", Summary: "Store personal keyboard shortcuts."},
	"GET /projects/{p}/gaps":                             {Tag: "insights", Summary: "Documentation gaps of a project (?staleDays, default 365).", Out: core.GapReport{}},
	"GET /projects/{p}/changes":                          {Tag: "insights", Summary: "What changed in the last ?days (default 7, at most 90).", Out: core.ChangeSummary{}},
	"POST /projects/{p}/documents/{d}/archive":           {Tag: "documents", Summary: "Keep a copy of a link page's target as a project file.", Out: core.DocResult{}},
	"POST /projects/{p}/links/archive":                   {Tag: "documents", Summary: "Archive all link pages without a copy (owners)."},
	"GET /projects/{p}/incidents":                        {Tag: "monitoring", Summary: "Incidents of the last 30 days and all open ones, with their timelines.", Out: []store.Incident{}},
	"POST /projects/{p}/incidents":                       {Tag: "monitoring", Summary: "Open an incident.", In: core.IncidentInput{}, Out: store.Incident{}},
	"PATCH /projects/{p}/incidents/{id}":                 {Tag: "monitoring", Summary: "Change title and visibility of an incident.", In: core.IncidentInput{}, Out: store.Incident{}},
	"DELETE /projects/{p}/incidents/{id}":                {Tag: "monitoring", Summary: "Delete an incident."},
	"POST /projects/{p}/incidents/{id}/updates":          {Tag: "monitoring", Summary: "Add an update to the timeline (and set the status).", In: core.IncidentUpdateInput{}, Out: store.Incident{}},
	"GET /projects/{p}/status-page":                      {Tag: "monitoring", Summary: "Whether the project has a public status page, and its address.", Out: core.StatusPageSettings{}},
	"PUT /projects/{p}/status-page":                      {Tag: "monitoring", Summary: "Turn the public status page on or off (owners).", Out: core.StatusPageSettings{}},
	"GET /status/{token}":                                {Tag: "monitoring", Summary: "A public status page as JSON (no sign-in).", Out: core.StatusPage{}},
	"GET /projects/{p}/files/{id}/text":                  {Tag: "files", Summary: "Text recognised in a file (PDF text or OCR); status is empty while processing.", Out: core.FileText{}},
	"POST /projects/{p}/files/{id}/plan-image":           {Tag: "files", Summary: "Render a PDF as floor plan background (PNG of the first page).", Out: store.File{}},
	"GET /files/{id}":                                    {Tag: "files", Summary: "Download a file (version, preview image).", Query: []string{"version", "preview", "download"}},
	"GET /files/{id}/{name}":                             {Tag: "files", Summary: "Download a file with its name in the URL.", Query: []string{"version", "preview", "download"}},
	"OPTIONS /projects/{p}/uploads":                      {Tag: "files", Summary: "tus: capabilities of resumable uploads."},
	"POST /projects/{p}/uploads":                         {Tag: "files", Summary: "tus: start a resumable upload (Upload-Length, Upload-Metadata)."},
	"HEAD /uploads/{id}":                                 {Tag: "files", Summary: "tus: how many bytes arrived (Upload-Offset)."},
	"PATCH /uploads/{id}":                                {Tag: "files", Summary: "tus: send the next chunk (Upload-Offset); the last one returns Phoedox-File-Id.", Raw: true},
	"DELETE /uploads/{id}":                               {Tag: "files", Summary: "tus: cancel an upload."},
}

var (
	specOnce sync.Once
	specJSON []byte
	paramRe  = regexp.MustCompile(`\{(\w+)\}`)
)

func normRoute(method, route string) string {
	route = strings.TrimPrefix(route, "/api/v1")
	if len(route) > 1 {
		route = strings.TrimSuffix(route, "/")
	}
	return method + " " + route
}

func schemaOf(v any) map[string]any {
	opts := &jsonschema.ForOptions{IgnoreInvalidTypes: true, TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[store.Time]():      {Types: []string{"string", "null"}, Format: "date-time"},
		reflect.TypeFor[store.JSON]():      {},
		reflect.TypeFor[json.RawMessage](): {},
	}}
	s, err := jsonschema.ForType(reflect.TypeOf(v), opts)
	if err != nil {
		return map[string]any{}
	}
	b, _ := json.Marshal(s)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	delete(out, "$schema")
	return out
}

// openAPI builds the OpenAPI 3.1 description from the router and apiDocs.
func (a *api) openAPI(router chi.Routes) []byte {
	paths := map[string]map[string]any{}
	_ = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/v1/") {
			return nil
		}
		key := normRoute(method, route)
		doc := apiDocs[key]
		path := strings.TrimPrefix(key, method+" ")
		o := map[string]any{"summary": doc.Summary, "tags": []string{doc.Tag}, "operationId": strings.ToLower(method) + strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(path)}
		var params []map[string]any
		for _, m := range paramRe.FindAllStringSubmatch(path, -1) {
			params = append(params, map[string]any{"name": m[1], "in": "path", "required": true, "schema": map[string]string{"type": "string"}})
		}
		for _, q := range doc.Query {
			params = append(params, map[string]any{"name": q, "in": "query", "schema": map[string]string{"type": "string"}})
		}
		if params != nil {
			o["parameters"] = params
		}
		if doc.Raw {
			o["requestBody"] = map[string]any{"content": map[string]any{"application/octet-stream": map[string]any{"schema": map[string]string{"type": "string", "format": "binary"}}}}
		} else if doc.In != nil {
			o["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": schemaOf(doc.In)}}}
		}
		resp := map[string]any{"description": "OK"}
		if doc.Out != nil {
			resp["content"] = map[string]any{"application/json": map[string]any{"schema": schemaOf(doc.Out)}}
		}
		o["responses"] = map[string]any{"2XX": resp, "default": map[string]any{"description": "Error", "content": map[string]any{"application/json": map[string]any{"schema": map[string]string{"$ref": "#/components/schemas/Error"}}}}}
		if doc.Public {
			o["security"] = []any{}
		}
		if paths[path] == nil {
			paths[path] = map[string]any{}
		}
		paths[path][strings.ToLower(method)] = o
		return nil
	})
	keys := make([]string, 0, len(paths))
	for k := range paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	spec := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{"title": "Phoedox API", "version": Version,
			"description": "REST API of Phoedox. Sign in with a session cookie (web app) or a personal API token as `Authorization: Bearer phx_…`. " +
				"Cookie requests that change data need the header `X-Phoedox: 1`. Errors carry a stable `code`, a translation `key` with `params` and an English `error`."},
		"servers": []map[string]string{{"url": "/api/v1"}},
		"paths":   paths,
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"token":   map[string]string{"type": "http", "scheme": "bearer", "description": "Personal API token (phx_…)"},
				"session": map[string]string{"type": "apiKey", "in": "cookie", "name": "phx_session"},
			},
			"schemas": map[string]any{"Error": schemaOf(apiError{})},
		},
		"security": []map[string][]string{{"token": {}}, {"session": {}}},
	}
	b, _ := json.MarshalIndent(spec, "", "  ")
	return b
}

// serveOpenAPI returns the description (public, so tools can read it).
func (a *api) serveOpenAPI(w http.ResponseWriter, _ *http.Request) {
	{
		specOnce.Do(func() { specJSON = a.openAPI(a.root) })
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write(specJSON)
	}
}
