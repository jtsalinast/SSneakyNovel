package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"showmethestory/internal/config"
	"showmethestory/internal/fsutil"
	"showmethestory/internal/i18n"
	"showmethestory/internal/llm"
	"showmethestory/internal/sse"
	"showmethestory/internal/story"
	"strings"
)

// SectionGenRequest carries everything one config-section generation needs
// (style, characters, organizations, relations, locations, worldview, motif,
// story_idea, audience_profile, inspirational_pieces). It is shared by the HTTP generate endpoint
// (config-page buttons) and the agent's generate_section chat tool so both
// paths have identical semantics. The agent package owns the type to avoid an
// import cycle with httpapi; httpapi aliases it.
type SectionGenRequest struct {
	Section     string // style|characters|organizations|relations|locations|worldview|motif|story_idea|audience_profile|inspirational_pieces
	StoryIdea   string
	Story       *config.StoryConfig // live (possibly unsaved) form values; nil = use saved config
	Name        string              // worldview entry name
	Category    string              // worldview category
	EntryID     string              // worldview: fill an existing entry by id
	SourceID    string              // relations pair mode
	TargetID    string              // relations pair mode
	CharacterID string              // single-character form mode
	OrgID       string              // single-organization form mode
	// WorldviewCategory optionally overrides the category derived from a
	// per-category worldview section key (wv_geography, ...). The HTTP layer
	// accepts it in the request body; empty means derive it from Section.
	WorldviewCategory string          // explicit worldview category override
	Ctx               context.Context // optional parent ctx for the generation (chat tool passes a detached one)
}

type Tool struct {
	Name        string
	Description string
	Parameters  string
	Execute     func(args json.RawMessage, ctx *AgentContext) (string, error)
}

type AgentContext struct {
	APICfg       *config.APIConfig
	Settings     *story.ProjectSettings
	SettingsPath string
	State        *story.Progress
	// Config is the live in-memory project config. Tests and embedded callers
	// set it directly; httpapi prefers ConfigGetter (below) so every read picks
	// up exactly what the author just saved through the form.
	Config *config.Config
	// ConfigGetter, when non-nil, supersedes Config: each cfg() call returns
	// the freshest on-disk config (falling back to the snapshot). This is what
	// makes the chat-side generate_section tool see the unsaved-but-PUT form
	// values the config-page buttons persist right before messaging the chat.
	ConfigGetter func() *config.Config
	Skills       []story.Skill
	Logger       *sse.LogBroadcaster
	ContextPage  string
	ProgressPath string
	CfgPath      string
	SessionsDir  string
	ProjectDir   string
	StartAsync   func(taskName string, fn func(goCtx context.Context) error)
	// StartSectionGenerate runs one config-section generation as a background
	// task (same engine as POST /api/generate/{section}). Wired by httpapi to
	// Handlers.StartSectionGenerateAsync with ownsLock=false. May be nil in
	// tests/embedded contexts; the generate_section tool reports that clearly.
	StartSectionGenerate func(req SectionGenRequest) error
	// StartPrePlanGenerate runs the book-level pre-planning generation as a
	// detached background task (same engine as POST /api/outline/preplan/generate).
	// Wired by httpapi to Handlers.StartDetachedPrePlanGenerate. May be nil in
	// tests/embedded contexts; the generate_section tool reports that clearly.
	StartPrePlanGenerate func() error
	// SectionGenCtx, when set, is the context handed to StartSectionGenerate
	// so the child generation keeps running even after this agent turn's task
	// lock is released (prevents "context canceled" mid-generation).
	SectionGenCtx context.Context
	toolMsgKey    string
	toolMsgArgs   []string
}

type AgentStep struct {
	Role           string
	Content        string
	ToolCall       *ToolCall
	ToolResult     string
	ToolResultKey  string
	ToolResultArgs []string
}

// ToolCall lives in story because persisted chat messages embed it.
type ToolCall = story.ToolCall

func RunAgentLoop(goCtx context.Context, ctx *AgentContext, userMessage string, history []AgentStep, maxSteps int) (string, []AgentStep, error) {
	tools := getBuiltinTools()
	toolDesc := buildToolDescriptions(tools)

	systemPrompt := buildAgentSystemPrompt(ctx, toolDesc)

	var messages []llm.Message
	messages = append(messages, llm.Message{Role: "system", Content: systemPrompt})

	toolResultLabel := "[Resultado de herramienta]"
	if projectLang(ctx) == i18n.LangEN {
		toolResultLabel = "[Tool result]"
	}

	// The newest user turn is carried by userMessage itself; the history slice
	// (which httpapi persists with the last user message appended at the end)
	// must not re-emit it, or the prompt would contain the same request twice.
	lastUserIdx := -1
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" {
			lastUserIdx = i
			break
		}
	}

	for i, step := range history {
		if step.Role == "user" {
			if i == lastUserIdx {
				continue
			}
			messages = append(messages, llm.Message{Role: "user", Content: step.Content})
		} else if step.Role == "assistant" {
			if step.ToolCall != nil {
				tcJSON, _ := json.Marshal(step.ToolCall)
				messages = append(messages, llm.Message{Role: "assistant", Content: fmt.Sprintf("<tool_call>\n%s\n</tool_call>", string(tcJSON))})
			} else {
				messages = append(messages, llm.Message{Role: "assistant", Content: step.Content})
			}
		} else if step.Role == "tool" {
			messages = append(messages, llm.Message{Role: "user", Content: fmt.Sprintf("%s\n%s", toolResultLabel, step.ToolResult)})
		}
	}

	messages = append(messages, llm.Message{Role: "user", Content: userMessage})

	// The Generate buttons prepend [generate-section:X]. The chat agent still
	// owns execution; the tag only makes the tool choice deterministic: on the
	// first step we skip the model round-trip entirely and execute the tool
	// ourselves, so a click ALWAYS triggers the generation even when the LLM
	// endpoint is down or answers with plain text. Later steps are normal
	// model turns that summarize the tool result for the user.
	forcedCall := parseGenerateSectionTag(userMessage)
	// Visible reply guaranteed for tagged turns even when the LLM never
	// answers (endpoint down / truncated): used if no later step returns prose.
	fallbackReply := ""

	// ponytail: one parse-retry per loop; ceiling = still-broken after retry → hard error (no silent final reply).
	parseRetryUsed := false

	for step := 0; step < maxSteps; step++ {
		if goCtx.Err() != nil {
			return "", history, agentErr(ctx, "agent.task_cancelled")
		}

		if forcedCall != nil && step == 0 {
			tcJSON, _ := json.Marshal(forcedCall)
			history = append(history, AgentStep{Role: "assistant", ToolCall: forcedCall})
			if ctx.Logger != nil {
				ctx.logger().ToolCallStart("", forcedCall.Name, string(tcJSON))
			}
			result, resultKey, resultArgs := executeTool(forcedCall, tools, ctx)
			history = append(history, AgentStep{
				Role:           "tool",
				ToolResult:     result,
				ToolResultKey:  resultKey,
				ToolResultArgs: resultArgs,
			})
			if ctx.Logger != nil {
				ctx.logger().ToolCallEnd("", forcedCall.Name, story.Truncate(result, 200), resultKey, resultArgs)
			}
			messages = append(messages, llm.Message{Role: "assistant", Content: fmt.Sprintf("<tool_call>\n%s\n</tool_call>", string(tcJSON))})
			messages = append(messages, llm.Message{Role: "user", Content: fmt.Sprintf("%s\n%s", toolResultLabel, result)})
			// The click ALWAYS produces a visible chat reply, even when the
			// summary LLM call below fails or loops out: this becomes the final
			// assistant message unless a later step returns prose first.
			fallbackReply = agentMsg(ctx, "agent.section_generate_started", forcedSectionName(forcedCall))
			continue
		}

		if ctx.Logger != nil {
			var roleSeq []string
			for _, m := range messages {
				roleSeq = append(roleSeq, fmt.Sprintf("%s(%d)", m.Role, len([]rune(m.Content))))
			}
			ctx.logger().Info(fmt.Sprintf("[Agent] paso %d/%d: %d mensajes: %v", step+1, maxSteps, len(messages), roleSeq))
		}

		fullResp := ""
		finishReason, err := callAgentAPI(goCtx, ctx.APICfg, messages, func(chunk string) {
			fullResp += chunk
		})
		if err != nil {
			if ctx.Logger != nil {
				ctx.logger().Error(fmt.Sprintf("[Agent] paso %d: fallo la llamada a la API: %v", step+1, err))
			}
			if forcedCall != nil {
				// Tagged turn: the generate_section tool already ran. Give the
				// summary LLM one more chance on the next step; if every model
				// call fails, fall through to the deterministic reply below so
				// the click still shows an assistant message in the chat.
				if fallbackReply != "" && step >= maxSteps-1 {
					break
				}
				continue
			}
			return "", history, agentErr(ctx, "agent.api_failed", err)
		}

		if ctx.Logger != nil {
			ctx.logger().Info(fmt.Sprintf("[Agent] step %d: API response %d chars (finish_reason=%s)", step+1, len(fullResp), finishReason))
		}

		toolCall := parseToolCall(fullResp)

		if forcedCall != nil && toolCall == nil {
			// Model answered plain text after the forced tool ran (or an odd
			// retry): keep its prose as the visible reply instead of looping.
			history = append(history, AgentStep{Role: "assistant", Content: fullResp})
			return fullResp, history, nil
		}

		if isFailedToolCallAttempt(fullResp, toolCall) {
			if !parseRetryUsed {
				parseRetryUsed = true
				feedback := toolCallParseRetryFeedback(ctx, finishReason, fullResp)
				if ctx.Logger != nil {
					ctx.logger().WarnKey("log.agent_tool_call_parse_retry", finishReason, len(fullResp))
				}
				// Keep broken output in messages only (not history) so the model can diagnose; UI/session stay clean.
				messages = append(messages, llm.Message{Role: "assistant", Content: fullResp})
				messages = append(messages, llm.Message{Role: "user", Content: feedback})
				continue
			}
			if ctx.Logger != nil {
				if finishReason == "length" || hasUnclosedToolCall(fullResp) {
					ctx.logger().WarnKey("log.agent_output_truncated", agentEffectiveMaxTokens(ctx.APICfg))
				} else {
					ctx.logger().WarnKey("log.agent_tool_call_parse_failed")
				}
			}
			if finishReason == "length" || hasUnclosedToolCall(fullResp) {
				return "", history, agentErr(ctx, "agent.output_truncated", agentEffectiveMaxTokens(ctx.APICfg))
			}
			return "", history, agentErr(ctx, "agent.tool_call_parse_failed")
		}

		if toolCall == nil {
			if ctx.Logger != nil {
				preview := fullResp
				if len([]rune(preview)) > 200 {
					preview = string([]rune(preview)[:200]) + "..."
				}
				ctx.logger().Info(fmt.Sprintf("[Agent] paso %d: no se detectó uso de herramientas; devolviendo texto final. Vista previa: %s", step+1, preview))
			}
			history = append(history, AgentStep{Role: "assistant", Content: fullResp})
			return fullResp, history, nil
		}

		if ctx.Logger != nil {
			ctx.logger().Info(fmt.Sprintf("[Agent] paso %d: se detectó uso de herramienta → %s", step+1, toolCall.Name))
		}

		// Para no duplicar el texto en la UI, el historial conserva solo el
		// contenido visible: se retira la etiqueta <tool_call> y se mantiene
		// únicamente el uso de herramienta parseado (m.tool_calls y m.content).
		strippedContent := stripToolCallTags(fullResp)
		history = append(history, AgentStep{Role: "assistant", Content: strippedContent, ToolCall: toolCall})

		if ctx.Logger != nil {
			ctx.logger().ToolCallStart("", toolCall.Name, string(toolCall.Arguments))
		}

		result, resultKey, resultArgs := executeTool(toolCall, tools, ctx)

		if ctx.Logger != nil {
			resultPreview := result
			if len([]rune(resultPreview)) > 100 {
				resultPreview = string([]rune(resultPreview)[:100]) + "..."
			}
			ctx.logger().Info(fmt.Sprintf("[Agent] %d: tool %s finished, result: %s", step+1, toolCall.Name, resultPreview))
		}

		history = append(history, AgentStep{
			Role:           "tool",
			ToolResult:     result,
			ToolResultKey:  resultKey,
			ToolResultArgs: resultArgs,
		})

		if ctx.Logger != nil {
			ctx.logger().ToolCallEnd("", toolCall.Name, story.Truncate(result, 200), resultKey, resultArgs)
		}

		messages = append(messages, llm.Message{Role: "assistant", Content: fmt.Sprintf("<tool_call>\n%s\n</tool_call>", func() string {
			tcJSON, _ := json.Marshal(toolCall)
			return string(tcJSON)
		}())})
		messages = append(messages, llm.Message{Role: "user", Content: fmt.Sprintf("%s\n%s", toolResultLabel, result)})
	}

	if fallbackReply != "" {
		// Tagged turn whose summary LLM never delivered prose (API down or
		// step ceiling): the tool DID run, so answer deterministically.
		history = append(history, AgentStep{Role: "assistant", Content: fallbackReply})
		return fallbackReply, history, nil
	}
	return agentMsg(ctx, "agent.max_steps"), history, nil
}

// generateSectionValid is the set of section keys accepted by the
// generate_section tool and its background runner.
var generateSectionValid = map[string]bool{
	"style": true, "characters": true, "organizations": true,
	"relations": true, "locations": true, "worldview": true,
	"wv_geography": true, "wv_location": true, "wv_faction": true, "wv_rule": true,
	"wv_knowledge": true, "wv_history": true, "wv_other": true,
	"motif": true, "story_idea": true, "audience_profile": true, "inspirational_pieces": true,
	"pre_planning": true,
}

// worldviewCategoryBySection maps the per-category worldview section keys
// (wv_geography, wv_faction, ...) to their worldview category. The Worldview
// card in the config page shows one section per category, each with its own
// Generate button; the batch generation across ALL categories was replaced by
// these targeted sections so the LLM only produces what the author actually
// needs for that category. Exported so the HTTP layer can validate routes and
// build section labels for these keys.
var worldviewCategoryBySection = map[string]string{
	"wv_geography": "geography",
	"wv_location":  "location",
	"wv_faction":   "faction",
	"wv_rule":      "rule",
	"wv_knowledge": "knowledge",
	"wv_history":   "history",
	"wv_other":     "other",
}

// WorldviewCategoryBySection is the exported accessor of
// worldviewCategoryBySection, used by the HTTP layer (route validation,
// section labels and the per-category worldview generator).
func WorldviewCategoryBySection() map[string]string {
	return worldviewCategoryBySection
}

// IsWorldviewCategorySection reports whether section is a per-category
// worldview key (wv_geography, wv_faction, ...).
func IsWorldviewCategorySection(section string) bool {
	_, ok := worldviewCategoryBySection[section]
	return ok
}

// parseGenerateSectionTag looks for the [generate-section:<section>] marker
// that the config-page Generate buttons prepend to their chat message (see the
// config.generate.chat.* i18n keys). When present and the section key is one
// the generate_section tool accepts, it returns a ready-to-execute ToolCall so
// the agent loop can force the call deterministically instead of relying on the
// model to emit the tool-call block itself. Returns nil when no valid tag is
// found, leaving normal model-driven behavior untouched.
// ParseGenerateSectionTag is the exported wrapper used by the HTTP layer to
// run the deterministic Generate-button fast path before the LLM turn.
func ParseGenerateSectionTag(userMessage string) *ToolCall {
	return parseGenerateSectionTag(userMessage)
}

// ForcedSectionName is the exported wrapper around forcedSectionName.
func ForcedSectionName(call *ToolCall) string {
	return forcedSectionName(call)
}

func parseGenerateSectionTag(userMessage string) *ToolCall {
	const marker = "[generate-section:"
	idx := strings.Index(userMessage, marker)
	if idx < 0 {
		return nil
	}
	rest := userMessage[idx+len(marker):]
	end := strings.Index(rest, "]")
	if end < 0 {
		return nil
	}
	section := strings.TrimSpace(rest[:end])
	if !generateSectionValid[section] {
		return nil
	}
	args, err := json.Marshal(map[string]string{"section": section})
	if err != nil {
		return nil
	}
	return &ToolCall{Name: "generate_section", Arguments: args}
}

// forcedSectionName extracts the section argument from a forced
// generate_section ToolCall (used to build the deterministic chat reply).
func forcedSectionName(call *ToolCall) string {
	var params struct {
		Section string `json:"section"`
	}
	if call != nil {
		_ = json.Unmarshal(call.Arguments, &params)
	}
	return params.Section
}

func callAgentAPI(ctx context.Context, apiCfg *config.APIConfig, messages []llm.Message, onChunk func(string)) (finishReason string, err error) {
	// The agent must emit tool calls as complete JSON; short caps truncate the
	// <tool_call> block mid-stream, so enforce a minimum max_tokens here.
	agentCfg := *apiCfg
	agentCfg.MaxTokens = agentEffectiveMaxTokens(apiCfg)

	result, err := llm.CallAPIStreamMessages(ctx, &agentCfg, messages, onChunk)
	if err == nil {
		return result.FinishReason, nil
	}
	// Never double-call after a partial stream: chunks were already forwarded to
	// the caller and appending a second reply would corrupt the message.
	if ctx.Err() != nil || result.Content != "" {
		return "", err
	}
	// No synchronous fallback here: CallAPIStreamMessages already parses
	// non-SSE JSON responses in-place, and re-sending the whole prompt used to
	// double the request load (and re-trigger generation side effects) during
	// error storms. Fail fast; the loop retries or falls back deterministically.
	return "", err
}

// agentEffectiveMaxTokens returns the max_tokens floor used for Agent API calls.
func agentEffectiveMaxTokens(apiCfg *config.APIConfig) int {
	if apiCfg == nil || apiCfg.MaxTokens < 8192 {
		return 8192
	}
	return apiCfg.MaxTokens
}

// hasUnclosedToolCall reports whether content has <tool_call> without a matching close tag.
func hasUnclosedToolCall(content string) bool {
	idx := strings.Index(content, "<tool_call>")
	if idx == -1 {
		return false
	}
	after := content[idx+len("<tool_call>"):]
	return !strings.Contains(after, "</tool_call>")
}

// isFailedToolCallAttempt reports a tool-call shaped reply that did not parse.
// Covers unclosed tags and closed-but-invalid JSON; excludes ordinary final text replies.
func isFailedToolCallAttempt(content string, tc *ToolCall) bool {
	if !strings.Contains(content, "<tool_call>") {
		return false
	}
	// Unclosed tag => the stream was truncated. Even when lenient JSON repair
	// recovered a tool name, its arguments may be silently cut off, so the
	// attempt must fail and be retried instead of executing partial args.
	if tc == nil || !strings.Contains(content[strings.Index(content, "<tool_call>")+len("<tool_call>"):], "</tool_call>") {
		return true
	}
	return false
}

// isAgentOutputTruncated detects max_tokens truncation that would make tool-call parsing unsafe.
// Used after the one-shot parse retry is exhausted. ponytail: no JSON repair / silent partial args.
func isAgentOutputTruncated(finishReason, content string, tc *ToolCall) bool {
	if finishReason != "length" {
		return false
	}
	if !strings.Contains(content, "<tool_call>") {
		return false
	}
	return hasUnclosedToolCall(content) || tc == nil
}

// toolCallParseRetryFeedback asks the model to diagnose a bad tool_call and emit one complete retry.
func toolCallParseRetryFeedback(ctx *AgentContext, finishReason, content string) string {
	lang := projectLang(ctx)
	reason := "parse_error"
	if finishReason == "length" || hasUnclosedToolCall(content) {
		reason = "truncated_or_unclosed"
	}
	return i18n.T(lang, "agent.tool_call_parse_retry_hint", reason, agentEffectiveMaxTokens(ctx.APICfg))
}

func buildAgentSystemPrompt(ctx *AgentContext, toolDesc string) string {
	if projectLang(ctx) == i18n.LangEN {
		return buildAgentSystemPromptEN(ctx, toolDesc)
	}
	return buildAgentSystemPromptEN(ctx, toolDesc)
}

// calcSynopsisLengthRange returns recommended synopsis length bounds (characters)
// scaled to planned total book length. ponytail: linear heuristic; tune divisors if field feels too short/long in practice.
func calcSynopsisLengthRange(chapterCount, targetWordsPerChapter int) (minLen, maxLen int) {
	if chapterCount < 1 {
		chapterCount = 1
	}
	if targetWordsPerChapter < 1 {
		targetWordsPerChapter = 2500
	}
	total := chapterCount * targetWordsPerChapter
	minLen = total / 80
	if minLen < 300 {
		minLen = 300
	}
	maxLen = total / 35
	if maxLen < 600 {
		maxLen = 600
	}
	if maxLen > 5000 {
		maxLen = 5000
	}
	if minLen > maxLen {
		minLen = maxLen * 2 / 3
	}
	return minLen, maxLen
}

func buildAgentSystemPromptEN(ctx *AgentContext, toolDesc string) string {
	var sb strings.Builder
	sb.WriteString("ending_intent is serial/final/sequel; ending_style is closed/open/custom, with ending_requirements required for custom. Ask the author before continuing past a planned ending and setting confirm_continue=true. Before editing prose, use read_chapter for facts and content_rev; disclose affected facts and linked chapters and obtain author consent before setting confirm_fact_impact=true. Accepted chapters automatically sync settings; failed sync can be retried from Writing.\n")
	sb.WriteString("Plan outlines in batches. generate_outline requires chapter_count (1–36) and outline_synopsis. Default to append; replace_last can only replan the last entirely unwritten batch. Read batch IDs with read_outline. Do not store a whole-book synopsis in config.\n")
	sb.WriteString("You are a novel-writing assistant in full charge of every operation on the project: generating/revising/confirming outlines, generating/revising/confirming chapters, managing characters/worldview/organisations/relations/foreshadows, skill management, project configuration, and so on. Reply to the user in English.\n\n")

	sb.WriteString("## Project info\n")
	if ctx.State.Title != "" {
		sb.WriteString(fmt.Sprintf("Novel title: \"%s\"\n", ctx.State.Title))
	}
	sb.WriteString(fmt.Sprintf("Current phase: %s\n", ctx.State.Phase))
	sb.WriteString(fmt.Sprintf("Current outline chapter count: %d\n", len(ctx.State.Chapters)))
	cfg := ctx.safeCfg()
	sb.WriteString(fmt.Sprintf("Target words per chapter: %d\n", cfg.Story.TargetWordsPerChapter))
	totalWords := len(ctx.State.Chapters) * cfg.Story.TargetWordsPerChapter
	sb.WriteString(fmt.Sprintf("Estimated length of existing outlined chapters: ~%d words\n", totalWords))

	if storyFields := story.FormatStoryConfigForPrompt(cfg.Story, i18n.LangEN); storyFields != "" {
		sb.WriteString("\n### Story parameters (current values from the config page; quote them directly; change them only via update_project_config)\n")
		sb.WriteString(storyFields)
	}

	if ctx.Settings != nil {
		sb.WriteString(fmt.Sprintf("Characters: %d\n", len(ctx.Settings.Characters)))
		sb.WriteString(fmt.Sprintf("Worldview entries: %d\n", len(ctx.Settings.Worldview)))
		sb.WriteString(fmt.Sprintf("Organisations: %d\n", len(ctx.Settings.Organizations)))
	}

	if ctx.ContextPage != "" {
		pageNames := map[string]string{
			"config":        "Config",
			"novel-params":  "Novel Parameters",
			"theme":         "Theme & Motif",
			"style":         "Writing Style & POV",
			"lore":          "Lore",
			"characters":    "Characters",
			"worldview":     "Worldview",
			"organizations": "Organizations",
			"relations":     "Relations",
			"foreshadows":   "Foreshadows",
			"memory":        "Memory",
			"proofread":     "Proofread",
			"outline":       "Outline",
			"writing":       "Writing",
			"skills":        "Skills",
		}
		if name, ok := pageNames[ctx.ContextPage]; ok {
			sb.WriteString(fmt.Sprintf("\nThe user is currently viewing the \"%s\" page.\n", name))
		}
	}

	if frontierInfo := story.FormatWritingFrontierInfo(ctx.State, i18n.LangEN); frontierInfo != "" {
		sb.WriteString("\n")
		sb.WriteString(frontierInfo)
	}

	sb.WriteString("\n")

	enabledSkills := story.ResolveSkills(ctx.Skills, cfg.SkillConfig, story.SkillScopeAssistantChat, cfg.Language)
	if len(enabledSkills) > 0 {
		sb.WriteString("## Enabled skills\n")
		sb.WriteString(story.FormatSkillsContent(enabledSkills))
		sb.WriteString("\n")
	}

	sb.WriteString("## Available tools\n")
	sb.WriteString(toolDesc)
	sb.WriteString("\n\n")

	sb.WriteString("## Tool-call format\n")
	sb.WriteString("When you need to call a tool, use this exact format. The payload must be valid JSON; do not wrap arguments in XML tags:\n")
	sb.WriteString("<tool_call>\n")
	sb.WriteString(`{"name": "tool_name", "arguments": {"arg_name": "arg_value"}}`)
	sb.WriteString("\n</tool_call>\n\n")
	sb.WriteString("Correct example:\n")
	sb.WriteString("<tool_call>\n")
	sb.WriteString(`{"name": "search_project", "arguments": {"query": "character"}}`)
	sb.WriteString("\n</tool_call>\n\n")
	sb.WriteString("Incorrect examples (do NOT do this):\n")
	sb.WriteString("- Do not use XML tags such as <arguments> inside <tool_call>\n")
	sb.WriteString("- Do not write tool-call JSON outside the <tool_call> tag\n")
	sb.WriteString("- Do not emit multiple <tool_call> tags at once\n")
	sb.WriteString("Call one tool at a time. Wait for the tool result before continuing.\n")
	sb.WriteString("If no tool is needed, just reply directly to the user.\n\n")

	sb.WriteString("## Safety rules (highest priority — violating them causes permanent user-data loss)\n")
	sb.WriteString("1. **Edit != Delete**. When the user asks to \"revise/adjust/polish/fix chapter N\", you MUST use the revise_chapter tool (pass the chapter number via the num argument). NEVER use delete_chapter / delete_chapters_from / delete_outline / reset_progress to satisfy any kind of \"edit\" request.\n")
	sb.WriteString("2. revise_chapter can revise any chapter that has content (including confirmed early chapters); it only modifies the target chapter and never touches the others. To tweak chapter 6, call revise_chapter(num=6, feedback=specific instructions). That's it.\n")
	sb.WriteString("3. Delete tools (delete_chapter, delete_chapters_from, delete_outline, reset_progress) are irreversible. Only use them when the user explicitly says \"delete/clear/reset\" and specifies the range. Before using one, first reply in plain text restating the exact range that will be affected (e.g. \"will clear content of chapters 6-30, 25 chapters — outlines will be preserved\") and wait for the user's explicit confirmation; then on the next turn call the tool with confirm=true. Note: delete_chapter / delete_chapters_from only clear content (Content, Summary, markdown file), keeping outline entries and **do not reduce the total chapter count**; delete_outline / reset_progress delete outlines and all data.\n")
	sb.WriteString("5. When user intent is ambiguous, ask a clarifying question instead of guessing into a write operation.\n\n")

	sb.WriteString("## Tool-selection guidance\n")
	sb.WriteString("- Tweak chapter content -> revise_chapter(num, feedback) (AI rewrites the whole chapter)\n")
	sb.WriteString("- Surgical edit of a chapter (replace lines/replace text/insert/append) -> edit_chapter_content(num, operation, ...) (precise edit without full rewrite; ideal for tweaking a paragraph or fixing a typo)\n")
	sb.WriteString("- Edit a chapter outline (pending / writing / review; not accepted) -> edit_chapter_outline(num, title, outline)\n")
	sb.WriteString("- Give feedback on the existing outline while **keeping the same chapter count** -> revise_outline(feedback) (updates title/outline of unconfirmed chapters only; cannot add or remove chapters)\n")
	sb.WriteString("- Delete prose at the writing frontier (chapter in review, or last accepted while the next chapter has not started) -> delete_chapter. Check \"delete_chapter can remove\" in project info; **never** use delete_chapters_from for this\n")
	sb.WriteString("- Delete an earlier chapter and all prose after it -> delete_chapters_from(num) (clears chapter num through the last outline slot; restate the range and get confirmation). If the user only wants the frontier chapter removed, use delete_chapter\n")
	sb.WriteString("- Generate the next chapter's prose -> generate_chapter\n")

	sb.WriteString("## Important rules\n")
	sb.WriteString("- Async tools (generate_outline, generate_chapter, etc.) return \"task started\" immediately; results are pushed to the UI via logs. You MUST call the tool first and tell the user it has started only after receiving the tool result. Never output \"please wait\", \"hold on\", \"generating now\", or similar text without actually calling a tool — if you cannot fulfil the request, just explain why.\n")
	sb.WriteString("- When calling a tool, **output NO explanatory text** — emit the <tool_call> tag directly. Explain after you receive the tool result.\n")
	sb.WriteString("- When the user submits a story-config update (e.g. \"please update the following story config\"), use update_project_config.\n")
	sb.WriteString("- When the user submits a writing-style or synopsis update (e.g. \"please update writing style:\" or \"please update synopsis:\"), use update_project_config to save the corresponding field.\n")
	sb.WriteString("- **Config protection**: If a field is already filled in by the user (non-empty), you must NOT overwrite it silently. Explain the diff and your reasoning in chat, wait for explicit user approval, then call update_project_config with confirm_overwrite=true.\n")
	sb.WriteString("- When the user asks you to create/edit characters, worldview, etc., use the corresponding tool directly.\n")
	sb.WriteString("- When the user asks for outline/chapter generation, use the corresponding tool. If async, tell the user to wait.\n")
	sb.WriteString("- When the user asks to generate a config section (writing style, characters, organizations, relations, locations, worldview, theme & motif, story idea, audience profile, inspirational pieces, pre-planning) — including messages triggered by the UI \"Generate\" buttons (e.g. \"please generate characters\", or a message tagged [generate-section:<section>]) — you MUST call the generate_section tool; if the message contains a [generate-section:X] tag, pass X verbatim as the section parameter (style / characters / organizations / relations / locations / worldview / motif / story_idea / audience_profile / inspirational_pieces / pre_planning) without reinterpreting it. It generates in the background using ALL novel parameters and writes the result into the matching fields.\n")
	sb.WriteString("- Before generating the outline, remind the user to check the Config page (story type, writing style, synopsis, characters, worldview) and confirm everything looks right.\n")
	sb.WriteString("- Before kicking off actual writing (confirming the outline), remind the user once more to confirm all settings, including character details and worldview entries.\n")
	sb.WriteString("- Before a write operation, prefer reading first (read_outline, read_chapter, etc.) to confirm the target exists and is in the expected state.\n")
	sb.WriteString("- [Tool result] messages in the conversation are the return values of your own prior tool calls — they represent operations you have already completed. If you have already fixed an issue via a tool call, do NOT re-check the same issue afterward. To verify a fix, rely on the tool's return value directly; do not re-read data to \"confirm\" what you already changed.\n")
	sb.WriteString("- After any operation, briefly report the result and suggest 1-2 next actions (e.g. check character settings, generate the outline, confirm the chapter) to help the user advance the project.\n")

	return sb.String()
}

func buildToolDescriptions(tools []Tool) string {
	var sb strings.Builder
	for _, t := range tools {
		sb.WriteString(fmt.Sprintf("- **%s**: %s\n number: %s\n", t.Name, t.Description, t.Parameters))
	}
	return sb.String()
}

// stripToolCallTags removes <tool_call>...</tool_call> blocks from content,
// leaving only the surrounding text. Prevents duplicate rendering in the
// chat UI (which renders tool calls from both m.tool_calls and m.content).
func stripToolCallTags(content string) string {
	var result strings.Builder
	remaining := content
	for {
		start := strings.Index(remaining, "<tool_call>")
		if start == -1 {
			result.WriteString(remaining)
			break
		}
		result.WriteString(remaining[:start])
		end := strings.Index(remaining[start:], "</tool_call>")
		if end == -1 {
			break
		}
		remaining = remaining[start+end+len("</tool_call>"):]
	}
	return strings.TrimSpace(result.String())
}

func parseToolCall(content string) *ToolCall {
	content = strings.TrimSpace(content)

	idx := strings.Index(content, "<tool_call>")
	if idx == -1 {
		if tc := parseToolCallFunctionName(content); tc != nil {
			return tc
		}
		return parseToolCallJSON(content)
	}

	endIdx := strings.Index(content[idx:], "</tool_call>")
	if endIdx == -1 {
		// metricnot yetjoin: cancanisdisconnect; not inthis JSON, finish_reason==length hour.
		inner := strings.TrimSpace(content[idx+len("<tool_call>"):])
		if tc := parseToolCallFromJSON(inner); tc != nil {
			return tc
		}
		if tc := parseToolCallJSON(inner); tc != nil {
			return tc
		}
		if tc := parseToolCallFunctionName(content); tc != nil {
			return tc
		}
		return parseToolCallJSON(content)
	}

	inner := strings.TrimSpace(content[idx+len("<tool_call>") : idx+endIdx])

	// First try parsing the inner block as JSON.
	if tc := parseToolCallFromJSON(inner); tc != nil {
		return tc
	}

	// Then try the XML format (<name>...</name> + <arguments>...</arguments>).
	if tc := parseToolCallFromXML(inner); tc != nil {
		return tc
	}

	// Last-resort fallback: a JSON record following the closing tag.
	remaining := content[idx+endIdx+len("</tool_call>"):]
	if tc := parseToolCallJSON(remaining); tc != nil {
		return tc
	}

	// most fallback: inpartinsidemiddlecord JSON tooluse
	if tc := parseToolCallJSON(content); tc != nil {
		return tc
	}

	if tc := parseToolCallFunctionName(content); tc != nil {
		return tc
	}

	return nil
}

func parseToolCallFunctionName(content string) *ToolCall {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "function.") {
			continue
		}
		rest := strings.TrimPrefix(line, "function.")
		parenIdx := strings.Index(rest, "(")
		if parenIdx == -1 {
			continue
		}
		name := rest[:parenIdx]
		if name == "" {
			continue
		}
		argsStr := strings.TrimSpace(rest[parenIdx+1:])
		argsStr = strings.TrimSuffix(argsStr, ")")
		argsStr = strings.TrimSpace(argsStr)
		if argsStr == "" {
			argsStr = "{}"
		}
		var args json.RawMessage
		if json.Unmarshal([]byte(argsStr), &args) != nil {
			args = json.RawMessage("{}")
		}
		return &ToolCall{Name: name, Arguments: args}
	}
	return nil
}

func parseToolCallFromXML(inner string) *ToolCall {
	// Parse XML format: <name>tool_name</name><arguments>{json}</arguments>
	nameStart := strings.Index(inner, "<name>")
	nameEnd := strings.Index(inner, "</name>")
	if nameStart == -1 || nameEnd == -1 || nameEnd <= nameStart {
		return nil
	}
	name := strings.TrimSpace(inner[nameStart+len("<name>") : nameEnd])
	if name == "" {
		return nil
	}

	args := json.RawMessage("{}")
	argsStart := strings.Index(inner, "<arguments>")
	argsEnd := strings.Index(inner, "</arguments>")
	if argsStart != -1 && argsEnd != -1 && argsEnd > argsStart {
		argsStr := strings.TrimSpace(inner[argsStart+len("<arguments>") : argsEnd])
		if argsStr != "" {
			var parsed json.RawMessage
			if json.Unmarshal([]byte(argsStr), &parsed) == nil {
				args = parsed
			}
		}
	}

	return &ToolCall{Name: name, Arguments: args}
}

func parseToolCallJSON(content string) *ToolCall {
	// Try all *structurally complete* JSON objects in the content, not just the
	// first one. Deliberately bypasses llm.ExtractJSON's truncation repair: a
	// half-emitted tool call must stay unparsed here so the agent loop can flag
	// it as a failed/truncated attempt and ask for a clean retry.
	remaining := content
	for {
		start := strings.Index(remaining, "{")
		if start == -1 {
			return nil
		}
		end := findBalancedJSONObject(remaining[start:])
		if end == -1 {
			return nil // only truncated JSON left
		}
		jsonStr := remaining[start : start+end]
		remaining = remaining[start+end:]

		if tc := parseToolCallFromJSON(jsonStr); tc != nil {
			return tc
		}
	}
}

// findBalancedJSONObject returns the length (in bytes) of the first balanced
// {...} object in s, skipping braces inside string literals and honoring
// escape sequences. Returns -1 when no complete object exists.
func findBalancedJSONObject(s string) int {
	depth := 0
	inStr := false
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

func parseToolCallFromJSON(jsonStr string) *ToolCall {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil
	}

	nameRaw, ok := raw["name"]
	if !ok {
		nameRaw, ok = raw["tool"]
	}
	if !ok {
		return nil
	}

	var name string
	if err := json.Unmarshal(nameRaw, &name); err != nil {
		return nil
	}

	args, _ := json.Marshal(raw["arguments"])
	if args == nil {
		args = json.RawMessage("{}")
	}

	return &ToolCall{Name: name, Arguments: args}
}

func executeTool(call *ToolCall, tools []Tool, ctx *AgentContext) (string, string, []string) {
	ctx.clearToolMsg()
	lang := projectLang(ctx)
	for _, t := range tools {
		if t.Name == call.Name {
			result, err := t.Execute(call.Arguments, ctx)
			if err != nil {
				return i18n.T(lang, "agent.tool_exec_error", err), "agent.tool_exec_error", i18n.MsgArgs(err)
			}
			key, args := ctx.takeToolMsg()
			return result, key, args
		}
	}
	return i18n.T(lang, "agent.unknown_tool", call.Name), "agent.unknown_tool", i18n.MsgArgs(call.Name)
}

// requireConfirm riskmake confirm number.
// not yethourreturnempty(makefortoolfruitgive AI, want toitsfirst useintent).
func requireConfirm(ctx *AgentContext, args json.RawMessage, action string) string {
	var params struct {
		Confirm bool `json:"confirm"`
	}
	json.Unmarshal(args, &params)
	if params.Confirm {
		return ""
	}
	return agentMsg(ctx, "agent.confirm_required", action)
}

func getBuiltinTools() []Tool {
	return []Tool{
		{
			Name:        "read_characters",
			Description: "List all characters; optional filter by name substring",
			Parameters:  `{"filter": "optional name filter"}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					Filter string `json:"filter"`
				}
				json.Unmarshal(args, &params)

				if ctx.Settings == nil {
					return agentMsg(ctx, "agent.no_characters"), nil
				}

				var result strings.Builder
				for _, c := range ctx.Settings.Characters {
					if params.Filter != "" && !strings.Contains(c.Name, params.Filter) {
						continue
					}
					result.WriteString(fmt.Sprintf("%s (ID:%s)\n", c.Name, c.ID))
					if c.Age != "" {
						result.WriteString(fmt.Sprintf("Age: %s\n", c.Age))
					}
					if c.Personality != "" {
						result.WriteString(fmt.Sprintf("Personality: %s\n", c.Personality))
					}
					if c.Background != "" {
						result.WriteString(fmt.Sprintf("Background: %s\n", c.Background))
					}
					result.WriteString("\n")
				}

				if result.Len() == 0 {
					return agentMsg(ctx, "agent.characters_not_found"), nil
				}
				return result.String(), nil
			},
		},
		{
			Name:        "read_character",
			Description: "Read the full profile of one character by ID or name",
			Parameters:  `{"id": "character ID or name"}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					ID string `json:"id"`
				}
				json.Unmarshal(args, &params)

				if ctx.Settings == nil {
					return agentMsg(ctx, "agent.no_characters"), nil
				}

				for _, c := range ctx.Settings.Characters {
					if c.ID == params.ID || c.Name == params.ID || story.StripNameMarks(c.Name) == params.ID {
						data, _ := json.MarshalIndent(c, "", "  ")
						return string(data), nil
					}
				}
				return agentMsg(ctx, "agent.character_not_found", params.ID), nil
			},
		},
		{
			Name:        "read_worldview",
			Description: "List worldview entries; optionally filter by category",
			Parameters:  `{"category": "optional category filter: geography/faction/rule/history/other"}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					Category string `json:"category"`
				}
				json.Unmarshal(args, &params)

				if ctx.Settings == nil || len(ctx.Settings.Worldview) == 0 {
					return agentMsg(ctx, "agent.no_worldview"), nil
				}

				var result strings.Builder
				for _, w := range ctx.Settings.Worldview {
					if params.Category != "" && w.Category != params.Category {
						continue
					}
					result.WriteString(fmt.Sprintf("%s (%s):\n%s\n\n", w.Name, w.Category, w.Description))
				}

				if result.Len() == 0 {
					return agentMsg(ctx, "agent.worldview_not_found"), nil
				}
				return result.String(), nil
			},
		},
		{
			Name:        "read_organizations",
			Description: "List all organizations and their members",
			Parameters:  `{}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				if ctx.Settings == nil || len(ctx.Settings.Organizations) == 0 {
					return agentMsg(ctx, "agent.no_organizations"), nil
				}

				var result strings.Builder
				for _, o := range ctx.Settings.Organizations {
					result.WriteString(fmt.Sprintf("%s (ID:%s, type:%s)\n%s\n", o.Name, o.ID, o.Type, o.Description))
					if len(o.Members) > 0 {
						result.WriteString(fmt.Sprintf("Member IDs: %s\n", strings.Join(o.Members, ", ")))
					}
					result.WriteString("\n")
				}
				return result.String(), nil
			},
		},
		{
			Name:        "read_chapter",
			Description: "Read a chapter: outline, summary and written content",
			Parameters:  `{"num": 1}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					Num int `json:"num"`
				}
				json.Unmarshal(args, &params)

				for _, ch := range ctx.State.Chapters {
					if ch.Num == params.Num {
						var result strings.Builder
						facts, _ := json.Marshal(story.FactsForChapter(ctx.State, ch.Num, 0))
						fmt.Fprintf(&result, "content_rev: %s\nfacts: %s\n", story.ChapterRevision(ch), facts)
						result.WriteString(fmt.Sprintf("Chapter #%d <%s> [%s]\n\n", ch.Num, ch.Title, ch.Status))
						if ch.Outline != "" {
							result.WriteString(fmt.Sprintf("Outline: %s\n\n", ch.Outline))
						}
						if ch.Summary != "" {
							result.WriteString(fmt.Sprintf("Summary: %s\n\n", ch.Summary))
						}
						if ch.Content != "" {
							result.WriteString(ch.Content)
						} else {
							result.WriteString("(not yet written)")
						}
						return result.String(), nil
					}
				}
				return agentMsg(ctx, "agent.chapter_not_found", params.Num), nil
			},
		},
		{
			Name:        "read_outline",
			Description: "Read the full chapter outline list with statuses",
			Parameters:  `{}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				if len(ctx.State.Chapters) == 0 {
					return agentMsg(ctx, "agent.no_outline"), nil
				}

				var result strings.Builder
				result.WriteString(story.BatchSynopses(ctx.State, ctx.safeCfg().Language))
				result.WriteString(fmt.Sprintf("# %s\n\n", ctx.State.Title))
				for _, ch := range ctx.State.Chapters {
					status := ""
					switch ch.Status {
					case story.StatusAccepted:
						status = "✅"
					case story.StatusReview:
						status = "👀"
					case story.StatusWriting:
						status = "⏳"
					}
					result.WriteString(fmt.Sprintf("Chapter #%d %s<%s>: %s\n", ch.Num, status, ch.Title, ch.Outline))
				}
				return result.String(), nil
			},
		},
		{
			Name:        "read_foreshadows",
			Description: "Read all foreshadowing entries and their status",
			Parameters:  `{}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				if len(ctx.State.Foreshadows) == 0 {
					return agentMsg(ctx, "agent.no_foreshadows"), nil
				}

				var result strings.Builder
				for _, fs := range ctx.State.Foreshadows {
					result.WriteString(fmt.Sprintf("#%d [%s] %s\n", fs.ID, story.ForeshadowStatusLabel(fs.Status), fs.Name))
					result.WriteString(fmt.Sprintf("Description: %s\n", fs.Description))
					result.WriteString(fmt.Sprintf("Planted in: chapter #%d", fs.PlantChapter))
					if fs.TargetChapter > 0 {
						result.WriteString(fmt.Sprintf(" -> planned payoff: chapter #%d", fs.TargetChapter))
					}
					result.WriteString("\n")
					if len(fs.Events) > 0 {
						result.WriteString("Progress:\n")
						for _, ev := range fs.Events {
							result.WriteString(fmt.Sprintf(" - chapter #%d: %s\n", ev.Chapter, ev.Note))
						}
					}
					if fs.Resolution != "" {
						result.WriteString(fmt.Sprintf("Resolution: %s\n", fs.Resolution))
					}
					result.WriteString("\n")
				}
				return result.String(), nil
			},
		},
		{
			Name:        "search_project",
			Description: "Search the project (characters, worldview entries, chapter outlines)",
			Parameters:  `{"query": "search keyword"}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					Query string `json:"query"`
				}
				json.Unmarshal(args, &params)

				if params.Query == "" {
					return agentMsg(ctx, "agent.search_keyword_required"), nil
				}

				var results []string
				q := strings.ToLower(params.Query)

				if ctx.Settings != nil {
					for _, c := range ctx.Settings.Characters {
						if strings.Contains(strings.ToLower(c.Name), q) || strings.Contains(strings.ToLower(c.Background), q) {
							results = append(results, fmt.Sprintf("[character] %s: %s", c.Name, story.Truncate(c.Background, 100)))
						}
					}
					for _, w := range ctx.Settings.Worldview {
						if strings.Contains(strings.ToLower(w.Name), q) || strings.Contains(strings.ToLower(w.Description), q) {
							results = append(results, fmt.Sprintf("[worldview] %s: %s", w.Name, story.Truncate(w.Description, 100)))
						}
					}
				}

				for _, ch := range ctx.State.Chapters {
					if strings.Contains(strings.ToLower(ch.Title), q) || strings.Contains(strings.ToLower(ch.Outline), q) {
						results = append(results, fmt.Sprintf("[chapter] #%d <%s>: %s", ch.Num, ch.Title, story.Truncate(ch.Outline, 100)))
					}
				}

				if len(results) == 0 {
					return agentMsg(ctx, "agent.search_no_results"), nil
				}
				return strings.Join(results, "\n"), nil
			},
		},
		{
			Name:        "create_character",
			Description: "Create a new character",
			Parameters:  `{"name": "character name", "age": "", "appearance": "", "personality": "", "background": "", "motivation": "", "abilities": "", "notes": ""}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var c story.Character
				if err := json.Unmarshal(args, &c); err != nil {
					return "", agentErr(ctx, "invalid_json", err)
				}
				if c.Name == "" {
					return "", agentErr(ctx, "character_name_empty")
				}

				c.ID = ctx.Settings.NextCharacterID()
				ctx.Settings.Characters = append(ctx.Settings.Characters, c)

				if err := story.SaveProjectSettings(ctx.SettingsPath, ctx.Settings); err != nil {
					return "", agentErr(ctx, "save_failed", err)
				}
				if ctx.Logger != nil {
					ctx.logger().SettingsUpdated()
				}

				return agentMsg(ctx, "agent.character_created", c.Name, c.ID), nil
			},
		},
		{
			Name:        "update_character",
			Description: "Update an existing character by ID or name",
			Parameters:  `{"id": "character ID", "name": "", "age": "", "personality": "", "background": ""}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					ID          string `json:"id"`
					Name        string `json:"name"`
					Age         string `json:"age"`
					Appearance  string `json:"appearance"`
					Personality string `json:"personality"`
					Background  string `json:"background"`
					Motivation  string `json:"motivation"`
					Abilities   string `json:"abilities"`
					Notes       string `json:"notes"`
				}
				if err := json.Unmarshal(args, &params); err != nil {
					return "", agentErr(ctx, "invalid_json", err)
				}

				for i, c := range ctx.Settings.Characters {
					if c.ID == params.ID || c.Name == params.ID || story.StripNameMarks(c.Name) == params.ID {
						if params.Name != "" {
							ctx.Settings.Characters[i].Name = params.Name
						}
						if params.Age != "" {
							ctx.Settings.Characters[i].Age = params.Age
						}
						if params.Appearance != "" {
							ctx.Settings.Characters[i].Appearance = params.Appearance
						}
						if params.Personality != "" {
							ctx.Settings.Characters[i].Personality = params.Personality
						}
						if params.Background != "" {
							ctx.Settings.Characters[i].Background = params.Background
						}
						if params.Motivation != "" {
							ctx.Settings.Characters[i].Motivation = params.Motivation
						}
						if params.Abilities != "" {
							ctx.Settings.Characters[i].Abilities = params.Abilities
						}
						if params.Notes != "" {
							ctx.Settings.Characters[i].Notes = params.Notes
						}

						if err := story.SaveProjectSettings(ctx.SettingsPath, ctx.Settings); err != nil {
							return "", agentErr(ctx, "save_failed", err)
						}
						if ctx.Logger != nil {
							ctx.logger().SettingsUpdated()
						}

						return agentMsg(ctx, "agent.character_updated", ctx.Settings.Characters[i].Name), nil
					}
				}
				return agentMsg(ctx, "agent.character_not_found", params.ID), nil
			},
		},
		{
			Name:        "delete_character",
			Description: "Delete a character by ID or name",
			Parameters:  `{"id": "character ID"}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					ID string `json:"id"`
				}
				json.Unmarshal(args, &params)

				for i, c := range ctx.Settings.Characters {
					if c.ID == params.ID || c.Name == params.ID || story.StripNameMarks(c.Name) == params.ID {
						ctx.Settings.Characters = append(ctx.Settings.Characters[:i], ctx.Settings.Characters[i+1:]...)
						if err := story.SaveProjectSettings(ctx.SettingsPath, ctx.Settings); err != nil {
							return "", agentErr(ctx, "save_failed", err)
						}
						if ctx.Logger != nil {
							ctx.logger().SettingsUpdated()
						}
						return agentMsg(ctx, "agent.character_deleted", c.Name), nil
					}
				}
				return agentMsg(ctx, "agent.character_not_found", params.ID), nil
			},
		},
		{
			Name:        "create_worldview",
			Description: "Create a worldview entry",
			Parameters:  `{"name": "", "category": "geography/faction/rule/history/other", "description": "", "tags": ""}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var w story.WorldviewEntry
				if err := json.Unmarshal(args, &w); err != nil {
					return "", agentErr(ctx, "invalid_json", err)
				}
				if w.Name == "" || w.Description == "" {
					return "", agentErr(ctx, "worldview_field_empty")
				}

				w.ID = ctx.Settings.NextWorldviewID()
				ctx.Settings.Worldview = append(ctx.Settings.Worldview, w)

				if err := story.SaveProjectSettings(ctx.SettingsPath, ctx.Settings); err != nil {
					return "", agentErr(ctx, "save_failed", err)
				}
				if ctx.Logger != nil {
					ctx.logger().SettingsUpdated()
				}

				return agentMsg(ctx, "agent.worldview_created", w.Name, w.ID), nil
			},
		},
		{
			Name:        "update_worldview",
			Description: "Update a worldview entry",
			Parameters:  `{"id": "entry ID", "name": "", "category": "", "description": "", "tags": ""}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					ID          string `json:"id"`
					Name        string `json:"name"`
					Category    string `json:"category"`
					Description string `json:"description"`
					Tags        string `json:"tags"`
				}
				if err := json.Unmarshal(args, &params); err != nil {
					return "", agentErr(ctx, "invalid_json", err)
				}

				for i, w := range ctx.Settings.Worldview {
					if w.ID == params.ID || w.Name == params.ID {
						if params.Name != "" {
							ctx.Settings.Worldview[i].Name = params.Name
						}
						if params.Category != "" {
							ctx.Settings.Worldview[i].Category = params.Category
						}
						if params.Description != "" {
							ctx.Settings.Worldview[i].Description = params.Description
						}
						if params.Tags != "" {
							ctx.Settings.Worldview[i].Tags = params.Tags
						}

						if err := story.SaveProjectSettings(ctx.SettingsPath, ctx.Settings); err != nil {
							return "", agentErr(ctx, "save_failed", err)
						}
						if ctx.Logger != nil {
							ctx.logger().SettingsUpdated()
						}

						return agentMsg(ctx, "agent.worldview_updated", ctx.Settings.Worldview[i].Name), nil
					}
				}
				return agentMsg(ctx, "agent.worldview_not_found", params.ID), nil
			},
		},
		{
			Name:        "delete_worldview",
			Description: "Delete a worldview entry",
			Parameters:  `{"id": "entry ID"}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					ID string `json:"id"`
				}
				json.Unmarshal(args, &params)

				for i, w := range ctx.Settings.Worldview {
					if w.ID == params.ID || w.Name == params.ID {
						ctx.Settings.Worldview = append(ctx.Settings.Worldview[:i], ctx.Settings.Worldview[i+1:]...)
						if err := story.SaveProjectSettings(ctx.SettingsPath, ctx.Settings); err != nil {
							return "", agentErr(ctx, "save_failed", err)
						}
						if ctx.Logger != nil {
							ctx.logger().SettingsUpdated()
						}
						return agentMsg(ctx, "agent.worldview_deleted", w.Name), nil
					}
				}
				return agentMsg(ctx, "agent.worldview_not_found", params.ID), nil
			},
		},
		{
			Name:        "read_project_config",
			Description: "Read the current project configuration (title, genre, theme, etc.)",
			Parameters:  `{}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				// Always read from the live config: the form (PUT /api/config) and
				// agent updates both mutate the live config Story, while the progress
				// snapshot can be stale after a manual save.
				data, _ := json.MarshalIndent(ctx.safeCfg().Story, "", "  ")
				return string(data), nil
			},
		},
		{
			Name:        "update_project_config",
			Description: "Update story-level parameters: type/subgenre/title/author/theme/tone/motif/story_length/structure/story_idea/conflict_scale/conflict_other/specific_settings/protagonist_type/protagonist_other/target_audience/audience_profile/inspirational_pieces/output_language/romance_level/sexual_content/gore_level/world_darkness/gender_bias/locations_enabled/character_arcs_enabled/target_words_per_chapter/writing_style/writing_pov. story_idea is the core premise/direction of the book (Story idea), not a per-chapter plan; chapter count and per-batch synopses belong to generate_outline, do not write them here. Set confirm_overwrite=true only when the field already has content.",
			Parameters:  `{"type":"genre","title":"book title","subgenre":"subgenre","theme":"","tone":"","author":"pen name","story_length":"length target","structure":"","motif":"","story_idea":"core premise (Story idea: main intent and direction, what chapters/outlines should serve)","conflict_scale":"","conflict_other":"","specific_settings":"(one per line)","protagonist_type":"protagonist type","protagonist_other":"custom character","target_audience":"target reader","audience_profile":"reader profile","inspirational_pieces":"works that inspire (one per line)","output_language":"en","romance_level":"romance level","sexual_content":"explicitness","gore_level":"violence level","world_darkness":"darkness level","gender_bias":"","locations_enabled":false,"character_arcs_enabled":false,"target_words_per_chapter":2500,"writing_style":"writing style","writing_pov":"POV","confirm_overwrite":false}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params map[string]json.RawMessage
				if err := json.Unmarshal(args, &params); err != nil {
					return "", agentErr(ctx, "invalid_json", err)
				}
				var overwrite bool
				if raw, ok := params["confirm_overwrite"]; ok {
					_ = json.Unmarshal(raw, &overwrite)
					delete(params, "confirm_overwrite")
				}
				if len(params) == 0 {
					return "", agentErr(ctx, "no_fields", fmt.Errorf("no supported config fields provided"))
				}

				cfg := ctx.safeCfg()
				proposed := cfg.Story
				for key, raw := range params {
					sp, ok := story.LookupStoryFieldSpec(key)
					if !ok {
						return "", agentErr(ctx, "unknown_field", fmt.Errorf("unsupported config field %q", key))
					}
					switch {
					case sp.SetStr != nil:
						var v string
						if err := json.Unmarshal(raw, &v); err != nil {
							return "", agentErr(ctx, "invalid_field_value", fmt.Errorf("field %q must be a string", key))
						}
						sp.SetStr(&proposed, v)
					case sp.SetBool != nil:
						var v bool
						if err := json.Unmarshal(raw, &v); err != nil {
							return "", agentErr(ctx, "invalid_field_value", fmt.Errorf("field %q must be a boolean", key))
						}
						sp.SetBool(&proposed, v)
					case sp.SetInt != nil:
						var v int
						if err := json.Unmarshal(raw, &v); err != nil {
							return "", agentErr(ctx, "invalid_field_value", fmt.Errorf("field %q must be an integer", key))
						}
						if v <= 0 {
							continue
						}
						sp.SetInt(&proposed, v)
					}
				}

				conflicts := story.CollectStoryConfigConflicts(cfg.Story, proposed, "agent", "")
				if len(conflicts) > 0 && !overwrite {
					return story.FormatConfigConflictMessage(conflicts, cfg.Language), nil
				}

				cfg.Story = proposed

				story.SyncProgressMetaFromStory(ctx.State, cfg.Story)

				if err := config.SaveConfig(ctx.CfgPath, cfg); err != nil {
					return "", agentErr(ctx, "save_config_failed", err)
				}

				if overwrite {
					pendingPath := story.PendingConfigChangesPath(ctx.ProgressPath)
					for _, c := range conflicts {
						_ = story.RemovePendingFields(pendingPath, c.Field)
					}
				}

				hasAccepted := false
				for _, ch := range ctx.State.Chapters {
					if ch.Status == story.StatusAccepted {
						hasAccepted = true
						break
					}
				}

				if hasAccepted && ctx.StartAsync != nil {
					newSettings := cfg.Story
					ctx.StartAsync("settings_reconciliation", func(goCtx context.Context) error {
						err := story.ReconcileSettingsAction(goCtx, ctx.APICfg, cfg, ctx.State, newSettings, ctx.Settings, ctx.ProgressPath, ctx.CfgPath, ctx.Logger)
						if err != nil {
							ctx.logger().Error(fmt.Sprintf(": %v", err))
						}
						return err
					})
					return agentMsg(ctx, "agent.config_saved_reconciling"), nil
				}

				if ctx.Logger != nil {
					ctx.logger().SettingsUpdated()
				}
				return agentMsg(ctx, "agent.config_saved"), nil
			},
		},
		{
			Name:        "generate_section",
			Description: "AI generation section blocks (style/characters/organizations/relations/locations/worldview/motif/story_idea/audience_profile/inspirational_pieces/pre_planning): generate the content of a given block type based on the current story parameters; pre_planning plans the book-level structure, chapter count, long-term direction and per-batch synopses before writing. First confirm the target book number, then fill in the novel parameters and write into the corresponding fields/body. Optional direct references: character_id/org_id/entry_id (only for entries that already exist with empty fields), source_id+target_id (only when these two characters have a relationship), name+category or entry_id (single worldview entry; without them, worldview batch-generates entries across all categories). Per-category worldview sections also exist: wv_geography/wv_location/wv_faction/wv_rule/wv_knowledge/wv_history/wv_other generate ONLY entries of that worldview category.",
			Parameters:  `{"section":"characters","character_id":"","org_id":"","entry_id":"","source_id":"","target_id":"","name":"","category":""}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					Section     string `json:"section"`
					CharacterID string `json:"character_id"`
					OrgID       string `json:"org_id"`
					EntryID     string `json:"entry_id"`
					SourceID    string `json:"source_id"`
					TargetID    string `json:"target_id"`
					Name        string `json:"name"`
					Category    string `json:"category"`
				}
				if err := json.Unmarshal(args, &params); err != nil {
					return "", agentErr(ctx, "invalid_json", err)
				}
				switch params.Section {
				case "style", "characters", "organizations", "relations", "locations", "worldview", "motif", "story_idea", "audience_profile", "inspirational_pieces", "pre_planning":
				default:
					if !IsWorldviewCategorySection(params.Section) {
						return "", agentErr(ctx, "unknown_field", fmt.Errorf("unsupported section %q", params.Section))
					}
				}
				if params.Section == "pre_planning" {
					// Book-level plan lives in Progress (not Story config): dedicated
					// detached runner, no config save/seed needed.
					runnerPre := ctx.StartPrePlanGenerate
					if runnerPre == nil {
						return "", agentErr(ctx, "agent_unavailable", fmt.Errorf("pre-plan generator unavailable"))
					}
					if err := runnerPre(); err != nil {
						return "", agentErr(ctx, "task_running_wait", err)
					}
					ctx.logger().InfoKey("log.section_generating", "pre_planning")
					return agentMsg(ctx, "agent.section_generate_started", "pre_planning"), nil
				}
				cfg := ctx.safeCfg()
				storyCfg := cfg.Story
				// Persist the current novel parameters first so the generation seed and
				// the saved project state stay identical (same as the config-page flow).
				if err := config.SaveConfig(ctx.CfgPath, cfg); err != nil {
					return "", agentErr(ctx, "save_config_failed", err)
				}
				req := SectionGenRequest{
					Section:     params.Section,
					StoryIdea:   strings.TrimSpace(storyCfg.StoryIdea),
					Story:       &storyCfg,
					Name:        params.Name,
					Category:    params.Category,
					EntryID:     params.EntryID,
					SourceID:    params.SourceID,
					TargetID:    params.TargetID,
					CharacterID: params.CharacterID,
					OrgID:       params.OrgID,
					Ctx:         ctx.SectionGenCtx,
				}
				runner := ctx.StartSectionGenerate
				if runner == nil {
					return "", agentErr(ctx, "agent_unavailable", fmt.Errorf("section generator unavailable"))
				}
				if err := runner(req); err != nil {
					return "", agentErr(ctx, "task_running_wait", err)
				}
				ctx.logger().InfoKey("log.section_generating", params.Section)
				return agentMsg(ctx, "agent.section_generate_started", params.Section), nil
			},
		},
		{
			Name:        "generate_outline",
			Description: "Generate chapter outlines for this batch from the required synopsis (async); appends by default. chapter_count is the number of chapters in this batch (1-36); replace_last only replaces all unwritten batches at the end and requires batch_id and confirm=true.",
			Parameters:  `{"chapter_count":12,"outline_synopsis":"thisbatchprobability","long_term_direction":"acrossbatchtimeslongexpectwalkdirection","mode":"append","batch_id":0,"confirm":false,"ending_intent":"serial|final|sequel","ending_style":"closed|open|custom","ending_requirements":"","confirm_continue":false}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var req story.OutlineBatchRequest
				if err := json.Unmarshal(args, &req); err != nil {
					return "", agentErr(ctx, "invalid_json", err)
				}
				if req.Mode == "replace_last" {
					if msg := requireConfirm(ctx, args, "replace_last"); msg != "" {
						return msg, nil
					}
				}
				if err := story.ValidateOutlineBatch(ctx.State, req, ctx.safeCfg().Language); err != nil {
					return "", err
				}
				if ctx.StartAsync == nil {
					return "", agentErr(ctx, "task_running_wait")
				}
				ctx.StartAsync("outline_generation", func(goCtx context.Context) error {
					return story.GenerateOutlineBatch(goCtx, ctx.APICfg, ctx.cfg(), ctx.State, ctx.Settings, req, ctx.ProgressPath, ctx.Logger)
				})
				return agentMsg(ctx, "agent.outline_task_started"), nil
			},
		},
		{
			Name:        "confirm_outline",
			Description: "Large step: enter the writing stage",
			Parameters:  `{}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				if ctx.State.Phase != "outline" {
					return "", agentErr(ctx, "phase_not_outline")
				}
				if len(ctx.State.Chapters) == 0 {
					return "", agentErr(ctx, "outline_empty")
				}
				if err := story.ConfirmOutlineAction(ctx.State, ctx.ProgressPath); err != nil {
					return "", agentErr(ctx, "outline_confirm_failed", err)
				}
				ctx.logger().SuccessKey("log.outline_confirmed")
				return agentMsg(ctx, "agent.outline_confirmed"), nil
			},
		},
		{
			Name:        "revise_outline",
			Description: "Revise the outline based on feedback (async). Only for adjustments that keep the chapter count unchanged (plot changes, per-chapter tweaks); it cannot add or remove chapters. If the user wants to shrink/expand the chapter count or regenerate the whole book, use generate_outline(mode=replace_last, batch_id, chapter_count, outline_synopsis, confirm=true) instead of this tool.",
			Parameters:  `{"feedback": "intentsee"}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					Feedback string `json:"feedback"`
				}
				if err := json.Unmarshal(args, &params); err != nil || params.Feedback == "" {
					return "", agentErr(ctx, "missing_feedback")
				}
				if ctx.StartAsync == nil {
					return "", agentErr(ctx, "task_running_wait")
				}
				feedback := params.Feedback
				ctx.StartAsync("outline_revision", func(goCtx context.Context) error {
					err := story.ReviseOutlineAction(goCtx, ctx.APICfg, ctx.cfg(), ctx.State, ctx.Settings, ctx.ProgressPath, ctx.CfgPath, feedback, ctx.Logger)
					if err != nil {
						ctx.logger().Error(fmt.Sprintf("big: %v", err))
					}
					return err
				})
				return agentMsg(ctx, "agent.outline_revise_started"), nil
			},
		},
		{
			Name:        "delete_outline",
			Description: "[DANGER - irreversible] Clear the entire outline and all chapter data. Use only when the user explicitly asks to delete/clear the outline and does not need an immediate regeneration. If the user wants to \"delete and regenerate N chapters\" and there are no accepted chapters, call generate_outline(mode=replace_last, batch_id, chapter_count, outline_synopsis, confirm=true) directly without this tool. Never use it to modify a single chapter's outline/prose.",
			Parameters:  `{"confirm": true}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				if msg := requireConfirm(ctx, args, fmt.Sprintf("exceptbig( %d chapter)", len(ctx.State.Chapters))); msg != "" {
					return msg, nil
				}
				for _, ch := range ctx.State.Chapters {
					if ch.Status == story.StatusWriting || ch.Status == story.StatusReview {
						return "", agentErr(ctx, "writing_chapter_present_delete")
					}
				}
				ctx.State.Title = ""
				ctx.State.CorePrompt = ""
				ctx.State.OutlineBatches = nil
				ctx.State.Chapters = nil
				ctx.State.StoryConfigSnapshot = nil
				ctx.State.CurrentChapterIndex = 0
				if err := story.SaveProgress(ctx.ProgressPath, ctx.State); err != nil {
					return "", agentErr(ctx, "save_progress_failed", err)
				}
				ctx.logger().SuccessKey("log.outline_deleted")
				return agentMsg(ctx, "agent.outline_deleted"), nil
			},
		},
		{
			Name:        "edit_chapter_outline",
			Description: "Edit a chapter's title and outline (allowed for pending / writing / review; not for accepted chapters)",
			Parameters:  `{"num": 1, "title": "new title", "outline": "new outline"}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					Num     int    `json:"num"`
					Title   string `json:"title"`
					Outline string `json:"outline"`
				}
				if err := json.Unmarshal(args, &params); err != nil {
					return "", agentErr(ctx, "invalid_json", err)
				}
				if err := story.EditChapterOutline(ctx.State, params.Num, params.Title, params.Outline, nil); err != nil {
					return "", err
				}
				if err := story.SaveProgress(ctx.ProgressPath, ctx.State); err != nil {
					return "", agentErr(ctx, "save_progress_failed", err)
				}
				ctx.logger().SuccessKey("log.chapter_outline_updated", params.Num)
				return agentMsg(ctx, "agent.chapter_outline_updated", params.Num), nil
			},
		},
		{
			Name:        "generate_chapter",
			Description: "Generate the current frontier chapter (async)",
			Parameters:  `{}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				if ctx.State.Phase != "writing" {
					return "", agentErr(ctx, "phase_not_writing")
				}
				if ctx.StartAsync == nil {
					return "", agentErr(ctx, "task_running_wait")
				}
				chIdx := ctx.State.CurrentChapterIndex
				ctx.StartAsync("chapter_generation", func(goCtx context.Context) error {
					err := story.GenerateChapterAction(goCtx, ctx.APICfg, ctx.cfg(), ctx.State, ctx.ProgressPath, ctx.Settings, ctx.Skills, ctx.Logger)
					if err != nil {
						ctx.logger().Error(fmt.Sprintf("chapter generation failed: %v", err))
					}
					return err
				})
				return agentMsg(ctx, "agent.chapter_task_started", chIdx+1), nil
			},
		},
		{
			Name:        "confirm_chapter",
			Description: "Confirm/accept the current frontier chapter",
			Parameters:  `{}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				if ctx.State.Phase != "writing" {
					return "", agentErr(ctx, "phase_not_writing")
				}
				if err := story.ConfirmChapterAction(ctx.State, ctx.ProgressPath); err != nil {
					return "", err
				}
				ch := ctx.State.Chapters[ctx.State.CurrentChapterIndex-1]
				ctx.logger().SuccessKey("log.chapter_confirmed", ch.Num)
				return agentMsg(ctx, "agent.chapter_confirmed", ch.Num, ch.Title), nil
			},
		},
		{
			Name:        "edit_chapter_content",
			Description: "Apply a targeted edit to one chapter's prose (does not rewrite the whole chapter). 4 operations: replace_lines (replace a line range), replace_text (replace one passage), insert_after_line (insert after a given line), append (add at the end). Multiple passages can be combined via separate calls.",
			Parameters:  `{"num": 1, "operation": "replace_lines|replace_text|insert_after_line|append", "start_line": 1, "end_line": 5, "old_text": "text to find", "line": 10, "new_text": "new content", "content_rev":"version returned by read_chapter", "confirm_fact_impact":false}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var req story.EditChapterContentRequest
				if err := json.Unmarshal(args, &req); err != nil {
					return "", agentErr(ctx, "invalid_json", err)
				}
				if req.Operation == "" {
					return "", agentErr(ctx, "chapter_edit_op_required")
				}
				if req.NewText == "" && req.Operation != story.EditOpReplaceText {
					return "", agentErr(ctx, "chapter_edit_text_required")
				}

				totalLines, err := story.EditChapterContent(ctx.State, req)
				if err != nil {
					return "", agentErr(ctx, "chapter_edit_failed", err)
				}

				if err := story.SaveProgress(ctx.ProgressPath, ctx.State); err != nil {
					return "", agentErr(ctx, "save_progress_failed", err)
				}

				story.SaveChapterMarkdown(ctx.ProjectDir, story.ChapterState{
					Num:     req.ChapterNum,
					Title:   ctx.State.Chapters[story.FindChapterIdx(ctx.State, req.ChapterNum)].Title,
					Content: ctx.State.Chapters[story.FindChapterIdx(ctx.State, req.ChapterNum)].Content,
				}, "")

				return agentMsg(ctx, "agent.chapter_content_edited", req.ChapterNum, string(req.Operation), totalLines), nil
			},
		},
		{
			Name:        "revise_chapter",
			Description: "Revise a chapter. Pass a chapter number (any existing chapter, including accepted ones); omit the number to revise the chapter currently being written. This is the only operation within a chapter: it rewrites just that chapter's body and does not touch other chapters or the outline.",
			Parameters:  `{"num": 6, "feedback": "toolbodyintentsee"}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					Num      int    `json:"num"`
					Feedback string `json:"feedback"`
				}
				if err := json.Unmarshal(args, &params); err != nil || strings.TrimSpace(params.Feedback) == "" {
					return "", agentErr(ctx, "missing_feedback")
				}
				if ctx.StartAsync == nil {
					return "", agentErr(ctx, "task_running_wait")
				}
				feedback := params.Feedback
				num := params.Num

				// no chapter number given -> frontier chapter (currently writing)
				if num <= 0 {
					if ctx.State.Phase != "writing" || ctx.State.CurrentChapterIndex >= len(ctx.State.Chapters) {
						return "", agentErr(ctx, "chapter_not_found")
					}
					num = ctx.State.Chapters[ctx.State.CurrentChapterIndex].Num
				}

				// Localizar el capítulo solicitado
				var target *story.ChapterState
				for i := range ctx.State.Chapters {
					if ctx.State.Chapters[i].Num == num {
						target = &ctx.State.Chapters[i]
						break
					}
				}
				if target == nil {
					return "", agentErr(ctx, "chapter_n_not_found", num)
				}
				if target.Content == "" {
					return "", agentErr(ctx, "chapter_content_empty")
				}

				// Si el capítulo es el actual en escritura (borrador en revisión),
				// la revisión se aplica directamente al borrador en curso.
				isCurrent := ctx.State.Phase == "writing" &&
					ctx.State.CurrentChapterIndex < len(ctx.State.Chapters) &&
					ctx.State.Chapters[ctx.State.CurrentChapterIndex].Num == num &&
					(target.Status == story.StatusReview || target.Status == story.StatusWriting)

				chNum := num
				ctx.StartAsync("chapter_revision", func(goCtx context.Context) error {
					var err error
					if isCurrent {
						err = story.ReviseChapterAction(goCtx, ctx.APICfg, ctx.cfg(), ctx.State, ctx.ProgressPath, feedback, ctx.Settings, ctx.Logger)
					} else {
						err = story.ReviseSpecificChapterAction(goCtx, ctx.APICfg, ctx.cfg(), ctx.State, ctx.ProgressPath, chNum, feedback, ctx.Settings, ctx.Logger)
					}
					if err != nil {
						ctx.logger().Error(fmt.Sprintf("chapter revision failed: %v", err))
					}
					return err
				})
				return agentMsg(ctx, "agent.chapter_revise_started", num), nil
			},
		},
		{
			Name:        "delete_chapter",
			Description: "[RISK · irreversible] Delete the writing-frontier chapter (keeps the rest). Frontier = CurrentChapterIndex (a reviewed chapter), or the one accepted chapter before a pending chapter, or the latest accepted chapter. Deletes only this one chapter; for multiple chapters use delete_chapters_from. Confirm before use. ",
			Parameters:  `{"confirm": true}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				if len(ctx.State.Chapters) == 0 {
					return "", agentErr(ctx, "no_chapters_to_delete")
				}
				idx, resolveErr := story.ResolveDeleteChapterTarget(ctx.State)
				if resolveErr != nil {
					switch resolveErr {
					case story.ErrWritingChapterCannotDelete:
						return "", agentErr(ctx, "writing_chapter_cannot_delete")
					case story.ErrDeleteFrontierUnavailable:
						return "", agentErr(ctx, "delete_frontier_unavailable")
					default:
						return "", agentErr(ctx, "no_chapters_to_delete")
					}
				}
				targetNum := ctx.State.Chapters[idx].Num
				if msg := requireConfirm(ctx, args, fmt.Sprintf("Delete frontier chapter #%d", targetNum)); msg != "" {
					return msg, nil
				}
				num, err := story.DeleteFrontierChapter(ctx.State, ctx.ProjectDir)
				if err != nil {
					switch err {
					case story.ErrWritingChapterCannotDelete:
						return "", agentErr(ctx, "writing_chapter_cannot_delete")
					case story.ErrDeleteFrontierUnavailable:
						return "", agentErr(ctx, "delete_frontier_unavailable")
					default:
						return "", agentErr(ctx, "no_chapters_to_delete")
					}
				}
				if err := story.SaveProgress(ctx.ProgressPath, ctx.State); err != nil {
					return "", agentErr(ctx, "save_progress_failed", err)
				}
				ctx.logger().SuccessKey("log.chapter_deleted", num)
				return agentMsg(ctx, "agent.chapter_deleted", num), nil
			},
		},
		{
			Name:        "delete_chapters_from",
			Description: "Delete chapters from a given chapter number to the end, keeping earlier chapters. Use only when the user wants to bulk-remove drafted chapters. Cannot be used to edit a single chapter or to regenerate the book — for regeneration use generate_outline(mode=replace_last, batch_id, chapter_count, outline_synopsis, confirm=true). For one chapter use revise_chapter.",
			Parameters:  `{"num": 6, "confirm": true}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					Num     int  `json:"num"`
					Confirm bool `json:"confirm"`
				}
				json.Unmarshal(args, &params)

				if !params.Confirm {
					affected := 0
					for _, ch := range ctx.State.Chapters {
						if ch.Num >= params.Num {
							affected++
						}
					}
					return agentMsg(ctx, "agent.chapters_bulk_delete_confirm", params.Num, affected), nil
				}

				startIdx := -1
				for i, ch := range ctx.State.Chapters {
					if ch.Num == params.Num {
						startIdx = i
						break
					}
				}
				if startIdx == -1 {
					return "", agentErr(ctx, "chapter_n_not_found", params.Num)
				}
				for i := startIdx; i < len(ctx.State.Chapters); i++ {
					if ctx.State.Chapters[i].Status == story.StatusWriting {
						return "", agentErr(ctx, "writing_range_has_writing")
					}
				}
				deletedCount := len(ctx.State.Chapters) - startIdx
				for i := startIdx; i < len(ctx.State.Chapters); i++ {
					ch := &ctx.State.Chapters[i]
					fsutil.Delete(story.ChapterMarkdownPath(ctx.ProjectDir, ch.Num))
					ch.Content = ""
					ch.Summary = ""
					ch.Status = story.StatusPending
				}
				if ctx.State.CurrentChapterIndex >= startIdx {
					ctx.State.CurrentChapterIndex = startIdx
				}
				if err := story.SaveProgress(ctx.ProgressPath, ctx.State); err != nil {
					return "", agentErr(ctx, "save_progress_failed", err)
				}
				ctx.logger().SuccessKey("log.chapters_deleted_from", params.Num, deletedCount)
				return agentMsg(ctx, "agent.chapters_deleted_from", params.Num, deletedCount), nil
			},
		},
		{
			Name:        "create_organization",
			Description: "Create an organization",
			Parameters:  `{"name": "organization name", "type": "category", "description": "", "members": ["character ID"]}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var o story.Organization
				if err := json.Unmarshal(args, &o); err != nil {
					return "", agentErr(ctx, "invalid_json", err)
				}
				if o.Name == "" {
					return "", agentErr(ctx, "organization_name_empty")
				}
				o.ID = ctx.Settings.NextOrganizationID()
				ctx.Settings.Organizations = append(ctx.Settings.Organizations, o)
				if err := story.SaveProjectSettings(ctx.SettingsPath, ctx.Settings); err != nil {
					return "", agentErr(ctx, "save_failed", err)
				}
				if ctx.Logger != nil {
					ctx.logger().SettingsUpdated()
				}
				return agentMsg(ctx, "agent.organization_created", o.Name, o.ID), nil
			},
		},
		{
			Name:        "update_organization",
			Description: "Update an organization",
			Parameters:  `{"id": "organization ID", "name": "", "type": "", "description": "", "members": []}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					ID          string   `json:"id"`
					Name        string   `json:"name"`
					Type        string   `json:"type"`
					Description string   `json:"description"`
					Members     []string `json:"members"`
				}
				if err := json.Unmarshal(args, &params); err != nil {
					return "", agentErr(ctx, "invalid_json", err)
				}
				for i, o := range ctx.Settings.Organizations {
					if o.ID == params.ID || o.Name == params.ID {
						if params.Name != "" {
							ctx.Settings.Organizations[i].Name = params.Name
						}
						if params.Type != "" {
							ctx.Settings.Organizations[i].Type = params.Type
						}
						if params.Description != "" {
							ctx.Settings.Organizations[i].Description = params.Description
						}
						if params.Members != nil {
							ctx.Settings.Organizations[i].Members = params.Members
						}
						if err := story.SaveProjectSettings(ctx.SettingsPath, ctx.Settings); err != nil {
							return "", agentErr(ctx, "save_failed", err)
						}
						if ctx.Logger != nil {
							ctx.logger().SettingsUpdated()
						}
						return agentMsg(ctx, "agent.organization_updated", ctx.Settings.Organizations[i].Name), nil
					}
				}
				return agentMsg(ctx, "agent.organization_not_found", params.ID), nil
			},
		},
		{
			Name:        "delete_organization",
			Description: "Delete an organization",
			Parameters:  `{"id": "organizationID"}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					ID string `json:"id"`
				}
				json.Unmarshal(args, &params)
				for i, o := range ctx.Settings.Organizations {
					if o.ID == params.ID || o.Name == params.ID {
						ctx.Settings.Organizations = append(ctx.Settings.Organizations[:i], ctx.Settings.Organizations[i+1:]...)
						if err := story.SaveProjectSettings(ctx.SettingsPath, ctx.Settings); err != nil {
							return "", agentErr(ctx, "save_failed", err)
						}
						if ctx.Logger != nil {
							ctx.logger().SettingsUpdated()
						}
						return agentMsg(ctx, "agent.organization_deleted", o.Name), nil
					}
				}
				return agentMsg(ctx, "agent.organization_not_found", params.ID), nil
			},
		},
		{
			Name:        "create_relation",
			Description: "Create a relationship between two entities (characters or organizations)",
			Parameters:  `{"source_id": "ID", "source_type": "character|organization", "target_id": "target ID", "target_type": "character|organization", "label": "relationship label"}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var rel story.Relation
				if err := json.Unmarshal(args, &rel); err != nil {
					return "", agentErr(ctx, "invalid_json", err)
				}
				if rel.SourceID == "" || rel.TargetID == "" {
					return "", agentErr(ctx, "relation_endpoints_empty")
				}
				rel.ID = ctx.Settings.NextRelationID()
				ctx.Settings.Relations = append(ctx.Settings.Relations, rel)
				if err := story.SaveProjectSettings(ctx.SettingsPath, ctx.Settings); err != nil {
					return "", agentErr(ctx, "save_failed", err)
				}
				if ctx.Logger != nil {
					ctx.logger().SettingsUpdated()
				}
				return agentMsg(ctx, "agent.relation_created", rel.ID), nil
			},
		},
		{
			Name:        "update_relation",
			Description: "Update an existing relationship",
			Parameters:  `{"id": "relation ID", "source_id": "", "source_type": "", "target_id": "", "target_type": "", "label": ""}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					ID         string `json:"id"`
					SourceID   string `json:"source_id"`
					SourceType string `json:"source_type"`
					TargetID   string `json:"target_id"`
					TargetType string `json:"target_type"`
					Label      string `json:"label"`
				}
				if err := json.Unmarshal(args, &params); err != nil {
					return "", agentErr(ctx, "invalid_json", err)
				}
				for i, rel := range ctx.Settings.Relations {
					if rel.ID == params.ID {
						if params.SourceID != "" {
							ctx.Settings.Relations[i].SourceID = params.SourceID
						}
						if params.SourceType != "" {
							ctx.Settings.Relations[i].SourceType = params.SourceType
						}
						if params.TargetID != "" {
							ctx.Settings.Relations[i].TargetID = params.TargetID
						}
						if params.TargetType != "" {
							ctx.Settings.Relations[i].TargetType = params.TargetType
						}
						if params.Label != "" {
							ctx.Settings.Relations[i].Label = params.Label
						}
						if err := story.SaveProjectSettings(ctx.SettingsPath, ctx.Settings); err != nil {
							return "", agentErr(ctx, "save_failed", err)
						}
						if ctx.Logger != nil {
							ctx.logger().SettingsUpdated()
						}
						return agentMsg(ctx, "agent.relation_updated", ctx.Settings.Relations[i].ID), nil
					}
				}
				return agentMsg(ctx, "agent.relation_not_found", params.ID), nil
			},
		},
		{
			Name:        "delete_relation",
			Description: "Delete a relationship",
			Parameters:  `{"id": "relation ID"}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					ID string `json:"id"`
				}
				json.Unmarshal(args, &params)
				for i, rel := range ctx.Settings.Relations {
					if rel.ID == params.ID {
						ctx.Settings.Relations = append(ctx.Settings.Relations[:i], ctx.Settings.Relations[i+1:]...)
						if err := story.SaveProjectSettings(ctx.SettingsPath, ctx.Settings); err != nil {
							return "", agentErr(ctx, "save_failed", err)
						}
						if ctx.Logger != nil {
							ctx.logger().SettingsUpdated()
						}
						return agentMsg(ctx, "agent.relation_deleted"), nil
					}
				}
				return agentMsg(ctx, "agent.relation_not_found", params.ID), nil
			},
		},
		{
			Name:        "suggest_foreshadows",
			Description: "Suggest foreshadowing entries with AI (async)",
			Parameters:  `{}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				if len(ctx.State.Chapters) == 0 {
					return "", agentErr(ctx, "need_generate_outline_first")
				}
				if ctx.StartAsync == nil {
					return "", agentErr(ctx, "task_running_wait")
				}
				ctx.StartAsync("foreshadow_suggest", func(goCtx context.Context) error {
					suggestions, err := story.SuggestForeshadows(goCtx, ctx.APICfg, ctx.cfg(), ctx.State, ctx.Logger)
					if err != nil {
						ctx.logger().Error(fmt.Sprintf("life: %v", err))
						return err
					}
					ctx.logger().Success(fmt.Sprintf("life, %d strip", len(suggestions)))
					ctx.logger().ForeshadowSuggestions(suggestions)
					return nil
				})
				return agentMsg(ctx, "agent.foreshadow_suggest_started"), nil
			},
		},
		{
			Name:        "create_foreshadow",
			Description: "Create a foreshadowing entry manually",
			Parameters:  `{"name": "", "description": "", "plant_chapter": 1, "target_chapter": 5}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var req struct {
					Name          string `json:"name"`
					Description   string `json:"description"`
					PlantChapter  int    `json:"plant_chapter"`
					TargetChapter int    `json:"target_chapter"`
				}
				if err := json.Unmarshal(args, &req); err != nil {
					return "", agentErr(ctx, "invalid_json", err)
				}
				if req.Name == "" || req.Description == "" {
					return "", agentErr(ctx, "worldview_field_empty")
				}
				fs := story.Foreshadow{
					ID:            story.NextForeshadowID(ctx.State.Foreshadows),
					Name:          req.Name,
					Description:   req.Description,
					PlantChapter:  req.PlantChapter,
					TargetChapter: req.TargetChapter,
					Status:        story.ForeshadowPlanted,
					Events:        []story.ForeshadowEvent{},
				}
				ctx.State.Foreshadows = append(ctx.State.Foreshadows, fs)
				if err := story.SaveProgress(ctx.ProgressPath, ctx.State); err != nil {
					return "", agentErr(ctx, "save_failed", err)
				}
				_ = story.SaveForeshadowRoadmap(filepath.Dir(ctx.ProgressPath), ctx.State)
				return agentMsg(ctx, "agent.foreshadow_created", fs.Name, fs.ID), nil
			},
		},
		{
			Name:        "update_foreshadow",
			Description: "Update a foreshadowing entry",
			Parameters:  `{"id": 1, "name": "", "description": "", "plant_chapter": 0, "target_chapter": 0, "status": "", "resolution": ""}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var req struct {
					ID            int    `json:"id"`
					Name          string `json:"name"`
					Description   string `json:"description"`
					PlantChapter  int    `json:"plant_chapter"`
					TargetChapter int    `json:"target_chapter"`
					Status        string `json:"status"`
					Resolution    string `json:"resolution"`
				}
				if err := json.Unmarshal(args, &req); err != nil {
					return "", agentErr(ctx, "invalid_json", err)
				}
				idx := -1
				for i, fs := range ctx.State.Foreshadows {
					if fs.ID == req.ID {
						idx = i
						break
					}
				}
				if idx == -1 {
					return "", agentErr(ctx, "foreshadow_not_found")
				}
				fs := &ctx.State.Foreshadows[idx]
				if req.Name != "" {
					fs.Name = req.Name
				}
				if req.Description != "" {
					fs.Description = req.Description
				}
				if req.PlantChapter > 0 {
					fs.PlantChapter = req.PlantChapter
				}
				if req.TargetChapter > 0 {
					fs.TargetChapter = req.TargetChapter
				}
				if req.Status != "" {
					fs.Status = story.ForeshadowStatus(req.Status)
				}
				if req.Resolution != "" {
					fs.Resolution = req.Resolution
				}
				if err := story.SaveProgress(ctx.ProgressPath, ctx.State); err != nil {
					return "", agentErr(ctx, "save_failed", err)
				}
				_ = story.SaveForeshadowRoadmap(filepath.Dir(ctx.ProgressPath), ctx.State)
				return agentMsg(ctx, "agent.foreshadow_updated", fs.Name), nil
			},
		},
		{
			Name:        "delete_foreshadow",
			Description: "Delete a foreshadowing entry",
			Parameters:  `{"id": 1}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					ID int `json:"id"`
				}
				json.Unmarshal(args, &params)
				for i, fs := range ctx.State.Foreshadows {
					if fs.ID == params.ID {
						ctx.State.Foreshadows = append(ctx.State.Foreshadows[:i], ctx.State.Foreshadows[i+1:]...)
						if err := story.SaveProgress(ctx.ProgressPath, ctx.State); err != nil {
							return "", agentErr(ctx, "save_failed", err)
						}
						_ = story.SaveForeshadowRoadmap(filepath.Dir(ctx.ProgressPath), ctx.State)
						return agentMsg(ctx, "agent.foreshadow_deleted", fs.Name), nil
					}
				}
				return agentMsg(ctx, "agent.foreshadow_not_found", params.ID), nil
			},
		},
		{
			Name:        "read_skills",
			Description: "Get all skills and their enabled state",
			Parameters:  `{}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var result strings.Builder
				for _, s := range ctx.Skills {
					enabled := false
					if sk := ctx.safeCfg().SkillConfig; sk != nil && sk.EnabledSkills != nil {
						enabled = sk.EnabledSkills[s.ID]
					}
					status := "❌"
					if enabled {
						status = "✅"
					}
					result.WriteString(fmt.Sprintf("%s [%s] %s (%s)\n  %s\n\n", status, s.Category, s.Name, s.ID, s.Description))
				}
				return result.String(), nil
			},
		},
		{
			Name:        "toggle_skill",
			Description: "Enable or disable a writing skill",
			Parameters:  `{"id": "skill ID", "enabled": true}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				var params struct {
					ID      string `json:"id"`
					Enabled bool   `json:"enabled"`
				}
				if err := json.Unmarshal(args, &params); err != nil {
					return "", agentErr(ctx, "invalid_json", err)
				}
				found := false
				for _, s := range ctx.Skills {
					if s.ID == params.ID {
						found = true
						break
					}
				}
				if !found {
					return "", agentErr(ctx, "skill_not_found")
				}
				cfg := ctx.safeCfg()
				if cfg.SkillConfig == nil {
					cfg.SkillConfig = &config.SkillConfig{EnabledSkills: make(map[string]bool)}
				}
				if cfg.SkillConfig.EnabledSkills == nil {
					cfg.SkillConfig.EnabledSkills = make(map[string]bool)
				}
				cfg.SkillConfig.EnabledSkills[params.ID] = params.Enabled
				if err := config.SaveConfig(ctx.CfgPath, cfg); err != nil {
					return "", agentErr(ctx, "save_config_failed", err)
				}
				status := "use"
				if params.Enabled {
					status = "enableuse"
				}
				return agentMsg(ctx, "agent.skill_toggled", params.ID, status), nil
			},
		},
		{
			Name:        "reset_progress",
			Description: "[DANGER - irreversible] Reset all progress, clearing every chapter, outline and foreshadowing. Use only when the user explicitly asks to reset/clear the whole project progress, and always confirm with the user first.",
			Parameters:  `{"confirm": true}`,
			Execute: func(args json.RawMessage, ctx *AgentContext) (string, error) {
				if msg := requireConfirm(ctx, args, fmt.Sprintf("Restablecer TODO el progreso del proyecto (se borrarán %d capítulos, outlines y foreshadows)", len(ctx.State.Chapters))); msg != "" {
					return msg, nil
				}
				if err := story.ResetProgressFiles(ctx.ProgressPath); err != nil {
					return "", agentErr(ctx, "delete_progress_failed", err)
				}
				// Se limpia el estado en memoria; los handlers persisten el progreso vacío automáticamente
				*ctx.State = story.Progress{Phase: "outline"}
				ctx.logger().Success("Progreso restablecido.")
				return agentMsg(ctx, "agent.progress_reset"), nil
			},
		},
	}
}

// ponytail: per-tool-call scratch fields on AgentContext; single-threaded agent loop only.
func (ctx *AgentContext) clearToolMsg() {
	ctx.toolMsgKey = ""
	ctx.toolMsgArgs = nil
}

func (ctx *AgentContext) setToolMsg(key string, args ...any) {
	ctx.toolMsgKey = key
	ctx.toolMsgArgs = i18n.MsgArgs(args...)
}

func (ctx *AgentContext) takeToolMsg() (string, []string) {
	k, a := ctx.toolMsgKey, ctx.toolMsgArgs
	ctx.clearToolMsg()
	return k, a
}

// cfg returns the live project config for this context. When ConfigGetter is
// set (httpapi wiring) it takes precedence so every read sees the freshest
// on-disk config the author just saved through the form; otherwise the plain
// Config snapshot is used (tests/embedded callers). Nil-safe.
func (ctx *AgentContext) cfg() *config.Config {
	if ctx == nil {
		return nil
	}
	if ctx.ConfigGetter != nil {
		if c := ctx.ConfigGetter(); c != nil {
			return c
		}
	}
	return ctx.Config
}

// logger returns the SSE broadcaster or a no-op instance so tool code never
// panics when Logger is unset (tests, early boot).
func (ctx *AgentContext) logger() *sse.LogBroadcaster {
	if ctx == nil || ctx.Logger == nil {
		return sse.NewLogBroadcaster()
	}
	return ctx.Logger
}

// safeCfg is cfg() with a zero-value fallback so callers never dereference nil
// (e.g. in tests or when no config file exists yet).
func (ctx *AgentContext) safeCfg() *config.Config {
	if c := ctx.cfg(); c != nil {
		return c
	}
	return &config.Config{}
}

func projectLang(ctx *AgentContext) string {
	c := ctx.cfg()
	if c == nil {
		return i18n.LangEN
	}
	return i18n.NormalizeLanguage(c.Language)
}

func agentMsg(ctx *AgentContext, key string, args ...any) string {
	ctx.setToolMsg(key, args...)
	return i18n.T(projectLang(ctx), key, args...)
}

func agentErr(ctx *AgentContext, key string, args ...any) error {
	return fmt.Errorf("%s", i18n.T(projectLang(ctx), key, args...))
}
