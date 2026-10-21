<script>
  import { onMount, tick } from 'svelte';
  import { api, apiFetch } from '../lib/api.js';
  import { config, progress, taskRunning, streamingContent, streamingChapterIdx, selectedChapter, autoConfirm, addToast, confirmModal } from '../lib/stores.js';
  import { navigate } from '../lib/router.js';
  import { t } from '../lib/i18n/index.js';
  import { countProseUnits } from '../lib/proseUnits.js';
  import TaskTokenBadge from '../components/TaskTokenBadge.svelte';
  import KnowledgePanel from '../components/KnowledgePanel.svelte';
  import InfoTip from '../lib/components/InfoTip.svelte';
  import ScenePlanEditor from '../components/ScenePlanEditor.svelte';
  import BestsellerCritiqueCard from '../components/BestsellerCritiqueCard.svelte';
  import { settings } from '../lib/stores.js';
  let showKnowledge = false;
  let knowledgeName = '', knowledgeDescription = '', knowledgeTags = '';
  let selectedSettings = [];
  let savingKnowledge = false;
  async function saveKnowledge(revise = false) {
    if (!knowledgeName.trim() || !knowledgeDescription.trim() || savingKnowledge) return;
    savingKnowledge = true;
    const target = ch?.num;
    try {
      const entry = await api('POST', '/api/worldview', {name: knowledgeName.trim(), description: knowledgeDescription.trim(), tags: knowledgeTags.trim(), category: 'knowledge'});
      settings.update(s => ({...s, worldview: [...(s?.worldview || []).filter(w => w.id !== entry.id), entry]}));
      knowledgeName = ''; knowledgeDescription = ''; knowledgeTags = '';
      if (ch?.num === target) selectedSettings = [...new Set([...selectedSettings, entry.id])];
      addToast($t('writing.knowledge.saved'), 'success');
      if (revise && ch?.num === target) await doRevise();
    } catch (e) { addToast(e.message, 'error'); }
    finally { savingKnowledge = false; }
  }
  let facts = [];
  let activeFact = null;
  let knowledgeExpanded = false;
  let returnRef = null;
  let highlightedBlock = null;
  let selectedBlockId = null;
  $: factsByBlock = new Map(chapterBlocks.map(b => [b.id, facts.filter(f => (f.references || []).some(r => r.chapter === ch?.num && r.block_id === b.id))]));
  const blockFacts = id => factsByBlock.get(id) || [];
  function confirmAction(message, action) {
    confirmModal.set({ message, onConfirm: action });
  }
  function selectBlock(id) {
    if (editingBlockId != null || revisingBlockId != null || insertAfterId != null) return;
    highlightedBlock = null;
    selectedBlockId = selectedBlockId === id ? null : id;
  }
  async function jumpToFact(ref, returning = false) {
    if (editingBlockId != null || revisingBlockId != null || insertAfterId != null) {
      confirmAction($t('facts.discard'), () => { cancelBlockOps(); jumpToFact(ref, returning); });
      return;
    }
    const idx = chapters.findIndex(c => c.num === ref.chapter);
    if (idx < 0) { addToast($t('facts.stale'), 'warning'); return; }
    if (!returning && !returnRef) returnRef = {chapter: ch?.num, block_id: highlightedBlock || chapterBlocks[0]?.id};
    try {
      const full = await api('GET', '/api/chapters/' + ref.chapter);
      const block = (full.blocks || []).find(b => b.id === ref.block_id);
      if (!block || (ref.quote && block.text !== ref.quote)) { addToast($t('facts.stale'), 'warning'); return; }
      selectedChapter.set(idx);
      loadedNum = ref.chapter;
      applyChapter(ref.chapter, full);
      highlightedBlock = ref.block_id;
      await tick();
      document.getElementById('story-block-' + ref.block_id)?.scrollIntoView({block: 'center', behavior: 'smooth'});
      if (returning) returnRef = null;
    } catch(e) { addToast(e.message, 'error'); }
  }
  function factHeaders(confirmed) {
    return {'X-Content-Rev': loadedRev || '', 'X-Confirm-Fact-Impact': String(confirmed)};
  }

  const OUTLINE_FOCUS_KEY = 'showmethestory.outlineFocusChapter';

  onMount(async () => {
    try {
      const res = await api('GET', '/api/autoconfirm');
      autoConfirm.set(!!res.enabled);
    } catch (e) {}
    try {
      const sk = await api('GET', '/api/skills');
      hasPolishSkills = (sk || []).some(s => s.enabled && s.skill?.category === 'polish');
    } catch (e) {}
    try {
      // Pre-planning act cards live in their own endpoint; the act-critic row
      // needs them to offer per-act manual analysis.
      const pp = await api('GET', '/api/outline/preplan');
      preplanActs = (pp?.acts || []).map((a, i) => ({
        no: i + 1, name: a.name || '', start_ch: a.start_ch || 0, end_ch: a.end_ch || 0,
      }));
    } catch (e) {}
  });

  async function toggleAutoConfirm(e) {
    const enabled = e.target.checked;
    try {
      const res = await api('PUT', '/api/autoconfirm', { enabled });
      autoConfirm.set(!!res.enabled);
      addToast(res.enabled ? $t('writing.toasts.autoConfirmOn') : $t('writing.toasts.autoConfirmOff'), 'info');
    } catch (err) {
      e.target.checked = $autoConfirm;
      addToast(err.message, 'error');
    }
  }

  $: p = $progress;
  $: inWriting = p?.phase === 'writing';
  $: chapters = p?.chapters || [];
  $: projectChapters = chapters.filter(c => !c.inherited);
  $: total = projectChapters.length;
  $: accepted = projectChapters.filter(c => c.status === 'accepted').length;
  $: pct = total > 0 ? Math.round(accepted / total * 100) : 0;
  $: currentIdx = p?.current_chapter_index ?? 0;

  // Select current chapter by default
  $: if (inWriting && ($selectedChapter < 0 || $selectedChapter >= chapters.length)) {
    selectedChapter.set(Math.min(currentIdx, chapters.length - 1));
  }

  // In auto-confirm mode, follow the chapter being generated
  $: if ($autoConfirm && $streamingChapterIdx >= 0 && $streamingChapterIdx < chapters.length && $streamingChapterIdx !== $selectedChapter) {
    selectedChapter.set($streamingChapterIdx);
  }

  $: ch = $selectedChapter >= 0 && $selectedChapter < chapters.length ? chapters[$selectedChapter] : null;
  $: isCurrent = ch && currentIdx === $selectedChapter;
  $: isStreamingThis = $streamingChapterIdx === $selectedChapter && $streamingContent;

  // /api/progress does not carry prose; fetch selected chapter content on demand，content_rev refresh on change
  let chapterContent = '';
  let chapterBlocks = [];
  let loadedNum = -1;
  let loadedRev = null;
  $: if (ch) maybeLoadContent(ch);
  function applyChapter(num, full) {
    if (loadedNum !== num) return;
    chapterContent = full.content || '';
    chapterBlocks = full.blocks || [];
    loadedRev = full.content_rev || '';
  }
  async function maybeLoadContent(c) {
    const rev = c.content_rev || '';
    if (c.num === loadedNum && rev === loadedRev) return;
    if (c.num !== loadedNum) selectedBlockId = null;
    loadedNum = c.num;
    loadedRev = rev;
    if (!rev) { chapterContent = ''; chapterBlocks = []; return; }
    try {
      const full = await api('GET', '/api/chapters/' + c.num);
      applyChapter(c.num, full);
    } catch (e) {}
  }
  $: hasContent = !!(ch?.content_rev);

  // —— Block Block editing ——
  let editingBlockId = null;   // block being edited inline
  let editingText = '';
  let revisingBlockId = null;  // block whose AI revision feedback is being typed
  let blockFeedback = '';
  let insertAfterId = null;    // id of the block after which a new block is inserted (0 = start)

  // —— Scene marker chips (trope-granular story blocks) ——————————————
  // The backend tags every block with its owning scene (scene_pos /
  // scene_title) and trope unit (trope_label / trope_index / trope_count).
  // Between two consecutive blocks whose scene changes we render an almost
  // invisible chip: very light grey background, near-transparent white text,
  // not selectable. On hover it reveals an ⓘ icon (same popover style as the
  // SmartSelect/SmartCombobox summary tips) describing the incoming scene.
  function sceneMarkerBefore(blocks, i) {
    if (!blocks || i <= 0 || i >= blocks.length) return null;
    const prev = blocks[i - 1], cur = blocks[i];
    const p = prev.scene_pos ?? -1, c = cur.scene_pos ?? -1;
    if (c < 0 || p === c) return null; // no bookkeeping or same scene
    return { pos: c + 1, title: cur.scene_title || '', label: cur.trope_label || '', idx: cur.trope_index || 0, count: cur.trope_count || 0 };
  }
  function sceneMarkerTip(m) {
    if (!m) return '';
    let s = $t('writing.blockSceneMarker', [m.pos]) + '\n\n' + (m.title || '');
    if (m.label && m.count > 0) s += '\n' + $t('writing.blockTropeHint', [m.label, m.idx, m.count]);
    return s.trim();
  }
  let insertText = '';

  function startBlockEdit(b) {
    selectedBlockId = b.id;
    editingBlockId = b.id;
    editingText = b.text;
    revisingBlockId = null;
    insertAfterId = null;
  }
  function startBlockRevise(b) {
    selectedBlockId = b.id;
    revisingBlockId = b.id;
    blockFeedback = '';
    editingBlockId = null;
    insertAfterId = null;
  }
  function startBlockInsert(afterId) {
    insertAfterId = afterId;
    insertText = '';
    editingBlockId = null;
    revisingBlockId = null;
  }
  function cancelBlockOps() {
    editingBlockId = null;
    revisingBlockId = null;
    insertAfterId = null;
  }

  async function saveBlockEdit(confirmed = false) {
    if (editingBlockId == null || !editingText.trim() || !ch) return;
    if (confirmed !== true && blockFacts(editingBlockId).length) {
      confirmAction($t('facts.confirm') + '\n' + blockFacts(editingBlockId).map(f => f.content).join('\n'), () => saveBlockEdit(true)); return;
    }
    try {
      const full = await api('PUT', `/api/chapters/${ch.num}/blocks/${editingBlockId}`, { text: editingText }, factHeaders(confirmed === true));
      applyChapter(ch.num, full);
      cancelBlockOps();
      addToast($t('writing.block.saved'), 'success');
    } catch (e) { addToast(e.message, 'error'); }
  }

  function deleteBlock(b) {
    if (!ch) return;
    confirmModal.set({
      message: $t('writing.block.deleteConfirm') + (blockFacts(b.id).length ? '\n' + $t('facts.confirm') + '\n' + blockFacts(b.id).map(f => f.content).join('\n') : ''),
      onConfirm: async () => {
        try {
          const full = await api('DELETE', `/api/chapters/${ch.num}/blocks/${b.id}`, null, factHeaders(true));
          applyChapter(ch.num, full);
          addToast($t('writing.block.deleted'), 'success');
        } catch (e) { addToast(e.message, 'error'); }
      },
    });
  }

  async function saveBlockInsert() {
    if (insertAfterId == null || !insertText.trim() || !ch) return;
    try {
      const full = await api('POST', `/api/chapters/${ch.num}/blocks`, { after_id: insertAfterId, text: insertText }, factHeaders(false));
      applyChapter(ch.num, full);
      cancelBlockOps();
      addToast($t('writing.block.inserted'), 'success');
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function submitBlockRevise(confirmed = false) {
    if (revisingBlockId == null || !blockFeedback.trim() || !ch) return;
    if (confirmed !== true && blockFacts(revisingBlockId).length) {
      confirmAction($t('facts.confirmAI') + '\n' + blockFacts(revisingBlockId).map(f => f.content).join('\n'), () => submitBlockRevise(true)); return;
    }
    try {
      await api('POST', `/api/chapters/${ch.num}/blocks/${revisingBlockId}/revise`, { feedback: blockFeedback }, factHeaders(confirmed === true));
      addToast($t('writing.block.reviseStarted'), 'info');
      cancelBlockOps();
    } catch (e) { addToast(e.message, 'error'); }
  }

  // During streaming $streamingContent only holds the tail window (perf guard); full text is fetched after generation ends
  $: displayContent = isStreamingThis ? $streamingContent : chapterContent;
  $: chapterWordCount = ch?.word_count || (chapterContent ? countProseUnits(chapterContent) : 0);
  $: showTaskTokens = $taskRunning && isCurrent;
  $: totalWords = chapters.reduce((sum, c) => sum + (c.word_count || 0), 0);

  $: foreshadows = p?.foreshadows || [];
  $: fsActive = foreshadows.filter(f => f.status === 'planted' || f.status === 'progressing');
  $: fsOverdue = fsActive.filter(f => f.target_chapter > 0 && (currentIdx + 1) > f.target_chapter);
  $: fsNearTarget = fsActive.filter(f =>
    f.target_chapter > 0 && (currentIdx + 1) >= f.target_chapter - 2 && (currentIdx + 1) <= f.target_chapter
  );
  $: writingConflict = p?.pending_writing_conflict || null;
  // Scene-plan review gate: the backend pauses chapter generation after
  // planning scenes (unless automation is on) and persists the pending
  // chapter number, so the editor auto-opens even after a page reload.
  $: scenePlanPendingNum = p?.pending_scene_plan_chapter || 0;
  let showScenePlan = false;
  let scenePlanChapter = null;
  $: if (scenePlanPendingNum && !$taskRunning) {
    if (scenePlanChapter !== scenePlanPendingNum) {
      scenePlanChapter = scenePlanPendingNum;
      addToast($t('writing.scenePlan.pendingToast', { num: scenePlanPendingNum }), 'info');
    }
    showScenePlan = true;
  }
  function openScenePlanEditor() {
    scenePlanChapter = ch?.num ?? chapters[currentIdx]?.num ?? null;
    showScenePlan = true;
  }
  $: orphanWriting = !!(ch && isCurrent && ch.status === 'writing' && !writingConflict && !$taskRunning);

  // --- Bestseller-editor critique passes (siblings of the outline one) ---
  // Scene-plan pass: one report over all PENDING beat sheets.
  $: scenePlanAnalysis = p?.scene_plan_analysis || null;
  $: scenePlanPendingCount = chapters.filter(c => c.status !== 'accepted' && c.scene_plan?.scenes?.length).length;
  // Act pass: reports keyed by pre-planning act number, rendered newest first.
  $: actAnalyses = Object.entries(p?.act_analyses || {})
    .map(([no, rep]) => ({ no: Number(no), rep }))
    .filter(x => x.rep)
    .sort((a, b) => b.no - a.no);
  // Chapter pass: report lives on the selected chapter itself.
  $: chapterAnalysis = ch?.analysis || null;
  // Pre-planning acts (loaded once in onMount) drive the manual act-critic
  // buttons; only acts whose last chapter is already accepted can be judged,
  // since the critic reviews drafted prose.
  let preplanActs = [];
  function critiqueAct(no) {
    api('POST', '/api/acts/analyze', { act_no: no })
      .then(() => addToast($t('act.analysis.toasts.started'), 'info'))
      .catch((e) => addToast(e.message, 'error'));
  }

  async function resolveWritingConflict(action) {
    if ($taskRunning) return;
    try {
      const res = await api('POST', '/api/chapter/conflict-resolve', { action });
      if (action === 'retry') {
        progress.set(await api('GET', '/api/progress'));
        await api('POST', '/api/chapter/generate');
        addToast($t('writing.toasts.generateStarted', { num: writingConflict?.chapter_num || ch?.num }), 'info');
        return;
      }
      progress.set(res);
      // dismiss ≡ force_review on the server
      if (action === 'force_review' || action === 'dismiss') {
        addToast($t('writing.conflict.forceReview'), 'success');
      } else if (action === 'revise_from_feedback') {
        // Server started a detached task (202 + saved progress snapshot).
        addToast($t('writing.conflict.reviseFromFeedbackStarted', { num: writingConflict?.chapter_num || ch?.num }), 'info');
      } else if (action === 'surgical_trope') {
        // Server started a detached surgical repair + re-verification task.
        progress.set(await api('GET', '/api/progress'));
        addToast($t('writing.conflict.surgicalTropeStarted', { num: writingConflict?.chapter_num || ch?.num }), 'info');
      }
    } catch (e) {
      addToast(e.message, 'error');
    }
  }

  function gotoOutlineForConflict() {
    const num = writingConflict?.chapter_num || ch?.num;
    if (num) {
      try { sessionStorage.setItem(OUTLINE_FOCUS_KEY, String(num)); } catch {}
    }
    navigate('outline');
  }

  function gotoForeshadows() {
    navigate('foreshadows');
  }

  $: statusMeta = {
    pending:  { label: $t('writing.status.pending'),  cls: 'badge-ghost',   dot: 'bg-base-content/20' },
    writing:  { label: $t('writing.status.writing'),  cls: 'badge-warning', dot: 'bg-warning animate-pulse' },
    review:   { label: $t('writing.status.review'),   cls: 'badge-info',    dot: 'bg-info' },
    accepted: { label: $t('writing.status.accepted'), cls: 'badge-success', dot: 'bg-success' },
  };

  let reviseFeedback = '';
  let showRevise = false;
  let contentEl;
  let reviseTextareaEl;
  let hasPolishSkills = false;

  // Floating "quote" button after selecting source text; null means hidden
  let quotePopover = null;

  function checkContentSelection() {
    const sel = window.getSelection();
    if (!sel || sel.isCollapsed || sel.rangeCount === 0) { quotePopover = null; return; }
    const text = sel.toString().trim();
    if (!text || text.length < 2) { quotePopover = null; return; }
    const range = sel.getRangeAt(0);
    if (!contentEl || !contentEl.contains(range.commonAncestorContainer)) {
      quotePopover = null; return;
    }
    const rect = range.getBoundingClientRect();
    if (rect.width === 0 && rect.height === 0) { quotePopover = null; return; }
    quotePopover = {
      text,
      x: rect.left + rect.width / 2,
      y: rect.top,
    };
  }

  function hideQuotePopover() { quotePopover = null; }

  function insertQuoteToFeedback() {
    if (!quotePopover) return;
    const text = quotePopover.text;
    const quoteLine = `> ${text}`;
    // If the user already wrote feedback, ensure a blank line separates the quote from it
    const current = reviseFeedback;
    let insertion;
    if (current === '') {
      insertion = quoteLine + '\n';
    } else if (current.endsWith('\n')) {
      insertion = quoteLine + '\n';
    } else {
      insertion = '\n' + quoteLine + '\n';
    }
    // Prefer inserting at the textarea cursor; otherwise append at the end
    const ta = reviseTextareaEl;
    if (ta && document.activeElement === ta && ta.selectionStart != null) {
      const start = ta.selectionStart;
      const end = ta.selectionEnd;
      reviseFeedback = current.slice(0, start) + insertion + current.slice(end);
      requestAnimationFrame(() => {
        ta.focus();
        const pos = start + insertion.length;
        ta.setSelectionRange(pos, pos);
      });
    } else {
      reviseFeedback = current + insertion;
      requestAnimationFrame(() => {
        if (ta) { ta.focus(); const pos = reviseFeedback.length; ta.setSelectionRange(pos, pos); }
      });
    }
    showRevise = true;
    addToast($t('writing.toasts.quoteInserted', { n: text.length }), 'success');
    // Clear the selection and hide the button
    const sel = window.getSelection();
    if (sel) sel.removeAllRanges();
    hideQuotePopover();
  }

  // Auto-scroll to bottom while streaming: coalesced into rAF, at most once per frame, to avoid forced reflows
  let scrollPending = false;
  function scheduleScroll() {
    if (scrollPending) return;
    scrollPending = true;
    requestAnimationFrame(() => {
      scrollPending = false;
      if (contentEl) contentEl.scrollTop = contentEl.scrollHeight;
    });
  }
  $: if (isStreamingThis && contentEl) scheduleScroll();

  function selectChapter(i) {
    if (savingKnowledge) return;
    selectedSettings = [];
    showKnowledge = false;
    selectedChapter.set(i);
    maybeLoadContent(chapters[i]);
    showRevise = false;
    reviseFeedback = '';
    hideQuotePopover();
    cancelBlockOps();
  }

  async function doGenerate() {
    try {
      await api('POST', '/api/chapter/generate');
      addToast($t('writing.toasts.generateStarted', { num: ch?.num }), 'info');
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function doConfirm() {
    try {
      await api('POST', '/api/chapter/confirm');
      progress.set(await api('GET', '/api/progress'));
      addToast($t('writing.toasts.confirmed', { num: ch?.num }), 'success');
      // Jump to next chapter
      const next = await api('GET', '/api/progress');
      if (next.current_chapter_index < (next.chapters || []).length) {
        selectedChapter.set(next.current_chapter_index);
      }
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function doRevise(confirmed = false) {
    const fb = reviseFeedback.trim();
    if (!fb && !selectedSettings.length) { addToast($t('writing.toasts.feedbackRequired'), 'error'); return; }
    if (!ch) return;
    const num = ch.num;
    const rev = loadedRev;
    if (selectedSettings.length && confirmed !== true) {
      try {
        const knowledge = await api('GET', '/api/knowledge?chapter=' + num);
        if (ch?.num !== num || loadedRev !== rev) return;
        if (knowledge.facts?.length) {
          confirmAction($t('writing.knowledge.confirm') + '\n' + knowledge.facts.map(f => f.content).join('\n'), () => {
            if (ch?.num === num && loadedRev === rev) doRevise(true);
          });
          return;
        }
      } catch (e) { addToast(e.message, 'error'); return; }
    }
    const body = { feedback: fb, worldview_ids: selectedSettings };
    try {
      if (!selectedSettings.length && isCurrent && ch.status === 'review') {
        // Current under-review chapter: full revision flow
        await api('POST', '/api/chapter/revise', body, factHeaders(confirmed === true));
      } else {
        // Other chapters (incl. confirmed): targeted minimal revision without touching other chapters
        await api('POST', '/api/chapter/revise/' + ch.num, body, factHeaders(confirmed === true));
      }
      addToast($t('writing.toasts.reviseStarted', { num: ch.num }), 'info');
      reviseFeedback = '';
      showRevise = false;
      showKnowledge = false;
      selectedSettings = [];
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function doPolish() {
    if (!ch) return;
    try {
      await api('POST', '/api/chapter/polish', { num: ch.num });
      addToast($t('writing.toasts.polishStarted', { num: ch.num }), 'info');
    } catch (e) { addToast(e.message, 'error'); }
  }

  // —— Draft recovery actions (resume / fact-check / reset) ————————
  // When a chapter generation was interrupted AFTER writing its prose (the
  // summary or critic step failed, the task deadline hit, or the user stopped
  // it), the backend keeps the prose as a resumable draft (draft_saved_at).
  // These buttons let the author finish, re-verify or discard that draft
  // without regenerating the expensive prose.
  async function doResumeDraft() {
    if (!ch) return;
    try {
      await api('POST', `/api/chapters/${ch.num}/resume`);
      addToast($t('writing.toasts.resumeStarted', { num: ch.num }), 'info');
    } catch (e) { addToast(e.message, 'error'); }
  }
  async function doFactCheckRerun() {
    if (!ch) return;
    try {
      await api('POST', `/api/chapters/${ch.num}/factcheck`);
      addToast($t('writing.toasts.factcheckStarted', { num: ch.num }), 'info');
    } catch (e) { addToast(e.message, 'error'); }
  }
  function doResetChapter() {
    if (!ch) return;
    const num = ch.num;
    confirmModal.set({
      message: $t('writing.chapter.resetAsk', { num }),
      onConfirm: async () => {
        try {
          progress.set(await api('POST', `/api/chapters/${num}/reset`));
          await loadChapterContent?.(num);
          addToast($t('writing.toasts.resetDone', { num }), 'success');
        } catch (e) { addToast(e.message, 'error'); }
      },
    });
  }

  async function copyContent() {
    if (!chapterContent) return;
    try {
      await navigator.clipboard.writeText(chapterContent);
      addToast($t('writing.toasts.copied'), 'success');
    } catch (e) { addToast($t('common.copy.failed'), 'error'); }
  }

  async function exportBook() {
    const written = chapters.filter(c => c.content_rev);
    if (written.length === 0) { addToast($t('writing.toasts.exportEmpty'), 'error'); return; }
    try {
      const r = await apiFetch('/api/export/txt');
      const blob = await r.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `${$config?.story?.title || p.title || $t('writing.export.defaultName')}.txt`;
      a.click();
      URL.revokeObjectURL(url);
      addToast($t('writing.toasts.exportDone', { n: written.length }), 'success');
    } catch (e) { addToast(e.message, 'error'); }
  }

  function prevChapter() { if ($selectedChapter > 0) selectChapter($selectedChapter - 1); }
  function nextChapter() { if ($selectedChapter < chapters.length - 1) selectChapter($selectedChapter + 1); }

  function smoothTransitions() {
    confirmModal.set({
      message: $t('writing.toasts.smoothAsk'),
      onConfirm: async () => {
        try {
          await api('POST', '/api/chapters/smooth-transitions');
          addToast($t('writing.toasts.smoothStarted'), 'info');
        } catch (e) { addToast(e.message, 'error'); }
      },
    });
  }
  async function setBookCompleted(completed) {
    const active = foreshadows.filter(f => f.status !== "resolved" && f.status !== "abandoned").length;
    const run = async () => {
      try {
        const suffix = completed && active ? "?confirm_foreshadows=true" : "";
        progress.set(await api("POST", completed ? "/api/story/complete" + suffix : "/api/story/resume"));
        addToast($t(completed ? "writing.book.completed" : "writing.book.resumed"), "success");
      } catch (e) { addToast(e.message, "error"); }
    };
    if (completed && active) {
      confirmModal.set({ message: $t("writing.book.foreshadowConfirm", { n: active }), onConfirm: run });
    } else {
      await run();
    }
  }
</script>

{#if !inWriting}
  <div id="WritingNotReadyState" class="text-center py-16 text-base-content/65">
    <div class="text-5xl mb-4"></div>
    <p class="text-base mb-1">{$t('writing.notReady.title')}</p>
    <p class="text-sm text-base-content/65 mb-6">{$t('writing.notReady.hint')}</p>
    <button class="btn btn-primary btn-sm" on:click={() => window.location.hash = '#outline'}>{$t('writing.notReady.goto')}</button>
  </div>
{:else}
  <div id="WritingPageContainer" class="space-y-4 mx-auto w-full" style="max-width:50rem;">
    {#if p.book_status !== 'completed' && (p.outline_batches || []).some(b => b.planned_final && chapters.find(c => c.num === b.end_ch)?.status === 'accepted')}
      <p class="alert alert-info">{$t('ending.completeHint')}</p>
    {/if}

    <!-- Progress -->
    <div id="CardSection_BookProgress" class="card bg-base-200">
      <div class="card-body p-4 gap-2">
        <div class="flex items-center gap-3">
          <h2 class="card-title text-base flex-1">{$t('writing.progress.title')}</h2>
          <label class="flex items-center gap-1.5 cursor-pointer" title={$t('writing.progress.autoConfirmTip')}>
            <input type="checkbox" class="toggle toggle-xs toggle-success" checked={$autoConfirm} on:change={toggleAutoConfirm} />
            <span class="text-xs text-base-content/60">{$t('writing.progress.autoConfirm')}</span>
          </label>
          <span class="text-xs text-base-content/65">{$t('writing.progress.totalWords', { n: totalWords.toLocaleString() })}</span>
          {#if accepted >= 2}
            <button class="btn btn-outline btn-xs" on:click={smoothTransitions} disabled={$taskRunning} title={$t('writing.btn.smoothTransitions.tip')}>{$t('writing.btn.smoothTransitions')}</button>
          {/if}
          <button class="btn btn-outline btn-xs" on:click={exportBook}>{$t('writing.btn.exportTxt')}</button>
			{#if p.book_status === 'completed'}
				<button class="btn btn-warning btn-xs" on:click={() => setBookCompleted(false)} disabled={$taskRunning}>{$t('writing.book.resume')}</button>
			{:else}
				<button class="btn btn-success btn-xs" on:click={() => setBookCompleted(true)} disabled={$taskRunning}>{$t('writing.book.complete')}</button>
			{/if}
        </div>
        <progress class="progress progress-primary w-full" value={pct} max="100"></progress>
        <div class="text-sm text-base-content/65">{$t('writing.progress.acceptedSummary', { pct, accepted, total })}</div>
      </div>
    </div>

    {#if writingConflict}
      <div id="CardSection_WritingConflict" class="card bg-error/10 border border-error/30 ">
        <div class="card-body p-4 gap-3">
          <h3 class="font-semibold text-error">{$t('writing.conflict.title')}</h3>
          <p class="text-sm">{$t('writing.conflict.summary')}：{writingConflict.summary}</p>
          {#if writingConflict.issues?.length}
            <div class="text-xs text-base-content/70">
              <div class="font-medium mb-1">{$t('writing.conflict.issues')}</div>
              <ul class="list-disc list-inside space-y-0.5">
                {#each writingConflict.issues as issue}
                  <li>{issue}</li>
                {/each}
              </ul>
            </div>
          {/if}
          <div class="flex flex-wrap gap-2">
            {#each (writingConflict.suggested_actions || []) as action}
              {#if action.id === 'surgical_trope'}
                <button class="btn btn-success btn-xs" disabled={$taskRunning} title={action.description} on:click={() => resolveWritingConflict('surgical_trope')}>{$t('writing.conflict.surgicalTrope')}</button>
              {:else if action.id === 'edit_outline'}
                <button class="btn btn-warning btn-xs" disabled={$taskRunning} on:click={gotoOutlineForConflict}>{$t('writing.conflict.gotoOutline')}</button>
              {:else if action.id === 'adjust_foreshadow'}
                <button class="btn btn-warning btn-xs" disabled={$taskRunning} on:click={gotoForeshadows}>{$t('writing.conflict.gotoForeshadows')}</button>
              {:else if action.id === 'retry'}
                <button class="btn btn-primary btn-xs" disabled={$taskRunning} on:click={() => resolveWritingConflict('retry')}>{$t('writing.conflict.retry')}</button>
              {:else if action.id === 'revise_from_feedback'}
                <button class="btn btn-primary btn-xs" disabled={$taskRunning} on:click={() => resolveWritingConflict('revise_from_feedback')}>{$t('writing.conflict.reviseFromFeedback')}</button>
              {:else if action.id === 'force_review'}
                <button class="btn btn-outline btn-xs" disabled={$taskRunning} on:click={() => resolveWritingConflict('force_review')}>{$t('writing.conflict.forceReview')}</button>
              {/if}
            {/each}
            <button class="btn btn-ghost btn-xs" disabled={$taskRunning} on:click={() => resolveWritingConflict('dismiss')}>{$t('writing.conflict.dismiss')}</button>
          </div>
        </div>
      </div>
    {:else if orphanWriting}
      <div id="CardSection_OrphanChapter" class="card bg-warning/10 border border-warning/30 ">
        <div class="card-body p-4 gap-3">
          <h3 class="font-semibold text-warning">{$t('writing.orphan.title')}</h3>
          <p class="text-sm text-base-content/70">{$t('writing.orphan.hint')}</p>
          <div class="flex flex-wrap gap-2">
            <button class="btn btn-primary btn-xs" disabled={$taskRunning} on:click={doGenerate}>{$t('writing.orphan.retry')}</button>
            <button class="btn btn-outline btn-xs" disabled={$taskRunning} on:click={() => resolveWritingConflict('force_review')}>{$t('writing.orphan.forceReview')}</button>
            <button class="btn btn-warning btn-xs" disabled={$taskRunning} on:click={gotoOutlineForConflict}>{$t('writing.conflict.gotoOutline')}</button>
            <button class="btn btn-warning btn-xs" disabled={$taskRunning} on:click={gotoForeshadows}>{$t('writing.conflict.gotoForeshadows')}</button>
          </div>
        </div>
      </div>
    {/if}

    <!-- Row: chapter index (left) + scene-plan access (right) -->
    <div id="WritingMetaRow" class="grid grid-cols-[minmax(0,1fr)_345px] gap-3 items-start">
      <div id="ChapterSidebarList" class="card bg-base-200 overflow-y-auto max-h-[calc(100vh-280px)] min-w-0">
        <ul class="menu menu-sm p-0 w-full">
          {#each chapters as c, i}
            <li>
              <button class="flex gap-2 items-center {$selectedChapter === i ? 'active' : ''}" on:click={() => selectChapter(i)}>
                <span class="w-2 h-2 rounded-full shrink-0 {statusMeta[c.status]?.dot || ''}"></span>
                <span class="text-base-content/65 w-6 shrink-0 text-right">{c.num}</span>
                <span class="flex-1 text-left truncate text-sm">{c.title}</span>
                {#if c.draft_saved_at && c.status !== 'accepted'}
                  <span class="badge badge-warning badge-outline badge-xs shrink-0" title={$t('writing.chapter.draftBadgeTip')}>{$t('writing.chapter.draftBadge')}</span>
                {/if}
                {#if i === currentIdx && c.status !== 'accepted'}
                  <span class="badge badge-primary badge-xs shrink-0">{$t('writing.tag.current')}</span>
                {/if}
              </button>
            </li>
          {/each}
        </ul>
      </div>

      <div id="CardSection_ScenePlanAccess" class="card bg-base-200 min-w-0">
        <div class="card-body p-4 gap-2">
          <h3 class="font-medium text-sm">{$t('writing.scenePlan.accessTitle')}</h3>
          <p class="text-xs text-base-content/65">{$t('writing.scenePlan.accessHint', { num: ch?.num ?? '-' })}</p>
          <button class="btn btn-outline btn-xs self-start" on:click={openScenePlanEditor} disabled={$taskRunning || !ch} title={$t('writing.scenePlan.btnTip')}>{$t('writing.scenePlan.btn')}</button>
        </div>
      </div>
    </div><!-- /WritingMetaRow -->

    <!-- Bestseller critic over the PENDING scene plans (beat sheets): same
         card as the outline analysis, but the editor knows it is reviewing
         scene plans, not prose. Scene-level replacements are proposed per
         chapter and applied back to each plan (re-opening its review gate). -->
    <div id="CardSection_ScenePlanAnalysis" class="mt-3 space-y-1">
      <BestsellerCritiqueCard
        kind="sceneplan"
        report={scenePlanAnalysis}
        disabled={$taskRunning}
        hasTargets={scenePlanPendingCount > 0}
        scrollId="CardSection_ScenePlanAnalysis" />
    </div>

    <!-- Bestseller critic over FINISHED ACTS of the pre-planning: one report
         per act, generated automatically right after the pipeline confirms
         the act's last chapter (and manually here). Suggestions carry small
         trope-unit interventions that rewriteSceneTrope repairs surgically. -->
    {#each actAnalyses as a (a.no)}
      <div id="CardSection_ActAnalysis_{a.no}" class="mt-3 space-y-1">
        <BestsellerCritiqueCard
          kind="act"
          report={a.rep}
          actNo={a.no}
          disabled={$taskRunning}
          hasTargets={true}
          scrollId="CardSection_ActAnalysis_" + a.no />
      </div>
    {/each}
    {#if preplanActs.length > 0}
      <div class="flex flex-wrap gap-2 justify-end mt-2">
        {#each preplanActs as act (act.no)}
          <button class="btn btn-outline btn-xs" disabled={$taskRunning || !!p?.act_analyses?.[act.no]}
            title={$t('act.analysis.manualBtnTip', { start: act.start_ch, end: act.end_ch })}
            on:click={() => critiqueAct(act.no)}>
            {$t('act.analysis.manualBtn', { no: act.no, name: act.name || '' })}
          </button>
        {/each}
      </div>
    {/if}

    <!-- Foreshadow tracker: own row below the chapter index -->
    {#if foreshadows.length > 0}
      <div id="CardSection_ForeshadowSummary" class="card bg-base-200 mt-3">
        <div class="card-body p-4 gap-2">
          <div class="flex items-center justify-between gap-2">
            <h3 class="font-medium text-sm">{$t('writing.fs.title')}</h3>
            <button class="btn btn-outline btn-xs" on:click={() => window.location.hash = '#foreshadows'}>{$t('writing.fs.goto')}</button>
          </div>
          <div class="flex flex-wrap gap-2 text-xs">
            <span class="badge badge-ghost">{$t('writing.fs.total', { n: foreshadows.length })}</span>
            <span class="badge badge-info badge-outline">{$t('writing.fs.active', { n: fsActive.length })}</span>
            {#if fsOverdue.length > 0}
              <span class="badge badge-error">{$t('writing.fs.overdue', { n: fsOverdue.length })}</span>
            {/if}
            {#if fsNearTarget.length > 0}
              <span class="badge badge-warning badge-outline">{$t('writing.fs.nearTarget', { n: fsNearTarget.length })}</span>
            {/if}
          </div>
          {#if fsOverdue.length > 0}
            <p class="text-xs text-warning">{$t('writing.fs.overdueDetail', { names: fsOverdue.map(f => `#${f.id} ${f.name}`).join(', ') })}</p>
          {:else if fsNearTarget.length > 0}
            <p class="text-xs text-base-content/65">{$t('writing.fs.nearDetail', { names: fsNearTarget.map(f => f.name).join(', ') })}</p>
          {/if}
        </div>
      </div>
    {:else}
      <div id="CardSection_ForeshadowSetup" class="card bg-base-200 mt-3">
        <div class="card-body p-4 flex items-center justify-between gap-2">
          <p class="text-sm text-base-content/65">{$t('writing.fs.none')}</p>
          <button class="btn btn-outline btn-xs" on:click={() => window.location.hash = '#foreshadows'}>{$t('writing.fs.setup')}</button>
        </div>
      </div>
    {/if}

    <!-- Chapter area -->
    <div id="WritingWorkspace" style="min-height:400px">
      <!-- Content area -->
      <div id="ChapterContentArea" class="min-w-0 w-full">
        {#if ch}
          <div id="CardSection_ChapterReader" class="card bg-base-200">
            <div class="card-body p-4 gap-2">
              <div class="flex items-center gap-2 flex-wrap">
                <h2 class="card-title text-base flex-1 min-w-0">{$t('writing.chapter.title', { num: ch.num, title: ch.title })}</h2>
                <span class="badge badge-sm {statusMeta[ch.status]?.cls || 'badge-ghost'}">{statusMeta[ch.status]?.label || ch.status}</span>
                {#if showTaskTokens}
                  <TaskTokenBadge className="text-xs text-base-content/65 font-mono" />
                {:else if chapterWordCount > 0}
                  <span class="text-xs text-base-content/65">{$t('writing.chapter.words', { n: chapterWordCount.toLocaleString() })}</span>
                {/if}
              </div>

              <KnowledgePanel chapterNum={ch.num} bind:facts bind:activeFact bind:expanded={knowledgeExpanded} on:jump={e => jumpToFact(e.detail)} />
              {#if returnRef}<button class="btn btn-outline btn-xs self-start" on:click={() => jumpToFact(returnRef, true)}>{$t('facts.return')}</button>{/if}

              {#if ch.outline}
                <details class="bg-base-100 rounded">
                  <summary class="p-2 text-xs text-base-content/65 cursor-pointer select-none">{$t('writing.chapter.outline')}</summary>
                  <div class="px-2 pb-2 text-sm text-base-content/70">{ch.outline}</div>
                </details>
              {/if}

              {#if ch.summary}
                <details class="bg-base-100 rounded">
                  <summary class="p-2 text-xs text-base-content/65 cursor-pointer select-none">{$t('writing.chapter.summary')}</summary>
                  <div class="px-2 pb-2 text-sm text-base-content/70 whitespace-pre-wrap">{ch.summary}</div>
                </details>
              {/if}

              {#if displayContent}
                {#if isStreamingThis}
                  <div class="text-xs text-warning/80 flex items-center gap-1.5">
                    <span class="loading loading-dots loading-xs"></span>
                    {$t('writing.chapter.streamHint')}
                  </div>
                {/if}
                <!-- svelte-ignore a11y-no-static-element-interactions -->
                <div id="ChapterProseViewer" bind:this={contentEl} class="rounded-lg p-4 text-[15px] chapter-content min-h-[200px]"
                     on:mouseup={checkContentSelection}
                     on:scroll={hideQuotePopover}>
                  {#if isStreamingThis}
                    {displayContent}
                    <span class="inline-block w-2 h-4 bg-primary/70 animate-pulse ml-0.5 align-text-bottom"></span>
                  {:else if chapterBlocks.length > 0}
                    <div class="space-y-3">
                      {#each chapterBlocks as b, bi (b.id)}
                        {#if sceneMarkerBefore(chapterBlocks, bi)}
                          {@const m = sceneMarkerBefore(chapterBlocks, bi)}
                          <div class="scene-marker-row flex justify-center -my-1" aria-hidden="true">
                            <span class="scene-marker-chip group inline-flex items-center gap-1 rounded-full bg-primary/15 border border-primary/30 px-2 py-0.5 select-none pointer-events-auto cursor-default">
                              <span class="scene-marker-text text-[10px] font-semibold text-primary/90 tracking-wider uppercase select-none">{$t('writing.blockSceneMarker', [m.pos])}</span>
                              <span class="opacity-60 group-hover:opacity-100 transition-opacity"><InfoTip text={sceneMarkerTip(m)} label={$t('writing.blockSceneMarker', [m.pos])} /></span>
                            </span>
                          </div>
                        {/if}
                        <div id={'story-block-' + b.id} class="relative rounded -mx-2 px-2 py-1 cursor-pointer transition-colors hover:bg-base-100/40 {highlightedBlock === b.id || activeFact?.references?.some(r => !r.stale && r.chapter === ch.num && r.block_id === b.id) ? 'story-block-highlight' : selectedBlockId === b.id ? 'story-block-selected' : ''}" role="button" tabindex="0" aria-pressed={selectedBlockId===b.id} on:click={() => selectBlock(b.id)} on:keydown={(e) => { if (e.currentTarget === e.target && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); selectBlock(b.id); } }}>
                          {#if blockFacts(b.id).length}<div class="flex flex-wrap gap-1 mb-2">{#each blockFacts(b.id) as fact}<button class="badge badge-warning badge-sm cursor-pointer" on:click|stopPropagation={() => { activeFact = fact; highlightedBlock = b.id; knowledgeExpanded = true; }}>{$t('facts.marker')} #{fact.id}</button>{/each}</div>{/if}
                          {#if editingBlockId === b.id}
                            <textarea class="textarea textarea-sm w-full text-[15px] leading-relaxed" rows={Math.max(3, Math.ceil(b.text.length / 40))} bind:value={editingText} disabled={$taskRunning}></textarea>
                            <div class="flex gap-2 justify-end mt-1">
                              <button class="btn btn-ghost btn-xs" on:click={cancelBlockOps}>{$t('common.cancel')}</button>
                              <button class="btn btn-primary btn-xs" on:click={saveBlockEdit} disabled={$taskRunning || !editingText.trim()}>{$t('common.save')}</button>
                            </div>
                          {:else}
                            <div class="whitespace-pre-wrap {b.type === 'scene_break' ? 'text-center text-base-content/65' : ''}">{b.text}</div>
                            {#if selectedBlockId === b.id}<div class="absolute right-1 top-1 flex gap-1 bg-base-200 border border-base-content/20 rounded px-1 py-0.5"><button class="btn btn-outline btn-xs" disabled={$taskRunning} on:click|stopPropagation={() => startBlockEdit(b)}>{$t('writing.block.edit')}</button><button class="btn btn-outline btn-xs" disabled={$taskRunning} on:click|stopPropagation={() => startBlockRevise(b)}>{$t('writing.block.revise')}</button><button class="btn btn-outline btn-xs" disabled={$taskRunning} on:click|stopPropagation={() => startBlockInsert(b.id)}>{$t('writing.block.insertAfter')}</button><button class="btn btn-error btn-outline btn-xs" disabled={$taskRunning} on:click|stopPropagation={() => deleteBlock(b)}>{$t('writing.block.delete')}</button></div>{/if}
                          {/if}
                          {#if revisingBlockId === b.id}
                            <div class="bg-base-100 rounded p-2 mt-1 space-y-1">
                              <textarea class="textarea textarea-sm w-full text-sm" bind:value={blockFeedback} placeholder={$t('writing.block.revisePlaceholder')} disabled={$taskRunning}></textarea>
                              <div class="flex gap-2 justify-end">
                                <button class="btn btn-ghost btn-xs" on:click={cancelBlockOps}>{$t('common.cancel')}</button>
                                <button class="btn btn-primary btn-xs" on:click={submitBlockRevise} disabled={$taskRunning || !blockFeedback.trim()}>{$t('writing.block.reviseSubmit')}</button>
                              </div>
                            </div>
                          {/if}
                          {#if insertAfterId === b.id}
                            <div class="bg-base-100 rounded p-2 mt-1 space-y-1">
                              <textarea class="textarea textarea-sm w-full text-sm" bind:value={insertText} placeholder={$t('writing.block.insertPlaceholder')} disabled={$taskRunning}></textarea>
                              <div class="flex gap-2 justify-end">
                                <button class="btn btn-ghost btn-xs" on:click={cancelBlockOps}>{$t('common.cancel')}</button>
                                <button class="btn btn-primary btn-xs" on:click={saveBlockInsert} disabled={$taskRunning || !insertText.trim()}>{$t('writing.block.insertSubmit')}</button>
                              </div>
                            </div>
                          {/if}
                        </div>
                      {/each}
                    </div>
                  {:else}
                    {displayContent}
                  {/if}
                </div>
                {#if quotePopover}
                  <button type="button"
                    class="fixed z-50 btn btn-primary btn-xs"
                    style="left: {quotePopover.x}px; top: {quotePopover.y}px; transform: translate(-50%, -100%); margin-top: -6px;"
                    on:click={insertQuoteToFeedback}
                    title={$t('writing.revise.quoteBtn.tip')}>
                    {$t('writing.revise.quoteBtn')}
                  </button>
                {/if}
              {:else if ch.status === 'pending'}
                <div class="bg-base-100 rounded-lg p-6 text-center text-sm text-base-content/65">
                  {#if isCurrent}
                    {$t('writing.chapter.pendingCurrent')}
                  {:else}
                    {$t('writing.chapter.pendingOther', { n: chapters[currentIdx]?.num ?? '-' })}
                  {/if}
                </div>
              {/if}

              <!-- Actions -->
              <div id="ChapterActionsBar" class="flex gap-2 flex-wrap items-center mt-1">
                {#if ch.status === 'pending' && isCurrent}
                  <button class="btn btn-primary btn-sm" on:click={doGenerate} disabled={$taskRunning}>{$t('writing.btn.generate')}</button>
                  <button class="btn btn-outline btn-sm" on:click={openScenePlanEditor} disabled={$taskRunning} title={$t('writing.scenePlan.btnTip')}>{$t('writing.scenePlan.btn')}</button>
                {/if}
                {#if ch.draft_saved_at && !isStreamingThis}
                  <button class="btn btn-warning btn-sm" on:click={doResumeDraft} disabled={$taskRunning} title={$t('writing.chapter.resumeTip')}>{$t('writing.chapter.resume')}</button>
                  <button class="btn btn-error btn-outline btn-sm" on:click={doResetChapter} disabled={$taskRunning}>{$t('writing.chapter.reset')}</button>
                {/if}
                {#if ch.status === 'review' && isCurrent}
                  <button class="btn btn-success btn-sm" on:click={doConfirm} disabled={$taskRunning}>{$t('writing.btn.confirm')}</button>
                {/if}
                {#if hasContent && ch.status !== 'writing'}
                  <button class="btn btn-outline btn-sm" on:click={() => showRevise = !showRevise} disabled={$taskRunning}>{$t('writing.btn.revise')}</button>
                  <button class="btn btn-outline btn-sm" on:click={doFactCheckRerun} disabled={$taskRunning} title={$t('writing.chapter.factcheckTip')}>{$t('writing.chapter.factcheck')}</button>
                  <button class="btn btn-outline btn-sm" on:click={() => { showKnowledge = !showKnowledge; showRevise = true; if (!showKnowledge) selectedSettings = []; }} disabled={$taskRunning || savingKnowledge}>{$t('writing.knowledge.title')}</button>
                  {#if hasPolishSkills}
                    <button class="btn btn-outline btn-sm" on:click={doPolish} disabled={$taskRunning} title={$t('writing.btn.polish.tip')}>{$t('writing.btn.polish')}</button>
                  {/if}
                  <button class="btn btn-outline btn-sm" on:click={copyContent}>{$t('writing.btn.copy')}</button>
                {/if}
                <div class="flex-1"></div>
                <div class="join">
                  <button class="btn btn-ghost btn-xs join-item" on:click={prevChapter} disabled={$selectedChapter <= 0}>{$t('writing.btn.prev')}</button>
                  <button class="btn btn-ghost btn-xs join-item" on:click={nextChapter} disabled={$selectedChapter >= chapters.length - 1}>{$t('writing.btn.next')}</button>
                </div>
              </div>

              <!-- Bestseller critic over THIS drafted chapter: same card as the
                   outline analysis, but the editor knows it is reviewing one
                   chapter and its suggestions carry trope-unit interventions. -->
              {#if hasContent && ch.status !== 'writing'}
                <div id="CardSection_ChapterAnalysis_{ch.num}" class="pt-1">
                  <BestsellerCritiqueCard
                    kind="chapter"
                    report={chapterAnalysis}
                    chapterNum={ch.num}
                    disabled={$taskRunning}
                    hasTargets={true}
                    scrollId="CardSection_ChapterAnalysis_" + ch.num />
                </div>
              {/if}

              {#if showRevise}
                <div id="CardSection_ChapterRevise" class="bg-base-100 rounded-lg p-3 space-y-2">
                    {#if showKnowledge}
                      <fieldset id="ChapterKnowledgePicker" class="border border-base-content/20 rounded-lg p-3 space-y-3" disabled={$taskRunning || savingKnowledge}>
                        <legend>{$t('writing.knowledge.title')}</legend>
                        <p class="text-sm">{$t('writing.knowledge.hint')}</p>
                        <label class="block text-sm">{$t('config.wv.name')}<input class="input input-sm w-full" bind:value={knowledgeName} /></label>
                        <label class="block text-sm">{$t('config.wv.description')}<textarea class="textarea w-full" rows="4" bind:value={knowledgeDescription}></textarea></label>
                        <label class="block text-sm">{$t('config.wv.tags')}<input class="input input-sm w-full" bind:value={knowledgeTags} /></label>
                        <div class="flex gap-2 flex-wrap">
                          <button class="btn btn-outline btn-sm" on:click={() => saveKnowledge()} disabled={!knowledgeName.trim() || !knowledgeDescription.trim()}>{$t('common.save')}</button>
                          <button class="btn btn-primary btn-sm" on:click={() => saveKnowledge(true)} disabled={!knowledgeName.trim() || !knowledgeDescription.trim()}>{$t('writing.knowledge.saveRevise')}</button>
                        </div>
                        <p class="text-sm">{$t('writing.knowledge.select')}</p>
                        <div class="max-h-48 overflow-y-auto space-y-2">
                          {#each ($settings?.worldview || []) as entry (entry.id)}
                            <label class="flex items-start gap-2 text-sm"><input type="checkbox" class="checkbox checkbox-sm" value={entry.id} bind:group={selectedSettings} /><span>{entry.name}</span></label>
                            {#if selectedSettings.includes(entry.id)}<p class="text-sm whitespace-pre-wrap pl-6">{entry.description}</p>{/if}
                          {/each}
                        </div>
                      </fieldset>
                    {/if}
                  <textarea
                    class="textarea textarea-sm w-full text-sm"
                    bind:value={reviseFeedback}
                    bind:this={reviseTextareaEl}
                    placeholder={$t('writing.revise.placeholder')}
                    disabled={$taskRunning}
                  ></textarea>
                  <div class="flex justify-between items-center gap-2 flex-wrap">
                    <span class="text-xs text-base-content/65">
                        {#if selectedSettings.length || !(isCurrent && ch.status === 'review')}
                        {$t('writing.revise.hintTargeted')}
                      {:else}
                        {$t('writing.revise.hintCurrent')}
                      {/if}
                      <span class="ml-1 text-base-content/30">· {$t('writing.revise.quoteHint')}</span>
                    </span>
                    <div class="flex gap-2">
                      <button class="btn btn-ghost btn-xs" on:click={() => { showRevise = false; reviseFeedback = ''; selectedSettings = []; showKnowledge = false; }} disabled={savingKnowledge}>{$t('common.cancel')}</button>
                      <button class="btn btn-primary btn-xs" on:click={() => doRevise()} disabled={$taskRunning || savingKnowledge || (!reviseFeedback.trim() && !selectedSettings.length)}>{selectedSettings.length ? $t('writing.knowledge.revise') : $t('writing.revise.submit')}</button>
                    </div>
                  </div>
                </div>
              {/if}
            </div>
          </div>
        {:else}
          <div class="text-center py-16 text-base-content/65 text-base">{$t('writing.emptySelection')}</div>
        {/if}
      </div>
    </div>
  </div>

  <ScenePlanEditor bind:open={showScenePlan} chapterNum={scenePlanChapter ?? ch?.num ?? scenePlanPendingNum} />
{/if}
