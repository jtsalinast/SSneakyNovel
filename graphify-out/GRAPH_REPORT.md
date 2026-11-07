# Graph Report - Ultima version buena copy  (2026-11-05)

## Corpus Check
- 297 files · ~465,501 words
- Verdict: corpus is large enough that graph structure adds value.
- Unclassified: 14 file(s) not represented in the graph (top: (none) 10, .command 1, .log 1)

## Summary
- 2791 nodes · 13198 edges · 123 communities (113 shown, 10 thin omitted)
- Extraction: 91% EXTRACTED · 9% INFERRED · 0% AMBIGUOUS · INFERRED: 1242 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `f743f7a9`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- agent.go
- index.js
- context.Context
- Progress
- testing.T
- RunAgentLoop
- Config.svelte
- beta_readers.go
- api
- net/http.Request
- GenreCascadeFields.svelte
- stores.js
- Handlers
- CountProseUnits
- LogBroadcaster
- SaveProgress
- SmartCombobox.svelte
- NormalizeLanguage
- Outline.svelte
- CallAPIStreamMessages
- mapArcIssueInfos
- .writeJSON
- NewLogBroadcaster
- .GetNovelParams
- SyncPendingKnowledge
- sse.check.js
- subgenreTaxonomy.js
- .PostChatMessage
- StoryConfig
- getBuiltinTools
- SaveConfig
- .blockEditChapter
- playwright
- applySettingDeltas
- InstallSkillFiles
- BrainstormPanel.svelte
- ComboSelect.svelte
- novelwriterGenres.check.js
- Handlers
- NewHandlers
- package.json
- CheckOutlineCharacterConsistency
- ExtractJSON
- saveProgressUnlocked
- ChapterState
- reviseOutline
- StartWebServer
- PillarAssessment
- syncSceneBlocks
- .brainstormPrompts
- applyChapterMemoryResult
- SyncChapterBlocks
- ScenePlan
- AsSaveError
- ChapterRevision
- .runPrePlanGenerateWith
- finalizePillarReport
- splitBlockParagraphs
- normalizeOutlineCharacters
- newContinuationFixture
- Design System: SSSNovelStudio
- User guide
- screenshots.mjs
- brainstorm_handlers_test.go
- classifyTrope
- splitTropesFromParas
- Core Writing Craft (English)
- SetScenePlanScenes
- WritingConflict
- Product
- .runSectionGenerate
- forceGraphLayout.check.js
- proseUnits.js
- .generateDifferentiatingIdea
- SuggestionCriterion
- SSSNovelStudio
- Skill
- BetaReaderCard.svelte
- .generateStoryIdeaFromParams
- buildCharacterQuestsPrompt
- 网文去AI味检测流程
- Story AI-Fingerprint Audit (English)
- SegmentedControl.svelte
- .generateCharacters
- locateSceneSuggestion
- ApplyScenePlanSuggestion
- AGENTS.md — AI 小说写手项目指南
- devDependencies
- SplitImportContent
- 核心流程
- 核心写作技法
- locateFactIssues
- splitTropes
- e2e_field_parity.mjs
- scripts
- newRecoveryState
- English De-AI Polish Rules
- 中文去AI味规则
- 2. Your first story: recommended order
- 5. Add story knowledge and correct errors
- dependencies
- tooltip.js
- candidateChapterJSONs
- apiUrl.js
- optionalDependencies
- .PostEnhanceText
- TestSubgenreSettingTokens_NotSharedAcrossParents
- ScenePlanPendingError
- extractBalanced
- parseMemoryUpdateResult
- PromptsConfig
- 1. Installation and API setup
- 7. Import and continuation
- parseUnplannedCastVerdicts
- ChapterLoadError
- projectCompatibilityError
- nextOrganizationIDFor
- nextRelationIDFor
- LogBroadcaster
- contextBudgetError
- outputLimitError
- cfg
- showmethestory

## God Nodes (most connected - your core abstractions)
1. `Progress` - 250 edges
2. `LogBroadcaster` - 152 edges
3. `Config` - 135 edges
4. `Handlers` - 128 edges
5. `APIConfig` - 120 edges
6. `ProjectSettings` - 119 edges
7. `SaveProgress()` - 104 edges
8. `NormalizeLanguage()` - 95 edges
9. `ChapterState` - 77 edges
10. `api()` - 73 edges

## Surprising Connections (you probably didn't know these)
- `前端约定` --references--> `api()`  [INFERRED]
  AGENTS.md → frontend/src/lib/api.js
- `前端约定` --references--> `apiFetch()`  [INFERRED]
  AGENTS.md → frontend/src/lib/api.js
- `LLM、提示词与 Skill` --references--> `resolveChatCompletionsURL()`  [INFERRED]
  AGENTS.md → frontend/src/lib/apiUrl.js
- `criterionLabel()` --calls--> `$t()`  [INFERRED]
  frontend/src/pages/Outline.svelte → frontend/src/pages/Writing.svelte
- `priorityLabel()` --calls--> `$t()`  [INFERRED]
  frontend/src/pages/Outline.svelte → frontend/src/pages/Writing.svelte

## Import Cycles
- None detected.

## Communities (123 total, 10 thin omitted)

### Community 0 - "agent.go"
Cohesion: 0.21
Nodes (51): go_pkg_archive_zip, go_pkg_bufio, go_pkg_bytes, go_pkg_context, go_pkg_crypto_sha256, go_pkg_embed, go_pkg_encoding_hex, go_pkg_encoding_json (+43 more)

### Community 1 - "index.js"
Cohesion: 0.04
Nodes (38): 前端约定, docs_sssnovelstudio, backToProjects(), closeAssistant(), closeNavigation(), goTo(), handleShellKeydown(), i() (+30 more)

### Community 2 - "context.Context"
Cohesion: 0.09
Nodes (97): context.Context, APIConfig, Config, RenderPrompt(), optimizeSkillWithAI(), validateSkillWithAI(), SystemPromptFor(), CallAPI() (+89 more)

### Community 3 - "Progress"
Cohesion: 0.05
Nodes (89): ActAnalysisReport, pillarContractPromptBlockEN(), latestBetaReport(), AnalyzeChapterPlan(), buildChapterPlanEditorPrompt(), buildChapterPlanEditorPromptForPillar(), fallbackPointCategory(), fallbackPointTextEN() (+81 more)

### Community 4 - "testing.T"
Cohesion: 0.03
Nodes (83): testing.T, calcSynopsisLengthRange(), TestParseGenerateSectionTagCreativityIsNumber(), TestParseGenerateSectionTagEnhanceField(), ParseGenerateSectionTag(), parseGenerateSectionTag(), setTagArg(), TestCalcSynopsisLengthRange() (+75 more)

### Community 5 - "RunAgentLoop"
Cohesion: 0.05
Nodes (64): AgentContext, Tool, encoding/json.RawMessage, net/http.HandlerFunc, agentEffectiveMaxTokens(), agentErr(), agentMsg(), buildAgentSystemPrompt() (+56 more)

### Community 6 - "Config.svelte"
Cohesion: 0.04
Nodes (45): applyGeneratedStory(), charFormSnapshotNow(), closeCharForm(), closeOrgForm(), closeRelForm(), closeWvForm(), discardUndo(), entitySnap() (+37 more)

### Community 7 - "beta_readers.go"
Cohesion: 0.08
Nodes (68): strings.Builder, actNameOf(), betaActMaterial(), betaAverages(), betaChaptersByNum(), betaConsolidationPrompt(), betaEmit(), betaEvaluationPrompt() (+60 more)

### Community 8 - "api"
Cohesion: 0.09
Nodes (68): sendToChat(), watchSendToChat(), api(), addToast(), showConfirm(), captureSnapshot(), captureStoryFields(), deleteCharacter() (+60 more)

### Community 9 - "net/http.Request"
Cohesion: 0.08
Nodes (17): net/http.Request, net/http.ResponseWriter, Handlers, Handlers, Handlers, Handlers, Handlers, Handlers (+9 more)

### Community 10 - "GenreCascadeFields.svelte"
Cohesion: 0.05
Nodes (58): applyConflictSelection(), applyFreeSettings(), applyLengthChange(), applyProtagSelection(), applyStructureChange(), checked(), clearLabel, CONFLICT_FAMILIES (+50 more)

### Community 11 - "stores.js"
Cohesion: 0.06
Nodes (57): locLabel(), translate(), brainstormUsage, clearChatBuf(), connectSSE(), taskLabel(), toolLogLabel(), doRefreshProgress() (+49 more)

### Community 12 - "Handlers"
Cohesion: 0.10
Nodes (10): context.CancelFunc, sync.RWMutex, time.Duration, time.Time, chapterProgress(), Handlers, NewTaskContext(), ImportStatePath() (+2 more)

### Community 13 - "CountProseUnits"
Cohesion: 0.07
Nodes (53): CountProseUnits(), isCJK(), isLatinAlnum(), isTokenGlue(), isWhitespace(), normalizeWideAlnum(), AuthorGuidanceBlock(), buildOutlineConstraintsForLang() (+45 more)

### Community 14 - "LogBroadcaster"
Cohesion: 0.08
Nodes (7): NewTaskTokenUsage(), newTaskTokenUsage(), WithTaskTokens(), Format(), LogBroadcaster, LogEntry, SSEMessage

### Community 15 - "SaveProgress"
Cohesion: 0.09
Nodes (13): Handlers, scenePlanErrCode(), Handlers, CompletedActForChapter(), DismissChapterIntervention(), FindChapterIdx(), ConfirmOutlineAction(), EditScenePlanRecommendation() (+5 more)

### Community 16 - "SmartCombobox.svelte"
Cohesion: 0.07
Nodes (38): addCustomItem(), addPlaceholder, allGroups, ariaLabel, clearAll(), closeDropdownOnly(), collapsed, customGroupLabel (+30 more)

### Community 17 - "NormalizeLanguage"
Cohesion: 0.08
Nodes (43): SuggestedChaptersByLength(), NormalizeLanguage(), buildAlreadyIntroducedBlock(), buildHardDebutBlock(), buildPrematureEventLines(), collectDebutedNames(), draftedCastNames(), futureDebuts() (+35 more)

### Community 18 - "Outline.svelte"
Cohesion: 0.07
Nodes (27): has(), STRUCTURE_ORDER, suggestStructure(), clearPrePlanBusy(), closeWeb(), criterionLabel(), #each(), focusChapterFromSession() (+19 more)

### Community 19 - "CallAPIStreamMessages"
Cohesion: 0.08
Nodes (35): net/http.Client, sync.Mutex, CallAPIMessages(), CallAPIMessagesSync(), CallAPIStreamMessages(), completionText(), FetchModelContextWindow(), Message (+27 more)

### Community 20 - "mapArcIssueInfos"
Cohesion: 0.08
Nodes (40): containsWordCI(), detectUnplannedCastChars(), sortUnplannedByFirstMention(), TestDetectUnplannedCastChars(), titleCaseSpan(), unplannedIsGenericEntity(), unplannedNameCores(), arcAllChapterRefs() (+32 more)

### Community 21 - ".writeJSON"
Cohesion: 0.15
Nodes (6): validCharacterEnum(), RegisterCastCharacters(), RegisteredCharacterNameSet(), NormalizeAliases(), SaveProjectSettings(), StripNameMarks()

### Community 22 - "NewLogBroadcaster"
Cohesion: 0.14
Nodes (38): DefaultConfigForLang(), NewLogBroadcaster(), TestAnalyzeFullOutlineRunsEditorialSequence(), TestForeshadowReportSaveFailurePreservesReportAndDiagnostics(), TestOutlineBatchPromptCarriesGuidance(), TestScenePlanPromptCarriesGuidance(), acceptedHistory(), TestNarrativeCheckpointBoundariesAndHierarchy() (+30 more)

### Community 23 - ".GetNovelParams"
Cohesion: 0.11
Nodes (34): tooltipCatalog, TooltipEntry, EffectiveOutputLanguage(), buildAnimeModifierPresets(), buildCanonicalSubgenreParents(), buildDramaSubgenrePresets(), buildSubgenreLabelPresets(), CanonicalParentForSubgenre() (+26 more)

### Community 24 - "SyncPendingKnowledge"
Cohesion: 0.11
Nodes (35): testing.B, TestRetrieveSettingsMatchesAliases(), TestWorldbuildingAestheticBlock(), BrainstormLoreContext(), memoryHasLiveReference(), SyncChapterMemory(), buildChapterSettingsContexts(), buildCharacterContextForLang() (+27 more)

### Community 25 - "sse.check.js"
Cohesion: 0.08
Nodes (25): groups, groupLogEntries(), logGroupKey(), calls, context, downloads, script, source (+17 more)

### Community 26 - "subgenreTaxonomy.js"
Cohesion: 0.13
Nodes (28): applyGenreSelection(), applySubSelection(), labelToKey(), onGenreChange(), onSmartGenreChange(), onSmartSubChange(), onSubChange(), buildSubgenreIndex() (+20 more)

### Community 27 - ".PostChatMessage"
Cohesion: 0.10
Nodes (23): showmethestory/internal/agent.ToolCall, AgentStep, Log(), forcedSectionCreativity(), forcedSectionEnhance(), forcedSectionEnhanceField(), saveAgentSteps(), skillScopeForTask() (+15 more)

### Community 28 - "StoryConfig"
Cohesion: 0.11
Nodes (21): ContentGuidance(), effectiveMulti(), StoryConfig, splitMultiField(), TestEnhancePromptHasNoCreativityLevel(), TestParseStoryFieldText(), TestStoryEnhanceFieldRoundTrip(), buildFieldEnhancePrompt() (+13 more)

### Community 29 - "getBuiltinTools"
Cohesion: 0.11
Nodes (24): getBuiltinTools(), Delete(), appendPendingChanges(), FormatConfigConflictMessage(), LoadPendingConfigChanges(), mergePending(), PendingConfigChangesPath(), RemovePendingFields() (+16 more)

### Community 30 - "SaveConfig"
Cohesion: 0.11
Nodes (16): archive/zip.Reader, DefaultTargetWordsForLength(), LoadConfig(), SaveConfig(), TestLegacyBriefMigratesToStoryIdea(), extractProjectBackup(), Handlers, validateBackupProject() (+8 more)

### Community 31 - ".blockEditChapter"
Cohesion: 0.10
Nodes (18): chapterRevisionRequest, Handlers, parseChapterBlockIDs(), FromRequest(), lookupCatalog(), T(), EnsureContextBudget(), WithBrainstormTokens() (+10 more)

### Community 32 - "playwright"
Cohesion: 0.13
Nodes (16): MIME, server, srv, types, srv, types, srv, types (+8 more)

### Community 33 - "applySettingDeltas"
Cohesion: 0.15
Nodes (25): TestSettingSyncAddsAliases(), TestSettingSyncAliasNameDoesNotRename(), ReferenceLive(), entityAliasList(), TestKnowledgeRetrievalLargeBook(), TestLongSettingExcerptPreservesOriginal(), TestRetrievedSettingsCollisionAndPendingDedup(), factChapter() (+17 more)

### Community 34 - "InstallSkillFiles"
Cohesion: 0.14
Nodes (25): DeleteGlobalSkill(), FilesFromSkillZip(), InstallSkillFiles(), LoadGlobalSkills(), loadRegistry(), loadSkillPackage(), normalizeSkillFiles(), normalizeSkillRoot() (+17 more)

### Community 35 - "BrainstormPanel.svelte"
Cohesion: 0.20
Nodes (19): applyIntent(), backHome(), callPayload(), communeKey(), defaultMode(), growComposer(), loadEntities(), newRequest() (+11 more)

### Community 36 - "ComboSelect.svelte"
Cohesion: 0.20
Nodes (23): afterEditClosed(), cancelEdit(), clearAll(), close(), commitEdit(), dispatchChange(), displayOf(), inSelf() (+15 more)

### Community 37 - "novelwriterGenres.check.js"
Cohesion: 0.13
Nodes (21): canonicalSubgenreLabel(), all, buildIndex(), canonicalParentFor(), fa, IDX, mt, normSub() (+13 more)

### Community 38 - "Handlers"
Cohesion: 0.28
Nodes (5): TestWriteFileAtomicReplacesExistingFile(), WriteFileAtomic(), Handlers, jsonRule(), storyParamsContext()

### Community 39 - "NewHandlers"
Cohesion: 0.12
Nodes (23): DefaultConfig(), TestProjectBackupRestoreRoundtrip(), TestProjectRestoreRejectsUnsafeOrInvalidArchive(), TestContinuationProjectCopiesPlanningWithoutProofreadProse(), TestOutlineExportIncludesEveryChapter(), TestAutomaticForeshadowCheckEarlyReturnsReleaseTask(), TestForeshadowChecksReserveTaskAndCanBeCancelled(), NewHandlers() (+15 more)

### Community 40 - "package.json"
Cohesion: 0.11
Nodes (16): name, private, type, version, daisyui, dompurify, marked, @rollup/rollup-darwin-arm64 (+8 more)

### Community 41 - "CheckOutlineCharacterConsistency"
Cohesion: 0.13
Nodes (21): applyForeshadowOutlineReport(), applyForeshadowUpdates(), buildAcceptedSummariesText(), buildFullOutlineText(), CheckForeshadowOutlineConsistency(), ForeshadowStatusLabel(), formatForeshadowsForPromptLang(), computeOutlinePostProcessReports() (+13 more)

### Community 42 - "ExtractJSON"
Cohesion: 0.16
Nodes (20): ExtractJSON(), TestArcDebug11(), betaCallJSON(), editorialOverallCall(), editorialPillarCall(), buildChapterCriticPrompt(), buildCriticSceneMapBlock(), enFB() (+12 more)

### Community 43 - "saveProgressUnlocked"
Cohesion: 0.17
Nodes (19): TestBlocksPersistRoundtrip(), saveProgressUnlocked(), cacheFor(), chapterFilePath(), chaptersDir(), cleanupChapterFiles(), commitProgressFiles(), prepareChapterFiles() (+11 more)

### Community 44 - "ChapterState"
Cohesion: 0.18
Nodes (19): ChapterAnalysisReport, bodyTexts(), SheetBlock, SetChapterSheetBlocks(), locateTropeUnitInContent(), ChapterState, containsWordWindow(), FindParaOffsetOfBlock() (+11 more)

### Community 45 - "reviseOutline"
Cohesion: 0.16
Nodes (19): applyOutlineMetaWithGuard(), ApplySelectedPendingChanges(), storyConfigFromOutline(), SyncProgressMetaFromStory(), applyOutlineRevision(), generateOutlineChaptersOnly(), buildOutlinePromptExtras(), calcOutlineLengthRange() (+11 more)

### Community 46 - "StartWebServer"
Cohesion: 0.14
Nodes (18): io/fs.FS, net/http.Handler, DefaultAPIConfig(), LoadAPIConfig(), saveAPIConfig(), appendLineLocked(), Enabled(), Init() (+10 more)

### Community 47 - "PillarAssessment"
Cohesion: 0.13
Nodes (11): countOpenDraftedSuggestions(), ActAnalysisReport, ChapterAnalysisReport, renderPreviousChecklistBlock(), contains(), TestRenderPreviousChecklistBlock(), renderPreviousPillarChecklist(), ActAnalysisReport (+3 more)

### Community 48 - "syncSceneBlocks"
Cohesion: 0.17
Nodes (18): absInt(), applyDerivedSceneSpans(), budgetWalkSceneSpans(), hasMeaningfulSceneMeta(), recordedSceneParaSpans(), remapScenePositions(), sceneDisplayTitle(), sceneMetaCollapsed() (+10 more)

### Community 49 - ".brainstormPrompts"
Cohesion: 0.17
Nodes (16): brainstormMessage, brainstormRequest, brainstormDimensionLabel(), brainstormExplainInstruction(), brainstormRewriteInstruction(), brainstormSenseLabel(), brainstormSystem(), brainstormVariantAngle() (+8 more)

### Community 50 - "applyChapterMemoryResult"
Cohesion: 0.15
Nodes (17): applyChapterMemoryResult(), dedupeMemoryIDs(), factContentEquivalent(), factNegationFlipped(), factNegationMarkers(), factTokenSet(), intersectTokenCount(), normalizeFactText() (+9 more)

### Community 51 - "SyncChapterBlocks"
Cohesion: 0.23
Nodes (16): TestDbgBlocks(), classifyBlock(), classifyTropeBlock(), DeleteBlock(), FindBlockIdx(), InsertBlockAfter(), looksLikeTropeRun(), rebuildContentFromBlocks() (+8 more)

### Community 52 - "ScenePlan"
Cohesion: 0.21
Nodes (16): applyTropeSegmentation(), buildTropeUnits(), retileSceneTropeUnits(), sceneRangeForBlock(), TestTropesOfSceneCachesOnScene(), tropesOfScene(), perSceneWordCounts(), remainingScenesFromPlan() (+8 more)

### Community 53 - "AsSaveError"
Cohesion: 0.15
Nodes (11): fileOps, SaveError, os.File, os.FileInfo, AsSaveError(), TestWriteFileAtomicFallsBackToBackupSwap(), TestWriteFileAtomicKeepsBackupWhenRestoreFails(), TestWriteFileAtomicRestoresOriginalWhenInstallFails() (+3 more)

### Community 54 - "ChapterRevision"
Cohesion: 0.19
Nodes (13): factHandlers(), Handlers, TestFactEditHTTPGuards(), TestKnowledgeQueryReturnsEvidenceAndPending(), TestRejectedBatchDoesNotRunPendingKnowledge(), TestSelectedSettingsRevision(), TestSettingRevisionEndpointsRejectStaleSelection(), TestSettingsViewDoesNotShipProvenance() (+5 more)

### Community 55 - ".runPrePlanGenerateWith"
Cohesion: 0.16
Nodes (10): DefaultStructureForLength(), NormalizeOutputLanguage(), OutputLanguageName(), structureLabel(), Handlers, TestCharacterQuestsBlockEmpty(), TestCharacterQuestsBlockRendersThreadsOnlyForQuestOwners(), CharacterQuestsBlock() (+2 more)

### Community 56 - "finalizePillarReport"
Cohesion: 0.16
Nodes (14): criterionNameSet(), deriveScoreFromCriteria(), deriveSuggestionPriority(), finalizePillarReport(), normalizeSuggestionPillar(), PillarChecklist(), pillarForCategory(), sanitizeSuggestionCriteria() (+6 more)

### Community 57 - "splitBlockParagraphs"
Cohesion: 0.30
Nodes (14): sceneTropeUnitSpans(), splitBlockParagraphs(), ChapterInterventionLocator(), fallbackTropeMapBlock(), liveTropeUnitIndex(), locateSubsceneUnit(), realignMatch(), RealignPendingInterventions() (+6 more)

### Community 58 - "normalizeOutlineCharacters"
Cohesion: 0.19
Nodes (14): TestCastSummaryAndSanitization(), chapterStateFromOutline(), characterStubsForChapter(), formatCharactersLine(), outlineRuneLen(), stubsFromStructuredCharacters(), TestCharacterStubsUseStructuredCast(), TestNormalizeOutlineCharacters() (+6 more)

### Community 59 - "newContinuationFixture"
Cohesion: 0.32
Nodes (14): newContinuationFixture(), streamAPI(), TestContinueTruncatedChapterAppendsRemainingScenes(), TestContinueTruncatedChapterStopsOnDegenerateSegment(), TestContinueTruncatedChapterStopsOnDuplicateSegment(), TestContinueTruncatedChapterStopsWhenAllScenesCovered(), TestSceneSequentialFallsBackWithoutPlan(), TestSceneSequentialHappyPath() (+6 more)

### Community 60 - "Design System: SSSNovelStudio"
Cohesion: 0.17
Nodes (11): Colors, Components, Design System: SSSNovelStudio, Do:, Do's and Don'ts, Don't:, Elevation & Depth, Layout (+3 more)

### Community 61 - "User guide"
Cohesion: 0.17
Nodes (12): 10. Saving, backups, and migration, 11. Troubleshooting, 3. Batch planning and endings, 4. Review, editing, and revision, 6. Facts, settings, and foreshadowing, 8. Completion, proofreading, and exports, 9. Skills and the assistant, Assistant (+4 more)

### Community 62 - "screenshots.mjs"
Cohesion: 0.17
Nodes (7): frontendDir, outputDir, server, stories, ref_node_child_process, ref_node_path, ref_node_url

### Community 63 - "brainstorm_handlers_test.go"
Cohesion: 0.27
Nodes (11): explanationPreview(), brainstormTestHandler(), Handlers, TestBrainstormCommuneNeedsEntity(), TestBrainstormExplainSchema(), TestBrainstormExplainSingleWordAndLanguage(), TestBrainstormFinalStyleLock(), TestBrainstormPreservesVoiceAndLanguage() (+3 more)

### Community 64 - "classifyTrope"
Cohesion: 0.21
Nodes (12): sameBlocks(), Block, groupsOverlapTropeChange(), sceneTropeLabels(), setBlockUnit(), setBlockUnitIdx(), tropeFallbackGroups(), unitIdxForPara() (+4 more)

### Community 65 - "splitTropesFromParas"
Cohesion: 0.20
Nodes (12): breakSceneSpans(), ensureFallbackScenePlan(), fallbackSceneTiling(), mergeShortSpans(), TestFirstSubsceneChipSurvivesUniformScene(), groupParasIntoBeats(), localSepOf(), paragraphCountWithSep() (+4 more)

### Community 66 - "Core Writing Craft (English)"
Cohesion: 0.17
Nodes (11): 13 chapter-ending hooks, 7 chapter-opening hooks, Character voice, Continuity rules, Core Writing Craft (English), Dialogue economy, Pacing control by scene type, Per-chapter shape (default; adapt freely) (+3 more)

### Community 67 - "SetScenePlanScenes"
Cohesion: 0.21
Nodes (12): hasCliffhangerTag(), hasHookTag(), normalizeScenePlan(), SetScenePlanScenes(), sumBudgets(), TestNormalizeScenePlanAllEmptyReturnsNil(), TestNormalizeScenePlanDropsEmptyScenes(), TestNormalizeScenePlanEvenSplitFallback() (+4 more)

### Community 68 - "WritingConflict"
Cohesion: 0.21
Nodes (11): attachConflictSceneTargets(), ensureSurgicalAction(), finalizeConflict(), isLLMConfigReason(), isTimeoutReason(), preserveSurgicalHistory(), RecordConflictFailure(), ConflictActionOption (+3 more)

### Community 69 - "Product"
Cohesion: 0.17
Nodes (11): Accessibility & Inclusion, Brand Commitments, Capabilities and Constraints, Evidence on Hand, Operating Context, Platform, Positioning, Product (+3 more)

### Community 70 - ".runSectionGenerate"
Cohesion: 0.29
Nodes (5): SectionGenRequest, IsWorldviewCategorySection(), WorldviewCategoryBySection(), sectionLabel(), worldviewCategoryForSection()

### Community 71 - "forceGraphLayout.check.js"
Cohesion: 0.25
Nodes (9): cameraFrame(), camera, empty, fit, p32, p8, fitTransform(), kineticEnergy() (+1 more)

### Community 72 - "proseUnits.js"
Cohesion: 0.33
Nodes (10): CJK_PUNCT, countProseUnits(), isBreakPunct(), isCJK(), isCJKPunct(), isLatinAlnum(), isLatinBreakPunct(), isTokenGlue() (+2 more)

### Community 73 - ".generateDifferentiatingIdea"
Cohesion: 0.24
Nodes (8): CreativityLevelLabel(), CreativityTemperature(), TestCreativityPlaybooksPerLevel(), TestDifferentiatingPromptUsesLevelPlaybook(), buildDifferentiatingPrompt(), buildEnhancePrompt(), creativityIdeaPlaybook(), parseDifferentiatingIdea()

### Community 74 - "SuggestionCriterion"
Cohesion: 0.18
Nodes (8): countOpenSuggestions(), OutlineAnalysisReport, renderSuggestionContractBlock(), OutlineAnalysisReport, FinalizableSuggestion, OutlineRecommendation, OutlineSuggestion, SuggestionCriterion

### Community 75 - "SSSNovelStudio"
Cohesion: 0.20
Nodes (7): Development, Features, Find the right workflow, First run, License, Running and storing data, SSSNovelStudio

### Community 76 - "Skill"
Cohesion: 0.31
Nodes (9): SkillConfig, FilterSkillsByLang(), GetEnabledSkills(), GetEnabledSkillsByCategory(), Skill, LoadAllSkills(), LoadBuiltinSkills(), MergeSkills() (+1 more)

### Community 77 - "BetaReaderCard.svelte"
Cohesion: 0.27
Nodes (4): archetypeLabel(), kindLabel(), sessionLabel(), $t()

### Community 78 - ".generateStoryIdeaFromParams"
Cohesion: 0.29
Nodes (7): AudienceLabel(), GoreLevelLabel(), RomanceLevelLabel(), SexualContentLabel(), WorldDarknessLabel(), SplitSubgenreList(), lengthLabelEN()

### Community 79 - "buildCharacterQuestsPrompt"
Cohesion: 0.22
Nodes (10): buildCharacterQuestsPrompt(), nonEmptyStrings(), TestCharacterQuestsPromptCarriesSheetAndSchema(), TestMergeCharactersFillsArcQuartet(), mergeCharacters(), nextCharacterIDFor(), TestNormalizeCharacterQuestsClearsAndReassigns(), Character (+2 more)

### Community 80 - "网文去AI味检测流程"
Cohesion: 0.20
Nodes (9): Gate 1: 词汇密度检测, Gate 2: 句式单一检测, Gate 3: 情感表达检测, Gate 4: 对话自然度检测, Gate 5: 节奏检测, Gate 6: 重复模式检测, 检测报告模板, 真人写作基准 (+1 more)

### Community 81 - "Story AI-Fingerprint Audit (English)"
Cohesion: 0.20
Nodes (9): Audit report template, Gate 1 — Slop-word density, Gate 2 — Cliché phrasing, Gate 3 — Em-dash and triadic-list audit, Gate 4 — Voice differentiation, Gate 5 — Emotion-by-body, Gate 6 — Pacing, hook, and scene rhythm, Human-writer benchmark (+1 more)

### Community 82 - "SegmentedControl.svelte"
Cohesion: 0.28
Nodes (4): inSelf(), onDocClick(), onDocMouseDown(), tr()

### Community 83 - ".generateCharacters"
Cohesion: 0.25
Nodes (8): MatchGenreKey(), GenreCharacterExtraFields(), MatchAnimeModifierKey(), splitSubgenreList(), SubgenreCharacterExtraFields(), TestCharacterFieldsFallbackToParent(), TestMatchGenreKeyNewGens(), TestAnimeModifierPresets()

### Community 84 - "locateSceneSuggestion"
Cohesion: 0.25
Nodes (7): countOpenSceneSuggestions(), ScenePlanAnalysisReport, findSceneSuggestion(), ScenePlanAnalysisReport, locateSceneSuggestion(), ScenePlanRecommendation, SceneSuggestion

### Community 85 - "ApplyScenePlanSuggestion"
Cohesion: 0.31
Nodes (9): ScenePlanAnalysisReport, newMergeTestState(), scenePlanTestRep(), TestApplyScenePlanSuggestion_InvalidMergeTargetDegradesToReplacement(), TestApplyScenePlanSuggestion_RealMergeDeletesAbsorbedScene(), TestEditScenePlanRecommendation_CancelMerge(), ApplyScenePlanSuggestion(), findChapterForMerge() (+1 more)

### Community 86 - "AGENTS.md — AI 小说写手项目指南"
Cohesion: 0.25
Nodes (8): AGENTS.md — AI 小说写手项目指南, graphify, 修改检查清单, 常用命令, 持久化与兼容边界, 架构边界, 测试与精简规则, 项目概览

### Community 87 - "devDependencies"
Cohesion: 0.25
Nodes (8): devDependencies, daisyui, playwright, svelte, @sveltejs/vite-plugin-svelte, tailwindcss, @tailwindcss/vite, vite

### Community 88 - "SplitImportContent"
Cohesion: 0.29
Nodes (8): BuildImportPreview(), chunkBySize(), SplitImportContent(), TestBuildImportPreview(), TestSplitImportContentHeadings(), TestSplitImportContentNoHeadings(), ImportChapterPreview, importedChapter

### Community 89 - "核心流程"
Cohesion: 0.29
Nodes (7): 写作、事实与设定, 导入与完稿校订, 异步任务, 批次大纲与结尾, 核心流程, 畅销书编辑批评（大纲 / 场景计划 / 已成稿章节与幕）, 自动 AI Beta 读者团（成稿幕）

### Community 90 - "核心写作技法"
Cohesion: 0.29
Nodes (6): 核心写作技法, 每章结构参考, 爽点密度规则, 章尾钩子十三式, 章首钩子七式, 节奏控制参考

### Community 91 - "locateFactIssues"
Cohesion: 0.29
Nodes (7): locateFactIssues(), mergeCriticQuotesIntoIssues(), scenePosFromIssue(), extractQuotedExcerpt(), CriticIssue, CriticVerdict, FactLoc

### Community 92 - "splitTropes"
Cohesion: 0.29
Nodes (7): joinWithSepForTest(), TestSplitTropesUniformLongSceneSegments(), paragraphCount(), splitTropes(), TestSplitTropesHardBreakSeparatesUnits(), TestSplitTropesMergesTinyTrailingUnit(), TestSplitTropesSegmentsByModeShifts()

### Community 93 - "e2e_field_parity.mjs"
Cohesion: 0.33
Nodes (4): KEYS, MIME, PROPS, server

### Community 94 - "scripts"
Cohesion: 0.33
Nodes (6): scripts, build, check, dev, preview, screenshots

### Community 95 - "newRecoveryState"
Cohesion: 0.53
Nodes (6): newRecoveryState(), newTestLogger(), TestAbortChapterWithDraftCheckpointKeepsProse(), TestResetChapterActionClearsDraftAndPersists(), TestResetChapterActionGuards(), ResetChapterAction()

### Community 96 - "English De-AI Polish Rules"
Cohesion: 0.33
Nodes (5): 1. Forbidden patterns (rewrite every occurrence), 2. High-frequency phrase swap table, 3. Style rules, 4. Format rules, English De-AI Polish Rules

### Community 97 - "中文去AI味规则"
Cohesion: 0.33
Nodes (5): 一、禁止模式（必须消除）, 三、口语化规则, 中文去AI味规则, 二、高频短语替换表, 四、格式规范

### Community 98 - "2. Your first story: recommended order"
Cohesion: 0.40
Nodes (5): 2. Your first story: recommended order, Step 1: Establish essential settings, Step 2: Plan a small first batch, Step 3: Generate and review one chapter, Step 4: Continue and reassess

### Community 99 - "5. Add story knowledge and correct errors"
Cohesion: 0.40
Nodes (5): 5. Add story knowledge and correct errors, Add or edit knowledge in Configuration first, Scope and limitations, The shortest correction workflow, Where knowledge belongs

### Community 100 - "dependencies"
Cohesion: 0.40
Nodes (5): dependencies, dompurify, marked, @types/node, @xianii/design-system

### Community 101 - "tooltip.js"
Cohesion: 0.70
Nodes (4): ensureTip(), hideTip(), showTip(), tooltip()

### Community 102 - "candidateChapterJSONs"
Cohesion: 0.40
Nodes (5): candidateChapterJSONs(), matchBrace(), repairTruncatedJSON(), trimToLastCompleteElement(), TestCandidateChapterJSONs()

### Community 103 - "apiUrl.js"
Cohesion: 0.67
Nodes (3): LLM、提示词与 Skill, hasAPIVersionSegment(), resolveChatCompletionsURL()

### Community 104 - "optionalDependencies"
Cohesion: 0.50
Nodes (4): optionalDependencies, @rollup/rollup-darwin-arm64, @rollup/rollup-darwin-x64, @rollup/rollup-linux-x64-gnu

### Community 105 - ".PostEnhanceText"
Cohesion: 0.67
Nodes (3): TestTextEnhancePromptAndParse(), buildTextEnhancePrompt(), parseEnhancedText()

### Community 106 - "TestSubgenreSettingTokens_NotSharedAcrossParents"
Cohesion: 0.67
Nodes (4): equalFoldToken(), knownParentKey(), TestSubgenreSettingTokens_NotSharedAcrossParents(), toLowerSnake()

### Community 107 - "ScenePlanPendingError"
Cohesion: 0.50
Nodes (3): asScenePlanPending(), ScenePlanPendingError, asScenePlanPending()

### Community 108 - "extractBalanced"
Cohesion: 0.50
Nodes (4): endsInString(), extractBalanced(), ExtractJSONArray(), WalkJSONStructure()

### Community 109 - "parseMemoryUpdateResult"
Cohesion: 0.67
Nodes (4): parseMemoryUpdateResult(), memoryNewEntry, memoryUpdateEntry, memoryUpdateResult

### Community 111 - "1. Installation and API setup"
Cohesion: 0.67
Nodes (3): 1. Installation and API setup, Connect a model, Launch and projects

### Community 112 - "7. Import and continuation"
Cohesion: 0.67
Nodes (3): 7. Import and continuation, Continue a completed work, Continue existing text

### Community 113 - "parseUnplannedCastVerdicts"
Cohesion: 0.67
Nodes (3): parseUnplannedCastVerdicts(), TestParseUnplannedCastVerdicts(), unplannedCastVerdict

## Knowledge Gaps
- **262 isolated node(s):** `MIME`, `server`, `PROPS`, `KEYS`, `MIME` (+257 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 424 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **10 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Handlers` connect `Handlers` to `agent.go`, `context.Context`, `Progress`, `NewHandlers`, `net/http.Request`, `Skill`, `LogBroadcaster`, `SaveProgress`, `CallAPIStreamMessages`, `.writeJSON`, `.PostChatMessage`, `StoryConfig`, `getBuiltinTools`, `SaveConfig`, `.blockEditChapter`?**
  _High betweenness centrality (0.051) - this node is a cross-community bridge._
- **Why does `Progress` connect `Progress` to `agent.go`, `context.Context`, `testing.T`, `RunAgentLoop`, `beta_readers.go`, `net/http.Request`, `Handlers`, `CountProseUnits`, `SaveProgress`, `NormalizeLanguage`, `mapArcIssueInfos`, `NewLogBroadcaster`, `SyncPendingKnowledge`, `StoryConfig`, `getBuiltinTools`, `SaveConfig`, `.blockEditChapter`, `applySettingDeltas`, `CheckOutlineCharacterConsistency`, `ExtractJSON`, `saveProgressUnlocked`, `ChapterState`, `reviseOutline`, `syncSceneBlocks`, `.brainstormPrompts`, `applyChapterMemoryResult`, `ChapterRevision`, `splitBlockParagraphs`, `newContinuationFixture`, `SetScenePlanScenes`, `WritingConflict`, `locateSceneSuggestion`, `ApplyScenePlanSuggestion`, `locateFactIssues`, `newRecoveryState`?**
  _High betweenness centrality (0.039) - this node is a cross-community bridge._
- **Why does `api()` connect `api` to `index.js`, `BrainstormPanel.svelte`, `Config.svelte`, `stores.js`, `Outline.svelte`?**
  _High betweenness centrality (0.020) - this node is a cross-community bridge._
- **What connects `MIME`, `server`, `PROPS` to the rest of the system?**
  _262 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `index.js` be split into smaller, more focused modules?**
  _Cohesion score 0.03710575139146568 - nodes in this community are weakly interconnected._
- **Should `context.Context` be split into smaller, more focused modules?**
  _Cohesion score 0.08815484956869346 - nodes in this community are weakly interconnected._
- **Should `Progress` be split into smaller, more focused modules?**
  _Cohesion score 0.0528555431131019 - nodes in this community are weakly interconnected._