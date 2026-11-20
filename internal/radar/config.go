package radar

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/ahpalmer/consigliere/internal/datapack"
)

const (
	WatchlistPath   = "radar/watchlist.yaml"
	PreferencesPath = "radar/preferences.yaml"
)

// Watchlist is radar/watchlist.yaml in the data pack.
type Watchlist struct {
	Sources []WatchedSource `yaml:"sources"`
}

type WatchedSource struct {
	Name string `yaml:"name"`
	// Source kind: greenhouse | lever | ashby | email.
	Ats string `yaml:"ats"`
	// Board token / company slug as it appears in the source URL.
	Board string `yaml:"board"`
	// low | normal | high. Nudges ranking; never hides items.
	Priority string `yaml:"priority"`
}

// Preferences is radar/preferences.yaml in the data pack.
type Preferences struct {
	Titles KeywordSet `yaml:"titles"`
	// Locations dropped before tiering; an item with no locations left is filtered out.
	ExcludeLocations []string `yaml:"exclude_locations"`
	// Keywords that reject an item when found in its title or description.
	DealBreakers []string `yaml:"deal_breakers"`
	// Evaluated in order; the first match wins. A tier with no locations and remote: false is a catch-all.
	Tiers []Tier `yaml:"tiers"`
	// Items posted longer ago than this are hidden from digests/alerts and never scored. 0 = no limit.
	MaxPostingAgeDays int `yaml:"max_posting_age_days"`
	// Ordered by priority, highest first. The scorer assigns each item one of these.
	Categories []Category `yaml:"categories"`
	// Company-level priority, matched on the item's company name, whatever source reported it.
	CompanyPriority CompanyPriority `yaml:"company_priority"`
	// How fit is put together from the resume match, location and pay.
	Fit FitSettings `yaml:"fit"`
}

// FitSettings weigh the three parts of fit. The model scores only how well the resume matches the posting; location
// (by tier) and pay (the listed range against what the owner makes now, from comp/current.yaml) are plain code.
type FitSettings struct {
	Weights FitWeights `yaml:"weights"`
	// 0-100 per tier name. Tiers left out score by their order: the first 100, each next one 25 less, at least 25.
	Location map[string]int `yaml:"location"`
	// The pay score for a posting that lists no pay. Default 50.
	UnknownPay *int `yaml:"unknown_pay"`
}

// FitWeights are relative; all zero means the defaults (resume 60, location 20, pay 20). Pay drops out when there's
// no current package to compare with.
type FitWeights struct {
	Resume   int `yaml:"resume"`
	Location int `yaml:"location"`
	Pay      int `yaml:"pay"`
}

type KeywordSet struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

type Tier struct {
	Name      string   `yaml:"name"`
	Locations []string `yaml:"locations"`
	Remote    bool     `yaml:"remote"`
	// instant | daily.
	Alert string `yaml:"alert"`
	// Reject items whose listed range tops out below this. Listed ranges are usually base pay.
	MinComp float64 `yaml:"min_comp"`
	// When MinComp is set and an item lists no pay, file it under this tier name instead. Unlisted tiers rank last.
	UnknownCompTier string `yaml:"unknown_comp_tier"`
}

func (t Tier) IsCatchAll() bool { return len(t.Locations) == 0 && !t.Remote }

type Category struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

type CompanyPriority struct {
	// Boosted, listed in a priority section at the top of digests, and alerted instantly in any tier.
	High []string `yaml:"high"`
	Low  []string `yaml:"low"`
}

// ConfigLoader reads Radar config from the data pack on each call, so edits apply on the next use.
type ConfigLoader struct{ Pack *datapack.Pack }

func (l ConfigLoader) Watchlist() (Watchlist, error) {
	var w Watchlist
	return w, l.load(WatchlistPath, &w)
}

func (l ConfigLoader) Preferences() (Preferences, error) {
	var p Preferences
	return p, l.load(PreferencesPath, &p)
}

func (l ConfigLoader) load(path string, into any) error {
	text, ok, err := l.Pack.ReadText(path)
	if err != nil || !ok {
		return err
	}
	if err := yaml.Unmarshal([]byte(text), into); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}
