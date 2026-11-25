package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Resources are the same reads as the read tools, for clients that attach context instead of calling tools.
// URIs: consigliere://summary, consigliere://status, consigliere://suggestions, consigliere://pursuits/{stage},
// consigliere://pursuit/{id}, consigliere://item/{id}, consigliere://draft/{id}.

var resourceURI = regexp.MustCompile(`^consigliere://(summary|status|suggestions|pursuits/(active|waiting|completed)|(pursuit|item|draft)/(\d+))$`)

// path maps a resource URI to its dashboard API path; ok=false for one that isn't ours.
func path(uri string) (string, url.Values, bool) {
	m := resourceURI.FindStringSubmatch(uri)
	switch {
	case m == nil:
		return "", nil, false
	case m[2] != "":
		return "/api/pursuits", url.Values{"stage": {m[2]}}, true
	case m[3] != "":
		return fmt.Sprintf("/api/%ss/%s", m[3], m[4]), nil, true
	}
	return "/api/" + m[1], nil, true
}

func (s *Server) resources(srv *mcp.Server) {
	read := func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI
		p, q, ok := path(uri)
		if !ok {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		raw, err := s.API.Get(ctx, p, q)
		if err != nil {
			if e := (*APIError)(nil); errors.As(err, &e) && e.Status == 404 {
				return nil, mcp.ResourceNotFoundError(uri)
			}
			return nil, err
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(raw)}}}, nil
	}
	for _, r := range []struct{ name, title, description string }{
		{"summary", "Today at a glance", "Pursuits by stage, what needs doing today, and the funnel."},
		{"status", "Bot status", "Paused or not, each source's last run, and scoring progress."},
		{"suggestions", "Suggested jobs", "The jobs the bot suggests applying to next."},
	} {
		srv.AddResource(&mcp.Resource{URI: "consigliere://" + r.name, Name: r.name, Title: r.title, Description: r.description,
			MIMEType: "application/json"}, read)
	}
	for _, t := range []struct{ template, name, title, description string }{
		{"consigliere://pursuits/{stage}", "pursuits", "Pursuits in a stage", "Pursuits that are active, waiting or completed."},
		{"consigliere://pursuit/{id}", "pursuit", "A pursuit", "One pursuit with its checklist, notes, history and drafts."},
		{"consigliere://item/{id}", "item", "A job", "One job posting with its score, pay and text."},
		{"consigliere://draft/{id}", "draft", "A draft", "One draft's text, or its status while it's being written."},
	} {
		srv.AddResourceTemplate(&mcp.ResourceTemplate{URITemplate: t.template, Name: t.name, Title: t.title, Description: t.description,
			MIMEType: "application/json"}, read)
	}
}

// --- prompts ---

func prompts(srv *mcp.Server) {
	prompt(srv, "triage_options", "Triage today's options",
		"Go through the best new jobs and decide which to pursue.",
		[]*mcp.PromptArgument{{Name: "count", Description: "how many to look at (default 10)"}},
		func(args map[string]string) string {
			count := or(args["count"], "10")
			return "Look at my top " + count + ` job options (search_options). For each, give a one-line verdict: pursue, maybe, or skip, ` +
				`with the reason that matters most (resume match, location, or pay against what I make now). Check get_summary first: ` +
				`if I'm near my cap of active pursuits, be stricter. Don't make anything active or rate anything until I say which.`
		})
	prompt(srv, "prep_interview", "Prep for an interview",
		"Get ready for a scheduled interview on a pursuit.",
		[]*mcp.PromptArgument{{Name: "pursuit_id", Description: "the pursuit with the interview", Required: true}},
		func(args map[string]string) string {
			return "Help me prepare for the interview on pursuit " + args["pursuit_id"] + `. Read it (get_pursuit) and its job ` +
				`(get_item), including notes about who I'm meeting. Give me: what the team does and why they're hiring, the ` +
				`three stories from my background that fit best, the questions they're likely to ask, and five sharp questions ` +
				`for them. If there's no prep draft yet, offer to write one (write_draft, kind prep).`
		})
	prompt(srv, "weekly_review", "Weekly review",
		"Walk through every open pursuit and what to do next.",
		nil,
		func(map[string]string) string {
			return `Do my weekly job-search review. Read get_summary, then the active and waiting pursuits (list_pursuits). ` +
				`For each: where it stands, the one next step, and whether it's gone quiet long enough to follow up or let go. ` +
				`End with the three things to do this week. Ask before changing anything.`
		})
	prompt(srv, "weigh_offer", "Weigh an offer",
		"Think through an offer against staying.",
		[]*mcp.PromptArgument{{Name: "pursuit_id", Description: "the pursuit with the offer", Required: true}},
		func(args map[string]string) string {
			return "Help me weigh the offer on pursuit " + args["pursuit_id"] + `. Read the comparison (list_offers): the yearly ` +
				`difference, what leaving gives up, and the open questions. Tell me what's strong, what's weak, what to ask ` +
				`before deciding, and what to negotiate, with numbers. If no offer is pasted yet, ask me for it.`
		})
}

func prompt(srv *mcp.Server, name, title, description string, args []*mcp.PromptArgument, text func(map[string]string) string) {
	srv.AddPrompt(&mcp.Prompt{Name: name, Title: title, Description: description, Arguments: args},
		func(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			values := map[string]string{}
			if req.Params != nil {
				for k, v := range req.Params.Arguments {
					values[k] = strings.TrimSpace(v)
				}
			}
			for _, a := range args {
				if a.Required && values[a.Name] == "" {
					return nil, fmt.Errorf("%s is required", a.Name)
				}
			}
			return &mcp.GetPromptResult{Description: description,
				Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: text(values)}}}}, nil
		})
}

func or(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
