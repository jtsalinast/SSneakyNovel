package agent

import (
	"testing"

	"showmethestory/internal/config"
)

func TestParseGenerateSectionTagSmoke(t *testing.T) {
	if tc := parseGenerateSectionTag("[generate-section:characters] go"); tc == nil || tc.Name != "generate_section" {
		t.Fatal("valid tag not parsed")
	}
	if tc := parseGenerateSectionTag("[generate-section:bogus] go"); tc != nil {
		t.Fatal("invalid section accepted")
	}
	if tc := parseGenerateSectionTag("plain message"); tc != nil {
		t.Fatal("plain message parsed")
	}
}

func TestCfgGetterPrecedence(t *testing.T) {
	snap := &config.Config{Language: "zh"}
	live := &config.Config{Language: "en"}
	ctx := &AgentContext{Config: snap, ConfigGetter: func() *config.Config { return live }}
	if ctx.cfg() != live {
		t.Fatal("getter should take precedence")
	}
	ctx2 := &AgentContext{Config: snap}
	if ctx2.cfg() != snap {
		t.Fatal("fallback to snapshot")
	}
	var nilCtx *AgentContext
	if nilCtx.cfg() != nil {
		t.Fatal("nil-safe")
	}
}
