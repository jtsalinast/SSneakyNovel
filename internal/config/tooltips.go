package config

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

// Static, non-runtime tooltips live in Tooltips_helpers.json: one bilingual
// catalog for subgenres, structures, conflict scales, protagonist types,
// specific-setting tokens, genres and story lengths. Dynamic descriptions
// (chapters, scenes, runtime state) stay in code.
//
//go:embed Tooltips_helpers.json
var tooltipsJSON []byte

// TooltipEntry is one bilingual blurb from the catalog.
type TooltipEntry struct {
	EN string `json:"en"`
	ES string `json:"es"`
}

// For returns the blurb in the requested language: Spanish for "es"/"es-*",
// English otherwise (the catalog has no Chinese variant).
func (e TooltipEntry) For(lang string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "es") {
		return e.ES
	}
	return e.EN
}

type tooltipCatalog struct {
	ConflictScales   map[string]TooltipEntry `json:"conflict_scales"`
	ProtagonistTypes map[string]TooltipEntry `json:"protagonist_types"`
	Subgenres        map[string]TooltipEntry `json:"subgenres"`
	Structures       map[string]TooltipEntry `json:"structures"`
	Genres           map[string]TooltipEntry `json:"genres"`
	Lengths          map[string]TooltipEntry `json:"lengths"`
	Settings         struct {
		Tokens map[string]TooltipEntry `json:"tokens"`
	} `json:"settings"`
}

var (
	tooltipsOnce sync.Once
	tooltipsData tooltipCatalog
)

func tooltips() *tooltipCatalog {
	tooltipsOnce.Do(func() {
		var raw tooltipCatalog
		if err := json.Unmarshal(tooltipsJSON, &raw); err != nil {
			// Tooltips are optional decoration: a malformed catalog must never
			// take the server down or block project loading.
			return
		}
		tooltipsData = tooltipCatalog{
			ConflictScales:   normalizeTooltipKeys(raw.ConflictScales),
			ProtagonistTypes: normalizeTooltipKeys(raw.ProtagonistTypes),
			Subgenres:        normalizeTooltipKeys(raw.Subgenres),
			Structures:       normalizeTooltipKeys(raw.Structures),
			Genres:           normalizeTooltipKeys(raw.Genres),
			Lengths:          normalizeTooltipKeys(raw.Lengths),
		}
		tooltipsData.Settings.Tokens = normalizeTooltipKeys(raw.Settings.Tokens)
	})
	return &tooltipsData
}

// TooltipKey normalizes a catalog label or token to the snake_case key used in
// Tooltips_helpers.json ("Insurgency & Occupation" -> "insurgency_and_occupation",
// "LitRPG" -> "litrpg"). The frontend mirrors this in tooltipKey().
func TooltipKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "’", "")
	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, "&", " and ")
	var b strings.Builder
	prevSep := true // trims leading separators
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevSep = false
		} else if !prevSep {
			b.WriteByte('_')
			prevSep = true
		}
	}
	return strings.TrimRight(b.String(), "_")
}

func normalizeTooltipKeys(in map[string]TooltipEntry) map[string]TooltipEntry {
	out := make(map[string]TooltipEntry, len(in))
	for k, v := range in {
		if key := TooltipKey(k); key != "" {
			out[key] = v
		}
	}
	return out
}

// TooltipLanguage resolves the language used for static tooltips: the story's
// pinned output language ("Story language") when set, otherwise Spanish only
// when the project language itself is Spanish, else English.
func TooltipLanguage(cfg *Config) string {
	if cfg == nil {
		return "en"
	}
	if lang := EffectiveOutputLanguage(cfg); lang != "" {
		return lang
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(cfg.Language)), "es") {
		return "es"
	}
	return "en"
}

// The accessors below return read-only maps keyed by TooltipKey.
func TooltipConflictScales() map[string]TooltipEntry   { return tooltips().ConflictScales }
func TooltipProtagonistTypes() map[string]TooltipEntry { return tooltips().ProtagonistTypes }
func TooltipSubgenres() map[string]TooltipEntry        { return tooltips().Subgenres }
func TooltipStructures() map[string]TooltipEntry       { return tooltips().Structures }
func TooltipGenres() map[string]TooltipEntry           { return tooltips().Genres }
func TooltipLengths() map[string]TooltipEntry          { return tooltips().Lengths }
func TooltipSettings() map[string]TooltipEntry         { return tooltips().Settings.Tokens }
