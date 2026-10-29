package httpapi

import (
	"strings"
	"testing"
)

func TestBrainstormPrompt(t *testing.T) {
	cases := []struct {
		name, lang, action, mode, want string
	}{
		{"alternatives", "en", "alternatives", "", "3 clearly distinct alternatives"},
		{"rewrite mode", "en", "rewrite", "show", "Show, don't tell"},
		{"rewrite default mode", "en", "rewrite", "", "keeping the meaning and voice"},
		{"describe sense", "en", "describe", "smell", `sense of "smell"`},
		{"feedback dimension", "en", "feedback", "pacing", `feedback notes on "pacing"`},
		{"expand", "en", "expand", "", "roughly twice its length"},
		{"unknown action falls back to rewrite", "en", "bogus", "", "only the rewritten text"},
		{"spanish", "es", "describe", "taste", "sentido «gusto»"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, user := brainstormPrompt(tc.lang, tc.action, tc.mode, false, "the sample text")
			if !strings.Contains(user, tc.want) {
				t.Fatalf("prompt missing %q:\n%s", tc.want, user)
			}
			if tc.lang == "en" && (!strings.Contains(user, "narrative person") || !strings.Contains(user, "tense")) {
				t.Fatalf("prompt must preserve person and tense:\n%s", user)
			}
			if !strings.HasSuffix(user, "the sample text") {
				t.Fatalf("author text must be last, got:\n%s", user)
			}
		})
	}
}

func TestBrainstormMoreAndOptions(t *testing.T) {
	_, user := brainstormPrompt("en", "rewrite", "shorter", true, "the sample text")
	if !strings.Contains(user, "follow-up options") {
		t.Fatalf("more request missing follow-up clause:\n%s", user)
	}
	options := splitBrainstormOptions("one\n===\n two \n\n====\nthree")
	if len(options) != 3 || options[0] != "one" || options[1] != "two" || options[2] != "three" {
		t.Fatalf("unexpected split: %#v", options)
	}
	if got := splitBrainstormOptions("single answer"); len(got) != 1 || got[0] != "single answer" {
		t.Fatalf("undelimited answer must stay one option: %#v", got)
	}
}
