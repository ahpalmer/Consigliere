package radar

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ahpalmer/consigliere/internal/clock"
	"github.com/ahpalmer/consigliere/internal/commands"
	"github.com/ahpalmer/consigliere/internal/store"
)

// Commands are Radar's slash commands.
type Commands struct {
	DB       *sql.DB
	Scanner  *Scanner
	Scoring  *ScoringRunner
	Digest   *Digest
	Feedback *Feedback
	Config   ConfigLoader
	Clock    clock.Clock
}

func (c *Commands) Commands() []commands.Info {
	return []commands.Info{
		{Name: "/scan", Description: "check all watched sources now, then score what's new"},
		{Name: "/score", Description: "scoring progress; starts scoring if anything is waiting"},
		{Name: "/digest", Description: "best new items; '/digest reset' starts over from the top"},
		{Name: "/item", Description: "full details for one item, e.g. /item 123"},
		{Name: "/good", Description: "mark item(s) as a good find, e.g. /good 123 456"},
		{Name: "/bad", Description: "mark item(s) as a miss, e.g. /bad 123"},
		{Name: "/sources", Description: "watched sources and their last run"},
	}
}

func (c *Commands) Handle(ctx context.Context, command, args string, _ int64) (string, error) {
	switch command {
	case "/scan":
		return c.scan(ctx)
	case "/score":
		return c.score(ctx)
	case "/digest":
		if strings.EqualFold(args, "reset") {
			n, err := c.Digest.Reset(ctx)
			if err != nil || n == 0 {
				return "Nothing to reset; no items have been sent yet.", err
			}
			return fmt.Sprintf("Reset %d item(s). Send /digest to start from the top.", n), nil
		}
		result, err := c.Digest.Next(ctx)
		if err != nil || result.Text == "" {
			return "Nothing new since the last digest.", err
		}
		return result.Text, nil
	case "/item":
		return c.item(ctx, args)
	case "/good":
		return c.feedback(ctx, args, 1)
	case "/bad":
		return c.feedback(ctx, args, -1)
	case "/sources":
		return c.sources(ctx)
	}
	return "", fmt.Errorf("unknown command %s", command)
}

func (c *Commands) scan(ctx context.Context) (string, error) {
	summary, ok, err := c.Scanner.Scan(ctx)
	if !ok {
		return "A scan is already running.", nil
	}
	if err != nil {
		return "", err
	}
	if len(summary.Sources) == 0 {
		return "Nothing to scan. Add sources to " + WatchlistPath + " in the data pack.", nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Scanned %d source(s): %d new, %d passed the filter.\n", len(summary.Sources), summary.New(), summary.Passed())
	for _, s := range summary.Sources {
		if s.Error != "" {
			fmt.Fprintf(&sb, "⚠ %s: %s\n", s.Name, s.Error)
		}
	}
	pending, err := c.Scoring.CountPending(ctx)
	if err != nil {
		return "", err
	}
	if pending > 0 {
		c.Scoring.RunInBackground()
		fmt.Fprintf(&sb, "Scoring %d item(s) in the background. Strong local matches will alert you; /score shows progress.", pending)
	}
	return strings.TrimSpace(sb.String()), nil
}

func (c *Commands) score(ctx context.Context) (string, error) {
	pending, err := c.Scoring.CountPending(ctx)
	if err != nil {
		return "", err
	}
	switch {
	case c.Scoring.IsRunning():
		return fmt.Sprintf("Scoring in progress; %d item(s) left.", pending), nil
	case pending == 0:
		return "Everything is scored.", nil
	}
	c.Scoring.RunInBackground()
	return fmt.Sprintf("Started scoring %d item(s).", pending), nil
}

func (c *Commands) item(ctx context.Context, args string) (string, error) {
	ids := parseIDs(args)
	if len(ids) != 1 {
		return "Usage: /item <id>", nil
	}
	item, err := scanItemView(c.DB.QueryRowContext(ctx, `SELECT `+itemViewColumns+` FROM "RadarItems" WHERE "Id" = ?`, ids[0]))
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Sprintf("No item #%d.", ids[0]), nil
	}
	if err != nil {
		return "", err
	}
	return Detail(item, c.Clock.Now()), nil
}

func (c *Commands) feedback(ctx context.Context, args string, value int) (string, error) {
	ids := parseIDs(args)
	if len(ids) == 0 {
		if value > 0 {
			return "Usage: /good <id> [id...]", nil
		}
		return "Usage: /bad <id> [id...]", nil
	}
	n, err := c.Feedback.Set(ctx, ids, value)
	if err != nil {
		return "", err
	}
	mark := "👎"
	if value > 0 {
		mark = "👍"
	}
	reply := fmt.Sprintf("Marked %d item(s) %s.", n, mark)
	if int(n) < len(ids) {
		reply += fmt.Sprintf(" %d id(s) not found.", len(ids)-int(n))
	}
	return reply, nil
}

func (c *Commands) sources(ctx context.Context) (string, error) {
	watchlist, err := c.Config.Watchlist()
	if err != nil {
		return "", err
	}
	if len(watchlist.Sources) == 0 {
		return "No sources. Add some to " + WatchlistPath + " in the data pack.", nil
	}
	rows, err := c.DB.QueryContext(ctx, `SELECT r."Source", r."Board", r."StartedAt", r."Fetched", r."New", r."Error" FROM "SourceRuns" r
		WHERE r."Id" = (SELECT max(x."Id") FROM "SourceRuns" x WHERE x."Source" = r."Source" AND x."Board" = r."Board")`)
	if err != nil {
		return "", err
	}
	type run struct {
		started      string
		fetched, new int
		err          sql.NullString
	}
	runs := map[string]run{}
	for rows.Next() {
		var source, board string
		var r run
		if err := rows.Scan(&source, &board, &r.started, &r.fetched, &r.new, &r.err); err != nil {
			rows.Close()
			return "", err
		}
		runs[strings.ToLower(source)+"/"+board] = r
	}
	rows.Close()

	var sb strings.Builder
	for _, s := range watchlist.Sources {
		status := "never run"
		if r, ok := runs[strings.ToLower(s.Ats)+"/"+s.Board]; ok {
			when := r.started
			if t, err := store.ParseTime(r.started); err == nil {
				when = t.Local().Format("1/2/2006 3:04 PM")
			}
			if r.err.Valid {
				status = fmt.Sprintf("FAILED %s: %s", when, r.err.String)
			} else {
				status = fmt.Sprintf("%d items, %d new at %s", r.fetched, r.new, when)
			}
		}
		priority := ""
		if s.Priority != "" && !strings.EqualFold(s.Priority, "normal") {
			priority = " [" + s.Priority + "]"
		}
		fmt.Fprintf(&sb, "• %s%s (%s/%s): %s\n", s.Name, priority, s.Ats, s.Board, status)
	}
	return strings.TrimSpace(sb.String()), nil
}

func parseIDs(args string) []int64 {
	var ids []int64
	seen := map[int64]bool{}
	for _, f := range strings.FieldsFunc(args, func(r rune) bool { return r == ' ' || r == ',' || r == '#' }) {
		if id, err := strconv.ParseInt(f, 10, 64); err == nil && id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}
