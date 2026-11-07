<script>
  import { api } from '../lib/api.js';
  import { progress, settings, addToast, currentProject } from '../lib/stores.js';
  import { t } from '../lib/i18n/index.js';
  // Interfaz unificada: el filtro de categoría usa SmartSelect en lugar del
  // <select> nativo (búsqueda + popover ⓘ, como el resto del proyecto).
  import SmartSelect from '../lib/components/SmartSelect.svelte';
  import { setRibbon } from '../lib/ribbon.js';
  import { readLastStep } from '../lib/lastPage.js';

  // Reopening the project returns to the same ribbon subsection.
  const savedView = readLastStep($currentProject, 'memory');
  let viewMode = savedView === 'progression' ? 'progression' : 'historic'; // historic | progression
  const memoryTabs = [
    { id: 'historic', titleKey: 'memory.tabs.historic' },
    { id: 'progression', titleKey: 'memory.tabs.progression' },
  ];
  $: setRibbon(
    'memory',
    memoryTabs.map(def => ({ id: def.id, label: $t(def.titleKey) })),
    viewMode,
    id => { viewMode = id; }
  );

  let categoryFilter = 'all';

  const categoryKeys = ['character', 'location', 'item', 'event', 'promise', 'other'];
  // Category tags borrow the chapter-writing critic-mark hues (.wash-*).
  const categoryWash = (c) => ({ character: 'wash-violet', location: 'wash-green', item: 'wash-amber', event: 'wash-blue', promise: 'wash-pink' }[c] || 'badge-ghost');
  $: entries = $progress?.memory_entries || [];
  $: maxTokens = $progress?.memory_max_tokens || 0;
  $: categoryCounts = categoryKeys.reduce((acc, key) => {
    acc[key] = entries.filter(e => e.category === key).length;
    return acc;
  }, {});
  $: contentChars = entries.reduce((sum, e) => sum + [...(e.content || '')].length, 0);
  $: filtered = entries.filter(e => {
    if (categoryFilter !== 'all' && e.category !== categoryFilter) return false;
    return true;
  });

  // —— Progression: one card per Lore entity, scrubbed by chapter ————————
  // Facts are matched by entity name (or any alias) against the memory entries
  // (the backend stores no entity id on memories); an entry lands on the
  // earliest chapter its references point at, or chapter 0 when it has none.
  function progressionEntities(s) {
    if (!s) return [];
    const out = [];
    for (const c of s.characters || []) {
      out.push({ key: 'character:' + c.id, kind: 'character', name: c.name || '', aliases: c.aliases || [], subtitle: c.personality || '' });
    }
    for (const w of s.worldview || []) {
      out.push({ key: 'worldview:' + w.id, kind: w.category === 'location' ? 'location' : (w.category || 'worldview'), name: w.name || '', aliases: w.aliases || [], subtitle: w.category || '' });
    }
    for (const o of s.organizations || []) {
      out.push({ key: 'organization:' + o.id, kind: 'organization', name: o.name || '', aliases: o.aliases || [], subtitle: o.type || '' });
    }
    return out;
  }
  function factChapter(entry) {
    const refs = entry.references || [];
    if (!refs.length) return 0;
    return Math.min(...refs.map(r => r.chapter || 0));
  }
  function factsFor(entity, upTo) {
    if (!entity || !entity.name) return [];
    const names = [entity.name, ...(entity.aliases || [])].filter(Boolean).map(n => n.toLowerCase());
    return entries
      .filter(m => {
        const content = (m.content || '').toLowerCase();
        return names.some(n => content.includes(n)) && factChapter(m) <= upTo;
      })
      .sort((a, b) => factChapter(a) - factChapter(b) || (a.id || 0) - (b.id || 0));
  }

  $: loreEntities = progressionEntities($settings);
  $: totalChapters = Math.max(1, ($progress?.chapters || []).length);
  let selectedEntityKey = '';
  let selectedChapter = 0;
  $: selectedEntity = loreEntities.find(e => e.key === selectedEntityKey) || loreEntities[0] || null;
  $: if (selectedChapter < 1 || selectedChapter > totalChapters) selectedChapter = totalChapters;
  $: relatedFacts = factsFor(selectedEntity, selectedChapter);
  $: entityFactCounts = loreEntities.reduce((acc, e) => {
    acc[e.key] = factsFor(e, totalChapters).length;
    return acc;
  }, {});

  // source snippets are parsed by the backend in /api/progress directly (prose is not delivered with progress progress)
  function extractSnippet(entry) {
    return entry?.snippet || '';
  }

  function formatEntryLine(e) {
    const snippet = extractSnippet(e);
    if (snippet) {
      return `${e.content} (source: “${snippet}”)`;
    }
    return e.content;
  }

  async function refreshProgress() {
    try {
      progress.set(await api('GET', '/api/progress'));
      addToast($t('memory.refreshed'), 'success');
    } catch (e) {
      addToast(e.message, 'error');
    }
  }

  async function copyAll() {
    if (entries.length === 0) return;
    const text = entries.map(formatEntryLine).join('\n');
    try {
      await navigator.clipboard.writeText(text);
      addToast($t('memory.copy.done'), 'success');
    } catch (e) {
      addToast($t('memory.copy.failed'), 'error');
    }
  }
</script>

{#if viewMode === 'progression'}
  <div id="MemoryProgressionPage" class="space-y-4 page-width">
    <div class="card bg-base-200">
      <div class="card-body py-4 gap-3">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <h2 class="card-title text-base">{$t('memory.progression.title')}</h2>
          <span class="badge badge-outline">{$t('memory.progression.chapters', { n: totalChapters })}</span>
        </div>
        <p class="text-xs text-base-content/65">{$t('memory.progression.hint')}</p>
      </div>
    </div>

    {#if loreEntities.length === 0}
      <div class="card bg-base-200">
        <div class="card-body py-10 text-center gap-2">
          <p class="font-medium text-base-content/70">{$t('memory.progression.empty.title')}</p>
          <p class="text-sm text-base-content/65 max-w-lg mx-auto">{$t('memory.progression.empty.hint')}</p>
        </div>
      </div>
    {:else}
      <div class="grid grid-cols-2 gap-3">
        {#each loreEntities as entity}
          <button
            class="rounded-2xl border bg-base-100 p-4 text-left transition-colors {selectedEntity?.key === entity.key ? 'border-base-content/60' : 'border-base-300 hover:border-base-content/30'}"
            on:click={() => { selectedEntityKey = entity.key; }}
          >
            <div class="flex items-start justify-between gap-2">
              <span class="text-sm font-semibold text-base-content truncate">{entity.name}</span>
              <span class="badge badge-ghost badge-sm whitespace-nowrap capitalize">{entity.kind}</span>
            </div>
            <p class="text-[11px] text-base-content/55 truncate mt-0.5">{entity.subtitle || '—'}</p>
            <p class="text-[11px] text-base-content/60 mt-2">{$t('memory.progression.facts', { n: entityFactCounts[entity.key] || 0 })}</p>
          </button>
        {/each}
      </div>

      {#if selectedEntity}
        <div class="card bg-base-200">
          <div class="card-body py-4 gap-4">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <div class="min-w-0">
                <h3 class="text-sm font-semibold text-base-content truncate">{selectedEntity.name}</h3>
                <p class="text-[11px] text-base-content/55 truncate">{selectedEntity.subtitle || $t('memory.progression.entity')}</p>
              </div>
              <span class="badge badge-outline">{$t('memory.progression.chapterLabel', { n: selectedChapter })}</span>
            </div>

            <div class="space-y-1">
              <input
                type="range"
                class="range range-sm w-full"
                min="1"
                max={totalChapters}
                step="1"
                value={selectedChapter}
                on:input={(e) => selectedChapter = Number(e.target.value)}
                aria-label={$t('memory.progression.chapterLabel', { n: selectedChapter })}
              />
              <div class="flex justify-between text-[10px] text-base-content/45 font-mono">
                <span>1</span>
                <span>{totalChapters}</span>
              </div>
            </div>

            {#if relatedFacts.length === 0}
              <p class="text-sm text-base-content/60 py-4 text-center">{$t('memory.progression.noFacts')}</p>
            {:else}
              <div class="space-y-2">
                {#each relatedFacts as fact (fact.id)}
                  {@const chNum = factChapter(fact)}
                  <div class="rounded-xl bg-base-100 p-3 space-y-1">
                    <div class="flex items-center gap-2">
                      <span class="badge badge-sm whitespace-nowrap {categoryWash(fact.category)}">{$t('memory.category.' + (fact.category || 'other'))}</span>
                      <span class="text-[10px] font-mono text-base-content/45">{chNum > 0 ? $t('memory.progression.fromChapter', { n: chNum }) : $t('memory.progression.unanchored')}</span>
                    </div>
                    <p class="text-sm text-base-content/85">{fact.content}</p>
                    {#if fact.snippet}
                      <p class="text-[11px] text-base-content/50 whitespace-normal">「{fact.snippet}」</p>
                    {/if}
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        </div>
      {/if}
    {/if}
  </div>
{:else}
  <div id="MemoryPage" class="space-y-4 page-width">
    <div id="CardSection_MemoryOverview" class="card bg-base-200">
      <div class="card-body py-4 gap-3">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <h2 class="card-title text-base">{$t('memory.title')}</h2>
          <div class="flex flex-wrap gap-2">
            <button id="RefreshMemoryButton" class="btn btn-outline btn-sm" on:click={refreshProgress}>{$t('common.refresh')}</button>
            <button id="CopyAllMemoryButton" class="btn btn-outline btn-sm" disabled={entries.length === 0} on:click={copyAll}>
              {$t('common.copy')}
            </button>
          </div>
        </div>
        <div class="flex flex-wrap gap-2 text-sm">
          <span class="badge badge-ghost">{$t('memory.stats.total', { n: entries.length })}</span>
          {#if maxTokens > 0}
            <span class="badge badge-outline">{$t('memory.stats.budget', { n: maxTokens })}</span>
          {/if}
          {#if contentChars > 0}
            <span class="badge badge-outline">{$t('memory.stats.chars', { n: contentChars })}</span>
          {/if}
        </div>
        <p class="text-xs text-base-content/65">{$t('memory.hint')}</p>
        <p class="text-xs text-base-content/65">{$t('memory.readonly')}</p>
      </div>
    </div>

    {#if entries.length === 0}
      <div id="MemoryEmptyState" class="card bg-base-200">
        <div class="card-body py-10 text-center gap-2">
          <p class="font-medium text-base-content/70">{$t('memory.empty.title')}</p>
          <p class="text-sm text-base-content/65 max-w-lg mx-auto">{$t('memory.empty.hint')}</p>
        </div>
      </div>
    {:else}
      <div id="CardSection_MemoryEntries" class="card bg-base-200">
        <div class="card-body py-4 gap-3">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div class="flex flex-wrap gap-2">
              <!-- svelte-ignore a11y-no-redundant-roles -->
              <SmartSelect
                id="MemoryCategoryFilter"
                classes="w-56"
                options={[
                  { value: 'all', label: $t('memory.filter.allCategories') },
                  ...categoryKeys
                    .filter((key) => categoryCounts[key] > 0)
                    .map((key) => ({ value: key, label: `${$t('memory.category.' + key)} (${categoryCounts[key]})` })),
                ]}
                emptyLabel={$t('memory.filter.allCategories')}
                allowCustom={false}
                allowClear={false}
                ariaLabel={$t('memory.filter.allCategories')}
                bind:value={categoryFilter}
              />
            </div>
          </div>

          {#if filtered.length === 0}
            <p class="text-sm text-base-content/65 py-6 text-center">{$t('memory.filter.noMatch')}</p>
          {:else}
            <div class="overflow-x-auto">
              <table id="MemoryEntriesTable" class="table table-sm">
                <thead>
                  <tr>
                    <th>{$t('memory.col.id')}</th>
                    <th>{$t('memory.col.category')}</th>
                    <th>{$t('memory.col.content')}</th>
                    <th>{$t('memory.col.snippet')}</th>
                  </tr>
                </thead>
                <tbody>
                  {#each filtered as e (e.id)}
                    {@const snippet = extractSnippet(e)}
                    <tr>
                      <td class="font-mono text-xs">{e.id}</td>
                      <td class="whitespace-nowrap">
                        <!-- SneakyUI 5 badge fixed height with no nowrap；in narrow columns CJK text wraps per character into vertical stacks -->
                        <span class="badge badge-sm whitespace-nowrap {categoryWash(e.category)}">
                          {$t('memory.category.' + (e.category || 'other'))}
                        </span>
                      </td>
                      <td class="max-w-md">{e.content}</td>
                      <td class="text-xs text-base-content/60 max-w-xs whitespace-normal">
                        {#if snippet}
                          「{snippet}」
                        {:else}
                          <span class="text-base-content/30">—</span>
                        {/if}
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {/if}
        </div>
      </div>
    {/if}
  </div>
{/if}
