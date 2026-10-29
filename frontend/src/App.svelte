<script>
  import { currentPage, navigate } from './lib/router.js';
  import { progress, taskRunning, contextPage, toastStore, addToast, currentProject, projectLanguage, config, settings, chatSessions, currentChatSession, logEntries } from './lib/stores.js';
  import { connectSSE } from './lib/sse.js';
  import { api } from './lib/api.js';
  import { ribbon } from './lib/ribbon.js';
  import { readLastPage, writeLastPage } from './lib/lastPage.js';
  import { writingToolbar } from './lib/toolbar.js';
  import ICONS from './lib/icons.js';
  import { onMount, onDestroy } from 'svelte';
  import { t, uiLocale } from './lib/i18n/index.js';
  import TaskTokenBadge from './components/TaskTokenBadge.svelte';
  import Projects from './pages/Projects.svelte';
  import Config from './pages/Config.svelte';
  import Outline from './pages/Outline.svelte';
  import Writing from './pages/Writing.svelte';
  import Proofread from './pages/Proofread.svelte';
  import Relations from './pages/Relations.svelte';
  import Skills from './pages/Skills.svelte';
  import Foreshadows from './pages/Foreshadows.svelte';
  import Memory from './pages/Memory.svelte';
  import ChatPanel from './components/ChatPanel.svelte';
  import BrainstormPanel from './components/BrainstormPanel.svelte';
  import ConfirmModal from './components/ConfirmModal.svelte';
  import StorageErrorModal from './components/StorageErrorModal.svelte';

  // Navigation groups: World building / Composing / Analysis / General. The
  // General group (Config + Skills) sits last, after the work itself. Icons
  // are inline 24px strokes so the rail needs no icon-font dependency and
  // inherits currentColor.
  const NAV_GROUPS = [
    {
      labelKey: 'nav.group.world',
      items: [
        { page: 'novel-params', labelKey: 'nav.novelParams', icon: 'novelParams' },
        { page: 'lore', labelKey: 'nav.lore', icon: 'lore' },
      ],
    },
    {
      labelKey: 'nav.group.composing',
      items: [
        { page: 'outline', labelKey: 'nav.outline', icon: 'outline' },
        { page: 'foreshadows', labelKey: 'nav.foreshadows', icon: 'foreshadows' },
        { page: 'writing', labelKey: 'nav.writing', icon: 'writing' },
      ],
    },
    {
      labelKey: 'nav.group.analysis',
      items: [
        { page: 'proofread', labelKey: 'nav.proofread', icon: 'proofread' },
        { page: 'memory', labelKey: 'nav.memory', icon: 'memory' },
        { page: 'relations', labelKey: 'nav.relations', icon: 'relations' },
      ],
    },
    {
      labelKey: 'nav.group.general',
      items: [
        { page: 'config', labelKey: 'nav.config', icon: 'config' },
        { page: 'skills', labelKey: 'nav.skills', icon: 'skills' },
      ],
    },
  ];

  $: pageMeta = NAV_GROUPS.flatMap(g => g.items.map(item => ({ ...item, groupKey: g.labelKey }))).find(item => item.page === $currentPage);
  $: ribbonData = $ribbon && $ribbon.page === $currentPage ? $ribbon : null;
  $: ribbonSteps = ribbonData?.steps?.length ? ribbonData.steps : (pageMeta ? [{ id: 'main', label: $t('app.ribbon.hint.' + pageMeta.page) }] : []);
  $: activeRibbonStep = ribbonData?.active ?? 'main';
  $: ribbonStepIndex = Math.max(1, ribbonSteps.findIndex(step => step.id === activeRibbonStep) + 1);
  $: ribbonProgress = ribbonSteps.length ? Math.round((ribbonStepIndex / ribbonSteps.length) * 100) : 0;
  $: breadcrumb = pageMeta ? `${$t(pageMeta.groupKey)} / ${$t(pageMeta.labelKey)}` : '';
  $: toolbarVisible = !!$currentProject;
  function selectRibbonStep(step) {
    if (ribbonData?.onSelect) ribbonData.onSelect(step.id);
    requestAnimationFrame(() => document.getElementById('WorkspaceMain')?.scrollTo({ top: 0, behavior: 'smooth' }));
  }

  // Shell toolbar actions. The writing page registers real handlers; the rest
  // of the mockup tools are shown but explain that the editor does not support
  // them yet instead of pretending to work.
  function toolbarUnavailable(tool) {
    addToast($t('app.toolbar.unavailable', { tool }), 'info');
  }
  function triggerToolbarSave() {
    const save = $writingToolbar.save;
    if (save) save();
    else addToast($t('app.toolbar.nothingToSave'), 'info');
  }
  function triggerNewScene() {
    const add = $writingToolbar.addScene;
    if (add) add();
    else toolbarUnavailable($t('app.toolbar.newScene'));
  }
  function triggerAcceptAll() {
    const accept = $writingToolbar.accept;
    if (accept) accept();
    else toolbarUnavailable($t('app.toolbar.acceptAll'));
  }
  function toggleAgentPanel() {
    const wasOther = logOpen || brainstormOpen;
    logOpen = false;
    brainstormOpen = false;
    if (window.matchMedia('(min-width: 1280px)').matches) {
      // Coming from another panel, show the agent instead of toggling it shut.
      if (wasOther) setChatCollapsed(false);
      else toggleChatCollapsed();
    } else if (assistantOpen && !wasOther) closeAssistant();
    else openAssistant();
  }
  // Log and Brainstorm share the assistant's lateral slot: opening either one
  // closes the others so they never stack.
  function toggleLogPanel() {
    const open = !logOpen;
    logOpen = open;
    brainstormOpen = false;
    if (open) setChatCollapsed(true);
  }
  function closeLogPanel() { logOpen = false; }
  function toggleBrainstormPanel() {
    const open = !brainstormOpen;
    brainstormOpen = open;
    logOpen = false;
    // Collapse the agent too: closing this panel must leave the slot empty,
    // not resurrect the agent the author had open before.
    if (open) setChatCollapsed(true);
  }
  function closeBrainstormPanel() { brainstormOpen = false; }

  let chatPanel;
  let initializing = true;
  let restoring = false;
  let restoreTimer;
  let destroyed = false;
  let navigationOpen = false;
  let assistantOpen = false;
  let logOpen = false;
  let brainstormOpen = false;
  let agentButton;
  // Collapsible lateral bar (desktop): persisted so the choice survives reloads.
  // Guarded for SSR / non-browser contexts.
  function readPersistedFlag(key) {
    try { return typeof localStorage !== 'undefined' && localStorage.getItem(key) === '1'; } catch (_) { return false; }
  }
  let navCollapsed = readPersistedFlag('navCollapsed');
  $: navToggleIcon = navCollapsed ? ICONS.indent : ICONS.outdent;
  function toggleNavCollapsed() {
    navCollapsed = !navCollapsed;
    try { localStorage.setItem('navCollapsed', navCollapsed ? '1' : '0'); } catch (_) {}
  }
  // Chat panel collapses into an "agent" pill docked at the bottom-right of
  // the chat column width (desktop only). Also persisted.
  let chatCollapsed = readPersistedFlag('chatCollapsed');
  function setChatCollapsed(value) {
    chatCollapsed = value;
    try { localStorage.setItem('chatCollapsed', chatCollapsed ? '1' : '0'); } catch (_) {}
    if (chatCollapsed) assistantOpen = false;
  }
  function toggleChatCollapsed() {
    setChatCollapsed(!chatCollapsed);
  }

  $: if ($taskRunning && !$currentProject) restoreCurrentProject();

  // Remember the last page per project. The first activation honors the URL
  // (deep link or restored project); later switches use that project's saved
  // page and otherwise fall back to Config instead of inheriting the page the
  // author was on in the previous project.
  let lastPageProject = null;
  let pendingPage = null;
  let lastPageBootstrapped = false;
  $: if (!$currentProject && lastPageProject !== null) {
    lastPageProject = null;
    pendingPage = null;
  }
  $: if ($currentProject && $currentProject !== lastPageProject) {
    lastPageProject = $currentProject;
    // While a task runs the shell stays on Writing, so do not queue a page
    // restore that would never be navigated.
    pendingPage = $taskRunning ? null : (readLastPage($currentProject) || (lastPageBootstrapped ? 'config' : null));
    lastPageBootstrapped = true;
    if (pendingPage) navigate(pendingPage);
  }
  // Wait for the pending navigation to land before writing, so the previous
  // project's page is never stored under the new project.
  $: if ($currentProject && $currentProject === lastPageProject && !$taskRunning) {
    if (!pendingPage || $currentPage === pendingPage) {
      pendingPage = null;
      writeLastPage($currentProject, $currentPage);
    }
  }

  onDestroy(() => { destroyed = true; clearTimeout(restoreTimer); });

  async function restoreCurrentProject() {
    if (restoring || destroyed) return;
    restoring = true;
    try {
      const cur = await api('GET', '/api/projects/current');
      if (destroyed) return;
      if (cur.name && cur.name !== $currentProject) {
        config.set(null);
        progress.set(null);
        settings.set(null);
        chatSessions.set([]);
        currentChatSession.set(null);
        currentProject.set(cur.name);
        initializing = false;
        if (cur.language) {
          projectLanguage.set(cur.language);
        }
        if ($taskRunning) navigate('writing');
        await Promise.allSettled([
          api('GET', '/api/config').then(config.set),
          api('GET', '/api/progress').then(progress.set),
          api('GET', '/api/settings').then(settings.set),
          api('GET', '/api/chat/sessions').then(chatSessions.set),
        ]);
      }
      initializing = false;
    } catch (_) {
      clearTimeout(restoreTimer);
      if (!destroyed) restoreTimer = setTimeout(restoreCurrentProject, 2000);
    } finally {
      restoring = false;
    }
  }

  let appVersion = '';
  let latestVersion = '';
  let hasUpdate = false;
  const latestReleaseURL = 'https://github.com/Nigh/show-me-the-story/releases/latest';

  $: $contextPage = $currentPage;

  onMount(async () => {
    restoreCurrentProject();
    connectSSE();
    // Fetch app version
    try {
      const ver = await api('GET', '/api/version');
      appVersion = ver.version || 'dev';
    } catch (e) {}
    // Check for updates (skip for dev builds)
    if (appVersion && appVersion !== 'dev') {
      try {
        const resp = await fetch('https://api.github.com/repos/Nigh/show-me-the-story/releases/latest');
        if (resp.ok) {
          const data = await resp.json();
          latestVersion = data.tag_name || '';
          if (latestVersion && latestVersion !== appVersion) {
            hasUpdate = true;
          }
        }
      } catch (e) {}
    }
  });

  $: phase = $progress
    ? ($progress.phase === 'outline' ? $t('app.phase.outline')
        : $progress.phase === 'writing' ? $t('app.phase.writing')
        : $progress.phase)
    : $t('app.phase.unstarted');

  // Programmatic chat invocation used by Generate buttons outside the panel.
  // Every rejection path THROWS so callers can surface the failure and clear
  // their busy flags — a silent no-op here was the "nothing happens" bug:
  // the click marked the section busy, nothing reached the chat, and the UI
  // hung until a manual refresh.
  async function sendToChat(text) {
    console.log('[chat] sendToChat invoked', text?.slice(0, 60));
    if (!chatPanel || typeof chatPanel.sendMessageToChat !== 'function') {
      throw new Error('Chat assistant is not ready yet');
    }
    // Make the turn visible: on narrow screens the panel lives in a drawer
    // that starts closed, and the user otherwise sees a busy button with no
    // chat activity at all.
    assistantOpen = true;
    // The POST /messages endpoint ACKNOWLEDGES the request immediately (the
    // agent turn runs server-side in background goroutines) — it never blocks
    // until the turn ends. So awaiting here resolves as soon as the message is
    // accepted and shows up in the panel. Awaiting the full turn instead (the
    // old behavior via sendMessageToChat's internal settle promise) meant this
    // call hung for minutes behind SSE/event-loop timing, which looked exactly
    // like "the button does nothing".
    const ok = await watchSendToChat(chatPanel.sendMessageToChat(text, { force: true }));
    console.log('[chat] sendToChat result', ok);
    if (!ok) throw new Error('Chat message could not be sent');
  }

  // Last-resort visibility net for programmatic chat sends (Generate buttons).
  // If sendMessageToChat never resolves — e.g. the POST hangs because the
  // browser is still holding an open SSE connection to the same origin and
  // Chrome's per-host connection cap starves the request — the click would
  // otherwise look like a total no-op: no chat activity, no toast, spinner
  // forever. Surface exactly what happened instead of staying silent.
  function watchSendToChat(promise, section) {
    const label = section ? ` [${section}]` : '';
    const timer = setTimeout(() => {
      addToast($t('app.chat.sendPending') + label, 'info');
    }, 8000);
    promise
      .catch((e) => {
        addToast(($t('app.chat.sendFailed') || 'Chat send failed') + label + ': ' + (e?.message || String(e)), 'error');
      })
      .finally(() => clearTimeout(timer));
    return promise;
  }

  async function backToProjects() {
    if ($taskRunning || restoring) return;
    try {
      const status = await api('GET', '/api/status');
      if (status.is_task_running || $taskRunning) {
        taskRunning.set(true);
        return;
      }
    } catch (_) { return; }
    currentProject.set(null);
    config.set(null);
  }

  function openAssistant() {
    assistantOpen = true;
    requestAnimationFrame(() => document.querySelector('#AgentChat button')?.focus());
  }
  function closeNavigation() {
    if (!navigationOpen) return;
    navigationOpen = false;
  }
  function closeAssistant() {
    if (!assistantOpen) return;
    assistantOpen = false;
    requestAnimationFrame(() => agentButton?.focus());
  }
  function goTo(page) {
    window.location.hash = '#' + page;
    closeNavigation();
  }
  function handleShellKeydown(event) {
    if (event.key === 'Escape') { closeNavigation(); closeAssistant(); closeLogPanel(); closeBrainstormPanel(); return; }
    if (event.key !== 'Tab') return;
    const panel = document.getElementById(navigationOpen ? 'LateralBar' : logOpen ? 'ShellLogPanel' : brainstormOpen ? 'BrainstormPanel' : assistantOpen ? 'AgentChat' : '');
    if (!panel) return;
    const controls = [...panel.querySelectorAll('button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])')];
    if (!controls.length) return;
    const first = controls[0], last = controls[controls.length - 1];
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
  }
</script>

<svelte:window on:keydown={handleShellKeydown} />

<div class="app-shell relative flex bg-base-100 text-base-content overflow-hidden">
  {#if $currentProject}
    <!-- Full-height navigation rail (mockup: starts at the very top) -->
    <nav id="LateralBar" class:drawer-open={navigationOpen} class:nav-collapsed={navCollapsed} class="app-navigation flex flex-col shrink-0 bg-white" aria-label={$t('app.navigation')}>
      <div class="nav-brand-row">
        <div class="nav-brand">
          <svg class="nav-brand-icon" viewBox={ICONS.brand.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.brand.d} /></svg>
          <span class="nav-brand-label nav-item-label">{$t('app.brand')}</span>
        </div>
        <button class="btn btn-ghost btn-xs lg:hidden" on:click={closeNavigation} aria-label={$t('common.close')}>
          <svg class="w-4 h-4" viewBox={ICONS.reject.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.reject.d} /></svg>
        </button>
      </div>

      <div class="nav-spacer" aria-hidden="true"></div>

      <div class="nav-scroll">
        <button
          class="nav-collapse-btn hidden lg:flex"
          on:click={toggleNavCollapsed}
          aria-expanded={!navCollapsed}
          aria-controls="LateralBar"
          title={navCollapsed ? $t('app.navigation.expand') : $t('app.navigation.collapse')}
        >
          <svg class="w-3 h-3" viewBox={navToggleIcon.vb} fill="currentColor" aria-hidden="true"><path d={navToggleIcon.d} /></svg>
        </button>

        {#each NAV_GROUPS as group}
          <div class="nav-group">
            <div class="nav-section-title nav-item-label">{$t(group.labelKey)}</div>
            <div class="space-y-0.5">
              {#each group.items as item}
                <button
                  class="nav-item"
                  class:active={$currentPage === item.page}
                  aria-current={$currentPage === item.page ? 'page' : undefined}
                  title={$t(item.labelKey)}
                  on:click={() => goTo(item.page)}
                >
                  <span class="nav-icon" aria-hidden="true">
                    <svg viewBox={ICONS[item.icon].vb} fill="currentColor"><path d={ICONS[item.icon].d} /></svg>
                  </span>
                  <span class="nav-item-label">{$t(item.labelKey)}</span>
                </button>
              {/each}
            </div>
          </div>
        {/each}
      </div>

      <div class="nav-footer">
        <span class="nav-footer-badge">{$projectLanguage === 'en' ? 'EN' : 'ZH'}</span>
        <div class="nav-footer-text nav-item-label">
          <p class="nav-footer-title">{appVersion || 'dev'}</p>
          <p class="nav-footer-sub">{phase}</p>
        </div>
      </div>
    </nav>
  {/if}

  <div class="app-main relative flex flex-col flex-1 min-w-0 overflow-hidden">
  {#if $currentProject && (navigationOpen || assistantOpen || logOpen)}
    <button class="workspace-scrim" aria-label={$t('common.close')} on:click={() => { if (navigationOpen) closeNavigation(); if (assistantOpen) closeAssistant(); if (logOpen) closeLogPanel(); }}></button>
  {/if}

  <!-- Shell header: project bar + ribbon (section steps) -->
  <header id="TopBar" class="app-shell-header shrink-0 bg-white border-b border-[#EAEAEA]">
    <div id="ProjectBar" class="project-bar">
      {#if hasUpdate}
        <a href={latestReleaseURL} target="_blank" rel="noopener" class="badge badge-xs badge-warning gap-0.5 no-underline max-sm:hidden">
          {$t('app.newVersion')}
        </a>
      {/if}
      <div class="project-bar-center">
        <svg class="w-2.5 h-2.5 shrink-0 text-gray-400" viewBox={ICONS.folder.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.folder.d} /></svg>
        <span class="project-title-wrap">
          <span class="app-project-title">{$currentProject || $t('app.title')}</span>
          {#if $currentProject}
            <button
              class="project-switch-btn"
              on:click={backToProjects}
              disabled={$taskRunning || restoring}
              title={$taskRunning ? $t('app.switchProject.disabled') : $t('app.switchProject.tooltip')}
            >
              {$t('app.switchProject')}
            </button>
          {/if}
        </span>
      </div>
    </div>

    {#if $currentProject}
      <div id="RibbonBar" class="ribbon-bar">
        <div class="ribbon-breadcrumb" title={breadcrumb}>{breadcrumb}</div>
        <div id="RibbonTabs" class="ribbon-tabs" role="tablist" aria-label={breadcrumb}>
          {#each ribbonSteps as step, i (step.id)}
            <button
              type="button"
              role="tab"
              aria-selected={step.id === activeRibbonStep}
              class="ribbon-tab"
              class:active={step.id === activeRibbonStep}
              on:click={() => selectRibbonStep(step)}
              title={step.label}
            >
              {#if ribbonSteps.length > 1}<span class="tab-num">{i + 1}</span>{/if}
              <span class="truncate">{step.label}</span>
            </button>
          {/each}
        </div>
        <div class="ribbon-token" title={$taskRunning ? $t('app.aiThinking') : $t('app.aiIdle')}>
          <span class="ribbon-token-dot" class:is-running={$taskRunning}></span>
          <span class="font-sans font-medium text-gray-500">{$taskRunning ? $t('app.aiThinking') : $t('app.aiIdle')}</span>
          {#if $taskRunning}
            <span class="text-gray-300">|</span>
            <TaskTokenBadge className="font-mono" />
          {/if}
        </div>
      </div>
      <div class="ribbon-progress" aria-hidden="true">
        <div id="RibbonProgressBar" style="width: {ribbonProgress}%"></div>
      </div>

      {#if toolbarVisible}
        <!-- Step-aware toolbar (mockup row 3): save · tools · panels -->
        <div id="ShellToolbar" class="h-10 bg-white flex items-center justify-between px-3.5 shrink-0 z-20">
          <div class="flex items-center gap-1.5 min-w-[120px]">
            <button class="group relative p-2 hover:bg-gray-100 rounded-md text-gray-500 hover:text-black transition-colors" on:click={triggerToolbarSave} title={$t('app.toolbar.saveTitle')} aria-label={$t('app.toolbar.saveTitle')}>
              <svg class="w-4 h-4" viewBox={ICONS.save.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.save.d} /></svg>
              <span class="absolute left-0 top-full mt-1.5 hidden group-hover:block bg-gray-800 text-white text-[10px] py-0.5 px-2 rounded whitespace-nowrap z-50">{$t('app.toolbar.save')}</span>
            </button>
          </div>

          <div class="hidden md:flex items-center justify-center min-w-0">
            {#if $currentPage === 'writing'}
            {#if activeRibbonStep === 'scene'}
              <div id="ToolsSceneMatrix" class="flex items-center gap-3 text-xs">
                <button class="flex items-center gap-1.5 px-2.5 py-1 hover:bg-gray-100 rounded text-gray-700 font-medium disabled:opacity-40 disabled:pointer-events-none" on:click={triggerNewScene} disabled={!$writingToolbar.addScene}>
                  <svg class="w-3 h-3" viewBox={ICONS.plus.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.plus.d} /></svg>
                  {$t('app.toolbar.newScene')}
                </button>
                <button class="flex items-center gap-1.5 px-2.5 py-1 hover:bg-gray-100 rounded text-gray-700 font-medium" on:click={() => toolbarUnavailable($t('app.toolbar.reorderPov'))}>
                  <svg class="w-3 h-3" viewBox={ICONS.reorderPov.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.reorderPov.d} /></svg>
                  {$t('app.toolbar.reorderPov')}
                </button>
                <button class="flex items-center gap-1.5 px-2.5 py-1 hover:bg-gray-100 rounded text-gray-700 font-medium" on:click={() => toolbarUnavailable($t('app.toolbar.filterChronology'))}>
                  <svg class="w-3 h-3" viewBox={ICONS.filter.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.filter.d} /></svg>
                  {$t('app.toolbar.filterChronology')}
                </button>
              </div>
            {:else if activeRibbonStep === 'writing'}
              <div id="ToolsChapterWriting" class="flex items-center gap-3 text-xs">
                <div class="flex items-center gap-1 border-r border-gray-200 pr-3">
                  <button class="w-7 h-7 hover:bg-gray-100 rounded text-gray-700 font-bold flex items-center justify-center text-sm" title={$t('app.toolbar.bold')} on:click={() => toolbarUnavailable($t('app.toolbar.bold'))}>B</button>
                  <button class="w-7 h-7 hover:bg-gray-100 rounded text-gray-700 italic font-serif flex items-center justify-center text-sm" title={$t('app.toolbar.italic')} on:click={() => toolbarUnavailable($t('app.toolbar.italic'))}>I</button>
                  <button class="w-7 h-7 hover:bg-gray-100 rounded text-gray-700 underline flex items-center justify-center text-sm" title={$t('app.toolbar.underline')} on:click={() => toolbarUnavailable($t('app.toolbar.underline'))}>U</button>
                  <button class="w-7 h-7 hover:bg-gray-100 rounded text-gray-700 line-through flex items-center justify-center text-sm" title={$t('app.toolbar.strike')} on:click={() => toolbarUnavailable($t('app.toolbar.strike'))}>S</button>
                </div>
                <div class="flex items-center gap-1.5">
                  <button class="px-2 py-1 hover:bg-gray-100 rounded text-gray-700 font-mono text-xs font-bold" title={$t('app.toolbar.h1')} on:click={() => toolbarUnavailable($t('app.toolbar.h1'))}>H1</button>
                  <button class="px-2 py-1 hover:bg-gray-100 rounded text-gray-700 font-mono text-xs font-bold" title={$t('app.toolbar.h2')} on:click={() => toolbarUnavailable($t('app.toolbar.h2'))}>H2</button>
                  <button class="px-2 py-1 hover:bg-gray-100 rounded text-gray-700 font-medium" on:click={() => toolbarUnavailable($t('app.toolbar.quote'))}>{$t('app.toolbar.quote')}</button>
                </div>
              </div>
            {:else}
              <div id="ToolsReview" class="flex items-center gap-3 text-xs">
                <button class="flex items-center gap-1.5 px-2.5 py-1 hover:bg-gray-100 rounded text-gray-700 font-medium disabled:opacity-40 disabled:pointer-events-none" on:click={triggerAcceptAll} disabled={!$writingToolbar.accept}>
                  <svg class="w-3 h-3" viewBox={ICONS.accept.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.accept.d} /></svg>
                  {$t('app.toolbar.acceptAll')}
                </button>
                <button class="flex items-center gap-1.5 px-2.5 py-1 hover:bg-gray-100 rounded text-gray-700 font-medium" on:click={() => toolbarUnavailable($t('app.toolbar.reject'))}>
                  <svg class="w-3 h-3" viewBox={ICONS.reject.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.reject.d} /></svg>
                  {$t('app.toolbar.reject')}
                </button>
                <button class="flex items-center gap-1.5 px-2.5 py-1 hover:bg-gray-100 rounded text-gray-700 font-medium" on:click={() => toolbarUnavailable($t('app.toolbar.comments'))}>
                  <svg class="w-3 h-3" viewBox={ICONS.comments.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.comments.d} /></svg>
                  {$t('app.toolbar.comments')}
                </button>
              </div>
            {/if}
            {/if}
          </div>

          <div class="flex items-center justify-end gap-1.5 min-w-[120px]">
            <button bind:this={agentButton} class="group relative p-2 hover:bg-gray-100 rounded-md text-gray-600 hover:text-black transition-colors" class:bg-gray-100={assistantOpen} on:click={toggleAgentPanel} title={$t('app.toolbar.agent')} aria-label={$t('app.toolbar.agent')} aria-expanded={assistantOpen} aria-controls="AgentChat">
              <svg class="w-4 h-4" viewBox={ICONS.agent.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.agent.d} /></svg>
              <span class="absolute right-0 top-full mt-1.5 hidden group-hover:block bg-gray-800 text-white text-[10px] py-0.5 px-2 rounded whitespace-nowrap z-50">{$t('app.toolbar.agentLabel')}</span>
            </button>
            <button class="group relative p-2 hover:bg-gray-100 rounded-md text-gray-600 hover:text-black transition-colors" class:bg-gray-100={brainstormOpen} on:click={toggleBrainstormPanel} title={$t('app.toolbar.brainstorm')} aria-label={$t('app.toolbar.brainstorm')} aria-expanded={brainstormOpen} aria-controls="BrainstormPanel">
              <svg class="w-4 h-4" viewBox={ICONS.brainstorm.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.brainstorm.d} /></svg>
              <span class="absolute right-0 top-full mt-1.5 hidden group-hover:block bg-gray-800 text-white text-[10px] py-0.5 px-2 rounded whitespace-nowrap z-50">{$t('app.toolbar.brainstormLabel')}</span>
            </button>
            <button class="group relative p-2 hover:bg-gray-100 rounded-md text-gray-600 hover:text-black transition-colors" class:bg-gray-100={logOpen} on:click={toggleLogPanel} title={$t('app.toolbar.log')} aria-label={$t('app.toolbar.log')} aria-expanded={logOpen} aria-controls="ShellLogPanel">
              <svg class="w-4 h-4" viewBox={ICONS.log.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.log.d} /></svg>
              <span class="absolute right-0 top-full mt-1.5 hidden group-hover:block bg-gray-800 text-white text-[10px] py-0.5 px-2 rounded whitespace-nowrap z-50">{$t('app.toolbar.logLabel')}</span>
            </button>
          </div>
        </div>
      {/if}
    {/if}
  </header>

  {#if initializing || ($taskRunning && !$currentProject)}
    <main id="AppLoadingScreen" class="flex-1 flex items-center justify-center" aria-busy="true">
      <span class="loading loading-spinner loading-lg"></span>
    </main>
  {:else if !$currentProject}
    <!-- Project selection -->
    <main id="ProjectPickerMain" class="project-picker flex-1 overflow-y-auto p-4 sm:p-8">
      <Projects />
    </main>
  {:else}
    <div class="workspace flex flex-1 overflow-hidden">
      <!-- Center: page content -->
      <main id="WorkspaceMain" class="workspace-main @container flex-[2] min-w-0 overflow-y-auto p-3 sm:p-5 xl:border-r xl:border-base-content/10">
        {#if $currentPage === 'config'}
          <Config tab="api" {sendToChat} />
        {:else if $currentPage === 'novel-params'}
          <Config tab="novelParams" {sendToChat} />
        {:else if $currentPage === 'lore'}
          <Config tab="lore" {sendToChat} />
        {:else if $currentPage === 'outline'}
          <Outline {sendToChat} />
        {:else if $currentPage === 'writing'}
          <Writing />
        {:else if $currentPage === 'proofread'}
          <Proofread />
        {:else if $currentPage === 'foreshadows'}
          <Foreshadows />
        {:else if $currentPage === 'memory'}
          <Memory />
        {:else if $currentPage === 'relations'}
          <Relations />
        {:else if $currentPage === 'skills'}
          <Skills />
        {/if}
      </main>

      <!-- Right: Chat Panel (toggled from the shell toolbar's Agent button) -->
      <aside id="AgentChat" class:drawer-open={assistantOpen} class:chat-collapsed={chatCollapsed || logOpen || brainstormOpen} class="assistant-panel relative flex-1 min-w-72 max-w-md bg-base-200 overflow-hidden" aria-label={$t('app.assistant')}>
        <button class="btn btn-ghost btn-sm m-2 mb-0 xl:hidden" on:click={closeAssistant}>{$t('common.close')}</button>
        <div class="chat-panel-body h-full">
          <ChatPanel bind:this={chatPanel} contextPage={$currentPage} />
        </div>
      </aside>

      <!-- Right: brainstorming partner on the second model, same lateral slot -->
      <aside id="BrainstormPanel" class:drawer-open={brainstormOpen} class:chat-collapsed={!brainstormOpen} class="assistant-panel relative flex-1 min-w-72 max-w-md bg-base-200 overflow-hidden" aria-label={$t('app.toolbar.brainstormLabel')}>
        <div class="chat-panel-body h-full">
          <BrainstormPanel onClose={closeBrainstormPanel} />
        </div>
      </aside>

      <!-- Right: full task log, same lateral slot as the assistant panel -->
      <aside id="ShellLogPanel" class:drawer-open={logOpen} class:chat-collapsed={!logOpen} class="assistant-panel relative flex-1 min-w-72 max-w-md bg-base-200 overflow-hidden" aria-label={$t('app.toolbar.logLabel')}>
        <div class="chat-panel-body h-full flex flex-col">
          <div class="flex items-center justify-between gap-2 px-4 py-3 border-b border-[#EAEAEA] shrink-0">
            <h2 class="text-sm font-semibold text-gray-900">{$t('app.toolbar.logLabel')}</h2>
            <button class="p-1.5 rounded-md text-gray-500 hover:bg-gray-100 hover:text-black transition-colors" on:click={closeLogPanel} title={$t('common.close')} aria-label={$t('common.close')}>
              <svg class="w-3.5 h-3.5" viewBox={ICONS.reject.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.reject.d} /></svg>
            </button>
          </div>
          <div id="ShellLogList" class="flex-1 overflow-y-auto font-mono text-xs leading-relaxed space-y-1 p-4">
            {#each $logEntries as entry}
              <div class="flex gap-2">
                <span class="text-base-content/30 shrink-0">{entry.time}</span>
                <span class={entry.level === 'error' ? 'text-error' : entry.level === 'warn' ? 'text-warning' : entry.level === 'success' ? 'text-success' : 'text-base-content/60'}>{entry.msg}</span>
              </div>
            {:else}
              <p class="font-sans text-sm text-base-content/60">{$t('app.log.empty')}</p>
            {/each}
          </div>
        </div>
      </aside>
    </div>
  {/if}
  </div>

  <!-- Toasts -->
  <div id="ToastNotifications" class="fixed top-5 right-5 z-50 flex flex-col gap-2">
    {#each $toastStore as t (t.id)}
      <div class="alert alert-sm {t.type === 'success' ? 'wash-green' : t.type === 'error' ? 'wash-red' : t.type === 'warning' ? 'wash-amber' : 'wash-blue'} toast-enter  max-w-sm">
        <span>{t.msg}</span>
      </div>
    {/each}
  </div>

  <ConfirmModal />
  <StorageErrorModal />
</div>
