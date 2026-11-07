package story

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeAliases(t *testing.T) {
	got := NormalizeAliases([]string{" Alice ", "alice", "", "  ", "Al", "AL"})
	if want := []string{"Alice", "Al"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := NormalizeAliases(nil); got != nil {
		t.Fatalf("empty input must stay empty, got %q", got)
	}
}

func TestWorldbuildingAestheticBlock(t *testing.T) {
	if WorldbuildingAestheticBlock(nil, "en") != "" || WorldbuildingAestheticBlock(&ProjectSettings{Aesthetic: "  "}, "en") != "" {
		t.Fatal("unset aesthetic must render nothing")
	}
	en := WorldbuildingAestheticBlock(&ProjectSettings{Aesthetic: "Burton stop-motion"}, "en")
	if !strings.Contains(en, "[WORLD AESTHETIC]") || !strings.Contains(en, "Burton stop-motion") {
		t.Fatalf("missing English aesthetic block: %q", en)
	}
	es := WorldbuildingAestheticBlock(&ProjectSettings{Aesthetic: "Burton stop-motion"}, "es")
	if !strings.Contains(es, "[ESTÉTICA DEL MUNDO]") || !strings.Contains(es, "Burton stop-motion") {
		t.Fatalf("missing Spanish aesthetic block: %q", es)
	}
}

func TestRetrieveSettingsMatchesAliases(t *testing.T) {
	settings := &ProjectSettings{
		Characters: []Character{{ID: "c_1", Name: "Alice", Aliases: []string{"Ali"}}},
		Worldview:  []WorldviewEntry{{ID: "w_1", Category: "location", Name: "Silver Bridge", Aliases: []string{"Old Span"}}},
	}
	if _, visible := retrieveSettings(settings, "Ali crosses the bridge", settingsContextRunes); !visible["c_1"] {
		t.Fatal("alias did not retrieve the character")
	}
	if _, visible := retrieveSettings(settings, "Old Span collapses", settingsContextRunes); !visible["w_1"] {
		t.Fatal("alias did not retrieve the worldview entry")
	}
	if _, visible := retrieveSettings(settings, "quartz", settingsContextRunes); len(visible) != 0 {
		t.Fatal("unrelated query matched an entity")
	}
}

func TestSettingSyncAddsAliases(t *testing.T) {
	ch := factChapter(1, "Alice, known as Ali, arrives.")
	s := &ProjectSettings{Characters: []Character{{ID: "c_1", Name: "Alice", Aliases: []string{"Al"}}}}
	if err := applySettingDeltas(s, ch, []settingDelta{{Kind: "characters", Entity: map[string]any{"id": "c_1", "aliases": []any{"Ali"}}, BlockID: 1}}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Al", "Ali"}; !reflect.DeepEqual(s.Characters[0].Aliases, want) {
		t.Fatalf("aliases not unioned: %q", s.Characters[0].Aliases)
	}
	if s.StoryChanges[0].Status != "applied" {
		t.Fatalf("alias-only update became %s", s.StoryChanges[0].Status)
	}
}

func TestSettingSyncAliasNameDoesNotRename(t *testing.T) {
	ch := factChapter(1, "Ali smiles.")
	s := &ProjectSettings{Characters: []Character{{ID: "c_1", Name: "Alice", Aliases: []string{"Ali"}}}}
	if err := applySettingDeltas(s, ch, []settingDelta{{Kind: "characters", Entity: map[string]any{"name": "Ali", "notes": "smiles"}, BlockID: 1}}); err != nil {
		t.Fatal(err)
	}
	if len(s.Characters) != 1 || s.Characters[0].Name != "Alice" || s.Characters[0].Notes != "smiles" {
		t.Fatalf("alias reference created a duplicate or renamed the entity: %+v", s.Characters)
	}
}
