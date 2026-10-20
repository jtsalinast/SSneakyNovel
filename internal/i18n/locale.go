package i18n

import (
	"fmt"
	"net/http"
	"strings"
)

const (
	LangEN = "en"
)

// NormalizeLanguage canonicalizes English locale variants to "en". Other
// language codes are passed through unchanged so that content filters (e.g.
// skill language matching) can still distinguish them; UI messages always
// use English regardless of the result.
func NormalizeLanguage(lang string) string {
	switch lang {
	case LangEN, "en-US", "en-GB":
		return LangEN
	default:
		return lang
	}
}

// FromRequest extracts the UI locale to use for response messages.
// Priority: X-UI-Locale header > locale query param > Accept-Language > "en".
func FromRequest(r *http.Request) string {
	if r == nil {
		return LangEN
	}
	if v := strings.TrimSpace(r.Header.Get("X-UI-Locale")); v != "" {
		return NormalizeLanguage(v)
	}
	if v := strings.TrimSpace(r.URL.Query().Get("locale")); v != "" {
		return NormalizeLanguage(v)
	}
	if v := strings.TrimSpace(r.Header.Get("Accept-Language")); v != "" {
		first := strings.SplitN(v, ",", 2)[0]
		return NormalizeLanguage(first)
	}
	return LangEN
}

// errorCatalog maps a stable error key to its English message.
// Messages may contain %s for args.
var errorCatalog = map[string]map[string]string{
	"story_idea_required":      {LangEN: "Please fill in the story idea first"},
	"unknown_section":          {LangEN: "Invalid generation section"},
	"generate_failed":          {LangEN: "Generation failed: %v"},
	"backup_failed":            {LangEN: "Project backup failed: %v"},
	"restore_failed":           {LangEN: "Project restore failed: %v"},
	"chapter_load_failed":      {LangEN: "Chapter %d could not be loaded; the project was not opened. Preserve the original file and check it or restore a backup: %s (%v)"},
	"project_load_failed":      {LangEN: "Project loading or save recovery failed; the project was not opened: %s"},
	"setting_has_dependents":   {LangEN: "Other relationships or organizations still reference this setting; review those dependencies first"},
	"ending_invalid":           {LangEN: "Invalid ending options; a custom ending requires instructions"},
	"ending_continue_confirm":  {LangEN: "Confirm continuing: this removes the previous planned final-chapter marker"},
	"fact_impact_confirm":      {LangEN: "This edit affects linked facts; review and confirm with the current content revision"},
	"content_version_conflict": {LangEN: "The chapter changed; refresh and review again"},
	"knowledge_failed":         {LangEN: "Fact or setting synchronization is incomplete; retry"},
	"batch_synopsis_required":  {LangEN: "An outline synopsis is required for this batch"},
	"batch_count_invalid":      {LangEN: "Each batch must contain 1 to 36 chapters"},
	"batch_mode_invalid":       {LangEN: "Invalid batch generation mode"},
	"batch_book_completed":     {LangEN: "Resume the completed book before generating a batch"},
	"batch_chapter_busy":       {LangEN: "Finish the chapter being written or reviewed first"},
	"batch_replace_invalid":    {LangEN: "Only the last entirely unwritten batch can be replanned"},
	"batch_response_invalid":   {LangEN: "The model returned incorrect chapter numbers or count; please retry"},

	"missing_project_name": {

		LangEN: "Project name is required"},
	"project_name_invalid_chars": {

		LangEN: "Project name contains invalid characters"},
	"project_exists": {

		LangEN: "Project already exists"},
	"project_incompatible": {

		LangEN: "This is not a v4 project. Open it with the matching older application version."},
	"create_project_dir_failed": {

		LangEN: "Failed to create project directory: %s"},
	"init_project_config_failed": {

		LangEN: "Failed to initialise project config: %s"},
	"select_project_first": {

		LangEN: "Please select a project first"},
	"task_running_locked": {

		LangEN: "An AI task is running; please wait or stop it before editing"},
	"task_running_wait": {

		LangEN: "A task is running; please wait until it finishes"},
	"no_task_running": {

		LangEN: "No task is currently running"},
	"invalid_json": {

		LangEN: "Invalid JSON: %s"},
	"missing_feedback": {

		LangEN: "feedback field is required"},
	"missing_content": {

		LangEN: "content field is required"},
	"missing_fields": {

		LangEN: "fields array is required"},
	"no_pending_changes": {

		LangEN: "No pending config changes"},
	"load_pending_config_failed": {

		LangEN: "Failed to load pending config changes: %s"},
	"save_pending_config_failed": {

		LangEN: "Failed to save pending config changes: %s"},
	"delete_pending_config_failed": {

		LangEN: "Failed to clear pending config changes: %s"},
	"invalid_chapter_num": {

		LangEN: "Invalid chapter number"},
	"chapter_not_found": {

		LangEN: "Chapter not found"},
	"chapter_n_not_found": {

		LangEN: "Chapter %s not found"},
	"block_not_found": {

		LangEN: "The specified block does not exist"},
	"block_text_required": {

		LangEN: "Block text is required"},
	"phase_not_outline": {

		LangEN: "Not in outline phase"},
	"phase_not_writing": {

		LangEN: "Not in writing phase"},
	"outline_empty": {

		LangEN: "Outline is empty; generate an outline first"},
	"outline_confirm_failed": {

		LangEN: "Failed to confirm outline: %s"},
	"writing_chapter_present": {

		LangEN: "There are chapters in writing/review; finish them before regenerating the outline"},
	"accepted_chapter_present": {

		LangEN: "Confirmed chapters exist; cannot regenerate the full outline. Use \"Generate Continuation Outline\" to append."},
	"writing_chapter_present_delete": {

		LangEN: "There are chapters in writing/review; finish them before deleting the outline"},
	"reset_progress_locked": {

		LangEN: "A task is running; cannot reset progress"},
	"delete_chapter_locked": {

		LangEN: "A task is running; cannot delete chapter"},
	"delete_outline_locked": {

		LangEN: "A task is running; cannot delete outline"},
	"delete_project_locked": {

		LangEN: "A task is running; cannot delete project"},
	"cannot_delete_current_project": {

		LangEN: "Cannot delete the currently active project"},
	"project_not_found": {

		LangEN: "Project not found"},
	"delete_project_failed": {

		LangEN: "Failed to delete project: %s"},
	"delete_progress_failed": {

		LangEN: "Failed to delete progress file: %s"},
	"no_chapters_to_delete": {

		LangEN: "No chapters to delete"},
	"writing_chapter_cannot_delete": {

		LangEN: "Cannot delete a chapter that is being written"},
	"delete_frontier_unavailable": {

		LangEN: "No chapter content at the writing frontier to delete"},
	"writing_range_has_writing": {

		LangEN: "Delete range contains a chapter being written; cannot delete"},
	"save_progress_failed": {

		LangEN: "Failed to save progress: %s"},
	"save_failed": {

		LangEN: "Save failed: %s"},
	"save_config_failed": {

		LangEN: "Failed to save config: %s"},
	"save_api_config_failed": {

		LangEN: "Failed to save API config: %s"},
	"serialize_config_failed": {

		LangEN: "Failed to serialise config: %s"},
	"serialize_api_config_failed": {

		LangEN: "Failed to serialise API config: %s"},
	"api_test_timeout": {

		LangEN: "Connection timed out (15s)"},
	"api_test_failed": {

		LangEN: "Test failed: %s"},
	"api_test_success": {

		LangEN: "Connection succeeded"},
	"character_name_empty": {

		LangEN: "Character name is required"},
	"character_not_found": {

		LangEN: "Character not found"},
	"worldview_field_empty": {

		LangEN: "Name and description are required"},
	"worldview_not_found": {

		LangEN: "Worldview entry not found"},
	"organization_name_empty": {

		LangEN: "Organization name is required"},
	"organization_not_found": {

		LangEN: "Organization not found"},
	"relation_endpoints_empty": {

		LangEN: "Source and target are required"},
	"relation_not_found": {

		LangEN: "Relation not found"},
	"foreshadow_name_required": {

		LangEN: "name field is required"},
	"foreshadow_desc_required": {

		LangEN: "description field is required"},
	"foreshadow_not_found": {

		LangEN: "Foreshadow not found"},
	"invalid_foreshadow_id": {

		LangEN: "Invalid foreshadow id"},
	"need_generate_outline_first": {

		LangEN: "Generate an outline first"},
	"import_project_not_empty": {

		LangEN: "Project already has chapters; import only works in an empty project"},
	"import_nothing_to_resume": {

		LangEN: "No interrupted import to resume"},
	"book_not_complete": {

		LangEN: "Book is not yet complete (all chapters must be confirmed)"},
	"proofread_backup_required": {

		LangEN: "Export the unproofread manuscript and outlines before final proofreading"},
	"proofread_resume_forbidden": {

		LangEN: "This manuscript has entered final proofreading; create a continuation project instead"},
	"proofread_undo_conflict": {

		LangEN: "Cannot undo proofreading: %s"},
	"proofread_issue_not_found": {

		LangEN: "Proofreading issue not found"},
	"need_polish_skill": {

		LangEN: "No polish skill enabled; enable a polish-type skill on the Skills page first"},
	"chapter_content_empty": {

		LangEN: "Chapter content is empty; cannot polish"},
	"chapter_edit_op_required": {

		LangEN: "Missing operation parameter; must be one of: replace_lines / replace_text / insert_after_line / append"},
	"chapter_edit_text_required": {

		LangEN: "new_text must not be empty"},
	"chapter_edit_failed": {

		LangEN: "Chapter edit failed: %s"},
	"chapter_in_writing": {

		LangEN: "Chapter is being written; cannot polish"},
	"chapter_num_required": {

		LangEN: "Chapter number is required"},
	"no_transitions_to_optimize": {

		LangEN: "No transitions to optimise (need at least two adjacent confirmed chapters)"},
	"missing_diagnosis_or_consistency": {

		LangEN: "Diagnosis or consistency report is missing; run book diagnosis first"},
	"no_roadmap_items": {

		LangEN: "No roadmap items to execute"},
	"select_at_least_one_item": {

		LangEN: "Select at least one pending roadmap item"},
	"clear_postprocess_failed": {

		LangEN: "Failed to clear: %s"},
	"chat_session_not_found": {

		LangEN: "Chat session not found"},
	"load_session_list_failed": {

		LangEN: "Failed to load chat sessions: %s"},
	"create_session_failed": {

		LangEN: "Failed to create chat session: %s"},
	"save_session_failed": {

		LangEN: "Failed to save chat session: %s"},
	"delete_session_failed": {

		LangEN: "Failed to delete chat session: %s"},
	"skill_not_found": {

		LangEN: "Skill not found"},
	"skill_install_failed":  {LangEN: "Failed to install skill: %s"},
	"skill_delete_failed":   {LangEN: "Failed to delete skill: %s"},
	"skill_cannot_enable":   {LangEN: "Skill cannot be enabled in validation state: %s"},
	"skill_not_optimizable": {LangEN: "This skill cannot currently be AI-optimized"},
	"invalid_request":       {LangEN: "Invalid request: %s"},
	"settings_ai_generate_moved": {

		LangEN: "This action has moved into the LLM chat; ask the assistant to generate settings for you"},
	"settings_polish_moved": {

		LangEN: "This action has moved into the LLM chat; ask the assistant to polish for you"},
	"writing_conflict_none": {

		LangEN: "No pending writing conflict to resolve"},
	"missing_action": {

		LangEN: "action field is required"},
	"invalid_conflict_chapter_idx": {

		LangEN: "Invalid conflict chapter index"},
	"unsupported_action": {

		LangEN: "Unsupported action: %s"},
	"no_foreshadows_to_check": {

		LangEN: "No foreshadows to check"}}

// systemPrompts maps a stable AI-system-prompt key to per-language text.
// These appear in api calls (CallAPI(ctx, cfg, systemPrompt, userPrompt)) and must
// be language-aware so an English project doesn't get a Chinese system role.
var systemPrompts = map[string]map[string]string{
	"outline_editor_json": {

		LangEN: "You are a professional novel-planning editor. Output strict JSON exactly as requested — no extra prose, no markdown code fences."},
	"outline_editor_locked_json": {

		LangEN: "You are a novel-planning editor. Output strict JSON exactly as requested — no extra prose, no markdown code fences. Locked chapters may not be modified."},
	"outline_editor_brief_json": {

		LangEN: "You are a strict novel-planning editor. Output strict JSON exactly as requested — no extra prose."},
	"summary_analyst": {

		LangEN: "You are a precise novel narrative-state analyst."},
	"fact_checker_json": {

		LangEN: "You are a strict novel fact-checker. Output strict JSON exactly as requested."},
	"narrative_architect_json": {

		LangEN: "You are a senior narrative architect. Output strict JSON exactly as requested — no extra prose, no markdown code fences."},
	"foreshadow_tracker_json": {

		LangEN: "You are a strict novel foreshadow tracker. Output strict JSON exactly as requested — no extra prose, no markdown code fences."},
	"foreshadow_outline_checker_json": {

		LangEN: "You are a strict narrative-consistency editor. Output strict JSON exactly as requested — no extra prose. When unsure, treat as no conflict."},
	"outline_character_checker_json": {

		LangEN: "You are a strict story-settings editor. Output strict JSON exactly as requested — no extra prose. When unsure, treat as no unregistered characters."},
	"writing_conflict_analyst_json": {

		LangEN: "You are a senior novel editor who diagnoses contradictions among outlines, foreshadows, and prior story. Output strict JSON exactly as requested — no extra prose."},
	"consistency_reviewer_json": {

		LangEN: "You are a professional novel-consistency reviewer. Output strict JSON exactly as requested — no extra prose, no markdown code fences."},
	"content_analyst_json": {

		LangEN: "You are a professional novel-analysis editor. Output strict JSON exactly as requested — no extra prose, no markdown code fences."},
	"transition_editor": {

		LangEN: "You are a senior novel editor specialising in chapter-to-chapter transitions. Follow the output instructions strictly."},
	"polish_editor": {

		LangEN: "You are a professional novel-polish editor. Apply the rules strictly and output the full revised chapter prose. No chapter titles, numbers, meta lines like \"End of chapter\", explanations, or markers."},
	"book_diagnosis": {

		LangEN: "You are a senior editor-in-chief specialising in full-novel post-completion review. Output the diagnostic report strictly per the requested format — do not rewrite the prose."},
	"book_consistency_check": {

		LangEN: "You are a strict novel fact-checker. Output a structured consistency report — do not rewrite the prose."},
	"book_roadmap": {

		LangEN: "You are a senior novel editor. Produce an executable revision-roadmap JSON from the reports — do not output rewritten prose."},
	"author_default": {

		LangEN: "You are a novelist. Output story prose only — no chapter titles, numbers, author notes, or meta lines like \"End of chapter\". Keep the specified narrative POV consistent throughout."},
	"chapter_revision_suffix": {

		LangEN: "\nYou are performing a chapter revision: make only the changes the feedback requires; leave everything else identical; output the full revised prose with no meta or explanatory text."},
	"segment_revision_default_feedback": {

		LangEN: "The user quoted this passage but gave no specific instruction. Apply minimal polish to the passage (tighten wording, sharpen sensory detail) and keep all plot beats intact."},
	"memory_manager": {

		LangEN: "You are a precise narrative memory manager. Output strict JSON exactly as requested — no extra prose."},
	"bestseller_chapter_critic_json": {

		LangEN: "You are a world-famous literary editor whose lists have produced multiple global bestsellers, running the final consistency-and-quality pass on one drafted chapter of a novel in progress. You know which act of the book the chapter belongs to and its scene breakdown. Output strict JSON exactly as requested — no extra prose, no markdown code fences."}}

// SystemPromptFor returns the AI system-prompt for the given key; falls back to English.
func SystemPromptFor(lang, key string) string {
	lang = NormalizeLanguage(lang)
	entry, ok := systemPrompts[key]
	if !ok {
		return ""
	}
	if v := entry[lang]; v != "" {
		return v
	}
	return entry[LangEN]
}

func lookupCatalog(lang, key string) (string, bool) {
	lang = NormalizeLanguage(lang)
	for _, catalog := range []map[string]map[string]string{messageCatalog, errorCatalog} {
		entry, ok := catalog[key]
		if !ok {
			continue
		}
		tpl := entry[lang]
		if tpl == "" {
			tpl = entry[LangEN]
		}
		if tpl != "" {
			return tpl, true
		}
	}
	return "", false
}

// T returns a localized message for the given key and args; falls back to English, then key.
func T(lang, key string, args ...any) string {
	tpl, ok := lookupCatalog(lang, key)
	if !ok {
		return key
	}
	if len(args) == 0 {
		return tpl
	}
	return fmt.Sprintf(tpl, args...)
}

func MsgArgs(args ...any) []string {
	if len(args) == 0 {
		return nil
	}
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = fmt.Sprint(a)
	}
	return out
}
