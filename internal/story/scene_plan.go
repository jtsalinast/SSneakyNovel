package story

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"sort"
	"strings"
	"time"

	"showmethestory/internal/sse"
)

// Scene planning: before writing a chapter, the LLM splits its outline into an
// ordered list of scenes ("beat sheet"), each with a word budget that sums to
// the chapter target. Writing then follows the plan scene by scene instead of
// improvising the whole chapter in one pass — a single 4000-word call habitually
// summarizes beats and lands at ~2600 words, while per-scene budgets make the
// length target reachable and keep every event tied to this chapter's outline.
// The author can review/edit the plan (add, reorder, delete scenes, change
// budgets) through the API, or enable auto-approve to skip the review step.

const (
	scenePlanMinScenes = 3
	scenePlanMaxScenes = 10
	// Never budget a scene below this; very short budgets produce summaries
	// instead of dramatized scenes.
	scenePlanMinSceneWords = 250
)

// Scene is one planned unit of the chapter, with an approximate word budget.
type Scene struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	PoV         string `json:"pov,omitempty"`
	WordsBudget int    `json:"words_budget"`
}

// ScenePlan is the editable beat sheet for one chapter. Persisted on the
// chapter (progress.json metadata), so it survives reloads and task restarts.
type ScenePlan struct {
	GeneratedAt string  `json:"generated_at,omitempty"`
	TargetWords int     `json:"target_words"`
	Scenes      []Scene `json:"scenes"`
	Approved    bool    `json:"approved,omitempty"`
}

// ScenePlanPendingError signals that GenerateChapterAction paused before
// writing because the current chapter has no approved scene plan and automatic
// approval is off. The HTTP layer maps it to a UI state (plan editor opens);
// auto-write mode stops the batch cleanly instead of erroring out.
type ScenePlanPendingError struct {
	ChapterNum int
}

func (e *ScenePlanPendingError) Error() string {
	return fmt.Sprintf("scene plan pending approval for chapter %d", e.ChapterNum)
}

// scenePlanSystemPrompt asks for a strict JSON array of scenes.
func scenePlanPrompt(ch ChapterState, outlineConstraints, targetBlock, lang string) string {
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	var sb strings.Builder
	if en {
		sb.WriteString(fmt.Sprintf(`You are a novel planner. Split chapter %d "%s" into an ordered scene plan (beat sheet) that the writer will follow scene by scene.

[Chapter outline — the ONLY source of events]
%s
%s
Rules:
- Cover the outline COMPLETELY: every beat must land in exactly one scene; invent no events outside the outline and do not advance later chapters' events.
- Output %s
- Each scene: title (short), description (2-4 sentences: what happens, who is present, where, the emotional turn), pov (the viewpoint character of that scene), words_budget (approximate words).
- The sum of all words_budget MUST equal the chapter target. Prefer 4–7 scenes; never fewer than %d nor more than %d.
- The LAST scene must end the chapter on the hook/cliffhanger implied by the outline.

Return ONLY a JSON array, no prose, no markdown fences:
[{"title":"...","description":"...","pov":"...","words_budget":900}, ...]`, ch.Num, ch.Title, ch.Outline, outlineConstraints, targetBlock, scenePlanMinScenes, scenePlanMaxScenes))
	} else {
		sb.WriteString(fmt.Sprintf(`Eres un planificador de novelas. Divide el capítulo %d "%s" en un plan de escenas ordenado (beat sheet) que el escritor seguirá escena por escena.

[Outline del capítulo — la ÚNICA fuente de eventos]
%s
%s
Reglas:
- Cubre el outline COMPLETAMENTE: cada beat debe caer en exactamente una escena; no inventes eventos fuera del outline ni avances eventos de capítulos posteriores.
- Salida: %s
- Cada escena: título (corto), descripción (2-4 frases: qué ocurre, quién participa, dónde, el giro emocional), pov (personaje punto de vista de esa escena), words_budget (palabras aproximadas).
- La suma de todos los words_budget DEBE ser igual al objetivo del capítulo. Prefiere 4–7 escenas; nunca menos de %d ni más de %d.
- La ÚLTIMA escena debe cerrar el capítulo con el gancho implícito en el outline.

Devuelve SOLO un arreglo JSON, sin prosa ni cercas markdown:
[{"title":"...","description":"...","pov":"...","words_budget":900}, ...]`, ch.Num, ch.Title, ch.Outline, outlineConstraints, targetBlock, scenePlanMinScenes, scenePlanMaxScenes))
	}
	return sb.String()
}

var scenePlanJSONRe = regexp.MustCompile(`\[[\s\S]*\]`)

// parseScenePlanJSON extracts the first JSON array from the model response
// (tolerating markdown fences and surrounding prose). NOTE: it must NOT go
// through cleanJSONResponse — that helper narrows the payload to its outermost
// JSON *object*, which destroys top-level arrays like the scene plan.
func parseScenePlanJSON(raw string) ([]Scene, error) {
	cleaned := raw
	if i := strings.Index(cleaned, "```"); i >= 0 {
		// Drop fence markers but keep everything else for the regex scan.
		cleaned = strings.ReplaceAll(cleaned, "```json", "")
		cleaned = strings.ReplaceAll(cleaned, "```", "")
	}
	match := scenePlanJSONRe.FindString(cleaned)
	if match == "" {
		return nil, fmt.Errorf("respuesta sin arreglo JSON de escenas")
	}
	var scenes []Scene
	if err := json.Unmarshal([]byte(match), &scenes); err != nil {
		return nil, fmt.Errorf("analizar plan de escenas: %w", err)
	}
	return scenes, nil
}

// normalizeScenePlan cleans up a parsed plan: drops empty scenes, renumbers
// IDs, and rescales word budgets so they sum exactly to targetWords. Budgets
// that are missing or absurd (<10) fall back to an even split.
func normalizeScenePlan(scenes []Scene, targetWords int) []Scene {
	out := make([]Scene, 0, len(scenes))
	for _, s := range scenes {
		s.Title = strings.TrimSpace(s.Title)
		s.Description = strings.TrimSpace(s.Description)
		s.PoV = strings.TrimSpace(s.PoV)
		if s.Title == "" && s.Description == "" {
			continue
		}
		if s.WordsBudget < 10 {
			s.WordsBudget = 0 // mark for even split below
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	// Even-split fallback for any zeroed budgets.
	fallback := targetWords / len(out)
	total := 0
	for i := range out {
		if out[i].WordsBudget == 0 {
			out[i].WordsBudget = fallback
		}
		total += out[i].WordsBudget
	}
	// Rescale so the sum matches the target exactly (largest remainder method).
	if total > 0 && targetWords > 0 && total != targetWords {
		type item struct {
			idx  int
			frac float64
		}
		items := make([]item, len(out))
		assigned := 0
		for i, s := range out {
			exact := float64(s.WordsBudget) * float64(targetWords) / float64(total)
			base := int(exact)
			if base < scenePlanMinSceneWords {
				base = scenePlanMinSceneWords
			}
			out[i].WordsBudget = base
			assigned += base
			items[i] = item{i, exact - float64(int(exact))}
		}
		diff := targetWords - assigned
		if diff > 0 {
			sort.Slice(items, func(a, b int) bool { return items[a].frac > items[b].frac })
			for k := 0; k < diff; k++ {
				out[items[k%len(items)].idx].WordsBudget++
			}
		} else {
			// Trim from the largest budgets, never below the per-scene floor.
			for diff < 0 {
				bi, bv := -1, scenePlanMinSceneWords
				for i := range out {
					if out[i].WordsBudget > bv || (out[i].WordsBudget > scenePlanMinSceneWords && bi == -1) {
						bv, bi = out[i].WordsBudget, i
					}
				}
				if bi == -1 {
					break
				}
				out[bi].WordsBudget--
				diff++
			}
		}
	}
	for i := range out {
		out[i].ID = i + 1
	}
	return out
}

// PlanChapterScenes generates (or regenerates) the scene plan for the current
// chapter and stores it on the chapter state. Returns the normalized plan.
func PlanChapterScenes(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, logger *sse.LogBroadcaster) (*ScenePlan, error) {
	return planChapterScenes(ctx, apiCfg, cfg, state, idx, "", logger)
}

// planChapterScenes is the internal implementation; extraContext lets callers
// (e.g. regeneration after user edits) add guidance to the planner prompt.
func planChapterScenes(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, extraContext string, logger *sse.LogBroadcaster) (*ScenePlan, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("generación cancelada")
	}
	if idx < 0 || idx >= len(state.Chapters) {
		return nil, fmt.Errorf("índice de capítulo fuera de rango")
	}
	ch := &state.Chapters[idx]
	lang := cfg.Language
	target, _ := resolveChapterTargetWords(state, cfg)
	minLen, maxLen := calcChapterLengthRange(target)

	var targetBlock string
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		targetBlock = fmt.Sprintf("between %d and %d scenes whose words_budget values sum to EXACTLY %d words (the chapter target; accepted final range %d–%d)", scenePlanMinScenes, scenePlanMaxScenes, target, minLen, maxLen)
	} else {
		targetBlock = fmt.Sprintf("%d a %d escenas cuyos words_budget suman EXACTAMENTE %d palabras (objetivo del capítulo; rango aceptado final %d–%d)", scenePlanMinScenes, scenePlanMaxScenes, target, minLen, maxLen)
	}

	constraints := buildOutlineConstraintsForLang(state, idx, lang)
	prompt := scenePlanPrompt(*ch, constraints, targetBlock, lang)
	if extraContext != "" {
		prompt += "\n\n" + extraContext
	}

	systemPrompt := i18n.SystemPromptFor(lang, "author_default")
	raw := llm.CallAPIWithRetryLog(ctx, apiCfg, systemPrompt, prompt, logger)
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("el planificador de escenas no produjo respuesta")
	}
	scenes, err := parseScenePlanJSON(raw)
	if err != nil {
		return nil, err
	}
	scenes = normalizeScenePlan(scenes, target)
	if len(scenes) == 0 {
		return nil, fmt.Errorf("el plan de escenas quedó vacío tras normalizar")
	}
	plan := &ScenePlan{
		GeneratedAt: time.Now().Format(time.RFC3339),
		TargetWords: target,
		Scenes:      scenes,
		Approved:    false,
	}
	ch.ScenePlan = plan
	return plan, nil
}

// renderScenePlanBlock formats the approved plan for the writing prompt: a
// numbered list with per-scene word budgets plus the running cumulative
// percentage, which anchors the model's sense of pacing mid-chapter.
func renderScenePlanBlock(plan *ScenePlan, lang string) string {
	if plan == nil || len(plan.Scenes) == 0 {
		return ""
	}
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	var sb strings.Builder
	if en {
		fmt.Fprintf(&sb, "\n[Scene plan for THIS chapter (follow it scene by scene; total %d words)]\n", plan.TargetWords)
	} else {
		fmt.Fprintf(&sb, "\n[Plan de escenas de ESTE capítulo (síguelo escena por escena; total %d palabras)]\n", plan.TargetWords)
	}
	cum := 0
	for _, s := range plan.Scenes {
		cum += s.WordsBudget
		pct := 100
		if plan.TargetWords > 0 {
			pct = cum * 100 / plan.TargetWords
		}
		meta := s.Title
		if s.PoV != "" {
			if en {
				meta = fmt.Sprintf("%s — PoV: %s", meta, s.PoV)
			} else {
				meta = fmt.Sprintf("%s — PdV: %s", meta, s.PoV)
			}
		}
		if en {
			fmt.Fprintf(&sb, "%d. [%d words · ends ≈%d%% of chapter] %s: %s\n", s.ID, s.WordsBudget, pct, meta, s.Description)
		} else {
			fmt.Fprintf(&sb, "%d. [%d palabras · termina ≈%d%% del capítulo] %s: %s\n", s.ID, s.WordsBudget, pct, meta, s.Description)
		}
	}
	if en {
		sb.WriteString("Write each scene as a FULL dramatized scene (action, sensory detail, dialogue) reaching its word budget before moving to the next; never compress a scene into one paragraph.\n")
	} else {
		sb.WriteString("Escribe cada escena como una escena completa dramatizada (acción, detalle sensorial, diálogo) alcanzando su presupuesto de palabras antes de pasar a la siguiente; nunca comprimas una escena a un párrafo.\n")
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Orchestration helpers (used by GenerateChapterAction and the HTTP layer)
// ---------------------------------------------------------------------------

// ensureScenePlanForWriting implements the scene-plan gate inside chapter
// generation:
//   - already approved plan  -> proceed to writing.
//   - auto mode on           -> generate the plan, approve it implicitly, proceed.
//   - otherwise              -> generate the plan if missing, persist it and
//     return *ScenePlanPendingError so the UI can review/edit it; the author
//     resumes generation once the plan is approved.
func ensureScenePlanForWriting(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, progressPath string, logger *sse.LogBroadcaster) error {
	ch := &state.Chapters[idx]
	if ch.ScenePlan != nil && ch.ScenePlan.Approved && len(ch.ScenePlan.Scenes) > 0 {
		return nil
	}
	if !state.AutoScenePlan && ch.ScenePlan != nil && len(ch.ScenePlan.Scenes) > 0 {
		// Plan exists but is not approved and auto mode is off: pause for review.
		if logger != nil {
			logger.InfoKey("log.scene_plan_pending", ch.Num)
		}
		return &ScenePlanPendingError{ChapterNum: ch.Num}
	}
	if logger != nil {
		logger.InfoKey("log.scene_plan_generating", ch.Num)
	}
	plan, err := PlanChapterScenes(ctx, apiCfg, cfg, state, idx, logger)
	if err != nil {
		if logger != nil {
			logger.WarnKey("log.scene_plan_failed", err)
		}
		// Planning failure must never block writing outright: fall back to the
		// previous single-pass behavior (length control still applies).
		return nil
	}
	if logger != nil {
		logger.SuccessKey("log.scene_plan_ready", ch.Num, len(plan.Scenes), plan.TargetWords)
	}
	if state.AutoScenePlan {
		plan.Approved = true
		if logger != nil {
			logger.InfoKey("log.scene_plan_auto_approved", ch.Num)
		}
	}
	if err := SaveProgress(progressPath, state); err != nil {
		return err
	}
	if !plan.Approved {
		if logger != nil {
			logger.InfoKey("log.scene_plan_pending", ch.Num)
		}
		return &ScenePlanPendingError{ChapterNum: ch.Num}
	}
	return nil
}

// GetScenePlan returns the current plan for a chapter (nil when absent).
func GetScenePlan(state *Progress, chapterNum int) (*ScenePlan, *ChapterState, error) {
	for i := range state.Chapters {
		if state.Chapters[i].Num == chapterNum {
			return state.Chapters[i].ScenePlan, &state.Chapters[i], nil
		}
	}
	return nil, nil, fmt.Errorf("capítulo %d no encontrado", chapterNum)
}

// SetScenePlanScenes replaces the scene list of a chapter's plan with the
// author's edits (add/remove/reorder scenes, change budgets). The plan is
// renumbered and its budgets rescaled to the chapter target unless the caller
// asks to keep the exact numbers (keepBudgets=true keeps user-typed values as
// long as they are positive; out-of-range totals are still normalized).
func SetScenePlanScenes(state *Progress, cfg *config.Config, chapterNum int, scenes []Scene, keepBudgets bool, progressPath string) (*ScenePlan, error) {
	plan, ch, err := GetScenePlan(state, chapterNum)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		target, _ := resolveChapterTargetWords(state, cfg)
		plan = &ScenePlan{TargetWords: target}
		ch.ScenePlan = plan
	}
	if len(scenes) == 0 {
		return nil, fmt.Errorf("el plan necesita al menos una escena")
	}
	var normalized []Scene
	if keepBudgets {
		// Preserve author-specified budgets verbatim; only renumber IDs and
		// fill in missing ones.
		normalized = make([]Scene, len(scenes))
		copy(normalized, scenes)
		for i := range normalized {
			normalized[i].Title = strings.TrimSpace(normalized[i].Title)
			normalized[i].Description = strings.TrimSpace(normalized[i].Description)
			normalized[i].PoV = strings.TrimSpace(normalized[i].PoV)
			if normalized[i].WordsBudget < scenePlanMinSceneWords {
				normalized[i].WordsBudget = scenePlanMinSceneWords
			}
			normalized[i].ID = i + 1
		}
	} else {
		normalized = normalizeScenePlan(scenes, plan.TargetWords)
	}
	if len(normalized) == 0 {
		return nil, fmt.Errorf("escenas inválidas tras normalizar")
	}
	plan.Scenes = normalized
	plan.Approved = false // any edit requires re-approval
	return plan, SaveProgress(progressPath, state)
}

// ApproveScenePlan marks the chapter's plan as approved so writing may start.
func ApproveScenePlan(state *Progress, chapterNum int, progressPath string) (*ScenePlan, error) {
	plan, _, err := GetScenePlan(state, chapterNum)
	if err != nil {
		return nil, err
	}
	if plan == nil || len(plan.Scenes) == 0 {
		return nil, fmt.Errorf("no hay plan de escenas que aprobar para el capítulo %d", chapterNum)
	}
	plan.Approved = true
	// Approving clears the review-gate pause marker so the UI stops
	// auto-opening the editor for this chapter.
	if state.PendingScenePlanChapter == chapterNum {
		state.PendingScenePlanChapter = 0
	}
	return plan, SaveProgress(progressPath, state)
}

// ClearScenePlan removes a chapter's plan (e.g. before regenerating one).
func ClearScenePlan(state *Progress, chapterNum int, progressPath string) error {
	_, ch, err := GetScenePlan(state, chapterNum)
	if err != nil {
		return err
	}
	ch.ScenePlan = nil
	return SaveProgress(progressPath, state)
}

// RegenerateScenePlan forces a fresh LLM plan for a chapter, optionally with
// extra guidance from the author (e.g. "merge scenes 2 and 3").
func RegenerateScenePlan(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, chapterNum int, extraContext string, progressPath string, logger *sse.LogBroadcaster) (*ScenePlan, error) {
	idx := -1
	for i := range state.Chapters {
		if state.Chapters[i].Num == chapterNum {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("capítulo %d no encontrado", chapterNum)
	}
	if err := ClearScenePlan(state, chapterNum, progressPath); err != nil {
		return nil, err
	}
	plan, err := planChapterScenes(ctx, apiCfg, cfg, state, idx, strings.TrimSpace(extraContext), logger)
	if err != nil {
		return nil, err
	}
	if state.AutoScenePlan {
		plan.Approved = true
	}
	if err := SaveProgress(progressPath, state); err != nil {
		return nil, err
	}
	return plan, nil
}

// SetAutoScenePlan toggles automatic plan approval project-wide.
func SetAutoScenePlan(state *Progress, auto bool, progressPath string) error {
	state.AutoScenePlan = auto
	return SaveProgress(progressPath, state)
}
