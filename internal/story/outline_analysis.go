package story

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
//
// Proposed marks a recommendation drafted on demand by the "Propose outline
// changes" action (as opposed to the ones bundled with the initial editorial
// pass). The frontend only surfaces proposed replacements — and keeps their
// accordions collapsed — until the author explicitly requests them, so no
// section silently rewrites the outline before that point.
//
// Cast carries the chapter's REPLACED cast when the editor proposes adding or
// removing characters from the chapter plan; nil means "keep the current cast".
type OutlineRecommendation struct {
	ChapterNum int                       `json:"chapter_num"`
	Present    bool                      `json:"present"`
	NewTitle   string                    `json:"new_title,omitempty"`
	NewOutline string                    `json:"new_outline,omitempty"`
	Cast       []OutlineChapterCharacter `json:"cast,omitempty"`
	Proposed   bool                      `json:"proposed,omitempty"`
}

// CastSummary renders the proposed chapter cast as a short human-readable line
// ("A; B; C") for the accordion, so the author can see at a glance whether a
// proposal touches the cast. Empty means "the current cast is kept".
func (r OutlineRecommendation) CastSummary() string {
	names := make([]string, 0, len(r.Cast))
	for _, c := range r.Cast {
		if n := strings.TrimSpace(c.Name); n != "" {
			names = append(names, n)
		}
	}
	return strings.Join(names, "; ")
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
	// Pillar is the analysis pillar this point serves (reader_profile |
	// inspirational_pieces | tone | story_structure). Filled by the rubric
	// contract: either declared by the editor or inferred from the category.
	Pillar string `json:"pillar,omitempty"`
	// Criteria lists the pillar-checklist items this point repairs, so the UI
	// can read "improve THESE areas of THAT pillar to reach 100%".
	Criteria []SuggestionCriterion `json:"criteria,omitempty"`
	// ScoreImpact is the points the pillar is expected to gain once this
	// point is fully applied, capped at the pillar's remaining headroom.
	ScoreImpact int `json:"score_impact,omitempty"`
	// Priority is the 3-level importance badge (high | medium | low) derived
	// deterministically from ScoreImpact + how many checklist items the point
	// repairs (see deriveSuggestionPriority), so it always agrees with the
	// rubric numbers shown next to it in the UI.
	Priority string `json:"priority,omitempty"`
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
	// CriteriaLevels is the editor's raw per-sub-criterion answer (only used
	// while parsing the model response; the sanitized report ships the full
	// Checklist below with levels filled in).
	CriteriaLevels []struct {
		Key   string `json:"key"`
		Level int    `json:"level"`
	} `json:"criteria_levels,omitempty"`
	// Checklist is the fixed rubric of this pillar with the graded level per
	// item. The frontend renders it as the "what must improve to reach 100%"
	// list, and the pillar Score is derived from these levels so closing every
	// item provably drives the pillar to 100.
	Checklist []PillarCriterion `json:"checklist,omitempty"`
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
	// OutlineFingerprint identifies the exact outline state (editable chapter
	// titles + synopses) the scores above were computed from. Re-analyzing an
	// UNCHANGED outline reuses the previous scores instead of paying for a new
	// LLM call; once the outline changes (e.g. suggestions are applied) the
	// fingerprint mismatches and the next pass genuinely re-scores everything.
	OutlineFingerprint string `json:"outline_fingerprint,omitempty"`
}

// outlineFingerprint builds a stable hash over the chapters the editor
// actually grades: every editable (non-accepted) chapter's number, title and
// outline text. Accepted chapters are excluded on purpose — they are locked
// and out of scope for scoring, so drafting or accepting prose must not
// invalidate the cached scores.
func outlineFingerprint(state *Progress) string {
	var b strings.Builder
	for _, ch := range state.Chapters {
		if ch.Status == StatusAccepted {
			continue
		}
		fmt.Fprintf(&b, "%d\x00%s\x00%s\x01", ch.Num, strings.TrimSpace(ch.Title), strings.TrimSpace(ch.Outline))
	}
	b.WriteString(gradeSalt()) // force a fresh LLM pass on every explicit re-analysis
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// buildOutlineEditorPrompt assembles the single LLM prompt for the full
// editorial pass. It deliberately centers the four author-facing pillars the
// product exposes: Ideal reader profile, Inspirational pieces, Tone and Story
// structure — plus theme/motif and the writing style as supporting context —
// and asks for strict JSON with advice only: concrete per-chapter replacements
// are drafted later, on demand, via ProposeOutlineReplacements ("Propose
// outline changes"), so the initial pass never suggests silent rewrites.
func buildOutlineEditorPrompt(cfg *config.Config, state *Progress, settings *ProjectSettings, lang string) string {
	// prevChecklist carries the previous pass's graded rubric (if any) so a
	// re-analysis after corrections RE-GRADES the same items and the score
	// provably rises where the author fixed things — instead of drifting
	// around randomized example anchors like before.
	var prevChecklist string
	if state.OutlineAnalysis != nil {
		prevChecklist = renderPreviousChecklistBlock(state.OutlineAnalysis, "THE OUTLINE")
	}
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

	return fmt.Sprintf(`You are %s, a world-famous literary editor whose lists have produced multiple global bestsellers. Your job: give the author a constructive, point-by-point editorial pass over the FULL OUTLINE (synopsis plan) below, with ONE clear goal: turn this plan into a global bestseller — every point you make must serve that bar, not mere competence.

CRITICAL CONTEXT — WHAT YOU ARE REVIEWING:
- You are reviewing an OUTLINE ONLY: per-chapter planning notes / synopses written BEFORE the book is drafted. The prose does not exist yet. No actual scenes, dialogue, sentences or style are available to you.
- Judge it as an outline: premise strength, structural beats, pacing plan, character arcs and stakes AS PLANNED. Never critique prose quality, sentence-level style, dialogue, "show don't tell", word choice or voice — those cannot be assessed from an outline. Phrase every point in terms of what the PLAN shows or lacks (e.g. "chapter 7's planned beat leaves...", not "the writing of chapter 7 feels...").
- This pass is ADVICE ONLY: do NOT draft replacement chapter plans here. The author requests concrete rewrites per point later, explicitly, via a "Propose outline changes" action — so nothing gets rewritten before they choose to act on a specific point.

NOVEL PARAMETERS (the author's contract with the reader):
%s
STORY STRUCTURE FRAMEWORK IN USE: %s

OUTLINE — planning synopses only, not drafted prose (%d chapter(s) shown; accepted chapters are locked and already drafted — do not propose replacements for them):
%s
%s
YOUR MISSION: do not just list flaws — reverse-engineer what makes books in this genre hit #1 (an irresistible hook, relentless pacing, arcs that pay off, stakes that escalate, a tone readers can feel on the cover) and grade THIS plan against that blueprint. Every suggestion you return must be a concrete step that measurably moves the outline toward bestseller level on its pillar, so that applying your top points in a few iterations raises the scores substantially.

%s
Analyze the outline against ALL of the parameters above, giving special weight to these four pillars:
1. Ideal reader profile — does every planned beat deliver what this reader came for? Promises kept? Genre expectations met without cliché?
2. Inspirational pieces — is the outline living up to the comp titles' standard (scope, hooks, emotional register) without copying them?
3. Tone — is the intended tone consistently baked into the chapter plans, escalating properly, and distinct enough to sell? (Judge the tonal plan, not prose.)
4. Story structure (%s) — are the framework's beats present, well-placed and proportioned (inciting incident, midpoint turn, dark moment, climax...)? Pacing dead zones or rushed payoffs in the plan?

Also weigh: premise hook & marketability, protagonist agency & arc, antagonist/stakes escalation, subplots, endings/payoff, chapter-level titles & blurbs (are they compelling back-cover lines?).

Return STRICT JSON only (no markdown fences, no prose):
{"summary":"2-4 paragraph editorial letter to the author, warm but honest, in %s","overall_score":75,"pillar_scores":[{"key":"reader_profile","score":0,"criteria_levels":[{"key":"genre_promise","level":0},{"key":"reader_hooks","level":0},{"key":"tropes_freshness","level":0},{"key":"emotional_payoff","level":0},{"key":"comp_distance","level":0}],"note":"one-sentence verdict in %s"},{"key":"inspirational_pieces","score":0,"criteria_levels":[...],"note":"..."},{"key":"tone","score":0,"criteria_levels":[...],"note":"..."},{"key":"story_structure","score":0,"criteria_levels":[...],"note":"..."}],"suggestions":[{"category":"hook|pacing|character|stakes|structure|market|tone|other","pillar":"reader_profile|inspirational_pieces|tone|story_structure","criteria":[{"key":"checklist_key_it_repairs"}],"score_impact":10,"title":"short imperative headline","detail":"what the outline currently does and why it falls short of bestseller level","action":"concrete guidance on what to change and how","applies_to_chapters":[3,4]}]}

Grading standard (you are ruthless — your reputation rests on books that hit #1 worldwide):
- Score each of the four pillars 0-100 against BESTSELLER level, not against "competent debut". 90+ means the PLAN is genuinely strong as-is: you would sign it without touching that pillar. Reserve 90+ for truly exceptional planning; most outlines land mid-range. Derive every score independently from this specific outline: the pillar number must equal the average of its criteria_levels, so re-grading corrected material raises exactly the items that improved. Grade hard: dead beats, vague stakes, generic hooks or an inconsistent tonal plan all cap scores below 85.
- Never penalize or praise things an outline cannot show (prose rhythm, dialogue quality, sentence variety). If a weakness could only be judged from the manuscript, it is out of scope — do not mention it.
- pillar_scores MUST contain exactly the four keys reader_profile, inspirational_pieces, tone, story_structure.
- Every pillar with score below 90 MUST have at least one corresponding suggestion (use its category: reader_profile→market, inspirational_pieces→other, tone→tone, story_structure→structure/pacing). A pillar scoring 90+ may have zero suggestions — then the UI shows "No changes needed" for it.

Rules:
- 5 to 12 suggestions, ordered most impactful first. Every pillar above must be considered; include a suggestion only when there is something real to improve.
- DO NOT include replacement chapter plans in this pass: return every suggestion WITHOUT a "recommendations" array (or with it empty). Concrete ready-to-apply replacements are generated later, ONLY after the author clicks "Propose outline changes" on a specific point — drafting them up front for every point risks inconsistent rewrites across sections.
- applies_to_chapters may still name the editable chapters (never locked/accepted ones) most affected by each point.
- action must describe WHAT to change and HOW at the outline level (beats/synopsis), framed as closing specific checklist items (e.g. "raise pacing_curve by merging the dead chapters 8–9"), so the author sees exactly which area of which pillar moves toward 100%%.
- Write summary, titles, details and actions in the SAME language as the outline text above.
- If the outline is genuinely strong, say so in the summary and return fewer, higher-value suggestions.`,
		editorPersona(lang), meta.String(), structureName, written, outlineText, prevChecklist,
		pillarContractPromptBlockEN(structureName), structureName, lang, lang)
}

func editorPersona(lang string) string {
	if strings.HasPrefix(lang, "es") {
		return "un célebre editor literario mundial"
	}
	return "a celebrated world-class literary editor"
}

// AnalyzeFullOutline runs the bestseller-editor pass over the whole outline in
// ONE LLM call and stores the report on state. Callers persist progress.
//
// Scoring is always recomputed against the CURRENT outline: before calling the
// model we store the fingerprint of the outline being graded, so re-analyzing
// after applying suggestions can never reuse stale scores (the fingerprint
// mismatch forces a fresh grading). As an optimization, if the outline has not
// changed at all since the previous pass, the existing report is kept verbatim
// and no LLM call is made — same input plan, same verdict.
func AnalyzeFullOutline(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, logger *sse.LogBroadcaster) error {
	if len(state.Chapters) == 0 {
		return fmt.Errorf("outline_empty")
	}
	fingerprint := outlineFingerprint(state)
	if prev := state.OutlineAnalysis; prev != nil && prev.OutlineFingerprint != "" && prev.OutlineFingerprint == fingerprint {
		logger.InfoKey("log.outline_analyze_unchanged")
		return nil
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

	// Sanitize: drop empty suggestions and invalid categories. Any
	// recommendation bundled with the initial pass is discarded (replacements
	// only exist after an explicit "Propose outline changes"), but chapters
	// named in applies_to_chapters are still validated against the editable set.
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
		// The initial editorial pass is advice-only: replacements are drafted
		// exclusively when the author clicks "Propose outline changes" on a
		// specific point (see ProposeOutlineReplacements). Any recommendation
		// the model returns anyway is dropped here so no section can silently
		// suggest — or apply — rewrites before that explicit action, which
		// would risk inconsistencies between sections.
		s.Recommendations = nil
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
	// Apply the rubric contract (see analysis_pillars.go): attach each
	// pillar's fixed checklist, DERIVE the pillar score from the graded
	// criteria levels, normalize every suggestion's declared pillar/criteria
	// and cap its claimed impact at the pillar's remaining headroom.
	finalIn := make([]FinalizableSuggestion, len(kept))
	for i, s := range kept {
		finalIn[i] = FinalizableSuggestion{Category: s.Category, Status: s.Status, Pillar: s.Pillar, Impact: s.ScoreImpact, Criteria: s.Criteria}
	}
	cleanPillars, finalOut := finalizePillarReport(result.PillarScores, finalIn)
	for i := range kept {
		kept[i].Pillar = finalOut[i].Pillar
		kept[i].Criteria = finalOut[i].Criteria
		kept[i].ScoreImpact = finalOut[i].Impact
		kept[i].Priority = finalOut[i].Priority
	}
	result.PillarScores = cleanPillars
	result.Summary = strings.TrimSpace(result.Summary)
	result.Suggestions = kept
	result.GeneratedAt = time.Now().Format(time.RFC3339)
	result.BasedOnStructure = structureLabel(cfg.Story.Structure, lang)
	// Record WHICH outline state these scores were computed from. Any later
	// change to an editable chapter (e.g. applying suggestions or editing by
	// hand) alters the fingerprint, so the next re-analysis is guaranteed to
	// re-score against the new outline instead of looking cached/stale.
	result.OutlineFingerprint = fingerprint
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
			// A recommendation may carry a REPLACED cast for the chapter; nil
			// (len == 0) means "keep the current cast untouched".
			var cast *[]OutlineChapterCharacter
			if len(r.Cast) > 0 {
				c := normalizeOutlineCharacters(r.Cast)
				cast = &c
			}
			if err := EditChapterOutline(state, ch.Num, newTitle, newOutline, cast); err == nil {
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
// ready-to-apply replacement title/outline (and, when useful, a replacement
// CAST) for the chosen editable chapters. The generated recommendations are
// stored on the suggestion itself, so the regular Apply button can write them
// into the outline.
//
// The goal of the drafted replacements is NOT merely to obey the advice: they
// must push the chapter toward BESTSELLER level on the pillar the point
// belongs to (a stronger hook, better-placed beats, sharper stakes...), while
// staying consistent with neighboring chapters, the structure framework and
// the cast. Cast changes may introduce brand-new characters; those get
// registered in the character sheet on apply (see registerCastCharacters).
func ProposeOutlineReplacements(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, suggestionID string, progressPath string, settingsPath string, logger *sse.LogBroadcaster) (*OutlineSuggestion, error) {
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
		castLine := "none listed"
		if len(ch.Characters) > 0 {
			names := make([]string, 0, len(ch.Characters))
			for _, oc := range ch.Characters {
				names = append(names, oc.Name)
			}
			castLine = strings.Join(names, ", ")
		}
		fmt.Fprintf(&chapBuf, "- Chapter #%d [%s] | %s | %s | cast: %s\n", ch.Num, mark, ch.Title, strings.TrimSpace(ch.Outline), castLine)
	}
	// Registered character sheet so cast proposals reuse canonical names.
	var castSheet strings.Builder
	if settings != nil {
		for _, c := range settings.Characters {
			line := c.Name
			if role := strings.TrimSpace(c.Background); role != "" {
				line += " — " + Truncate(role, 140)
			}
			castSheet.WriteString(line + "; ")
		}
	}
	sc := cfg.Story
	prompt := fmt.Sprintf(`You are %s. You previously gave the author this editorial point about their novel OUTLINE (a planning synopsis per chapter — the prose is NOT drafted yet, so never critique sentences, dialogue or writing style):

POINT: %s
WHAT THE OUTLINE DOES NOW: %s
YOUR ADVICE: %s

NOVEL PARAMETERS: Tone: %s | Theme: %s | Structure framework: %s | Ideal reader: %s | Inspirational pieces: %s

FULL OUTLINE (chapter plans only):
%s

REGISTERED CHARACTERS (the canonical character sheet — reuse these names verbatim when a registered character belongs in a chapter's cast):
%s

This point currently has NO concrete replacement proposals. Your goal is NOT merely to obey the advice: every replacement you draft must measurably push these chapters toward GLOBAL BESTSELLER level on the pillar this point belongs to (irresistible hook, better-placed beats, sharper escalating stakes, a tonal plan readers can feel...) while staying consistent with neighboring chapters, the structure framework and the cast — so that applying this batch makes a real difference in a few iterations. Produce the changes at the OUTLINE level:
1. Decide which 1-3 EDITABLE chapters would benefit most from applying this point (never locked/accepted ones).
2. For EACH chosen chapter, write a complete replacement: a sharper chapter title and the FULL new outline text (same language as the outline, same length +/-30%%, consistent with neighboring chapters, the cast and the structure framework). It must be a ready-to-paste chapter PLAN (beats/synopsis), not drafted prose and not a note about what to change. Incorporate concretely what your advice asks for (e.g. add a beat, raise stakes, plant a foreshadow).
3. You may also reshape each chosen chapter's CAST to serve the bestseller goal — add a character who raises stakes or conflict, remove one who adds nothing, mark first_appearance:true when a character debuts here. Prefer REGISTERED CHARACTERS above; you may invent brand-new characters when the improvement demands it (give them a one-line role in "note"). Only return "cast" for chapters whose cast should CHANGE; omit it to keep the current cast.

Return STRICT JSON only (no markdown fences): {"recommendations":[{"chapter_num":3,"present":true,"new_title":"...","new_outline":"...","cast":[{"name":"...","first_appearance":true,"note":"one-line role"}]}]}
Only include chapters that exist and are marked editable.`,
		editorPersona(cfg.Language), s.Title, s.Detail, s.Action,
		sc.Tone, sc.Theme, structureLabel(sc.Structure, cfg.Language), sc.AudienceProfile, sc.InspirationalPieces,
		chapBuf.String(), castSheetOrNone(settings))
	system := "You are a world-famous literary editor turning feedback into concrete OUTLINE (chapter-plan) rewrites — never manuscript prose. You rewrite chapter plans with one clear goal: make the book a global bestseller on the pillar under discussion, not just fix the complaint. Output strict JSON exactly as requested — no extra prose, no markdown code fences."

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
				// Sanitize a proposed replacement cast (if any): canonical names,
				// deduped; an empty list means "keep the current cast".
				r.Cast = normalizeOutlineCharacters(r.Cast)
				if !r.Present || !editable[r.ChapterNum] || (r.NewTitle == "" && r.NewOutline == "") {
					continue
				}
				// Mark as drafted on demand: the frontend only surfaces these
				// replacements — inside collapsed accordions — after this explicit
				// "Propose outline changes" action.
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
		if !chosen[old.ChapterNum] && old.Proposed {
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

// castSheetOrNone renders the registered character sheet for the proposal
// prompt (name — one-line role), or a placeholder when none are registered.
func castSheetOrNone(settings *ProjectSettings) string {
	if settings == nil || len(settings.Characters) == 0 {
		return "(none registered yet)"
	}
	var b strings.Builder
	for _, c := range settings.Characters {
		line := c.Name
		if role := strings.TrimSpace(c.Background); role != "" {
			line += " — " + Truncate(role, 140)
		}
		b.WriteString(line + "; ")
	}
	return b.String()
}

// RegisterCastCharacters adds to the project character sheet every character
// that appears in an applied recommendation's proposed cast but is not yet
// registered (the editor may invent brand-new characters when reshaping a
// chapter). Debut notes become the character's background so later generation
// steps know the intended role. Returns how many characters were added.
func RegisterCastCharacters(settings *ProjectSettings, recs []OutlineRecommendation, settingsPath string) (int, error) {
	if settings == nil || len(recs) == 0 {
		return 0, nil
	}
	added := 0
	existing := RegisteredCharacterNameSet(settings)
	for _, r := range recs {
		if !r.Present {
			continue
		}
		for _, c := range normalizeOutlineCharacters(r.Cast) {
			if existing[c.Name] {
				continue
			}
			settings.Characters = append(settings.Characters, Character{
				ID:         settings.NextCharacterID(),
				Name:       c.Name,
				Background: c.Note,
				Notes:      fmt.Sprintf("Added by bestseller-editor outline proposal (chapter %d)", r.ChapterNum),
			})
			existing[c.Name] = true
			added++
		}
	}
	if added == 0 {
		return 0, nil
	}
	if err := SaveProjectSettings(settingsPath, settings); err != nil {
		return added, err
	}
	return added, nil
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
