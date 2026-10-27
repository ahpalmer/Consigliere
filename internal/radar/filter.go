package radar

import "fmt"

// FilterOutcome is the rule filter's verdict.
type FilterOutcome struct {
	Passed bool
	Tier   string // "" when no tier applies
	Reason string
}

func reject(reason, tier string) FilterOutcome { return FilterOutcome{Tier: tier, Reason: reason} }

// Evaluate is the cheap, deterministic first pass. Anything that survives goes on to scoring.
func Evaluate(item FetchedItem, prefs Preferences) FilterOutcome {
	if len(prefs.Titles.Include) > 0 && !ContainsAnyKeyword(item.Title, prefs.Titles.Include) {
		return reject("title: no include keyword", "")
	}
	for _, k := range prefs.Titles.Exclude {
		if ContainsKeyword(item.Title, k) {
			return reject(fmt.Sprintf("title: excluded '%s'", k), "")
		}
	}
	for _, k := range prefs.DealBreakers {
		if ContainsKeyword(item.Title, k) || ContainsKeyword(item.Description, k) {
			return reject(fmt.Sprintf("deal-breaker '%s'", k), "")
		}
	}

	var locations []string
	for _, l := range item.Locations {
		if !ContainsAnyKeyword(l, prefs.ExcludeLocations) {
			locations = append(locations, l)
		}
	}
	if len(item.Locations) > 0 && len(locations) == 0 {
		return reject("location: all excluded", "")
	}

	if len(prefs.Tiers) == 0 {
		return FilterOutcome{Passed: true}
	}
	var tier *Tier
	for i := range prefs.Tiers {
		if tierMatches(prefs.Tiers[i], locations) {
			tier = &prefs.Tiers[i]
			break
		}
	}
	if tier == nil {
		return reject("location: no matching tier", "")
	}

	top := item.CompMax
	if top == nil {
		top = item.CompMin
	}
	if tier.MinComp > 0 && top != nil && *top < tier.MinComp {
		return reject(fmt.Sprintf("comp: %s below %s floor %s", thousands(*top), tier.Name, thousands(tier.MinComp)), tier.Name)
	}
	if top == nil && tier.MinComp > 0 {
		name := tier.Name
		if tier.UnknownCompTier != "" {
			name = tier.UnknownCompTier
		}
		return FilterOutcome{Passed: true, Tier: name, Reason: "comp unknown"}
	}
	return FilterOutcome{Passed: true, Tier: tier.Name}
}

func tierMatches(t Tier, locations []string) bool {
	if t.IsCatchAll() {
		return true
	}
	for _, l := range locations {
		if ContainsAnyKeyword(l, t.Locations) || (t.Remote && ContainsKeyword(l, "remote")) {
			return true
		}
	}
	return false
}

// thousands formats 250000 as "250,000".
func thousands(v float64) string {
	s := fmt.Sprintf("%.0f", v)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
