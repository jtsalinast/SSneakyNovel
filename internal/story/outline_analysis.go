package story

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"showmethestory/internal/config"
	"showmethestory/internal/llm"
	"showmethestory/internal/sse"
)

// OutlineRecommendation is one concrete replacement suggestion for a chapter
// outline produced by the bestseller-editor analysis. When Present is true,
// applying the recommendation swaps that chapter's title and/or outline text
// (accepted chapters are never touched).
type OutlineRecommendation struct {
	ChapterNum int    `json:"chapter_num"`
	Present    bool   `json:"present"`
	NewTitle   string `json:"new_title,omitempty"`
	NewOutline string `json:"new_outline,omitempty"`
}

// OutlineSuggestion is one point of the editor's feedback: what works / what
// must change and, when it applies, exactly what to replace in which chapter.
type OutlineSuggestion struct {
	ID                string                  `json:"id"`
	Category          string                  `json:"category"` // hook|pacing|character|stakes|structure|market|tone|other
	Title             string                  `json:"title"`
	Detail            string                  `json:"detail"`
	Action            string                  `json:"action"`
	Status            string                  `json:"status"` // pending | applied
	AppliedAt         string                  `json:"applied_at,omitempty"`
	AppliesToChapters []int                   `json:"applies_to_chapters,omitempty"`
	Recommendations   []OutlineRecommendation `json:"recommendations,omitempty"`
}

// PillarAssessment is the editor's verdict on one of the four analysis
// pillars (reader profile, inspirational pieces, tone, story structure):
// a 0-100 score plus a short justification. When a pillar scores at or above
// strongPillarScore and needs no suggestions, the frontend shows
// "No changes needed" instead of the "Propose outline changes" button.
type PillarAssessment struct {
	Key   string `json:"key"` // reader_profile|inspirational_pieces|tone|story_structure
	Score int    `json:"score"`
	Note  string `json:"note,omitempty"`
}

// strongPillarScore is the bar a pillar must clear to be considered
// bestseller-ready (and therefore eligible for "no changes needed").
const strongPillarScore = 90

// IsStrong reports whether the assessment marks its pillar as excellent.
func (p PillarAssessment) IsStrong() bool { return p.Score >= strongPillarScore }

// OutlineAnalysisReport is the full editorial pass over the novel outline,
// persisted in Progress so the frontend can render it point by point with an
// "apply" button per actionable suggestion.
type OutlineAnalysisReport struct {
	GeneratedAt      string              `json:"generated_at"`
	BasedOnStructure string              `json:"based_on_structure,omitempty"`
	Summary          string              `json:"summary"`
	OverallScore     int                 `json:"overall_score,omitempty"`
	PillarScores     []PillarAssessment  `json:"pillar_scores,omitempty"`
	Suggestions      []OutlineSuggestion `json:"suggestions"`
}

// buildOutlineEditorPrompt assembles the single LLM prompt for the full
// editorial pass. It deliberately centers the four author-facing pillars the
// product exposes: Ideal reader profile, Inspirational pieces, Tone and Story
// structure — plus theme/motif and the writing style as supporting context —
// and asks for strict JSON with per-chapter replacement recommendations.
func buildOutlineEditorPrompt(cfg *config.Config, state *Progress, settings *ProjectSettings, lang string) string {
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
	add("Literary motif", sc.Motif)
	add("Tone", sc.Tone)
	add("Story structure (framework the book must follow)", sc.Structure)
	add("Writing style & POV", strings.TrimSpace(sc.WritingStyle+" / "+sc.WritingPOV))
	add("Target audience", sc.TargetAudience)
	add("Ideal reader profile", sc.AudienceProfile)
	add("Inspirational pieces (comp titles / mood references)", sc.InspirationalPieces)
	add("Conflict scale", sc.EffectiveConflict())
	add("Protagonist type", sc.EffectiveProtagonist())
	add("Romance level", sc.RomanceLevel)
	add("World darkness", sc.WorldDarkness)
	add("Specific settings", sc.SpecificSettings)
	if settings != nil {
		cast := make([]string, 0, len(settings.Characters))
		for _, c := range settings.Characters {
			line := c.Name
			if role := strings.TrimSpace(c.Background); role != "" {
				line += " — " + Truncate(role, 160)
			}
			cast = append(cast, line)
		}
		add("Registered characters", strings.Join(cast, "; "))
		orgs := make([]string, 0, len(settings.Organizations))
		for _, o := range settings.Organizations {
			orgs = append(orgs, o.Name)
		}
		add("Organizations", strings.Join(orgs, ", "))
	}

	var chapBuf strings.Builder
	written := 0
	for _, ch := range state.Chapters {
		if ch.Status == StatusAccepted {
			continue
		}
		written++
		fmt.Fprintf(&chapBuf, "- Chapter #%d | %s | %s\n", ch.Num, ch.Title, strings.TrimSpace(ch.Outline))
	}
	outlineText := chapBuf.String()
	if strings.TrimSpace(outlineText) == "" {
		for _, ch := range state.Chapters {
			fmt.Fprintf(&chapBuf, "- Chapter #%d | %s | %s\n", ch.Num, ch.Title, strings.TrimSpace(ch.Outline))
		}
		outlineText = chapBuf.String()
	}

	structureName := structureLabel(sc.Structure, lang)

	return fmt.Sprintf(`You are %s, a world-famous literary editor whose lists have produced multiple global bestsellers. Your job: give the author a constructive, point-by-point editorial pass over the FULL OUTLINE (synopsis plan) below, aimed at maximizing its commercial and artistic potential for its ideal reader.

CRITICAL CONTEXT — WHAT YOU ARE REVIEWING:
- You are reviewing an OUTLINE ONLY: per-chapter planning notes / synopses written BEFORE the book is drafted. The prose does not exist yet. No actual scenes, dialogue, sentences or style are available to you.
- Judge it as an outline: premise strength, structural beats, pacing plan, character arcs and stakes AS PLANNED. Never critique prose quality, sentence-level style, dialogue, "show don't tell", word choice or voice — those cannot be assessed from an outline. Phrase every point in terms of what the PLAN shows or lacks (e.g. "chapter 7's planned beat leaves...", not "the writing of chapter 7 feels...").
- Your suggested rewrites must also be outline-level: replacement chapter plans (beats/synopses), not manuscript pages.

NOVEL PARAMETERS (the author's contract with the reader):
%s
STORY STRUCTURE FRAMEWORK IN USE: %s

OUTLINE — planning synopses only, not drafted prose (%d chapter(s) shown; accepted chapters are locked and already drafted — do not propose replacements for them):
%s

Analyze the outline against ALL of the parameters above, giving special weight to these four pillars:
1. Ideal reader profile — does every planned beat deliver what this reader came for? Promises kept? Genre expectations met without cliché?
2. Inspirational pieces — is the outline living up to the comp titles' standard (scope, hooks, emotional register) without copying them?
3. Tone — is the intended tone consistently baked into the chapter plans, escalating properly, and distinct enough to sell? (Judge the tonal plan, not prose.)
4. Story structure (%s) — are the framework's beats present, well-placed and proportioned (inciting incident, midpoint turn, dark moment, climax...)? Pacing dead zones or rushed payoffs in the plan?

Also weigh: premise hook & marketability, protagonist agency & arc, antagonist/stakes escalation, subplots, endings/payoff, chapter-level titles & blurbs (are they compelling back-cover lines?).

Return STRICT JSON only (no markdown fences, no prose):
{"summary":"2-4 paragraph editorial letter to the author, warm but honest, in %s","overall_score":72,"pillar_scores":[{"key":"reader_profile","score":68,"note":"one-sentence verdict in %s"},{"key":"inspirational_pieces","score":61,"note":"..."},{"key":"tone","score":74,"note":"..."},{"key":"story_structure","score":55,"note":"..."}],"suggestions":[{"category":"hook|pacing|character|stakes|structure|market|tone|other","title":"short imperative headline","detail":"what the outline currently does and why it falls short of bestseller level","action":"concrete guidance on what to change and how","applies_to_chapters":[3,4],"recommendations":[{"chapter_num":3,"present":true,"new_title":"a sharper chapter title","new_outline":"the COMPLETE replacement outline text for this chapter, same language as the outline, matching the original length +/-30%%"}]}]}

Grading standard (you are ruthless — your reputation rests on books that hit #1 worldwide):
- Score each of the four pillars 0-100 against BESTSELLER level, not against "competent debut". 90+ means the PLAN is genuinely strong as-is: you would sign it without touching that pillar. Reserve 90+ for truly exceptional planning; most outlines land 55-80. Grade hard: dead beats, vague stakes, generic hooks or an inconsistent tonal plan all cap scores below 85.
- Never penalize or praise things an outline cannot show (prose rhythm, dialogue quality, sentence variety). If a weakness could only be judged from the manuscript, it is out of scope — do not mention it.
- pillar_scores MUST contain exactly the four keys reader_profile, inspirational_pieces, tone, story_structure.
- Every pillar with score below 90 MUST have at least one corresponding suggestion (use its category: reader_profile→market, inspirational_pieces→other, tone→tone, story_structure→structure/pacing). A pillar scoring 90+ may have zero suggestions — then the UI shows "No changes needed" for it.

Rules:
- 5 to 12 suggestions, ordered most impactful first. Every pillar above must be considered; include a suggestion only when there is something real to improve.
- For each suggestion where a concrete rewrite would fix it, add one recommendation per affected editable chapter with present=true and BOTH new_title and new_outline fully written out (drop the recommendation if you cannot write a complete replacement). Never touch locked/accepted chapters.
- new_outline must be a ready-to-paste replacement chapter PLAN (beats/synopsis), consistent with surrounding chapters, the registered cast and the story structure — not a note about what to change and not drafted prose.
- Write summary, titles, details, actions and replacements in the SAME language as the outline text above.
- If the outline is genuinely strong, say so in the summary and return fewer, higher-value suggestions; recommendations may be empty overall.`,
		editorPersona(lang), meta.String(), structureName, written, outlineText, structureName, lang, lang, lang)
}

func editorPersona(lang string) string {
	if strings.HasPrefix(lang, "es") {
		return "un célebre editor literario mundial"
	}
	return "a celebrated world-class literary editor"
}

// keptSuggestionsForPillar returns the suggestions whose category belongs to
// the set of categories mapped to a pillar. Used to keep the editor honest:
// a pillar cannot be marked "excellent / no changes needed" while an open
// suggestion still targets it.
func keptSuggestionsForPillar(suggestions []OutlineSuggestion, categories []string) []OutlineSuggestion {
	in := make(map[string]bool, len(categories))
	for _, c := range categories {
		in[c] = true
	}
	var out []OutlineSuggestion
	for _, s := range suggestions {
		if in[s.Category] && s.Status != "applied" {
			out = append(out, s)
		}
	}
	return out
}

// AnalyzeFullOutline runs the bestseller-editor pass over the whole outline in
// ONE LLM call and stores the report on state. Callers persist progress.
func AnalyzeFullOutline(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, logger *sse.LogBroadcaster) error {
	if len(state.Chapters) == 0 {
		return fmt.Errorf("outline_empty")
	}
	lang := cfg.Language
	settingsForPrompt := settings
	if settings != nil {
		if selected, _ := retrieveSettings(settings, collectAllOutlines(state), settingsContextRunes); selected != nil {
			settingsForPrompt = selected
		}
	}
	prompt := buildOutlineEditorPrompt(cfg, state, settingsForPrompt, lang)
	system := "You are a world-famous literary editor specializing in turning novels into global bestsellers. You are reviewing an OUTLINE (per-chapter planning synopses), NOT drafted prose — never critique sentences, dialogue or writing style. Be constructive, specific and honest. Output strict JSON exactly as requested — no extra prose, no markdown code fences."

	logger.InfoKey("log.outline_analyze_running")
	var result OutlineAnalysisReport
	for attempt := 0; attempt < 2; attempt++ {
		raw := llm.CallAPIWithRetryLog(ctx, apiCfg, system, prompt, logger)
		if raw == "" {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("API call failed or was canceled")
		}
		j := llm.ExtractJSON(raw)
		result = OutlineAnalysisReport{}
		if j != "" && json.Unmarshal([]byte(j), &result) == nil && len(result.Suggestions) >= 0 && (result.Summary != "" || len(result.Suggestions) > 0) {
			break
		}
		if attempt == 1 {
			return fmt.Errorf("model response is not valid outline-analysis JSON")
		}
		prompt += "\n\nYour previous response was invalid. Return one complete JSON object only, using the required schema."
	}

	// Sanitize: drop empty suggestions, invalid categories, and any
	// recommendation targeting locked/accepted or unknown chapters.
	editable := map[int]bool{}
	for _, ch := range state.Chapters {
		if ch.Status != StatusAccepted {
			editable[ch.Num] = true
		}
	}
	validCategory := map[string]bool{"hook": true, "pacing": true, "character": true, "stakes": true, "structure": true, "market": true, "tone": true, "other": true}
	kept := make([]OutlineSuggestion, 0, len(result.Suggestions))
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
		cleanRecs := make([]OutlineRecommendation, 0, len(s.Recommendations))
		for _, r := range s.Recommendations {
			if !r.Present || !editable[r.ChapterNum] {
				continue
			}
			r.NewTitle = strings.TrimSpace(r.NewTitle)
			r.NewOutline = strings.TrimSpace(r.NewOutline)
			if r.NewTitle == "" && r.NewOutline == "" {
				continue
			}
			cleanRecs = append(cleanRecs, r)
		}
		s.Recommendations = cleanRecs
		cleanApps := make([]int, 0, len(s.AppliesToChapters))
		seenApp := map[int]bool{}
		for _, n := range s.AppliesToChapters {
			if editable[n] && !seenApp[n] {
				seenApp[n] = true
				cleanApps = append(cleanApps, n)
			}
		}
		sort.Ints(cleanApps)
		s.AppliesToChapters = cleanApps
		s.ID = fmt.Sprintf("oa_%d_%d", time.Now().Unix(), i)
		if s.Status != "applied" {
			s.Status = "pending"
		}
		kept = append(kept, s)
	}
	if result.OverallScore < 0 || result.OverallScore > 100 {
		result.OverallScore = 0
	}
	// Sanitize pillar scores: keep only the four known keys, clamp values,
	// and guarantee every pillar is represented (missing ones default to a
	// conservative score so "No changes needed" never shows without proof).
	validPillar := map[string]bool{"reader_profile": true, "inspirational_pieces": true, "tone": true, "story_structure": true}
	pillarDownstream := map[string][]string{
		"reader_profile":        {"market"},
		"inspirational_pieces":  {"other", "hook"},
		"tone":                  {"tone"},
		"story_structure":       {"structure", "pacing"},
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
		// A pillar can only claim "no changes needed" if it clears the bar
		// AND no suggestion targets its categories. The editor is graded
		// against bestseller level, so this should be rare by design.
		if p.IsStrong() && len(keptSuggestionsForPillar(kept, pillarDownstream[p.Key])) > 0 {
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
	state.OutlineAnalysis = &result
	return nil
}

// ApplyOutlineSuggestion applies every replacement recommendation of one
// analysis point to the live outline (only pending/writing/review chapters)
// and marks the suggestion applied. Returns the number of chapters changed.
func ApplyOutlineSuggestion(state *Progress, suggestionID string, progressPath string) (int, error) {
	rep := state.OutlineAnalysis
	if rep == nil {
		return 0, fmt.Errorf("no_outline_analysis")
	}
	idx := -1
	for i, s := range rep.Suggestions {
		if s.ID == suggestionID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return 0, fmt.Errorf("suggestion_not_found")
	}
	s := rep.Suggestions[idx]
	if s.Status == "applied" {
		return 0, fmt.Errorf("suggestion_already_applied")
	}
	changed := 0
	appliedChapters := map[int]bool{}
	for _, r := range s.Recommendations {
		if !r.Present || appliedChapters[r.ChapterNum] {
			continue
		}
		title, outline := r.NewTitle, r.NewOutline
		for i, ch := range state.Chapters {
			if ch.Num != r.ChapterNum {
				continue
			}
			if !outlineEditable(ch.Status) {
				break
			}
			newTitle := ch.Title
			if strings.TrimSpace(title) != "" {
				newTitle = title
			}
			newOutline := ch.Outline
			if strings.TrimSpace(outline) != "" {
				newOutline = outline
			}
			if err := EditChapterOutline(state, ch.Num, newTitle, newOutline, nil); err == nil {
				changed++
				appliedChapters[ch.Num] = true
			}
			_ = i
			break
		}
	}
	if changed == 0 {
		return 0, fmt.Errorf("no_editable_chapters_for_suggestion")
	}
	// If some recommendations still target chapters that were not touched
	// (e.g. a chapter got locked in the meantime), keep the suggestion
	// actionable instead of marking it fully applied.
	pendingRecs := false
	for _, r := range s.Recommendations {
		if r.Present && !appliedChapters[r.ChapterNum] {
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

// ProposeOutlineReplacements fills the gap for an analysis point that came
// back WITHOUT automatic replacements ("No automatic replacements proposed"):
// it asks the editor to translate its advice into concrete outline-level
// suggestions — WHAT to add/change and in WHICH chapters — and then drafts a
// ready-to-apply replacement title/outline for one chosen editable chapter.
// The generated recommendations are stored on the suggestion itself, so the
// regular Apply button can write them into the outline.
func ProposeOutlineReplacements(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, suggestionID string, progressPath string, logger *sse.LogBroadcaster) (*OutlineSuggestion, error) {
	rep := state.OutlineAnalysis
	if rep == nil {
		return nil, fmt.Errorf("no_outline_analysis")
	}
	idx := -1
	for i, s := range rep.Suggestions {
		if s.ID == suggestionID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil, fmt.Errorf("suggestion_not_found")
	}
	s := rep.Suggestions[idx]
	if s.Status == "applied" {
		return nil, fmt.Errorf("suggestion_already_applied")
	}

	var chapBuf strings.Builder
	for _, ch := range state.Chapters {
		mark := "editable"
		if ch.Status == StatusAccepted {
			mark = "LOCKED/accepted"
		}
		fmt.Fprintf(&chapBuf, "- Chapter #%d [%s] | %s | %s\n", ch.Num, mark, ch.Title, strings.TrimSpace(ch.Outline))
	}
	sc := cfg.Story
	prompt := fmt.Sprintf(`You are %s. You previously gave the author this editorial point about their novel OUTLINE (a planning synopsis per chapter — the prose is NOT drafted yet, so never critique sentences, dialogue or writing style):

POINT: %s
WHAT THE OUTLINE DOES NOW: %s
YOUR ADVICE: %s

NOVEL PARAMETERS: Tone: %s | Theme: %s | Structure framework: %s | Ideal reader: %s | Inspirational pieces: %s

FULL OUTLINE (chapter plans only):
%s

This point currently has NO concrete replacement proposals. Produce them at the OUTLINE level:
1. Decide which 1-3 EDITABLE chapters would benefit most from applying this point (never locked/accepted ones).
2. For EACH chosen chapter, write a complete replacement: a sharper chapter title and the FULL new outline text (same language as the outline, same length +/-30%%, consistent with neighboring chapters, the cast and the structure framework). It must be a ready-to-paste chapter PLAN (beats/synopsis), not drafted prose and not a note about what to change. Incorporate concretely what your advice asks for (e.g. add a beat, raise stakes, plant a foreshadow).

Return STRICT JSON only (no markdown fences): {"recommendations":[{"chapter_num":3,"present":true,"new_title":"...","new_outline":"..."}]}
Only include chapters that exist and are marked editable.`,
		editorPersona(cfg.Language), s.Title, s.Detail, s.Action,
		sc.Tone, sc.Theme, structureLabel(sc.Structure, cfg.Language), sc.AudienceProfile, sc.InspirationalPieces,
		chapBuf.String())
	system := "You are a world-famous literary editor turning feedback into concrete OUTLINE (chapter-plan) rewrites — never manuscript prose. Output strict JSON exactly as requested — no extra prose, no markdown code fences."

	logger.InfoKey("log.outline_analyze_proposing")
	type proposal struct {
		Recommendations []OutlineRecommendation `json:"recommendations"`
	}
	editable := map[int]bool{}
	for _, ch := range state.Chapters {
		if ch.Status != StatusAccepted {
			editable[ch.Num] = true
		}
	}
	var recs []OutlineRecommendation
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
				r.NewOutline = strings.TrimSpace(r.NewOutline)
				if !r.Present || !editable[r.ChapterNum] || (r.NewTitle == "" && r.NewOutline == "") {
					continue
				}
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
	// Accumulate instead of overwriting: each section can run "Propose
	// outline changes" multiple times (e.g. after applying a previous
	// batch) without discarding the earlier proposals. New recommendations
	// take precedence over older ones for the same chapter.
	merged := make([]OutlineRecommendation, 0, len(recs)+len(rep.Suggestions[idx].Recommendations))
	chosen := map[int]bool{}
	for _, r := range recs {
		chosen[r.ChapterNum] = true
	}
	merged = append(merged, recs...)
	for _, old := range rep.Suggestions[idx].Recommendations {
		if !chosen[old.ChapterNum] {
			merged = append(merged, old)
		}
	}
	rep.Suggestions[idx].Recommendations = merged
	seen := map[int]bool{}
	for _, r := range merged {
		if !seen[r.ChapterNum] {
			seen[r.ChapterNum] = true
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

// collectAllOutlines concatenates chapter outlines for retrieval queries.
func collectAllOutlines(state *Progress) string {
	var b strings.Builder
	for _, ch := range state.Chapters {
		b.WriteString(ch.Title)
		b.WriteString(" ")
		b.WriteString(ch.Outline)
		b.WriteString("\n")
	}
	return b.String()
}
