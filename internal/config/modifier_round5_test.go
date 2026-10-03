package config

import "testing"

func TestAnimeModifierPresets(t *testing.T) {
	cases := []struct{ text, want string }{
		{"Isekai Villainess", "isekai:villainess"},
		{"romance otome isekai", "isekai:otome"},
		{"magical girl", "mahou_shoujo"},
		{"mecha war", "mecha"},
		{"slice of life cafe", "slice_of_life"},
		{"iyashikei", "iyashikei"},
		{"solo leveling hunter system", "manhwa_system"},
		{"regression rebirth revenge", "regression_rebirth"},
		{"tower climb", "tower_climbing"},
		{"battle royale island", "battle_royale"},
		{"dungeon delver party", "dungeon"},
		{"spokon baseball", "spokon"},
		{"harem comedy", "harem"},
		{"ecchi", "ecchi"},
		{"revenge drama", "revenge"},
		{"battle shonen tournament", "battle_shonen"},
		{"異世界転生", "isekai"},
	}
	for _, c := range cases {
		if got := MatchAnimeModifierKey(c.text); got != c.want {
			t.Errorf("MatchAnimeModifierKey(%q)=%q want %q", c.text, got, c.want)
		}
	}
	// presets populated & end with "other"
	for k := range animeModifierPresets {
		if len(GenreConflictScales[k]) == 0 || GenreConflictScales[k][len(GenreConflictScales[k])-1] != "other" {
			t.Errorf("conflict scales for %s missing 'other'", k)
		}
		if len(GenreProtagonistTypes[k]) == 0 || GenreProtagonistTypes[k][len(GenreProtagonistTypes[k])-1] != "other" {
			t.Errorf("protagonists for %s missing 'other'", k)
		}
		if len(GenreSpecificSettings[k]) == 0 || GenreSpecificSettings[k][len(GenreSpecificSettings[k])-1] != "other" {
			t.Errorf("settings for %s missing 'other'", k)
		}
	}
	// character extra fields resolve for modifiers via SubgenreCharacterExtraFields
	if f := SubgenreCharacterExtraFields("Magical Girl"); len(f) == 0 {
		t.Error("no char fields for magical girl")
	}
	if f := SubgenreCharacterExtraFields("Isekai Villainess"); len(f) == 0 {
		t.Error("no char fields for villainess")
	}
}
