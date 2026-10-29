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
	"sort"
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
	// SceneDescription carries the full scene-plan description of the owning
	// scene; the viewer shows it as the tooltip text of the scene-marker chip.
	SceneDescription string `json:"scene_description,omitempty"`
	TropeLabel       string `json:"trope_label,omitempty"`
	TropeIndex       int    `json:"trope_index,omitempty"`
	TropeCount       int    `json:"trope_count,omitempty"`
	// SceneHasMeta states whether the owning scene carries REAL plan metadata
	// (title/description). Blocks built from paragraph fallbacks without a
	// usable scene plan leave it nil, so the reader can tell "no info" apart
	// from "scene exists but is unnamed".
	SceneHasMeta *bool `json:"scene_has_meta,omitempty"`
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

// sceneMetaCollapsed reports whether every block still belongs to one scene
// while the plan now has several. That happens when the scene plan was
// regenerated (or expanded) after the prose had already been tiled: the reader
// then shows a single "Scene 1" chip and hides the other scene boundaries.
func sceneMetaCollapsed(ch *ChapterState) bool {
	// Only complete prose is safe to re-tile: a chapter still being written can
	// legitimately have all its blocks in scene 1 because later scenes have not
	// been drafted yet, and repairing it would mislabel that prose.
	if ch.Status != StatusReview && ch.Status != StatusAccepted {
		return false
	}
	if ch.ScenePlan == nil || len(ch.ScenePlan.Scenes) < 2 || len(ch.Blocks) < 2 {
		return false
	}
	pos := ch.Blocks[0].ScenePos
	for _, b := range ch.Blocks[1:] {
		if b.ScenePos != pos {
			return false
		}
	}
	return true
}

// remapScenePositions reassigns the existing blocks to the repaired scene
// spans without re-tiling the prose: block text, IDs and trope labels stay
// intact, except a block that crosses a scene boundary is split (first part
// keeps the ID). Returns false (leaving the chapter untouched) when a block
// cannot be located in the current prose, because a wrong remap is worse than
// one missing chip.
func remapScenePositions(ch *ChapterState) bool {
	paras, sep := splitBlockParagraphs(ch.Content)
	spans := sceneParaSpans(ch, paras)
	if spans == nil || len(paras) == 0 || len(ch.Blocks) == 0 {
		return false
	}
	// Locate every existing block by the paragraph its text starts with, in
	// order; block i then spans [starts[i], starts[i+1]).
	starts := make([]int, len(ch.Blocks))
	cursor := 0
	for i, b := range ch.Blocks {
		first := strings.Split(b.Text, sep)[0]
		start := -1
		for j := cursor; j < len(paras); j++ {
			if !paraIsBreak(paras[j]) && paras[j] == first {
				start = j
				break
			}
		}
		if start < 0 {
			return false
		}
		starts[i] = start
		cursor = start + 1
	}

	next := ch.NextBlockID
	if next <= 0 {
		next = 1
	}
	blocks := make([]Block, 0, len(ch.Blocks)+len(spans))
	counts := make([]int, len(spans))
	for i, b := range ch.Blocks {
		end := len(paras)
		if i+1 < len(ch.Blocks) {
			end = starts[i+1]
		}
		firstPart := true
		for pos, sp := range spans {
			lo, hi := max(starts[i], sp[0]), min(end, sp[1])
			if lo >= hi {
				continue
			}
			var kept []string
			for _, p := range paras[lo:hi] {
				if !paraIsBreak(p) {
					kept = append(kept, p)
				}
			}
			if len(kept) == 0 {
				continue
			}
			text := strings.Join(kept, sep)
			// A block that crosses a scene boundary is split so every scene
			// owns its own blocks (the chip row needs a block per scene). The
			// first part keeps the ID, later parts get fresh ones — same rule
			// SyncChapterBlocks uses for changed prose.
			id := b.ID
			if !firstPart {
				id = next
				next++
			}
			firstPart = false
			sc := ch.ScenePlan.Scenes[pos]
			metaOK := hasMeaningfulSceneMeta(sc)
			blocks = append(blocks, Block{
				ID: id, Type: classifyTropeBlock(text), Text: text,
				ScenePos: pos, SceneTitle: sceneDisplayTitle(sc, pos),
				SceneDescription: strings.TrimSpace(sc.Description),
				SceneHasMeta: &metaOK, TropeLabel: b.TropeLabel,
			})
			counts[pos]++
		}
	}
	if len(blocks) == 0 {
		return false
	}
	seen := make([]int, len(spans))
	for i := range blocks {
		pos := blocks[i].ScenePos
		seen[pos]++
		blocks[i].TropeIndex = seen[pos]
		blocks[i].TropeCount = counts[pos]
	}
	ch.Blocks = blocks
	ch.NextBlockID = next
	ch.BlockSep = sep
	return true
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

	// No reliable scene plan -> still show TROPE-level blocks whenever the
	// chapter is segmented with blank-line paragraphs: merge runs of
	// consecutive same-mode paragraphs (dialogue vs prose) into beats, exactly
	// like the in-scene segmentation but applied over the whole chapter, so
	// the viewer stays faithful to "blocks are subscenes, not line breaks". The
	// grouping is SKIPPED when it would merge paragraphs that already live in
	// separate blocks (lossless-edit guard) or when the separator is a single
	// newline (that mode intentionally mirrors one block per line).
	// Fresh sync (no previous blocks — e.g. pasted or first-time content):
	// keep the classic one-block-per-paragraph split. Trope merging exists to
	// re-tile prose the writer segmented per scene/unit while protecting
	// existing finer boundaries; collapsing brand-new independent paragraphs
	// just because they share a mode would surprise editors and break ID
	// stability expectations at import time.
	freshSync := len(ch.Blocks) == 0
	var groups [][2]int
	if !freshSync {
		groups = tropeFallbackGroups(ch.Blocks, paras, sep)
	}
	if freshSync || len(groups) == 0 {
		groups = make([][2]int, len(paras))
		for i := range paras {
			groups[i] = [2]int{i, i + 1}
		}
	}

	used := make([]bool, len(ch.Blocks))
	next := ch.NextBlockID
	if next <= 0 {
		next = 1
	}
	blocks := make([]Block, 0, len(groups))
	// Fallback scene map: pseudo-scenes delimited by HARD BREAK markers. Every
	// block's first paragraph resolves to the scene it belongs to (shared id
	// across all groups of one scene → exactly one "Scene N" chip per break),
	// and fbSceneOpen remembers each scene's [lo,hi) para span so trope units
	// can be tiled once per scene with the writer's own splitter.
	fbSceneStart := make([]int, len(paras))
	fbSceneOpen := map[int][2]int{}
	fbUnits := map[int][]string{}      // scene -> trope-unit beat texts (tiled once)
	fbUnitRanges := map[int][][2]int{} // scene -> unit para ranges (scene-local)
	fbLastUnit := map[int]int{}        // scene -> last emitted unit index
	{
		s := 0
		for j := range paras {
			if j > 0 && paraIsBreak(paras[j-1]) {
				s++
			}
			fbSceneStart[j] = s
			if paraIsBreak(paras[j]) {
				continue // a break paragraph opens nothing itself
			}
			if e, ok := fbSceneOpen[s]; ok {
				// Keep the span CONTIGUOUS: trope merging produces several groups
				// per scene; a later group must EXTEND the open span (never reset
				// it), otherwise the [lo,hi) used to tile trope units would be
				// shorter than the scene and unit ranges would misalign.
				if e[1] <= j+1 {
					e[1] = j + 1
					fbSceneOpen[s] = e
				}
			} else {
				fbSceneOpen[s] = [2]int{j, j + 1}
			}
		}
	}
	for _, g := range groups {
		text := strings.Join(paras[g[0]:g[1]], sep)
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
		// Scene/trope bookkeeping for the reader chips. The chapter's own
		// ScenePlan (when present AND aligned with the prose spans) is
		// authoritative: scenes then map to their planned positions and carry
		// real titles / descriptions into the chip tooltips. When no usable plan
		// exists we fall back to pseudo-scenes delimited by HARD BREAK markers
		// ("---") — one chip per real break instead of one per paragraph, which
		// would be noise — and those pseudo-scenes get a synthetic title so the
		// "Scene N" chip still renders (the viewer hides scene chips whose scene
		// carries NO metadata at all). Trope membership inside the scene comes
		// from re-running the SAME splitter the sequential writer uses over the
		// scene's paras, so the trope chips between blocks stay truthful in both
		// modes.
		spos := fbSceneStart[g[0]]
		metaOK := false
		b := Block{ID: id, Type: classifyTropeBlock(text), Text: text, ScenePos: spos, SceneHasMeta: &metaOK}
		var sc Scene
		planAligned := false
		if ch.ScenePlan != nil && spos >= 0 && spos < len(ch.ScenePlan.Scenes) {
			if span, ok := fbSceneOpen[spos]; ok {
				psc := ch.ScenePlan.Scenes[spos]
				if psc.ParaStart == span[0] && psc.ParaEnd == span[1] {
					sc = psc
					metaOK = hasMeaningfulSceneMeta(sc)
					planAligned = true
				} else {
					// Span metadata is stale (e.g. after a surgical rewrite that
					// changed paragraph counts): still honour the PLANNED scene so
					// real titles/descriptions reach the chips instead of collapsing
					// everything into synthetic pseudo-scenes with no info.
					sc = psc
					metaOK = hasMeaningfulSceneMeta(sc)
				}
			} else {
				// No break-derived span for this planned scene: trust the plan
				// outright (same rationale as above).
				sc = ch.ScenePlan.Scenes[spos]
				metaOK = hasMeaningfulSceneMeta(sc)
			}
			if !planAligned && len(ch.ScenePlan.Scenes) == len(fbSceneOpen) {
				// Recorded spans missing/stale: only trust the plan when its scene
				// count matches the break-derived scene count exactly; otherwise
				// positions would misalign and the chips would lie about scenes.
				sc = ch.ScenePlan.Scenes[spos]
				metaOK = hasMeaningfulSceneMeta(sc)
			}
		}
		if sc.Title == "" && sc.Description == "" {
			// Pseudo-scene without plan metadata: give it a minimal synthetic
			// label so the "Scene N" chip renders with a tooltip instead of
			// being suppressed as noise.
			sc = Scene{ID: spos + 1, Title: fmt.Sprintf("Scene %d", spos+1)}
			metaOK = true
		}
		b.SceneTitle = sceneDisplayTitle(sc, spos)
		b.SceneDescription = strings.TrimSpace(sc.Description)
		// Trope chips: tile each scene ONCE (lazily, on its first block) with the
		// writer's own splitter over the scene's full paragraph span, then resolve
		// every block's unit index from that shared tiling. Tiling only when a
		// group happens to start exactly at the scene span would leave later groups
		// without trope metadata — i.e. no trope chips after the first one.
		if _, tiled := fbUnits[spos]; !tiled {
			if span, ok := fbSceneOpen[spos]; ok && span[1] > span[0] {
				sl, sh := span[0], span[1]
				var kept []string
				keptIdx := make([]int, 0, sh-sl)
				for j := sl; j < sh; j++ {
					if paraIsBreak(paras[j]) {
						continue
					}
					kept = append(kept, paras[j])
					keptIdx = append(keptIdx, j)
				}
				if len(kept) == 0 {
					kept = paras[sl:sh]
					keptIdx = nil
					for j := sl; j < sh; j++ {
						keptIdx = append(keptIdx, j)
					}
				}
				beats, ranges := splitTropesFromParas(kept, "", sep)
				if len(beats) == 0 {
					beats = []string{strings.Join(kept, sep)}
					ranges = [][2]int{{0, len(kept)}}
				}
				fbUnits[spos] = beats
				// Unit ranges are relative to `kept`; map them back onto absolute
				// paragraph indices so any group can find its owning unit. Each
				// absolute range spans from the unit's first kept paragraph up to
				// (exclusive) the next unit's first kept paragraph (or the scene end).
				abs := make([][2]int, len(ranges))
				for k, r := range ranges {
					lo := keptIdx[r[0]]
					var hi int
					if k+1 < len(ranges) {
						hi = keptIdx[ranges[k+1][0]]
					} else {
						hi = sh
					}
					if hi <= lo {
						hi = lo + 1
					}
					abs[k] = [2]int{lo, hi}
				}
				fbUnitRanges[spos] = abs
			} else {
				fbUnits[spos] = []string{text}
				fbUnitRanges[spos] = [][2]int{{g[0], g[1]}}
			}
		}
		if beats := fbUnits[spos]; len(beats) > 0 {
			ui := fbLastUnit[spos]
			found := false
			for k, r := range fbUnitRanges[spos] {
				if r[0] <= g[0] && g[0] < r[1] {
					ui = k
					found = true
					break
				}
			}
			if !found {
				for k, r := range fbUnitRanges[spos] {
					if r[0] <= g[0] {
						ui = k
					}
				}
			}
			fbLastUnit[spos] = ui
			setBlockUnitIdx(&b, beats, ui)
		} else {
			b.TropeCount = 1
			b.TropeIndex = 1
			b.TropeLabel = string(classifyTrope(text, spos == 0, g[1] >= len(paras)))
		}
		blocks = append(blocks, b)
	}
	ch.Blocks = blocks
	ch.NextBlockID = next
}

// tropeFallbackGroups segments the whole paragraph list into trope beats via
// the same deterministic splitter the sequential writer uses inside scenes
// (splitTropesFromParas: dialogue/prose mode runs plus hard-break markers,
// tiny trailing beats merged into their predecessor). Chapters with fewer
// than 2*tropeMinParagraphs paragraphs keep one group per paragraph so
// classic block editing stays predictable at that size.
func tropeFallbackGroups(blocks []Block, paras []string, sep string) [][2]int {
	single := func() [][2]int {
		out := make([][2]int, len(paras))
		for i := range paras {
			out[i] = [2]int{i, i + 1}
		}
		return out
	}
	if sep != "\n\n" || len(paras) < 2*tropeMinParagraphs {
		return single()
	}
	_, ranges := splitTropesFromParas(paras, "", sep)
	if len(ranges) == 0 {
		return single()
	}
	// Re-tile the FULL paragraph list so nothing is dropped: break-marker
	// paragraphs become their own divider blocks (the viewer renders them
	// specially) and each trope range stays one block. splitTropesFromParas
	// only emits spans over kept prose paragraphs, so a naive pass would
	// silently drop "---" lines when content is rebuilt from blocks. The
	// walk below covers every paragraph: positions that START a trope range
	// emit that range, break markers always get singleton groups, and any
	// other uncovered stretch falls back to singletons (conservative — never
	// merges across an unexpected shape).
	out := make([][2]int, 0, len(paras))
	rangeEnd := make(map[int]int) // paragraph index -> end of trope range starting here
	for _, r := range ranges {
		rangeEnd[r[0]] = r[1]
	}
	for cur := 0; cur < len(paras); {
		if end, ok := rangeEnd[cur]; ok && end > cur {
			out = append(out, [2]int{cur, end})
			cur = end
			continue
		}
		if paraIsBreak(paras[cur]) {
			out = append(out, [2]int{cur, cur + 1})
			cur++
			continue
		}
		out = append(out, [2]int{cur, cur + 1})
		cur++
	}
	if groupsOverlapTropeChange(blocks, paras, out) {
		return single()
	}
	return out
}

// groupsOverlapTropeChange reports whether regrouping paras into trope beats
// would merge two paragraphs that currently live in DIFFERENT blocks — i.e.
// an existing finer segmentation (e.g. one block per paragraph after a
// single-newline split) would be destroyed. Block editing must stay
// lossless: UpdateBlock/DeleteBlock splice block texts back into content, so
// we only collapse paragraphs into beats when no block boundary falls inside
// a proposed group.
func groupsOverlapTropeChange(blocks []Block, paras []string, groups [][2]int) bool {
	if len(blocks) == 0 {
		return false
	}
	belongs := make([]int, len(paras)) // paragraph -> block index (-1 none)
	for bi, b := range blocks {
		if strings.TrimSpace(b.Text) == "" {
			continue
		}
		found := -1
		for pi, p := range paras {
			if p == b.Text {
				found = pi
				break
			}
		}
		if found < 0 {
			continue // block text not aligned with paragraphs: treat as boundary anywhere
		}
		belongs[found] = bi
	}
	for _, g := range groups {
		first := -1
		for i := g[0]; i < g[1]; i++ {
			if belongs[i] < 0 {
				continue
			}
			if first < 0 {
				first = belongs[i]
			} else if belongs[i] != first {
				return true
			}
		}
	}
	return false
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
	// A single-scene plan is still worth tiling at trope granularity: the
	// fallback planner materializes one scene per break-delimited group, so
	// requiring >=2 scenes here used to send every legacy/plan-less chapter
	// down the paragraph path, where fbSceneStart counts only HARD BREAKS —
	// leaving just one "Scene 1" chip and no trope chips in the UI.
	if ch.ScenePlan == nil || len(ch.ScenePlan.Scenes) < 1 {
		return false
	}
	paras, sep := splitBlockParagraphs(ch.Content)
	if len(paras) == 0 {
		return false
	}
	spans := sceneParaSpans(ch, paras)
	if spans == nil {
		// Stale recorded spans (e.g. a sheet save added/removed paragraphs after
		// the plan was written) make the exact tiling unusable. Before giving up
		// on scene-aware blocks entirely, repair ParaStart/ParaEnd from the
		// CURRENT prose (break markers first, budget walk as fallback) and retry.
		// Without this, chapters edited outside the sequential writer collapse to
		// the paragraph path where fbSceneStart counts only HARD BREAKS — the UI
		// then shows a single "Scene 1" chip and hides every other scene chip.
		repairSceneSpans(ch)
		spans = sceneParaSpans(ch, paras)
	}
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
			// Trope text excludes pure break-marker paragraphs (*** / ---),
			// but the span keeps covering them so scenes/units still tile.
			var kept []string
			for _, p := range paras[start:end] {
				if paraIsBreak(p) {
					continue
				}
				kept = append(kept, p)
			}
			if len(kept) == 0 {
				continue // a unit made only of separators carries no prose
			}
			text := strings.Join(kept, sep)
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
			metaOK := hasMeaningfulSceneMeta(sc)
			blocks = append(blocks, Block{
				ID: id, Type: typ, Text: text,
				ScenePos: pos, SceneTitle: title, SceneDescription: strings.TrimSpace(sc.Description),
				TropeLabel: label, TropeIndex: ui + 1, TropeCount: len(units),
				SceneHasMeta: &metaOK,
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
		return "subscene"
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
	// Re-derive tropes from the scene prose currently on disk, over the SAME
	// paragraph slice (keeping ch.BlockSep as join separator) so the returned
	// spans are scene-local paragraph indices that tile it exactly.
	if _, ranges := splitTropesFromParas(sceneParas, "", ch.BlockSep); len(ranges) > 0 {
		return ranges
	}
	return fallback
}

// sceneDisplayTitle returns a short human label for the scene marker tooltip.
// When the plan carries no title/description it degrades to "Scene N" — and
// callers use hasMeaningfulSceneMeta to decide whether that placeholder is all
// they have (in which case the viewer hides the chip instead of showing noise).
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

// hasMeaningfulSceneMeta reports whether a planned scene carries real plan
// metadata (title or description) worth surfacing in the reader UI. Chapters
// written without a scene plan produce blocks whose only scene info is the
// auto-generated "Scene N" placeholder; chips built from those are noise.
func hasMeaningfulSceneMeta(sc Scene) bool {
	return strings.TrimSpace(sc.Title) != "" || strings.TrimSpace(sc.Description) != ""
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

// breakSceneSpans derives pseudo-scenes from HARD BREAK markers ("---"), the
// same tiling SyncChapterBlocks uses for the reader chips when no usable scene
// plan exists. Returns one [lo,hi) span per break-delimited group (paragraphs
// equal to a break marker are excluded from their span). The whole chapter is
// one span when it contains no breaks. Always non-empty for non-empty paras.
func breakSceneSpans(paras []string) [][2]int {
	if len(paras) == 0 {
		return nil
	}
	var spans [][2]int
	start := -1
	for j := range paras {
		if paraIsBreak(paras[j]) {
			if start >= 0 && j > start {
				spans = append(spans, [2]int{start, j})
			}
			start = -1
			continue
		}
		if start < 0 {
			start = j
		}
	}
	if start >= 0 && len(paras) > start {
		spans = append(spans, [2]int{start, len(paras)})
	}
	return spans
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
			continue // degenerate budget → ignored; budgetWalkSceneSpans re-checks
		}
		totalBudget += sc.WordsBudget
	}
	if spans := budgetWalkSceneSpans(plan, paras, totalBudget); spans != nil {
		return spans
	}
	return structuralSceneSpans(ch, plan, paras)
}

// budgetWalkSceneSpans distributes paragraphs proportionally to word budgets:
// each boundary snaps to the paragraph whose cumulative words pass the
// cumulative budget share. Returns nil when the mapping is unreliable
// (degenerate budgets, more scenes than paragraphs, empty prose).
func budgetWalkSceneSpans(plan *ScenePlan, paras []string, totalBudget int) [][2]int {
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
	spans := make([][2]int, 0, len(plan.Scenes))
	cumBudget := 0
	start := 0
	for pos, sc := range plan.Scenes {
		if sc.WordsBudget < 1 {
			return nil // degenerate budget → can't map reliably
		}
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

// structuralSceneSpans is the last-resort scene mapper used when neither the
// recorded spans nor the budget walk can be trusted — typically chapters whose
// plan has no usable word budgets AND whose prose was edited outside the
// sequential writer (stale ParaStart/ParaEnd). Without it such chapters fall
// back to the paragraph path where pseudo-scenes come ONLY from hard-break
// markers, which is exactly why the UI showed a single "Scene 1" chip and hid
// every other scene chip whenever the prose separated scenes with blank lines
// instead of "---". Strategy: honour the break-marker tiling when its group
// count matches the plan's scene count exactly; otherwise cut the prose at the
// most STRUCTURALLY salient boundaries — paragraphs that flip dialogue↔prose
// mode or open with a narrative-turn pivot cue ("pero", "suddenly", …), the
// same cues splitTropesFromParas uses for beats — picking the N-1 strongest
// cuts spread across the chapter so every scene gets at least one paragraph.
// The derived spans are written back onto the plan (with fresh trope-unit
// tilings) so recordedSceneParaSpans accepts them on every later sync and the
// chips stay stable until the prose changes again.
func structuralSceneSpans(ch *ChapterState, plan *ScenePlan, paras []string) [][2]int {
	n := len(plan.Scenes)
	if n < 2 || len(paras) < n {
		return nil
	}
	// Preferred: hard-break groups matching the plan's scene count exactly.
	if bs := breakSceneSpansNoMarker(paras); len(bs) == n {
		applyDerivedSceneSpans(ch, bs)
		return bs
	}
	// Score every interior paragraph as a candidate scene cut.
	type cand struct {
		idx   int
		score int
	}
	var cands []cand
	prevMode := paraIsDialogue(paras[0])
	for i := 1; i < len(paras); i++ {
		if paraIsBreak(paras[i]) {
			continue
		}
		m := paraIsDialogue(paras[i])
		score := 0
		if m != prevMode {
			score += 3 // dialogue↔prose flip: strong structural seam
		}
		if paraHasPivot(paras[i]) {
			score += 2 // explicit narrative-turn cue
		}
		if score > 0 {
			cands = append(cands, cand{i, score})
		}
		prevMode = m
	}
	// If structural evidence is thin we still proceed: the greedy pass below
	// may place fewer cuts and the even-tiling fallback guarantees every planned
	// scene gets a chip instead of collapsing to a single "Scene 1".
	// Greedy best-score selection with spacing guarantees: every chosen cut
	// must leave ≥1 paragraph for each still-unplaced scene boundary ahead and
	// behind it, so no scene ends up empty.
	chosen := make([]int, 0, n-1)
	used := make(map[int]bool)
	for len(chosen) < n-1 {
		best, bestScore := -1, 0
		for _, c := range cands {
			if used[c.idx] {
				continue
			}
			okPos := true
			for _, x := range chosen {
				if absInt(x-c.idx) < 1 {
					okPos = false
					break
				}
			}
			if !okPos {
				continue
			}
			sorted := append([]int{}, chosen...)
			sort.Ints(sorted)
			sorted = append(sorted, c.idx)
			sort.Ints(sorted)
			// feasibility: gaps between consecutive cuts (and the edges) must
			// fit one paragraph per scene they delimit.
			prev := 0
			for _, x := range sorted {
				if x-prev < 1 {
					okPos = false
					break
				}
				prev = x
			}
			if okPos && len(paras)-prev < 1 {
				okPos = false
			}
			if !okPos {
				continue
			}
			if c.score > bestScore {
				best, bestScore = c.idx, c.score
			}
		}
		if best < 0 {
			break // cannot place all cuts without emptying a scene
		}
		used[best] = true
		chosen = append(chosen, best)
	}
	if len(chosen) != n-1 {
		// Not enough structural evidence for every cut: fall back to an even
		// tiling (≥1 paragraph per scene). Without this the chapter dropped to
		// the paragraph path where pseudo-scenes come ONLY from hard-break
		// markers — exactly why the UI showed a single "Scene 1" chip and hid
		// every other scene chip. An approximate map is far better than none:
		// all planned scenes get real chips with their titles/descriptions.
		even := make([][2]int, 0, n)
		start := 0
		for pos := 0; pos < n-1; pos++ {
			end := start + 1 + (len(paras)-pos-1)/(n-pos-1)
			if end > len(paras)-(n-1-pos) {
				end = len(paras) - (n - 1 - pos)
			}
			if end <= start {
				end = start + 1
			}
			even = append(even, [2]int{start, end})
			start = end
		}
		even = append(even, [2]int{start, len(paras)})
		applyDerivedSceneSpans(ch, even)
		return even
	}
	sort.Ints(chosen)
	spans := make([][2]int, 0, n)
	start := 0
	for _, c := range chosen {
		spans = append(spans, [2]int{start, c})
		start = c
	}
	spans = append(spans, [2]int{start, len(paras)})
	applyDerivedSceneSpans(ch, spans)
	return spans
}

// applyDerivedSceneSpans writes freshly derived paragraph spans onto the
// chapter's scene plan and re-tiles each scene's trope units over them, so the
// next recordedSceneParaSpans call trusts the exact tiling and the reader
// renders real scene/subscene chips instead of collapsing to pseudo-scenes.
func applyDerivedSceneSpans(ch *ChapterState, spans [][2]int) {
	if ch.ScenePlan == nil {
		return
	}
	paras, sep := splitBlockParagraphs(ch.Content)
	for pos := range ch.ScenePlan.Scenes {
		if pos >= len(spans) {
			break
		}
		sc := &ch.ScenePlan.Scenes[pos]
		sc.ParaStart, sc.ParaEnd = spans[pos][0], spans[pos][1]
		retileSceneTropeUnits(sc, paras, sep)
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
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
	if plan == nil || len(plan.Scenes) < 1 {
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

		spans = append(spans, [2]int{sc.ParaStart, sc.ParaEnd})
	}
	if spans[len(spans)-1][1] != len(paras) {
		return nil // trailing paragraphs unaccounted for
	}
	// Trope units are a per-scene refinement: when they are stale (a manual
	// edit inside one scene changed its paragraph count) drop ONLY that
	// scene's units so sceneTropeUnitSpans re-derives them live from the
	// current text — instead of discarding the whole exact scene tiling and
	// falling back to budget-proportional blocks.
	for i := range plan.Scenes {
		sc := &plan.Scenes[i]
		if len(sc.TropeUnits) == 0 {
			continue
		}
		sceneLen := sc.ParaEnd - sc.ParaStart
		ok := true
		maxEnd := 0
		for _, u := range sc.TropeUnits {
			if u.ParaStart < 0 || u.ParaEnd <= u.ParaStart || u.ParaEnd > sceneLen {
				ok = false
				break
			}
			if u.ParaEnd > maxEnd {
				maxEnd = u.ParaEnd
			}
		}
		if !ok || maxEnd != sceneLen {
			sc.TropeUnits = nil
			sc.TropeRanges = nil
		}
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

// SheetBlock is one paragraph of the continuous-sheet editor payload: the
// stable block ID plus its (possibly edited) text. The reader serializes the
// whole editable sheet as a list of these and PUTs it to /sheet in one shot.
type SheetBlock struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

// SetChapterSheetBlocks applies a full-sheet edit coming from the continuous
// reader: every existing block gets the text sent for its ID; paragraphs the
// author split or typed without an ID are appended with fresh IDs at the
// position they were sent in. Content is rebuilt from the ordered texts and
// re-synced scene-aware, so trope/scene bookkeeping (and therefore the visual
// grouping by tropes) survives the edit. Empty texts drop the block — that is
// how the sheet deletes a paragraph.
func SetChapterSheetBlocks(ch *ChapterState, incoming []SheetBlock) error {
	if len(incoming) == 0 {
		return fmt.Errorf("empty_sheet")
	}
	byID := make(map[int]int, len(ch.Blocks))
	for i, b := range ch.Blocks {
		byID[b.ID] = i
	}
	next := ch.NextBlockID
	if next <= 0 {
		next = 1
	}
	texts := make([]string, 0, len(incoming))
	seen := map[int]bool{}
	for _, ib := range incoming {
		t := strings.TrimRight(ib.Text, " \t")
		if strings.TrimSpace(t) == "" {
			continue
		}
		if ib.ID > 0 && byID[ib.ID] >= 0 && !seen[ib.ID] {
			seen[ib.ID] = true
		} else if ib.ID > 0 {
			// Unknown/duplicate ID: treat as new paragraph, keep requested order.
			ib.ID = next
			next++
		}
		texts = append(texts, t)
	}
	if len(texts) == 0 {
		return fmt.Errorf("empty_sheet")
	}
	sep := ch.BlockSep
	if sep == "" {
		sep = "\n\n"
	}
	ch.Content = strings.Join(texts, sep)
	ch.NextBlockID = next
	// Scene-aware sync: rebuilds blocks AND re-derives scene/trope spans over
	// the new content, so the chips separators and trope grouping (and with
	// them the critic's highlighted beats) survive a full-sheet save.
	SyncChapterBlocksSceneAware(ch, ch.Content)
	return nil
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

// setBlockUnit fills a block's trope-unit chip fields from a scene-level
// tiling produced by splitTropesFromParas. ranges are scene-local (relative
// to sl); firstPara is the chapter-global index of the block's first para.
func setBlockUnit(b *Block, beats []string, ranges [][2]int, sl, firstPara int, sceneFirst, sceneLast bool) {
	count := len(beats)
	if count < 1 {
		count = 1
	}
	b.TropeCount = count
	if count == 1 {
		b.TropeLabel = string(classifyTrope(beats[0], sceneFirst, sceneLast))
		b.TropeIndex = 1
		return
	}
	ui := unitIdxForPara(ranges, firstPara-sl, len(beats))
	setBlockUnitIdx(b, beats, ui)
}

// setBlockUnitIdx assigns index/label for unit ui (0-based) over tiled beats.
func setBlockUnitIdx(b *Block, beats []string, ui int) {
	if ui < 0 {
		ui = 0
	}
	if ui >= len(beats) {
		ui = len(beats) - 1
	}
	b.TropeCount = len(beats)
	b.TropeIndex = ui + 1
	isFirst := ui == 0
	isLast := ui == len(beats)-1
	b.TropeLabel = string(classifyTrope(beats[ui], isFirst, isLast))
}

// unitIdxForPara returns which unit (0-based) owns paragraph pi in a tiling;
// falls back to the last unit whose range starts at or before pi.
func unitIdxForPara(ranges [][2]int, pi, n int) int {
	for k, r := range ranges {
		if r[0] <= pi && pi < r[1] {
			return k
		}
	}
	last := 0
	for k, r := range ranges {
		if r[0] <= pi {
			last = k
		}
	}
	if last >= n {
		last = n - 1
	}
	return last
}
