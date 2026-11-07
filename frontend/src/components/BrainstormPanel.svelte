<script>
  import { onMount, tick } from 'svelte';
  import { api, apiFetch } from '../lib/api.js';
  import { renderMarkdown } from '../lib/markdown.js';
  import { apiConfig, brainstormIntent, brainstormTokens } from '../lib/stores.js';
  import { t } from '../lib/i18n/index.js';
  import { navigate } from '../lib/router.js';
  import ICONS from '../lib/icons.js';

  export let onClose = () => {};

  const SENSES = ['sight', 'sound', 'smell', 'taste', 'tactile', 'temperature', 'pain', 'spatial', 'proprioception', 'sixth_sense', 'ambient'];
  const SENSE_ICONS = {
    sight: 'eye', sound: 'earListen', smell: 'wind', taste: 'tongue', tactile: 'hand',
    temperature: 'temperatureHalf', pain: 'bolt', spatial: 'compass',
    proprioception: 'personWalking', sixth_sense: 'brain', ambient: 'cloud',
  };
  const REWRITE_MODES = ['shorter', 'longer', 'expand', 'rephrase', 'descriptive', 'show', 'inner_conflict', 'intense', 'other'];
  const FEEDBACK_MODES = ['originality', 'clarity', 'pacing', 'grammar', 'feasibility'];
  // Same order as the floating selection toolbar in Chapter writing.
  const HOME = [
    { id: 'explain', icon: 'circleInfo' },
    { id: 'alternatives', icon: 'codeBranch' },
    { id: 'describe', icon: 'glasses' },
    { id: 'rewrite', icon: 'feather' },
    { id: 'feedback', icon: 'userTie' },
    { id: 'commune', icon: 'personPraying' },
  ];

  let stage = 'home'; // home | compose | describe | results | explain | commune
  let action = 'alternatives';
  let mode = '';
  let text = '';
  let custom = '';
  let context = '';
  let options = [];
  let describeOptions = [];
  let explain = null;
  let busy = false;
  let error = '';
  let copiedIndex = -1;
  let copyTimer;
  let requestId = '';
  let composerEl;
  let communeEl;
  let selectedSenses = [];
  let explainSingleWord = false;
  // One AbortController per in-flight card stream, so Stop cancels every
  // parallel request (three options, one describe sense, commune, explain).
  const activeControllers = new Set();

  let entities = [];
  let entitiesLoaded = false;
  let entityFilter = '';
  let communeEntity = null;
  let communeMessages = [];
  let communeInput = '';
  let communeBusy = false;
  let messagesEl;

  $: configured = !!(($apiConfig?.brainstorm?.model || '').trim());
  $: resultTitle = action === 'describe' && mode
    ? $t('brainstorm.mode.' + mode)
    : $t('brainstorm.action.' + action);
  $: placeholder = action === 'describe'
    ? $t('brainstorm.textPlaceholderDescribe')
    : action === 'explain'
      ? $t('brainstorm.textPlaceholderExplain')
      : action === 'feedback'
        ? $t('brainstorm.textPlaceholderFeedback')
        : $t('brainstorm.textPlaceholder');
  $: primaryLabel = action === 'describe' ? $t('brainstorm.describeRun') : $t('brainstorm.run');
  $: filteredEntities = entities.filter(e =>
    !entityFilter.trim() || (e.name || '').toLowerCase().includes(entityFilter.trim().toLowerCase()));

  let handledNonce = 0;
  $: if ($brainstormIntent && $brainstormIntent.nonce !== handledNonce) {
    handledNonce = $brainstormIntent.nonce;
    applyIntent($brainstormIntent);
  }

  // Config loads apiConfig; opening Brainstorm straight from Writing must not
  // show "no second model" just because Config was never visited.
  onMount(async () => {
    if ($apiConfig) return;
    try { apiConfig.set(await api('GET', '/api/config/api')); } catch (_) {}
  });

  function defaultMode(a) {
    if (a === 'rewrite') return 'shorter';
    if (a === 'feedback') return 'clarity';
    if (a === 'describe') return 'sight';
    return '';
  }

  function newRequestId() {
    requestId = 'bs-' + Date.now() + '-' + Math.random().toString(36).slice(2, 8);
    brainstormTokens.set(null);
    return requestId;
  }

  function applyIntent(intent) {
    text = intent.text || '';
    context = intent.context || '';
    action = intent.action || 'alternatives';
    mode = intent.mode || defaultMode(action);
    custom = intent.instruction || '';
    options = [];
    describeOptions = [];
    explain = null;
    error = '';
    selectedSenses = intent.mode && SENSES.includes(intent.mode) ? [intent.mode] : [];
    communeMessages = [];
    communeEntity = null;
    if (action === 'commune') {
      stage = 'commune';
      loadEntities();
    } else {
      stage = 'compose';
    }
    if (intent.text && intent.autoRun !== false) {
      if (action === 'alternatives' || action === 'rewrite' || action === 'feedback') runSet();
      else if (action === 'describe' && selectedSenses.length) runDescribe();
      else if (action === 'explain') runExplain();
    }
    tick().then(() => { growComposer(); composerEl?.focus(); });
  }

  function pickAction(id) {
    action = id;
    mode = defaultMode(id);
    custom = '';
    options = [];
    describeOptions = [];
    explain = null;
    error = '';
    selectedSenses = [];
    if (id === 'commune') {
      stage = 'commune';
      loadEntities();
      return;
    }
    stage = 'compose';
    tick().then(() => { growComposer(); composerEl?.focus(); });
  }

  function backHome() {
    stopAll();
    stage = 'home';
    options = [];
    describeOptions = [];
    explain = null;
    error = '';
    busy = false;
    copiedIndex = -1;
  }

  function newRequest() {
    stopAll();
    stage = 'compose';
    options = [];
    describeOptions = [];
    explain = null;
    error = '';
    copiedIndex = -1;
    tick().then(() => { growComposer(); composerEl?.focus(); });
  }

  function growComposer() {
    if (!composerEl) return;
    composerEl.style.height = 'auto';
    composerEl.style.height = Math.min(composerEl.scrollHeight, 160) + 'px';
  }

  function growCommune() {
    if (!communeEl) return;
    communeEl.style.height = 'auto';
    communeEl.style.height = Math.min(communeEl.scrollHeight, 120) + 'px';
  }

  function callPayload(rid, extra = {}) {
    const a = extra.action || action;
    const m = extra.mode !== undefined ? extra.mode : mode;
    return {
      action: a,
      mode: m === 'other' ? '' : m,
      text: extra.text !== undefined ? extra.text : text,
      instruction: a === 'rewrite' && m === 'other' ? custom : '',
      request_id: rid,
      variant: extra.variant || 1,
      more: !!extra.more,
      context,
      entity_kind: extra.entity_kind || '',
      entity_id: extra.entity_id || '',
      history: extra.history || [],
    };
  }

  // One SSE stream per card. onText receives the accumulated text (and, for
  // Explain, the partial explanation extracted from the streaming JSON).
  async function streamCall(payload, onText) {
    const ctrl = new AbortController();
    activeControllers.add(ctrl);
    try {
      const res = await apiFetch('/api/brainstorm/stream', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
        signal: ctrl.signal,
        noBody: true,
      });
      if (!res.body) throw new Error($t('brainstorm.empty'));
      const reader = res.body.getReader();
      const dec = new TextDecoder();
      let buf = '';
      let full = '';
      let done = null;
      while (true) {
        const step = await reader.read();
        if (step.done) break;
        buf += dec.decode(step.value, { stream: true });
        let cut;
        while ((cut = buf.indexOf('\n\n')) >= 0) {
          const frame = buf.slice(0, cut);
          buf = buf.slice(cut + 2);
          for (const line of frame.split('\n')) {
            if (!line.startsWith('data:')) continue;
            let evt;
            try { evt = JSON.parse(line.slice(5).trim()); } catch (_) { continue; }
            if (evt.error) throw new Error(evt.error);
            if (evt.delta) {
              full += evt.delta;
              onText(full, evt.preview || '');
            }
            if (evt.done) done = evt;
          }
        }
      }
      if (done?.result) {
        full = done.result;
        onText(full, done.explain?.explanation || '');
      }
      return done || { result: full };
    } finally {
      activeControllers.delete(ctrl);
    }
  }

  function stopAll() {
    // Invalidate the run first: aborted streams then see rid !== requestId and
    // never publish a late result over the next run.
    requestId = '';
    for (const ctrl of activeControllers) {
      try { ctrl.abort(); } catch (_) {}
    }
    activeControllers.clear();
    busy = false;
    communeBusy = false;
    describeOptions = describeOptions.map(d => ({ ...d, busy: false }));
    options = options.filter(Boolean);
  }

  async function runSet() {
    if (busy || !text.trim()) return;
    if (action === 'rewrite' && mode === 'other' && !custom.trim()) {
      error = $t('brainstorm.otherRequired');
      return;
    }
    busy = true;
    error = '';
    options = ['', '', ''];
    copiedIndex = -1;
    stage = 'results';
    const rid = newRequestId();
    const results = ['', '', ''];
    await Promise.all([0, 1, 2].map(async (i) => {
      try {
        await streamCall(callPayload(rid, { variant: i + 1 }), (full) => {
          if (rid !== requestId) return;
          results[i] = full;
          options = [...results];
        });
      } catch (_) {
        if (rid !== requestId) return;
      }
    }));
    if (rid !== requestId) return;
    busy = false;
    options = results.filter(Boolean);
    if (!options.length) error = $t('brainstorm.empty');
  }

  async function runMore() {
    if (busy) return;
    busy = true;
    error = '';
    const rid = requestId || newRequestId();
    const base = options.filter(Boolean);
    const results = [...base, '', '', ''];
    options = [...results];
    await Promise.all([0, 1, 2].map(async (i) => {
      try {
        await streamCall(callPayload(rid, { variant: i + 1, more: true }), (full) => {
          if (rid !== requestId) return;
          results[base.length + i] = full;
          options = [...results];
        });
      } catch (_) {
        if (rid !== requestId) return;
      }
    }));
    if (rid !== requestId) return;
    busy = false;
    options = results.filter(Boolean);
    if (options.length <= base.length) error = $t('brainstorm.empty');
  }

  function toggleSense(sense) {
    selectedSenses = selectedSenses.includes(sense)
      ? selectedSenses.filter(s => s !== sense)
      : [...selectedSenses, sense];
  }

  async function runDescribe(all = false) {
    if (busy || !text.trim()) return;
    if (all) selectedSenses = [...SENSES];
    const picks = all ? [...SENSES] : SENSES.filter(s => selectedSenses.includes(s));
    if (!picks.length) {
      error = $t('brainstorm.sensesRequired');
      return;
    }
    busy = true;
    error = '';
    options = [];
    copiedIndex = -1;
    stage = 'describe';
    describeOptions = picks.map(s => ({ sense: s, option: '', busy: true }));
    const rid = newRequestId();
    // Strictly one sense at a time: each card streams its own answer before
    // the next request starts, so the model never juggles overlapping calls.
    for (let i = 0; i < describeOptions.length; i++) {
      if (rid !== requestId) return;
      const sense = describeOptions[i].sense;
      try {
        await streamCall(callPayload(rid, { action: 'describe', mode: sense, variant: i + 1 }), (full) => {
          if (rid !== requestId) return;
          describeOptions = describeOptions.map((d, k) => k === i ? { ...d, option: full, busy: true } : d);
        });
      } catch (_) {
        if (rid !== requestId) return;
      }
      if (rid !== requestId) return;
      describeOptions = describeOptions.map((d, k) => k === i ? { ...d, busy: false } : d);
    }
    busy = false;
    if (!describeOptions.some(d => d.option)) error = $t('brainstorm.empty');
  }

  async function runExplain() {
    if (busy || !text.trim()) return;
    busy = true;
    error = '';
    explain = null;
    explainSingleWord = text.trim().split(/\s+/).length === 1;
    stage = 'explain';
    const rid = newRequestId();
    try {
      const res = await streamCall(callPayload(rid, { action: 'explain', variant: 1 }), (full, preview) => {
        if (rid !== requestId) return;
        if (preview) explain = { explanation: preview };
      });
      if (rid !== requestId) return;
      explain = res?.explain || { explanation: res?.result || '' };
    } catch (e) {
      if (rid === requestId) error = e?.message || String(e);
    } finally {
      if (rid === requestId) busy = false;
    }
  }

  function primaryAction() {
    if (busy) { stopAll(); return; }
    if (action === 'explain') runExplain();
    else if (action === 'describe') runDescribe();
    else runSet();
  }

  function openSense(sense) {
    action = 'describe';
    mode = sense;
    options = [];
    runSet();
  }

  async function copyOption(index, value) {
    try {
      await navigator.clipboard.writeText(value);
      copiedIndex = index;
      clearTimeout(copyTimer);
      copyTimer = setTimeout(() => { copiedIndex = -1; }, 1500);
    } catch (_) {}
  }

  async function loadEntities() {
    if (entitiesLoaded) return;
    entitiesLoaded = true;
    try {
      entities = (await api('GET', '/api/brainstorm/entities')) || [];
    } catch (e) {
      entitiesLoaded = false;
      error = e?.message || String(e);
    }
  }

  function selectEntity(e) {
    communeEntity = e;
    communeMessages = [];
    communeInput = '';
    error = '';
    tick().then(() => communeEl?.focus());
  }

  async function sendCommune() {
    const msg = communeInput.trim();
    if (!msg || communeBusy || !communeEntity) return;
    communeInput = '';
    communeMessages = [...communeMessages, { role: 'user', content: msg }];
    communeBusy = true;
    error = '';
    const rid = newRequestId();
    const history = communeMessages.slice(0, -1).map(m => ({ role: m.role, content: m.content }));
    const replyIndex = communeMessages.length;
    communeMessages = [...communeMessages, { role: 'assistant', content: '' }];
    try {
      const res = await streamCall(callPayload(rid, {
        action: 'commune',
        text: msg,
        entity_kind: communeEntity.kind,
        entity_id: communeEntity.id,
        history,
      }), (full) => {
        if (rid !== requestId) return;
        communeMessages = communeMessages.map((m, k) => k === replyIndex ? { ...m, content: full } : m);
      });
      if (rid !== requestId) return;
      if (!res?.result) {
        communeMessages = communeMessages.map((m, k) => k === replyIndex ? { ...m, content: $t('brainstorm.empty') } : m);
      }
    } catch (e) {
      if (rid === requestId) error = e?.message || String(e);
      // Keep a partial answer if the stream had already produced text; drop the
      // empty placeholder when it was cancelled before the first token.
      const partial = communeMessages[replyIndex]?.content || '';
      if (!partial) communeMessages = communeMessages.filter((m, k) => k !== replyIndex);
    } finally {
      if (rid === requestId) communeBusy = false;
    }
    tick().then(() => { if (messagesEl) messagesEl.scrollTop = messagesEl.scrollHeight; });
  }

  function communeKey(e) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendCommune();
    }
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

  <div class="flex-1 overflow-y-auto p-4 space-y-3 bg-[#FAFAFA]/50">
    {#if !configured}
      <div class="rounded-xl border border-amber-200 bg-amber-50 p-3 text-xs text-amber-900 space-y-1">
        <p class="font-semibold">{$t('brainstorm.notConfigured')}</p>
        <button class="underline font-medium" on:click={() => { onClose(); navigate('config'); }}>{$t('brainstorm.openConfig')}</button>
      </div>
    {/if}

    {#if error}
      <div class="rounded-xl border border-red-200 bg-red-50 p-3 text-xs text-red-700">{error}</div>
    {/if}

    {#if stage === 'home'}
      <p class="text-xs text-gray-500">{$t('brainstorm.homeHint')}</p>
      <div class="grid grid-cols-2 gap-2">
        {#each HOME as card}
          <button
            id={'BrainstormCard_' + card.id}
            class="group flex flex-col gap-2 rounded-xl border border-[#EAEAEA] bg-white p-3 text-left hover:border-gray-300 hover:bg-gray-50 transition-colors"
            on:click={() => pickAction(card.id)}
          >
            <span class="w-8 h-8 rounded-lg bg-gray-100 flex items-center justify-center text-gray-700 group-hover:bg-black group-hover:text-white transition-colors">
              <svg class="w-3.5 h-3.5" viewBox={ICONS[card.icon].vb} fill="currentColor" aria-hidden="true"><path d={ICONS[card.icon].d} /></svg>
            </span>
            <span class="text-xs font-semibold text-gray-900">{$t('brainstorm.action.' + card.id)}</span>
            <span class="text-[10px] leading-snug text-gray-500">{$t('brainstorm.card.' + card.id)}</span>
          </button>
        {/each}
      </div>

    {:else if stage === 'compose'}
      <div class="flex items-center justify-between gap-2">
        <button class="text-[11px] text-gray-500 hover:text-black" on:click={backHome}>&larr; {$t('brainstorm.back')}</button>
        <span class="text-[11px] font-semibold text-gray-700">{$t('brainstorm.action.' + action)}</span>
      </div>

      {#if action === 'rewrite' || action === 'feedback'}
        <div class="grid grid-cols-2 gap-1.5">
          {#each (action === 'rewrite' ? REWRITE_MODES : FEEDBACK_MODES) as m}
            <button
              class="rounded-lg border px-2.5 py-2 text-left transition-colors {mode === m ? 'border-gray-900 bg-gray-900 text-white' : 'border-[#EAEAEA] bg-white hover:border-gray-300 hover:bg-gray-50'}"
              on:click={() => mode = m}
            >
              <span class="block text-[11px] font-semibold">{m === 'other' ? $t('brainstorm.mode.other') : $t('brainstorm.mode.' + m)}</span>
              <span class="block text-[10px] leading-snug {mode === m ? 'text-gray-300' : 'text-gray-500'}">{$t('brainstorm.modeHint.' + m)}</span>
            </button>
          {/each}
        </div>
        {#if action === 'rewrite' && mode === 'other'}
          <input
            class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-[12px] text-gray-800 placeholder-gray-400 focus:outline-none focus:border-gray-400"
            bind:value={custom}
            placeholder={$t('brainstorm.otherPlaceholder')}
          />
        {/if}
      {:else if action === 'describe'}
        <p class="text-xs text-gray-500">{$t('brainstorm.describeHint')}</p>
        <div class="flex items-center justify-between gap-2">
          <span class="text-[10px] font-semibold uppercase tracking-wider text-gray-400">{$t('brainstorm.sensesTitle')}</span>
          <div class="flex gap-1">
            <button class="px-1.5 py-0.5 rounded border border-gray-200 text-[10px] text-gray-600 hover:bg-gray-50" on:click={() => selectedSenses = [...SENSES]}>{$t('brainstorm.sensesAll')}</button>
            <button class="px-1.5 py-0.5 rounded border border-gray-200 text-[10px] text-gray-600 hover:bg-gray-50" on:click={() => selectedSenses = []}>{$t('brainstorm.sensesNone')}</button>
          </div>
        </div>
        <div class="grid grid-cols-2 gap-1.5">
          {#each SENSES as s}
            <button
              class="flex items-center gap-1.5 rounded-lg border px-2 py-1.5 text-[11px] transition-colors {selectedSenses.includes(s) ? 'border-gray-900 bg-gray-900 text-white' : 'border-[#EAEAEA] bg-white text-gray-600 hover:border-gray-300'}"
              aria-pressed={selectedSenses.includes(s)}
              on:click={() => toggleSense(s)}
            >
              <svg class="w-3 h-3 shrink-0" viewBox={ICONS[SENSE_ICONS[s]].vb} fill="currentColor" aria-hidden="true"><path d={ICONS[SENSE_ICONS[s]].d} /></svg>
              <span class="truncate">{$t('brainstorm.mode.' + s)}</span>
            </button>
          {/each}
        </div>
      {:else if action === 'explain'}
        <p class="text-xs text-gray-500">{$t('brainstorm.explainHint')}</p>
      {:else}
        <p class="text-xs text-gray-500">{$t('brainstorm.alternativesHint')}</p>
      {/if}

    {:else if stage === 'describe'}
      <div class="flex items-center justify-between gap-2">
        <button class="text-[11px] text-gray-500 hover:text-black" on:click={newRequest}>&larr; {$t('brainstorm.back')}</button>
        <span class="text-[11px] font-semibold text-gray-700">{$t('brainstorm.describeTitle')}</span>
      </div>
      <div class="space-y-2">
        {#each describeOptions as d}
          <button
            class="w-full flex items-start gap-3 rounded-xl border border-[#EAEAEA] bg-white p-3 text-left transition-colors {d.option ? 'hover:border-gray-300 hover:bg-gray-50' : 'opacity-70'}"
            on:click={() => d.option && openSense(d.sense)}
            disabled={!d.option}
          >
            <span class="w-7 h-7 shrink-0 rounded-lg bg-gray-100 flex items-center justify-center text-gray-600">
              <svg class="w-3 h-3" viewBox={ICONS[SENSE_ICONS[d.sense]].vb} fill="currentColor" aria-hidden="true"><path d={ICONS[SENSE_ICONS[d.sense]].d} /></svg>
            </span>
            <span class="flex-1 min-w-0">
              <span class="flex items-center justify-between gap-2">
                <span class="text-[11px] font-semibold text-gray-800">{$t('brainstorm.mode.' + d.sense)}</span>
                <span class="text-[10px] text-gray-400 shrink-0">{d.busy ? $t('brainstorm.running') : d.option ? $t('brainstorm.go') : '—'}</span>
              </span>
              <span class="block text-[11px] leading-relaxed text-gray-600 mt-1 whitespace-pre-wrap">{d.option || ''}</span>
            </span>
          </button>
        {/each}
      </div>

    {:else if stage === 'results'}
      <div class="flex items-center justify-between gap-2">
        <button class="text-[11px] text-gray-500 hover:text-black" on:click={newRequest}>&larr; {$t('brainstorm.back')}</button>
        <span class="text-[11px] font-semibold text-gray-700">{resultTitle}</span>
      </div>
      {#if busy && !options.some(Boolean)}
        <p class="text-xs text-gray-500 py-6 text-center">{$t('brainstorm.running')}</p>
      {/if}
      <div id="BrainstormResult" class="space-y-3">
        {#each options as option, i}
          <div class="rounded-xl border border-[#EAEAEA] bg-white p-4 space-y-2">
            <div class="flex items-center justify-between gap-2">
              <span class="text-[11px] font-semibold text-gray-500">{$t('brainstorm.option', { n: i + 1 })}</span>
              {#if option}
                <button class="px-2 py-0.5 rounded-md border border-gray-200 text-[10px] text-gray-600 hover:bg-gray-50" on:click={() => copyOption(i, option)}>
                  {copiedIndex === i ? $t('brainstorm.copied') : $t('brainstorm.copy')}
                </button>
              {/if}
            </div>
            {#if option}
              <div class="text-[13px] leading-relaxed text-gray-800 md-body">{@html renderMarkdown(option)}</div>
            {:else}
              <p class="text-xs text-gray-400">{$t('brainstorm.running')}</p>
            {/if}
          </div>
        {/each}
      </div>
      {#if options.some(Boolean) && !busy}
        <button
          id="BrainstormMoreButton"
          class="w-full px-3.5 py-2 rounded-lg border border-gray-300 bg-white text-xs font-medium text-gray-700 hover:bg-gray-50 active:scale-[0.99] transition-all disabled:opacity-40 disabled:pointer-events-none"
          on:click={runMore}
        >{$t('brainstorm.more')}</button>
      {/if}

    {:else if stage === 'explain'}
      <div class="flex items-center justify-between gap-2">
        <button class="text-[11px] text-gray-500 hover:text-black" on:click={newRequest}>&larr; {$t('brainstorm.back')}</button>
        <span class="text-[11px] font-semibold text-gray-700">{$t('brainstorm.explainTitle')}</span>
      </div>
      {#if busy && !explain}
        <p class="text-xs text-gray-500 py-6 text-center">{$t('brainstorm.running')}</p>
      {/if}
      {#if explain}
        <section class="rounded-xl border border-[#EAEAEA] bg-white p-4 space-y-1">
          <h3 class="text-[10px] font-semibold uppercase tracking-wider text-gray-400">{$t('brainstorm.explain.explanation')}</h3>
          <p class="text-[13px] leading-relaxed text-gray-800">{explain.explanation}</p>
        </section>
        {#if explainSingleWord && explain.definition}
          <section class="rounded-xl border border-[#EAEAEA] bg-white p-4 space-y-1">
            <h3 class="text-[10px] font-semibold uppercase tracking-wider text-gray-400">{$t('brainstorm.explain.definition')}</h3>
            <p class="text-[13px] leading-relaxed text-gray-800">{explain.definition}</p>
          </section>
        {/if}
        {#if explainSingleWord && explain.synonyms?.length}
          <section class="rounded-xl border border-[#EAEAEA] bg-white p-4 space-y-2">
            <h3 class="text-[10px] font-semibold uppercase tracking-wider text-gray-400">{$t('brainstorm.explain.synonyms')}</h3>
            <div class="flex flex-wrap gap-1.5">
              {#each explain.synonyms as s}<span class="px-2 py-0.5 rounded-md bg-gray-100 text-[11px] text-gray-700">{s}</span>{/each}
            </div>
          </section>
        {/if}
        {#if explainSingleWord && explain.antonyms?.length}
          <section class="rounded-xl border border-[#EAEAEA] bg-white p-4 space-y-2">
            <h3 class="text-[10px] font-semibold uppercase tracking-wider text-gray-400">{$t('brainstorm.explain.antonyms')}</h3>
            <div class="flex flex-wrap gap-1.5">
              {#each explain.antonyms as s}<span class="px-2 py-0.5 rounded-md bg-gray-100 text-[11px] text-gray-700">{s}</span>{/each}
            </div>
          </section>
        {/if}
        {#if explain.semantic_field?.length}
          <section class="rounded-xl border border-[#EAEAEA] bg-white p-4 space-y-2">
            <h3 class="text-[10px] font-semibold uppercase tracking-wider text-gray-400">{$t('brainstorm.explain.semanticField')}</h3>
            <div class="flex flex-wrap gap-1.5">
              {#each explain.semantic_field as s}<span class="px-2 py-0.5 rounded-md bg-gray-100 text-[11px] text-gray-700">{s}</span>{/each}
            </div>
          </section>
        {/if}
        {#if explain.tone?.label || explain.tone?.note}
          <section class="rounded-xl border border-[#EAEAEA] bg-white p-4 space-y-1">
            <h3 class="text-[10px] font-semibold uppercase tracking-wider text-gray-400">{$t('brainstorm.explain.tone')}</h3>
            {#if explain.tone.label}<span class="inline-block px-2 py-0.5 rounded-md bg-gray-900 text-white text-[10px] font-medium">{explain.tone.label}</span>{/if}
            {#if explain.tone.note}<p class="text-[12px] leading-relaxed text-gray-700 pt-1">{explain.tone.note}</p>{/if}
          </section>
        {/if}
        {#if explain.readings?.length}
          <section class="rounded-xl border border-[#EAEAEA] bg-white p-4 space-y-3">
            <h3 class="text-[10px] font-semibold uppercase tracking-wider text-gray-400">{$t('brainstorm.explain.readings')}</h3>
            {#each explain.readings as r}
              <div class="space-y-0.5">
                <p class="text-[11px] font-semibold text-gray-800">{r.label}</p>
                <p class="text-[12px] leading-relaxed text-gray-600">{r.text}</p>
              </div>
            {/each}
          </section>
        {/if}
      {/if}

    {:else if stage === 'commune'}
      {#if !communeEntity}
        <div class="flex items-center justify-between gap-2">
          <button class="text-[11px] text-gray-500 hover:text-black" on:click={backHome}>&larr; {$t('brainstorm.back')}</button>
          <span class="text-[11px] font-semibold text-gray-700">{$t('brainstorm.communeTitle')}</span>
        </div>
        <p class="text-xs text-gray-500">{$t('brainstorm.communeHint')}</p>
        <input
          class="w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-[12px] text-gray-800 placeholder-gray-400 focus:outline-none focus:border-gray-400"
          bind:value={entityFilter}
          placeholder={$t('brainstorm.communeSearch')}
        />
        {#if !filteredEntities.length}
          <p class="text-xs text-gray-500 py-6 text-center">{$t('brainstorm.communeEmpty')}</p>
        {:else}
          <div class="space-y-1">
            {#each filteredEntities as e}
              <button class="w-full flex items-center justify-between gap-2 rounded-lg border border-[#EAEAEA] bg-white px-3 py-2 text-left hover:border-gray-300 hover:bg-gray-50 transition-colors" on:click={() => selectEntity(e)}>
                <span class="min-w-0">
                  <span class="block text-[12px] font-medium text-gray-900 truncate">{e.name}</span>
                  <span class="block text-[10px] text-gray-400 truncate">{e.kind}{e.subtitle ? ' · ' + e.subtitle : ''}</span>
                </span>
                <span class="text-[10px] text-gray-400 shrink-0">{$t('brainstorm.go')}</span>
              </button>
            {/each}
          </div>
        {/if}
      {:else}
        <div class="flex items-center justify-between gap-2">
          <button class="text-[11px] text-gray-500 hover:text-black" on:click={() => { communeEntity = null; communeMessages = []; }}>&larr; {$t('brainstorm.communeChange')}</button>
          <span class="text-[11px] font-semibold text-gray-700 truncate">{communeEntity.name}</span>
        </div>
        <div bind:this={messagesEl} class="space-y-2">
          {#if !communeMessages.length}
            <p class="text-xs text-gray-500 py-4 text-center">{$t('brainstorm.communeStart', { name: communeEntity.name })}</p>
          {/if}
          {#each communeMessages as m}
            <div class="flex {m.role === 'user' ? 'justify-end' : 'justify-start'}">
              <div class="max-w-[85%] rounded-xl px-3 py-2 text-[12px] leading-relaxed whitespace-pre-wrap {m.role === 'user' ? 'bg-white border border-[#EAEAEA] text-gray-800' : 'bg-[#F4F4F5] text-gray-800'}">{m.content}</div>
            </div>
          {/each}
          {#if communeBusy}
            <div class="flex justify-start"><div class="rounded-xl bg-[#F4F4F5] px-3 py-2 text-[12px] text-gray-500">{$t('brainstorm.running')}</div></div>
          {/if}
        </div>
      {/if}
    {/if}
  </div>

  {#if stage !== 'home'}
    <div class="shrink-0 border-t border-[#EAEAEA] bg-white p-3">
      {#if stage === 'commune' && communeEntity}
        <div class="flex items-end gap-1 rounded-xl border border-gray-200 focus-within:border-gray-400 transition-all">
          <textarea
            bind:this={communeEl}
            bind:value={communeInput}
            on:input={growCommune}
            on:keydown={communeKey}
            rows="1"
            class="bs-grow w-full pl-3.5 pr-2 py-2.5 bg-transparent text-sm text-gray-800 placeholder-gray-400 focus:outline-none leading-relaxed"
            placeholder={$t('brainstorm.communePlaceholder', { name: communeEntity.name })}
            disabled={communeBusy}
          ></textarea>
          <button
            class="shrink-0 m-1 w-8 h-8 rounded-lg bg-black text-white flex items-center justify-center hover:bg-gray-800 disabled:opacity-30 disabled:pointer-events-none transition-colors"
            on:click={communeBusy ? stopAll : sendCommune}
            disabled={!communeBusy && !communeInput.trim()}
            title={communeBusy ? $t('brainstorm.stop') : $t('brainstorm.send')}
            aria-label={communeBusy ? $t('brainstorm.stop') : $t('brainstorm.send')}
          >
            {#if communeBusy}
              <svg class="w-3 h-3" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><rect x="6" y="6" width="12" height="12" rx="2" /></svg>
            {:else}
              <svg class="w-3 h-3" viewBox={ICONS.paperPlane.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.paperPlane.d} /></svg>
            {/if}
          </button>
        </div>
      {:else if stage !== 'commune'}
        {#if stage === 'compose' && action === 'describe'}
          <button
            id="BrainstormDescribeAllButton"
            class="w-full mb-2 px-3.5 py-2 rounded-lg border border-gray-300 bg-white text-xs font-medium text-gray-700 hover:bg-gray-50 active:scale-[0.99] transition-all disabled:opacity-40 disabled:pointer-events-none"
            on:click={() => runDescribe(true)}
            disabled={busy || !text.trim()}
          >{$t('brainstorm.describeAll')}</button>
        {/if}
        <div class="flex items-end gap-1 rounded-xl border border-gray-200 focus-within:border-gray-400 transition-all">
          <textarea
            id="BrainstormTextInput"
            bind:this={composerEl}
            bind:value={text}
            on:input={growComposer}
            rows="1"
            class="bs-grow w-full pl-3.5 pr-2 py-2.5 bg-transparent text-sm text-gray-800 placeholder-gray-400 focus:outline-none leading-relaxed"
            {placeholder}
            disabled={busy}
          ></textarea>
          <button
            id="BrainstormRunButton"
            class="shrink-0 m-1 px-3 h-8 rounded-lg bg-black hover:bg-gray-800 text-white text-xs font-medium disabled:opacity-30 disabled:pointer-events-none transition-colors"
            on:click={primaryAction}
            disabled={!busy && !text.trim()}
          >{busy ? $t('brainstorm.stop') : primaryLabel}</button>
        </div>
      {/if}
    </div>
  {/if}
</div>
