<script>
  // Automated AI beta reader panel — the simulated reader section that sits
  // BEFORE the bestseller-editor card in the act review. The author never
  // configures anything: the backend generates the panel, runs every reader,
  // consolidates the report and feeds it to the expert. This component only
  // displays progress, the consolidated report and the individual evaluations.
  import { t } from '../lib/i18n/index.js';

  export let actNo = 0;
  export let reports = [];     // BetaReaderReport[] (oldest first)
  export let panel = null;     // BetaPanel (project-wide, reused every act)
  export let progress = null;  // latest beta_progress event for this act
  export let disabled = false;
  export let scrollId = '';
  export let onRun = null;      // optional () => void: runs the beta pass independently

  // Keep the newest session selected when a fresh one arrives; the selector
  // below lets the author compare it with earlier sessions of the same act.
  let selectedIdx = -1;
  let seenLen = 0;
  $: if (reports.length !== seenLen) {
    seenLen = reports.length;
    selectedIdx = reports.length - 1;
  }
  $: report = selectedIdx >= 0 ? (reports[selectedIdx] || null) : null;

  const STAGES = [
    { key: 'panel', label: 'beta.stage.panel' },
    { key: 'personas', label: 'beta.stage.personas' },
    { key: 'evaluating', label: 'beta.stage.evaluating' },
    { key: 'consolidating', label: 'beta.stage.consolidating' },
    { key: 'expert', label: 'beta.stage.expert' },
  ];
  const STAGE_ORDER = ['panel', 'personas', 'reading', 'evaluating', 'consolidating', 'expert', 'done'];
  function stageState(key) {
    if (report) return 'done';
    const cur = progress?.stage || '';
    if (!cur) return 'pending';
    if (cur === 'done') return 'done';
    const ci = STAGE_ORDER.indexOf(cur);
    const si = STAGE_ORDER.indexOf(key);
    if (ci < 0 || si < 0) return 'pending';
    if (si < ci) return 'done';
    if (si === ci) return 'active';
    // The backend uses separate reading/evaluating events for one UI step.
    if (key === 'evaluating' && (cur === 'reading' || cur === 'evaluating')) return 'active';
    return 'pending';
  }
  $: running = !!progress && !report && progress.stage !== 'done';

  const GROUPS = [
    ['shared_positives', 'beta.group.sharedPositives'],
    ['shared_concerns', 'beta.group.sharedConcerns'],
    ['polarized_reactions', 'beta.group.polarized'],
    ['unexpected_interpretations', 'beta.group.unexpected'],
    ['unfulfilled_expectations', 'beta.group.unfulfilled'],
    ['engagement_patterns', 'beta.group.engagement'],
    ['character_perception', 'beta.group.characters'],
    ['memorable_elements', 'beta.group.memorable'],
    ['productive_ambiguity', 'beta.group.ambiguity'],
    ['individual_observations', 'beta.group.individual'],
  ];
  $: groups = report
    ? GROUPS.map(([key, label]) => ({ key, label, items: report[key] || [] })).filter(g => g.items.length)
    : [];

  function levelWash(level) {
    return level === 'high' ? 'wash-red' : level === 'low' ? 'wash-green' : 'wash-amber';
  }
  function archetypeLabel(a) {
    const key = 'beta.archetype.' + a;
    const msg = $t(key);
    return msg && msg !== key ? msg : a;
  }
  function kindLabel(kind) {
    return kind === 'subjective_preference' ? $t('beta.kind.preference') : $t('beta.kind.narrative');
  }
  function readerNames(ids) {
    const byId = new Map((panel?.personas || []).map(p => [p.id, p.name]));
    return (ids || []).map(id => byId.get(id) || id).join(', ');
  }
  function sessionLabel(i) {
    const r = reports[i];
    const n = i + 1;
    const date = r?.generated_at ? r.generated_at.slice(0, 16).replace('T', ' ') : '';
    return `${$t('beta.session', { n })}${date ? ' · ' + date : ''}`;
  }

  // Compact persona inspector: identity + the parameters that actually change
  // reading behavior, collapsed by default so the report stays readable.
  let openPersona = null;
  function togglePersona(id) {
    openPersona = openPersona === id ? null : id;
  }

  function momentGroups(ev) {
    return [
      { label: 'beta.mostEngaging', items: ev.most_engaging || [] },
      { label: 'beta.leastEngaging', items: ev.least_engaging || [] },
      { label: 'beta.memorable', items: ev.memorable || [] },
      { label: 'beta.confusing', items: ev.confusing || [] },
      { label: 'beta.surprising', items: ev.surprising || [] },
      { label: 'beta.emotional', items: ev.emotional || [] },
    ].filter(g => g.items.length);
  }
</script>

<div id={scrollId} class="card bg-base-200 min-w-0">
  <div class="card-body p-6 gap-4">
    <div class="flex items-start justify-between gap-4 flex-wrap">
      <div class="min-w-0">
        <h3 class="card-title text-base flex items-center gap-2 flex-wrap">
          {$t('beta.title')}
          <span class="badge badge-outline badge-sm">{$t('beta.simulated')}</span>
        </h3>
        <span class="field-label">{$t('beta.subtitle')}</span>
      </div>
      {#if reports.length > 1}
        <label class="flex items-center gap-2 text-xs shrink-0">
          <span class="text-base-content/60">{$t('beta.history')}</span>
          <select class="select select-sm select-bordered" disabled={disabled}
            value={String(selectedIdx)}
            on:change={(e) => (selectedIdx = Number(e.currentTarget.value))}>
            {#each reports as _r, i}
              <option value={String(i)}>{sessionLabel(i)}</option>
            {/each}
          </select>
        </label>
      {/if}
      {#if onRun && report}
        <button class="btn btn-ghost btn-xs shrink-0" disabled={disabled} on:click={onRun}>
          {$t('beta.rerun')}
        </button>
      {/if}
    </div>

    <!-- Progress checklist: visible while the automated pipeline runs, and
         collapsed into a completed strip once the report exists. -->
    {#if running}
      <ol class="grid grid-cols-1 sm:grid-cols-5 gap-2 text-xs" aria-live="polite">
        {#each STAGES as stage (stage.key)}
          {@const st = stageState(stage.key)}
          <li class="rounded-lg px-3 py-2 flex items-center gap-2 {st === 'active' ? 'bg-base-300/40 font-semibold' : 'bg-base-100 text-base-content/60'}">
            {#if st === 'done'}
              <span class="text-[#16a34a] shrink-0">✓</span>
            {:else if st === 'active'}
              <span class="loading loading-spinner loading-xs shrink-0"></span>
            {:else}
              <span class="w-2 h-2 rounded-full bg-base-content/20 shrink-0"></span>
            {/if}
            <span class="min-w-0 truncate">{$t(stage.label)}</span>
            {#if stage.key === 'evaluating' && st === 'active' && progress?.total}
              <span class="ml-auto shrink-0 tabular-nums">{progress.index || 0}/{progress.total}</span>
            {/if}
          </li>
        {/each}
      </ol>
      {#if progress?.detail}
        <p class="text-xs text-base-content/60">{$t('beta.evaluatingReader', { name: progress.detail })}</p>
      {/if}
    {/if}

    <!-- Project-wide panel: same readers across every act. -->
    {#if panel?.personas?.length}
      <details class="bg-base-100 rounded-lg p-4">
        <summary class="cursor-pointer text-sm font-semibold select-none flex items-center gap-2 flex-wrap">
          {$t('beta.panel')}
          <span class="badge badge-ghost badge-sm">{$t('beta.panelSize', { n: panel.personas.length })}</span>
        </summary>
        <div class="mt-3 flex flex-wrap gap-1.5">
          {#each panel.personas as p (p.id)}
            <button type="button"
              class="badge badge-outline badge-sm cursor-pointer {openPersona === p.id ? 'wash-amber' : ''}"
              title={archetypeLabel(p.archetype)}
              on:click={() => togglePersona(p.id)}>
              {p.name} · {p.age}
            </button>
          {/each}
        </div>
        {#if openPersona}
          {@const p = panel.personas.find(x => x.id === openPersona)}
          {#if p}
            <div class="mt-3 grid grid-cols-1 md:grid-cols-2 gap-3 text-xs">
              <div class="space-y-1.5">
                <p><span class="font-semibold">{$t('beta.persona.identity')}:</span> {archetypeLabel(p.archetype)} · {p.age}</p>
                {#if p.background}<p><span class="font-semibold">{$t('beta.persona.background')}:</span> {p.background}</p>{/if}
                {#if p.reading_habits}<p><span class="font-semibold">{$t('beta.persona.habits')}:</span> {p.reading_habits}</p>{/if}
                {#if p.biases?.length}<p><span class="font-semibold">{$t('beta.persona.biases')}:</span> {p.biases.join(' · ')}</p>{/if}
                {#if p.blind_spots?.length}<p><span class="font-semibold">{$t('beta.persona.blindSpots')}:</span> {p.blind_spots.join(' · ')}</p>{/if}
              </div>
              <div class="space-y-1.5">
                <p><span class="font-semibold">{$t('beta.persona.psychology')}:</span>
                  {p.psychology.emotional_sensitivity}/10 · {p.psychology.empathy}/10 · {p.psychology.intellectual_curiosity}/10 · {p.psychology.tolerance_for_ambiguity}/10
                </p>
                <p><span class="font-semibold">{$t('beta.persona.sensitivity')}:</span>
                  {p.sensitivity.tolerance_for_ambiguity} · {p.sensitivity.need_for_fast_pacing} · {p.sensitivity.attention_to_continuity} · {p.sensitivity.sensitivity_to_exposition} · {p.sensitivity.preference_for_psychological_complexity} · {p.sensitivity.need_for_narrative_closure} · {p.sensitivity.tolerance_for_unresolved_plotlines} · {p.sensitivity.sensitivity_to_implausible_character_behavior} · {p.sensitivity.attention_to_dialogue} · {p.sensitivity.sensitivity_to_predictable_twists}
                </p>
                <p><span class="font-semibold">{$t('beta.persona.priorities')}:</span>
                  {p.priorities.plot} · {p.priorities.characters} · {p.priorities.prose} · {p.priorities.pacing} · {p.priorities.worldbuilding} · {p.priorities.dialogue} · {p.priorities.emotional_impact} · {p.priorities.originality} · {p.priorities.themes}
                </p>
              </div>
            </div>
          {/if}
        {/if}
      </details>
    {/if}

    {#if report}
      <div class="flex items-center gap-2 flex-wrap text-sm">
        <span class="badge badge-lg wash-amber">{$t('beta.engagement')}: {report.average_engagement}/100</span>
        <span class="badge badge-lg wash-green">{$t('beta.continue')}: {report.average_continue_reading}/100</span>
        <span class="text-base-content/60 text-xs">{$t('beta.panelSize', { n: report.panel_size })}</span>
        <span class="ml-auto text-xs text-base-content/60">{report.generated_at?.slice(0, 10)}</span>
      </div>

      {#if report.summary}
        <section class="bg-base-100 rounded-lg p-4">
          <h4 class="text-sm font-semibold mb-1.5">{$t('beta.summary')}</h4>
          <p class="text-sm text-base-content/80 whitespace-pre-wrap">{report.summary}</p>
        </section>
      {/if}

      <div class="space-y-3">
        {#each groups as g (g.key)}
          <section class="bg-base-100 rounded-lg p-4 space-y-2">
            <h4 class="text-sm font-semibold">{$t(g.label)} <span class="badge badge-ghost badge-sm">{g.items.length}</span></h4>
            {#each g.items as f, i (g.key + ':' + i)}
              <div class="bg-base-300/25 rounded-lg p-3 space-y-1.5">
                <div class="flex items-center gap-1.5 flex-wrap">
                  <span class="badge badge-outline badge-xs">{kindLabel(f.kind)}</span>
                  <span class="badge badge-xs {levelWash(f.severity)}">{$t('beta.severity')}: {$t('beta.level.' + (f.severity || 'medium'))}</span>
                  <span class="badge badge-xs {levelWash(f.confidence)}">{$t('beta.confidence')}: {$t('beta.level.' + (f.confidence || 'medium'))}</span>
                  <span class="badge badge-ghost badge-xs" title={readerNames(f.readers)}>{$t('beta.readerCount', { n: f.reader_count, total: report.panel_size })}</span>
                </div>
                <p class="text-sm text-base-content/85 whitespace-pre-wrap">{f.description}</p>
                {#if f.readers?.length}
                  <p class="text-[11px] text-base-content/55">{$t('beta.readers')}: {readerNames(f.readers)}</p>
                {/if}
                {#if f.locations?.length}
                  <ul class="text-[11px] text-base-content/60 space-y-0.5">
                    {#each f.locations as l, li (li)}
                      <li>
                        {#if l.chapter}<span class="font-medium">{$t('beta.chapter', { n: l.chapter })}</span>{/if}
                        {#if l.scene} · {l.scene}{/if}
                        {#if l.quote}<span class="italic"> “{l.quote}”</span>{/if}
                      </li>
                    {/each}
                  </ul>
                {/if}
              </div>
            {/each}
          </section>
        {/each}
      </div>

      <!-- Individual evaluations: inspectable, never required for the flow. -->
      {#if report.evaluations?.length}
        <details class="bg-base-100 rounded-lg p-4">
          <summary class="cursor-pointer text-sm font-semibold select-none">
            {$t('beta.individual')} <span class="badge badge-ghost badge-sm">{report.evaluations.length}</span>
          </summary>
          <div class="mt-3 space-y-2">
            {#each report.evaluations as ev (ev.persona_id)}
              <details class="bg-base-300/25 rounded-lg p-3">
                <summary class="cursor-pointer select-none flex items-center gap-2 flex-wrap text-sm">
                  <span class="font-semibold">{ev.persona_name}</span>
                  <span class="badge badge-outline badge-xs">{archetypeLabel(ev.archetype)}</span>
                  {#if ev.age}<span class="text-xs text-base-content/55">{ev.age}</span>{/if}
                  <span class="ml-auto flex items-center gap-1 flex-wrap">
                    <span class="badge badge-xs wash-amber">{$t('beta.engagement')} {ev.engagement}</span>
                    <span class="badge badge-xs wash-green">{$t('beta.continue')} {ev.continue_reading}</span>
                    <span class="badge badge-xs wash-green">{$t('beta.satisfaction')} {ev.satisfaction}</span>
                  </span>
                </summary>
                <div class="mt-2 space-y-2 text-xs">
                  {#if ev.summary}<p class="text-sm text-base-content/80 whitespace-pre-wrap">{ev.summary}</p>{/if}
                  {#each momentGroups(ev) as mg (mg.label)}
                    <div>
                      <span class="font-semibold">{$t(mg.label)}:</span>
                      <ul class="mt-0.5 space-y-1">
                        {#each mg.items as m, mi (mi)}
                          <li class="text-base-content/70">
                            {#if m.chapter}<span class="font-medium">{$t('beta.chapter', { n: m.chapter })}</span>{/if}
                            {#if m.location} · {m.location}{/if}
                            {#if m.quote}<span class="italic"> “{m.quote}”</span>{/if}
                            {#if m.note}<span> — {m.note}</span>{/if}
                          </li>
                        {/each}
                      </ul>
                    </div>
                  {/each}
                  {#if ev.character_perceptions?.length}
                    <div>
                      <span class="font-semibold">{$t('beta.characterPerceptions')}:</span>
                      <ul class="mt-0.5 space-y-1">
                        {#each ev.character_perceptions as cp, ci (ci)}
                          <li class="text-base-content/70"><span class="font-medium">{cp.character}:</span> {cp.perception}{#if cp.emotional_connection} — <span class="italic">{cp.emotional_connection}</span>{/if}</li>
                        {/each}
                      </ul>
                    </div>
                  {/if}
                  {#each [['understood', 'beta.understood'], ['misunderstood', 'beta.misunderstood'], ['suspects', 'beta.suspects'], ['expectations', 'beta.expectations'], ['worked', 'beta.worked'], ['personal_preferences', 'beta.preferences']] as [field, label] (field)}
                    {#if ev[field]?.length}
                      <div>
                        <span class="font-semibold">{$t(label)}:</span>
                        <ul class="mt-0.5 list-disc list-inside text-base-content/70">
                          {#each ev[field] as item, ii (ii)}<li>{item}</li>{/each}
                        </ul>
                      </div>
                    {/if}
                  {/each}
                  {#if ev.concerns?.length}
                    <div>
                      <span class="font-semibold">{$t('beta.concerns')}:</span>
                      <ul class="mt-0.5 space-y-1">
                        {#each ev.concerns as c, ci (ci)}
                          <li class="text-base-content/70">
                            <span class="badge badge-xs {c.kind === 'personal_preference' ? 'wash-green' : 'wash-red'}">{kindLabel(c.kind)}</span>
                            {#if c.chapter}<span class="font-medium">{$t('beta.chapter', { n: c.chapter })}</span>{/if}
                            {#if c.location} · {c.location}{/if}
                            — {c.description}
                            {#if c.quote}<span class="italic"> “{c.quote}”</span>{/if}
                          </li>
                        {/each}
                      </ul>
                    </div>
                  {/if}
                  {#if ev.engagement_by_chapter?.length}
                    <div>
                      <span class="font-semibold">{$t('beta.engagementByChapter')}:</span>
                      <div class="mt-1 flex flex-wrap gap-1">
                        {#each ev.engagement_by_chapter as ce (ce.chapter)}
                          <span class="badge badge-xs badge-ghost" title={ce.note || ''}>{$t('beta.chapter', { n: ce.chapter })}: {ce.engagement}</span>
                        {/each}
                      </div>
                    </div>
                  {/if}
                </div>
              </details>
            {/each}
          </div>
        </details>
      {/if}
    {:else if !running}
      <div class="bg-base-100 rounded-lg p-4 flex flex-col sm:flex-row sm:items-center gap-3">
        <p class="text-sm text-base-content/60 flex-1">{$t('beta.noReport')}</p>
        {#if onRun}
          <button class="btn btn-primary btn-sm shrink-0" disabled={disabled} on:click={onRun}>
            {#if disabled}<span class="loading loading-spinner loading-xs"></span>{/if}
            {$t('beta.run')}
          </button>
        {/if}
      </div>
    {/if}

    <p class="text-[11px] text-base-content/50 border-t border-base-300 pt-2">{$t('beta.disclaimer')}</p>
  </div>
</div>
