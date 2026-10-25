package story

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/prose"
	"showmethestory/internal/sse"
)

// Drafted-artifact bestseller critiques: the siblings of the outline pass
// (outline_analysis.go) and the scene-plan pass (scene_plan_analysis.go),
// aimed at artifacts that ALREADY EXIST AS PROSE:
//
//   - CHAPTER pass (ChapterAnalysisReport, stored per chapter): reviews ONE
//     drafted chapter. The editor knows it is grading a single chapter inside
//     its act, scores the four bestseller pillars and returns suggestions
//     whose INTERVENTIONS target concrete trope units of the written prose
//     (chapter/scene/trope triplets read from ScenePlan.Scenes[].TropeUnits).
//
//   - ACT pass (ActAnalysisReport, stored on Progress keyed by act number):
//     reviews one whole pre-planning act once all its chapters have been
//     drafted — the writing pipeline triggers it automatically when the last
//     chapter of an act is confirmed, and POST /api/acts/analyze re-runs it
//     on demand. Same editorial frame, plus small surgical interventions in
//     the tropos of individual scenes so the author can lift the act without
//     regenerating chapters.
//
// Applying an intervention runs rewriteSceneTrope (tropes.go): the proven
// surgical beat-rewrite channel that replaces exactly one trope unit of one
// scene while preserving voice, tense and surrounding prose.

// TropeIntervention is one small, surgical change requested on a concrete
// trope unit of drafted prose. ChapterNum/ScenePos address the live chapter
// and its beat sheet; TropeIndex addresses one TropeUnit of that scene (the
// same index space buildCriticSceneMapBlock shows to the critic). Feedback is
// the instruction handed to rewriteSceneTrope. Status tracks application:
// pending | applied | failed.
type TropeIntervention struct {
	ChapterNum int    `json:"chapter_num"`
	ScenePos   int    `json:"scene_pos"`   // index into ScenePlan.Scenes
	TropeIndex int    `json:"trope_index"` // index into Scene.TropeUnits
	TropeLabel string `json:"trope_label,omitempty"`
	// TargetKind distinguishes prose repairs on drafted chapters ("", the
	// default) from surgical OUTLINE edits demanded by an act critic for the
	// VENUE (not-yet-drafted) chapters: target_kind="outline" interventions
	// rewrite chapter N's outline text instead of trope prose. They are only
	// ever produced by act reports and applied through ApplyActOutlineEdits.
	TargetKind string `json:"target_kind,omitempty"`
	Feedback   string `json:"feedback"`
	Status     string `json:"status"` // pending | applied | failed
	// CurrentText is the exact prose of the targeted trope unit AT PROPOSAL
	// TIME. The viewer uses it to highlight the passage inline (fuzzy-matched
	// against the live chapter text) and to show the author what a click will
	// replace before applying anything.
	CurrentText string `json:"current_text,omitempty"`
	// Quote is an optional short excerpt the editor quotes as evidence for the
	// intervention; used as a secondary highlight anchor when CurrentText no
	// longer matches after manual edits.
	Quote string `json:"quote,omitempty"`
	// Action states what the intervention does to the highlighted passage:
	// rewrite (default) | delete | insert. Drives the popup wording ("replace
	// with…", "delete this passage").
	Action string `json:"action,omitempty"`
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

var validToneCategories = map[string]bool{
	"hook": true, "pacing": true, "scene_economy": true, "stakes": true,
	"structure": true, "emotion": true, "pov": true, "budget": true, "other": true,
}

func draftTruncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// draftedChapterFingerprint hashes the summary + prose length + content hash
// of one chapter, so re-analyzing unchanged prose reuses the cached report.
func draftedChapterFingerprint(ch ChapterState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d\x01%s\x01%d\x01%d", ch.Num, strings.TrimSpace(ch.Summary),
		ch.WordCount, HashContent(ch.Content))
	b.WriteString(gradeSalt()) // force a fresh LLM pass on every explicit re-analysis
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// gradeSalt returns a per-process nonce mixed into every critique fingerprint.
// Without it, clicking "Re-analyze" on unchanged material silently returned the
// cached report (log.*_analyze_unchanged) and the UI showed the exact same
// scores again — which read as "the critic never recalculates". The nonce keeps
// genuine within-session dedup impossible for stale echoes while still changing
// whenever the graded artifact itself changes.
func gradeSalt() string { return analysisGradeSalt }

// analysisGradeSalt is a per-process nonce mixed into every critique
// fingerprint so an explicit re-analysis always recomputes scores against the
// current artifact instead of silently echoing the cached report.
var analysisGradeSalt = func() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf[:])
}()

// chapterTropeMapBlock renders the WRITTEN chapter's scene/trope map with
// exact indexes so the editor can point interventions at real units. Only
// scenes with recorded TropeUnits are listed as intervention targets.
func chapterTropeMapBlock(ch ChapterState, lang string) string {
	en := normalizeLangEN(lang)
	if ch.ScenePlan == nil || len(ch.ScenePlan.Scenes) == 0 {
		// Chapters drafted WITHOUT a scene plan still tile into break-delimited
		// pseudo-scenes and trope units in the reader (SyncChapterBlocks fallback
		// + breakSceneSpans). Report that same tiling here so the editor model can
		// target concrete scene_pos/trope_index values and the proposals carry a
		// highlightable CurrentText anchor instead of being untargetable.
		if mapStr := fallbackTropeMapBlock(ch, lang); mapStr != "" {
			return mapStr
		}
		if en {
			return "[Written structure]\nThis chapter was written without a scene plan — interventions cannot be targeted at concrete scenes.\n\n"
		}
		return "[Estructura escrita]\nEste capítulo se escribió sin plan de escenas — las intervenciones no pueden apuntar a escenas concretas.\n\n"
	}
	var sb strings.Builder
	if en {
		sb.WriteString("[Written structure — scenes and their subscenes (use THESE exact indexes for interventions)]\n")
	} else {
		sb.WriteString("[Estructura escrita — escenas y sus subescenas (usa ESTOS índices exactos para las intervenciones)]\n")
	}
	for pos, sc := range ch.ScenePlan.Scenes {
		title := sceneTitleOf(sc, lang)
		if len(sc.TropeUnits) == 0 {
			if en {
				fmt.Fprintf(&sb, "- scene_pos=%d \"%s\" (~%d words) — no subscene segmentation recorded\n", pos, title, sc.WordsBudget)
			} else {
				fmt.Fprintf(&sb, "- scene_pos=%d \"%s\" (~%d palabras) — sin segmentación de subescenas registrada\n", pos, title, sc.WordsBudget)
			}
			continue
		}
		units := make([]string, 0, len(sc.TropeUnits))
		for _, u := range sc.TropeUnits {
			units = append(units, fmt.Sprintf("trope_index=%d:%s(%dw)", u.Index, u.Label, u.Words))
		}
		if en {
			fmt.Fprintf(&sb, "- scene_pos=%d \"%s\" (~%d words) — subscenes: %s\n", pos, title, sc.WordsBudget, strings.Join(units, " → "))
		} else {
			fmt.Fprintf(&sb, "- scene_pos=%d \"%s\" (~%d palabras) — subescenas: %s\n", pos, title, sc.WordsBudget, strings.Join(units, " → "))
		}
	}
	sb.WriteString("\n")
	return sb.String()
}

func normalizeLangEN(lang string) bool {
	return i18n.NormalizeLanguage(lang) == i18n.LangEN
}

// sanitizeDraftedSuggestions validates categories, drops empty points,
// discards any bundled recommendations (advice-only first pass) and validates
// proposed trope interventions against the LIVE chapter data when present.
// When validateTargets is false (initial analysis pass) interventions are
// dropped entirely — they only appear after an explicit propose action.
func sanitizeDraftedSuggestions(in []DraftedSuggestion, now int64, prefix string, validateTargets func(chapterNum, scenePos, tropeIndex int) bool, keepInterventions bool) []DraftedSuggestion {
	// Idempotency guard: chapter reports are re-sanitized on EVERY save round
	// (proposal/apply passes call this again over the already-processed list).
	// Regenerating IDs at that point orphans the frontend's pending-intervention
	// keys and silently resets applied/dismissed statuses; worse, an ID change
	// made right before the auto-propose loop meant ProposeChapterInterventions
	// could no longer find the suggestion it was asked to enrich ("suggestion_not_
	// found") — the exact "critic scores < 100 but never generates inline
	// suggestions" regression. A second pass over processed input must therefore
	// preserve IDs verbatim; fresh model output always arrives with an empty ID.
	reprocessing := false
	for _, s := range in {
		if strings.TrimSpace(s.ID) != "" {
			reprocessing = true
			break
		}
	}
	kept := make([]DraftedSuggestion, 0, len(in))
	for i, s := range in {
		s.Title = strings.TrimSpace(s.Title)
		s.Detail = strings.TrimSpace(s.Detail)
		s.Action = strings.TrimSpace(s.Action)
		if !validToneCategories[s.Category] {
			s.Category = "other"
		}
		if s.Title == "" && s.Detail == "" {
			continue
		}
		// Chapter reports are cached by DraftFingerprint and re-saved on every
		// proposal/apply round; a second sanitize pass over an already-processed
		// report must NOT regenerate IDs (that would orphan the frontend's
		// pending-intervention keys and reset applied statuses). Preserve the
		// original ID when present.
		if s.ID == "" {
			s.ID = fmt.Sprintf("%s_%d_%d", prefix, now, i)
		}
		s.Recommendations = nil
		cleanApps := make([]int, 0, len(s.AppliesToChapters))
		seenApp := map[int]bool{}
		for _, n := range s.AppliesToChapters {
			if n > 0 && !seenApp[n] {
				seenApp[n] = true
				cleanApps = append(cleanApps, n)
			}
		}
		sort.Ints(cleanApps)
		s.AppliesToChapters = cleanApps
		if !reprocessing {
			// Fresh model output always starts pending. A re-sanitize pass over an
			// already-processed report must NOT reset statuses: doing so resurrected
			// applied/dismissed suggestions as "pending" on every save round.
			s.Status = "pending"
		}
		if keepInterventions && validateTargets != nil {
			var ivs []TropeIntervention
			for _, iv := range s.Interventions {
				iv.Feedback = strings.TrimSpace(iv.Feedback)
				if iv.Feedback == "" {
					continue
				}
				// Clamp out-of-range targets into the live tiling instead of
				// silently dropping them: an editor model that overshoots a
				// scene/trope index still meant THAT chapter's prose, so the
				// feedback stays visible (contextual highlight) rather than
				// vanishing into "0 editorial suggestions".
				iv.ChapterNum, iv.ScenePos, iv.TropeIndex = clampInterventionTarget(validateTargets, iv.ChapterNum, iv.ScenePos, iv.TropeIndex)
				ivs = append(ivs, iv)
			}
			if len(ivs) > 4 {
				ivs = ivs[:4]
			}
			s.Interventions = ivs
			// INVARIANT: every surviving intervention must carry Status="pending"
			// when it enters the report fresh. The frontend filters inline
			// highlights by `iv.status === 'pending'`; interventions parsed from
			// model JSON arrive with an EMPTY status (the proposal schema only
			// declares chapter/scene/trope/label/feedback), which used to make
			// perfectly valid bundled suggestions invisible in the sheet — the
			// exact "critic scores < 100 but shows no inline suggestions"
			// symptom. Already-processed reports re-sanitized on save keep
			// their applied/dismissed statuses untouched.
			for j := range s.Interventions {
				if s.Interventions[j].Status == "" {
					s.Interventions[j].Status = "pending"
				}
			}
		} else {
			// Advice-only pass. The previous behavior dropped EVERY bundled
			// intervention here, which made the critic look broken ("score is
			// not 100/100 yet zero inline suggestions") whenever the pipeline
			// was interrupted before the auto-propose step — and the analysis
			// fingerprint then cached the suggestion-less report forever. Keep
			// the model's own interventions as UNVALIDATED anchors: the frontend
			// resolves them against the live tiling at highlight time (unit-level
			// fallback), and ProposeChapterInterventions replaces them with
			// validated ones when the author triggers it manually.
			var ivs []TropeIntervention
			for _, iv := range s.Interventions {
				iv.Feedback = strings.TrimSpace(iv.Feedback)
				if iv.Feedback == "" {
					continue
				}
				ivs = append(ivs, iv)
			}
			if len(ivs) > 4 {
				ivs = ivs[:4]
			}
			s.Interventions = ivs
			for j := range s.Interventions {
				if s.Interventions[j].Status == "" {
					s.Interventions[j].Status = "pending"
				}
			}
		}
		kept = append(kept, s)
	}
	return kept
}

// clampInterventionTarget nudges an intervention triplet whose exact target is
// not part of the live scene/trope tiling onto the CLOSEST valid one, keeping
// the critic feedback highlightable. The validator only answers yes/no, so the
// clamp walks a small neighborhood (same scene first, then neighbouring scenes
// and units) and falls back to leaving the triplet untouched when nothing near
// validates — the frontend's unit-level fallback still renders it in context.
func clampInterventionTarget(valid func(chapterNum, scenePos, tropeIndex int) bool, chap, pos, idx int) (int, int, int) {
	if valid(chap, pos, idx) {
		return chap, pos, idx
	}
	try := func(p, i int) (int, int, bool) {
		if p < 0 || i < 0 {
			return p, i, false
		}
		if valid(chap, p, i) {
			return p, i, true
		}
		return p, i, false
	}
	// Scene-only sentinel: a validator built over a plan WITHOUT trope units
	// (legacy chapters whose per-scene bookkeeping never landed) answers yes
	// for (chap, pos, -1). When that happens the scene exists but no unit is
	// registered, so keep the ORIGINAL index — sanitize leaves it untouched and
	// the frontend's unit-level fallback resolves it against the live tiling at
	// highlight time instead of dropping the feedback.
	if pos >= 0 && valid(chap, pos, -1) {
		return chap, pos, idx
	}
	// Same scene, next/prev units first (index drift is the most common error).
	for d := 1; d <= 6; d++ {
		if p, i, ok := try(pos, idx+d); ok {
			return chap, p, i
		}
		if p, i, ok := try(pos, idx-d); ok {
			return chap, p, i
		}
	}
	// Then neighbouring scenes at a nearby unit.
	for dp := 1; dp <= 3; dp++ {
		for _, p := range []int{pos + dp, pos - dp} {
			for di := 0; di <= 6; di++ {
				if q, i, ok := try(p, idx+di); ok {
					return chap, q, i
				}
				if q, i, ok := try(p, max(idx-di, 0)); ok {
					return chap, q, i
				}
			}
		}
	}
	return chap, pos, idx
}

// finalizePillars applies the shared rubric contract (analysis_pillars.go) to
// a drafted-pass report: checklists attached, pillar scores DERIVED from the
// graded criteria levels, suggestion pillar/criteria normalized and claimed
// impacts capped at each pillar's remaining headroom. The concrete suggestion
// structs are updated in place with the sanitized values.
func finalizePillars(in []PillarAssessment, suggestions []DraftedSuggestion) []PillarAssessment {
	finalIn := make([]FinalizableSuggestion, len(suggestions))
	for i, s := range suggestions {
		finalIn[i] = FinalizableSuggestion{Category: s.Category, Status: s.Status, Pillar: s.Pillar, Impact: s.ScoreImpact, Criteria: s.Criteria}
	}
	out, finalOut := finalizePillarReport(in, finalIn)
	for i := range suggestions {
		suggestions[i].Pillar = finalOut[i].Pillar
		suggestions[i].Criteria = finalOut[i].Criteria
		suggestions[i].ScoreImpact = finalOut[i].Impact
		suggestions[i].Priority = finalOut[i].Priority
	}
	return out
}

// draftedJSONSchemaEN builds the shared strict-JSON contract for both passes.
// Pillar scores are DERIVED from the per-criterion checklist levels (see
// analysis_pillars.go): the model grades concrete sub-criteria, so re-grading
// after corrections raises exactly the items that improved instead of drifting
// around randomized example anchors like before.
func draftedJSONSchemaEN() string {
	return `{"summary":"...","overall_score":75,"pillar_scores":[{"key":"reader_profile","score":0,"criteria_levels":[{"key":"genre_promise","level":0},{"key":"reader_hooks","level":0},{"key":"tropes_freshness","level":0},{"key":"emotional_payoff","level":0},{"key":"comp_distance","level":0}],"note":"..."},{"key":"inspirational_pieces","score":0,"criteria_levels":[...],"note":"..."},{"key":"tone","score":0,"criteria_levels":[...],"note":"..."},{"key":"story_structure","score":0,"criteria_levels":[...],"note":"..."}],"suggestions":[{"category":"hook|pacing|scene_economy|stakes|structure|emotion|pov|budget|other","pillar":"reader_profile|inspirational_pieces|tone|story_structure","criteria":[{"key":"checklist_key_it_repairs"}],"score_impact":10,"title":"short imperative headline","detail":"what the drafted text currently does and why it falls short of bestseller level","action":"concrete guidance on what to change and where","applies_to_chapters":[3],"interventions":[{"chapter_num":3,"scene_pos":2,"trope_index":1,"trope_label":"tension","feedback":"one or two imperative sentences telling the rewriter exactly what to fix in THIS subscene only"}]}]}`
}

// interventionsRulesEN explains the intervention contract in the prompt.
func interventionsRulesBlock(lang string) string {
	if normalizeLangEN(lang) {
		return `- interventions: 0 to 6 SMALL SURGICAL fixes per suggestion, each pointing at ONE concrete (chapter_num, scene_pos, trope_index) triplet shown in the written-structure map above. Each feedback must describe a minimal change confined to that subscene (sharpen the hook of this opening, cut redundancy here, raise the stakes in this exchange...) — never a whole-chapter rewrite, never a target that is not in the map. One and the same beat MAY receive more than one intervention when it needs fixes on DIFFERENT axes (e.g. pacing AND emotion): emit them as separate entries with distinct feedback texts; never merge two unrelated fixes into one long feedback.`
	}
	return `- interventions: de 0 a 6 correcciones QUIRÚRGICAS y pequeñas por sugerencia, cada una apuntando a UN triplete concreto (chapter_num, scene_pos, trope_index) mostrado en el mapa de estructura escrita de arriba. Cada feedback debe describir un cambio mínimo limitado a esa subescena (afilar el gancho de esta apertura, recortar redundancia aquí, subir la apuesta en este intercambio...) — nunca un reescritado del capítulo entero, nunca un destino que no aparezca en el mapa. UNA MISMA subescena PUEDE recibir más de una intervención cuando necesita arreglos en EJES DISTINTOS (p. ej. ritmo Y emoción): emítelas como entradas separadas con feedbacks distintos; nunca mezcles dos arreglos no relacionados en un solo feedback largo.`
}

// loadDraftedChapterFilesInto fills Content for chapters whose in-memory
// Content is empty by reading their persisted chapter JSON files (same layout
// SaveProgress writes). Best-effort: unreadable files are skipped.
func loadDraftedChapterFilesInto(progressPath string, state *Progress) {
	dir := filepath.Join(filepath.Dir(progressPath), "chapters")
	for i := range state.Chapters {
		ch := &state.Chapters[i]
		if ch.Content != "" || ch.WordCount == 0 {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("%06d.json", ch.Num)))
		if err != nil {
			continue
		}
		var cf struct {
			Content string `json:"content"`
		}
		if json.Unmarshal(data, &cf) == nil && cf.Content != "" {
			ch.Content = cf.Content
			if len(ch.Blocks) == 0 {
				SyncChapterBlocks(ch)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// DraftedSuggestion — shared suggestion type for chapter and act reports
// ---------------------------------------------------------------------------

// DraftedSuggestion is one editorial point on drafted prose. Unlike the
// outline/scene-plan passes it carries Interventions: concrete, small fixes
// targeting trope units of the existing text, applicable via rewriteSceneTrope.
type DraftedSuggestion struct {
	ID                string                  `json:"id"`
	Category          string                  `json:"category"` // hook|pacing|scene_economy|stakes|structure|emotion|pov|budget|other
	Title             string                  `json:"title"`
	Detail            string                  `json:"detail"`
	Action            string                  `json:"action"`
	Status            string                  `json:"status"` // pending | applied
	AppliedAt         string                  `json:"applied_at,omitempty"`
	AppliesToChapters []int                   `json:"applies_to_chapters,omitempty"`
	Interventions     []TropeIntervention     `json:"interventions,omitempty"`
	Recommendations   []OutlineRecommendation `json:"recommendations,omitempty"` // always discarded: advice-only first pass
	// Pillar is the analysis pillar this point serves (reader_profile |
	// inspirational_pieces | tone | story_structure), declared by the editor or
	// inferred from the category during finalization.
	Pillar string `json:"pillar,omitempty"`
	// Criteria lists the pillar-checklist items this point repairs.
	Criteria []SuggestionCriterion `json:"criteria,omitempty"`
	// ScoreImpact is the points the pillar should gain once this point is fully
	// applied, capped at the pillar's remaining headroom.
	ScoreImpact int `json:"score_impact,omitempty"`
	// Priority is the 3-level importance badge (high | medium | low) derived
	// deterministically from ScoreImpact + how many checklist items the point
	// repairs (see deriveSuggestionPriority), so it always agrees with the
	// rubric numbers shown next to it in the UI.
	Priority string `json:"priority,omitempty"`
}

// fallbackTropeMapBlock renders the SAME break-delimited pseudo-scene / trope
// tiling that SyncChapterBlocks and breakSceneSpans use for chapters drafted
// without a scene plan. The editor model can then target real scene_pos /
// trope_index values; ProposeChapterInterventions records the derived units on
// a synthetic ScenePlan so CurrentText anchoring (tropeUnitProseAt) resolves
// them later. Returns "" when there is no prose to map.
func fallbackTropeMapBlock(ch ChapterState, lang string) string {
	paras, sep := splitBlockParagraphs(ch.Content)
	if len(paras) == 0 || sep == "" {
		sep = "\n\n"
	}
	spans := writtenSceneTiling(&ch, paras)
	if len(spans) == 0 {
		return ""
	}
	en := normalizeLangEN(lang)
	var sb strings.Builder
	if en {
		sb.WriteString("[Written structure — pseudo-scenes delimited by \"---\" breaks and their subscenes (use THESE exact indexes for interventions)]\n")
	} else {
		sb.WriteString("[Estructura escrita — pseudo-escenas delimitadas por saltos \"---\" y sus subescenas (usa ESTOS índices exactos para las intervenciones)]\n")
	}
	for pos, sp := range spans {
		lo, hi := sp[0], sp[1]
		if hi <= lo {
			continue
		}
		sceneParas := paras[lo:hi]
		beats, _ := splitTropesFromParas(sceneParas, "", sep)
		if len(beats) == 0 {
			beats = []string{strings.Join(sceneParas, sep)}
		}
		units := make([]string, 0, len(beats))
		for i, b := range beats {
			units = append(units, fmt.Sprintf("trope_index=%d:%s(%dw)", i, classifyTrope(b, i == 0, i == len(beats)-1), prose.CountProseUnits(b)))
		}
		if en {
			fmt.Fprintf(&sb, "- scene_pos=%d (break-delimited section %d) — subscenes: %s\n", pos, pos+1, strings.Join(units, " → "))
		} else {
			fmt.Fprintf(&sb, "- scene_pos=%d (sección %d delimitada por saltos) — subescenas: %s\n", pos, pos+1, strings.Join(units, " → "))
		}
	}
	sb.WriteString("\n")
	return sb.String()
}

// sceneMinParagraphs is the smallest number of paragraphs a break-delimited
// group may have to count as its OWN pseudo-scene in the fallback plan;
// shorter groups merge into their predecessor. Keeps stray "---"-adjacent
// fragments from exploding a chapter into dozens of one-paragraph "scenes".
const sceneMinParagraphs = 2

// mergeShortSpans coalesces spans shorter than minLen into their predecessor
// so a break-derived tiling never produces degenerate single-line pseudo-
// scenes. The first span always survives even when short.
func mergeShortSpans(spans [][2]int, minLen int) [][2]int {
	out := make([][2]int, 0, len(spans))
	for _, sp := range spans {
		if len(out) > 0 && sp[1]-sp[0] < minLen {
			out[len(out)-1][1] = sp[1]
			continue
		}
		out = append(out, sp)
	}
	return out
}

// writtenSceneTiling returns the [start,end) paragraph spans of every scene as
// THE READER (SyncChapterBlocks / syncSceneBlocks) sees them: exact recorded
// spans when they tile the prose, budget-proportional mapping when a real plan
// exists but its bookkeeping is stale, and the break/soft-grouped fallback
// tiling for plan-less chapters. Proposal-time code MUST derive targets through
// this helper so the trope map sent to the editor model, the target validator
// and the CurrentText anchor all agree with the chips drawn in the sheet —
// divergent tilings used to make every intervention fail validation ("0
// editorial suggestions" despite a low critic score).
func writtenSceneTiling(ch *ChapterState, paras []string) [][2]int {
	if ch != nil && ch.ScenePlan != nil && len(ch.ScenePlan.Scenes) > 0 {
		// The reader (syncSceneBlocks) accepts plans with a SINGLE scene; the
		// recorded-span checker historically demanded >=2, so a one-scene plan
		// diverged here and proposals anchored on the wrong tiling. Mirror the
		// reader exactly: trust recorded spans whenever they tile the prose.
		if spans := recordedSceneParaSpans(ch, paras); spans != nil {
			return spans
		}
		if spans := sceneParaSpans(ch, paras); spans != nil {
			return spans
		}
	}
	return fallbackSceneTiling(paras)
}

// liveTropeUnitIndex maps ONE absolute paragraph index onto the trope-unit
// index the reader actually highlights for it. The unit spans come from
// sceneTropeUnitSpans — the EXACT function syncSceneBlocks uses to build the
// chips — including its "recorded units are stale → re-segment live" rule, so
// an intervention stamped at proposal time can never anchor on a different
// segmentation than the chip it lands on. Returns -1 when the chapter has no
// usable scene tiling at all.
func liveTropeUnitIndex(ch *ChapterState, absPara int) (scenePos, unitIdx int, ok bool) {
	if ch == nil {
		return -1, -1, false
	}
	paras, _ := splitBlockParagraphs(ch.Content)
	if len(paras) == 0 {
		return -1, -1, false
	}
	spans := writtenSceneTiling(ch, paras)
	for pos, sp := range spans {
		if absPara < sp[0] || absPara >= sp[1] {
			continue
		}
		units := sceneTropeUnitSpans(ch, pos, paras[sp[0]:sp[1]])
		for ui, u := range units {
			if absPara-sp[0] >= u[0] && absPara-sp[0] < u[1] {
				return pos, ui, true
			}
		}
		return pos, 0, true
	}
	return -1, -1, false
}

// fallbackSceneTiling tiles paragraphs into pseudo-scenes exactly like
// ensureFallbackScenePlan does: hard-break groups first, soft paragraph groups
// when there are no breaks, short groups merged into their predecessor.
func fallbackSceneTiling(paras []string) [][2]int {
	spans := breakSceneSpans(paras)
	if len(spans) == 1 && len(paras) >= 4 {
		group := max(sceneMinParagraphs, min(6, len(paras)/3))
		spans = spans[:0:0]
		for i := 0; i < len(paras); i += group {
			end := min(i+group, len(paras))
			if len(paras)-end < sceneMinParagraphs {
				end = len(paras)
			}
			spans = append(spans, [2]int{i, end})
		}
	}
	return mergeShortSpans(spans, sceneMinParagraphs)
}

// ensureFallbackScenePlan records the break-derived pseudo-scene/trope tiling
// on a SYNTHETIC ScenePlan for plan-less chapters, so intervention targets
// (scene_pos + trope_index) resolve consistently across proposal time
// (fallbackTropeMapBlock), apply time (rewriteSceneTrope) and highlight time
// (tropeUnitProseAt). Chapters WITHOUT any "---" breaks still get a plan: the
// prose is tiled into paragraph groups of roughly equal size (≈5 paragraphs
// per pseudo-scene, minimum 2 scenes whenever the text has enough paragraphs),
// otherwise the whole chapter would collapse into a single scene chip with no
// visible boundaries. No-op when a real plan exists or there is no prose.
func ensureFallbackScenePlan(ch *ChapterState) {
	if ch.ScenePlan != nil && len(ch.ScenePlan.Scenes) > 0 {
		return
	}
	paras, sep := splitBlockParagraphs(ch.Content)
	if len(paras) == 0 {
		return
	}
	if sep == "" {
		sep = "\n\n"
	}
	spans := fallbackSceneTiling(paras)
	if len(spans) == 0 {
		return
	}
	scenes := make([]Scene, 0, len(spans))
	for _, sp := range spans {
		lo, hi := sp[0], sp[1]
		if hi <= lo {
			continue
		}
		sceneParas := paras[lo:hi]
		beats, ranges := splitTropesFromParas(sceneParas, "", sep)
		if len(beats) == 0 {
			beats = []string{strings.Join(sceneParas, sep)}
			ranges = [][2]int{{0, len(sceneParas)}}
		}
		sc := Scene{ID: len(scenes) + 1, ParaStart: lo, ParaEnd: hi}
		applyTropeSegmentation(&sc, beats, ranges)
		scenes = append(scenes, sc)
	}
	if len(scenes) == 0 {
		return
	}
	ch.ScenePlan = &ScenePlan{Scenes: scenes}
}

func findDraftedSuggestion(list []DraftedSuggestion, id string) int {
	for i, s := range list {
		if s.ID == id {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// CHAPTER pass — bestseller critique of ONE drafted chapter
// ---------------------------------------------------------------------------

// ChapterAnalysisReport is the bestseller-editor pass over one drafted
// chapter, persisted on ChapterState.Analysis.
type ChapterAnalysisReport struct {
	GeneratedAt      string              `json:"generated_at"`
	BasedOnStructure string              `json:"based_on_structure,omitempty"`
	ChapterNum       int                 `json:"chapter_num"`
	Summary          string              `json:"summary"`
	OverallScore     int                 `json:"overall_score,omitempty"`
	PillarScores     []PillarAssessment  `json:"pillar_scores,omitempty"`
	Suggestions      []DraftedSuggestion `json:"suggestions"`
	// DraftFingerprint identifies the exact prose + summary the scores above
	// were computed from. Re-analyzing an unchanged chapter reuses the report;
	// any edit (or applied intervention) changes the hash and forces a
	// genuine re-grade.
	DraftFingerprint string `json:"draft_fingerprint,omitempty"`
}

// buildChapterEditorPrompt assembles the LLM prompt reviewing ONE drafted
// chapter. It explicitly frames the artifact as a single DRAFTED CHAPTER
// inside its act so the editor grades prose — unlike the scene-plan pass.
func buildChapterEditorPrompt(cfg *config.Config, state *Progress, settings *ProjectSettings, idx int, lang string) string {
	sc := cfg.Story
	ch := state.Chapters[idx]
	var meta strings.Builder
	add := func(label, v string) {
		if strings.TrimSpace(v) != "" {
			fmt.Fprintf(&meta, "%s: %s\n", label, strings.TrimSpace(v))
		}
	}
	add("Story idea", sc.StoryIdea)
	add("Genre / subgenre", strings.TrimSpace(sc.Type+" / "+sc.Subgenre))
	add("Theme", sc.Theme)
	add("Tone", sc.Tone)
	add("Story structure (framework the book must follow)", sc.Structure)
	add("Writing style & POV", strings.TrimSpace(sc.WritingStyle+" / "+sc.WritingPOV))
	add("Ideal reader profile", sc.AudienceProfile)
	add("Inspirational pieces (comp titles / mood references)", sc.InspirationalPieces)
	add("Conflict scale", sc.EffectiveConflict())
	add("Protagonist type", sc.EffectiveProtagonist())
	if settings != nil {
		cast := make([]string, 0, len(settings.Characters))
		for _, c := range settings.Characters {
			cast = append(cast, c.Name)
		}
		add("Registered characters", strings.Join(cast, ", "))
	}

	actNo, actName, hasAct := actForChapter(state, ch.Num)
	structureName := structureLabel(sc.Structure, lang)
	history := Truncate(buildHistorySummary(state, idx), 4000)
	tropeMap := chapterTropeMapBlock(ch, lang)
	content := draftTruncate(ch.Content, 26000)

	whereEn := fmt.Sprintf("Chapter #%d \"%s\" of %d", ch.Num, ch.Title, len(state.Chapters))
	whereEs := fmt.Sprintf("Capítulo #%d \"%s\" de %d", ch.Num, ch.Title, len(state.Chapters))
	if hasAct {
		whereEn += fmt.Sprintf(", inside ACT %d (%s) of the book's planned structure", actNo, actName)
		whereEs += fmt.Sprintf(", dentro del ACTO %d (%s) de la estructura planificada", actNo, actName)
	}

	if normalizeLangEN(lang) {
		return fmt.Sprintf(`You are %s, a world-famous literary editor whose lists have produced multiple global bestsellers. Your job: give the author a constructive, point-by-point editorial pass over ONE DRAFTED CHAPTER of their novel in progress, with ONE clear goal: turn this book into a global bestseller — every point you make must serve that bar, not mere competence.

CRITICAL CONTEXT — WHAT YOU ARE REVIEWING:
- You are reviewing %s AS DRAFTED PROSE. This chapter EXISTS as written text (shown below); judge actual sentences, scenes, dialogue, pacing and hooks — this is NOT an outline or a beat sheet.
- Grade the chapter both as a standalone reading experience AND as one link in its act's chain: does it deliver the function the structure assigns to this position, escalate stakes, and land a chapter-ending hook?
- This pass is ADVICE ONLY for the initial scoring: do NOT draft replacement text. The author requests concrete micro-fixes later via a "Propose interventions" action, so nothing gets rewritten before they choose to act on a specific point.

NOVEL PARAMETERS (the author's contract with the reader):
%s
STORY STRUCTURE FRAMEWORK IN USE: %s

[Story-so-far, condensed]
%s

[Chapter outline — the contract this chapter must fulfil]
%s

%s
[CHAPTER PROSE (%d words)]
%s

YOUR MISSION: reverse-engineer what makes books in this genre hit #1 (an irresistible hook, relentless pacing, arcs that pay off, stakes that escalate) and grade THIS CHAPTER against that blueprint. Every suggestion must be a concrete step that measurably moves the chapter toward bestseller level on its pillar.

Analyze against ALL parameters above, giving special weight to these four pillars:
1. Ideal reader profile — does this chapter deliver what this reader came for? Genre expectations met without cliché?
2. Inspirational pieces — does the chapter live up to the comp titles' standard without copying them?
3. Tone — is the intended tone sustained and escalating properly across the chapter's scenes?
4. Story structure (%s) — does the chapter advance the framework's beats for its position? Dead zones, rushed payoffs, weak ending hook?

Return STRICT JSON only (no markdown fences, no prose): %s

Grading standard (you are ruthless — your reputation rests on books that hit #1 worldwide):
- Score each of the four pillars 0-100 against BESTSELLER level, not "competent". 90+ means the chapter is genuinely strong as-is. Most drafted chapters land mid-range. Derive every score independently from THIS specific chapter: grade each pillar's checklist item (criteria_levels, 0–100) and let the pillar score be their average, so re-grading corrected material raises exactly the items that improved; grading different chapters must produce clearly different numbers wherever their quality differs.
- pillar_scores MUST contain exactly the four keys reader_profile, inspirational_pieces, tone, story_structure.
- Each pillar_scores entry MUST include criteria_levels grading EVERY item of that pillar's checklist above ({"key":"...","level":0..100}) — the pillar score is derived from those levels, never guessed.
- Every suggestion MUST declare its "pillar" (one of the four keys), the checklist "criteria" keys it repairs, and a realistic "score_impact" (points that pillar should gain once this point is fully applied).
- Every pillar scoring below 90 MUST have at least one corresponding suggestion (reader_profile→hook/other, inspirational_pieces→other, tone→emotion/hook, story_structure→structure/pacing/scene_economy/stakes). A pillar scoring 90+ may have zero suggestions — then the UI shows "No changes needed" for it.

Rules:
- 3 to 8 suggestions, ordered most impactful first.
- DO NOT include replacement prose in this pass: return every suggestion WITHOUT an "interventions" array and WITHOUT "recommendations" (interventions are drafted later, ONLY after the author clicks "Propose interventions" on a specific point).
- applies_to_chapters should just name this chapter (%d).
- Write summary, titles, details and actions in the SAME language as the chapter prose above.`,
			editorPersona(lang), whereEn, meta.String(), structureName, history,
			strings.TrimSpace(ch.Outline), tropeMap, ch.WordCount, content,
			structureName, draftedJSONSchemaEN(), ch.Num)
	}
	return fmt.Sprintf(`Eres %s, un editor literario mundialmente famoso cuyas listas han producido múltiples bestsellers globales. Tu trabajo: dar al autor una pasada editorial constructiva y punto por punto sobre UN CAPÍTULO BORRONEADO de su novela en curso, con UN objetivo claro: convertir este libro en un bestseller global — cada punto que plantees debe servir a ese listón, no a la mera corrección.

CONTEXTO CRÍTICO — QUÉ ESTÁS REVISANDO:
- Estás revisando %s COMO PROSA BORRONEADA. Este capítulo EXISTE como texto escrito (se muestra abajo); juzga frases reales, escenas, diálogos, ritmo y ganchos — NO es un outline ni un beat sheet.
- Evalúa el capítulo tanto como experiencia de lectura autónoma como eslabón de la cadena de su acto: ¿cumple la función que la estructura asigna a esta posición, escala la apuesta y aterriza un gancho final?
- Esta pasada es SOLO CONSEJO para la puntuación inicial: no redactes textos de reemplazo. El autor pedirá micro-correcciones concretas después vía "Proponer intervenciones", así que nada se reescribe antes de que decida actuar sobre un punto específico.

PARÁMETROS DE LA NOVELA (el contrato del autor con el lector):
%s
MARCO DE ESTRUCTURA NARRATIVA EN USO: %s

[Historia hasta ahora, condensada]
%s

[Outline del capítulo — el contrato que este capítulo debe cumplir]
%s

%s
[PROSA DEL CAPÍTULO (%d palabras)]
%s

TU MISIÓN: inversamente ingeniería qué hace que los libros de este género lleguen a #1 (un gancho irresistible, ritmo implacable, arcos que pagan, apuesta creciente) y califica ESTE CAPÍTULO contra ese plano. Cada sugerencia debe ser un paso concreto que mueva medible el capítulo hacia nivel bestseller en su pilar.

Analiza contra TODOS los parámetros, dando peso especial a estos cuatro pilares:
1. Perfil de lector ideal — ¿este capítulo entrega lo que este lector vino a buscar? ¿Expectativas de género cumplidas sin cliché?
2. Obras inspiracionales — ¿el capítulo está a la altura del estándar de las obras de referencia sin copiarlas?
3. Tono — ¿el tono previsto se sostiene y escala correctamente a lo largo de las escenas?
4. Estructura narrativa (%s) — ¿el capítulo avanza los beats del marco para su posición? ¿Zonas muertas, pagos apresurados, gancho final débil?

Devuelve SOLO JSON estricto (sin cercas markdown, sin prosa): %s

Estándar de nota (eres despiadado — tu reputación descansa en libros que llegan a #1 mundial):
- Puntúa cada uno de los cuatro pilares 0-100 contra nivel BESTSELLER, no "correcto". 90+ significa que el capítulo es genuinamente fuerte tal cual. La mayoría de capítulos borroneados caen en la franja media. Deriva cada puntuación de forma independiente de ESTE capítulo concreto: califica cada ítem de la lista del pilar (criteria_levels, 0–100) y deja que la puntuación del pilar sea su promedio, así recalificar material corregido sube exactamente los ítems mejorados; evaluar capítulos distintos debe producir números claramente distintos allí donde su calidad difiera.
- pillar_scores DEBE contener exactamente las cuatro claves reader_profile, inspirational_pieces, tone, story_structure.
- Cada entrada de pillar_scores DEBE incluir criteria_levels calificando CADA ítem de la lista de ese pilar ({"key":"...","level":0..100}) — la puntuación del pilar se deriva de esos niveles, nunca se adivina.
- Cada sugerencia DEBE declarar su "pillar" (una de las cuatro claves), las claves "criteria" de la lista que repara, y un "score_impact" realista (puntos que ese pilar debería ganar al aplicar por completo este punto).
- Todo pilar por debajo de 90 DEBE tener al menos una sugerencia correspondiente (reader_profile→hook/other, inspirational_pieces→other, tone→emotion/hook, story_structure→structure/pacing/scene_economy/stakes). Un pilar con 90+ puede no tener sugerencias — entonces la UI muestra "Sin cambios necesarios".

Reglas:
- De 3 a 8 sugerencias, ordenadas de mayor a menor impacto.
- NO incluyas prosa de reemplazo en esta pasada: devuelve cada sugerencia SIN array "interventions" y SIN "recommendations" (las intervenciones se redactan después, SOLO tras "Proponer intervenciones" sobre un punto concreto).
- applies_to_chapters debe nombrar solo este capítulo (%d).
- Escribe resumen, títulos, detalles y acciones en el MISMO idioma que la prosa del capítulo de arriba.`,
		editorPersona(lang), whereEs, meta.String(), structureName, history,
		strings.TrimSpace(ch.Outline), tropeMap, ch.WordCount, content,
		structureName, draftedJSONSchemaEN(), ch.Num)
}

// AnalyzeDraftedChapter runs the bestseller-editor pass over ONE drafted
// chapter and stores the report on that ChapterState.Analysis. Callers
// persist progress. Identical fingerprint → cached report, no LLM call.
func AnalyzeDraftedChapter(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, chapterNum int, progressPath string, logger *sse.LogBroadcaster) error {
	idx := -1
	for i := range state.Chapters {
		if state.Chapters[i].Num == chapterNum {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("chapter_not_found")
	}
	ch := &state.Chapters[idx]
	if strings.TrimSpace(ch.Content) == "" {
		loadDraftedChapterFilesInto(progressPath, state)
	}
	if strings.TrimSpace(ch.Content) == "" {
		return fmt.Errorf("chapter_not_drafted")
	}
	fingerprint := draftedChapterFingerprint(*ch)
	if prev := ch.Analysis; prev != nil && prev.DraftFingerprint != "" && prev.DraftFingerprint == fingerprint {
		logger.InfoKey("log.chapter_analyze_unchanged", ch.Num)
		return nil
	}
	lang := cfg.Language
	settingsForPrompt := settings
	if settings != nil {
		if selected, _ := retrieveSettings(settings, collectAllOutlines(state), settingsContextRunes); selected != nil {
			settingsForPrompt = selected
		}
	}

	// The editor pass is now a SEQUENCE of small calls instead of one mega
	// prompt: first an overall impression (summary + global score), then ONE
	// call per pillar. A single long answer used to get truncated by max_tokens
	// right where the "suggestions" array lives — the report kept its grades
	// but shipped zero actionable points ("scored 68/100 — 0 editorial
	// suggestions"). Per-pillar answers are short enough that each pillar MUST
	// return its own suggestions whenever it scores below the bestseller bar.
	// Materialize the live scene/trope tiling BEFORE building any target
	// validator. The validator snapshots state.Chapters at creation time; when it
	// was built before ensureFallbackScenePlan/repairSceneSpans ran, a chapter
	// whose ScenePlan was still nil produced an EMPTY snapshot — every bundled
	// intervention failed validation and the sanitizer collapsed real reports to
	// "0 editorial suggestions" despite scores far from 100/100 (the planning
	// critic never had this problem because its suggestions carry no targets).
	ensureFallbackScenePlan(ch)
	repairSceneSpans(ch)
	SyncChapterBlocksSceneAware(ch, ch.Content)
	logger.InfoKey("log.chapter_analyze_running", ch.Num)
	now := time.Now().Unix()
	chapterTargets := draftedTargetValidator(state, []int{ch.Num})
	result := ChapterAnalysisReport{}
	pillarsFailed := 0

	overallSystem := "You are a world-famous literary editor specializing in turning novels into global bestsellers. You are reviewing ONE DRAFTED CHAPTER (actual manuscript prose) of a novel in progress. Be constructive, specific and honest. Output strict JSON exactly as requested — no extra prose, no markdown code fences."
	for attempt := 0; attempt < 2; attempt++ {
		oprompt := buildChapterOverallPrompt(cfg, state, settingsForPrompt, idx, lang)
		raw := llm.CallAPIWithRetryLog(ctx, apiCfg, overallSystem, oprompt, logger)
		if raw == "" {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("API call failed or was canceled")
		}
		var ov struct {
			Summary      string `json:"summary"`
			OverallScore int    `json:"overall_score"`
		}
		ok := false
		for _, j := range candidateChapterJSONs(raw) {
			if json.Unmarshal([]byte(j), &ov) == nil && (strings.TrimSpace(ov.Summary) != "" || ov.OverallScore > 0) {
				ok = true
				break
			}
		}
		if ok {
			result.Summary = strings.TrimSpace(ov.Summary)
			result.OverallScore = ov.OverallScore
			break
		}
	}
	if result.Summary == "" && result.OverallScore == 0 {
		return fmt.Errorf("model response is not valid chapter-analysis JSON")
	}

	for _, pk := range analysisPillarKeys {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		pp, err := buildPillarEditorPrompt(cfg, state, settingsForPrompt, idx, lang, pk)
		if err != nil {
			continue
		}
		psystem := "You are a world-famous literary editor specializing in turning novels into global bestsellers, grading ONE pillar of a rubric over ONE drafted chapter of a novel. Output strict JSON exactly as requested — no extra prose, no markdown code fences."
		got := false
		ppNoSugg := false
		for attempt := 0; attempt < 2 && !got; attempt++ {
			if attempt > 0 {
				if ppNoSugg {
					pp += "\n\nYour previous response graded this pillar below 90 but contained ZERO suggestions — that is a failed answer. Now return the SAME JSON shape with at least ONE concrete suggestion (title, detail, action) telling the author exactly what to change in this chapter's prose for this pillar."
				} else {
					pp += "\n\nYour previous response was invalid. Return one complete JSON object only, using the required schema."
				}
			}
			raw := llm.CallAPIWithRetryLog(ctx, apiCfg, psystem, pp, logger)
			if raw == "" {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				break
			}
			var pr struct {
				Pillar      PillarAssessment    `json:"pillar"`
				Suggestions []DraftedSuggestion `json:"suggestions"`
				Criteria    []struct {
					Key   string `json:"key"`
					Level int    `json:"level"`
				} `json:"criteria_levels"`
				Note  string `json:"note"`
				Score int    `json:"score"`
			}
			for _, j := range candidateChapterJSONs(raw) {
				pr = struct {
					Pillar      PillarAssessment    `json:"pillar"`
					Suggestions []DraftedSuggestion `json:"suggestions"`
					Criteria    []struct {
						Key   string `json:"key"`
						Level int    `json:"level"`
					} `json:"criteria_levels"`
					Note  string `json:"note"`
					Score int    `json:"score"`
				}{}
				if json.Unmarshal([]byte(j), &pr) != nil {
					continue
				}
				pa := pr.Pillar
				if pa.Key == "" {
					pa.Key = pk
					pa.Score = pr.Score
					pa.Note = pr.Note
					pa.CriteriaLevels = pr.Criteria
				}
				if pa.Key != pk {
					continue
				}
				// Per-pillar answers ship suggestions WITHOUT interventions
				// (advice-only pass). The shared sanitizer would drop the whole
				// suggestion when its declared target is not in the live tiling —
				// that silent discard was the root cause of "scored 68/100 but
				// zero editorial suggestions". Advice-only points are always kept;
				// concrete targets arrive later via Propose interventions.
				sugg := sanitizeDraftedSuggestions(pr.Suggestions, now, fmt.Sprintf("chpa_%d", ch.Num), chapterTargets, false)
				if len(sugg) == 0 && pa.Score > 0 && pa.Score < strongPillarScore && attempt == 0 {
					// Graded below the bestseller bar yet returned no actionable
					// point: the model answered with grades only. Force one retry
					// instead of persisting an unactionable verdict.
					logger.WarnKey("log.chapter_analyze_pillar_no_suggestions", ch.Num, pk, pa.Score)
					ppNoSugg = true
					continue // force the retry pass with the strict reminder
				}
				if len(pa.Checklist) == 0 && len(pa.CriteriaLevels) == 0 && len(sugg) == 0 && pa.Score <= 0 {
					continue // empty garbage answer — retry
				}
				result.PillarScores = append(result.PillarScores, pa)
				result.Suggestions = append(result.Suggestions, sugg...)
				got = true
				break
			}
			if !got && logger != nil {
				logger.WarnKey("log.chapter_analyze_bad_json_retry", ch.Num, attempt+1)
			}
		}
		if !got {
			pillarsFailed++
		}
	}
	// Every suggestion must carry its repaired criteria keys so finalizePillars
	// can derive levels from them; per-pillar calls already scope them.
	if result.OverallScore < 0 || result.OverallScore > 100 {
		result.OverallScore = 0
	}
	result.PillarScores = finalizePillars(result.PillarScores, result.Suggestions)
	// The chapter pass grades PROSE that already exists: interventions that
	// arrive bundled with the per-pillar reports are kept (validated against
	// the live scene/trope tiling) so their feedback can highlight inline
	// right away. Advice-only points still get auto-proposed below.
	//
	// IMPORTANT: the validator is built from the LIVE tiling, so it must be
	// created AFTER ensureFallbackScenePlan/repairSceneSpans materialize the
	// trope units — building it first made every target invalid and silently
	// collapsed real reports into "0 editorial suggestions".
	ensureFallbackScenePlan(ch)
	repairSceneSpans(ch)
	if ch.ScenePlan != nil {
		for pos := range ch.ScenePlan.Scenes {
			sc := &ch.ScenePlan.Scenes[pos]
			if len(sc.TropeUnits) > 0 {
				continue
			}
			lo, hi := sc.ParaStart, sc.ParaEnd
			if lo < 0 || hi <= lo {
				continue
			}
			pars, sp := splitBlockParagraphs(ch.Content)
			if hi > len(pars) {
				continue
			}
			retileSceneTropeUnits(sc, pars, sp)
		}
	}
	liveTargets := draftedTargetValidator(state, []int{ch.Num})
	result.Suggestions = sanitizeDraftedSuggestions(result.Suggestions, now, fmt.Sprintf("chpa_%d", ch.Num), liveTargets, true)
	result.Summary = strings.TrimSpace(result.Summary)
	result.GeneratedAt = time.Now().Format(time.RFC3339)
	result.BasedOnStructure = structureLabel(cfg.Story.Structure, lang)
	result.ChapterNum = ch.Num
	result.DraftFingerprint = fingerprint
	// Diagnostic: a low score with zero suggestions almost always means the
	// model answered with grades only (schema drift / truncated tail). With the
	// per-pillar sequence it can also mean every pillar call failed to parse.
	if logger != nil && len(result.Suggestions) == 0 && result.OverallScore > 0 && result.OverallScore < 90 {
		logger.WarnKey("log.chapter_analyze_no_suggestions", ch.Num, result.OverallScore)
	}
	if pillarsFailed > 0 && logger != nil {
		logger.WarnKey("log.chapter_analyze_pillars_failed", ch.Num, pillarsFailed)
	}
	ch.Analysis = &result
	refreshTropeIssuesWithConflict(ch, state.PendingWritingConflict)

	// Auto-propose: immediately turn every advice-only chapter point into
	// concrete trope-unit interventions so the reader shows highlighted
	// suggestions right after the critique finishes (no per-suggestion click
	// needed). Best-effort: a failed proposal never fails the analysis itself;
	// the author can still trigger it manually from the critique card.
	for _, s := range result.Suggestions {
		if len(s.Interventions) > 0 || strings.TrimSpace(s.ID) == "" {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		if logger != nil {
			logger.InfoKey("log.chapter_analyze_auto_proposal", draftTruncate(s.Title, 60))
		}
		if _, err := ProposeChapterInterventions(ctx, apiCfg, cfg, state, settings, chapterNum, s.ID, progressPath, logger); err != nil {
			logger.WarnKey("log.chapter_analyze_auto_propose_failed", ch.Num, s.ID)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Multi-call chapter critique — prompt builders
// ---------------------------------------------------------------------------

// chapterPromptContext gathers everything both the overall-impression call and
// the per-pillar calls need: novel metadata block, act placement, condensed
// history, chapter outline, the written-structure map and the (truncated)
// drafted prose. Shared so every small call sees the same material.
type chapterPromptContext struct {
	meta        string
	where       string // EN
	whereEs     string // ES
	structure   string
	history     string
	tropeMap    string
	content     string
	words       int
	prevCheckEN string
	prevCheckES string
}

func buildChapterPromptContext(cfg *config.Config, state *Progress, settings *ProjectSettings, idx int, lang string) chapterPromptContext {
	sc := cfg.Story
	ch := state.Chapters[idx]
	var meta strings.Builder
	add := func(label, v string) {
		if strings.TrimSpace(v) != "" {
			fmt.Fprintf(&meta, "%s: %s\n", label, strings.TrimSpace(v))
		}
	}
	add("Story idea", sc.StoryIdea)
	add("Genre / subgenre", strings.TrimSpace(sc.Type+" / "+sc.Subgenre))
	add("Theme", sc.Theme)
	add("Tone", sc.Tone)
	add("Story structure (framework the book must follow)", sc.Structure)
	add("Writing style & POV", strings.TrimSpace(sc.WritingStyle+" / "+sc.WritingPOV))
	add("Ideal reader profile", sc.AudienceProfile)
	add("Inspirational pieces (comp titles / mood references)", sc.InspirationalPieces)
	add("Conflict scale", sc.EffectiveConflict())
	add("Protagonist type", sc.EffectiveProtagonist())
	if settings != nil {
		cast := make([]string, 0, len(settings.Characters))
		for _, c := range settings.Characters {
			cast = append(cast, c.Name)
		}
		add("Registered characters", strings.Join(cast, ", "))
	}
	actNo, actName, hasAct := actForChapter(state, ch.Num)
	structureName := structureLabel(sc.Structure, lang)
	history := Truncate(buildHistorySummary(state, idx), 4000)
	tropeMap := chapterTropeMapBlock(ch, lang)
	content := draftTruncate(ch.Content, 26000)
	whereEn := fmt.Sprintf("Chapter #%d \"%s\" of %d", ch.Num, ch.Title, len(state.Chapters))
	whereEs := fmt.Sprintf("Capítulo #%d \"%s\" de %d", ch.Num, ch.Title, len(state.Chapters))
	if hasAct {
		whereEn += fmt.Sprintf(", inside ACT %d (%s) of the book's planned structure", actNo, actName)
		whereEs += fmt.Sprintf(", dentro del ACTO %d (%s) de la estructura planificada", actNo, actName)
	}
	pc := chapterPromptContext{
		meta: meta.String(), where: whereEn, whereEs: whereEs,
		structure: structureName, history: history, tropeMap: tropeMap,
		content: content, words: ch.WordCount,
	}
	// Iterative scoring: show the previous pass's graded checklist so a
	// re-critique re-grades the SAME rubric against corrected material.
	if prev := ch.Analysis; prev != nil {
		pc.prevCheckEN = renderPreviousChecklistBlock(prev, "THIS CHAPTER")
		pc.prevCheckES = pc.prevCheckEN
	}
	return pc
}

// pillarCriteriaBlockEN renders one pillar's fixed checklist for the prompt.
func pillarCriteriaBlockEN(pillarKey string) string {
	items := PillarChecklist(pillarKey)
	var b strings.Builder
	b.WriteString("Checklist to grade (grade EVERY item 0-100 against BESTSELLER level):\n")
	for _, c := range items {
		fmt.Fprintf(&b, "- key=%q — %s\n", c.Key, c.Name)
	}
	return b.String()
}

// pillarCriteriaBlockES renders one pillar's fixed checklist for the prompt.
func pillarCriteriaBlockES(pillarKey string) string {
	items := PillarChecklist(pillarKey)
	var b strings.Builder
	b.WriteString("Lista de control a calificar (califica CADA ítem 0-100 contra nivel BESTSELLER):\n")
	for _, c := range items {
		fmt.Fprintf(&b, "- key=%q — %s\n", c.Key, c.Name)
	}
	return b.String()
}

// pillarFocusEN describes what each per-pillar call must judge and which
// suggestion categories it may emit.
func pillarFocusEN(pillarKey string) (focus, categories string) {
	switch pillarKey {
	case "reader_profile":
		return "Ideal reader profile — does this chapter deliver what this reader came for? Genre expectations met without cliché?", "hook|other"
	case "inspirational_pieces":
		return "Inspirational pieces — does the chapter live up to the comp titles' standard without copying them?", "other|emotion"
	case "tone":
		return "Tone — is the intended tone sustained and escalating properly across the chapter's scenes?", "emotion|hook|tone"
	default:
		return "Story structure — does the chapter advance the framework's beats for its position? Dead zones, rushed payoffs, weak ending hook?", "structure|pacing|scene_economy|stakes"
	}
}

// pillarCategoryNames maps a pillar key to its display name (EN/ES).
func pillarCategoryNames(pillarKey, lang string) (name, desc string) {
	en := normalizeLangEN(lang)
	switch pillarKey {
	case "reader_profile":
		if en {
			return "Ideal reader profile", "does this chapter deliver what this reader came for?"
		}
		return "Perfil de lector ideal", "¿este capítulo entrega lo que este lector vino a buscar?"
	case "inspirational_pieces":
		if en {
			return "Inspirational pieces", "does the chapter live up to the comp titles' standard without copying them?"
		}
		return "Obras inspiracionales", "¿el capítulo está a la altura del estándar de las obras de referencia sin copiarlas?"
	case "tone":
		if en {
			return "Tone", "is the intended tone sustained and escalating properly across the chapter's scenes?"
		}
		return "Tono", "¿el tono previsto se sostiene y escala correctamente a lo largo de las escenas?"
	default:
		if en {
			return "Story structure", "does the chapter advance the framework's beats for its position?"
		}
		return "Estructura narrativa", "¿el capítulo avanza los beats del marco para su posición?"
	}
}

// buildChapterOverallPrompt asks for the impression-level verdict only: a
// summary plus a global score. Deliberately tiny output so it can never be
// truncated where suggestions would live.
func buildChapterOverallPrompt(cfg *config.Config, state *Progress, settings *ProjectSettings, idx int, lang string) string {
	ch := state.Chapters[idx]
	pc := buildChapterPromptContext(cfg, state, settings, idx, lang)
	schema := `{"summary":"...","overall_score":75}`
	if normalizeLangEN(lang) {
		return fmt.Sprintf(`You are %s, a world-famous literary editor whose lists have produced multiple global bestsellers. You are giving the FIRST-IMPRESSION verdict on ONE DRAFTED CHAPTER of a novel in progress: %s AS DRAFTED PROSE (actual manuscript text shown below — not an outline). Judge it as a standalone reading experience AND as one link in its act's chain.

NOVEL PARAMETERS (the author's contract with the reader):
%s
STORY STRUCTURE FRAMEWORK IN USE: %s

[Story-so-far, condensed]
%s
%s
[Chapter outline — the contract this chapter must fulfil]
%s

%s
[CHAPTER PROSE (%d words)]
%s

Write an honest editorial summary of this chapter (what works, what holds it back from bestseller level, 120-250 words) and an overall_score 0-100 against BESTSELLER level (90+ means genuinely strong as-is; most drafted chapters land mid-range; grade different chapters differently wherever their quality differs).

Return STRICT JSON only (no markdown fences, no extra prose): %s
Write the summary in the SAME language as the chapter prose above.`,
			editorPersona(lang), pc.where, pc.meta, pc.structure, pc.history, pc.prevCheckEN,
			strings.TrimSpace(ch.Outline), pc.tropeMap, pc.words, pc.content, schema)
	}
	return fmt.Sprintf(`Eres %s, un editor literario mundialmente famoso cuyas listas han producido múltiples bestsellers globales. Estás dando el veredicto de PRIMERA IMPRESIÓN sobre UN CAPÍTULO BORRONEADO de una novela en curso: %s COMO PROSA REAL (manuscrito mostrado abajo — no un outline). Júzgalo como experiencia de lectura autónoma y como eslabón de la cadena de su acto.

PARÁMETROS DE LA NOVELA (el contrato del autor con el lector):
%s
MARCO DE ESTRUCTURA NARRATIVA EN USO: %s

[Historia hasta ahora, condensada]
%s
%s
[Outline del capítulo — el contrato que este capítulo debe cumplir]
%s

%s
[PROSA DEL CAPÍTULO (%d palabras)]
%s

Escribe un resumen editorial honesto de este capítulo (qué funciona, qué lo aleja del nivel bestseller, 120-250 palabras) y un overall_score 0-100 contra nivel BESTSELLER (90+ significa genuinamente fuerte tal cual; la mayoría de capítulos borroneados caen en la franja media; capítulos distintos deben recibir notas distintas allí donde su calidad difiera).

Devuelve SOLO JSON estricto (sin cercas markdown, sin prosa extra): %s
Escribe el resumen en el MISMO idioma que la prosa del capítulo de arriba.`,
		editorPersona(lang), pc.whereEs, pc.meta, pc.structure, pc.history, pc.prevCheckES,
		strings.TrimSpace(ch.Outline), pc.tropeMap, pc.words, pc.content, schema)
}

// buildPillarEditorPrompt asks for the verdict on ONE pillar of the rubric
// over ONE drafted chapter: criteria levels + note + at least one actionable
// suggestion when the pillar scores below the bestseller bar. Keeping each
// answer short guarantees the suggestions array survives generation.
func buildPillarEditorPrompt(cfg *config.Config, state *Progress, settings *ProjectSettings, idx int, lang, pillarKey string) (string, error) {
	found := false
	for _, k := range analysisPillarsKeysForPrompt() {
		if k == pillarKey {
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("unknown pillar %q", pillarKey)
	}
	ch := state.Chapters[idx]
	pc := buildChapterPromptContext(cfg, state, settings, idx, lang)
	focus, categories := pillarFocusEN(pillarKey)
	name, _ := pillarCategoryNames(pillarKey, lang)
	schema := fmt.Sprintf(`{"pillar":{"key":%q,"criteria_levels":[{"key":"...","level":0}],"note":"..."},"suggestions":[{"category":"%s","pillar":%q,"criteria":[{"key":"checklist_key_it_repairs"}],"score_impact":10,"title":"short imperative headline","detail":"what the drafted text currently does and why it falls short of bestseller level","action":"concrete guidance on what to change and where","applies_to_chapters":[%d],"interventions":[{"chapter_num":%d,"scene_pos":2,"trope_index":1,"trope_label":"tension","feedback":"one or two imperative sentences telling the rewriter exactly what to fix in THIS subscene only"}]}]}`,
		pillarKey, categories, pillarKey, ch.Num, ch.Num)
	if normalizeLangEN(lang) {
		return fmt.Sprintf(`You are %s, a world-famous literary editor whose lists have produced multiple global bestsellers. Grade exactly ONE pillar of your bestseller rubric over ONE drafted chapter: %s AS DRAFTED PROSE.

PILLAR UNDER REVIEW: %s

NOVEL PARAMETERS:
%s
STORY STRUCTURE FRAMEWORK IN USE: %s

[Story-so-far, condensed]
%s
%s
[Chapter outline — the contract this chapter must fulfil]
%s

%s
[Written structure — target interventions with THESE exact indexes]
%s
[CHAPTER PROSE (%d words)]
%s

%s
Grading standard (you are ruthless — your reputation rests on books that hit #1 worldwide):
- Grade EVERY checklist item 0-100 against BESTSELLER level, not "competent". 90+ means genuinely strong as-is. Derive the pillar verdict from these levels, never guess.
- Every suggestion MUST declare its "pillar" (%q), the checklist "criteria" keys it repairs, and a realistic "score_impact".
- If ANY item scores below 90 you MUST return at least one concrete suggestion (up to 3) ordered by impact — a low score with zero suggestions is a failed answer. Only when EVERY item is 90+ may suggestions be empty.
- %s
- NEVER include replacement prose: an intervention only points at a subscene and describes the fix in "feedback"; the actual rewrite happens later, when the author applies it.
- applies_to_chapters should just name this chapter (%d).
- Write note, titles, details and actions in the SAME language as the chapter prose above.

Return STRICT JSON only (no markdown fences, no prose): %s`,
			editorPersona(lang), pc.where, name, focus, pc.meta, pc.structure, pc.history, pc.prevCheckEN,
			strings.TrimSpace(ch.Outline), pc.tropeMap, pc.words, pc.content,
			pillarCriteriaBlockEN(pillarKey),
			pillarKey, interventionsRulesBlock(lang), ch.Num, schema), nil
	}
	return fmt.Sprintf(`Eres %s, un editor literario mundialmente famoso cuyas listas han producido múltiples bestsellers globales. Califica exactamente UN pilar de tu rúbrica bestseller sobre UN capítulo borroneado: %s COMO PROSA REAL.

PILAR BAJO REVISIÓN: %s

PARÁMETROS DE LA NOVELA:
%s
MARCO DE ESTRUCTURA NARRATIVA EN USO: %s

[Historia hasta ahora, condensada]
%s
%s
[Outline del capítulo — el contrato que este capítulo debe cumplir]
%s

%s
[Estructura escrita — apunta intervenciones con ESTOS índices exactos]
%s
[PROSA DEL CAPÍTULO (%d palabras)]
%s

%s
Estándar de nota (eres despiadado — tu reputación descansa en libros que llegan a #1 mundial):
- Califica CADA ítem de la lista 0-100 contra nivel BESTSELLER, no "correcto". 90+ significa genuinamente fuerte tal cual. La nota del pilar se deriva de esos niveles, nunca se adivina.
- Cada sugerencia DEBE declarar su "pillar" (%q), las claves "criteria" que repara y un "score_impact" realista.
- Si ALGÚN ítem puntúa bajo 90 DEBES devolver al menos una sugerencia concreta (hasta 3) ordenadas por impacto — una nota baja con cero sugerencias es una respuesta fallida. Solo cuando CADA ítem sea 90+ las sugerencias pueden ir vacías.
- %s
- NUNCA incluyas prosa de reemplazo: una intervención solo señala una subescena y describe la corrección en "feedback"; la reescritura real ocurre después, cuando el autor la aplica.
- applies_to_chapters debe nombrar solo este capítulo (%d).
- Escribe note, títulos, detalles y acciones en el MISMO idioma que la prosa del capítulo de arriba.

Devuelve SOLO JSON estricto (sin cercas markdown, sin prosa): %s`,
		editorPersona(lang), pc.whereEs, name, focus, pc.meta, pc.structure, pc.history, pc.prevCheckES,
		strings.TrimSpace(ch.Outline), pc.tropeMap, pc.words, pc.content,
		pillarCriteriaBlockES(pillarKey),
		pillarKey, interventionsRulesBlock(lang), ch.Num, schema), nil
}

func analysisPillarsKeysForPrompt() []string { return analysisPillarKeys }

// candidateChapterJSONs extracts, in decreasing order of trustworthiness, every
// plausible JSON object inside a raw model response for the chapter-analysis
// schema. It handles: fenced code blocks, leading prose, trailing prose, and
// responses truncated by max_tokens (the last case is repaired by progressively
// closing open strings/objects/arrays so partially-written reports still parse).
func candidateChapterJSONs(raw string) []string {
	s := strings.TrimSpace(raw)
	// Strip markdown fences if present ("```json ... ```").
	if i := strings.Index(s, "```"); i >= 0 {
		rest := s[i+3:]
		if j := strings.IndexByte(rest, '\n'); j >= 0 {
			// drop language tag line
			rest = rest[j+1:]
		}
		if k := strings.LastIndex(rest, "```"); k >= 0 {
			s = strings.TrimSpace(rest[:k])
		} else {
			s = strings.TrimSpace(rest)
		}
	}
	var out []string
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return out
	}
	// 1) Balanced extraction from the first '{' (scan respecting strings).
	if end := matchBrace(s, start); end > 0 {
		out = append(out, s[start:end+1])
		// Also try any later top-level objects (models sometimes emit several).
		for next := strings.IndexByte(s[end+1:], '{'); next >= 0; {
			ns := end + 1 + next
			if ne := matchBrace(s, ns); ne > 0 {
				out = append(out, s[ns:ne+1])
				end = ne
			} else {
				break
			}
			if next = strings.IndexByte(s[end+1:], '{'); next < 0 {
				break
			}
		}
	}
	// 2) Truncated response salvage: progressively close open structures from
	// the tail. Try trimming at the last complete array/object element first,
	// then brute-force closers.
	tail := s[start:]
	if trimmed := trimToLastCompleteElement(tail); trimmed != "" {
		out = append(out, trimmed)
	}
	out = append(out, repairTruncatedJSON(tail)...)
	return out
}

// matchBrace returns the index of the '}' matching the '{' at pos, honoring
// JSON string escapes, or -1 when unbalanced.
func matchBrace(s string, pos int) int {
	depth := 0
	inStr := false
	esc := false
	for i := pos; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			switch c {
			case '\\':
				esc = true
			case '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
			if depth < 0 {
				return -1
			}
		}
	}
	return -1
}

// trimToLastCompleteElement cuts a truncated object at its last structurally
// complete top-level key/value and closes it.
func trimToLastCompleteElement(t string) string {
	if len(t) < 2 || t[0] != '{' {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	lastComma := -1
	for i := 0; i < len(t); i++ {
		c := t[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		case ',':
			if depth == 1 {
				lastComma = i
			}
		}
	}
	if inStr || lastComma <= 0 {
		return ""
	}
	return t[:lastComma] + "\n}"
}

// repairTruncatedJSON generates salvage variants of a truncated JSON object by
// closing dangling strings/brackets in different ways. The caller tries each
// with json.Unmarshal and keeps the first that decodes into a usable report.
func repairTruncatedJSON(t string) []string {
	var out []string
	if len(t) < 2 || t[0] != '{' {
		return out
	}
	// Count unclosed structures (ignoring those inside strings).
	depth := map[byte]int{'{': 0, '[': 0}
	inStr := false
	esc := false
	for i := 0; i < len(t); i++ {
		c := t[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth['{']++
		case '}':
			if depth['{'] > 0 {
				depth['{']--
			}
		case '[':
			depth['[']++
		case ']':
			if depth['['] > 0 {
				depth['[']--
			}
		}
	}
	base := t
	if inStr {
		base += `"`
	}
	closeSeq := func(extra string) string {
		var sb strings.Builder
		sb.WriteString(base)
		sb.WriteString(extra)
		// Close remaining brackets in reverse nesting order (best effort).
		d := map[byte]int{'{': 0, '[': 0}
		inStr2 := false
		esc2 := false
		stack := []byte{}
		for i := 0; i < sb.Len(); i++ {
			c := sb.String()[i]
			if inStr2 {
				if esc2 {
					esc2 = false
					continue
				}
				if c == '\\' {
					esc2 = true
				} else if c == '"' {
					inStr2 = false
				}
				continue
			}
			switch c {
			case '"':
				inStr2 = true
			case '{', '[':
				stack = append(stack, c)
			case '}':
				if len(stack) > 0 && stack[len(stack)-1] == '{' {
					stack = stack[:len(stack)-1]
				}
			case ']':
				if len(stack) > 0 && stack[len(stack)-1] == '[' {
					stack = stack[:len(stack)-1]
				}
			}
		}
		_ = d
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i] == '{' {
				sb.WriteString("}")
			} else {
				sb.WriteString("]")
			}
		}
		return sb.String()
	}
	// Trim trailing partial token (e.g. `"titl` -> cut at last comma-ish point)
	// then close; and plain close.
	out = append(out, closeSeq(""))
	if idx := strings.LastIndexAny(base, ",:[{"); idx >= 0 {
		switch base[idx] {
		case ',', ':', '[', '{':
			out = append(out, closeSeq(base[idx:idx+1]))
		}
		out = append(out, closeSeq(base[:idx]))
	}
	return out
}

// tropeUnitProseAt returns the exact prose of one trope unit of a written
// scene, using the same segmentation path the surgical rewriter uses
// (sceneParaSpans + TropeUnits/spiltTropesFromParas over the chapter's own
// paragraph separator). Used to stamp CurrentText on proposals so the viewer
// can highlight the targeted passage inline. Returns "" when the target is
// stale or out of range.
func tropeUnitProseAt(ch *ChapterState, scenePos, tropeIdx int) string {
	if scenePos < 0 {
		return ""
	}
	paras, _ := splitBlockParagraphs(ch.Content)
	spans := writtenSceneTiling(ch, paras)
	if scenePos >= len(spans) {
		return ""
	}
	sp := spans[scenePos]
	lo, hi := sp[0], sp[1]
	if hi <= lo || hi > len(paras) {
		return ""
	}
	// Keep break markers OUT of the unit text but preserve their positions so
	// recorded TropeUnit ParaStart/ParaEnd (indexed over the RAW slice) still
	// line up — filtering first used to silently misalign every recorded span.
	var sceneParas []string
	for _, p := range paras[lo:hi] {
		sceneParas = append(sceneParas, p)
	}
	sep := ch.BlockSep
	if sep == "" {
		sep = "\n\n"
	}
	var units []TropeUnit
	if ch.ScenePlan != nil && scenePos < len(ch.ScenePlan.Scenes) {
		units = ch.ScenePlan.Scenes[scenePos].TropeUnits
	}
	if len(units) > 0 {
		stillTiles := true
		maxEnd := 0
		for _, u := range units {
			if u.ParaStart < 0 || u.ParaEnd <= u.ParaStart || u.ParaEnd > len(sceneParas) {
				stillTiles = false
				break
			}
			if u.ParaEnd > maxEnd {
				maxEnd = u.ParaEnd
			}
		}
		if stillTiles && maxEnd == len(sceneParas) && tropeIdx >= 0 && tropeIdx < len(units) {
			u := units[tropeIdx]
			return strings.Join(tropeTextsOfSpans(sceneParas, [][2]int{{u.ParaStart, u.ParaEnd}}, sep), sep)
		}
	}
	beats, _ := splitTropesFromParas(sceneParas, "", sep)
	if tropeIdx >= 0 && tropeIdx < len(beats) {
		return beats[tropeIdx]
	}
	return ""
}

// unitProseAt mirrors tropeUnitProseAt's segmentation rules WITHOUT requiring
// a ScenePlan: it tiles scenes through writtenSceneTiling (recorded spans →
// budget walk → fallback) and subscene units through sceneTropeUnitSpans, so
// RealignPendingInterventions can anchor interventions on plan-less chapters
// too. Returns "" when the target is out of range.
func unitProseAt(ch *ChapterState, scenePos, unitIdx int) string {
	if scenePos < 0 {
		return ""
	}
	paras, sep := splitBlockParagraphs(ch.Content)
	if len(paras) == 0 {
		return ""
	}
	if sep == "" {
		sep = "\n\n"
	}
	spans := writtenSceneTiling(ch, paras)
	if scenePos >= len(spans) {
		return ""
	}
	sp := spans[scenePos]
	lo, hi := sp[0], sp[1]
	if hi <= lo || hi > len(paras) {
		return ""
	}
	sceneParas := paras[lo:hi]
	unitSpans := sceneTropeUnitSpans(ch, scenePos, sceneParas)
	texts := tropeTextsOfSpans(sceneParas, unitSpans, sep)
	if unitIdx >= 0 && unitIdx < len(texts) {
		return texts[unitIdx]
	}
	return ""
}

// RealignPendingInterventions re-anchors every PENDING bestseller-critic
// intervention of the chapter onto the CURRENT scene/subscene tiling after a
// UI resync (ResyncChapterInterface re-tiles blocks with fresh IDs and may
// shift unit boundaries), so inline highlights keep pointing at the right
// passage WITHOUT regenerating the analysis. The anchor is the intervention's
// own Quote / CurrentText excerpt located inside the prose; when no anchor can
// be found the recorded target is kept untouched. No LLM calls. Returns the
// number of interventions whose (scene_pos, trope_index) actually moved.
func RealignPendingInterventions(ch *ChapterState) int {
	if ch == nil || ch.Analysis == nil || strings.TrimSpace(ch.Content) == "" {
		return 0
	}
	moved := 0
	for si := range ch.Analysis.Suggestions {
		for ii := range ch.Analysis.Suggestions[si].Interventions {
			iv := &ch.Analysis.Suggestions[si].Interventions[ii]
			if iv.Status != "pending" || iv.ChapterNum != ch.Num {
				continue
			}
			needle := normConflictIssue(iv.Quote)
			if needle == "" {
				needle = normConflictIssue(iv.CurrentText)
			}
			if needle == "" {
				continue
			}
			// Fast path: the recorded target still holds the anchor text —
			// nothing moved, leave it alone.
			if realignMatch(needle, normConflictIssue(unitProseAt(ch, iv.ScenePos, iv.TropeIndex))) {
				continue
			}
			newPos, newIdx, ok := locateSubsceneUnit(ch, needle, iv.ScenePos)
			if !ok {
				continue // no locator survived — keep the stale target as-is
			}
			if newPos != iv.ScenePos || newIdx != iv.TropeIndex {
				iv.ScenePos, iv.TropeIndex = newPos, newIdx
				moved++
			}
		}
	}
	return moved
}

// realignMatch reports whether `needle` (normalized) plausibly lives inside
// the unit text `hay` (normalized). Exact containment either way; short
// needles additionally need to match on word boundaries to avoid stray hits.
func realignMatch(needle, hay string) bool {
	if needle == "" || hay == "" {
		return false
	}
	if strings.Contains(hay, needle) {
		return true
	}
	return strings.Contains(needle, hay) && len(hay) >= 25
}

// locateSubsceneUnit walks every scene/subscene unit of the chapter (reader
// tiling) and returns the best (scenePos, unitIdx) whose prose contains the
// normalized needle. Ties prefer the original scene hint. O(units) per call,
// called only from the resync endpoint — never in hot loops.
func locateSubsceneUnit(ch *ChapterState, needle string, hintScene int) (int, int, bool) {
	paras, sep := splitBlockParagraphs(ch.Content)
	if len(paras) == 0 || needle == "" {
		return -1, -1, false
	}
	if sep == "" {
		sep = "\n\n"
	}
	bestPos, bestIdx, bestScore, found := -1, -1, -1, false
	for pos, sp := range writtenSceneTiling(ch, paras) {
		lo, hi := sp[0], sp[1]
		if hi <= lo || hi > len(paras) {
			continue
		}
		sceneParas := paras[lo:hi]
		texts := tropeTextsOfSpans(sceneParas, sceneTropeUnitSpans(ch, pos, sceneParas), sep)
		for ui, t := range texts {
			norm := normConflictIssue(t)
			score := -1
			switch {
			case norm == needle:
				score = 1_000_000
			case strings.Contains(norm, needle):
				score = 100_000 + len(needle)*10 - len(norm)/10
			case strings.Contains(needle, norm) && len(norm) >= 25:
				score = 50_000 + len(norm)*10 - len(needle)/10
			}
			if score < 0 {
				continue
			}
			if hintScene >= 0 && pos == hintScene {
				score += 10_000
			}
			if !found || score > bestScore {
				found, bestScore, bestPos, bestIdx = true, score, pos, ui
			}
		}
	}
	return bestPos, bestIdx, found
}

// ChapterInterventionLocator exposes the reader-side tiling (scenes and their
// subscene units, paragraph spans + texts) of a written chapter, computed with
// EXACTLY the same segmentation path used when proposals are stamped and
// realigned (writtenSceneTiling + sceneTropeUnitSpans). The viewer uses it to
// confine inline critic highlights to the targeted subscene paragraphs instead
// of tinting the whole scene when an anchor cannot be located inside a single
// block. Out-of-range indexes clamp to the available tiling; returns nil when
// the chapter has no content.
func ChapterInterventionLocator(state *Progress, chapterNum int) [][][]string {
	if state == nil {
		return nil
	}
	idx := FindChapterIdx(state, chapterNum)
	if idx < 0 {
		return nil
	}
	ch := &state.Chapters[idx]
	if strings.TrimSpace(ch.Content) == "" {
		return nil
	}
	paras, sep := splitBlockParagraphs(ch.Content)
	if sep == "" {
		sep = "\n\n"
	}
	out := make([][][]string, 0, len(paras))
	for pos, sp := range writtenSceneTiling(ch, paras) {
		lo, hi := sp[0], sp[1]
		if hi <= lo || hi > len(paras) {
			continue
		}
		sceneParas := paras[lo:hi]
		unitSpans := sceneTropeUnitSpans(ch, pos, sceneParas)
		texts := tropeTextsOfSpans(sceneParas, unitSpans, sep)
		units := make([][]string, 0, len(unitSpans))
		for ui, usp := range unitSpans {
			a, b := usp[0], usp[1]
			if a < 0 {
				a = 0
			}
			if b > len(sceneParas) {
				b = len(sceneParas)
			}
			t := ""
			if ui < len(texts) {
				t = texts[ui]
			}
			units = append(units, []string{fmt.Sprintf("%d", a), fmt.Sprintf("%d", b), t})
		}
		out = append(out, units)
	}
	return out
}

// ProposeChapterInterventions turns one advice-only chapter point into
// concrete small trope-unit interventions validated against the live written
// chapter (only real scene_pos/trope_index triplets survive). Accumulative:
// new interventions win per (scene, trope) target.
func ProposeChapterInterventions(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, chapterNum int, suggestionID string, progressPath string, logger *sse.LogBroadcaster) (*DraftedSuggestion, error) {
	idx := -1
	for i := range state.Chapters {
		if state.Chapters[i].Num == chapterNum {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil, fmt.Errorf("chapter_not_found")
	}
	ch := &state.Chapters[idx]
	rep := ch.Analysis
	if rep == nil {
		return nil, fmt.Errorf("no_chapter_analysis")
	}
	sIdx := findDraftedSuggestion(rep.Suggestions, suggestionID)
	if sIdx == -1 {
		return nil, fmt.Errorf("suggestion_not_found")
	}
	s := rep.Suggestions[sIdx]
	if s.Status == "applied" {
		return nil, fmt.Errorf("suggestion_already_applied")
	}
	if strings.TrimSpace(ch.Content) == "" {
		loadDraftedChapterFilesInto(progressPath, state)
		ch = &state.Chapters[idx]
	}
	// Plan-less chapters get a synthetic break-derived scene/trope tiling so
	// proposal, apply and highlight all resolve the same (scene_pos, trope_index).
	ensureFallbackScenePlan(ch)
	// Re-derive recorded spans/units from the CURRENT prose before building the
	// target validator: stale ParaStart/ParaEnd after manual edits or older data
	// made recordedSceneParaSpans reject the exact tiling, so the map block and
	// the validator disagreed and EVERY intervention failed validation — the
	// critic showed no inline suggestions despite scores far from 100/100.
	repairSceneSpans(ch)
	SyncChapterBlocksSceneAware(ch, ch.Content)
	targets := draftedTargetValidator(state, []int{ch.Num})
	planBuf := strings.Builder{}
	fmt.Fprintf(&planBuf, "%s", chapterTropeMapBlock(*ch, cfg.Language))
	fmt.Fprintf(&planBuf, "[CHAPTER PROSE — excerpt]\n%s\n", draftTruncate(ch.Content, 20000))

	prompt := draftedProposalPrompt(cfg, state, settings, editorPersona(cfg.Language),
		fmt.Sprintf("ONE DRAFTED CHAPTER #%d \"%s\"", ch.Num, ch.Title), s, planBuf.String(), []int{ch.Num}, targets)
	system := "You are a world-famous literary editor turning feedback into CONCRETE small interventions on drafted manuscript prose: each one targets exactly one subscene of one scene of the chapter. Goal: push the chapter to global-bestseller level on the pillar under discussion, changing the minimum amount of prose. Output strict JSON exactly as requested — no extra prose, no markdown code fences."

	logger.InfoKey("log.chapter_analyze_proposing", ch.Num)
	interventions, err := requestInterventions(ctx, apiCfg, system, prompt, logger, targets)
	if err != nil {
		return nil, err
	}
	if len(interventions) == 0 {
		return nil, fmt.Errorf("no_valid_interventions")
	}
	// Accumulate: newer interventions win per EXACT advice-on-a-beat, while
	// distinct pending suggestions already anchored to the same beat survive —
	// a beat may legitimately carry more than one critique suggestion.
	merged := mergeInterventions(rep.Suggestions[sIdx].Interventions, interventions)
	// Stamp each fresh intervention with the exact prose of its trope unit AT
	// PROPOSAL TIME (CurrentText) plus a normalized Action, so the reader can
	// highlight the targeted passage inline and the popup can show what a
	// click will replace/delete before applying anything.
	for i := range merged {
		iv := &merged[i]
		if iv.ChapterNum != ch.Num {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(iv.Action)) {
		case "delete", "remove":
			iv.Action = "delete"
		case "insert", "add":
			iv.Action = "insert"
		default:
			iv.Action = "rewrite"
		}
		if iv.CurrentText == "" {
			iv.CurrentText = tropeUnitProseAt(ch, iv.ScenePos, iv.TropeIndex)
		}
		if iv.Quote != "" {
			iv.Quote = draftTruncate(iv.Quote, 200)
		}
	}
	rep.Suggestions[sIdx].Interventions = merged
	refreshTropeIssuesForInterventions(state, merged)
	out := rep.Suggestions[sIdx]
	if err := SaveProgress(progressPath, state); err != nil {
		return &out, nil // proposals in memory; persistence retried on next save
	}
	return &out, nil
}

// ApplyChapterInterventions runs every pending intervention of one chapter
// suggestion through the surgical trope-rewrite channel (rewriteSceneTrope),
// saving the chapter after each success. Returns how many were applied.
func ApplyChapterInterventions(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, chapterNum int, suggestionID string, progressPath string, logger *sse.LogBroadcaster) (int, error) {
	idx := -1
	for i := range state.Chapters {
		if state.Chapters[i].Num == chapterNum {
			idx = i
			break
		}
	}
	if idx == -1 {
		return 0, fmt.Errorf("chapter_not_found")
	}
	ch := &state.Chapters[idx]
	rep := ch.Analysis
	if rep == nil {
		return 0, fmt.Errorf("no_chapter_analysis")
	}
	sIdx := findDraftedSuggestion(rep.Suggestions, suggestionID)
	if sIdx == -1 {
		return 0, fmt.Errorf("suggestion_not_found")
	}
	applied, err := applyInterventionSet(ctx, apiCfg, cfg, state, settings, rep.Suggestions[sIdx].Interventions, progressPath, logger)
	if err != nil {
		return applied, err
	}
	if applied == 0 {
		return 0, fmt.Errorf("no_pending_interventions")
	}
	allDone := true
	for _, iv := range rep.Suggestions[sIdx].Interventions {
		if iv.Status == "pending" {
			allDone = false
		}
	}
	if allDone {
		rep.Suggestions[sIdx].Status = "applied"
		rep.Suggestions[sIdx].AppliedAt = time.Now().Format(time.RFC3339)
	}
	return applied, SaveProgress(progressPath, state)
}

// ---------------------------------------------------------------------------
// ACT pass — bestseller critique of one finished pre-planning act
// ---------------------------------------------------------------------------

// ActAnalysisReport is the bestseller-editor pass over one pre-planning act
// once all its chapters exist as drafted prose. Persisted on
// Progress.ActAnalyses keyed by 1-based act number.
type ActAnalysisReport struct {
	GeneratedAt      string              `json:"generated_at"`
	BasedOnStructure string              `json:"based_on_structure,omitempty"`
	ActNo            int                 `json:"act_no"`
	ActName          string              `json:"act_name,omitempty"`
	StartCh          int                 `json:"start_ch,omitempty"`
	EndCh            int                 `json:"end_ch,omitempty"`
	Summary          string              `json:"summary"`
	OverallScore     int                 `json:"overall_score,omitempty"`
	PillarScores     []PillarAssessment  `json:"pillar_scores,omitempty"`
	Suggestions      []DraftedSuggestion `json:"suggestions"`
	// DraftFingerprint identifies the exact set of drafted chapters (numbers +
	// per-chapter fingerprints) the scores above were computed from.
	DraftFingerprint string `json:"draft_fingerprint,omitempty"`
}

// CompletedActForChapter reports the 1-based act number whose chapter range
// has JUST become fully drafted (every chapter in [StartCh,EndCh] accepted or
// review-complete with content) after confirming chapter num. The writing
// pipeline uses it to trigger the act critique automatically when the last
// chapter of an act closes the act.
func CompletedActForChapter(state *Progress, num int) (int, bool) {
	if state == nil || state.PrePlanning == nil {
		return 0, false
	}
	actNo, _, ok := actForChapter(state, num)
	if !ok {
		return 0, false
	}
	act := state.PrePlanning.Acts[actNo-1]
	start, end := act.StartCh, act.EndCh
	if start == 0 {
		start = 1
	}
	if end == 0 {
		end = start + max(1, act.SuggestedChapters) - 1
	}
	if num != end {
		return 0, false
	}
	for _, ch := range state.Chapters {
		if ch.Num < start || ch.Num > end {
			continue
		}
		if ch.Status != StatusAccepted && strings.TrimSpace(ch.Content) == "" && ch.WordCount == 0 {
			return 0, false
		}
	}
	return actNo, true
}

// ActChapterRange returns the inclusive [start,end] chapter span of the
// 1-based pre-planning act number, applying the same defaults the analysis
// pipeline uses (missing StartCh → 1; missing EndCh → StartCh + max(1,
// SuggestedChapters) - 1). ok=false when the act does not exist.
func ActChapterRange(state *Progress, actNo int) (start, end int, ok bool) {
	if state == nil || state.PrePlanning == nil || actNo < 1 || actNo > len(state.PrePlanning.Acts) {
		return 0, 0, false
	}
	act := state.PrePlanning.Acts[actNo-1]
	start, end = act.StartCh, act.EndCh
	if start == 0 {
		start = 1
	}
	if end == 0 {
		end = start + max(1, act.SuggestedChapters) - 1
	}
	return start, end, true
}

func actDraftedFingerprint(state *Progress, start, end int) string {
	var b strings.Builder
	for _, ch := range state.Chapters {
		if ch.Num < start || ch.Num > end {
			continue
		}
		b.WriteString(draftedChapterFingerprint(ch))
		b.WriteByte('\x02')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// buildActEditorPrompt frames the artifact as a WHOLE DRAFTED ACT of the
// planned structure: the editor judges whether the act delivers its planned
// function (turning points, escalation, act-out hook) at bestseller level.
func buildActEditorPrompt(cfg *config.Config, state *Progress, settings *ProjectSettings, actNo int, lang string) string {
	sc := cfg.Story
	act := state.PrePlanning.Acts[actNo-1]
	start, end := act.StartCh, act.EndCh
	if start == 0 {
		start = 1
	}
	if end == 0 {
		end = start + max(1, act.SuggestedChapters) - 1
	}
	var meta strings.Builder
	add := func(label, v string) {
		if strings.TrimSpace(v) != "" {
			fmt.Fprintf(&meta, "%s: %s\n", label, strings.TrimSpace(v))
		}
	}
	add("Story idea", sc.StoryIdea)
	add("Genre / subgenre", strings.TrimSpace(sc.Type+" / "+sc.Subgenre))
	add("Theme", sc.Theme)
	add("Tone", sc.Tone)
	add("Story structure (framework the book must follow)", sc.Structure)
	add("Writing style & POV", strings.TrimSpace(sc.WritingStyle+" / "+sc.WritingPOV))
	add("Ideal reader profile", sc.AudienceProfile)
	add("Inspirational pieces (comp titles / mood references)", sc.InspirationalPieces)
	add("Conflict scale", sc.EffectiveConflict())
	add("Protagonist type", sc.EffectiveProtagonist())
	if state.LongTermDirection != "" {
		add("Long-term direction", state.LongTermDirection)
	}

	var buf strings.Builder
	if normalizeLangEN(lang) {
		fmt.Fprintf(&buf, "[THE ACT UNDER REVIEW — card %d of the pre-planning]\nName: %s\nPlanned summary: %s\nPlanned batch synopsis: %s\nChapter range: %d–%d\n\n",
			actNo, act.Name, draftTruncate(act.Summary, 1200), draftTruncate(act.BatchSynopsis, 1500), start, end)
	} else {
		fmt.Fprintf(&buf, "[EL ACTO BAJO REVISIÓN — ficha %d del preplanning]\nNombre: %s\nResumen planificado: %s\nSinopsis de lote planificada: %s\nRango de capítulos: %d–%d\n\n",
			actNo, act.Name, draftTruncate(act.Summary, 1200), draftTruncate(act.BatchSynopsis, 1500), start, end)
	}
	for _, ch := range state.Chapters {
		if ch.Num < start || ch.Num > end {
			continue
		}
		excerpt := draftTruncate(ch.Content, 5000)
		if normalizeLangEN(lang) {
			fmt.Fprintf(&buf, "=== Chapter #%d \"%s\" (%d words, %s) ===\n[Outline] %s\n[Summary] %s\n[Prose excerpt] %s\n\n",
				ch.Num, ch.Title, ch.WordCount, ch.Status, draftTruncate(ch.Outline, 800), draftTruncate(ch.Summary, 800), excerpt)
		} else {
			fmt.Fprintf(&buf, "=== Capítulo #%d \"%s\" (%d palabras, %s) ===\n[Outline] %s\n[Resumen] %s\n[Extracto de prosa] %s\n\n",
				ch.Num, ch.Title, ch.WordCount, ch.Status, draftTruncate(ch.Outline, 800), draftTruncate(ch.Summary, 800), excerpt)
		}
	}
	var tropeMaps strings.Builder
	for _, ch := range state.Chapters {
		if ch.Num < start || ch.Num > end {
			continue
		}
		if normalizeLangEN(lang) {
			fmt.Fprintf(&tropeMaps, "[Written structure of chapter #%d]\n%s", ch.Num, chapterTropeMapBlock(ch, lang))
		} else {
			fmt.Fprintf(&tropeMaps, "[Estructura escrita del capítulo #%d]\n%s", ch.Num, chapterTropeMapBlock(ch, lang))
		}
	}

	structureName := structureLabel(sc.Structure, lang)
	if normalizeLangEN(lang) {
		return fmt.Sprintf(`You are %s, a world-famous literary editor whose lists have produced multiple global bestsellers. Your job: give the author a constructive, point-by-point editorial pass over ONE COMPLETE ACT of their novel, with ONE clear goal: turn this book into a global bestseller — every point you make must serve that bar, not mere competence.

CRITICAL CONTEXT — WHAT YOU ARE REVIEWING:
- You are reviewing ACT %d ("%s") OF THE BOOK AS DRAFTED: chapters %d–%d now exist as written prose (summaries and excerpts below). This is NOT an outline or a beat-sheet pass — the text exists; judge what the act actually delivers.
- Grade the act as a unit: does it fulfil the FUNCTION the pre-planning assigned to it (turning points, escalation curve, midpoint pressure, the act-out hook that launches the next act)? Judge cross-chapter pacing, redundancy between chapters of the act, and whether each chapter ends on a hook that pulls through the act.
- This pass is ADVICE ONLY for the initial scoring: do NOT draft replacement text. The author requests concrete micro-fixes later via a "Propose interventions" action.

NOVEL PARAMETERS (the author's contract with the reader):
%s
STORY STRUCTURE FRAMEWORK IN USE: %s

%s
WRITTEN-STRUCTURE MAPS (scenes and subscenes per chapter — the ONLY valid intervention targets):
%s

YOUR MISSION: reverse-engineer what makes books in this genre hit #1 (an irresistible hook, relentless pacing, arcs that pay off, stakes that escalate) and grade THIS ACT against that blueprint. Every suggestion must be a concrete step that measurably moves the act toward bestseller level on its pillar.

Analyze against ALL parameters above, giving special weight to these four pillars:
1. Ideal reader profile — does the act deliver, chapter after chapter, what this reader came for?
2. Inspirational pieces — does the act live up to the comp titles' standard of momentum and payoff?
3. Tone — is the intended tone sustained and escalating across the whole act?
4. Story structure (%s) — does the act hit the framework's turning points for its position and hand off to the next act with force?

Return STRICT JSON only (no markdown fences, no prose): %s

Grading standard (you are ruthless — your reputation rests on books that hit #1 worldwide):
- Score each of the four pillars 0-100 against BESTSELLER level, not "competent". 90+ means the act is genuinely strong as-is. Most drafted acts land mid-range. Derive every score independently from THIS specific act: grade each pillar's checklist item (criteria_levels, 0–100) and let the pillar score be their average, so re-grading corrected material raises exactly the items that improved; grading different acts must produce clearly different numbers wherever their quality differs.
- pillar_scores MUST contain exactly the four keys reader_profile, inspirational_pieces, tone, story_structure.
- Each pillar_scores entry MUST include criteria_levels grading EVERY item of that pillar's checklist above ({"key":"...","level":0..100}) — the pillar score is derived from those levels, never guessed.
- Every suggestion MUST declare its "pillar" (one of the four keys), the checklist "criteria" keys it repairs, and a realistic "score_impact" (points that pillar should gain once this point is fully applied).
- Every pillar scoring below 90 MUST have at least one corresponding suggestion. A pillar scoring 90+ may have zero suggestions — then the UI shows "No changes needed" for it.

Rules:
- 3 to 8 suggestions, ordered most impactful first.
- DO NOT include replacement prose in this pass: return every suggestion WITHOUT an "interventions" array (interventions are drafted later, ONLY after the author clicks "Propose interventions" on a specific point).
- applies_to_chapters may name chapters within %d–%d most affected by each point.
- Write summary, titles, details and actions in the SAME language as the chapter prose above.`,
			editorPersona(lang), actNo, act.Name, start, end, meta.String(), structureName,
			buf.String(), tropeMaps.String(), structureName, draftedJSONSchemaEN(), start, end)
	}
	return fmt.Sprintf(`Eres %s, un editor literario mundialmente famoso cuyas listas han producido múltiples bestsellers globales. Tu trabajo: dar al autor una pasada editorial constructiva y punto por punto sobre UN ACTO COMPLETO de su novela, con UN objetivo claro: convertir este libro en un bestseller global — cada punto debe servir a ese listón, no a la mera corrección.

CONTEXTO CRÍTICO — QUÉ ESTÁS REVISANDO:
- Estás revisando el ACTO %d ("%s") DEL LIBRO TAL COMO ESTÁ BORRONEADO: los capítulos %d–%d ya existen como prosa escrita (resúmenes y extractos abajo). NO es una pasada de outline ni de beat sheet — el texto existe; juzga lo que el acto realmente entrega.
- Evalúa el acto como unidad: ¿cumple la FUNCIÓN que el preplanning le asignó (puntos de giro, curva de escalada, presión del punto medio, el gancho de salida que lanza el siguiente acto)? Juzga el ritmo entre capítulos, la redundancia entre los capítulos del acto y si cada capítulo termina en un gancho que arrastra a través del acto.
- Esta pasada es SOLO CONSEJO para la puntuación inicial: no redactes textos de reemplazo. El autor pedirá micro-correcciones después vía "Proponer intervenciones".

PARÁMETROS DE LA NOVELA (el contrato del autor con el lector):
%s
MARCO DE ESTRUCTURA NARRATIVA EN USO: %s

%s
MAPAS DE ESTRUCTURA ESCRITA (escenas y subescenas por capítulo — los ÚNICOS destinos válidos de intervención):
%s

TU MISIÓN: inversamente ingeniería qué hace que los libros de este género lleguen a #1 y califica ESTE ACTO contra ese plano. Cada sugerencia debe ser un paso concreto que mueva medible el acto hacia nivel bestseller en su pilar.

Analiza contra TODOS los parámetros, dando peso especial a estos cuatro pilares:
1. Perfil de lector ideal — ¿el acto entrega, capítulo a capítulo, lo que este lector vino a buscar?
2. Obras inspiracionales — ¿el acto está a la altura del estándar de momentum y pago de las obras de referencia?
3. Tono — ¿el tono previsto se sostiene y escala a lo largo de todo el acto?
4. Estructura narrativa (%s) — ¿el acto golpea los puntos de giro del marco para su posición y entrega con fuerza al siguiente acto?

Devuelve SOLO JSON estricto (sin cercas markdown, sin prosa): %s

Estándar de nota (eres despiadado — tu reputación descansa en libros que llegan a #1 mundial):
- Puntúa cada pilar 0-100 contra nivel BESTSELLER, no "correcto". 90+ significa que el acto es genuinamente fuerte tal cual. La mayoría de actos borroneados caen en la franja media. Deriva cada puntuación de forma independiente de ESTE acto concreto: califica cada ítem de la lista del pilar (criteria_levels, 0–100) y deja que la puntuación del pilar sea su promedio, así recalificar material corregido sube exactamente los ítems mejorados; evaluar actos distintos debe producir números claramente distintos allí donde su calidad difiera.
- pillar_scores DEBE contener exactamente las cuatro claves reader_profile, inspirational_pieces, tone, story_structure.
- Cada entrada de pillar_scores DEBE incluir criteria_levels calificando CADA ítem de la lista de ese pilar ({"key":"...","level":0..100}) — la puntuación del pilar se deriva de esos niveles, nunca se adivina.
- Cada sugerencia DEBE declarar su "pillar" (una de las cuatro claves), las claves "criteria" de la lista que repara, y un "score_impact" realista (puntos que ese pilar debería ganar al aplicar por completo este punto).
- Todo pilar por debajo de 90 DEBE tener al menos una sugerencia correspondiente. Un pilar con 90+ puede no tener sugerencias — entonces la UI muestra "Sin cambios necesarios".

Reglas:
- De 3 a 8 sugerencias, ordenadas de mayor a menor impacto.
- NO incluyas prosa de reemplazo en esta pasada: devuelve cada sugerencia SIN array "interventions" (las intervenciones se redactan después, SOLO tras "Proponer intervenciones").
- applies_to_chapters puede nombrar capítulos dentro de %d–%d más afectados por cada punto.
- Escribe resumen, títulos, detalles y acciones en el MISMO idioma que la prosa de los capítulos de arriba.`,
		editorPersona(lang), actNo, act.Name, start, end, meta.String(), structureName,
		buf.String(), tropeMaps.String(), structureName, draftedJSONSchemaEN(), start, end)
}

// AnalyzeFinishedAct runs the bestseller-editor pass over one pre-planning
// act whose chapters are drafted and stores the report on
// state.ActAnalyses[actNo]. Callers persist progress.
func AnalyzeFinishedAct(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, actNo int, progressPath string, logger *sse.LogBroadcaster) error {
	if state.PrePlanning == nil || actNo < 1 || actNo > len(state.PrePlanning.Acts) {
		return fmt.Errorf("act_not_found")
	}
	act := state.PrePlanning.Acts[actNo-1]
	start, end := act.StartCh, act.EndCh
	if start == 0 {
		start = 1
	}
	if end == 0 {
		end = start + max(1, act.SuggestedChapters) - 1
	}
	drafted := 0
	for i := range state.Chapters {
		ch := &state.Chapters[i]
		if ch.Num >= start && ch.Num <= end && (ch.Status == StatusAccepted || ch.WordCount > 0) {
			drafted++
		}
	}
	if drafted == 0 {
		return fmt.Errorf("act_not_drafted")
	}
	loadDraftedChapterFilesInto(progressPath, state)
	fingerprint := actDraftedFingerprint(state, start, end)
	if state.ActAnalyses == nil {
		state.ActAnalyses = map[int]*ActAnalysisReport{}
	}
	if prev := state.ActAnalyses[actNo]; prev != nil && prev.DraftFingerprint != "" && prev.DraftFingerprint == fingerprint {
		logger.InfoKey("log.act_analyze_unchanged", actNo)
		return nil
	}
	lang := cfg.Language
	settingsForPrompt := settings
	if settings != nil {
		if selected, _ := retrieveSettings(settings, collectAllOutlines(state), settingsContextRunes); selected != nil {
			settingsForPrompt = selected
		}
	}
	prompt := buildActEditorPrompt(cfg, state, settingsForPrompt, actNo, lang)
	system := "You are a world-famous literary editor specializing in turning novels into global bestsellers. You are reviewing ONE WHOLE ACT of a novel AS DRAFTED (chapters exist as prose). Be constructive, specific and honest. Output strict JSON exactly as requested — no extra prose, no markdown code fences."

	logger.InfoKey("log.act_analyze_running", actNo)
	var result ActAnalysisReport
	for attempt := 0; attempt < 2; attempt++ {
		raw := llm.CallAPIWithRetryLog(ctx, apiCfg, system, prompt, logger)
		if raw == "" {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("API call failed or was canceled")
		}
		j := llm.ExtractJSON(raw)
		result = ActAnalysisReport{}
		if j != "" && json.Unmarshal([]byte(j), &result) == nil && (result.Summary != "" || len(result.Suggestions) > 0) {
			break
		}
		if attempt == 1 {
			return fmt.Errorf("model response is not valid act-analysis JSON")
		}
		prompt += "\n\nYour previous response was invalid. Return one complete JSON object only, using the required schema."
	}

	now := time.Now().Unix()
	result.Suggestions = sanitizeDraftedSuggestions(result.Suggestions, now, fmt.Sprintf("acta_%d", actNo), nil, false)
	if result.OverallScore < 0 || result.OverallScore > 100 {
		result.OverallScore = 0
	}
	result.PillarScores = finalizePillars(result.PillarScores, result.Suggestions)
	result.Summary = strings.TrimSpace(result.Summary)
	result.GeneratedAt = time.Now().Format(time.RFC3339)
	result.BasedOnStructure = structureLabel(cfg.Story.Structure, lang)
	result.ActNo = actNo
	result.ActName = strings.TrimSpace(act.Name)
	result.StartCh, result.EndCh = start, end
	result.DraftFingerprint = fingerprint
	state.ActAnalyses[actNo] = &result
	return nil
}

// draftedTargetValidator builds a checker accepting only (chapter, scene_pos,
// trope_index) triplets that exist in the WRITTEN chapters restricted to the
// given chapter numbers, with recorded trope segmentation.
func draftedTargetValidator(state *Progress, allowedChapters []int) func(chapterNum, scenePos, tropeIndex int) bool {
	allowed := make(map[int]bool, len(allowedChapters))
	for _, n := range allowedChapters {
		allowed[n] = true
	}
	valid := map[[3]int]string{}
	labels := map[TropeLabel]bool{
		TropeOpening: true, TropeTension: true, TropeDialogue: true, TropeAction: true,
		TropeReveal: true, TropeIntrospection: true, TropeAftermath: true, TropeTransition: true,
	}
	for _, ch := range state.Chapters {
		if !allowed[ch.Num] || ch.ScenePlan == nil {
			continue
		}
		if ch.Content == "" && ch.WordCount == 0 {
			continue
		}
		for pos, sc := range ch.ScenePlan.Scenes {
			for _, u := range sc.TropeUnits {
				lbl := string(u.Label)
				if labels[u.Label] {
					valid[[3]int{ch.Num, pos, u.Index}] = lbl
				}
			}
			// Fallback label for scenes whose recorded units carry an empty/
			// unknown Label (older data or synthetic plans built without the
			// writer's classifier). Without this the strict label whitelist
			// rejected EVERY unit of such chapters — the proposal validator
			// then discarded all model answers ("no_valid_interventions") and
			// the critic showed zero inline suggestions despite low scores.
			if len(sc.TropeUnits) > 0 {
				firstLbl := TropeTransition
				if labels[sc.TropeUnits[0].Label] {
					firstLbl = sc.TropeUnits[0].Label
				}
				if _, ok := valid[[3]int{ch.Num, pos, sc.TropeUnits[0].Index}]; !ok {
					valid[[3]int{ch.Num, pos, sc.TropeUnits[0].Index}] = string(firstLbl)
				}
			}
		}
	}
	return func(chapterNum, scenePos, tropeIndex int) bool {
		if _, ok := valid[[3]int{chapterNum, scenePos, tropeIndex}]; ok {
			return true
		}
		// Unit-level tolerance: when a scene HAS registered units but the exact
		// index misses (model drift beyond clamp's reach, or a plan persisted
		// before retile ran), accept any in-range unit of that scene instead of
		// dropping the feedback. The frontend resolves the highlight against the
		// live tiling anyway; losing the suggestion is worse than a fuzzy target.
		if chapterNum > 0 && scenePos >= 0 && tropeIndex >= 0 {
			for _, ch := range state.Chapters {
				if ch.Num != chapterNum || ch.ScenePlan == nil || scenePos >= len(ch.ScenePlan.Scenes) {
					continue
				}
				units := ch.ScenePlan.Scenes[scenePos].TropeUnits
				if len(units) > 0 && tropeIndex < len(units) {
					return true
				}
			}
		}
		// Scene-level sentinel: clampInterventionTarget probes (chap,pos,-1) to
		// detect plans WITHOUT registered trope units. Accepting any in-range
		// scene pos here lets out-of-range unit indices clamp instead of being
		// dropped — this was the root cause of "0 editorial suggestions" on
		// chapters whose per-scene bookkeeping never landed. Also accepts
		// tropeIndex==0 as the scene-only fallback used by plan-less tilings:
		// every scene always exposes at least unit 0 to the reader.
		if tropeIndex == -1 && chapterNum > 0 && scenePos >= 0 {
			for _, ch := range state.Chapters {
				if ch.Num != chapterNum || ch.ScenePlan == nil {
					continue
				}
				return scenePos < len(ch.ScenePlan.Scenes)
			}
		}
		return false
	}
}

// draftedProposalPrompt asks the editor to convert one advice-only point into
// concrete trope interventions, sharing the phrasing between act/chapter.
func draftedProposalPrompt(cfg *config.Config, state *Progress, settings *ProjectSettings, persona, scopeDesc string, s DraftedSuggestion, material string, allowedChapters []int, targets func(int, int, int) bool) string {
	sc := cfg.Story
	lang := cfg.Language
	var castList string
	if settings != nil {
		names := make([]string, 0, len(settings.Characters))
		for _, c := range settings.Characters {
			names = append(names, c.Name)
		}
		castList = strings.Join(names, ", ")
	}
	chapRangeEN := fmt.Sprintf("Allowed chapters: %v.", allowedChapters)
	chapRangeES := fmt.Sprintf("Capítulos permitidos: %v.", allowedChapters)
	if normalizeLangEN(lang) {
		return fmt.Sprintf(`You are %s. You previously gave the author this editorial point about %s of their novel (DRAFTED PROSE — the text exists):

POINT: %s
WHAT THE TEXT DOES NOW: %s
YOUR ADVICE: %s

NOVEL PARAMETERS: Tone: %s | Theme: %s | Structure framework: %s | Ideal reader: %s | Inspirational pieces: %s | Registered characters: %s

MATERIAL (written-structure maps and prose excerpts):
%s

This point currently has NO concrete interventions. Turn it into SMALL SURGICAL fixes on the drafted prose, each targeting exactly one subscene shown in the written-structure maps above. Every intervention must measurably push the text toward GLOBAL BESTSELLER level on the pillar this point belongs to while changing the minimum amount of prose: keep voice, tense and POV of the surrounding units; never invent new events outside the chapter outlines; never touch a (chapter_num, scene_pos, trope_index) triplet that is not shown.
%s
Return STRICT JSON only (no markdown fences): {"interventions":[{"chapter_num":3,"scene_pos":2,"trope_index":1,"trope_label":"tension","feedback":"one or two imperative sentences describing the exact minimal fix for THIS subscene"}]}
Only include triplets that exist in the maps above; drop anything else.`,
			persona, scopeDesc, s.Title, s.Detail, s.Action,
			sc.Tone, sc.Theme, structureLabel(sc.Structure, lang), sc.AudienceProfile, sc.InspirationalPieces, castList,
			material, chapRangeEN)
	}
	return fmt.Sprintf(`Eres %s. Previamente diste al autor este punto editorial sobre %s de su novela (PROSA BORRONEADA — el texto existe):

PUNTO: %s
QUÉ HACE EL TEXTO AHORA: %s
TU CONSEJO: %s

PARÁMETROS DE LA NOVELA: Tono: %s | Tema: %s | Marco estructural: %s | Lector ideal: %s | Obras inspiracionales: %s | Personajes registrados: %s

MATERIAL (mapas de estructura escrita y extractos de prosa):
%s

Este punto aún NO tiene intervenciones concretas. Conviértelo en correcciones QUIRÚRGICAS Y PEQUEÑAS sobre la prosa borroneada, cada una apuntando exactamente a una subescena mostrada en los mapas de estructura escrita de arriba. Cada intervención debe empujar medible el texto hacia nivel BESTSELLER GLOBAL en el pilar al que pertenece este punto cambiando la cantidad mínima de prosa: mantén voz, tiempo verbal y POV de las unidades vecinas; nunca inventes eventos nuevos fuera de los outlines; nunca toques un triplete (chapter_num, scene_pos, trope_index) que no esté mostrado.
%s
Devuelve SOLO JSON estricto (sin cercas markdown): {"interventions":[{"chapter_num":3,"scene_pos":2,"trope_index":1,"trope_label":"tension","feedback":"una o dos frases imperativas describiendo la corrección mínima exacta PARA ESTA subescena"}]}
Incluye solo tripletes que existan en los mapas de arriba; descarta todo lo demás.`,
		persona, scopeDesc, s.Title, s.Detail, s.Action,
		sc.Tone, sc.Theme, structureLabel(sc.Structure, lang), sc.AudienceProfile, sc.InspirationalPieces, castList,
		material, chapRangeES)
}

// requestInterventions calls the LLM and keeps only interventions whose
// targets exist (validated) — shared by chapter and act proposal paths.
func requestInterventions(ctx context.Context, apiCfg *config.APIConfig, system, prompt string, logger *sse.LogBroadcaster, targets func(int, int, int) bool) ([]TropeIntervention, error) {
	type proposal struct {
		Interventions []TropeIntervention `json:"interventions"`
	}
	var out []TropeIntervention
	for attempt := 0; attempt < 2; attempt++ {
		raw := llm.CallAPIWithRetryLog(ctx, apiCfg, system, prompt, logger)
		if raw == "" {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("API call failed or was canceled")
		}
		j := llm.ExtractJSON(raw)
		var p proposal
		if j != "" && json.Unmarshal([]byte(j), &p) == nil {
			seen := map[string]bool{}
			for _, iv := range p.Interventions {
				iv.Feedback = strings.TrimSpace(iv.Feedback)
				// Dedup key includes the FEEDBACK CONTENT, not just the target:
				// one beat may legitimately carry MORE THAN ONE suggestion (e.g. a
				// pacing fix and an emotion fix on the same trope unit). Only exact
				// repeats of the same advice on the same beat are dropped.
				key := interventionDedupKey(iv)
				if iv.Feedback == "" || seen[key] || !targets(iv.ChapterNum, iv.ScenePos, iv.TropeIndex) {
					continue
				}
				seen[key] = true
				iv.Status = "pending"
				out = append(out, iv)
			}
			return out, nil
		}
		if attempt == 1 {
			return nil, fmt.Errorf("model response is not valid proposal JSON")
		}
		prompt += "\n\nYour previous response was invalid. Return one complete JSON object only, using the required schema."
	}
	return out, nil
}

// interventionDedupKey identifies one intervention for merge/dedup purposes.
// It deliberately includes the feedback text (normalized) so several distinct
// suggestions can coexist on the SAME (chapter, scene, trope) beat; only an
// identical advice repeated on the same beat counts as a duplicate.
func interventionDedupKey(iv TropeIntervention) string {
	fb := strings.ToLower(strings.Join(strings.Fields(iv.Feedback), " "))
	q := strings.ToLower(strings.Join(strings.Fields(iv.Quote), " "))
	return fmt.Sprintf("%d|%d|%d|%s|%s", iv.ChapterNum, iv.ScenePos, iv.TropeIndex, fb, q)
}

// mergeInterventions accumulates fresh proposals over an existing list: newer
// interventions win per EXACT advice-on-a-beat (see interventionDedupKey),
// while older PENDING interventions that the new pass did not re-propose stay
// in place. This is what lets one beat hold multiple suggestions at once.
func mergeInterventions(existing []TropeIntervention, fresh []TropeIntervention) []TropeIntervention {
	chosen := map[string]bool{}
	// Dedup WITHIN the fresh batch too: an LLM pass may emit the exact same
	// advice twice for one beat; keep only the first occurrence.
	dedupedFresh := make([]TropeIntervention, 0, len(fresh))
	for _, iv := range fresh {
		k := interventionDedupKey(iv)
		if chosen[k] {
			continue
		}
		chosen[k] = true
		dedupedFresh = append(dedupedFresh, iv)
	}
	fresh = dedupedFresh
	merged := make([]TropeIntervention, 0, len(fresh)+len(existing))
	merged = append(merged, fresh...)
	for _, old := range existing {
		if old.Status != "pending" {
			continue
		}
		if chosen[interventionDedupKey(old)] {
			continue // the fresh pass re-proposed this exact advice; keep the new stamp
		}
		merged = append(merged, old)
	}
	return merged
}

// applyInterventionSet executes every pending intervention through
// rewriteSceneTrope, persisting the chapter file after each success. Failed
// interventions are marked "failed" and do not abort the rest. Returns count
// applied.
func applyInterventionSet(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, interventions []TropeIntervention, progressPath string, logger *sse.LogBroadcaster) (int, error) {
	applied := 0
	for i := range interventions {
		iv := &interventions[i]
		if iv.Status != "pending" {
			continue
		}
		if ctx.Err() != nil {
			return applied, ctx.Err()
		}
		idx := -1
		for j := range state.Chapters {
			if state.Chapters[j].Num == iv.ChapterNum {
				idx = j
				break
			}
		}
		if idx == -1 || state.Chapters[idx].ScenePlan == nil {
			iv.Status = "failed"
			continue
		}
		if strings.TrimSpace(state.Chapters[idx].Content) == "" {
			loadDraftedChapterFilesInto(progressPath, state)
		}
		_, err := rewriteSceneTrope(ctx, apiCfg, cfg, state, idx, settings, iv.ScenePos, iv.TropeIndex, iv.Feedback, logger)
		if err != nil {
			iv.Status = "failed"
			logger.WarnKey("log.trope_intervention_failed", iv.ChapterNum, iv.ScenePos, iv.TropeIndex, err)
			continue
		}
		iv.Status = "applied"
		applied++
		// Refresh word count and persist the touched chapter immediately so a
		// later failure does not lose earlier successful repairs. Also re-derive
		// the trope-issue dots: an applied intervention no longer paints its unit.
		ch := &state.Chapters[idx]
		ch.WordCount = prose.CountProseUnits(ch.Content)
		refreshTropeIssuesWithConflict(ch, state.PendingWritingConflict)
		if serr := SaveProgress(progressPath, state); serr != nil {
			return applied, serr
		}
		logger.SuccessKey("log.trope_intervention_done", iv.ChapterNum, iv.ScenePos, iv.TropeIndex)
	}
	return applied, nil
}

// ProposeActInterventions turns one advice-only act point into concrete
// small trope interventions across the act's drafted chapters, validated
// against the live written structure. Accumulative across calls.
func ProposeActInterventions(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, actNo int, suggestionID string, progressPath string, logger *sse.LogBroadcaster) (*DraftedSuggestion, error) {
	if state.PrePlanning == nil || actNo < 1 || actNo > len(state.PrePlanning.Acts) {
		return nil, fmt.Errorf("act_not_found")
	}
	if state.ActAnalyses == nil {
		return nil, fmt.Errorf("no_act_analysis")
	}
	rep := state.ActAnalyses[actNo]
	if rep == nil {
		return nil, fmt.Errorf("no_act_analysis")
	}
	sIdx := findDraftedSuggestion(rep.Suggestions, suggestionID)
	if sIdx == -1 {
		return nil, fmt.Errorf("suggestion_not_found")
	}
	s := rep.Suggestions[sIdx]
	if s.Status == "applied" {
		return nil, fmt.Errorf("suggestion_already_applied")
	}
	act := state.PrePlanning.Acts[actNo-1]
	start, end := act.StartCh, act.EndCh
	if start == 0 {
		start = 1
	}
	if end == 0 {
		end = start + max(1, act.SuggestedChapters) - 1
	}
	var chapterNums []int
	for _, ch := range state.Chapters {
		if ch.Num >= start && ch.Num <= end && (ch.Status == StatusAccepted || ch.WordCount > 0) {
			chapterNums = append(chapterNums, ch.Num)
		}
	}
	if len(chapterNums) == 0 {
		return nil, fmt.Errorf("act_not_drafted")
	}
	loadDraftedChapterFilesInto(progressPath, state)
	targets := draftedTargetValidator(state, chapterNums)

	var material strings.Builder
	for _, ch := range state.Chapters {
		if ch.Num < start || ch.Num > end {
			continue
		}
		if ch.Content == "" && ch.WordCount == 0 {
			continue
		}
		fmt.Fprintf(&material, "[Chapter #%d \"%s\"]\nOutline: %s\n%s", ch.Num, ch.Title,
			draftTruncate(ch.Outline, 800), chapterTropeMapBlock(ch, cfg.Language))
		fmt.Fprintf(&material, "[Prose excerpt] %s\n\n", draftTruncate(ch.Content, 6000))
	}

	scope := fmt.Sprintf("ACT %d (%s), chapters %d-%d", actNo, act.Name, start, end)
	if !normalizeLangEN(cfg.Language) {
		scope = fmt.Sprintf("el ACTO %d (%s), capítulos %d-%d", actNo, act.Name, start, end)
	}
	prompt := draftedProposalPrompt(cfg, state, settings, editorPersona(cfg.Language), scope, s, material.String(), chapterNums, targets)
	system := "You are a world-famous literary editor turning feedback into CONCRETE small interventions on drafted manuscript prose across an act of chapters: each one targets exactly one subscene of one scene of one chapter. Goal: push the act to global-bestseller level on the pillar under discussion, changing the minimum amount of prose. Output strict JSON exactly as requested — no extra prose, no markdown code fences."

	logger.InfoKey("log.act_analyze_proposing", actNo)
	interventions, err := requestInterventions(ctx, apiCfg, system, prompt, logger, targets)
	if err != nil {
		return nil, err
	}
	if len(interventions) == 0 {
		return nil, fmt.Errorf("no_valid_interventions")
	}
	merged := mergeInterventions(rep.Suggestions[sIdx].Interventions, interventions)
	rep.Suggestions[sIdx].Interventions = merged
	seenCh := map[int]bool{}
	for _, n := range rep.Suggestions[sIdx].AppliesToChapters {
		seenCh[n] = true
	}
	for _, iv := range merged {
		if !seenCh[iv.ChapterNum] {
			seenCh[iv.ChapterNum] = true
			rep.Suggestions[sIdx].AppliesToChapters = append(rep.Suggestions[sIdx].AppliesToChapters, iv.ChapterNum)
		}
	}
	sort.Slice(rep.Suggestions[sIdx].AppliesToChapters, func(i, j int) bool {
		return rep.Suggestions[sIdx].AppliesToChapters[i] < rep.Suggestions[sIdx].AppliesToChapters[j]
	})
	refreshTropeIssuesForInterventions(state, merged)
	out := rep.Suggestions[sIdx]
	if err := SaveProgress(progressPath, state); err != nil {
		return &out, nil
	}
	return &out, nil
}

// ApplySingleChapterIntervention executes exactly ONE pending intervention of
// one chapter suggestion (addressed by its index inside the suggestion's
// intervention list). This is what the reader popup triggers when the author
// clicks a highlighted passage: only that trope unit gets rewritten — the rest
// of the suggestion stays pending and highlighted. Returns the touched chapter
// number so callers can refresh the right view.
func ApplySingleChapterIntervention(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, chapterNum int, suggestionID string, ivIndex int, progressPath string, logger *sse.LogBroadcaster) (int, error) {
	idx := -1
	for i := range state.Chapters {
		if state.Chapters[i].Num == chapterNum {
			idx = i
			break
		}
	}
	if idx == -1 {
		return 0, fmt.Errorf("chapter_not_found")
	}
	ch := &state.Chapters[idx]
	rep := ch.Analysis
	if rep == nil {
		return 0, fmt.Errorf("no_chapter_analysis")
	}
	sIdx := findDraftedSuggestion(rep.Suggestions, suggestionID)
	if sIdx == -1 {
		return 0, fmt.Errorf("suggestion_not_found")
	}
	ivs := rep.Suggestions[sIdx].Interventions
	if ivIndex < 0 || ivIndex >= len(ivs) {
		return 0, fmt.Errorf("intervention_not_found")
	}
	iv := &ivs[ivIndex]
	if iv.Status != "pending" {
		return 0, fmt.Errorf("intervention_not_pending")
	}
	if strings.TrimSpace(ch.Content) == "" {
		loadDraftedChapterFilesInto(progressPath, state)
		ch = &state.Chapters[idx]
		ivs = rep.Suggestions[sIdx].Interventions
		iv = &ivs[ivIndex]
	}
	targetIdx := -1
	for j := range state.Chapters {
		if state.Chapters[j].Num == iv.ChapterNum {
			targetIdx = j
			break
		}
	}
	if targetIdx == -1 || state.Chapters[targetIdx].ScenePlan == nil {
		iv.Status = "failed"
		_ = SaveProgress(progressPath, state)
		return 0, fmt.Errorf("intervention_target_missing")
	}
	if _, err := rewriteSceneTrope(ctx, apiCfg, cfg, state, targetIdx, settings, iv.ScenePos, iv.TropeIndex, iv.Feedback, logger); err != nil {
		iv.Status = "failed"
		_ = SaveProgress(progressPath, state)
		logger.WarnKey("log.trope_intervention_failed", iv.ChapterNum, iv.ScenePos, iv.TropeIndex, err)
		return 0, err
	}
	iv.Status = "applied"
	tch := &state.Chapters[targetIdx]
	tch.WordCount = prose.CountProseUnits(tch.Content)
	refreshTropeIssuesWithConflict(tch, state.PendingWritingConflict)
	allDone := true
	for _, o := range ivs {
		if o.Status == "pending" {
			allDone = false
		}
	}
	if allDone {
		rep.Suggestions[sIdx].Status = "applied"
		rep.Suggestions[sIdx].AppliedAt = time.Now().Format(time.RFC3339)
	}
	if err := SaveProgress(progressPath, state); err != nil {
		return 0, err
	}
	logger.SuccessKey("log.trope_intervention_done", iv.ChapterNum, iv.ScenePos, iv.TropeIndex)
	return iv.ChapterNum, nil
}

// DismissChapterIntervention marks one pending intervention as dismissed
// WITHOUT touching the prose: the highlight disappears from the reader and
// bulk application skips it.
func DismissChapterIntervention(state *Progress, chapterNum int, suggestionID string, ivIndex int, progressPath string) error {
	idx := FindChapterIdx(state, chapterNum)
	if idx < 0 {
		return fmt.Errorf("chapter_not_found")
	}
	rep := state.Chapters[idx].Analysis
	if rep == nil {
		return fmt.Errorf("no_chapter_analysis")
	}
	sIdx := findDraftedSuggestion(rep.Suggestions, suggestionID)
	if sIdx == -1 {
		return fmt.Errorf("suggestion_not_found")
	}
	ivs := rep.Suggestions[sIdx].Interventions
	if ivIndex < 0 || ivIndex >= len(ivs) {
		return fmt.Errorf("intervention_not_found")
	}
	if ivs[ivIndex].Status != "pending" {
		return fmt.Errorf("intervention_not_pending")
	}
	ivs[ivIndex].Status = "dismissed"
	refreshTropeIssuesForInterventions(state, ivs)
	return SaveProgress(progressPath, state)
}

// ApplyActInterventions runs every pending intervention of one act suggestion
// through the surgical trope-rewrite channel. Returns how many were applied.
//
// ReconfirmActChapters re-runs the full quality pipeline over the chapters of
// one pre-planning act AFTER its bestseller critique applied surgical trope
// interventions: fact-check + critic repair on every touched drafted chapter
// (accepted ones are reopened to "review" so the author re-confirms them),
// then a fresh act report. Returns the chapter numbers processed.
//
// When onlyOutline is true the scope widens to the VENUE chapters after the
// act: their scene plans and outlines get marked stale (ScenePlan=nil +
// review gate) so the next writing steps regenerate them from the corrected
// act context — no prose is rewritten there.
func ReconfirmActChapters(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, skills []Skill, actNo int, onlyOutline bool, progressPath string, logger *sse.LogBroadcaster) ([]int, error) {
	if state.PrePlanning == nil || actNo < 1 || actNo > len(state.PrePlanning.Acts) {
		return nil, fmt.Errorf("act_not_found")
	}
	start, end, ok := ActChapterRange(state, actNo)
	if !ok {
		return nil, fmt.Errorf("act_not_found")
	}
	loadDraftedChapterFilesInto(progressPath, state)

	// Chapters to re-verify: every drafted chapter inside the act range that
	// carries at least one APPLIED trope intervention coming from this act's
	// report (surgical repairs changed its prose). When no intervention was
	// applied, fall back to every non-accepted drafted chapter of the act so a
	// bare "reconfirm" still closes open quality loops. With onlyOutline we
	// skip the prose pass entirely.
	targets := []int{}
	if !onlyOutline {
		appliedCh := map[int]bool{}
		if rep := state.ActAnalyses[actNo]; rep != nil {
			for _, s := range rep.Suggestions {
				for _, iv := range s.Interventions {
					if iv.Status == "applied" {
						appliedCh[iv.ChapterNum] = true
					}
				}
			}
		}
		for i := range state.Chapters {
			ch := &state.Chapters[i]
			if ch.Num < start || ch.Num > end {
				continue
			}
			if len(appliedCh) > 0 {
				if !appliedCh[ch.Num] {
					continue
				}
			} else if ch.Status == StatusAccepted {
				continue
			}
			if strings.TrimSpace(ch.Content) == "" && ch.WordCount == 0 {
				continue
			}
			targets = append(targets, ch.Num)
		}
	}

	for _, num := range targets {
		if ctx.Err() != nil {
			SaveProgress(progressPath, state)
			return targets, ctx.Err()
		}
		idx := -1
		for i := range state.Chapters {
			if state.Chapters[i].Num == num {
				idx = i
				break
			}
		}
		if idx < 0 {
			continue
		}
		// An accepted chapter whose prose was surgically repaired must be
		// re-approved by the author: reopen it before the verification pass
		// (FactCheckChapterAction refuses accepted chapters).
		if state.Chapters[idx].Status == StatusAccepted {
			state.Chapters[idx].Status = StatusReview
		}
		err := FactCheckChapterAction(ctx, apiCfg, cfg, state, progressPath, settings, skills, num, logger)
		var wcErr *WritingConflictError
		if err != nil && !errors.As(err, &wcErr) {
			logger.WarnKey("log.act_reconfirm_chapter_failed", num, err)
			continue
		}
		// Refresh the per-chapter bestseller report against the new prose.
		if aerr := AnalyzeChapterPlan(ctx, apiCfg, cfg, state, settings, num, progressPath, logger); aerr != nil {
			logger.WarnKey("log.chapter_analyze_failed", aerr)
		}
		if serr := SaveProgress(progressPath, state); serr != nil {
			return targets, serr
		}
	}

	if onlyOutline {
		// Surgical outline changes for the VENUE chapters: invalidate their
		// beat sheets so the scene-plan gate forces a regeneration aligned
		// with the corrections the act critic demanded.
		widened := []int{}
		for i := range state.Chapters {
			ch := &state.Chapters[i]
			if ch.Num <= end || ch.Status == StatusAccepted {
				continue
			}
			if ch.ScenePlan != nil {
				ch.ScenePlan = nil
			}
			widened = append(widened, ch.Num)
		}
		if len(widened) > 0 {
			targets = widened
			clearScenePlanCacheOnEdit(state)
			if state.ScenePlanAnalysis != nil {
				state.ScenePlanAnalysis = nil
			}
			if serr := SaveProgress(progressPath, state); serr != nil {
				return targets, serr
			}
		}
		return targets, nil
	}

	// Re-sign the act report over the repaired prose (fingerprint-aware:
	// unchanged drafts reuse the cached report without an LLM call).
	if err := AnalyzeFinishedAct(ctx, apiCfg, cfg, state, settings, actNo, progressPath, logger); err != nil {
		logger.WarnKey("log.act_analyze_failed", err)
	}
	if serr := SaveProgress(progressPath, state); serr != nil {
		return targets, serr
	}
	return targets, nil
}

func ApplyActInterventions(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, actNo int, suggestionID string, progressPath string, logger *sse.LogBroadcaster) (int, error) {
	if state.ActAnalyses == nil || state.ActAnalyses[actNo] == nil {
		return 0, fmt.Errorf("no_act_analysis")
	}
	rep := state.ActAnalyses[actNo]
	sIdx := findDraftedSuggestion(rep.Suggestions, suggestionID)
	if sIdx == -1 {
		return 0, fmt.Errorf("suggestion_not_found")
	}
	applied, err := applyInterventionSet(ctx, apiCfg, cfg, state, settings, rep.Suggestions[sIdx].Interventions, progressPath, logger)
	if err != nil {
		return applied, err
	}
	if applied == 0 {
		return 0, fmt.Errorf("no_pending_interventions")
	}
	allDone := true
	for _, iv := range rep.Suggestions[sIdx].Interventions {
		if iv.Status == "pending" {
			allDone = false
		}
	}
	if allDone {
		rep.Suggestions[sIdx].Status = "applied"
		rep.Suggestions[sIdx].AppliedAt = time.Now().Format(time.RFC3339)
	}
	return applied, SaveProgress(progressPath, state)
}
