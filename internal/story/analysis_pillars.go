package story

import (
	"fmt"
	"math"
	"strings"
)

// ---------------------------------------------------------------------------
// Bestseller-editor critique: per-pillar improvement contracts
// ---------------------------------------------------------------------------
//
// The editorial passes (outline, scene plan, drafted chapter, drafted act)
// score four bestseller pillars 0-100. Authors reported that the feedback
// points did not reliably move the scores after corrections: the editor was
// free to grade each pillar on an implicit "overall vibe" with randomized
// anchors in the prompt, so applying every suggestion could still leave a
// pillar stuck mid-range, and re-grading felt arbitrary. These helpers turn
// the critique into a rubric-driven contract used by ALL passes:
//
//   - Every pillar is graded against a fixed checklist of concrete sub-criteria
//     (hook, pacing, emotion, tone, structure... depending on the pillar).
//   - The prompt embeds the REAL previous checklist levels whenever a report
//     exists, so the editor re-grades the same items against the material as
//     it stands now — corrections must visibly raise specific items instead of
//     the whole number drifting around random anchors.
//   - Each open suggestion MUST declare which pillar it serves and which
//     checklist items it fixes (pillar + criteria fields in the JSON schema),
//     so feedback reads as "improve THIS area of THAT pillar to reach 100".
//   - A pillar's score is DERIVED from its checklist — the model grades the
//     individual items, the code averages them — so closing every item drives
//     the pillar to 100 and each applied fix has a deterministic, visible
//     effect on the number.
//   - Suggestions whose declared impact contradicts the checklist are capped
//     at that pillar's remaining headroom, keeping the report coherent.

// analysisPillarKeys lists the four pillars every editorial pass grades, in
// UI display order.
var analysisPillarKeys = []string{"reader_profile", "inspirational_pieces", "tone", "story_structure"}

// PillarCriterion is one checkable sub-criterion of a pillar. Level holds the
// editor's 0-100 grade for it (-1 when the model did not grade this item).
type PillarCriterion struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Level int    `json:"level"` // 0 missing | 30 weak | 60 adequate | 85 strong | 100 bestseller-ready; -1 ungraded
}

// OpenCritiqueReport is the shape shared by every persisted editorial report
// (outline / scene plan / drafted chapter / drafted act), which lets the
// prompt builders show the PREVIOUS graded checklist to the editor so a
// re-analysis re-grades the same rubric against the corrected material.
type OpenCritiqueReport interface {
	Pillars() []PillarAssessment
	OpenSuggestions() int
}

func (r *OutlineAnalysisReport) Pillars() []PillarAssessment { return r.PillarScores }
func (r *OutlineAnalysisReport) OpenSuggestions() int        { return countOpenSuggestions(r.Suggestions) }

func (r *ScenePlanAnalysisReport) Pillars() []PillarAssessment { return r.PillarScores }
func (r *ScenePlanAnalysisReport) OpenSuggestions() int        { return countOpenSceneSuggestions(r.Suggestions) }

func (r *ChapterAnalysisReport) Pillars() []PillarAssessment { return r.PillarScores }
func (r *ChapterAnalysisReport) OpenSuggestions() int        { return countOpenDraftedSuggestions(r.Suggestions) }

func (r *ActAnalysisReport) Pillars() []PillarAssessment { return r.PillarScores }
func (r *ActAnalysisReport) OpenSuggestions() int        { return countOpenDraftedSuggestions(r.Suggestions) }

func countOpenSuggestions(s []OutlineSuggestion) int {
	n := 0
	for _, x := range s {
		if x.Status != "applied" {
			n++
		}
	}
	return n
}

func countOpenSceneSuggestions(s []SceneSuggestion) int {
	n := 0
	for _, x := range s {
		if x.Status != "applied" {
			n++
		}
	}
	return n
}

func countOpenDraftedSuggestions(s []DraftedSuggestion) int {
	n := 0
	for _, x := range s {
		if x.Status != "applied" {
			n++
		}
	}
	return n
}

// renderPreviousChecklistBlock formats the previous pass's graded checklist
// for embedding in the editorial prompt. Returns "" when there is no prior
// report. This is what makes iterative scoring traceable: the editor sees the
// exact per-item levels it gave last time and must justify every unchanged
// level after the author applied corrections.
func renderPreviousChecklistBlock(rep OpenCritiqueReport, scope string) string {
	if rep == nil {
		return ""
	}
	pillars := rep.Pillars()
	var b strings.Builder
	printed := false
	for _, key := range analysisPillarKeys {
		for i := range pillars {
			p := pillars[i]
			if p.Key != key || len(p.Checklist) == 0 {
				continue
			}
			if !printed {
				fmt.Fprintf(&b, "\nPREVIOUS EDITORIAL PASS ON %s — your own rubric grades, per sub-criterion. Re-grade EVERY item below against the CURRENT material: where the author applied corrections, the affected items MUST rise (and their suggestions close); never lower an item without stating why in its pillar note; keep items you cannot see changed in the material.\n", strings.ToUpper(scope))
				printed = true
			}
			names := make([]string, 0, len(p.Checklist))
			for _, c := range p.Checklist {
				if c.Level < 0 {
					names = append(names, c.Key+"=ungraded")
				} else {
					names = append(names, fmt.Sprintf("%s=%d", c.Key, c.Level))
				}
			}
			fmt.Fprintf(&b, "- %s (previous derived score %d/100): %s\n", key, p.Score, strings.Join(names, ", "))
		}
	}
	if !printed {
		return ""
	}
	if n := rep.OpenSuggestions(); n > 0 {
		fmt.Fprintf(&b, "The previous pass left %d open suggestion(s); if they were applied, reflect the improvement in the corresponding criteria levels.\n", n)
	}
	b.WriteString("\n")
	return b.String()
}

// SuggestionCriterion names one checklist item a suggestion repairs.
type SuggestionCriterion struct {
	Key  string `json:"key"`
	Name string `json:"name,omitempty"`
}

// PillarChecklist returns the fixed craft rubric for one pillar key. Unknown
// keys get an empty checklist (the pillar then keeps the model's raw score).
func PillarChecklist(key string) []PillarCriterion {
	switch key {
	case "reader_profile":
		return []PillarCriterion{
			{Key: "genre_promise", Name: "Genre promise kept"},
			{Key: "reader_hooks", Name: "Reader-hooks per arc"},
			{Key: "tropes_freshness", Name: "Tropes delivered without cliché"},
			{Key: "emotional_payoff", Name: "Emotional payoff for this reader"},
			{Key: "comp_distance", Name: "Distinct from comp titles"},
		}
	case "inspirational_pieces":
		return []PillarCriterion{
			{Key: "comp_scope", Name: "Scope matches comps"},
			{Key: "comp_hook_strength", Name: "Hook strength vs comps"},
			{Key: "comp_emotional_register", Name: "Emotional register of comps"},
			{Key: "comp_pacing_momentum", Name: "Momentum & payoff like comps"},
			{Key: "comp_originality", Name: "Original — no copying"},
		}
	case "tone":
		return []PillarCriterion{
			{Key: "tone_consistency", Name: "Consistent tonal register"},
			{Key: "tone_escalation", Name: "Tone escalates with stakes"},
			{Key: "tone_scene_coloring", Name: "Tone colors every beat/scene"},
			{Key: "tone_distinctiveness", Name: "Distinctive, marketable voice"},
			{Key: "tone_mood_contrast", Name: "Mood contrast & relief"},
		}
	case "story_structure":
		return []PillarCriterion{
			{Key: "hook_opening", Name: "Opening hook lands early"},
			{Key: "beat_coverage", Name: "Framework beats present & placed"},
			{Key: "pacing_curve", Name: "Pacing curve (no dead zones)"},
			{Key: "stakes_escalation", Name: "Stakes escalate"},
			{Key: "arc_payoffs", Name: "Arc setups pay off"},
			{Key: "chapter_end_hooks", Name: "Chapter-ending hooks"},
			{Key: "scene_economy", Name: "Every scene/beat earns its place"},
		}
	}
	return nil
}

// deriveScoreFromCriteria averages the levels the model graded for one
// pillar's checklist. Levels outside 0-100 are clamped; the result is
// rounded. An empty list yields -1 (no derivation possible).
func deriveScoreFromCriteria(levels []int) int {
	if len(levels) == 0 {
		return -1
	}
	sum := 0
	for _, lv := range levels {
		if lv < 0 {
			lv = 0
		}
		if lv > 100 {
			lv = 100
		}
		sum += lv
	}
	return int(math.Round(float64(sum) / float64(len(levels))))
}

// pillarDownstreamCategories maps each pillar to the suggestion categories
// that can serve it in any pass (superset across the outline / scene-plan /
// drafted vocabularies). Used only as a fallback when a suggestion declares
// no pillar.
var pillarDownstreamCategories = map[string][]string{
	"reader_profile":       {"market", "hook", "other"},
	"inspirational_pieces": {"other", "emotion"},
	"tone":                 {"tone", "emotion"},
	"story_structure":      {"structure", "pacing", "scene_economy", "stakes", "budget", "pov"},
}

// pillarForCategory infers the pillar a suggestion serves from its category,
// used when the model omits the explicit "pillar" field.
func pillarForCategory(category string) string {
	for _, key := range analysisPillarKeys {
		for _, c := range pillarDownstreamCategories[key] {
			if c == category {
				return key
			}
		}
	}
	return ""
}

// criterionNameSet returns the valid criterion keys of a pillar plus a
// lowercase display-name lookup so name-only answers still resolve.
func criterionNameSet(pillarKey string) (keys map[string]bool, byName map[string]string) {
	keys = map[string]bool{}
	byName = map[string]string{}
	for _, c := range PillarChecklist(pillarKey) {
		keys[c.Key] = true
		byName[strings.ToLower(c.Name)] = c.Key
	}
	return keys, byName
}

// normalizeCriterionToken lowercases and strips every non-alphanumeric rune,
// so "Reader-hooks per arc", "reader hooks per arc" and "ReaderHooksPerArc"
// all collapse to the same token for fuzzy matching.
func normalizeCriterionToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// resolveCriterionKey maps a model-supplied criterion key/name back onto the
// pillar's fixed checklist. Exact key → exact name → normalized-token match →
// unique prefix/substring match. Returns "" when nothing unambiguous matches,
// so callers never guess: an unresolved criterion stays dropped (evidence rule).
func resolveCriterionKey(pillarKey, rawKey, rawName string) string {
	keys, _ := criterionNameSet(pillarKey)
	k := strings.TrimSpace(rawKey)
	if keys[k] {
		return k
	}
	normKey := normalizeCriterionToken(rawKey)
	normName := normalizeCriterionToken(rawName)
	checklist := PillarChecklist(pillarKey)
	// Pass 1: full normalized equality on key or display name.
	for _, c := range checklist {
		ck := normalizeCriterionToken(c.Key)
		cn := normalizeCriterionToken(c.Name)
		if (normKey != "" && (ck == normKey || cn == normKey)) ||
			(normName != "" && (ck == normName || cn == normName)) {
			return c.Key
		}
	}
	// Pass 2: unique containment either way ("pacing" ⊂ "pacing_curve",
	// "genre promise kept" ⊃ "genre_promise"). Only accept if exactly one
	// checklist item matches — ambiguity must not be resolved by invention.
	match := ""
	count := 0
	for _, c := range checklist {
		ck := normalizeCriterionToken(c.Key)
		cn := normalizeCriterionToken(c.Name)
		hit := false
		for _, probe := range []string{normKey, normName} {
			if probe == "" {
				continue
			}
			if strings.Contains(ck, probe) || strings.Contains(probe, ck) ||
				strings.Contains(cn, probe) || strings.Contains(probe, cn) {
				hit = true
				break
			}
		}
		if hit {
			match = c.Key
			count++
		}
	}
	if count == 1 {
		return match
	}
	return ""
}

// normalizeSuggestionPillar fills in a missing/invalid declared pillar from
// the suggestion's category. Returns the sanitized pillar key (possibly "").
func normalizeSuggestionPillar(pillar, category string) string {
	p := strings.TrimSpace(pillar)
	for _, k := range analysisPillarKeys {
		if k == p {
			return p
		}
	}
	return pillarForCategory(strings.TrimSpace(category))
}

// sanitizeSuggestionCriteria drops criteria that don't belong to the given
// pillar's checklist; name-based answers are mapped back to their key.
func sanitizeSuggestionCriteria(pillarKey string, crits []SuggestionCriterion) []SuggestionCriterion {
	if pillarKey == "" || len(crits) == 0 {
		return nil
	}
	out := make([]SuggestionCriterion, 0, len(crits))
	seen := map[string]bool{}
	for _, c := range crits {
		k := resolveCriterionKey(pillarKey, c.Key, c.Name)
		if k == "" {
			continue
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		c.Key = k
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// SuggestionPriority is the 3-level importance label attached to every
// critique point so the author can triage: high = must fix before anything
// else, medium = clearly worth fixing, low = polish. Derived deterministically
// in finalizePillarReport from the declared score impact + how many checklist
// items the point repairs — never trusted straight from the model, which makes
// the badge consistent with the rubric numbers shown next to it.
const (
	suggestionPriorityHigh   = "high"
	suggestionPriorityMedium = "medium"
	suggestionPriorityLow    = "low"
)

// deriveSuggestionPriority maps the rubric contract of one point onto its
// 3-level importance badge. An applied point no longer needs attention and
// drops to low; an unattributed point with no measurable impact is polish.
func deriveSuggestionPriority(status string, impact int, criteriaCount int) string {
	if status == "applied" {
		return suggestionPriorityLow
	}
	switch {
	case impact >= 15 || (impact >= 8 && criteriaCount >= 2):
		return suggestionPriorityHigh
	case impact >= 4 || criteriaCount >= 1:
		return suggestionPriorityMedium
	default:
		return suggestionPriorityLow
	}
}

// FinalizableSuggestion is the pillar-contract view of a suggestion, shared
// by the three report kinds (outline / scene plan / drafted) without making
// the concrete suggestion types depend on each other. Callers fill Category,
// Status, Pillar, Impact and Criteria from the model answer and read the
// sanitized values (including the derived Priority) back from the returned
// slice (same order/length).
type FinalizableSuggestion struct {
	Category string
	Status   string // pending | applied | partially_applied
	Pillar   string
	Impact   int
	Criteria []SuggestionCriterion
	Priority string // high | medium | low (derived from Impact + Criteria)
}

// finalizePillarReport applies the rubric contract to one editorial pass:
//
//  1. Keep only the four known pillars, deduped, scores clamped to 0-100.
//  2. Attach the fixed checklist to every pillar and fill each item's level
//     from the model's criteria_levels answer.
//  3. When the model graded criteria for a pillar, DERIVE the pillar score
//     from them (average) — the score becomes a function of the checklist,
//     so closing items provably raises it toward 100.
//  4. Normalize every suggestion's declared pillar/criteria, then cap the
//     claimed score_impact at the pillar's remaining headroom (100 minus its
//     current derived score): no point may promise more than the material
//     actually lacks. Applied suggestions keep their claim but are exempt
//     from capping (they already moved the graded material).
//  5. Missing pillars default to the strongest-not-perfect score so the UI
//     never shows "No changes needed" without proof.
func finalizePillarReport(in []PillarAssessment, suggestions []FinalizableSuggestion) ([]PillarAssessment, []FinalizableSuggestion) {
	type pillarGrades struct {
		hasScore bool
		score    int
		note     string
		levels   map[string]int
	}
	graded := map[string]*pillarGrades{}
	for _, p := range in {
		k := strings.TrimSpace(p.Key)
		valid := false
		for _, vk := range analysisPillarKeys {
			if k == vk {
				valid = true
				break
			}
		}
		if !valid || graded[k] != nil {
			continue
		}
		g := &pillarGrades{levels: map[string]int{}, note: strings.TrimSpace(p.Note), hasScore: true}
		g.score = p.Score
		if g.score < 0 || g.score > 100 {
			g.score = 0
		}
		for _, cl := range p.CriteriaLevels {
			key := strings.TrimSpace(cl.Key)
			if key == "" {
				continue
			}
			lv := cl.Level
			if lv < 0 {
				lv = 0
			}
			if lv > 100 {
				lv = 100
			}
			// The model sometimes answers with the display name ("Pacing curve
			// (no dead zones)") or a paraphrase instead of the checklist key.
			// Resolve it back onto the fixed rubric; genuinely unknown items
			// stay dropped — never invented, never averaged in from nothing.
			resolved := resolveCriterionKey(k, key, "")
			if resolved == "" {
				continue
			}
			if _, dup := g.levels[resolved]; !dup {
				g.levels[resolved] = lv
			}
		}
		graded[k] = g
	}

	out := make([]PillarAssessment, 0, 4)
	for _, key := range analysisPillarKeys {
		g := graded[key]
		pa := PillarAssessment{Key: key}
		checklist := PillarChecklist(key)
		var levels []int
		for i, c := range checklist {
			lv := -1
			if g != nil {
				if v, ok := g.levels[c.Key]; ok {
					lv = v
					levels = append(levels, v)
				}
			}
			checklist[i].Level = lv
		}
		pa.Checklist = checklist
		switch {
		case g == nil:
			pa.Score = strongPillarScore - 1 // ungraded pillar: never claim "strong"
		case len(levels) > 0:
			pa.Score = deriveScoreFromCriteria(levels) // rubric-derived score
			pa.Note = g.note
		default:
			pa.Score = g.score
			pa.Note = g.note
		}
		out = append(out, pa)
	}

	scoreOf := map[string]int{}
	for _, p := range out {
		scoreOf[p.Key] = p.Score
	}

	sanitized := make([]FinalizableSuggestion, len(suggestions))
	for i, s := range suggestions {
		s.Pillar = normalizeSuggestionPillar(s.Pillar, s.Category)
		s.Criteria = sanitizeSuggestionCriteria(s.Pillar, s.Criteria)
		if s.Pillar != "" && s.Impact > 0 && s.Status != "applied" {
			if cap := 100 - scoreOf[s.Pillar]; s.Impact > cap {
				s.Impact = cap
				if s.Impact < 0 {
					s.Impact = 0
				}
			}
		} else if s.Status != "applied" {
			s.Impact = 0
		}
		// Deterministic 3-level importance badge derived from the (capped)
		// impact + how many checklist items the point repairs, so it always
		// agrees with the rubric numbers shown next to it in the UI.
		s.Priority = deriveSuggestionPriority(s.Status, s.Impact, len(s.Criteria))
		sanitized[i] = s
	}
	return out, sanitized
}

// pillarContractPromptBlockEN renders the grading-rubric instructions added
// to every editorial prompt: suggestions must declare their pillar + the
// checklist items they fix, and pillar scores come from the criteria levels,
// so the feedback reads as "improve THIS area of THAT pillar to reach 100".
func pillarContractPromptBlockEN(structureName string) string {
	return fmt.Sprintf(`RUBRIC CONTRACT (this makes your feedback actionable toward 100%% on every pillar):
Each pillar is graded against a FIXED checklist of sub-criteria. Grade every item 0-100 (0 missing | 30 weak | 60 adequate | 85 strong | 100 bestseller-ready); the pillar score is DERIVED as the average of its items — so telling the author exactly WHICH area of WHICH pillar must improve is the whole point of this pass.
CHECKLISTS:
- reader_profile: genre_promise, reader_hooks, tropes_freshness, emotional_payoff, comp_distance
- inspirational_pieces: comp_scope, comp_hook_strength, comp_emotional_register, comp_pacing_momentum, comp_originality
- tone: tone_consistency, tone_escalation, tone_scene_coloring, tone_distinctiveness, tone_mood_contrast
- story_structure (%[1]s): hook_opening, beat_coverage, pacing_curve, stakes_escalation, arc_payoffs, chapter_end_hooks, scene_economy
Return "criteria_levels" inside EVERY pillar_scores entry as an array of objects: {"key":"<checklist key verbatim>","level":<0-100>} — one object for EVERY item of that pillar, keys copied exactly from the lists above (never the display name, never a paraphrase). If you genuinely cannot grade an item from the material shown, still include it with your best evidence-based level; do not omit items.
Every suggestion MUST also declare: "pillar" (one of reader_profile|inspirational_pieces|tone|story_structure), "criteria" (the checklist keys it repairs) and "score_impact" (points you expect that pillar gains once this point is fully applied — realistic, consistent with the levels you graded: applying ALL points of a pillar must bring it to 100). Never invent criteria outside the lists above.
The UI shows every checklist item ("mini category") with its exact level next to the pillar score, and badges each suggestion with a High/Medium/Low importance derived from its declared impact and the number of items it repairs — so grade levels honestly and reserve the biggest score_impact for the points that truly gate bestseller level.`, structureName)
}

// renderSuggestionContractBlock formats one critique point's rubric contract
// for embedding in on-demand proposal prompts ("Propose changes"): the exact
// pillar, checklist items and expected impact the editor itself declared when
// grading, so drafted replacements aim at CLOSING those specific sub-criteria
// instead of generically obeying the advice. Returns "" when the point
// carries no contract (older reports finalized before the rubric existed).
func renderSuggestionContractBlock(pillar string, crits []SuggestionCriterion, impact int) string {
	pillar = strings.TrimSpace(pillar)
	if pillar == "" && len(crits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nRUBRIC CONTRACT OF THIS POINT (draft replacements that close EXACTLY these gaps; re-grading after applying them must raise these checklist items):\n")
	if pillar != "" {
		fmt.Fprintf(&b, "- Pillar: %s\n", pillar)
	}
	if len(crits) > 0 {
		names := make([]string, 0, len(crits))
		for _, c := range crits {
			n := strings.TrimSpace(c.Name)
			if n == "" {
				n = c.Key
			}
			names = append(names, fmt.Sprintf("%s (%s)", c.Key, n))
		}
		fmt.Fprintf(&b, "- Checklist items to repair: %s\n", strings.Join(names, ", "))
	}
	if impact > 0 {
		fmt.Fprintf(&b, "- Expected pillar gain once fully applied: +%d points toward 100.\n", impact)
	}
	return b.String()
}
