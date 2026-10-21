package story

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"showmethestory/internal/config"
	"showmethestory/internal/llm"
	"showmethestory/internal/sse"
)

// Scene-plan bestseller critique: the sibling of the outline editorial pass
// (outline_analysis.go), aimed at the NEXT planning artifact — the per-chapter
// beat sheets generated before writing. The editor knows it is reviewing a
// SCENE PLAN (scenes not yet written): it judges scene economy, beat coverage
// of the chapter outline, POV distribution, word budgets and the closing hook,
// and NEVER critiques prose. Like the outline pass it is advice-only until the
// author clicks "Propose scene plan changes", which drafts concrete per-scene
// replacements kept in collapsed accordions until applied.

// ScenePlanRecommendation is one concrete replacement for a single scene of a
// chapter's beat sheet. Applying it swaps title/description/POV/word budget on
// that scene (chapters already accepted are never touched). Proposed marks a
// replacement drafted on demand by "Propose scene plan changes"; the frontend
// only surfaces proposed replacements — inside collapsed accordions — after
// that explicit action, so no plan silently rewrites itself.
type ScenePlanRecommendation struct {
	ChapterNum    int    `json:"chapter_num"`
	SceneID       int    `json:"scene_id"`
	Present       bool   `json:"present"`
	NewTitle      string `json:"new_title,omitempty"`
	NewDescription string `json:"new_description,omitempty"`
	NewPoV        string `json:"new_pov,omitempty"`
	NewWordsBudget int   `json:"new_words_budget,omitempty"`
	Proposed      bool   `json:"proposed,omitempty"`
}

// SceneSuggestion is one point of the editor's feedback on the scene plans:
// what works / what must change and, once proposed, exactly which scenes to
// replace. Categories mirror the craft axes a beat sheet can be judged on.
type SceneSuggestion struct {
	ID              string                    `json:"id"`
	Category        string                    `json:"category"` // hook|pacing|scene_economy|stakes|structure|emotion|pov|budget|other
	Title           string                    `json:"title"`
	Detail          string                    `json:"detail"`
	Action          string                    `json:"action"`
	Status          string                    `json:"status"` // pending | applied
	AppliedAt       string                    `json:"applied_at,omitempty"`
	AppliesToChapters []int                   `json:"applies_to_chapters,omitempty"`
	Recommendations []ScenePlanRecommendation `json:"recommendations,omitempty"`
}

// ScenePlanAnalysisReport is the full editorial pass over the novel's pending
// scene plans, persisted on Progress so the Writing page can render it point
// by point with propose/apply actions per suggestion.
type ScenePlanAnalysisReport struct {
	GeneratedAt      string            `json:"generated_at"`
	BasedOnStructure string            `json:"based_on_structure,omitempty"`
	Summary          string            `json:"summary"`
	OverallScore     int               `json:"overall_score,omitempty"`
	PillarScores     []PillarAssessment `json:"pillar_scores,omitempty"`
	Suggestions      []SceneSuggestion `json:"suggestions"`
	// ScenePlanFingerprint identifies the exact set of pending beat sheets
	// (chapter numbers + scene titles/descriptions/budgets) the scores above
	// were computed from. Re-analyzing UNCHANGED plans reuses the previous
	// report without paying for a new LLM call; any edit (or applying a
	// suggestion) changes the hash and forces a genuine re-grade.
	ScenePlanFingerprint string `json:"scene_plan_fingerprint,omitempty"`
}

// scenePlanFingerprint hashes every non-accepted chapter that HAS a scene
// plan: chapter number plus each scene's title/description/POV/budget.
// Accepted chapters are excluded on purpose — they are locked and out of
// scope, so drafting or accepting prose must not invalidate cached scores.
func scenePlanFingerprint(state *Progress) string {
	var b strings.Builder
	for _, ch := range state.Chapters {
		if ch.Status == StatusAccepted || ch.ScenePlan == nil || len(ch.ScenePlan.Scenes) == 0 {
			continue
		}
		fmt.Fprintf(&b, "%d\x01%d\x01", ch.Num, ch.ScenePlan.TargetWords)
		for _, sc := range ch.ScenePlan.Scenes {
			fmt.Fprintf(&b, "%d\x00%s\x00%s\x00%s\x00%d\x01", sc.ID,
				strings.TrimSpace(sc.Title), strings.TrimSpace(sc.Description),
				strings.TrimSpace(sc.PoV), sc.WordsBudget)
		}
	}
	b.WriteString(gradeSalt()) // force a fresh LLM pass on every explicit re-analysis
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// buildScenePlanEditorPrompt assembles the single LLM prompt for the
// bestseller-editor pass over the pending beat sheets. It explicitly frames
// the artifact under review as a SCENE PLAN so the model never slips into
// prose criticism.
func buildScenePlanEditorPrompt(cfg *config.Config, state *Progress, settings *ProjectSettings, lang string) string {
	sc := cfg.Story
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

	var planBuf strings.Builder
	planned := 0
	for _, ch := range state.Chapters {
		if ch.Status == StatusAccepted || ch.ScenePlan == nil || len(ch.ScenePlan.Scenes) == 0 {
			continue
		}
		planned++
		actNote := ""
		if actNo, actName, ok := actForChapter(state, ch.Num); ok {
			actNote = fmt.Sprintf(" (act %d: %s)", actNo, actName)
		}
		fmt.Fprintf(&planBuf, "\n=== Chapter #%d \"%s\"%s ===\n", ch.Num, ch.Title, actNote)
		fmt.Fprintf(&planBuf, "[Chapter outline — the contract the plan must fulfil]\n%s\n", strings.TrimSpace(ch.Outline))
		fmt.Fprintf(&planBuf, "[Scene plan — target %d words]\n", ch.ScenePlan.TargetWords)
		for _, s := range ch.ScenePlan.Scenes {
			pov := strings.TrimSpace(s.PoV)
			if pov == "" {
				pov = "-"
			}
			fmt.Fprintf(&planBuf, "  scene id=%d | %s | %s | pov: %s | ~%d words\n",
				s.ID, s.Title, strings.TrimSpace(s.Description), pov, s.WordsBudget)
		}
	}

	structureName := structureLabel(sc.Structure, lang)

	return fmt.Sprintf(`You are %s, a world-famous literary editor whose lists have produced multiple global bestsellers. Your job: give the author a constructive, point-by-point editorial pass over the SCENE PLANS below, with ONE clear goal: turn this book plan into a global bestseller — every point you make must serve that bar, not mere competence.

CRITICAL CONTEXT — WHAT YOU ARE REVIEWING:
- You are reviewing SCENE PLANS (beat sheets): ordered per-chapter scene breakdowns generated AFTER the outline and BEFORE the prose is written. No actual scenes exist yet — only planned beats with POV and word budgets.
- Judge them as beat sheets: scene economy (does every scene earn its place?), whether the plan COVERS the chapter outline completely without inventing off-outline events, POV distribution across scenes, per-scene word budgets vs the chapter target (overloaded or starved scenes), escalation within the chapter, and whether the LAST scene lands the hook the outline implies. Never critique prose quality, dialogue lines, sentences or voice — those cannot be assessed from a plan. Phrase every point in terms of what the PLAN shows or lacks (e.g. "scene 3 of chapter 5 duplicates scene 2's beat...", not "the writing will feel...").
- This pass is ADVICE ONLY: do NOT draft replacement scenes here. The author requests concrete per-scene rewrites later, explicitly, via a "Propose scene plan changes" action — so nothing gets rewritten before they choose to act on a specific point.

NOVEL PARAMETERS (the author's contract with the reader):
%s
STORY STRUCTURE FRAMEWORK IN USE: %s

SCENE PLANS UNDER REVIEW (%d chapter plan(s) shown; accepted chapters are locked and already drafted — they are out of scope):
%s

YOUR MISSION: reverse-engineer what makes books in this genre hit #1 (an irresistible hook, relentless pacing, arcs that pay off, stakes that escalate) and grade THESE PLANS against that blueprint. Every suggestion must be a concrete step that measurably moves the beat sheets toward bestseller level on its pillar.

Analyze the plans against ALL parameters above, giving special weight to these four pillars:
1. Ideal reader profile — does every planned scene deliver what this reader came for? Genre expectations met without cliché?
2. Inspirational pieces — do the plans live up to the comp titles' standard of scene craft (scope, hooks, emotional register) without copying them?
3. Tone — is the intended tone baked into the scene descriptions and escalating properly? (Judge the tonal plan, not prose.)
4. Story structure (%s) — do the chapter plans advance the framework's beats in the right proportion? Dead-zone scenes, redundant beats, rushed payoffs in the plan?

Also weigh: outline coverage fidelity, scene count and budget realism, POV clarity, chapter-ending hooks.

Return STRICT JSON only (no markdown fences, no prose):
{"summary":"2-4 paragraph editorial letter to the author, warm but honest, in %s","overall_score":"%d","pillar_scores":[{"key":"reader_profile","score":%d,"note":"one-sentence verdict in %s"},{"key":"inspirational_pieces","score":%d,"note":"..."},{"key":"tone","score":%d,"note":"..."},{"key":"story_structure","score":%d,"note":"..."}],"suggestions":[{"category":"hook|pacing|scene_economy|stakes|structure|emotion|pov|budget|other","title":"short imperative headline","detail":"what the plans currently do and why they fall short of bestseller level","action":"concrete guidance on which scenes to change and how","applies_to_chapters":[3,4]}]}

Grading standard (you are ruthless — your reputation rests on books that hit #1 worldwide):
- Score each of the four pillars 0-100 against BESTSELLER level, not "competent". 90+ means the PLANS are genuinely strong as-is. Reserve 90+ for truly exceptional planning; most plans land mid-range. The numeric examples in the JSON schema above are PLACEHOLDERS, not your answer — derive every score independently from THESE specific plans, and re-grading after edits must produce different numbers wherever the material changed. Redundant scenes, vague beats, unrealistic budgets or weak chapter-ending hooks cap scores below 85.
- Never penalize or praise things a scene plan cannot show (prose rhythm, dialogue quality). If a weakness could only be judged from the manuscript, it is out of scope.
- pillar_scores MUST contain exactly the four keys reader_profile, inspirational_pieces, tone, story_structure.
- Every pillar scoring below 90 MUST have at least one corresponding suggestion (reader_profile→market/other, inspirational_pieces→other, tone→emotion/tone-like, story_structure→structure/pacing/scene_economy). A pillar scoring 90+ may have zero suggestions — then the UI shows "No changes needed" for it.

Rules:
- 4 to 10 suggestions, ordered most impactful first.
- DO NOT include replacement scenes in this pass: return every suggestion WITHOUT a "recommendations" array (replacements are drafted later, ONLY after the author clicks "Propose scene plan changes" on a specific point).
- applies_to_chapters may name the planned (non-accepted) chapters most affected by each point.
- action must describe WHAT to change and HOW at the beat-sheet level (merge/split/reorder scenes, re-budget words, sharpen a beat, fix POV), so the author understands the advice before requesting concrete replacements.
- Write summary, titles, details and actions in the SAME language as the plans above.
- If the plans are genuinely strong, say so and return fewer, higher-value suggestions.`,
		editorPersona(lang), meta.String(), structureName, planned, planBuf.String(),
		structureName, rand.Intn(21)+60, gradeAnchorFor("reader_profile"), lang, gradeAnchorFor("inspirational_pieces"), gradeAnchorFor("tone"), gradeAnchorFor("story_structure"), lang)
}

// AnalyzePendingScenePlans runs the bestseller-editor pass over all pending
// (non-accepted) chapter scene plans in ONE LLM call and stores the report on
// state. Callers persist progress. Scoring always recomputes against the
// CURRENT plans: identical fingerprint → cached report, no LLM call.
func AnalyzePendingScenePlans(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, logger *sse.LogBroadcaster) error {
	fingerprint := scenePlanFingerprint(state)
	if fingerprint == "" {
		return fmt.Errorf("no_scene_plans")
	}
	if prev := state.ScenePlanAnalysis; prev != nil && prev.ScenePlanFingerprint != "" && prev.ScenePlanFingerprint == fingerprint {
		logger.InfoKey("log.sceneplan_analyze_unchanged")
		return nil
	}
	lang := cfg.Language
	settingsForPrompt := settings
	if settings != nil {
		if selected, _ := retrieveSettings(settings, collectAllOutlines(state), settingsContextRunes); selected != nil {
			settingsForPrompt = selected
		}
	}
	prompt := buildScenePlanEditorPrompt(cfg, state, settingsForPrompt, lang)
	system := "You are a world-famous literary editor specializing in turning novels into global bestsellers. You are reviewing SCENE PLANS (per-chapter beat sheets with word budgets), NOT drafted prose — never critique sentences, dialogue or writing style. Be constructive, specific and honest. Output strict JSON exactly as requested — no extra prose, no markdown code fences."

	logger.InfoKey("log.sceneplan_analyze_running")
	var result ScenePlanAnalysisReport
	for attempt := 0; attempt < 2; attempt++ {
		raw := llm.CallAPIWithRetryLog(ctx, apiCfg, system, prompt, logger)
		if raw == "" {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("API call failed or was canceled")
		}
		j := llm.ExtractJSON(raw)
		result = ScenePlanAnalysisReport{}
		if j != "" && json.Unmarshal([]byte(j), &result) == nil && (result.Summary != "" || len(result.Suggestions) > 0) {
			break
		}
		if attempt == 1 {
			return fmt.Errorf("model response is not valid scene-plan-analysis JSON")
		}
		prompt += "\n\nYour previous response was invalid. Return one complete JSON object only, using the required schema."
	}

	// Sanitize: the initial pass is advice-only; bundled recommendations are
	// discarded so no plan can silently suggest rewrites before an explicit
	// "Propose scene plan changes". Chapters named in applies_to_chapters are
	// validated against the planned (non-accepted, with-plan) set.
	plannedSet := map[int]bool{}
	for _, ch := range state.Chapters {
		if ch.Status != StatusAccepted && ch.ScenePlan != nil && len(ch.ScenePlan.Scenes) > 0 {
			plannedSet[ch.Num] = true
		}
	}
	validCategory := map[string]bool{"hook": true, "pacing": true, "scene_economy": true, "stakes": true, "structure": true, "emotion": true, "pov": true, "budget": true, "other": true}
	kept := make([]SceneSuggestion, 0, len(result.Suggestions))
	for i, s := range result.Suggestions {
		s.Title = strings.TrimSpace(s.Title)
		s.Detail = strings.TrimSpace(s.Detail)
		s.Action = strings.TrimSpace(s.Action)
		if !validCategory[s.Category] {
			s.Category = "other"
		}
		if s.Title == "" && s.Detail == "" {
			continue
		}
		s.Recommendations = nil
		cleanApps := make([]int, 0, len(s.AppliesToChapters))
		seenApp := map[int]bool{}
		for _, n := range s.AppliesToChapters {
			if plannedSet[n] && !seenApp[n] {
				seenApp[n] = true
				cleanApps = append(cleanApps, n)
			}
		}
		sort.Ints(cleanApps)
		s.AppliesToChapters = cleanApps
		s.ID = fmt.Sprintf("spa_%d_%d", time.Now().Unix(), i)
		if s.Status != "applied" {
			s.Status = "pending"
		}
		kept = append(kept, s)
	}
	if result.OverallScore < 0 || result.OverallScore > 100 {
		result.OverallScore = 0
	}
	validPillar := map[string]bool{"reader_profile": true, "inspirational_pieces": true, "tone": true, "story_structure": true}
	pillarDownstream := map[string][]string{
		"reader_profile":       {"market", "hook"},
		"inspirational_pieces": {"other", "emotion"},
		"tone":                 {"emotion", "hook"},
		"story_structure":      {"structure", "pacing", "scene_economy", "budget", "pov", "stakes"},
	}
	cleanPillars := make([]PillarAssessment, 0, 4)
	seenPillar := map[string]bool{}
	for _, p := range result.PillarScores {
		if !validPillar[p.Key] || seenPillar[p.Key] {
			continue
		}
		seenPillar[p.Key] = true
		if p.Score < 0 || p.Score > 100 {
			p.Score = 0
		}
		p.Note = strings.TrimSpace(p.Note)
		if p.IsStrong() && len(keptSceneSuggestionsForPillar(kept, pillarDownstream[p.Key])) > 0 {
			p.Score = strongPillarScore - 1
		}
		cleanPillars = append(cleanPillars, p)
	}
	for _, key := range []string{"reader_profile", "inspirational_pieces", "tone", "story_structure"} {
		if !seenPillar[key] {
			cleanPillars = append(cleanPillars, PillarAssessment{Key: key, Score: strongPillarScore - 1})
		}
	}
	result.PillarScores = cleanPillars
	result.Summary = strings.TrimSpace(result.Summary)
	result.Suggestions = kept
	result.GeneratedAt = time.Now().Format(time.RFC3339)
	result.BasedOnStructure = structureLabel(cfg.Story.Structure, lang)
	result.ScenePlanFingerprint = fingerprint
	state.ScenePlanAnalysis = &result
	return nil
}

// keptSceneSuggestionsForPillar mirrors keptSuggestionsForPillar for scene
// suggestions: a pillar cannot claim "no changes needed" while an open
// suggestion still targets its categories.
func keptSceneSuggestionsForPillar(suggestions []SceneSuggestion, categories []string) []SceneSuggestion {
	in := make(map[string]bool, len(categories))
	for _, c := range categories {
		in[c] = true
	}
	var out []SceneSuggestion
	for _, s := range suggestions {
		if in[s.Category] && s.Status != "applied" {
			out = append(out, s)
		}
	}
	return out
}

// clearScenePlanCacheOnEdit drops the stored scene-plan analysis when the
// underlying plans change through the editing API, so stale proposals can
// never be applied to scenes that no longer exist.
func clearScenePlanCacheOnEdit(state *Progress) {
	if state != nil && state.ScenePlanAnalysis != nil {
		state.ScenePlanAnalysis = nil
	}
}

// findSceneSuggestion locates a suggestion by ID in the stored report.
func findSceneSuggestion(rep *ScenePlanAnalysisReport, id string) int {
	if rep == nil {
		return -1
	}
	for i, s := range rep.Suggestions {
		if s.ID == id {
			return i
		}
	}
	return -1
}

// ProposeScenePlanReplacements turns one advice-only scene-plan point into
// concrete ready-to-apply per-scene replacements, drafted with the same
// bestseller bar as the outline pass: the goal is not merely to obey the
// advice but to push these scenes toward #1-level craft on the pillar the
// point belongs to, while staying consistent with the chapter outline, the
// neighbouring scenes and the total word target. Proposals ACCUMULATE across
// calls (new ones win per scene) and are marked Proposed:true so the UI only
// reveals them inside collapsed accordions after this explicit action.
func ProposeScenePlanReplacements(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, suggestionID string, progressPath string, logger *sse.LogBroadcaster) (*SceneSuggestion, error) {
	rep := state.ScenePlanAnalysis
	if rep == nil {
		return nil, fmt.Errorf("no_scene_plan_analysis")
	}
	idx := findSceneSuggestion(rep, suggestionID)
	if idx == -1 {
		return nil, fmt.Errorf("suggestion_not_found")
	}
	s := rep.Suggestions[idx]
	if s.Status == "applied" {
		return nil, fmt.Errorf("suggestion_already_applied")
	}

	var planBuf strings.Builder
	refs := map[[2]int]bool{}
	plannedChapters := map[int]bool{}
	for _, ch := range state.Chapters {
		if ch.Status == StatusAccepted || ch.ScenePlan == nil || len(ch.ScenePlan.Scenes) == 0 {
			continue
		}
		plannedChapters[ch.Num] = true
		fmt.Fprintf(&planBuf, "\n=== Chapter #%d \"%s\" (target %d words) ===\nOutline: %s\n",
			ch.Num, ch.Title, ch.ScenePlan.TargetWords, strings.TrimSpace(ch.Outline))
		for _, sc := range ch.ScenePlan.Scenes {
			fmt.Fprintf(&planBuf, "  scene id=%d | %s | %s | pov: %s | ~%d words\n",
				sc.ID, sc.Title, strings.TrimSpace(sc.Description), sc.PoV, sc.WordsBudget)
			refs[[2]int{ch.Num, sc.ID}] = true
		}
	}
	if len(plannedChapters) == 0 {
		return nil, fmt.Errorf("no_scene_plans")
	}

	sc := cfg.Story
	prompt := fmt.Sprintf(`You are %s. You previously gave the author this editorial point about their novel's SCENE PLANS (per-chapter beat sheets — the prose is NOT written yet, so never critique sentences, dialogue or writing style):

POINT: %s
WHAT THE PLANS DO NOW: %s
YOUR ADVICE: %s

NOVEL PARAMETERS: Tone: %s | Theme: %s | Structure framework: %s | Ideal reader: %s | Inspirational pieces: %s

SCENE PLANS UNDER REVIEW:
%s

This point currently has NO concrete replacement proposals. Every replacement you draft must measurably push these scenes toward GLOBAL BESTSELLER level on the pillar this point belongs to (tighter scene economy, sharper beats, stronger chapter-ending hooks, realistic budgets...) while staying consistent with the chapter outlines, neighbouring scenes and each chapter's total word target. Produce the changes at the BEAT-SHEET level:
1. Decide which 1-4 scenes (across at most 2 chapters from the plans above) would benefit most from applying this point. Only scenes shown above exist — never invent chapter numbers or scene ids.
2. For EACH chosen scene, write a COMPLETE replacement: a sharper scene title, the FULL new scene description (2-4 sentences: what happens, who is present, where, the emotional turn — same language as the plans, consistent with the chapter outline), the scene POV (a registered/main character when sensible) and a realistic words_budget. Keep each chapter's TOTAL budget close to its target (±10%%): if you raise one scene's budget, lower another's or say so in the description.
3. Do NOT add or remove scenes — reshape existing ones only.

Return STRICT JSON only (no markdown fences): {"recommendations":[{"chapter_num":3,"scene_id":2,"present":true,"new_title":"...","new_description":"...","new_pov":"...","new_words_budget":900}]}
Only include chapters/scenes that exist in the plans above.`,
		editorPersona(cfg.Language), s.Title, s.Detail, s.Action,
		sc.Tone, sc.Theme, structureLabel(sc.Structure, cfg.Language), sc.AudienceProfile, sc.InspirationalPieces,
		planBuf.String())
	system := "You are a world-famous literary editor turning feedback into concrete SCENE-PLAN (beat sheet) rewrites — never manuscript prose. You rewrite planned scenes with one clear goal: make the book a global bestseller on the pillar under discussion, not just fix the complaint. Output strict JSON exactly as requested — no extra prose, no markdown code fences."

	logger.InfoKey("log.sceneplan_analyze_proposing")
	type proposal struct {
		Recommendations []ScenePlanRecommendation `json:"recommendations"`
	}
	var recs []ScenePlanRecommendation
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
		if j != "" && json.Unmarshal([]byte(j), &p) == nil && len(p.Recommendations) > 0 {
			for _, r := range p.Recommendations {
				r.NewTitle = strings.TrimSpace(r.NewTitle)
				r.NewDescription = strings.TrimSpace(r.NewDescription)
				r.NewPoV = strings.TrimSpace(r.NewPoV)
				if !r.Present || !plannedChapters[r.ChapterNum] || !refs[[2]int{r.ChapterNum, r.SceneID}] {
					continue
				}
				if r.NewTitle == "" && r.NewDescription == "" && r.NewWordsBudget <= 0 {
					continue
				}
				if r.NewWordsBudget < 0 {
					r.NewWordsBudget = 0
				}
				r.Proposed = true
				recs = append(recs, r)
			}
			break
		}
		if attempt == 1 {
			return nil, fmt.Errorf("model response is not valid proposal JSON")
		}
		prompt += "\n\nYour previous response was invalid. Return one complete JSON object only, using the required schema."
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("no_valid_proposals")
	}
	// Accumulate instead of overwriting: repeated proposals stack on top of
	// earlier ones; newer recommendations win for the same (chapter, scene).
	merged := make([]ScenePlanRecommendation, 0, len(recs)+len(rep.Suggestions[idx].Recommendations))
	chosen := map[[2]int]bool{}
	for _, r := range recs {
		chosen[[2]int{r.ChapterNum, r.SceneID}] = true
	}
	merged = append(merged, recs...)
	for _, old := range rep.Suggestions[idx].Recommendations {
		if !chosen[[2]int{old.ChapterNum, old.SceneID}] && old.Proposed {
			merged = append(merged, old)
		}
	}
	rep.Suggestions[idx].Recommendations = merged
	seenCh := map[int]bool{}
	for _, r := range merged {
		if !seenCh[r.ChapterNum] {
			seenCh[r.ChapterNum] = true
			rep.Suggestions[idx].AppliesToChapters = append(rep.Suggestions[idx].AppliesToChapters, r.ChapterNum)
		}
	}
	sort.Slice(rep.Suggestions[idx].AppliesToChapters, func(i, j int) bool {
		return rep.Suggestions[idx].AppliesToChapters[i] < rep.Suggestions[idx].AppliesToChapters[j]
	})
	out := rep.Suggestions[idx]
	if err := SaveProgress(progressPath, state); err != nil {
		return &out, nil // proposals are in memory; persistence retried on next save
	}
	return &out, nil
}

// ApplyScenePlanSuggestion applies every replacement recommendation of one
// scene-plan analysis point to the live beat sheets (only non-accepted
// chapters) and marks the suggestion applied. Editing a plan un-approves it
// server-side so the author re-reviews before writing. Returns the number of
// scenes changed.
func ApplyScenePlanSuggestion(state *Progress, suggestionID string, progressPath string) (int, error) {
	rep := state.ScenePlanAnalysis
	if rep == nil {
		return 0, fmt.Errorf("no_scene_plan_analysis")
	}
	idx := findSceneSuggestion(rep, suggestionID)
	if idx == -1 {
		return 0, fmt.Errorf("suggestion_not_found")
	}
	s := rep.Suggestions[idx]
	if s.Status == "applied" {
		return 0, fmt.Errorf("suggestion_already_applied")
	}
	changed := 0
	applied := map[[2]int]bool{}
	for _, r := range s.Recommendations {
		if !r.Present || applied[[2]int{r.ChapterNum, r.SceneID}] {
			continue
		}
		for ci := range state.Chapters {
			ch := &state.Chapters[ci]
			if ch.Num != r.ChapterNum || ch.Status == StatusAccepted || ch.ScenePlan == nil {
				continue
			}
			for si := range ch.ScenePlan.Scenes {
				sc := &ch.ScenePlan.Scenes[si]
				if sc.ID != r.SceneID {
					continue
				}
				if strings.TrimSpace(r.NewTitle) != "" {
					sc.Title = r.NewTitle
				}
				if strings.TrimSpace(r.NewDescription) != "" {
					sc.Description = r.NewDescription
				}
				if strings.TrimSpace(r.NewPoV) != "" {
					sc.PoV = r.NewPoV
				}
				if r.NewWordsBudget > 0 {
					sc.WordsBudget = r.NewWordsBudget
				}
				ch.ScenePlan.Approved = false
				changed++
				applied[[2]int{r.ChapterNum, r.SceneID}] = true
				break
			}
			break
		}
	}
	if changed == 0 {
		return 0, fmt.Errorf("no_editable_scenes_for_suggestion")
	}
	pendingRecs := false
	for _, r := range s.Recommendations {
		if r.Present && !applied[[2]int{r.ChapterNum, r.SceneID}] {
			pendingRecs = true
			break
		}
	}
	if pendingRecs {
		rep.Suggestions[idx].Status = "partially_applied"
	} else {
		rep.Suggestions[idx].Status = "applied"
	}
	rep.Suggestions[idx].AppliedAt = time.Now().Format(time.RFC3339)
	return changed, SaveProgress(progressPath, state)
}

// EditScenePlanRecommendation persists a manual edit made in one of the
// accordion fields of a proposed scene replacement before applying it.
func EditScenePlanRecommendation(state *Progress, suggestionID string, chapterNum, sceneID int, field, value string, progressPath string) error {
	rep := state.ScenePlanAnalysis
	if rep == nil {
		return fmt.Errorf("no_scene_plan_analysis")
	}
	idx := findSceneSuggestion(rep, suggestionID)
	if idx == -1 {
		return fmt.Errorf("suggestion_not_found")
	}
	recs := rep.Suggestions[idx].Recommendations
	for j := range recs {
		r := &recs[j]
		if !r.Present || r.ChapterNum != chapterNum || r.SceneID != sceneID {
			continue
		}
		switch field {
		case "new_title":
			r.NewTitle = value
		case "new_description":
			r.NewDescription = value
		case "new_pov":
			r.NewPoV = value
		case "new_words_budget":
			var parsed int
			if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil && parsed >= 0 {
				r.NewWordsBudget = parsed
			}
		default:
			return fmt.Errorf("unknown_field")
		}
		return SaveProgress(progressPath, state)
	}
	return fmt.Errorf("recommendation_not_found")
}
