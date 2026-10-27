<script>
  // ScenePlanEditor: editable chapter beat sheet (scene list with per-scene
  // word budgets + "variation" delta), rendered INLINE inside the "Scene
  // matrix" step tab of the Writing page (it used to live in a modal opened
  // from a "Scene plan" button). The author can add / delete / reorder scenes
  // and change budgets; any edit un-approves the plan server-side until it is
  // approved again. The auto switch skips review.
  //
  // Aesthetics: Notion / Revolut-style inline document — borderless rows that
  // reveal a soft hover surface, big clear titles, muted helper texts, pill
  // badges and a dark neutral palette (mirrors the WritingStepTabs stepper),
  // instead of the old DaisyUI table-in-a-card.
  import { api } from '../lib/api.js';
  import { progress, taskRunning, addToast } from '../lib/stores.js';
  import { t } from '../lib/i18n/index.js';

  export let chapterNum = null;   // chapter whose plan is edited
  export let onGenerated = () => {}; // called after approve+generate succeeds

  let plan = null;          // { target_words, scenes: [...], approved }
  let auto = false;         // auto-approve switch state
  let loading = false;
  let saving = false;
  let feedback = '';        // regenerate guidance (optional)

  $: totalBudget = (plan?.scenes || []).reduce((s, sc) => s + (Number(sc.words_budget) || 0), 0);
  $: budgetDelta = totalBudget - (plan?.target_words || 0);

  // —— Variation presets (Notion-style quick-select row under each scene) ———
  // Clicking a preset REPLACES the variation text (the field stays fully
  // editable for custom deltas like "Tiempo límite -3hrs").
  const VARIATION_PRESETS = [
    { label: '⚡ Hook', value: 'Hook: opening question that grabs the reader' },
    { label: '🎯 New objective', value: 'Finds a new objective' },
    { label: '💥 Reversal', value: 'Reversal: situation flips against the POV' },
    { label: '🔥 Cliffhanger', value: 'Cliffhanger: unresolved danger left hanging' },
  ];

  async function load() {
    if (!chapterNum) return;
    loading = true;
    try {
      const res = await api('GET', `/api/chapters/${chapterNum}/scene-plan`);
      plan = res.plan;
      auto = !!res.auto_scene_plan;
    } catch (e) {
      addToast(e.message, 'error');
      plan = null;
    } finally {
      loading = false;
    }
  }

  // Reload whenever the editor mounts or the chapter changes. As an inline
  // panel it is re-keyed (`key={ch?.num}`) by the Writing page, so each
  // chapter switch remounts and loads fresh; the reactive statement also
  // covers in-place num changes.
  $: if (chapterNum) load();

  function addScene() {
    if (!plan) plan = { scenes: [], target_words: 0, approved: false };
    if (!plan.scenes) plan.scenes = [];
    const n = plan.scenes.length;
    const rest = Math.max(250, (plan.target_words || 0) - n * 0);
    plan.scenes.push({ id: n + 1, title: '', description: '', pov: '', words_budget: Math.min(600, Math.round(rest / Math.max(1, 4 - n))) || 500, variation: '' });
    plan.scenes = plan.scenes; // trigger reactivity
    plan.approved = false;
  }

  function removeScene(i) {
    plan.scenes.splice(i, 1);
    plan.scenes = [...plan.scenes];
    plan.approved = false;
  }

  function moveScene(i, dir) {
    const j = i + dir;
    if (j < 0 || j >= plan.scenes.length) return;
    const arr = plan.scenes;
    [arr[i], arr[j]] = [arr[j], arr[i]];
    plan.scenes = [...arr];
    plan.approved = false;
  }

  function markDirty() {
    if (plan) plan.approved = false;
  }

  function setVariation(s, v) {
    s.variation = v;
    markDirty();
  }

  // Split a variation delta into its sign token ("+2", "-3hrs") and the rest,
  // so the UI can render the delta as a green/red chip (Revolut-style).
  function splitVariation(v) {
    const raw = (v || '').trim();
    const m = raw.match(/^([+-−]\s?[\w]+(?:\s?(?:hrs?|h|pts?|%))?)\b\s*(.*)$/i);
    if (!m) return { delta: '', rest: raw, sign: 0 };
    const delta = m[1].replace(/\s+/g, '');
    const sign = /^[+]/.test(delta) ? 1 : -1;
    return { delta, rest: m[2].trim(), sign };
  }

  async function persist() {
    const res = await api('PUT', `/api/chapters/${chapterNum}/scene-plan`, {
      scenes: (plan?.scenes || []).map(s => ({
        title: s.title || '',
        description: s.description || '',
        pov: s.pov || '',
        words_budget: Number(s.words_budget) || 0,
        variation: s.variation || '',
      })),
      keep_budgets: false, // author edits are authoritative
    });
    plan = res;
  }

  async function saveOnly() {
    saving = true;
    try {
      await persist();
      addToast($t('writing.scenePlan.saved'), 'success');
    } catch (e) { addToast(e.message, 'error'); }
    finally { saving = false; }
  }

  async function saveAndApprove() {
    saving = true;
    try {
      await persist();
      plan = await api('POST', `/api/chapters/${chapterNum}/scene-plan/approve`);
      addToast($t('writing.scenePlan.approved'), 'success');
      progress.set(await api('GET', '/api/progress'));
    } catch (e) { addToast(e.message, 'error'); }
    finally { saving = false; }
  }

  async function regenerate() {
    saving = true;
    try {
      await api('POST', `/api/chapters/${chapterNum}/scene-plan/regenerate`, { feedback: feedback.trim() });
      addToast($t('writing.scenePlan.regenStarted'), 'info');
      // The task broadcasts progress when done; poll for the new plan.
      let tries = 0;
      const poll = setInterval(async () => {
        tries++;
        if (tries > 300) { clearInterval(poll); return; }
        if ($taskRunning) return;
        clearInterval(poll);
        await load();
      }, 1000);
    } catch (e) { addToast(e.message, 'error'); }
    finally { saving = false; }
  }

  async function toggleAuto(e) {
    const next = e.target.checked;
    try {
      const res = await api('PUT', '/api/scene-plan/auto', { auto: next });
      auto = !!res.auto_scene_plan;
      addToast(auto ? $t('writing.scenePlan.autoOn') : $t('writing.scenePlan.autoOff'), 'info');
      progress.set(await api('GET', '/api/progress'));
    } catch (err) {
      e.target.checked = auto;
      addToast(err.message, 'error');
    }
  }

  async function approveAndGenerate() {
    await saveAndApprove();
    if (plan?.approved) {
      try {
        await api('POST', '/api/chapter/generate');
        addToast($t('writing.toasts.generateStarted', { num: chapterNum }), 'info');
        onGenerated();
      } catch (e) { addToast(e.message, 'error'); }
    }
  }
</script>

<div id="CardSection_ScenePlanInline" class="rounded-xl bg-white shadow-subtle p-8">
  <!-- HEADER: big clear title + status pill + automation switch -->
  <div class="flex items-center gap-3 flex-wrap px-5 pt-5 pb-3 border-b border-neutral-100">
    <h3 class="text-lg font-semibold text-neutral-900 flex-1 leading-tight">
      {$t('writing.scenePlan.title', { num: chapterNum })}
    </h3>
    {#if plan && (plan.scenes || []).length}
      {#if plan.approved}
        <span class="inline-flex items-center gap-1 rounded-full bg-emerald-50 border border-emerald-200 px-2.5 py-0.5 text-[11px] font-medium text-emerald-700">✓ {$t('writing.scenePlan.approvedBadge')}</span>
      {:else}
        <span class="inline-flex items-center gap-1 rounded-full bg-amber-50 border border-amber-200 px-2.5 py-0.5 text-[11px] font-medium text-amber-700">• {$t('writing.scenePlan.needsApprovalBadge')}</span>
      {/if}
    {/if}
    <label class="flex items-center gap-2 cursor-pointer select-none" title={$t('writing.scenePlan.autoTip')}>
      <input type="checkbox" class="toggle toggle-xs toggle-neutral" checked={auto} on:change={toggleAuto} />
      <span class="text-xs text-neutral-500">{$t('writing.scenePlan.auto')}</span>
    </label>
  </div>

  {#if loading}
    <div class="px-5 py-8 flex justify-center"><span class="loading loading-dots loading-md text-neutral-400"></span></div>
  {:else if !plan || !(plan.scenes || []).length}
    <!-- EMPTY STATE: generate the first beat sheet -->
    <div class="px-5 py-6">
      <p class="text-sm text-neutral-600 mb-4">{$t('writing.scenePlan.empty')}</p>
      <div class="flex gap-2 items-end">
        <input class="input input-sm w-full rounded-lg border-neutral-200 bg-neutral-50 focus:bg-white" bind:value={feedback} placeholder={$t('writing.scenePlan.regenPlaceholder')} disabled={$taskRunning || saving} />
        <button class="btn btn-neutral btn-sm rounded-lg" on:click={regenerate} disabled={$taskRunning || saving}>{$t('writing.scenePlan.generate')}</button>
      </div>
    </div>
  {:else}
    <!-- BUDGET STRIP: target vs sum, delta pill -->
    <div class="flex flex-wrap items-center gap-x-4 gap-y-1 px-5 pt-3 text-xs text-neutral-500">
      <span>{$t('writing.scenePlan.target', { n: plan.target_words })}</span>
      <span>{$t('writing.scenePlan.budgetTotal', { n: totalBudget })}</span>
      {#if budgetDelta !== 0}
        <span class="rounded-full px-2 py-0.5 font-mono text-[11px] {budgetDelta > 0 ? 'bg-amber-50 text-amber-700' : 'bg-sky-50 text-sky-700'}">{budgetDelta > 0 ? '+' : ''}{budgetDelta}</span>
      {/if}
    </div>

    <!-- SCENE ROWS: Notion-style borderless rows revealed on hover -->
    <div class="px-2 py-2 space-y-5">
      {#each plan.scenes as s, i (i)}
        {@const role = plan.scenes.length === 1 ? 'both' : (i === 0 ? 'hook' : (i === plan.scenes.length - 1 ? 'cliff' : 'mid'))}
        <div class="spe-row group rounded-lg px-3 py-2.5 transition-colors">
          <!-- line 1: number badge · title · POV · budget · row actions -->
          <div class="flex items-center gap-2">
            <span class="shrink-0 w-6 h-6 rounded-md bg-neutral-900 text-white text-[11px] font-bold flex items-center justify-center">{i + 1}</span>
            <input
              class="spe-title flex-1 min-w-0 bg-transparent border-0 rounded-md px-1.5 py-0.5 text-[15px] font-semibold text-neutral-900 placeholder:text-neutral-300 focus:outline-none focus:bg-neutral-50"
              bind:value={s.title} on:input={markDirty} disabled={$taskRunning}
              placeholder={$t('writing.scenePlan.colTitle')} />
            {#if role === 'hook' || role === 'both'}
              <span class="hidden sm:inline-flex shrink-0 items-center rounded-full bg-violet-50 border border-violet-200 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-violet-700">Hook</span>
            {:else if role === 'cliff'}
              <span class="hidden sm:inline-flex shrink-0 items-center rounded-full bg-rose-50 border border-rose-200 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-rose-700">Cliffhanger</span>
            {/if}
            <input
              class="hidden sm:block spe-field w-28 shrink-0 rounded-md border border-neutral-200 bg-neutral-50 px-2 py-1 text-xs text-neutral-700 focus:outline-none focus:border-neutral-400 focus:bg-white"
              bind:value={s.pov} on:input={markDirty} disabled={$taskRunning}
              placeholder={$t('writing.scenePlan.colPov')} />
            <div class="flex items-center gap-1 shrink-0">
              <input
                class="spe-field w-16 rounded-md border border-neutral-200 bg-neutral-50 px-2 py-1 text-xs text-right font-mono text-neutral-700 focus:outline-none focus:border-neutral-400 focus:bg-white"
                type="number" step="50" min="0" bind:value={s.words_budget} on:input={markDirty} disabled={$taskRunning} />
              <span class="text-[10px] text-neutral-400">w</span>
            </div>
            <div class="flex gap-0.5 shrink-0 opacity-0 group-hover:opacity-100 transition-opacity">
              <button class="btn btn-ghost btn-xs rounded-md text-neutral-500" on:click={() => moveScene(i, -1)} disabled={i === 0 || $taskRunning} title={$t('writing.scenePlan.moveUp')}>↑</button>
              <button class="btn btn-ghost btn-xs rounded-md text-neutral-500" on:click={() => moveScene(i, 1)} disabled={i === plan.scenes.length - 1 || $taskRunning} title={$t('writing.scenePlan.moveDown')}>↓</button>
              <button class="btn btn-ghost btn-xs rounded-md text-rose-500" on:click={() => removeScene(i)} disabled={$taskRunning} title={$t('writing.scenePlan.delete')}>✕</button>
            </div>
          </div>

          <!-- line 2: description (what happens) — always visible, grows.
               Uses the daisyUI .textarea class so it inherits the global
               `field-sizing: content` auto-grow rule (same as novel
               parameters). Width is computed with calc() so ml-8 + px-2
               never overflow the padded parent container. -->
          <textarea
            class="textarea spe-desc w-[calc(100%-2rem)] mt-1.5 ml-8 rounded-md border-transparent bg-transparent px-2 py-1 text-[13px] leading-relaxed text-neutral-600 placeholder:text-neutral-300 focus:border-neutral-200 focus:bg-neutral-50 min-h-0"
            bind:value={s.description} on:input={markDirty} disabled={$taskRunning}
            placeholder={$t('writing.scenePlan.colDesc')}></textarea>

          <!-- line 3: VARIATION — what concretely changes (delta chip + free text + presets) -->
          <div class="mt-1 ml-8 mr-2 flex flex-wrap items-center gap-1.5">
            <span class="text-[10px] font-semibold uppercase tracking-wide text-neutral-400 shrink-0">{$t('writing.scenePlan.colVariation')}</span>
            {#if splitVariation(s.variation).delta}
              <span class="rounded-full px-2 py-0.5 font-mono text-[11px] font-semibold {splitVariation(s.variation).sign >= 0 ? 'bg-emerald-50 text-emerald-700 border border-emerald-200' : 'bg-rose-50 text-rose-700 border border-rose-200'}">{splitVariation(s.variation).delta}</span>
            {/if}
            <input
              class="spe-field flex-1 min-w-32 rounded-md border border-neutral-200 bg-neutral-50 px-2 py-1 text-xs text-neutral-700 placeholder:text-neutral-300 focus:outline-none focus:border-neutral-400 focus:bg-white"
              bind:value={s.variation} on:input={markDirty} disabled={$taskRunning}
              placeholder={'Carisma +2 · Tiempo límite -3hrs · encuentra nuevo objetivo…'} />
            {#each VARIATION_PRESETS as p}
              <button type="button" class="rounded-full border border-neutral-200 px-2 py-0.5 text-[10px] text-neutral-500 hover:bg-neutral-100 hover:text-neutral-800 transition-colors disabled:opacity-40"
                      on:click={() => setVariation(s, p.value)} disabled={$taskRunning}>{p.label}</button>
            {/each}
          </div>
        </div>
      {/each}

      <button class="btn btn-ghost btn-sm w-full rounded-lg border border-dashed border-neutral-300 text-neutral-500 hover:border-neutral-500 hover:text-neutral-800 normal-case"
              on:click={addScene} disabled={$taskRunning}>＋ {$t('writing.scenePlan.addScene')}</button>
    </div>

    <!-- FOOTER: regeneration guidance + save / approve actions -->
    <div class="px-5 py-4 border-t border-neutral-100 flex gap-2 flex-wrap items-center">
      <input class="input input-sm rounded-lg border-neutral-200 bg-neutral-50 focus:bg-white flex-1 min-w-40" bind:value={feedback} placeholder={$t('writing.scenePlan.regenPlaceholder')} disabled={$taskRunning || saving} />
      <button class="btn btn-outline btn-sm rounded-lg normal-case" on:click={regenerate} disabled={$taskRunning || saving} title={$t('writing.scenePlan.regenTip')}>{$t('writing.scenePlan.regen')}</button>
      <div class="divider divider-horizontal mx-0"></div>
      <button class="btn btn-outline btn-sm rounded-lg normal-case" on:click={saveOnly} disabled={$taskRunning || saving}>{$t('writing.scenePlan.save')}</button>
      <button class="btn btn-outline btn-sm rounded-lg normal-case" on:click={saveAndApprove} disabled={$taskRunning || saving}>{$t('writing.scenePlan.saveApprove')}</button>
      {#if !plan.approved}
        <button class="btn btn-neutral btn-sm rounded-lg normal-case" on:click={approveAndGenerate} disabled={$taskRunning || saving}>{$t('writing.scenePlan.approveGenerate')}</button>
      {/if}
    </div>
  {/if}
</div>

<style>
  .spe-row { border: 1px solid transparent; }
  .spe-row:hover { background: #fafafa; border-color: #f0f0f0; }
  /* Extra breathing room BETWEEN scenes: the row container already uses
     space-y-5 (1.25rem); add margin on top of that so consecutive scene rows
     feel clearly separated (Notion-style block spacing). */
  .spe-row + .spe-row { margin-top: 0.75rem; }
  .spe-title:focus { box-shadow: inset 0 -1px 0 #e5e5e5; }
  /* The scene description textarea must NEVER overflow the card padding:
     hard cap on top of the calc() width, and it only grows vertically. */
  .spe-desc { max-width: calc(100% - 2rem); box-sizing: border-box; resize: vertical; }
</style>
