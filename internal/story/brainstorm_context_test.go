package story

import (
	"strings"
	"testing"
)

func TestBrainstormEntitiesAndContext(t *testing.T) {
	s := &ProjectSettings{
		Characters:    []Character{{ID: "c1", Name: "Mara", Personality: "guarded"}},
		Worldview:     []WorldviewEntry{{ID: "w1", Name: "Harbor", Category: WorldviewCategoryLocation}},
		Organizations: []Organization{{ID: "o1", Name: "Guild", Type: "guild"}},
		Relations:     []Relation{{ID: "r1", SourceID: "c1", SourceType: "character", TargetID: "o1", TargetType: "organization", Label: "member"}},
	}
	state := &Progress{MemoryEntries: []MemoryEntry{
		{ID: 1, Category: "character", Content: "Mara lost her brother"},
		{ID: 2, Category: "event", Content: "A storm closed the roads"},
	}}

	entities := BrainstormEntities(s)
	if len(entities) != 3 {
		t.Fatalf("expected 3 entities, got %#v", entities)
	}
	if entities[1].Kind != "location" {
		t.Fatalf("worldview location must map to location kind: %#v", entities[1])
	}

	entity, ctx := BrainstormEntityContext(s, state, "character", "c1", "en")
	if entity.Name != "Mara" {
		t.Fatalf("wrong entity: %#v", entity)
	}
	for _, want := range []string{"Mara", "Guild", "member", "lost her brother"} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("entity context missing %q:\n%s", want, ctx)
		}
	}
	if strings.Contains(ctx, "storm closed") {
		t.Fatalf("entity context leaked an unrelated fact:\n%s", ctx)
	}

	if _, empty := BrainstormEntityContext(s, state, "character", "missing", "en"); empty != "" {
		t.Fatalf("missing entity must return empty context")
	}
}
