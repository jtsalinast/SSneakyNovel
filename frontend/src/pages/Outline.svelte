<script>
  import { api } from '../lib/api.js';
  import { progress, config, streamingContent, streamingChapterIdx, taskRunning, addToast, showConfirm, outlineCharacterSuggestions, outlineCharacterShowSuggestions, settings } from '../lib/stores.js';
  import { t } from '../lib/i18n/index.js';
  import { onMount, tick } from 'svelte';
  import ConfigChangePanel from '../components/ConfigChangePanel.svelte';
  import AiGenerateButton from '../components/AiGenerateButton.svelte';
  // Interfaz unificada: los <select> nativos del panel de pre-planificación
  // usan SmartSelect (single-select con búsqueda y popover ⓘ igual que el
  // resto de la app). allowClear={false}: son campos obligatorios sin opción
  // vacía, como el <select> original.
  import SmartSelect from '../lib/components/SmartSelect.svelte';

  // Passed down from App.svelte: the same chat dispatch the config-page
  // Generate buttons use, so Pre-planning's button runs through the agent's
  // generate_section tool ([generate-section:pre_planning] fast path).
  export let sendToChat = async () => {};

  const OUTLINE_FOCUS_KEY = 'showmethestory.outlineFocusChapter';

  function isOutlineEditable(status) {
    return status === 'pending' || status === 'writing' || status === 'review';
  }

  $: p = $progress;
  $: displayTitle = $config?.story?.title || p?.title || '';
  $: chapters = p?.chapters || [];
  $: projectChapterCount = chapters.filter(ch => !ch.inherited).length;
  $: hasOutline = chapters.length > 0;
  $: hasAccepted = chapters.some(c => c.status === 'accepted');
  $: inOutlinePhase = p?.phase === 'outline';
  $: pendingCount = chapters.filter(c => c.status === 'pending').length;

  $: statusMeta = {
    pending:  { label: $t('outline.status.pending'),  cls: 'badge-ghost' },
    writing:  { label: $t('outline.status.writing'),  cls: 'badge-warning' },
    review:   { label: $t('outline.status.review'),   cls: 'badge-info' },
    accepted: { label: $t('outline.status.accepted'), cls: 'badge-success' },
  };

  let reviseFeedback = '';
  let showRevise = false;

  // Inline editing
  let editingNum = -1;
  let editTitle = '';
  let editOutline = '';
  let editCharactersText = '';

  // Cast edit lines: "Name", "Name*", "Name|note", "Name*|note" (* = first appearance)
  function formatCharactersEdit(chars) {
    if (!chars?.length) return '';
    return chars.map(c => {
      let line = c.name || '';
      if (c.first_appearance) line += '*';
      if (c.note) line += '|' + c.note;
      return line;
    }).join('\n');
  }

  function parseCharactersEdit(text) {
    const out = [];
    const seen = new Set();
    for (const raw of (text || '').split('\n')) {
      const line = raw.trim();
      if (!line) continue;
      let namePart = line;
      let note = '';
      const bar = line.indexOf('|');
      if (bar >= 0) {
        namePart = line.slice(0, bar).trim();
        note = line.slice(bar + 1).trim();
      }
      let first = false;
      if (namePart.endsWith('*')) {
        first = true;
        namePart = namePart.slice(0, -1).trim();
      }
      if (!namePart || seen.has(namePart)) continue;
      seen.add(namePart);
      const entry = { name: namePart };
      if (first) entry.first_appearance = true;
      if (note) entry.note = note;
      out.push(entry);
    }
    return out;
  }

  // Import continuation
  let showImport = false;
  let importContent = '';
  let importPreview = null; // [{num,title,word_count,preview}]
  let importStatus = null;  // {active,total,cursor} resume state
  let continuationCount = 5;
  let planningRequirements = "";
  let longTermDirection = "";
  let endingIntent = 'serial';
  let endingStyle = 'closed';
  let endingRequirements = '';
  let directionLoaded = false;
  $: if (p && !directionLoaded) { longTermDirection = p.long_term_direction || ''; directionLoaded = true; }
  let replacingBatch = null;
  let submittedBatch = null;
  $: batches = p?.outline_batches || [];
  $: groups = [
    { id: 0, chapters: chapters.filter(ch => !batches.some(b => ch.num >= b.start_ch && ch.num <= b.end_ch)) },
    ...batches.map(b => ({ ...b, chapters: chapters.filter(ch => ch.num >= b.start_ch && ch.num <= b.end_ch) }))
  ].filter(g => g.chapters.length);
  $: batchActionKey = replacingBatch ? 'outline.batch.replan' : hasOutline ? 'outline.batch.generate' : 'outline.batch.first';
  $: batchStart = replacingBatch ? replacingBatch.start_ch : Math.max(0, ...chapters.map(ch => ch.num)) + 1;
  $: validCount = Number.isInteger(Number(continuationCount)) && continuationCount >= 1 && continuationCount <= 36;
  $: batchBlocked = $taskRunning || p?.book_status === 'completed' || chapters.some(ch => ch.status === 'writing' || ch.status === 'review');
  $: if (submittedBatch && batches.some(b => b.start_ch === submittedBatch.start && b.end_ch === submittedBatch.end && b.synopsis === submittedBatch.synopsis && b.revision === submittedBatch.revision)) {
    planningRequirements = ''; replacingBatch = null; submittedBatch = null;
    endingIntent = 'serial'; endingStyle = 'closed'; endingRequirements = '';
  }
  function canReplan(b) {
    return b.id && batches[batches.length - 1]?.id === b.id && b.end_ch === Math.max(0, ...chapters.map(ch => ch.num)) && b.chapters.every(ch => ch.status === 'pending');
  }
  function replan(b) {
    endingIntent = b.ending_intent || 'serial'; endingStyle = b.ending_style || 'closed'; endingRequirements = b.ending_requirements || '';
    replacingBatch = b; planningRequirements = b.synopsis; continuationCount = b.end_ch - b.start_ch + 1;
    document.getElementById('CardSection_BatchPlanning')?.scrollIntoView({ behavior: 'smooth' });
  }


  onMount(() => { refreshImportStatus(); loadPrePlan(); });
  $: if (!$taskRunning) refreshImportStatus();

  let outlineFocusTried = false;
  $: if (!outlineFocusTried && chapters.length > 0) {
    outlineFocusTried = true;
    focusChapterFromSession();
  }

  async function focusChapterFromSession() {
    let raw;
    try { raw = sessionStorage.getItem(OUTLINE_FOCUS_KEY); } catch { return; }
    if (!raw) return;
    try { sessionStorage.removeItem(OUTLINE_FOCUS_KEY); } catch {}
    const num = parseInt(raw, 10);
    if (!num) return;
    const ch = chapters.find(c => c.num === num);
    if (!ch || !isOutlineEditable(ch.status) || $taskRunning) return;
    startEdit(ch);
    await tick();
    const el = document.querySelector(`[data-outline-chapter="${num}"]`);
    if (el) el.scrollIntoView({ block: 'center', behavior: 'smooth' });
  }

  async function refreshImportStatus() {
    try {
      const st = await api('GET', '/api/import/status');
      importStatus = st?.active ? st : null;
    } catch { importStatus = null; }
  }

  async function confirmOutline() {
    showConfirm($t('outline.toasts.confirmAsk'), async () => {
      try {
        await api('POST', '/api/outline/confirm');
        progress.set(await api('GET', '/api/progress'));
        addToast($t('outline.toasts.outlineConfirmed'), 'success');
        window.location.hash = '#writing';
      } catch (e) { addToast(e.message, 'error'); }
    });
  }

  async function reviseOutline() {
    const fb = reviseFeedback.trim();
    if (!fb) { addToast($t('outline.toasts.reviseFeedbackRequired'), 'error'); return; }
    try {
      await api('POST', '/api/outline/revise', { feedback: fb });
      addToast($t('outline.toasts.reviseStarted'), 'info');
      reviseFeedback = '';
      showRevise = false;
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function deleteOutline() {
    showConfirm($t('outline.toasts.deleteConfirm', { n: chapters.length }), async () => {
      try {
        await api('DELETE', '/api/outline');
        progress.set(await api('GET', '/api/progress'));
        addToast($t('outline.toasts.deleted'), 'success');
      } catch (e) { addToast(e.message, 'error'); }
    });
  }

  // --- Bestseller-editor outline analysis ---
  $: outlineAnalysis = p?.outline_analysis || null;
  $: analysisHasProposals = (outlineAnalysis?.suggestions || []).some(s => (s.recommendations || []).length > 0);
  let applyingSuggestionId = '';
  let proposingSuggestionId = '';

  // Rubric helpers (mirror BestsellerCritiqueCard): transparent sub-criteria
  // levels per pillar, weakest first — the score is their average, so this
  // list literally shows what must improve to reach 100%.
  function criterionLabel(key) {
    const msg = $t('criteria.' + key);
    return msg && msg !== 'criteria.' + key ? msg : key.replace(/_/g, ' ');
  }
  function sortedChecklist(pl) {
    return [...(pl?.checklist || [])].sort((a, b) => a.level - b.level);
  }
  const PRIORITY_BADGE = { high: 'badge-error', medium: 'badge-warning', low: 'badge-ghost' };
  function priorityLabel(p) {
    const msg = $t('analysis.priority.' + p);
    return msg && msg !== 'analysis.priority.' + p ? msg : p;
  }

  // Local (unsaved) edits the user makes in a recommendation's "Replace:
  // Outline" textarea before hitting Apply. Keyed by `${suggestionId}:${chapter}`
  // so each chapter proposal keeps its own draft. Persisted to the backend on
  // change (debounced) so the edited text survives reloads and is what gets
  // written into the outline when the suggestion is applied.
  let recOutlineEdits = {};
  let recEditTimers = {};

  function recEditKey(s, rec) { return s.id + ':' + rec.chapter_num; }

  function recOutlineValue(s, rec) {
    const k = recEditKey(s, rec);
    return k in recOutlineEdits ? recOutlineEdits[k] : (rec.new_outline || '');
  }

  function persistRecEdit(s, rec) {
    const k = recEditKey(s, rec);
    const val = recOutlineEdits[k];
    if (val === undefined) return;
    clearTimeout(recEditTimers[k]);
    recEditTimers[k] = setTimeout(() => {
      api('POST', '/api/outline/analysis/edit-recommendation', {
        id: s.id, chapter_num: rec.chapter_num, new_outline: val,
      }).catch(() => {});
    }, 400);
  }

  function onRecOutlineInput(s, rec, e) {
    recOutlineEdits = { ...recOutlineEdits, [recEditKey(s, rec)]: e.target.value };
    persistRecEdit(s, rec);
  }

  // Auto-grow the recommendation textareas to fit their content (same trick
  // used elsewhere: reset height then match scrollHeight).
  function autosizeRecTextarea(el) {
    if (!el) return;
    const grow = () => { el.style.height = 'auto'; el.style.height = el.scrollHeight + 'px'; };
    grow();
    el.addEventListener('input', grow);
    return { destroy() { el.removeEventListener('input', grow); } };
  }

  // Re-analyze lives at the END of the card; clicking it scrolls back to the
  // top of the card first so the user sees the fresh report as it arrives.
  function analyzeOutline() {
    document.getElementById('CardSection_OutlineAnalysis')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    api('POST', '/api/outline/analyze')
      .then(() => addToast($t('outline.analysis.toasts.started'), 'info'))
      .catch((e) => addToast(e.message, 'error'));
  }

  // Ask the editor to draft concrete outline-level replacements for a point
  // that came back without automatic ones — or to ADD more proposals on top
  // of the existing ones (each section keeps its own button and the backend
  // accumulates, so previously applied improvements are never lost).
  async function proposeForSuggestion(s) {
    if (!s?.id) return;
    proposingSuggestionId = s.id;
    try {
      const res = await api('POST', '/api/outline/analysis/propose', { id: s.id });
      if (res?.progress) progress.set(res.progress);
      else progress.set(await api('GET', '/api/progress'));
      addToast($t('outline.analysis.toasts.proposed'), 'success');
    } catch (e) { addToast(e.message, 'error'); }
    finally { proposingSuggestionId = ''; }
  }

  async function applyAnalysisSuggestion(s) {
    if (!s?.id) return;
    applyingSuggestionId = s.id;
    try {
      // Flush any pending local edits so the backend applies exactly what the
      // user sees in the "Replace: Outline" textareas.
      for (const [k, t] of Object.entries(recEditTimers)) {
        if (!k.startsWith(s.id + ':')) continue;
        clearTimeout(t);
        const [id, chStr] = k.split(':');
        await api('POST', '/api/outline/analysis/edit-recommendation', {
          id, chapter_num: Number(chStr), new_outline: recOutlineEdits[k] ?? '',
        }).catch(() => {});
      }
      const res = await api('POST', '/api/outline/analysis/apply', { id: s.id });
      if (res?.progress) progress.set(res.progress);
      else progress.set(await api('GET', '/api/progress'));
      addToast($t('outline.analysis.toasts.applied', { n: res?.chapters ?? 0 }), 'success');
    } catch (e) { addToast(e.message, 'error'); }
    finally { applyingSuggestionId = ''; }
  }

  async function clearOutlineAnalysis() {
    showConfirm($t('outline.analysis.clear') + '?', async () => {
      try {
        await api('DELETE', '/api/outline/analysis');
        progress.set(await api('GET', '/api/progress'));
        addToast($t('outline.analysis.toasts.cleared'), 'success');
      } catch (e) { addToast(e.message, 'error'); }
    });
  }

  function chapterTitleOf(num) {
    const ch = chapters.find(c => c.num === num);
    return ch ? ch.title : '';
  }

  async function generateContinuation() {
    if (!planningRequirements.trim() || !validCount || batchBlocked) return;
    if (endingIntent !== 'serial' && endingStyle === 'custom' && !endingRequirements.trim()) return;
    const body = { chapter_count: Number(continuationCount), outline_synopsis: planningRequirements.trim(), long_term_direction: longTermDirection.trim(), mode: replacingBatch ? 'replace_last' : 'append', batch_id: replacingBatch?.id || 0 };
    Object.assign(body, { ending_intent: endingIntent, ending_style: endingIntent === 'serial' ? '' : endingStyle, ending_requirements: endingRequirements.trim() });
    const submitted = { revision: replacingBatch ? (replacingBatch.revision || 0) + 1 : 1, start: batchStart, end: batchStart + body.chapter_count - 1, synopsis: body.outline_synopsis };
    const run = async () => {
      try {
        await api('POST', '/api/outline/generate-continuation', body);
        submittedBatch = submitted;
        addToast($t('outline.toasts.continuationStarted'), 'info');
      } catch (e) { addToast(e.message, 'error'); }
    };
    if (replacingBatch) showConfirm($t('outline.batch.replaceConfirm'), run);
    else if (batches.some(b => b.planned_final)) showConfirm($t('ending.continueConfirm'), () => { body.confirm_continue = true; return run(); });
    else await run();
  }

  function startEdit(ch) {
    editingNum = ch.num;
    editTitle = ch.title;
    editOutline = ch.outline;
    editCharactersText = formatCharactersEdit(ch.characters);
  }

  function cancelEdit() {
    editingNum = -1;
  }

  async function saveEdit() {
    if (!editTitle.trim() || !editOutline.trim()) { addToast($t('outline.toasts.editRequired'), 'error'); return; }
    try {
      await api('PUT', '/api/outline/' + editingNum, {
        title: editTitle.trim(),
        outline: editOutline.trim(),
        characters: parseCharactersEdit(editCharactersText),
      });
      progress.set(await api('GET', '/api/progress'));
      addToast($t('outline.toasts.editSaved', { num: editingNum }), 'success');
      editingNum = -1;
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function previewImportSplit() {
    const content = importContent.trim();
    if (!content) { addToast($t('outline.toasts.importContentRequired'), 'error'); return; }
    try {
      const res = await api('POST', '/api/import/split', { content });
      importPreview = res.chapters || [];
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function startImport() {
    try {
      await api('POST', '/api/import/start', { content: importContent.trim() });
      addToast($t('outline.toasts.importStarted'), 'info');
      showImport = false;
      importContent = '';
      importPreview = null;
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function resumeImport() {
    try {
      await api('POST', '/api/import/resume');
      addToast($t('outline.toasts.importResumed'), 'info');
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function confirmCharacterSuggestions() {
    const selected = $outlineCharacterSuggestions.filter(s => s._selected !== false);
    if (selected.length === 0) {
      addToast($t('outline.charSuggestions.noneSelected'), 'error');
      return;
    }
    try {
      await api('POST', '/api/outline/characters/confirm', { characters: selected });
      settings.set(await api('GET', '/api/settings'));
      outlineCharacterSuggestions.set([]);
      outlineCharacterShowSuggestions.set(false);
      addToast($t('outline.charSuggestions.adopted', { n: selected.length }), 'success');
    } catch (e) { addToast(e.message, 'error'); }
  }

  function dismissCharacterSuggestions() {
    outlineCharacterSuggestions.set([]);
    outlineCharacterShowSuggestions.set(false);
  }
  async function reviewStory() {
    try {
      await api("POST", "/api/story/review");
      addToast($t("outline.dynamic.reviewStarted"), "info");
    } catch (e) { addToast(e.message, "error"); }
  }

  // —— Pre-planning (book-level plan above the batch cards) ——
  let prePlan = null;
  let prePlanLoaded = false;
  let prePlanBusy = false;

  async function loadPrePlan() {
    try {
      prePlan = await api('GET', '/api/outline/preplan');
    } catch { prePlan = null; }
    prePlanLoaded = true;
  }
  $: if (p && !prePlanLoaded) { prePlan = p.pre_planning || null; prePlanLoaded = true; }
  $: if (p?.pre_planning) { prePlan = p.pre_planning; }

  // Same pattern as the config-page Generate buttons: the message carries the
  // [generate-section:pre_planning] tag, which the backend fast path executes
  // deterministically (StartDetachedPrePlanGenerate) before touching the LLM,
  // and the agent's generate_section tool also accepts the key.

  // Module-level busy flag (Config.svelte's sharedGenBusy pattern): Svelte
  // destroys and recreates this page when switching tabs, so a component-local
  // flag lost the "generation running" state mid-flight.
  let sharedPrePlanBusy = false;
  let prePlanPollTimer = null;

  // Same flow as Config.svelte's generateSection(): the message carries the
  // [generate-section:pre_planning] tag, App.svelte's sendToChat posts it to
  // the chat panel (so the user sees the instruction, the agent's tool call
  // and all live logs/token activity in the panel), and the backend fast path
  // starts the detached generation before the summary LLM turn runs.
  async function generatePrePlan() {
    console.log('[generate] clicked', 'pre_planning');
    if (sharedPrePlanBusy) { console.log('[generate] blocked by busy flag', 'pre_planning'); return; }
    sharedPrePlanBusy = true;
    prePlanBusy = true;

    const msg = $t('config.generate.chat.pre_planning');
    // Immediate feedback, same as the config-page buttons' dispatched toast.
    addToast($t('config.generate.chat.dispatched', { section: 'pre_planning' }), 'info');
    try {
      // sendToChat resolves as soon as the POST is accepted; the backend
      // fast path ([generate-section:pre_planning]) starts the detached
      // generation before the LLM turn even runs.
      await sendToChat(msg);
      // Mirror the busy state across pages / SSE reconnects.
      taskRunning.set(true);
      startPrePlanPolling();
    } catch (e) {
      console.log('[preplan] chat dispatch failed:', e?.message || String(e));
      clearPrePlanBusy();
      addToast(e?.message || String(e), 'error');
    }
  }

  function clearPrePlanBusy() {
    sharedPrePlanBusy = false;
    prePlanBusy = false;
    if (prePlanPollTimer) { clearInterval(prePlanPollTimer); prePlanPollTimer = null; }
  }

  // Same /api/status poller Config.svelte uses after a chat-dispatched
  // generation: when the server-side task ends, refresh the plan and release
  // the busy flags (with a 10-minute cap so the buttons can't deadlock).
  function startPrePlanPolling() {
    if (prePlanPollTimer) return;
    const startedAt = Date.now();
    prePlanPollTimer = setInterval(async () => {
      let st = null;
      try { st = await api('GET', '/api/status').catch(() => null); } catch (e) {}
      if (st?.is_task_running && Date.now() - startedAt < 600000) return; // still generating
      clearPrePlanBusy();
      taskRunning.set(false);
      prePlanLoaded = false;
      await loadPrePlan();
      try { progress.set(await api('GET', '/api/progress')); } catch (e) {}
      addToast($t('config.generate.done'), 'success');
    }, 1200);
  }

  async function deletePrePlan() {
    showConfirm($t('outline.preplan.deleteConfirm'), async () => {
      try {
        await api('DELETE', '/api/outline/preplan');
        prePlan = null;
        progress.set(await api('GET', '/api/progress'));
        addToast($t('outline.preplan.deleted'), 'success');
      } catch (e) { addToast(e.message, 'error'); }
    });
  }

  // Fill the batch-planning card with one act's suggestion and scroll to it.
  function usePrePlanAct(act) {
    continuationCount = Math.min(36, Math.max(1, act.suggested_chapters || 5));
    planningRequirements = act.batch_synopsis || '';
    if (prePlan?.long_term_direction && !longTermDirection.trim()) {
      longTermDirection = prePlan.long_term_direction;
    }
    replacingBatch = null;
    document.getElementById('CardSection_BatchPlanning')?.scrollIntoView({ behavior: 'smooth' });
  }
</script>

<div class="space-y-3">
  <div id="CardSection_StoryPrePlan" class="card bg-base-200">
    <div class="card-body p-4 gap-3">
      <div class="flex items-center justify-between gap-2 flex-wrap">
        <h3 class="card-title text-base">{$t('outline.preplan.title')}</h3>
        <div class="flex gap-2">
          {#if prePlan}
            <button class="btn btn-sm btn-ghost" on:click={deletePrePlan}>{$t('outline.preplan.clear')}</button>
          {/if}
          <AiGenerateButton busy={prePlanBusy} disabled={prePlanBusy} text={prePlan ? $t('outline.preplan.regenerate') : $t('outline.preplan.generate')} title={prePlan ? $t('outline.preplan.regenerate') : $t('outline.preplan.generate')} on:click={generatePrePlan} />
        </div>
      </div>
      <p class="text-xs text-base-content/70">{$t('outline.preplan.help')}</p>
      {#if !prePlan}
        <div class="text-sm text-base-content/60 border border-dashed rounded-lg p-4 text-center">{$t('outline.preplan.empty')}</div>
      {:else}
        <div class="flex flex-wrap gap-4 text-sm">
          <div><span class="font-semibold">{$t('outline.preplan.total')}:</span> {prePlan.total_chapters}</div>
          {#if prePlan.based_on_structure}<div><span class="font-semibold">{$t('outline.preplan.structure')}:</span> {prePlan.based_on_structure}</div>{/if}
        </div>
        {#if prePlan.long_term_direction}
          <div>
            <div class="text-sm font-semibold mb-1">{$t('outline.preplan.direction')}</div>
            <div class="whitespace-pre-wrap text-sm rounded-lg bg-base-100 border p-3">{prePlan.long_term_direction}</div>
          </div>
        {/if}
        <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
          {#each prePlan.acts || [] as act, i}
            <div class="card bg-base-100 border shadow-sm">
              <div class="card-body p-3 gap-2">
                <div class="flex items-start justify-between gap-2">
                  <h4 class="font-semibold text-sm">{$t('outline.preplan.act', { n: i + 1 })}: {act.name}</h4>
                  <button class="btn btn-xs btn-primary shrink-0" on:click={() => usePrePlanAct(act)}>{$t('outline.preplan.use')}</button>
                </div>
                {#if act.summary}<p class="text-xs text-base-content/80">{act.summary}</p>{/if}
                <div class="text-xs">
                  <span class="badge badge-outline badge-sm">{$t('outline.preplan.chapters', { n: act.suggested_chapters })}</span>
                  {#if act.start_ch}<span class="badge badge-ghost badge-sm ml-1">{$t('outline.preplan.range', { start: act.start_ch, end: act.end_ch })}</span>{/if}
                </div>
                {#if act.batch_synopsis}
                  <details class="text-xs">
                    <summary class="cursor-pointer text-base-content/70">{$t('outline.preplan.synopsis')}</summary>
                    <p class="whitespace-pre-wrap mt-1 text-base-content/80">{act.batch_synopsis}</p>
                  </details>
                {/if}
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </div>

  <div id="CardSection_BatchPlanning" class="card bg-base-200">
    <div class="card-body p-4 gap-3">
      <h3 class="card-title text-base">{$t(batchActionKey)}</h3>
      <label class="block text-sm" for="batch-count">{$t('outline.batch.count')}</label>
      <div class="flex flex-wrap items-center gap-x-3 gap-y-2">
        <input id="batch-count" type="number" min="1" max="36" step="1" class="input input-sm w-24" bind:value={continuationCount} disabled={batchBlocked} aria-describedby="batch-range" />
        <p id="batch-range" class="text-sm text-base-content/70" aria-live="polite">
          {#if validCount}
            {$t(replacingBatch
              ? (Number(continuationCount) === 1 ? 'outline.batch.regenerateSingle' : 'outline.batch.regenerateRange')
              : (Number(continuationCount) === 1 ? 'outline.batch.generateSingle' : 'outline.batch.generateRange'),
              { start: batchStart, end: batchStart + Number(continuationCount) - 1 })}
          {/if}
        </p>
      </div>
      <label class="block text-sm" for="batch-synopsis">{$t('outline.batch.synopsis')}</label>
      <textarea id="batch-synopsis" class="textarea w-full" bind:value={planningRequirements} placeholder={$t('outline.batch.placeholder')} disabled={batchBlocked}></textarea>
      <label class="block text-sm" for="batch-direction">{$t('ending.direction')}</label>
      <p class="text-xs text-base-content/70">{$t('ending.directionHelp')}</p>
      <textarea id="batch-direction" class="textarea textarea-sm w-full" bind:value={longTermDirection} placeholder={$t('ending.directionExample')} disabled={batchBlocked}></textarea>
      <label class="block text-sm" for="ending-intent">{$t('ending.intent')}</label>
      <!-- svelte-ignore a11y-no-redundant-roles -->
      <SmartSelect
        id="ending-intent"
        options={[
          { value: 'serial', label: $t('ending.serial') },
          { value: 'final', label: $t('ending.final') },
          { value: 'sequel', label: $t('ending.sequel') },
        ]}
        allowCustom={false}
        allowClear={false}
        disabled={batchBlocked}
        ariaLabel={$t('ending.intent')}
        bind:value={endingIntent}
      />
      {#if endingIntent !== 'serial'}
        <label class="block text-sm" for="ending-style">{$t('ending.style')}</label>
        <!-- svelte-ignore a11y-no-redundant-roles -->
        <SmartSelect
          id="ending-style"
          options={[
            { value: 'closed', label: $t('ending.closed') },
            { value: 'open', label: $t('ending.open') },
            { value: 'custom', label: $t('ending.custom') },
          ]}
          allowCustom={false}
          allowClear={false}
          disabled={batchBlocked}
          ariaLabel={$t('ending.style')}
          bind:value={endingStyle}
        />
        <p class="text-xs text-base-content/70">{$t('ending.help')}</p>
        <label class="block text-sm" for="ending-requirements">{$t('ending.requirements')}</label>
        <textarea id="ending-requirements" class="textarea textarea-sm w-full" bind:value={endingRequirements} required={endingStyle === 'custom'} disabled={batchBlocked}></textarea>
      {/if}
      <div class="flex justify-end gap-2">
        {#if replacingBatch}<button class="btn btn-ghost btn-sm" disabled={$taskRunning} on:click={() => { replacingBatch = null; planningRequirements = ''; }}>{$t('common.cancel')}</button>{/if}
        <button class="btn btn-primary btn-sm" on:click={generateContinuation} disabled={batchBlocked || !validCount || !planningRequirements.trim() || (endingIntent !== 'serial' && endingStyle === 'custom' && !endingRequirements.trim())}>{$t(batchActionKey)}</button>
      </div>
    </div>
  </div>
  {#if !hasOutline}
    <!-- Empty state -->
    <div id="OutlineEmptyState" class="text-center py-14 text-base-content/65">
      <div class="text-5xl mb-3"></div>
      <p class="text-base mb-1">{$t('outline.empty.title')}</p>
      <p class="text-sm text-base-content/65 mb-6">{$t('outline.empty.hint')}</p>
      <button class="btn btn-outline btn-sm" on:click={() => showImport = !showImport} disabled={$taskRunning}>{$t('outline.btn.import')}</button>
    </div>

    {#if showImport}
      <div id="CardSection_ImportExistingText" class="card bg-base-200">
        <div class="card-body p-4 gap-2">
          <h3 class="card-title text-base">{$t('outline.import.title')}</h3>
          <p class="text-xs text-base-content/65">{$t('outline.import.hint')}</p>
          <textarea class="textarea w-full text-sm font-serif" bind:value={importContent} on:input={() => importPreview = null} placeholder={$t('outline.import.placeholder')} disabled={$taskRunning}></textarea>
          <div class="flex justify-end gap-2">
            <button class="btn btn-ghost btn-xs" on:click={() => { showImport = false; importContent = ''; importPreview = null; }}>{$t('common.cancel')}</button>
            <button class="btn btn-primary btn-xs" on:click={previewImportSplit} disabled={$taskRunning || !importContent.trim()}>{$t('outline.import.preview')}</button>
          </div>

          {#if importPreview}
            <div class="bg-base-100 rounded-lg p-3 space-y-2">
              <div class="text-sm font-medium">{$t('outline.import.previewTitle', { n: importPreview.length })}</div>
              <div class="max-h-64 overflow-y-auto space-y-1">
                {#each importPreview as ch (ch.num)}
                  <div class="bg-base-100/50 rounded p-2 text-xs flex items-baseline gap-2">
                    <span class="font-bold text-base-content/65 w-8 shrink-0">{ch.num}</span>
                    <span class="font-medium shrink-0">{ch.title}</span>
                    <span class="text-base-content/65 shrink-0">{$t('outline.import.words', { n: ch.word_count })}</span>
                    <span class="text-base-content/65 truncate">{ch.preview}</span>
                  </div>
                {/each}
              </div>
              <p class="text-xs text-base-content/65">{$t('outline.import.startHint')}</p>
              <div class="flex justify-end">
                <button class="btn btn-success btn-xs" on:click={startImport} disabled={$taskRunning || importPreview.length === 0}>{$t('outline.import.start')}</button>
              </div>
            </div>
          {/if}
        </div>
      </div>
    {/if}
  {:else}
    {#if importStatus}
      <div id="OutlineImportResumeBanner" class="alert alert-warning py-2 text-sm flex items-center justify-between">
        <span>{$t('outline.import.resumeBanner', { done: importStatus.cursor, total: importStatus.total })}</span>
        <button class="btn btn-primary btn-xs" on:click={resumeImport} disabled={$taskRunning}>{$t('outline.import.resume')}</button>
      </div>
    {/if}
    <ConfigChangePanel />

    {#if $outlineCharacterShowSuggestions && $outlineCharacterSuggestions.length > 0}
      <div id="CardSection_CharacterSuggestions" class="card bg-base-200 border border-primary/30 ">
        <div class="card-body py-4 gap-3">
          <h3 class="font-semibold">{$t('outline.charSuggestions.title', { n: $outlineCharacterSuggestions.length })}</h3>
          <p class="text-sm text-base-content/60">{$t('outline.charSuggestions.hint')}</p>
          <div class="space-y-2 max-h-72 overflow-y-auto">
            {#each $outlineCharacterSuggestions as s}
              <label class="flex gap-3 p-3 rounded-lg bg-base-100/50 cursor-pointer">
                <input type="checkbox" class="checkbox checkbox-sm mt-1" bind:checked={s._selected} />
                <div class="min-w-0 flex-1">
                  <div class="font-medium">{s.name}</div>
                  {#if s.description}
                    <div class="text-sm text-base-content/70 mt-1">{s.description}</div>
                  {/if}
                  <div class="text-xs text-base-content/65 mt-1">
                    {$t('outline.charSuggestions.line', { chapter: s.chapter_num, role: s.role || $t('outline.charSuggestions.noRole') })}
                  </div>
                </div>
              </label>
            {/each}
          </div>
          <div class="flex gap-2">
            <button class="btn btn-primary btn-sm" disabled={$taskRunning} on:click={confirmCharacterSuggestions}>{$t('outline.charSuggestions.adopt')}</button>
            <button class="btn btn-ghost btn-sm" on:click={dismissCharacterSuggestions}>{$t('outline.charSuggestions.dismiss')}</button>
          </div>
        </div>
      </div>
    {/if}

    <!-- Action bar -->
    <div id="OutlineActionsBar" class="card bg-base-200">
      <div class="card-body p-4 gap-2">
        <div class="flex items-center gap-2 flex-wrap">
          <h3 class="text-base font-semibold flex-1 min-w-0 truncate">{displayTitle || $t('common.untitled')}</h3>
          {#if inOutlinePhase}
            <button class="btn btn-success btn-xs" on:click={confirmOutline} disabled={$taskRunning || chapters.length === 0}>{$t('outline.btn.confirm')}</button>
          {/if}
          <button class="btn btn-outline btn-xs" on:click={reviewStory} disabled={$taskRunning || !hasAccepted}>{$t('outline.dynamic.review')}</button>
          <button class="btn btn-outline btn-xs" on:click={() => showRevise = !showRevise} disabled={$taskRunning}>{$t('outline.btn.revise')}</button>
          {#if !hasAccepted}
            <button class="btn btn-error btn-outline btn-xs" on:click={deleteOutline} disabled={$taskRunning}>{$t('outline.btn.deleteOutline')}</button>
          {/if}
        </div>

        {#if showRevise}
          <div class="bg-base-100 rounded-lg p-3 space-y-2">
            <textarea class="textarea textarea-sm w-full text-sm" bind:value={reviseFeedback} placeholder={$t('outline.revise.placeholder')} disabled={$taskRunning}></textarea>
            <div class="flex justify-between items-center">
              <span class="text-xs text-base-content/65">{$t('outline.revise.hint')}</span>
              <div class="flex gap-2">
                <button class="btn btn-ghost btn-xs" on:click={() => { showRevise = false; reviseFeedback = ''; }}>{$t('common.cancel')}</button>
                <button class="btn btn-primary btn-xs" on:click={reviseOutline} disabled={$taskRunning || !reviseFeedback.trim()}>{$t('outline.revise.submit')}</button>
              </div>
            </div>
          </div>
        {/if}

		{#if p.latest_planning_review}
			<div class="bg-secondary/10 border border-secondary/30 rounded p-3 text-sm whitespace-pre-wrap">{p.latest_planning_review.content}</div>
		{/if}

        {#if p.core_prompt}
          <div>
            <span class="text-xs text-base-content/65">{$t('outline.corePrompt')}</span>
            <div class="bg-base-100 rounded p-2 text-sm mt-0.5 max-h-24 overflow-y-auto">{p.core_prompt}</div>
          </div>
        {/if}

      </div>
    </div>

    <!-- Chapter outline list -->
    <div id="ChapterOutlineList" class="card bg-base-200">
      <div class="card-body p-4 gap-2">
        <div class="flex items-center justify-between">
          <h4 class="text-sm font-semibold text-base-content/60">{$t('outline.chapterList')} <span class="font-normal text-base-content/65">{$t('outline.chapterList.summary', { total: projectChapterCount, suffix: pendingCount ? $t('outline.chapterList.pendingSuffix', { n: pendingCount }) : '' })}</span></h4>
          <span class="text-xs text-base-content/65">{$t('outline.chapterList.editHint')}</span>
        </div>
        <div class="space-y-1.5">
          {#each groups as group (group.id)}
            <section id="OutlineBatchGroup" class="border border-base-content/10 rounded-lg p-3 space-y-2">
              <div class="flex items-center justify-between gap-2">
                <h4 class="font-semibold text-sm">{group.id ? $t('outline.batch.range', { start: group.start_ch, end: group.end_ch }) : $t('outline.batch.imported')}</h4>
                {#if group.planned_final}<span class="badge badge-info">{$t('ending.marker', {num: group.end_ch})}</span>{/if}
                {#if canReplan(group)}<button class="btn btn-outline btn-xs" disabled={batchBlocked} on:click={() => replan(group)}>{$t('outline.batch.replan')}</button>{/if}
              </div>
              {#if group.synopsis}<p class="whitespace-pre-wrap text-sm text-base-content/70 mb-3">{group.synopsis}</p>{/if}
          {#each group.chapters as ch (ch.num)}
            {#if editingNum === ch.num}
              <div data-outline-chapter={ch.num} id="ChapterCard_Edit" class="bg-base-100 rounded-lg p-3 space-y-2 ring-1 ring-primary/50">
                <div class="flex items-center gap-2">
                  <span class="text-sm font-bold text-base-content/65 shrink-0">{$t('outline.chapter.chapterLabel', { num: ch.num })}</span>
                  <input type="text" class="input input-sm flex-1" bind:value={editTitle} placeholder={$t('outline.chapter.titlePlaceholder')} disabled={$taskRunning} />
                </div>
                <textarea class="textarea textarea-sm w-full text-sm" bind:value={editOutline} placeholder={$t('outline.chapter.outlinePlaceholder')} disabled={$taskRunning}></textarea>
                <div>
                  <label for="chapter-cast" class="text-xs text-base-content/65 mb-1 block">{$t('outline.chapter.castLabel')}</label>
                  <textarea id="chapter-cast" class="textarea textarea-sm w-full text-sm font-mono" bind:value={editCharactersText} placeholder={$t('outline.chapter.castPlaceholder')} disabled={$taskRunning}></textarea>
                  <p class="text-xs text-base-content/65 mt-0.5">{$t('outline.chapter.castHint')}</p>
                </div>
                <div class="flex justify-end gap-2">
                  <button class="btn btn-ghost btn-xs" on:click={cancelEdit}>{$t('common.cancel')}</button>
                  <button class="btn btn-success btn-xs" on:click={saveEdit} disabled={$taskRunning}>{$t('common.save')}</button>
                </div>
              </div>
            {:else}
              <!-- svelte-ignore a11y-click-events-have-key-events -->
              <!-- svelte-ignore a11y-no-static-element-interactions -->
              <div
                data-outline-chapter={ch.num}
                id="ChapterCard_Summary"
                class="bg-base-100 rounded-lg p-2.5 group {isOutlineEditable(ch.status) && !$taskRunning ? 'cursor-pointer hover:ring-1 hover:ring-primary/40' : ''}"
                on:click={() => isOutlineEditable(ch.status) && !$taskRunning && startEdit(ch)}
              >
                <div class="flex items-center gap-2">
                  <span class="text-sm font-bold text-base-content/65 w-12 shrink-0">{ch.num}</span>
                  <span class="text-sm font-medium flex-1 min-w-0 truncate">{ch.title}</span>
                  <span class="badge badge-xs {statusMeta[ch.status]?.cls || 'badge-ghost'}">{statusMeta[ch.status]?.label || ch.status}</span>
                  {#if isOutlineEditable(ch.status)}
                    <span class="text-xs text-primary opacity-0 group-hover:opacity-100 transition-opacity shrink-0">{$t('outline.chapter.editTag')}</span>
                  {/if}
                </div>
                {#if ch.characters?.length}
                  <div class="flex flex-wrap gap-1 mt-1.5 ml-14">
                    {#each ch.characters as c}
                      <span class="badge badge-ghost badge-xs gap-0.5" title={c.note || ''}>
                        {c.name}{#if c.first_appearance}<span class="text-warning">*</span>{/if}
                      </span>
                    {/each}
                  </div>
                {/if}
                <p class="text-xs text-base-content/65 mt-1 ml-14 line-clamp-2">{ch.outline}</p>
              </div>
            {/if}
          {/each}
            </section>
          {/each}
        </div>

        {#if $streamingChapterIdx >= 0 && $streamingContent}
          <div class="bg-base-100 rounded p-3 mt-1 text-sm max-h-48 overflow-y-auto chapter-content">
            <div class="text-xs text-base-content/65 mb-1 flex items-center gap-1">
              <span class="loading loading-dots loading-xs"></span> {$t('outline.streamHint')}
            </div>
            {$streamingContent}
          </div>
        {/if}
      </div>
    </div>
    <!-- Bestseller-editor full-outline analysis -->
    {#if hasOutline}
      <div id="CardSection_OutlineAnalysis" class="card bg-base-200">
        <div class="card-body p-4 gap-3">
          <div class="flex items-start justify-between gap-2 flex-wrap">
            <div>
              <h3 class="card-title text-base">{$t('outline.analysis.title')}</h3>
              <span class="field-label">{$t('outline.analysis.subtitle')}</span>
            </div>
            {#if outlineAnalysis}
              <button class="btn btn-ghost btn-xs shrink-0" on:click={clearOutlineAnalysis} disabled={$taskRunning}>{$t('outline.analysis.clear')}</button>
            {/if}
          </div>

          {#if outlineAnalysis}
            <div class="space-y-3">
              <div class="flex items-center gap-3 text-sm text-base-content/70">
                {#if outlineAnalysis.overall_score}
                  <span class="badge badge-lg {outlineAnalysis.overall_score >= 80 ? 'badge-success' : outlineAnalysis.overall_score >= 60 ? 'badge-warning' : 'badge-error'}">
                    {$t('outline.analysis.score')}: {outlineAnalysis.overall_score}/100
                  </span>
                {/if}
                {#if outlineAnalysis.based_on_structure}
                  <span>{outlineAnalysis.based_on_structure}</span>
                {/if}
                <span class="ml-auto text-xs">{outlineAnalysis.generated_at?.slice(0, 10)}</span>
              </div>

              {#if outlineAnalysis.summary}
                <details open class="bg-base-100 rounded-lg p-3">
                  <summary class="cursor-pointer text-sm font-semibold">{$t('outline.analysis.summary')}</summary>
                  <p class="whitespace-pre-wrap mt-2 text-sm text-base-content/80">{outlineAnalysis.summary}</p>
                </details>
              {/if}

              {#if outlineAnalysis.pillar_scores?.length}
                {@const pillarCatMap = {
                  reader_profile: ['market'],
                  inspirational_pieces: ['other', 'hook'],
                  tone: ['tone'],
                  story_structure: ['structure', 'pacing']
                }}
                {@const hasOpenSuggestionFor = (key) =>
                  (outlineAnalysis.suggestions || []).some(
                    (s) => (pillarCatMap[key] || []).includes(s.category) && s.status !== 'applied'
                  )}
                <div class="grid grid-cols-2 lg:grid-cols-4 gap-2">
                  {#each outlineAnalysis.pillar_scores as p (p.key)}
                    {@const strong = p.score >= 90 && !hasOpenSuggestionFor(p.key)}
                    <div class="bg-base-100 rounded-lg p-2.5 space-y-1">
                      <div class="flex items-center justify-between gap-1">
                        <span class="text-xs font-semibold truncate">{$t('outline.analysis.pillar.' + p.key)}</span>
                        <span class="badge badge-sm {p.score >= 90 ? 'badge-success' : p.score >= 75 ? 'badge-info' : p.score >= 60 ? 'badge-warning' : 'badge-error'}">{p.score}</span>
                      </div>
                      {#if strong}
                        <div class="text-xs text-success font-medium">✓ {$t('outline.analysis.noChangesNeeded')}</div>
                      {:else}
                        <div class="w-full h-1.5 rounded-full bg-base-content/10 overflow-hidden">
                          <div class="h-full rounded-full {p.score >= 90 ? 'bg-success' : p.score >= 75 ? 'bg-info' : p.score >= 60 ? 'bg-warning' : 'bg-error'}" style="width: {p.score}%"></div>
                        </div>
                      {/if}
                      {#if p.note}<p class="text-[11px] leading-snug text-base-content/60 line-clamp-3">{p.note}</p>{/if}
                      {#if p.checklist?.length}
                        <!-- Transparent sub-criteria: the pillar score is derived
                             from these levels, so this list is literally the
                             roadmap to 100% for this pillar. -->
                        <details class="text-[11px]" title={$t('analysis.checklist.tip')}>
                          <summary class="cursor-pointer text-base-content/55 select-none">{$t('analysis.checklist.title')} ({sortedChecklist(p).length})</summary>
                          <ul class="mt-1 space-y-0.5">
                            {#each sortedChecklist(p) as c (c.key)}
                              <li class="flex items-center justify-between gap-1.5">
                                <span class="truncate text-base-content/70">{criterionLabel(c.key)}</span>
                                <span class="badge badge-xs normal-case {c.level >= 85 ? 'badge-success' : c.level >= 60 ? 'badge-warning' : 'badge-error'}">{c.level}</span>
                              </li>
                            {/each}
                          </ul>
                        </details>
                      {/if}
                    </div>
                  {/each}
                </div>
              {/if}

              <div class="font-semibold text-sm">{$t('outline.analysis.suggestions')} ({outlineAnalysis.suggestions?.length || 0})</div>
              <div class="space-y-2">
                {#each outlineAnalysis.suggestions || [] as s (s.id)}
                  <section class="border border-base-content/10 rounded-lg p-3 space-y-2">
                    <div class="flex items-center gap-2 flex-wrap">
                      <span class="badge badge-outline badge-sm">{$t('outline.analysis.category.' + (s.category || 'other'))}</span>
                      {#if s.priority}
                        <span class="badge badge-sm normal-case {PRIORITY_BADGE[s.priority] || 'badge-ghost'}">{$t('analysis.priority.' + s.priority)}</span>
                      {/if}
                      <h4 class="font-semibold text-sm flex-1 min-w-0">{s.title}</h4>
                      {#if s.score_impact > 0}
                        <span class="badge badge-success badge-sm normal-case" title={$t('analysis.checklist.tip')}>{$t('analysis.scoreImpact', { n: s.score_impact })}</span>
                      {/if}
                      {#if s.status === 'applied'}
                        <span class="badge badge-success badge-sm">✓ {$t('outline.analysis.applied')}</span>
                      {/if}
                    </div>
                    {#if s.criteria?.length}
                      <p class="text-[11px] text-base-content/55">
                        {$t('analysis.criteriaLabel')}: {s.criteria.map(criterionLabel).join(', ')}
                        {#if s.pillar} · {$t('outline.analysis.pillar.' + s.pillar)}{/if}
                      </p>
                    {/if}
                    {#if s.detail}<p class="text-sm text-base-content/80 whitespace-pre-wrap">{s.detail}</p>{/if}
                    {#if s.action}<p class="text-sm text-base-content/70 italic whitespace-pre-wrap">{s.action}</p>{/if}
                    {#if s.applies_to_chapters?.length}
                      <p class="text-xs text-base-content/60">{$t('outline.analysis.chapters', { list: s.applies_to_chapters.join(', ') })}</p>
                    {/if}
                    {#if s.recommendations?.length}
                      <div class="space-y-2">
                        {#each s.recommendations as rec (rec.chapter_num)}
                          <!-- Accordion collapsed by default: the author opens each
                               proposed replacement on purpose before applying. -->
                          <details class="bg-base-100 rounded p-2.5 text-xs">
                            <summary class="cursor-pointer font-semibold select-none">
                              Ch. {rec.chapter_num}{chapterTitleOf(rec.chapter_num) ? ' — ' + chapterTitleOf(rec.chapter_num) : ''}
                              {#if rec.cast?.length}<span class="badge badge-outline badge-xs ml-1 normal-case">{$t('outline.analysis.castBadge')}: {rec.cast.map(c => c.name).join(', ')}</span>{/if}
                            </summary>
                            <div class="space-y-1.5 pt-1.5">
                            {#if rec.new_title}
                              <div><span class="text-base-content/60">{$t('outline.analysis.replacePreview')} Title:</span> <span class="line-through opacity-60">{chapterTitleOf(rec.chapter_num)}</span></div>
                              <div><span class="text-base-content/60">{$t('outline.analysis.with')}</span> <span class="font-medium">{rec.new_title}</span></div>
                            {/if}
                            {#if rec.new_outline}
                              <div>
                                <div class="text-base-content/60">{$t('outline.analysis.replacePreview')} Outline</div>
                                <!-- Editable before applying: the user can tweak the
                                     editor's proposed chapter plan; edits are saved to
                                     the backend (debounced) and flushed on Apply. -->
                                <textarea
                                  class="textarea textarea-sm w-full text-xs mt-1 leading-relaxed overflow-hidden"
                                  style="min-height: 3rem"
                                  value={recOutlineValue(s, rec)}
                                  use:autosizeRecTextarea
                                  on:input={(e) => onRecOutlineInput(s, rec, e)}
                                  disabled={$taskRunning || s.status === 'applied'}
                                  placeholder={$t('outline.analysis.editableHint')}
                                ></textarea>
                                <p class="text-[10px] text-base-content/45">{$t('outline.analysis.editableHint')}</p>
                              </div>
                            {/if}
                            {#if rec.cast?.length}
                              {@const currentCast = (chapters.find(c => c.num === rec.chapter_num)?.characters || []).map(c => c.name)}
                              <div>
                                <div class="text-base-content/60">{$t('outline.analysis.replacePreview')} {$t('outline.analysis.castTitle')}</div>
                                {#if currentCast.length}
                                  <div class="line-through opacity-60">{currentCast.join('; ')}</div>
                                {/if}
                                <ul class="mt-1 space-y-0.5">
                                  {#each rec.cast as cc (cc.name)}
                                    <li>
                                      <span class="font-medium">{cc.name}</span>
                                      {#if cc.first_appearance}<span class="badge badge-info badge-xs ml-1 normal-case">{$t('outline.analysis.castDebut')}</span>{/if}
                                      {#if cc.note}<span class="text-base-content/55"> — {cc.note}</span>{/if}
                                    </li>
                                  {/each}
                                </ul>
                                <p class="text-[10px] text-base-content/45 mt-0.5">{$t('outline.analysis.castNote')}</p>
                              </div>
                            {/if}
                            </div>
                          </details>
                        {/each}
                      </div>
                      {#if s.status !== 'applied'}
                        <div class="flex justify-end gap-2">
                          <!-- Each section keeps its own Propose button: the backend
                               accumulates new proposals on top of the existing ones,
                               so improvements applied earlier are never discarded. -->
                          <button class="btn btn-outline btn-xs" on:click={() => proposeForSuggestion(s)} disabled={$taskRunning || proposingSuggestionId === s.id}>
                            {#if proposingSuggestionId === s.id}<span class="loading loading-spinner loading-xs"></span>{/if}
                            {$t('outline.analysis.propose')}
                          </button>
                          <button class="btn btn-success btn-xs" on:click={() => applyAnalysisSuggestion(s)} disabled={$taskRunning || applyingSuggestionId === s.id}>
                            {#if applyingSuggestionId === s.id}<span class="loading loading-spinner loading-xs"></span>{/if}
                            {$t('outline.analysis.apply')}
                          </button>
                        </div>
                      {/if}
                    {:else if s.status !== 'applied'}
                      <div class="space-y-2">
                        <p class="text-xs text-base-content/50">
                          {$t(analysisHasProposals ? 'outline.analysis.noApply' : 'outline.analysis.adviceOnly')}
                        </p>
                        <div class="flex justify-end">
                          <button class="btn btn-outline btn-xs" on:click={() => proposeForSuggestion(s)} disabled={$taskRunning || proposingSuggestionId === s.id}>
                            {#if proposingSuggestionId === s.id}<span class="loading loading-spinner loading-xs"></span>{/if}
                            {$t('outline.analysis.propose')}
                          </button>
                        </div>
                      </div>
                    {/if}
                  </section>
                {/each}
              </div>
            </div>
          {/if}

          <div class="flex justify-end pt-1">
            <button class="btn btn-primary btn-sm" on:click={analyzeOutline} disabled={$taskRunning || chapters.length === 0}>
              {#if $taskRunning && outlineAnalysis == null}
                <span class="loading loading-spinner loading-xs"></span>
              {/if}
              {$t(outlineAnalysis ? 'outline.analysis.reanalyze' : 'outline.analysis.btn')}
            </button>
          </div>
        </div>
      </div>
    {/if}
  {/if}

</div>
