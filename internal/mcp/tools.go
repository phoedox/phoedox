package mcp

import (
	"context"
	"encoding/json"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/phoedox/phoedox/internal/core"
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
	Assignee string `json:"assignee,omitempty" jsonschema:"me, a user id or an e-mail"`
	Open     bool   `json:"open,omitempty" jsonschema:"only tasks that are not done"`
}

type createTaskIn struct {
	Project     string   `json:"project" jsonschema:"project slug"`
	Board       string   `json:"board" jsonschema:"board slug or id (a document of type board)"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty" jsonschema:"Markdown; @mentions allowed"`
	Column      string   `json:"column,omitempty" jsonschema:"column id or name; default: first column"`
	Assignee    string   `json:"assignee,omitempty" jsonschema:"user id or e-mail of a project member"`
	Due         string   `json:"due,omitempty" jsonschema:"YYYY-MM-DD"`
	Priority    int      `json:"priority,omitempty" jsonschema:"0 none, 1 low, 2 medium, 3 high"`
	Labels      []string `json:"labels,omitempty"`
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

type createBoardIn struct {
	Project     string             `json:"project" jsonschema:"project slug"`
	Title       string             `json:"title"`
	Category    string             `json:"category,omitempty"`
	Description string             `json:"description,omitempty" jsonschema:"Markdown shown above the board"`
	Columns     []core.BoardColumn `json:"columns,omitempty" jsonschema:"columns in order; mark the last one done=true. Default: To do, In progress, Done"`
}

type getDocIn struct {
	docRef
	Lang string `json:"lang,omitempty" jsonschema:"language of a multilingual project (e.g. de); empty = main language"`
}

type translateIn struct {
	docRef
	Lang         string  `json:"lang" jsonschema:"one of the project's further languages (see get_project: locales)"`
	Title        string  `json:"title"`
	Markdown     *string `json:"markdown,omitempty" jsonschema:"pages: the whole translated Markdown; keep @mentions, links, code and {{variables}} unchanged"`
	Context      *string `json:"context,omitempty" jsonschema:"decisions: translated context"`
	Decision     *string `json:"decision,omitempty" jsonschema:"decisions: translated decision"`
	Consequences *string `json:"consequences,omitempty"`
	Alternatives *string `json:"alternatives,omitempty"`
	Description  *string `json:"description,omitempty" jsonschema:"links: translated description"`
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

type createProjectIn struct {
	Name         string   `json:"name"`
	Slug         string   `json:"slug,omitempty" jsonschema:"optional, derived from name"`
	Description  string   `json:"description,omitempty"`
	PrimaryColor string   `json:"primary_color,omitempty" jsonschema:"hex color like #F26A1B"`
	AIWriteMode  string   `json:"ai_write_mode,omitempty" jsonschema:"suggest (default) or direct"`
	Features     []string `json:"features,omitempty" jsonschema:"optional tabs to switch on: decisions, graph, model, files (default none; pages and boards always exist)"`
}

type updateProjectIn struct {
	Project      string    `json:"project" jsonschema:"project slug"`
	Name         *string   `json:"name,omitempty"`
	Description  *string   `json:"description,omitempty"`
	PrimaryColor *string   `json:"primary_color,omitempty"`
	AIWriteMode  *string   `json:"ai_write_mode,omitempty" jsonschema:"suggest or direct (owner only)"`
	MainLocale   *string   `json:"main_locale,omitempty" jsonschema:"main language of the content, e.g. en (makes the project multilingual)"`
	Locales      *[]string `json:"locales,omitempty" jsonschema:"further languages with translations, e.g. [de]"`
	Features     *[]string `json:"features,omitempty" jsonschema:"optional tabs shown in the web app: decisions, graph, model, files (the full list replaces the old one)"`
}

type listDocsIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Type     string `json:"type,omitempty" jsonschema:"markdown, adr or link"`
	Category string `json:"category,omitempty"`
}

type createDocIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Title    string `json:"title"`
	Markdown string `json:"markdown" jsonschema:"page content in Markdown; mention entities with @slug"`
	Category string `json:"category,omitempty" jsonschema:"sidebar group, e.g. Architektur"`
	Slug     string `json:"slug,omitempty" jsonschema:"optional, derived from title"`
	Position int    `json:"position,omitempty" jsonschema:"order within the category"`
}

type updateDocIn struct {
	Project       string  `json:"project" jsonschema:"project slug"`
	Document      string  `json:"document" jsonschema:"document slug or id"`
	Title         *string `json:"title,omitempty"`
	Markdown      *string `json:"markdown,omitempty" jsonschema:"replaces the whole markdown content"`
	Category      *string `json:"category,omitempty"`
	Slug          *string `json:"slug,omitempty"`
	Position      *int    `json:"position,omitempty"`
	ExpectVersion int     `json:"expect_version,omitempty" jsonschema:"fail if the document changed since this version"`
}

type decisionFields struct {
	Status       string   `json:"status,omitempty" jsonschema:"proposed, accepted, rejected, deprecated or superseded"`
	Date         string   `json:"date,omitempty" jsonschema:"YYYY-MM-DD"`
	Deciders     []string `json:"deciders,omitempty"`
	Context      string   `json:"context,omitempty" jsonschema:"situation and forces, Markdown"`
	Decision     string   `json:"decision,omitempty" jsonschema:"what was decided, Markdown"`
	Consequences string   `json:"consequences,omitempty" jsonschema:"Markdown"`
	Alternatives string   `json:"alternatives,omitempty" jsonschema:"considered options, Markdown"`
	Supersedes   string   `json:"supersedes,omitempty" jsonschema:"slug or number (ADR-0003) of the decision this one replaces; it is marked superseded automatically"`
}

func (f decisionFields) adr() core.ADR {
	return core.ADR{Status: f.Status, Date: f.Date, Deciders: f.Deciders, Context: f.Context, Decision: f.Decision,
		Consequences: f.Consequences, Alternatives: f.Alternatives, Supersedes: f.Supersedes}
}

type listDecisionsIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	Status  string `json:"status,omitempty" jsonschema:"only decisions with this status: proposed, accepted, rejected, deprecated or superseded"`
	Query   string `json:"query,omitempty" jsonschema:"only decisions whose number, title or text contain all these words"`
}

type createDecisionIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Title    string `json:"title"`
	Category string `json:"category,omitempty" jsonschema:"defaults to Entscheidungen"`
	Slug     string `json:"slug,omitempty"`
	decisionFields
}

type updateDecisionIn struct {
	Project       string  `json:"project" jsonschema:"project slug"`
	Document      string  `json:"document" jsonschema:"decision slug, id or number like ADR-0003"`
	Title         *string `json:"title,omitempty"`
	Category      *string `json:"category,omitempty"`
	ExpectVersion int     `json:"expect_version,omitempty"`
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

type listFilesIn struct {
	Project  string `json:"project" jsonschema:"project slug"`
	Document string `json:"document,omitempty" jsonschema:"only files attached to this document (slug)"`
	Entity   string `json:"entity,omitempty" jsonschema:"only files attached to this entity (slug)"`
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
	Attrs   map[string]any `json:"attrs,omitempty"`
}

type unlinkIn struct {
	Project string `json:"project" jsonschema:"project slug"`
	From    string `json:"from"`
	Rel     string `json:"rel,omitempty" jsonschema:"empty removes all relations between the two"`
	To      string `json:"to"`
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
	Categories         map[string][]docSummary `json:"documentsByCategory"`
	EntityKinds        map[string]int          `json:"entityKinds"`
	PendingSuggestions int                     `json:"pendingSuggestions"`
	Hint               string                  `json:"hint,omitempty"`
}

type docSummary struct {
	Ref     string `json:"ref,omitempty"` // ADR-0003 for decisions
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
	tool(s, "get_project", "Overview of a project: settings, documents grouped by category, entity kinds, pending suggestions. Start here.", func(ctx context.Context, a core.Actor, in projectRef) (any, error) {
		p, err := svc.Project(ctx, a, in.Project)
		if err != nil {
			return nil, err
		}
		docs, err := svc.Documents(ctx, a, p.Slug, store.DocFilter{})
		if err != nil {
			return nil, err
		}
		ov := projectOverview{Project: p, Categories: map[string][]docSummary{}, EntityKinds: map[string]int{}}
		for _, d := range docs {
			cat := d.Category
			if cat == "" {
				cat = "(ohne Kategorie)"
			}
			ov.Categories[cat] = append(ov.Categories[cat], docSummary{Ref: decisionRef(d), Slug: d.Slug, Title: d.Title, Type: d.Type, Version: d.Version})
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
		if p.AIWriteMode == "suggest" {
			ov.Hint = "Writes in this project become suggestions that a human must accept."
		}
		return ov, nil
	})
	tool(s, "create_project", "Create a new project. You become its owner.", func(ctx context.Context, a core.Actor, in createProjectIn) (any, error) {
		return svc.CreateProject(ctx, a, core.ProjectInput{Name: in.Name, Slug: in.Slug, Description: in.Description, PrimaryColor: in.PrimaryColor, AIWriteMode: in.AIWriteMode, Features: &in.Features})
	})
	tool(s, "update_project_settings", "Change name, description, color, AI write mode, languages or optional tabs (features) of a project (owner only).", func(ctx context.Context, a core.Actor, in updateProjectIn) (any, error) {
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
		return svc.UpdateProject(ctx, a, p.Slug, upd)
	})
	tool(s, "list_documents", "List documents of a project (without content), optionally filtered by type or category.", func(ctx context.Context, a core.Actor, in listDocsIn) (any, error) {
		return svc.Documents(ctx, a, in.Project, store.DocFilter{Type: in.Type, Category: in.Category})
	})
	tool(s, "get_document", "Read a document with its full content (the published version). If the page has an open draft, it is returned as \"draft\" too – your writes go into it. In multilingual projects, lang picks a translation; \"translation.sourceVersion\" lower than \"version\" means the translation is outdated.", func(ctx context.Context, a core.Actor, in getDocIn) (any, error) {
		d, err := svc.LocalizedDocument(ctx, a, in.Project, in.Document, in.Lang)
		if err != nil {
			return nil, err
		}
		v := view(d)
		if dr, err := svc.Draft(ctx, a, in.Project, d.ID); err == nil {
			return map[string]any{"document": v, "draft": map[string]any{
				"title": dr.Title, "category": dr.Category, "markdown": dr.Markdown, "updatedAt": dr.UpdatedAt,
				"participants": dr.Participants, "note": "Unpublished draft. A person publishes it in Phoedox.",
			}}, nil
		}
		return v, nil
	})
	tool(s, "translate_document", "Write the translation of a document into one of the project's further languages (multilingual projects, see get_project). Read the main language with get_document first. The translation refers to the current main version and is marked as machine translation until a person reviews it. Number, status, slug and category stay shared.", func(ctx context.Context, a core.Actor, in translateIn) (any, error) {
		return svc.SaveTranslation(ctx, a, in.Project, in.Document, in.Lang, core.TranslationInput{Title: in.Title, Markdown: in.Markdown,
			Context: in.Context, Decision: in.Decision, Consequences: in.Consequences, Alternatives: in.Alternatives, Description: in.Description})
	})
	tool(s, "list_tasks", "List tasks (cards on boards) of a project, optionally of one board, column or assignee. Boards are documents of type board; their columns are in the board's content.", func(ctx context.Context, a core.Actor, in listTasksIn) (any, error) {
		return svc.Tasks(ctx, a, in.Project, in.Board, in.Column, in.Assignee, in.Open)
	})
	tool(s, "save_floorplan", "Create or replace a floor plan (document type floorplan). Read it first with get_document when replacing, and create the room and device entries before referencing them.", func(ctx context.Context, a core.Actor, in saveFloorplanIn) (any, error) {
		if in.Document != "" {
			return svc.UpdateDocument(ctx, a, in.Project, in.Document, core.DocumentPatch{Floorplan: &in.Floorplan})
		}
		return svc.CreateDocument(ctx, a, in.Project, core.DocumentInput{Type: store.DocFloorplan, Title: in.Title, Category: in.Category, Floorplan: &in.Floorplan})
	})
	tool(s, "create_board", "Create a task board (a document of type board with columns).", func(ctx context.Context, a core.Actor, in createBoardIn) (any, error) {
		return svc.CreateDocument(ctx, a, in.Project, core.DocumentInput{Type: store.DocBoard, Title: in.Title, Category: in.Category, Board: &core.Board{Description: in.Description, Columns: in.Columns}})
	})
	tool(s, "create_task", "Add a task to a board. Check list_tasks first to avoid duplicates.", func(ctx context.Context, a core.Actor, in createTaskIn) (any, error) {
		return svc.CreateTask(ctx, a, in.Project, in.Board, core.TaskInput{Title: in.Title, Description: in.Description, Column: in.Column, Assignee: in.Assignee, Due: in.Due, Priority: in.Priority, Labels: in.Labels})
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
		return svc.CreateDocument(ctx, a, in.Project, core.DocumentInput{Type: store.DocMarkdown, Title: in.Title, Slug: in.Slug, Category: in.Category, Position: in.Position, Markdown: in.Markdown})
	})
	tool(s, "update_document", "Update a Markdown page. Only given fields change; markdown replaces the whole content. If the page has an open draft (someone is editing it), the change goes into the draft and is published together with it by a person; the result then contains \"draft\".", func(ctx context.Context, a core.Actor, in updateDocIn) (any, error) {
		return svc.UpdateDocument(ctx, a, in.Project, in.Document, core.DocumentPatch{Title: in.Title, Markdown: in.Markdown, Category: in.Category, Slug: in.Slug, Position: in.Position, ExpectVersion: in.ExpectVersion})
	})
	tool(s, "create_decision", "Record a decision (ADR) with context, decision, consequences and alternatives.", func(ctx context.Context, a core.Actor, in createDecisionIn) (any, error) {
		if in.Category == "" {
			in.Category = "Entscheidungen"
		}
		adr := in.adr()
		return svc.CreateDocument(ctx, a, in.Project, core.DocumentInput{Type: store.DocADR, Title: in.Title, Slug: in.Slug, Category: in.Category, ADR: &adr})
	})
	tool(s, "list_decisions", "The decision log of a project: number (ADR-0003), title, status, date and which decision supersedes which, ordered by number.", func(ctx context.Context, a core.Actor, in listDecisionsIn) (any, error) {
		return svc.Decisions(ctx, a, in.Project, in.Status, in.Query)
	})
	tool(s, "update_decision", "Update a decision. Given fields replace the current ones, others stay.", func(ctx context.Context, a core.Actor, in updateDecisionIn) (any, error) {
		cur, err := svc.Document(ctx, a, in.Project, in.Document)
		if err != nil {
			return nil, err
		}
		var adr core.ADR
		_ = json.Unmarshal(cur.Content, &adr)
		f := in.decisionFields
		setIf(&adr.Status, &f.Status)
		setIf(&adr.Date, &f.Date)
		setIf(&adr.Context, &f.Context)
		setIf(&adr.Decision, &f.Decision)
		setIf(&adr.Consequences, &f.Consequences)
		setIf(&adr.Alternatives, &f.Alternatives)
		setIf(&adr.Supersedes, &f.Supersedes)
		if len(f.Deciders) > 0 {
			adr.Deciders = f.Deciders
		}
		return svc.UpdateDocument(ctx, a, in.Project, cur.ID, core.DocumentPatch{Title: in.Title, Category: in.Category, ADR: &adr, ExpectVersion: in.ExpectVersion})
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
	tool(s, "search", "Search documents and entities in one or all visible projects.", func(ctx context.Context, a core.Actor, in searchIn) (any, error) {
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
	tool(s, "upsert_entity", "Create an entity or update it if the slug exists.", func(ctx context.Context, a core.Actor, in upsertEntityIn) (any, error) {
		return svc.UpsertEntity(ctx, a, in.Project, core.EntityInput{Kind: in.Kind, Name: in.Name, Slug: in.Slug, Attrs: in.Attrs})
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
	tool(s, "diagnose", "Check the network path of a device (or of every device in a room) towards the internet along uplink, wired_to and runs_on, with a status per station from agent reports and state attributes. Use it first when someone says something does not work.", func(ctx context.Context, a core.Actor, in entityRefIn) (any, error) {
		return svc.Diagnose(ctx, a, in.Project, in.Entity)
	})
	tool(s, "list_suggestions", "List suggestions (pending AI changes) of a project.", func(ctx context.Context, a core.Actor, in suggestionsIn) (any, error) {
		return svc.Suggestions(ctx, a, in.Project, in.Status)
	})
}

func setIf(dst *string, v *string) {
	if v != nil && *v != "" {
		*dst = *v
	}
}
