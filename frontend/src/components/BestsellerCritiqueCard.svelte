<script>
  // Bestseller-editor critique card — the Writing-page sibling of the
  // "CardSection_OutlineAnalysis" block in Outline.svelte. One component is
  // reused by three passes that each KNOW what they are reviewing:
  //   kind="sceneplan" : pending per-chapter beat sheets (scene replacements)
  //   kind="act"       : a finished pre-planning act as drafted (trope interventions)
  //   kind="chapter"   : one drafted chapter (trope interventions)
  // The report shape mirrors OutlineAnalysisReport: score badge, editorial
  // letter, pillar grid with "no changes needed", advice-only suggestions and
  // per-suggestion Propose/Apply accordions.
  import { api } from '../lib/api.js';
  import { progress, taskRunning, addToast, confirmModal } from '../lib/stores.js';
  import { t } from '../lib/i18n/index.js';

  export let kind = 'sceneplan';        // 'sceneplan' | 'act' | 'chapter'
  export let report = null;             // ScenePlanAnalysisReport | ActAnalysisReport | ChapterAnalysisReport
  export let actNo = 0;                 // required for kind="act" endpoints
  export let chapterNum = 0;            // required for kind="chapter" endpoints
  export let disabled = false;          // usually $taskRunning
  export let hasTargets = true;         // hide the analyze button when nothing to review
  export let scrollId = '';             // element id to scroll into view on Analyze

  // Endpoint map per pass. Bodies always carry id (+act_no/chapter_num where
  // the route needs it); responses use {progress} like the outline ones.
  const ENDPOINTS = {
    sceneplan: {
      analyze: () => ['POST', '/api/scene-plan/analyze', {}],
      propose: (id) => ['POST', '/api/scene-plan/analysis/propose', { id }],
      apply: (id) => ['POST', '/api/scene-plan/analysis/apply', { id }],
      edit: (id, payload) => ['POST', '/api/scene-plan/analysis/edit-recommendation', { id, ...payload }],
      clear: () => ['DELETE', '/api/scene-plan/analysis'],
    },
    act: {
      analyze: () => ['POST', '/api/acts/analyze', { act_no: actNo }],
      propose: (id) => ['POST', '/api/acts/analysis/propose-interventions', { act_no: actNo, id }],
      apply: (id) => ['POST', '/api/acts/analysis/apply-interventions', { act_no: actNo, id }],
      clear: () => ['DELETE', '/api/acts/analysis?act=' + actNo],
    },
    chapter: {
      analyze: () => ['POST', `/api/chapters/${chapterNum}/analyze`, {}],
      propose: (id) => ['POST', `/api/chapters/${chapterNum}/analysis/propose-interventions`, { id }],
      apply: (id) => ['POST', `/api/chapters/${chapterNum}/analysis/apply-interventions`, { id }],
      clear: () => ['DELETE', `/api/chapters/${chapterNum}/critique`],
    },
  };

  let proposingSuggestionId = '';
  let applyingSuggestionId = '';

  // Whole-card accordion: the critique card itself collapses to a single small
  // horizontal strip (title + score badge only) with a smooth height animation,
  // so long reports don't dominate the page. Inner sections render exactly as
  // before once the card is expanded — no per-section collapsing.
  // Default state: chapter cards start collapsed (they sit under every drafted
  // chapter); scene-plan / act cards start expanded.
  let cardOpen = null; // null = use per-kind default
  $: cardIsOpen = cardOpen ?? kind !== 'chapter';
  function toggleCard() {
    cardOpen = !cardIsOpen;
  }

  // Local (unsaved) edits in a proposed scene replacement's textarea, keyed
  // `${suggestionId}:${chapter}:${scene}:${field}` — same trick as the
  // outline card: debounced persistence so edits survive reloads and Apply
  // writes exactly what the author sees.
  let recEdits = {};
  let recEditTimers = {};

  function recEditKey(s, rec, field) { return s.id + ':' + rec.chapter_num + ':' + rec.scene_id + ':' + field; }

  function recValue(s, rec, field) {
    const k = recEditKey(s, rec, field);
    if (k in recEdits) return recEdits[k];
    if (field === 'new_title') return rec.new_title || '';
    if (field === 'new_pov') return rec.new_pov || '';
    if (field === 'new_description') return rec.new_description || '';
    return rec.new_words_budget ? String(rec.new_words_budget) : '';
  }

  function persistRecEdit(s, rec, field) {
    const ep = ENDPOINTS[kind]?.edit;
    if (!ep) return;
    const k = recEditKey(s, rec, field);
    const val = recEdits[k];
    if (val === undefined) return;
    clearTimeout(recEditTimers[k]);
    recEditTimers[k] = setTimeout(() => {
      api(...ep(s.id, { chapter_num: rec.chapter_num, scene_id: rec.scene_id, field, value: val })).catch(() => {});
    }, 400);
  }

  function onRecInput(s, rec, field, e) {
    recEdits = { ...recEdits, [recEditKey(s, rec, field)]: e.target.value };
    persistRecEdit(s, rec, field);
  }

  async function flushRecEdits(s) {
    const ep = ENDPOINTS[kind]?.edit;
    if (!ep) return;
    for (const [k, timer] of Object.entries(recEditTimers)) {
      if (!k.startsWith(s.id + ':')) continue;
      clearTimeout(timer);
      const [, chStr, scStr, field] = k.split(':');
      await api(...ep(s.id, {
        chapter_num: Number(chStr), scene_id: Number(scStr), field, value: recEdits[k] ?? '',
      })).catch(() => {});
    }
  }

  function autosizeTextarea(el) {
    if (!el) return;
    const grow = () => { el.style.height = 'auto'; el.style.height = el.scrollHeight + 'px'; };
    grow();
    el.addEventListener('input', grow);
    return { destroy() { el.removeEventListener('input', grow); } };
  }

  // --- Rubric helpers (shared by every pass) -------------------------------
  // Transparent sub-criteria: each pillar card lists its checklist items with
  // the graded level, weakest first — literally "what to improve to reach
  // 100%" since the pillar score is derived as their average.
  function criterionLabel(key) {
    const msg = $t('criteria.' + key);
    return msg && msg !== 'criteria.' + key ? msg : key.replace(/_/g, ' ');
  }
  function sortedChecklist(pl) {
    return [...(pl?.checklist || [])].sort((a, b) => a.level - b.level);
  }

  // 3-level importance badge, derived deterministically server-side from the
  // rubric numbers (score impact + criteria repaired).
  const PRIORITY_BADGE = { high: 'badge-error', medium: 'badge-warning', low: 'badge-ghost' };
  function priorityLabel(p) {
    const msg = $t('analysis.priority.' + p);
    return msg && msg !== 'analysis.priority.' + p ? msg : p;
  }

  // Scene-plan merge UI state: per recommendation, whether the author wants
  // this scene absorbed into another one, and which survivor to fold it into.
  // Keyed `${suggestionId}:${chapter}:${scene}`. Persisted through the same
  // debounced edit endpoint via field "merge_into_scene_id".
  let mergeToggles = {};
  function mergeKey(s, rec) { return s.id + ':' + rec.chapter_num + ':' + rec.scene_id; }
  function mergeState(s, rec) {
    const k = mergeKey(s, rec);
    if (k in mergeToggles) return mergeToggles[k];
    return { on: !!rec.merge_into_scene_id, target: rec.merge_into_scene_id || 0 };
  }
  function siblingSceneIds(s, rec) {
    const ids = new Set();
    for (const r of s.recommendations || []) {
      if (r.chapter_num === rec.chapter_num && r.scene_id !== rec.scene_id) ids.add(r.scene_id);
    }
    // Also offer live plan scenes of that chapter (survivor need not be rewritten).
    const ch = ($progress?.chapters || []).find((c) => c.num === rec.chapter_num);
    for (const sc of ch?.scene_plan?.scenes || []) {
      if (sc.id !== rec.scene_id) ids.add(sc.id);
    }
    return [...ids].sort((a, b) => a - b);
  }
  function setMerge(s, rec, on, target) {
    const k = mergeKey(s, rec);
    const st = { ...mergeState(s, rec), on, target: on ? (target || mergeState(s, rec).target || siblingSceneIds(s, rec)[0] || 0) : 0 };
    if (st.on && !st.target) st.on = false;
    mergeToggles = { ...mergeToggles, [k]: st };
    api('POST', '/api/scene-plan/analysis/edit-recommendation', {
      id: s.id, chapter_num: rec.chapter_num, scene_id: rec.scene_id,
      field: 'merge_into_scene_id', value: '', merge_into_scene_id: st.on ? st.target : 0,
    }).catch(() => {});
  }
  function mergedScenesOf(s) {
    // Absorbed scenes declared by the editor (or toggled by the author).
    return (s.recommendations || [])
      .map((r) => ({ r, m: mergeState(s, r) }))
      .filter(({ m }) => m.on && m.target)
      .map(({ r, m }) => $t('sceneplan.analysis.sceneLabel', { id: r.scene_id }) + '→' + $t('sceneplan.analysis.sceneLabel', { id: m.target }));
  }

  function analyze() {
    if (scrollId) document.getElementById(scrollId)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    const [m, u, b] = ENDPOINTS[kind].analyze();
    api(m, u, b)
      .then(() => addToast($t(`${kind}.analysis.toasts.started`), 'info'))
      .catch((e) => addToast(e.message, 'error'));
  }

  async function proposeForSuggestion(s) {
    if (!s?.id) return;
    proposingSuggestionId = s.id;
    try {
      const [m, u, b] = ENDPOINTS[kind].propose(s.id);
      const res = await api(m, u, b);
      if (res?.progress) progress.set(res.progress);
      else progress.set(await api('GET', '/api/progress'));
      addToast($t(`${kind}.analysis.toasts.proposed`), 'success');
    } catch (e) { addToast(e.message, 'error'); }
    finally { proposingSuggestionId = ''; }
  }

  async function applySuggestion(s) {
    if (!s?.id) return;
    applyingSuggestionId = s.id;
    try {
      await flushRecEdits(s);
      const [m, u, b] = ENDPOINTS[kind].apply(s.id);
      const res = await api(m, u, b);
      if (res?.progress) progress.set(res.progress);
      else progress.set(await api('GET', '/api/progress'));
      const n = res?.scenes ?? res?.interventions ?? 0;
      addToast($t(`${kind}.analysis.toasts.applied`, { n }), 'success');
    } catch (e) { addToast(e.message, 'error'); }
    finally { applyingSuggestionId = ''; }
  }

  function clearReport() {
    confirmModal.set({
      message: $t(`${kind}.analysis.clear`) + '?',
      onConfirm: async () => {
        try {
          const [m, u] = ENDPOINTS[kind].clear();
          await api(m, u);
          progress.set(await api('GET', '/api/progress'));
          addToast($t(`${kind}.analysis.toasts.cleared`), 'success');
        } catch (e) { addToast(e.message, 'error'); }
      },
    });
  }

  // Pillar labels always come from the outline catalog (same four pillars in
  // every pass); per-pass catalogs below may override individual keys.
  const PILLAR_PREFIX = {
    reader_profile: 'outline.analysis.pillar.reader_profile',
    inspirational_pieces: 'outline.analysis.pillar.inspirational_pieces',
    tone: 'outline.analysis.pillar.tone',
    story_structure: 'outline.analysis.pillar.story_structure',
  };
  function pillarLabel(key) {
    const shared = `${kind}.analysis.pillar.` + key;
    const msg = $t(shared);
    if (msg && msg !== shared) return msg;
    return $t(PILLAR_PREFIX[key] || shared);
  }

  // A pillar counts as "bestseller-ready" only when strong AND no open
  // suggestion maps to it (identical rule to the outline card).
  const pillarCatMap = {
    reader_profile: ['market', 'hook'],
    inspirational_pieces: ['other', 'hook', 'emotion'],
    tone: ['tone', 'emotion'],
    story_structure: ['structure', 'pacing', 'scene_economy', 'stakes', 'budget', 'pov'],
  };
  function hasOpenSuggestionFor(key) {
    return (report?.suggestions || []).some(
      (s) => (pillarCatMap[key] || ['other']).includes(s.category) && s.status !== 'applied'
    );
  }

  // Category badges reuse the outline catalog, falling back to a prettified
  // label for categories that only exist in these passes.
  const CAT_PREFIX = {
    hook: 'outline.analysis.category.hook',
    pacing: 'outline.analysis.category.pacing',
    stakes: 'outline.analysis.category.stakes',
    structure: 'outline.analysis.category.structure',
    market: 'outline.analysis.category.market',
    tone: 'outline.analysis.category.tone',
    character: 'outline.analysis.category.character',
    emotion: 'outline.analysis.category.other',
    other: 'outline.analysis.category.other',
  };
  function catLabel(cat) {
    const c = cat || 'other';
    const shared = `${kind}.analysis.category.` + c;
    const msg = $t(shared);
    if (msg && msg !== shared) return msg;
    const mapped = CAT_PREFIX[c];
    if (mapped) { const m2 = $t(mapped); if (m2 && m2 !== mapped) return m2; }
    return c.replace(/_/g, ' ');
  }

  // Per-suggestion state so the advice-only vs. no-apply hint reads correctly
  // for each point independently.
  function suggestionHasProposals(s) {
    return !!((s?.recommendations || []).length || (s?.interventions || []).length);
  }
</script>

{#if report}
  <div id={scrollId || undefined} class="card bg-base-200">
    <!-- Collapsed strip: one small horizontal row with just the title + score. -->
    {#if !cardIsOpen}
      <button type="button" class="flex items-center gap-2 px-4 py-2.5 w-full text-left cursor-pointer select-none hover:bg-base-100/60 transition-colors" on:click={toggleCard}>
        <span class="inline-block transition-transform duration-200 ease-out rotate-0 text-base-content/50 text-xs shrink-0">▶</span>
        <span class="text-sm font-semibold truncate">{$t(`${kind}.analysis.title`)}</span>
        {#if kind === 'act' && report.act_name}
          <span class="text-xs text-base-content/60 shrink-0">{$t('act.analysis.actLabel', { no: report.act_no, name: report.act_name, start: report.start_ch, end: report.end_ch })}</span>
        {:else if kind === 'chapter' && report.chapter_num}
          <span class="text-xs text-base-content/60 shrink-0">{$t('chapter.analysis.chapterLabel', { num: report.chapter_num })}</span>
        {/if}
        {#if report.overall_score}
          <span class="badge badge-sm shrink-0 {report.overall_score >= 80 ? 'badge-success' : report.overall_score >= 60 ? 'badge-warning' : 'badge-error'}">{$t(`${kind}.analysis.score`)}: {report.overall_score}/100</span>
        {/if}
        {#if report.suggestions?.length}
          <span class="badge badge-outline badge-sm shrink-0">{report.suggestions.length}</span>
        {/if}
      </button>
    {/if}
    <!-- Animated expand/collapse: grid-rows trick keeps the transition smooth
         for arbitrary content height without measuring in JS. -->
    <div class="grid transition-[grid-template-rows] duration-300 ease-in-out" style="grid-template-rows: {cardIsOpen ? '1fr' : '0fr'};">
      <div class="overflow-hidden min-h-0">
    {#if cardIsOpen}
    <div class="card-body p-4 gap-3">
      <div class="flex items-start justify-between gap-2 flex-wrap">
        <div class="flex items-start gap-1.5 min-w-0">
          <button type="button" class="btn btn-ghost btn-xs mt-0.5 shrink-0" aria-label={$t('analysis.collapseCard')} on:click={toggleCard}>
            <span class="inline-block transition-transform duration-200 ease-out rotate-90 text-base-content/60 text-xs">▶</span>
          </button>
          <div class="min-w-0">
            <h3 class="card-title text-base">{$t(`${kind}.analysis.title`)}</h3>
            <span class="field-label">{$t(`${kind}.analysis.subtitle`)}</span>
          </div>
        </div>
        <button class="btn btn-ghost btn-xs shrink-0" on:click={clearReport} disabled={disabled}>{$t(`${kind}.analysis.clear`)}</button>
      </div>

      <div class="space-y-3">
        <div class="flex items-center gap-3 text-sm text-base-content/70 flex-wrap">
          {#if report.overall_score}
            <span class="badge badge-lg {report.overall_score >= 80 ? 'badge-success' : report.overall_score >= 60 ? 'badge-warning' : 'badge-error'}">
              {$t(`${kind}.analysis.score`)}: {report.overall_score}/100
            </span>
          {/if}
          {#if kind === 'act' && report.act_name}
            <span class="font-medium">{$t('act.analysis.actLabel', { no: report.act_no, name: report.act_name, start: report.start_ch, end: report.end_ch })}</span>
          {:else if kind === 'chapter' && report.chapter_num}
            <span class="font-medium">{$t('chapter.analysis.chapterLabel', { num: report.chapter_num })}</span>
          {/if}
          {#if report.based_on_structure}<span>{report.based_on_structure}</span>{/if}
          <span class="ml-auto text-xs">{report.generated_at?.slice(0, 10)}</span>
        </div>

        {#if report.summary}
          <details open class="bg-base-100 rounded-lg p-3">
            <summary class="cursor-pointer text-sm font-semibold">{$t(`${kind}.analysis.summary`)}</summary>
            <p class="whitespace-pre-wrap mt-2 text-sm text-base-content/80">{report.summary}</p>
          </details>
        {/if}

        {#if report.pillar_scores?.length}
          <section class="rounded-lg border border-base-content/10 bg-base-100/40 p-3 space-y-2">
            <div class="flex items-center gap-2">
              <span class="text-sm font-semibold">{$t('analysis.pillarsTitle')}</span>
              {#if report.overall_score}
                <span class="badge badge-sm {report.overall_score >= 80 ? "badge-success" : report.overall_score >= 60 ? "badge-warning" : "badge-error"}">{$t(`${kind}.analysis.score`)}: {report.overall_score}/100</span>
              {/if}
            </div>
            <div class="grid grid-cols-2 lg:grid-cols-4 gap-2">
              {#each report.pillar_scores as pl (pl.key)}
                {@const strong = pl.score >= 90 && !hasOpenSuggestionFor(pl.key)}
                <div class="bg-base-100 rounded-lg p-2.5 space-y-1">
                  <div class="flex items-center justify-between gap-1">
                    <span class="text-xs font-semibold truncate">{pillarLabel(pl.key)}</span>
                    <span class="badge badge-sm {pl.score >= 90 ? 'badge-success' : pl.score >= 75 ? 'badge-info' : pl.score >= 60 ? 'badge-warning' : 'badge-error'}">{pl.score}</span>
                  </div>
                  {#if strong}
                    <div class="text-xs text-success font-medium">✓ {$t(`${kind}.analysis.noChangesNeeded`)}</div>
                  {:else}
                    <div class="w-full h-1.5 rounded-full bg-base-content/10 overflow-hidden">
                      <div class="h-full rounded-full {pl.score >= 90 ? 'bg-success' : pl.score >= 75 ? 'bg-info' : pl.score >= 60 ? 'bg-warning' : 'bg-error'}" style="width: {pl.score}%"></div>
                    </div>
                  {/if}
                  {#if pl.note}<p class="text-[11px] leading-snug text-base-content/60 line-clamp-3">{pl.note}</p>{/if}
                  <!-- Transparent sub-criteria: the score IS the average of these
                       levels, so this list is literally the roadmap to 100%. -->
                  {#if pl.checklist?.length}
                    <details class="text-[11px]">
                      <summary class="cursor-pointer text-base-content/55 select-none">{$t('analysis.checklist.title')}</summary>
                      <ul class="mt-1 space-y-0.5" title={$t('analysis.checklist.tip')}>
                        {#each sortedChecklist(pl) as c (c.key)}
                          <li class="flex items-center justify-between gap-1">
                            <span class="truncate text-base-content/70">{criterionLabel(c.key)}</span>
                            <span class="shrink-0 font-mono {c.level >= 85 ? 'text-success' : c.level >= 60 ? 'text-warning' : 'text-error'}">{c.level < 0 ? '—' : c.level}</span>
                          </li>
                        {/each}
                      </ul>
                    </details>
                  {/if}
                </div>
              {/each}
            </div>
          </section>
        {/if}

        {#if report.suggestions?.length}
        <div class="flex items-center gap-2 pt-1">
          <span class="font-semibold text-sm">{$t(`${kind}.analysis.suggestions`)} ({report.suggestions.length})</span>
        </div>
        {/if}
        <div class="space-y-2">
          {#each report.suggestions || [] as s (s.id)}
            <section class="border border-base-content/10 rounded-lg p-3 space-y-2">
              <div class="flex items-center gap-2 flex-wrap">
                <span class="badge badge-outline badge-sm">{catLabel(s.category)}</span>
                {#if s.priority}
                  <span class="badge badge-sm {PRIORITY_BADGE[s.priority] || 'badge-ghost'}">{$t('analysis.priority.' + s.priority)}</span>
                {/if}
                <span class="font-semibold text-sm flex-1 min-w-0">{s.title}</span>
                {#if s.score_impact}
                  <span class="badge badge-success badge-sm shrink-0" title={$t('analysis.checklist.tip')}>{$t('analysis.scoreImpact', { n: s.score_impact })}</span>
                {/if}
                {#if s.status === 'applied'}
                  <span class="badge badge-success badge-sm">✓ {$t(`${kind}.analysis.applied`)}</span>
                {/if}
              </div>
              <div class="space-y-2">
              {#if s.criteria?.length}
                <p class="text-xs text-base-content/60">
                  {$t('analysis.criteriaLabel')}
                  {s.criteria.map((c) => criterionLabel(c.key)).join(', ')}
                  {#if s.pillar} · {pillarLabel(s.pillar)}{/if}
                </p>
              {/if}
              {#if s.detail}<p class="text-sm text-base-content/80 whitespace-pre-wrap">{s.detail}</p>{/if}
              {#if s.action}<p class="text-sm text-base-content/70 italic whitespace-pre-wrap">{s.action}</p>{/if}
              {#if s.applies_to_chapters?.length}
                <p class="text-xs text-base-content/60">{$t(`${kind}.analysis.chapters`, { list: s.applies_to_chapters.join(', ') })}</p>
              {/if}
              {#if kind === 'sceneplan' && mergedScenesOf(s).length}
                <p class="text-xs text-error/80 font-medium">⚠ {$t('analysis.mergeSurvivor')}: {mergedScenesOf(s).join(', ')}</p>
              {/if}

              <!-- Scene-level replacements (sceneplan pass): editable accordions -->
              {#if s.recommendations?.length}
                <div class="space-y-2">
                  {#each s.recommendations as rec (rec.chapter_num + ':' + rec.scene_id)}
                    {@const ms = mergeState(s, rec)}
                    {@const siblings = siblingSceneIds(s, rec)}
                    <details class="bg-base-100 rounded p-2.5 text-xs" open={ms.on}>
                      <summary class="cursor-pointer font-semibold select-none flex items-center gap-2 flex-wrap">
                        <span>Ch. {rec.chapter_num} · {$t('sceneplan.analysis.sceneLabel', { id: rec.scene_id })}</span>
                        {#if ms.on && ms.target}
                          <span class="badge badge-error badge-xs normal-case">{$t('analysis.mergeChip', { id: ms.target })}</span>
                        {:else if rec.merge_scene_ids?.includes(rec.scene_id)}
                          <span class="badge badge-warning badge-xs normal-case">{$t('analysis.mergeSurvivor')}</span>
                        {/if}
                      </summary>
                      <div class="space-y-1.5 pt-1.5">
                        {#if rec.new_title}
                          <div><span class="text-base-content/60">{$t('sceneplan.analysis.replacePreview')} Title:</span></div>
                          <textarea class="textarea textarea-sm w-full text-xs leading-relaxed overflow-hidden" style="min-height: 2rem"
                            value={recValue(s, rec, 'new_title')} use:autosizeTextarea
                            on:input={(e) => onRecInput(s, rec, 'new_title', e)}
                            disabled={disabled || s.status === 'applied'}
                            placeholder={$t('sceneplan.analysis.editableHint')}></textarea>
                        {/if}
                        {#if rec.new_description}
                          <div><span class="text-base-content/60">{$t('sceneplan.analysis.replacePreview')} {$t('sceneplan.analysis.description')}:</span></div>
                          <textarea class="textarea textarea-sm w-full text-xs leading-relaxed overflow-hidden" style="min-height: 3rem"
                            value={recValue(s, rec, 'new_description')} use:autosizeTextarea
                            on:input={(e) => onRecInput(s, rec, 'new_description', e)}
                            disabled={disabled || s.status === 'applied'}
                            placeholder={$t('sceneplan.analysis.editableHint')}></textarea>
                        {/if}
                        <div class="flex gap-3 flex-wrap">
                          {#if rec.new_pov}
                            <label class="flex items-center gap-1.5">
                              <span class="text-base-content/60">POV:</span>
                              <input class="input input-xs input-bordered w-32" type="text"
                                value={recValue(s, rec, 'new_pov')}
                                on:input={(e) => onRecInput(s, rec, 'new_pov', e)}
                                disabled={disabled || s.status === 'applied'} />
                            </label>
                          {/if}
                          {#if rec.new_words_budget}
                            <label class="flex items-center gap-1.5">
                              <span class="text-base-content/60">{$t('sceneplan.analysis.budget')}:</span>
                              <input class="input input-xs input-bordered w-20" type="number" min="0"
                                value={recValue(s, rec, 'new_words_budget')}
                                on:input={(e) => onRecInput(s, rec, 'new_words_budget', e)}
                                disabled={disabled || s.status === 'applied'} />
                            </label>
                          {/if}
                        </div>
                        {#if kind === 'sceneplan' && siblings.length}
                          <div class="pt-1 border-t border-base-300 flex flex-wrap items-center gap-2">
                            <label class="flex items-center gap-1.5 cursor-pointer">
                              <input type="checkbox" class="checkbox checkbox-xs"
                                checked={ms.on}
                                disabled={disabled || s.status === 'applied'}
                                on:change={(e) => setMerge(s, rec, e.currentTarget.checked, ms.target)} />
                              <span class="text-base-content/70">{$t('analysis.mergeToggleOn')}</span>
                            </label>
                            {#if ms.on}
                              <label class="flex items-center gap-1.5">
                                <span class="text-base-content/60">{$t('analysis.mergeTarget')}:</span>
                                <select class="select select-xs select-bordered w-auto"
                                  value={ms.target}
                                  disabled={disabled || s.status === 'applied'}
                                  on:change={(e) => setMerge(s, rec, true, parseInt(e.currentTarget.value, 10))}>
                                  {#each siblings as sid (sid)}
                                    <option value={sid}>{$t('sceneplan.analysis.sceneLabel', { id: sid })}</option>
                                  {/each}
                                </select>
                              </label>
                              <span class="text-[10px] text-error/80">{$t('analysis.mergeWarning', { absorbed: rec.scene_id, survivor: ms.target })}</span>
                            {/if}
                          </div>
                        {/if}
                        <p class="text-[10px] text-base-content/45">{$t('sceneplan.analysis.editableHint')}</p>
                      </div>
                    </details>
                  {/each}
                </div>
              {/if}

              <!-- Trope-unit interventions (act & chapter passes): surgical,
                   small fixes targeting one trope unit of one scene. -->
              {#if s.interventions?.length}
                <div class="space-y-1.5">
                  {#each s.interventions as iv, i (i)}
                    <div class="bg-base-100 rounded p-2 text-xs flex items-start gap-2">
                      <span class="badge badge-outline badge-xs normal-case shrink-0 mt-0.5">
                        {$t(`${kind}.analysis.tropeChip`, { ch: iv.chapter_num, scene: iv.scene_pos + 1, trope: iv.trope_label || '—' })}
                      </span>
                      <span class="flex-1 min-w-0 whitespace-pre-wrap text-base-content/80">{iv.feedback}</span>
                      {#if iv.status === 'applied'}<span class="badge badge-success badge-xs shrink-0">✓</span>
                      {:else if iv.status === 'failed'}<span class="badge badge-error badge-xs shrink-0">✗</span>{/if}
                    </div>
                  {/each}
                </div>
              {/if}

              {#if s.status !== 'applied'}
                <div class="flex flex-wrap justify-end gap-2">
                  {#if s.recommendations?.length || s.interventions?.length}
                    <button class="btn btn-outline btn-xs" on:click={() => proposeForSuggestion(s)} disabled={disabled || proposingSuggestionId === s.id}>
                      {#if proposingSuggestionId === s.id}<span class="loading loading-spinner loading-xs"></span>{/if}
                      {$t(`${kind}.analysis.addProposals`)}
                    </button>
                    <button class="btn btn-success btn-xs" on:click={() => applySuggestion(s)} disabled={disabled || applyingSuggestionId === s.id}>
                      {#if applyingSuggestionId === s.id}<span class="loading loading-spinner loading-xs"></span>{/if}
                      {$t(`${kind}.analysis.apply`)}
                    </button>
                  {:else}
                    <p class="text-xs text-base-content/50 flex-1">
                      {$t(suggestionHasProposals(s) ? `${kind}.analysis.noApply` : `${kind}.analysis.adviceOnly`)}
                    </p>
                    <button class="btn btn-outline btn-xs" on:click={() => proposeForSuggestion(s)} disabled={disabled || proposingSuggestionId === s.id}>
                      {#if proposingSuggestionId === s.id}<span class="loading loading-spinner loading-xs"></span>{/if}
                      {$t(`${kind}.analysis.propose`)}
                    </button>
                  {/if}
                </div>
              {/if}
              </div>
            </section>
          {/each}
        </div>
      </div>
    </div>
    {/if}
      </div>
    </div>
  </div>
{/if}

{#if hasTargets}
  <div class="flex justify-end pt-1">
    <button class="btn btn-primary btn-sm" on:click={analyze} disabled={disabled}>
      {#if disabled && report == null}<span class="loading loading-spinner loading-xs"></span>{/if}
      {$t(report ? `${kind}.analysis.reanalyze` : `${kind}.analysis.btn`)}
    </button>
  </div>
{/if}
