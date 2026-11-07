package config

import (
	"strings"
	"testing"
)

// Regression guards for the shared tooltip catalog
// (internal/config/Tooltips_helpers.json). These used to validate the
// hand-maintained SubgenreHints literal; the catalog is now the single source
// of truth, so the guards check the embedded entries directly.

func TestEverySubgenrePresetHasHint(t *testing.T) {
	hints := TooltipSubgenres()
	seen := map[string]bool{}
	for _, p := range SubgenrePresetKeys {
		if seen[p] {
			continue
		}
		seen[p] = true
		if strings.TrimSpace(hints[TooltipKey(p)].EN) == "" {
			t.Errorf("SubgenrePresetKeys entry %q has no tooltip entry", p)
		}
	}
}

func TestSubgenreHintsBothLanguagesNonEmpty(t *testing.T) {
	for k, v := range TooltipSubgenres() {
		if strings.TrimSpace(v.EN) == "" || strings.TrimSpace(v.ES) == "" {
			t.Errorf("subgenres[%q] has an empty en/es blurb: %#v", k, v)
		}
	}
}

func TestEveryFrontendMirrorLabelHasHint(t *testing.T) {
	hints := TooltipSubgenres()
	check := func(label string) {
		if strings.TrimSpace(hints[TooltipKey(label)].EN) == "" {
			t.Errorf("frontend-mirrored subgenre %q has no tooltip entry", label)
		}
	}
	for label := range subgenreLabelParents {
		check(label)
	}
	for label := range frontendOnlySubgenreParents {
		check(label)
	}
}
