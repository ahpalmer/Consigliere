package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server builds the MCP server for one bot.
type Server struct {
	API *API
	// How often a draft being written is checked, and how long a tool waits for it before handing back its id.
	// Zero: every 3 seconds, for up to 4 minutes.
	PollEvery, MaxWait time.Duration
}

const instructions = `Consigliere is the owner's job-search agent. Options are scored job postings (fit 0-100: the resume match
blended with location and pay against what the owner makes now). A pursuit is a job the owner is going after; it moves
Active → Waiting (applied) → Completed. Drafts (apply kits, cover letters, interview prep, thank-you notes) are written
by the bot and are never sent anywhere: the owner sends everything themselves. Ids are the bot's own numbers.`

// MCP returns the server with every tool, resource and prompt registered.
func (s *Server) MCP(version string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "consigliere", Title: "Consigliere", Version: version},
		&mcp.ServerOptions{Instructions: instructions})
	s.readTools(srv)
	s.writeTools(srv)
	s.resources(srv)
	prompts(srv)
	return srv
}

// --- inputs ---

type noInput struct{}

type itemInput struct {
	ItemID int64 `json:"item_id" jsonschema:"the job's id"`
}

type pursuitInput struct {
	PursuitID int64 `json:"pursuit_id" jsonschema:"the pursuit's id"`
}

type draftInput struct {
	DraftID int64 `json:"draft_id" jsonschema:"the draft's id"`
}

type searchInput struct {
	Query    string `json:"query,omitempty" jsonschema:"words to match in the title, company or location"`
	Tier     string `json:"tier,omitempty" jsonschema:"a location tier from the owner's preferences, e.g. local or remote"`
	Category string `json:"category,omitempty" jsonschema:"a category from the owner's preferences"`
	MinScore *int   `json:"min_score,omitempty" jsonschema:"lowest fit to include, 0-100; default the dashboard's"`
	Pay      string `json:"pay,omitempty" jsonschema:"listed: lists pay; reach: the top of the range reaches total pay now; beats: the whole range is above it"`
	Limit    int    `json:"limit,omitempty" jsonschema:"how many to return, best first; default 25"`
}

type stageInput struct {
	Stage string `json:"stage" jsonschema:"active, waiting or completed"`
}

// --- read tools ---

func (s *Server) readTools(srv *mcp.Server) {
	readTool(srv, "get_status", "Bot status",
		"Whether the bot is paused, each source's last run, and scoring progress.",
		func(ctx context.Context, _ noInput) (json.RawMessage, error) {
			return s.API.Get(ctx, "/api/status", nil)
		})
	readTool(srv, "get_summary", "Today at a glance",
		"Pursuits by stage, what needs doing today (follow-ups, interviews, steps), and the funnel: applied, interviewed, offers.",
		func(ctx context.Context, _ noInput) (json.RawMessage, error) {
			return s.API.Get(ctx, "/api/summary", nil)
		})
	readTool(srv, "search_options", "Search job options",
		"Scored job postings, best fit first. Each has its fit and how it's made up, why it scored that way, its listed pay "+
			"against what the owner makes now, and its pursuit if there is one.",
		func(ctx context.Context, in searchInput) (json.RawMessage, error) {
			q := url.Values{}
			set := func(k, v string) {
				if v != "" {
					q.Set(k, v)
				}
			}
			set("q", in.Query)
			set("tier", in.Tier)
			set("category", in.Category)
			set("pay", in.Pay)
			if in.MinScore != nil {
				q.Set("minScore", strconv.Itoa(*in.MinScore))
			}
			limit := in.Limit
			if limit <= 0 {
				limit = 25
			}
			q.Set("limit", strconv.Itoa(limit))
			return s.API.Get(ctx, "/api/options", q)
		})
	readTool(srv, "get_item", "Get a job",
		"One job posting: its score and reasons, pay, the posting text, drafts written for it, and company research links.",
		func(ctx context.Context, in itemInput) (json.RawMessage, error) {
			return s.API.Get(ctx, fmt.Sprintf("/api/items/%d", in.ItemID), nil)
		})
	readTool(srv, "list_pursuits", "List pursuits",
		"Pursuits in one stage: active (preparing), waiting (applied, waiting to hear) or completed.",
		func(ctx context.Context, in stageInput) (json.RawMessage, error) {
			return s.API.Get(ctx, "/api/pursuits", url.Values{"stage": {in.Stage}})
		})
	readTool(srv, "get_pursuit", "Get a pursuit",
		"One pursuit: its checklist, dates, notes, history, drafts, and people the owner knows at the company.",
		func(ctx context.Context, in pursuitInput) (json.RawMessage, error) {
			return s.API.Get(ctx, fmt.Sprintf("/api/pursuits/%d", in.PursuitID), nil)
		})
	readTool(srv, "get_suggestions", "Suggested jobs",
		"The jobs the bot suggests applying to next, high and medium, with apply kits where they're ready.",
		func(ctx context.Context, _ noInput) (json.RawMessage, error) {
			return s.API.Get(ctx, "/api/suggestions", nil)
		})
	readTool(srv, "get_draft", "Get a draft",
		"One draft (apply kit, cover letter, interview prep or thank-you note) with its text, or its status while it's being written.",
		func(ctx context.Context, in draftInput) (json.RawMessage, error) {
			return s.API.Get(ctx, fmt.Sprintf("/api/drafts/%d", in.DraftID), nil)
		})
	readTool(srv, "list_offers", "Offers on a pursuit",
		"Offers pasted into a pursuit, each compared with the owner's current pay year by year: totals, what leaving gives "+
			"up, and questions to ask.",
		func(ctx context.Context, in pursuitInput) (json.RawMessage, error) {
			return s.API.Get(ctx, fmt.Sprintf("/api/pursuits/%d/offers", in.PursuitID), nil)
		})
}

// --- helpers ---

var (
	no  = false
	yes = true
)

// readTool registers a tool that only reads.
func readTool[In any](srv *mcp.Server, name, title, description string, call func(context.Context, In) (json.RawMessage, error)) {
	mcp.AddTool(srv, &mcp.Tool{Name: name, Title: title, Description: description,
		Annotations: &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: &no}},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			return result(call(ctx, in))
		})
}

// hints describe a tool that changes something.
type hints struct {
	// Destructive: replaces or closes something (undoable at most by hand); otherwise the tool only adds.
	destructive bool
	// Idempotent: calling it again with the same input changes nothing more.
	idempotent bool
}

func writeTool[In any](srv *mcp.Server, name, title, description string, h hints,
	call func(context.Context, *mcp.CallToolRequest, In) (json.RawMessage, error)) {
	destructive := h.destructive
	mcp.AddTool(srv, &mcp.Tool{Name: name, Title: title, Description: description,
		Annotations: &mcp.ToolAnnotations{Title: title, DestructiveHint: &destructive, IdempotentHint: h.idempotent, OpenWorldHint: &no}},
		func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			return result(call(ctx, req, in))
		})
}

// result is the API's JSON as the tool's text, or the dashboard's refusal as a tool error the model can read.
func result(raw json.RawMessage, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}, nil, nil
}
