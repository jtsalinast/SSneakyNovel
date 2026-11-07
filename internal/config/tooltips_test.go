package config

import (
	"strings"
	"testing"
)

func TestTooltipLanguageSelection(t *testing.T) {
	e := TooltipEntry{EN: "english", ES: "español"}
	cases := map[string]string{
		"es": "español", "es-AR": "español", "ES": "español",
		"en": "english", "zh": "english", "": "english",
	}
	for lang, want := range cases {
		if got := e.For(lang); got != want {
			t.Errorf("For(%q) = %q, want %q", lang, got, want)
		}
	}
}

func TestTooltipKeyNormalization(t *testing.T) {
	cases := map[string]string{
		"Insurgency & Occupation":         "insurgency_and_occupation",
		"Timing & Miscommunication Farce": "timing_and_miscommunication_farce",
		"LitRPG":                          "litrpg",
		"EMP_collapse":                    "emp_collapse",
		"  Cozy Mystery  ":                "cozy_mystery",
	}
	for in, want := range cases {
		if got := TooltipKey(in); got != want {
			t.Errorf("TooltipKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// The catalog must stay bilingual; the UI renders the entry for the story
// language and falls back to keyword-family blurbs for terms that are not in
// the catalog (e.g. terms contributed at runtime by the anime presets).
func TestTooltipCatalogBilingual(t *testing.T) {
	check := func(section string, entries map[string]TooltipEntry) {
		if len(entries) == 0 {
			t.Fatalf("%s catalog is empty", section)
		}
		for k, e := range entries {
			if strings.TrimSpace(e.EN) == "" || strings.TrimSpace(e.ES) == "" {
				t.Errorf("%s[%q] has an empty en/es blurb: %#v", section, k, e)
			}
		}
	}
	check("conflict_scales", TooltipConflictScales())
	check("protagonist_types", TooltipProtagonistTypes())
	check("structures", TooltipStructures())
	check("genres", TooltipGenres())
	check("lengths", TooltipLengths())
	check("settings", TooltipSettings())
}

// Every term the Novel Parameters UI can render must resolve to a catalog
// entry: conflict scales, protagonist types, specific-setting tokens (including
// the anime/drama subgenre presets merged at init) and the fixed structure and
// length lists. Free-text terms have no entry by design and fall back to the
// keyword-family blurbs in the frontend.
func TestTooltipCatalogCoversPresetTerms(t *testing.T) {
	check := func(family string, labels []string, have map[string]TooltipEntry) {
		seen := map[string]bool{}
		for _, label := range labels {
			if label == "" || label == "other" || seen[label] {
				continue
			}
			seen[label] = true
			if k := TooltipKey(label); strings.TrimSpace(have[k].EN) == "" || strings.TrimSpace(have[k].ES) == "" {
				t.Errorf("%s: no bilingual tooltip for %q (key %q)", family, label, k)
			}
		}
	}
	for _, list := range GenreConflictScales {
		check("conflict_scales", list, TooltipConflictScales())
	}
	for _, list := range GenreProtagonistTypes {
		check("protagonist_types", list, TooltipProtagonistTypes())
	}
	var settings []string
	for _, list := range GenreSpecificSettings {
		settings = append(settings, list...)
	}
	for _, list := range ModifierPresets {
		settings = append(settings, list...)
	}
	check("settings", settings, TooltipSettings())
	check("structures", AllStructureKeys, TooltipStructures())
	check("lengths", LengthKeys, TooltipLengths())
}

// Representative exact lookups, including the label -> token normalization the
// backend and frontend share.
func TestTooltipCatalogLookup(t *testing.T) {
	if got := TooltipConflictScales()[TooltipKey("Insurgency & Occupation")].EN; got == "" {
		t.Error("conflict scale lookup failed for a label containing &")
	}
	if got := TooltipProtagonistTypes()[TooltipKey("Chosen One")].ES; got == "" {
		t.Error("protagonist type lookup failed")
	}
	if got := TooltipStructures()[TooltipKey("three_act")].EN; got == "" {
		t.Error("structure lookup failed")
	}
	if got := TooltipGenres()[TooltipKey("mystery_thriller")].ES; got == "" {
		t.Error("genre lookup failed")
	}
	if got := TooltipLengths()[TooltipKey("light_novel")].EN; got == "" {
		t.Error("length lookup failed")
	}
	if got := TooltipSettings()[TooltipKey("EMP_collapse")].ES; got == "" {
		t.Error("setting token lookup failed")
	}
}
