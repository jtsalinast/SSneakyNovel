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

// ---------------------------------------------------------------------------
// Automated AI beta reader panel
// ---------------------------------------------------------------------------
//
// Before the bestseller editor grades a finished act, a panel of simulated
// readers reads that act independently and a consolidation pass turns their
// evaluations into one qualitative report. The report is persisted per act
// (Progress.BetaReports) and injected into the expert's act prompt and into
// the surgical-intervention proposal prompt, so the expert can weigh reader
// experience without blindly reproducing it.
//
// The author never manages the panel: it is generated once per project from
// the existing metadata (Progress.BetaPanel) and reused for every act. Each
// act gets a NEW evaluation session appended to that act's history; older
// sessions are never overwritten. Per-reader memory (Progress.BetaMemories)
// carries knowledge, suspicions and predictions from one act to the next.
//
// Call budget per act (deliberately split, never one mega call):
//   - 1 panel-generation call, only on the first act of the project
//   - 2 calls per reader (sequential reading, then evaluation)
//   - 2 consolidation calls (experience findings + interpretation findings)
// The existing editorial sequence (5 calls) runs afterwards.

// BetaPersona is one simulated reader. Numbers are 1–10 unless noted.
type BetaPersona struct {
	ID                  string          `json:"id"`
	Archetype           string          `json:"archetype"`
	Name                string          `json:"name"`
	Age                 int             `json:"age"`
	Background          string          `json:"background,omitempty"`
	ReadingHabits       string          `json:"reading_habits,omitempty"`
	FavoriteGenres      []string        `json:"favorite_genres,omitempty"`
	FavoriteAuthors     []string        `json:"favorite_authors,omitempty"`
	PreferredStyles     []string        `json:"preferred_styles,omitempty"`
	PreferredCharacters []string        `json:"preferred_character_types,omitempty"`
	PreferredThemes     []string        `json:"preferred_themes,omitempty"`
	Psychology          BetaPsychology  `json:"psychology"`
	Sensitivity         BetaSensitivity `json:"sensitivity"`
	Motivations         []string        `json:"motivations,omitempty"`
	Critical            BetaCritical    `json:"critical_behavior"`
	Priorities          BetaPriorities  `json:"priorities"`
	Biases              []string        `json:"biases,omitempty"`
	BlindSpots          []string        `json:"blind_spots,omitempty"`
}

// BetaPsychology is the persona's emotional/intellectual profile.
type BetaPsychology struct {
	EmotionalSensitivity  int `json:"emotional_sensitivity"`
	Empathy               int `json:"empathy"`
	IntellectualCuriosity int `json:"intellectual_curiosity"`
	ToleranceForAmbiguity int `json:"tolerance_for_ambiguity"`
}

// BetaSensitivity holds the ten narrative-sensitivity parameters.
type BetaSensitivity struct {
	ToleranceForAmbiguity                int `json:"tolerance_for_ambiguity"`
	NeedForFastPacing                    int `json:"need_for_fast_pacing"`
	AttentionToContinuity                int `json:"attention_to_continuity"`
	SensitivityToExposition              int `json:"sensitivity_to_exposition"`
	PreferenceForPsychologicalComplexity int `json:"preference_for_psychological_complexity"`
	NeedForNarrativeClosure              int `json:"need_for_narrative_closure"`
	ToleranceForUnresolvedPlots          int `json:"tolerance_for_unresolved_plotlines"`
	SensitivityToImplausibleBehavior     int `json:"sensitivity_to_implausible_character_behavior"`
	AttentionToDialogue                  int `json:"attention_to_dialogue"`
	SensitivityToPredictableTwists       int `json:"sensitivity_to_predictable_twists"`
}

// BetaCritical is how the persona criticizes.
type BetaCritical struct {
	Directness                  int `json:"directness"`
	AnalyticalDepth             int `json:"analytical_depth"`
	AttentionToDetail           int `json:"attention_to_detail"`
	Severity                    int `json:"severity"`
	ToleranceForExperimentation int `json:"tolerance_for_experimentation"`
}

// BetaPriorities is the persona's evaluation weighting (1–10).
type BetaPriorities struct {
	Plot            int `json:"plot"`
	Characters      int `json:"characters"`
	Prose           int `json:"prose"`
	Pacing          int `json:"pacing"`
	Worldbuilding   int `json:"worldbuilding"`
	Dialogue        int `json:"dialogue"`
	EmotionalImpact int `json:"emotional_impact"`
	Originality     int `json:"originality"`
	Themes          int `json:"themes"`
}

// BetaPanel is the project-wide reader panel. Generated once from the novel
// metadata and reused for every act so the audience segments stay comparable.
type BetaPanel struct {
	GeneratedAt string        `json:"generated_at"`
	BasedOn     string        `json:"based_on,omitempty"`
	Personas    []BetaPersona `json:"personas"`
}

// BetaReaderMemory is the per-persona reading memory carried across acts.
type BetaReaderMemory struct {
	PersonaID      string   `json:"persona_id,omitempty"`
	ThroughChapter int      `json:"through_chapter,omitempty"`
	KnownEvents    []string `json:"known_events,omitempty"`
	CharacterViews []string `json:"character_views,omitempty"`
	Predictions    []string `json:"predictions,omitempty"`
	OpenQuestions  []string `json:"open_questions,omitempty"`
	EmotionalState string   `json:"emotional_state,omitempty"`
	UpdatedAt      string   `json:"updated_at,omitempty"`
}

// BetaMoment is a manuscript location plus what the reader felt there.
type BetaMoment struct {
	Chapter  int    `json:"chapter,omitempty"`
	Location string `json:"location,omitempty"`
	Quote    string `json:"quote,omitempty"`
	Note     string `json:"note,omitempty"`
}

// BetaChapterEngagement records how interested the reader was chapter by chapter.
type BetaChapterEngagement struct {
	Chapter    int    `json:"chapter"`
	Engagement int    `json:"engagement"`
	Note       string `json:"note,omitempty"`
}

// BetaCharacterPerception is how one reader sees one character.
type BetaCharacterPerception struct {
	Character           string `json:"character"`
	Perception          string `json:"perception"`
	EmotionalConnection string `json:"emotional_connection,omitempty"`
}

// BetaConcern is one possible problem raised by one reader.
type BetaConcern struct {
	Description string `json:"description"`
	Chapter     int    `json:"chapter,omitempty"`
	Location    string `json:"location,omitempty"`
	Quote       string `json:"quote,omitempty"`
	Kind        string `json:"kind,omitempty"` // narrative_problem | personal_preference
}

// BetaReaderEvaluation is the persisted individual evaluation of one persona.
type BetaReaderEvaluation struct {
	PersonaID            string                    `json:"persona_id"`
	PersonaName          string                    `json:"persona_name"`
	Archetype            string                    `json:"archetype"`
	Age                  int                       `json:"age,omitempty"`
	Engagement           int                       `json:"engagement"`
	Satisfaction         int                       `json:"satisfaction"`
	ContinueReading      int                       `json:"continue_reading"`
	ReadingNotes         string                    `json:"reading_notes,omitempty"`
	EngagementByChapter  []BetaChapterEngagement   `json:"engagement_by_chapter,omitempty"`
	MostEngaging         []BetaMoment              `json:"most_engaging,omitempty"`
	LeastEngaging        []BetaMoment              `json:"least_engaging,omitempty"`
	Memorable            []BetaMoment              `json:"memorable,omitempty"`
	Confusing            []BetaMoment              `json:"confusing,omitempty"`
	Surprising           []BetaMoment              `json:"surprising,omitempty"`
	Emotional            []BetaMoment              `json:"emotional,omitempty"`
	CharacterPerceptions []BetaCharacterPerception `json:"character_perceptions,omitempty"`
	Understood           []string                  `json:"understood,omitempty"`
	Misunderstood        []string                  `json:"misunderstood,omitempty"`
	Suspects             []string                  `json:"suspects,omitempty"`
	Expectations         []string                  `json:"expectations,omitempty"`
	Worked               []string                  `json:"worked,omitempty"`
	Concerns             []BetaConcern             `json:"concerns,omitempty"`
	PersonalPreferences  []string                  `json:"personal_preferences,omitempty"`
	Summary              string                    `json:"summary,omitempty"`
}

// BetaLocation is a manuscript anchor used by consolidated findings.
type BetaLocation struct {
	Chapter int    `json:"chapter,omitempty"`
	Scene   string `json:"scene,omitempty"`
	Quote   string `json:"quote,omitempty"`
}

// BetaFinding is one consolidated observation. ReaderCount is derived in Go
// from Readers, never trusted from the model.
type BetaFinding struct {
	Description string         `json:"description"`
	Locations   []BetaLocation `json:"locations,omitempty"`
	Readers     []string       `json:"readers,omitempty"`
	ReaderCount int            `json:"reader_count"`
	Severity    string         `json:"severity,omitempty"`   // low | medium | high
	Confidence  string         `json:"confidence,omitempty"` // low | medium | high
	Kind        string         `json:"kind,omitempty"`       // narrative_problem | subjective_preference
}

// BetaReaderReport is one evaluation session for one act. Sessions are
// appended to Progress.BetaReports[actNo]; history is never overwritten.
type BetaReaderReport struct {
	GeneratedAt      string `json:"generated_at"`
	SessionID        string `json:"session_id"`
	ActNo            int    `json:"act_no"`
	ActName          string `json:"act_name,omitempty"`
	StartCh          int    `json:"start_ch"`
	EndCh            int    `json:"end_ch"`
	DraftFingerprint string `json:"draft_fingerprint,omitempty"`
	// ContentFingerprint is the unsalted prose fingerprint this session read.
	// Lookup keys on it so a panel survives server restarts; legacy sessions
	// written before this field existed fall back to DraftFingerprint once.
	ContentFingerprint        string                 `json:"content_fingerprint,omitempty"`
	PanelSize                 int                    `json:"panel_size"`
	Summary                   string                 `json:"summary"`
	AverageEngagement         int                    `json:"average_engagement"`
	AverageContinue           int                    `json:"average_continue_reading"`
	SharedPositives           []BetaFinding          `json:"shared_positives,omitempty"`
	SharedConcerns            []BetaFinding          `json:"shared_concerns,omitempty"`
	PolarizedReactions        []BetaFinding          `json:"polarized_reactions,omitempty"`
	UnexpectedInterpretations []BetaFinding          `json:"unexpected_interpretations,omitempty"`
	UnfulfilledExpectations   []BetaFinding          `json:"unfulfilled_expectations,omitempty"`
	EngagementPatterns        []BetaFinding          `json:"engagement_patterns,omitempty"`
	CharacterPerception       []BetaFinding          `json:"character_perception,omitempty"`
	MemorableElements         []BetaFinding          `json:"memorable_elements,omitempty"`
	ProductiveAmbiguity       []BetaFinding          `json:"productive_ambiguity,omitempty"`
	IndividualObservations    []BetaFinding          `json:"individual_observations,omitempty"`
	Evaluations               []BetaReaderEvaluation `json:"evaluations"`
}

// BetaProgressEvent is the payload of the `beta_progress` SSE event consumed
// by the Writing page checklist.
type BetaProgressEvent struct {
	ActNo  int        `json:"act_no"`
	Stage  string     `json:"stage"`
	Index  int        `json:"index,omitempty"`
	Total  int        `json:"total,omitempty"`
	Detail string     `json:"detail,omitempty"`
	Panel  *BetaPanel `json:"panel,omitempty"`
}

type betaReadingResult struct {
	ReadingNotes        string                  `json:"reading_notes"`
	EngagementByChapter []BetaChapterEngagement `json:"engagement_by_chapter"`
	MostEngaging        []BetaMoment            `json:"most_engaging"`
	LeastEngaging       []BetaMoment            `json:"least_engaging"`
	Memorable           []BetaMoment            `json:"memorable"`
	Confusing           []BetaMoment            `json:"confusing"`
	Surprising          []BetaMoment            `json:"surprising"`
	Emotional           []BetaMoment            `json:"emotional"`
	Memory              BetaReaderMemory        `json:"memory"`
}

type betaEvaluationResult struct {
	Engagement           int                       `json:"engagement"`
	Satisfaction         int                       `json:"satisfaction"`
	ContinueReading      int                       `json:"continue_reading"`
	CharacterPerceptions []BetaCharacterPerception `json:"character_perceptions"`
	Understood           []string                  `json:"understood"`
	Misunderstood        []string                  `json:"misunderstood"`
	Suspects             []string                  `json:"suspects"`
	Expectations         []string                  `json:"expectations"`
	Worked               []string                  `json:"worked"`
	Concerns             []BetaConcern             `json:"concerns"`
	PersonalPreferences  []string                  `json:"personal_preferences"`
	Summary              string                    `json:"summary"`
}

type betaConsolidationExperience struct {
	Summary                 string        `json:"summary"`
	SharedPositives         []BetaFinding `json:"shared_positives"`
	SharedConcerns          []BetaFinding `json:"shared_concerns"`
	PolarizedReactions      []BetaFinding `json:"polarized_reactions"`
	UnfulfilledExpectations []BetaFinding `json:"unfulfilled_expectations"`
	ProductiveAmbiguity     []BetaFinding `json:"productive_ambiguity"`
}

type betaConsolidationInterpretation struct {
	UnexpectedInterpretations []BetaFinding `json:"unexpected_interpretations"`
	EngagementPatterns        []BetaFinding `json:"engagement_patterns"`
	CharacterPerception       []BetaFinding `json:"character_perception"`
	MemorableElements         []BetaFinding `json:"memorable_elements"`
	IndividualObservations    []BetaFinding `json:"individual_observations"`
}

// betaArchetypes is the fixed composition target: three target-audience
// readers plus one per specialist lens.
var betaArchetypes = []string{
	"target_audience", "target_audience", "target_audience",
	"literary", "entertainment", "psychological", "analytical", "general",
}

func normalizeBetaArchetype(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.Contains(s, "target"), strings.Contains(s, "audience"), strings.Contains(s, "fan"):
		return "target_audience"
	case strings.Contains(s, "literary"), strings.Contains(s, "prose"):
		return "literary"
	case strings.Contains(s, "entertain"), strings.Contains(s, "plot"), strings.Contains(s, "casual"):
		return "entertainment"
	case strings.Contains(s, "psych"), strings.Contains(s, "emotion"), strings.Contains(s, "character"):
		return "psychological"
	case strings.Contains(s, "analy"), strings.Contains(s, "skept"), strings.Contains(s, "logic"):
		return "analytical"
	case strings.Contains(s, "general"), strings.Contains(s, "mainstream"), strings.Contains(s, "outsider"):
		return "general"
	}
	return ""
}

func clampBetaInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func betaNormSpace(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}

func betaTrimStrings(in []string, maxItems, maxRunes int) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(draftTruncate(s, maxRunes))
		if s == "" || seen[betaNormSpace(s)] {
			continue
		}
		seen[betaNormSpace(s)] = true
		out = append(out, s)
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

// sanitizeBetaPanel assigns stable IDs, enforces the composition and clamps
// every numeric parameter. Returns false when the model gave too few personas.
func sanitizeBetaPanel(p *BetaPanel) bool {
	if p == nil || len(p.Personas) < 5 {
		return false
	}
	if len(p.Personas) > 10 {
		p.Personas = p.Personas[:10]
	}
	used := map[string]bool{}
	for i := range p.Personas {
		pr := &p.Personas[i]
		pr.ID = fmt.Sprintf("r%d", i+1)
		pr.Name = strings.TrimSpace(pr.Name)
		if pr.Name == "" {
			pr.Name = fmt.Sprintf("Reader %d", i+1)
		}
		pr.Archetype = normalizeBetaArchetype(pr.Archetype)
		if pr.Archetype == "" || used[pr.Archetype] && i < 3 {
			// Keep the panel balanced even when the model omits a label.
			pr.Archetype = betaArchetypes[i%len(betaArchetypes)]
		}
		used[pr.Archetype] = true
		if pr.Age <= 0 || pr.Age > 100 {
			pr.Age = 25 + (i*7)%40
		}
		pr.Psychology.EmotionalSensitivity = clampBetaInt(pr.Psychology.EmotionalSensitivity, 1, 10)
		pr.Psychology.Empathy = clampBetaInt(pr.Psychology.Empathy, 1, 10)
		pr.Psychology.IntellectualCuriosity = clampBetaInt(pr.Psychology.IntellectualCuriosity, 1, 10)
		pr.Psychology.ToleranceForAmbiguity = clampBetaInt(pr.Psychology.ToleranceForAmbiguity, 1, 10)
		clampSensitivity(&pr.Sensitivity)
		pr.Critical.Directness = clampBetaInt(pr.Critical.Directness, 1, 10)
		pr.Critical.AnalyticalDepth = clampBetaInt(pr.Critical.AnalyticalDepth, 1, 10)
		pr.Critical.AttentionToDetail = clampBetaInt(pr.Critical.AttentionToDetail, 1, 10)
		pr.Critical.Severity = clampBetaInt(pr.Critical.Severity, 1, 10)
		pr.Critical.ToleranceForExperimentation = clampBetaInt(pr.Critical.ToleranceForExperimentation, 1, 10)
		clampPriorities(&pr.Priorities)
		pr.FavoriteGenres = betaTrimStrings(pr.FavoriteGenres, 6, 80)
		pr.FavoriteAuthors = betaTrimStrings(pr.FavoriteAuthors, 6, 80)
		pr.PreferredStyles = betaTrimStrings(pr.PreferredStyles, 6, 120)
		pr.PreferredCharacters = betaTrimStrings(pr.PreferredCharacters, 6, 120)
		pr.PreferredThemes = betaTrimStrings(pr.PreferredThemes, 6, 120)
		pr.Motivations = betaTrimStrings(pr.Motivations, 6, 120)
		pr.Biases = betaTrimStrings(pr.Biases, 6, 160)
		pr.BlindSpots = betaTrimStrings(pr.BlindSpots, 6, 160)
		pr.Background = draftTruncate(strings.TrimSpace(pr.Background), 500)
		pr.ReadingHabits = draftTruncate(strings.TrimSpace(pr.ReadingHabits), 500)
	}
	return true
}

func clampSensitivity(s *BetaSensitivity) {
	vals := []*int{
		&s.ToleranceForAmbiguity, &s.NeedForFastPacing, &s.AttentionToContinuity,
		&s.SensitivityToExposition, &s.PreferenceForPsychologicalComplexity,
		&s.NeedForNarrativeClosure, &s.ToleranceForUnresolvedPlots,
		&s.SensitivityToImplausibleBehavior, &s.AttentionToDialogue,
		&s.SensitivityToPredictableTwists,
	}
	for _, v := range vals {
		*v = clampBetaInt(*v, 1, 10)
	}
}

func clampPriorities(p *BetaPriorities) {
	vals := []*int{
		&p.Plot, &p.Characters, &p.Prose, &p.Pacing, &p.Worldbuilding,
		&p.Dialogue, &p.EmotionalImpact, &p.Originality, &p.Themes,
	}
	allZero := true
	for _, v := range vals {
		if *v != 0 {
			allZero = false
			break
		}
	}
	for _, v := range vals {
		if allZero {
			*v = 6
			continue
		}
		*v = clampBetaInt(*v, 1, 10)
	}
}

// defaultBetaPanel is the deterministic fallback used when panel generation
// fails: the beta pass must never block the expert review.
func defaultBetaPanel(cfg *config.Config) *BetaPanel {
	type seed struct {
		name, archetype, background string
		age                         int
	}
	seeds := []seed{
		{"Mara", "target_audience", "Reads two or three books a month in this genre and buys on recommendation.", 29},
		{"Diego", "target_audience", "Long-time genre reader who wants the familiar pleasures done well.", 41},
		{"Lena", "target_audience", "Newer reader drawn in by the premise and the word-of-mouth buzz.", 22},
		{"Iván", "literary", "Reads reviews, prizes and literary fiction; notices prose, structure and symbolism.", 47},
		{"Ruth", "entertainment", "Reads for momentum, twists and the feeling of not wanting to put the book down.", 33},
		{"Tomás", "psychological", "Reads for characters, relationships and emotional truth.", 38},
		{"Aiko", "analytical", "Reads with a continuity spreadsheet in her head and dislikes convenient coincidences.", 35},
		{"Sam", "general", "Reads widely but not this genre; the book has to earn attention beyond the shelf.", 44},
	}
	panel := &BetaPanel{GeneratedAt: time.Now().Format(time.RFC3339), BasedOn: "deterministic fallback"}
	for i, s := range seeds {
		p := BetaPersona{
			ID: fmt.Sprintf("r%d", i+1), Archetype: s.archetype, Name: s.name, Age: s.age,
			Background:    s.background,
			ReadingHabits: "Reads in the evening, in long sittings, and remembers how a book made them feel.",
			Psychology:    BetaPsychology{EmotionalSensitivity: 5 + i%4, Empathy: 5 + (i+1)%4, IntellectualCuriosity: 4 + i%5, ToleranceForAmbiguity: 3 + i%6},
			Sensitivity: BetaSensitivity{
				ToleranceForAmbiguity: 3 + i%6, NeedForFastPacing: 4 + (i*3)%7,
				AttentionToContinuity: 4 + (i*2)%7, SensitivityToExposition: 4 + i%6,
				PreferenceForPsychologicalComplexity: 4 + (i+2)%7, NeedForNarrativeClosure: 3 + (i*2)%7,
				ToleranceForUnresolvedPlots: 3 + (i*4)%7, SensitivityToImplausibleBehavior: 4 + i%6,
				AttentionToDialogue: 4 + (i*3)%7, SensitivityToPredictableTwists: 4 + (i*2)%7,
			},
			Motivations: betaTrimStrings([]string{"entertainment", "emotional experience", "character identification", "intellectual stimulation"}, 4, 80),
			Critical:    BetaCritical{Directness: 4 + i%5, AnalyticalDepth: 4 + (i*2)%6, AttentionToDetail: 4 + i%6, Severity: 3 + (i*3)%6, ToleranceForExperimentation: 3 + i%6},
			Priorities:  BetaPriorities{Plot: 5 + (i*2)%6, Characters: 5 + (i+1)%5, Prose: 4 + i%6, Pacing: 5 + (i*3)%5, Worldbuilding: 4 + (i+2)%6, Dialogue: 4 + i%6, EmotionalImpact: 5 + (i+1)%5, Originality: 4 + (i*2)%6, Themes: 4 + i%6},
			Biases:      []string{"Judges the book against the genre promises they already enjoy."},
			BlindSpots:  []string{"May forgive a familiar trope because it feels comfortable."},
		}
		if s.archetype == "literary" {
			p.Priorities.Prose, p.Priorities.Themes, p.Priorities.Originality = 9, 9, 8
			p.Critical.AnalyticalDepth, p.Critical.ToleranceForExperimentation = 8, 8
		}
		if s.archetype == "entertainment" {
			p.Priorities.Pacing, p.Priorities.Plot = 9, 9
			p.Sensitivity.NeedForFastPacing = 9
		}
		if s.archetype == "psychological" {
			p.Priorities.Characters, p.Priorities.EmotionalImpact = 9, 9
			p.Psychology.Empathy, p.Psychology.EmotionalSensitivity = 9, 9
		}
		if s.archetype == "analytical" {
			p.Priorities.Plot, p.Priorities.Worldbuilding = 8, 8
			p.Sensitivity.AttentionToContinuity, p.Sensitivity.SensitivityToImplausibleBehavior = 9, 9
		}
		if s.archetype == "general" {
			p.Priorities.Pacing, p.Priorities.Characters = 7, 7
			p.Sensitivity.SensitivityToExposition = 8
		}
		if s.archetype == "target_audience" {
			p.FavoriteGenres = []string{cfg.Story.Type, cfg.Story.Subgenre}
		}
		panel.Personas = append(panel.Personas, p)
	}
	sanitizeBetaPanel(panel)
	return panel
}

// ---------------------------------------------------------------------------
// Prompt building
// ---------------------------------------------------------------------------

func betaSystemPrompt(lang string) string {
	if normalizeLangEN(lang) {
		return `You are a SIMULATED READER in an automated beta-reading panel. You are not an editor, a writing assistant or the author. Never address the author and never rewrite the manuscript.
Embody the persona you are given consistently: its tastes, sensitivities, motives, critical habits and blind spots must change what you notice and how you judge.
React honestly. Do not try to please the author, and do not manufacture criticism when you enjoyed the act.
Judge what the manuscript actually communicates, not what the author supposedly intended.
Distinguish clearly what you know, what you suspect, what you believe, what you misunderstand and what you expect.
Distinguish genuine narrative problems from personal preferences and genre expectations.
Ground observations in specific manuscript locations and short verbatim excerpts whenever possible. Never invent events or quotations; if you cannot quote, describe the location instead.
Do not propose solutions or fixes unless explicitly asked.
Never present your reactions as empirical human feedback, statistics or a representative sample: you are one simulated reader.
Output strict JSON exactly as requested — no markdown fences, no extra prose.`
	}
	return `Eres un LECTOR SIMULADO de un panel automático de beta readers. No eres un editor, un asistente de escritura ni el autor. Nunca te dirijas al autor ni reescribas el manuscrito.
Encarna de forma consistente la persona que se te asigna: sus gustos, sensibilidades, motivos, hábitos críticos y puntos ciegos deben cambiar qué notas y cómo juzgas.
Reacciona con honestidad. No intentes complacer al autor ni fabriques críticas cuando el acto te gustó.
Juzga lo que el manuscrito realmente comunica, no lo que el autor supuestamente pretendía.
Distingue con claridad qué sabes, qué sospechas, qué crees, qué malinterpretas y qué esperas.
Distingue problemas narrativos reales de preferencias personales y expectativas de género.
Ancla las observaciones en lugares concretos del manuscrito y en extractos textuales breves siempre que sea posible. Nunca inventes eventos ni citas; si no puedes citar, describe la ubicación.
No propongas soluciones ni correcciones salvo que se te pida explícitamente.
Nunca presentes tus reacciones como feedback humano empírico, estadísticas ni una muestra representativa: eres un lector simulado.
Devuelve JSON estricto exactamente como se pide — sin cercas markdown ni prosa adicional.`
}

func betaMetadataBlock(cfg *config.Config, settings *ProjectSettings, lang string) string {
	sc := cfg.Story
	var b strings.Builder
	add := func(label, v string) {
		if v = strings.TrimSpace(v); v != "" {
			fmt.Fprintf(&b, "%s: %s\n", label, draftTruncate(v, 700))
		}
	}
	add("Title", sc.Title)
	add("Story idea", sc.StoryIdea)
	add("Elevator pitch", sc.ElevatorPitch)
	add("Genre / subgenre", strings.TrimSpace(sc.Type+" / "+sc.Subgenre))
	add("Target audience", sc.TargetAudience)
	add("Ideal reader profile", sc.AudienceProfile)
	add("Reader expectation", sc.ReaderExpectation)
	add("Theme / motif", strings.TrimSpace(sc.Theme+" / "+sc.Motif))
	add("Philosophical dilemma", sc.PhilosophicalDilemma)
	add("Tone", sc.Tone)
	add("Writing style & POV", strings.TrimSpace(sc.WritingStyle+" / "+sc.WritingPOV))
	add("Story structure", structureLabel(sc.Structure, lang))
	add("Setting", sc.SpecificSettings)
	add("Conflict scale", sc.EffectiveConflict())
	add("Protagonist type", sc.EffectiveProtagonist())
	add("Inspirational pieces", sc.InspirationalPieces)
	if settings != nil {
		add("World aesthetic", settings.Aesthetic)
	}
	add("Length", sc.StoryLength)
	if strings.EqualFold(sc.WorkType, "fanfic") {
		add("Fan fiction source", sc.FanficSource)
	}
	if settings != nil {
		names := make([]string, 0, len(settings.Characters))
		for _, c := range settings.Characters {
			desc := strings.TrimSpace(c.Personality + " " + c.Motivation)
			names = append(names, strings.TrimSpace(c.Name+" ("+c.Affiliation+") — "+draftTruncate(desc, 180)))
		}
		add("Main characters", strings.Join(names, "; "))
		wv := make([]string, 0, len(settings.Worldview))
		for _, w := range settings.Worldview {
			wv = append(wv, strings.TrimSpace(w.Category+": "+w.Name))
		}
		add("Worldbuilding entries", strings.Join(wv, "; "))
	}
	return b.String()
}

func betaPanelPrompt(cfg *config.Config, settings *ProjectSettings, lang string) string {
	meta := betaMetadataBlock(cfg, settings, lang)
	if normalizeLangEN(lang) {
		return fmt.Sprintf(`Build the reader panel for an automated beta-reading process. Use ONLY the novel metadata below; do not ask the author anything.

NOVEL METADATA:
%s

Create EXACTLY 8 simulated readers with this composition (adapt tastes to the novel, never duplicate a persona):
- 3 target_audience readers who genuinely belong to the intended readership but differ in preferences (e.g. one plot-first, one character-first, one atmosphere/worldbuilding-first).
- 1 literary reader: prose, originality, symbolism, thematic depth.
- 1 entertainment reader: pacing, suspense, engagement, plot.
- 1 psychological/emotional reader: characters, relationships, emotional authenticity.
- 1 analytical/skeptical reader: coherence, causality, plausibility.
- 1 general reader: accessibility and engagement beyond genre enthusiasts.
Every reader needs a DIFFERENT reading behavior, not just a different name.

Return STRICT JSON only:
{"personas":[{"archetype":"target_audience|literary|entertainment|psychological|analytical|general","name":"...","age":34,"background":"...","reading_habits":"...","favorite_genres":["..."],"favorite_authors":["..."],"preferred_styles":["..."],"preferred_character_types":["..."],"preferred_themes":["..."],"psychology":{"emotional_sensitivity":1-10,"empathy":1-10,"intellectual_curiosity":1-10,"tolerance_for_ambiguity":1-10},"sensitivity":{"tolerance_for_ambiguity":1-10,"need_for_fast_pacing":1-10,"attention_to_continuity":1-10,"sensitivity_to_exposition":1-10,"preference_for_psychological_complexity":1-10,"need_for_narrative_closure":1-10,"tolerance_for_unresolved_plotlines":1-10,"sensitivity_to_implausible_character_behavior":1-10,"attention_to_dialogue":1-10,"sensitivity_to_predictable_twists":1-10},"motivations":["entertainment|intellectual stimulation|emotional experience|escapism|philosophical exploration|character identification"],"critical_behavior":{"directness":1-10,"analytical_depth":1-10,"attention_to_detail":1-10,"severity":1-10,"tolerance_for_experimentation":1-10},"priorities":{"plot":1-10,"characters":1-10,"prose":1-10,"pacing":1-10,"worldbuilding":1-10,"dialogue":1-10,"emotional_impact":1-10,"originality":1-10,"themes":1-10},"biases":["..."],"blind_spots":["..."]}]}
Write names, backgrounds and all descriptive text in the same language as the novel's prose.`, meta)
	}
	return fmt.Sprintf(`Construye el panel de lectores de un proceso automático de beta reading. Usa SOLO los metadatos de la novela de abajo; no le preguntes nada al autor.

METADATOS DE LA NOVELA:
%s

Crea EXACTAMENTE 8 lectores simulados con esta composición (adapta los gustos a la novela, nunca dupliques una persona):
- 3 lectores de target_audience que pertenezcan de verdad al público objetivo pero difieran en preferencias (p. ej. uno de trama, uno de personajes, uno de atmósfera/mundo).
- 1 lector literario: prosa, originalidad, simbolismo, profundidad temática.
- 1 lector de entretenimiento: ritmo, suspenso, engagement, trama.
- 1 lector psicológico/emocional: personajes, relaciones, autenticidad emocional.
- 1 lector analítico/escéptico: coherencia, causalidad, verosimilitud.
- 1 lector general: accesibilidad y engagement más allá de los fans del género.
Cada lector necesita un COMPORTAMIENTO de lectura DISTINTO, no solo un nombre distinto.

Devuelve SOLO JSON estricto:
{"personas":[{"archetype":"target_audience|literary|entertainment|psychological|analytical|general","name":"...","age":34,"background":"...","reading_habits":"...","favorite_genres":["..."],"favorite_authors":["..."],"preferred_styles":["..."],"preferred_character_types":["..."],"preferred_themes":["..."],"psychology":{"emotional_sensitivity":1-10,"empathy":1-10,"intellectual_curiosity":1-10,"tolerance_for_ambiguity":1-10},"sensitivity":{"tolerance_for_ambiguity":1-10,"need_for_fast_pacing":1-10,"attention_to_continuity":1-10,"sensitivity_to_exposition":1-10,"preference_for_psychological_complexity":1-10,"need_for_narrative_closure":1-10,"tolerance_for_unresolved_plotlines":1-10,"sensitivity_to_implausible_character_behavior":1-10,"attention_to_dialogue":1-10,"sensitivity_to_predictable_twists":1-10},"motivations":["entretenimiento|estímulo intelectual|experiencia emocional|evasión|exploración filosófica|identificación con personajes"],"critical_behavior":{"directness":1-10,"analytical_depth":1-10,"attention_to_detail":1-10,"severity":1-10,"tolerance_for_experimentation":1-10},"priorities":{"plot":1-10,"characters":1-10,"prose":1-10,"pacing":1-10,"worldbuilding":1-10,"dialogue":1-10,"emotional_impact":1-10,"originality":1-10,"themes":1-10},"biases":["..."],"blind_spots":["..."]}]}
Escribe nombres, antecedentes y todo el texto descriptivo en el mismo idioma que la prosa de la novela.`, meta)
}

func betaPersonaBlock(p *BetaPersona, lang string) string {
	en := normalizeLangEN(lang)
	join := func(v []string) string { return strings.Join(v, ", ") }
	if en {
		return fmt.Sprintf(`[READER %s — %s, age %d, %s]
Background: %s
Reading habits: %s
Favorite genres: %s
Favorite authors: %s
Preferred styles: %s
Preferred character types: %s
Preferred themes: %s
Psychology: emotional sensitivity %d/10, empathy %d/10, intellectual curiosity %d/10, tolerance for ambiguity %d/10
Narrative sensitivity: ambiguity %d/10, need for fast pacing %d/10, continuity %d/10, sensitivity to exposition %d/10, psychological complexity %d/10, need for closure %d/10, tolerance for unresolved plots %d/10, implausible behavior %d/10, dialogue %d/10, predictable twists %d/10
Motivations: %s
Critical behavior: directness %d/10, analytical depth %d/10, attention to detail %d/10, severity %d/10, tolerance for experimentation %d/10
Evaluation priorities: plot %d/10, characters %d/10, prose %d/10, pacing %d/10, worldbuilding %d/10, dialogue %d/10, emotional impact %d/10, originality %d/10, themes %d/10
Biases: %s
Blind spots: %s`,
			p.ID, p.Name, p.Age, p.Archetype, p.Background, p.ReadingHabits,
			join(p.FavoriteGenres), join(p.FavoriteAuthors), join(p.PreferredStyles), join(p.PreferredCharacters), join(p.PreferredThemes),
			p.Psychology.EmotionalSensitivity, p.Psychology.Empathy, p.Psychology.IntellectualCuriosity, p.Psychology.ToleranceForAmbiguity,
			p.Sensitivity.ToleranceForAmbiguity, p.Sensitivity.NeedForFastPacing, p.Sensitivity.AttentionToContinuity,
			p.Sensitivity.SensitivityToExposition, p.Sensitivity.PreferenceForPsychologicalComplexity, p.Sensitivity.NeedForNarrativeClosure,
			p.Sensitivity.ToleranceForUnresolvedPlots, p.Sensitivity.SensitivityToImplausibleBehavior, p.Sensitivity.AttentionToDialogue,
			p.Sensitivity.SensitivityToPredictableTwists, join(p.Motivations),
			p.Critical.Directness, p.Critical.AnalyticalDepth, p.Critical.AttentionToDetail, p.Critical.Severity, p.Critical.ToleranceForExperimentation,
			p.Priorities.Plot, p.Priorities.Characters, p.Priorities.Prose, p.Priorities.Pacing, p.Priorities.Worldbuilding,
			p.Priorities.Dialogue, p.Priorities.EmotionalImpact, p.Priorities.Originality, p.Priorities.Themes,
			join(p.Biases), join(p.BlindSpots))
	}
	return fmt.Sprintf(`[LECTOR %s — %s, %d años, %s]
Antecedentes: %s
Hábitos de lectura: %s
Géneros favoritos: %s
Autores favoritos: %s
Estilos preferidos: %s
Tipos de personaje preferidos: %s
Temas preferidos: %s
Psicología: sensibilidad emocional %d/10, empatía %d/10, curiosidad intelectual %d/10, tolerancia a la ambigüedad %d/10
Sensibilidad narrativa: ambigüedad %d/10, necesidad de ritmo rápido %d/10, continuidad %d/10, sensibilidad a la exposición %d/10, complejidad psicológica %d/10, necesidad de cierre %d/10, tolerancia a tramas sin resolver %d/10, conducta inverosímil %d/10, diálogo %d/10, giros predecibles %d/10
Motivaciones: %s
Comportamiento crítico: franqueza %d/10, profundidad analítica %d/10, atención al detalle %d/10, severidad %d/10, tolerancia a la experimentación %d/10
Prioridades de evaluación: trama %d/10, personajes %d/10, prosa %d/10, ritmo %d/10, mundo %d/10, diálogo %d/10, impacto emocional %d/10, originalidad %d/10, temas %d/10
Sesgos: %s
Puntos ciegos: %s`,
		p.ID, p.Name, p.Age, p.Archetype, p.Background, p.ReadingHabits,
		join(p.FavoriteGenres), join(p.FavoriteAuthors), join(p.PreferredStyles), join(p.PreferredCharacters), join(p.PreferredThemes),
		p.Psychology.EmotionalSensitivity, p.Psychology.Empathy, p.Psychology.IntellectualCuriosity, p.Psychology.ToleranceForAmbiguity,
		p.Sensitivity.ToleranceForAmbiguity, p.Sensitivity.NeedForFastPacing, p.Sensitivity.AttentionToContinuity,
		p.Sensitivity.SensitivityToExposition, p.Sensitivity.PreferenceForPsychologicalComplexity, p.Sensitivity.NeedForNarrativeClosure,
		p.Sensitivity.ToleranceForUnresolvedPlots, p.Sensitivity.SensitivityToImplausibleBehavior, p.Sensitivity.AttentionToDialogue,
		p.Sensitivity.SensitivityToPredictableTwists, join(p.Motivations),
		p.Critical.Directness, p.Critical.AnalyticalDepth, p.Critical.AttentionToDetail, p.Critical.Severity, p.Critical.ToleranceForExperimentation,
		p.Priorities.Plot, p.Priorities.Characters, p.Priorities.Prose, p.Priorities.Pacing, p.Priorities.Worldbuilding,
		p.Priorities.Dialogue, p.Priorities.EmotionalImpact, p.Priorities.Originality, p.Priorities.Themes,
		join(p.Biases), join(p.BlindSpots))
}

func betaMemoryBlock(m *BetaReaderMemory, lang string) string {
	if m == nil {
		if normalizeLangEN(lang) {
			return "PRIOR READING MEMORY: none — this is your first act with this novel."
		}
		return "MEMORIA DE LECTURA PREVIA: ninguna — este es tu primer acto con esta novela."
	}
	en := normalizeLangEN(lang)
	join := func(v []string) string {
		if len(v) == 0 {
			return "—"
		}
		return strings.Join(v, "; ")
	}
	if en {
		return fmt.Sprintf(`PRIOR READING MEMORY (you read up to chapter %d; do NOT use anything later):
Known events: %s
Character views: %s
Predictions: %s
Open questions: %s
Emotional state: %s`,
			m.ThroughChapter, join(m.KnownEvents), join(m.CharacterViews), join(m.Predictions), join(m.OpenQuestions), m.EmotionalState)
	}
	return fmt.Sprintf(`MEMORIA DE LECTURA PREVIA (leíste hasta el capítulo %d; NO uses nada posterior):
Eventos conocidos: %s
Visión de personajes: %s
Predicciones: %s
Preguntas abiertas: %s
Estado emocional: %s`,
		m.ThroughChapter, join(m.KnownEvents), join(m.CharacterViews), join(m.Predictions), join(m.OpenQuestions), m.EmotionalState)
}

func betaActMaterial(state *Progress, start, end int, lang string) string {
	en := normalizeLangEN(lang)
	var b strings.Builder
	for _, ch := range state.Chapters {
		if ch.Num < start || ch.Num > end {
			continue
		}
		if en {
			fmt.Fprintf(&b, "=== CHAPTER #%d \"%s\" (%d words) ===\n[Outline] %s\n[Summary] %s\n[Prose]\n%s\n\n",
				ch.Num, ch.Title, ch.WordCount, draftTruncate(ch.Outline, 900), draftTruncate(ch.Summary, 900), draftTruncate(ch.Content, 9000))
		} else {
			fmt.Fprintf(&b, "=== CAPÍTULO #%d \"%s\" (%d palabras) ===\n[Outline] %s\n[Resumen] %s\n[Prosa]\n%s\n\n",
				ch.Num, ch.Title, ch.WordCount, draftTruncate(ch.Outline, 900), draftTruncate(ch.Summary, 900), draftTruncate(ch.Content, 9000))
		}
	}
	return b.String()
}

func betaReadingPrompt(cfg *config.Config, persona *BetaPersona, memory *BetaReaderMemory, state *Progress, actNo, start, end int, lang string) string {
	actName := ""
	if state.PrePlanning != nil && actNo >= 1 && actNo <= len(state.PrePlanning.Acts) {
		actName = state.PrePlanning.Acts[actNo-1].Name
	}
	meta := betaMetadataBlock(cfg, nil, lang)
	material := betaActMaterial(state, start, end, lang)
	if normalizeLangEN(lang) {
		return fmt.Sprintf(`%s

NOVEL METADATA:
%s

YOU ARE READING ACT %d ("%s"), CHAPTERS %d–%d, IN ORDER.
Read sequentially, chapter by chapter. At each chapter use ONLY what has been revealed up to that point: no future events, no outside knowledge of the book. Keep your persona's expectations, suspicions and interpretations alive as you read.

%s

%s

%s

After reading, return STRICT JSON only:
{"reading_notes":"your private chapter-by-chapter reading notes, including what you expected, suspected and felt","engagement_by_chapter":[{"chapter":1,"engagement":0-100,"note":"why interest rose or fell"}],"most_engaging":[{"chapter":1,"location":"scene/paragraph description","quote":"short verbatim excerpt (max 25 words) or empty","note":"why"}],"least_engaging":[...same shape...],"memorable":[...],"confusing":[...],"surprising":[...],"emotional":[...],"memory":{"known_events":["..."],"character_views":["..."],"predictions":["..."],"open_questions":["..."],"emotional_state":"..."}}
Rules: 1-4 moments per category; every moment needs a real chapter number and location. Quotes must be copied verbatim from the prose above — if you cannot copy exactly, leave "quote" empty. Never invent events or quotations. Do not propose fixes.`,
			betaSystemPrompt(lang), meta, actNo, actName, start, end,
			betaPersonaBlock(persona, lang), betaMemoryBlock(memory, lang), material)
	}
	return fmt.Sprintf(`%s

METADATOS DE LA NOVELA:
%s

ESTÁS LEYENDO EL ACTO %d ("%s"), CAPÍTULOS %d–%d, EN ORDEN.
Lee de forma secuencial, capítulo a capítulo. En cada capítulo usa SOLO lo revelado hasta ese punto: nada de eventos futuros ni conocimiento externo del libro. Mantén vivas las expectativas, sospechas e interpretaciones de tu persona mientras lees.

%s

%s

%s

Después de leer, devuelve SOLO JSON estricto:
{"reading_notes":"tus notas privadas de lectura capítulo a capítulo, incluyendo qué esperabas, sospechabas y sentías","engagement_by_chapter":[{"chapter":1,"engagement":0-100,"note":"por qué subió o bajó el interés"}],"most_engaging":[{"chapter":1,"location":"descripción de la escena/párrafo","quote":"extracto textual breve (máx. 25 palabras) o vacío","note":"por qué"}],"least_engaging":[...misma forma...],"memorable":[...],"confusing":[...],"surprising":[...],"emotional":[...],"memory":{"known_events":["..."],"character_views":["..."],"predictions":["..."],"open_questions":["..."],"emotional_state":"..."}}
Reglas: 1-4 momentos por categoría; cada momento necesita un número de capítulo y una ubicación reales. Las citas deben copiarse textualmente de la prosa de arriba — si no puedes copiarlas exactamente, deja "quote" vacío. Nunca inventes eventos ni citas. No propongas correcciones.`,
		betaSystemPrompt(lang), meta, actNo, actName, start, end,
		betaPersonaBlock(persona, lang), betaMemoryBlock(memory, lang), material)
}

func betaEvaluationPrompt(cfg *config.Config, persona *BetaPersona, memory *BetaReaderMemory, state *Progress, actNo, start, end int, reading betaReadingResult, lang string) string {
	actName := ""
	if state.PrePlanning != nil && actNo >= 1 && actNo <= len(state.PrePlanning.Acts) {
		actName = state.PrePlanning.Acts[actNo-1].Name
	}
	notes, _ := json.Marshal(map[string]any{
		"reading_notes":         draftTruncate(reading.ReadingNotes, 4000),
		"engagement_by_chapter": reading.EngagementByChapter,
		"most_engaging":         reading.MostEngaging,
		"least_engaging":        reading.LeastEngaging,
		"memorable":             reading.Memorable,
		"confusing":             reading.Confusing,
		"surprising":            reading.Surprising,
		"emotional":             reading.Emotional,
	})
	meta := betaMetadataBlock(cfg, nil, lang)
	if normalizeLangEN(lang) {
		return fmt.Sprintf(`%s

%s

%s

You have just finished ACT %d ("%s"), chapters %d–%d. Your private reading notes from that act:
%s

Now give YOUR individual evaluation as this persona. Judge the act as a reading experience, not as an editor. Be honest: if you enjoyed it, say so; do not invent criticism to seem rigorous. Ground every observation in the act you just read.

Return STRICT JSON only:
{"engagement":0-100,"satisfaction":0-100,"continue_reading":0-100,"character_perceptions":[{"character":"...","perception":"...","emotional_connection":"..."}],"understood":["..."],"misunderstood":["..."],"suspects":["..."],"expectations":["what you expect next"],"worked":["what worked particularly well"],"concerns":[{"description":"...","chapter":1,"location":"...","quote":"verbatim excerpt or empty","kind":"narrative_problem|personal_preference"}],"personal_preferences":["preferences that shaped your judgment"],"summary":"your overall reaction in 2-4 sentences"}
Rules: distinguish narrative problems from personal preference in every concern's "kind". Never invent events or quotations. Do not propose fixes. Do not speak for other readers.`,
			betaSystemPrompt(lang), betaPersonaBlock(persona, lang), meta, actNo, actName, start, end, string(notes))
	}
	return fmt.Sprintf(`%s

%s

%s

Acabas de terminar el ACTO %d ("%s"), capítulos %d–%d. Tus notas privadas de lectura de ese acto:
%s

Ahora da TU evaluación individual como esta persona. Juzga el acto como experiencia de lectura, no como editor. Sé honesto: si te gustó, dilo; no inventes críticas para parecer riguroso. Ancla cada observación en el acto que acabas de leer.

Devuelve SOLO JSON estricto:
{"engagement":0-100,"satisfaction":0-100,"continue_reading":0-100,"character_perceptions":[{"character":"...","perception":"...","emotional_connection":"..."}],"understood":["..."],"misunderstood":["..."],"suspects":["..."],"expectations":["qué esperas después"],"worked":["qué funcionó especialmente bien"],"concerns":[{"description":"...","chapter":1,"location":"...","quote":"extracto textual o vacío","kind":"narrative_problem|personal_preference"}],"personal_preferences":["preferencias que moldearon tu juicio"],"summary":"tu reacción general en 2-4 frases"}
Reglas: distingue problemas narrativos de preferencia personal en el "kind" de cada concern. Nunca inventes eventos ni citas. No propongas correcciones. No hables por otros lectores.`,
		betaSystemPrompt(lang), betaPersonaBlock(persona, lang), meta, actNo, actName, start, end, string(notes))
}

func betaConsolidationPrompt(cfg *config.Config, state *Progress, actNo, start, end int, evals []BetaReaderEvaluation, part string, lang string) string {
	actName := ""
	if state.PrePlanning != nil && actNo >= 1 && actNo <= len(state.PrePlanning.Acts) {
		actName = state.PrePlanning.Acts[actNo-1].Name
	}
	data, _ := json.Marshal(evals)
	meta := betaMetadataBlock(cfg, nil, lang)
	if part == "experience" {
		if normalizeLangEN(lang) {
			return fmt.Sprintf(`You are the neutral facilitator of a simulated beta-reading panel. Eight simulated readers independently evaluated ACT %d ("%s"), chapters %d–%d of a novel. Consolidate their feedback. This is qualitative simulation, NOT statistical evidence about real readers.

NOVEL METADATA:
%s

INDIVIDUAL EVALUATIONS (JSON; readers are identified by persona_id):
%s

Return STRICT JSON only:
{"summary":"one paragraph on the shared reading experience","shared_positives":[FINDING],"shared_concerns":[FINDING],"polarized_reactions":[FINDING],"unfulfilled_expectations":[FINDING],"productive_ambiguity":[FINDING]}
FINDING = {"description":"...","locations":[{"chapter":1,"scene":"...","quote":"verbatim excerpt or empty"}],"readers":["r1","r3"],"severity":"low|medium|high","confidence":"low|medium|high","kind":"narrative_problem|subjective_preference"}
Rules: only report a shared reaction when at least TWO readers support it; use "individual_observations" for single readers (not in this call). "readers" must contain persona_id values from the evaluations above. "kind" must distinguish a likely narrative problem from a subjective preference or genre expectation. Severity = impact on the reading experience; confidence = how well the evidence supports it. Never invent quotations; copy only excerpts present in the evaluations. Do not propose fixes. Max 6 findings per category.`, actNo, actName, start, end, meta, string(data))
		}
		return fmt.Sprintf(`Eres el facilitador neutral de un panel simulado de beta readers. Ocho lectores simulados evaluaron de forma independiente el ACTO %d ("%s"), capítulos %d–%d de una novela. Consolida su feedback. Esto es simulación cualitativa, NO evidencia estadística sobre lectores reales.

METADATOS DE LA NOVELA:
%s

EVALUACIONES INDIVIDUALES (JSON; los lectores se identifican por persona_id):
%s

Devuelve SOLO JSON estricto:
{"summary":"un párrafo sobre la experiencia de lectura compartida","shared_positives":[HALLAZGO],"shared_concerns":[HALLAZGO],"polarized_reactions":[HALLAZGO],"unfulfilled_expectations":[HALLAZGO],"productive_ambiguity":[HALLAZGO]}
HALLAZGO = {"description":"...","locations":[{"chapter":1,"scene":"...","quote":"extracto textual o vacío"}],"readers":["r1","r3"],"severity":"low|medium|high","confidence":"low|medium|high","kind":"narrative_problem|subjective_preference"}
Reglas: reporta una reacción compartida solo cuando la apoyen al menos DOS lectores; usa "individual_observations" para lectores únicos (no en esta llamada). "readers" debe contener valores persona_id de las evaluaciones de arriba. "kind" debe distinguir un probable problema narrativo de una preferencia subjetiva o expectativa de género. Severidad = impacto en la experiencia; confianza = cuánto la evidencia lo sostiene. Nunca inventes citas; copia solo extractos presentes en las evaluaciones. No propongas correcciones. Máx. 6 hallazgos por categoría.`, actNo, actName, start, end, meta, string(data))
	}
	if normalizeLangEN(lang) {
		return fmt.Sprintf(`You are the neutral facilitator of a simulated beta-reading panel. Eight simulated readers independently evaluated ACT %d ("%s"), chapters %d–%d of a novel. Consolidate the interpretation and engagement side of their feedback. This is qualitative simulation, NOT statistical evidence about real readers.

NOVEL METADATA:
%s

INDIVIDUAL EVALUATIONS (JSON; readers are identified by persona_id):
%s

Return STRICT JSON only:
{"unexpected_interpretations":[FINDING],"engagement_patterns":[FINDING],"character_perception":[FINDING],"memorable_elements":[FINDING],"individual_observations":[FINDING]}
FINDING = {"description":"...","locations":[{"chapter":1,"scene":"...","quote":"verbatim excerpt or empty"}],"readers":["r1","r3"],"severity":"low|medium|high","confidence":"low|medium|high","kind":"narrative_problem|subjective_preference"}
Rules: engagement_patterns should say where interest rose or fell and why. unexpected_interpretations = what readers understood differently from the likely intent. character_perception = how readers see characters and their development. individual_observations = interesting feedback from a SINGLE reader. "readers" must contain persona_id values from the evaluations above. Never invent quotations. Do not propose fixes. Max 6 findings per category.`, actNo, actName, start, end, meta, string(data))
	}
	return fmt.Sprintf(`Eres el facilitador neutral de un panel simulado de beta readers. Ocho lectores simulados evaluaron de forma independiente el ACTO %d ("%s"), capítulos %d–%d de una novela. Consolida el lado de interpretación y engagement de su feedback. Esto es simulación cualitativa, NO evidencia estadística sobre lectores reales.

METADATOS DE LA NOVELA:
%s

EVALUACIONES INDIVIDUALES (JSON; los lectores se identifican por persona_id):
%s

Devuelve SOLO JSON estricto:
{"unexpected_interpretations":[HALLAZGO],"engagement_patterns":[HALLAZGO],"character_perception":[HALLAZGO],"memorable_elements":[HALLAZGO],"individual_observations":[HALLAZGO]}
HALLAZGO = {"description":"...","locations":[{"chapter":1,"scene":"...","quote":"extracto textual o vacío"}],"readers":["r1","r3"],"severity":"low|medium|high","confidence":"low|medium|high","kind":"narrative_problem|subjective_preference"}
Reglas: engagement_patterns debe decir dónde subió o bajó el interés y por qué. unexpected_interpretations = lo que los lectores entendieron distinto de la intención probable. character_perception = cómo ven a los personajes y su desarrollo. individual_observations = feedback interesante de UN SOLO lector. "readers" debe contener valores persona_id de las evaluaciones de arriba. Nunca inventes citas. No propongas correcciones. Máx. 6 hallazgos por categoría.`, actNo, actName, start, end, meta, string(data))
}

// ---------------------------------------------------------------------------
// Sanitizers
// ---------------------------------------------------------------------------

func betaChaptersByNum(state *Progress, start, end int) map[int]string {
	out := map[int]string{}
	for _, ch := range state.Chapters {
		if ch.Num >= start && ch.Num <= end {
			out[ch.Num] = ch.Content
		}
	}
	return out
}

// betaQuoteGrounded rejects invented quotations: the quote must appear (after
// whitespace/case normalization) in the chapter prose. When it cannot be
// verified the quote is cleared and the location is kept.
func betaQuoteGrounded(quote string, chapter int, chapters map[int]string) string {
	q := strings.Trim(strings.TrimSpace(quote), "\"“”‘’'")
	nq := betaNormSpace(q)
	if nq == "" {
		return ""
	}
	check := func(content string) bool {
		return strings.Contains(betaNormSpace(content), nq)
	}
	if content, ok := chapters[chapter]; ok && check(content) {
		return draftTruncate(q, 200)
	}
	// The reader may have attributed a cross-chapter quote to the wrong
	// chapter; accept it when it exists anywhere in the act.
	for _, content := range chapters {
		if check(content) {
			return draftTruncate(q, 200)
		}
	}
	// Last chance: the first six words, for excerpts the model trimmed.
	words := strings.Fields(nq)
	if len(words) > 6 {
		prefix := strings.Join(words[:6], " ")
		for _, content := range chapters {
			if strings.Contains(betaNormSpace(content), prefix) {
				return draftTruncate(q, 200)
			}
		}
	}
	return ""
}

func sanitizeBetaMoments(in []BetaMoment, chapters map[int]string, start, end int) []BetaMoment {
	out := make([]BetaMoment, 0, len(in))
	for _, m := range in {
		if len(out) >= 4 {
			break
		}
		if m.Chapter < start || m.Chapter > end {
			m.Chapter = 0
		}
		m.Location = draftTruncate(strings.TrimSpace(m.Location), 240)
		m.Note = draftTruncate(strings.TrimSpace(m.Note), 500)
		m.Quote = betaQuoteGrounded(m.Quote, m.Chapter, chapters)
		if m.Location == "" && m.Quote == "" && m.Note == "" {
			continue
		}
		out = append(out, m)
	}
	return out
}

func sanitizeBetaConcerns(in []BetaConcern, chapters map[int]string, start, end int) []BetaConcern {
	out := make([]BetaConcern, 0, len(in))
	for _, c := range in {
		if len(out) >= 6 {
			break
		}
		c.Description = draftTruncate(strings.TrimSpace(c.Description), 600)
		if c.Description == "" {
			continue
		}
		if c.Chapter < start || c.Chapter > end {
			c.Chapter = 0
		}
		c.Location = draftTruncate(strings.TrimSpace(c.Location), 240)
		c.Quote = betaQuoteGrounded(c.Quote, c.Chapter, chapters)
		if c.Kind != "personal_preference" {
			c.Kind = "narrative_problem"
		}
		out = append(out, c)
	}
	return out
}

func sanitizeBetaReading(r *betaReadingResult, chapters map[int]string, start, end int) {
	r.ReadingNotes = draftTruncate(strings.TrimSpace(r.ReadingNotes), 4000)
	byCh := map[int]bool{}
	out := make([]BetaChapterEngagement, 0, len(r.EngagementByChapter))
	for _, e := range r.EngagementByChapter {
		if e.Chapter < start || e.Chapter > end || byCh[e.Chapter] {
			continue
		}
		byCh[e.Chapter] = true
		e.Engagement = clampBetaInt(e.Engagement, 0, 100)
		e.Note = draftTruncate(strings.TrimSpace(e.Note), 300)
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Chapter < out[j].Chapter })
	r.EngagementByChapter = out
	r.MostEngaging = sanitizeBetaMoments(r.MostEngaging, chapters, start, end)
	r.LeastEngaging = sanitizeBetaMoments(r.LeastEngaging, chapters, start, end)
	r.Memorable = sanitizeBetaMoments(r.Memorable, chapters, start, end)
	r.Confusing = sanitizeBetaMoments(r.Confusing, chapters, start, end)
	r.Surprising = sanitizeBetaMoments(r.Surprising, chapters, start, end)
	r.Emotional = sanitizeBetaMoments(r.Emotional, chapters, start, end)
	r.Memory.KnownEvents = betaTrimStrings(r.Memory.KnownEvents, 12, 240)
	r.Memory.CharacterViews = betaTrimStrings(r.Memory.CharacterViews, 12, 240)
	r.Memory.Predictions = betaTrimStrings(r.Memory.Predictions, 10, 240)
	r.Memory.OpenQuestions = betaTrimStrings(r.Memory.OpenQuestions, 10, 240)
	r.Memory.EmotionalState = draftTruncate(strings.TrimSpace(r.Memory.EmotionalState), 400)
}

func sanitizeBetaEvaluation(r *betaEvaluationResult) {
	r.Engagement = clampBetaInt(r.Engagement, 0, 100)
	r.Satisfaction = clampBetaInt(r.Satisfaction, 0, 100)
	r.ContinueReading = clampBetaInt(r.ContinueReading, 0, 100)
	r.Understood = betaTrimStrings(r.Understood, 8, 400)
	r.Misunderstood = betaTrimStrings(r.Misunderstood, 8, 400)
	r.Suspects = betaTrimStrings(r.Suspects, 8, 400)
	r.Expectations = betaTrimStrings(r.Expectations, 8, 400)
	r.Worked = betaTrimStrings(r.Worked, 8, 400)
	r.PersonalPreferences = betaTrimStrings(r.PersonalPreferences, 8, 400)
	r.Summary = draftTruncate(strings.TrimSpace(r.Summary), 800)
	seen := map[string]bool{}
	out := make([]BetaCharacterPerception, 0, len(r.CharacterPerceptions))
	for _, cp := range r.CharacterPerceptions {
		cp.Character = draftTruncate(strings.TrimSpace(cp.Character), 120)
		cp.Perception = draftTruncate(strings.TrimSpace(cp.Perception), 500)
		cp.EmotionalConnection = draftTruncate(strings.TrimSpace(cp.EmotionalConnection), 300)
		if cp.Character == "" || cp.Perception == "" || seen[betaNormSpace(cp.Character)] {
			continue
		}
		seen[betaNormSpace(cp.Character)] = true
		out = append(out, cp)
		if len(out) >= 10 {
			break
		}
	}
	r.CharacterPerceptions = out
}

func sanitizeBetaFinding(f BetaFinding, chapters map[int]string, start, end int, validReaders map[string]bool) BetaFinding {
	f.Description = draftTruncate(strings.TrimSpace(f.Description), 900)
	f.Severity = normalizeBetaLevel(f.Severity)
	f.Confidence = normalizeBetaLevel(f.Confidence)
	if f.Kind != "subjective_preference" {
		f.Kind = "narrative_problem"
	}
	locs := make([]BetaLocation, 0, len(f.Locations))
	for _, l := range f.Locations {
		if len(locs) >= 4 {
			break
		}
		if l.Chapter < start || l.Chapter > end {
			l.Chapter = 0
		}
		l.Scene = draftTruncate(strings.TrimSpace(l.Scene), 240)
		l.Quote = betaQuoteGrounded(l.Quote, l.Chapter, chapters)
		if l.Scene == "" && l.Quote == "" && l.Chapter == 0 {
			continue
		}
		locs = append(locs, l)
	}
	f.Locations = locs
	readers := make([]string, 0, len(f.Readers))
	seen := map[string]bool{}
	for _, id := range f.Readers {
		id = strings.TrimSpace(id)
		if validReaders[id] && !seen[id] {
			seen[id] = true
			readers = append(readers, id)
		}
	}
	f.Readers = readers
	f.ReaderCount = len(readers)
	return f
}

func normalizeBetaLevel(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high", "alta", "alto":
		return "high"
	case "low", "baja", "bajo":
		return "low"
	default:
		return "medium"
	}
}

func sanitizeBetaFindings(in []BetaFinding, chapters map[int]string, start, end int, validReaders map[string]bool) []BetaFinding {
	out := make([]BetaFinding, 0, len(in))
	for _, f := range in {
		if len(out) >= 6 {
			break
		}
		f = sanitizeBetaFinding(f, chapters, start, end, validReaders)
		if f.Description == "" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// ---------------------------------------------------------------------------
// Execution
// ---------------------------------------------------------------------------

func betaEmit(logger *sse.LogBroadcaster, ev BetaProgressEvent) {
	if logger == nil {
		return
	}
	logger.Emit("beta_progress", ev)
}

func betaCallJSON(ctx context.Context, apiCfg *config.APIConfig, logger *sse.LogBroadcaster, system, prompt string, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		p := prompt
		if attempt > 0 {
			p += "\n\nYour previous response was invalid. Return one complete JSON object only, using the required schema."
		}
		raw := llm.CallAPIWithRetryLog(ctx, apiCfg, system, p, logger)
		if raw == "" {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		j := llm.ExtractJSON(raw)
		if j == "" {
			continue
		}
		var probe map[string]any
		if json.Unmarshal([]byte(j), &probe) != nil {
			continue
		}
		if json.Unmarshal([]byte(j), out) == nil {
			return nil
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("model response is not valid beta-reader JSON")
}

// ensureBetaPanel reuses the project panel or generates it once. The panel is
// intentionally NOT regenerated per act: the same readers follow the book.
func ensureBetaPanel(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, actNo int, logger *sse.LogBroadcaster) (*BetaPanel, bool, error) {
	if state.BetaPanel != nil && len(state.BetaPanel.Personas) >= 5 {
		return state.BetaPanel, false, nil
	}
	lang := ContentLanguage(cfg)
	betaEmit(logger, BetaProgressEvent{ActNo: actNo, Stage: "panel", Total: 1})
	var gen struct {
		Personas []BetaPersona `json:"personas"`
	}
	err := betaCallJSON(ctx, apiCfg, logger, betaSystemPrompt(lang), betaPanelPrompt(cfg, settings, lang), &gen)
	panel := &BetaPanel{GeneratedAt: time.Now().Format(time.RFC3339), BasedOn: "novel metadata", Personas: gen.Personas}
	if err != nil || !sanitizeBetaPanel(panel) {
		if logger != nil {
			if err != nil {
				logger.WarnKey("log.beta_panel_failed", err)
			} else {
				logger.WarnKey("log.beta_panel_fallback")
			}
		}
		panel = defaultBetaPanel(cfg)
	}
	state.BetaPanel = panel
	return panel, true, nil
}

// RunBetaReading produces one NEW beta-reader session for the act and appends
// it to the act's history. It assumes the drafted chapter contents are loaded.
func RunBetaReading(ctx context.Context, apiCfg *config.APIConfig, cfg *config.Config, state *Progress, settings *ProjectSettings, actNo, start, end int, fingerprint, progressPath string, logger *sse.LogBroadcaster) (*BetaReaderReport, error) {
	lang := ContentLanguage(cfg)
	panel, created, err := ensureBetaPanel(ctx, apiCfg, cfg, state, settings, actNo, logger)
	if err != nil {
		return nil, err
	}
	if created && progressPath != "" {
		if serr := SaveProgress(progressPath, state); serr != nil && logger != nil {
			logger.StorageError(serr)
		}
	}
	betaEmit(logger, BetaProgressEvent{ActNo: actNo, Stage: "personas", Total: len(panel.Personas), Panel: panel})
	if state.BetaMemories == nil {
		state.BetaMemories = map[string]*BetaReaderMemory{}
	}
	chapters := betaChaptersByNum(state, start, end)
	total := len(panel.Personas)
	evals := make([]BetaReaderEvaluation, 0, total)
	for i := range panel.Personas {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		persona := &panel.Personas[i]
		memory := state.BetaMemories[persona.ID]
		betaEmit(logger, BetaProgressEvent{ActNo: actNo, Stage: "reading", Index: i + 1, Total: total, Detail: persona.Name})
		var reading betaReadingResult
		if err := betaCallJSON(ctx, apiCfg, logger, betaSystemPrompt(lang),
			betaReadingPrompt(cfg, persona, memory, state, actNo, start, end, lang), &reading); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if logger != nil {
				logger.WarnKey("log.beta_reader_failed", persona.Name, err)
			}
			continue
		}
		sanitizeBetaReading(&reading, chapters, start, end)
		betaEmit(logger, BetaProgressEvent{ActNo: actNo, Stage: "evaluating", Index: i + 1, Total: total, Detail: persona.Name})
		var eval betaEvaluationResult
		if err := betaCallJSON(ctx, apiCfg, logger, betaSystemPrompt(lang),
			betaEvaluationPrompt(cfg, persona, memory, state, actNo, start, end, reading, lang), &eval); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if logger != nil {
				logger.WarnKey("log.beta_reader_failed", persona.Name, err)
			}
			continue
		}
		sanitizeBetaEvaluation(&eval)
		evals = append(evals, BetaReaderEvaluation{
			PersonaID: persona.ID, PersonaName: persona.Name, Archetype: persona.Archetype, Age: persona.Age,
			Engagement: eval.Engagement, Satisfaction: eval.Satisfaction, ContinueReading: eval.ContinueReading,
			ReadingNotes:        reading.ReadingNotes,
			EngagementByChapter: reading.EngagementByChapter,
			MostEngaging:        reading.MostEngaging, LeastEngaging: reading.LeastEngaging, Memorable: reading.Memorable,
			Confusing: reading.Confusing, Surprising: reading.Surprising, Emotional: reading.Emotional,
			CharacterPerceptions: eval.CharacterPerceptions, Understood: eval.Understood, Misunderstood: eval.Misunderstood,
			Suspects: eval.Suspects, Expectations: eval.Expectations, Worked: eval.Worked, Concerns: sanitizeBetaConcerns(eval.Concerns, chapters, start, end),
			PersonalPreferences: eval.PersonalPreferences, Summary: eval.Summary,
		})
		reading.Memory.PersonaID = persona.ID
		reading.Memory.ThroughChapter = end
		reading.Memory.UpdatedAt = time.Now().Format(time.RFC3339)
		state.BetaMemories[persona.ID] = &reading.Memory
	}
	if len(evals) == 0 {
		return nil, fmt.Errorf("all beta readers failed")
	}
	if len(evals) < total && logger != nil {
		logger.WarnKey("log.beta_panel_partial", len(evals), total)
	}

	validReaders := map[string]bool{}
	for _, e := range evals {
		validReaders[e.PersonaID] = true
	}
	betaEmit(logger, BetaProgressEvent{ActNo: actNo, Stage: "consolidating", Total: 2})
	var exp betaConsolidationExperience
	var interp betaConsolidationInterpretation
	expErr := betaCallJSON(ctx, apiCfg, logger, betaSystemPrompt(lang),
		betaConsolidationPrompt(cfg, state, actNo, start, end, evals, "experience", lang), &exp)
	if expErr != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	betaEmit(logger, BetaProgressEvent{ActNo: actNo, Stage: "consolidating", Index: 2, Total: 2})
	interpErr := betaCallJSON(ctx, apiCfg, logger, betaSystemPrompt(lang),
		betaConsolidationPrompt(cfg, state, actNo, start, end, evals, "interpretation", lang), &interp)
	if interpErr != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if expErr != nil && logger != nil {
		logger.WarnKey("log.beta_consolidation_partial", expErr)
	}
	if interpErr != nil && logger != nil {
		logger.WarnKey("log.beta_consolidation_partial", interpErr)
	}

	report := buildBetaReport(actNo, actNameOf(state, actNo), start, end, fingerprint, evals,
		exp, interp, chapters, validReaders, lang)
	report.GeneratedAt = time.Now().Format(time.RFC3339)
	report.SessionID = fmt.Sprintf("act%d-%d", actNo, time.Now().Unix())
	if state.BetaReports == nil {
		state.BetaReports = map[int][]*BetaReaderReport{}
	}
	state.BetaReports[actNo] = append(state.BetaReports[actNo], report)
	if progressPath != "" {
		if serr := SaveProgress(progressPath, state); serr != nil && logger != nil {
			logger.StorageError(serr)
		}
	}
	return report, nil
}

func actNameOf(state *Progress, actNo int) string {
	if state.PrePlanning != nil && actNo >= 1 && actNo <= len(state.PrePlanning.Acts) {
		return strings.TrimSpace(state.PrePlanning.Acts[actNo-1].Name)
	}
	return ""
}

func buildBetaReport(actNo int, actName string, start, end int, fingerprint string, evals []BetaReaderEvaluation,
	exp betaConsolidationExperience, interp betaConsolidationInterpretation,
	chapters map[int]string, validReaders map[string]bool, lang string) *BetaReaderReport {

	report := &BetaReaderReport{
		ActNo: actNo, ActName: actName, StartCh: start, EndCh: end,
		DraftFingerprint: fingerprint, ContentFingerprint: fingerprint, PanelSize: len(evals), Evaluations: evals,
		Summary:                   draftTruncate(strings.TrimSpace(exp.Summary), 4000),
		SharedPositives:           sanitizeBetaFindings(exp.SharedPositives, chapters, start, end, validReaders),
		SharedConcerns:            sanitizeBetaFindings(exp.SharedConcerns, chapters, start, end, validReaders),
		PolarizedReactions:        sanitizeBetaFindings(exp.PolarizedReactions, chapters, start, end, validReaders),
		UnfulfilledExpectations:   sanitizeBetaFindings(exp.UnfulfilledExpectations, chapters, start, end, validReaders),
		ProductiveAmbiguity:       sanitizeBetaFindings(exp.ProductiveAmbiguity, chapters, start, end, validReaders),
		UnexpectedInterpretations: sanitizeBetaFindings(interp.UnexpectedInterpretations, chapters, start, end, validReaders),
		EngagementPatterns:        sanitizeBetaFindings(interp.EngagementPatterns, chapters, start, end, validReaders),
		CharacterPerception:       sanitizeBetaFindings(interp.CharacterPerception, chapters, start, end, validReaders),
		MemorableElements:         sanitizeBetaFindings(interp.MemorableElements, chapters, start, end, validReaders),
		IndividualObservations:    sanitizeBetaFindings(interp.IndividualObservations, chapters, start, end, validReaders),
	}
	report.AverageEngagement, report.AverageContinue = betaAverages(evals)
	fillBetaReportFallback(report, evals, lang)
	return report
}

func betaAverages(evals []BetaReaderEvaluation) (int, int) {
	if len(evals) == 0 {
		return 0, 0
	}
	var eng, cont int
	for _, e := range evals {
		eng += e.Engagement
		cont += e.ContinueReading
	}
	return eng / len(evals), cont / len(evals)
}

// fillBetaReportFallback fills missing consolidated categories directly from
// the individual evaluations, so a failed consolidation call still yields a
// usable report instead of discarding eight readers' work.
func fillBetaReportFallback(report *BetaReaderReport, evals []BetaReaderEvaluation, lang string) {
	en := normalizeLangEN(lang)
	if report.Summary == "" {
		if en {
			report.Summary = fmt.Sprintf("%d simulated readers evaluated this act. Average engagement %d/100; average desire to continue %d/100. The consolidated findings below are assembled directly from their individual evaluations.", len(evals), report.AverageEngagement, report.AverageContinue)
		} else {
			report.Summary = fmt.Sprintf("%d lectores simulados evaluaron este acto. Engagement medio %d/100; deseo medio de continuar %d/100. Los hallazgos consolidados de abajo se arman directamente a partir de sus evaluaciones individuales.", len(evals), report.AverageEngagement, report.AverageContinue)
		}
	}
	if len(report.SharedPositives) == 0 {
		for _, e := range evals {
			for _, w := range e.Worked {
				report.SharedPositives = append(report.SharedPositives, BetaFinding{Description: w, Readers: []string{e.PersonaID}, ReaderCount: 1, Severity: "low", Confidence: "medium", Kind: "subjective_preference"})
				if len(report.SharedPositives) >= 6 {
					break
				}
			}
			if len(report.SharedPositives) >= 6 {
				break
			}
		}
	}
	if len(report.SharedConcerns) == 0 {
		for _, e := range evals {
			for _, c := range e.Concerns {
				f := BetaFinding{Description: c.Description, Readers: []string{e.PersonaID}, ReaderCount: 1, Severity: "medium", Confidence: "medium", Kind: c.Kind}
				if c.Chapter > 0 || c.Location != "" || c.Quote != "" {
					f.Locations = []BetaLocation{{Chapter: c.Chapter, Scene: c.Location, Quote: c.Quote}}
				}
				report.SharedConcerns = append(report.SharedConcerns, f)
				if len(report.SharedConcerns) >= 6 {
					break
				}
			}
			if len(report.SharedConcerns) >= 6 {
				break
			}
		}
	}
	if len(report.EngagementPatterns) == 0 {
		byCh := map[int][]int{}
		for _, e := range evals {
			for _, ce := range e.EngagementByChapter {
				byCh[ce.Chapter] = append(byCh[ce.Chapter], ce.Engagement)
			}
		}
		nums := make([]int, 0, len(byCh))
		for n := range byCh {
			nums = append(nums, n)
		}
		sort.Ints(nums)
		for _, n := range nums {
			vals := byCh[n]
			avg := 0
			for _, v := range vals {
				avg += v
			}
			avg /= len(vals)
			desc := fmt.Sprintf("Chapter %d: average engagement %d/100 across %d readers.", n, avg, len(vals))
			if !en {
				desc = fmt.Sprintf("Capítulo %d: engagement medio %d/100 entre %d lectores.", n, avg, len(vals))
			}
			sev := "low"
			if avg < 50 {
				sev = "high"
			} else if avg < 70 {
				sev = "medium"
			}
			report.EngagementPatterns = append(report.EngagementPatterns, BetaFinding{Description: desc, Locations: []BetaLocation{{Chapter: n}}, Severity: sev, Confidence: "medium", Kind: "narrative_problem"})
		}
	}
	if len(report.MemorableElements) == 0 {
		for _, e := range evals {
			for _, m := range e.Memorable {
				report.MemorableElements = append(report.MemorableElements, BetaFinding{Description: m.Note, Readers: []string{e.PersonaID}, ReaderCount: 1, Severity: "low", Confidence: "medium", Kind: "subjective_preference", Locations: []BetaLocation{{Chapter: m.Chapter, Scene: m.Location, Quote: m.Quote}}})
				if len(report.MemorableElements) >= 6 {
					break
				}
			}
			if len(report.MemorableElements) >= 6 {
				break
			}
		}
	}
	if len(report.CharacterPerception) == 0 {
		for _, e := range evals {
			for _, cp := range e.CharacterPerceptions {
				desc := cp.Character + ": " + cp.Perception
				if cp.EmotionalConnection != "" {
					desc += " (" + cp.EmotionalConnection + ")"
				}
				report.CharacterPerception = append(report.CharacterPerception, BetaFinding{Description: desc, Readers: []string{e.PersonaID}, ReaderCount: 1, Severity: "low", Confidence: "medium", Kind: "subjective_preference"})
				if len(report.CharacterPerception) >= 6 {
					break
				}
			}
			if len(report.CharacterPerception) >= 6 {
				break
			}
		}
	}
	if len(report.UnexpectedInterpretations) == 0 {
		for _, e := range evals {
			for _, m := range e.Misunderstood {
				report.UnexpectedInterpretations = append(report.UnexpectedInterpretations, BetaFinding{Description: m, Readers: []string{e.PersonaID}, ReaderCount: 1, Severity: "medium", Confidence: "medium", Kind: "narrative_problem"})
				if len(report.UnexpectedInterpretations) >= 6 {
					break
				}
			}
			if len(report.UnexpectedInterpretations) >= 6 {
				break
			}
		}
	}
	if len(report.UnfulfilledExpectations) == 0 {
		for _, e := range evals {
			for _, x := range e.Expectations {
				report.UnfulfilledExpectations = append(report.UnfulfilledExpectations, BetaFinding{Description: x, Readers: []string{e.PersonaID}, ReaderCount: 1, Severity: "low", Confidence: "low", Kind: "subjective_preference"})
				if len(report.UnfulfilledExpectations) >= 6 {
					break
				}
			}
			if len(report.UnfulfilledExpectations) >= 6 {
				break
			}
		}
	}
	if len(report.IndividualObservations) == 0 {
		for _, e := range evals {
			if strings.TrimSpace(e.Summary) == "" {
				continue
			}
			report.IndividualObservations = append(report.IndividualObservations, BetaFinding{Description: e.Summary, Readers: []string{e.PersonaID}, ReaderCount: 1, Severity: "low", Confidence: "medium", Kind: "subjective_preference"})
			if len(report.IndividualObservations) >= 6 {
				break
			}
		}
	}
}

// latestBetaReport returns the newest session for the act, optionally
// restricted to one draft fingerprint.
func latestBetaReport(state *Progress, actNo int, fingerprint string) *BetaReaderReport {
	list := state.BetaReports[actNo]
	for i := len(list) - 1; i >= 0; i-- {
		r := list[i]
		if r == nil {
			continue
		}
		if fingerprint == "" || r.ContentFingerprint == fingerprint || (r.ContentFingerprint == "" && r.DraftFingerprint == fingerprint) {
			return r
		}
	}
	// Legacy sessions predate ContentFingerprint: reuse the newest one so an
	// upgrade does not force a full re-read of unchanged prose. Any session
	// written from now on carries the field and is matched exactly, so this
	// fallback stops applying once one exists.
	for _, r := range list {
		if r != nil && r.ContentFingerprint != "" {
			return nil
		}
	}
	for i := len(list) - 1; i >= 0; i-- {
		if r := list[i]; r != nil && r.ContentFingerprint == "" {
			return r
		}
	}
	return nil
}

// betaReportBlock renders the consolidated report for the expert prompt. It is
// deliberately bounded: the expert sees the panel's strongest signals, not a
// transcript, and is told to judge independently.
func betaReportBlock(r *BetaReaderReport, lang string) string {
	if r == nil || len(r.Evaluations) == 0 {
		return ""
	}
	en := normalizeLangEN(lang)
	var b strings.Builder
	if en {
		fmt.Fprintf(&b, "[SIMULATED BETA READER PANEL — QUALITATIVE SIMULATION, NOT STATISTICAL EVIDENCE]\n")
		fmt.Fprintf(&b, "%d simulated readers (not real people) independently read this act chapter by chapter. Consolidated report: %s\n", r.PanelSize, draftTruncate(r.Summary, 1200))
		fmt.Fprintf(&b, "Average engagement %d/100; average desire to continue %d/100.\n", r.AverageEngagement, r.AverageContinue)
		writeBetaFindings(&b, "Shared positives", r.SharedPositives, r.PanelSize)
		writeBetaFindings(&b, "Shared concerns", r.SharedConcerns, r.PanelSize)
		writeBetaFindings(&b, "Polarized reactions", r.PolarizedReactions, r.PanelSize)
		writeBetaFindings(&b, "Unexpected interpretations", r.UnexpectedInterpretations, r.PanelSize)
		writeBetaFindings(&b, "Unfulfilled expectations", r.UnfulfilledExpectations, r.PanelSize)
		writeBetaFindings(&b, "Engagement patterns", r.EngagementPatterns, r.PanelSize)
		writeBetaFindings(&b, "Character perception", r.CharacterPerception, r.PanelSize)
		writeBetaFindings(&b, "Memorable elements", r.MemorableElements, r.PanelSize)
		writeBetaFindings(&b, "Productive ambiguity", r.ProductiveAmbiguity, r.PanelSize)
		writeBetaFindings(&b, "Individual observations", r.IndividualObservations, r.PanelSize)
		b.WriteString("\nHOW TO USE THIS REPORT: judge independently — their average engagement is evidence about the reading experience, not a score for you to copy, and their wording is not yours to reuse. First separate genuine craft weaknesses from subjective preferences and intentional choices. Then RECONCILE: compare the panel's engagement with the reader_profile criteria you grade; if your grade and their experience diverge by more than 15 points, explain the gap in that pillar's note using the concrete findings above, and make sure your criteria levels and suggestions answer the strongest shared concerns. Your job is to be the expert complement to the panel — take its signals, add the craft judgment it cannot provide, and aim every point at raising each pillar as close to 100 as the material honestly allows.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "[PANEL SIMULADO DE BETA READERS — SIMULACIÓN CUALITATIVA, NO EVIDENCIA ESTADÍSTICA]\n")
	fmt.Fprintf(&b, "%d lectores simulados (no personas reales) leyeron este acto de forma independiente, capítulo a capítulo. Informe consolidado: %s\n", r.PanelSize, draftTruncate(r.Summary, 1200))
	fmt.Fprintf(&b, "Engagement medio %d/100; deseo medio de continuar %d/100.\n", r.AverageEngagement, r.AverageContinue)
	writeBetaFindings(&b, "Reacciones positivas compartidas", r.SharedPositives, r.PanelSize)
	writeBetaFindings(&b, "Preocupaciones compartidas", r.SharedConcerns, r.PanelSize)
	writeBetaFindings(&b, "Reacciones polarizadas", r.PolarizedReactions, r.PanelSize)
	writeBetaFindings(&b, "Interpretaciones inesperadas", r.UnexpectedInterpretations, r.PanelSize)
	writeBetaFindings(&b, "Expectativas incumplidas", r.UnfulfilledExpectations, r.PanelSize)
	writeBetaFindings(&b, "Patrones de engagement", r.EngagementPatterns, r.PanelSize)
	writeBetaFindings(&b, "Percepción de personajes", r.CharacterPerception, r.PanelSize)
	writeBetaFindings(&b, "Elementos memorables", r.MemorableElements, r.PanelSize)
	writeBetaFindings(&b, "Ambigüedad productiva", r.ProductiveAmbiguity, r.PanelSize)
	writeBetaFindings(&b, "Observaciones individuales", r.IndividualObservations, r.PanelSize)
	b.WriteString("\nCÓMO USAR ESTE INFORME: juzga de forma independiente — su engagement medio es evidencia sobre la experiencia de lectura, no una nota que debas copiar, y sus palabras no son tuyas para reutilizar. Primero separa debilidades reales de oficio de preferencias subjetivas y elecciones intencionales. Después RECONCILIA: compara el engagement del panel con los criterios de reader_profile que calificas; si tu nota y su experiencia difieren en más de 15 puntos, explica la brecha en la nota de ese pilar usando los hallazgos concretos de arriba, y asegúrate de que tus niveles y sugerencias respondan a las preocupaciones compartidas más fuertes. Tu trabajo es ser el complemento experto del panel: toma sus señales, añade el juicio de oficio que el panel no puede dar y apunta cada punto a subir cada pilar lo más cerca de 100 que el material permita honestamente.\n")
	return b.String()
}

// betaReconciliationBlock makes the engagement number an explicit grading
// obligation. The expert may disagree with the panel, but a large gap between
// its reader_profile grade and the panel's engagement must be explained with
// evidence instead of silently ignored.
func betaReconciliationBlock(r *BetaReaderReport, lang string) string {
	if r == nil || len(r.Evaluations) == 0 {
		return ""
	}
	if normalizeLangEN(lang) {
		return fmt.Sprintf("\nBETA RECONCILIATION (MANDATORY): the panel's average engagement is %d/100. Grade reader_profile with your own expert judgment — do NOT copy this number — but if your reader_profile score differs from %d by more than 15 points, you MUST explain the gap in that pillar's note using concrete evidence from the act, and include at least one suggestion that answers the panel's strongest shared concern.\n", r.AverageEngagement, r.AverageEngagement)
	}
	return fmt.Sprintf("\nRECONCILIACIÓN CON EL PANEL BETA (OBLIGATORIA): el engagement medio del panel es %d/100. Califica reader_profile con tu propio juicio experto — NO copies este número — pero si tu nota de reader_profile difiere de %d en más de 15 puntos, DEBES explicar la brecha en la nota de ese pilar con evidencia concreta del acto, e incluir al menos una sugerencia que responda a la preocupación compartida más fuerte del panel.\n", r.AverageEngagement, r.AverageEngagement)
}

// reconcileReaderProfileWithBeta guarantees the persisted report visibly
// accounts for the panel's engagement. The expert may disagree with the panel,
// but a gap wider than 15 points must not go unmentioned: when the model's own
// note does not reference the panel, append the measured comparison instead of
// inventing evidence.
func reconcileReaderProfileWithBeta(pillars []PillarAssessment, r *BetaReaderReport, lang string) []PillarAssessment {
	if r == nil || len(r.Evaluations) == 0 {
		return pillars
	}
	for i := range pillars {
		if pillars[i].Key != "reader_profile" {
			continue
		}
		gap := pillars[i].Score - r.AverageEngagement
		if gap < 0 {
			gap = -gap
		}
		if gap <= 15 {
			return pillars
		}
		note := strings.ToLower(pillars[i].Note)
		if strings.Contains(note, "beta") || strings.Contains(note, "panel") ||
			strings.Contains(note, "simulated") || strings.Contains(note, "engagement") ||
			strings.Contains(pillars[i].Note, fmt.Sprintf("%d", r.AverageEngagement)) {
			return pillars
		}
		if normalizeLangEN(lang) {
			pillars[i].Note = strings.TrimSpace(pillars[i].Note + fmt.Sprintf(" Beta panel reconciliation: the simulated readers averaged %d/100 engagement while this expert grade is %d/100 (%d-point gap); the expert weighed the checklist craft issues above more heavily than the panel's immersion.", r.AverageEngagement, pillars[i].Score, gap))
		} else {
			pillars[i].Note = strings.TrimSpace(pillars[i].Note + fmt.Sprintf(" Reconciliación con el panel beta: los lectores simulados promediaron %d/100 de engagement mientras esta nota experta es %d/100 (brecha de %d puntos); el experto ponderó más los problemas de oficio de la lista que la inmersión del panel.", r.AverageEngagement, pillars[i].Score, gap))
		}
		return pillars
	}
	return pillars
}

func writeBetaFindings(b *strings.Builder, label string, findings []BetaFinding, total int) {
	if len(findings) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", label)
	for _, f := range findings {
		locs := ""
		for _, l := range f.Locations {
			if l.Chapter > 0 {
				locs += fmt.Sprintf(" ch%d", l.Chapter)
			}
			if l.Scene != "" {
				locs += " " + draftTruncate(l.Scene, 80)
			}
		}
		fmt.Fprintf(b, "- %s [%d/%d readers; severity %s; confidence %s; %s]%s\n",
			draftTruncate(f.Description, 420), f.ReaderCount, total, f.Severity, f.Confidence, f.Kind, locs)
	}
}
