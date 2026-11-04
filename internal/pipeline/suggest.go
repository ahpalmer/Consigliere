package pipeline

import (
	"context"
	"log/slog"

	"github.com/ahpalmer/consigliere/internal/kit"
	"github.com/ahpalmer/consigliere/internal/radar"
)

// Suggestions picks what to apply to next from the scored options that aren't in the pipeline yet:
//   - high: strong matches (ranked score ≥ HighMinScore, so priority companies count their bonus) worth making
//     active and working on by hand;
//   - medium: decent matches (≥ MediumMinScore) where the dashboard prepares the application instead: a tailored
//     resume, drafted answers, and a link straight to the form. Nothing is submitted; the owner applies.
//
// Each company appears at most PerCompany times across both lists, so one big board doesn't crowd out the rest.
type Suggestions struct {
	Catalog  *radar.Catalog
	Pipeline *Pipeline
	Drafts   *Drafts

	HighMinScore, MediumMinScore int
	MaxHigh, MaxMedium           int
	PerCompany                   int
	// How many medium suggestions get a kit prepared by the scheduled job, per run.
	AutoPrepare int
	Cron        string
}

// Suggestion is one option to act on, with its prepared kit (if any).
type Suggestion struct {
	radar.Option
	ApplyURL string
	Kit      *Draft
}

func (s *Suggestions) List(ctx context.Context) (high, medium []Suggestion, err error) {
	floor := s.MediumMinScore
	page, err := s.Catalog.Options(ctx, radar.OptionsQuery{MinScore: &floor, Limit: 1000})
	if err != nil {
		return nil, nil, err
	}
	ids := make([]int64, len(page.Options))
	for i, o := range page.Options {
		ids[i] = o.ID
	}
	pursued, err := s.Pipeline.ForItems(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	kits, err := s.Drafts.LatestKits(ctx, ids)
	if err != nil {
		return nil, nil, err
	}

	perCompany := map[string]int{}
	for _, o := range page.Options {
		if pursued[o.ID] != nil || (o.Feedback != nil && *o.Feedback < 0) {
			continue
		}
		company := radar.NormalizeCompany(o.Company)
		if s.PerCompany > 0 && perCompany[company] >= s.PerCompany {
			continue
		}
		sg := Suggestion{Option: o, ApplyURL: radar.ApplyURL(o.URL)}
		if k, ok := kits[o.ID]; ok {
			sg.Kit = &k
		}
		switch {
		case o.RankScore >= s.HighMinScore && len(high) < s.MaxHigh:
			high = append(high, sg)
		case o.RankScore >= s.HighMinScore:
			continue // the high list is full; don't demote strong matches into "prepare it for me"
		case len(medium) < s.MaxMedium:
			medium = append(medium, sg)
		default:
			continue
		}
		perCompany[company]++
	}
	return high, medium, nil
}

// Applicant is the owner's standard form answers (name, contact, application.yaml).
func (s *Suggestions) Applicant() ([]kit.Field, error) { return kit.Applicant(s.Drafts.Kit.Pack) }

// Prepare starts a kit for a radar item from the dashboard.
func (s *Suggestions) Prepare(ctx context.Context, itemID int64) (Draft, error) {
	return s.Drafts.StartKit(ctx, nil, &itemID, nil)
}

// AppliedItem records that the owner applied to a radar item: it becomes a pursuit (if it isn't one) and moves to
// waiting, so follow-ups and email updates find it.
func (s *Suggestions) AppliedItem(ctx context.Context, itemID int64) (*Pursuit, error) {
	p, _, err := s.Pipeline.ActivateItem(ctx, itemID)
	if err != nil {
		return nil, err
	}
	return s.Pipeline.Applied(ctx, p.ID)
}

// Ready counts medium suggestions with a finished kit, for the morning briefing.
func (s *Suggestions) Ready(ctx context.Context) (int, error) {
	_, medium, err := s.List(ctx)
	n := 0
	for _, sg := range medium {
		if sg.Kit != nil && sg.Kit.Status == DraftReady {
			n++
		}
	}
	return n, err
}

func (s *Suggestions) Name() string     { return "prepare-applications" }
func (s *Suggestions) Schedule() string { return s.Cron }

// Run prepares kits for the best medium suggestions that don't have one yet. A failed kit isn't retried
// automatically (the dashboard's Prepare button does that), so a posting that always fails can't burn money daily.
func (s *Suggestions) Run(ctx context.Context) error {
	_, medium, err := s.List(ctx)
	if err != nil {
		return err
	}
	started := 0
	for _, sg := range medium {
		if started >= s.AutoPrepare {
			break
		}
		if sg.Kit != nil {
			continue
		}
		if _, err := s.Prepare(ctx, sg.ID); err != nil {
			return err
		}
		started++
	}
	if started > 0 {
		slog.Info("preparing applications", "count", started)
	}
	return nil
}
