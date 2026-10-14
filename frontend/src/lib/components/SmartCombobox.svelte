<script>
  // SmartCombobox — advanced multi-select combobox (Revolut & Notion style),
  // adapted from the standalone HTML/Alpine prototype the user provided and
  // reimplemented for this Svelte 4 + Tailwind 4 + DaisyUI 5 app.
  //
  // Features kept from the prototype:
  //  - Chip-style multi-select with removable pills inside the control box.
  //  - Live search input that filters the option list across every group.
  //  - Collapsible group accordion headers ("GRUPO (n)") in the dropdown.
  //  - "Add <query>" custom-value row when the typed text has no exact match
  //    (custom entries land in a dedicated "Personalizados" group).
  //  - Dark summary popover (ⓘ button) listing the current selection with
  //    per-item remove buttons, plus clear-all and animated chevron.
  //  - Enter selects an exact label match or creates the custom item.
  //
  // Mock data: when no `groups` prop is passed the component renders the
  // prototype's technology catalogs (Desarrollo Frontend / Infraestructura &
  // Backend / DevOps & Herramientas) so it can be dropped anywhere as a
  // self-contained demo. The parent (GenreCascadeFields) passes groups built
  // from the REAL genre/subgenre cascade data instead.
  //
  // API:
  //  bind:value   string  selected items joined by `sep` (same storage format
  //                       as ComboSelect, so parents can treat both alike).
  //  groups       [{label, items:[{id,label,description}]}]  optional source.
  //  initialIds   number[] preselected ids (mock mode only; the prototype
  //                       shipped with [1, 5]).
  //  placeholder / addPlaceholder / emptyGroupLabel — localized chrome texts.
  //  disabled     boolean
  //  on:change    CustomEvent { detail: { value, ids, items } } whenever the
  //                       selection changes.
  import { createEventDispatcher } from 'svelte';

  const dispatch = createEventDispatcher();

  export let value = '';
  export let groups = null;          // [{label, items:[{id,label,description}]}]
  export let initialIds = [];        // preselected ids (mock mode default [1, 5])
  export let sep = ', ';
  export let placeholder = 'Buscar o seleccionar elementos...';
  export let addPlaceholder = 'Añadir...';
  export let customGroupLabel = 'Personalizados';
  export let disabled = false;
  export let ariaLabel = '';
  // When true, the dropdown becomes a SINGLE-CHOICE list (radio behaviour):
  // picking an item replaces the selection instead of adding to it. Used by
  // the "Protagonist type" proof of concept, whose real field stores exactly
  // one value in cfg.protagonist_type.
  export let single = false;
  // Flat mode: when false, the dropdown renders its items as ONE plain list —
  // no accordion group headers at all (the groups are still used internally
  // as data containers). Used by the "Genre" PoC combobox, which per user
  // request must show its options without any grouping.
  export let grouped = true;

  // —— Mock catalog (prototype defaults) ————————————————————————————————
  const MOCK_GROUPS = [
    {
      label: 'Desarrollo Frontend',
      items: [
        { id: 1, label: 'React.js', description: 'Biblioteca UI declarativa creada por Meta' },
        { id: 2, label: 'Vue.js', description: 'Framework progresivo e intuitivo para la web' },
        { id: 3, label: 'Svelte', description: 'Reactividad sin Virtual DOM en tiempo de ejecución' },
        { id: 4, label: 'Tailwind CSS', description: 'Framework CSS basado en clases de utilidad' },
      ],
    },
    {
      label: 'Infraestructura & Backend',
      items: [
        { id: 5, label: 'Node.js', description: 'Entorno de ejecución asíncrono en V8 JavaScript' },
        { id: 6, label: 'Python / FastAPI', description: 'Framework ultrarrápido para construir REST APIs' },
        { id: 7, label: 'Go', description: 'Lenguaje concurrente optimizado para microservicios' },
        { id: 8, label: 'PostgreSQL', description: 'Base de datos relacional potente y robusta' },
      ],
    },
    {
      label: 'DevOps & Herramientas',
      items: [
        { id: 9, label: 'Docker', description: 'Contenedores estandarizados para desplegar apps' },
        { id: 10, label: 'Kubernetes', description: 'Orquestación de contenedores a escala automatizada' },
        { id: 11, label: 'AWS Cloud', description: 'Servicios en la nube e infraestructura global' },
        { id: 12, label: 'GitHub Actions', description: 'Automatización e integración continua (CI/CD)' },
      ],
    },
  ];

  let isOpen = false;
  let isTooltipOpen = false;
  let searchQuery = '';
  // Accordion collapse state, held in a local writable STORE ($collapsed).
  // Why a store: Svelte 4 only invalidates reactive dependents on WHOLE
  // VARIABLE ASSIGNMENTS (`x = ...`). Mutating a plain `let mySet = new Set()`
  // through methods (`mySet.add(...)` / `mySet.delete(...)`) is invisible to
  // the compiler — no dirty flag is set, no `{#if}`/`$:` re-runs — which is
  // exactly why earlier revisions of this component appeared to "not collapse
  // live": the toggle worked in JS but nothing re-rendered until the panel was
  // destroyed and rebuilt on close/reopen. The store's `update()` notifies
  // subscribers explicitly, and every template read goes through `$collapsed`,
  // so toggles render immediately while the dropdown stays open.
  const collapsed = writableStore(new Set());

  function writableStore(initial) {
    let value = initial;
    const subs = new Set();
    return {
      subscribe(run) { subs.add(run); run(value); return () => subs.delete(run); },
      update(fn) { value = fn(value); subs.forEach((f) => f(value)); },
    };
  }

  let selectedIds = [...(initialIds || [])]; // mock/demo preselection ([1,5])
  let inputEl;
  let root;

  function toggleGroup(label) {
    collapsed.update((prev) => {
      const next = new Set(prev);
      if (next.has(label)) next.delete(label);
      else next.add(label);
      return next;
    });
  }

  // Outside-click handling (same window-listener pattern ComboSelect uses —
  // Svelte 4 has no `click/outside` modifier). A press that begins inside
  // this component never closes it via its trailing click.
  let pressStartedInside = false;
  function inSelf(t) {
    return !!(t instanceof Element && root && t.closest('.smart-combo-root') === root);
  }
  function onDocMouseDown(e) {
    const t = e.target;
    if (!(t instanceof Element)) { pressStartedInside = false; return; }
    if (inSelf(t)) { pressStartedInside = true; return; }
    pressStartedInside = false;
    if (isOpen || isTooltipOpen) closeDropdownOnly();
  }
  function onDocClick(e) {
    const t = e.target;
    if (!(t instanceof Element)) return;
    if (inSelf(t)) return;
    if (pressStartedInside) { pressStartedInside = false; return; }
    if (isOpen || isTooltipOpen) closeDropdownOnly();
  }

  // —— Working catalog (props groups + locally created "Personalizados") ———
  // The parent passes a `groups` prop that is REBUILT on every cascade change
  // (Sugeridos/Relacionados/Otros depend on the current genre/subgenre
  // selection). We keep a local working copy so custom terms added through
  // "+ Añadir ..." survive those rebuilds: whenever the prop changes we take
  // the new catalog and re-attach the previous Personalizados group in front.
  let allGroups = [];
  $: {
    const src = groups && groups.length ? groups : MOCK_GROUPS;
    const prevCustom = allGroups.find((g) => g.label === customGroupLabel);
    if (prevCustom && prevCustom.items.length) {
      const rest = src.filter((g) => g.label !== customGroupLabel);
      allGroups = [prevCustom, ...rest];
    } else {
      allGroups = src;
    }
  }

  // Selection-driven ids derived from the stored `value` string. Matching is
  // by LABEL (case-insensitive), which also adopts custom items whose stable
  // 'cx:' ids survive catalog rebuilds. Recomputed reactively so chips whose
  // option disappeared from the regenerated buckets still resolve when they
  // come back (and custom items always do, thanks to the working copy above).
  $: valueIds = (() => {
    const wanted = String(value || '')
      .split(sep)
      .map((x) => x.trim())
      .filter(Boolean)
      .map((x) => x.toLowerCase());
    return allGroups
      .flatMap((g) => g.items)
      .filter((it) => wanted.includes(String(it.label).toLowerCase()))
      .map((it) => it.id);
  })();

  // —— Selection <-> value plumbing ————————————————————————————————————
  // `lastEmitted` is the exact string this instance last wrote to `value`;
  // anything different coming from the parent (one-shot seeds, external cfg
  // edits) is adopted by resolving labels -> catalog ids. The adoption block
  // NEVER re-emits: it only syncs selectedIds, so no echo can bounce back to
  // the parent and custom ("Personalizados") chips survive every catalog
  // refresh. User interactions push state OUT through emitValue().
  let lastEmitted = null;

  function emitValue() {
    // Selection-driven value snapshot computed DIRECTLY from `selectedIds` —
    // never from the reactive `selectedItems`. Reason (the real "smart genre
    // ignores selections / updates lists only with 2+ chips" bug): inside a
    // click handler Svelte 4 does not run derived `$:` statements between the
    // `selectedIds` assignment and this call, so `selectedItems` is still the
    // STALE pre-click list. The emitted string therefore lacked the just-added
    // chip, the parent wrote that stale text into cfg.type, and every derived
    // list (subgenres, conflict scale, protagonist type, settings, tone) was
    // filtered WITHOUT the newest selection until the next interaction pushed
    // it in. Deriving the labels here keeps emission truthful on the first
    // click.
    const v = selectedIds
      .map((id) => itemLabelById(id))
      .filter(Boolean)
      .map((s) => s.trim())
      .join(sep)
      .trim();
    lastEmitted = v;
    if (v !== value) value = v;
    // `selectedIds` already IS the source of truth here — re-adopting from
    // `value` would be a no-op at best, and could clobber it when the parent's
    // change handler normalizes the bound string before flushing back.
    // Items resolved by id (never the possibly-stale reactive list).
    const items = selectedIds.map((id) => findItemById(id)).filter(Boolean);
    dispatch('change', { value: v, ids: [...selectedIds], items });
  }

  function findItemById(id) {
    for (const g of allGroups) {
      const it = g.items.find((x) => x.id === id);
      if (it) return it;
    }
    return null;
  }

  // Adopt an EXTERNALLY-written `value` (parent handler mirroring a sibling
  // widget's selection into the shared storage field, one-shot seeds, etc.):
  // resolve the comma-separated labels back to catalog ids so the chips/radios
  // update live — exactly what the legacy ComboSelect gets for free via
  // bind:value. Guarded against echo: skipped when the string is the one this
  // instance just emitted itself.
  function syncFromValue() {
    if (normalizedEq(value, lastEmitted)) return;
    const adopted = [...valueIds];
    if (!adopted.length && !String(value || '').trim() && initialIds && initialIds.length) {
      // Mount-time demo preselection: nothing stored yet but initialIds given
      // (mock mode ships [1, 5] = React.js + Node.js like the prototype).
      selectedIds = [...initialIds];
    } else {
      // Echo guard #2: when the parent normalizes/echoes the bound string
      // back at us (e.g. reordering known genres first and appending unknown
      // free-text entries), some labels may fail to resolve against the
      // catalog in that same flush. Adopting blindly would silently DROP the
      // chips the user just added — exactly what the legacy ComboSelect never
      // does, because bind:value there only carries a display string and the
      // real selection is its internal state. So we only clobber the current
      // selection when BOTH conditions hold:
      //   1. The incoming string is NOT just an echo of what this instance
      //      last emitted (checked with normalizedEq above — the parent's
      //      reorder/normalize pass makes raw strings differ while the label
      //      SET stays identical).
      //   2. The adoption loses NOTHING: either every currently-selected id is
      //      still resolvable (`!lost.length`) or the incoming string is a
      //      genuine full replacement whose labels ALL resolve in the catalog
      //      (`fullyResolved`). An unresolvable token means either a custom
      //      chip still pending creation or a mid-flush catalog; in that case
      //      keep the current selection untouched. NOTE: previously the guard
      //      accepted "no losses" on its own, which let a PARTIALLY resolvable
      //      stale echo (missing the newest chip) overwrite the live selection
      //      — the other half of the "smart genre ignores my picks" bug.
      const lost = selectedIds.filter((id) => !adopted.includes(id));
      const wantedLabels = splitLabels(value);
      const adoptedLabels = splitLabels(adopted.map((id) => itemLabelById(id)).join(sep));
      const fullyResolved = wantedLabels.length === adoptedLabels.length;
      if (!lost.length && fullyResolved) {
        // Nothing to lose and everything resolves → adopt freely (also lets
        // chips whose option temporarily vanished from a rebuilt bucket come
        // back once it reappears).
        selectedIds = adopted;
      } else if (lost.length && fullyResolved && wantedLabels.length > 0) {
        // Real external replacement (e.g. a sibling widget wrote a different
        // complete selection): adopt only when EVERY wanted label resolved.
        selectedIds = adopted;
      }
      // Otherwise: partial/unresolvable adoption → no-op (current selection
      // remains source of truth until a real external change arrives).
    }
  }

  function itemLabelById(id) {
    for (const g of allGroups) {
      const it = g.items.find((x) => x.id === id);
      if (it) return it.label;
    }
    return '';
  }

  // Label-set helpers used by the echo guards above: compare selections by
  // their normalized, sorted label lists so ordering/spacing differences in
  // the bound string never look like a real external change.
  function splitLabels(s) {
    return String(s || '')
      .split(sep)
      .map((x) => x.trim().toLowerCase())
      .filter(Boolean)
      .sort();
  }
  function normalizedEq(a, b) {
    if (a === b) return true;
    if (a == null || b == null) return false;
    return splitLabels(a).join('\u0000') === splitLabels(b).join('\u0000');
  }

  $: if (value !== lastEmitted) syncFromValue();

  function closeDropdownOnly() {
    isOpen = false;
    isTooltipOpen = false;
  }
  function focusAndOpen() {
    if (disabled) return;
    isOpen = true;
    isTooltipOpen = false;
  }
  function toggleDropdown() {
    if (disabled) return;
    isOpen = !isOpen;
    if (isOpen) {
      isTooltipOpen = false;
      setTimeout(() => inputEl && inputEl.focus(), 0);
    }
  }
  function toggleTooltip() {
    isTooltipOpen = !isTooltipOpen;
    if (isTooltipOpen) isOpen = false;
  }
  function isGroupCollapsed(label) {
    // Direct store read via a manual subscribe/unsubscribe snapshot. NOTE: the
    // template does NOT call this function — it reads `$collapsed.has(...)`
    // directly, because Svelte only auto-subscribes `$store` when the `$`-name
    // appears in the TEMPLATE or in a `$:` statement; inside a plain function
    // called from the template there is no subscription, so relying on it for
    // reactivity would silently never re-render (the original bug).
    let snap;
    const un = collapsed.subscribe((v) => { snap = v; });
    un();
    return snap.has(label);
  }
  function isSelected(id) {
    return selectedIds.includes(id);
  }
  function toggleItem(id) {
    if (single) {
      // Radio behaviour: replace the selection (or clear it when clicking the
      // already-active option). Keeps `value` in sync with a single label so
      // parents can bind it straight to a one-value field.
      selectedIds = isSelected(id) ? [] : [id];
      emitValue();
      closeDropdownOnly();
      return;
    }
    if (selectedIds.includes(id)) removeItem(id);
    else {
      selectedIds = [...selectedIds, id];
      emitValue();
    }
  }
  function removeItem(id) {
    selectedIds = selectedIds.filter((i) => i !== id);
    emitValue();
  }
  function clearAll() {
    selectedIds = [];
    emitValue();
  }

  $: allItems = allGroups.flatMap((g) => g.items);
  $: selectedItems = allItems.filter((it) => selectedIds.includes(it.id));
  $: hasExactMatch = (() => {
    const q = searchQuery.toLowerCase().trim();
    if (!q) return false;
    return allItems.some((it) => it.label.toLowerCase() === q);
  })();
  $: filteredGroups = (() => {
    const q = searchQuery.toLowerCase().trim();
    if (!q) return allGroups;
    return allGroups
      .map((g) => ({
        ...g,
        items: g.items.filter(
          (it) =>
            it.label.toLowerCase().includes(q) ||
            (it.description || '').toLowerCase().includes(q),
        ),
      }))
      .filter((g) => g.items.length > 0);
  })();

  // Stable custom id: same term always maps to the same id across catalog
  // refreshes (Date.now() would create a brand-new id on every re-add, which
  // broke adoption and risked duplicate keyed items).
  const customId = (label) => 'cx:' + String(label).trim().toLowerCase();

  function addCustomItem() {
    const query = searchQuery.trim();
    if (!query || disabled) return;
    const existing = allItems.find((i) => i.label.toLowerCase() === query.toLowerCase());
    if (existing) {
      if (single) selectedIds = [existing.id];
      else if (!selectedIds.includes(existing.id)) selectedIds = [...selectedIds, existing.id];
    } else {
      const newId = customId(query);
      let customGroup = allGroups.find((g) => g.label === customGroupLabel);
      if (!customGroup) {
        customGroup = { label: customGroupLabel, items: [] };
        allGroups = [customGroup, ...allGroups];
        customGroup = allGroups[0];
      }
      if (!customGroup.items.some((it) => it.id === newId)) {
        customGroup = { ...customGroup, items: [...customGroup.items, { id: newId, label: query, description: 'Añadido manualmente' }] };
        allGroups = allGroups.map((g) => (g.label === customGroupLabel ? customGroup : g));
      }
      selectedIds = single ? [newId] : [...selectedIds, newId];
    }
    searchQuery = '';
    emitValue();
    if (single) closeDropdownOnly();
  }

  function handleEnter() {
    const query = searchQuery.trim().toLowerCase();
    if (!query) return;
    const matched = allItems.find((i) => i.label.toLowerCase() === query);
    if (matched) {
      if (single) selectedIds = [matched.id];
      else if (!selectedIds.includes(matched.id)) selectedIds = [...selectedIds, matched.id];
      searchQuery = '';
      emitValue();
      if (single) closeDropdownOnly();
    } else {
      addCustomItem();
    }
  }
</script>

<svelte:window on:mousedown={onDocMouseDown} on:click={onDocClick} />

<div
  bind:this={root}
  class="smart-combo-root relative select-none w-full"
  role="group"
  aria-label={ariaLabel || undefined}
>
  <!-- Control box / input area -->
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
  <!-- svelte-ignore a11y-no-redundant-roles -->
  <div
    role="button"
    tabindex="-1"
    on:click={focusAndOpen}
    class="min-h-[52px] border rounded-xl px-3 py-2 flex flex-wrap items-center gap-1.5 transition-all duration-150
           {disabled ? 'opacity-60 cursor-not-allowed bg-base-200' : 'cursor-pointer'}
           {isOpen
             ? 'ring-2 ring-black/[0.08] dark:ring-white/15 border-neutral-400 dark:border-neutral-500 bg-white dark:bg-base-100 shadow-sm'
             : 'border-neutral-200/80 dark:border-base-300 bg-neutral-50/50 dark:bg-base-200/50 hover:bg-neutral-50 dark:hover:bg-base-200 hover:border-neutral-300 dark:hover:border-base-300'}"
  >
    <!-- Selected chips (Notion-style pills) -->
    {#each selectedItems as item (item.id)}
      <span
        class="inline-flex items-center gap-1.5 bg-neutral-100 hover:bg-neutral-200/70 dark:bg-base-300 dark:hover:bg-base-300/70 text-neutral-800 dark:text-base-content border border-black/[0.06] dark:border-white/10 pl-2.5 pr-1.5 py-1 rounded-lg text-xs font-medium transition-colors animate-pop"
      >
        <span class="w-1.5 h-1.5 rounded-full bg-neutral-400 shrink-0"></span>
        <span class="max-w-[140px] truncate">{item.label}</span>
        <button
          type="button"
          on:click|stopPropagation={() => removeItem(item.id)}
          class="w-4 h-4 rounded-md flex items-center justify-center text-neutral-400 hover:text-neutral-900 dark:hover:text-white hover:bg-black/5 dark:hover:bg-white/10 transition-colors"
          aria-label="Quitar {item.label}"
        >
          <svg class="w-3 h-3" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12"></path></svg>
        </button>
      </span>
    {/each}

    <!-- Search input. The native placeholder shows the full prompt while
         nothing is selected (caret sits right after it, exactly like the
         prototype); once chips exist it shrinks to "Añadir...". The old
         sibling <span> pseudo-placeholder was what pushed the caret away
         from the text in the PoC combos — removed. -->
    <div class="flex-1 min-w-[110px] flex items-center">
      <input
        type="text"
        bind:this={inputEl}
        bind:value={searchQuery}
        on:focus={() => { isOpen = true; }}
        on:click|stopPropagation={() => { isOpen = true; }}
        on:keydown.enter={(e) => { e.preventDefault(); handleEnter(); }}
        on:keydown.escape={closeDropdownOnly}
        placeholder={selectedIds.length > 0 ? addPlaceholder : placeholder}
        disabled={disabled}
        class="w-full border-none outline-none text-xs font-medium text-neutral-900 dark:text-base-content bg-transparent py-1 pl-1 placeholder:text-neutral-400 focus:outline-none focus:ring-0"
      />
    </div>

    <!-- Actions: tooltip summary / clear all / chevron -->
    <div class="flex items-center gap-1 ml-auto text-neutral-400">
      <div class="relative">
        <button
          type="button"
          on:click|stopPropagation={toggleTooltip}
          class="p-1.5 rounded-lg transition-colors relative flex items-center justify-center
                 {isTooltipOpen ? 'bg-neutral-200/60 dark:bg-base-300 text-neutral-900 dark:text-white' : 'hover:bg-neutral-200/50 dark:hover:bg-base-300 hover:text-neutral-700 dark:hover:text-neutral-200'}"
          title="Resumen de selección"
          aria-label="Resumen de selección"
        >
          <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"></path></svg>
          {#if selectedIds.length > 0}
            <span class="absolute -top-1 -right-1 bg-blue-600 text-white rounded-full w-3.5 h-3.5 text-[8px] flex items-center justify-center font-bold">{selectedIds.length}</span>
          {/if}
        </button>

        {#if isTooltipOpen}
          <!-- Revolut-style dark summary popover (closed by the window-level
               outside-click handler above) -->
          <div
            class="absolute right-0 mt-2 w-72 bg-neutral-900 text-white text-xs rounded-xl p-3.5 shadow-2xl z-50 border border-white/10 animate-pop"
          >
            <div class="font-semibold mb-2 flex items-center justify-between text-neutral-200 border-b border-white/10 pb-2 text-[11px]">
              <span class="uppercase tracking-wider">Seleccionados ({selectedItems.length})</span>
              <button type="button" on:click={() => { isTooltipOpen = false; }} class="text-neutral-400 hover:text-white">&times;</button>
            </div>
            {#if selectedItems.length === 0}
              <p class="text-neutral-400 italic py-2 text-[11px] text-center">Sin elementos seleccionados.</p>
            {:else}
              <ul class="space-y-1.5 max-h-52 overflow-y-auto pr-1">
                {#each selectedItems as item (item.id)}
                  <li class="flex flex-col gap-0.5 bg-neutral-800/80 p-2 rounded-lg border border-white/5">
                    <div class="flex items-center justify-between">
                      <span class="font-medium text-neutral-100 text-xs">{item.label}</span>
                      <button on:click|stopPropagation={() => removeItem(item.id)} class="text-neutral-400 hover:text-red-400 text-[10px]" aria-label="Quitar {item.label}">&times;</button>
                    </div>
                    <span class="text-[10px] text-neutral-400 leading-tight">{item.description || 'Elemento personalizado'}</span>
                  </li>
                {/each}
              </ul>
            {/if}
          </div>
        {/if}
      </div>

      {#if selectedIds.length > 0}
        <button
          type="button"
          on:click|stopPropagation={clearAll}
          class="p-1.5 rounded-lg hover:bg-neutral-200/50 dark:hover:bg-base-300 hover:text-neutral-700 dark:hover:text-neutral-200 transition-colors"
          title="Limpiar todo"
          aria-label="Limpiar todo"
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

  <!-- Dropdown panel -->
  {#if isOpen}
    <div class="absolute left-0 right-0 mt-1.5 bg-white dark:bg-base-100 border border-black/[0.08] dark:border-base-300 rounded-xl shadow-[0_12px_32px_rgba(0,0,0,0.08)] dark:shadow-[0_12px_32px_rgba(0,0,0,0.4)] z-50 max-h-80 overflow-y-auto p-1.5 animate-pop">
      {#if searchQuery.trim() && !hasExactMatch}
        <!-- Create-custom row -->
        <!-- svelte-ignore a11y-click-events-have-key-events -->
        <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
        <div
          on:click={addCustomItem}
          class="mb-1 p-2.5 bg-neutral-50 dark:bg-base-200 hover:bg-neutral-100 dark:hover:bg-base-300 border border-neutral-200/80 dark:border-base-300 rounded-lg cursor-pointer transition-all flex items-center justify-between text-neutral-900 dark:text-base-content text-xs font-medium"
          role="button"
          tabindex="-1"
        >
          <div class="flex items-center gap-2">
            <span class="w-5 h-5 rounded-md bg-blue-600 text-white flex items-center justify-center font-bold text-xs">+</span>
            <span>Añadir "<strong class="text-blue-600 dark:text-blue-400">{searchQuery}</strong>"</span>
          </div>
          <span class="text-[10px] bg-neutral-200 dark:bg-base-300 text-neutral-700 dark:text-neutral-300 px-2 py-0.5 rounded font-mono">Enter ↵</span>
        </div>
      {/if}

      {#if filteredGroups.length === 0 && (!searchQuery.trim() || hasExactMatch)}
        <div class="p-6 text-center text-neutral-400 text-xs">No se encontraron opciones.</div>
      {/if}

      {#if grouped}
        {#each filteredGroups as group (group.label)}
          <div class="mb-1 last:mb-0">
            <!-- Group accordion header. type="button" + preventDefault: this
                 button sits INSIDE the contenteditable/inputs' control box
                 region; without these, clicking it could blur/re-render the
                 panel (and submit a wrapping <form>), which made the collapse
                 appear to apply only after closing and reopening the dropdown. -->
            <button
              type="button"
              on:mousedown|stopPropagation={(e) => e.preventDefault()}
              on:click|stopPropagation|preventDefault={() => toggleGroup(group.label)}
              class="w-full flex items-center justify-between px-2.5 py-1.5 hover:bg-neutral-100/70 dark:hover:bg-base-200 rounded-md text-[11px] font-semibold text-neutral-400 dark:text-neutral-500 tracking-wider uppercase transition-colors"
            >
              <span>{group.label} ({group.items.length})</span>
              <svg class="w-3 h-3 transition-transform duration-200 text-neutral-400 {$collapsed.has(group.label) ? '' : 'rotate-180'}" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M19 9l-7 7-7-7"></path></svg>
            </button>

            {#if !$collapsed.has(group.label)}
              <div class="mt-0.5 space-y-0.5">
                {#each group.items as item (item.id)}
                  {@const checkedBox = isSelected(item.id)}
                  <!-- svelte-ignore a11y-click-events-have-key-events -->
                  <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
                  <div
                    on:click|stopPropagation={() => toggleItem(item.id)}
                    class="flex items-center gap-2.5 px-2.5 py-2 rounded-lg cursor-pointer transition-colors text-xs group
                           {checkedBox
                             ? 'bg-neutral-100/80 dark:bg-base-300/60 text-neutral-900 dark:text-base-content'
                             : 'hover:bg-neutral-50 dark:hover:bg-base-200 text-neutral-700 dark:text-neutral-300'}"
                    role={single ? 'radio' : 'checkbox'}
                    tabindex="-1"
                    aria-checked={checkedBox}
                  >
                    <!-- Notion-style checkbox -->
                    <div
                      class="w-4 h-4 {single ? 'rounded-full' : 'rounded'} border flex items-center justify-center transition-all shrink-0
                             {checkedBox
                               ? 'bg-neutral-900 dark:bg-neutral-100 border-neutral-900 dark:border-neutral-100 text-white dark:text-neutral-900'
                               : 'border-neutral-300 dark:border-base-300 bg-white dark:bg-base-100 group-hover:border-neutral-400'}"
                    >
                      {#if checkedBox}
                        {#if single}
                          <span class="w-1.5 h-1.5 rounded-full bg-white dark:bg-neutral-900"></span>
                        {:else}
                          <svg class="w-2.5 h-2.5" fill="none" stroke="currentColor" stroke-width="3" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"></path></svg>
                        {/if}
                      {/if}
                    </div>
                    <div class="flex flex-col flex-1 min-w-0">
                      <span class="font-medium truncate text-neutral-800 dark:text-neutral-100">{item.label}</span>
                      {#if item.description}
                        <span class="text-[11px] text-neutral-400 truncate leading-tight font-normal">{item.description}</span>
                      {/if}
                    </div>
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        {/each}
      {:else}
        <!-- Flat mode (grouped=false): ONE plain list, no group headers. -->
        <div class="space-y-0.5">
          {#each filteredGroups.flatMap((g) => g.items) as item (item.id)}
            {@const checkedBox = isSelected(item.id)}
            <!-- svelte-ignore a11y-click-events-have-key-events -->
            <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
            <div
              on:click|stopPropagation={() => toggleItem(item.id)}
              class="flex items-center gap-2.5 px-2.5 py-2 rounded-lg cursor-pointer transition-colors text-xs group
                     {checkedBox
                       ? 'bg-neutral-100/80 dark:bg-base-300/60 text-neutral-900 dark:text-base-content'
                       : 'hover:bg-neutral-50 dark:hover:bg-base-200 text-neutral-700 dark:text-neutral-300'}"
              role={single ? 'radio' : 'checkbox'}
              tabindex="-1"
              aria-checked={checkedBox}
            >
              <div
                class="w-4 h-4 {single ? 'rounded-full' : 'rounded'} border flex items-center justify-center transition-all shrink-0
                       {checkedBox
                         ? 'bg-neutral-900 dark:bg-neutral-100 border-neutral-900 dark:border-neutral-100 text-white dark:text-neutral-900'
                         : 'border-neutral-300 dark:border-base-300 bg-white dark:bg-base-100 group-hover:border-neutral-400'}"
              >
                {#if checkedBox}
                  {#if single}
                    <span class="w-1.5 h-1.5 rounded-full bg-white dark:bg-neutral-900"></span>
                  {:else}
                    <svg class="w-2.5 h-2.5" fill="none" stroke="currentColor" stroke-width="3" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"></path></svg>
                  {/if}
                {/if}
              </div>
              <div class="flex flex-col flex-1 min-w-0">
                <span class="font-medium truncate text-neutral-800 dark:text-neutral-100">{item.label}</span>
                {#if item.description}
                  <span class="text-[11px] text-neutral-400 truncate leading-tight font-normal">{item.description}</span>
                {/if}
              </div>
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
</style>
