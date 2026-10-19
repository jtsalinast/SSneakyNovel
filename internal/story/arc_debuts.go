package story

import (
	"fmt"
	"showmethestory/internal/i18n"
	"strings"
)

// This file derives HARD, machine-readable sequencing constraints from the
// structured chapter data instead of relying on the model to infer them from
// free-text outlines:
//
//   - Characters whose outline entry is flagged FirstAppearance in a LATER
//     chapter have not debuted yet; naming or staging them in this chapter is
//     exactly the "premature introduction" failure the fact-checker keeps
//     reporting. The debut list below turns that into an explicit ban.
//   - Characters already flagged (or named) in drafted chapters HAVE debuted;
//     their first meetings must never be re-enacted.
//   - One-time events (first meeting A↔B) planned for later chapters are
//     labelled with their destination chapter so both channels carry the same
//     unambiguous metadata.

const arcDebutListBudget = 2500

type arcDebut struct {
	name string
	num  int // planned debut chapter number
	idx  int // chapter index
}

func normalizeArcName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// collectDebutedNames returns the lower-cased names of every character in the
// structured cast of chapters up to (excluding) idx that have real prose —
// their debut has been consumed by the written story, so later planned debuts
// of the same name are no-ops and must not be banned again.
func collectDebutedNames(state *Progress, idx int) map[string]bool {
	debuted := map[string]bool{}
	if state == nil {
		return debuted
	}
	for i := 0; i < idx && i < len(state.Chapters); i++ {
		ch := state.Chapters[i]
		if !isDraftedChapter(ch) {
			continue
		}
		for _, c := range ch.Characters {
			if n := normalizeArcName(c.Name); n != "" {
				debuted[n] = true
			}
		}
	}
	return debuted
}

// futureDebuts lists characters flagged FirstAppearance in chapters AFTER idx
// whose debut has not occurred in any chapter before idx. Entries duplicated
// across several future chapters keep only the earliest planned debut.
func futureDebuts(state *Progress, idx int) []arcDebut {
	debuted := collectDebutedNames(state, idx)
	seen := map[string]bool{}
	var out []arcDebut
	if state == nil {
		return out
	}
	for i := idx + 1; i < len(state.Chapters); i++ {
		ch := state.Chapters[i]
		for _, c := range ch.Characters {
			n := normalizeArcName(c.Name)
			if n == "" || !c.FirstAppearance || debuted[n] || seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, arcDebut{name: strings.TrimSpace(c.Name), num: ch.Num, idx: i})
		}
	}
	return out
}

// draftedCastNames returns lower-cased names of characters appearing in the
// structured cast of drafted chapters up to idx (their presence means they
// already exist in the written story world).
func draftedCastNames(state *Progress, idx int) map[string]bool {
	names := map[string]bool{}
	for i := 0; i < idx && i < len(state.Chapters); i++ {
		ch := state.Chapters[i]
		if !isDraftedChapter(ch) {
			continue
		}
		for _, c := range ch.Characters {
			if n := normalizeArcName(c.Name); n != "" {
				names[n] = true
			}
		}
	}
	return names
}

// buildPrematureEventLines renders one line per planned one-time event inside
// the future-outline window, tagged with its destination chapter and marked
// UNDECLARED when no structured cast exists to confirm it is a genuine debut.
// These lines feed the future channel of the arc block; the returned slice is
// also used by the hard-debut filter to avoid contradicting itself.
func buildPrematureEventLines(state *Progress, idx int, lang string) []string {
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	debuted := collectDebutedNames(state, idx)
	castKnown := false
	for i := idx + 1; i < len(state.Chapters) && i <= idx+futureOutlineWindow; i++ {
		if len(state.Chapters[i].Characters) > 0 {
			castKnown = true
			break
		}
	}
	end := min(idx+1+futureOutlineWindow, len(state.Chapters))
	var out []string
	seenPair := map[string]bool{}
	for i := idx + 1; i < end; i++ {
		ch := state.Chapters[i]
		var pairs []string
		for _, c := range ch.Characters {
			n := normalizeArcName(c.Name)
			if n == "" || !c.FirstAppearance || debuted[n] {
				continue
			}
			pairs = append(pairs, c.Name)
		}
		if len(pairs) == 0 {
			continue
		}
		key := fmt.Sprintf("%d|%s", ch.Num, strings.Join(pairs, "|"))
		if seenPair[key] {
			continue
		}
		seenPair[key] = true
		list := strings.Join(pairs, ", ")
		if en {
			out = append(out, fmt.Sprintf("UNIQUE EVENT — Chapter %d \"%s\": first appearance / first meeting of %s. Must NOT occur or be hinted in this chapter.\n", ch.Num, ch.Title, list))
		} else {
			out = append(out, fmt.Sprintf("EVENTO ÚNICO — Capítulo %d «%s»: primera aparición / primer encuentro de %s. NO debe ocurrir ni insinuarse en este capítulo.\n", ch.Num, ch.Title, list))
		}
	}
	if !castKnown && len(out) == 0 {
		return nil
	}
	return out
}

// buildHardDebutBlock builds the [Do-not-introduce] section: an explicit,
// deduplicated ban list derived from FirstAppearance flags. When the flag data
// is too sparse to produce a meaningful list, it returns "" and the caller
// falls back to the generic upcoming-outline warning.
func buildHardDebutBlock(state *Progress, idx int, lang string) string {
	if idx < 0 || idx >= len(state.Chapters) {
		return ""
	}
	debutSet := map[string]bool{}
	for _, d := range futureDebuts(state, idx) {
		debutSet[d.name] = true
	}
	// Also ban characters implied by the premature-event lines within the
	// window (same source data, so this stays consistent).
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	var entries []arcDebut
	for i := idx + 1; i < min(idx+1+futureOutlineWindow, len(state.Chapters)); i++ {
		ch := state.Chapters[i]
		for _, c := range ch.Characters {
			if c.FirstAppearance && debutSet[c.Name] {
				entries = append(entries, arcDebut{name: c.Name, num: ch.Num, idx: i})
			}
		}
	}
	if len(entries) == 0 {
		return ""
	}
	var lines []string
	seen := map[string]bool{}
	for _, e := range entries {
		key := normalizeArcName(e.name)
		if seen[key] {
			continue
		}
		seen[key] = true
		note := ""
		for _, c := range state.Chapters[e.idx].Characters {
			if normalizeArcName(c.Name) == key && strings.TrimSpace(c.Note) != "" {
				note = " (" + Truncate(strings.TrimSpace(c.Note), 80) + ")"
				break
			}
		}
		if en {
			lines = append(lines, fmt.Sprintf("- %s → debuts in Chapter %d%s", e.name, e.num, note))
		} else {
			lines = append(lines, fmt.Sprintf("- %s → debuta en el Capítulo %d%s", e.name, e.num, note))
		}
	}
	body := boundedHistoryFallback(lines, arcDebutListBudget)
	if body == "" {
		return ""
	}
	if en {
		return "[Characters who have NOT debuted yet — do-not-introduce list, derived from the outline's first-appearance data]\n" +
			body + "\n" +
			"Do NOT introduce, name, meet, describe, or even hint at these characters in this chapter — no disguised cameo, no \"a stranger whose…\", no foreshadowing of their identity. Their debut belongs EXCLUSIVELY to the chapter listed above.\n\n"
	}
	return "[Personajes que TODAVÍA NO han debutado — lista de prohibición de introducción, derivada de los datos de primera aparición del outline]\n" +
		body + "\n" +
		"NO introduzcas, nombres, hagas encontrarse, describas ni siquiera insinúes a estos personajes en este capítulo — sin cameos disfrazados, sin «un desconocido cuyo…», sin prefigurar su identidad. Su debut pertenece EXCLUSIVAMENTE al capítulo indicado arriba.\n\n"
}

// buildAlreadyIntroducedBlock reassures the writer which cast members are
// safe: everyone flagged in a drafted chapter has already appeared, so their
// established facts may be referenced freely (never re-enacted).
func buildAlreadyIntroducedBlock(state *Progress, idx int, lang string) string {
	drafted := draftedCastNames(state, idx)
	if len(drafted) == 0 {
		return ""
	}
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	var names []string
	for _, i := range orderedChaptersUpTo(state, idx) {
		for _, c := range state.Chapters[i].Characters {
			n := normalizeArcName(c.Name)
			if n == "" || !drafted[n] {
				continue
			}
			delete(drafted, n) // dedup
			names = append(names, c.Name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	list := strings.Join(names, ", ")
	if en {
		return "[Characters already introduced in written chapters — safe to reference as established facts, but NEVER re-enact their first appearances]\n" + list + "\n\n"
	}
	return "[Personajes ya introducidos en capítulos escritos — puedes referenciarlos como hechos establecidos, pero NUNCA vuelvas a representar sus primeras apariciones]\n" + list + "\n\n"
}

func orderedChaptersUpTo(state *Progress, idx int) []int {
	out := make([]int, 0, idx)
	for i := 0; i < idx && i < len(state.Chapters); i++ {
		if isDraftedChapter(state.Chapters[i]) {
			out = append(out, i)
		}
	}
	return out
}
