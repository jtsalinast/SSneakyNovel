package story

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	ScenePos   int    `json:"scene_pos"` // index into ScenePlan.Scenes
	TropeIndex int    `json:"trope_index"` // index into Scene.TropeUnits
	TropeLabel string `json:"trope_label,omitempty"`
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
		if en {
			return "[Written structure]\nThis chapter was written without a scene plan — interventions cannot be targeted at concrete scenes.\n\n"
		}
		return "[Estructura escrita]\nEste capítulo se escribió sin plan de escenas — las intervenciones no pueden apuntar a escenas concretas.\n\n"
	}
	var sb strings.Builder
	if en {
		sb.WriteString("[Written structure — scenes and their trope units (use THESE exact indexes for interventions)]\n")
	} else {
		sb.WriteString("[Estructura escrita — escenas y sus unidades de tropo (usa ESTOS índices exactos para las intervenciones)]\n")
	}
	for pos, sc := range ch.ScenePlan.Scenes {
		title := sceneTitleOf(sc, lang)
		if len(sc.TropeUnits) == 0 {
			if en {
				fmt.Fprintf(&sb, "- scene_pos=%d \"%s\" (~%d words) — no trope segmentation recorded\n", pos, title, sc.WordsBudget)
			} else {
				fmt.Fprintf(&sb, "- scene_pos=%d \"%s\" (~%d palabras) — sin segmentación de tropos registrada\n", pos, title, sc.WordsBudget)
			}
			continue
		}
		units := make([]string, 0, len(sc.TropeUnits))
		for _, u := range sc.TropeUnits {
			units = append(units, fmt.Sprintf("trope_index=%d:%s(%dw)", u.Index, u.Label, u.Words))
		}
		if en {
			fmt.Fprintf(&sb, "- scene_pos=%d \"%s\" (~%d words) — trope units: %s\n", pos, title, sc.WordsBudget, strings.Join(units, " → "))
		} else {
			fmt.Fprintf(&sb, "- scene_pos=%d \"%s\" (~%d palabras) — unidades de tropo: %s\n", pos, title, sc.WordsBudget, strings.Join(units, " → "))
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
		s.ID = fmt.Sprintf("%s_%d_%d", prefix, now, i)
		s.Status = "pending"
		if keepInterventions && validateTargets != nil {
			var ivs []TropeIntervention
			for _, iv := range s.Interventions {
				iv.Feedback = strings.TrimSpace(iv.Feedback)
				if iv.Feedback == "" || !validateTargets(iv.ChapterNum, iv.ScenePos, iv.TropeIndex) {
					continue
				}
				iv.Status = "pending"
				ivs = append(ivs, iv)
			}
			if len(ivs) > 4 {
				ivs = ivs[:4]
			}
			s.Interventions = ivs
		} else {
			s.Interventions = nil
		}
		kept = append(kept, s)
	}
	return kept
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
return `{"summary":"...","overall_score":75,"pillar_scores":[{"key":"reader_profile","score":0,"criteria_levels":[{"key":"genre_promise","level":0},{"key":"reader_hooks","level":0},{"key":"tropes_freshness","level":0},{"key":"emotional_payoff","level":0},{"key":"comp_distance","level":0}],"note":"..."},{"key":"inspirational_pieces","score":0,"criteria_levels":[...],"note":"..."},{"key":"tone","score":0,"criteria_levels":[...],"note":"..."},{"key":"story_structure","score":0,"criteria_levels":[...],"note":"..."}],"suggestions":[{"category":"hook|pacing|scene_economy|stakes|structure|emotion|pov|budget|other","pillar":"reader_profile|inspirational_pieces|tone|story_structure","criteria":[{"key":"checklist_key_it_repairs"}],"score_impact":10,"title":"short imperative headline","detail":"what the drafted text currently does and why it falls short of bestseller level","action":"concrete guidance on what to change and where","applies_to_chapters":[3],"interventions":[{"chapter_num":3,"scene_pos":2,"trope_index":1,"trope_label":"tension","feedback":"one or two imperative sentences telling the rewriter exactly what to fix in THIS trope unit only"}]}]}`
}

// interventionsRulesEN explains the intervention contract in the prompt.
func interventionsRulesBlock(lang string) string {
	if normalizeLangEN(lang) {
		return `- interventions: 0 to 4 SMALL SURGICAL fixes per suggestion, each pointing at ONE concrete (chapter_num, scene_pos, trope_index) triplet shown in the written-structure map above. Each feedback must describe a minimal change confined to that trope unit (sharpen the hook of this opening, cut redundancy here, raise the stakes in this exchange...) — never a whole-chapter rewrite, never a target that is not in the map. Prefer few high-value interventions over many.`
	}
	return `- interventions: de 0 a 4 correcciones QUIRÚRGICAS y pequeñas por sugerencia, cada una apuntando a UN triplete concreto (chapter_num, scene_pos, trope_index) mostrado en el mapa de estructura escrita de arriba. Cada feedback debe describir un cambio mínimo limitado a esa unidad de tropo (afilar el gancho de esta apertura, recortar redundancia aquí, subir la apuesta en este intercambio...) — nunca un reescritado del capítulo entero, nunca un destino que no aparezca en el mapa. Prefiere pocas intervenciones de alto valor frente a muchas.`
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
	ID                string                      `json:"id"`
	Category          string                      `json:"category"` // hook|pacing|scene_economy|stakes|structure|emotion|pov|budget|other
	Title             string                      `json:"title"`
	Detail            string                      `json:"detail"`
	Action            string                      `json:"action"`
	Status            string                      `json:"status"` // pending | applied
	AppliedAt         string                      `json:"applied_at,omitempty"`
	AppliesToChapters []int                       `json:"applies_to_chapters,omitempty"`
	Interventions     []TropeIntervention         `json:"interventions,omitempty"`
	Recommendations   []OutlineRecommendation     `json:"recommendations,omitempty"` // always discarded: advice-only first pass
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

// findDraftedSuggestion locates a suggestion by id within a slice.
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
	GeneratedAt      string             `json:"generated_at"`
	BasedOnStructure string             `json:"based_on_structure,omitempty"`
	ChapterNum       int                `json:"chapter_num"`
	Summary          string             `json:"summary"`
	OverallScore     int                `json:"overall_score,omitempty"`
	PillarScores     []PillarAssessment `json:"pillar_scores,omitempty"`
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
	prompt := buildChapterEditorPrompt(cfg, state, settingsForPrompt, idx, lang)
	system := "You are a world-famous literary editor specializing in turning novels into global bestsellers. You are reviewing ONE DRAFTED CHAPTER (actual manuscript prose) of a novel in progress. Be constructive, specific and honest. Output strict JSON exactly as requested — no extra prose, no markdown code fences."

	logger.InfoKey("log.chapter_analyze_running", ch.Num)
	var result ChapterAnalysisReport
	for attempt := 0; attempt < 2; attempt++ {
		raw := llm.CallAPIWithRetryLog(ctx, apiCfg, system, prompt, logger)
		if raw == "" {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("API call failed or was canceled")
		}
		j := llm.ExtractJSON(raw)
		result = ChapterAnalysisReport{}
		if j != "" && json.Unmarshal([]byte(j), &result) == nil && (result.Summary != "" || len(result.Suggestions) > 0) {
			break
		}
		if attempt == 1 {
			return fmt.Errorf("model response is not valid chapter-analysis JSON")
		}
		prompt += "\n\nYour previous response was invalid. Return one complete JSON object only, using the required schema."
	}

	now := time.Now().Unix()
	result.Suggestions = sanitizeDraftedSuggestions(result.Suggestions, now, fmt.Sprintf("chpa_%d", ch.Num), nil, false)
	if result.OverallScore < 0 || result.OverallScore > 100 {
		result.OverallScore = 0
	}
	result.PillarScores = finalizePillars(result.PillarScores, result.Suggestions)
	result.Summary = strings.TrimSpace(result.Summary)
	result.GeneratedAt = time.Now().Format(time.RFC3339)
	result.BasedOnStructure = structureLabel(cfg.Story.Structure, lang)
	result.ChapterNum = ch.Num
	result.DraftFingerprint = fingerprint
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
	spans := sceneParaSpans(ch, paras)
	if spans == nil {
		// No usable scene plan (or degenerate budgets): fall back to the SAME
		// break-delimited pseudo-scene tiling SyncChapterBlocks uses for the
		// reader chips, so proposals still carry a CurrentText anchor and the
		// sheet can highlight them.
		spans = breakSceneSpans(paras)
	}
	if scenePos >= len(spans) {
		return ""
	}
	sp := spans[scenePos]
	lo, hi := sp[0], sp[1]
	if hi <= lo || hi > len(paras) {
		return ""
	}
	var sceneParas []string
	for _, p := range paras[lo:hi] {
		if strings.TrimSpace(p) != "" {
			sceneParas = append(sceneParas, p)
		}
	}
	if len(sceneParas) == 0 {
		return ""
	}
	sep := ch.BlockSep
	if sep == "" {
		sep = "\n\n"
	}
	units := ch.ScenePlan.Scenes[scenePos].TropeUnits
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
	targets := draftedTargetValidator(state, []int{ch.Num})
	planBuf := strings.Builder{}
	fmt.Fprintf(&planBuf, "%s", chapterTropeMapBlock(*ch, cfg.Language))
	fmt.Fprintf(&planBuf, "[CHAPTER PROSE — excerpt]\n%s\n", draftTruncate(ch.Content, 20000))

	prompt := draftedProposalPrompt(cfg, state, settings, editorPersona(cfg.Language),
		fmt.Sprintf("ONE DRAFTED CHAPTER #%d \"%s\"", ch.Num, ch.Title), s, planBuf.String(), []int{ch.Num}, targets)
	system := "You are a world-famous literary editor turning feedback into CONCRETE small interventions on drafted manuscript prose: each one targets exactly one trope unit of one scene of the chapter. Goal: push the chapter to global-bestseller level on the pillar under discussion, changing the minimum amount of prose. Output strict JSON exactly as requested — no extra prose, no markdown code fences."

	logger.InfoKey("log.chapter_analyze_proposing", ch.Num)
	interventions, err := requestInterventions(ctx, apiCfg, system, prompt, logger, targets)
	if err != nil {
		return nil, err
	}
	if len(interventions) == 0 {
		return nil, fmt.Errorf("no_valid_interventions")
	}
	// Accumulate: newer interventions win per (chapter, scene, trope) target.
	merged := interventions
	chosen := map[[3]int]bool{}
	for _, iv := range interventions {
		chosen[[3]int{iv.ChapterNum, iv.ScenePos, iv.TropeIndex}] = true
	}
	for _, old := range rep.Suggestions[sIdx].Interventions {
		if !chosen[[3]int{old.ChapterNum, old.ScenePos, old.TropeIndex}] && old.Status == "pending" {
			merged = append(merged, old)
		}
	}
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
	GeneratedAt      string             `json:"generated_at"`
	BasedOnStructure string             `json:"based_on_structure,omitempty"`
	ActNo            int                `json:"act_no"`
	ActName          string             `json:"act_name,omitempty"`
	StartCh          int                `json:"start_ch,omitempty"`
	EndCh            int                `json:"end_ch,omitempty"`
	Summary          string             `json:"summary"`
	OverallScore     int                `json:"overall_score,omitempty"`
	PillarScores     []PillarAssessment `json:"pillar_scores,omitempty"`
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
WRITTEN-STRUCTURE MAPS (scenes and trope units per chapter — the ONLY valid intervention targets):
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
MAPAS DE ESTRUCTURA ESCRITA (escenas y unidades de tropo por capítulo — los ÚNICOS destinos válidos de intervención):
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
		}
	}
	return func(chapterNum, scenePos, tropeIndex int) bool {
		_, ok := valid[[3]int{chapterNum, scenePos, tropeIndex}]
		return ok
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

This point currently has NO concrete interventions. Turn it into SMALL SURGICAL fixes on the drafted prose, each targeting exactly one trope unit shown in the written-structure maps above. Every intervention must measurably push the text toward GLOBAL BESTSELLER level on the pillar this point belongs to while changing the minimum amount of prose: keep voice, tense and POV of the surrounding units; never invent new events outside the chapter outlines; never touch a (chapter_num, scene_pos, trope_index) triplet that is not shown.
%s
Return STRICT JSON only (no markdown fences): {"interventions":[{"chapter_num":3,"scene_pos":2,"trope_index":1,"trope_label":"tension","feedback":"one or two imperative sentences describing the exact minimal fix for THIS trope unit"}]}
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

Este punto aún NO tiene intervenciones concretas. Conviértelo en correcciones QUIRÚRGICAS Y PEQUEÑAS sobre la prosa borroneada, cada una apuntando exactamente a una unidad de tropo mostrada en los mapas de estructura escrita de arriba. Cada intervención debe empujar medible el texto hacia nivel BESTSELLER GLOBAL en el pilar al que pertenece este punto cambiando la cantidad mínima de prosa: mantén voz, tiempo verbal y POV de las unidades vecinas; nunca inventes eventos nuevos fuera de los outlines; nunca toques un triplete (chapter_num, scene_pos, trope_index) que no esté mostrado.
%s
Devuelve SOLO JSON estricto (sin cercas markdown): {"interventions":[{"chapter_num":3,"scene_pos":2,"trope_index":1,"trope_label":"tension","feedback":"una o dos frases imperativas describiendo la corrección mínima exacta PARA ESTA unidad de tropo"}]}
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
			seen := map[[3]int]bool{}
			for _, iv := range p.Interventions {
				iv.Feedback = strings.TrimSpace(iv.Feedback)
				if iv.Feedback == "" || seen[[3]int{iv.ChapterNum, iv.ScenePos, iv.TropeIndex}] || !targets(iv.ChapterNum, iv.ScenePos, iv.TropeIndex) {
					continue
				}
				seen[[3]int{iv.ChapterNum, iv.ScenePos, iv.TropeIndex}] = true
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
	system := "You are a world-famous literary editor turning feedback into CONCRETE small interventions on drafted manuscript prose across an act of chapters: each one targets exactly one trope unit of one scene of one chapter. Goal: push the act to global-bestseller level on the pillar under discussion, changing the minimum amount of prose. Output strict JSON exactly as requested — no extra prose, no markdown code fences."

	logger.InfoKey("log.act_analyze_proposing", actNo)
	interventions, err := requestInterventions(ctx, apiCfg, system, prompt, logger, targets)
	if err != nil {
		return nil, err
	}
	if len(interventions) == 0 {
		return nil, fmt.Errorf("no_valid_interventions")
	}
	merged := interventions
	chosen := map[[3]int]bool{}
	for _, iv := range interventions {
		chosen[[3]int{iv.ChapterNum, iv.ScenePos, iv.TropeIndex}] = true
	}
	for _, old := range rep.Suggestions[sIdx].Interventions {
		if !chosen[[3]int{old.ChapterNum, old.ScenePos, old.TropeIndex}] && old.Status == "pending" {
			merged = append(merged, old)
		}
	}
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
