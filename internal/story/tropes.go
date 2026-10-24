package story

// Tropes: the subdivision of a written scene.
//
// A trope unit is NOT as atomic as a narrative beat (one emotional turn) nor
// as broad as a whole scene: it is a recognizable story *move* with its own
// dramatic identity — will-they-won't-they tension, a confrontation, a
// comforting interlude, a discovery/reveal, a chase, a bargaining session, a
// quiet aftermath, a betrayal turn. Scenes are segmented into trope units by
// classifying contiguous paragraph runs with structural cues (dialogue vs
// narration mode shifts, separators, size floors) plus a lightweight label
// pass, so indexes stay deterministic and survive reloads.
//
// Why tropes exist: when the fact-checker or the bestseller critic reports a
// continuity problem, the fix no longer has to rewrite the whole scene nor
// the whole chapter — it can target the exact trope unit(s) that contain the
// offending quote, keeping everything else byte-identical. That preserves
// voice, tense and word budget while still allowing surgical edits.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/prose"
	"showmethestory/internal/sse"
	"strings"
	"time"
)

const (
	// tropeMinParagraphs merges tiny units so a trope unit is never a single
	// stray line; units shorter than this get folded into their previous
	// sibling.
	tropeMinParagraphs = 2
	// maxTropeRewriteRetries bounds controlled retries when a trope-unit
	// rewrite comes back empty or collapses in length.
	maxTropeRewriteRetries = 2
	// sceneShrinkMaxRetries bounds the surgical "rewrite only the longest
	// scene" passes used to bring an over-long chapter back under its cap
	// before ever regenerating the whole chapter.
	sceneShrinkMaxRetries = 3
)

// TropeLabel classifies one trope unit of a scene by its dramatic move. It is
// derived from structural cues (dialogue density, position inside the scene) —
// no LLM call needed — and exists so critiques/repairs can talk about units
// ("the confrontation", "the quiet aftermath") instead of raw indexes.
type TropeLabel string

const (
	TropeOpening       TropeLabel = "opening"       // scene setup / arrival
	TropeTension       TropeLabel = "tension"       // will-they-won't-they, friction
	TropeDialogue      TropeLabel = "dialogue"      // dialogue-led exchange
	TropeAction        TropeLabel = "action"        // chase/fight/physical turn
	TropeReveal        TropeLabel = "reveal"        // discovery / information drop
	TropeIntrospection TropeLabel = "introspection" // interiority / processing
	TropeAftermath     TropeLabel = "aftermath"     // quiet resolution / hook close
	TropeTransition    TropeLabel = "transition"    // movement/time bridge
)

// paragraphUnits splits scene/chapter prose into trimmed paragraph units and
// the separator used to rebuild them (blank-line first, single-newline
// fallback — same rule as splitBlockParagraphs so block math matches).
func paragraphUnits(content string) ([]string, string) {
	return splitBlockParagraphs(strings.TrimSpace(content))
}

// paraIsDialogue reports whether a paragraph is led by spoken dialogue.
func paraIsDialogue(p string) bool {
	t := strings.TrimSpace(p)
	if t == "" {
		return false
	}
	for _, r := range t {
		switch r {
		case '“', '"', '«', '\'', '‘', '-': // open quotes / em-dash speaker cue
			return true
		case ' ', '\t':
			continue
		default:
		}
		break
	}
	// Dialogue buried mid-paragraph still counts when quoted speech makes up
	// most of the text (quick heuristic: at least two quote marks).
	q := strings.Count(t, "“") + strings.Count(t, "\"") + strings.Count(t, "«")
	return q >= 2
}

// splitTropes segments one scene's prose into narrative beats using
// structural cues only:
//   - a hard break on "***"/"---" separators (mini scene-shifts);
//   - a soft break where narration gives way to a dialogue-led paragraph or
//     vice versa AND the current beat already holds tropeMinParagraphs
//     paragraphs (alternation marks the turn between action and exchange).
//
// Returns the beat texts (never empty entries) and their [start,end)
// paragraph ranges over the scene's paragraph list.
func splitTropes(sceneProse string) ([]string, [][2]int) {
	paras, sep := paragraphUnits(sceneProse)
	return splitTropesFromParas(paras, sceneProse, sep)
}

// splitTropesFromParas is the paragraph-list core of splitTropes. It works on
// an ALREADY-SPLIT paragraph slice (so callers that segmented content with a
// different separator — e.g. single newlines — keep their own join separator)
// and returns beat texts joined with sep plus their [start,end) paragraph
// ranges. Break-marker paragraphs are dropped from BOTH the unit text and the
// spans: every span covers only kept prose paragraphs, so units never start
// or end on a "***" line. When a break marker is the sole divider between two
// beats they remain separate units (hard break = mini scene-shift).
func splitTropesFromParas(paras []string, sceneProse, sep string) ([]string, [][2]int) {
	if len(paras) == 0 {
		return nil, nil
	}
	if sep == "" {
		sep = "\n\n"
	}
	var beats []string
	var ranges [][2]int
	start := 0
	mode := paraIsDialogue(paras[0])
	// flush closes the current beat over paras[start:end]. The emitted span
	// is [firstKept, lastKept+1] over non-break paragraphs; the marker-only
	// edges are absorbed by re-anchoring start to the first kept paragraph
	// after the cut so consecutive ranges still tile all PROSE paragraphs.
	flush := func(end int) {
		if end <= start {
			return
		}
		var kept []string
		first, last := -1, -1
		for i, p := range paras[start:min(end, len(paras))] {
			if paraIsBreak(p) {
				continue
			}
			if first < 0 {
				first = i
			}
			last = i
			kept = append(kept, p)
		}
		text := strings.Join(kept, sep)
		if strings.TrimSpace(text) != "" {
			beats = append(beats, text)
			ranges = append(ranges, [2]int{start + first, start + last + 1})
		}
		start = end
	}
	for i := 1; i < len(paras); i++ {
		if paraIsBreak(paras[i]) {
			// Hard break: close the current beat at the marker (the marker
			// itself belongs to no unit) and reset the mode from the next
			// real paragraph.
			flush(i)
			for start < len(paras) && paraIsBreak(paras[start]) {
				start++
			}
			if start < len(paras) {
				mode = paraIsDialogue(paras[start])
			}
			continue
		}
		m := paraIsDialogue(paras[i])
		if m != mode && i-start >= tropeMinParagraphs {
			flush(i)
			mode = m
		}
	}
	flush(len(paras))
	// Merge tiny trailing beats into their predecessor so a trope unit is
	// never a single stray paragraph when merging is possible. Merging keeps
	// the spans contiguous (prev.lo .. last.hi), preserving the tiling.
	for len(beats) > 1 && paragraphCountWithSep(beats[len(beats)-1], sep) < tropeMinParagraphs {
		last := beats[len(beats)-1]
		prev := beats[len(beats)-2]
		beats = append(beats[:len(beats)-2], prev+sep+last)
		lo := ranges[len(ranges)-2][0]
		hi := ranges[len(ranges)-1][1]
		ranges = append(ranges[:len(ranges)-2], [2]int{lo, hi})
	}
	if len(beats) == 0 {
		return []string{strings.TrimSpace(sceneProse)}, [][2]int{{0, len(paras)}}
	}
	if n := len(ranges); n > 0 && ranges[n-1][1] != len(paras) {
		ranges[n-1][1] = len(paras) // absorb any skipped empty tail
	}
	return beats, ranges
}

// paraIsBreak reports whether a paragraph is only a scene-break marker.
func paraIsBreak(p string) bool {
	switch strings.TrimSpace(p) {
	case "---", "----", "***", "⁂", "* * *":
		return true
	}
	return false
}

func paragraphCount(text string) int {
	p, _ := paragraphUnits(text)
	return len(p)
}

func paragraphCountWithSep(text, sep string) int {
	if sep == "" {
		sep = "\n\n"
	}
	n := 1
	for i := 0; i+len(sep) <= len(text); {
		if strings.HasPrefix(text[i:], sep) {
			n++
			i += len(sep)
			continue
		}
		i++
	}
	return n
}

// sceneRangeForBlock maps a block index to the planned scene that owns it,
// using BlockStart/BlockEnd recorded by SyncChapterBlocksSceneAware. Returns
// (-1,false) when the chapter has no per-scene block bookkeeping.
func sceneRangeForBlock(plan *ScenePlan, blockIdx int) (int, bool) {
	if plan == nil {
		return -1, false
	}
	for _, sc := range plan.Scenes {
		if sc.BlockEnd > 0 && blockIdx >= sc.BlockStart && blockIdx <= sc.BlockEnd {
			return sc.ID, true
		}
	}
	return -1, false
}

// TropeUnit is one trope segment of a scene: contiguous prose that carries a
// recognizable story move (not as atomic as a single emotional beat, not as
// broad as the whole scene). Units are derived deterministically from the
// accepted prose so indexes survive reloads without extra LLM calls.
type TropeUnit struct {
	Index     int        `json:"index"`
	Label     TropeLabel `json:"label"`
	ParaStart int        `json:"para_start"` // inclusive, scene-local
	ParaEnd   int        `json:"para_end"`   // exclusive
	Words     int        `json:"words"`
	Excerpt   string     `json:"excerpt,omitempty"` // first ~15 words
}

// classifyTrope labels a unit from structural cues only: dialogue density,
// position within the scene and small keyword heuristics on the opening line.
func classifyTrope(text string, isFirst, isLast bool) TropeLabel {
	paras, _ := paragraphUnits(text)
	if len(paras) == 0 {
		return TropeTransition
	}
	dial := 0
	for _, p := range paras {
		if paraIsDialogue(p) {
			dial++
		}
	}
	density := float64(dial) / float64(len(paras))
	lower := strings.ToLower(text)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(lower, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("suddenly", "ran", "ran at", "charged", "attacked", "chased", "fought", "shot", "escaped", "de repente", "corrió", "atacó", "persigui", "luchó", "dispar", "escap"):
		return TropeAction
	case has("realized", "discovered", "found out", "revealed", "confessed", "the letter said", "se dio cuenta", "descubri", "revel", "confes", "la carta dec"):
		return TropeReveal
	case density >= 0.55:
		return TropeDialogue
	case has("but he did not answer", "hesitated", "tension", "argued", "refused", "negotiat", "dud", "discuti", "se neg", "negoci", "tenso"):
		return TropeTension
	case isLast && density < 0.3:
		return TropeAftermath
	case isFirst && density < 0.3:
		return TropeOpening
	case density < 0.2:
		return TropeIntrospection
	default:
		return TropeTransition
	}
}

// buildTropeUnits turns splitTropes output into labeled TropeUnits with word
// counts and short excerpts.
func buildTropeUnits(beats []string, ranges [][2]int) []TropeUnit {
	units := make([]TropeUnit, 0, len(beats))
	for i, b := range beats {
		r := [2]int{i, i + 1}
		if i < len(ranges) {
			r = ranges[i]
		}
		fields := strings.Fields(b)
		excerpt := strings.Join(fields[:min(15, len(fields))], " ")
		units = append(units, TropeUnit{
			Index:     i,
			Label:     classifyTrope(b, i == 0, i == len(beats)-1),
			ParaStart: r[0],
			ParaEnd:   r[1],
			Words:     prose.CountProseUnits(b),
			Excerpt:   excerpt,
		})
	}
	return units
}

// tropesOfScene returns the labeled trope units of one planned scene, caching
// them on Scene.TropeUnits when the plan object is available.
func tropesOfScene(sceneText string, sc *Scene) []TropeUnit {
	beats, ranges := splitTropes(sceneText)
	return applyTropeSegmentation(sc, beats, ranges)
}

// applyTropeSegmentation records a pre-computed trope segmentation (beats +
// paragraph ranges produced by splitTropesFromParas over the scene's final
// paragraph slice) on the scene. Same contract as tropesOfScene but avoids
// re-splitting with a possibly different separator.
func applyTropeSegmentation(sc *Scene, beats []string, ranges [][2]int) []TropeUnit {
	units := buildTropeUnits(beats, ranges)
	if sc != nil {
		sc.TropeRanges = ranges
		sc.TropeUnits = units
	}
	return units
}

// SceneSpanForPos resolves the paragraph span of scene pos over paras using
// recorded BlockStart/BlockEnd when present, falling back to budget walking.
// Exported wrapper used by the writing pipeline to record trope ranges right
// after each scene is accepted.
func SceneSpanForPos(ch *ChapterState, scenePos int, paras []string) (int, int) {
	return sceneSpanFromBlocks(ch, scenePos, paras)
}

// LongestWrittenScenePos returns the index (into plan.Scenes) of the scene
// whose written prose is furthest ABOVE its word budget — the surgical target
// when a chapter overshoots its maximum length. Returns (-1, 0) when no scene
// overshoots.
func LongestWrittenScenePos(plan *ScenePlan, content string) (int, int) {
	if plan == nil || len(plan.Scenes) == 0 || strings.TrimSpace(content) == "" {
		return -1, 0
	}
	paras, sep := paragraphUnits(content)
	best, bestExcess := -1, 0
	cum := 0
	for pos := range plan.Scenes {
		n := sceneParaEstimate(paras, cum, plan, pos)
		hi := min(cum+n, len(paras))
		if hi <= cum {
			break
		}
		w := prose.CountProseUnits(strings.Join(paras[cum:hi], sep))
		if ex := w - plan.Scenes[pos].WordsBudget; ex > bestExcess {
			best, bestExcess = pos, ex
		}
		cum = hi
		if cum >= len(paras) {
			break
		}
	}
	return best, bestExcess
}

// shrinkSceneToBudget surgically rewrites ONE scene of an over-long chapter so
// it lands near its word budget, leaving every other scene byte-identical. It
// retries up to sceneShrinkMaxRetries times when the replacement comes back
// empty or still wildly over budget. Returns the new full-chapter content.
func shrinkSceneToBudget(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, settings *ProjectSettings, scenePos, maxLen int, logger *sse.LogBroadcaster) (string, bool) {
	ch := &state.Chapters[idx]
	plan := ch.ScenePlan
	if plan == nil || scenePos < 0 || scenePos >= len(plan.Scenes) {
		return "", false
	}
	en := i18n.NormalizeLanguage(cfg.Language) == i18n.LangEN
	paras, sep := paragraphUnits(ch.Content)
	lo, hi := sceneSpanFromBlocks(ch, scenePos, paras)
	if hi <= lo {
		return "", false
	}
	sceneText := strings.Join(paras[lo:hi], sep)
	sc := plan.Scenes[scenePos]
	target := sc.WordsBudget
	if target <= 0 {
		target = maxLen / maxOf(1, len(plan.Scenes))
	}
	bandMin, bandMax := calcSceneLengthRange(target)
	title := sceneTitleOf(sc, cfg.Language)
	if logger != nil {
		logger.InfoKey("log.scene_shrink_start", ch.Num, scenePos+1, len(plan.Scenes), title, prose.CountProseUnits(sceneText), target)
	}
	feedback := ""
	var best string
	bestDist := -1
	for attempt := 0; attempt <= sceneShrinkMaxRetries; attempt++ {
		if ctx.Err() != nil {
			return "", false
		}
		var userPrompt strings.Builder
		if en {
			fmt.Fprintf(&userPrompt, "You are revising ONE scene of chapter %d \"%s\" because the whole chapter overshot its maximum length. Rewrite ONLY this scene so it comes in around %d words (acceptable range %d–%d). Keep every plot event, character action and key dialogue of the scene, but cut redundant description, repeated narration and padded exchanges until it fits the range. Do not touch any other scene.\n\n", ch.Num, ch.Title, target, bandMin, bandMax)
			userPrompt.WriteString("[Scene to rewrite]\n" + sceneText + "\n\n")
			if feedback != "" {
				userPrompt.WriteString(feedback + "\n\n")
			}
			userPrompt.WriteString("Output ONLY the rewritten scene prose — no titles, notes or dividers.")
		} else {
			fmt.Fprintf(&userPrompt, "Estás revisando UNA escena del capítulo %d \"%s\" porque el capítulo superó su longitud máxima. Reescribe SOLO esta escena para que quede cerca de %d palabras (rango aceptable %d–%d). Conserva todos los eventos, acciones y diálogos clave de la escena, pero corta descripción redundante, narración repetida e intercambios inflados hasta entrar en el rango. No toques ninguna otra escena.\n\n", ch.Num, ch.Title, target, bandMin, bandMax)
			userPrompt.WriteString("[Escena a reescribir]\n" + sceneText + "\n\n")
			if feedback != "" {
				userPrompt.WriteString(feedback + "\n\n")
			}
			userPrompt.WriteString("Devuelve SOLO la prosa de la escena reescrita — sin títulos, notas ni separadores.")
		}
		systemPrompt := state.CorePrompt
		if systemPrompt == "" {
			systemPrompt = i18n.SystemPromptFor(cfg.Language, "author_default")
		}
		var raw string
		if logger != nil {
			raw = llm.CallAPIWithRetryLog(ctx, apiCfg, systemPrompt, userPrompt.String(), logger)
		} else {
			raw = llm.CallAPIWithRetry(ctx, apiCfg, systemPrompt, userPrompt.String())
		}
		newScene := stripChapterMetaProse(raw, cfg.Language)
		if strings.TrimSpace(newScene) == "" {
			if logger != nil {
				logger.WarnKey("log.scene_shrink_failed", ch.Num, scenePos+1, title, attempt+1)
			}
			continue
		}
		n := prose.CountProseUnits(newScene)
		dist := n - target
		if dist < 0 {
			dist = -dist
		}
		if bestDist < 0 || dist < bestDist {
			bestDist, best = dist, newScene
		}
		if n >= bandMin && n <= bandMax {
			break
		}
		if attempt < sceneShrinkMaxRetries {
			if logger != nil {
				logger.WarnKey("log.scene_shrink_retry", ch.Num, scenePos+1, title, n, target, attempt+1, sceneShrinkMaxRetries)
			}
			if n > bandMax {
				feedback = fmt.Sprintf("Your previous rewrite was %d words — still too long. Cut at least %d more words.", n, n-bandMax)
			} else {
				feedback = fmt.Sprintf("Your previous rewrite was %d words — too short now. Deepen it with roughly %d more words without inventing new events.", n, bandMin-n)
			}
			continue
		}
	}
	if best == "" {
		return "", false
	}
	newParas, _ := paragraphUnits(best)
	replaced := make([]string, 0, len(paras)-(hi-lo)+len(newParas))
	replaced = append(replaced, paras[:lo]...)
	replaced = append(replaced, newParas...)
	replaced = append(replaced, paras[hi:]...)
	newContent := strings.Join(replaced, sep)
	SyncChapterBlocksSceneAware(ch, newContent)
	if logger != nil {
		logger.SuccessKey("log.scene_shrink_done", ch.Num, scenePos+1, title, prose.CountProseUnits(best), prose.CountProseUnits(newContent))
	}
	return newContent, true
}

// locateQuoteInContent finds the paragraph containing quote (exact → then
// case-insensitive → then normalized word overlap) and returns its position
// plus the scene ID and beat index that own it. Falls back gracefully when
// the chapter has no scene plan (sceneID=-1, beatIdx points into the
// whole-chapter beat list).
type tropeLocation struct {
	SceneID   int
	ScenePos  int // index into plan.Scenes, -1 when unplanned
	BeatIdx   int // index within the owning scene's beats
	ParaStart int // absolute paragraph offset in the chapter
	ParaEnd   int // exclusive
	Found     bool
}

func locateTropeForQuote(ch *ChapterState, quote string) tropeLocation {
	loc := tropeLocation{SceneID: -1, ScenePos: -1, BeatIdx: -1}
	if strings.TrimSpace(quote) == "" || strings.TrimSpace(ch.Content) == "" {
		return loc
	}
	norm := normalizeFactText(quote)
	paras, _ := paragraphUnits(ch.Content)
	matchAt := func(p string) bool {
		if strings.Contains(p, quote) {
			return true
		}
		np := normalizeFactText(p)
		if np != "" && norm != "" {
			if strings.Contains(np, norm) || containsWordWindow(norm, np) {
				return true
			}
		}
		return false
	}
	pi := -1
	for i, p := range paras {
		if matchAt(p) {
			pi = i
			break
		}
	}
	if pi < 0 {
		return loc
	}
	loc.Found = true
	loc.ParaStart, loc.ParaEnd = pi, pi+1
	plan := ch.ScenePlan
	if plan == nil || len(plan.Scenes) == 0 {
		// Unplanned chapter: treat the whole content as one pseudo-scene and
		// split it into beats so a targeted rewrite still has a scope.
		beats, _ := splitTropes(ch.Content)
		for bi, b := range beats {
			if matchAt(b) {
				loc.BeatIdx = bi
				break
			}
		}
		return loc
	}
	// Walk scenes in order counting their paragraphs to map the absolute
	// paragraph index onto (scene, beat).
	cum := 0
	for pos := range plan.Scenes {
		blockLo, blockHi := plan.Scenes[pos].BlockStart, plan.Scenes[pos].BlockEnd
		spanLo, spanHi := -1, -1
		if blockHi > 0 && len(ch.Blocks) > 0 {
			// Prefer block-based ranges when available.
			if blockLo < len(ch.Blocks) && blockHi < len(ch.Blocks) {
				spanLo = FindParaOffsetOfBlock(ch, blockLo)
				spanHi = FindParaOffsetOfBlock(ch, blockHi) + 1
			}
		}
		if spanLo < 0 {
			// Fallback: derive the scene span from TropeRanges if recorded,
			// otherwise estimate proportionally from budgets.
			spanLo = cum
			n := sceneParaEstimate(paras, cum, plan, pos)
			spanHi = min(spanLo+n, len(paras))
		}
		if pi >= spanLo && pi < spanHi {
			loc.SceneID = plan.Scenes[pos].ID
			loc.ScenePos = pos
			sceneText := strings.Join(paras[spanLo:spanHi], "\n\n")
			_, beats := splitTropes(sceneText)
			rel := pi - spanLo
			for bi, r := range beats {
				if rel >= r[0] && rel < r[1] {
					loc.BeatIdx = bi
					loc.ParaStart, loc.ParaEnd = spanLo+r[0], spanLo+r[1]
					break
				}
			}
			if loc.BeatIdx < 0 {
				loc.BeatIdx = 0
				loc.ParaStart, loc.ParaEnd = spanLo, spanHi
			}
			return loc
		}
		cum = spanHi
	}
	// Quote matched but outside every known span — keep paragraph hit info.
	loc.SceneID, loc.ScenePos = plan.Scenes[len(plan.Scenes)-1].ID, len(plan.Scenes)-1
	return loc
}

// FindParaOffsetOfBlock maps a block index to its absolute paragraph offset
// in ch.Content (blocks are rebuilt from paragraphs one-to-one, so the block
// index itself is the paragraph index unless scene_break markers were kept).
func FindParaOffsetOfBlock(ch *ChapterState, blockIdx int) int {
	if blockIdx < 0 || blockIdx >= len(ch.Blocks) {
		return -1
	}
	paras, _ := paragraphUnits(ch.Content)
	want := strings.TrimSpace(ch.Blocks[blockIdx].Text)
	acc := 0
	for i, p := range paras {
		if strings.TrimSpace(p) == want {
			return i
		}
		acc = i
	}
	_ = acc
	return -1
}

// sceneParaEstimate estimates how many paragraphs scene pos occupies starting
// at offset cum, using its word budget relative to neighbouring budgets.
func sceneParaEstimate(paras []string, cum int, plan *ScenePlan, pos int) int {
	if pos < 0 || pos >= len(plan.Scenes) {
		return len(paras) - cum
	}
	sc := plan.Scenes[pos]
	if sc.WordsBudget <= 0 {
		share := (len(paras) - cum) / max(1, len(plan.Scenes)-pos)
		return max(1, share)
	}
	// Consume paragraphs until the running word count passes the budget.
	counted := 0
	for i := cum; i < len(paras); i++ {
		counted += prose.CountProseUnits(paras[i])
		if counted >= sc.WordsBudget {
			return i - cum + 1
		}
	}
	return len(paras) - cum
}

// containsWordWindow reports whether every ~6-word window of needle appears
// (normalized) inside hay — tolerant quote matching for model-quoted text.
func containsWordWindow(needle, hay string) bool {
	nw := strings.Fields(needle)
	hw := strings.Fields(hay)
	if len(nw) == 0 || len(hw) == 0 {
		return false
	}
	w := min(6, len(nw))
	haySet := map[string]bool{}
	for i := 0; i+w <= len(hw); i++ {
		haySet[strings.Join(hw[i:i+w], " ")] = true
	}
	for i := 0; i+w <= len(nw); i++ {
		if !haySet[strings.Join(nw[i:i+w], " ")] {
			return false
		}
	}
	return true
}

// buildTropeRewritePrompt asks the model to rewrite ONLY the failing beat of
// ONE scene while keeping the rest of the scene verbatim. The reply must be
// the full new scene text so the replacement is unambiguous.
func buildTropeRewritePrompt(cfg *config.Config, state *Progress, idx int, settings *ProjectSettings, sceneTitle string, sceneText string, beats []string, beatIdx int, feedback string, lang string) (userPrompt, systemPrompt string) {
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	var sb strings.Builder
	characterContext, worldviewContext := buildChapterSettingsContexts(settings, state.Chapters[idx], lang)
	if characterContext != "" {
		if en {
			sb.WriteString("[Characters]\n" + characterContext + "\n\n")
		} else {
			sb.WriteString("[Personajes]\n" + characterContext + "\n\n")
		}
	}
	if worldviewContext != "" {
		if en {
			sb.WriteString("[Worldview]\n" + worldviewContext + "\n\n")
		} else {
			sb.WriteString("[Cosmovisión]\n" + worldviewContext + "\n\n")
		}
	}
	if en {
		fmt.Fprintf(&sb, "The scene \"%s\" of chapter %d needs ONE beat fixed for consistency. Rewrite ONLY that beat and return the COMPLETE scene with every other beat copied verbatim.\n\n", sceneTitle, state.Chapters[idx].Num)
		sb.WriteString("[Full current scene text]\n" + strings.TrimSpace(sceneText) + "\n\n")
		sb.WriteString(fmt.Sprintf("[Beat to rewrite — beat %d of %d]\n%s\n\n", beatIdx+1, len(beats), beats[beatIdx]))
		sb.WriteString("[Editor feedback about this beat]\n" + strings.TrimSpace(feedback) + "\n\n")
		sb.WriteString("Rules:\n- Keep voice, tense and point of view identical to the surrounding beats.\n- Fix exactly what the feedback describes; change nothing else.\n- Preserve the beat's approximate length (±10%).\n- Return ONLY the complete revised scene prose — no titles, notes or dividers.\n")
	} else {
		fmt.Fprintf(&sb, "La escena \"%s\" del capítulo %d necesita corregir UN beat por consistencia. Reescribe SOLO ese beat y devuelve la escena COMPLETA con los demás beats copiados literalmente.\n\n", sceneTitle, state.Chapters[idx].Num)
		sb.WriteString("[Texto actual completo de la escena]\n" + strings.TrimSpace(sceneText) + "\n\n")
		sb.WriteString(fmt.Sprintf("[Beat a reescribir — beat %d de %d]\n%s\n\n", beatIdx+1, len(beats), beats[beatIdx]))
		sb.WriteString("[Feedback editorial sobre este beat]\n" + strings.TrimSpace(feedback) + "\n\n")
		sb.WriteString("Reglas:\n- Mantén voz, tiempo verbal y punto de vista idénticos a los beats vecinos.\n- Corrige exactamente lo que describe el feedback; no cambies nada más.\n- Conserva la extensión aproximada del beat (±10%).\n- Devuelve SOLO la prosa completa de la escena revisada — sin títulos, notas ni separadores.\n")
	}
	userPrompt = sb.String() + factProtection(state, state.Chapters[idx].Num, lang)
	systemPrompt = state.CorePrompt
	if systemPrompt == "" {
		systemPrompt = i18n.SystemPromptFor(lang, "author_default")
	}
	return userPrompt, systemPrompt
}

// rewriteSceneTrope replaces one beat inside a scene via the LLM, verifies the
// returned text really changed only that beat region and keeps the scene
// length within ±15% of the original, and splices it back into the chapter
// content. It reports the new content and the updated scene text.
func rewriteSceneTrope(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, settings *ProjectSettings, scenePos, beatIdx int, feedback string, logger *sse.LogBroadcaster) (newContent string, err error) {
	ch := &state.Chapters[idx]
	plan := ch.ScenePlan
	if plan == nil || scenePos < 0 || scenePos >= len(plan.Scenes) {
		return "", fmt.Errorf("no scene plan for beat rewrite")
	}
	paras, sep := paragraphUnits(ch.Content)
	sc := plan.Scenes[scenePos]
	spanLo, spanHi := sceneSpanFromBlocks(ch, scenePos, paras)
	if spanHi <= spanLo {
		return "", fmt.Errorf("scene span not found for beat rewrite")
	}
	sceneText := strings.Join(paras[spanLo:spanHi], sep)
	// Segment the scene over the SAME paragraph slice the splicer uses, with the
	// chapter's own separator, so beat indices and ParaStart/ParaEnd ranges stay
	// consistent with the block/trope bookkeeping (avoids confusing tropes with
	// beats when the content mixes blank-line and single-newline separators).
	var sceneParas []string
	for _, p := range paras[spanLo:spanHi] {
		if strings.TrimSpace(p) == "" {
			continue
		}
		sceneParas = append(sceneParas, p)
	}
	if ch.BlockSep != "" {
		sep = ch.BlockSep
	}
	beats, beatRanges := splitTropesFromParas(sceneParas, sceneText, sep)
	if len(beats) == 0 {
		return "", fmt.Errorf("scene has no prose beats to rewrite")
	}
	// Out-of-range indexes are clamped to the nearest real beat instead of
	// silently rewriting beat 1 — an intervention that points past the end of
	// the scene almost always means "the last unit".
	if beatIdx < 0 {
		beatIdx = 0
	}
	if beatIdx >= len(beats) {
		beatIdx = len(beats) - 1
	}
	if logger != nil {
		logger.InfoKey("log.trope_rewrite_start", ch.Num, sc.ID, beatIdx+1, len(beats))
	}
	userPrompt, systemPrompt := buildTropeRewritePrompt(cfg, state, idx, settings, sceneTitleOf(sc, cfg.Language), sceneText, beats, beatIdx, feedback, cfg.Language)
	origWords := prose.CountProseUnits(sceneText)
	// Length guard tiers: the strict band (±25%) accepts immediately; a relaxed
	// band (±45%) is kept as best-effort fallback so one chatty LLM answer
	// cannot make the whole intervention fail. The rewrite only replaces ONE
	// beat, so ±15% on the WHOLE scene was far too tight and rejected valid
	// surgical fixes whenever the fixed beat grew or shrank a little.
	const strictLo, strictHi = 75, 125
	bestScene, bestDev := "", int(^uint(0)>>1)
	for attempt := 0; attempt <= maxTropeRewriteRetries; attempt++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		var raw string
		if logger != nil {
			raw = llm.CallAPIWithRetryLog(ctx, apiCfg, systemPrompt, userPrompt, logger)
		} else {
			raw = llm.CallAPIWithRetry(ctx, apiCfg, systemPrompt, userPrompt)
		}
		newScene := stripChapterMetaProse(raw, cfg.Language)
		if strings.TrimSpace(newScene) == "" {
			if logger != nil {
				logger.WarnKey("log.trope_rewrite_failed", ch.Num, sc.ID, attempt+1)
			}
			continue
		}
		nw := prose.CountProseUnits(newScene)
		pct := 100
		if origWords > 0 {
			pct = nw * 100 / origWords
		}
		dev := pct - 100
		if dev < 0 {
			dev = -dev
		}
		inStrict := pct >= strictLo && pct <= strictHi
		if !inStrict && logger != nil {
			logger.WarnKey("log.trope_rewrite_length_guard", ch.Num, sc.ID, nw, origWords)
		}
		if origWords > 0 && (!inStrict || dev < bestDev) {
			// Remember the closest-to-original attempt: if no retry lands inside
			// the strict band we still apply the best-effort rewrite below.
			if !inStrict || dev < bestDev {
				bestDev = dev
				bestScene = newScene
			}
			if !inStrict {
				continue
			}
		}
		newContent = spliceSceneIntoContent(paras, sep, spanLo, spanHi, newScene)
		SyncChapterBlocksSceneAware(ch, newContent)
		resegmentTouchedScene(ch, scenePos, beatRanges)
		if logger != nil {
			logger.SuccessKey("log.trope_rewrite_done", ch.Num, sc.ID, beatIdx+1, nw)
		}
		return newContent, nil
	}
	// Every attempt missed the strict band but produced usable prose: apply the
	// closest-to-target-length rewrite as a best effort instead of failing the
	// intervention outright (the author can always re-run it from the UI).
	if strings.TrimSpace(bestScene) != "" {
		newContent := spliceSceneIntoContent(paras, sep, spanLo, spanHi, bestScene)
		SyncChapterBlocksSceneAware(ch, newContent)
		resegmentTouchedScene(ch, scenePos, beatRanges)
		if logger != nil {
			logger.WarnKey("log.trope_rewrite_besteffort", ch.Num, sc.ID, beatIdx+1)
			logger.SuccessKey("log.trope_rewrite_done", ch.Num, sc.ID, beatIdx+1, prose.CountProseUnits(bestScene))
		}
		return newContent, nil
	}
	return "", fmt.Errorf("beat rewrite failed after %d attempts", maxTropeRewriteRetries+1)
}

// spliceSceneIntoContent replaces paras[lo:hi] with the rewritten scene text,
// keeping every other paragraph untouched and joining with sep.
func spliceSceneIntoContent(paras []string, sep string, lo, hi int, newScene string) string {
	newParas, _ := paragraphUnits(newScene)
	replaced := make([]string, 0, len(paras)-(hi-lo)+len(newParas))
	replaced = append(replaced, paras[:lo]...)
	replaced = append(replaced, newParas...)
	replaced = append(replaced, paras[hi:]...)
	return strings.Join(replaced, sep)
}

// resegmentTouchedScene refreshes the trope segmentation (TropeUnits /
// TropeRanges with scene-local ParaStart/ParaEnd) of one scene right after a
// surgical beat rewrite. Without it the recorded units keep pointing at the
// pre-rewrite paragraph layout: syncSceneBlocks then fails its tiling check,
// silently falls back to paragraph blocks and the viewer loses the per-trope
// visual grouping ("everything merged after a rewrite"). The ranges come from
// the exact segmentation used to build the rewrite prompt, so unit indexes
// stay aligned with what the critic saw.
func resegmentTouchedScene(ch *ChapterState, scenePos int, ranges [][2]int) {
	if ch.ScenePlan == nil || scenePos < 0 || scenePos >= len(ch.ScenePlan.Scenes) || len(ranges) == 0 {
		return
	}
	sc := &ch.ScenePlan.Scenes[scenePos]
	lo, hi := sc.ParaStart, sc.ParaEnd
	paras, _ := splitBlockParagraphs(ch.Content)
	if lo < 0 || hi > len(paras) || hi <= lo {
		// Span bookkeeping is stale after the rewrite: re-derive it from the
		// refreshed block ranges before segmenting.
		refreshSceneBlockRanges(ch)
		lo, hi = sc.BlockStart, sc.BlockEnd
		if hi >= len(ch.Blocks) || lo < 0 {
			return
		}
		var sceneParas []string
		for bi := lo; bi <= hi && bi < len(ch.Blocks); bi++ {
			for _, p := range strings.Split(ch.Blocks[bi].Text, "\n\n") {
				if strings.TrimSpace(p) != "" {
					sceneParas = append(sceneParas, p)
				}
			}
		}
		if len(sceneParas) == 0 {
			return
		}
		_, fresh := splitTropesFromParas(sceneParas, "", ch.BlockSep)
		if len(fresh) > 0 {
			applyTropeSegmentation(sc, tropeTextsOfSpans(sceneParas, fresh, ch.BlockSep), fresh)
		}
		return
	}
	sceneParas := paras[lo:hi]
	if len(ranges) > len(sceneParas) {
		// The rewritten scene changed its paragraph count: re-segment live over
		// the current slice so units tile exactly.
		_, fresh := splitTropesFromParas(sceneParas, "", ch.BlockSep)
		if len(fresh) > 0 {
			applyTropeSegmentation(sc, tropeTextsOfSpans(sceneParas, fresh, ch.BlockSep), fresh)
		}
		return
	}
	beats := tropeTextsOfSpans(sceneParas, ranges, ch.BlockSep)
	applyTropeSegmentation(sc, beats, ranges)
	sc.ParaStart, sc.ParaEnd = lo, hi
}

// tropeTextsOfSpans joins the paragraphs covered by each [start,end) span.
func tropeTextsOfSpans(paras []string, spans [][2]int, sep string) []string {
	if sep == "" {
		sep = "\n\n"
	}
	out := make([]string, 0, len(spans))
	for _, sp := range spans {
		lo, hi := sp[0], sp[1]
		if lo < 0 {
			lo = 0
		}
		if hi > len(paras) {
			hi = len(paras)
		}
		if lo >= hi {
			continue
		}
		out = append(out, strings.Join(paras[lo:hi], sep))
	}
	return out
}

// sceneSpanFromBlocks resolves the paragraph span of scene pos from its
// BlockStart/BlockEnd when present, falling back to budget walking.
func sceneSpanFromBlocks(ch *ChapterState, scenePos int, paras []string) (int, int) {
	plan := ch.ScenePlan
	if plan == nil || scenePos >= len(plan.Scenes) {
		return 0, len(paras)
	}
	sc := plan.Scenes[scenePos]
	// Preferred source: the EXACT absolute paragraph span recorded by the
	// sequential writer when the scene was accepted. Trusting it first keeps
	// trope/beat bookkeeping aligned with the real text even when block ranges
	// were derived with a different granularity (avoids confusing tropes with
	// beats or scenes with adjacent ones).
	if sc.ParaEnd > sc.ParaStart && sc.ParaEnd <= len(paras) {
		return sc.ParaStart, sc.ParaEnd
	}
	if sc.BlockEnd > 0 && len(ch.Blocks) > 0 && sc.BlockEnd < len(ch.Blocks) {
		lo := FindParaOffsetOfBlock(ch, sc.BlockStart)
		hi := FindParaOffsetOfBlock(ch, sc.BlockEnd)
		if lo >= 0 && hi >= lo {
			return lo, hi + 1
		}
	}
	// Budget-walk fallback across all scenes.
	cum := 0
	for pos := range plan.Scenes {
		n := sceneParaEstimate(paras, cum, plan, pos)
		if pos == scenePos {
			return cum, min(cum+n, len(paras))
		}
		cum += n
		if cum >= len(paras) {
			break
		}
	}
	return 0, len(paras)
}

func sceneTitleOf(sc Scene, lang string) string {
	if t := strings.TrimSpace(sc.Title); t != "" {
		return t
	}
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return fmt.Sprintf("Scene %d", sc.ID)
	}
	return fmt.Sprintf("Escena %d", sc.ID)
}

// SyncChapterBlocksSceneAware re-derives blocks from content EXCEPT that it
// keeps the existing block list whenever its texts still match the content
// paragraph-for-paragraph (typical after a beat rewrite inside one scene:
// only that scene's blocks change). New content paragraphs get fresh IDs;
// removed ones drop out. Afterwards it refreshes each planned scene's
// BlockStart/BlockEnd so block↔scene mapping stays accurate.
func SyncChapterBlocksSceneAware(ch *ChapterState, newContent string) {
	oldParas, oldSep := paragraphUnits(ch.Content)
	newParas, newSep := paragraphUnits(newContent)
	ch.Content = newContent
	ch.BlockSep = newSep
	if len(oldParas) == len(newParas) && oldSep == newSep {
		diff := 0
		firstDiff := -1
		for i := range oldParas {
			if oldParas[i] != newParas[i] {
				diff++
				if firstDiff < 0 {
					firstDiff = i
				}
			}
		}
		if diff == 0 {
			SyncChapterBlocks(ch)
			refreshSceneBlockRanges(ch)
			return
		}
	}
	SyncChapterBlocks(ch)
	refreshSceneBlockRanges(ch)
}

// refreshSceneBlockRanges recomputes BlockStart/BlockEnd for every scene in
// the plan against the chapter's current block list.
func refreshSceneBlockRanges(ch *ChapterState) {
	if ch.ScenePlan == nil {
		return
	}
	paras := make([]string, len(ch.Blocks))
	for i, b := range ch.Blocks {
		paras[i] = b.Text
	}
	cum := 0
	for pos := range ch.ScenePlan.Scenes {
		n := sceneParaEstimate(paras, cum, ch.ScenePlan, pos)
		if cum >= len(paras) {
			break
		}
		end := min(cum+n, len(paras)) - 1
		ch.ScenePlan.Scenes[pos].BlockStart = cum
		ch.ScenePlan.Scenes[pos].BlockEnd = end
		cum = end + 1
	}
}

// SaveChapterTropeSnapshot persists content + blocks after a beat rewrite.
func SaveChapterTropeSnapshot(progressPath string, state *Progress, idx int, title string) error {
	ch := state.Chapters[idx]
	SaveChapterMarkdown(filepath.Dir(progressPath), ch, title)
	return SaveProgress(progressPath, state)
}

// ResyncChapterInterface re-derives all the editor bookkeeping a chapter needs
// (blocks, scene/trope spans, fallback scene plan for plan-less legacy prose)
// WITHOUT touching the prose itself. Chapters generated before the chips /
// contextual-critic features landed have scene_pos = -1 or missing block
// ranges, so the frontend hides their chips and anchors. This forces the same
// scene-aware sync the sheet-save path runs, plus the break-derived pseudo
// scene plan, so existing text suddenly exposes its full metadata UI. Returns
// the number of scenes materialized.
func ResyncChapterInterface(ch *ChapterState) int {
	if strings.TrimSpace(ch.Content) == "" {
		SyncChapterBlocksSceneAware(ch, ch.Content)
		return 0
	}
	// Force a FULL re-tile instead of the incremental path: SyncChapterBlocks
	// keeps previous block boundaries when they still match the prose, so a
	// chapter whose stale blocks were split one-per-paragraph (or merged into a
	// single block) would "resync" to zero changes and keep hiding its scene/
	// trope chips. Dropping the old block list makes the sync re-derive every
	// boundary from the current scene plan / fallback tiling; block IDs are
	// freshly allocated but the prose itself is untouched.
	ch.Blocks = nil
	ch.NextBlockID = 0
	ensureFallbackScenePlan(ch)
	repairSceneSpans(ch)
	SyncChapterBlocksSceneAware(ch, ch.Content)
	scenes := 0
	units := 0
	if ch.ScenePlan != nil {
		scenes = len(ch.ScenePlan.Scenes)
		for i := range ch.ScenePlan.Scenes {
			units += len(ch.ScenePlan.Scenes[i].TropeUnits)
		}
	}
	if units == 0 {
		units = len(ch.Blocks)
	}
	ch.InterfaceSyncedAt = time.Now().Unix()
	ch.InterfaceSyncCount++
	ch.LastInterfaceReport = fmt.Sprintf("%d scenes, %d subscenes, %d blocks", scenes, units, len(ch.Blocks))
	return scenes
}

// repairSceneSpans re-derives the absolute paragraph span (ParaStart/ParaEnd)
// of every planned scene from the CURRENT prose, walking the same budget-based
// estimator the sequential writer uses, and re-tiles each scene's trope units
// over its repaired span with the writer's own splitter. Chapters whose plan
// was produced before per-scene bookkeeping landed — or whose prose changed
// afterwards — carry stale spans that make recordedSceneParaSpans reject the
// exact tiling; without repair the whole chapter collapses into fallback
// pseudo-scenes and the UI shows a single "Scene 1" chip with no subscene chips.
// No-op when there is no plan or no content.
func repairSceneSpans(ch *ChapterState) {
	if ch.ScenePlan == nil || len(ch.ScenePlan.Scenes) < 2 || strings.TrimSpace(ch.Content) == "" {
		return
	}
	paras, sep := splitBlockParagraphs(ch.Content)
	if len(paras) == 0 {
		return
	}
	plan := ch.ScenePlan

	// Preferred strategy: trust the HARD BREAK markers ("---" / "***) the
	// sequential writer emits between scenes. The marker paragraph itself is
	// consumed as the scene boundary (it belongs to no scene), so every group
	// EXCLUDES its trailing marker and the groups tile the prose exactly with
	// no gaps. Previously the marker stayed inside the previous scene's span;
	// retiled trope units never cover it (splitTropesFromParas emits prose-only
	// ranges), so recordedSceneParaSpans saw a gap at the end of each scene,
	// discarded the whole exact tiling and the chapter fell back to budget
	// estimates that hide the real scene boundaries in the UI.
	if bs := breakSceneSpansNoMarker(paras); len(bs) == len(plan.Scenes) {
		for pos := range plan.Scenes {
			sc := &plan.Scenes[pos]
			sc.ParaStart, sc.ParaEnd = bs[pos][0], bs[pos][1]
			retileSceneTropeUnits(sc, paras, sep)
		}
		return
	}

	// Budget-proportional walk (same math as refreshSceneBlockRanges, but in
	// paragraph space): each scene consumes paragraphs until its word budget is
	// covered; the last scene always absorbs the tail so spans tile exactly.
	cum := 0
	for pos := range plan.Scenes {
		if cum >= len(paras) {
			// Ran out of prose: give degenerate empty spans to the remaining
			// scenes instead of leaving stale numbers pointing at wrong text.
			for p := pos; p < len(plan.Scenes); p++ {
				plan.Scenes[p].ParaStart = len(paras)
				plan.Scenes[p].ParaEnd = len(paras)
				plan.Scenes[p].TropeUnits = nil
				plan.Scenes[p].TropeRanges = nil
			}
			break
		}
		n := sceneParaEstimate(paras, cum, plan, pos)
		hi := min(cum+n, len(paras))
		if pos == len(plan.Scenes)-1 {
			hi = len(paras)
		}
		if hi <= cum {
			hi = min(cum+1, len(paras))
		}
		sc := &plan.Scenes[pos]
		sc.ParaStart, sc.ParaEnd = cum, hi
		retileSceneTropeUnits(sc, paras, sep)
		cum = hi
	}
}

// retileSceneTropeUnits re-segments one scene's trope units over its current
// [ParaStart,ParaEnd) span using the writer's own splitter (shared by the
// break-marker and budget-walk repairs).
func retileSceneTropeUnits(sc *Scene, paras []string, sep string) {
	lo, hi := sc.ParaStart, sc.ParaEnd
	if lo < 0 || hi <= lo || hi > len(paras) {
		sc.TropeUnits = nil
		sc.TropeRanges = nil
		return
	}
	sceneParas := paras[lo:hi]
	beats, ranges := splitTropesFromParas(sceneParas, "", sep)
	if len(beats) == 0 {
		beats = []string{strings.Join(sceneParas, sep)}
		ranges = [][2]int{{0, len(sceneParas)}}
	} else if len(ranges) == len(beats) && len(sceneParas) > 0 {
		// splitTropesFromParas emits PROSE-only ranges (break markers inside the
		// span are skipped). When a scene's span still contains such a marker the
		// units would leave a gap, recordedSceneParaSpans would judge the tiling
		// stale and discard every exact scene boundary — one "Scene 1" chip again.
		// Extend each unit to absorb the paragraphs between it and the next so the
		// units tile [0, len(sceneParas)) exactly; block sync drops break-only
		// paragraphs from the visible text anyway.
		filled := make([][2]int, len(ranges))
		for i := range ranges {
			end := len(sceneParas)
			if i+1 < len(ranges) {
				end = ranges[i+1][0]
			}
			if end < ranges[i][1] {
				end = ranges[i][1]
			}
			filled[i] = [2]int{ranges[i][0], end}
		}
		ranges = filled
	}
	applyTropeSegmentation(sc, beats, ranges)
}

// breakSceneSpansNoMarker derives HARD-BREAK-delimited scene spans that tile
// the prose EXACTLY: the paragraph right after each break marker belongs to
// the NEXT group, but the marker itself is consumed as the boundary of the
// previous group (markers carry no prose, so dropping them from spans loses
// nothing visible). Unlike breakSceneSpans, consecutive/edge markers never
// create gaps, which makes these spans directly usable as Scene.ParaStart /
// ParaEnd for recordedSceneParaSpans' exact-tiling check.
func breakSceneSpansNoMarker(paras []string) [][2]int {
	if len(paras) == 0 {
		return nil
	}
	var spans [][2]int
	start := -1
	for j := range paras {
		if paraIsBreak(paras[j]) {
			if start >= 0 {
				spans = append(spans, [2]int{start, j})
				start = -1
			}
			continue // stray/consecutive markers outside any group are ignored
		}
		if start < 0 {
			start = j
		}
	}
	if start >= 0 {
		spans = append(spans, [2]int{start, len(paras)})
	}
	return spans
}

// SubdivideScenesIntoBeats is the TRANSITIONAL one-shot subdivision of every
// already-written scene into subscenes/beat units (the same structural
// segmentation splitTropesFromParas performs for freshly written scenes).
// Chapters generated before per-scene trope bookkeeping landed carry plans
// whose scenes have no TropeUnits at all: the locator then tiles each scene as
// a single unit and inline critic highlights can only cover whole scenes. This
// function re-derives ParaStart/ParaEnd from the CURRENT prose (hard-break
// groups when they match the plan's scene count, budget walk otherwise — the
// exact logic ResyncChapterInterface's repairSceneSpans uses) and segments each
// scene into beat units, WITHOUT touching ch.Content or ch.Blocks. Afterwards
// a normal "Refresh UI" re-tiles blocks with real subscene chips and pending
// interventions get re-anchored onto the new beats. Idempotent: running it
// again just re-derives the same deterministic segmentation. Returns the
// number of scenes that ended up subdivided into more than one beat; 0 (with
// nil error) means there was nothing to subdivide.
func SubdivideScenesIntoBeats(ch *ChapterState) (int, error) {
	if ch == nil || strings.TrimSpace(ch.Content) == "" {
		return 0, errors.New("chapter_has_no_content")
	}
	if ch.ScenePlan == nil || len(ch.ScenePlan.Scenes) == 0 {
		ensureFallbackScenePlan(ch)
	}
	if ch.ScenePlan == nil || len(ch.ScenePlan.Scenes) == 0 {
		return 0, errors.New("no_scene_plan")
	}
	paras, sep := splitBlockParagraphs(ch.Content)
	if len(paras) == 0 {
		return 0, errors.New("no_paragraphs")
	}
	if sep == "" {
		sep = "\n\n"
	}
	plan := ch.ScenePlan
	// Re-anchor scene spans onto the current prose first: stale ParaStart /
	// ParaEnd values would segment paragraphs that no longer belong to the
	// scene (same failure mode repairSceneSpans fixes for resync).
	repaired := false
	if bs := breakSceneSpansNoMarker(paras); len(bs) == len(plan.Scenes) {
		for pos := range plan.Scenes {
			plan.Scenes[pos].ParaStart, plan.Scenes[pos].ParaEnd = bs[pos][0], bs[pos][1]
		}
		repaired = true
	} else {
		cum := 0
		for pos := range plan.Scenes {
			if cum >= len(paras) {
				break
			}
			n := sceneParaEstimate(paras, cum, plan, pos)
			hi := min(cum+n, len(paras))
			if pos == len(plan.Scenes)-1 {
				hi = len(paras)
			}
			if hi <= cum {
				hi = min(cum+1, len(paras))
			}
			plan.Scenes[pos].ParaStart, plan.Scenes[pos].ParaEnd = cum, hi
			cum = hi
		}
		if cum >= len(paras) {
			repaired = true
		}
	}
	if !repaired {
		// The budget walk could not account for every paragraph: refuse to
		// subdivide over untrusted spans instead of corrupting the tiling.
		return 0, errors.New("scene_spans_unreliable")
	}
	subdivided := 0
	for i := range plan.Scenes {
		sc := &plan.Scenes[i]
		lo, hi := sc.ParaStart, sc.ParaEnd
		if lo < 0 || hi <= lo || hi > len(paras) {
			continue
		}
		sceneParas := paras[lo:hi]
		// Segment with the EXACT same function syncSceneBlocks uses when it
		// re-derives unit spans live (sceneTropeUnitSpans -> splitTropesFromParas
		// over ch.BlockSep). Using ch.BlockSep here instead of the local sep
		// guarantees the recorded TropeUnits.Paras match what the iv-locator will
		// expose after Refresh UI, so inline highlights land on beats.
		beats, ranges := splitTropesFromParas(sceneParas, "", ch.BlockSep)
		if len(beats) == 0 {
			continue
		}
		if len(ranges) == len(beats) && len(sceneParas) > 0 {
			filled := make([][2]int, len(ranges))
			for j := range ranges {
				end := len(sceneParas)
				if j+1 < len(ranges) {
					end = ranges[j+1][0]
				}
				if end < ranges[j][1] {
					end = ranges[j][1]
				}
				filled[j] = [2]int{ranges[j][0], end}
			}
			ranges = filled
		}
		applyTropeSegmentation(sc, beats, ranges)
		if len(beats) > 1 {
			subdivided++
		}
	}
	return subdivided, nil
}
