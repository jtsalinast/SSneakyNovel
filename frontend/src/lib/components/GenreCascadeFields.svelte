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

  // —— Modifier presets (round 23) ————————————————————————————————————————
  // Cross-cutting subgenre modifiers from /api/novel-params -> modifier_presets
  // ({modifier: [setting tokens]}). Stackable on top of ANY genre/subgenre
  // selection; checking one appends its settings (deduped), unchecking removes
  // exactly those lines again without touching custom entries. Same storage
  // format as the legacy block's modifier section.
  $: modifierMap = (paramsPayload && paramsPayload.modifier_presets) || {};
  $: modifierGroups = Object.keys(modifierMap).sort((a, b) => a.localeCompare(b))
    .map((name) => ({ name, settings: modifierMap[name] || [] }))
    .filter((g) => g.settings.length);
  function modifierChecked(name) {
    const list = currentList();
    const s = modifierMap[name] || [];
    return s.length > 0 && s.every((x) => list.includes(x));
  }
  function toggleModifier(name) {
    const s = modifierMap[name] || [];
    if (!s.length) return;
    const cur = currentList();
    if (modifierChecked(name)) {
      cfg.specific_settings = cur.filter((x) => !s.includes(x)).join('\n');
    } else {
      cfg.specific_settings = [...new Set([...cur, ...s])].join('\n');
    }
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

  // —— Free-text specific settings ————————————————————————————————————————
  // The textarea holds ONLY the entries that are not part of the current
  // filtered suggestion list (same semantics as the legacy block's custom box).
  // It is re-derived whenever cfg.specific_settings changes from either block;
  // while the user is mid-edit we don't clobber the draft.
  let freeSettingsText = '';
  let freeSettingsDirty = false;
  $: if (!freeSettingsDirty) {
    const known = new Set(settingOpts);
    freeSettingsText = currentList().filter((x) => !known.has(x)).join('\n');
  }
  function applyFreeSettings() {
    freeSettingsDirty = false;
    const known = new Set(settingOpts);
    const checkedNow = currentList().filter((x) => known.has(x));
    const custom = freeSettingsText.split('\n').map((x) => x.trim()).filter(Boolean);
    cfg.specific_settings = [...new Set([...checkedNow, ...custom])].join('\n');
    dispatch('settingsChange', { value: cfg.specific_settings });
  }

  // —— Tone suggestions ——————————————————————————————————————————————————
  // Free-text tone field + suggestion chips from the resolved subgenre catalog
  // (backend `subgenre_catalogs.tones` / static SUBGENRE_DATA). Same storage
  // format as the legacy block: comma-separated labels in cfg.tone.
  $: toneOpts = cascade.tones || [];
  function toneList(v) {
    return String(v || '').split(',').map((x) => x.trim()).filter(Boolean);
  }
  function toneChecked(t) { return toneList(cfg.tone).includes(t); }
  function toggleTone(t) {
    const cur = toneList(cfg.tone);
    const i = cur.indexOf(t);
    if (i >= 0) cur.splice(i, 1); else cur.push(t);
    cfg.tone = cur.join(', ');
  }

  // —— Story length combobox (round 23) ————————————————————————————————
  // Options come from the backend payload (`length_keys`) with a static
  // fallback; picking a length applies its words-per-chapter preset
  // (`target_words_by_length`), same behaviour as the legacy field.
  const STATIC_LENGTH_KEYS = ['flash', 'short', 'novelette', 'novella', 'light_novel', 'anthology', 'novel', 'epic'];
  const STATIC_TARGET_WORDS = { flash: 1000, short: 2000, novelette: 2500, novella: 2500, light_novel: 3500, anthology: 4000, novel: 4000, epic: 5000 };
  $: lengthOpts = (() => {
    const vals = (paramsPayload && Array.isArray(paramsPayload.length_keys) && paramsPayload.length_keys.length)
      ? paramsPayload.length_keys : STATIC_LENGTH_KEYS;
    return vals.map((k) => ({ value: k, label: String(k).replaceAll('_', ' '), desc: '' }));
  })();
  function onLengthChange(e) {
    const v = String(e.detail.value || '').trim();
    cfg.story_length = v;
    const tw = (paramsPayload && paramsPayload.target_words_by_length) || STATIC_TARGET_WORDS;
    if (tw[v]) cfg.target_words_per_chapter = tw[v];
    dispatch('lengthChange', { value: v });
  }

  // —— Narrative structure combobox (round 23) ——————————————————————————
  // Single-select over the backend `all_structure_keys` payload (static
  // fallback), storing the same plain key as the legacy field (e.g. "three_act").
  const STATIC_STRUCTURE_KEYS = ['three_act', 'six_act', 'fichtean', 'freytag', 'seven_point', 'heros_journey', 'heros_journey_simple', 'save_the_cat', 'story_circle', 'kishotenketsu', 'snowflake', 'quest', 'romance_arc', 'episodic'];
  $: structureOpts = (() => {
    const vals = (paramsPayload && Array.isArray(paramsPayload.all_structure_keys) && paramsPayload.all_structure_keys.length)
      ? paramsPayload.all_structure_keys : STATIC_STRUCTURE_KEYS;
    return vals.map((k) => ({ value: k, label: String(k).replaceAll('_', ' '), desc: '' }));
  })();
  function onStructureChange(e) {
    cfg.structure = String(e.detail.value || '').trim();
    dispatch('structureChange', { value: cfg.structure });
  }
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
    {#if modifierGroups.length}
      <details class="mt-1.5" open>
        <summary class="text-xs text-base-content/65 cursor-pointer select-none">{labels.modifiers || 'Modifiers'}</summary>
        <div class="grid grid-cols-3 gap-x-3 gap-y-1 mt-1">
          {#each modifierGroups as grp}
            <label class="flex items-center gap-1.5 text-xs cursor-pointer" title={grp.settings.map((x) => x.replaceAll('_', ' ')).join(', ')}>
              <input type="checkbox" class="checkbox checkbox-xs" checked={modifierChecked(grp.name)} on:change={() => toggleModifier(grp.name)} disabled={disabled} />
              {grp.name}
            </label>
          {/each}
        </div>
      </details>
    {/if}
  </div>

  <!-- Free-text specific settings (newline-separated tokens), kept in the SAME
       storage field as the checkboxes so both stay in sync. -->
  <textarea
    class="textarea textarea-sm w-full h-12 text-xs font-mono"
    bind:value={freeSettingsText}
    on:blur={applyFreeSettings}
    on:input={() => { freeSettingsDirty = true; }}
    placeholder={labels.settingsPlaceholder || 'custom_setting_per_line'}
    disabled={disabled}
  ></textarea>

  <!-- Tone: free text + per-subgenre suggestion chips (from the resolved
       catalog). Same comma-separated storage as the legacy field. -->
  <div>
    <span class="text-xs text-base-content/65 mb-0.5 block">{labels.tone || 'Tone'}</span>
    <input type="text" class="input input-sm w-full mb-1" bind:value={cfg.tone} placeholder={labels.tonePlaceholder || ''} disabled={disabled} />
    {#if toneOpts.length}
      <div class="flex flex-wrap gap-x-3 gap-y-1">
        {#each toneOpts as t}
          <label class="flex items-center gap-1.5 text-xs cursor-pointer">
            <input type="checkbox" class="checkbox checkbox-xs" checked={toneChecked(t)} on:change={() => toggleTone(t)} disabled={disabled} />
            {t}
          </label>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Length & structure: same comboboxes as the legacy fields, populated from
       the backend payload. Picking a length applies its word-per-chapter
       preset and a default structure when the current one is empty/auto. -->
  <div class="grid grid-cols-2 gap-3">
    <div>
      <span class="text-xs text-base-content/65 mb-0.5 block">{labels.length || 'Story length'}</span>
      <ComboSelect
        options={lengthOpts}
        allowOther={false}
        placeholder={labels.lengthNone || '—'}
        emptyLabel={labels.lengthNone || '—'}
        clearLabel={clearLabel}
        disabled={disabled}
        bind:value={cfg.story_length}
        on:change={onLengthChange}
      />
    </div>
    <div>
      <span class="text-xs text-base-content/65 mb-0.5 block">{labels.structure || 'Narrative structure'}</span>
      <ComboSelect
        options={structureOpts}
        allowOther={true}
        otherLabel={otherLabel}
        placeholder={labels.structurePlaceholder || ''}
        emptyLabel={emptyLabel}
        clearLabel={clearLabel}
        disabled={disabled}
        bind:value={cfg.structure}
        on:change={onStructureChange}
      />
    </div>
  </div>
</div>
