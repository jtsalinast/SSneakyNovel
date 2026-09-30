package config

import "testing"

func TestMatchGenreKeyNewGens(t *testing.T) {
	cases := map[string]string{
		"LitRPG":            "litrpg",
		"系统流网文":             "litrpg",
		"都市异能":              "urban",
		"军旅文":               "military",
		"末世废土":              "postapo",
		"盗墓探险":              "adventure",
		"武侠":                "wuxia",
		"仙侠修真":              "xianxia",
		"cozy mystery":      "cozy",
		"Heist thriller":    "heist",
		"Cyberpunk 2077":    "cyberpunk",
		"solarpunk garden":  "solarpunk",
		"Romantasy fae":     "romantasy",
		"Dystopian regime":  "dystopian",
		"Space opera fleet": "space_opera",
		"克苏鲁恐怖":             "horror",
		"fantasy dragons":   "fantasy",
		"科幻":                "scifi",
		// round 4: demoted subgenres now resolve to composite parent:subgenre keys
		"steampunk airships":          "scifi:steampunk",
		"superhero legacy":            "scifi:superhero",
		"dark academia murder":        "horror:dark_academia",
		"pirate revenge":              "adventure:swashbuckler",
		"voyage at sea":               "adventure:nautical",
		"picaresque con artist":       "adventure:picaresque",
		"gothic estate":               "horror:gothic",
		"time travel paradox":         "scifi:time_travel",
		"cli-fi drought":              "scifi:cli_fi",
		"afrofuturism diaspora":       "scifi:afrofuturism",
		"hard sci-fi generation ship": "scifi:hard",
		"graphic novel memoir":        "graphic_novel",
		"rom-com office wedding":      "romance:romcom",
		"爱情喜剧":                        "romance:romcom",
		"literary fiction grief":      "literary",
		"存在主义小说":                      "literary",
		"police procedural precinct":  "mystery:police_procedural",
		"刑侦小组":                        "mystery:police_procedural",
		// audience words no longer map to genre keys (moved to target_audience)
		"middle grade adventure": "adventure",
		"young adult romance":    "romance",
		"青春校园":                   "",
		"海盗大航海":                  "adventure:swashbuckler",
		"穿越时空的爱恋":                "scifi:time_travel",
		"random poetry":          "",
	}
	for in, want := range cases {
		if got := MatchGenreKey(in); got != want {
			t.Errorf("MatchGenreKey(%q)=%q want %q", in, got, want)
		}
	}
}

func TestEveryGenreKeyHasPresets(t *testing.T) {
	keys := []string{"fantasy", "scifi", "mystery", "romance", "thriller", "horror", "historical", "western", "litrpg", "urban", "military", "postapo", "adventure", "wuxia", "xianxia", "cozy", "heist", "cyberpunk", "solarpunk", "romantasy", "dystopian", "space_opera", "graphic_novel",
		// demoted subgenres: composite parent:subgenre keys
		"scifi:steampunk", "scifi:superhero", "scifi:time_travel", "scifi:cli_fi", "scifi:afrofuturism", "scifi:hard",
		"horror:dark_academia", "horror:gothic",
		"adventure:swashbuckler", "adventure:nautical", "adventure:picaresque",
		"romance:romcom", "mystery:police_procedural", "literary"}
	for _, k := range keys {
		if len(GenreConflictScales[k]) == 0 {
			t.Errorf("no conflict scales for %s", k)
		}
		if len(GenreProtagonistTypes[k]) == 0 {
			t.Errorf("no protagonist types for %s", k)
		}
		if len(GenreSpecificSettings[k]) == 0 {
			t.Errorf("no specific settings for %s", k)
		}
		if len(genreCharacterExtraFields[k]) == 0 {
			t.Errorf("no char extra fields for %s", k)
		}
		if _, ok := genreCharacterExtraDesc[k]; !ok {
			t.Errorf("no char extra desc for %s", k)
		}
		last := GenreConflictScales[k][len(GenreConflictScales[k])-1]
		if last != "other" {
			t.Errorf("conflict list for %s must end with other, got %q", k, last)
		}
		lastP := GenreProtagonistTypes[k][len(GenreProtagonistTypes[k])-1]
		if lastP != "other" {
			t.Errorf("protagonist list for %s must end with other, got %q", k, lastP)
		}
	}
}

func TestSubgenreExtraFieldsKeysHaveDescCoverage(t *testing.T) {
	for sub := range subgenreCharacterExtraFields {
		if len(subgenreCharacterExtraFields[sub]) == 0 {
			t.Errorf("empty subgenre fields %s", sub)
		}
	}
}

func TestAudienceGuidance(t *testing.T) {
	for _, k := range AudienceKeys {
		if AudienceGuidance(k, "zh") == "" {
			t.Errorf("no zh guidance for %s", k)
		}
		if AudienceGuidance(k, "en") == "" {
			t.Errorf("no en guidance for %s", k)
		}
	}
	if AudienceGuidance("", "en") != "" || AudienceGuidance("bogus", "en") != "" {
		t.Errorf("unknown/empty audience must yield empty guidance")
	}
}

func TestCharacterFieldsFallbackToParent(t *testing.T) {
	keys, _ := GenreCharacterExtraFields("scifi:cyberpunk", false)
	if len(keys) == 0 {
		t.Fatalf("expected fallback to scifi fields for unknown composite key")
	}
	if keys2, _ := GenreCharacterExtraFields("scifi:steampunk", false); len(keys2) == 0 {
		t.Fatalf("expected own entry for scifi:steampunk")
	}
}
