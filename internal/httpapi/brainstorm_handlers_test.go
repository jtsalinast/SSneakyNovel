package httpapi

import (
	"strings"
	"testing"

	"showmethestory/internal/config"
)

func brainstormTestHandler(lang string) *Handlers {
	return &Handlers{cfg: &config.Config{Language: lang}}
}

func TestBrainstormPrompt(t *testing.T) {
	cases := []struct {
		name, lang, action, mode, want string
	}{
		{"alternatives", "en", "alternatives", "", "ONE alternative"},
		{"rewrite mode", "en", "rewrite", "show", "Show, don't tell"},
		{"rewrite default mode", "en", "rewrite", "", "keeping the meaning and voice"},
		{"describe sense", "en", "describe", "smell", `sense of "smell"`},
		{"feedback dimension", "en", "feedback", "pacing", `feedback note on "pacing"`},
		{"expand mode", "en", "rewrite", "expand", "roughly twice its length"},
		{"unknown action falls back to alternatives", "en", "bogus", "", "ONE alternative"},
		{"spanish", "es", "describe", "taste", "sentido «gusto»"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := brainstormTestHandler(tc.lang)
			_, user, errKey := h.brainstormPrompts(brainstormRequest{Action: tc.action, Mode: tc.mode, Text: "the sample text", Variant: 1})
			if errKey != "" {
				t.Fatalf("unexpected error key %q", errKey)
			}
			if !strings.Contains(user, tc.want) {
				t.Fatalf("prompt missing %q:\n%s", tc.want, user)
			}
			header := "[AUTHOR TEXT]"
			if tc.lang != "en" {
				header = "[TEXTO DEL AUTOR]"
			}
			if !strings.Contains(user, header+"\nthe sample text") {
				t.Fatalf("author text block missing, got:\n%s", user)
			}
		})
	}
}

func TestBrainstormPreservesVoiceAndLanguage(t *testing.T) {
	h := brainstormTestHandler("en")
	system, _, _ := h.brainstormPrompts(brainstormRequest{Action: "rewrite", Mode: "shorter", Text: "hola"})
	for _, want := range []string{"narrative person", "tense", "language of the AUTHOR TEXT", "Never switch POV"} {
		if !strings.Contains(system, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, system)
		}
	}
}

func TestBrainstormVariantsAndCustomInstruction(t *testing.T) {
	h := brainstormTestHandler("en")
	_, one, _ := h.brainstormPrompts(brainstormRequest{Action: "rewrite", Mode: "shorter", Text: "t", Variant: 1})
	_, three, _ := h.brainstormPrompts(brainstormRequest{Action: "rewrite", Mode: "shorter", Text: "t", Variant: 3})
	if one == three || !strings.Contains(three, "bolder variation") {
		t.Fatalf("variants must differ:\n1: %s\n3: %s", one, three)
	}
	_, custom, _ := h.brainstormPrompts(brainstormRequest{Action: "rewrite", Text: "t", Instruction: "make it sarcastic"})
	if !strings.Contains(custom, "make it sarcastic") {
		t.Fatalf("custom rewrite instruction missing:\n%s", custom)
	}
}

func TestBrainstormExplainSchema(t *testing.T) {
	h := brainstormTestHandler("en")
	_, user, _ := h.brainstormPrompts(brainstormRequest{Action: "explain", Text: "cynical"})
	for _, want := range []string{"synonyms", "antonyms", "semantic_field", "tone", "readings"} {
		if !strings.Contains(user, want) {
			t.Fatalf("explain prompt missing %q:\n%s", want, user)
		}
	}
}

// A phrase must not be treated as a dictionary entry, and the answer language
// is taken from the author text, not from the prompt language.
func TestBrainstormExplainSingleWordAndLanguage(t *testing.T) {
	h := brainstormTestHandler("en")
	_, word, _ := h.brainstormPrompts(brainstormRequest{Action: "explain", Text: "cynical"})
	if !strings.Contains(word, "SINGLE WORD") || strings.Contains(word, "not a single word") {
		t.Fatalf("single word prompt wrong:\n%s", word)
	}
	_, phrase, _ := h.brainstormPrompts(brainstormRequest{Action: "explain", Text: "no me digas eso"})
	if !strings.Contains(phrase, "not a single word") {
		t.Fatalf("phrase prompt must skip dictionary fields:\n%s", phrase)
	}
	if !strings.Contains(phrase, "ANSWER LANGUAGE") {
		t.Fatalf("explain prompt must pin the answer language:\n%s", phrase)
	}
	// The reminder must come after the author text, where recency helps.
	if !strings.HasSuffix(strings.TrimSpace(phrase), "Spanish text → Spanish answer; English text → English answer).") {
		t.Fatalf("explain prompt must end with the language reminder:\n%s", phrase)
	}
}

func TestBrainstormCommuneNeedsEntity(t *testing.T) {
	h := brainstormTestHandler("en")
	_, _, errKey := h.brainstormPrompts(brainstormRequest{Action: "commune", Text: "hello", EntityKind: "character", EntityID: "missing"})
	if errKey != "brainstorm_entity_not_found" {
		t.Fatalf("expected missing-entity error, got %q", errKey)
	}
}

func TestBrainstormFinalStyleLock(t *testing.T) {
	h := brainstormTestHandler("en")
	_, describe, _ := h.brainstormPrompts(brainstormRequest{Action: "describe", Mode: "smell", Text: "the kitchen"})
	if !strings.Contains(describe, "[FINAL RULES]") {
		t.Fatalf("describe prompt must end with the style lock:\n%s", describe)
	}
	_, explain, _ := h.brainstormPrompts(brainstormRequest{Action: "explain", Text: "kitchen"})
	if strings.Contains(explain, "[FINAL RULES]") {
		t.Fatalf("explain must keep its JSON language reminder instead:\n%s", explain)
	}
}

func TestExplanationPreview(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"explanation":"A cozy room`, "A cozy room"},
		{`{"explanation":"Line one\nLine two"}`, "Line one\nLine two"},
		{`{"definition":"x","explanation":"late"}`, "late"},
		{`{"explanation":`, ""},
		{`no json here`, ""},
	}
	for _, tc := range cases {
		if got := explanationPreview(tc.in); got != tc.want {
			t.Fatalf("explanationPreview(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
