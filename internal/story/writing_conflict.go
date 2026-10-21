package story

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/prose"
	"showmethestory/internal/sse"
	"sort"
	"strings"
	"time"
)

type WritingConflictError struct {
	Conflict *WritingConflict
}

func (e *WritingConflictError) Error() string {
	if e == nil || e.Conflict == nil {
		return "writing in progress"
	}
	return e.Conflict.Summary
}

type writingConflictAnalysis struct {
	Reconcilable     bool                   `json:"reconcilable"`
	Summary          string                 `json:"summary"`
	RootCause        string                 `json:"root_cause"`
	ExtraConstraints string                 `json:"extra_constraints"`
	SuggestedActions []ConflictActionOption `json:"suggested_actions"`
}

func analyzeWritingConflict(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, idx int, content string, failedIssues []string, logger *sse.LogBroadcaster) (*writingConflictAnalysis, error) {
	ch := state.Chapters[idx]
	lang := cfg.Language

	excerpt := content
	if runes := []rune(excerpt); len(runes) > 1200 {
		excerpt = string(runes[:600]) + "\n...\n" + string(runes[len(runes)-600:])
	}

	foreshadowBlock := formatActiveForeshadowsForChapterLang(state.Foreshadows, ch.Num, lang)
	outlineConstraints := buildArcConstraintsForReview(state, idx, lang)

	userPrompt := config.RenderPrompt(cfg.Prompts.WritingConflictAnalysis, map[string]string{
		"ChapterNum":         fmt.Sprintf("%d", ch.Num),
		"ChapterTitle":       ch.Title,
		"ChapterOutline":     ch.Outline,
		"HistorySummary":     buildHistorySummaryForLang(state, idx, lang),
		"OutlineConstraints": outlineConstraints,
		"Foreshadows":        foreshadowBlock,
		"FailedIssues":       strings.Join(failedIssues, "\n"),
		"ContentExcerpt":     excerpt,
	})

	systemPrompt := i18n.SystemPromptFor(lang, "writing_conflict_analyst_json")
	rawResp := llm.CallAPIWithRetryLog(ctx, apiCfg, systemPrompt, userPrompt, logger)
	if rawResp == "" {
		return nil, fmt.Errorf("API call failed or was canceled")
	}

	var analysis writingConflictAnalysis
	jsonStr := llm.ExtractJSON(cleanJSONResponse(rawResp))
	if jsonStr == "" {
		return nil, fmt.Errorf("writing segment split produced no result")
	}
	if err := json.Unmarshal([]byte(jsonStr), &analysis); err != nil {
		return nil, fmt.Errorf("parse writing segment split JSON: %w", err)
	}
	analysis.SuggestedActions = ensureConflictActions(analysis.SuggestedActions, lang)
	return &analysis, nil
}

func ensureConflictActions(actions []ConflictActionOption, lang string) []ConflictActionOption {
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	byID := map[string]ConflictActionOption{}
	for _, a := range actions {
		if a.ID != "" {
			byID[a.ID] = a
		}
	}
	defaults := []ConflictActionOption{
		{ID: "edit_outline", Label: "Edit chapter outline", Description: "Adjust this or later chapter outlines on the Outline page"},
		{ID: "adjust_foreshadow", Label: "Adjust foreshadows", Description: "Change plant/payoff chapters or descriptions on the Foreshadows page, or abandon unworkable foreshadows"},
		{ID: "retry", Label: "Retry after edits", Description: "Regenerate this chapter after you have edited the outline or foreshadows"},
		{ID: "revise_from_feedback", Label: "Rewrite from feedback", Description: "Keep this draft as the base and rewrite it applying the fact-check feedback, then re-run the checks"},
		{ID: "force_review", Label: "Keep draft for review", Description: "Accept the current draft and review it manually"},
	}
	_ = en
	out := make([]ConflictActionOption, 0, len(defaults))
	for _, d := range defaults {
		if a, ok := byID[d.ID]; ok {
			if a.Label == "" {
				a.Label = d.Label
			}
			if a.Description == "" {
				a.Description = d.Description
			}
			out = append(out, a)
		} else if d.ID == "retry" || d.ID == "force_review" || d.ID == "edit_outline" || d.ID == "adjust_foreshadow" || d.ID == "revise_from_feedback" {
			out = append(out, d)
		}
	}
	return out
}

func buildWritingConflict(state *Progress, idx int, failedIssues []string, analysis *writingConflictAnalysis) *WritingConflict {
	ch := state.Chapters[idx]
	summary := "multitimes, squaredirection"
	rootCause := "other"
	reconcilable := false
	actions := ensureConflictActions(nil, i18n.LangEN)
	if analysis != nil {
		if strings.TrimSpace(analysis.Summary) != "" {
			summary = strings.TrimSpace(analysis.Summary)
		}
		rootCause = analysis.RootCause
		reconcilable = analysis.Reconcilable
		if len(analysis.SuggestedActions) > 0 {
			actions = analysis.SuggestedActions
		}
	}
	return &WritingConflict{
		ChapterIndex:     idx,
		ChapterNum:       ch.Num,
		ChapterTitle:     ch.Title,
		Issues:           failedIssues,
		Summary:          summary,
		RootCause:        rootCause,
		Reconcilable:     reconcilable,
		SuggestedActions: actions,
	}
}

func mergeUniqueIssues(issues ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range issues {
		for _, item := range list {
			item = strings.TrimSpace(item)
			if item == "" || seen[item] {
				continue
			}
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

// dedupeArcIssues collapses fact-check issues that describe the SAME root
// event (e.g. seven wordings of "Lord Edmund introduced in chapter 1 while his
// debut belongs to chapter 3"). Without this, every retry and the conflict
// panel repeat the same violation under different phrasings, which both floods
// the context and makes the model think many distinct problems exist.
// Grouping rules (the first wording of each group is kept):
//   - exact key: same normalized character name + same destination chapter;
//   - same violation category AND overlapping character names OR overlapping
//     destination chapters — an issue that shares either anchor with an
//     already-kept issue of the same class describes the same root event.
//
// Issues with different characters/categories/chapters are never merged, so
// genuine multi-problem reports survive.
func dedupeArcIssues(issues []string) []string {
	return dedupeArcIssuesInfo(mapArcIssueInfos(issues))
}

// arcIssueInfo is the parsed fingerprint of one fact-check issue used by the
// dedup passes.
type arcIssueInfo struct {
	text            string
	key             [2]string
	cat             string
	names           map[string]bool
	chrs            map[string]bool
	sceneNames      map[string]bool
	isContradiction bool
}

func mapArcIssueInfos(issues []string) []arcIssueInfo {
	infos := make([]arcIssueInfo, 0, len(issues))
	for _, it := range issues {
		it = strings.TrimSpace(it)
		if it == "" {
			continue
		}
		k := arcIssueKey(it)
		lower := strings.ToLower(it)
		// Names are matched case-insensitively over the whole issue: genuine
		// mentions appear lower-cased inside quoted chapter prose ("…who danced
		// with Cordelia…" → "cordelia"), and relying only on capitalized spans
		// would leave such restatements name-less and unclusterable.
		names := arcDetectedNames(it, lower)
		// A quoted scene that mentions a character already introduced (e.g.
		// just "Cordelia") carries no NEW information about which reserved
		// debut was violated — treat it as name-less so the restatement passes
		// collapse into the concrete cluster instead of surviving on their own.
		if len(names) == 1 && names[knownProtagonist(lower)] {
			names = nil
		}
		infos = append(infos, arcIssueInfo{
			text:            it,
			key:             [2]string{k.ent, k.chRef},
			cat:             arcCategory(it),
			names:           names,
			chrs:            arcAllChapterRefs(lower),
			sceneNames:      arcSceneNames(it),
			isContradiction: arcIsContradiction(it),
		})
	}
	return infos
}

// knownProtagonists are characters who legitimately appear from Chapter 1 on;
// an issue whose only character mention is one of these is really about some
// other (un-named) violation. The set is intentionally tiny — false positives
// here merely make dedup slightly more aggressive, never merge two issues
// about different named characters.
var knownProtagonists = map[string]bool{"cordelia": true}

// castAwareIssueStats reports whether each issue explicitly mentions at least
// one reserved (not-yet-debuted) cast name, case-insensitively over the whole
// wording. Used by the cast-aware dedup path: a premature-intro issue that
// names NONE of the reserved characters is almost always the reviewer
// hallucinating a banned debut onto an anonymous background figure ("a
// laughing couple", "a murmuring group") — such wordings collapse into the
// cluster that actually spells the reserved name out, and when no such cluster
// exists they are dropped entirely (see dropFalsePrematureIssues).
func castAwareIssueStats(issues []string) []bool {
	cores := arcCastCoreNamesCache
	out := make([]bool, len(issues))
	for i, it := range issues {
		lower := strings.ToLower(it)
		for _, core := range cores {
			if core != "" && strings.Contains(lower, core) {
				out[i] = true
				break
			}
		}
	}
	return out
}

// dropFalsePrematureIssues removes premature-introduction wordings that never
// quote any reserved cast member. Genuine violations produced by this pipeline
// always name the character whose debut was staged (the ban list is rendered
// with their names, and the reviewer quotes the offending prose), so a
// "premature" complaint that mentions none of them is speculation about an
// unnamed extra — exactly the false positive reported against the fact-checker.
// Issues of other categories (timeline, recap…) are never touched.
func dropFalsePrematureIssues(issues []string) []string {
	stats := castAwareIssueStats(issues)
	var out []string
	for i, it := range issues {
		if arcCategory(it) == "premature-intro" && !stats[i] {
			// Keep only wordings that quote a reserved cast member with at
			// least 3 characters — honorific-only complaints ("the Lord",
			// "my lady") name nobody and are treated as speculation.
			lower := strings.ToLower(it)
			named := false
			for _, core := range arcCastCoreNamesCache {
				if len([]rune(core)) >= 3 && strings.Contains(lower, core) {
					named = true
					break
				}
			}
			if !named {
				continue
			}
		}
		out = append(out, it)
	}
	return out
}

func knownProtagonist(lower string) string {
	for p := range knownProtagonists {
		if strings.Contains(lower, p) {
			return p
		}
	}
	return ""
}

// arcDetectedNames returns arcNames(issue) plus every cast core name occurring
// in the lower-cased text (used by the cast-aware path to cluster wordings
// that mention reserved debuts without capitalizing them).
func arcDetectedNames(issue, lower string) map[string]bool {
	out := arcNames(issue)
	for _, core := range arcCastCoreNamesCache {
		if !out[core] && strings.Contains(lower, core) {
			out[core] = true
		}
	}
	return out
}

// arcCastCoreNamesCache holds the normalized core names of the structured
// cast for the current dedupe run (set by dedupeArcIssuesWithCast). It lets
// entity-free wordings like "…the stranger …" resolve to "edmund"/"ashworth"
// when those tokens do occur in the text.
var arcCastCoreNamesCache []string

// arcSceneNames returns lower-cased proper names appearing inside quoted
// passages of an issue — these are fragments of the CHAPTER TEXT (the staged
// scene), not meta-phrasing about which chapter owns the event. They let two
// wordings that quote the same sentence cluster together even when neither
// spells out the character whose debut was violated.
func arcSceneNames(issue string) map[string]bool {
	out := map[string]bool{}
	for _, q := range arcQuoteRe.FindAllStringSubmatch(issue, -1) {
		for e := range entitySet(q[1]) {
			if !isArcGenericEntity(e) {
				out[e] = true
			}
		}
	}
	return out
}

var arcQuoteRe = regexp.MustCompile("'([^']{4,})'|\u201c([^\u201d]{4,})\u201d")

func dedupeArcIssuesInfo(infos []arcIssueInfo) []string {
	if len(infos) < 2 {
		out := make([]string, 0, len(infos))
		for _, i := range infos {
			out = append(out, i.text)
		}
		return out
	}
	var kept []arcIssueInfo
	var out []string
	for _, cur := range infos {
		dup := false
		for _, p := range kept {
			if p.key == cur.key && cur.key[0] != "" {
				dup = true
				break
			}
			// Generic "contradiction with the outline" phrasings are almost
			// always restatements of another violation, not problems of their
			// own: drop them when they share a character name or a destination
			// chapter with an already-kept issue. Contradictions that share
			// nothing with what came before (e.g. a genuine recap complaint
			// after only premature-intro issues) survive as real problems.
			if cur.isContradiction && !p.isContradiction &&
				(nameOverlap(p, cur) || chOverlap(p, cur)) {
				dup = true
				break
			}
			if p.cat == "other" || cur.cat == "other" || p.cat != cur.cat {
				continue
			}
			if nameOverlap(p, cur) || chOverlap(p, cur) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		kept = append(kept, cur)
		out = append(out, cur.text)
	}
	if len(out) == 0 {
		out = []string{}
		for _, i := range infos {
			out = append(out, i.text)
		}
		return out
	}
	return out
}

// dedupeArcIssuesWithCast behaves like dedupeArcIssues but first resolves the
// reviewer's oblique mentions ("the stranger", "a dancing lord") to real cast
// names using the structured outline, then runs a two-pass dedup: wordings
// that spell a name out form the primary clusters, and in a second pass the
// unresolved (name-less) wordings are dropped when they share a destination
// chapter or quoted-scene characters with any cluster — an issue that names
// nobody is by construction a restatement of one already captured. Purely
// lexical — no extra LLM call.
func dedupeArcIssuesWithCast(issues []string, state *Progress, idx int) []string {
	// Publish the structured cast's core names for this run so name detection
	// can cluster wordings that mention reserved debuts in lower case. The
	// debut names are already normalized to lower case — entitySet only picks
	// up CAPITALIZED spans, so feeding it lower-cased names yielded nothing
	// and every premature-intro wording was silently dropped as "speculation".
	var cores []string
	for n := range pendingDebutNames(state, idx) {
		cores = append(cores, arcCoreNames(n)...)
	}
	if len(cores) == 0 {
		// No structured reserved-debut cast available (nil state / outline
		// without future debuts): fall back to the plain lexical dedup. In
		// particular we must NOT run dropFalsePrematureIssues here — with an
		// empty ban list every premature-intro wording would "name nobody"
		// and be discarded as speculation, silently swallowing genuine
		// complaints from the reviewer.
		return dedupeArcIssuesTwoPass(issues)
	}
	sort.Strings(cores)
	prev := arcCastCoreNamesCache
	arcCastCoreNamesCache = cores
	defer func() { arcCastCoreNamesCache = prev }()
	return dedupeArcIssuesTwoPass(resolveObliqueArcIssues(dropFalsePrematureIssues(issues), state, idx))
}

// dedupeArcIssuesTwoPass applies dedupeArcIssues, then removes leftover
// name-less issues that overlap an already-kept issue on destination chapters
// or quoted-scene character mentions.
func dedupeArcIssuesTwoPass(issues []string) []string {
	first := mapArcIssueInfos(dedupeArcIssues(issues))
	out := make([]string, 0, len(first))
	kept := make([]arcIssueInfo, 0, len(first))
	for _, cur := range first {
		dup := false
		if len(cur.names) == 0 {
			for _, p := range kept {
				if len(p.names) == 0 {
					continue
				}
				if chOverlap(p, cur) || sceneMentionOverlap(p, cur) ||
					cur.isContradiction {
					dup = true
					break
				}
			}
		}
		if dup {
			continue
		}
		// A generic "contradiction with the outline" wording that names no
		// character is by construction a restatement of some concrete
		// violation already reported — drop it. (Contradictions that DO name
		// someone are kept: they may point at a genuinely different problem.)
		if cur.isContradiction && len(cur.names) == 0 && len(kept) > 0 {
			continue
		}
		kept = append(kept, cur)
		out = append(out, cur.text)
	}
	if len(out) == 0 {
		return issues
	}
	return out
}

// sceneMentionOverlap reports whether both issues quote at least one common
// proper name from the chapter text itself (as opposed to only naming the
// owner chapter of the reserved event). Two wordings of the same staged scene
// almost always quote the same participant ("Cordelia"), which lets a
// name-less restatement collapse into the right cluster even when it cites a
// different destination chapter number.
func sceneMentionOverlap(a, b arcIssueInfo) bool {
	for n := range a.sceneNames {
		if b.sceneNames[n] {
			return true
		}
	}
	return false
}

// resolveObliqueArcIssues rewrites premature-introduction issues that carry no
// detectable proper name but do quote exactly one future-debut cast member.
func resolveObliqueArcIssues(issues []string, state *Progress, idx int) []string {
	if state == nil || idx < 0 || idx >= len(state.Chapters) || len(issues) < 2 {
		return issues
	}
	var pending map[string]bool
	for _, it := range issues {
		if arcCategory(it) != "premature-intro" || len(arcIssueKey(it).ent) > 0 {
			continue
		}
		if pending == nil {
			pending = pendingDebutNames(state, idx)
			if len(pending) == 0 {
				return issues
			}
		}
		found := arcCastMentions(it, pending)
		if len(found) != 1 {
			continue
		}
		// Prepend the resolved name so downstream name-based dedup merges this
		// wording with the cluster that already spells the character out.
		for i, raw := range issues {
			if raw == it {
				issues[i] = titleName(found[0]) + " — " + it
				break
			}
		}
	}
	return issues
}

// pendingDebutNames returns lower-cased full names of characters flagged
// FirstAppearance in chapters AFTER idx (their debut has not occurred yet).
func pendingDebutNames(state *Progress, idx int) map[string]bool {
	out := map[string]bool{}
	for _, d := range futureDebuts(state, idx) {
		n := strings.ToLower(strings.TrimSpace(d.name))
		if n != "" {
			out[n] = true
		}
	}
	return out
}

// arcCastMentions lists cast names mentioned in the text, bounded by
// non-letter characters on both sides (so "edmund" never matches inside a
// longer word), sorted for determinism across map iteration.
func arcCastMentions(text string, cast map[string]bool) []string {
	lower := strings.ToLower(text)
	var found []string
	for name := range cast {
		if mentionsWordBounded(lower, name) {
			found = append(found, name)
		}
	}
	sort.Strings(found)
	return found
}

// mentionsWordBounded reports whether name appears in lower surrounded by
// non-letter runes.
func mentionsWordBounded(lower, name string) bool {
	for start := 0; ; {
		i := strings.Index(lower[start:], name)
		if i < 0 {
			return false
		}
		i += start
		j := i + len(name)
		beforeOK := i == 0 || !isArcLetterByte(lower[i-1])
		afterOK := j >= len(lower) || !isArcLetterByte(lower[j])
		if beforeOK && afterOK {
			return true
		}
		start = i + 1
	}
}

// isArcLetterByte treats ASCII letters/digits and every byte of multi-byte
// UTF-8 sequences as word characters (byte-level checks are safe here because
// continuation/lead bytes of valid UTF-8 are all >= 0x80).
func isArcLetterByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b >= 0x80
}

// titleName upper-cases the first rune of a lower-cased cast name for display.
func titleName(name string) string {
	r := []rune(name)
	if len(r) == 0 {
		return name
	}
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// nameOverlap reports whether two issues mention at least one common core
// character name.
func nameOverlap(a, b arcIssueInfo) bool {
	for n := range a.names {
		if b.names[n] {
			return true
		}
	}
	return false
}

// chOverlap reports whether two issues cite at least one common destination
// chapter number.
func chOverlap(a, b arcIssueInfo) bool {
	for c := range a.chrs {
		if b.chrs[c] {
			return true
		}
	}
	return false
}

var arcChapterRefRe = regexp.MustCompile(`(?i)\b(?:chapter|cap[ií]tulo|cap|capitulo)\s*(\d+)`)

// arcTimelineRe marks issues about in-chapter chronology (night→morning
// reversals, recaps of already-written scenes). Checked before the premature-
// intro class so a "timeline contradiction" is not swallowed by the broad
// contradiction pattern.
var arcTimelineRe = regexp.MustCompile(`(?i)\b(timeline|reverts?|recap\w*|already happened|again told|re-tell\w*)`)

// arcIsContradiction reports whether an issue belongs to the generic
// contradiction/inconsistency family — phrasings that re-describe some other
// root violation rather than a distinct problem of their own.
var arcContradictionRe = regexp.MustCompile(`(?i)\b(contradic\w*|inconsist\w*|violat\w*|against the outline|outline arc)`)

func arcIsContradiction(issue string) bool {
	return !arcTimelineRe.MatchString(issue) && arcContradictionRe.MatchString(issue)
}

// arcGenericTerms are capitalized words that appear inside issue phrasing
// themselves ("Premature", "Contradiction", "Identity"...) and must never be
// treated as characters when computing the dedup key.
var arcGenericTerms = map[string]bool{
	// Reviewer meta-vocabulary (never story entities).
	"premature": true, "previously": true, "premise": true, "present": true,
	"identity": true, "reveal": true, "reveals": true, "introduction": true,
	"introductory": true, "character": true, "characters": true,
	"contradiction": true, "conflict": true, "consistency": true,
	"timeline": true, "motif": true, "event": true, "events": true,
	"outline": true, "arcs": true, "arc": true, "analysis": true,
	"failed": true, "check": true, "checks": true, "summary": true,
	"first": true, "later": true, "latest": true, "background": true,
	"hidden": true, "stranger": true, "meeting": true, "encounter": true,
	"glove": true, "departure": true, "interaction": true, "exchange": true,
	"unrelated": true, "name": true, "names": true, "inconsistency": true,
	"occurs": true, "occur": true, "occurred": true, "planned": true,
	"assigned": true, "belongs": true, "belong": true, "debut": true,
	"debuts": true, "appear": true, "appearance": true, "appears": true,
	"this": true, "that": true, "these": true, "those": true, "the": true,
	"one": true, "key": true, "specific": true, "specifically": true,
	"again": true, "also": true, "thus": true, "therefore": true,
	"which": true, "whose": true, "when": true, "where": true,
	"chapter": true, "capitulo": true, "capítulo": true,
	// Ordinary English words that entitySet picks up from capitalized clause
	// openings inside quoted chapter text ("Not quite the same man...").
	"not": true, "but": true, "and": true, "or": true, "yet": true,
	"so": true, "nor": true, "as": true, "if": true, "while": true,
	"then": true, "hence": true, "however": true,
	"meanwhile": true, "finally": true, "instead": true,
	"never": true, "nothing": true, "someone": true, "somehow": true,
	"every": true, "each": true, "both": true, "either": true, "neither": true,
	"other": true, "others": true, "another": true, "same": true,
	"what": true, "who": true, "whom": true, "why": true,
	"how": true, "there": true, "here": true, "now": true, "soon": true,
	"once": true, "always": true, "often": true, "sometimes": true,
	"perhaps": true, "maybe": true, "indeed": true, "certainly": true,
	"almost": true, "even": true, "just": true, "only": true,
	"more": true, "most": true, "less": true, "least": true, "much": true,
	"many": true, "few": true, "all": true, "any": true, "no": true,
	"yes": true, "still": true, "already": true, "although": true,
	"because": true, "since": true, "until": true, "unless": true,
	"though": true, "after": true, "before": true, "during": true,
	"within": true, "without": true, "toward": true, "upon": true,
}

// arcCommonNouns are lowercase common nouns frequently embedded in multi-word
// entity spans picked up from quoted chapter text. An entity is generic when
// EVERY one of its tokens (honorifics aside) is meta-vocabulary or a common
// noun — a real name like "Lord Edmund Ashworth" always carries at least one
// proper token ("edmund", "ashworth") and survives.
var arcCommonNouns = map[string]bool{
	"man": true, "woman": true, "men": true, "women": true, "boy": true,
	"girl": true, "child": true, "children": true, "person": true,
	"people": true, "figure": true, "gentleman": true,
	"lineage": true, "past": true,
	"gathering": true, "ballroom": true, "stairs": true, "garden": true,
	"door": true, "doors": true, "window": true, "room": true, "house": true,
	"truth": true, "secret": true, "secrets": true, "promise": true,
	"moment": true, "moments": true, "day": true, "days": true, "night": true,
	"nights": true, "morning": true, "evening": true, "year": true,
	"years": true, "time": true, "times": true, "hour": true, "hours": true,
	"voice": true, "voices": true, "murmur": true, "whisper": true,
	"whispers": true, "glance": true, "look": true, "eye": true, "eyes": true,
	"hand": true, "hands": true, "face": true, "smile": true,
	"word": true, "words": true, "silence": true,
	"shadow": true, "shadows": true, "light": true, "darkness": true,
	"crowd": true, "group": true, "couple": true, "couples": true,
	"guest": true, "guests": true, "servant": true, "servants": true,
	"dance": true, "dancer": true, "waltz": true,
	"letter": true, "letters": true, "note": true, "notes": true,
	"news": true, "rumor": true, "rumours": true, "rumors": true,
	"edge": true, "entrance": true, "exit": true, "hall": true, "hallway": true,
	"relief": true, "clarity": true, "attention": true, "presence": true,
	"absence": true, "arrival": true,
	"revelation": true, "discovery": true,
	"scene": true, "scenes": true,
	"plot": true, "story": true, "tale": true,
	"fact": true, "facts": true, "setting": true,
	"settings":    true,
	"protagonist": true, "antagonist": true, "reader": true, "readers": true,
	"author": true, "text": true, "passage": true, "paragraph": true,
	"sentence": true, "sentences": true, "line": true, "lines": true,
	"phrase": true, "phrases": true, "tone": true, "style": true,
	"theme": true, "themes": true, "motifs": true,
	"foreshadowing": true, "appearances": true,
	"identities": true, "history": true, "memory": true, "memories": true,
	"future": true, "futures": true, "destiny": true, "fate": true,
	"world": true, "life": true, "lives": true, "death": true, "love": true,
	"hate": true, "fear": true, "hope": true, "pain": true, "joy": true,
	"sorrow": true, "anger": true, "pride": true, "shame": true, "guilt": true,
	"honor": true, "honour": true, "duty": true, "power": true, "wealth": true,
	"poverty": true, "status": true, "rank": true, "title": true,
	"titles": true, "position": true, "role": true, "roles": true,
	"last": true, "next": true, "previous": true,
	"earlier": true, "new": true, "old": true, "young": true,
	"different": true, "similar": true, "unlike": true,
	"something": true, "anything": true, "everything": true,
	"everybody": true, "nobody": true, "somebody": true, "anyone": true,
	"everyone": true, "strangers": true,
	"friend": true, "friends": true, "enemy": true, "enemies": true,
	"family": true, "father": true, "mother": true, "brother": true,
	"sister": true, "son": true, "daughter": true, "uncle": true,
	"aunt": true, "cousin": true, "husband": true, "wife": true,
}

// isArcGenericEntity reports whether a capitalized token span is just part of
// the reviewer's meta-phrasing or quoted common-noun prose rather than a story
// character name. Single words hit the explicit stoplists; multi-word spans
// are generic only when every meaningful token (honorifics ignored) is either
// meta-vocabulary or a common noun — so genuine names like "Lord Edmund
// Ashworth" survive while "The Curtsy Of Truth" collapses to its proper core.
func isArcGenericEntity(ent string) bool {
	lower := strings.ToLower(strings.TrimSpace(ent))
	if lower == "" {
		return true
	}
	toks := strings.Fields(lower)
	if len(toks) == 1 {
		return arcGenericTerms[lower] || arcCommonNouns[lower]
	}
	meaningful := 0
	for _, t := range toks {
		if arcHonorifics[t] {
			continue
		}
		meaningful++
		if !arcGenericTerms[t] && !arcCommonNouns[t] {
			return false // at least one proper-looking token → real name
		}
	}
	return meaningful == 0
}

// arcHonorifics are titles that prefix character names ("Lord Edmund
// Ashworth", "Countess Vivienne D'Aubrey") and must be stripped from an
// entity's leading tokens before matching, so wordings with and without the
// honorific resolve to the same core name.
var arcHonorifics = map[string]bool{
	"lord": true, "lady": true, "countess": true, "count": true, "duke": true,
	"duchess": true, "baron": true, "baroness": true, "earl": true, "sir": true,
	"madam": true, "mister": true, "master": true, "prince": true,
	"princess": true, "king": true, "queen": true, "mr": true, "mrs": true,
	"ms": true, "dr": true, "don": true, "doña": true, "dona": true,
}

// stripArcHonorifics removes leading (and trailing) honorific tokens from a
// lower-cased multi-word entity name.
func stripArcHonorifics(name string) string {
	toks := strings.Fields(name)
	for len(toks) > 1 && arcHonorifics[toks[0]] {
		toks = toks[1:]
	}
	for len(toks) > 1 && arcHonorifics[toks[len(toks)-1]] {
		toks = toks[:len(toks)-1]
	}
	return strings.Join(toks, " ")
}

// arcCoreNames returns the candidate core names of an entity (lower-cased):
// the full name minus honorifics plus each individual token. A multi-word
// span like "Lord Edmund Ashworth" yields {"edmund ashworth", "lord", ...,
// "edmund", "ashworth"} so it can match the shorter mention "Edmund".
func arcCoreNames(ent string) []string {
	full := stripArcHonorifics(strings.ToLower(ent))
	out := []string{}
	if full != "" && !isArcGenericEntity(full) {
		out = append(out, full)
	}
	for _, tok := range strings.Fields(full) {
		if len([]rune(tok)) >= 3 && !arcGenericTerms[tok] && !arcHonorifics[tok] {
			out = append(out, tok)
		}
	}
	return out
}

// arcCategoryPatterns classify issues by the KIND of arc violation they
// describe. Categories are intentionally coarse: several phrasings of one root
// event (premature debut, hidden-identity hint, motif leak) all belong to the
// same class "premature-intro", so they collapse even when worded differently.
// The catch-all last entry gives unclassified issues a category that never
// matches anything ("other" issues are only merged by exact key), preventing
// accidental merges of unrelated problems.
var arcCategoryPatterns = []struct {
	category string
	re       *regexp.Regexp
}{
	{"timeline", regexp.MustCompile(`(?i)\b(timeline|reverts?|recap\w*|already happened|again told|re-tell\w*)`)},
	{"premature-intro", regexp.MustCompile(`(?i)\b(premature\w*|introduc\w+|first\s+meeting|debut|reveal\w*|allusion|foreshadow\w*|motif|glove|keepsake|token|hidden\s+(background|truth|identity)|unmistakably|who\s+he\s+really)`)},
	{"outline-contradiction", regexp.MustCompile(`(?i)\b(contradic\w*|inconsist\w*|violat\w*|against the outline|outline arc|assigned to later|planned for later|reserved for)`)},
	{"other", nil},
}

// arcCategory returns the violation class of an issue.
func arcCategory(issue string) string {
	for _, p := range arcCategoryPatterns {
		if p.re == nil || p.re.MatchString(issue) {
			return p.category
		}
	}
	return "other"
}

// arcAllChapterRefs returns every chapter number mentioned in the issue
// ("assigned to Chapter 3… investigation begins in Chapter 2" → {"3","2"}).
// Two issues that share at least one destination chapter are considered to
// point at the same owner of the event.
func arcAllChapterRefs(lower string) map[string]bool {
	set := map[string]bool{}
	for _, m := range arcChapterRefRe.FindAllStringSubmatch(lower, -1) {
		set[m[1]] = true
	}
	return set
}

// arcNames returns the set of normalized core character names detected in an
// issue (honorifics stripped, generic meta-words filtered out).
func arcNames(issue string) map[string]bool {
	lower := strings.ToLower(issue)
	out := map[string]bool{}
	for e := range entitySet(issue) {
		if isArcGenericEntity(e) {
			continue
		}
		for _, core := range arcCoreNames(e) {
			if strings.Contains(lower, core) {
				out[core] = true
			}
		}
	}
	return out
}

// arcIssueKey builds the primary dedup key for a fact-check issue: the FIRST
// real character name mentioned (left-to-right), normalized to its core form
// so honorifics and surname variants merge ("Lord Edmund Ashworth" ==
// "Edmund"), plus the first destination chapter number mentioned. Different
// wordings of the same root event collapse into one issue. Issues without a
// detectable name get an empty entity key and are handled by the
// category/name-overlap passes in dedupeArcIssues.
func arcIssueKey(issue string) struct{ ent, chRef string } {
	lower := strings.ToLower(issue)
	type cand struct {
		name string
		pos  int
	}
	var first *cand
	for e := range entitySet(issue) {
		if isArcGenericEntity(e) {
			continue
		}
		for _, core := range arcCoreNames(e) {
			pos := strings.Index(lower, core)
			if pos < 0 {
				continue
			}
			if first == nil || pos < first.pos {
				first = &cand{core, pos}
			}
			break // this entity already contributed its earliest core match
		}
	}
	ent := ""
	if first != nil {
		ent = first.name
	}
	m := arcChapterRefRe.FindStringSubmatch(lower)
	chRef := ""
	if m != nil {
		chRef = m[1]
	}
	return struct{ ent, chRef string }{ent, chRef}
}

func splitFactCheckIssues(issues string) []string {
	if strings.TrimSpace(issues) == "" {
		return nil
	}
	parts := strings.Split(issues, "; ")
	if len(parts) == 1 {
		parts = strings.Split(issues, ";")
	}
	// Trim trailing periods left by the "; ." separator so sentence-ending
	// punctuation does not glue onto the next fragment's first word (e.g.
	// "beatrice." would otherwise stop "Lady Beatrice" from being detected).
	for i, p := range parts {
		parts[i] = strings.TrimRight(strings.TrimSpace(p), ".")
	}
	return dedupeArcIssues(mergeUniqueIssues(parts))
}

// extractQuotedExcerpt returns the first quoted passage inside an issue
// wording (single or double quotes, ≥4 chars — the critic quotes the offending
// prose verbatim). Used to locate the owning trope unit for surgical repairs.
var conflictQuoteRe = regexp.MustCompile("'([^']{4,})'|\u201c([^\u201d]{4,})\u201d|\"([^\"]{4,})\"")

func extractQuotedExcerpt(issue string) string {
	for _, m := range conflictQuoteRe.FindAllStringSubmatch(issue, -1) {
		for _, g := range m[1:] {
			if g != "" {
				return strings.TrimSpace(g)
			}
		}
	}
	return ""
}

// attachConflictSceneTargets resolves, for every issue wording, the
// (scene position, trope-unit index) whose prose contains the quoted excerpt.
// Unlocatable issues get (-1,-1) so the returned slice always aligns 1:1 with
// issues. Purely lexical — no extra LLM call.
func attachConflictSceneTargets(state *Progress, idx int, issues []string) [][2]int {
	out := make([][2]int, 0, len(issues))
	if state == nil || idx < 0 || idx >= len(state.Chapters) {
		for range issues {
			out = append(out, [2]int{-1, -1})
		}
		return out
	}
	ch := &state.Chapters[idx]
	for _, it := range issues {
		q := extractQuotedExcerpt(it)
		if q == "" || ch.ScenePlan == nil || strings.TrimSpace(ch.Content) == "" {
			out = append(out, [2]int{-1, -1})
			continue
		}
		pos, ti, ok := locateTropeUnitInContent(ch, ch.Content, q)
		if !ok {
			out = append(out, [2]int{-1, -1})
			continue
		}
		out = append(out, [2]int{pos, ti})
	}
	return out
}

// ensureSurgicalAction adds the "repair only this trope unit" action as the
// FIRST suggested option whenever the conflict carries at least one locatable
// scene target — a single-unit fix is far cheaper than rewriting the chapter.
func ensureSurgicalAction(actions []ConflictActionOption, targets [][2]int, lang string) []ConflictActionOption {
	locatable := false
	for _, t := range targets {
		if t[0] >= 0 {
			locatable = true
			break
		}
	}
	if !locatable {
		return actions
	}
	for _, a := range actions {
		if a.ID == "surgical_trope" {
			return actions
		}
	}
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	label := "Editar solo este tropo"
	desc := "Reescribe únicamente la unidad de tropo donde vive el problema; el resto del capítulo queda intacto"
	if en {
		label = "Edit only this trope"
		desc = "Rewrite just the trope unit carrying the problem; the rest of the chapter stays untouched"
	}
	out := make([]ConflictActionOption, 0, len(actions)+1)
	out = append(out, ConflictActionOption{ID: "surgical_trope", Label: label, Description: desc})
	return append(out, actions...)
}

// finalizeConflict decorates a freshly built conflict with per-issue trope
// locations and the surgical repair action. Callers must invoke it AFTER the
// chapter's final content/blocks are in state (so quote→unit matching works).
func finalizeConflict(state *Progress, idx int, c *WritingConflict, lang string) {
	if c == nil {
		return
	}
	c.SceneTargets = attachConflictSceneTargets(state, idx, c.Issues)
	c.SuggestedActions = ensureSurgicalAction(c.SuggestedActions, c.SceneTargets, lang)
	preserveSurgicalHistory(state, c)
	if state != nil && idx >= 0 && idx < len(state.Chapters) {
		refreshTropeIssuesWithConflict(&state.Chapters[idx], c)
	}
}

// refreshTropeIssuesWithConflict rebuilds the per-trope-unit roll-up of OPEN
// editorial findings for one chapter: unresolved fact-check/conflict issues
// (located via the pending conflict's SceneTargets) plus PENDING bestseller
// critic interventions. The block viewer uses it to paint colored dots on the
// affected trope blocks (red = fact-check, violet = critic), each dot opening
// a tooltip with the specific issue wordings. Call it whenever ch.Analysis or
// the chapter's live conflict changes; pure and idempotent.
func refreshTropeIssuesWithConflict(ch *ChapterState, conflict *WritingConflict) {
	if ch == nil {
		return
	}
	type key struct{ s, t int }
	fact := map[key][]string{}
	critic := map[key][]string{}
	add := func(m map[key][]string, k key, s string) {
		s = strings.TrimSpace(s)
		if s == "" || k.s < 0 || k.t < 0 {
			return
		}
		for _, prev := range m[k] {
			if normConflictIssue(prev) == normConflictIssue(s) {
				return // dedupe identical wordings
			}
		}
		m[k] = append(m[k], draftTruncate(s, 320))
	}
	if ch.Analysis != nil {
		for _, sg := range ch.Analysis.Suggestions {
			for _, iv := range sg.Interventions {
				if iv.Status == "applied" {
					continue
				}
				txt := iv.Feedback
				if label := strings.TrimSpace(iv.TropeLabel); label != "" {
					txt = "[" + label + "] " + txt
				}
				add(critic, key{iv.ScenePos, iv.TropeIndex}, txt)
			}
		}
	}
	if conflict != nil && conflict.ChapterNum == ch.Num {
		for i, iss := range conflict.Issues {
			if i >= len(conflict.SceneTargets) {
				break
			}
			t := conflict.SceneTargets[i]
			add(fact, key{t[0], t[1]}, iss)
		}
	}
	keys := make([]key, 0, len(fact)+len(critic))
	for k := range fact {
		keys = append(keys, k)
	}
	for k := range critic {
		if _, dup := fact[k]; !dup {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a].s != keys[b].s {
			return keys[a].s < keys[b].s
		}
		return keys[a].t < keys[b].t
	})
	groups := make([]TropeIssueGroup, 0, len(keys))
	for _, k := range keys {
		g := TropeIssueGroup{ScenePos: k.s, TropeIndex: k.t}
		if fs := fact[k]; len(fs) > 0 {
			g.Fact = fs
		}
		if cs := critic[k]; len(cs) > 0 {
			g.Critic = cs
		}
		groups = append(groups, g)
	}
	if len(groups) == 0 {
		ch.TropeIssues = nil
		return
	}
	ch.TropeIssues = groups
}

// RefreshTropeIssuesForChapter is the exported entry point used by the block
// editor handlers: after a surgical single-block edit, rebuild that chapter's
// trope_issues roll-up (fact dots from the live pending conflict + critic dots
// from PENDING interventions) so the colored markers match the edited text.
// It never mutates the conflict or analysis records themselves — only the
// per-unit mapping — and is safe to call on chapters without either source.
func RefreshTropeIssuesForChapter(state *Progress, ch *ChapterState) {
	if state == nil || ch == nil {
		return
	}
	refreshTropeIssuesWithConflict(ch, state.PendingWritingConflict)
}

// refreshTropeIssuesForInterventions re-derives the trope-issue dots for EVERY
// chapter touched by the given intervention set, using each chapter's own live
// conflict when available. Propose* paths only hold the pending conflict of
// the chapter currently being written, so a plain per-chapter refresh would
// silently drop the red fact-check dots of other chapters; this variant walks
// the affected chapters and rebuilds each one against its own state.
func refreshTropeIssuesForInterventions(state *Progress, interventions []TropeIntervention) {
	if state == nil {
		return
	}
	seen := map[int]bool{}
	for _, iv := range interventions {
		if seen[iv.ChapterNum] {
			continue
		}
		seen[iv.ChapterNum] = true
		for i := range state.Chapters {
			if state.Chapters[i].Num != iv.ChapterNum {
				continue
			}
			var conflict *WritingConflict
			if state.PendingWritingConflict != nil &&
				state.PendingWritingConflict.ChapterNum == iv.ChapterNum {
				conflict = state.PendingWritingConflict
			}
			refreshTropeIssuesWithConflict(&state.Chapters[i], conflict)
			break
		}
	}
}

// preserveSurgicalHistory carries the surgical-repair bookkeeping from the
// conflict being replaced (state.PendingWritingConflict) onto a freshly built
// one, so "rewrite from feedback" loops cannot silently reset the attempt
// counter. When the same trope unit fails again after an already-applied
// surgery, the loop is not a prose problem — the surgical option is demoted to
// LAST and its description tells the author to fix the plan/facts instead of
// clicking the same button again.
func preserveSurgicalHistory(state *Progress, c *WritingConflict) {
	prev := state.PendingWritingConflict
	if prev == nil || c == nil || prev.ChapterIndex != c.ChapterIndex {
		return
	}
	c.SurgicalAttempts = prev.SurgicalAttempts
	c.LastSurgicalTarget = prev.LastSurgicalTarget
	c.ReviseAttempts = prev.ReviseAttempts
	if c.LastFailure == nil {
		c.LastFailure = prev.LastFailure
	}
	if c.SurgicalAttempts <= 0 {
		return
	}
	for i, a := range c.SuggestedActions {
		if a.ID != "surgical_trope" {
			continue
		}
		a.Description = fmt.Sprintf("Already repaired %d time(s) on the same trope unit without resolving the issue — this contradiction lives in the plan/facts, not in the local prose. Prefer revising the scene plan or the fact entries.", c.SurgicalAttempts)
		c.SuggestedActions = append(append(c.SuggestedActions[:i], c.SuggestedActions[i+1:]...), a)
		return
	}
}

// RecordReviseAttempt bumps the whole-chapter "rewrite from feedback" counter
// on the pending conflict BEFORE the pass starts, so the successor conflict
// raised by finalizeConflict (via preserveSurgicalHistory) carries an accurate
// attempt number and the UI can show "attempt #N" instead of pretending every
// retry is fresh. Safe to call with no pending conflict.
func RecordReviseAttempt(state *Progress) {
	if state == nil || state.PendingWritingConflict == nil {
		return
	}
	state.PendingWritingConflict.ReviseAttempts++
}

// recordReviseLoopFailure explains, on a successor conflict raised by the
// "rewrite from feedback" pass, WHY the same issue came back even though the
// pass ran to completion (no hard error). Without this note the UI shows a
// fresh-looking conflict after every attempt and the author cannot tell
// whether surgery was applied, failed, or is looping on the same trope unit.
// prev is the snapshot taken at entry; repaired/failed are this pass's surgical
// counters. No-op when nothing worth explaining happened.
func recordReviseLoopFailure(cur, prev *WritingConflict, repaired, failed int) {
	if cur == nil || prev == nil {
		return
	}
	looping := SameSurgicalTargetFailedAgain(prev, cur)
	if !looping && repaired == 0 && failed == 0 {
		return // first plain rewrite: nothing to diagnose yet
	}
	reason := ""
	switch {
	case looping:
		reason = fmt.Sprintf("The flagged contradiction survived %d surgical repair(s) on the SAME trope unit plus a full-chapter rewrite — it lives in the scene plan / established facts, not in the local prose. Edit the scene plan or the fact entries directly; retrying the rewrite will keep reproducing it.", prev.SurgicalAttempts)
	case failed > 0 && repaired > 0:
		reason = fmt.Sprintf("%d trope-unit repair(s) were applied but %d failed with API errors during this pass; the remaining issues went through the full rewrite, which did not clear them either. Retry the ones that errored, or edit the scene/facts manually.", repaired, failed)
	case failed > 0:
		reason = fmt.Sprintf("All %d trope-unit repair(s) errored out (API/provider failures), so only the full-chapter rewrite ran — and it reproduced the same issue. Check the API settings, then retry.", failed)
	default:
		reason = fmt.Sprintf("%d trope-unit repair(s) were applied successfully, yet the verification pass still flags the same scene — the contradiction most likely comes from the scene plan or an established fact, not the prose. Prefer editing those instead of rewriting again.", repaired)
	}
	f := &ConflictFailure{
		Stage:            "revise",
		Reason:           reason,
		Time:             time.Now().UTC().Format(time.RFC3339),
		KeptDraft:        true,
		ChapterNum:       cur.ChapterNum,
		FailedCount:      cur.ReviseAttempts + cur.SurgicalAttempts,
		SurgicalRepaired: repaired,
		SurgicalFailed:   failed,
	}
	cur.LastFailure = f
}

// RecordConflictFailure attaches a machine-readable explanation of WHY the
// last repair attempt did not resolve the conflict to whichever conflict ends
// up pending: if the pass raised a successor (still failing checks) the note
// lands on it; if the pass errored out before raising one, the orphaned draft
// gets a minimal conflict carrying just the failure + the always-available
// actions, so the UI keeps offering Retry / Rewrite / Keep-for-review instead
// of a dead end ("failed and won't let me retry").
func RecordConflictFailure(state *Progress, progressPath string, stage string, cause error, logger *sse.LogBroadcaster) {
	if state == nil {
		return
	}
	reason := "unknown error"
	if cause != nil {
		if s := strings.TrimSpace(cause.Error()); s != "" {
			reason = s
		}
	}
	// The raw LLM/HTTP error text is opaque to the author; classify it so the
	// panel can say WHY the repair failed (API misconfigured / network timeout /
	// provider error / empty prose) and that retrying is always allowed.
	if s := strings.ToLower(reason); isLLMConfigReason(s) {
		reason = "The API configuration is invalid or missing (provider key/base URL). Fix it in Settings — retrying without changing it will fail again."
	} else if isTimeoutReason(s) {
		reason = "A network/API timeout interrupted the rewrite (the model call took too long). Your draft was kept untouched — you can retry safely."
	}
	f := &ConflictFailure{
		Stage:     stage,
		Reason:    reason,
		Time:      time.Now().UTC().Format(time.RFC3339),
		KeptDraft: true,
	}
	if c := state.PendingWritingConflict; c != nil {
		// Successor conflict already raised: explain the loop on top of it.
		f.FailedCount = c.ReviseAttempts + c.SurgicalAttempts
		if c.ChapterIndex >= 0 && c.ChapterIndex < len(state.Chapters) {
			f.ChapterNum = state.Chapters[c.ChapterIndex].Num
		}
		c.LastFailure = f
		_ = SaveProgress(progressPath, state)
		if logger != nil {
			logger.WarnKey("log.conflict_failure_recorded", f.Stage, f.Reason)
		}
		return
	}
	idx := -1
	for i := range state.Chapters {
		if state.Chapters[i].Status == StatusWriting {
			idx = i
			break
		}
	}
	if idx < 0 {
		// Nothing mid-writing: nowhere to attach the note; log only.
		if logger != nil {
			logger.WarnKey("log.conflict_failure_dropped", stage, reason)
		}
		return
	}
	ch := &state.Chapters[idx]
	f.ChapterNum = ch.Num
	c := buildWritingConflict(state, idx, nil, nil)
	c.ReviseAttempts = 0
	c.SurgicalAttempts = 0
	c.LastFailure = f
	finalizeConflict(state, idx, c, i18n.LangEN)
	state.PendingWritingConflict = c
	_ = SaveProgress(progressPath, state)
	if logger != nil {
		logger.WritingConflict(c)
		logger.WarnKey("log.conflict_failure_recorded", f.Stage, f.Reason)
	}
}

// isLLMConfigReason reports whether a failure string means the API config is
// invalid/missing (retrying without fixing Settings will fail again).
func isLLMConfigReason(s string) bool {
	return strings.Contains(s, "api key") || strings.Contains(s, "configuraci") ||
		strings.Contains(s, "config invalid") || strings.Contains(s, "missing api") ||
		strings.Contains(s, "validate config") || strings.Contains(s, "base url")
}

// isTimeoutReason reports whether a failure string is a network/API timeout —
// transient by nature, so the UI should always offer a safe retry.
func isTimeoutReason(s string) bool {
	return strings.Contains(s, "timeout") || strings.Contains(s, "timed out") ||
		strings.Contains(s, "context deadline") || strings.Contains(s, "deadline exceeded") ||
		strings.Contains(s, "i/o timeout") || strings.Contains(s, "connection reset")
}

// SurgicalTropeRepair rewrites ONLY the trope unit (scenePos, beatIdx) of the
// conflicted chapter via the same prompt/length-guard machinery the critic's
// automatic repair pass uses, then re-syncs scene-aware blocks and persists
// progress. The rest of the chapter stays byte-identical. Returns the new
// full content.
func SurgicalTropeRepair(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, progressPath string, idx int, scenePos, beatIdx int, feedback string, settings *ProjectSettings, logger *sse.LogBroadcaster) (string, error) {
	if err := llm.ValidateConfig(apiCfg); err != nil {
		return "", err
	}
	if idx < 0 || idx >= len(state.Chapters) {
		return "", fmt.Errorf("índice de capítulo inválido")
	}
	ch := &state.Chapters[idx]
	if strings.TrimSpace(ch.Content) == "" {
		return "", fmt.Errorf("chapter %d has no generated content yet", ch.Num)
	}
	newContent, err := rewriteSceneTrope(ctx, apiCfg, cfg, state, idx, settings, scenePos, beatIdx, feedback, logger)
	if err != nil {
		return "", err
	}
	ch = &state.Chapters[idx]
	ch.Content = newContent
	SyncChapterBlocksSceneAware(ch, newContent)
	SaveChapterMarkdown(filepath.Dir(progressPath), *ch, state.Title)
	if err := SaveProgress(progressPath, state); err != nil {
		return "", err
	}
	return newContent, nil
}

// ResolveForceReviewIndex picks the chapter to promote writing→review:
// pending conflict chapter if set, otherwise the writing frontier chapter.
func ResolveForceReviewIndex(state *Progress) (int, error) {
	if state == nil {
		return -1, fmt.Errorf("no hay un capítulo en estado de avance")
	}
	if c := state.PendingWritingConflict; c != nil {
		if c.ChapterIndex < 0 || c.ChapterIndex >= len(state.Chapters) {
			return -1, fmt.Errorf("índice de capítulo inválido")
		}
		return c.ChapterIndex, nil
	}
	i := state.CurrentChapterIndex
	if i >= 0 && i < len(state.Chapters) && state.Chapters[i].Status == StatusWriting {
		return i, nil
	}
	return -1, fmt.Errorf("there is already a confirmed chapter ready to advance into the writing stage")
}

// PromoteWritingToReview moves the chapter at idx to review and clears any pending conflict.
func PromoteWritingToReview(state *Progress, idx int) error {
	if state == nil || idx < 0 || idx >= len(state.Chapters) {
		return fmt.Errorf("índice de capítulo inválido")
	}
	// The user chose to keep the conflicted draft in review: capture its
	// unresolved fact-check issues as continuity notes before the conflict is
	// cleared, so later chapters and outline plannings avoid repeating them.
	if c := state.PendingWritingConflict; c != nil && c.ChapterIndex == idx && len(state.Chapters[idx].ContinuityNotes) == 0 {
		notes := []string{}
		for _, issue := range c.Issues {
			if n := strings.TrimSpace(issue); n != "" {
				notes = append(notes, Truncate(n, 200))
			}
		}
		if len(notes) > 0 {
			state.Chapters[idx].ContinuityNotes = notes[:min(len(notes), 6)]
		}
	}
	state.Chapters[idx].Status = StatusReview
	state.PendingWritingConflict = nil
	return nil
}

// ReviseFromConflictAction rewrites the conflicted chapter IN PLACE from its
// current draft — not from scratch — applying the pending fact-check feedback,
// then re-runs the full post-writing analysis (summary → fact-check →
// foreshadows → memory). If the revised draft still fails the arc checks, a
// fresh conflict is raised with the remaining issues so the user can iterate.
func ReviseFromConflictAction(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, progressPath string, settings *ProjectSettings, skills []Skill, logger *sse.LogBroadcaster) error {
	if err := llm.ValidateConfig(apiCfg); err != nil {
		return err
	}
	conflict := state.PendingWritingConflict
	if conflict == nil {
		return fmt.Errorf("writing_conflict_none")
	}
	idx := conflict.ChapterIndex
	if idx < 0 || idx >= len(state.Chapters) {
		return fmt.Errorf("índice de capítulo inválido")
	}
	feedbackIssues := append([]string(nil), conflict.Issues...)
	feedbackSummary := conflict.Summary
	surgicalBefore := conflict.SurgicalAttempts
	// Snapshot of the incoming conflict: finalizeConflict clears
	// state.PendingWritingConflict before this function reaches the successor
	// check below, so the loop diagnosis at the end compares against this copy.
	prevConflict := conflict

	// Surgical-first: fact-check issues that quote offending prose have been
	// located down to a single trope unit (SceneTargets). Rewriting those units
	// surgically BEFORE any whole-chapter pass is what makes this path actually
	// fix the flagged contradiction — a full rewrite regenerates the prose from
	// the same outline and tends to reproduce the very same issue ("rewrite from
	// feedback keeps failing on the same thing"). Only unlocatable issues (or
	// leftovers after surgery) go through the classic full-chapter revision.
	var surgicalTargets [][2]int
	seenTarget := map[[2]int]bool{}
	for _, t := range conflict.SceneTargets {
		if t[0] >= 0 && t[1] >= 0 && !seenTarget[t] {
			seenTarget[t] = true
			surgicalTargets = append(surgicalTargets, t)
		}
	}
	if len(surgicalTargets) > 8 {
		surgicalTargets = surgicalTargets[:8] // cost guard: at most 8 unit rewrites
	}
	// Loop-diagnosis counters: how many trope-unit surgeries this pass
	// completed vs. errored. Surfaced on the successor conflict's last_failure
	// so the panel can explain WHY the same issue came back.
	var surgicalRepaired, surgicalFailed int
	ch := &state.Chapters[idx]
	if len(surgicalTargets) > 0 && strings.TrimSpace(ch.Content) != "" {
		logger.StepInfo(1, 4, "Applying surgical trope-unit repairs for the locatable issues before touching the rest...")
		done := 0
		failedTargets := map[[2]int]bool{}
		for _, t := range surgicalTargets {
			if ctx.Err() != nil {
				return fmt.Errorf("generación cancelada")
			}
			var parts []string
			if strings.TrimSpace(feedbackSummary) != "" {
				parts = append(parts, feedbackSummary)
			}
			for i, iss := range feedbackIssues {
				if i < len(conflict.SceneTargets) && conflict.SceneTargets[i] == t {
					parts = append(parts, "- "+iss)
				}
			}
			if len(parts) == 0 {
				continue
			}
			done++
			chNum := ch.Num
			factSkills := ResolveSkills(skills, cfg.SkillConfig, SkillScopeChapterFactCheck, cfg.Language)
			factCtx := llm.WithPromptAddon(ctx, FormatSkillsContent(factSkills))
			if _, err := SurgicalTropeRepair(factCtx, apiCfg, cfg, state, progressPath, idx, t[0], t[1], strings.Join(parts, "\n"), settings, logger); err != nil {
				if ctx.Err() != nil {
					return fmt.Errorf("generación cancelada")
				}
				logger.WarnKey("log.trope_intervention_failed", chNum, t[0], t[1], err)
				failedTargets[t] = true
				surgicalFailed++
				continue
			}
			surgicalRepaired++
			// Persist the attempt bookkeeping immediately: if the follow-up check
			// raises a successor conflict, preserveSurgicalHistory carries these
			// numbers over so the UI can flag a looping surgical repair.
			conflict.SurgicalAttempts = surgicalBefore + done
			conflict.LastSurgicalTarget = t
			state.PendingWritingConflict = conflict
			_ = SaveProgress(progressPath, state)
			ch = &state.Chapters[idx]
		}
		if done > 0 {
			// Issues whose unit was successfully repaired leave the full-revision
			// backlog; failed/unlocatable ones stay and drive the classic rewrite.
			var remaining []string
			for i, iss := range feedbackIssues {
				if i < len(conflict.SceneTargets) {
					t := conflict.SceneTargets[i]
					if seenTarget[t] && !failedTargets[t] {
						continue
					}
				}
				remaining = append(remaining, iss)
			}
			feedbackIssues = remaining
			if len(feedbackIssues) == 0 {
				feedbackSummary = ""
			}
		}
	}

	// Clear the conflict up front: if the revision regenerates a failing draft,
	// buildWritingConflict will set a fresh one with the remaining issues.
	// preserveSurgicalHistory (inside finalizeConflict) carries the surgical
	// attempt bookkeeping onto the successor so repeated loops stay visible.
	state.PendingWritingConflict = nil
	if err := SaveProgress(progressPath, state); err != nil {
		return err
	}

	ch = &state.Chapters[idx]
	if strings.TrimSpace(ch.Content) == "" {
		return fmt.Errorf("the conflicted chapter has no draft content to revise")
	}
	if len(feedbackIssues) == 0 && strings.TrimSpace(feedbackSummary) == "" {
		// Every issue was handled by surgical repairs — no full rewrite needed.
		// Re-run the verification pass so the chapter ends review-ready or raises
		// a fresh conflict with updated targets.
		if err := FactCheckChapterAction(ctx, apiCfg, cfg, state, progressPath, settings, skills, ch.Num, logger); err != nil {
			return err
		}
		logger.SuccessKey("log.chapter_revised")
		return nil
	}
	ch.Status = StatusWriting
	logger.InfoKey("log.chapter_modifying", ch.Num, ch.Title)

	feedback := strings.TrimSpace(feedbackSummary)
	if len(feedbackIssues) > 0 {
		list := make([]string, 0, len(feedbackIssues))
		for _, it := range feedbackIssues {
			if t := strings.TrimSpace(it); t != "" {
				list = append(list, "- "+t)
			}
		}
		if strings.TrimSpace(feedback) != "" {
			feedback += "\n"
		}
		feedback += "Fact-check issues:\n" + strings.Join(list, "\n")
	}

	logger.StepInfo(1, 4, "Rewriting the chapter from its current draft, applying the fact-check feedback...")
	// Re-inject the hard sequencing constraints (debut ban + reserved events)
	// so the rewrite removes the premature material without replacing it with
	// an equally premature substitute.
	var arcCtx strings.Builder
	if block := buildHardDebutBlock(state, idx, cfg.Language); block != "" {
		arcCtx.WriteString(block)
	}
	if eventLines := buildPrematureEventLines(state, idx, cfg.Language); len(eventLines) > 0 {
		arcCtx.WriteString(strings.Join(eventLines, ""))
	}
	revisedContent, err := reviseChapterContentStream(ctx, apiCfg, cfg, state, idx, feedback, settings, logger, arcCtx.String())
	if err != nil {
		return fmt.Errorf("chapter revision: %w", err)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("generación cancelada")
	}
	if strings.TrimSpace(revisedContent) == "" {
		return fmt.Errorf("la generación de prosa falló o fue cancelada")
	}
	// Length guard for conflict rewrites: removing a premature scene must not
	// silently shrink the chapter far below the configured word band.
	origContent := ch.Content
	targetWords, _ := resolveChapterTargetWords(state, cfg)
	minLen, maxLen := calcChapterLengthRange(targetWords)
	revisedContent, _, err = enforceRevisionLength(ctx, apiCfg, cfg, origContent, revisedContent, feedback, minLen, maxLen, logger,
		func(guardedFeedback string) (string, error) {
			return reviseChapterContentStream(ctx, apiCfg, cfg, state, idx, guardedFeedback, settings, logger, arcCtx.String())
		})
	if err != nil {
		return fmt.Errorf("chapter revision: %w", err)
	}
	ch.Content = revisedContent
	logger.InfoKey("log.prose_revised", prose.CountProseUnits(revisedContent))

	logger.StepInfo(2, 4, "Resumiendo este capítulo para el historial...")
	if _, err := finalizeChapterSummary(ctx, apiCfg, cfg, state, idx, progressPath, logger, ch.Content); err != nil {
		return err
	}
	ch = &state.Chapters[idx]
	logger.InfoKey("log.summary_done")

	logger.StepInfo(3, 4, "Verifying this chapter against established facts...")
	factSkills := ResolveSkills(skills, cfg.SkillConfig, SkillScopeChapterFactCheck, cfg.Language)
	factCtx := llm.WithPromptAddon(ctx, FormatSkillsContent(factSkills))
	historySummary := buildHistorySummary(state, idx)
	factCheckResult := generateChapterFactCheckWithRetryLog(factCtx, apiCfg, cfg, state, idx, ch.Content, historySummary, logger)

	failed, issues := parseFactCheckResult(factCheckResult)
	if failed {
		newIssues := dedupeArcIssuesWithCast(splitFactCheckIssues(issues), state, idx)
		var analysis *writingConflictAnalysis
		if a, err := analyzeWritingConflict(ctx, apiCfg, cfg, state, idx, ch.Content, newIssues, logger); err == nil {
			analysis = a
		} else {
			logger.WarnKey("log.conflict_analyze_failed", err)
		}
		conflict := buildWritingConflict(state, idx, newIssues, analysis)
		conflict.SuggestedActions = ensureConflictActions(conflict.SuggestedActions, cfg.Language)
		finalizeConflict(state, idx, conflict, cfg.Language)
		recordReviseLoopFailure(conflict, prevConflict, surgicalRepaired, surgicalFailed)
		state.PendingWritingConflict = conflict
		if err := SaveProgress(progressPath, state); err != nil {
			return err
		}
		logger.WritingConflict(conflict)
		return &WritingConflictError{Conflict: conflict}
	}
	logger.InfoKey("log.factcheck_pass")

	logger.StepInfo(4, 4, "Syncing narrative memory...")
	if len(state.Foreshadows) > 0 {
		syncForeshadowsAfterChapter(ctx, apiCfg, cfg, state, idx, progressPath, logger)
	}
	syncMemoryAfterChapter(ctx, apiCfg, cfg, state, idx, progressPath, logger)
	ch = &state.Chapters[idx]
	SaveChapterMarkdown(filepath.Dir(progressPath), *ch, state.Title)

	ch.Status = StatusReview
	if err := SaveProgress(progressPath, state); err != nil {
		return err
	}
	logger.SuccessKey("log.chapter_revised")
	return nil
}
