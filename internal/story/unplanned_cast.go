package story

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/prose"
	"showmethestory/internal/sse"
)

// Unplanned characters: when a chapter's prose introduces a named character
// that appears in NO cast of the whole outline, the fact-checker has nothing
// to compare against and the character silently escapes arc tracking. This
// file detects those debuts deterministically (lexical, no LLM), asks the LLM
// whether the figure is plot-relevant, and — on approval — registers them in
// the chapter's structured cast so future chapters' debut bans and "already
// introduced" blocks stay accurate.

const unplannedCastMaxNames = 6

// UnplannedCastReport lists characters staged in a drafted chapter that belong
// to no cast anywhere in the outline.
type UnplannedCastReport struct {
	ChapterIndex int      `json:"chapter_index"`
	ChapterNum   int      `json:"chapter_num"`
	Names        []string `json:"names"`
}

// allCastNames returns every lower-cased character name declared in the
// structured cast of ALL chapters (the outline is complete by the time
// writing starts, so a name absent here was never planned at all).
func allCastNames(state *Progress) map[string]bool {
	out := map[string]bool{}
	for _, ch := range state.Chapters {
		for _, c := range ch.Characters {
			if n := normalizeArcName(c.Name); n != "" {
				out[n] = true
			}
		}
	}
	return out
}

// unplannedStopwords are function words that may appear INSIDE a genuine
// multi-word name span (entityPattern happily matches "The Old Cook" because
// every token starts capitalized). They never make a name non-generic on
// their own; only the meaningful tokens decide.
var unplannedStopwords = map[string]bool{
	"the": true, "a": true, "an": true, "of": true, "and": true, "or": true,
	"de": true, "del": true, "la": true, "el": true, "los": true, "las": true,
	"van": true, "von": true, "y": true, "e": true,
}

// unplannedCommonNouns extend arcCommonNouns with role/occupation nouns that
// frequently capitalize inside epithets ("The Old Cook", "The Stable Boy").
// A name span is generic when NONE of its meaningful tokens looks proper —
// i.e. every one is either an arc common noun or one of these extras.
var unplannedCommonNouns = map[string]bool{
	"cook": true, "chef": true, "baker": true, "smith": true, "carpenter": true,
	"waiter": true, "waitress": true, "maid": true, "butler": true, "valet": true,
	"page": true, "squire": true, "guard": true, "guards": true, "soldier": true,
	"sailor": true, "fisherman": true, "farmer": true, "merchant": true,
	"innkeeper": true, "tavern": true, "stranger": true, "woman": true,
	"man": true, "boy": true, "girl": true, "child": true, "servant": true,
	"servants": true, "worker": true, "workers": true, "clerk": true,
	"nurse": true, "doctor": true, "priest": true, "monk": true, "nun": true,
	"captain": true, "sergeant": true, "officer": true, "constable": true,
	"judge": true, "lawyer": true, "notary": true, "scribe": true,
	"elder": true, "widow": true, "orphan": true, "guest": true, "guests": true,
	"crowd": true, "mob": true, "passerby": true, "messenger": true,
	"courier": true, "driver": true, "coachman": true, "groom": true,
	"stable": true, "hand": true, "hands": true, "voice": true, "voices": true,
	"shadow": true, "shadows": true, "figure": true, "figures": true,
	"old": true, "young": true, "new": true, "little": true, "big": true,
	"tall": true, "short": true, "gaunt": true, "pale": true, "dark": true,
	"fair": true, "kind": true, "stern": true, "quiet": true, "loud": true,
}

// unplannedIsGenericEntity reports whether a candidate name span (lower-cased)
// describes a role/epithet rather than a proper name: true when it has no
// meaningful token at all ("the"), or when EVERY meaningful token (stopwords
// and honorifics skipped) is a common noun. Unlike isArcGenericEntity this
// also filters single common nouns like "Cook" so epithets collapse cleanly.
func unplannedIsGenericEntity(lower string) bool {
	lower = stripArcHonorifics(lower)
	toks := strings.Fields(lower)
	meaningful := 0
	for _, t := range toks {
		if unplannedStopwords[t] || arcHonorifics[t] {
			continue
		}
		meaningful++
		if !arcCommonNouns[t] && !unplannedCommonNouns[t] {
			return false // at least one proper-looking token → real name
		}
	}
	// No meaningful tokens ("The") or every meaningful token is a common noun
	// ("Old Cook") → epithet/background noise, not a registrable person.
	return true
}

// unplannedNameCores returns the lower-cased core forms of a name: the full
// span minus honorifics plus each individual proper-looking token. Two names
// sharing any core are treated as mentions of the SAME person, which lets the
// LLM's shortened verdict ("Quill") still match the detected full name
// ("Bartholomew Quill") and vice versa.
func unplannedNameCores(name string) []string {
	full := stripArcHonorifics(strings.ToLower(strings.TrimSpace(name)))
	out := []string{}
	if full != "" {
		out = append(out, full)
	}
	for _, tok := range strings.Fields(full) {
		if len([]rune(tok)) >= 3 && !unplannedStopwords[tok] && !arcHonorifics[tok] &&
			!arcCommonNouns[tok] && !unplannedCommonNouns[tok] {
			out = append(out, tok)
		}
	}
	// Epithets like "Old Cook" have no proper-looking tokens; fall back to
	// their meaningful (common-noun) tokens so verdicts naming the extra can
	// still be matched ("Old Cook" ↔ "Cook").
	if len(out) == 1 {
		for _, tok := range strings.Fields(full) {
			if len([]rune(tok)) >= 3 && !unplannedStopwords[tok] && !arcHonorifics[tok] &&
				!containsWordCI(out[0], tok) {
				out = append(out, tok)
			}
		}
	}
	return out
}

// containsWordCI reports whether the space-separated phrase contains tok as a
// whole word, preventing redundant core tokens ("old cook" already holds
// "cook", so it must not be added again).
func containsWordCI(phrase, tok string) bool {
	for _, t := range strings.Fields(phrase) {
		if t == tok {
			return true
		}
	}
	return false
}

// titleCaseSpan capitalizes the first letter of every word in a lower-cased
// entity span, restoring the proper-name display form lost by entitySet's
// normalization ("bartholomew quill" → "Bartholomew Quill"). Unicode-aware.
func titleCaseSpan(s string) string {
	var sb strings.Builder
	ws := true // previous rune was whitespace (or start of string)
	for _, r := range s {
		if r == ' ' {
			ws = true
			sb.WriteRune(r)
			continue
		}
		if ws {
			sb.WriteRune(unicode.ToUpper(r))
		} else {
			sb.WriteRune(r)
		}
		ws = false
	}
	return sb.String()
}

// detectUnplannedCastChars scans chapter prose for capitalized proper-name
// spans that match NO cast member of the entire outline AND no member of the
// chapter's own scene plan. Word-bounded, case-insensitive matching prevents
// both substring false positives and honorific variants ("Lord Edmund" vs the
// planned "Edmund Ashworth") from being flagged as new people. Role epithets
// ("The Old Cook") are filtered as background noise, and results are ordered
// by first appearance in the prose for deterministic output.
func detectUnplannedCastChars(state *Progress, idx int, content string) []string {
	if strings.TrimSpace(content) == "" || idx < 0 || idx >= len(state.Chapters) {
		return nil
	}
	ch := state.Chapters[idx]
	known := map[string]bool{}
	addTo := func(set map[string]bool, name string) {
		n := normalizeArcName(name)
		if n == "" {
			return
		}
		set[n] = true
		for _, core := range arcCoreNames(n) {
			set[core] = true
		}
	}
	addKnown := func(name string) { addTo(known, name) }
	// Every name planned in ANY other chapter — earlier or later — is NOT
	// unplanned: future names belong to the debut-ban machinery (the fact
	// checker flags them as premature introductions), past ones are already
	// established. Only names absent from the ENTIRE outline cast reach this
	// flow.
	plannedElsewhere := map[string]bool{}
	for i, c2 := range state.Chapters {
		if i == idx {
			continue
		}
		for _, c := range c2.Characters {
			addTo(plannedElsewhere, c.Name)
		}
	}
	for _, c := range ch.Characters {
		addKnown(c.Name)
	}
	if ch.ScenePlan != nil {
		for _, sc := range ch.ScenePlan.Scenes {
			addKnown(sc.PoV)
			// Scene descriptions often name the participants ("Lord Vane
			// interrogates Mira"); harvest capitalized spans so scene-plan
			// mentions count as planned and are not flagged as unplanned.
			for e := range entitySet(sc.Description + " " + sc.Title) {
				if !isArcGenericEntity(e) {
					addKnown(e)
				}
			}
		}
	}
	seen := map[string]bool{}
	var out []string
	for e := range entitySet(content) {
		if isArcGenericEntity(e) || unplannedIsGenericEntity(e) {
			continue
		}
		cores := arcCoreNames(e)
		if len(cores) == 0 {
			continue
		}
		excluded := false
		for _, core := range cores {
			if known[core] || plannedElsewhere[core] {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		// The candidate is only a person if it behaves like one in the prose:
		// some word-bounded form of its name must actually occur in the text
		// (entitySet already guarantees this) and none of its cores may be a
		// common noun or place-like artifact — the stoplists above handle the
		// latter via isArcGenericEntity.
		name := titleCaseSpan(strings.TrimSpace(e))
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		// Drop candidates that are sub-spans of an already-kept longer name
		// ("quill" inside "Bartholomew Quill"), keeping the richest form.
		dominated := false
		for i, prev := range out {
			prevLower := strings.ToLower(prev)
			if strings.Contains(prevLower, key) {
				dominated = true
				break
			}
			if strings.Contains(key, prevLower) {
				out[i] = name // longer span wins; replace in place
			}
		}
		if dominated {
			continue
		}
		seen[key] = true
		out = append(out, name)
		if len(out) >= unplannedCastMaxNames {
			break
		}
	}
	sortUnplannedByFirstMention(out, content)
	return out
}

// sortUnplannedByFirstMention orders detected names by where they first occur
// in the prose (entitySet iteration order is map-random; without this the LLM
// would see a different name list on every run). Unmatched names sink to the
// end, alphabetically.
func sortUnplannedByFirstMention(names []string, content string) {
	lowerContent := strings.ToLower(content)
	pos := func(n string) int {
		if p := strings.Index(lowerContent, strings.ToLower(n)); p >= 0 {
			return p
		}
		return len(content) + 1
	}
	sort.SliceStable(names, func(i, j int) bool {
		pi, pj := pos(names[i]), pos(names[j])
		if pi != pj {
			return pi < pj
		}
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
}

// unplannedCastJudgeSystemPrompt is the system half of the relevance judge.
func unplannedCastJudgeSystemPrompt(lang string) string {
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return "You are a meticulous novel continuity editor. Answer only with valid JSON, no prose around it."
	}
	return "Sos un editor de continuidad de novelas meticuloso. Respondé solo con JSON válido, sin prosa alrededor."
}

// unplannedCastJudgePrompt asks the model whether each unplanned name is a
// plot-relevant recurring character worth registering in the outline cast.
func unplannedCastJudgePrompt(cfg *config.Config, ch ChapterState, names []string, lang string) string {
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	var sb strings.Builder
	if en {
		sb.WriteString("You are a novel continuity editor. These character names appear in the draft of Chapter " +
			fmt.Sprintf("%d", ch.Num) + " but are NOT listed in any chapter's planned cast:\n\n")
	} else {
		sb.WriteString("Sos un editor de continuidad de novelas. Estos nombres de personaje aparecen en el borrador del Capítulo " +
			fmt.Sprintf("%d", ch.Num) + " pero NO están listados en el cast planificado de ningún capítulo:\n\n")
	}
	for _, n := range names {
		sb.WriteString("- " + n + "\n")
	}
	sb.WriteString("\n")
	if en {
		sb.WriteString("Chapter outline:\n" + Truncate(ch.Outline, 1200) + "\n\n")
		sb.WriteString("For EACH name decide: is this figure plot-relevant (named again later, drives events, or clearly recurring) rather than a throwaway extra (servant, passerby, one-line voice)?\n")
		sb.WriteString("Answer ONLY with a JSON array, one object per name:\n")
		sb.WriteString(`[{"name":"...","relevant":true|false,"role":"one-line description"}]` + "\n")
	} else {
		sb.WriteString("Outline del capítulo:\n" + Truncate(ch.Outline, 1200) + "\n\n")
		sb.WriteString("Para CADA nombre decide: ¿esta figura es relevante para la trama (vuelve a aparecer, impulsa eventos o es claramente recurrente) en lugar de un extra desechable (sirviente, transeúnte, voz de una línea)?\n")
		sb.WriteString("Responde SOLO con un arreglo JSON, un objeto por nombre:\n")
		sb.WriteString(`[{"name":"...","relevant":true|false,"role":"descripción en una línea"}]` + "\n")
	}
	return sb.String()
}

var unplannedCastJSONRe = regexp.MustCompile(`\[[\s\S]*\]`)

type unplannedCastVerdict struct {
	Name     string `json:"name"`
	Relevant bool   `json:"relevant"`
	Role     string `json:"role"`
}

func parseUnplannedCastVerdicts(raw string) []unplannedCastVerdict {
	m := unplannedCastJSONRe.FindString(raw)
	if m == "" {
		return nil
	}
	var out []unplannedCastVerdict
	if err := json.Unmarshal([]byte(m), &out); err != nil {
		return nil
	}
	return out
}

// RegisterUnplannedCastResult reports what happened after a registration run.
type RegisterUnplannedCastResult struct {
	Registered []string `json:"registered"`
	Skipped    []string `json:"skipped"`
	Notes      []string `json:"notes,omitempty"`
}

// RegisterUnplannedCastAction detects unplanned characters in the accepted /
// reviewed chapter at idx, judges their relevance with one cheap LLM call, and
// adds the relevant ones to that chapter's structured cast. Because presence
// in ANY chapter cast marks a character as "belongs to the story up to here",
// registering them removes them from every later chapter's premature-debut ban
// and adds them to the "already introduced" block automatically — no manual
// outline surgery required. Throwaway extras are left unregistered (they are
// background noise, not tracked entities).
func RegisterUnplannedCastAction(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, progressPath string, logger *sse.LogBroadcaster) (*RegisterUnplannedCastResult, error) {
	if idx < 0 || idx >= len(state.Chapters) {
		return nil, fmt.Errorf("chapter index out of range")
	}
	ch := state.Chapters[idx]
	if strings.TrimSpace(ch.Content) == "" {
		return nil, fmt.Errorf("chapter has no draft content to scan")
	}
	// detectUnplannedCastChars already excludes every name planned in any
	// chapter of the outline (this one's cast + scene plan included), so what
	// comes back is genuinely unplanned.
	fresh := detectUnplannedCastChars(state, idx, ch.Content)
	res := &RegisterUnplannedCastResult{}
	if len(fresh) == 0 {
		return res, nil
	}
	if logger != nil {
		logger.InfoKey("log.unplanned_cast_detected", ch.Num, strings.Join(fresh, ", "))
	}
	raw, err := llm.CallAPI(ctx, apiCfg, unplannedCastJudgeSystemPrompt(cfg.Language), unplannedCastJudgePrompt(cfg, ch, fresh, cfg.Language))
	if err != nil && raw == "" {
		return nil, err
	}
	verdicts := parseUnplannedCastVerdicts(raw)
	if len(verdicts) == 0 {
		// The judge returned no usable JSON. Registering EVERY candidate would
		// promote throwaway extras into the outline cast, so instead ask once
		// more with a stricter instruction; only if that also fails do we fall
		// back to registering everything (never silently lose a real debut).
		raw2, err2 := llm.CallAPI(ctx, apiCfg, unplannedCastJudgeSystemPrompt(cfg.Language),
			unplannedCastJudgePrompt(cfg, ch, fresh, cfg.Language)+"\n\nIMPORTANT: your previous answer was not valid JSON. Reply with ONLY the JSON array, nothing else.")
		if v2 := parseUnplannedCastVerdicts(raw2); len(v2) > 0 {
			verdicts = v2
		} else if err2 != nil && logger != nil {
			logger.WarnKey("log.unplanned_cast_failed", err2)
		}
	}
	en := i18n.NormalizeLanguage(cfg.Language) == i18n.LangEN
	next := *state
	next.Chapters = append([]ChapterState(nil), state.Chapters...)
	for _, name := range fresh {
		// Tolerant verdict matching: the LLM may echo the exact name, shorten it
		// ("Quill"), add an honorific, or even answer in another casing. Exact
		// normalized lookup first; otherwise fall back to any verdict sharing a
		// proper core name with the candidate.
		var v unplannedCastVerdict
		judged := false
		for _, cand := range verdicts {
			cn := normalizeArcName(cand.Name)
			if cn == "" {
				continue
			}
			if cn == normalizeArcName(name) {
				v, judged = cand, true
				break
			}
		}
		if !judged {
			nameCores := unplannedNameCores(name)
			for _, cand := range verdicts {
				match := false
				for _, cc := range unplannedNameCores(cand.Name) {
					for _, nc := range nameCores {
						if cc == nc || strings.Contains(cc, nc) || strings.Contains(nc, cc) {
							match = true
							break
						}
					}
					if match {
						break
					}
				}
				if match {
					v, judged = cand, true
					break
				}
			}
		}
		relevant := !judged || v.Relevant // unjudged names default to relevant: missing them silently breaks arc tracking forever
		if !relevant {
			res.Skipped = append(res.Skipped, name)
			continue
		}
		note := strings.TrimSpace(v.Role)
		if note == "" {
			if en {
				note = "introduced spontaneously in this chapter's draft"
			} else {
				note = "introducido espontáneamente en el borrador de este capítulo"
			}
		}
		next.Chapters[idx].Characters = append(next.Chapters[idx].Characters, OutlineChapterCharacter{
			Name: name,
			Note: Truncate(note, 120),
		})
		res.Registered = append(res.Registered, name)
		if judged && strings.TrimSpace(v.Role) != "" {
			res.Notes = append(res.Notes, name+": "+Truncate(v.Role, 120))
		}
	}
	if len(res.Registered) > 0 {
		if err := SaveProgress(progressPath, &next); err != nil {
			return nil, err
		}
		*state = next
		if logger != nil {
			logger.SuccessKey("log.unplanned_cast_registered", ch.Num, strings.Join(res.Registered, ", "))
		}
	}
	if len(res.Skipped) > 0 && logger != nil {
		logger.InfoKey("log.unplanned_cast_skipped", ch.Num, strings.Join(res.Skipped, ", "))
	}
	return res, nil
}

// AutoRegisterUnplannedCast runs the registration silently after a chapter is
// generated, keeping the outline cast truthful without user intervention. Any
// failure is logged and swallowed — the chapter itself is already written.
func AutoRegisterUnplannedCast(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, progressPath string, logger *sse.LogBroadcaster) {
	if _, err := RegisterUnplannedCastAction(ctx, apiCfg, cfg, state, idx, progressPath, logger); err != nil && logger != nil {
		logger.WarnKey("log.unplanned_cast_failed", err)
	}
}

// ensure unused-import guards for helpers referenced only in tests.
var _ = prose.CountProseUnits
