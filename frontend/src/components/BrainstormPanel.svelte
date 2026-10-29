<script>
  import { api } from '../lib/api.js';
  import { renderMarkdown } from '../lib/markdown.js';
  import { apiConfig } from '../lib/stores.js';
  import { t } from '../lib/i18n/index.js';
  import { navigate } from '../lib/router.js';
  import ICONS from '../lib/icons.js';

  export let onClose = () => {};

  const ACTIONS = ['alternatives', 'rewrite', 'describe', 'expand', 'feedback'];
  const MODES = {
    rewrite: ['shorter', 'longer', 'rephrase', 'descriptive', 'show', 'inner_conflict', 'intense'],
    // Primary senses first, then body/environment senses, so the grid reads
    // from the most obvious to the most subtle.
    describe: ['sight', 'sound', 'smell', 'taste', 'tactile', 'temperature', 'pain', 'spatial', 'proprioception', 'sixth_sense', 'ambient'],
    feedback: ['originality', 'clarity', 'pacing', 'grammar', 'feasibility'],
  };
  const SENSE_ICONS = {
    sight: 'eye',
    sound: 'earListen',
    smell: 'wind',
    taste: 'tongue',
    tactile: 'hand',
    temperature: 'temperatureHalf',
    pain: 'bolt',
    spatial: 'compass',
    proprioception: 'personWalking',
    sixth_sense: 'brain',
    ambient: 'cloud',
  };

  let action = 'alternatives';
  let mode = '';
  let text = '';
  let options = [];
  let tokenUsage = null;
  let busy = false;
  let error = '';
  let copiedIndex = -1;
  let copyTimer;

  $: modes = MODES[action] || [];
  $: configured = !!($apiConfig?.brainstorm?.model || '').trim();

  function pickAction(next) {
    action = next;
    mode = MODES[next]?.[0] || '';
    error = '';
  }

  async function run(more = false) {
    if (busy || !text.trim()) return;
    busy = true;
    error = '';
    copiedIndex = -1;
    if (!more) {
      options = [];
      tokenUsage = null;
    }
    try {
      const timeoutMs = (($apiConfig?.brainstorm?.http_timeout_seconds || 120) + 30) * 1000;
      const res = await api('POST', '/api/brainstorm', { action, mode, text, more }, {}, timeoutMs);
      const next = Array.isArray(res?.options) && res.options.length
        ? res.options
        : (res?.result ? [res.result] : []);
      options = more ? [...options, ...next] : next;
      const prompt = Number(res?.prompt_tokens) || 0;
      const completion = Number(res?.completion_tokens) || 0;
      tokenUsage = {
        prompt: (tokenUsage?.prompt || 0) + prompt,
        completion: (tokenUsage?.completion || 0) + completion,
      };
      if (!options.length) error = $t('brainstorm.empty');
    } catch (e) {
      const msg = e?.message || String(e);
      error = msg === 'brainstorm_not_configured' ? $t('brainstorm.notConfigured') : msg;
    } finally {
      busy = false;
    }
  }

  function back() {
    options = [];
    tokenUsage = null;
    error = '';
    copiedIndex = -1;
  }

  async function copyOption(index, value) {
    try {
      await navigator.clipboard.writeText(value);
      copiedIndex = index;
      clearTimeout(copyTimer);
      copyTimer = setTimeout(() => { copiedIndex = -1; }, 1500);
    } catch (_) {}
  }
</script>

<div id="BrainstormPanelRoot" class="flex flex-col h-full bg-white">
  <div class="h-14 shrink-0 border-b border-[#EAEAEA] bg-white px-3 flex items-center gap-2">
    <svg class="w-4 h-4 text-gray-500" viewBox={ICONS.brainstorm.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.brainstorm.d} /></svg>
    <h2 class="text-sm font-semibold text-gray-900">{$t('brainstorm.title')}</h2>
    <span class="text-[10px] font-mono text-gray-400 truncate flex-1">
      {configured ? $apiConfig.brainstorm.model : $t('brainstorm.notConfiguredShort')}
    </span>
    <button class="p-1.5 rounded-md text-gray-500 hover:bg-gray-100 hover:text-black transition-colors" on:click={onClose} title={$t('common.close')} aria-label={$t('common.close')}>
      <svg class="w-3.5 h-3.5" viewBox={ICONS.reject.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.reject.d} /></svg>
    </button>
  </div>

  <div class="flex-1 overflow-y-auto p-4 space-y-4 bg-[#FAFAFA]/50">
    {#if !configured}
      <div class="rounded-xl border border-amber-200 bg-amber-50 p-3 text-xs text-amber-900 space-y-1">
        <p class="font-semibold">{$t('brainstorm.notConfigured')}</p>
        <button class="underline font-medium" on:click={() => { onClose(); navigate('config'); }}>{$t('brainstorm.openConfig')}</button>
      </div>
    {/if}

    {#if error}
      <div class="rounded-xl border border-red-200 bg-red-50 p-3 text-xs text-red-700">{error}</div>
    {/if}

    {#if options.length}
      <div class="flex items-center justify-between gap-2">
        <button id="BrainstormBackButton" class="px-2.5 py-1 rounded-lg border border-gray-200 bg-white text-[11px] font-medium text-gray-600 hover:bg-gray-50" on:click={back}>
          &larr; {$t('brainstorm.back')}
        </button>
        {#if tokenUsage}
          <span class="badge badge-xs badge-outline gap-1 font-mono whitespace-nowrap">
            {$t('task.tokens.usage', {
              prompt: tokenUsage.prompt.toLocaleString(),
              completion: tokenUsage.completion.toLocaleString(),
            })}
          </span>
        {/if}
      </div>

      <div id="BrainstormResult" class="space-y-3">
        {#each options as option, i}
          <div class="rounded-xl border border-[#EAEAEA] bg-white p-4 space-y-2">
            <div class="flex items-center justify-between gap-2">
              <span class="text-[11px] font-semibold text-gray-500">{$t('brainstorm.option', { n: i + 1 })}</span>
              <button class="px-2 py-0.5 rounded-md border border-gray-200 text-[10px] text-gray-600 hover:bg-gray-50" on:click={() => copyOption(i, option)}>
                {copiedIndex === i ? $t('brainstorm.copied') : $t('brainstorm.copy')}
              </button>
            </div>
            <div class="text-[13px] leading-relaxed text-gray-800 md-body">{@html renderMarkdown(option)}</div>
          </div>
        {/each}
      </div>

      <button
        id="BrainstormMoreButton"
        class="w-full px-3.5 py-2 rounded-lg border border-gray-300 bg-white text-xs font-medium text-gray-700 hover:bg-gray-50 active:scale-[0.99] transition-all disabled:opacity-40 disabled:pointer-events-none"
        on:click={() => run(true)}
        disabled={busy}
      >{busy ? $t('brainstorm.running') : $t('brainstorm.more')}</button>
    {:else}
      <div class="space-y-2">
        <span class="field-label mt-0">{$t('brainstorm.action')}</span>
        <div class="flex flex-wrap gap-1.5">
          {#each ACTIONS as a}
            <button
              class="px-2.5 py-1 rounded-lg border text-[11px] font-medium transition-colors {action === a ? 'bg-black text-white border-black' : 'border-gray-200 bg-white text-gray-600 hover:bg-gray-50'}"
              on:click={() => pickAction(a)}
            >{$t('brainstorm.action.' + a)}</button>
          {/each}
        </div>
        {#if action === 'describe'}
          <div class="grid grid-cols-2 gap-1.5 pt-1">
            {#each modes as m}
              <button
                class="flex items-center gap-1.5 px-2 py-1.5 rounded-md border text-[10px] text-left transition-colors {mode === m ? 'bg-gray-900 text-white border-gray-900' : 'border-gray-200 bg-white text-gray-500 hover:bg-gray-50'}"
                on:click={() => mode = m}
              >
                <svg class="w-3 h-3 shrink-0" viewBox={ICONS[SENSE_ICONS[m]].vb} fill="currentColor" aria-hidden="true"><path d={ICONS[SENSE_ICONS[m]].d} /></svg>
                <span class="truncate">{$t('brainstorm.mode.' + m)}</span>
              </button>
            {/each}
          </div>
        {:else if modes.length}
          <div class="flex flex-wrap gap-1.5 pt-1">
            {#each modes as m}
              <button
                class="px-2 py-0.5 rounded-md border text-[10px] transition-colors {mode === m ? 'bg-gray-900 text-white border-gray-900' : 'border-gray-200 bg-white text-gray-500 hover:bg-gray-50'}"
                on:click={() => mode = m}
              >{$t('brainstorm.mode.' + m)}</button>
            {/each}
          </div>
        {/if}
      </div>

      <div>
        <span class="field-label mt-0">{$t('brainstorm.text')}</span>
        <textarea
          id="BrainstormTextInput"
          class="w-full min-h-[9rem] rounded-xl border border-gray-200 bg-white p-3 text-[13px] text-gray-800 placeholder-gray-400 focus:outline-none focus:border-gray-400 focus:ring-2 focus:ring-gray-100 resize-y"
          bind:value={text}
          placeholder={$t('brainstorm.textPlaceholder')}
          disabled={busy}
        ></textarea>
        <div class="flex justify-end mt-2">
          <button
            id="BrainstormRunButton"
            class="px-3.5 py-1.5 bg-black hover:bg-gray-800 active:scale-95 text-white text-xs font-medium rounded-lg transition-all disabled:opacity-40 disabled:pointer-events-none"
            on:click={() => run(false)}
            disabled={busy || !text.trim()}
          >{busy ? $t('brainstorm.running') : $t('brainstorm.run')}</button>
        </div>
      </div>
    {/if}
  </div>
</div>
