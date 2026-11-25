package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/publish"
	"github.com/phoedox/phoedox/internal/store"
)

type projectRef struct {
	Project string `json:"project" jsonschema:"project slug"`
}

type docRef struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Document string `json:"document" jsonschema:"document slug, id or decision number like ADR-0003"`
}

type listTasksIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Board    string `json:"board,omitempty" jsonschema:"board slug or id; empty = all boards"`
	Column   string `json:"column,omitempty" jsonschema:"column id or name (needs board)"`
	Assignee string `json:"assignee,omitempty" jsonschema:"me, a user id, an e-mail or a free-text assignee such as Claude"`
	Open     bool   `json:"open,omitempty" jsonschema:"only tasks that are not done"`
}

type createTaskIn struct {
	Project     string   `json:"project" jsonschema:"project slug"`
	Board       string   `json:"board" jsonschema:"board slug or id (a document of type board)"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty" jsonschema:"Markdown; @mentions allowed"`
	Column      string   `json:"column,omitempty" jsonschema:"column id or name; default: first column"`
	Assignee    string   `json:"assignee,omitempty" jsonschema:"user id or e-mail of a project member, or free text for anyone else (e.g. Claude); empty = nobody"`
	Due         string   `json:"due,omitempty" jsonschema:"YYYY-MM-DD"`
	Priority    int      `json:"priority,omitempty" jsonschema:"0 none, 1 low, 2 medium, 3 high"`
	Labels      []string `json:"labels,omitempty"`
	Documents   []string `json:"documents,omitempty" jsonschema:"pages, decisions or other documents to link to the card: slugs, ids or decision numbers like ADR-0003"`
	Subtasks    []string `json:"subtasks,omitempty" jsonschema:"titles of the card's subtasks (a checklist); change them later with update_task"`
}

type linkCodeIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Task    string `json:"task" jsonschema:"task id"`
	core.CodeLinkInput
}

type searchCommitsIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Repo    string `json:"repo" jsonschema:"repository id or owner/name (see list_code_repos)"`
	Query   string `json:"query,omitempty" jsonschema:"a commit SHA or words of the commit message; empty lists the latest commits"`
}

type updateTaskIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Task    string `json:"task" jsonschema:"task id"`
	core.TaskPatch
}

type saveFloorplanIn struct {
	Project   string         `json:"project" jsonschema:"project slug"`
	Document  string         `json:"document,omitempty" jsonschema:"slug of an existing floor plan to replace; empty creates a new one"`
	Title     string         `json:"title,omitempty" jsonschema:"title of a new floor plan, e.g. Ground floor"`
	Category  string         `json:"category,omitempty"`
	Floorplan core.Floorplan `json:"floorplan" jsonschema:"the whole plan in millimetres: width, height, rooms (id, name, entity slug of a room entry, points [[x,y],…] clockwise, color gray|red|orange|yellow|green|blue|purple|pink), devices (id, entity slug, x, y), floor (slug of a floor entry). Saving links devices located_in the room they stand in."`
}

type wbDocIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Document string `json:"document" jsonschema:"whiteboard slug or id"`
}

type editWhiteboardIn struct {
	Project  string      `json:"project" jsonschema:"project slug"`
	Document string      `json:"document" jsonschema:"whiteboard slug or id"`
	Ops      []core.WBOp `json:"ops" jsonschema:"operations in order: {op: add, element: {...}} | {op: update, id, element: {changed fields, null removes}} | {op: delete, id}"`
}

type drawDiagramIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Document string `json:"document,omitempty" jsonschema:"whiteboard to draw on (slug); empty creates a new whiteboard"`
	Title    string `json:"title,omitempty" jsonschema:"title of a new whiteboard"`
	Category string `json:"category,omitempty"`
	Mermaid  string `json:"mermaid,omitempty" jsonschema:"a Mermaid flowchart (graph/flowchart) to turn into shapes instead of nodes and edges"`
	core.DiagramInput
}

type drawScreensIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Document string `json:"document,omitempty" jsonschema:"whiteboard to draw on (slug); empty creates a new whiteboard"`
	Title    string `json:"title,omitempty" jsonschema:"title of a new whiteboard"`
	Category string `json:"category,omitempty"`
	core.ScreensInput
}

type replyCommentIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Document string `json:"document" jsonschema:"whiteboard slug or id"`
	Comment  string `json:"comment" jsonschema:"comment id from get_whiteboard"`
	Text     string `json:"text"`
}

type saveWhiteboardIn struct {
	Project    string          `json:"project" jsonschema:"project slug"`
	Document   string          `json:"document,omitempty" jsonschema:"slug of an existing whiteboard to replace; empty creates a new one"`
	Title      string          `json:"title,omitempty" jsonschema:"title of a new whiteboard"`
	Category   string          `json:"category,omitempty"`
	Whiteboard core.Whiteboard `json:"whiteboard" jsonschema:"the whole board: elements (see edit_whiteboard) and comments"`
}

type createAppIn struct {
	Project   string            `json:"project" jsonschema:"project slug"`
	Title     string            `json:"title"`
	Category  string            `json:"category,omitempty"`
	Files     map[string]string `json:"files,omitempty" jsonschema:"file name → source: index.html, or app.tsx/app.jsx (React, compiled on save) with an index.html made around it; plus .css, .js, .ts, .json, .svg. At most 40 files, 1 MB"`
	Level     int               `json:"level,omitempty" jsonschema:"bridge level the app asks for: 0 none, 1 read its sources, 2 own storage, 3 propose changes the person confirms"`
	Sources   []core.AppSource  `json:"sources,omitempty" jsonschema:"what the app may read at level 1: {id, kind query|board|checks|entities|request|readings, ref (saved query, board, kind, page#n for the n-th request block of a page (GET only), or entry[:metric] for readings), label}"`
	StorageMB int               `json:"storageMb,omitempty" jsonschema:"limit of the app's own storage in MB (default 10)"`
	AI        bool              `json:"ai,omitempty" jsonschema:"the app may call phoedox.ai(prompt, {json}) on the viewer's AI budget"`
	Devices   []string          `json:"devices,omitempty" jsonschema:"device features the app may ask for: camera, microphone, screen, location, motion; each person allows each one separately"`
	SelfEdit  bool              `json:"self_edit,omitempty" jsonschema:"the app may save its own files with phoedox.self.save (people who may edit it confirm each save)"`
	Starter   string            `json:"starter,omitempty" jsonschema:"start from a built-in kind: slides (Markdown deck in slides.md, present, print to PDF), dashboard (cards for each source), sheet (shared spreadsheet with formulas), design (the project's theme as a design system); files given replace the kind's files"`
}

type editAppIn struct {
	Project  string             `json:"project" jsonschema:"project slug"`
	Document string             `json:"document" jsonschema:"app slug or id"`
	Edits    []core.AppEdit     `json:"edits,omitempty" jsonschema:"exact replacements {file, old, new}; old must occur exactly once in the file"`
	Files    map[string]*string `json:"files,omitempty" jsonschema:"whole files to add or replace; null removes a file"`
	Manifest *core.App          `json:"manifest,omitempty" jsonschema:"new level, sources, storageMb, ai, devices, selfEdit (files here are ignored)"`
}

type appAssetIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Document string `json:"document" jsonschema:"app slug or id"`
	Name     string `json:"name" jsonschema:"file name, e.g. logo.png or data.json; the app loads it as assets/<name>; the same name replaces the asset"`
	Data     string `json:"data" jsonschema:"content as base64 (or a data: URL), at most 25 MB"`
}

type copyAppAssetsIn struct {
	Project     string   `json:"project" jsonschema:"project of the app that receives the assets"`
	Document    string   `json:"document" jsonschema:"app slug or id that receives them"`
	FromProject string   `json:"from_project,omitempty" jsonschema:"project of the source app (default: the same project)"`
	From        string   `json:"from" jsonschema:"source app slug or id"`
	Names       []string `json:"names,omitempty" jsonschema:"asset names to copy; empty copies all"`
}

type appScreenshotIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Document string `json:"document" jsonschema:"app slug or id"`
}

type whiteboardScreenshotIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Document string `json:"document" jsonschema:"whiteboard slug or id"`
	Frame    string `json:"frame,omitempty" jsonschema:"only this frame or prototype screen: its id or title (from get_whiteboard)"`
	Dark     bool   `json:"dark,omitempty" jsonschema:"draw in the dark theme"`
}

type waitChangesIn struct {
	Project   string   `json:"project" jsonschema:"project slug"`
	Documents []string `json:"documents,omitempty" jsonschema:"only changes of these pages or apps (slugs); empty = the whole project"`
	Cursor    int64    `json:"cursor,omitempty" jsonschema:"cursor from the previous answer; without it the tool returns the current cursor at once"`
	Wait      int      `json:"wait,omitempty" jsonschema:"seconds to wait for the first change, at most 50 (default 0: answer at once)"`
}

type createBoardIn struct {
	Project     string             `json:"project" jsonschema:"project slug"`
	Title       string             `json:"title"`
	Category    string             `json:"category,omitempty"`
	Description string             `json:"description,omitempty" jsonschema:"Markdown shown above the board"`
	Columns     []core.BoardColumn `json:"columns,omitempty" jsonschema:"columns in order; mark the last one done=true. Default: To do, In progress, Done"`
}

type getDocIn struct {
	docRef
	Lang    string `json:"lang,omitempty" jsonschema:"language of a multilingual project (e.g. de); empty = main language"`
	Heading string `json:"heading,omitempty" jsonschema:"read only one section of a page: a heading's text or its number (e.g. 6.4 or Backups); the section runs to the next heading of the same or a higher level. Unknown headings return the page's outline"`
}

type translateIn struct {
	docRef
	Lang         string  `json:"lang" jsonschema:"one of the project's further languages (see get_project: locales)"`
	Title        string  `json:"title,omitempty" jsonschema:"translated title; may be left out when the translation exists (keeps its title)"`
	Markdown     *string `json:"markdown,omitempty" jsonschema:"pages: the whole translated Markdown; keep @mentions, links, code and {{variables}} unchanged. Left out = the existing translated text stays; only an empty string clears it"`
	Context      *string `json:"context,omitempty" jsonschema:"decisions: translated context"`
	Decision     *string `json:"decision,omitempty" jsonschema:"decisions: translated decision"`
	Consequences *string `json:"consequences,omitempty"`
	Alternatives *string `json:"alternatives,omitempty"`
	Description  *string `json:"description,omitempty" jsonschema:"links: translated description"`
	// SupersedesScope and Original as in translationIn.
	SupersedesScope *string         `json:"supersedes_scope,omitempty" jsonschema:"decisions that replace part of an older one: the translated scope"`
	Original        bool            `json:"original,omitempty" jsonschema:"true only when this language is the user's own original text (e.g. importing their German originals), not a translation you made; otherwise it is shown as machine translation until a person reviews it"`
	Edits           []core.TextEdit `json:"edits,omitempty" jsonschema:"change exact passages of the existing translation instead of sending it whole: {old, new} where old occurs exactly once (field for decisions: context, decision, consequences, alternatives); all or nothing"`
	Verbose         bool            `json:"verbose,omitempty" jsonschema:"true returns the whole translation; default: only its state"`
}

// translationIn is one language of a document created together with it.
type translationIn struct {
	Title           string  `json:"title"`
	Markdown        *string `json:"markdown,omitempty" jsonschema:"pages: the translated Markdown"`
	Context         *string `json:"context,omitempty"`
	Decision        *string `json:"decision,omitempty"`
	Consequences    *string `json:"consequences,omitempty"`
	Alternatives    *string `json:"alternatives,omitempty"`
	SupersedesScope *string `json:"supersedes_scope,omitempty"`
	Original        bool    `json:"original,omitempty" jsonschema:"true only when this text is the user's own original in this language, not your translation"`
}

func (t translationIn) input() core.TranslationInput {
	return core.TranslationInput{Title: t.Title, Markdown: t.Markdown, Context: t.Context, Decision: t.Decision, Consequences: t.Consequences,
		Alternatives: t.Alternatives, SupersedesScope: t.SupersedesScope, Original: t.Original}
}

type versionRef struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Document string `json:"document" jsonschema:"document slug, id or decision number like ADR-0003"`
	Version  int    `json:"version" jsonschema:"version number from list_document_versions"`
}

type runRequestIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Document string `json:"document" jsonschema:"slug of the Markdown page with the request block"`
	Index    int    `json:"index" jsonschema:"0-based number of the request block in the page"`
}

type addCommentIn struct {
	Project  string   `json:"project" jsonschema:"project slug"`
	Document string   `json:"document" jsonschema:"page slug"`
	Body     string   `json:"body" jsonschema:"the comment (plain text)"`
	Anchor   string   `json:"anchor,omitempty" jsonschema:"quote of the block the comment is about"`
	Parent   string   `json:"parent,omitempty" jsonschema:"id of the thread to answer"`
	Mentions []string `json:"mentions,omitempty" jsonschema:"e-mails of people to notify"`
}

type maintenanceListIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Entity  string `json:"entity,omitempty" jsonschema:"entry slug; empty = all plans of the project"`
}

type maintenanceSaveIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	ID      string `json:"id,omitempty" jsonschema:"plan id to change; empty creates a plan"`
	core.MaintenanceInput
}

type runQueryIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Query   string `json:"query" jsonschema:"slug or id of a saved query (see list_saved_queries)"`
}

type queryEntitiesIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	core.GraphQuery
}

type calendarIn struct {
	Project   string   `json:"project" jsonschema:"project slug"`
	From      string   `json:"from,omitempty" jsonschema:"first day (YYYY-MM-DD); default: the start of this month minus a week"`
	To        string   `json:"to,omitempty" jsonschema:"day after the last (YYYY-MM-DD); default: about six weeks after from; at most 400 days"`
	Document  string   `json:"document,omitempty" jsonschema:"a calendar document whose filters to use"`
	Sources   []string `json:"sources,omitempty" jsonschema:"maintenance, tasks, entities, external; empty = all"`
	Kinds     []string `json:"kinds,omitempty" jsonschema:"entry kinds for date fields and maintenance; empty = all"`
	Calendars []string `json:"calendars,omitempty" jsonschema:"ids of external calendars; empty = all"`
}

type createCalendarIn struct {
	Project  string            `json:"project" jsonschema:"project slug"`
	Title    string            `json:"title"`
	Category string            `json:"category,omitempty" jsonschema:"sidebar group; reuse existing categories"`
	Calendar core.CalendarSpec `json:"calendar" jsonschema:"view (month or list), sources, kinds, calendars (external calendar ids), days (list view)"`
}

type projectOnlyIn struct {
	Project string `json:"project" jsonschema:"project slug"`
}

type gapsIn struct {
	Project   string `json:"project" jsonschema:"project slug"`
	StaleDays int    `json:"staleDays,omitempty" jsonschema:"pages unchanged for this many days count as stale (default 365)"`
}

type exportIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Format  string `json:"format,omitempty" jsonschema:"zip (default): Markdown with front matter plus JSON, importable; git: a Git repository with one commit per document version"`
}

type changesIn struct {
	Project     string `json:"project" jsonschema:"project slug"`
	Days        int    `json:"days,omitempty" jsonschema:"look back this many days (default 7, at most 90)"`
	PullRequest string `json:"pull_request,omitempty" jsonschema:"instead: the documentation changes tied to this pull request or branch (owner/repo#12, #12, URL, owner/repo@branch) with a line diff each"`
}

type readingsIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Entity  string `json:"entity" jsonschema:"entry slug"`
	Metric  string `json:"metric,omitempty" jsonschema:"one metric; empty = all"`
	Hours   int    `json:"hours,omitempty" jsonschema:"time span in hours (default 24, at most 2160)"`
}

type skillsIn struct {
	Project string `json:"project,omitempty" jsonschema:"project slug; empty = all your projects"`
}

type skillIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Name    string `json:"name" jsonschema:"skill name from list_skills"`
}

type archiveIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Document string `json:"document" jsonschema:"slug of a link page"`
}

type listChecksIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Document string `json:"document,omitempty" jsonschema:"only checks of this page (slug)"`
}

type createCheckIn struct {
	Project     string `json:"project" jsonschema:"project slug"`
	Document    string `json:"document" jsonschema:"slug of the Markdown page with the request block"`
	Index       int    `json:"index" jsonschema:"0-based number of the request block in the page"`
	Name        string `json:"name,omitempty" jsonschema:"short name, e.g. Nextcloud reachable"`
	IntervalSec int    `json:"intervalSec,omitempty" jsonschema:"seconds between runs, 60 to 86400 (default 300)"`
	Entity      string `json:"entity,omitempty" jsonschema:"slug of an entry whose state follows the check"`
}

type createProjectIn struct {
	Name         string   `json:"name"`
	Slug         string   `json:"slug,omitempty" jsonschema:"optional, derived from name"`
	Description  string   `json:"description,omitempty"`
	PrimaryColor string   `json:"primary_color,omitempty" jsonschema:"hex color like #F26A1B"`
	AIWriteMode  string   `json:"ai_write_mode,omitempty" jsonschema:"suggest (default) or direct"`
	Features     []string `json:"features,omitempty" jsonschema:"optional tabs to switch on: decisions, graph, model, files (default none; pages and boards always exist)"`
	Template     string   `json:"template,omitempty" jsonschema:"optional starting content: home, homelab, it-client or software"`
	Language     string   `json:"language,omitempty" jsonschema:"language of the template content: de or en (default en)"`
}

type updateProjectIn struct {
	Project      string               `json:"project" jsonschema:"project slug"`
	Name         *string              `json:"name,omitempty"`
	Description  *string              `json:"description,omitempty"`
	PrimaryColor *string              `json:"primary_color,omitempty"`
	AIWriteMode  *string              `json:"ai_write_mode,omitempty" jsonschema:"suggest or direct (owner only)"`
	MainLocale   *string              `json:"main_locale,omitempty" jsonschema:"main language of the content, e.g. en (makes the project multilingual)"`
	Locales      *[]string            `json:"locales,omitempty" jsonschema:"further languages with translations, e.g. [de]"`
	Features     *[]string            `json:"features,omitempty" jsonschema:"optional tabs shown in the web app: decisions, graph, model, files (the full list replaces the old one)"`
	AutoLinks    *[]core.AutoLinkRule `json:"auto_links,omitempty" jsonschema:"own auto-link rules (the full list replaces the old one): {pattern (regular expression, e.g. JIRA-\\d+), to url|ref, url (https://… or /…, $0 = match, $1… groups; for to=url), note}. to=ref links the match to the document with that ext ref. Built in, without rules: decision numbers (ADR-0012), ext refs and §6.4 section numbers."`
}

type listDocsIn struct {
	Project     string `json:"project" jsonschema:"project slug"`
	Type        string `json:"type,omitempty" jsonschema:"markdown, adr, link, board or floorplan"`
	Category    string `json:"category,omitempty"`
	Lang        string `json:"lang,omitempty" jsonschema:"multilingual projects: show titles in this translation where one exists"`
	Translation string `json:"translation,omitempty" jsonschema:"multilingual projects: only documents whose translation is outdated, missing or either (stale) in some further language (or in lang)"`
}

type createDocIn struct {
	Project      string                   `json:"project" jsonschema:"project slug"`
	Title        string                   `json:"title"`
	Markdown     string                   `json:"markdown" jsonschema:"page content in Markdown; mention entities with @slug"`
	Category     string                   `json:"category,omitempty" jsonschema:"sidebar group; a / makes sub-folders, e.g. Architecture/Backend. Titles should not repeat the folder name"`
	Slug         string                   `json:"slug,omitempty" jsonschema:"optional, derived from title"`
	Position     int                      `json:"position,omitempty" jsonschema:"order within the category"`
	After        string                   `json:"after,omitempty" jsonschema:"slug or id of a page to insert this one right after (the pages below move down; without category it goes into that page's category)"`
	Translations map[string]translationIn `json:"translations,omitempty" jsonschema:"multilingual projects: language (one of the further locales) → title and markdown, saved together with the page"`
	Verbose      bool                     `json:"verbose,omitempty" jsonschema:"true returns the whole document; default: id, slug, version and url"`
}

type updateDocIn struct {
	Project       string          `json:"project" jsonschema:"project slug"`
	Document      string          `json:"document" jsonschema:"document slug or id"`
	Title         *string         `json:"title,omitempty"`
	Markdown      *string         `json:"markdown,omitempty" jsonschema:"replaces the whole markdown content; for small changes to long pages use edits"`
	Edits         []core.TextEdit `json:"edits,omitempty" jsonschema:"change exact passages instead of sending the whole text: {old, new} pairs applied in order; old must occur exactly once (add context if needed); all or nothing. Decisions: field context, decision, consequences or alternatives. Applies to the open draft if there is one"`
	Category      *string         `json:"category,omitempty"`
	Slug          *string         `json:"slug,omitempty"`
	Position      *int            `json:"position,omitempty"`
	After         string          `json:"after,omitempty" jsonschema:"slug or id of a page in the same category to move this one right after"`
	URL           *string         `json:"url,omitempty" jsonschema:"links only: the new address"`
	Description   *string         `json:"description,omitempty" jsonschema:"links only: the new description"`
	ExpectVersion int             `json:"expect_version,omitempty" jsonschema:"fail if the document changed since this version"`
	Verbose       bool            `json:"verbose,omitempty" jsonschema:"true returns the whole document; default: id, slug, version, url and draft/suggestion state"`
	PullRequest   string          `json:"pull_request,omitempty" jsonschema:"tie this change to a pull request or branch of the project's code connection: owner/repo#12, #12, a pull request URL, or owner/repo@branch. The change goes into a draft that is published when the pull request is merged and discarded when it is closed; the pull request gets a Docs changes comment"`
}

type updateBoardIn struct {
	Project       string             `json:"project" jsonschema:"project slug"`
	Board         string             `json:"board" jsonschema:"board slug or id"`
	Title         *string            `json:"title,omitempty"`
	Category      *string            `json:"category,omitempty"`
	Description   *string            `json:"description,omitempty" jsonschema:"Markdown shown above the board"`
	Columns       []core.BoardColumn `json:"columns,omitempty" jsonschema:"all columns in order (replaces them); keep the ids of existing columns; a column can only be removed when it has no tasks"`
	ExpectVersion int                `json:"expect_version,omitempty"`
}

type decisionFields struct {
	Status       string   `json:"status,omitempty" jsonschema:"proposed, accepted, rejected, deprecated, partially_superseded or superseded (the last two are set automatically by supersedes)"`
	Date         string   `json:"date,omitempty" jsonschema:"YYYY-MM-DD"`
	Deciders     []string `json:"deciders,omitempty"`
	Context      string   `json:"context,omitempty" jsonschema:"situation and forces, Markdown"`
	Decision     string   `json:"decision,omitempty" jsonschema:"what was decided, Markdown"`
	Consequences string   `json:"consequences,omitempty" jsonschema:"Markdown"`
	Alternatives string   `json:"alternatives,omitempty" jsonschema:"considered options, Markdown"`
	Supersedes   string   `json:"supersedes,omitempty" jsonschema:"slug or number (ADR-0003) of the decision this one replaces; it is marked superseded automatically"`
	// Pointer so update_decision can tell "not given" from false.
	SupersedesPartly *bool  `json:"supersedes_partly,omitempty" jsonschema:"true if this decision replaces only part of the older one; the older one is then marked partially_superseded and stays valid otherwise"`
	SupersedesScope  string `json:"supersedes_scope,omitempty" jsonschema:"with supersedes_partly: which part is replaced, e.g. only the choice of database"`
	// Options of a proposed decision; nil means "not given" for update_decision.
	Options []core.ADROption `json:"options,omitempty" jsonschema:"for status proposed: the options people choose from in Phoedox (title, description in Markdown with pros and cons; id is assigned). decision may stay empty then. Replaces all options; responses stay."`
}

func (f decisionFields) adr() core.ADR {
	return core.ADR{Status: f.Status, Date: f.Date, Deciders: f.Deciders, Context: f.Context, Decision: f.Decision,
		Consequences: f.Consequences, Alternatives: f.Alternatives, Supersedes: f.Supersedes,
		SupersedesPartly: f.SupersedesPartly != nil && *f.SupersedesPartly, SupersedesScope: f.SupersedesScope, Options: f.Options}
}

type listDecisionsIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Status  string `json:"status,omitempty" jsonschema:"only decisions with this status: proposed, accepted, rejected, deprecated, partially_superseded or superseded"`
	Query   string `json:"query,omitempty" jsonschema:"only decisions whose number, title or text contain all these words"`
}

type createDecisionIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	decisionItem
}

// decisionItem is one decision to create (create_decision, create_decisions).
type decisionItem struct {
	Title        string                   `json:"title"`
	Category     string                   `json:"category,omitempty" jsonschema:"defaults to the category of the project's other decisions, else Decisions"`
	Slug         string                   `json:"slug,omitempty"`
	ExtRef       string                   `json:"ext_ref,omitempty" jsonschema:"the decision's own number in another system, e.g. D-640 (letters, digits, . _ : # / -). Phoedox still assigns ADR-0001…; links like [[D-640]], search and every tool that takes a document find it by this number too"`
	Translations map[string]translationIn `json:"translations,omitempty" jsonschema:"multilingual projects: language (one of the further locales) → title and translated text, saved together with the decision"`
	decisionFields
}

type createDecisionsIn struct {
	Project   string         `json:"project" jsonschema:"project slug"`
	Decisions []decisionItem `json:"decisions" jsonschema:"up to 50 decisions, created in this order (an item may supersede an earlier one of the same call)"`
}

type updateDecisionIn struct {
	Project       string  `json:"project" jsonschema:"project slug"`
	Document      string  `json:"document" jsonschema:"decision slug, id or number like ADR-0003"`
	Title         *string `json:"title,omitempty"`
	Category      *string `json:"category,omitempty"`
	ExtRef        *string `json:"ext_ref,omitempty" jsonschema:"own number in another system, e.g. D-640; empty removes it"`
	ExpectVersion int     `json:"expect_version,omitempty"`
	PullRequest   string  `json:"pull_request,omitempty" jsonschema:"tie this change to a pull request or branch (owner/repo#12, #12, URL, owner/repo@branch): it stays a draft until the pull request is merged (published) or closed (discarded)"`
	decisionFields
}

type createLinkIn struct {
	Project     string `json:"project" jsonschema:"project slug"`
	Title       string `json:"title"`
	URL         string `json:"url" jsonschema:"http(s) URL of the external documentation"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
}

type searchIn struct {
	Query   string `json:"query" jsonschema:"words to find in titles, content, entity names and attributes"`
	Project string `json:"project,omitempty" jsonschema:"limit to one project slug"`
	Limit   int    `json:"limit,omitempty"`
}

type listEntitiesIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Kind    string `json:"kind,omitempty"`
}

type logTimeIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Task     string `json:"task" jsonschema:"task id"`
	Duration string `json:"duration" jsonschema:"e.g. 1:30, 45m, 1.5h or minutes"`
	Day      string `json:"day,omitempty" jsonschema:"YYYY-MM-DD, default today"`
	Note     string `json:"note,omitempty"`
}

type feedbackListIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Portal   string `json:"portal,omitempty" jsonschema:"portal handle or id (default: all portals the person moderates)"`
	State    string `json:"state,omitempty" jsonschema:"only topics in this state, e.g. new"`
	Category string `json:"category,omitempty"`
	Priority int    `json:"priority,omitempty" jsonschema:"1 urgent … 4 low"`
	Q        string `json:"q,omitempty" jsonschema:"words in title or summary"`
}

type feedbackTopicIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Topic   string `json:"topic" jsonschema:"topic id"`
}

type feedbackUpdateIn struct {
	Project          string    `json:"project" jsonschema:"project slug"`
	Topic            string    `json:"topic" jsonschema:"topic id"`
	Title            *string   `json:"title,omitempty"`
	Summary          *string   `json:"summary,omitempty" jsonschema:"what the reports are about, in the project's main language"`
	Category         *string   `json:"category,omitempty" jsonschema:"one of the portal's categories (never invent new ones)"`
	Tags             *[]string `json:"tags,omitempty"`
	Priority         *int      `json:"priority,omitempty" jsonschema:"1 urgent, 2 high, 3 normal, 4 low"`
	State            *string   `json:"state,omitempty" jsonschema:"one of the portal's states"`
	Answer           *string   `json:"answer,omitempty" jsonschema:"answer shown to reporters and sent to them – only when a person asked for it"`
	Spam             *bool     `json:"spam,omitempty" jsonschema:"false clears the AI's spam flag (only after reading the reports)"`
	DismissDuplicate bool      `json:"dismiss_duplicate,omitempty" jsonschema:"drop the AI's duplicate suggestion (duplicateOf) when the topics differ"`
	FixedIn          *string   `json:"fixed_in,omitempty" jsonschema:"product version that fixed it; reporters are told"`
	Public           *bool     `json:"public,omitempty" jsonschema:"release the topic on the portal's public page (votes, comments, followers) – only when a person asked for it; requires the portal's public view"`
	Entities         *[]string `json:"entities,omitempty" jsonschema:"replaces the linked knowledge-graph entries (slugs) the topic is about"`
	Question         *string   `json:"question,omitempty" jsonschema:"ask the reporters for more information (they answer on their status link); only when a person asked for it, \"\" withdraws it"`
}

type feedbackMergeIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Into    string `json:"into" jsonschema:"topic id that stays"`
	From    string `json:"from" jsonschema:"topic id merged into it and removed"`
}

type feedbackBoardIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Topic   string `json:"topic" jsonschema:"topic id"`
	Board   string `json:"board" jsonschema:"board slug or id"`
	Column  string `json:"column,omitempty" jsonschema:"column id or name (default: first column)"`
	Title   string `json:"title,omitempty" jsonschema:"card title (default: the topic's title)"`
}

type feedbackForwardIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Topic   string `json:"topic" jsonschema:"topic id"`
	Tracker string `json:"tracker" jsonschema:"name or id of one of the project's external trackers (get_feedback_topic lists them under trackers)"`
}

type feedbackSubmitIn struct {
	Portal string         `json:"portal" jsonschema:"portal handle, e.g. starfall (the address /feedback/<handle>)"`
	Type   string         `json:"type,omitempty" jsonschema:"bug, idea, question or praise (one the portal accepts)"`
	Title  string         `json:"title"`
	Body   string         `json:"body,omitempty"`
	Fields map[string]any `json:"fields,omitempty" jsonschema:"the portal's own fields by id (see get_feedback_portal)"`
}

type feedbackPortalIn struct {
	Portal string `json:"portal" jsonschema:"portal handle"`
	Lang   string `json:"lang,omitempty" jsonschema:"en or de"`
}

type timelineIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Entity  string `json:"entity" jsonschema:"entity slug or id"`
	Limit   int    `json:"limit,omitempty" jsonschema:"at most this many items (default 100, max 300)"`
}

type entityRef struct {
	Project string `json:"project" jsonschema:"project slug"`
	Entity  string `json:"entity" jsonschema:"entity slug or id"`
}

type upsertEntityIn struct {
	Project string         `json:"project" jsonschema:"project slug"`
	Kind    string         `json:"kind" jsonschema:"a kind from get_data_model, e.g. service, server, router, room"`
	Name    string         `json:"name"`
	Slug    string         `json:"slug,omitempty" jsonschema:"optional; an existing slug updates that entity"`
	Attrs   map[string]any `json:"attrs,omitempty" jsonschema:"attribute values as described by the kind's schema; replaces all attributes"`
}

type upsertEntitiesIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Entities []struct {
		Kind  string         `json:"kind" jsonschema:"a kind from get_data_model"`
		Name  string         `json:"name"`
		Slug  string         `json:"slug,omitempty" jsonschema:"optional; an existing slug updates that entity"`
		Attrs map[string]any `json:"attrs,omitempty" jsonschema:"replaces all attributes of that entity"`
	} `json:"entities" jsonschema:"up to 200 entities, each like upsert_entity"`
}

// maxMCPBulk keeps one tool call small enough to review as suggestions.
const maxMCPBulk = 200

type listFilesIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Document string `json:"document,omitempty" jsonschema:"only files attached to this document (slug)"`
	Entity   string `json:"entity,omitempty" jsonschema:"only files attached to this entity (slug)"`
}

type uploadImageIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Data     string `json:"data" jsonschema:"the picture as base64 (or a data: URL); PNG, JPEG, GIF or WebP, at most 8 MB"`
	Name     string `json:"name,omitempty" jsonschema:"file name, e.g. rack-front.jpg (the extension is fixed to the real type)"`
	Alt      string `json:"alt,omitempty" jsonschema:"short description for the Markdown image (default: the file name)"`
	Document string `json:"document,omitempty" jsonschema:"attach to this page (slug or ADR number)"`
	Entity   string `json:"entity,omitempty" jsonschema:"attach to this entity (slug)"`
	Task     string `json:"task,omitempty" jsonschema:"task id: the picture is stored with the card's board and added to the end of the card's description"`
}

type projectImageIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Data    string `json:"data" jsonschema:"the picture as base64 (or a data: URL); PNG, JPEG, GIF or WebP; scaled to 512 px"`
}

type defineKindIn struct {
	Project     string          `json:"project" jsonschema:"project slug"`
	ID          string          `json:"id" jsonschema:"kind id, lowercase with _, e.g. solar_inverter; a core kind id extends that kind"`
	Name        string          `json:"name,omitempty" jsonschema:"display name (own kinds only)"`
	Description string          `json:"description,omitempty"`
	Icon        string          `json:"icon,omitempty" jsonschema:"Lucide icon name, see get_data_model icons"`
	Attrs       []core.Attr     `json:"attrs,omitempty" jsonschema:"attributes: name, type (text, number, bool, date, ip, url, select), unit, options (select), required, label"`
	Relations   []core.Relation `json:"relations,omitempty" jsonschema:"allowed outgoing relations: rel and target kinds (* = any)"`
}

type modelKind struct {
	ID          string          `json:"id"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Group       string          `json:"group"`
	Source      string          `json:"source"`
	Count       int             `json:"count"`
	Schema      map[string]any  `json:"schema"`
	Relations   []core.Relation `json:"relations,omitempty"`
}

type linkIn struct {
	Project string         `json:"project" jsonschema:"project slug"`
	From    string         `json:"from" jsonschema:"entity slug or id"`
	Rel     string         `json:"rel" jsonschema:"relation, e.g. depends_on, runs_on, located_in, uplink, documents"`
	To      string         `json:"to" jsonschema:"entity slug or id"`
	Attrs   map[string]any `json:"attrs,omitempty" jsonschema:"attributes of the relation, e.g. for mounted_in: u (lowest height unit, 1 = bottom) and height (units)"`
}

type unlinkIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	From    string `json:"from"`
	Rel     string `json:"rel,omitempty" jsonschema:"empty removes all relations between the two"`
	To      string `json:"to"`
}

type mergeIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Keep    string `json:"keep" jsonschema:"entry that stays (slug)"`
	Remove  string `json:"remove" jsonschema:"entry that is merged into keep and goes to the trash (slug)"`
}

type agentActionIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Entity  string `json:"entity" jsonschema:"entity slug"`
	Action  string `json:"action" jsonschema:"one of the actions list_agent_actions returns"`
}

type entityRefIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Entity  string `json:"entity" jsonschema:"entity slug or id (a device, server or room)"`
}

type traverseIn struct {
	Project   string `json:"project" jsonschema:"project slug"`
	Entity    string `json:"entity" jsonschema:"start entity slug or id"`
	Rel       string `json:"rel,omitempty" jsonschema:"follow only this relation"`
	Direction string `json:"direction,omitempty" jsonschema:"out (default), in or both"`
	Depth     int    `json:"depth,omitempty" jsonschema:"max hops, default and limit 6"`
}

type suggestionsIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Status  string `json:"status,omitempty" jsonschema:"pending, accepted or rejected"`
}

type empty struct{}

// projectOverview is what get_project returns.
type projectOverview struct {
	Project            store.Project           `json:"project"`
	Categories         map[string][]docSummary `json:"documentsByCategory,omitempty"`
	CategoryCounts     map[string]int          `json:"documentCountsByCategory,omitempty"`
	Documents          int                     `json:"documents"`
	EntityKinds        map[string]int          `json:"entityKinds"`
	PendingSuggestions int                     `json:"pendingSuggestions"`
	// PluginBlocks are block kinds the project's plugins add (ADR-0086):
	// write them as fenced blocks like core blocks; the example shows the form.
	PluginBlocks []pluginBlockSummary `json:"pluginBlocks,omitempty"`
	Hint         string               `json:"hint,omitempty"`
}

type pluginBlockSummary struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Plugin  string `json:"plugin"`
	Example string `json:"example,omitempty"`
	// Missing: a kind that moved into this plugin, which is not active in the
	// project (ADR-0088); such blocks keep their source and show a note.
	Missing bool `json:"missing,omitempty"`
}

type docSummary struct {
	Ref     string `json:"ref,omitempty"` // ADR-0003 for decisions
	ExtRef  string `json:"extRef,omitempty"`
	Slug    string `json:"slug"`
	Title   string `json:"title"`
	Type    string `json:"type"`
	Version int    `json:"version"`
}

// docView is a document with decoded content for AI clients.
type docView struct {
	store.Document
	Ref     string `json:"ref,omitempty"`
	Content any    `json:"content"`
	// Review is the page's review interval and next review date, if set.
	Review *reviewView `json:"review,omitempty"`
	// Glossary: the project's terms in this document (when reading for a
	// translation); GlossaryProblems: deviations from glossary and style.
	Glossary         []core.GlossaryEntry   `json:"glossary,omitempty"`
	GlossaryProblems []core.GlossaryProblem `json:"glossaryProblems,omitempty"`
}

type glossaryIn struct {
	Project string                `json:"project" jsonschema:"project slug"`
	Entries *[]core.GlossaryEntry `json:"entries,omitempty" jsonschema:"replace the whole glossary (editors): [{term (main language), translations {lang: term}, keep (never translate), avoid {lang or \"\" for the main language: [spellings not to use]}, note}]; omit to read it"`
}

type reviewView struct {
	EveryDays       int    `json:"everyDays,omitempty"`
	LastReviewed    string `json:"lastReviewed,omitempty"`
	Due             string `json:"due,omitempty"`
	Overdue         bool   `json:"overdue,omitempty"`
	RequiredReading bool   `json:"requiredReading,omitempty"`
}

type snapshotIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Name    string `json:"name,omitempty" jsonschema:"snapshot name, e.g. 0.27.4; empty lists all snapshots"`
	Note    string `json:"note,omitempty"`
	At      string `json:"at,omitempty" jsonschema:"create: point in time (RFC 3339), default now"`
	Create  bool   `json:"create,omitempty" jsonschema:"true creates the snapshot name (editors)"`
	Compare string `json:"compare_to,omitempty" jsonschema:"compare name with this snapshot; \"now\" compares with the current state"`
}

type reviewIn struct {
	Project         string `json:"project" jsonschema:"project slug"`
	Document        string `json:"document" jsonschema:"page slug or id"`
	EveryDays       *int   `json:"everyDays,omitempty" jsonschema:"review the page every N days (0 = no review date)"`
	RequiredReading *bool  `json:"requiredReading,omitempty" jsonschema:"members must confirm they read the current version"`
	Reviewed        bool   `json:"reviewed,omitempty" jsonschema:"mark the page as reviewed now (only after you checked that it is still correct)"`
}

func withReview(ctx context.Context, svc *core.Service, a core.Actor, project string, v docView) docView {
	r, err := svc.PageRules(ctx, a, project, v.ID)
	if err != nil || (r.ReviewDays == 0 && !r.RequiredReading) {
		return v
	}
	rv := &reviewView{EveryDays: r.ReviewDays, Due: r.ReviewDue, Overdue: r.Overdue, RequiredReading: r.RequiredReading}
	if !r.ReviewedAt.IsZero() {
		rv.LastReviewed = r.ReviewedAt.Format(time.DateOnly)
	}
	v.Review = rv
	return v
}

func view(d store.Document) docView {
	var c any
	_ = json.Unmarshal(d.Content, &c)
	return docView{Document: d, Ref: decisionRef(d), Content: c}
}

func decisionRef(d store.Document) string {
	if d.Type != store.DocADR || d.Number == 0 {
		return ""
	}
	return core.DecisionRef(d.Number)
}

func registerTools(s *sdk.Server, svc *core.Service) {
	tool(s, "list_projects", "List all projects you can read, with your role.", func(ctx context.Context, a core.Actor, _ empty) (any, error) {
		return svc.Projects(ctx, a)
	})
	tool(s, "get_project", "Overview of a project: settings, documents grouped by category, entity kinds, pending suggestions. Start here. Projects with many documents get only the number of documents per category; list them with list_documents (category) or set documents: full.", func(ctx context.Context, a core.Actor, in getProjectIn) (any, error) {
		p, err := svc.Project(ctx, a, in.Project)
		if err != nil {
			return nil, err
		}
		docs, err := svc.Documents(ctx, a, p.Slug, store.DocFilter{})
		if err != nil {
			return nil, err
		}
		ov := projectOverview{Project: p, EntityKinds: map[string]int{}, Documents: len(docs)}
		// Large projects: counts per category keep the answer small.
		summary := in.Documents == "summary" || in.Documents != "full" && len(docs) > overviewMaxDocs
		if summary {
			ov.CategoryCounts = map[string]int{}
		} else {
			ov.Categories = map[string][]docSummary{}
		}
		for _, d := range docs {
			cat := d.Category
			if cat == "" {
				cat = "(no category)"
			}
			if summary {
				ov.CategoryCounts[cat]++
				continue
			}
			ov.Categories[cat] = append(ov.Categories[cat], docSummary{Ref: decisionRef(d), ExtRef: d.ExtRef, Slug: d.Slug, Title: d.Title, Type: d.Type, Version: d.Version})
		}
		ents, err := svc.Entities(ctx, a, p.Slug, "")
		if err != nil {
			return nil, err
		}
		for _, e := range ents {
			ov.EntityKinds[e.Kind]++
		}
		if sugs, err := svc.Suggestions(ctx, a, p.Slug, "pending"); err == nil {
			ov.PendingSuggestions = len(sugs)
		}
		if pbs, err := svc.PluginBlocks(ctx, a, p.Slug); err == nil {
			for _, b := range pbs {
				ov.PluginBlocks = append(ov.PluginBlocks, pluginBlockSummary{Kind: b.Kind, Label: b.Label, Plugin: b.Plugin, Example: b.Example, Missing: b.Missing != nil})
			}
		}
		if p.AIWriteMode == "suggest" {
			ov.Hint = "Writes in this project become suggestions that a human must accept."
		}
		if summary {
			ov.Hint = strings.TrimSpace(ov.Hint + " Documents are counted per category only; list them with list_documents (category, type) or list_decisions.")
		}
		return ov, nil
	})
	tool(s, "create_project", "Create a new project. You become its owner.", func(ctx context.Context, a core.Actor, in createProjectIn) (any, error) {
		var features *[]string
		if in.Features != nil || in.Template == "" {
			features = &in.Features
		}
		return svc.CreateProject(ctx, a, core.ProjectInput{Name: in.Name, Slug: in.Slug, Description: in.Description, PrimaryColor: in.PrimaryColor, AIWriteMode: in.AIWriteMode,
			Features: features, Template: in.Template, TemplateLocale: in.Language})
	})
	tool(s, "update_project_settings", "Change name, description, color, AI write mode, languages, optional tabs (features) or own auto-link rules of a project (owner only).", func(ctx context.Context, a core.Actor, in updateProjectIn) (any, error) {
		p, err := svc.Project(ctx, a, in.Project)
		if err != nil {
			return nil, err
		}
		upd := core.ProjectInput{Name: p.Name, Short: p.Short, Description: p.Description, PrimaryColor: p.PrimaryColor, AIWriteMode: p.AIWriteMode}
		setIf(&upd.Name, in.Name)
		setIf(&upd.Description, in.Description)
		setIf(&upd.PrimaryColor, in.PrimaryColor)
		setIf(&upd.AIWriteMode, in.AIWriteMode)
		upd.MainLocale, upd.Locales, upd.Features = in.MainLocale, in.Locales, in.Features
		if in.AutoLinks != nil {
			if _, err := svc.SetAutoLinkRules(ctx, a, p.Slug, *in.AutoLinks); err != nil {
				return nil, err
			}
		}
		return svc.UpdateProject(ctx, a, p.Slug, upd)
	})
	tool(s, "list_documents", "List documents of a project (without content), optionally filtered by type or category. Multilingual projects: translationState per further language (current, outdated, missing); translation: stale lists only those that need work.", func(ctx context.Context, a core.Actor, in listDocsIn) (any, error) {
		docs, err := svc.Documents(ctx, a, in.Project, store.DocFilter{Type: in.Type, Category: in.Category, Locale: in.Lang})
		if err != nil {
			return nil, err
		}
		states, err := svc.TranslationStates(ctx, a, in.Project, docs)
		if err != nil || states == nil {
			return docs, err
		}
		out := []docWithTranslations{}
		for _, d := range docs {
			st := states[d.ID]
			if in.Translation != "" {
				hit := false
				for l, v := range st {
					if in.Lang != "" && l != in.Lang {
						continue
					}
					if v == in.Translation || in.Translation == "stale" && v != "current" {
						hit = true
					}
				}
				if !hit {
					continue
				}
			}
			out = append(out, docWithTranslations{Document: d, TranslationState: st})
		}
		return out, nil
	})
	tool(s, "get_document", "Read a document with its full content (the published version); heading reads one section only. If the page has an open draft, it is returned as \"draft\" too – your writes go into it. In multilingual projects, lang picks a translation; \"translation.sourceVersion\" lower than \"version\" means the translation is outdated.", func(ctx context.Context, a core.Actor, in getDocIn) (any, error) {
		d, err := svc.LocalizedDocument(ctx, a, in.Project, in.Document, in.Lang)
		if err != nil {
			return nil, err
		}
		if in.Heading != "" {
			var c struct {
				Markdown string `json:"markdown"`
			}
			_ = json.Unmarshal(d.Content, &c)
			sec, outline, ok := core.Section(c.Markdown, in.Heading)
			out := map[string]any{"id": d.ID, "projectId": d.ProjectID, "type": d.Type, "slug": d.Slug, "title": d.Title, "version": d.Version}
			if ok {
				out["section"] = sec
			} else {
				out["error"] = "no heading matches; pick one from outline"
				out["outline"] = outline
			}
			return out, nil
		}
		v := withReview(ctx, svc, a, in.Project, view(d))
		// Translating: the glossary terms of this document; main language:
		// spellings the style guide asks to avoid.
		if in.Lang != "" {
			if g, err := svc.GlossaryForDocument(ctx, a, in.Project, d.ID); err == nil && len(g) > 0 {
				v.Glossary = g
			}
		}
		if ps, err := svc.GlossaryCheck(ctx, a, in.Project, d.ID, in.Lang); err == nil && len(ps) > 0 {
			v.GlossaryProblems = ps
		}
		if dr, err := svc.Draft(ctx, a, in.Project, d.ID); err == nil {
			draft := map[string]any{
				"title": dr.Title, "category": dr.Category, "markdown": dr.Markdown, "updatedAt": dr.UpdatedAt,
				"participants": dr.Participants, "note": "Unpublished draft. A person publishes it in Phoedox.",
			}
			if d.Type == "floorplan" {
				delete(draft, "markdown")
				draft["floorplan"] = json.RawMessage(dr.Markdown)
			}
			return map[string]any{"document": v, "draft": draft}, nil
		}
		return v, nil
	})
	tool(s, "snapshots", "Documentation snapshots: named points in the project history, e.g. the docs at release 0.27.4. Without name: list them. With name: the documents as they were then, each with its version (read one with get_document_version). create: true names the current point (or at). compare_to: what changed since another snapshot or \"now\". Release tags of a connected repository become snapshots by themselves.", func(ctx context.Context, a core.Actor, in snapshotIn) (any, error) {
		switch {
		case in.Name == "":
			return svc.Snapshots(ctx, a, in.Project)
		case in.Create:
			return svc.CreateSnapshot(ctx, a, in.Project, core.SnapshotInput{Name: in.Name, Note: in.Note, At: in.At})
		case in.Compare != "":
			to := in.Compare
			if to == "now" {
				to = ""
			}
			return svc.CompareSnapshots(ctx, a, in.Project, in.Name, to)
		}
		return svc.Snapshot(ctx, a, in.Project, in.Name)
	})
	tool(s, "set_page_review", "Set a page's review interval (everyDays, 0 = none) and required reading, or mark it as reviewed now (reviewed: true – only after checking that the page is still correct). get_document shows review.due; find_documentation_gaps lists pages whose review is due (review_due).", func(ctx context.Context, a core.Actor, in reviewIn) (any, error) {
		if in.EveryDays != nil || in.RequiredReading != nil {
			if _, err := svc.SetPageRules(ctx, a, in.Project, in.Document, core.PageRulesInput{ReviewDays: in.EveryDays, RequiredReading: in.RequiredReading}); err != nil {
				return nil, err
			}
		}
		if in.Reviewed {
			return svc.MarkReviewed(ctx, a, in.Project, in.Document)
		}
		return svc.PageRules(ctx, a, in.Project, in.Document)
	})
	tool(s, "translate_document", "Write the translation of a document into one of the project's further languages (multilingual projects, see get_project). Read the main language with get_document first. Fields you leave out keep their translated text (only an empty string clears one); edits change exact passages. With text or edits the translation counts as current; a title-only change keeps the text and is current only if the main text did not change since (the answer says outdated). Machine translation until a person reviews it. Number, status, slug and category stay shared.", func(ctx context.Context, a core.Actor, in translateIn) (any, error) {
		t, err := svc.SaveTranslation(ctx, a, in.Project, in.Document, in.Lang, core.TranslationInput{Title: in.Title, Markdown: in.Markdown,
			Context: in.Context, Decision: in.Decision, Consequences: in.Consequences, Alternatives: in.Alternatives, Description: in.Description,
			SupersedesScope: in.SupersedesScope, Original: in.Original, Edits: in.Edits})
		if err != nil {
			return t, err
		}
		problems, _ := svc.GlossaryCheck(ctx, a, in.Project, in.Document, in.Lang)
		if !in.Verbose {
			t.Content = nil
		}
		if len(problems) > 0 {
			return map[string]any{"translation": t, "glossaryProblems": problems, "note": "Fix these with translate_document edits: use the glossary terms (expected) or leave kept terms untranslated."}, nil
		}
		return t, nil
	})
	tool(s, "glossary", "Read or replace the project's glossary and style guide: terms in the main language with their translation per language, terms to keep untranslated, and spellings to avoid (e.g. email → e-mail). Translations must use these terms: get_document with lang returns the terms of the page, translate_document reports deviations (glossaryProblems), and get_document marks spellings to avoid.", func(ctx context.Context, a core.Actor, in glossaryIn) (any, error) {
		if in.Entries != nil {
			return svc.SetGlossary(ctx, a, in.Project, *in.Entries)
		}
		return svc.Glossary(ctx, a, in.Project)
	})
	tool(s, "validate_blocks", "Check the mermaid, dbmodel, chart, beforeafter, annotate, tree, rack and floorplan blocks and the blocks of the project's plugins (e.g. gantt, matrix, calc, mindmap, poll, quiz) of a page (its open draft if any) or of Markdown you are about to save: unknown diagram types, Mermaid keywords used as names, unclosed blocks, unbalanced brackets or quotes, dbmodel tables that are not defined anywhere in the project, chart JSON that does not parse or does not fit (y not a number, unknown colour, dates the axis cannot read), images that are not project files or https addresses, markers outside 0–100, trees, racks and floor plan excerpts naming entries, floor plans or rooms the project does not have. The Mermaid check catches common mistakes, not everything the renderer rejects. create_document and update_document return the same findings as blockProblems.", func(ctx context.Context, a core.Actor, in validateBlocksIn) (any, error) {
		if in.Document == "" && in.Markdown == nil {
			return nil, errors.New("give document or markdown")
		}
		ps, err := svc.ValidateBlocks(ctx, a, in.Project, in.Document, in.Markdown)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": len(ps) == 0, "problems": ps}, nil
	})
	tool(s, "find_in_documents", "Find exact text or a regular expression in one, several or all documents of a project: page text (the open draft if any), decision texts, link descriptions, optionally titles; the main language or one translation (lang). Every match comes with its line and surrounding text. Unlike search (words, ranked), this finds every literal occurrence.", func(ctx context.Context, a core.Actor, in findIn) (any, error) {
		return svc.FindInDocuments(ctx, a, in.Project, in.input(nil, false))
	})
	tool(s, "replace_in_documents", "Replace text across one, several or all documents. Run it with apply false first: the preview shows every match with its replacement and number; then apply with only (document id → match numbers) to leave matches out, and versions from the preview so documents changed since are skipped. Regex replacements may use $1. Writes are normal versions: in projects where AI writes become suggestions, each document becomes a suggestion; open drafts receive the change.", func(ctx context.Context, a core.Actor, in replaceIn) (any, error) {
		in2 := in.findIn.input(&in.Replace, in.Apply)
		in2.Only, in2.Versions = in.Only, in.Versions
		return svc.FindInDocuments(ctx, a, in.Project, in2)
	})
	tool(s, "batch_update", "Several changes in one call: update (title, markdown or edits, category, position/after of the main language) and translate (title, text or edits of one language) operations, up to 100, applied in order. Every operation is checked first; if one is invalid, nothing is written. Answers are short (id, version, translation state).", func(ctx context.Context, a core.Actor, in batchIn) (any, error) {
		return batchUpdate(ctx, svc, a, in)
	})
	tool(s, "translate_category", "Name a folder (category, e.g. Architecture/Backend) in one of the project's further languages, so the sidebar shows it in that language; the name is the last part only (Backend → Hintergrund). An empty name removes it. Only where AI writes directly.", func(ctx context.Context, a core.Actor, in translateCategoryIn) (any, error) {
		p, err := svc.SetCategoryName(ctx, a, in.Project, in.Category, in.Lang, in.Name)
		if err != nil {
			return nil, err
		}
		return map[string]any{"project": p.Slug, "categoryNames": p.CategoryNames}, nil
	})
	tool(s, "list_tasks", "List tasks (cards on boards) of a project, optionally of one board, column or assignee. Boards are documents of type board; their columns are in the board's content.", func(ctx context.Context, a core.Actor, in listTasksIn) (any, error) {
		return svc.Tasks(ctx, a, in.Project, in.Board, in.Column, in.Assignee, in.Open)
	})
	tool(s, "list_code_repos", "GitHub or Gitea/Forgejo repositories connected to a project; their commits, pull requests and releases can be linked to task cards.", func(ctx context.Context, a core.Actor, in projectRef) (any, error) {
		return svc.CodeRepos(ctx, a, in.Project)
	})
	tool(s, "search_commits", "Find commits of a connected repository by SHA or message, e.g. to link the commit that finished a task.", func(ctx context.Context, a core.Actor, in searchCommitsIn) (any, error) {
		return svc.CodeCommits(ctx, a, in.Project, in.Repo, in.Query)
	})
	tool(s, "link_task_code", "Link a commit, pull request or release of a connected repository to a task card. Phoedox links the first release that contains a linked commit by itself. Only where AI writes directly.", func(ctx context.Context, a core.Actor, in linkCodeIn) (any, error) {
		return svc.LinkTaskCode(ctx, a, in.Project, in.Task, in.CodeLinkInput)
	})
	tool(s, "save_floorplan", "Create or replace a floor plan (document type floorplan). Read it first with get_document when replacing, and create the room and device entries before referencing them.", func(ctx context.Context, a core.Actor, in saveFloorplanIn) (any, error) {
		if in.Document != "" {
			return svc.UpdateDocument(ctx, a, in.Project, in.Document, core.DocumentPatch{Floorplan: &in.Floorplan})
		}
		return svc.CreateDocument(ctx, a, in.Project, core.DocumentInput{Type: store.DocFloorplan, Title: in.Title, Category: in.Category, Floorplan: &in.Floorplan})
	})
	tool(s, "get_whiteboard", "Read a whiteboard (document type whiteboard): elements with id, type, text, box [x,y,w,h] or points, bound ends (from/to), frames with their content, linked entries, embeds with title and status, prototypes (screens are frames with screen, components have ui and actions) and the open comments with the element they point to. Coordinates are board units (about pixels at 100 %); y grows downwards.", func(ctx context.Context, a core.Actor, in wbDocIn) (any, error) {
		return svc.WhiteboardView(ctx, a, in.Project, in.Document)
	})
	tool(s, "edit_whiteboard", "Change a whiteboard with operations; everyone on the board sees them at once. Elements: type rect|ellipse|diamond|text|sticky|frame|line|arrow|pen|image|embed|entity|block (block: kind = a block kind such as chart, source = the same text as that fenced block in a page, see page syntax); boxes have x, y, w, h; lines and arrows have points [[x,y],...] (waypoints between the ends) and bind their ends with start/end {element} (towards the centre) or {element, fx, fy} (fixed point, 0–1 of the box); round: true for smooth corners. text, stroke and fill colours gray|red|orange|yellow|green|blue|purple|pink (follow the theme; prefer them) or a custom #rrggbb, dash dashed|dotted, frame (id of a frame), entity (entry slug: shows the entry), doc (page slug), url (embed: a video, pull request, issue or website; kind and preview are filled in; or kind app with doc = an app of the project, which runs on the board with each viewer's rights), file (image: a project file id). For diagrams prefer draw_diagram. Read the board first with get_whiteboard.", func(ctx context.Context, a core.Actor, in editWhiteboardIn) (any, error) {
		return svc.EditWhiteboard(ctx, a, in.Project, in.Document, in.Ops)
	})
	tool(s, "draw_diagram", "Draw a diagram on a whiteboard from its structure; Phoedox lays it out (layers in direction right or down), places it next to what is on the board and binds the arrows. nodes: {id, text, shape rect|ellipse|diamond|sticky, color, entity (entry slug), group}; edges: {from, to, label, dashed, line (no arrow head)}; groups: {id, label} become frames; title frames the whole diagram. Or send mermaid: a Mermaid flowchart (nodes, shapes, labelled, dotted and thick links, subgraphs) that becomes editable shapes. Without document a new whiteboard is created.", func(ctx context.Context, a core.Actor, in drawDiagramIn) (any, error) {
		if strings.TrimSpace(in.Mermaid) != "" {
			return svc.DrawMermaid(ctx, a, in.Project, in.Document, in.Title, in.Category, in.Mermaid)
		}
		return svc.DrawDiagram(ctx, a, in.Project, in.Document, in.Title, in.Category, in.DiagramInput)
	})
	tool(s, "draw_screens", "Draw a clickable prototype on a whiteboard: screens (frames of a phone, tablet or desktop) with components and interactions; people play it full screen, comment on it and export it. prototype: name (existing prototypes are extended; a screen with an existing name is redrawn in place). device phone|tablet|desktop, look \"\"|sketch (wireframe). vars: [{name, type bool|text|number|list, value}]; data: [{name, source sample|query|tasks|checks, ref (saved query or board slug), rows (sample rows, also shown where live data cannot be read)}]. screens: [{name, start, overlay modal|sheet, actions (on open|delay), html, elements}]. html makes a screen free HTML/CSS like an artifact (high fidelity; sandboxed, no network, the app's theme as CSS variables --accent, --fg, --surface …): data-goto=\"Screen\", data-overlay, data-back, data-close, data-set=\"var=value\", data-toggle, data-inc, data-toast, data-link on clickable elements; data-show=\"var\", data-text=\"var\", data-bind=\"var\" show variables; elements, if any, lie on top. look \"\" is clean in the app's look (default), sketch a hand-drawn wireframe (only when asked). Elements stack from the top; let Phoedox lay out instead of giving x/y (relative to the screen, only for floating things): containers {layout row|column|grid, items, gap, pad, cols, style card} place their items side by side, stacked or in a grid (row items with w keep it, others share by flex; style card or a fill draws a surface), nesting up to four levels; long screens grow and scroll when played. Texts: {type text, text, style title|heading|subheading|body|caption|label}. The result lists warnings (outside the screen, overlapping): fix them and check with whiteboard_screenshot. Elements: {comp button|link|input|textarea|checkbox|switch|radio|select|slider|tabs|navbar|list|card|table|toast|icon|chart|avatar|badge|progress, text (label; {{var}} inserts a variable, {{row.field}} a list row's field), variant primary|secondary|ghost|danger, placeholder, value, options (radio, select, tabs, navbar, list), icon (Lucide name), bind (variable), data (data source), fields (list: title and subtitle field; table/chart: columns; tabs/navbar: icons), min, max, show (\"var\", \"!var\", \"var=value\"), actions} or {type text|rect|ellipse|sticky|line, text, fontSize}. actions: [{on click|change|hover|open|delay, do goto|back|overlay|close|set|toggle|inc|add|link|toast, target (screen name, or variable), value, item (only for this option or row), anim slide|fade, delay ms}]. Without document a new whiteboard is created. Read prototypes with get_whiteboard.", func(ctx context.Context, a core.Actor, in drawScreensIn) (any, error) {
		return svc.DrawScreens(ctx, a, in.Project, in.Document, in.Title, in.Category, in.ScreensInput)
	})
	tool(s, "reply_comment", "Answer a comment on a whiteboard (ids from get_whiteboard). People mark comments as done themselves.", func(ctx context.Context, a core.Actor, in replyCommentIn) (any, error) {
		return svc.ReplyWhiteboardComment(ctx, a, in.Project, in.Document, in.Comment, in.Text)
	})
	tool(s, "save_whiteboard", "Create or replace a whole whiteboard (e.g. an import). For changes use edit_whiteboard, for diagrams draw_diagram.", func(ctx context.Context, a core.Actor, in saveWhiteboardIn) (any, error) {
		if in.Document != "" {
			return svc.UpdateDocument(ctx, a, in.Project, in.Document, core.DocumentPatch{Whiteboard: &in.Whiteboard})
		}
		return svc.CreateDocument(ctx, a, in.Project, core.DocumentInput{Type: store.DocWhiteboard, Title: in.Title, Category: in.Category, Whiteboard: &in.Whiteboard})
	})
	tool(s, "create_app", "Create an app: a small web program in the project (HTML/CSS/JS or React JSX/TypeScript, compiled on save) that runs in an isolated frame. Libraries by import name: react, react-dom/client, lit, chart.js, d3; no other network (pictures, fonts, data files: upload_app_asset, used as assets/<name>). Data only through window.phoedox: info(), requestAccess(), me() and read(sourceId) (level 1); storage.get/set/delete/list(key, value, {personal}), db.collection(name, {personal}) with add/put/update/delete/query({where, order, desc, limit})/subscribe(f) for records and live changes, files.upload/list/get/delete for files people add (level 2); propose({kind: task|comment|entity, ...}) (level 3, the person confirms); download(name, data, type) (the person confirms); device.request(camera|microphone|screen|location|motion) for features listed in devices (each person allows each one); self.files()/save(files) with self_edit; open(ref); ai(prompt, {json}) (with ai: true, on the viewer's budget). Ask for the lowest level and the fewest devices that do the job. Check get_app afterwards for compile and console errors, app_screenshot for how it looks.", func(ctx context.Context, a core.Actor, in createAppIn) (any, error) {
		return svc.CreateDocument(ctx, a, in.Project, core.DocumentInput{Type: store.DocApp, Title: in.Title, Category: in.Category, App: &core.App{Files: in.Files, Level: in.Level, Sources: in.Sources, StorageMB: in.StorageMB, AI: in.AI, Devices: in.Devices, SelfEdit: in.SelfEdit, Starter: in.Starter}})
	})
	tool(s, "get_app", "Read an app: its files, manifest (level, sources, storage, devices, selfEdit), assets, compile errors and the last console errors and warnings from people using it. Use it before edit_app and after changes to fix errors.", func(ctx context.Context, a core.Actor, in wbDocIn) (any, error) {
		return svc.GetApp(ctx, a, in.Project, in.Document)
	})
	tool(s, "edit_app", "Change an app with exact edits {file, old, new} (old exactly once), whole files (null removes one) or a new manifest. Saved as a new version; compile errors come back in the result's build.", func(ctx context.Context, a core.Actor, in editAppIn) (any, error) {
		return svc.EditApp(ctx, a, in.Project, in.Document, in.Files, in.Edits, in.Manifest)
	})
	appScreenshotTool(s, svc)
	whiteboardScreenshotTool(s, svc)
	tool(s, "upload_app_asset", "Add a binary file to an app (picture, font, sound, data file) from base64; the app loads it as assets/<name> (e.g. <img src=\"assets/logo.png\">). Needs the project's AI write mode \"direct\".", func(ctx context.Context, a core.Actor, in appAssetIn) (any, error) {
		return svc.AddAppAssetBase64(ctx, a, in.Project, in.Document, in.Name, in.Data)
	})
	tool(s, "copy_app_assets", "Copy assets from another app (also in another project you can read) into an app, e.g. shared logos or fonts. Needs the project's AI write mode \"direct\".", func(ctx context.Context, a core.Actor, in copyAppAssetsIn) (any, error) {
		return svc.CopyAppAssets(ctx, a, in.Project, in.Document, in.FromProject, in.From, in.Names)
	})
	tool(s, "wait_for_changes", "Notice what people do while you work: changes and comments on pages and apps (also whiteboard comments, cards, decisions, uploads, app permissions) by anyone but this AI session. Call once without cursor to get one, then with the cursor and wait (up to 50 s) to wait for the next changes; answers with the changes (kind, document, comment text, who, when) and the cursor for the next call. Use it when the person asks you to watch a page or app, e.g. to answer their comments.", func(ctx context.Context, a core.Actor, in waitChangesIn) (any, error) {
		return svc.WaitForChanges(ctx, a, in.Project, in.Documents, in.Cursor, in.Wait)
	})
	tool(s, "create_board", "Create a task board (a document of type board with columns).", func(ctx context.Context, a core.Actor, in createBoardIn) (any, error) {
		return svc.CreateDocument(ctx, a, in.Project, core.DocumentInput{Type: store.DocBoard, Title: in.Title, Category: in.Category, Board: &core.Board{Description: in.Description, Columns: in.Columns}})
	})
	tool(s, "create_task", "Add a task to a board. Check list_tasks first to avoid duplicates.", func(ctx context.Context, a core.Actor, in createTaskIn) (any, error) {
		return svc.CreateTask(ctx, a, in.Project, in.Board, core.TaskInput{Title: in.Title, Description: in.Description, Column: in.Column, Assignee: in.Assignee, Due: in.Due, Priority: in.Priority, Labels: in.Labels, Documents: in.Documents, Subtasks: subtasksOf(in.Subtasks)})
	})
	tool(s, "update_task", "Change a task or move it to another column (e.g. the done column) or board. Only given fields change.", func(ctx context.Context, a core.Actor, in updateTaskIn) (any, error) {
		return svc.UpdateTask(ctx, a, in.Project, in.Task, in.TaskPatch)
	})
	tool(s, "list_document_versions", "List the version history of a document (who changed it when).", func(ctx context.Context, a core.Actor, in docRef) (any, error) {
		return svc.VersionHistory(ctx, a, in.Project, in.Document)
	})
	tool(s, "get_document_version", "Read an older version of a document with its content.", func(ctx context.Context, a core.Actor, in versionRef) (any, error) {
		return svc.DocumentVersion(ctx, a, in.Project, in.Document, in.Version)
	})
	tool(s, "restore_document_version", "Make an older version the current one (stored as a new version).", func(ctx context.Context, a core.Actor, in versionRef) (any, error) {
		return svc.RestoreVersion(ctx, a, in.Project, in.Document, in.Version)
	})
	tool(s, "create_document", "Create a Markdown page.", func(ctx context.Context, a core.Actor, in createDocIn) (any, error) {
		res, err := svc.CreateDocument(ctx, a, in.Project, core.DocumentInput{Type: store.DocMarkdown, Title: in.Title, Slug: in.Slug, Category: in.Category, Position: in.Position, Markdown: in.Markdown, After: in.After})
		if err != nil {
			return nil, err
		}
		tr := saveTranslations(ctx, svc, a, in.Project, res, in.Translations)
		if !in.Verbose {
			out := briefResult(res, tr)
			checkBlocks(ctx, svc, a, in.Project, res, &in.Markdown, &out)
			return out, nil
		}
		return withTranslations{DocResult: res, Translations: tr}, nil
	})
	tool(s, "update_document", "Update a Markdown page, a decision's texts (via edits) or a link. Only given fields change; markdown replaces the whole content, edits change exact passages (cheaper for long pages); url and description apply to links. The answer is short (id, version, url) unless verbose. If the page has an open draft (someone is editing it), the change goes into the draft and is published together with it by a person; the result then contains \"draft\". With pull_request the change goes into a draft tied to that pull request (published on merge).", func(ctx context.Context, a core.Actor, in updateDocIn) (any, error) {
		if err := linkPR(ctx, svc, a, in.Project, in.Document, in.PullRequest); err != nil {
			return nil, err
		}
		patch := core.DocumentPatch{Title: in.Title, Markdown: in.Markdown, Category: in.Category, Slug: in.Slug, Position: in.Position, ExpectVersion: in.ExpectVersion,
			Edits: in.Edits, After: in.After}
		if in.URL != nil || in.Description != nil {
			cur, err := svc.Document(ctx, a, in.Project, in.Document)
			if err != nil {
				return nil, err
			}
			if cur.Type != store.DocLink {
				return nil, errors.New("url and description apply to links only")
			}
			var link core.Link
			_ = json.Unmarshal(cur.Content, &link)
			setIf(&link.URL, in.URL)
			setIf(&link.Description, in.Description)
			patch.Link = &link
		}
		res, err := svc.UpdateDocument(ctx, a, in.Project, in.Document, patch)
		if err != nil || in.Verbose {
			return res, err
		}
		out := briefResult(res, nil)
		if in.Markdown != nil || len(in.Edits) > 0 {
			checkBlocks(ctx, svc, a, in.Project, res, in.Markdown, &out)
		}
		return out, nil
	})
	tool(s, "update_board", "Change a board: title, category, description above it, or its columns (rename, reorder, add; remove only empty ones). Only given fields change.", func(ctx context.Context, a core.Actor, in updateBoardIn) (any, error) {
		cur, err := svc.Document(ctx, a, in.Project, in.Board)
		if err != nil {
			return nil, err
		}
		if cur.Type != store.DocBoard {
			return nil, errors.New("this document is not a board")
		}
		var b core.Board
		_ = json.Unmarshal(cur.Content, &b)
		setIf(&b.Description, in.Description)
		if len(in.Columns) > 0 {
			for _, old := range b.Columns {
				if slices.ContainsFunc(in.Columns, func(c core.BoardColumn) bool { return c.ID == old.ID }) {
					continue
				}
				tasks, err := svc.Tasks(ctx, a, in.Project, cur.ID, old.ID, "", false)
				if err != nil {
					return nil, err
				}
				if len(tasks) > 0 {
					return nil, fmt.Errorf("column %q still has %d tasks; move them first", old.Name, len(tasks))
				}
			}
			b.Columns = in.Columns
		}
		return svc.UpdateDocument(ctx, a, in.Project, cur.ID, core.DocumentPatch{Title: in.Title, Category: in.Category, Board: &b, ExpectVersion: in.ExpectVersion})
	})
	tool(s, "create_decision", "Record a decision (ADR) with context, decision, consequences and alternatives. For a decision people still have to make, use status proposed with options; they choose one in Phoedox or answer that nothing fits. In multilingual projects pass translations to save them in the same call; for many decisions use create_decisions.", func(ctx context.Context, a core.Actor, in createDecisionIn) (any, error) {
		cat, err := decisionCategory(ctx, svc, a, in.Project)
		if err != nil {
			return nil, err
		}
		res, err := createDecision(ctx, svc, a, in.Project, in.decisionItem, cat)
		if err != nil {
			return nil, err
		}
		return withTranslations{DocResult: res, Translations: saveTranslations(ctx, svc, a, in.Project, res, in.Translations)}, nil
	})
	tool(s, "create_decisions", "Record many decisions in one call (up to 50, e.g. when importing a decision log), each with its translations. Items are created in order, so one may supersede an earlier item. An item that fails is reported and the others continue. Returns per item: ref, slug, ext_ref, url, translations, or error.", func(ctx context.Context, a core.Actor, in createDecisionsIn) (any, error) {
		if len(in.Decisions) == 0 || len(in.Decisions) > maxBulkDecisions {
			return nil, fmt.Errorf("give 1 to %d decisions per call", maxBulkDecisions)
		}
		cat, err := decisionCategory(ctx, svc, a, in.Project)
		if err != nil {
			return nil, err
		}
		out := bulkDecisions{Results: make([]bulkDecision, 0, len(in.Decisions))}
		for i, item := range in.Decisions {
			r := bulkDecision{Index: i, Title: item.Title, Type: store.DocADR}
			res, err := createDecision(ctx, svc, a, in.Project, item, cat)
			switch {
			case err != nil:
				r.Error = err.Error()
				out.Failed++
			case res.Document != nil:
				d := res.Document
				r.ID, r.ProjectID, r.Slug, r.Title, r.Ref, r.ExtRef = d.ID, d.ProjectID, d.Slug, d.Title, decisionRef(*d), d.ExtRef
				r.Translations = saveTranslations(ctx, svc, a, in.Project, res, item.Translations)
				out.Created++
			default:
				r.Suggestion = res.Suggestion
				out.Suggested++
			}
			out.Results = append(out.Results, r)
		}
		return out, nil
	})
	tool(s, "list_decisions", "The decision log of a project: number (ADR-0003), title, status, date and which decision supersedes which (fully, or partly with a scope), ordered by number.", func(ctx context.Context, a core.Actor, in listDecisionsIn) (any, error) {
		return svc.Decisions(ctx, a, in.Project, in.Status, in.Query)
	})
	tool(s, "update_decision", "Update a decision. Given fields replace the current ones, others stay. With pull_request the change waits in a draft until that pull request is merged.", func(ctx context.Context, a core.Actor, in updateDecisionIn) (any, error) {
		cur, err := svc.Document(ctx, a, in.Project, in.Document)
		if err != nil {
			return nil, err
		}
		if err := linkPR(ctx, svc, a, in.Project, cur.ID, in.PullRequest); err != nil {
			return nil, err
		}
		var adr core.ADR
		_ = json.Unmarshal(cur.Content, &adr)
		// An open draft (e.g. of a pull request) is the base for the change.
		if d, err := svc.Draft(ctx, a, in.Project, cur.ID); err == nil && d.Markdown != "" {
			_ = json.Unmarshal([]byte(d.Markdown), &adr)
		}
		f := in.decisionFields
		setIf(&adr.Status, &f.Status)
		setIf(&adr.Date, &f.Date)
		setIf(&adr.Context, &f.Context)
		setIf(&adr.Decision, &f.Decision)
		setIf(&adr.Consequences, &f.Consequences)
		setIf(&adr.Alternatives, &f.Alternatives)
		setIf(&adr.Supersedes, &f.Supersedes)
		setIf(&adr.SupersedesScope, &f.SupersedesScope)
		if f.SupersedesPartly != nil {
			adr.SupersedesPartly = *f.SupersedesPartly
		}
		if len(f.Deciders) > 0 {
			adr.Deciders = f.Deciders
		}
		if f.Options != nil {
			adr.Options = f.Options
		}
		return svc.UpdateDocument(ctx, a, in.Project, cur.ID, core.DocumentPatch{Title: in.Title, Category: in.Category, ADR: &adr, ExtRef: in.ExtRef, ExpectVersion: in.ExpectVersion})
	})
	tool(s, "create_link", "Add a link to external documentation (shown in the sidebar with an external-link icon).", func(ctx context.Context, a core.Actor, in createLinkIn) (any, error) {
		return svc.CreateDocument(ctx, a, in.Project, core.DocumentInput{Type: store.DocLink, Title: in.Title, Category: in.Category, Link: &core.Link{URL: in.URL, Description: in.Description}})
	})
	tool(s, "delete_document", "Move a document to the trash.", func(ctx context.Context, a core.Actor, in docRef) (any, error) {
		return svc.DeleteDocument(ctx, a, in.Project, in.Document)
	})
	tool(s, "list_environment", "Variables of the project environment used by request blocks. Secret values are never shown.", func(ctx context.Context, a core.Actor, in projectRef) (any, error) {
		return svc.Environment(ctx, a, in.Project)
	})
	tool(s, "run_request", "Run a request block (```request) of a page and check its expectations. Only reading requests (GET, HEAD, OPTIONS); secrets are masked.", func(ctx context.Context, a core.Actor, in runRequestIn) (any, error) {
		return svc.RunRequest(ctx, a, in.Project, in.Document, in.Index)
	})
	tool(s, "my_tasks", "Open tasks assigned to the token owner in all visible projects, due ones first (with project and board).", func(ctx context.Context, a core.Actor, _ struct{}) (any, error) {
		return svc.MyTasks(ctx, a)
	})
	tool(s, "list_comments", "Comment threads of a page with replies; open threads first. anchor quotes the block a thread is about.", func(ctx context.Context, a core.Actor, in docRef) (any, error) {
		return svc.Comments(ctx, a, in.Project, in.Document)
	})
	tool(s, "add_comment", "Comment on a page, on one block of it (anchor = a short quote of the block's text) or answer a thread (parent = comment id). Mentioned people (e-mails of people who can read the page) get a mail.", func(ctx context.Context, a core.Actor, in addCommentIn) (any, error) {
		return svc.AddComment(ctx, a, in.Project, in.Document, core.CommentInput{Body: in.Body, Anchor: in.Anchor, Parent: in.Parent, Mentions: in.Mentions})
	})
	tool(s, "list_maintenance_plans", "List maintenance plans (recurring tasks per entry, e.g. filter changes or battery swaps) with their next due date.", func(ctx context.Context, a core.Actor, in maintenanceListIn) (any, error) {
		return svc.MaintenancePlans(ctx, a, in.Project, in.Entity)
	})
	tool(s, "save_maintenance_plan", "Create or change a maintenance plan for an entry: every N days/weeks/months/years Phoedox puts a card on the given board, leadDays before it is due. Only in projects where AI writes directly.", func(ctx context.Context, a core.Actor, in maintenanceSaveIn) (any, error) {
		return svc.SaveMaintenancePlan(ctx, a, in.Project, in.ID, in.MaintenanceInput)
	})
	tool(s, "calendar", "The project's dates in a range (ADR-0089): maintenance plans (with the following dates), cards with a due date, date fields of entries (e.g. a contract's cancel_by) and events of external calendars. Each event has start/end (all-day dates or times), source, link (e/<entry> or d/<board>?task=<id>) and for date fields the field name.", func(ctx context.Context, a core.Actor, in calendarIn) (any, error) {
		from, to, err := core.CalendarRange(in.From, in.To, time.Now())
		if err != nil {
			return nil, err
		}
		spec := &core.CalendarSpec{}
		if in.Document != "" {
			if spec, err = svc.CalendarDocSpec(ctx, a, in.Project, in.Document); err != nil {
				return nil, err
			}
		}
		if len(in.Sources) > 0 {
			spec.Sources = in.Sources
		}
		if len(in.Kinds) > 0 {
			spec.Kinds = in.Kinds
		}
		if len(in.Calendars) > 0 {
			spec.Calendars = in.Calendars
		}
		v, err := svc.Calendar(ctx, a, in.Project, from, to, spec)
		v.Feed = nil
		return v, err
	})
	tool(s, "list_external_calendars", "External calendars of a project (CalDAV or iCal URLs; whether Phoedox writes its dates into them, last sync, last error). People add them in Phoedox (project settings → Calendars), because they hold passwords.", func(ctx context.Context, a core.Actor, in projectOnlyIn) (any, error) {
		return svc.ExternalCalendars(ctx, a, in.Project)
	})
	tool(s, "create_calendar", "Create a calendar document: a month or list view of the project's dates with filters (sources, entry kinds, external calendars). Pages can show the same with a ```calendar block.", func(ctx context.Context, a core.Actor, in createCalendarIn) (any, error) {
		return svc.CreateDocument(ctx, a, in.Project, core.DocumentInput{Type: store.DocCalendar, Title: in.Title, Category: in.Category, Calendar: &in.Calendar})
	})
	tool(s, "list_saved_queries", "Saved queries of a project (name, slug and definition: kind, field and relation conditions).", func(ctx context.Context, a core.Actor, in projectOnlyIn) (any, error) {
		return svc.SavedQueries(ctx, a, in.Project)
	})
	tool(s, "run_saved_query", "Run a saved query and get the matching entries with their fields.", func(ctx context.Context, a core.Actor, in runQueryIn) (any, error) {
		return svc.RunSavedQuery(ctx, a, in.Project, in.Query)
	})
	tool(s, "query_entities", "Find entries of one kind by field conditions (op: eq, ne, contains, empty, set, gt, lt) and relation conditions (rel, in, to, kind, missing), e.g. servers without a located_in edge.", func(ctx context.Context, a core.Actor, in queryEntitiesIn) (any, error) {
		return svc.RunQuery(ctx, a, in.Project, in.GraphQuery)
	})
	tool(s, "household_overview", "Household overview of a project: entries with price, purchase date, warranty end (days left) and yearly power use and cost, plus totals.", func(ctx context.Context, a core.Actor, in projectOnlyIn) (any, error) {
		return svc.Household(ctx, a, in.Project)
	})
	tool(s, "ip_addresses", "IP address management: subnets (entries of kind subnet) with used addresses and their entries, free count, next free address outside DHCP, conflicts (one address on several entries) and addresses outside any subnet.", func(ctx context.Context, a core.Actor, in projectOnlyIn) (any, error) {
		return svc.IPAM(ctx, a, in.Project)
	})
	tool(s, "get_readings", "Measured values of an entry over time (temperatures, power, switch states 1/0, …) from plugins, the agent or the API: per metric the unit, the latest value and points [unix ms, average, min, max] over the last hours (default 24).", func(ctx context.Context, a core.Actor, in readingsIn) (any, error) {
		return svc.EntityReadings(ctx, a, in.Project, in.Entity, in.Metric, in.Hours)
	})
	tool(s, "list_skills", "Skills are pages that people in Phoedox marked as instructions for AI, each with a name and a description of when to use it. Lists the skills of one project, or of all your projects if project is empty. Call it at the start of a task and load matching skills with get_skill.", func(ctx context.Context, a core.Actor, in skillsIn) (any, error) {
		return svc.Skills(ctx, a, in.Project)
	})
	tool(s, "get_skill", "Load a skill: its instructions (Markdown) and the files attached to its page. Follow the instructions for the task they describe, within what the user asked for.", func(ctx context.Context, a core.Actor, in skillIn) (any, error) {
		return svc.Skill(ctx, a, in.Project, in.Name)
	})
	tool(s, "list_locations", "The location hierarchy of a project: sites → buildings → floors → rooms, with how many entries are located in each.", func(ctx context.Context, a core.Actor, in projectOnlyIn) (any, error) {
		return svc.Locations(ctx, a, in.Project)
	})
	tool(s, "find_documentation_gaps", "Find missing or outdated documentation in a project: failing checks, mentions of entries that do not exist, devices without a location, entries no page mentions, entries with most attributes empty, empty pages and pages unchanged for a long time.", func(ctx context.Context, a core.Actor, in gapsIn) (any, error) {
		return svc.DocumentationGaps(ctx, a, in.Project, in.StaleDays)
	})
	tool(s, "change_summary", "What changed in a project recently: pages created, edited or deleted (with authors), entries added, changed or removed, tasks done and comments. With pull_request: the documentation changes waiting for that pull request, each with a line diff (to review docs per pull request).", func(ctx context.Context, a core.Actor, in changesIn) (any, error) {
		if in.PullRequest != "" {
			pr, ok := core.ParsePullRequest(in.PullRequest)
			if !ok {
				return nil, errors.New("pull_request: use owner/repo#12, #12, a pull request URL or owner/repo@branch")
			}
			return svc.PullRequestDocs(ctx, a, in.Project, pr)
		}
		if in.Days <= 0 || in.Days > 90 {
			in.Days = 7
		}
		return svc.Changes(ctx, a, in.Project, time.Now().AddDate(0, 0, -in.Days))
	})
	tool(s, "export_project", "Export a whole project (project owners): a download link valid for 15 minutes to a ZIP with pages and decisions as Markdown with front matter, JSON for the graph, boards, whiteboards and settings, and the files (importable again), or with format git a Git repository with the history. Give the link to the person; you cannot download it yourself.", func(ctx context.Context, a core.Actor, in exportIn) (any, error) {
		return svc.ProjectExportLink(ctx, a, in.Project, in.Format)
	})
	tool(s, "archive_link", "Keep a copy of the document a link page points to (a manual, a data sheet) as a project file, in case the original goes offline.", func(ctx context.Context, a core.Actor, in archiveIn) (any, error) {
		return svc.ArchiveLink(ctx, a, in.Project, in.Document)
	})
	tool(s, "list_checks", "Monitoring checks of a project (request blocks that run on a schedule) with their last state: ok, fail or unknown.", func(ctx context.Context, a core.Actor, in listChecksIn) (any, error) {
		return svc.Checks(ctx, a, in.Project, in.Document)
	})
	tool(s, "create_check", "Monitor a reading request block (```request with GET, HEAD or OPTIONS) of a page every intervalSec seconds (60–86400, default 300). Optionally link an entry; its state attribute then shows online/offline. The creator gets a mail when the state changes.", func(ctx context.Context, a core.Actor, in createCheckIn) (any, error) {
		return svc.CreateCheck(ctx, a, in.Project, core.CheckInput{Document: in.Document, Block: in.Index, Entity: in.Entity, Name: in.Name, IntervalSec: in.IntervalSec})
	})
	tool(s, "search", "Search documents and entities in one or all visible projects. When nothing matches as typed, typos are corrected and hits carry \"corrected\".", func(ctx context.Context, a core.Actor, in searchIn) (any, error) {
		return svc.Search(ctx, a, in.Query, in.Project, in.Limit)
	})
	tool(s, "get_data_model", "The data model of a project: kinds of entities with their attributes as JSON schema and the allowed relations. Read it before creating entities or relations.", func(ctx context.Context, a core.Actor, in projectRef) (any, error) {
		m, err := svc.DataModel(ctx, a, in.Project)
		if err != nil {
			return nil, err
		}
		kinds := make([]modelKind, 0, len(m.Kinds))
		for _, k := range m.Kinds {
			kinds = append(kinds, modelKind{ID: k.ID, Name: k.Name, Description: k.Description, Group: k.Group, Source: k.Source, Count: k.Count, Schema: k.JSONSchema(), Relations: k.Relations})
		}
		return map[string]any{"kinds": kinds, "genericRelations": m.Generic, "kindsWithoutDefinition": m.Unknown, "icons": core.KindIcons}, nil
	})
	tool(s, "define_kind", "Define an own kind of entity for a project, or extend a core kind with extra attributes and relations. Replaces the project's previous definition of this kind, so repeat existing extras you want to keep.", func(ctx context.Context, a core.Actor, in defineKindIn) (any, error) {
		return svc.DefineKind(ctx, a, in.Project, core.KindInput{ID: in.ID, Name: in.Name, Description: in.Description, Icon: in.Icon, Attrs: in.Attrs, Relations: in.Relations})
	})
	tool(s, "list_files", "List uploaded files of a project (name, type, size, where attached). Contents are not returned.", func(ctx context.Context, a core.Actor, in listFilesIn) (any, error) {
		return svc.Files(ctx, a, in.Project, core.FileFilter{Document: in.Document, Entity: in.Entity})
	})
	tool(s, "upload_image", "Upload a picture sent as base64 (screenshot, photo, diagram) into a project and get its Markdown. Attach it to a page or entity, or give a task id to add it to that card's description. Insert the returned markdown into pages yourself (update_document). Needs the project's AI write mode \"direct\".", func(ctx context.Context, a core.Actor, in uploadImageIn) (any, error) {
		return svc.UploadImage(ctx, a, in.Project, core.ImageUpload{Name: in.Name, Data: in.Data, Alt: in.Alt, Document: in.Document, Entity: in.Entity, Task: in.Task})
	})
	tool(s, "set_project_image", "Set the picture (logo) of a project from base64; it is scaled to 512 px. Owners only; needs the project's AI write mode \"direct\".", func(ctx context.Context, a core.Actor, in projectImageIn) (any, error) {
		v, err := svc.SetProjectImageBase64(ctx, a, in.Project, in.Data)
		if err != nil {
			return nil, err
		}
		return map[string]string{"version": v}, nil
	})
	tool(s, "list_entities", "List entities of a project, optionally of one kind.", func(ctx context.Context, a core.Actor, in listEntitiesIn) (any, error) {
		return svc.Entities(ctx, a, in.Project, in.Kind)
	})
	tool(s, "get_entity", "Read an entity: a context pack (values, status, network path, relations, document snippets, what runs on it) followed by the raw entity, relations and documents.", func(ctx context.Context, a core.Actor, in entityRef) (any, error) {
		d, err := svc.Entity(ctx, a, in.Project, in.Entity)
		if err != nil {
			return nil, err
		}
		pack, err := svc.EntityContext(ctx, a, in.Project, in.Entity)
		if err != nil {
			return nil, err
		}
		return struct {
			Context string `json:"context"`
			core.EntityDetail
		}{pack, d}, nil
	})
	tool(s, "list_discovered", "Devices the project's agents saw in the network (neighbour table, mDNS) with IP, MAC, host name, announced services, a suggested kind and the entry they already match. Add the unknown ones you are asked to document with upsert_entities (keep ip, mac, hostname).", func(ctx context.Context, a core.Actor, in projectRef) (any, error) {
		return svc.Discovered(ctx, a, in.Project, false)
	})
	tool(s, "log_time", "Log time spent on a card (task), e.g. after work someone describes: duration like \"1:30\", \"45m\" or \"1.5h\", optional day (YYYY-MM-DD, default today) and note. It is logged for the signed-in person.", func(ctx context.Context, a core.Actor, in logTimeIn) (any, error) {
		return svc.LogTime(ctx, a, in.Project, in.Task, core.TimeInput{Duration: in.Duration, Day: in.Day, Note: in.Note})
	})
	tool(s, "list_feedback_topics", "Topics of a project's feedback portals (reports from people grouped by what they are about), highest priority first, with state, category, number of reports and tickets. For moderators: editors and owners, or people made moderator of a portal.", func(ctx context.Context, a core.Actor, in feedbackListIn) (any, error) {
		return svc.FeedbackTopics(ctx, a, in.Project, core.TopicQuery{Portal: in.Portal, State: in.State, Category: in.Category, Priority: in.Priority, Q: in.Q})
	})
	tool(s, "get_feedback_topic", "A feedback topic with all its reports (text, form fields, context, attachments' names and types), the portal's settings (states, categories) and its tickets.", func(ctx context.Context, a core.Actor, in feedbackTopicIn) (any, error) {
		return svc.FeedbackTopic(ctx, a, in.Project, in.Topic)
	})
	tool(s, "update_feedback_topic", "Triage a feedback topic: title, summary, category and tags from the portal's lists, priority 1–4, state, the version that fixed it, the knowledge-graph entries it is about, whether it is shown on the portal's public page; an answer is sent to the reporters, so set it only when a person asked for it.", func(ctx context.Context, a core.Actor, in feedbackUpdateIn) (any, error) {
		return svc.UpdateFeedbackTopic(ctx, a, in.Project, in.Topic, core.TopicPatch{Title: in.Title, Summary: in.Summary, Category: in.Category, Tags: in.Tags, Priority: in.Priority, State: in.State, Answer: in.Answer, Spam: in.Spam, DismissDuplicate: in.DismissDuplicate, FixedIn: in.FixedIn, Public: in.Public, Entities: in.Entities, Question: in.Question})
	})
	tool(s, "merge_feedback_topics", "Merge a duplicate feedback topic into another: its reports and tickets move over and it is removed.", func(ctx context.Context, a core.Actor, in feedbackMergeIn) (any, error) {
		return svc.MergeFeedbackTopics(ctx, a, in.Project, in.Into, in.From)
	})
	tool(s, "feedback_to_board", "Turn a feedback topic into a ticket: a card on a board, linked to the topic and its reports. A topic can get several tickets (e.g. frontend and backend); when all are done, the topic is done and reporters are told.", func(ctx context.Context, a core.Actor, in feedbackBoardIn) (any, error) {
		return svc.FeedbackToBoard(ctx, a, in.Project, in.Topic, core.TicketInput{Board: in.Board, Column: in.Column, Title: in.Title})
	})
	tool(s, "forward_feedback_topic", "Forward a feedback topic to an external tracker the project connected (GitHub, Gitea/Forgejo, GitLab, Jira): opens an issue with the summary and report texts (never who reported). Later reports are added there as +n comments; when the issue is closed, the topic counts as done like a finished ticket. Only when a person asked for it.", func(ctx context.Context, a core.Actor, in feedbackForwardIn) (any, error) {
		return svc.ForwardFeedbackTopic(ctx, a, in.Project, in.Topic, in.Tracker)
	})
	tool(s, "get_feedback_portal", "The public settings of a feedback portal by its handle: report types, own form fields (ids, types, options, required), who may report.", func(ctx context.Context, a core.Actor, in feedbackPortalIn) (any, error) {
		return svc.PublicFeedbackPortal(ctx, in.Portal, in.Lang)
	})
	tool(s, "submit_feedback", "Send a report to a feedback portal as the signed-in person, e.g. when someone describes a bug in the chat and wants to report it. Fill the portal's required fields (get_feedback_portal).", func(ctx context.Context, a core.Actor, in feedbackSubmitIn) (any, error) {
		res, err := svc.SubmitFeedback(ctx, &a, in.Portal, core.ReportInput{Type: in.Type, Title: in.Title, Body: in.Body, Fields: in.Fields}, nil, core.Intake{Via: "mcp"})
		if err != nil {
			return nil, err
		}
		return map[string]string{"id": res.ID, "status": "/feedback/status/" + res.StatusToken}, nil
	})
	tool(s, "entity_timeline", "What happened to an entry, newest first: changes (by people, AI, agents, plugins, with the changed fields), check failures and recoveries, switching actions, agent jobs, reviews, and comments and cards that mention it. Use it to answer \"what changed on the NAS lately?\" or \"when did the router last go down?\".", func(ctx context.Context, a core.Actor, in timelineIn) (any, error) {
		return svc.EntityTimeline(ctx, a, in.Project, in.Entity, in.Limit)
	})
	tool(s, "upsert_entity", "Create an entity or update it if the slug exists.", func(ctx context.Context, a core.Actor, in upsertEntityIn) (any, error) {
		return svc.UpsertEntity(ctx, a, in.Project, core.EntityInput{Kind: in.Kind, Name: in.Name, Slug: in.Slug, Attrs: in.Attrs})
	})
	tool(s, "upsert_entities", "Create or update many entities in one call (up to 200), e.g. all devices of a network. Each row works like upsert_entity; a bad row is reported in errors and the others go on. Relations still use link_entities.", func(ctx context.Context, a core.Actor, in upsertEntitiesIn) (any, error) {
		if len(in.Entities) > maxMCPBulk {
			return nil, core.Invalid("validation.bulk_size", map[string]any{"max": maxMCPBulk}, "at most %d entities per call", maxMCPBulk)
		}
		ups := make([]core.EntityInput, 0, len(in.Entities))
		for _, e := range in.Entities {
			ups = append(ups, core.EntityInput{Kind: e.Kind, Name: e.Name, Slug: e.Slug, Attrs: e.Attrs})
		}
		return svc.Bulk(ctx, a, in.Project, core.BulkInput{Upserts: ups})
	})
	tool(s, "delete_entity", "Move an entity and its relations to the trash.", func(ctx context.Context, a core.Actor, in entityRef) (any, error) {
		return svc.DeleteEntity(ctx, a, in.Project, in.Entity)
	})
	tool(s, "link_entities", "Create a relation between two entities (from –rel→ to). Only relations allowed by the data model (see get_data_model) or the generic ones.", func(ctx context.Context, a core.Actor, in linkIn) (any, error) {
		return svc.Link(ctx, a, in.Project, core.EdgeInput{From: in.From, Rel: in.Rel, To: in.To, Attrs: in.Attrs})
	})
	tool(s, "unlink_entities", "Remove a relation between two entities.", func(ctx context.Context, a core.Actor, in unlinkIn) (any, error) {
		return svc.Unlink(ctx, a, in.Project, core.EdgeInput{From: in.From, Rel: in.Rel, To: in.To})
	})
	tool(s, "traverse", "Follow relations from an entity, e.g. the path to the internet (rel uplink) or everything a service depends on.", func(ctx context.Context, a core.Actor, in traverseIn) (any, error) {
		return svc.Traverse(ctx, a, in.Project, in.Entity, in.Rel, in.Direction, in.Depth)
	})
	tool(s, "software_tree", "What runs on a server, VM or LXC as a tree: installed software, VMs, stacks and containers with versions and states.", func(ctx context.Context, a core.Actor, in entityRefIn) (any, error) {
		return svc.SoftwareTree(ctx, a, in.Project, in.Entity)
	})
	tool(s, "prepare_agent_action", "Prepare an agent action for an entry (e.g. container.restart, stack.redeploy, system.update). It does NOT run: a person must sign it in Phoedox with their own key, and the host must allow it. Use list_agent_actions first.", func(ctx context.Context, a core.Actor, in agentActionIn) (any, error) {
		return svc.CreateAgentJob(ctx, a, in.Project, in.Entity, in.Action)
	})
	tool(s, "list_agent_actions", "Agent actions available for an entry (container, stack, server, VM, LXC) and its recent jobs with results.", func(ctx context.Context, a core.Actor, in entityRefIn) (any, error) {
		return svc.EntityAgentActions(ctx, a, in.Project, in.Entity)
	})
	tool(s, "list_duplicates", "Entries that look like the same device (same MAC address, serial number or hostname), e.g. reported by the agent and by Home Assistant.", func(ctx context.Context, a core.Actor, in projectRef) (any, error) {
		return svc.Duplicates(ctx, a, in.Project)
	})
	tool(s, "merge_entities", "Merge entry remove into keep (relations, page links and attachments move over; keep's attributes win). Only in projects where AI writes directly; otherwise ask a person to merge in Phoedox.", func(ctx context.Context, a core.Actor, in mergeIn) (any, error) {
		return svc.MergeEntities(ctx, a, in.Project, in.Keep, in.Remove)
	})
	tool(s, "diagnose", "Check the network path of a device (or of every device in a room) towards the internet along uplink, wired_to and runs_on, with a status per station from agent reports and state attributes. Use it first when someone says something does not work.", func(ctx context.Context, a core.Actor, in entityRefIn) (any, error) {
		return svc.Diagnose(ctx, a, in.Project, in.Entity)
	})
	tool(s, "list_suggestions", "List suggestions (pending AI changes) of a project.", func(ctx context.Context, a core.Actor, in suggestionsIn) (any, error) {
		return svc.Suggestions(ctx, a, in.Project, in.Status)
	})
}

// linkPR ties a document's draft to a pull request before a change.
func linkPR(ctx context.Context, svc *core.Service, a core.Actor, project, doc, ref string) error {
	if strings.TrimSpace(ref) == "" {
		return nil
	}
	pr, ok := core.ParsePullRequest(ref)
	if !ok {
		return errors.New("pull_request: use owner/repo#12, #12, a pull request URL or owner/repo@branch")
	}
	_, err := svc.LinkDraftCode(ctx, a, project, doc, pr)
	return err
}

func setIf(dst *string, v *string) {
	if v != nil && *v != "" {
		*dst = *v
	}
}

func subtasksOf(titles []string) []store.Subtask {
	out := make([]store.Subtask, 0, len(titles))
	for _, t := range titles {
		out = append(out, store.Subtask{Title: t})
	}
	return out
}

// overviewMaxDocs: above this, get_project counts documents per category.
const overviewMaxDocs = 80

// maxBulkDecisions caps create_decisions.
const maxBulkDecisions = 50

type getProjectIn struct {
	Project   string `json:"project" jsonschema:"project slug"`
	Documents string `json:"documents,omitempty" jsonschema:"full: every document by category; summary: only the number per category (the default for projects with more than 80 documents)"`
}

// withTranslations is a created document with the result per language.
type withTranslations struct {
	core.DocResult
	Translations map[string]string `json:"translations,omitempty"`
}

type bulkDecision struct {
	Index        int               `json:"index"`
	ID           string            `json:"id,omitempty"`
	Ref          string            `json:"ref,omitempty"`
	ExtRef       string            `json:"extRef,omitempty"`
	Slug         string            `json:"slug,omitempty"`
	Title        string            `json:"title"`
	Type         string            `json:"type"`
	ProjectID    string            `json:"projectId,omitempty"`
	Translations map[string]string `json:"translations,omitempty"`
	Suggestion   *store.Suggestion `json:"suggestion,omitempty"`
	Error        string            `json:"error,omitempty"`
}

type bulkDecisions struct {
	Created   int            `json:"created"`
	Suggested int            `json:"suggested,omitempty"`
	Failed    int            `json:"failed,omitempty"`
	Results   []bulkDecision `json:"results"`
}

// decisionCategory is where the project keeps its decisions; "Decisions"
// for the first one.
func decisionCategory(ctx context.Context, svc *core.Service, a core.Actor, project string) (string, error) {
	cat := "Decisions"
	docs, err := svc.Documents(ctx, a, project, store.DocFilter{Type: store.DocADR})
	if err != nil {
		return "", err
	}
	for _, d := range docs {
		if d.Category != "" && d.Number > 0 {
			cat = d.Category
		}
	}
	return cat, nil
}

func createDecision(ctx context.Context, svc *core.Service, a core.Actor, project string, in decisionItem, defaultCategory string) (core.DocResult, error) {
	if in.Category == "" {
		in.Category = defaultCategory
	}
	adr := in.adr()
	return svc.CreateDocument(ctx, a, project, core.DocumentInput{Type: store.DocADR, Title: in.Title, Slug: in.Slug, Category: in.Category, ExtRef: in.ExtRef, ADR: &adr})
}

// saveTranslations stores the translations of a document just created:
// "saved" or the error per language. A suggestion has no document yet.
func saveTranslations(ctx context.Context, svc *core.Service, a core.Actor, project string, res core.DocResult, tr map[string]translationIn) map[string]string {
	if len(tr) == 0 {
		return nil
	}
	out := map[string]string{}
	for lang, t := range tr {
		if res.Document == nil {
			out[lang] = "not saved: the document is a pending suggestion; translate it after a person accepts it"
			continue
		}
		if _, err := svc.SaveTranslation(ctx, a, project, res.Document.ID, lang, t.input()); err != nil {
			out[lang] = "error: " + err.Error()
			continue
		}
		out[lang] = "saved"
	}
	return out
}

type translateCategoryIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Category string `json:"category" jsonschema:"the folder path in the main language, e.g. Architecture/Backend"`
	Lang     string `json:"lang" jsonschema:"one of the project's further languages"`
	Name     string `json:"name" jsonschema:"the folder's own name in that language (the last part of the path, no /); empty removes it"`
}

// briefDoc is the short answer to a write: enough to refer to the
// document again and link it.
type briefDoc struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Type      string `json:"type"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Ref       string `json:"ref,omitempty"`
	Version   int    `json:"version"`
}

type briefDocResult struct {
	Applied      bool              `json:"applied"`
	Document     *briefDoc         `json:"document,omitempty"`
	Suggestion   any               `json:"suggestion,omitempty"`
	Draft        any               `json:"draft,omitempty"`
	Translations map[string]string `json:"translations,omitempty"`
	// BlockProblems are findings in mermaid and dbmodel blocks of the page.
	BlockProblems []core.BlockProblem `json:"blockProblems,omitempty"`
}

// checkBlocks adds the findings in diagram blocks to a write's answer: of
// the saved page (or its draft), or of the suggested text.
func checkBlocks(ctx context.Context, svc *core.Service, a core.Actor, project string, res core.DocResult, md *string, out *briefDocResult) {
	var ps []core.BlockProblem
	switch {
	case res.Applied && res.Document != nil && res.Document.Type == store.DocMarkdown:
		ps, _ = svc.ValidateBlocks(ctx, a, project, res.Document.ID, nil)
	case md != nil:
		ps, _ = svc.ValidateBlocks(ctx, a, project, "", md)
	}
	out.BlockProblems = ps
}

// briefResult drops the content from a write's answer (verbose: false).
func briefResult(res core.DocResult, translations map[string]string) briefDocResult {
	out := briefDocResult{Applied: res.Applied, Translations: translations}
	if res.Suggestion != nil {
		out.Suggestion = map[string]any{"id": res.Suggestion.ID, "status": res.Suggestion.Status, "note": "pending: a person accepts it in Phoedox"}
	}
	if res.Draft != nil {
		out.Draft = map[string]any{"note": "written into the open draft; a person publishes it", "updatedAt": res.Draft.UpdatedAt}
	}
	if d := res.Document; d != nil {
		out.Document = &briefDoc{ID: d.ID, ProjectID: d.ProjectID, Type: d.Type, Slug: d.Slug, Title: d.Title, Ref: decisionRef(*d), Version: d.Version}
	}
	return out
}

// docWithTranslations is a list entry with the state of its translations.
type docWithTranslations struct {
	store.Document
	TranslationState map[string]string `json:"translationState,omitempty"`
}

// batchOp is one operation of batch_update.
type batchOp struct {
	Action        string          `json:"action" jsonschema:"update (the main language) or translate (one further language)"`
	Document      string          `json:"document" jsonschema:"document slug, id or decision number"`
	Lang          string          `json:"lang,omitempty" jsonschema:"translate: the language"`
	Title         *string         `json:"title,omitempty"`
	Markdown      *string         `json:"markdown,omitempty" jsonschema:"the whole text; prefer edits for small changes"`
	Edits         []core.TextEdit `json:"edits,omitempty" jsonschema:"{old, new} pairs; old must occur exactly once (field for decisions)"`
	Category      *string         `json:"category,omitempty" jsonschema:"update only"`
	Position      *int            `json:"position,omitempty" jsonschema:"update only"`
	After         string          `json:"after,omitempty" jsonschema:"update only: move right after this page"`
	ExpectVersion int             `json:"expect_version,omitempty" jsonschema:"update only"`
	Context       *string         `json:"context,omitempty" jsonschema:"translate, decisions"`
	Decision      *string         `json:"decision,omitempty" jsonschema:"translate, decisions"`
	Consequences  *string         `json:"consequences,omitempty" jsonschema:"translate, decisions"`
	Alternatives  *string         `json:"alternatives,omitempty" jsonschema:"translate, decisions"`
	Description   *string         `json:"description,omitempty" jsonschema:"translate, links"`
}

type batchIn struct {
	Project    string    `json:"project" jsonschema:"project slug"`
	Operations []batchOp `json:"operations" jsonschema:"up to 100 operations, applied in order"`
}

const batchMax = 100

func (o batchOp) patch() core.DocumentPatch {
	return core.DocumentPatch{Title: o.Title, Markdown: o.Markdown, Edits: o.Edits, Category: o.Category, Position: o.Position, After: o.After, ExpectVersion: o.ExpectVersion}
}

func (o batchOp) translation() core.TranslationInput {
	in := core.TranslationInput{Markdown: o.Markdown, Edits: o.Edits, Context: o.Context, Decision: o.Decision, Consequences: o.Consequences,
		Alternatives: o.Alternatives, Description: o.Description}
	if o.Title != nil {
		in.Title = *o.Title
	}
	return in
}

// batchUpdate checks every operation first and writes only when all are
// valid; then it applies them in order.
func batchUpdate(ctx context.Context, svc *core.Service, a core.Actor, in batchIn) (any, error) {
	if len(in.Operations) == 0 || len(in.Operations) > batchMax {
		return nil, fmt.Errorf("send 1 to %d operations", batchMax)
	}
	seen := map[string]int{}
	for i, o := range in.Operations {
		key := o.Action + "\x00" + o.Document + "\x00" + o.Lang
		if j, ok := seen[key]; ok {
			return nil, fmt.Errorf("operation %d: same document and action as operation %d; combine them into one", i+1, j+1)
		}
		seen[key] = i
		var err error
		switch o.Action {
		case "update":
			if o.Lang != "" {
				err = errors.New("update changes the main language; use action translate with lang")
			} else {
				err = svc.CheckUpdate(ctx, a, in.Project, o.Document, o.patch())
			}
		case "translate":
			if o.Category != nil || o.Position != nil || o.After != "" {
				err = errors.New("category and position belong to the document; change them with an update operation")
			} else {
				err = svc.CheckTranslation(ctx, a, in.Project, o.Document, o.Lang, o.translation())
			}
		default:
			err = errors.New(`action must be "update" or "translate"`)
		}
		if err != nil {
			return nil, fmt.Errorf("operation %d (%s %s): %v; nothing was written", i+1, o.Action, o.Document, friendly(err))
		}
	}
	results := make([]any, 0, len(in.Operations))
	for i, o := range in.Operations {
		var r any
		var err error
		if o.Action == "update" {
			var res core.DocResult
			if res, err = svc.UpdateDocument(ctx, a, in.Project, o.Document, o.patch()); err == nil {
				b := briefResult(res, nil)
				if o.Markdown != nil || len(o.Edits) > 0 {
					checkBlocks(ctx, svc, a, in.Project, res, o.Markdown, &b)
				}
				r = b
			}
		} else {
			var t core.TranslationInfo
			if t, err = svc.SaveTranslation(ctx, a, in.Project, o.Document, o.Lang, o.translation()); err == nil {
				r = map[string]any{"document": o.Document, "lang": o.Lang, "title": t.Title, "sourceVersion": t.SourceVersion, "outdated": t.Outdated, "machine": t.Machine}
			}
		}
		if err != nil {
			return map[string]any{"applied": i, "results": results,
				"error": fmt.Sprintf("operation %d (%s %s) failed after the check: %v; operations before it were written, later ones were not", i+1, o.Action, o.Document, err)}, nil
		}
		results = append(results, r)
	}
	return map[string]any{"applied": len(results), "results": results}, nil
}

type validateBlocksIn struct {
	Project  string  `json:"project" jsonschema:"project slug"`
	Document string  `json:"document,omitempty" jsonschema:"page slug or id to check"`
	Markdown *string `json:"markdown,omitempty" jsonschema:"or: Markdown to check before saving"`
}

type findIn struct {
	Project       string   `json:"project" jsonschema:"project slug"`
	Find          string   `json:"find" jsonschema:"text to find, or a regular expression with regex: true (Go/RE2 syntax)"`
	Regex         bool     `json:"regex,omitempty"`
	CaseSensitive bool     `json:"case_sensitive,omitempty"`
	WholeWord     bool     `json:"whole_word,omitempty"`
	Titles        bool     `json:"titles,omitempty" jsonschema:"also search document titles"`
	Documents     []string `json:"documents,omitempty" jsonschema:"slugs, ids or decision numbers; empty = all documents"`
	Lang          string   `json:"lang,omitempty" jsonschema:"a further language: search its translations instead of the main language"`
}

func (f findIn) input(replace *string, apply bool) core.FindInput {
	return core.FindInput{Find: f.Find, Replace: replace, Regex: f.Regex, CaseSensitive: f.CaseSensitive, WholeWord: f.WholeWord, Titles: f.Titles,
		Documents: f.Documents, Lang: f.Lang, Apply: apply}
}

type replaceIn struct {
	findIn
	Replace  string           `json:"replace" jsonschema:"replacement text; with regex, $1 inserts a group"`
	Apply    bool             `json:"apply,omitempty" jsonschema:"false (default): preview only; true: write the replacement"`
	Only     map[string][]int `json:"only,omitempty" jsonschema:"document id → match numbers from the preview to replace; a document not listed gets all its matches, an empty list none"`
	Versions map[string]int   `json:"versions,omitempty" jsonschema:"document id → version from the preview; documents changed since are skipped"`
}

// whiteboardScreenshotTool draws a whiteboard (or one frame) on the server
// and returns it as an image, so AI clients can check what they drew.
func whiteboardScreenshotTool(s *sdk.Server, svc *core.Service) {
	schema, err := inputSchema[whiteboardScreenshotIn]()
	if err != nil {
		panic(err)
	}
	desc := "See a whiteboard as a picture (PNG), drawn now from its current content like the editor shows it (hand-drawn strokes, the app's fonts and colours), e.g. to check a diagram or prototype screens you drew. frame: only one frame or prototype screen (id or title). Embeds, entries and blocks appear as cards; read exact content with get_whiteboard. HTML prototype screens appear as the browser drew them: pictures are made while someone who may edit the board has it open (the tool waits a few seconds for new ones); without one the screen shows its text and the note says so."
	sdk.AddTool(s, &sdk.Tool{Name: "whiteboard_screenshot", Description: desc, InputSchema: schema}, func(ctx context.Context, req *sdk.CallToolRequest, in whiteboardScreenshotIn) (*sdk.CallToolResult, any, error) {
		a, err := actorFrom(req)
		if err != nil {
			return nil, nil, err
		}
		bp, err := svc.WhiteboardForPicture(ctx, a, in.Project, in.Document)
		if err != nil {
			return nil, nil, friendly(err)
		}
		// HTML screens are drawn by the browser of someone who has the board
		// open: wait a little for pictures of new or changed screens.
		missing := bp.Missing(in.Frame, in.Dark)
		for i := 0; len(missing) > 0 && bp.Viewers > 0 && i < 15; i++ {
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(time.Second):
			}
			missing = bp.Missing(in.Frame, in.Dark)
		}
		wb, title := bp.Board, bp.Title
		pic, info, err := publish.WhiteboardPNG(wb, publish.PNGOptions{Frame: in.Frame, Dark: in.Dark, Image: bp.Image, Screen: bp.Screen})
		if errors.Is(err, publish.ErrNoFrame) {
			return nil, nil, fmt.Errorf("there is no frame %q on %s; get_whiteboard lists the frames", in.Frame, title)
		}
		if err != nil {
			return nil, nil, err
		}
		note := fmt.Sprintf("%s: %d×%d px, board area x %.0f, y %.0f, %.0f×%.0f units (%.2f px per unit).", title, info.Width, info.Height, info.Area[0], info.Area[1], info.Area[2], info.Area[3], info.Scale)
		if len(info.Simplified) > 0 {
			parts := make([]string, 0, len(info.Simplified))
			for k, n := range info.Simplified {
				parts = append(parts, fmt.Sprintf("%d %s", n, k))
			}
			slices.Sort(parts)
			note += " Shown simplified: " + strings.Join(parts, ", ") + "."
		}
		if len(missing) > 0 {
			note += " HTML screens shown only as text (no picture yet: " + strings.Join(missing, ", ") + "). Pictures of HTML screens are made in the browser: ask someone who may edit the board to open it, then take the screenshot again."
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.ImageContent{Data: pic, MIMEType: "image/png"}, &sdk.TextContent{Text: note}}}, nil, nil
	})
}

// appScreenshotTool returns the latest picture of an app as an image, so AI
// clients see how it looks (taken in the browser of someone who may edit it).
func appScreenshotTool(s *sdk.Server, svc *core.Service) {
	schema, err := inputSchema[appScreenshotIn]()
	if err != nil {
		panic(err)
	}
	desc := "See how an app looks: the latest picture of it as PNG, taken when someone who may edit it had it open in the browser after its last change. The note says which version it shows; if it is older than the current one, ask the person to open the app once."
	sdk.AddTool(s, &sdk.Tool{Name: "app_screenshot", Description: desc, InputSchema: schema}, func(ctx context.Context, req *sdk.CallToolRequest, in appScreenshotIn) (*sdk.CallToolResult, any, error) {
		a, err := actorFrom(req)
		if err != nil {
			return nil, nil, err
		}
		pic, err := svc.AppPicture(ctx, a, in.Project, in.Document)
		if err != nil {
			return nil, nil, friendly(err)
		}
		note := fmt.Sprintf("Picture of version %d, taken %s.", pic.Version, pic.At.UTC().Format(time.RFC3339))
		if pic.Version < pic.Current {
			note += fmt.Sprintf(" The app is at version %d now: the picture may be outdated until someone opens the app.", pic.Current)
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.ImageContent{Data: pic.PNG, MIMEType: "image/png"}, &sdk.TextContent{Text: note}}}, nil, nil
	})
}
