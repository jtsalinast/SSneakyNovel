<script>
  // SmartSelect — simplified, SINGLE-select sibling of SmartCombobox.
  //
  // Derived from SmartCombobox.svelte (kept intact for the multi-select
  // widgets) and stripped down to behave like a plain <select>:
  //  - ONE value at a time: picking an option replaces the selection and
  //    closes the dropdown; no chips, no checkbox/radio squares.
  //  - NO field label rendered by this component — the parent owns the label.
  //  - The trigger icons that open tooltips are KEPT, identical to
  //    SmartCombobox:
  //      * ⓘ button at the right end of the control box → opens the SAME
  //        Revolut-style dark summary popover ("Seleccionados" panel) that
  //        SmartCombobox renders — header + localized title, ✕ close button,
  //        one card per entry showing label + description. With single
  //        select it simply lists the one chosen option (or "Sin elementos
  //        seleccionados." when empty).
  //      * each option row is TWO LINES (label on top, muted description
  //        below), exactly like SmartCombobox's rows; descriptions are shown
  //        inline in the row rather than behind a per-row ⓘ.
  //  - Live search filter over labels + descriptions, "Añadir" custom-value
  //    row (Enter or click) and clear button, inherited from SmartCombobox.
  //
  // API:
  //  bind:value  string   the selected option's value ('value' field), '' = none.
  //  options     [{value,label,description}]  flat list (no groups needed).
  //  placeholder / emptyLabel / addPlaceholder / clearLabel — chrome texts.
  //  disabled    boolean
  //  on:change   CustomEvent { detail: { value, item } }
  import { createEventDispatcher } from 'svelte';
  import { get } from 'svelte/store';
  import { uiLocale, translate } from '../i18n/index.js';

  const dispatch = createEventDispatcher();

  export let value = '';
  export let options = [];
  export let placeholder = '';
  export let emptyLabel = 'Auto';
  export let addPlaceholder = 'Añadir...';
  export let clearLabel = 'Clear';
  export let otherLabel = 'Other';
  export let allowCustom = true;
  export let ariaLabel = '';
  export let disabled = false;

  let isOpen = false;
  let isTooltipOpen = false;
  let searchQuery = '';
  let root;
  let inputEl;

  // Localized chrome texts resolved directly from the i18n catalog (same keys
  // the legacy ComboSelect fields use), so the control is fully translatable
  // without widening any parent's `labels` prop. Reactive on uiLocale.
  // `translate()` returns the raw key when it has no entry → guard for that.
  function tr(key, fallback) {
    const v = translate(key, undefined, get(uiLocale));
    return v && v !== key ? v : fallback;
  }
  $: addPlaceholderText = addPlaceholder || tr('config.story.add', 'Añadir...');
  $: clearLabelText = clearLabel || tr('config.story.clear', 'Clear');
  // Summary-popover chrome — same strings the SmartCombobox panel shows.
  $: selectedTitleText = tr('config.story.selected', 'Seleccionados');
  $: emptySummaryText = tr('config.story.none_selected', 'Sin elementos seleccionados.');
  $: customSummaryText = tr('config.story.custom', 'Elemento personalizado');

  // —— Outside-click handling (same window-listener pattern SmartCombobox uses)
  let pressStartedInside = false;
  function inSelf(t) {
    return !!(t instanceof Element && root && t.closest('.smart-select-root') === root);
  }
  function onDocMouseDown(e) {
    const t = e.target;
    if (!(t instanceof Element)) { pressStartedInside = false; return; }
    if (inSelf(t)) { pressStartedInside = true; return; }
    pressStartedInside = false;
    if (isOpen) isOpen = false;
    if (isTooltipOpen) isTooltipOpen = false;
  }
  function onDocClick(e) {
    const t = e.target;
    if (!(t instanceof Element)) return;
    if (inSelf(t)) return;
    if (pressStartedInside) { pressStartedInside = false; return; }
    if (isOpen) isOpen = false;
    if (isTooltipOpen) isTooltipOpen = false;
  }

  // —— Selection display mode ———————————————————————————————————
  // When the search box is empty, the current selection is rendered as REAL
  // input text in full-strength foreground color instead of the native
  // `placeholder` attribute. The old approach parked the selected label
  // in the placeholder, which Tailwind's preflight styles with
  // `color: color-mix(currentcolor 50%, transparent)` (plus
  // `placeholder:text-neutral-400`), so every chosen option looked washed-out
  // grey. It must stay real text even while the dropdown is open (focusing the
  // input opens it): an empty box would otherwise show the "Añadir..." prompt
  // over the selection and lose the mirrored value on blur. Only actual typed
  // search text replaces the mirror; the caret behaviour when typing is
  // unchanged from SmartCombobox.
  // Color parity with SmartCombobox's chips is handled by utility classes on
  // the input itself (`text-neutral-800 dark:text-base-content`) — do NOT add
  // a scoped `.ss-value`/`.ss-input { color: ... }` rule: scoped styles win
  // over utilities and hardcoding `var(--color-base-content)` made the text
  // near-white under this app's default dark theme (xianii).
  $: showValueAsText = !searchQuery.trim();

  // —— Value <-> display plumbing (single choice) ————————————————
  // `lastEmitted` guards against echo loops when the parent normalizes the
  // bound string back at us (mirrors SmartCombobox's adoption strategy, but
  // trivial here since there is only one token to resolve).
  let lastEmitted = null;

  function findItem(v) {
    const s = String(v || '').trim().toLowerCase();
    if (!s) return null;
    return allOptions.find((it) => String(it.value).toLowerCase() === s) || null;
  }

  function emitValue(v, item) {
    const nv = String(v || '');
    lastEmitted = nv;
    if (nv !== value) value = nv;
    dispatch('change', { value: nv, item });
  }

  // Adopt externally-written values (parent seeds / cfg loads): if the stored
  // string matches an option, keep it; unknown non-empty strings stay as-is so
  // the control can still display them (custom selections persist in cfg).
  $: if (String(value || '') !== String(lastEmitted || '')) {
    lastEmitted = String(value || '');
  }

  // —— Options list —————————————————————————————————————————————
  // Custom terms added through "+ Añadir ..." live in a local overlay so they
  // survive parent-driven rebuilds of the `options` prop.
  let customItems = [];
  $: allOptions = [...customItems, ...options];

  // Summary-popover entry. `allOptions` (custom overlay + prop catalog) is
  // declared below, so this derivation must live after it — otherwise the
  // temporal-dead-zone crash silently kills the whole component flush and the
  // two-line option rows never render. Custom values added through "+ Añadir"
  // are matched case-insensitively by LABEL as a fallback: they persist in cfg
  // under their raw text, which never equals their internal 'cx:' id.
  $: currentItem = (() => {
    const hit = findItem(value);
    if (hit) return hit;
    const v = String(value || '').trim();
    if (!v) return null;
    const byLabel = allOptions.find(
      (it) => String(it.label).trim().toLowerCase() === v.toLowerCase(),
    );
    return byLabel || { value: v, label: v, description: '' };
  })();

  $: displayLabel = (() => {
    const v = String(value || '').trim();
    if (!v) return emptyLabel;
    const it = findItem(v);
    return it ? it.label : v;
  })();

  $: filteredOptions = (() => {
    const q = searchQuery.toLowerCase().trim();
    if (!q) return allOptions;
    return allOptions.filter(
      (it) =>
        String(it.label).toLowerCase().includes(q) ||
        String(it.description || '').toLowerCase().includes(q),
    );
  })();

  $: hasExactMatch = (() => {
    const q = searchQuery.toLowerCase().trim();
    if (!q) return false;
    return allOptions.some((it) => String(it.label).toLowerCase() === q);
  })();

  const customId = (label) => 'cx:' + String(label).trim().toLowerCase();

  function selectOption(item) {
    emitValue(item.value, item);
    searchQuery = '';
    isOpen = false;
  }

  function addCustomItem() {
    const query = searchQuery.trim();
    if (!query || disabled) return;
    const existing = allOptions.find((it) => String(it.label).toLowerCase() === query.toLowerCase());
    if (existing) {
      selectOption(existing);
      return;
    }
    const id = customId(query);
    if (!customItems.some((it) => it.value === id)) {
      customItems = [...customItems, { value: id, label: query, description: otherLabel }];
    }
    selectOption({ value: id, label: query, description: otherLabel });
  }

  function handleEnter() {
    const query = searchQuery.trim().toLowerCase();
    if (!query) return;
    const matched = allOptions.find((it) => String(it.label).toLowerCase() === query);
    if (matched) selectOption(matched);
    else addCustomItem();
  }

  function clearSelection() {
    emitValue('', null);
    searchQuery = '';
  }

  function toggleDropdown() {
    if (disabled) return;
    isOpen = !isOpen;
    if (isOpen) {
      isTooltipOpen = false;
      setTimeout(() => inputEl && inputEl.focus(), 0);
    }
  }
  // Clicking anywhere in the control box opens the list and closes the summary
  // popover — same focusAndOpen behaviour as SmartCombobox. Without closing
  // the popover here, clicking the input while it is open would re-trigger the
  // ⓘ button's click (the button sits inside this box) and the popover would
  // never close, hiding the description the user expects to see.
  function focusAndOpen() {
    if (disabled) return;
    isOpen = true;
    isTooltipOpen = false;
  }
  // ⓘ trigger: toggles the dark summary popover (same as SmartCombobox's
  // toggleTooltip). Opening it closes the option list, and vice versa.
  function toggleTooltip() {
    isTooltipOpen = !isTooltipOpen;
    if (isTooltipOpen) isOpen = false;
  }
</script>

<svelte:window on:mousedown={onDocMouseDown} on:click={onDocClick} />

<div
  bind:this={root}
  class="smart-select-root group relative select-none w-full"
  role="group"
  aria-label={ariaLabel || undefined}
>
  <!-- Control box: label-less, like a plain <select>. Center: live search
       input that mirrors the current selection as full-strength text while
       closed (see `.ss-input` below — never via the translucent native
       placeholder) and switches to the "Añadir..." prompt once typing starts;
       right: the ⓘ summary trigger, clear ✕ and animated chevron — same order
       and styling as SmartCombobox. -->
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
  <div
    role="combobox"
    tabindex="-1"
    aria-haspopup="listbox"
    aria-expanded={isOpen}
    aria-controls="smart-select-list"
    on:click={focusAndOpen}
    class="min-h-[36px] border rounded-lg pl-3 pr-1 py-1 flex items-center gap-1 transition-all duration-150
           {disabled ? 'opacity-60 cursor-not-allowed bg-base-200' : 'cursor-pointer'}
           {isOpen
             ? 'ring-2 ring-black/[0.08] dark:ring-white/15 border-neutral-400 dark:border-neutral-500 bg-white dark:bg-base-100 shadow-sm'
             : 'border-neutral-200/80 dark:border-base-300 bg-neutral-50/50 dark:bg-base-200/50 hover:bg-neutral-50 dark:hover:bg-base-200 hover:border-neutral-300 dark:hover:border-base-300'}"
  >
    <!-- The selected label is REAL input text (full-strength foreground,
         `text-neutral-800 dark:text-base-content` — same color as SmartCombobox's
         chips), never the native placeholder — Tailwind preflight renders
         ::placeholder at 50% alpha, which made selections look grey.
         The placeholder attribute only carries genuine hints: "Añadir..."
         while searching, or the field's empty hint when nothing is chosen. -->
    <input
      type="text"
      bind:this={inputEl}
      value={showValueAsText ? displayLabel : searchQuery}
      on:focus={focusAndOpen}
      on:click|stopPropagation={focusAndOpen}
      on:input={(e) => {
        // Typing while the selection is mirrored in the box replaces it.
        if (showValueAsText) searchQuery = '';
        searchQuery = e.target.value;
      }}
      on:keydown.enter={(e) => { e.preventDefault(); handleEnter(); }}
      on:keydown.escape={() => { isOpen = false; }}
      placeholder={searchQuery.trim()
        ? addPlaceholderText
        : (String(value || '').trim() ? '' : (placeholder || emptyLabel))}
      disabled={disabled}
      class="ss-input flex-1 min-w-0 border-none outline-none text-xs font-medium text-neutral-800 dark:text-base-content bg-transparent py-1 pl-1 placeholder:text-neutral-400 focus:outline-none focus:ring-0"
    />

    <div class="flex items-center gap-1 ml-auto shrink-0 text-neutral-400 dark:text-neutral-500">
      <!-- Summary tooltip trigger (ⓘ), identical to SmartCombobox's: opens
           the Revolut-style dark popover listing the current selection. -->
      <div class="relative">
        <button
          type="button"
          on:click|stopPropagation={toggleTooltip}
          class="p-1.5 rounded-lg transition-colors relative flex items-center justify-center
                 opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 focus:opacity-100
                 {isTooltipOpen ? 'bg-neutral-200/60 dark:bg-base-300 text-neutral-900 dark:text-white' : 'hover:bg-neutral-200/50 dark:hover:bg-base-300 hover:text-neutral-700 dark:hover:text-neutral-200'}"
          title="Resumen de selección"
          aria-label="Resumen de selección"
        >
          <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path></svg>
          {#if String(value || '').trim()}
            <span class="absolute -top-1 -right-1 bg-primary text-primary-content rounded-full w-3.5 h-3.5 text-[8px] flex items-center justify-center font-bold">1</span>
          {/if}
        </button>

        {#if isTooltipOpen}
          <!-- Revolut-style dark summary popover (closed by the window-level
               outside-click handler above) — same panel SmartCombobox renders,
               adapted to a single entry. -->
          <div
            class="absolute right-0 mt-2 w-72 bg-neutral-900 text-white text-xs rounded-xl p-3.5 shadow-2xl z-50 border border-white/10 animate-pop"
          >
            <div class="font-semibold mb-2 flex items-center justify-between text-neutral-200 border-b border-white/10 pb-2 text-[11px]">
              <span class="uppercase tracking-wider">{selectedTitleText} ({currentItem ? 1 : 0})</span>
              <button type="button" on:click={() => { isTooltipOpen = false; }} class="text-neutral-400 hover:text-white">&times;</button>
            </div>
            {#if !currentItem}
              <p class="text-neutral-400 italic py-2 text-[11px] text-center">{emptySummaryText}</p>
            {:else}
              <!-- Flat entry: no per-item remove button (the ✕ of the control
                   already clears the single selection) and no card background —
                   just label + description over the popover's own surface. -->
              <div class="flex flex-col gap-0.5 px-0.5 py-1">
                <span class="font-medium text-neutral-100 text-xs">{currentItem.label}</span>
                <span class="text-[10px] text-neutral-400 leading-tight">{currentItem.description || customSummaryText}</span>
              </div>
            {/if}
          </div>
        {/if}
      </div>

      {#if String(value || '').trim()}
        <button
          type="button"
          on:click|stopPropagation={clearSelection}
          class="p-1.5 rounded-lg hover:bg-neutral-200/50 dark:hover:bg-base-300 hover:text-neutral-700 dark:hover:text-neutral-200 transition-colors"
          title={clearLabelText}
          aria-label={clearLabelText}
        >
          <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12"></path></svg>
        </button>
      {/if}
      <button
        type="button"
        on:click|stopPropagation={toggleDropdown}
        class="p-1.5 rounded-lg hover:bg-neutral-200/50 dark:hover:bg-base-300 hover:text-neutral-700 dark:hover:text-neutral-200 transition-colors"
        aria-label="Abrir lista"
      >
        <svg class="w-3.5 h-3.5 transition-transform duration-200 {isOpen ? 'rotate-180' : ''}" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7"></path></svg>
      </button>
    </div>
  </div>

  <!-- Dropdown panel: flat single-choice list, two-line rows like SmartCombobox -->
  {#if isOpen}
    <div id="smart-select-list" role="listbox" class="absolute left-0 right-0 mt-1.5 bg-white dark:bg-base-100 border border-black/[0.08] dark:border-base-300 rounded-xl shadow-[0_12px_32px_rgba(0,0,0,0.08)] dark:shadow-[0_12px_32px_rgba(0,0,0,0.4)] z-50 max-h-80 overflow-y-auto p-1.5 animate-pop">
      {#if allowCustom && searchQuery.trim() && !hasExactMatch}
        <!-- Create-custom row (coral accent, unified with the app theme) -->
        <!-- svelte-ignore a11y-click-events-have-key-events -->
        <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
        <div
          on:click={addCustomItem}
          class="mb-1 p-2.5 bg-neutral-50 dark:bg-base-200 hover:bg-neutral-100 dark:hover:bg-base-300 border border-neutral-200/80 dark:border-base-300 rounded-lg cursor-pointer transition-all flex items-center justify-between text-neutral-900 dark:text-base-content text-xs font-medium"
          role="button"
          tabindex="-1"
        >
          <div class="flex items-center gap-2">
            <span class="w-5 h-5 rounded-md bg-primary text-primary-content flex items-center justify-center font-bold text-xs">+</span>
            <span>{addPlaceholderText} "<strong class="text-primary">{searchQuery}</strong>"</span>
          </div>
          <span class="text-[10px] bg-neutral-200 dark:bg-base-300 text-neutral-700 dark:text-neutral-300 px-2 py-0.5 rounded font-mono">Enter ↵</span>
        </div>
      {/if}

      {#if filteredOptions.length === 0}
        <div class="p-6 text-center text-neutral-400 text-xs">No se encontraron opciones.</div>
      {:else}
        <div class="space-y-0.5">
          {#each filteredOptions as item (item.value)}
            {@const active = currentItem && currentItem.value === item.value}
            <!-- svelte-ignore a11y-click-events-have-key-events -->
            <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
            <div
              on:click|stopPropagation={() => selectOption(item)}
              class="flex items-center gap-2.5 px-2.5 py-2 rounded-lg cursor-pointer transition-colors text-xs group
                     {active
                       ? 'bg-neutral-100/80 dark:bg-base-300/60 text-neutral-900 dark:text-base-content'
                       : 'hover:bg-neutral-50 dark:hover:bg-base-200 text-neutral-700 dark:text-neutral-300'}"
              role="option"
              tabindex="-1"
              aria-selected={active}
            >
              <!-- Two-line row, exactly like SmartCombobox: label on top,
                   muted description below (both truncate). -->
              <div class="flex flex-col flex-1 min-w-0">
                <span class="font-medium truncate text-neutral-800 dark:text-neutral-100">{item.label}</span>
                {#if item.description}
                  <span class="text-[11px] text-neutral-400 truncate leading-tight font-normal">{item.description}</span>
                {/if}
              </div>
              {#if active}
                <svg class="w-3.5 h-3.5 shrink-0 text-primary" fill="none" stroke="currentColor" stroke-width="2.5" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"></path></svg>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  @keyframes popIn {
    0% { opacity: 0; transform: scale(0.97) translateY(-2px); }
    100% { opacity: 1; transform: scale(1) translateY(0); }
  }
  .animate-pop {
    animation: popIn 0.15s cubic-bezier(0.16, 1, 0.3, 1) forwards;
  }

  /* The selected value must read exactly like SmartCombobox's chips: the
     chip text uses `text-neutral-800 dark:text-base-content` (see the option
     rows too). The hardcoded fallback below was wrong — this app ships a DARK
     theme by default (`data-theme="xianii"` sets --color-base-content to a
     near-WHITE oklch(96%)), so a light-mode fallback of #171717 made the input
     inherit that white token and the selection looked white instead of black.
     No color override here: let the Tailwind classes on `.ss-input` decide,
     which keeps light/dark parity with the chips automatically. */
  .ss-input {
    -webkit-text-fill-color: currentColor;
    opacity: 1;
  }

  /* Genuine hints only (empty field / "Añadir..." while searching) stay muted,
     never the mirrored selection text. */
  .ss-input::placeholder {
    opacity: 1;
  }
</style>
