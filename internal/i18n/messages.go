package i18n

// messageCatalog holds localized UI/log/agent status strings (key → English template).
// Templates use fmt.Sprintf verbs (%s, %d, %v). Frontend mirrors keys with {0},{1},… placeholders.
var messageCatalog = map[string]map[string]string{
	"log.history_degraded":           {LangEN: "History summaries are using locally shortened text; generation will be retried on next use."},
	"log.section_generating":         {LangEN: "Generating %s from the novel parameters and story idea..."},
	"log.section_generate_failed":    {LangEN: "Generation failed: %v"},
	"log.section_generate_done":      {LangEN: "%s generated!"},
	"log.preplan_generating":         {LangEN: "Generating book pre-planning from the story structure..."},
	"log.preplan_generate_failed":    {LangEN: "Pre-planning generation failed: %v"},
	"log.preplan_generate_done":      {LangEN: "Pre-planning generated!"},
	"log.section_generate_cancelled": {LangEN: "Generation cancelled"},
	"log.knowledge_failed":           {LangEN: "Fact or setting sync incomplete; retry from Writing: %v"},
	"log.skills_activated":           {LangEN: "Skills activated for this task: %s"},
	"log.skill_validation_done":      {LangEN: "AI validation completed for skill \"%s\""},
	"log.skill_validation_failed":    {LangEN: "Skill AI validation failed: %s"},
	"log.skill_optimization_done":    {LangEN: "Optimized skill copy \"%s\" was created and revalidated"},
	"log.skill_optimization_failed":  {LangEN: "Skill AI optimization failed: %s"},
	// ---- Task / handler logs ----
	"log.autoconfirm_on": {

		LangEN: "Auto-confirm enabled: each chapter is automatically confirmed once generation completes, then the next chapter begins."},
	"log.autoconfirm_off": {

		LangEN: "Auto-confirm disabled"},
	"log.outline_generating": {

		LangEN: "Generating novel outline..."},
	"log.outline_confirmed": {

		LangEN: "Outline confirmed. Entering writing phase."},
	"log.outline_revising": {

		LangEN: "Revising outline based on feedback..."},
	"log.outline_revise_cancelled": {

		LangEN: "Outline revision cancelled"},
	"log.outline_revise_failed": {

		LangEN: "Outline revision failed: %s"},
	"log.outline_revised": {

		LangEN: "Outline revised."},
	"log.outline_analyze_running": {

		LangEN: "Running bestseller-editor analysis over the full outline..."},
	"log.outline_analyze_unchanged": {

		LangEN: "Outline unchanged since the last editorial pass — scores reused, no new analysis needed"},
	"log.outline_analyze_cancelled": {

		LangEN: "Outline analysis cancelled"},
	"log.outline_analyze_failed": {

		LangEN: "Outline analysis failed: %s"},
	"log.outline_analyze_done": {

		LangEN: "Outline analysis complete — %d editorial suggestions"},
	"log.outline_analyze_proposing": {

		LangEN: "Editor is drafting concrete outline replacements for this point..."},
	"log.outline_analyze_proposed": {

		LangEN: "Proposals ready — %d chapter replacement(s) drafted"},
	"log.outline_analysis_applied": {

		LangEN: "Editorial suggestion applied to %d chapter(s)"},
	"log.outline_analysis_cleared": {

		LangEN: "Outline analysis cleared"},
	"log.chapter_writing": {

		LangEN: "Writing chapter %d..."},
	"log.chapter_write_cancelled": {

		LangEN: "Chapter writing cancelled"},
	"log.chapter_write_conflict_pause": {

		LangEN: "Chapter writing paused due to fact-check conflict — choose how to proceed"},
	"log.scene_plan_generating": {

		LangEN: "Chapter %d: planning scenes (beat sheet) before writing..."},
	"log.scene_plan_ready": {

		LangEN: "Scene plan ready for chapter %d: %d scenes, %d words budgeted"},
	"log.scene_plan_pending": {

		LangEN: "Chapter %d: scene plan awaiting your review — edit or approve it, then resume generation"},
	"log.scene_plan_approved": {

		LangEN: "Scene plan approved for chapter %d"},
	"log.scene_plan_auto_approved": {

		LangEN: "Chapter %d: scene plan auto-approved (automation on)"},
	"log.scene_plan_failed": {

		LangEN: "Scene plan generation failed: %v — falling back to single-pass writing"},
	"log.scene_plan_cancelled": {

		LangEN: "Scene plan generation cancelled"},
	// ---- Scene-by-scene sequential writing (writing_scenes.go) ----
	"log.scene_seq_start": {

		LangEN: "[Scenes] Chapter %d: writing scene by scene — %d scenes, target %d words. Each scene is generated and verified against its word budget (±10%) before the next one starts."},
	"log.scene_gen_start": {

		LangEN: "[Scenes] Writing scene %d/%d \"%s\" — budget %d words, accepted range %d–%d words..."},
	"log.scene_gen_failed": {

		LangEN: "[Scenes] Scene %d \"%s\": attempt %d produced no text"},
	"log.scene_call_retry": {

		LangEN: "[Scenes] Scene %d/%d \"%s\": call failed — controlled retry, %d attempt(s) left for this scene..."},
	"log.scene_truncated": {

		LangEN: "[Scenes] Scene %d \"%s\": output was cut off by the token cap at %d words"},
	"log.scene_count_ok": {

		LangEN: "[Scenes] Scene %d \"%s\" ✓ %d words (budget %d, band %d–%d) — moving to the next scene"},
	"log.scene_count_retry": {

		LangEN: "[Scenes] Scene %d/%d \"%s\" is %d words (budget %d, band %d–%d) — %s. Controlled retry %d/%d..."},
	"log.scene_count_keep_best": {

		LangEN: "[Scenes] Scene %d \"%s\" still %d words after %d retries (budget %d, band %d–%d) — keeping closest draft and continuing"},
	"log.scene_done": {

		LangEN: "[Scenes] Scene %d/%d \"%s\" accepted — chapter now at %d words total"},
	"log.scene_seq_fallback": {

		LangEN: "[Scenes] No scene could be written sequentially — falling back to single-pass chapter generation"},
	"log.chapter_total_check": {

		LangEN: "[Length] All scenes written: chapter total is %d WORDS (accepted range %d–%d) — verifying whole chapter..."},
	"log.chapter_total_ok": {

		LangEN: "[Length] Chapter total verified ✓ %d words (range %d–%d)"},
	"log.chapter_total_adjust": {

		LangEN: "[Length] Chapter total %d words is outside the %d–%d range — adjusting the full draft..."},
	"log.chapter_total_final": {

		LangEN: "[Length] After adjustment the chapter is %d words (target range %d–%d) — keeping best available draft"},
	"log.auto_scene_plan_on": {

		LangEN: "Scene-plan automation enabled: plans are generated and accepted automatically"},
	"log.auto_scene_plan_off": {

		LangEN: "Scene-plan automation disabled: you will review each plan before writing"},
	"log.unplanned_cast_detected": {

		LangEN: "Chapter %d: unplanned characters in draft: %s — judging relevance..."},
	"log.unplanned_cast_registered": {

		LangEN: "Chapter %d: registered in cast: %s"},
	"log.unplanned_cast_skipped": {

		LangEN: "Chapter %d: left unregistered (background extras): %s"},
	"log.unplanned_cast_failed": {

		LangEN: "Unplanned-cast registration failed: %v"},
	"log.unplanned_cast_cancelled": {

		LangEN: "Unplanned-cast scan cancelled"},
	"log.chapter_write_failed": {

		LangEN: "Chapter writing failed: %s"},
	"log.chapter_write_done": {

		LangEN: "Chapter %d \"%s\" complete!"},
	"log.chapter_autoconfirm_failed": {

		LangEN: "Auto-confirm failed: %s"},
	"log.chapter_autoconfirmed": {

		LangEN: "Chapter %d \"%s\" auto-confirmed."},
	"log.all_chapters_done": {

		LangEN: "All chapters generated."},
	"log.autowrite_cancelled": {

		LangEN: "Task cancelled; auto-continue stopped"},
	"log.chapter_kept_review": {

		LangEN: "Chapter %d kept as draft and moved to review."},
	"log.chapter_confirmed": {

		LangEN: "Chapter %d confirmed."},
	"log.chapter_revising": {

		LangEN: "Revising current chapter based on feedback..."},
	"log.chapter_revise_cancelled": {

		LangEN: "Chapter revision cancelled"},
	"log.chapter_revise_failed": {

		LangEN: "Chapter revision failed: %s"},
	"log.chapter_revised": {

		LangEN: "Chapter revised."},
	"log.chapter_specific_revising": {

		LangEN: "Targeted revision of chapter %d..."},
	"log.smooth_transitions_cancelled": {

		LangEN: "Transition smoothing cancelled (completed parts are preserved)"},
	"log.smooth_transitions_failed": {

		LangEN: "Transition smoothing failed: %s"},
	"log.chapter_deleted": {

		LangEN: "Deleted chapter %d."},
	"log.outline_deleted": {

		LangEN: "Outline deleted."},
	"log.chapter_outline_updated": {

		LangEN: "Chapter %d outline updated."},
	"log.settings_reconciling": {

		LangEN: "Reconciling new settings with existing content..."},
	"log.settings_reconcile_cancelled": {

		LangEN: "Settings reconciliation cancelled"},
	"log.settings_reconcile_failed": {

		LangEN: "Settings reconciliation failed: %s"},
	"log.settings_reconcile_done": {

		LangEN: "Settings reconciliation complete."},
	"log.delete_file_failed": {

		LangEN: "Failed to delete file %s: %v"},
	"log.chapters_deleted_from": {

		LangEN: "Deleted from chapter %d to end (%d chapters)."},
	"log.foreshadow_roadmap_save_failed": {

		LangEN: "Failed to save foreshadow roadmap: %v"},
	"log.foreshadow_suggesting": {

		LangEN: "Analysing outline and designing foreshadow plan..."},
	"log.foreshadow_suggest_cancelled": {

		LangEN: "Foreshadow suggestions cancelled"},
	"log.foreshadow_suggest_failed": {

		LangEN: "Foreshadow suggestion failed: %s"},
	"log.foreshadow_suggest_done": {

		LangEN: "Foreshadow suggestions ready (%d items)"},
	"log.continuation_outline_generating": {

		LangEN: "Generating continuation outline..."},
	"log.continuation_outline_cancelled": {

		LangEN: "Continuation outline cancelled"},
	"log.continuation_outline_failed": {

		LangEN: "Continuation outline failed: %s"},
	"log.continuation_outline_done": {

		LangEN: "Continuation outline complete."},
	"log.chapter_polish_cancelled": {

		LangEN: "Chapter polish cancelled"},
	"log.chapter_polish_failed": {

		LangEN: "Chapter polish failed: %s"},
	"log.postprocess_diagnose_cancelled": {

		LangEN: "Full-book analysis cancelled"},
	"log.postprocess_diagnose_failed": {

		LangEN: "Full-book analysis failed: %s"},
	"log.postprocess_consistency_cancelled": {

		LangEN: "Consistency check cancelled"},
	"log.postprocess_consistency_failed": {

		LangEN: "Consistency check failed: %s"},
	"log.postprocess_roadmap_cancelled": {

		LangEN: "Roadmap generation cancelled"},
	"log.postprocess_roadmap_failed": {

		LangEN: "Roadmap generation failed: %s"},
	"log.postprocess_execute_cancelled": {

		LangEN: "Optimisation execution cancelled (completed items are preserved)"},
	"log.postprocess_execute_failed": {

		LangEN: "Optimisation execution failed: %s"},
	"log.child_task_start_failed": {

		LangEN: "Cannot start child task %s: parent task already ended"},
	"log.save_session_failed": {

		LangEN: "Failed to save chat session: %v"},
	"log.chat_cancelled": {

		LangEN: "Assistant chat cancelled"},
	"log.chat_failed": {

		LangEN: "Assistant reply failed: %v"},
	"log.agent_output_truncated": {

		LangEN: "Assistant output truncated at max_tokens=%d — tool call aborted"},
	"log.agent_tool_call_parse_retry": {

		LangEN: "Tool-call parse failed (finish_reason=%s, %d chars) — asking model to diagnose and retry once"},
	"log.agent_tool_call_parse_failed": {

		LangEN: "Tool-call parse failed again after retry — aborted"},
	"log.chat_done": {

		LangEN: "Assistant reply complete."},
	"log.project_deleted": {

		LangEN: "Project \"%s\" deleted"},
	"log.project_created": {

		LangEN: "Project \"%s\" created"},

	// ---- Writing pipeline logs ----
	"log.chapter_start": {

		LangEN: "Starting chapter %d: \"%s\""},
	"log.outline_check_failed": {

		LangEN: "Outline consistency check failed: %v (continuing with original outline)"},
	"log.outline_auto_revised": {

		LangEN: "Chapter outline auto-revised to match current story"},
	"log.outline_consistent": {

		LangEN: "Chapter outline consistent with story ✓"},
	"log.chapter_length_target": {

		LangEN: "[Length] Chapter %d target: %d WORDS (accepted range %d–%d words; source: %s) — measured in words, not characters"},
	"log.prose_done": {

		LangEN: "Prose complete — %d words"},
	"log.chapter_length_retry": {

		LangEN: "[Length] Draft is %d words, need %d–%d — regenerating (attempt %d)..."},
	"log.chapter_length_retry_truncated": {

		LangEN: "[Length] Draft was cut off by the output token cap at %d words (need %d–%d) — continuing the remaining scenes (attempt %d). Consider raising max_tokens in the API config."},
	"log.chapter_length_continue": {

		LangEN: "[Length] Continuation appended — chapter now at %d words (round %d)"},
	"log.chapter_length_continue_failed": {

		LangEN: "[Length] Continuation round %d failed: %v — keeping partial draft"},
	"log.chapter_length_continue_duplicate": {

		LangEN: "[Length] Continuation round %d restated already-written scenes — stopping continuation to avoid duplicates"},
	"log.chapter_length_adjust": {

		LangEN: "[Length] Still %d words after rewrite (need %d–%d) — compressing/expanding draft..."},
	"log.chapter_length_adjust_failed": {

		LangEN: "[Length] Compress/expand failed: %v — keeping best draft"},
	"log.chapter_length_soft_keep": {

		LangEN: "[Length] Best draft %d words is slightly outside %d–%d — within soft tolerance, skipping adjust"},
	"log.chapter_length_skip_adjust": {

		LangEN: "[Length] Best draft %d words too far from %d–%d — skipping adjust"},
	"log.chapter_length_adjust_reverted": {

		LangEN: "[Length] After adjust %d words not better than best %d (need %d–%d) — keeping best draft"},
	"log.chapter_length_off_range": {

		LangEN: "[Length] Still %d words after adjust (need %d–%d) — please review manually; auto-confirm will continue to the next chapter"},
	"log.chapter_revision_short_retry": {

		LangEN: "[Length] Revision is only %d words (need %d–%d) — retrying rewrite keeping all original scenes intact..."},
	"log.chapter_revision_reverted": {

		LangEN: "[Length] Revision (%d words) still shorter than original (%d words, need %d–%d) — keeping the original text"},
	"log.summary_done": {

		LangEN: "Summary extraction complete"},
	"log.factcheck_retry": {

		LangEN: "[Fact-check] Issues found — regenerating chapter %d (retry %d)..."},
	"log.factcheck_details": {

		LangEN: "Check details: %s"},
	"log.factcheck_max_retries": {

		LangEN: "[Fact-check] Max retries reached — analysing conflict root cause..."},
	"log.conflict_analyze_failed": {

		LangEN: "Conflict analysis failed: %v — keeping current version"},
	"log.conflict_retry": {

		LangEN: "Resolvable conflict detected — final attempt with extra constraints..."},
	"log.factcheck_constraint_pass": {

		LangEN: "[Fact-check] Constrained retry passed ✓"},
	"log.factcheck_pass": {

		LangEN: "[Fact-check] Passed ✓"},
	"log.chapter_write_complete": {

		LangEN: "Chapter %d writing complete!"},
	"log.outline_conflict": {

		LangEN: "Chapter %d outline conflicts with story: %s"},
	"log.outline_revision_rejected": {

		LangEN: "Proposed outline revision for chapter %d was rejected (it dropped detail or named entities); keeping the approved outline"},
	"log.chapter_modifying": {

		LangEN: "Revising chapter %d \"%s\"..."},
	"log.prose_revised": {

		LangEN: "Revision complete — %d words"},
	"log.subsequent_outline_failed": {

		LangEN: "Subsequent outline revision failed: %v (current chapter unaffected)"},
	"log.subsequent_outline_done": {

		LangEN: "Subsequent outline revision complete"},
	"log.chapter_specific_revising_long": {

		LangEN: "Targeted revision of chapter %d \"%s\" (other chapters untouched)..."},
	"log.prose_specific_revised": {

		LangEN: "Targeted revision complete — %d words"},
	"log.block_revising": {

		LangEN: "Revising chapter %d, block #%d..."},
	"log.block_revised": {

		LangEN: "Chapter %d block #%d revised"},
	"log.chapter_segment_revising": {

		LangEN: "Detected %d quoted passage(s) — revising only the matched paragraph(s)..."},
	"log.chapter_segment_fallback": {

		LangEN: "Local revision unavailable (quoted passage not located or paragraph count mismatch) — falling back to full-chapter revision"},
	"log.chapter_specific_done": {

		LangEN: "Chapter %d targeted revision complete (other chapters untouched)."},
	"log.fatal_no_retry": {

		LangEN: "Fatal error: %v — not retrying"},
	"log.content_gen_retry": {

		LangEN: "Prose generation failed: %v. Retry %d, waiting %ds..."},
	"log.summary_retry": {

		LangEN: "Summary extraction failed: %v. Retry %d, waiting %ds..."},
	"log.factcheck_api_retry": {

		LangEN: "Fact-check failed: %v. Retry %d, waiting %ds..."},
	"log.smooth_start": {

		LangEN: "Starting transition smoothing — %d chapters to check"},
	"log.smooth_natural": {

		LangEN: "Chapter %d transition is smooth — no change needed"},
	"log.smooth_optimized": {

		LangEN: "Chapter %d opening optimised and saved"},
	"log.smooth_done": {

		LangEN: "Transition smoothing complete: checked %d, optimised %d"},
	"log.outline_revise_summary": {

		LangEN: "Outline revised — %d chapters"},
	"log.reconcile_pending_outline_failed": {

		LangEN: "Failed to regenerate pending outlines: %v (settings updated)"},
	"log.reconcile_done_explain": {

		LangEN: "Settings reconciliation complete. %s"},
	"log.continuation_outline_summary": {

		LangEN: "Continuation outline complete — added %d chapters (total %d)"},
	"log.foreshadow_outline_check_failed": {

		LangEN: "Foreshadow/outline consistency check failed: %v"},
	"log.foreshadow_outline_report_save_failed": {

		LangEN: "Failed to save foreshadow/outline check report: %v"},
	"log.foreshadow_outline_check_pass": {

		LangEN: "Foreshadow/outline consistency check passed ✓"},
	"log.outline_chapters_too_short": {

		LangEN: "Chapter %s outline(s) under %d characters — asking AI to expand…"},
	"log.outline_chapters_still_short": {

		LangEN: "Chapter %s outline(s) still under %d characters after retry — please expand on the Outline page"},
	"log.outline_character_check_failed": {

		LangEN: "Outline character check failed: %v"},
	"log.outline_postprocess_skipped": {

		LangEN: "Post-batch consistency checks skipped (state snapshot failed): %v"},
	"log.outline_postprocess_panic": {

		LangEN: "Post-batch consistency check crashed and was recovered: %v"},
	"log.outline_character_report_save_failed": {

		LangEN: "Failed to save outline character check report: %v"},
	"log.outline_character_check_pass": {

		LangEN: "Outline characters match registered list ✓"},
	"log.foreshadow_plan_parsed": {

		LangEN: "Foreshadow plan parsed — %d items"},
	"log.foreshadow_status_updated": {

		LangEN: "Foreshadow status updated — %d changes"},
	"log.foreshadow_sync_failed": {

		LangEN: "Foreshadow sync failed: %v (chapter unaffected)"},
	"log.foreshadow_sync_summary": {

		LangEN: "Foreshadow status updated (active: %d, resolved: %d)"},
	"log.memory_update_failed": {

		LangEN: "Narrative memory update failed (chapter unaffected)"},
	"log.memory_save_failed": {

		LangEN: "Failed to save narrative memory"},
	"log.import_split_done": {

		LangEN: "Local split done: %d chapters, starting per-chapter analysis"},
	"log.import_resuming": {

		LangEN: "Resuming import from checkpoint: %d / %d chapters done"},
	"log.import_meta_done": {

		LangEN: "Metadata analysis done: \"%s\""},
	"log.import_meta_failed": {

		LangEN: "Metadata analysis failed: %v (chapter processing unaffected)"},
	"log.import_chapter_done": {

		LangEN: "Chapter %d / %d analyzed"},
	"log.import_cancelled": {

		LangEN: "Import paused (%d / %d chapters done, resume anytime)"},
	"log.import_done": {

		LangEN: "Import complete: %d chapters, ready for continuation"},
	"log.import_task_cancelled": {

		LangEN: "Import task cancelled (progress saved, resumable)"},
	"log.import_task_failed": {

		LangEN: "Import task failed: %s (progress saved, resumable)"},
	"log.postprocess_material": {

		LangEN: "Book material: ~%d chars, ~%d tokens, diagnosis mode: %s"},
	"log.postprocess_consistency_single": {

		LangEN: "Starting full-book consistency check (single volume)..."},
	"log.postprocess_consistency_multi": {

		LangEN: "Long text — consistency check split into %d volumes..."},
	"log.postprocess_roadmap_items": {

		LangEN: "Generated %d optimisation roadmap items"},
	"log.postprocess_smooth_preface": {

		LangEN: "Preface: optimising chapter transitions..."},
	"log.postprocess_smooth_skip": {

		LangEN: "Transition smoothing skipped or failed: %v"},
	"log.postprocess_batch_failed": {

		LangEN: "Chapter %d roadmap batch failed: %v"},
	"log.postprocess_batch_done": {

		LangEN: "Chapter %d done (merged %d items)"},
	"log.postprocess_batch_skip": {

		LangEN: "Chapter %d unchanged — skipped"},
	"log.postprocess_execute_summary": {

		LangEN: "Optimisation execution complete: %d chapters (%d items), %d modified"},
	"log.api_fatal": {

		LangEN: "Fatal error: %v — not retrying"},
	"log.api_retry": {

		LangEN: "API call failed: %v. Retry %d, waiting %ds..."},
	"log.api_stream_retry": {

		LangEN: "Streaming API call failed: %v. Retry %d, waiting %ds..."},

	// ---- Agent tool status messages ----
	"agent.task_cancelled": {

		LangEN: "Task cancelled"},
	"agent.api_failed": {

		LangEN: "Agent API call failed: %v"},
	"agent.output_truncated": {

		LangEN: "Assistant output was cut off at the max_tokens limit (%d). The tool call did not complete. Increase max_tokens on the Config page, shorten your message, then click Retry to resend."},
	"agent.tool_call_parse_retry_hint": {

		LangEN: "[Tool-call parse failed — retry once]\nReason tag: %s; Agent max_tokens=%d.\nYour previous <tool_call> could not be parsed (common causes: missing </tool_call>, truncated/incomplete JSON, oversized arguments, bad escapes).\nDiagnose the previous output, then retry with exactly one complete <tool_call>...</tool_call> containing a valid JSON object.\nIf writing_style or another field is long, split it into multiple update_project_config calls with few fields each.\nDo not explain — emit the tool call only."},
	"agent.tool_call_parse_failed": {

		LangEN: "Assistant tool call was invalid and still could not be parsed after one retry. Shorten arguments (split long fields across calls), or click Retry to resend."},
	"agent.max_steps": {

		LangEN: "Reached the maximum tool-call step limit."},
	"agent.section_generate_started": {

		LangEN: "Background generation for \"%s\" has started; related data refreshes automatically when it finishes. Tell the user to watch the progress indicator."},
	"agent.tool_exec_error": {

		LangEN: "Tool execution error: %v"},
	"agent.unknown_tool": {

		LangEN: "Unknown tool: %s"},
	"agent.confirm_required": {

		LangEN: "⚠️ Not executed: \"%s\" is irreversible. Restate the impact to the user and get explicit consent, then retry with confirm=true. If the user meant to edit content, use the revise tool instead."},
	"agent.no_characters": {

		LangEN: "No character data"},
	"agent.characters_not_found": {

		LangEN: "No matching characters found"},
	"agent.character_not_found": {

		LangEN: "Character not found: %s"},
	"agent.no_worldview": {

		LangEN: "No worldview data"},
	"agent.worldview_not_found": {

		LangEN: "No matching worldview entries"},
	"agent.no_organizations": {

		LangEN: "No organization data"},
	"agent.chapter_not_found": {

		LangEN: "Chapter %d not found"},
	"agent.no_outline": {

		LangEN: "No outline yet"},
	"agent.no_foreshadows": {

		LangEN: "No foreshadows yet"},
	"agent.search_keyword_required": {

		LangEN: "Search keyword required"},
	"agent.search_no_results": {

		LangEN: "No matching content found"},
	"agent.character_created": {

		LangEN: "Character \"%s\" created (ID: %s)"},
	"agent.character_updated": {

		LangEN: "Character \"%s\" updated"},
	"agent.character_deleted": {

		LangEN: "Character \"%s\" deleted"},
	"agent.worldview_created": {

		LangEN: "Worldview entry \"%s\" created (ID: %s)"},
	"agent.worldview_updated": {

		LangEN: "Worldview entry \"%s\" updated"},
	"agent.worldview_deleted": {

		LangEN: "Worldview entry \"%s\" deleted"},
	"agent.config_saved_reconciling": {

		LangEN: "Story config saved — reconciling existing content..."},
	"agent.config_saved": {

		LangEN: "Story config saved"},
	"agent.outline_task_started": {

		LangEN: "Outline generation started — please wait."},
	"agent.outline_confirmed": {

		LangEN: "Outline confirmed — entering writing phase."},
	"agent.outline_revise_started": {

		LangEN: "Outline revision started — please wait."},
	"agent.outline_deleted": {

		LangEN: "Outline deleted."},
	"agent.chapter_outline_updated": {

		LangEN: "Chapter %d outline updated."},
	"agent.chapter_task_started": {

		LangEN: "Chapter %d generation started — please wait."},
	"agent.chapter_confirmed": {

		LangEN: "Chapter %d \"%s\" confirmed."},
	"agent.chapter_revise_started": {

		LangEN: "Chapter %d revision started (this chapter only) — please wait."},
	"agent.chapter_deleted": {

		LangEN: "Deleted chapter %d."},
	"agent.chapter_content_edited": {

		LangEN: "Chapter %d content edited (op: %s, %d lines total)."},
	"agent.chapter_edit_op_required": {

		LangEN: "Missing operation parameter; must be one of: replace_lines / replace_text / insert_after_line / append"},
	"agent.chapter_edit_text_required": {

		LangEN: "new_text must not be empty"},
	"agent.chapters_bulk_delete_confirm": {

		LangEN: "⚠️ Not executed: this would permanently delete chapters %d through end (%d chapters). Restate the impact and get consent, then retry with confirm=true. To edit content, use revise_chapter."},
	"agent.chapters_deleted_from": {

		LangEN: "Deleted from chapter %d to end (%d chapters)."},
	"agent.organization_created": {

		LangEN: "Organization \"%s\" created (ID: %s)"},
	"agent.organization_updated": {

		LangEN: "Organization \"%s\" updated"},
	"agent.organization_deleted": {

		LangEN: "Organization \"%s\" deleted"},
	"agent.organization_not_found": {

		LangEN: "Organization not found: %s"},
	"agent.relation_created": {

		LangEN: "Relation created (ID: %s)"},
	"agent.relation_updated": {

		LangEN: "Relation updated (ID: %s)"},
	"agent.relation_deleted": {

		LangEN: "Relation deleted"},
	"agent.relation_not_found": {

		LangEN: "Relation not found: %s"},
	"agent.foreshadow_suggest_started": {

		LangEN: "Foreshadow suggestion task started — please wait."},
	"agent.foreshadow_created": {

		LangEN: "Foreshadow \"%s\" created (ID: %d)"},
	"agent.foreshadow_updated": {

		LangEN: "Foreshadow \"%s\" updated"},
	"agent.foreshadow_deleted": {

		LangEN: "Foreshadow \"%s\" deleted"},
	"agent.foreshadow_not_found": {

		LangEN: "Foreshadow %d not found"},
	"agent.skill_toggled": {

		LangEN: "Skill \"%s\" %s"},
	"agent.progress_reset": {

		LangEN: "Progress reset."}}
