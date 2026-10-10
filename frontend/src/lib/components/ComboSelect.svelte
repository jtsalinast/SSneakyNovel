<script>
  // ComboSelect — accessible combobox (WAI-ARIA pattern) used by the Novel
  // parameters form for Genre / Subgenre / Story length / Story structure /
  // Conflict scale / Protagonist type.
  //
  // Features:
  //  - Text input with live filtering over `options` (edit-in-place).
  //  - Rich item list: bold primary label + secondary description line.
  //  - Single-select (default) or multi-select chips (`multiple` prop).
  //  - "Other": any typed text that doesn't match an option is offered as a
  //    free-text value and committed on Enter/blur; suggestions keep showing
  //    what the list has while typing (search-like behavior).
  //  - Info panel below the field describing the highlighted/selected term
  //    (fed from options[].desc).
  //
  // API:
  //  bind:value   string  current value ('' = auto/unset). In multi-select it
  //                       is the selected items joined by `sep`.
  //  options      [{value,label,desc}]  preprocessed by the caller (labels
  //                       already localized where needed).
  //  multiple     boolean chip-style multi select.
  //  sep          string  join separator when multiple (default ', ').
  //  allowOther   boolean accept free-text values not in `options`.
  //  otherLabel   string  label for the "Other" row (localized).
  //  placeholder, disabled, title, emptyLabel ('Auto'), clearLabel.
  //  on:change    native-like CustomEvent dispatched whenever the value
  //               changes (selection, chip removal, blur-commit, clear), so
  //               callers can keep their existing on:change handlers.
  import { tick } from 'svelte';
  import InfoTip from './InfoTip.svelte';

  export let value = '';
  export let options = [];
  export let multiple = false;
  export let sep = ', ';
  export let allowOther = true;
  export let otherLabel = 'Other';
  export let placeholder = '';
  export let disabled = false;
  export let title = '';
  export let emptyLabel = 'Auto';
  export let clearLabel = 'Clear';
  export let ariaLabel = '';
  // Explanation shown by the clickable ⓘ icon when nothing is selected/high-
  // lighted (what this field is and how to use it).
  export let fieldTip = '';
  // Short note shown by the ⓘ icon while a free-text "Other" value is active.
  export let otherTip = 'Custom “Other” value: not in the catalog — saved as-is and used verbatim by the generator. Click the text to edit; suggestions from the list appear while typing.';

  let root, input, listbox, editInput;
  let openMenu = false;
  let query = '';
  let activeIdx = -1;

  // Local search query for the inline "Other" editor (separate from `query`,
  // which drives the dropdown of the main combobox).
  let editQuery = '';
  $: editSugg = (() => {
    if (!editOpen) return [];
    const q = editQuery.trim().toLowerCase();
    if (!q) return [];
    return options.filter(
      (o) =>
        displayOf(o).toLowerCase().includes(q) ||
        o.value.toLowerCase().includes(q) ||
        (o.desc || '').toLowerCase().includes(q)
    ).slice(0, 8);
  })();

  $: selected = multiple
    ? String(value || '').split(sep).map((s) => s.trim()).filter(Boolean)
    : (value ? [String(value)] : []);

  function optByValue(v) {
    return options.find((o) => o.value === v) || null;
  }
  function displayOf(o) { return o.label || o.value; }
  function labelFor(v) {
    const o = optByValue(v);
    return o ? displayOf(o) : v;
  }

  // Dropdown rows: filtered by the typed query (case-insensitive substring
  // match on label / value / description).
  $: filtered = (() => {
    const q = query.trim().toLowerCase();
    if (!q) return options;
    return options.filter(
      (o) =>
        displayOf(o).toLowerCase().includes(q) ||
        (o.desc || '').toLowerCase().includes(q) ||
        o.value.toLowerCase().includes(q)
    );
  })();

  // Rows = optional "Auto" row + filtered options + optional "Other" row.
  $: rows = (() => {
    const r = [];
    if (!multiple) r.push({ kind: 'auto', value: '', label: emptyLabel, desc: '' });
    for (const o of filtered) r.push({ kind: 'opt', value: o.value, label: displayOf(o), desc: o.desc || '' });
    const t = query.trim();
    const exact = t && options.some((o) => o.value.toLowerCase() === t.toLowerCase() || displayOf(o).toLowerCase() === t.toLowerCase());
    if (allowOther && t && !exact) {
      r.push({ kind: 'other', value: t, label: t, desc: otherLabel });
    }
    return r;
  })();

  $: activeRow = activeIdx >= 0 && activeIdx < rows.length ? rows[activeIdx] : null;

  // True when `v` matches an option exactly (value or label, case-insensitive).
  function isKnown(v) {
    const s = String(v || '').trim().toLowerCase();
    if (!s) return false;
    return options.some((o) => o.value.toLowerCase() === s || displayOf(o).toLowerCase() === s);
  }

  // Info panel: description of the highlighted row, else of the current
  // selection (free-text values get no description).
  $: infoText = (() => {
    if (activeRow && (activeRow.kind === 'opt' || activeRow.kind === 'other')) return activeRow.desc || '';
    const cur = optByValue(multiple ? selected[selected.length - 1] : value);
    return cur ? cur.desc || '' : '';
  })();

  // Field-level explanation shown by the clickable ⓘ icon next to the input:
  // description of the highlighted/selected item, else the generic field tip.
  $: tipText = (() => {
    if (infoText) return infoText;
    const cur = multiple ? selected[selected.length - 1] : value;
    if (cur && !isKnown(cur)) return otherTip;
    return fieldTip || '';
  })();

  function dispatchChange() {
    if (root) root.dispatchEvent(new CustomEvent('change', { detail: { value }, bubbles: true }));
  }

  function syncInputText() {
    if (input) input.value = multiple ? '' : (value ? labelFor(value) : '');
  }

  function open() {
    if (disabled || openMenu) return;
    openMenu = true;
    activeIdx = -1;
    tick().then(() => input && input.focus());
  }
  function close() {
    openMenu = false;
    activeIdx = -1;
    query = '';
    syncInputText();
  }
  function toggleOpen(e) {
    if (e) { e.preventDefault(); e.stopPropagation(); }
    if (openMenu) close();
    else open();
  }

  function pick(row) {
    if (!row) return;
    if (row.kind === 'auto') {
      value = '';
    } else if (multiple) {
      const set = [...selected];
      const i = set.indexOf(row.value);
      if (i >= 0) set.splice(i, 1);
      else set.push(row.value);
      value = set.join(sep);
    } else {
      value = row.value;
    }
    query = '';
    syncInputText();
    dispatchChange();
    if (!multiple) close();
    else { activeIdx = -1; tick().then(() => input && input.focus()); }
  }

  // Edit-in-place "Other" (single-select): when the committed value is free
  // text that matches no option, replace the input with an inline editable
  // field pre-loaded with that text. Suggestions from the list keep filtering
  // under it as the user types (search-like behaviour).
  let editOpen = false;
  let editText = '';
  function startEdit() {
    if (disabled || multiple) return;
    editText = String(value || '');
    editQuery = editText;
    openMenu = false;
    editOpen = true;
    tick().then(() => {
      if (editInput) { editInput.focus(); editInput.select(); }
    });
  }
  function onEditInput(e) {
    // The editor text is the value being typed; keep the suggestion search in sync.
    editText = e.target.value;
    editQuery = e.target.value;
  }
  function commitEdit() {
    const t = editText.trim();
    // Canonicalize against the list when possible (same rule as blur-commit).
    const m = options.find((o) => o.value.toLowerCase() === t.toLowerCase() || displayOf(o).toLowerCase() === t.toLowerCase());
    value = m ? m.value : t;
    editOpen = false;
    editQuery = '';
    query = '';
    syncInputText();
    dispatchChange();
  }
  function cancelEdit() {
    editOpen = false;
    editQuery = '';
    syncInputText();
  }
  function pickFromEditor(o) {
    // Choosing a suggestion from the editor replaces the free text with it.
    value = o.value;
    editOpen = false;
    editQuery = '';
    query = '';
    syncInputText();
    dispatchChange();
  }
  // Close the inline editor whenever the stored value becomes a known option
  // or empties out (e.g. cleared externally).
  $: if (editOpen && (!value || isKnown(value))) { editOpen = false; }

  function removeChip(v) {
    if (disabled) return;
    value = selected.filter((x) => x !== v).join(sep);
    dispatchChange();
  }

  function clearAll() {
    if (disabled) return;
    value = '';
    query = '';
    syncInputText();
    dispatchChange();
  }

  function onKeydown(e) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (!openMenu) { openMenu = true; return; }
      activeIdx = Math.min(activeIdx + 1, rows.length - 1);
      scrollActive();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      if (!openMenu) { openMenu = true; return; }
      activeIdx = Math.max(activeIdx - 1, 0);
      scrollActive();
    } else if (e.key === 'Enter') {
      if (openMenu && activeRow) { e.preventDefault(); pick(activeRow); }
      else if (allowOther && query.trim()) {
        e.preventDefault();
        pick({ kind: 'other', value: query.trim(), label: query.trim(), desc: '' });
      }
    } else if (e.key === 'Escape') {
      if (openMenu) { e.stopPropagation(); close(); }
    } else if (e.key === 'Backspace' && !query && !multiple && value) {
      // Backspace on an empty query clears the single selection (tag-input feel).
      value = '';
      query = '';
      syncInputText();
      dispatchChange();
    }
  }

  function scrollActive() {
    tick().then(() => {
      const el = listbox && listbox.querySelector('[data-active="true"]');
      if (el && listbox) el.scrollIntoView({ block: 'nearest' });
    });
  }

  function onInput() {
    if (!openMenu) openMenu = true;
    activeIdx = -1;
  }

  // Commit typed free text when leaving the field (the edit-in-place part of
  // "Other"): canonicalize against the option list when possible; otherwise
  // keep the raw text if allowOther, else revert to the stored value.
  function onBlurCommit() {
    if (!openMenu) { syncInputText(); return; }
    const t = query.trim();
    if (!t) { close(); return; }
    const m = options.find((o) => o.value.toLowerCase() === t.toLowerCase() || displayOf(o).toLowerCase() === t.toLowerCase());
    if (m) {
      value = multiple ? [...new Set([...selected, m.value])].join(sep) : m.value;
    } else if (allowOther) {
      value = multiple ? [...new Set([...selected, t])].join(sep) : t;
    }
    close();
    dispatchChange();
  }

  // Keep the visible text in sync when `value` changes externally (load/save/
  // randomize/reset) — but never while the menu is open (that would clobber
  // the user's in-progress search text).
  $: if (!openMenu) { void value; syncInputText(); }

  function onDocClick(e) {
    if (openMenu && root && !root.contains(e.target)) close();
  }
</script>

<svelte:window on:mousedown={onDocClick} />

<div bind:this={root} class="combo-root relative w-full" {title}>
  <div class="flex items-stretch gap-1">
    <div class="relative flex-1 min-w-0">
      {#if multiple && selected.length}
        <div class="flex flex-wrap gap-1 mb-1">
          {#each selected as v (v)}
            <span class="badge badge-sm badge-outline gap-1 font-normal max-w-full" title={labelFor(v)}>
              <span class="truncate">{labelFor(v)}</span>
              <button type="button" class="hover:text-error leading-none" aria-label="remove" on:click={() => removeChip(v)} {disabled}>×</button>
            </span>
          {/each}
        </div>
      {/if}
      {#if editOpen}
        <!-- Edit-in-place "Other": the free-text value becomes an editable
             field; list suggestions filter under it as you search. -->
        <input
          bind:this={editInput}
          type="text"
          class="input input-sm w-full pr-7 border-primary/60"
          autocomplete="off"
          spellcheck="false"
          value={editText}
          disabled={disabled}
          on:input={onEditInput}
          on:keydown={(e) => {
            if (e.key === 'Enter') { e.preventDefault(); commitEdit(); }
            else if (e.key === 'Escape') { e.preventDefault(); cancelEdit(); }
          }}
          on:blur={() => setTimeout(() => { if (editOpen) commitEdit(); }, 120)}
        />
        <button type="button" class="absolute right-1 top-1 btn btn-ghost btn-xs h-6 min-h-0 px-1" tabindex="-1" aria-label="edit other value" title="✎">✓</button>
        {#if editSugg.length}
          <ul role="listbox" class="absolute z-30 mt-1 w-full max-h-48 overflow-y-auto rounded-md border border-base-content/20 bg-base-100 shadow-lg py-1">
            {#each editSugg as o (o.value)}
              <li
                role="option"
                aria-selected={false}
                class="px-3 py-1.5 cursor-pointer text-left hover:bg-base-200"
                on:mousedown|preventDefault={() => pickFromEditor(o)}
              >
                <span class="block text-sm leading-tight truncate font-semibold">{displayOf(o)}</span>
                {#if o.desc}
                  <span class="block text-[11px] leading-snug opacity-60 line-clamp-2">{o.desc}</span>
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
      {:else}
        <input
          bind:this={input}
          type="text"
          role="combobox"
          aria-expanded={openMenu}
          aria-controls="combo-list"
          aria-autocomplete="list"
          aria-activedescendant={openMenu && activeRow ? 'combo-opt-' + activeIdx : undefined}
          class="input input-sm w-full pr-7"
          class:opacity-50={disabled}
          placeholder={(multiple && selected.length) ? '' : placeholder}
          {disabled}
          autocomplete="off"
          on:focus={open}
          on:input={onInput}
          on:keydown={onKeydown}
          on:blur={() => setTimeout(onBlurCommit, 120)}
          on:dblclick={startEdit}
        />
        <button type="button" class="absolute right-1 top-1 btn btn-ghost btn-xs h-6 min-h-0 px-1" tabindex="-1" on:mousedown={toggleOpen} {disabled} aria-label="toggle list">▾</button>
        {#if !multiple && value && !isKnown(value)}
          <!-- Free-text ("Other") value committed: pencil opens the inline editor. -->
          <button
            type="button"
            class="absolute right-7 top-1 btn btn-ghost btn-xs h-6 min-h-0 w-6 px-1 opacity-70 hover:opacity-100"
            tabindex="-1"
            on:mousedown={(e) => { e.preventDefault(); e.stopPropagation(); startEdit(); }}
            {disabled}
            aria-label="edit custom value"
            title="✎"
          >✎</button>
        {/if}
      {/if}
    </div>
    {#if value}
      <button type="button" class="btn btn-ghost btn-xs h-8 min-h-0 px-1.5 opacity-60 hover:opacity-100 self-center" on:click={clearAll} {disabled} title={clearLabel}>✕</button>
    {/if}
    <span class="self-center shrink-0"><InfoTip text={tipText} label={ariaLabel || title || 'info'} /></span>
  </div>

  {#if openMenu}
    <ul
      bind:this={listbox}
      id="combo-list"
      role="listbox"
      aria-multiselectable={multiple}
      class="absolute z-30 mt-1 w-full max-h-64 overflow-y-auto rounded-md border border-base-content/20 bg-base-100 shadow-lg py-1"
    >
      {#if rows.length === 0}
        <li class="px-3 py-1.5 text-xs opacity-50">—</li>
      {/if}
      {#each rows as row, i (row.kind + ':' + row.value)}
        <li
          id="combo-opt-{i}"
          role="option"
          data-active={i === activeIdx}
          aria-selected={selected.includes(row.value)}
          class="px-3 py-1.5 cursor-pointer text-left {i === activeIdx ? 'bg-base-200' : ''} {selected.includes(row.value) && row.kind !== 'auto' ? 'font-semibold' : ''}"
          on:mousedown|preventDefault={() => pick(row)}
          on:mouseenter={() => (activeIdx = i)}
        >
          <span class="block text-sm leading-tight truncate">{row.kind === 'other' ? `${otherLabel}: ${row.label}` : row.label}</span>
          {#if row.desc}
            <span class="block text-[11px] leading-snug opacity-60 line-clamp-2">{row.desc}</span>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}

  {#if infoText}
    <div class="mt-1 text-[11px] leading-snug opacity-70 border-l-2 border-base-content/20 pl-2 line-clamp-3">{infoText}</div>
  {/if}
</div>
