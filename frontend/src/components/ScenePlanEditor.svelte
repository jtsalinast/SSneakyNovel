<script>
  // ScenePlanEditor: editable chapter beat sheet (scene list with per-scene
  // word budgets). Opened from the Writing page when a scene plan is pending
  // review, or manually via the "Scene plan" button. The author can add /
  // delete / reorder scenes and change budgets; any edit un-approves the plan
  // server-side until it is approved again. The auto switch skips review.
  import { api } from '../lib/api.js';
  import { progress, taskRunning, addToast } from '../lib/stores.js';
  import { t } from '../lib/i18n/index.js';

  export let chapterNum = null;   // chapter whose plan is edited
  export let open = false;
  export let onGenerated = () => {}; // called after approve+generate succeeds

  let plan = null;          // { target_words, scenes: [...], approved }
  let auto = false;         // auto-approve switch state
  let loading = false;
  let saving = false;
  let feedback = '';        // regenerate guidance (optional)

  $: totalBudget = (plan?.scenes || []).reduce((s, sc) => s + (Number(sc.words_budget) || 0), 0);
  $: budgetDelta = totalBudget - (plan?.target_words || 0);

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

  // Reload whenever the modal opens or the chapter changes.
  $: if (open && chapterNum) load();

  function addScene() {
    if (!plan) plan = { scenes: [], target_words: 0, approved: false };
    if (!plan.scenes) plan.scenes = [];
    const n = plan.scenes.length;
    const rest = Math.max(250, (plan.target_words || 0) - n * 0);
    plan.scenes.push({ id: n + 1, title: '', description: '', pov: '', words_budget: Math.min(600, Math.round(rest / Math.max(1, 4 - n))) || 500 });
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

  async function persist() {
    const res = await api('PUT', `/api/chapters/${chapterNum}/scene-plan`, {
      scenes: (plan?.scenes || []).map(s => ({
        title: s.title || '',
        description: s.description || '',
        pov: s.pov || '',
        words_budget: Number(s.words_budget) || 0,
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

  async function approveOnly() {
    saving = true;
    try {
      plan = await api('POST', `/api/chapters/${chapterNum}/scene-plan/approve`);
      addToast($t('writing.scenePlan.approved'), 'success');
      progress.set(await api('GET', '/api/progress'));
    } catch (e) { addToast(e.message, 'error'); }
    finally { saving = false; }
  }

  async function regenerate() {
    saving = true;
    try {
      if (plan) await persist().catch(() => {}); // keep manual edits out of the way? no — regen discards them
    } catch (e) {}
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
        open = false;
        onGenerated();
      } catch (e) { addToast(e.message, 'error'); }
    }
  }
</script>

{#if open}
  <div class="fixed inset-0 z-50 bg-black/50 flex items-center justify-center p-4" role="dialog" aria-modal="true">
    <div class="card bg-base-100 w-full max-w-3xl max-h-[90vh] overflow-y-auto shadow-xl">
      <div class="card-body p-5 gap-3">
        <div class="flex items-center gap-2">
          <h3 class="card-title text-base flex-1">{$t('writing.scenePlan.title', { num: chapterNum })}</h3>
          <label class="flex items-center gap-1.5 cursor-pointer" title={$t('writing.scenePlan.autoTip')}>
            <input type="checkbox" class="toggle toggle-xs toggle-success" checked={auto} on:change={toggleAuto} />
            <span class="text-xs text-base-content/60">{$t('writing.scenePlan.auto')}</span>
          </label>
          <button class="btn btn-ghost btn-sm" on:click={() => open = false}>✕</button>
        </div>

        {#if loading}
          <span class="loading loading-dots loading-md"></span>
        {:else if !plan || !(plan.scenes || []).length}
          <p class="text-sm text-base-content/65">{$t('writing.scenePlan.empty')}</p>
          <div class="flex gap-2 items-end">
            <div class="flex-1">
              <input class="input input-sm w-full" bind:value={feedback} placeholder={$t('writing.scenePlan.regenPlaceholder')} disabled={$taskRunning || saving} />
            </div>
            <button class="btn btn-primary btn-sm" on:click={regenerate} disabled={$taskRunning || saving}>{$t('writing.scenePlan.generate')}</button>
          </div>
        {:else}
          <div class="text-xs text-base-content/65 flex flex-wrap gap-x-4 gap-y-1">
            <span>{$t('writing.scenePlan.target', { n: plan.target_words })}</span>
            <span>{$t('writing.scenePlan.budgetTotal', { n: totalBudget })}</span>
            {#if budgetDelta !== 0}
              <span class={budgetDelta > 0 ? 'text-warning' : 'text-info'}>{budgetDelta > 0 ? '+' : ''}{budgetDelta}</span>
            {/if}
            {#if plan.approved}
              <span class="badge badge-success badge-sm">{$t('writing.scenePlan.approvedBadge')}</span>
            {:else}
              <span class="badge badge-warning badge-sm">{$t('writing.scenePlan.needsApprovalBadge')}</span>
            {/if}
          </div>

          <div class="overflow-x-auto border border-base-content/10 rounded-lg">
            <table class="table table-sm">
              <thead>
                <tr class="text-xs text-base-content/60">
                  <th>#</th>
                  <th>{$t('writing.scenePlan.colTitle')}</th>
                  <th>{$t('writing.scenePlan.colDesc')}</th>
                  <th>{$t('writing.scenePlan.colPov')}</th>
                  <th>{$t('writing.scenePlan.colWords')}</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {#each plan.scenes as s, i (i)}
                  <tr>
                    <td class="align-top text-xs">{i + 1}</td>
                    <td><input class="input input-xs w-full" bind:value={s.title} on:input={markDirty} disabled={$taskRunning} /></td>
                    <td><textarea class="textarea textarea-xs w-full min-h-[2rem]" rows="1" bind:value={s.description} on:input={markDirty} disabled={$taskRunning}></textarea></td>
                    <td><input class="input input-xs w-20" bind:value={s.pov} on:input={markDirty} disabled={$taskRunning} /></td>
                    <td><input class="input input-xs w-20 text-right" type="number" step="50" min="0" bind:value={s.words_budget} on:input={markDirty} disabled={$taskRunning} /></td>
                    <td class="align-top">
                      <div class="join join-vertical-xs">
                        <div class="flex gap-0.5">
                          <button class="btn btn-ghost btn-xs" on:click={() => moveScene(i, -1)} disabled={i === 0 || $taskRunning} title={$t('writing.scenePlan.moveUp')}>↑</button>
                          <button class="btn btn-ghost btn-xs" on:click={() => moveScene(i, 1)} disabled={i === plan.scenes.length - 1 || $taskRunning} title={$t('writing.scenePlan.moveDown')}>↓</button>
                          <button class="btn btn-ghost btn-xs text-error" on:click={() => removeScene(i)} disabled={$taskRunning} title={$t('writing.scenePlan.delete')}>✕</button>
                        </div>
                      </div>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>

          <div class="flex gap-2 flex-wrap items-center">
            <button class="btn btn-outline btn-sm" on:click={addScene} disabled={$taskRunning}>＋ {$t('writing.scenePlan.addScene')}</button>
            <input class="input input-sm flex-1 min-w-40" bind:value={feedback} placeholder={$t('writing.scenePlan.regenPlaceholder')} disabled={$taskRunning || saving} />
            <button class="btn btn-outline btn-sm" on:click={regenerate} disabled={$taskRunning || saving} title={$t('writing.scenePlan.regenTip')}>{$t('writing.scenePlan.regen')}</button>
          </div>

          <div class="modal-action flex-wrap gap-2 mt-1">
            <button class="btn btn-ghost btn-sm" on:click={() => open = false}>{$t('common.close')}</button>
            <button class="btn btn-outline btn-sm" on:click={saveOnly} disabled={$taskRunning || saving}>{$t('writing.scenePlan.save')}</button>
            <button class="btn btn-outline btn-sm" on:click={saveAndApprove} disabled={$taskRunning || saving}>{$t('writing.scenePlan.saveApprove')}</button>
            {#if plan.approved}
              <button class="btn btn-primary btn-sm" on:click={() => { open = false; }} disabled={$taskRunning}>{$t('writing.scenePlan.doneClose')}</button>
            {:else}
              <button class="btn btn-primary btn-sm" on:click={approveAndGenerate} disabled={$taskRunning || saving}>{$t('writing.scenePlan.approveGenerate')}</button>
            {/if}
          </div>
        {/if}
      </div>
    </div>
  </div>
{/if}
