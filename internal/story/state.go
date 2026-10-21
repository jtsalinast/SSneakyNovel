package story

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"showmethestory/internal/config"
	"showmethestory/internal/fsutil"
)

type ChapterState struct {
	KnowledgeTracked bool   `json:"knowledge_tracked,omitempty"`
	MemoryRevision   string `json:"memory_revision,omitempty"`
	Inherited        bool   `json:"inherited,omitempty"`
	Num              int    `json:"num"`
	Title            string `json:"title"`
	Outline          string `json:"outline"`
	// Characters is the structured cast for this chapter's outline (proper names only).
	Characters []OutlineChapterCharacter `json:"characters,omitempty"`
	Content    string                    `json:"content,omitempty"`
	Summary    string                    `json:"summary"`
	Status     string                    `json:"status"` // pending | writing | review | accepted
	// ContinuityNotes records the fact-check issues that were resolved while
	// writing this chapter (what had to be fixed so it stayed consistent). When
	// later chapters or outline revisions are generated, these notes are fed as
	// hard continuity constraints so the same mistakes are not repeated.
	ContinuityNotes []string `json:"continuity_notes,omitempty"`
	// WordCount is the prose-unit count of Content, refreshed on save so the
	// frontend can show word counts without fetching full chapter content.
	WordCount int `json:"word_count,omitempty"`
	// ContentRev is a content hash for API responses; the frontend uses it to
	// invalidate its per-chapter content cache. Never persisted.
	ContentRev string `json:"content_rev,omitempty"`
	// DraftSavedAt marks when an interrupted write checkpointed its prose into
	// Content (see saveChapterDraftCheckpoint). Set when GenerateChapterAction
	// aborts AFTER expensive prose was already produced (summary/critic step
	// failed, task deadline hit, manual stop), so the draft survives reloads and
	// can be resumed from the failed step instead of being thrown away. Cleared
	// when the chapter completes normally or is reset.
	DraftSavedAt string `json:"draft_saved_at,omitempty"`
	// Blocks is the editable-block view of Content (one block per paragraph,
	// stable IDs). Persisted in the chapter file, stripped from progress.json
	// and /api/progress; ships with GET /api/chapters/{num}.
	Blocks      []Block `json:"blocks,omitempty"`
	NextBlockID int     `json:"next_block_id,omitempty"`
	BlockSep    string  `json:"block_sep,omitempty"`
	// ScenePlan is the editable beat sheet generated before writing this
	// chapter (see scene_plan.go). nil until the author (or auto mode)
	// requests a plan. Persisted with the chapter so it survives reloads.
	ScenePlan *ScenePlan `json:"scene_plan,omitempty"`
	// Analysis is the bestseller-editor pass over THIS DRAFTED CHAPTER
	// (POST /api/chapters/{num}/analyze): pillar scores plus suggestions
	// whose interventions target concrete trope units of the written prose.
	// nil until the author runs an analysis on this chapter.
	Analysis *ChapterAnalysisReport `json:"analysis,omitempty"`
}

type ForeshadowStatus string

const (
	ForeshadowPlanted     ForeshadowStatus = "planted"
	ForeshadowProgressing ForeshadowStatus = "progressing"
	ForeshadowResolved    ForeshadowStatus = "resolved"
	ForeshadowAbandoned   ForeshadowStatus = "abandoned"
)

type ForeshadowEvent struct {
	Chapter int    `json:"chapter"`
	Note    string `json:"note"`
}

type Foreshadow struct {
	ID            int               `json:"id"`
	Inherited     bool              `json:"inherited,omitempty"`
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	PlantChapter  int               `json:"plant_chapter"`
	TargetChapter int               `json:"target_chapter,omitempty"`
	TargetHorizon string            `json:"target_horizon,omitempty"`
	Status        ForeshadowStatus  `json:"status"`
	Events        []ForeshadowEvent `json:"events"`
	Resolution    string            `json:"resolution"`
}

type ForeshadowOutlineConflict struct {
	ForeshadowID   int    `json:"foreshadow_id"`
	ForeshadowName string `json:"foreshadow_name"`
	ConflictType   string `json:"conflict_type"`
	Description    string `json:"description"`
	SuggestedFix   string `json:"suggested_fix"`
}

type ForeshadowOutlineReport struct {
	HasConflicts bool                        `json:"has_conflicts"`
	Conflicts    []ForeshadowOutlineConflict `json:"conflicts"`
	Summary      string                      `json:"summary"`
}

type ConflictActionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type WritingConflict struct {
	ChapterIndex     int                    `json:"chapter_index"`
	ChapterNum       int                    `json:"chapter_num"`
	ChapterTitle     string                 `json:"chapter_title"`
	Issues           []string               `json:"issues"`
	Summary          string                 `json:"summary"`
	RootCause        string                 `json:"root_cause"`
	Reconcilable     bool                   `json:"reconcilable"`
	SuggestedActions []ConflictActionOption `json:"suggested_actions"`
	// SceneTargets locates the (scene position, trope-unit index) owning each
	// entry of Issues (-1,-1 when unlocatable). The UI offers a surgical
	// "edit only this trope" repair per located target, so single-unit
	// contradictions no longer force full-chapter rewrites.
	SceneTargets [][2]int `json:"scene_targets,omitempty"`
}

type MemoryEntry struct {
	References []MemoryReference `json:"references,omitempty"`
	ID         int               `json:"id"`
	Content    string            `json:"content"`
	Category   string            `json:"category"` // character | location | item | event | promise | other
	Inherited  bool              `json:"inherited,omitempty"`
	// Snippet is resolved server-side for API responses (chapter content no
	// longer travels with /api/progress); never persisted.
	Snippet string `json:"snippet,omitempty"`
}

type Progress struct {
	NextMemoryID                int                      `json:"next_memory_id,omitempty"`
	OutlineBatches              []OutlineBatch           `json:"outline_batches,omitempty"`
	Phase                       string                   `json:"phase"`
	Title                       string                   `json:"title"`
	CorePrompt                  string                   `json:"core_prompt"`
	Chapters                    []ChapterState           `json:"chapters"`
	CurrentChapterIndex         int                      `json:"current_chapter_index"`
	StoryConfigSnapshot         *config.StoryConfig      `json:"story_config_snapshot,omitempty"`
	Foreshadows                 []Foreshadow             `json:"foreshadows,omitempty"`
	LastForeshadowOutlineReport *ForeshadowOutlineReport `json:"last_foreshadow_outline_report,omitempty"`
	LastOutlineCharacterReport  *OutlineCharacterReport  `json:"last_outline_character_report,omitempty"`
	PendingWritingConflict      *WritingConflict         `json:"pending_writing_conflict,omitempty"`
	MemoryEntries               []MemoryEntry            `json:"memory_entries,omitempty"`
	MemoryMaxTokens             int                      `json:"memory_max_tokens,omitempty"`
	BookStatus                  string                   `json:"book_status,omitempty"`
	LongTermDirection           string                   `json:"long_term_direction,omitempty"`
	LatestPlanningReview        *PlanningReview          `json:"latest_planning_review,omitempty"`
	NarrativeCheckpoints        []NarrativeCheckpoint    `json:"narrative_checkpoints,omitempty"`
	PrePlanning                 *PrePlanning             `json:"pre_planning,omitempty"`
	// OutlineAnalysis is the last bestseller-editor pass over the full outline
	// (POST /api/outline/analyze); nil until the author runs an analysis.
	OutlineAnalysis *OutlineAnalysisReport `json:"outline_analysis,omitempty"`
	// ScenePlanAnalysis is the last bestseller-editor pass over the pending
	// chapter scene plans (POST /api/scene-plan/analyze); nil until the author
	// runs an analysis.
	ScenePlanAnalysis *ScenePlanAnalysisReport `json:"scene_plan_analysis,omitempty"`
	// ActAnalyses holds one bestseller-editor pass per pre-planning act whose
	// chapters have just finished being drafted (the writing pipeline triggers
	// it when the last chapter of an act is confirmed; POST /api/acts/analyze
	// re-runs it on demand). Keyed by 1-based act number.
	ActAnalyses map[int]*ActAnalysisReport `json:"act_analyses,omitempty"`
	// AutoScenePlan enables automatic scene-plan approval: when true, chapter
	// generation plans its scenes and proceeds to write without waiting for
	// the author to review/edit the plan in the UI. When false (default),
	// GenerateChapterAction pauses with ScenePlanPendingError until the plan
	// is approved via the API.
	AutoScenePlan bool `json:"auto_scene_plan,omitempty"`
	// PendingScenePlanChapter is the chapter number whose scene plan awaits
	// author review (set when GenerateChapterAction pauses with
	// ScenePlanPendingError; cleared once writing proceeds). The UI uses it to
	// auto-open the plan editor, surviving reloads like PendingWritingConflict.
	PendingScenePlanChapter int `json:"pending_scene_plan_chapter,omitempty"`
}

// PrePlanning is the book-level plan generated before any outline batch: total
// chapter recommendation, long-term direction and one card per act, derived
// from the configured Story structure (three-act, hero's journey, ...). Each
// act carries a suggested chapter count for its batch and a ready-to-use
// batch synopsis so the author can start generating outlines directly.
type PrePlanning struct {
	GeneratedAt       string           `json:"generated_at,omitempty"`
	BasedOnStructure  string           `json:"based_on_structure,omitempty"`
	TotalChapters     int              `json:"total_chapters,omitempty"`
	LongTermDirection string           `json:"long_term_direction,omitempty"`
	Acts              []PrePlanningAct `json:"acts,omitempty"`
}

// PrePlanningAct is one act card of the pre-planning result.
type PrePlanningAct struct {
	Name              string `json:"name"`
	Summary           string `json:"summary,omitempty"`
	SuggestedChapters int    `json:"suggested_chapters,omitempty"`
	StartCh           int    `json:"start_ch,omitempty"`
	EndCh             int    `json:"end_ch,omitempty"`
	BatchSynopsis     string `json:"batch_synopsis,omitempty"`
}

const (
	BookStatusActive    = "active"
	BookStatusCompleted = "completed"
)

type PlanningReview struct {
	ThroughChapter int    `json:"through_chapter"`
	Content        string `json:"content"`
	CreatedAt      string `json:"created_at"`
}

type NarrativeCheckpoint struct {
	StartChapter int    `json:"start_chapter"`
	EndChapter   int    `json:"end_chapter"`
	Level        int    `json:"level,omitempty"`
	SourceHash   string `json:"source_hash,omitempty"`
	Summary      string `json:"summary"`
	Degraded     bool   `json:"degraded,omitempty"`
	// Threads captures the per-storyline state at the end of this checkpoint's
	// chapter range (e.g. "Rebelión de Alden: prófugo, oculto en el norte").
	// Cold history compressed by chapter blocks loses track of WHICH thread is
	// in WHAT state; when writing a later chapter that only advances one
	// storyline, the model needs each thread's last-known position separately,
	// not one blended recap. Optional: older checkpoints lack it and prompts
	// fall back to Summary-only rendering.
	Threads map[string]string `json:"threads,omitempty"`
}

const (
	StatusPending  = "pending"
	StatusWriting  = "writing"
	StatusReview   = "review"
	StatusAccepted = "accepted"
)

// LoadProgress loads project metadata and chapter prose.
func LoadProgress(path string) (*Progress, error) {
	progressStorageMu.Lock()
	defer progressStorageMu.Unlock()
	return loadProgressUnlocked(path)
}

// loadProgressUnlocked performs the actual load. Callers must hold
// progressStorageMu (or call it via LoadProgress).
func loadProgressUnlocked(path string) (*Progress, error) {
	if err := recoverProgress(path, fsutil.WriteFileAtomic); err != nil {
		return nil, err
	}
	resetCache(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("leer progreso: %w", err)
	}

	var p Progress
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("analizar progreso: %w", err)
	}

	if err := loadChapterContents(path, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// SaveProgress persists chapter prose to per-chapter files (only chapters
// whose content changed since load/last save are rewritten), then writes the
// metadata file without prose content.
func SaveProgress(path string, p *Progress) error {
	progressStorageMu.Lock()
	defer progressStorageMu.Unlock()
	return saveProgressUnlocked(path, p)
}

// saveProgressUnlocked performs the actual save. Callers must hold
// progressStorageMu (or call it via SaveProgress).
func saveProgressUnlocked(path string, p *Progress) error {
	if err := recoverProgress(path, fsutil.WriteFileAtomic); err != nil {
		return err
	}
	if err := os.MkdirAll(chaptersDir(path), 0755); err != nil {
		return &fsutil.SaveError{Path: path, Stage: "create_chapters", OriginalPreserved: true, Err: err}
	}
	files, err := prepareChapterFiles(path, p)
	if err != nil {
		return err
	}

	meta := *p
	meta.Chapters = make([]ChapterState, len(p.Chapters))
	for i, ch := range p.Chapters {
		ch.Content = ""
		ch.Blocks = nil
		ch.NextBlockID = 0
		ch.BlockSep = ""
		meta.Chapters[i] = ch
	}
	data, err := json.MarshalIndent(&meta, "", "  ")
	if err != nil {
		return fmt.Errorf("ordenar progreso: %w", err)
	}
	files = append(files, progressFile{Num: 0, Data: data})
	if err := commitProgressFiles(path, files, fsutil.WriteFileAtomic); err != nil {
		return fmt.Errorf("guardar progreso: %w", err)
	}
	cache := cacheFor(path)
	contentHashCache.Lock()
	for _, ch := range p.Chapters {
		cache[ch.Num] = HashContent(ch.Content)
	}
	contentHashCache.Unlock()
	cleanupChapterFiles(path, p)
	return nil
}

// ChapterMarkdownPath returns the markdown file path for a chapter inside the project directory.
func ChapterMarkdownPath(projectDir string, num int) string {
	return filepath.Join(projectDir, fmt.Sprintf("Chapter_%02d.md", num))
}

func SaveChapterMarkdown(projectDir string, ch ChapterState, title string) {
	content := fmt.Sprintf("# # %d chapter: %s\n\n> **thischapterwant to**: %s\n\n---\n\n%s", ch.Num, ch.Title, ch.Summary, ch.Content)
	_ = os.WriteFile(ChapterMarkdownPath(projectDir, ch.Num), []byte(content), 0644)
}

// ForeshadowRoadmapPath returns the foreshadow roadmap markdown path inside the project directory.
func ForeshadowRoadmapPath(projectDir string) string {
	return filepath.Join(projectDir, "Foreshadows.md")
}
