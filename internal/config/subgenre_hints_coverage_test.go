package config

import (
	"strings"
	"testing"
)

// Round 27 regression guard: every subgenre preset advertised to the frontend
// (SubgenrePresetKeys, exposed via /api/novel-params "subgenre_presets") must
// have a one-line hint in SubgenreHints. Previously ~60 presets shipped with
// no description at all, so their chips rendered without the second line or
// tooltip the user expects.
func TestEverySubgenrePresetHasHint(t *testing.T) {
	lower := make(map[string]bool, len(SubgenreHints))
	for k := range SubgenreHints {
		lower[strings.ToLower(k)] = true
	}
	seen := map[string]bool{}
	for _, p := range SubgenrePresetKeys {
		if seen[p] {
			continue
		}
		seen[p] = true
		if !lower[strings.ToLower(p)] {
			t.Errorf("SubgenrePresetKeys entry %q has no SubgenreHints entry", p)
		}
	}
}

// Hint values must be non-empty in BOTH languages ([zh, en]).
func TestSubgenreHintsBothLanguagesNonEmpty(t *testing.T) {
	for k, v := range SubgenreHints {
		if strings.TrimSpace(v[0]) == "" || strings.TrimSpace(v[1]) == "" {
			t.Errorf("SubgenreHints[%q] has an empty zh/en blurb: %#v", k, v)
		}
	}
}

// Round 28 regression guard: the frontend GENRES catalog (novelwriterGenres.js)
// lists display labels like "High Fantasy" or "Road-trip Comedy" that reach
// hintFor() verbatim. Every label present in subgenreLabelParents or
// frontendOnlySubgenreParents — the backend mirror of that catalog — must have
// a SubgenreHints entry under its exact lowercase label, otherwise the UI's
// normalized lookups silently miss and the chip renders without description.
func TestEveryFrontendMirrorLabelHasHint(t *testing.T) {
	lower := make(map[string]bool, len(SubgenreHints))
	for k := range SubgenreHints {
		lower[strings.ToLower(k)] = true
	}
	check := func(label string) {
		l := strings.ToLower(label)
		if !lower[l] {
			t.Errorf("frontend-mirrored subgenre %q has no SubgenreHints entry", label)
		}
	}
	for label := range subgenreLabelParents {
		check(label)
	}
	for label := range frontendOnlySubgenreParents {
		check(label)
	}
}
