package config

import "testing"

// Round 17 rule: every subgenre belongs to EXACTLY ONE parent genre. The
// subgenreLabelParents map (backend) and the GENRES lists in
// frontend/src/lib/novelwriterGenres.js are the two halves of that taxonomy;
// these tests guard the single-parent invariant on the backend side.

func TestSubgenreLabelParents_HaveExactlyOneParent(t *testing.T) {
	for label, parents := range subgenreLabelParents {
		if len(parents) != 1 {
			t.Errorf("subgenre %q has %d parents (%v); each subgenre must belong to exactly one parent genre", label, len(parents), parents)
		}
	}
}

func TestSubgenreLabelParents_ParentIsKnownGenreKey(t *testing.T) {
	known := map[string]bool{
		"fantasy": true, "scifi": true, "mystery_thriller": true, "romance": true,
		"adventure": true, "horror": true, "historical": true, "western": true,
		"drama": true, "comedy": true,
	}
	for label, parents := range subgenreLabelParents {
		if len(parents) == 1 && !known[parents[0]] {
			t.Errorf("subgenre %q assigned to unknown parent genre key %q", label, parents[0])
		}
	}
}

// A subgenre display label must not be registered as a setting token under a
// second parent genre: buildSubgenreLabelPresets appends tokens per parent, so
// with the single-parent map each token appears at most once across genres.
func TestSubgenreSettingTokens_NotSharedAcrossParents(t *testing.T) {
	for label, parents := range subgenreLabelParents {
		if len(parents) != 1 {
			continue // covered by the first test
		}
		token := toLowerSnake(label)
		hits := 0
		for key, list := range GenreSpecificSettings {
			if !knownParentKey(key) {
				continue // composite/anime preset buckets may legitimately repeat tokens
			}
			for _, x := range list {
				if equalFoldToken(x, token) {
					hits++
				}
			}
		}
		if hits > 1 {
			t.Errorf("subgenre %q setting token %q surfaces under %d parent genres; expected 1", label, token, hits)
		}
	}
}

func knownParentKey(k string) bool {
	switch k {
	case "fantasy", "scifi", "mystery_thriller", "romance", "adventure",
		"horror", "historical", "western", "drama", "comedy":
		return true
	}
	return false
}

func toLowerSnake(s string) string {
	out := []rune{}
	for _, r := range s {
		if r == ' ' || r == '-' {
			r = '_'
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		out = append(out, r)
	}
	return string(out)
}

func equalFoldToken(a, b string) bool {
	return toLowerSnake(a) == b
}
