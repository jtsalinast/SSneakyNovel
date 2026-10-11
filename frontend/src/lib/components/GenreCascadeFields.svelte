<script>
  // GenreCascadeFields — round 21, written FROM SCRATCH (per user request) as a
  // self-contained replacement for the legacy inline cascade in Config.svelte.
  // The old fields stay in the file untouched; this block renders BELOW them.
  //
  // Design contract:
  //  - The backend (/api/novel-params) ships ALL parameters UNFILTERED.
  //  - This component filters EVERYTHING client-side through
  //    lib/subgenreTaxonomy.js — the single taxonomy implementation:
  //      * Subgenre list  -> subgenresForKeys() (one canonical parent each)
  //      * Specific Settings / Conflict Scale / Protagonist Type
  //                       -> cascadeForSelection() (subgenre catalog when its
  //                          parent is among the selected genres, otherwise the
  //                          union of the SELECTED genres' buckets only).
  //  - A subgenre or bucket from another genre can NEVER leak into these lists:
  //    there is no "unknown parent → show everything" fallback anywhere.
  //
  // It writes into the SAME story-config object as the legacy fields via
  // `bind:cfg`, so saving behaves identically whichever block the user edits.
  import ComboSelect from './ComboSelect.svelte';
  import { createEventDispatcher } from 'svelte';
  import { GENRES, SUBGENRE_DATA } from '../novelwriterGenres.js';
  import {
    buildSubgenreIndex, subgenresForKeys, resolveParent, norm, cascadeForSelection,
    splitSubgenreText, subgenreBelongsToAny,
  } from '../subgenreTaxonomy.js';

  const dispatch = createEventDispatcher();

  export let paramsPayload = null;   // raw, unfiltered /api/novel-params blob
  export let genreKeys = [];         // selected frontend GENRES keys (legacy block)
  export let cfg;                    // localStoryCfg (bound by the parent)
  export let disabled = false;
  export let otherLabel = 'Other';
  export let placeholderGenre = '';
  export let placeholderSub = '';
  export let placeholderConflict = '';
  export let placeholderProtag = '';
  export let emptyLabel = 'Auto';
  export let clearLabel = 'Clear';
  export let labels = {};            // localized field labels from i18n
  export let hints = {};             // token -> localized subgenre hint

  // —— Genre combobox (self-contained; no dependency on the legacy block) ——
  // Options come from the STATIC GENRES table, so they render immediately even
  // before /api/novel-params arrives. Multi-select chips, exactly like the
  // legacy Genre field; free text becomes "Other".
  $: genreOpts = GENRES.map((g) => ({ value: g.label, label: g.label, desc: '' }));
  // Reactive local selection keys, synced from cfg.type (so editing either
  // block keeps both in sync — same storage format).
  $: myGenreKeys = (() => {
    const p = String(cfg.type || '').split(',').map((x) => x.trim()).filter(Boolean);
    const keys = [];
    for (const part of p) {
      const g = GENRES.find(
        (x) => x.label.toLowerCase() === part.toLowerCase() || x.key.toLowerCase() === part.toLowerCase(),
      );
      if (g) keys.push(g.key); else keys.push('__other__');
    }
    return keys.length ? keys : (genreKeys || []);
  })();
  // Effective keys driving every filter below: this block's own selection when
  // present, otherwise whatever the parent already knows (legacy block).
  $: effectiveKeys = String(cfg.type || '').trim() ? myGenreKeys : (genreKeys || []);

  function labelToKey(label) {
    return GENRES.find(
      (x) => x.label.toLowerCase() === label.toLowerCase() || x.key.toLowerCase() === label.toLowerCase(),
    )?.key || '__other__';
  }

  function onGenreChange(e) {
    const v = String(e.detail.value || '');
    const parts = v.split(',').map((x) => x.trim()).filter(Boolean);
    const unknown = parts.filter(
      (p) => !GENRES.some((x) => x.label.toLowerCase() === p.toLowerCase() || x.key.toLowerCase() === p.toLowerCase()),
    );
    cfg.type = [...parts.filter((p) => !unknown.includes(p)), ...unknown].join(', ');
    // Point-7 behaviour (round 22, multi-subgenre): keep ONLY the chips that
    // still belong to the new genre union; drop the ones that don't. If every
    // chip is orphaned the field resets to Auto — same semantics as before,
    // but per-chip instead of all-or-nothing.
    const keys = parts.map(labelToKey);
    const union = subgenresForKeys(index, keys);
    const kept = splitSubgenreText(cfg.subgenre).filter(
      (c) => union.some((s) => s.toLowerCase() === c.toLowerCase()) || subgenreBelongsToAny(index, c, keys),
    );
    if (kept.join(', ') !== splitSubgenreText(cfg.subgenre).join(', ')) {
      cfg.subgenre = kept.join(', ');
    }
    dispatch('genreChange', { keys, value: cfg.type });
  }

  // —— Subgenre combobox (MULTI-SELECT, round 22) ————————————————————————
  // Same interaction as the Genre field: chips + free text. The stored format
  // is a comma-separated list of labels ("Cozy Mystery, Noir"), identical to
  // how Type stores multiple genres and to what the backend now understands
  // (config.SplitSubgenreList). Options are filtered by EVERY selected genre's
  // union; already-picked chips stay in the option list even if a later genre
  // change would no longer offer them (they get dropped from the VALUE only
  // when none of them belongs to the new selection anymore — see onGenreChange).
  $: index = buildSubgenreIndex(paramsPayload);
  $: filteredLabels = subgenresForKeys(index, effectiveKeys);
  $: subOpts = (() => {
    const opts = filteredLabels.map((s) => ({
      value: s, label: s, desc: hints[norm(s)] || hints[String(s).toLowerCase()] || '',
    }));
    for (const cur of splitSubgenreText(cfg.subgenre)) {
      if (!opts.some((o) => o.value.toLowerCase() === cur.toLowerCase())) {
        opts.push({ value: cur, label: cur, desc: hints[norm(cur)] || '' });
      }
    }
    return opts;
  })();

  // —— Dependent cascade fields (Specific Settings / Conflict / Protagonist) —
  // Passes the FULL multi-select text: cascadeForSelection resolves each chip
  // and unions their own catalogs when every chip belongs to the selection.
  $: cascade = cascadeForSelection(
    paramsPayload,
    { conflicts: {}, protagonists: {}, settings: {}, subgenreData: SUBGENRE_DATA },
    cfg.subgenre,
    effectiveKeys,
  );
  $: conflictOpts = (cascade.conflicts || []).map((c) => ({ value: c, label: c, desc: '' }));
  $: protagOpts = (cascade.protagonists || []).map((p) => ({ value: p, label: p, desc: '' }));
  $: settingOpts = cascade.settings || [];

  // Checked-state helpers over cfg.specific_settings (newline-separated tokens,
  // exactly like the legacy block — same storage format, so both stay in sync).
  function currentList() {
    return String(cfg.specific_settings || '').split('\n').map((x) => x.trim()).filter(Boolean);
  }
  function checked(s) { return currentList().includes(s); }
  function toggle(s) {
    const cur = currentList();
    const i = cur.indexOf(s);
    if (i >= 0) cur.splice(i, 1); else cur.push(s);
    cfg.specific_settings = cur.join('\n');
    dispatch('settingsChange', { value: cfg.specific_settings });
  }

  function onSubChange(e) {
    // Multi-select: normalize the joined chip list ("Cozy Mystery, Noir").
    const chips = splitSubgenreText(String(e.detail.value || ''));
    cfg.subgenre = chips.join(', ');
    const first = chips[0] || '';
    dispatch('subChange', { value: cfg.subgenre, chips, parent: resolveParent(index, first) });
  }
  function onConflictChange(e) { cfg.conflict_scale = String(e.detail.value || '').trim(); }
  function onProtagChange(e) { cfg.protagonist_type = String(e.detail.value || '').trim(); }
</script>

<div class="rounded-md border border-primary/35 bg-base-100/40 p-3 space-y-3">
  <div class="text-xs font-semibold uppercase tracking-wide text-primary/80">
    {labels.sectionTitle || 'Genre-filtered parameters (new)'}
  </div>

  <div class="grid grid-cols-2 gap-3">
    <div>
      <span class="text-xs text-base-content/65 mb-0.5 block">{labels.genre || 'Genre'}</span>
      <ComboSelect
        options={genreOpts}
        multiple={true}
        allowOther={true}
        otherLabel={otherLabel}
        placeholder={placeholderGenre}
        emptyLabel={emptyLabel}
        clearLabel={clearLabel}
        disabled={disabled}
        bind:value={cfg.type}
        on:change={onGenreChange}
      />
    </div>
    <div>
      <span class="text-xs text-base-content/65 mb-0.5 block">{labels.subgenre || 'Subgenre'}</span>
      <ComboSelect
        options={subOpts}
        multiple={true}
        allowOther={true}
        otherLabel={otherLabel}
        placeholder={placeholderSub}
        emptyLabel={emptyLabel}
        clearLabel={clearLabel}
        disabled={disabled}
        bind:value={cfg.subgenre}
        on:change={onSubChange}
      />
    </div>
    <div class="rounded border border-base-content/10 px-2 py-1.5 text-[11px] leading-relaxed opacity-70">
      {#each effectiveKeys.filter(k => k !== '__other__') as k}
        <div>{#if cascade.source === 'subgenre'}{labels.fromSubgenre || 'From subgenre:'} {:else}{labels.fromGenres || 'From genres:'} {/if}
          {k.replaceAll('_', ' ')}</div>
      {/each}
      {#if !effectiveKeys.filter(k => k !== '__other__').length}
        <div>{labels.pickGenre || 'Pick a genre to filter these lists.'}</div>
      {/if}
    </div>

    <div>
      <span class="text-xs text-base-content/65 mb-0.5 flex items-center gap-1">
        {labels.conflict || 'Conflict scale'}
        <a class="badge badge-ghost badge-sm font-mono normal-case pointer-events-none">{conflictOpts.length}</a>
      </span>
      <ComboSelect
        options={conflictOpts}
        allowOther={true}
        otherLabel={otherLabel}
        placeholder={placeholderConflict}
        emptyLabel={emptyLabel}
        clearLabel={clearLabel}
        disabled={disabled}
        bind:value={cfg.conflict_scale}
        on:change={onConflictChange}
      />
    </div>
    <div>
      <span class="text-xs text-base-content/65 mb-0.5 flex items-center gap-1">
        {labels.protagonist || 'Protagonist type'}
        <a class="badge badge-ghost badge-sm font-mono normal-case pointer-events-none">{protagOpts.length}</a>
      </span>
      <ComboSelect
        options={protagOpts}
        allowOther={true}
        otherLabel={otherLabel}
        placeholder={placeholderProtag}
        emptyLabel={emptyLabel}
        clearLabel={clearLabel}
        disabled={disabled}
        bind:value={cfg.protagonist_type}
        on:change={onProtagChange}
      />
    </div>
  </div>

  <div>
    <span class="text-xs text-base-content/65 mb-0.5 flex items-center gap-1">
      {labels.settings || 'Specific settings'}
      <a class="badge badge-ghost badge-sm font-mono normal-case pointer-events-none">{settingOpts.length}</a>
    </span>
    {#if settingOpts.length}
      <div class="grid grid-cols-2 gap-x-3 gap-y-1">
        {#each settingOpts as s}
          <label class="flex items-center gap-1.5 text-xs cursor-pointer font-mono">
            <input type="checkbox" class="checkbox checkbox-xs" checked={checked(s)} on:change={() => toggle(s)} disabled={disabled} />
            {s.replaceAll('_', ' ')}
          </label>
        {/each}
      </div>
    {:else}
      <p class="text-xs text-base-content/45">{labels.pickGenre || 'Pick a genre to populate these checkboxes.'}</p>
    {/if}
  </div>
</div>
