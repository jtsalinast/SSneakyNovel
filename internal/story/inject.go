package story

import (
	"encoding/json"
	"fmt"
	"regexp"
	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"sort"
	"strings"
	"unicode/utf8"
)

// Helper functions that produce language-specific text fragments injected
// into prompt templates. These are NOT the prompt templates themselves
// (those live in prompts.go / prompts_en.go) — these are the runtime
// context blocks built from project state.

// Context budgets for the reverse-constraint block. The PAST gets priority:
// when the combined text exceeds the total budget, silent truncation used to
// drop the oldest (i.e. most fact-dense) lines while keeping upcoming outlines,
// which made the model treat planned future events as if they had already
// happened. Past context now claims most of the budget up front.
const (
	outlineArcTotalBudget   = 14000
	outlineArcPastBudget    = 8000 // past outlines + retrieved older-chapter facts
	outlineArcFutureBudget  = 4000 // upcoming-chapter outlines
	outlineArcPastRetrieval = 3000 // subset of outlineArcPastBudget spent on older chapters
)

// appendArcBlock appends a standalone channel block to the arc string while
// keeping the total under outlineArcTotalBudget. The main past/future channels
// are always preserved; if there is no room left, the block is truncated to
// whatever fits instead of silently dropping content from the primary channels.
func appendArcBlock(arc, block string) string {
	if block == "" {
		return arc
	}
	remaining := outlineArcTotalBudget - utf8.RuneCountInString(arc)
	if remaining <= 0 {
		return arc
	}
	block = truncateAtLineBoundary(block, remaining)
	return arc + block
}

// truncateAtLineBoundary shortens s to at most limit runes without ever
// cutting a line mid-sentence: the previous rune-level truncation could split
// the "already happened" channel in the middle of an event description, which
// the model read as license to re-enact the whole event. If even the first
// line does not fit, the block is dropped entirely rather than fed half-read.
func truncateAtLineBoundary(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	cut := strings.LastIndexByte(truncateRunesExact(s, limit), '\n')
	if cut <= 0 {
		return ""
	}
	return s[:cut+1]
}

// appendIfBudgetOver adds a notice when truncateAtLineBoundary had to drop
// content, so the reader of the prompt knows the block is partial instead of
// silently treating the truncated tail as the complete set.
func appendIfBudgetOver(full, kept string, lang string) string {
	if kept == "" || utf8.RuneCountInString(full) <= utf8.RuneCountInString(kept) {
		return kept
	}
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return kept + "[Note: this list was shortened for context budget; omitted entries follow the same rules.]\n"
	}
	return kept + "[Nota: esta lista se acortó por presupuesto de contexto; las entradas omitidas siguen las mismas reglas.]\n"
}

// isDraftedChapter reports whether a chapter has real written prose and its
// story-so-far summary, meaning its events truly happened for continuity
// purposes. Chapters still in pending/writing/review only carry PLANNED
// outlines — feeding those into the "already happened" channel is what caused
// cross-chapter event duplication (e.g. a meeting outlined for chapter 3 being
// written into chapter 2 because the arc block claimed it had already occurred).
func isDraftedChapter(ch ChapterState) bool {
	return ch.Status == StatusAccepted || (ch.Content != "" && ch.Summary != "")
}

// arcAudience selects which channels the arc constraint block carries. The
// writer receives ONLY the hard machine-derived constraints (debut ban list,
// one-time-event reservations, already-introduced cast, continuity notes):
// feeding it the verbatim upcoming outlines was proven counterproductive —
// the model raids the future text as inspiration material even under a
// "locked vault" warning, staging premature debuts and reveals. The direction
// each chapter must end in belongs in its own outline instead. Reviewers
// (fact-checker, conflict analyst) keep the full block including the upcoming
// outlines, because they must judge prose against planned events rather than
// be tempted to write them.
type arcAudience int

const (
	arcWriter arcAudience = iota
	arcReviewer
)

// buildOutlineConstraintsForLang returns the chapter-arc reverse-constraint
// block in the requested language. It deliberately EXCLUDES the current
// chapter's own outline: that belongs exclusively to the [Task for this
// chapter] section of the prompt, so the model can never confuse "what to
// write now" with "what already happened" or "what comes later".
func buildOutlineConstraintsForLang(state *Progress, idx int, lang string) string {
	return buildOutlineConstraintsForAudience(state, idx, lang, arcWriter)
}

// buildArcConstraintsForReview is the fact-check / conflict-analysis variant:
// same channel layout as the writer block but WITH the upcoming-outline
// window, so reviewers can verify nothing from later chapters leaked early.
func buildArcConstraintsForReview(state *Progress, idx int, lang string) string {
	return buildOutlineConstraintsForAudience(state, idx, lang, arcReviewer)
}

func buildOutlineConstraintsForAudience(state *Progress, idx int, lang string, aud arcAudience) string {
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	var arcHeader, upcomingHeader, happenedHeader string
	if en {
		arcHeader = "[Full-novel chapter arc (reverse constraint, must be obeyed strictly)]\n"
		upcomingHeader = "- Upcoming chapters — PLANNED events (character debuts, first meetings, identity reveals etc.) assigned to specific LATER chapters. They have NOT happened yet. This chapter MUST NOT make them happen early, nor hint at or spoil them:\n"
		happenedHeader = "- Already happened — the events below actually occurred in drafted chapters. This chapter must not re-enact them as new events (especially one-time events like first meetings or identity reveals — only continue them as established facts):\n"
	} else {
		arcHeader = "[Arco de capítulos de la novela (restricción inversa, debe obedecerse estrictamente)]\n"
		upcomingHeader = "- Capítulos próximos — eventos PLANIFICADOS (debut de personajes, primeros encuentros, revelaciones de identidad, etc.) asignados a capítulos POSTERIORES específicos. TODAVÍA NO han ocurrido. Este capítulo NO debe adelantarlos ni insinuarlos o arruinarlos:\n"
		happenedHeader = "- Ya ocurrido — los eventos siguientes sucedieron realmente en capítulos ya redactados. Este capítulo no debe volver a representarlos como eventos nuevos (especialmente eventos únicos como primeros encuentros o revelaciones de identidad — solo continúalos como hechos establecidos):\n"
	}

	var pastLines, futureLines []string
	if idx >= 0 && idx < len(state.Chapters) {
		pastLines = append(pastLines, chapterEnding(state, state.Chapters[idx].Num, lang))
	}
	recentStart := max(0, idx-historyRecentChapters)
	for i := recentStart; i < idx && i < len(state.Chapters); i++ {
		ch := state.Chapters[i]
		if !isDraftedChapter(ch) || strings.TrimSpace(ch.Outline) == "" {
			continue
		}
		pastLines = append(pastLines, formatChapterLine(ch.Num, ch.Title, ch.Outline, lang))
	}
	old := []knowledgeDocument{}
	if idx > recentStart {
		query := chapterKnowledgeQuery(state.Chapters[idx])
		for i := 0; i < recentStart; i++ {
			ch := state.Chapters[i]
			if !isDraftedChapter(ch) || strings.TrimSpace(ch.Outline) == "" {
				continue
			}
			old = append(old, knowledgeDocument{text: formatChapterLine(ch.Num, ch.Title, ch.Outline, lang)})
		}
		for _, i := range packKnowledge(old, rankKnowledge(old, query), outlineArcPastRetrieval) {
			pastLines = append(pastLines, old[i].text)
		}
	}
	end := min(idx+1+futureOutlineWindow, len(state.Chapters))
	if aud == arcReviewer {
		for i := idx + 1; i < end; i++ {
			ch := state.Chapters[i]
			if strings.TrimSpace(ch.Outline) == "" {
				continue
			}
			futureLines = append(futureLines, formatChapterLine(ch.Num, ch.Title, ch.Outline, lang))
		}
	}
	// Tag the future channel with the one-time events (debut / first meeting)
	// each upcoming outline owns, so reviewers can match a leaked event to its
	// destination chapter. The writer never sees the verbatim future outlines
	// at all — only these machine-derived reservations (see arcAudience):
	// handing it the raw text of later chapters is what let it raid reveals
	// (a "stranger" that is unmistakably Lord Edmund, a dropped glove) despite
	// every warning.
	eventLines := buildPrematureEventLines(state, idx, lang)
	if len(eventLines) > 0 {
		joined := strings.TrimRight(strings.Join(eventLines, ""), "\n")
		if aud == arcReviewer && len(futureLines) > 0 {
			futureLines = append([]string{joined}, futureLines...)
		} else {
			// No verbatim future material (writer audience, or reviewer with no
			// outlines yet): the reserved events need their own labelled entry
			// in the channel or they would be silently lost.
			var evHead string
			if en {
				evHead = "- UNIQUE EVENTS reserved for LATER chapters (must NOT occur or be hinted here):\n"
			} else {
				evHead = "- EVENTOS ÚNICOS reservados para capítulos POSTERIORES (NO deben ocurrir ni insinuarse aquí):\n"
			}
			futureLines = append(futureLines, evHead+joined+"\n")
		}
	}

	hardDebut := buildHardDebutBlock(state, idx, lang)
	alreadyIntroduced := buildAlreadyIntroducedBlock(state, idx, lang)

	hasPast := false
	for _, l := range pastLines {
		if strings.TrimSpace(l) != "" {
			hasPast = true
			break
		}
	}
	if !hasPast && len(futureLines) == 0 && hardDebut == "" {
		return ""
	}

	// Budget the two channels independently so long upcoming-outline windows
	// can no longer evict the key facts of previous chapters. The hard debut
	// ban rides FIRST: it is the most actionable, machine-derived constraint
	// and must never be evicted by budget trimming.
	var sb strings.Builder
	sb.WriteString(arcHeader)
	if hardDebut != "" {
		sb.WriteString(hardDebut)
	}
	if len(futureLines) > 0 {
		header := upcomingHeader
		if aud == arcWriter {
			// No verbatim outlines here — this channel only carries event
			// reservations; the vault wording below would mislead.
			if en {
				header = "- Reserved for later chapters — events that have NOT happened yet and must not occur or be hinted in this chapter (their destination chapter is listed):\n"
			} else {
				header = "- Reservado para capítulos posteriores — eventos que TODAVÍA NO han ocurrido y no deben ocurrir ni insinuarse en este capítulo (se indica su capítulo destino):\n"
			}
		}
		sb.WriteString(header)
		futureBody := boundedHistoryFallback(futureLines, outlineArcFutureBudget)
		sb.WriteString(appendIfBudgetOver(strings.Join(futureLines, ""), futureBody, lang))
		sb.WriteString("\n")
	}
	if hasPast {
		sb.WriteString(happenedHeader)
		pastBody := boundedHistoryFallback(pastLines, outlineArcPastBudget)
		sb.WriteString(appendIfBudgetOver(strings.Join(pastLines, ""), pastBody, lang))
		sb.WriteString("\n")
	}
	if alreadyIntroduced != "" {
		sb.WriteString(alreadyIntroduced)
	}
	// Continuity notes ride the past channel: they are hard "already learned
	// the hard way" constraints about what went wrong before. Appended with
	// dedicated headroom so a full arc block can no longer evict them.
	arcFull := sb.String()
	arcKept := truncateAtLineBoundary(arcFull, outlineArcTotalBudget)
	return appendArcBlock(appendIfBudgetOver(arcFull, arcKept, lang), buildContinuityNotesForLang(state, idx, lang))
}

func formatChapterLine(num int, title, outline, lang string) string {
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return fmt.Sprintf("Chapter %d \"%s\": %s\n", num, title, outline)
	}
	return fmt.Sprintf("Capítulo %d «%s»: %s\n", num, title, outline)
}

// statusTag labels a chapter's real state so the model never mistakes a merely
// planned chapter for one that has actually been written.
func statusTag(ch ChapterState, lang string) string {
	if ch.Status == StatusAccepted {
		return ""
	}
	if isDraftedChapter(ch) {
		if i18n.NormalizeLanguage(lang) == i18n.LangEN {
			return " [drafted, awaiting review — treat its events as occurred]"
		}
		return " [redactado, pendiente de revisión — trata sus eventos como ocurridos]"
	}
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return " [PLANNED ONLY — not written yet, do not treat as happened]"
	}
	return " [SOLO PLANIFICADO — aún no escrito, no lo trates como ocurrido]"
}

// quotedSpeechRe captures a line of direct speech: a run of text enclosed in
// typographic or straight double quotes, with an optional attribution tail
// ("—dijo Ana.", said Ben, —susurró ella.). Used to harvest per-character voice
// samples from already-written chapters.
var quotedSpeechRe = regexp.MustCompile(`[“"«]([^“”"»«]{15,160})[”"»](\s*[—–-]\s*[\p{L} ]{1,30}\.)?`)

// characterNameList returns the known character names from the settings
// snapshot (original + inherited), used to attribute dialogue lines.
func characterNameList(settings *ProjectSettings) []string {
	if settings == nil {
		return nil
	}
	names := make([]string, 0, len(settings.Characters))
	for _, c := range settings.Characters {
		if n := strings.TrimSpace(c.Name); n != "" {
			names = append(names, n)
		}
	}
	return names
}

// extractDialogueSamples scans prose for quoted speech attributed to name
// (either "…" dijo Name, or Name said "…"), returning up to maxSamples of
// one line each, trimmed to maxRunes per sample.
func extractDialogueSamples(content string, name string, maxSamples, maxRunes int) []string {
	if content == "" || name == "" {
		return nil
	}
	out := []string{}
	lowerName := strings.ToLower(name)
	for _, m := range quotedSpeechRe.FindAllStringSubmatch(content, -1) {
		if len(out) >= maxSamples {
			break
		}
		body := strings.TrimSpace(m[1])
		attr := strings.ToLower(m[2])
		attributed := strings.Contains(attr, lowerName)
		if !attributed {
			// also accept "Name:" style lead-ins directly before the quote
			idx := strings.Index(strings.ToLower(content), strings.ToLower("\""+body))
			if idx > 0 {
				lead := content[max(0, idx-60):idx]
				attributed = strings.Contains(strings.ToLower(lead), lowerName+":") || strings.HasSuffix(strings.TrimSpace(lead), name)
			}
		}
		if !attributed || body == "" {
			continue
		}
		sample := truncateRunesExact(body, maxRunes)
		if !strings.Contains(strings.Join(out, "\n"), sample) {
			out = append(out, sample)
		}
	}
	return out
}

// buildContinuityNotesForLang collects the fact-check issues that earlier
// accepted chapters had to resolve, as explicit "do not repeat these mistakes"
// constraints for the chapter being planned or written. Returns "" when no
// notes exist yet (early chapters / clean runs).
func buildContinuityNotesForLang(state *Progress, idx int, lang string) string {
	if idx <= 0 || state == nil {
		return ""
	}
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	var lines []string
	seen := map[string]bool{}
	start := max(0, idx-historyRecentChapters)
	for i := start; i < idx && i < len(state.Chapters); i++ {
		ch := state.Chapters[i]
		for _, note := range ch.ContinuityNotes {
			key := strings.ToLower(note)
			if note == "" || seen[key] {
				continue
			}
			seen[key] = true
			if en {
				lines = append(lines, fmt.Sprintf("- Chapter %d: %s", ch.Num, note))
			} else {
				lines = append(lines, fmt.Sprintf("- Capítulo %d: %s", ch.Num, note))
			}
		}
	}
	if len(lines) == 0 {
		return ""
	}
	body := boundedHistoryFallback(lines, 2000)
	if en {
		return "\n[Continuity notes — consistency problems that occurred in earlier chapters and were fixed. Do NOT repeat them in this chapter]\n" + body
	}
	return "\n[Notas de continuidad — problemas de consistencia que ocurrieron en capítulos anteriores y fueron corregidos. NO los repitas en este capítulo]\n" + body
}

// buildVoiceSamplesForLang produces a "[Character voice reference]" block with
// short dialogue excerpts harvested from already-drafted chapters for the
// characters appearing in the current chapter. Rule 6 of the writing prompt
// demands each character keep a distinct voice; without concrete examples the
// model flattens every character into the same register. Budget is capped so
// this never crowds out the history/arc channels.
func buildVoiceSamplesForLang(state *Progress, idx int, settings *ProjectSettings, lang string) string {
	if idx < 1 || idx >= len(state.Chapters) {
		return ""
	}
	ch := state.Chapters[idx]
	cast := []string{}
	for _, oc := range ch.Characters {
		if n := strings.TrimSpace(oc.Name); n != "" {
			cast = append(cast, n)
		}
	}
	if len(cast) == 0 {
		// Fall back to scanning known names inside the outline text.
		for _, n := range characterNameList(settingsAtChapter(settings, ch.Num)) {
			if strings.Contains(ch.Outline, n) {
				cast = append(cast, n)
			}
		}
	}
	if len(cast) == 0 {
		return ""
	}
	const voiceTotalBudget = 1800
	const perCharSamples = 2
	const perSampleRunes = 160
	var parts []string
	for _, name := range cast {
		samples := []string{}
		// Newest chapters first: recent voice is the closest reference.
		for i := idx - 1; i >= 0 && len(samples) < perCharSamples; i-- {
			prev := state.Chapters[i]
			if !isDraftedChapter(prev) || prev.Content == "" {
				continue
			}
			got := extractDialogueSamples(prev.Content, name, perCharSamples-len(samples), perSampleRunes)
			samples = append(samples, got...)
		}
		if len(samples) == 0 {
			continue
		}
		lines := make([]string, len(samples))
		for i, s := range samples {
			lines[i] = "- \"" + s + "\""
		}
		parts = append(parts, name+":\n"+strings.Join(lines, "\n"))
	}
	if len(parts) == 0 {
		return ""
	}
	body := boundedHistoryFallback(parts, voiceTotalBudget)
	if body == "" {
		return ""
	}
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return "\n\n[Character voice reference — real excerpts of how these characters speak in earlier chapters; imitate their register, vocabulary and rhythm, do not reuse the lines themselves]\n" + body
	}
	return "\n\n[Referencia de voz de los personajes — fragmentos reales de cómo hablan estos personajes en capítulos anteriores; imita su registro, vocabulario y ritmo, no reutilices las frases]\n" + body
}

func buildPreviousChapterTailForLang(state *Progress, idx int, lang string) string {
	if idx <= 0 || idx >= len(state.Chapters) {
		return ""
	}
	prev := state.Chapters[idx-1]
	if prev.Content == "" {
		return ""
	}
	tail := tailAtParagraph(prev.Content, prevTailMaxRunes)
	if tail == "" {
		return ""
	}
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return fmt.Sprintf("[Previous chapter ending (for seamless scene/mood continuation only — do NOT recap or rewrite)]\n%s\n\n", tail)
	}
	return fmt.Sprintf("[Final del capítulo anterior (solo para continuar la escena y el tono sin costura — NO resumas ni reescribas)]\n%s\n\n", tail)
}

func buildHistorySummaryForLang(state *Progress, idx int, lang string) string {
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	var header, emptyText string
	if en {
		header = "[Story so far — these events HAVE already happened in written chapters]\n"
		emptyText = "This is the opening of the story; no prior context."
	} else {
		header = "[Historia hasta ahora — estos eventos YA OCURRIERON en capítulos escritos]\n"
		emptyText = "Este es el inicio de la historia; no hay contexto previo."
	}
	startIdx := max(0, idx-historyRecentChapters)
	parts := []string{}
	if idx > 0 {
		parts = append(parts, formatCheckpointHistory(state, state.Chapters[startIdx].Num, historyPromptRunes/2, lang))
	}
	for i := startIdx; i < idx; i++ {
		ch := state.Chapters[i]
		// Only chapters with real prose and a generated summary describe what
		// actually happened. Pending/review chapters carry planned outlines,
		// which belong to the future-constraint channel, never to the history.
		if !isDraftedChapter(ch) || strings.TrimSpace(ch.Summary) == "" {
			continue
		}
		tag := statusTag(ch, lang)
		if en {
			parts = append(parts, fmt.Sprintf("[Chapter %d summary]%s: %s", ch.Num, tag, ch.Summary))
		} else {
			parts = append(parts, fmt.Sprintf("[Resumen del capítulo %d]%s: %s", ch.Num, tag, ch.Summary))
		}
	}
	history := boundedHistoryFallback(parts, historyPromptRunes)
	if history == "" {
		if idx <= 0 {
			return emptyText
		}
		// Chapters exist before this one but none has a usable summary yet.
		// Say so explicitly instead of silently feeding the model an empty or
		// opening-of-the-story history.
		if en {
			return fmt.Sprintf("(No verified summaries exist for the %d earlier chapter(s); rely on the memory entries and the chapter arc constraints below.)", idx)
		}
		return fmt.Sprintf("(No existen resúmenes verificados de los %d capítulo(s) anteriores; apóyate en las entradas de memoria y en las restricciones del arco de capítulos indicadas abajo.)", idx)
	}
	return header + history
}

// buildCharacterContextForLang retrieves chapter-relevant original settings.
func buildCharacterContextForLang(settings *ProjectSettings, ch ChapterState, lang string) string {
	snapshot := settingsAtChapter(settings, ch.Num)
	selected, _ := retrieveSettings(snapshot, chapterKnowledgeQuery(ch), settingsContextRunes)
	selected.Worldview, selected.Organizations = nil, nil
	data, _ := json.Marshal(settingsEntities(selected))
	derived := buildOutlineDerivedCharacterContext(ch, snapshot, lang)
	if utf8.RuneCountInString(derived) > 1000 {
		derived = ""
	}
	return knowledgeSelectionNotice(lang) + string(data) + "\n" + derived
}

func buildWorldviewContextForLang(settings *ProjectSettings, chapterOutline, lang string) string {
	selected, _ := retrieveSettings(settings, chapterOutline, settingsContextRunes)
	selected.Characters, selected.Relations = nil, nil
	data, _ := json.Marshal(settingsEntities(selected))
	return knowledgeSelectionNotice(lang) + string(data)
}

func chapterWorldview(settings *ProjectSettings, ch ChapterState, lang string) string {
	return buildWorldviewContextForLang(settingsAtChapter(settings, ch.Num), chapterKnowledgeQuery(ch), lang)
}

func buildChapterSettingsContexts(settings *ProjectSettings, ch ChapterState, lang string) (string, string) {
	snapshot := settingsAtChapter(settings, ch.Num)
	selected, _ := retrieveSettings(snapshot, chapterKnowledgeQuery(ch), settingsContextRunes)
	characters := *selected
	characters.Worldview, characters.Organizations = nil, nil
	world := *selected
	world.Characters, world.Relations = nil, nil
	charData, _ := json.Marshal(settingsEntities(&characters))
	worldData, _ := json.Marshal(settingsEntities(&world))
	derived := buildOutlineDerivedCharacterContext(ch, snapshot, lang)
	if utf8.RuneCountInString(derived) > 1000 {
		derived = ""
	}
	notice := knowledgeSelectionNotice(lang)
	return notice + string(charData) + "\n" + derived, notice + string(worldData)
}

// An explicit notice prevents a retrieved subset being mistaken for the full registry.
func knowledgeSelectionNotice(lang string) string {
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return "\n[Retrieved original records; this is a budgeted subset, not the complete registry. Missing records do not imply absence of a fact/entity. Do not infer new identities or overwrite unseen fields. Evidence snippets may be excerpts. Worldview knowledge entries are rules of this novel, including fictional rules; do not replace them with real-world assumptions.]\n"
	}
	return "\n[Registros originales recuperados: es un subconjunto presupuestado, no el registro completo. La ausencia de un registro no implica la ausencia de un hecho o entidad. No infieras identidades nuevas ni sobrescribas campos no visibles. Los fragmentos de evidencia pueden ser extractos. Las entradas de conocimiento del mundo son reglas de esta novela, incluidas reglas ficticias; no las reemplaces con suposiciones del mundo real.]\n"
}

// The optional query includes freshly generated prose during fact checking.
func buildMemoryForLang(state *Progress, idx int, lang string, query ...string) string {
	if idx < 0 || idx >= len(state.Chapters) || len(state.MemoryEntries) == 0 {
		return ""
	}
	ch := state.Chapters[idx]
	if len(query) > 0 {
		ch.Content = query[0]
	}
	budget := memoryContextRunes
	if state.MemoryMaxTokens > 0 {
		budget = min(budget, state.MemoryMaxTokens*2/3)
	}
	notice := knowledgeSelectionNotice(lang)
	remaining := budget - utf8.RuneCountInString(notice) - 2
	if remaining <= 0 {
		return ""
	}
	entries := retrieveMemories(state, ch, remaining, true)
	data, _ := json.Marshal(entries)
	return notice + string(data)
}

// formatMemoryForUpdatePrompt renders the existing memory list for the memory update prompt.
func formatMemoryForUpdatePrompt(entries []MemoryEntry, lang string) string {
	if len(entries) == 0 {
		if i18n.NormalizeLanguage(lang) == i18n.LangEN {
			return "(empty — no memories yet)"
		}
		return "(empty——nomemory)"
	}
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	var sb strings.Builder
	for _, m := range entries {
		if en {
			sb.WriteString(fmt.Sprintf("#%d [%s]: %s\n", m.ID, m.Category, m.Content))
		} else {
			sb.WriteString(fmt.Sprintf("#%d [%s]: %s\n", m.ID, m.Category, m.Content))
		}
	}
	return sb.String()
}

// formatActiveForeshadowsForChapterLang renders the "active foreshadows" block in the requested language.
func formatActiveForeshadowsForChapterLang(foreshadows []Foreshadow, chapterNum int, lang string) string {
	var active []Foreshadow
	var overdue []Foreshadow

	for _, fs := range foreshadows {
		if fs.Status == ForeshadowPlanted || fs.Status == ForeshadowProgressing {
			active = append(active, fs)
			if fs.TargetChapter > 0 && chapterNum >= fs.TargetChapter {
				overdue = append(overdue, fs)
			}
		}
	}
	sort.SliceStable(active, func(i, j int) bool {
		due := func(fs Foreshadow) int {
			if fs.TargetChapter <= 0 {
				return 1 << 30
			}
			return fs.TargetChapter
		}
		return due(active[i]) < due(active[j])
	})
	if len(active) == 0 {
		return ""
	}

	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	var sb strings.Builder
	if en {
		sb.WriteString("[Active foreshadows (you must advance or pay them off when writing)]\n")
	} else {
		sb.WriteString("[active (during writing these must be advanced or resolved)]\n")
	}

	for _, fs := range active {
		if en {
			sb.WriteString(fmt.Sprintf("#%d \"%s\" [planted in chapter %d", fs.ID, fs.Name, fs.PlantChapter))
			if fs.TargetChapter > 0 {
				sb.WriteString(fmt.Sprintf(", expected payoff chapter %d", fs.TargetChapter))
			}
			sb.WriteString("]\n")
			sb.WriteString(fmt.Sprintf("   Description: %s\n", fs.Description))
		} else {
			sb.WriteString(fmt.Sprintf("#%d \"%s\" [#%dchapter", fs.ID, fs.Name, fs.PlantChapter))
			if fs.TargetChapter > 0 {
				sb.WriteString(fmt.Sprintf(", count#%dchapterreturncollect", fs.TargetChapter))
			}
			sb.WriteString("]\n")
			sb.WriteString(fmt.Sprintf(" : %s\n", fs.Description))
		}

		if len(fs.Events) > 0 {
			events := fs.Events[max(0, len(fs.Events)-3):]
			if en {
				sb.WriteString("   Progress so far:\n")
				for _, ev := range events {
					sb.WriteString(fmt.Sprintf("   - Chapter %d: %s\n", ev.Chapter, ev.Note))
				}
			} else {
				sb.WriteString(" alreadyhasadvance:\n")
				for _, ev := range events {
					sb.WriteString(fmt.Sprintf(" - #%dchapter: %s\n", ev.Chapter, ev.Note))
				}
			}
		}

		isOverdue := false
		for _, od := range overdue {
			if od.ID == fs.ID {
				isOverdue = true
				break
			}
		}

		if isOverdue {
			if en {
				sb.WriteString(fmt.Sprintf("   ⚠️ This foreshadow is past its expected payoff chapter (%d); this chapter should prioritise paying it off.\n", fs.TargetChapter))
			} else {
				sb.WriteString(fmt.Sprintf("   ⚠️ Esta promesa narrativa ya superó su capítulo de pago esperado (#%d); este capítulo debería priorizar resolverla.\n", fs.TargetChapter))
			}
		} else if fs.TargetChapter > 0 && chapterNum >= fs.TargetChapter-2 {
			if en {
				sb.WriteString(fmt.Sprintf("   → Approaching the expected payoff (chapter %d); start closing it.\n", fs.TargetChapter))
			} else {
				sb.WriteString(fmt.Sprintf("   → Se acerca el pago esperado (capítulo #%d); empieza a cerrarla.\n", fs.TargetChapter))
			}
		}

		sb.WriteString("\n")
	}

	return truncateRunesExact(sb.String(), 6000)
}

// formatForeshadowsForPromptLang renders the foreshadow list given to the update tracker.
func formatForeshadowsForPromptLang(foreshadows []Foreshadow, lang string) string {
	if len(foreshadows) == 0 {
		if i18n.NormalizeLanguage(lang) == i18n.LangEN {
			return "(none)"
		}
		return "no"
	}

	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	var sb strings.Builder
	for _, fs := range foreshadows {
		if fs.Status == ForeshadowResolved || fs.Status == ForeshadowAbandoned {
			continue
		}
		sb.WriteString(fmt.Sprintf("#%d [%s] %s\n", fs.ID, fs.Status, fs.Name))
		if en {
			sb.WriteString(fmt.Sprintf("   Description: %s\n", fs.Description))
			sb.WriteString(fmt.Sprintf("   Planted at: chapter %d", fs.PlantChapter))
			if fs.TargetChapter > 0 {
				sb.WriteString(fmt.Sprintf(", expected payoff: chapter %d", fs.TargetChapter))
			}
		} else {
			sb.WriteString(fmt.Sprintf(" : %s\n", fs.Description))
			sb.WriteString(fmt.Sprintf(" in: #%dchapter", fs.PlantChapter))
			if fs.TargetChapter > 0 {
				sb.WriteString(fmt.Sprintf(", resolución prevista: capítulo #%d", fs.TargetChapter))
			}
		}
		sb.WriteString("\n")

		if len(fs.Events) > 0 {
			events := fs.Events[max(0, len(fs.Events)-3):]
			if en {
				sb.WriteString("   Progress so far:\n")
				for _, ev := range events {
					sb.WriteString(fmt.Sprintf("   - Chapter %d: %s\n", ev.Chapter, ev.Note))
				}
			} else {
				sb.WriteString(" alreadyhasadvance:\n")
				for _, ev := range events {
					sb.WriteString(fmt.Sprintf(" - #%dchapter: %s\n", ev.Chapter, ev.Note))
				}
			}
		}

		if fs.Resolution != "" {
			if en {
				sb.WriteString(fmt.Sprintf("   Resolution: %s\n", fs.Resolution))
			} else {
				sb.WriteString(fmt.Sprintf(" returncollectsquare: %s\n", fs.Resolution))
			}
		}

		sb.WriteString("\n")
	}

	if sb.Len() == 0 {
		if en {
			return "(none)"
		}
		return "no"
	}
	return truncateRunesExact(sb.String(), 6000)
}

func BatchSynopses(state *Progress, lang string) string {
	var out strings.Builder
	for _, b := range state.OutlineBatches {
		out.WriteString(endingPrompt(b, 0, lang))
		if i18n.NormalizeLanguage(lang) == i18n.LangEN {
			fmt.Fprintf(&out, "[Batch %d, chapters %d–%d]\n%s\n\n", b.ID, b.StartCh, b.EndCh, b.Synopsis)
		} else {
			fmt.Fprintf(&out, "[batchtimes %d, # %d–%d chapterbigprobability]\n%s\n\n", b.ID, b.StartCh, b.EndCh, b.Synopsis)
		}
	}
	return out.String()
}

func memoryLinkPrompt(lang string) string {
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return "\nRequired output override: return JSON {\"new_memories\":[{\"id\":0,\"content\":\"fact\",\"category\":\"character|location|item|event|promise|other\",\"block_ids\":[1]}]}. Extract all consistency-critical facts, including those in the outline. Link each fact to ALL relevant blocks of THIS chapter using the supplied block IDs, not paragraph positions. For an existing fact reuse its exact ID and content; use id=0 only for a new fact. Include existing facts mentioned again. Do not delete, merge or alter existing facts to meet a token budget. Return an empty array only when no facts apply. Block evidence:\n"
	}
	return "\nOutput format: return JSON {\"new_memories\":[{\"id\":0,\"content\":\"\",\"category\":\"character|location|item|event|promise|other\",\"block_ids\":[1]}]}. Extract every key fact, including those already present. Attach to each memory the block IDs of the passages in this chapter that evidence it (real Block IDs from below, not passage ordinals). For existing memories keep their ID and content verbatim; use id=0 only for genuinely new ones. Do not repeat what this chapter already has too much. Never fabricate for token savings; merge duplicates rather than both keeping them. Return an empty array only when nothing qualifies. Evidence passages: \n"
}

func memoryRetryPrompt(lang string, err error) string {
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return "\nThe previous response failed validation: " + err.Error() + ". Return the complete corrected JSON. Only reuse IDs from the supplied existing memories and copy their content exactly. For new or changed facts use id=0. Do not invent IDs or merge existing facts.\n"
	}
	return "\nPrevious attempt failed: " + err.Error() + ". Please return valid JSON. Only reference existing memory IDs, copying their content word for word; use id=0 for anything new. Never invent IDs or merge with existing entries. \n"
}

func settingUpdatePrompt(lang string) string {
	schema := "\nJSON: {\"changes\":[{\"kind\":\"characters|worldview|organizations|relations\",\"entity\":{\"id\":\"existing ID, or empty for new\"},\"block_id\":1,\"evolution\":false,\"conflict\":false,\"reason\":\"evidence explanation\"}]}\n"
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		schema += "Keep fields concise: describe enduring attributes and current state, not a chapter-by-chapter plot log. Preserve existing independent constraints when updating a field. Do not append paraphrases of known facts. Event history and original evidence are stored separately.\n"
	} else {
		schema += "Mantén los campos concisos: describe atributos duraderos y estado actual, no un registro capítulo a capítulo. Al actualizar un campo, conserva las restricciones independientes existentes. No añadas paráfrasis de hechos conocidos. El historial de eventos y la evidencia original se almacenan por separado.\n"
	}
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return "Extract setting changes evidenced in the accepted chapter blocks. Reuse existing entity IDs; never guess identities. Supply only changed fields for existing entities, complete required fields for new entities. Mark contradictions, uncertain identities or inferences conflict=true. evolution=true only for explicit chronological developments (e.g. allies becoming enemies), never for factual contradictions. No deletions. New characters: name; worldview: name/category/description; organizations: name/type/description/members; relations: source_id/source_type/target_id/target_type/label (types character/worldview/organization). For new entities use local IDs starting with $ (e.g. $alice); list new characters/worldview first, organizations next, relations last. References may use existing IDs or earlier unambiguous new $IDs. Never link uncertain new entities. The server allocates permanent IDs. Use exact block IDs as evidence. Return changes:[] when nothing changes. Below: existing entities, then chapter blocks." + schema
	}
	return "Extract setting changes evidenced in the accepted chapter blocks. Reuse existing entity IDs, supplying only changed fields; provide required fields for new entities. Copy names verbatim. Mark contradictions, ambiguous identities or inference as conflict=true. Set evolution=true only for explicit chronological developments (e.g. allies becoming enemies), never for factual contradictions. No deletions. New characters: name; worldview: name/category/description; organizations: name/type/description/members; relations: source_id/source_type/target_id/target_type/label (types character/worldview/organization). For new entities use local IDs starting with $ (e.g. $alice); list characters and worldview first, then organizations, then relations; references may use existing IDs or earlier unambiguous new $IDs; never link entities that are still ambiguous. The server allocates permanent IDs. Use real evidence block IDs in block_id. Return changes:[] when there are none. Below: existing settings, then this chapter's blocks. " + schema
}

func BookSynopsis(cfg *config.Config, state *Progress) string {
	return BatchSynopses(state, cfg.Language)
}

func ChapterSynopsis(cfg *config.Config, state *Progress, num int) string {
	for _, b := range state.OutlineBatches {
		if num >= b.StartCh && num <= b.EndCh {
			if i18n.NormalizeLanguage(cfg.Language) == i18n.LangEN {
				return fmt.Sprintf("[Batch synopsis, chapters %d–%d; follow this chapter's outline without advancing later events]\n%s", b.StartCh, b.EndCh, b.Synopsis)
			}
			return fmt.Sprintf("[Resumen del lote, capítulos %d–%d; sigue solo el outline de este capítulo sin avanzar eventos posteriores]\n%s", b.StartCh, b.EndCh, b.Synopsis)
		}
	}
	return ""
}

func batchScopeTemplate(lang string) string {
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return "\n[Required batch synopsis: chapters {{.StartNum}}–{{.EndNum}}]\n{{.OutlineSynopsis}}\n[Long-term direction, optional]\n{{.LongTermDirection}}\nGenerate exactly {{.NewChapterCount}} consecutive chapters in this range, constrained by this batch synopsis. Start the story when there are no existing chapters; otherwise continue the existing plot. Do not treat this batch as the whole book. When the supplied novel title is blank, include an inferred non-empty top-level JSON field named title."
	} else {
		return "\n[Resumen obligatorio del lote: capítulos {{.StartNum}}–{{.EndNum}}]\n{{.OutlineSynopsis}}\n[Dirección a largo plazo, opcional]\n{{.LongTermDirection}}\nGenera exactamente {{.NewChapterCount}} capítulos consecutivos dentro de este rango, ceñidos al resumen del lote. Si no hay capítulos existentes, inicia la historia; si los hay, continúa la trama existente. No trates este lote como el libro completo. Cuando el título de la novela proporcionado esté en blanco, incluye un campo JSON de nivel superior llamado title con un valor inferido no vacío."
	}
}

func endingPrompt(b OutlineBatch, num int, lang string) string {
	if b.EndingIntent == "" && !b.PlannedFinal {
		return ""
	}
	// The "CLOSES THE BOOK" distribution instructions are for the FINAL chapter
	// only. Injected verbatim into earlier chapters of the same batch they made
	// the writer believe the book ends now — one cause of cross-chapter plot
	// confusion (rushing resolutions or raiding later chapters' material). For
	// non-final chapters emit a short serialization reminder instead.
	// num == 0 means batch PLANNING (outline generation): there is no "current
	// chapter" yet, so the full ending block must be included regardless.
	isFinalChapter := num == 0 || num >= b.EndCh
	requirements := truncateToRunes(strings.TrimSpace(b.EndingRequirements), maxEndingRequirementsRunes)
	serializationNote := func() string {
		if i18n.NormalizeLanguage(lang) == i18n.LangEN {
			return "\n[Ending control] Continue serialization. Close the local beat while leaving a natural next step; do not finish the entire book.\n"
		}
		return "\n[Control de final] Continúa la serialización. Cierra el conflicto local dejando un paso natural hacia el siguiente; no termines todo el libro.\n"
	}
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		if !b.PlannedFinal || !isFinalChapter {
			return serializationNote()
		}
		s := fmt.Sprintf("\n[Ending control — this batch CLOSES THE BOOK at chapter %d]\n"+
			"Distribute the remaining narrative across exactly these chapters so the story reaches its conclusion only in chapter %d. Never resolve the main conflict earlier.\n"+
			"- Early chapters of this batch: escalate the central conflict and tighten unresolved threads; plant or deepen payoffs for anything still open.\n"+
			"- Penultimate chapter(s): bring all major character arcs and subplots to their decisive moment; NO final resolution yet.\n"+
			"- Final chapter (%d): resolve the main conflict, pay off every significant planted thread introduced in earlier outlines, and give the principal characters their closing beats.\n"+
			"Current chapter: %d (0 means batch planning). In the final chapter, the ending requirements below override generic cliffhanger rules.\n",
			b.EndCh, b.EndCh, b.EndCh, num)
		if b.EndingIntent == "sequel" {
			s += "Complete this book's arc and preserve a concrete entry point for a sequel.\n"
		}
		if b.EndingStyle == "open" {
			s += "Open ending: establish the main outcome, leaving the future or thematic interpretation open.\n"
		} else if b.EndingStyle != "custom" {
			s += "Closed ending: settle the main conflict and principal character arcs.\n"
		}
		if requirements != "" {
			s += "Author's ending requirements (must be honored):\n" + requirements + "\n"
		}
		return s
	}
	if !b.PlannedFinal || !isFinalChapter {
		return serializationNote()
	}
	s := fmt.Sprintf("\n[Control de final — este lote CIERRA EL LIBRO en el capítulo %d]\n"+
		"Distribuye el desenlace exactamente entre estos capítulos de modo que la historia concluya solo en el capítulo %d. Nunca resuelvas el conflicto principal antes.\n"+
		"- Capítulos iniciales del lote: intensifica el conflicto central y aprieta los hilos abiertos; siembra o profundiza pagos de todo lo pendiente.\n"+
		"- Penúltimos capítulos: lleva los arcos principales y subtramas a su momento decisivo; SIN resolución final todavía.\n"+
		"- Capítulo final (%d): resuelve el conflicto principal, paga cada hilo significativo sembrado en outlines anteriores y entrega el cierre de los personajes principales.\n"+
		"Capítulo actual: %d (0 significa planificación del lote). En el capítulo final, los requisitos de cierre prevalecen sobre las reglas genéricas de gancho.\n",
		b.EndCh, b.EndCh, b.EndCh, num)
	if b.EndingIntent == "sequel" {
		s += "Completa el arco de este libro y deja un punto de entrada concreto para una secuela.\n"
	}
	if b.EndingStyle == "open" {
		s += "Final abierto: establece el resultado principal dejando el futuro o la interpretación temática abiertos.\n"
	} else if b.EndingStyle != "custom" {
		s += "Final cerrado: resuelve el conflicto principal y los arcos de los personajes principales.\n"
	}
	if requirements != "" {
		s += "Requisitos de final del autor (deben cumplirse):\n" + requirements + "\n"
	}
	return s
}
