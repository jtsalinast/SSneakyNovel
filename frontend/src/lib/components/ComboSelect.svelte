<script>
  // ComboSelect — accessible dropdown selector (WAI-ARIA listbox pattern) used
  // by the Novel parameters form for Genre / Subgenre / Story length / Story
  // structure / Conflict scale / Protagonist type.
  //
  // Features (round 28 redesign):
  //  - NO edit-in-place: the trigger is a read-only button, never a text field.
  //    Selection happens exclusively through the dropdown list.
  //  - Rich item list: bold primary label + secondary description line, live
  //    keyboard navigation (arrows / Enter / Escape).
  //  - Single-select (default) or multi-select chips (`multiple` prop).
  //  - "Other" row at the bottom of every list (when `allowOther`). Pressing it
  //    opens a dedicated search input where ALL catalog options are searchable
  //    (unfiltered list that narrows down as you type, like a typical search
  //    field). If nothing matches what you typed, the raw text can still be
  //    accepted as a custom value. ✓ accepts, ✕ closes without changes.
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
  //  allowOther   boolean show the "Other" search row (free-text values).
  //  otherLabel   string  label for the "Other" row (localized).
  //  placeholder, disabled, title, emptyLabel ('Auto'), clearLabel.
  //  on:change    native-like CustomEvent dispatched whenever the value
  //               changes (selection, chip removal, Other-accept, clear), so
  //               callers can keep their existing on:change handlers.
  import { tick } from 'svelte';
  import InfoTip from './InfoTip.svelte';
  import { uiLocale } from '../i18n/index.js';

  // Localized fallbacks for the "Other" search editor chrome (accept / close /
  // search placeholder). Callers can still override via props. Reactive on
  // uiLocale so switching the UI language updates the buttons live.
  const L10N = {
    en: { accept: 'Accept', close: 'Close', search: 'Search all options…' },
    es: { accept: 'Aceptar', close: 'Cerrar', search: 'Buscar en todas las opciones…' },
    zh: { accept: '接受', close: '关闭', search: '搜索全部选项…' },
  };
  $: l = L10N[$uiLocale] || L10N.en;

  export let value = '';
  export let options = [];
  export let multiple = false;
  export let sep = ', ';
  export let allowOther = true;
  export let otherLabel = 'Other';
  // Optional: full option list used ONLY by the "Other" search editor, when it
  // must differ from the dropdown's (filtered) `options`. E.g. the Subgenre
  // field passes every subgenre of the catalog here so its search always shows
  // the complete unfiltered list regardless of the selected Genre, while the
  // normal dropdown keeps showing only the genre-filtered options.
  export let otherSearchOptions = null;
  export let placeholder = '';
  export let disabled = false;
  export let title = '';
  export let emptyLabel = 'Auto';
  export let clearLabel = 'Clear';
  export let acceptLabel = '';
  export let closeLabel = '';
  export let searchPlaceholder = '';
  export let ariaLabel = '';
  // Explanation shown by the clickable ⓘ icon when nothing is selected/high-
  // lighted (what this field is and how to use it).
  export let fieldTip = '';
  // Short note shown by the ⓘ icon while a free-text "Other" value is active.
  export let otherTip = 'Custom “Other” value: not in the catalog — saved as-is and used verbatim by the generator. Use the “Other” option in the dropdown to search the full list or type a new custom value.';

  let root, trigger, listbox, searchInput;
  let openMenu = false;
  let activeIdx = -1;

  // —— "Other" search editor ————————————————————————————————————————————————
  // When true, the dropdown shows a search input instead of the normal rows.
  // The search covers ALL `options` with no genre/subgenre pre-filtering; it
  // narrows down as the user types and always offers the raw text as a
  // custom value when it matches nothing (or even when it does — accepting a
  // match canonicalizes to that option).
  let editOpen = false;
  let editText = '';
  // The normal dropdown list is visible while the menu is open and this
  // combo's "Other" search editor is closed (the editor overlays the field).
  $: fieldVisible = openMenu && !editOpen;
  // Editor geometry: where the trigger button sits relative to the root, so
  // the search panel can be rendered exactly on top of it (covering it) while
  // staying inside the flow for layout purposes.
  let triggerTop = 0;
  let triggerHeight = 34;
  // Width available next to the side controls (clear ✕ + ⓘ). The editor row
  // uses exactly this width so the search input is the SAME size as the select
  // it covers — otherwise the smaller field would leave a strip of the
  // original combobox visible underneath.
  let editorWidth = 0;
  // Rows shown by the editor's result list: while the query matches at least
  // one catalog option we show only those matches; when NOTHING matches we
  // still render the typed text as a selectable custom row, so the user can
  // accept a fully customized value straight from the list (✓ / Enter do the
  // same). The list never goes blank-looking on a no-match query.
  $: editRows = (() => {
    if (!editOpen) return [];
    // Search source: the FULL unfiltered list when provided (otherSearchOptions),
    // otherwise the combo's own options.
    const pool = Array.isArray(otherSearchOptions) && otherSearchOptions.length
      ? otherSearchOptions : options;
    const q = editText.trim().toLowerCase();
    const matches = !q
      ? pool
      : pool.filter(
          (o) =>
            displayOf(o).toLowerCase().includes(q) ||
            o.value.toLowerCase().includes(q) ||
            (o.desc || '').toLowerCase().includes(q)
        );
    if (matches.length) return matches.map((o) => ({ kind: 'opt', opt: o }));
    const t = editText.trim();
    return t ? [{ kind: 'custom', opt: { value: t, label: t, desc: '' } }] : [];
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

  // Dropdown rows: the full option list (no typing filter anymore — the list
  // itself is browsable; searching lives in the "Other" editor) + optional
  // "Auto" row on top + optional "Other" row at the bottom.
  $: rows = (() => {
    const r = [];
    if (!multiple) r.push({ kind: 'auto', value: '', label: emptyLabel, desc: '' });
    for (const o of options) r.push({ kind: 'opt', value: o.value, label: displayOf(o), desc: o.desc || '' });
    if (allowOther) r.push({ kind: 'other', value: '__other__', label: otherLabel, desc: '' });
    return r;
  })();

  $: activeRow = activeIdx >= 0 && activeIdx < rows.length ? rows[activeIdx] : null;

  // True when `v` matches an option exactly (value or label, case-insensitive).
  function isKnown(v) {
    const s = String(v || '').trim().toLowerCase();
    if (!s) return false;
    return options.some((o) => o.value.toLowerCase() === s || displayOf(o).toLowerCase() === s);
  }

  // Field-level explanation shown by the clickable ⓘ icon next to the input:
  //   • nothing selected/highlighted → generic field tip;
  //   • one or more catalog items selected → their title + description joined
  //     (round 25: with several genres/subgenres selected, the popover shows
  //     the description of ALL of them, not just the last one);
  //   • free-text "Other" value → otherTip.
  $: tipText = (() => {
    if (activeRow && activeRow.kind === 'opt' && activeRow.desc) {
      return `${activeRow.label}: ${activeRow.desc}`;
    }
    const parts = [];
    if (multiple) {
      for (const v of selected) {
        const o = optByValue(v);
        if (o && o.desc) parts.push(`${displayOf(o)}: ${o.desc}`);
        else if (!isKnown(v)) parts.push(otherTip);
      }
    } else if (value) {
      const o = optByValue(value);
      if (o && o.desc) parts.push(`${displayOf(o)}: ${o.desc}`);
      else if (!isKnown(value)) parts.push(otherTip);
    }
    if (parts.length) return parts.join('\n\n');
    return fieldTip || '';
  })();

  function dispatchChange() {
    if (root) root.dispatchEvent(new CustomEvent('change', { detail: { value }, bubbles: true }));
  }

  function open() {
    if (disabled || openMenu) return;
    measureTrigger();
    openMenu = true;
    editOpen = false;
    activeIdx = -1;
    tick().then(() => trigger && trigger.focus());
  }
  function close() {
    openMenu = false;
    editOpen = false;
    editText = '';
    activeIdx = -1;
  }
  // Record where the trigger button sits inside the root so the "Other"
  // search panel can be painted exactly over it (covering it) when opened.
  // Also records the flex-1 column width, so the editor row can match the
  // combobox size exactly (same top/height/width → full cover).
  function measureTrigger() {
    const el = trigger || (root ? root.querySelector('[role="combobox"]') : null);
    if (el && root) {
      const r = el.getBoundingClientRect();
      const rr = root.getBoundingClientRect();
      triggerTop = r.top - rr.top;
      triggerHeight = r.height || 34;
      const col = el.parentElement; // the relative flex-1 wrapper around the btn
      editorWidth = col ? col.getBoundingClientRect().width : r.width;
    }
  }
  function toggleOpen(e) {
    if (e) { e.preventDefault(); e.stopPropagation(); }
    if (openMenu) close();
    else open();
  }

  // Open the "Other" search editor. MUST be called synchronously from the
  // row's mousedown (before Svelte flushes and removes the dropdown from the
  // DOM) so that any window-level outside-click handler still sees the click
  // target inside `root` and bails out instead of cancelling the editor.
  function openOtherEditor() {
    measureTrigger();
    editOpen = true;
    editText = (!multiple && value && !isKnown(value)) ? String(value) : '';
    activeIdx = -1;
    tick().then(() => searchInput && searchInput.focus());
  }

  function pick(row, e) {
    if (!row) return;
    if (row.kind === 'other') {
      // Open the dedicated search editor for custom / full-list values. The
      // rows use on:mousedown, which fires BEFORE the window-level mousedown
      // listener (onDocClick) — without stopping propagation here, that
      // listener would see the click as "outside an already-closed menu" and
      // immediately cancel the editor we are opening, so the search box would
      // never appear. Hence: stopPropagation + open synchronously while the
      // dropdown is still in the DOM (so onDocClick's root.contains check
      // bails out harmlessly).
      if (e && typeof e.stopPropagation === 'function') e.stopPropagation();
      openOtherEditor();
      return;
    }
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
    dispatchChange();
    if (!multiple) close();
    else { activeIdx = -1; tick().then(() => trigger && trigger.focus()); }
  }

  // —— "Other" editor actions ——————————————————————————————————————————————
  function afterEditClosed() {
    // Return focus to the trigger button (it was covered by the editor while
    // open) so keyboard flow keeps working.
    tick().then(() => {
      if (!trigger && root) trigger = root.querySelector('[role="combobox"]');
      if (trigger) trigger.focus();
    });
  }
  function commitEdit() {
    const t = editText.trim();
    if (!t) return;
    // Canonicalize against the full list when the text matches an option
    // exactly (value or label, case-insensitive); otherwise keep it as a
    // custom free-text value.
    const m = options.find((o) => o.value.toLowerCase() === t.toLowerCase() || displayOf(o).toLowerCase() === t.toLowerCase());
    const v = m ? m.value : t;
    if (multiple) {
      value = [...new Set([...selected, v])].join(sep);
    } else {
      value = v;
    }
    dispatchChange();
    editOpen = false;
    editText = '';
    if (!multiple) close();
    else afterEditClosed();
  }
  function cancelEdit() {
    editOpen = false;
    editText = '';
    afterEditClosed();
  }
  function pickFromEditor(o) {
    // Choosing a search result replaces the typed text with that option.
    if (multiple) {
      value = [...new Set([...selected, o.value])].join(sep);
      dispatchChange();
      // stay open in multi-select so the user can keep searching.
      editText = '';
      tick().then(() => searchInput && searchInput.focus());
    } else {
      value = o.value;
      editOpen = false;
      editText = '';
      dispatchChange();
      close();
    }
  }

  function removeChip(v) {
    if (disabled) return;
    value = selected.filter((x) => x !== v).join(sep);
    dispatchChange();
  }

  function clearAll() {
    if (disabled) return;
    value = '';
    editOpen = false;
    editText = '';
    dispatchChange();
  }

  function onKeydown(e) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (!openMenu) { openMenu = true; return; }
      if (editOpen) return;
      activeIdx = Math.min(activeIdx + 1, rows.length - 1);
      scrollActive();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      if (!openMenu) { openMenu = true; return; }
      if (editOpen) return;
      activeIdx = Math.max(activeIdx - 1, 0);
      scrollActive();
    } else if (e.key === 'Enter' || e.key === ' ') {
      if (openMenu && !editOpen) {
        e.preventDefault();
        if (document.activeElement === trigger && activeRow) pick(activeRow);
        else toggleOpen();
      } else if (!openMenu) {
        e.preventDefault();
        open();
      }
    } else if (e.key === 'Escape') {
      if (openMenu) {
        e.stopPropagation();
        if (editOpen) cancelEdit(); // hides the editor and brings the combobox back
        else close();
      } else if (editOpen) {
        e.stopPropagation();
        cancelEdit();
      }
    }
  }

  function onSearchKeydown(e) {
    if (e.key === 'Enter') { e.preventDefault(); commitEdit(); }
    else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); cancelEdit(); }
  }

  function scrollActive() {
    tick().then(() => {
      const el = listbox && listbox.querySelector('[data-active="true"]');
      if (el && listbox) el.scrollIntoView({ block: 'nearest' });
    });
  }

  let lastInsideMousedown = 0;
  function onRootMousedown(e) {
    // Remember that a press started inside this combo. Used by the window
    // click handler below: when a dropdown row is pressed, Svelte may remove
    // the list from the DOM before the matching `click` fires, making the
    // event target an outside ancestor — without this guard that click would
    // look like an outside click and instantly close whatever we just opened
    // (e.g. the "Other" search editor).
    if (root && root.contains(e.target)) lastInsideMousedown = Date.now();
  }
  function onDocClick(e) {
    // Outside-click handling: a click anywhere outside this combo's root
    // closes whatever is open — the "Other" search editor (acts like ✕,
    // without saving) or the normal dropdown.
    if (root && root.contains(e.target)) return;
    // Ignore synthetic "outside" clicks whose press actually began inside us.
    if (Date.now() - lastInsideMousedown < 400) return;
    if (editOpen) cancelEdit();
    else if (openMenu) close();
  }


</script>

<svelte:window onclick={onDocClick} />

<div bind:this={root} class="combo-root relative w-full" {title} on:mousedown={onRootMousedown}>
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
      <!-- Read-only trigger button: selection happens only through the
           dropdown (no edit-in-place since round 28). While the "Other" search
           editor is open, the editor panel is painted exactly over this button
           (covering it); both reappear together when the editor closes or a
           value is submitted. -->
      <button
        bind:this={trigger}
        type="button"
        class="btn btn-sm w-full justify-start rounded-md border border-base-content/20 bg-base-100 px-3 font-normal text-left"
        class:opacity-50={disabled}
        role="combobox"
        aria-expanded={fieldVisible}
        aria-haspopup="listbox"
        aria-controls="combo-list"
        disabled={disabled}
        on:click={toggleOpen}
        on:keydown={onKeydown}
      >
        <span class="block truncate {(!multiple && value) || (multiple && selected.length) ? '' : 'opacity-50'}">
          {multiple
            ? (selected.length ? '' : (placeholder || emptyLabel))
            : (value ? labelFor(value) : (placeholder || emptyLabel))}
        </span>
        <span class="ml-auto shrink-0 opacity-60">▾</span>
      </button>
    </div>
    {#if value}
      <button type="button" class="btn btn-ghost btn-xs h-8 min-h-0 px-1.5 opacity-60 hover:opacity-100 self-center" on:click={clearAll} {disabled} title={clearLabel}>✕</button>
    {/if}
    <span class="self-center shrink-0"><InfoTip text={tipText} label={ariaLabel || title || 'info'} /></span>
  </div>

  {#if fieldVisible}
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
          class="px-3 py-1.5 cursor-pointer text-left {i === activeIdx ? 'bg-base-200' : ''} {selected.includes(row.value) && row.kind !== 'auto' ? 'font-semibold' : ''} {row.kind === 'other' ? 'border-t border-base-content/10 mt-1 pt-2' : ''}"
          on:mousedown|preventDefault={(e) => pick(row, e)}
          on:mouseenter={() => (activeIdx = i)}
        >
          {#if row.kind === 'other'}
            <span class="block text-sm leading-tight">＋ {row.label}</span>
          {:else}
            <span class="block text-sm leading-tight truncate">{row.label}</span>
            {#if row.desc}
              <span class="block text-[11px] leading-snug opacity-60 line-clamp-2">{row.desc}</span>
            {/if}
          {/if}
        </li>
      {/each}
    </ul>
  {/if}

  {#if editOpen}
    <!-- "Other" search editor: type to search the FULL option list (no
         category filtering); results narrow as you type. ✓ accepts the typed
         text as a custom value when nothing matches; ✕ closes without saving.

         The panel is painted exactly over the combobox trigger (same top,
         height and width — measured before opening), so the original select
         is fully covered by a field of the SAME size while searching. Its
         min-height equals the trigger's height, guaranteeing full cover even
         when the results list is empty. Clicking outside the panel closes it
         automatically. -->
    <div
      class="absolute z-40 left-0 rounded-md border border-primary/60 bg-base-100 shadow-lg p-2 space-y-1.5"
      style="top: {triggerTop}px; min-height: {triggerHeight}px; {editorWidth ? `width: ${editorWidth}px;` : 'width: calc(100% - 3.75rem);'}"
    >
      <div class="flex items-stretch gap-1">
        <input
          bind:this={searchInput}
          type="text"
          class="input input-sm w-full flex-1"
          autocomplete="off"
          spellcheck="false"
          placeholder={searchPlaceholder || l.search}
          bind:value={editText}
          disabled={disabled}
          on:focus={measureTrigger}
          on:keydown={onSearchKeydown}
        />
        <button
          type="button"
          class="btn btn-success btn-xs min-h-0 h-auto px-2 shrink-0"
          aria-label={acceptLabel || l.accept}
          title={acceptLabel || l.accept}
          on:click={commitEdit}
          disabled={disabled || !editText.trim()}
        >✓</button>
        <button
          type="button"
          class="btn btn-ghost btn-xs min-h-0 h-auto px-2 shrink-0"
          aria-label={closeLabel || l.close}
          title={closeLabel || l.close}
          on:click={cancelEdit}
        >✕</button>
      </div>
      <ul role="listbox" aria-label={otherLabel} class="max-h-48 overflow-y-auto rounded-md border border-base-content/10 py-1">
        {#each editRows as row (row.kind + ':' + row.opt.value)}
          <li
            role="option"
            aria-selected={false}
            class="px-3 py-1.5 cursor-pointer text-left hover:bg-base-200 {row.kind === 'custom' ? 'border-t border-base-content/10' : ''}"
            on:mousedown|preventDefault={() => pickFromEditor(row.opt)}
          >
            {#if row.kind === 'custom'}
              <span class="block text-sm leading-tight truncate font-semibold">＋ {otherLabel}: “{row.opt.label}”</span>
              <span class="block text-[11px] leading-snug opacity-60">{acceptLabel || l.accept}</span>
            {:else}
              <span class="block text-sm leading-tight truncate font-semibold">{displayOf(row.opt)}</span>
              {#if row.opt.desc}
                <span class="block text-[11px] leading-snug opacity-60 line-clamp-2">{row.opt.desc}</span>
              {/if}
            {/if}
          </li>
        {:else}
          <li class="px-3 py-1.5 text-xs opacity-50 italic">—</li>
        {/each}
      </ul>
      <p class="text-[11px] leading-snug opacity-60">
        {#if editText.trim()}
          ✓ {acceptLabel || l.accept}: “{editText.trim()}” ({otherLabel})
        {:else}
          {otherLabel}
        {/if}
      </p>
    </div>
  {/if}

  <!-- Round 25: the info panel below the combobox was removed per user
       request; descriptions now only appear in the side ⓘ popover. -->
</div>

<style>
  .combo-root :global(.btn:focus-visible) {
    outline: 2px solid var(--fallback-p, oklch(0.55 0.2 260));
    outline-offset: 1px;
  }
</style>
