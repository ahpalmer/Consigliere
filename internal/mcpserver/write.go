package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type activateInput struct {
	ItemID  *int64 `json:"item_id,omitempty" jsonschema:"a job from options; or give company and title for one found elsewhere"`
	Company string `json:"company,omitempty" jsonschema:"for a job that isn't in options"`
	Title   string `json:"title,omitempty" jsonschema:"for a job that isn't in options"`
	URL     string `json:"url,omitempty" jsonschema:"the posting's link, for a job that isn't in options"`
}

type rateInput struct {
	ItemID int64  `json:"item_id" jsonschema:"the job's id"`
	Rating string `json:"rating" jsonschema:"up or down; teaches the bot what the owner wants"`
}

type pasteInput struct {
	ItemID int64  `json:"item_id" jsonschema:"the job's id"`
	Text   string `json:"text" jsonschema:"the posting's full text, as copied from the job's page"`
}

type stepInput struct {
	PursuitID int64  `json:"pursuit_id" jsonschema:"the pursuit's id"`
	Step      string `json:"step" jsonschema:"resume (cover letter), people, outreach, submit, followup, prep, interview or thanks"`
	Done      bool   `json:"done" jsonschema:"true to tick it, false to untick it"`
}

type appliedInput struct {
	PursuitID *int64 `json:"pursuit_id,omitempty" jsonschema:"the pursuit that was applied to"`
	ItemID    *int64 `json:"item_id,omitempty" jsonschema:"or a job with no pursuit yet: it becomes one, already applied"`
}

type interviewInput struct {
	PursuitID int64  `json:"pursuit_id" jsonschema:"the pursuit's id"`
	At        string `json:"at" jsonschema:"when, as 2026-11-03T14:00 in the owner's time zone, or RFC 3339"`
	Note      string `json:"note,omitempty" jsonschema:"who, format, anything to remember"`
}

type completeInput struct {
	PursuitID int64  `json:"pursuit_id" jsonschema:"the pursuit's id"`
	Outcome   string `json:"outcome" jsonschema:"offer, rejected, ghosted, withdrawn or dropped"`
	Note      string `json:"note,omitempty" jsonschema:"why, or what was learned"`
}

type noteInput struct {
	PursuitID int64  `json:"pursuit_id" jsonschema:"the pursuit's id"`
	Text      string `json:"text" jsonschema:"the note"`
}

type writeDraftInput struct {
	Kind      string `json:"kind" jsonschema:"kit (resume and answers), cover (cover letter), prep (interview prep) or thanks (thank-you note)"`
	PursuitID *int64 `json:"pursuit_id,omitempty" jsonschema:"the pursuit to write for; needed for prep and thanks"`
	ItemID    *int64 `json:"item_id,omitempty" jsonschema:"or a job with no pursuit, for kit or cover"`
	Notes     string `json:"notes,omitempty" jsonschema:"what to stress or include, e.g. who the interviewers are"`
}

type offerInput struct {
	PursuitID int64  `json:"pursuit_id" jsonschema:"the pursuit the offer is for"`
	Text      string `json:"text" jsonschema:"the offer letter, the recruiter's email, or notes from the call"`
}

func (s *Server) writeTools(srv *mcp.Server) {
	writeTool(srv, "make_active", "Make a job active",
		"Start a pursuit for a job: from options by item_id, or one found elsewhere by company and title. Making the same "+
			"option active twice returns the pursuit it already has.",
		hints{idempotent: true},
		func(ctx context.Context, _ *mcp.CallToolRequest, in activateInput) (json.RawMessage, error) {
			return s.API.Do(ctx, http.MethodPost, "/api/pursuits", map[string]any{"itemId": in.ItemID, "company": in.Company, "title": in.Title, "url": in.URL})
		})
	writeTool(srv, "rate_job", "Rate a job",
		"Thumbs up or down on a job, as in Telegram and the dashboard. A later rating replaces the earlier one.",
		hints{idempotent: true},
		func(ctx context.Context, _ *mcp.CallToolRequest, in rateInput) (json.RawMessage, error) {
			value := map[string]int{"up": 1, "down": -1}[in.Rating]
			if value == 0 {
				return nil, errors.New(`rating must be "up" or "down"`)
			}
			return s.API.Do(ctx, http.MethodPut, fmt.Sprintf("/api/items/%d/feedback", in.ItemID), map[string]int{"value": value})
		})
	writeTool(srv, "paste_posting", "Paste a full posting",
		"Replace a job's text (alert emails carry only a snippet) with the full posting and re-score it at once; a pay range "+
			"in the text replaces the listed pay. Spends a model call; refused while the bot is paused.",
		hints{destructive: true, idempotent: true},
		func(ctx context.Context, _ *mcp.CallToolRequest, in pasteInput) (json.RawMessage, error) {
			return s.API.Do(ctx, http.MethodPost, fmt.Sprintf("/api/items/%d/posting", in.ItemID), map[string]string{"text": in.Text})
		})
	writeTool(srv, "set_step", "Tick a checklist step",
		"Tick or untick one step on a pursuit's checklist.",
		hints{idempotent: true},
		func(ctx context.Context, _ *mcp.CallToolRequest, in stepInput) (json.RawMessage, error) {
			method := http.MethodPut
			if !in.Done {
				method = http.MethodDelete
			}
			return s.API.Do(ctx, method, fmt.Sprintf("/api/pursuits/%d/steps/%s", in.PursuitID, in.Step), nil)
		})
	writeTool(srv, "mark_applied", "Mark applied",
		"Record that the owner submitted an application: the pursuit moves to Waiting and follow-ups are scheduled. Only "+
			"after the owner says they applied; the bot never applies for them.",
		hints{idempotent: true},
		func(ctx context.Context, _ *mcp.CallToolRequest, in appliedInput) (json.RawMessage, error) {
			switch {
			case in.PursuitID != nil:
				return s.API.Do(ctx, http.MethodPost, fmt.Sprintf("/api/pursuits/%d/applied", *in.PursuitID), nil)
			case in.ItemID != nil:
				return s.API.Do(ctx, http.MethodPost, fmt.Sprintf("/api/items/%d/applied", *in.ItemID), nil)
			}
			return nil, errors.New("give a pursuit_id or an item_id")
		})
	pursuitPost(s, srv, "record_follow_up", "Record a follow-up",
		"Record that the owner followed up on a waiting application; the next follow-up is scheduled from today.", "followed-up")
	pursuitPost(s, srv, "keep_waiting", "Keep waiting",
		"Keep a quiet application open instead of calling it ghosted; the clock restarts.", "keep-waiting")
	pursuitPost(s, srv, "reopen_pursuit", "Reopen a pursuit",
		"Bring a completed pursuit back to Active, clearing its outcome.", "reopen")
	writeTool(srv, "schedule_interview", "Schedule an interview",
		"Record an interview's time; it unlocks the prep and thank-you steps and shows in the morning briefing.",
		hints{idempotent: true},
		func(ctx context.Context, _ *mcp.CallToolRequest, in interviewInput) (json.RawMessage, error) {
			return s.API.Do(ctx, http.MethodPost, fmt.Sprintf("/api/pursuits/%d/interview", in.PursuitID), map[string]string{"at": in.At, "note": in.Note})
		})
	writeTool(srv, "complete_pursuit", "Complete a pursuit",
		"Close a pursuit with its outcome. Confirm with the owner first; reopen_pursuit undoes it.",
		hints{destructive: true},
		func(ctx context.Context, _ *mcp.CallToolRequest, in completeInput) (json.RawMessage, error) {
			return s.API.Do(ctx, http.MethodPost, fmt.Sprintf("/api/pursuits/%d/complete", in.PursuitID), map[string]string{"outcome": in.Outcome, "note": in.Note})
		})
	writeTool(srv, "add_note", "Add a note",
		"Add a dated note to a pursuit's history.",
		hints{},
		func(ctx context.Context, _ *mcp.CallToolRequest, in noteInput) (json.RawMessage, error) {
			return s.API.Do(ctx, http.MethodPost, fmt.Sprintf("/api/pursuits/%d/notes", in.PursuitID), map[string]string{"text": in.Text})
		})
	writeTool(srv, "write_draft", "Write a draft",
		"Have the bot write an apply kit, cover letter, interview prep or thank-you note, and wait for it (about a minute; "+
			"progress is reported). Drafts are never sent: the owner sends everything. Spends model calls; refused while paused.",
		hints{},
		func(ctx context.Context, req *mcp.CallToolRequest, in writeDraftInput) (json.RawMessage, error) {
			raw, err := s.startDraft(ctx, in)
			if err != nil {
				return nil, err
			}
			return s.waitForDraft(ctx, req, raw)
		})
	writeTool(srv, "compare_offer", "Compare an offer",
		"Read an offer (letter, recruiter email or call notes) into a package and compare it with the owner's current pay, "+
			"year by year. Spends a model call; refused while paused.",
		hints{},
		func(ctx context.Context, _ *mcp.CallToolRequest, in offerInput) (json.RawMessage, error) {
			return s.API.Do(ctx, http.MethodPost, fmt.Sprintf("/api/pursuits/%d/offers", in.PursuitID), map[string]string{"text": in.Text})
		})
	writeTool(srv, "pause_bot", "Pause the bot",
		"The kill switch: stops every model call (scoring, drafts, mail reading) until resumed.",
		hints{idempotent: true},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (json.RawMessage, error) {
			return s.API.Do(ctx, http.MethodPost, "/api/pause", nil)
		})
	writeTool(srv, "resume_bot", "Resume the bot",
		"Turn the kill switch back off. Only when the owner asks.",
		hints{idempotent: true},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (json.RawMessage, error) {
			return s.API.Do(ctx, http.MethodPost, "/api/resume", nil)
		})
}

// pursuitPost registers a no-argument action on one pursuit.
func pursuitPost(s *Server, srv *mcp.Server, name, title, description, action string) {
	writeTool(srv, name, title, description, hints{},
		func(ctx context.Context, _ *mcp.CallToolRequest, in pursuitInput) (json.RawMessage, error) {
			return s.API.Do(ctx, http.MethodPost, fmt.Sprintf("/api/pursuits/%d/%s", in.PursuitID, action), nil)
		})
}

func (s *Server) startDraft(ctx context.Context, in writeDraftInput) (json.RawMessage, error) {
	switch {
	case in.PursuitID != nil:
		return s.API.Do(ctx, http.MethodPost, fmt.Sprintf("/api/pursuits/%d/drafts", *in.PursuitID), map[string]string{"kind": in.Kind, "notes": in.Notes})
	case in.ItemID != nil && in.Kind == "cover":
		return s.API.Do(ctx, http.MethodPost, fmt.Sprintf("/api/items/%d/cover", *in.ItemID), map[string]string{"instructions": in.Notes})
	case in.ItemID != nil && in.Kind == "kit":
		return s.API.Do(ctx, http.MethodPost, fmt.Sprintf("/api/items/%d/kit", *in.ItemID), nil)
	case in.ItemID != nil:
		return nil, errors.New("prep and thank-you notes are for a pursuit: give its pursuit_id")
	}
	return nil, errors.New("give a pursuit_id or an item_id")
}

// waitForDraft checks a started draft until it's written or failed, reporting progress when the client asked for
// it. A draft still being written after MaxWait comes back as is, for get_draft later.
func (s *Server) waitForDraft(ctx context.Context, req *mcp.CallToolRequest, raw json.RawMessage) (json.RawMessage, error) {
	every, limit := s.PollEvery, s.MaxWait
	if every <= 0 {
		every = 3 * time.Second
	}
	if limit <= 0 {
		limit = 4 * time.Minute
	}
	var token any
	if req != nil && req.Params != nil {
		token = req.Params.GetProgressToken()
	}
	start := time.Now()
	for checks := 1; ; checks++ {
		var d struct {
			ID     int64
			Status string
		}
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, err
		}
		if d.Status != "pending" || time.Since(start) >= limit {
			return raw, nil
		}
		if token != nil && req.Session != nil {
			// The total is unknown, so progress just counts up.
			_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{ProgressToken: token, Progress: float64(checks),
				Message: fmt.Sprintf("Writing draft %d (%s so far)", d.ID, time.Since(start).Round(time.Second))})
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(every):
		}
		var err error
		if raw, err = s.API.Get(ctx, fmt.Sprintf("/api/drafts/%d", d.ID), nil); err != nil {
			return nil, err
		}
	}
}
