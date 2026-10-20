package story

// Chapter prose is exposed as an ordered list of editable
// blocks (one per natural paragraph). In-memory ch.Content stays the single
// source of truth for all AI flows; blocks are (re)derived from content at
// save time with stable IDs, and block edits rebuild content from blocks.

import (
	"context"
	"fmt"
	"path/filepath"
	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/prose"
	"showmethestory/internal/sse"
	"strings"
)

const (
	BlockParagraph  = "paragraph"
	BlockDialogue   = "dialogue"
	BlockSceneBreak = "scene_break"
)

type Block struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
	Text string `json:"text"`
	// ScenePos is the 0-based index of the scene this block belongs to (-1
	// when the chapter has no scene bookkeeping). The viewer uses it to draw
	// a discreet marker whenever the scene changes between consecutive blocks.
	ScenePos int `json:"scene_pos"`
	// SceneTitle / TropeLabel describe the owning scene and the trope unit
	// inside it, for hover tooltips on the marker chip.
	SceneTitle string `json:"scene_title,omitempty"`
	TropeLabel string `json:"trope_label,omitempty"`
	TropeIndex int    `json:"trope_index,omitempty"`
	TropeCount int    `json:"trope_count,omitempty"`
}

// splitBlockParagraphs splits content the same way segment revision does:
// blank-line paragraphs first, single-newline fallback. Returns paragraphs
// and the separator to use when rebuilding.
func splitBlockParagraphs(content string) ([]string, string) {
	sep := "\n\n"
	paras := strings.Split(content, sep)
	if len(paras) <= 1 && strings.Contains(content, "\n") {
		sep = "\n"
		paras = strings.Split(content, sep)
	}
	out := make([]string, 0, len(paras))
	for _, p := range paras {
		if strings.TrimSpace(p) == "" {
			continue
		}
		out = append(out, p)
	}
	return out, sep
}

// classifyBlock is a display hint only; misclassification is harmless.
func classifyBlock(text string) string {
	t := strings.TrimSpace(text)
	switch t {
	case "---", "----", "", "***", "⁂", "* * *":
		return BlockSceneBreak
	}
	switch {
	case strings.HasPrefix(t, "“"), strings.HasPrefix(t, "\""), strings.HasPrefix(t, "\""),
		strings.HasPrefix(t, "\""), strings.HasPrefix(t, "'"), strings.HasPrefix(t, "‘"):
		return BlockDialogue
	}
	return BlockParagraph
}

// SyncChapterBlocks re-derives ch.Blocks from ch.Content, preserving the IDs
// of blocks whose text is unchanged (first-unused exact match, in order).
//
// Trope-aware mode: when the chapter carries a scene plan with at least two
// scenes whose paragraph spans map reliably onto the content, blocks are
// grouped PER TROPE UNIT inside each scene (dialogue exchange, action run,
// interiority beat…) — finer than one-block-per-scene, sturdier than
// one-block-per-paragraph. Each block records its owning scene (ScenePos /
// SceneTitle) and trope position (TropeLabel / TropeIndex / TropeCount) so
// the viewer can render the discreet scene marker and its tooltip, and so
// surgical critic repairs can splice a single trope unit. Chapters without
// such bookkeeping keep the classic one-block-per-paragraph split.
func SyncChapterBlocks(ch *ChapterState) {
	if syncSceneBlocks(ch) {
		return
	}
	paras, sep := splitBlockParagraphs(ch.Content)
	ch.BlockSep = sep

	used := make([]bool, len(ch.Blocks))
	next := ch.NextBlockID
	if next <= 0 {
		next = 1
	}
	blocks := make([]Block, 0, len(paras))
	for _, p := range paras {
		id := 0
		for i, old := range ch.Blocks {
			if !used[i] && old.Text == p {
				used[i] = true
				id = old.ID
				break
			}
		}
		if id == 0 {
			id = next
			next++
		}
		blocks = append(blocks, Block{ID: id, Type: classifyBlock(p), Text: p, ScenePos: -1})
	}
	ch.Blocks = blocks
	ch.NextBlockID = next
}

// syncSceneBlocks rebuilds ch.Blocks at TROPE granularity: one story-block per
// trope unit of each planned scene (dialogue exchange, action run, interiority
// beat…), which sits between "one block per blank-line paragraph" (too atomic)
// and "one block per scene" (too coarse for surgical critic repairs). Scene
// membership is recovered from each scene's recorded TropeRanges so the viewer
// can render a discreet marker between scenes while blocks stay small enough
// for single-unit rewrites. Returns false when the chapter has no usable
// per-scene bookkeeping (caller falls back to paragraph blocks). The scene
// boundaries must tile the whole content; otherwise derived ranges are stale
// (e.g. after a manual edit) and paragraph mode is safer.
func syncSceneBlocks(ch *ChapterState) bool {
	if ch.ScenePlan == nil || len(ch.ScenePlan.Scenes) < 2 {
		return false
	}
	paras, sep := splitBlockParagraphs(ch.Content)
	if len(paras) == 0 {
		return false
	}
	spans := sceneParaSpans(ch, paras)
	if spans == nil {
		return false
	}
	used := make([]bool, len(ch.Blocks))
	next := ch.NextBlockID
	if next <= 0 {
		next = 1
	}
	blocks := make([]Block, 0, len(spans)*2)
	for pos, sp := range spans {
		sceneParas := paras[sp[0]:sp[1]]
		units := sceneTropeUnitSpans(ch, pos, sceneParas)
		sc := ch.ScenePlan.Scenes[pos]
		title := sceneDisplayTitle(sc, pos)
		labels := sceneTropeLabels(ch, pos, units, sceneParas)
		for ui, u := range units {
			start, end := sp[0]+u[0], sp[0]+u[1]
			if end > len(paras) {
				end = len(paras)
			}
			if start >= end {
				continue
			}
			text := strings.Join(paras[start:end], sep)
			id := 0
			for i, old := range ch.Blocks {
				if !used[i] && old.Text == text {
					used[i] = true
					id = old.ID
					break
				}
			}
			if id == 0 {
				id = next
				next++
			}
			typ := classifyTropeBlock(text)
			label := ""
			if ui < len(labels) {
				label = labels[ui]
			}
			blocks = append(blocks, Block{
				ID: id, Type: typ, Text: text,
				ScenePos: pos, SceneTitle: title,
				TropeLabel: label, TropeIndex: ui + 1, TropeCount: len(units),
			})
		}
	}
	if len(blocks) == 0 {
		return false
	}
	ch.Blocks = blocks
	ch.NextBlockID = next
	ch.BlockSep = sep
	return true
}

// classifyTropeBlock labels a trope-level block for display hints. Multi-
// paragraph trope runs keep the legacy "scene" type only when they contain a
// hard break marker; otherwise dialogue vs paragraph classification applies.
func classifyTropeBlock(text string) string {
	if strings.Contains(text, "\n\n") && looksLikeTropeRun(text) {
		return "trope"
	}
	return classifyBlock(text)
}

func looksLikeTropeRun(text string) bool {
	n := strings.Count(text, "\n\n") + 1
	return n >= 2
}

// sceneTropeUnitSpans returns [start,end) paragraph spans (scene-local) for
// every trope unit of the scene at index pos. It prefers the segmentation
// recorded by the sequential writer (Scene.TropeUnits/TropeRanges) when it is
// consistent with the current paragraphs — i.e. the same number of units and
// every recorded ParaEnd within range — and otherwise re-derives the tropes
// live from the scene prose via splitTropes (keeps blocks fresh after manual
// edits inside a scene). Always returns at least one span tiling the scene.
func sceneTropeUnitSpans(ch *ChapterState, pos int, sceneParas []string) [][2]int {
	fallback := [][2]int{{0, len(sceneParas)}}
	if ch.ScenePlan == nil || pos >= len(ch.ScenePlan.Scenes) {
		return fallback
	}
	sc := ch.ScenePlan.Scenes[pos]
	if len(sc.TropeUnits) > 0 {
		ok := true
		maxEnd := 0
		for _, u := range sc.TropeUnits {
			if u.ParaStart < 0 || u.ParaEnd <= u.ParaStart || u.ParaEnd > len(sceneParas) {
				ok = false
				break
			}
			if u.ParaEnd > maxEnd {
				maxEnd = u.ParaEnd
			}
		}
		// Recorded ranges must tile the scene exactly; if the prose changed
		// length the bookkeeping is stale and we re-segment instead.
		if ok && maxEnd == len(sceneParas) {
			spans := make([][2]int, 0, len(sc.TropeUnits))
			for _, u := range sc.TropeUnits {
				spans = append(spans, [2]int{u.ParaStart, u.ParaEnd})
			}
			return spans
		}
	}
	// Re-derive tropes from the scene prose currently on disk.
	sceneText := strings.Join(sceneParas, "\n\n")
	if _, ranges := splitTropes(sceneText); len(ranges) > 0 {
		return ranges
	}
	return fallback
}

// sceneDisplayTitle returns a short human label for the scene marker tooltip.
func sceneDisplayTitle(sc Scene, pos int) string {
	if t := strings.TrimSpace(sc.Title); t != "" {
		return t
	}
	if d := strings.TrimSpace(sc.Description); d != "" {
		if rs := []rune(d); len(rs) > 60 {
			return string(rs[:60]) + "…"
		}
		return d
	}
	return fmt.Sprintf("Scene %d", pos+1)
}

// sceneTropeLabels maps each trope span of a scene to its trope label
// (opening/tension/dialogue/…). Recorded unit labels are used when they align
// 1:1 with the spans; otherwise labels are re-derived deterministically from
// the span text so every block carries a meaningful label for the viewer's
// tooltip (same cues as classifyTrope — no LLM call).
func sceneTropeLabels(ch *ChapterState, pos int, spans [][2]int, sceneParas []string) []string {
	out := make([]string, len(spans))
	var units []TropeUnit
	if ch.ScenePlan != nil && pos < len(ch.ScenePlan.Scenes) {
		units = ch.ScenePlan.Scenes[pos].TropeUnits
	}
	if len(units) == len(spans) {
		for i, u := range units {
			out[i] = string(u.Label)
		}
		return out
	}
	// Spans were re-derived live (or units are missing): classify each span's
	// own text positionally within the scene.
	for i, sp := range spans {
		lo, hi := sp[0], sp[1]
		if lo < 0 {
			lo = 0
		}
		if hi > len(sceneParas) {
			hi = len(sceneParas)
		}
		if lo >= hi {
			continue
		}
		text := strings.Join(sceneParas[lo:hi], "\n\n")
		out[i] = string(classifyTrope(text, i == 0, i == len(spans)-1))
	}
	return out
}

// sceneParaSpans returns [start,end) paragraph spans for every planned scene.
// It prefers the EXACT absolute spans recorded by the sequential writer
// (Scene.ParaStart/ParaEnd, plus per-scene trope units that tile each span);
// when that bookkeeping is missing or stale (e.g. after a manual edit changed
// the paragraph count) it falls back to distributing paragraphs proportionally
// to word budgets. Returns nil when even the fallback mapping is unreliable.
func sceneParaSpans(ch *ChapterState, paras []string) [][2]int {
	plan := ch.ScenePlan
	if spans := recordedSceneParaSpans(ch, paras); spans != nil {
		return spans
	}
	totalBudget := 0
	for _, sc := range plan.Scenes {
		if sc.WordsBudget < 1 {
			return nil // degenerate budget → can't map reliably
		}
		totalBudget += sc.WordsBudget
	}
	if totalBudget <= 0 || len(plan.Scenes) > len(paras) {
		return nil
	}
	totalWords := 0
	counts := make([]int, len(paras))
	for i, p := range paras {
		counts[i] = prose.CountProseUnits(p)
		totalWords += counts[i]
	}
	if totalWords == 0 {
		return nil
	}
	// Distribute paragraphs proportionally to budgets; each boundary snaps to
	// the paragraph whose cumulative words pass the cumulative budget share.
	spans := make([][2]int, 0, len(plan.Scenes))
	cumBudget := 0
	start := 0
	for pos, sc := range plan.Scenes {
		cumBudget += sc.WordsBudget
		end := len(paras)
		if pos != len(plan.Scenes)-1 {
			want := float64(cumBudget) / float64(totalBudget) * float64(totalWords)
			acc := 0
			end = start
			for i := start; i < len(paras); i++ {
				acc += counts[i]
				end = i + 1
				if float64(acc) >= want {
					break
				}
			}
			if end > len(paras)-1 {
				end = len(paras) - 1 // always leave at least one paragraph for later scenes
			}
		}
		if end <= start {
			return nil
		}
		spans = append(spans, [2]int{start, end})
		start = end
	}
	if start != len(paras) {
		spans[len(spans)-1][1] = len(paras)
	}
	return spans
}

// recordedSceneParaSpans rebuilds the exact scene spans the sequential writer
// stored on Scene.ParaStart/ParaEnd. The spans are trusted only when they tile
// the whole content with no gaps or overlaps AND every scene's recorded trope
// units tile its own span (scene-local ParaStart/ParaEnd within range, last
// unit ending at the scene end). Anything else means the bookkeeping is stale
// (manual edit, older data) and the caller should fall back to budget-based
// proportional mapping. Returns nil when unusable.
func recordedSceneParaSpans(ch *ChapterState, paras []string) [][2]int {
	plan := ch.ScenePlan
	if plan == nil || len(plan.Scenes) < 2 {
		return nil
	}
	spans := make([][2]int, 0, len(plan.Scenes))
	for _, sc := range plan.Scenes {
		if sc.ParaStart <= 0 && sc.ParaEnd <= 0 {
			return nil // never recorded (older data)
		}
		if sc.ParaStart < 0 || sc.ParaEnd <= sc.ParaStart || sc.ParaEnd > len(paras) {
			return nil
		}
		if len(spans) > 0 && sc.ParaStart != spans[len(spans)-1][1] {
			return nil // gap or overlap → stale
		}
		// Trope units must tile this scene's span exactly.
		if len(sc.TropeUnits) > 0 {
			maxEnd := 0
			for _, u := range sc.TropeUnits {
				if u.ParaStart < 0 || u.ParaEnd <= u.ParaStart || u.ParaEnd > sc.ParaEnd-sc.ParaStart {
					return nil
				}
				if u.ParaEnd > maxEnd {
					maxEnd = u.ParaEnd
				}
			}
			if maxEnd != sc.ParaEnd-sc.ParaStart {
				return nil
			}
		}

		spans = append(spans, [2]int{sc.ParaStart, sc.ParaEnd})
	}
	if spans[len(spans)-1][1] != len(paras) {
		return nil // trailing paragraphs unaccounted for
	}
	return spans
}

// rebuildContentFromBlocks makes ch.Content match ch.Blocks after a block edit.
func rebuildContentFromBlocks(ch *ChapterState) {
	sep := ch.BlockSep
	if sep == "" {
		sep = "\n\n"
	}
	texts := make([]string, len(ch.Blocks))
	for i, b := range ch.Blocks {
		texts[i] = b.Text
	}
	ch.Content = strings.Join(texts, sep)
}

func FindBlockIdx(ch *ChapterState, blockID int) int {
	for i, b := range ch.Blocks {
		if b.ID == blockID {
			return i
		}
	}
	return -1
}

var ErrBlockNotFound = fmt.Errorf("block not found")

// UpdateBlock replaces the text of one block and rebuilds chapter content.
func UpdateBlock(ch *ChapterState, blockID int, text string) error {
	i := FindBlockIdx(ch, blockID)
	if i < 0 {
		return ErrBlockNotFound
	}
	ch.Blocks[i].Text = text
	ch.Blocks[i].Type = classifyBlock(text)
	rebuildContentFromBlocks(ch)
	SyncChapterBlocks(ch)
	return nil
}

// DeleteBlock removes one block and rebuilds chapter content.
func DeleteBlock(ch *ChapterState, blockID int) error {
	i := FindBlockIdx(ch, blockID)
	if i < 0 {
		return ErrBlockNotFound
	}
	ch.Blocks = append(ch.Blocks[:i], ch.Blocks[i+1:]...)
	rebuildContentFromBlocks(ch)
	SyncChapterBlocks(ch)
	return nil
}

// InsertBlockAfter inserts a new block after blockID (0 = at the beginning)
// and returns the new block's ID.
func InsertBlockAfter(ch *ChapterState, blockID int, text string) (int, error) {
	pos := 0
	if blockID != 0 {
		i := FindBlockIdx(ch, blockID)
		if i < 0 {
			return 0, ErrBlockNotFound
		}
		pos = i + 1
	}
	if ch.NextBlockID <= 0 {
		ch.NextBlockID = 1
	}
	nb := Block{ID: ch.NextBlockID, Type: classifyBlock(text), Text: text}
	ch.NextBlockID++
	ch.Blocks = append(ch.Blocks[:pos], append([]Block{nb}, ch.Blocks[pos:]...)...)
	rebuildContentFromBlocks(ch)
	SyncChapterBlocks(ch)
	return nb.ID, nil
}

// ReviseBlockAction runs an AI revision scoped to a single block, using the
// ChapterSegmentRevision prompt. The AI reply replaces the block; if it
// contains multiple paragraphs the block is split on the next sync.
func ReviseBlockAction(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, progressPath string, chapterNum, blockID int, feedback string, settings *ProjectSettings, logger *sse.LogBroadcaster) error {
	chapterIdx := FindChapterIdx(state, chapterNum)
	if chapterIdx < 0 {
		return fmt.Errorf("el capítulo #%d no existe", chapterNum)
	}
	ch := &state.Chapters[chapterIdx]
	bi := FindBlockIdx(ch, blockID)
	if bi < 0 {
		return ErrBlockNotFound
	}
	lang := cfg.Language
	original := ch.Blocks[bi].Text

	feedbackForAI := strings.TrimSpace(feedback)
	if feedbackForAI == "" {
		feedbackForAI = i18n.SystemPromptFor(lang, "segment_revision_default_feedback")
	}
	contextChapter := *ch
	contextChapter.Outline += "\n" + feedbackForAI

	userPrompt := config.RenderPrompt(cfg.Prompts.ChapterSegmentRevision, map[string]string{
		"ChapterNum":       fmt.Sprintf("%d", ch.Num),
		"ChapterTitle":     ch.Title,
		"CorePrompt":       state.CorePrompt,
		"HistorySummary":   buildHistorySummaryForLang(state, chapterIdx, lang),
		"WritingStyle":     cfg.Story.WritingStyle,
		"WritingPOV":       cfg.Story.WritingPOV,
		"CharacterContext": buildCharacterContextForLang(settings, contextChapter, lang),
		"WorldviewContext": chapterWorldview(settings, contextChapter, lang),
		"QuotedText":       original,
		"SegmentOriginal":  original,
		"UserFeedback":     feedbackForAI,
	})
	userPrompt += factProtection(state, ch.Num, lang)

	systemPrompt := state.CorePrompt
	if systemPrompt == "" {
		systemPrompt = i18n.SystemPromptFor(lang, "author_default")
	}
	systemPrompt += i18n.SystemPromptFor(lang, "chapter_revision_suffix")

	rawResp := llm.CallAPIWithRetryLog(ctx, apiCfg, systemPrompt, userPrompt, logger)
	if rawResp == "" {
		return fmt.Errorf("block revision API call failed or was canceled")
	}
	newText := strings.TrimSpace(stripChapterMetaProse(rawResp, lang))
	if newText == "" {
		return fmt.Errorf("block returnemptyinside")
	}

	ch.Blocks[bi].Text = newText
	ch.KnowledgeTracked = true
	rebuildContentFromBlocks(ch)
	SyncChapterBlocks(ch)

	SaveChapterMarkdown(filepath.Dir(progressPath), *ch, state.Title)
	if err := SaveProgress(progressPath, state); err != nil {
		return fmt.Errorf("guardar progreso: %w", err)
	}
	logger.SuccessKey("log.block_revised", ch.Num, blockID)
	return nil
}
