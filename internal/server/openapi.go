package server

import (
	"encoding/json"
	"github.com/phoedox/phoedox/internal/backup"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/google/jsonschema-go/jsonschema"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/plugins"
	"github.com/phoedox/phoedox/internal/requests"
	"github.com/phoedox/phoedox/internal/store"
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
	"GET /auth/links/{token}":            {Tag: "auth", Summary: "Read an invitation or reset link.", Public: true},
	"POST /auth/links/{token}":           {Tag: "auth", Summary: "Set the password with an invitation or reset link.", Public: true},
	"GET /auth/oidc/start":               {Tag: "auth", Summary: "Start single sign-on (redirect).", Public: true},
	"GET /auth/oidc/callback":            {Tag: "auth", Summary: "Single sign-on callback (redirect).", Public: true},
	"GET /instance":                      {Tag: "auth", Summary: "Version and sign-in options of this instance.", Public: true},
	"GET /openapi.json":                  {Tag: "auth", Summary: "This description (OpenAPI 3.1).", Public: true},

	"GET /me":                          {Tag: "account", Summary: "The signed-in user.", Out: store.User{}},
	"PATCH /me":                        {Tag: "account", Summary: "Change name or language."},
	"POST /me/password":                {Tag: "account", Summary: "Change the password."},
	"GET /me/security":                 {Tag: "account", Summary: "Second factors and passkeys."},
	"POST /me/totp":                    {Tag: "account", Summary: "Start setting up TOTP."},
	"POST /me/totp/confirm":            {Tag: "account", Summary: "Confirm TOTP with a code."},
	"POST /me/totp/disable":            {Tag: "account", Summary: "Turn TOTP off."},
	"POST /me/recovery-codes":          {Tag: "account", Summary: "New recovery codes."},
	"POST /me/passkeys/begin":          {Tag: "account", Summary: "Start adding a passkey."},
	"POST /me/passkeys/finish":         {Tag: "account", Summary: "Finish adding a passkey."},
	"PATCH /me/passkeys/{id}":          {Tag: "account", Summary: "Rename a passkey.", In: nameIn{}},
	"DELETE /me/passkeys/{id}":         {Tag: "account", Summary: "Remove a passkey."},
	"GET /tokens":                      {Tag: "account", Summary: "Personal API tokens.", Out: []store.Token{}},
	"POST /tokens":                     {Tag: "account", Summary: "Create an API token (read/write, all or some projects); the token is shown once.", In: tokenIn{}},
	"DELETE /tokens/{id}":              {Tag: "account", Summary: "Revoke an API token."},
	"GET /oauth/grants":                {Tag: "account", Summary: "Apps connected with OAuth (MCP).", Out: []store.OAuthGrant{}},
	"DELETE /oauth/grants/{id}":        {Tag: "account", Summary: "Disconnect an app."},
	"GET /oauth/request":               {Tag: "account", Summary: "Consent page: the app asking for access."},
	"POST /oauth/authorize":            {Tag: "account", Summary: "Consent page: allow or deny."},
	"GET /admin/settings":              {Tag: "admin", Summary: "Instance settings.", Out: core.InstanceSettings{}},
	"PUT /admin/settings":              {Tag: "admin", Summary: "Change instance settings (AI switch, storage quota).", In: core.SettingsPatch{}, Out: core.InstanceSettings{}},
	"GET /admin/users":                 {Tag: "admin", Summary: "All users.", Out: []store.User{}},
	"POST /admin/users":                {Tag: "admin", Summary: "Invite a user."},
	"PATCH /admin/users/{id}":          {Tag: "admin", Summary: "Change, disable or make admin."},
	"POST /admin/users/{id}/link":      {Tag: "admin", Summary: "New invitation or reset link."},
	"POST /admin/users/{id}/reset-2fa": {Tag: "admin", Summary: "Remove a user's second factors."},

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

	"GET /projects":                                {Tag: "projects", Summary: "Projects you can see, with your role.", Out: []store.Project{}},
	"POST /projects":                               {Tag: "projects", Summary: "Create a project (personal or in an organization).", In: core.ProjectInput{}, Out: store.Project{}},
	"GET /projects/{p}":                            {Tag: "projects", Summary: "A project.", Out: store.Project{}},
	"PATCH /projects/{p}":                          {Tag: "projects", Summary: "Change settings (owners).", In: core.ProjectInput{}, Out: store.Project{}},
	"GET /projects/{p}/members":                    {Tag: "projects", Summary: "Members (guests have an end date).", Out: []store.Member{}},
	"PUT /projects/{p}/members":                    {Tag: "projects", Summary: "Add a member or guest (until = end date).", In: memberIn{}},
	"DELETE /projects/{p}/members/{user}":          {Tag: "projects", Summary: "Remove a member.", Out: ok{}},
	"GET /projects/{p}/groups":                     {Tag: "projects", Summary: "Groups with access.", Out: []store.ProjectGroup{}},
	"PUT /projects/{p}/groups/{g}":                 {Tag: "projects", Summary: "Give a group a role.", In: roleIn{}, Out: ok{}},
	"DELETE /projects/{p}/groups/{g}":              {Tag: "projects", Summary: "Remove a group's access.", Out: ok{}},
	"GET /projects/{p}/storage":                    {Tag: "projects", Summary: "Storage use and quota.", Out: core.Storage{}},
	"PUT /projects/{p}/storage":                    {Tag: "projects", Summary: "Set the project's own quota (owners).", In: quotaIn{}, Out: core.Storage{}},
	"GET /projects/{p}/env":                        {Tag: "projects", Summary: "Environment variables for request blocks (secrets hidden).", Out: []store.EnvVar{}},
	"PUT /projects/{p}/env/{name}":                 {Tag: "projects", Summary: "Set a variable or secret."},
	"DELETE /projects/{p}/env/{name}":              {Tag: "projects", Summary: "Delete a variable."},
	"GET /projects/{p}/trash":                      {Tag: "projects", Summary: "Deleted pages, entries and files.", Out: []store.TrashItem{}},
	"POST /projects/{p}/trash/{id}/restore":        {Tag: "projects", Summary: "Restore from the trash.", Out: store.TrashItem{}},
	"DELETE /projects/{p}/trash/{id}":              {Tag: "projects", Summary: "Delete for good (owners)."},
	"GET /projects/{p}/favorites":                  {Tag: "projects", Summary: "Project and personal favorites.", Out: []store.Favorite{}},
	"PUT /projects/{p}/favorites":                  {Tag: "projects", Summary: "Reorder favorites.", Query: []string{"scope"}},
	"PUT /projects/{p}/favorites/{d}":              {Tag: "projects", Summary: "Add a favorite.", Query: []string{"scope"}},
	"DELETE /projects/{p}/favorites/{d}":           {Tag: "projects", Summary: "Remove a favorite.", Query: []string{"scope"}},
	"GET /projects/{p}/suggestions":                {Tag: "projects", Summary: "AI suggestions waiting for a decision.", Out: []store.Suggestion{}, Query: []string{"status"}},
	"POST /suggestions/{id}/accept":                {Tag: "projects", Summary: "Accept (apply) a suggestion."},
	"POST /suggestions/{id}/reject":                {Tag: "projects", Summary: "Reject a suggestion."},
	"GET /projects/{p}/share-links":                {Tag: "sharing", Summary: "Active read-only links.", Out: []store.ShareLink{}},
	"POST /projects/{p}/share-links":               {Tag: "sharing", Summary: "Create a read-only link (project or one page); the token is shown once.", In: core.ShareLinkInput{}, Out: core.NewShareLink{}},
	"DELETE /projects/{p}/share-links/{id}":        {Tag: "sharing", Summary: "Revoke a link.", Out: ok{}},
	"GET /shared/{token}":                          {Tag: "sharing", Summary: "Open a share link (no account).", Out: core.SharedView{}, Public: true},
	"GET /shared/{token}/documents/{d}":            {Tag: "sharing", Summary: "A page of a share link.", Out: store.Document{}, Public: true},
	"GET /shared/{token}/files/{id}":               {Tag: "sharing", Summary: "A file of a shared page.", Public: true, Query: []string{"preview"}},
	"GET /shared/{token}/files/{id}/{name}":        {Tag: "sharing", Summary: "A file of a shared page (with file name).", Public: true, Query: []string{"preview"}},
	"GET /admin/plugins":                           {Tag: "plugins", Summary: "Installed plugins with permissions, license and problems.", Out: pluginList{}},
	"POST /admin/plugins":                          {Tag: "plugins", Summary: "Install or update a plugin package (.phxp body; signature in the Phoedox-Signature header).", Raw: true, Out: core.PluginInfo{}},
	"GET /admin/plugins/catalog":                   {Tag: "plugins", Summary: "Plugins the store offers for this server.", Out: []core.CatalogEntry{}},
	"POST /admin/plugins/install":                  {Tag: "plugins", Summary: "Install a plugin version from the store.", In: storeInstallIn{}, Out: core.PluginInfo{}},
	"PATCH /admin/plugins/{id}":                    {Tag: "plugins", Summary: "Switch a plugin on or off for the instance.", In: enabledIn{}, Out: core.PluginInfo{}},
	"DELETE /admin/plugins/{id}":                   {Tag: "plugins", Summary: "Uninstall a plugin with its data.", Out: ok{}},
	"GET /admin/plugins/{id}/logs":                 {Tag: "plugins", Summary: "Recent log lines of a plugin.", Out: []plugins.LogLine{}},
	"PUT /admin/plugins/{id}/license":              {Tag: "plugins", Summary: "Store a signed license file.", Raw: true, Out: core.PluginInfo{}},
	"POST /admin/plugins/{id}/license/redeem":      {Tag: "plugins", Summary: "Redeem a license code at the store.", In: licenseCodeIn{}, Out: core.PluginInfo{}},
	"GET /plugins/{id}/{v}/files/*":                {Tag: "plugins", Summary: "A frontend file of an enabled plugin (cacheable, the version is in the path)."},
	"GET /projects/{p}/plugins":                    {Tag: "plugins", Summary: "Plugins of a project (owners see all installed ones with settings).", Out: []core.ProjectPluginInfo{}},
	"PUT /projects/{p}/plugins/{id}":               {Tag: "plugins", Summary: "Enable or disable a plugin for the project (owners).", In: enabledIn{}, Out: ok{}},
	"PUT /projects/{p}/plugins/{id}/secrets":       {Tag: "plugins", Summary: "Set secrets the plugin asks for; empty values remove them. Values are never returned.", In: map[string]string{}, Out: ok{}},
	"POST /projects/{p}/plugins/{id}/tools/{tool}": {Tag: "plugins", Summary: "Run a tool of a plugin with the caller's rights (JSON arguments as body).", Raw: true},
	"GET /projects/{p}/webhooks":                   {Tag: "webhooks", Summary: "Webhooks and the event types they can receive.", Out: webhookList{}},
	"POST /projects/{p}/webhooks":                  {Tag: "webhooks", Summary: "Add a webhook; the signing secret is shown once.", In: core.WebhookInput{}, Out: core.NewWebhook{}},
	"PATCH /projects/{p}/webhooks/{id}":            {Tag: "webhooks", Summary: "Change or switch a webhook on/off.", In: core.WebhookInput{}, Out: store.Webhook{}},
	"DELETE /projects/{p}/webhooks/{id}":           {Tag: "webhooks", Summary: "Delete a webhook.", Out: ok{}},
	"GET /projects/{p}/webhooks/{id}/deliveries":   {Tag: "webhooks", Summary: "The latest deliveries.", Out: []store.Delivery{}},
	"POST /projects/{p}/webhooks/{id}/test":        {Tag: "webhooks", Summary: "Send a ping now.", Out: store.Delivery{}},

	"GET /projects/{p}/documents":                            {Tag: "documents", Summary: "Pages, decisions and links (without content).", Out: []store.Document{}, Query: []string{"type", "category"}},
	"POST /projects/{p}/documents":                           {Tag: "documents", Summary: "Create a page, decision (ADR) or link.", In: core.DocumentInput{}, Out: core.DocResult{}},
	"GET /projects/{p}/documents/{d}":                        {Tag: "documents", Summary: "A document with content (slug, id or ADR-0001); lang picks a translation in multilingual projects.", Out: store.Document{}, Query: []string{"lang"}},
	"GET /projects/{p}/documents/{d}/translations/{lang}":    {Tag: "documents", Summary: "One translation with its text and whether it is outdated.", Out: core.TranslationInfo{}},
	"PUT /projects/{p}/documents/{d}/translations/{lang}":    {Tag: "documents", Summary: "Create or replace a translation; it refers to the current main version.", In: core.TranslationInput{}, Out: core.TranslationInfo{}},
	"DELETE /projects/{p}/documents/{d}/translations/{lang}": {Tag: "documents", Summary: "Delete a translation."},
	"PATCH /projects/{p}/documents/{d}":                      {Tag: "documents", Summary: "Change a document (new version).", In: core.DocumentPatch{}, Out: core.DocResult{}},
	"DELETE /projects/{p}/documents/{d}":                     {Tag: "documents", Summary: "Move to the trash.", Out: core.DocResult{}},
	"GET /projects/{p}/drafts":                               {Tag: "documents", Summary: "Open drafts of the project (editors; others get an empty list).", Out: []core.DraftInfo{}},
	"GET /projects/{p}/documents/{d}/draft":                  {Tag: "documents", Summary: "The open draft of a page (editors).", Out: core.DraftInfo{}},
	"POST /projects/{p}/documents/{d}/draft":                 {Tag: "documents", Summary: "Start or join the draft of a Markdown page (edit session).", Out: core.DraftInfo{}},
	"PUT /projects/{p}/documents/{d}/draft":                  {Tag: "documents", Summary: "Save the draft (Markdown, title, category); the published page stays unchanged.", In: core.DraftPatch{}, Out: core.DraftInfo{}},
	"DELETE /projects/{p}/documents/{d}/draft":               {Tag: "documents", Summary: "Cancel the draft: discards the changes of all participants and ends the session.", Out: ok{}},
	"POST /projects/{p}/documents/{d}/draft/publish":         {Tag: "documents", Summary: "Publish the draft as a new version and end the session for everyone.", Out: core.DocResult{}},
	"GET /projects/{p}/documents/{d}/versions":               {Tag: "documents", Summary: "Version history.", Out: []core.VersionInfo{}},
	"GET /projects/{p}/documents/{d}/versions/{n}":           {Tag: "documents", Summary: "An older version.", Out: core.VersionInfo{}},
	"POST /projects/{p}/documents/{d}/versions/{n}/restore":  {Tag: "documents", Summary: "Restore an older version (as a new one).", Out: core.DocResult{}},
	"POST /projects/{p}/documents/{d}/requests/{n}/run":      {Tag: "documents", Summary: "Run a request block of the page.", Out: requests.Result{}},
	"GET /projects/{p}/documents/{d}/sharing":                {Tag: "sharing", Summary: "Restriction and people/groups a page is shared with (owners).", Out: core.DocSharing{}},
	"PUT /projects/{p}/documents/{d}/sharing":                {Tag: "sharing", Summary: "Restrict a page to owners and shares.", In: restrictIn{}, Out: core.DocSharing{}},
	"POST /projects/{p}/documents/{d}/sharing/grants":        {Tag: "sharing", Summary: "Share a page with a person or group.", In: core.GrantInput{}, Out: core.DocSharing{}},
	"DELETE /projects/{p}/documents/{d}/sharing/grants/{id}": {Tag: "sharing", Summary: "Stop sharing.", Out: core.DocSharing{}},
	"GET /projects/{p}/entities/{e}/tree":                    {Tag: "graph", Summary: "Software tree of a host: software, VMs, stacks and containers that are installed on, run on or are part of it.", Out: core.TreeNode{}},
	"GET /projects/{p}/entities/{e}/diagnose":                {Tag: "graph", Summary: "Network path of a device or of the devices in a room, with a status per station.", Out: core.Diagnosis{}},
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
