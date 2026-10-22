package story

import (
	"context"
	"encoding/json"
	"fmt"
	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/sse"
	"strings"
	"unicode"
)

type MemoryReference struct {
	Chapter    int    `json:"chapter"`
	BlockID    int    `json:"block_id"`
	Quote      string `json:"quote"`
	ContentRev string `json:"content_rev"`
	Stale      bool   `json:"stale,omitempty"`
}
type FactEditOptions struct {
	ContentRev        string `json:"content_rev"`
	ConfirmFactImpact bool   `json:"confirm_fact_impact"`
}

func ChapterRevision(ch ChapterState) string { return fmt.Sprintf("%x", HashContent(ch.Content)) }
func ReferenceLive(state *Progress, ref MemoryReference) bool {
	if ref.Stale {
		return false
	}
	i := FindChapterIdx(state, ref.Chapter)
	if i < 0 {
		return false
	}
	j := FindBlockIdx(&state.Chapters[i], ref.BlockID)
	return j >= 0 && state.Chapters[i].Blocks[j].Text == ref.Quote
}
func memoryHasLiveReference(state *Progress, m MemoryEntry) bool {
	for _, r := range m.References {
		if ReferenceLive(state, r) {
			return true
		}
	}
	return false
}
func FactsForChapter(state *Progress, num, blockID int) []MemoryEntry {
	out := []MemoryEntry{}
	for _, m := range state.MemoryEntries {
		match := false
		refs := append([]MemoryReference(nil), m.References...)
		for i := range refs {
			refs[i].Stale = refs[i].Stale || !ReferenceLive(state, refs[i])
			if refs[i].Chapter == num && (blockID == 0 || refs[i].BlockID == blockID) {
				match = true
			}
		}
		if match {
			m.References = refs
			out = append(out, m)
		}
	}
	return out
}
func ValidateFactEdit(state *Progress, before, after ChapterState, opts FactEditOptions, lang string) error {
	if opts.ContentRev != "" && opts.ContentRev != ChapterRevision(before) {
		return fmt.Errorf("%s", i18n.T(lang, "content_version_conflict"))
	}
	affected := false
	for _, m := range FactsForChapter(state, before.Num, 0) {
		for _, r := range m.References {
			if r.Chapter != before.Num {
				continue
			}
			a, b := FindBlockIdx(&before, r.BlockID), FindBlockIdx(&after, r.BlockID)
			if a >= 0 && (b < 0 || before.Blocks[a].Text != after.Blocks[b].Text) {
				affected = true
			}
		}
	}
	if affected && (!opts.ConfirmFactImpact || opts.ContentRev != ChapterRevision(before)) {
		return fmt.Errorf("%s", i18n.T(lang, "fact_impact_confirm"))
	}
	return nil
}
func factProtection(state *Progress, num int, lang string) string {
	facts := FactsForChapter(state, num, 0)
	live := facts[:0]
	for _, f := range facts {
		if memoryHasLiveReference(state, f) {
			// Revision must preserve every linked fact, but does not need copies
			// of all historical evidence paragraphs attached to those facts.
			f.References = nil
			f.Snippet = ""
			live = append(live, f)
		}
	}
	facts = live
	if len(facts) == 0 {
		return chapterEnding(state, num, lang)
	}
	data, _ := json.Marshal(facts)
	s := "\nPreserve these linked consistency facts unless the author explicitly authorizes changing them. Do not silently alter related passages.\n"
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		s = "\nSalvo autorización explícita del autor, conserva estos hechos de consistencia vinculados y no alteres en silencio los pasajes relacionados.\n"
	}
	return chapterEnding(state, num, lang) + s + string(data)
}

func SyncChapterMemory(ctx context.Context, api *config.APIConfig, cfg *config.Config, state *Progress, idx int, path string, logger *sse.LogBroadcaster) error {
	if idx < 0 || idx >= len(state.Chapters) {
		return fmt.Errorf("chapter index out of range")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ch := state.Chapters[idx]
	if ch.Content == "" {
		return nil
	}
	SyncChapterBlocks(&ch)
	rev := ChapterRevision(ch)
	if ch.MemoryRevision == rev {
		return nil
	}
	if err := llm.ValidateConfig(api); err != nil {
		return err
	}
	next := *state
	next.Chapters = append([]ChapterState(nil), state.Chapters...)
	next.Chapters[idx] = ch
	next.Chapters[idx].KnowledgeTracked = true
	// Persist the retry marker before invoking the model.
	if err := SaveProgress(path, &next); err != nil {
		return err
	}
	*state = next
	if next.MemoryMaxTokens <= 0 {
		next.MemoryMaxTokens = calcMemoryMaxTokens(len(state.Chapters), cfg.Story.TargetWordsPerChapter)
	}
	existing := retrieveMemories(state, ch, memoryContextRunes, false)
	visibleFacts := map[int]bool{}
	for _, m := range existing {
		visibleFacts[m.ID] = true
	}
	blocks, _ := json.Marshal(ch.Blocks)
	prompt := config.RenderPrompt(cfg.Prompts.MemoryUpdate, map[string]string{
		"Title": state.Title, "ChapterNum": fmt.Sprint(ch.Num), "ChapterTitle": ch.Title, "ChapterOutline": ch.Outline,
		"ChapterContent": ch.Content, "ExistingMemory": knowledgeSelectionNotice(cfg.Language) + formatMemoryForUpdatePrompt(existing, cfg.Language),
		"MemoryMaxTokens": fmt.Sprint(next.MemoryMaxTokens),
	})
	prompt += memoryLinkPrompt(cfg.Language) + "\n" + string(blocks)
	var syncErr error
	for attempt := 0; attempt < 2; attempt++ {
		raw := llm.CallAPIWithRetryLog(ctx, api, i18n.SystemPromptFor(cfg.Language, "memory_manager"), prompt, logger)
		if err := ctx.Err(); err != nil {
			return err
		}
		if raw == "" {
			return fmt.Errorf("%s", i18n.T(cfg.Language, "knowledge_failed"))
		}
		next = *state
		if next.MemoryMaxTokens <= 0 {
			next.MemoryMaxTokens = calcMemoryMaxTokens(len(state.Chapters), cfg.Story.TargetWordsPerChapter)
		}
		syncErr = applyChapterMemoryResult(&next, ch, visibleFacts, raw)
		if syncErr == nil {
			break
		}
		prompt += memoryRetryPrompt(cfg.Language, syncErr)
	}
	if syncErr != nil {
		return syncErr
	}
	next.Chapters = append([]ChapterState(nil), state.Chapters...)
	next.Chapters[idx].MemoryRevision = rev
	if err := SaveProgress(path, &next); err != nil {
		return err
	}
	*state = next
	return nil
}

// Validate a fresh copy for each attempt; no partial facts reach live state.
func applyChapterMemoryResult(next *Progress, ch ChapterState, visibleFacts map[int]bool, raw string) error {
	rev := ChapterRevision(ch)
	var result struct {
		NewMemories *[]struct {
			ID       int    `json:"id"`
			Content  string `json:"content"`
			Category string `json:"category"`
			BlockIDs []int  `json:"block_ids"`
		} `json:"new_memories"`
	}
	payload := cleanJSONResponse(raw)
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		repaired, ok := repairMemoryJSONPayload(raw, payload)
		if !ok {
			return err
		}
		if err := json.Unmarshal([]byte(repaired), &result); err != nil {
			return err
		}
	}
	if result.NewMemories == nil {
		return fmt.Errorf("missing new_memories array")
	}
	// Pass 1: validate every entry before mutating anything, so one bad row
	// cannot leave the batch half-applied and trip "duplicate fact identity"
	// on the retry.
	parsed := make([]struct {
		nmID     int
		content  string
		category string
		blockIDs []int
		target   int
	}, 0, len(*result.NewMemories))
	next.NextMemoryID = max(next.NextMemoryID, nextMemoryID(next.MemoryEntries))
	claimedTargets := map[int]bool{}
	storedByID := map[int]string{}
	for _, m := range next.MemoryEntries {
		storedByID[m.ID] = m.Content
	}
	for idx := range *result.NewMemories {
		nm := &(*result.NewMemories)[idx]
		if nm.ID > 0 && !visibleFacts[nm.ID] {
			// The memory prompt only shows a budgeted subset of facts, yet models
			// routinely echo ids from earlier turns or invent their own numbering.
			// If the id exists in storage and the content matches that stored fact
			// (verbatim or paraphrase), accept it as an update instead of failing
			// the whole sync — the previous hard rejection made sync fail
			// permanently ("id=N was not supplied") on every retry. A non-paraphrase
			// rewrite of a REAL stored id keeps the same lenient policy used below:
			// fall back to the stored text (identity preserved) unless it flips a
			// negation. Only unknown/invented ids stay fatal.
			if stored, ok := storedByID[nm.ID]; ok {
				oldN, newN := normalizeFactText(stored), normalizeFactText(nm.Content)
				switch {
				case oldN == newN || (!factNegationFlipped(oldN, newN) && factContentEquivalent(oldN, newN)):
					visibleFacts[nm.ID] = true
				case factNegationFlipped(oldN, newN):
					return fmt.Errorf("invalid existing fact identity: id=%d contradicts the stored fact (negation flipped)", nm.ID)
				default:
					nm.Content = stored
					visibleFacts[nm.ID] = true
				}
			} else {
				return fmt.Errorf("invalid existing fact identity: id=%d was not supplied; use id=0 for new facts", nm.ID)
			}
		}
		if nm.ID < 0 || strings.TrimSpace(nm.Content) == "" || len(nm.BlockIDs) == 0 {
			return fmt.Errorf("invalid fact evidence")
		}
		switch nm.Category {
		case "character", "location", "item", "event", "promise", "other":
		default:
			return fmt.Errorf("invalid fact category")
		}
		target := -1
		for j, m := range next.MemoryEntries {
			if nm.ID > 0 && m.ID == nm.ID {
				target = j
				break
			}
		}
		if target < 0 && nm.ID == 0 {
			// Paraphrase-dedup only for genuinely new facts (id=0), and never onto
			// an entry another row already claims, so two different facts cannot be
			// merged into one identity by fuzzy matching. Existing facts must echo
			// their stored id to be updated.
			for j, m := range next.MemoryEntries {
				if claimedTargets[j] {
					continue
				}
				if m.Content == nm.Content || factContentEquivalent(m.Content, nm.Content) {
					target = j
					break
				}
			}
		}
		if target >= 0 {
			claimedTargets[target] = true
		}
		// An existing fact keeps its identity when the model echoes the stored
		// content verbatim OR paraphrases it with the same meaning. Memory-sync
		// prompts legitimately ask for deduplicated/rewritten entries; rejecting
		// every wording change made sync fail permanently on models that never
		// reproduce text byte-for-byte ("must preserve the supplied content
		// exactly"). Negations flip meaning entirely, so they are rejected even
		// though they share most tokens. Evidence references stay validated below,
		// so nothing unanchored can be claimed.
		if nm.ID > 0 && target >= 0 {
			old := normalizeFactText(next.MemoryEntries[target].Content)
			newC := normalizeFactText(nm.Content)
			if old != newC {
				// A paraphrase that keeps the meaning evolves the fact in place. A
				// wording change that drops below the equivalence threshold is NOT a
				// hard failure anymore: after a chapter rewrite the model legitimately
				// re-summarizes facts with different vocabulary, and aborting the whole
				// batch ("must preserve the supplied content exactly") made memory sync
				// fail PERMANENTLY for every later action. Instead the row falls back
				// to the stored text — the identity stays anchored and only the exact
				// phrasing is preserved. Negation flips remain fatal because they
				// invert the fact's truth value.
				// Negation polarity must never flip silently: if one statement is
				// negated and the other is not, the model contradicts the stored
				// fact's truth value — fatal. Checked on top of the token-set
				// heuristic so contracted forms ("didn't" -> "didnt") count too.
				if factNegationFlipped(old, newC) ||
					factContainsNegation(old) != factContainsNegation(newC) {
					return fmt.Errorf("invalid existing fact identity: id=%d contradicts the stored fact (negation flipped)", nm.ID)
				}
				nm.Content = next.MemoryEntries[target].Content
			}
		}
		parsed = append(parsed, struct {
			nmID     int
			content  string
			category string
			blockIDs []int
			target   int
		}{nm.ID, nm.Content, nm.Category, nm.BlockIDs, target})
	}

	next.MemoryEntries = append([]MemoryEntry(nil), next.MemoryEntries...)
	for j := range next.MemoryEntries {
		next.MemoryEntries[j].References = append([]MemoryReference(nil), next.MemoryEntries[j].References...)
		for k := range next.MemoryEntries[j].References {
			if next.MemoryEntries[j].References[k].Chapter == ch.Num {
				next.MemoryEntries[j].References[k].Stale = true
			}
		}
	}
	// Old references remain as historical evidence until their replacement is valid.
	seenFacts := map[int]bool{}
	for _, p := range parsed {
		refs := []MemoryReference{}
		seen := map[int]bool{}
		for _, id := range p.blockIDs {
			bi := FindBlockIdx(&ch, id)
			if bi < 0 {
				return fmt.Errorf("invalid fact block")
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			refs = append(refs, MemoryReference{Chapter: ch.Num, BlockID: id, Quote: ch.Blocks[bi].Text, ContentRev: rev})
		}
		if p.target < 0 {
			next.MemoryEntries = append(next.MemoryEntries, MemoryEntry{ID: next.NextMemoryID, Content: p.content, Category: p.category})
			next.NextMemoryID++
			p.target = len(next.MemoryEntries) - 1
		}
		m := next.MemoryEntries[p.target]
		if seenFacts[m.ID] {
			return fmt.Errorf("duplicate fact identity")
		}
		seenFacts[m.ID] = true
		kept := []MemoryReference{}
		for _, r := range m.References {
			if r.Chapter != ch.Num {
				kept = append(kept, r)
			}
		}
		m.References = append(kept, refs...)
		next.MemoryEntries[p.target] = m
	}
	return nil
}

// repairMemoryJSONPayload salvages a memory-sync response that failed to
// parse. Models frequently emit invalid JSON around the payload — prose
// preambles, truncated output, raw newlines or unescaped quotes inside string
// values — and the resulting "invalid character 'i' looking for beginning of
// value" made sync abort permanently after every rewrite. Strategy: try
// progressively more aggressive cleanups (greedy balanced-object extraction,
// control-character escaping inside strings) and return the first candidate
// that parses as a well-formed object. ok=false means nothing could be saved.
func repairMemoryJSONPayload(raw, payload string) (string, bool) {
	candidates := []string{
		extractJSONObject(raw),
		sanitizeJSONControlChars(payload),
		sanitizeJSONControlChars(extractJSONObject(raw)),
	}
	for _, c := range candidates {
		if c == "" || c == payload {
			continue
		}
		var probe map[string]json.RawMessage
		if json.Unmarshal([]byte(c), &probe) == nil && len(probe) > 0 {
			return c, true
		}
	}
	return "", false
}

// sanitizeJSONControlChars escapes literal control characters (newlines, tabs)
// that appear INSIDE JSON string values — the most common malformation in
// long-form model output — leaving the rest of the document untouched.
func sanitizeJSONControlChars(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 16)
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case ch == '\\':
				esc = true
			case ch == '"':
				inStr = false
			case ch == '\n':
				b.WriteString("\\n")
				continue
			case ch == '\r':
				continue
			case ch == '\t':
				b.WriteString("\\t")
				continue
			}
		} else if ch == '"' {
			inStr = true
		}
		b.WriteByte(ch)
	}
	return b.String()
}


// factContentEquivalent reports whether two memory texts express the same
// fact despite wording differences: identical after normalization, or sharing
// at least half of their word set (Jaccard ≥ 0.5). It exists so the memory
// sync accepts harmless paraphrase/dedup rewrites of an existing fact instead
// of failing forever on byte-exact equality.
func factContentEquivalent(a, b string) bool {
	na, nb := normalizeFactText(a), normalizeFactText(b)
	if na == nb {
		return true
	}
	sa, sb := factTokenSet(na), factTokenSet(nb)
	if len(sa) == 0 || len(sb) == 0 {
		return false
	}
	inter := 0
	for t := range sa {
		if sb[t] {
			inter++
		}
	}
	union := len(sa) + len(sb) - inter
	return union > 0 && float64(inter)/float64(union) >= 0.5
}

func normalizeFactText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevSpace := true
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevSpace = false
		case prevSpace:
			// collapse repeated separators
		default:
			b.WriteRune(' ')
			prevSpace = true
		}
	}
	return b.String()
}

func factTokenSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.Fields(s) {
		set[w] = true
	}
	return set
}

// factNegationFlipped reports whether two normalized fact texts disagree only
// because one negates what the other asserts (e.g. "he is alive" vs "he is
// not dead"). Such pairs share most tokens yet state opposite facts, so they
// must never be treated as an equivalent paraphrase of an existing entry.
var factNegationWords = map[string]bool{
	// english
	"not": true, "no": true, "never": true, "neither": true, "nor": true,
	"cannot": true, "nobody": true, "nothing": true, "nowhere": true,
	"without": true, "unless": true,
	// spanish
	"nunca": true, "jamás": true, "jamas": true, "tampoco": true,
	"ningún": true, "ningun": true, "ninguna": true, "ninguno": true,
	"nadie": true, "nada": true,
	"sin": true,
}

// factNegationPrefixes are per-language negation prefixes glued to the verb
// ("didn't", "noquemó"). The tokenizer strips apostrophes, so "didn't" arrives
// as the single token "didnt"; without this list a plain English negation flip
// would go undetected and silently overwrite a stored fact's truth value.
var factNegationPrefixes = map[string]bool{
	// english (contracted forms, post-normalization)
	"didnt": true, "doesnt": true, "dont": true, "isnt": true, "wasnt": true,
	"wont": true, "cant": true, "couldnt": true, "shouldnt": true,
	"wouldnt": true, "havent": true, "hasnt": true, "hadnt": true,
	"arent": true, "aint": true,
	// spanish (glued preverbal negation, e.g. "noquemó" -> "noquemo")
	"no": true,
}

// factContainsNegation reports whether s carries any explicit negation marker,
// either as a standalone word or as a contracted/glued prefix.
func factContainsNegation(s string) bool {
	set := factTokenSet(normalizeFactText(s))
	for t := range set {
		if factNegationWords[t] || factNegationPrefixes[t] {
			return true
		}
	}
	return false
}

func factNegationFlipped(a, b string) bool {
	// Flipping means requires changing the number of negation markers while the
	// surrounding content stays substantially the same. A paraphrase that keeps
	// the same set/count of negation words ("has a scar" -> "bears no scar") is
	// an equivalence, not a flip; only a strict difference in marker count is.
	sa, sb := factTokenSet(a), factTokenSet(b)
	if len(sa) == 0 || len(sb) == 0 {
		return false
	}
	inter := 0
	for t := range sa {
		if sb[t] {
			inter++
		}
	}
	union := len(sa) + len(sb) - inter
	if union == 0 || float64(inter)/float64(union) < 0.5 {
		return false
	}
	return !sameNegationMarkerSet(factNegationMarkers(sa), factNegationMarkers(sb))
}

func factNegationMarkers(set map[string]bool) map[string]bool {
	m := map[string]bool{}
	for t := range set {
		if factNegationWords[t] {
			m[t] = true
		}
	}
	return m
}

func sameNegationMarkerSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for t := range a {
		if !b[t] {
			return false
		}
	}
	return true
}
