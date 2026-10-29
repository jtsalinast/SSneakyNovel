<script>
  import { onMount, afterUpdate } from 'svelte';
  import { api } from '../lib/api.js';
  import { renderMarkdown } from '../lib/markdown.js';
  import { chatSessions, currentChatSession, addToast, showConfirm, taskRunning, lastFailedTask, logEntries, currentTaskName, chatTurnEnd } from '../lib/stores.js';
  import { t, uiLocale, formatToolResult } from '../lib/i18n/index.js';
  import ICONS from '../lib/icons.js';
  import TaskTokenBadge from './TaskTokenBadge.svelte';

  export let contextPage = 'config';

  let chatInput = '';
  let messagesContainer;
  let inputEl;
  let showSessionList = false;
  let autoScroll = true;

  $: sessions = ($chatSessions?.sessions || []);
  $: msgs = ($currentChatSession?.messages || []);
  $: streamingText = $currentChatSession?.streaming_text || '';
  $: pendingTools = $currentChatSession?.pending_tool_calls || [];
  $: taskLogs = ($logEntries || []).slice(-20);
  let taskStatusCollapsed = false;

  // Tool cards reuse the Writing mark palette (.wash-*): read = blue,
  // generate/revise = violet, create = green, update = amber, delete = red,
  // anything else = pink. Only feature markers get colour; normal replies stay
  // neutral.
  function toolTone(name) {
    const n = String(name || '');
    if (n.startsWith('delete') || n.startsWith('reset')) return 'wash-red';
    if (n.startsWith('read') || n === 'search_project') return 'wash-blue';
    if (n.startsWith('generate') || n.startsWith('revise') || n.startsWith('suggest')) return 'wash-violet';
    if (n.startsWith('create') || n.startsWith('confirm')) return 'wash-green';
    if (n.startsWith('update') || n.startsWith('edit') || n.startsWith('toggle')) return 'wash-amber';
    return 'wash-pink';
  }
  // Tool-result rows do not persist the tool name; the call it answers is the
  // nearest previous assistant message that carried tool_calls.
  function toolNameFor(msgIdx) {
    for (let i = msgIdx - 1; i >= 0 && i >= msgIdx - 2; i--) {
      const calls = msgs[i]?.tool_calls;
      if (calls?.length) return calls[calls.length - 1].name;
    }
    return '';
  }

  function toolLabel(name) {
    if (!name) return '';
    const key = 'chat.toolNames.' + name;
    const label = $t(key);
    return label === key ? name : label;
  }
  function toolResultText(msg, key, args) {
    return formatToolResult(msg, key, args, $uiLocale);
  }
  function fmtArgs(args) {
    const s = typeof args === 'string' ? args : JSON.stringify(args);
    if (!s || s === '{}' || s === 'null') return '';
    return s.length > 300 ? s.slice(0, 300) + '...' : s;
  }
  function fmtTime(ts) {
    if (!ts) return '';
    try {
      const tag = $uiLocale === 'en' ? 'en-US' : 'zh-CN';
      return new Date(ts).toLocaleTimeString(tag, { hour12: false, hour: '2-digit', minute: '2-digit' });
    } catch { return ''; }
  }

  // Retry API endpoint mapping
  const retryEndpoints = {
    'outline_revision': { method: 'POST', url: '/api/outline/revise' },
    'chapter_generation': { method: 'POST', url: '/api/chapter/generate' },
    'chapter_revision': { method: 'POST', url: '/api/chapter/revise' },
    'foreshadow_suggest': { method: 'POST', url: '/api/foreshadows/suggest' },
    'continuation_outline': { method: 'POST', url: '/api/outline/generate-continuation' },
    'settings_reconciliation': { method: 'POST', url: '/api/settings/reconcile' },
    'knowledge_sync': { method: 'POST', url: '/api/knowledge/sync' },
  };

  function isHallucinatedWait(msg, allMsgs, idx) {
    if (msg.role !== 'assistant' || !msg.content) return false;
    if (msg.tool_calls?.length > 0) return false;
    // ponytail: heuristic on both zh and en wait-phrases; false positives are acceptable.
    const waitPattern = /please wait|generating|in progress|hold on|one moment/i;
    if (!waitPattern.test(msg.content)) return false;
    for (let i = idx - 1; i >= 0; i--) {
      if (allMsgs[i].role === 'user') break;
      if (allMsgs[i].role === 'assistant' && allMsgs[i].tool_calls?.length > 0) return false;
    }
    return true;
  }

  function parseContentSegments(text) {
    if (!text) return [{ type: 'text', content: '' }];
    const segments = [];
    const regex = /<tool_call>([\s\S]*?)<\/tool_call>|<tool_call>([\s\S]*)/g;
    let lastIdx = 0;
    let match;
    while ((match = regex.exec(text)) !== null) {
      if (match.index > lastIdx) {
        segments.push({ type: 'text', content: text.slice(lastIdx, match.index) });
      }
      const jsonStr = (match[1] || match[2] || '').trim();
      try {
        const tc = JSON.parse(jsonStr);
        segments.push({ type: 'tool_call', name: tc.name || tc.tool || $t('chat.tool.unknown'), args: tc.arguments || tc.args || {} });
      } catch {
        // Unclosed/incomplete tool_call (mid-stream): show as a tool-call placeholder
        segments.push({ type: 'tool_call', name: $t('chat.tool.preparing'), args: '' });
      }
      lastIdx = match.index + match[0].length;
    }
    if (lastIdx < text.length) {
      segments.push({ type: 'text', content: text.slice(lastIdx) });
    }
    return segments;
  }

  onMount(async () => {
    try {
      const idx = await api('GET', '/api/chat/sessions');
      chatSessions.set(idx);
      if (!$currentChatSession) {
        // Robust session pick: the index shape has varied across versions
        // ({sessions:[...]}, {items:[...]} or a bare array). Reading only one
        // shape used to leave $currentChatSession null with no visible error,
        // so programmatic sends (Generate buttons) silently did nothing.
        const list = Array.isArray(idx?.sessions) ? idx.sessions
          : Array.isArray(idx?.items) ? idx.items
          : Array.isArray(idx) ? idx : [];
        if (list.length > 0) {
          await selectSession(list[0].id);
        } else {
          await createSession();
        }
      }
      if (!$currentChatSession) addToast('Chat assistant could not open a session', 'error');
    } catch (e) {
      addToast('Chat assistant failed to load: ' + (e?.message || e), 'error');
    }
  });

  function handleScroll() {
    if (!messagesContainer) return;
    const nearBottom = messagesContainer.scrollHeight - messagesContainer.scrollTop - messagesContainer.clientHeight < 80;
    autoScroll = nearBottom;
  }

  // Scroll guard: afterUpdate fires on any store change (e.g. token count,
  // log append) and unconditionally writing scrollTop causes high-frequency forced reflow.
  // Only scroll whenthe message content actually changes。
  let lastScrollKey = '';
  afterUpdate(() => {
    const key = msgs.length + ':' + streamingText.length + ':' + pendingTools.map(t => t.status).join(',');
    if (key === lastScrollKey) return;
    lastScrollKey = key;
    if (messagesContainer && autoScroll) messagesContainer.scrollTop = messagesContainer.scrollHeight;
  });

  // Resolves when the assistant turn finishes (SSE 'chat_turn_end') so callers
  // like Config's Generate buttons can refresh their busy state. Also resolves
  // after a timeout as a safety net if the SSE event never arrives.
  let pendingChatResolve = null;

  function settleChat() {
    if (pendingChatResolve) {
      const r = pendingChatResolve;
      pendingChatResolve = null;
      r();
    }
  }

  chatTurnEnd.subscribe(() => settleChat());

  // Programmatic sends used by the Generate buttons: resolve as soon as the
  // POST is ACCEPTED (the message appears in the panel and the agent turn
  // starts server-side). Do NOT await the settle/chatTurnEnd promise here —
  // that waits for task_end SSE, which may never arrive if the browser's SSE
  // stream was opened before this session's task started or got starved; the
  // caller then hung behind a 180 s timeout and looked like a dead button.
  // Manual sends keep waiting for the full turn via their own path.
  export async function sendMessageToChat(text, opts = {}) {
    console.log('[chat] sendMessageToChat', text?.slice(0, 60), 'force=', !!opts.force);
    if (!text || !String(text).trim()) throw new Error('Empty chat message');
    // Wait until the session bootstrap (onMount) has settled: on a cold load
    // $currentChatSession is still null for a moment and createSession() here
    // used to race the bootstrap, leaving the message unsent with no feedback.
    if (!$currentChatSession) {
      const deadline = Date.now() + 5000;
      while (!$currentChatSession && Date.now() < deadline) {
        await new Promise((r) => setTimeout(r, 100));
      }
    }
    if (!$currentChatSession) {
      await createSession();
    }
    if (!$currentChatSession) throw new Error('No active chat session');
    // The message text lives in the shared input; postChatMessage() reads it
    // from there. Programmatic sends (Generate buttons) BYPASS the client-side
    // $taskRunning guard entirely: a stale-true flag used to swallow every
    // click as a silent no-op. The server is the authority and answers 409
    // for a genuinely busy backend, which surfaces as an error toast.
    chatInput = String(text);
    const sent = opts.force ? await postChatMessage() : await sendMessage();
    if (!sent) {
      // postChatMessage returns false when the textarea was empty — impossible
      // once chatInput is set above — or after it already toasted the real
      // failure (needSession / POST error). Make sure the caller always sees
      // a concrete reason instead of a silent no-op.
      chatInput = '';
      throw new Error('Chat message could not be sent');
    }
    if (opts.awaitTurn) {
      await new Promise((resolve) => {
        pendingChatResolve = resolve;
        setTimeout(() => { if (pendingChatResolve === resolve) settleChat(); }, 180000);
      });
    }
    return true;
  }

  async function createSession() {
    try {
      const session = await api('POST', '/api/chat/sessions');
      chatSessions.set(await api('GET', '/api/chat/sessions'));
      await selectSession(session.id);
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function selectSession(id) {
    try {
      const session = await api('GET', '/api/chat/sessions/' + id);
      currentChatSession.set(session);
      showSessionList = false;
      autoScroll = true;
    } catch (e) { addToast(e.message, 'error'); }
  }

  async function deleteSession(id, e) {
    e.stopPropagation();
    showConfirm($t('chat.session.deleteConfirm'), async () => {
      try {
        await api('DELETE', '/api/chat/sessions/' + id);
        chatSessions.set(await api('GET', '/api/chat/sessions'));
        if ($currentChatSession?.id === id) {
          currentChatSession.set(null);
          const updated = (await api('GET', '/api/chat/sessions')).sessions || [];
          if (updated.length > 0) await selectSession(updated[0].id);
        }
      } catch (e) { addToast(e.message, 'error'); }
    });
  }

  async function sendMessage() {
    if ($taskRunning) { addToast($t('chat.toast.taskRunning'), 'error'); return false; }
    return postChatMessage();
  }

  // Same as sendMessage() but skips the client-side $taskRunning guard.
  // Used by programmatic sends (Generate buttons): a stale flag must not
  // swallow the request — the server is the authority and answers 409 for a
  // genuinely busy backend, which surfaces as an error toast via postChatMessage.
  async function sendMessageForce() {
    return postChatMessage();
  }

  async function postChatMessage() {
    if (!$currentChatSession) { addToast($t('chat.toast.needSession'), 'error'); return false; }
    const msg = (chatInput || '').trim();
    if (!msg) return false;
    chatInput = '';
    if (inputEl) inputEl.style.height = 'auto';
    autoScroll = true;

    currentChatSession.update(s => {
      if (!s) return s;
      const messages = [...(s.messages || []), { role: 'user', content: msg, timestamp: new Date().toISOString() }];
      return { ...s, messages, streaming_text: '', pending_tool_calls: [] };
    });

    try {
      await api('POST', '/api/chat/sessions/' + $currentChatSession.id + '/messages', { content: msg, context_page: contextPage });
      return true;
    } catch (e) { addToast(e.message, 'error'); return false; }
  }

  function handleKeydown(e) {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendMessage(); }
  }

  function autoGrow() {
    if (!inputEl) return;
    inputEl.style.height = 'auto';
    inputEl.style.height = Math.min(inputEl.scrollHeight, 120) + 'px';
  }

  async function stopTask() {
    try {
      await api('POST', '/api/task/stop');
      addToast($t('chat.toast.stopping'), 'info');
    } catch (e) {}
  }

  async function retryTask() {
    const failed = $lastFailedTask;
    if (!failed) return;
    lastFailedTask.set(null);

    if (failed.task === 'chat_message') {
      if ($currentChatSession?.messages?.length > 0) {
        const lastUserMsg = [...$currentChatSession.messages].reverse().find(m => m.role === 'user');
        if (lastUserMsg) {
          chatInput = lastUserMsg.content;
          await sendMessage();
          return;
        }
      }
      addToast($t('chat.toast.retryNoMsg'), 'error');
      return;
    }

    const endpoint = retryEndpoints[failed.task];
    if (endpoint) {
      try {
        await api(endpoint.method, endpoint.url);
      } catch (e) { addToast($t('chat.toast.retryFailed', { msg: e.message }), 'error'); }
    } else {
      addToast($t('chat.toast.retryUnsupported'), 'error');
    }
  }

  $: welcomeHints = [
    $t('chat.welcome.q1'),
    $t('chat.welcome.q2'),
    $t('chat.welcome.q3'),
    $t('chat.welcome.q4'),
  ];
</script>

<div id="ChatPanelRoot" class="flex flex-col h-full bg-white">
  <!-- Session header: sessions dropdown, current title, new session -->
  <div id="ChatSessionToolbar" class="h-14 shrink-0 border-b border-[#EAEAEA] bg-white px-3 flex items-center gap-2">
    <button
      id="ChatSessionMenuToggle"
      class="flex items-center gap-2 px-3 py-1.5 rounded-lg border border-gray-200/80 bg-gray-50/60 hover:bg-gray-100/80 text-xs font-semibold text-gray-800 transition-all"
      class:border-gray-400={showSessionList}
      on:click={() => showSessionList = !showSessionList}
    >
      <svg class="w-3.5 h-3.5 text-gray-500" viewBox={ICONS.outdent.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.outdent.d} /></svg>
      <span>{$t('chat.session.menu')}</span>
      <svg class="w-2.5 h-2.5 text-gray-400 transition-transform" class:rotate-180={showSessionList} viewBox={ICONS.chevronUp.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.chevronUp.d} /></svg>
    </button>
    <div class="h-4 w-px bg-gray-200"></div>
    <span class="text-xs font-mono text-gray-400 truncate flex-1" title={$currentChatSession?.title || ''}>
      {$currentChatSession?.title || $t('chat.session.placeholder')}
    </span>
    {#if $taskRunning}
      <button id="StopTaskButton" class="px-2.5 py-1.5 rounded-lg border border-gray-200 bg-white hover:bg-gray-50 text-xs font-semibold text-gray-700 transition-all" on:click={stopTask}>{$t('chat.session.stop')}</button>
    {/if}
    <button
      id="NewChatSessionButton"
      class="flex items-center gap-1.5 px-3 py-1.5 rounded-lg border border-gray-200/80 bg-gray-50/60 hover:bg-gray-100/80 text-xs font-semibold text-gray-800 transition-all active:scale-[0.98] disabled:opacity-50"
      on:click={createSession}
      disabled={$taskRunning}
    >
      <svg class="w-2.5 h-2.5 text-gray-500" viewBox={ICONS.plus.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.plus.d} /></svg>
      <span>{$t('chat.session.new')}</span>
    </button>
  </div>

  {#if showSessionList}
    <div id="ChatSessionListDropdown" class="border-b border-[#EAEAEA] max-h-[200px] overflow-y-auto bg-white shrink-0">
      {#each sessions as s}
        <!-- svelte-ignore a11y-click-events-have-key-events -->
        <!-- svelte-ignore a11y-no-static-element-interactions -->
        <div
          id="ChatSessionItem_{s.id}"
          class="px-3 py-2 border-b border-gray-100 cursor-pointer hover:bg-gray-50 transition-colors flex items-center gap-2 group"
          class:bg-gray-50={$currentChatSession?.id === s.id}
          on:click={() => selectSession(s.id)}
        >
          <div class="flex-1 min-w-0">
            <div class="text-xs font-medium truncate text-gray-800">{s.title}</div>
            <div class="text-[10px] font-mono text-gray-400">{new Date(s.updated_at).toLocaleString($uiLocale === 'en' ? 'en-US' : 'zh-CN')} · {$t('chat.session.msgs', { n: s.msg_count || 0 })}</div>
          </div>
          <button class="px-2 py-1 rounded-md border border-red-200 text-[10px] font-semibold text-red-600 hover:bg-red-50 opacity-0 group-hover:opacity-100 transition-opacity" on:click={(e) => deleteSession(s.id, e)}>{$t('common.delete')}</button>
        </div>
      {/each}
      {#if sessions.length === 0}
        <div class="px-3 py-2 text-xs text-gray-400">{$t('chat.session.empty')}</div>
      {/if}
    </div>
  {/if}

  <!-- Task status -->
  {#if $taskRunning || taskLogs.length > 0}
    <div id="ChatTaskStatusPanel" class="border-b border-[#EAEAEA] shrink-0 bg-white">
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div class="px-3 py-1.5 cursor-pointer hover:bg-gray-50/80" on:click={() => taskStatusCollapsed = !taskStatusCollapsed}>
        <div class="flex items-center gap-2">
          {#if $taskRunning}
            <span class="loading loading-spinner loading-xs text-amber-500"></span>
          {:else}
            <span class="text-gray-500 text-xs">●</span>
          {/if}
          <span class="text-[11px] font-semibold text-gray-600">{$currentTaskName || $t('chat.task.placeholder')}{$taskRunning ? $t('chat.task.running') : $t('chat.task.ended')}</span>
          <span class="text-[10px] text-gray-400 ml-auto">{$t(taskStatusCollapsed ? 'chat.task.expand' : 'chat.task.collapse')}</span>
        </div>
        {#if $taskRunning}
          <div class="mt-1 pl-5"><TaskTokenBadge className="badge badge-xs badge-outline gap-1 font-mono whitespace-nowrap" /></div>
        {/if}
      </div>
      {#if !taskStatusCollapsed && taskLogs.length > 0}
        <div id="ChatTaskLogList" class="max-h-[150px] overflow-y-auto px-3 py-1 font-mono text-[10px] leading-relaxed space-y-0.5">
          {#each taskLogs as entry}
            <div class="flex gap-2">
              <span class="text-gray-300 shrink-0">{entry.time}</span>
              <span class={entry.level === 'error' ? 'text-red-600' : entry.level === 'warn' ? 'text-amber-600' : entry.level === 'success' ? 'text-gray-700' : 'text-gray-500'}>{entry.msg}</span>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}

  <!-- Messages -->
  <div id="ChatMessagesArea" bind:this={messagesContainer} on:scroll={handleScroll} class="flex-1 overflow-y-auto p-4 space-y-5 bg-[#FAFAFA]/50">
    {#if !$currentChatSession}
      <div id="ChatNoSessionState" class="text-center text-gray-400 py-8 text-xs">{$t('chat.notSelected')}</div>
    {:else}
      {#if msgs.length === 0 && !streamingText}
        <div id="ChatWelcomeState" class="text-center text-gray-500 py-10 space-y-3">
          <div class="text-3xl">✳</div>
          <p class="text-xs leading-relaxed px-4">{$t('chat.welcome.hint')}</p>
          <div class="flex flex-wrap justify-center gap-1.5 px-4">
            {#each welcomeHints as hint}
              <button class="px-3 py-1.5 rounded-lg border border-gray-200 bg-white hover:bg-gray-50 text-[11px] text-gray-600 transition-colors" on:click={() => { chatInput = hint; sendMessage(); }}>{hint}</button>
            {/each}
          </div>
        </div>
      {/if}
      {#each msgs as m, msgIdx}
        {#if m.role === 'user'}
          <div class="flex flex-col items-end group">
            <div class="bubble-user max-w-[85%] px-4 py-2.5 rounded-2xl text-[13px] leading-relaxed whitespace-pre-wrap">{m.content}</div>
            <div class="mt-1 mr-1 flex items-center gap-1.5 opacity-60 group-hover:opacity-100 transition-opacity">
              <span class="text-[10px] font-mono text-gray-400">{fmtTime(m.timestamp)}</span>
              <svg class="w-2.5 h-2.5 text-gray-400" viewBox={ICONS.accept.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.accept.d} /></svg>
            </div>
          </div>
        {:else if m.role === 'assistant'}
          {#if m.tool_calls?.length > 0}
            {#each m.tool_calls as tc}
              <div class="flex flex-col items-start group">
                <div class="bubble-tool max-w-[85%] p-3 rounded-2xl text-xs font-mono">
                  <div class="inline-flex items-center gap-2 font-bold mb-1.5 px-2 py-1 rounded-md {toolTone(tc.name)}">
                    <svg class="w-3 h-3" viewBox={ICONS.config.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.config.d} /></svg>
                    <span>{toolLabel(tc.name)}</span>
                  </div>
                  {#if fmtArgs(tc.arguments)}
                    <div class="bg-white/80 border border-gray-200/50 p-2 rounded-lg text-gray-600 text-[11px] overflow-x-auto whitespace-pre-wrap break-all">{fmtArgs(tc.arguments)}</div>
                  {/if}
                </div>
                <span class="mt-1 ml-1 text-[10px] font-mono text-gray-400 opacity-60 group-hover:opacity-100 transition-opacity">{$t('chat.tool.call')}</span>
              </div>
            {/each}
          {/if}
          {#if m.content}
            {#if isHallucinatedWait(m, msgs, msgIdx)}
              <div class="flex flex-col items-start group">
                <div class="bubble-tool max-w-[85%] px-4 py-3 rounded-2xl text-[13px] leading-relaxed">
                  <div class="text-amber-600 font-semibold mb-1 text-xs">{$t('chat.assistant.maybeNoop')}</div>
                  <div class="text-gray-700 md-body">{@html renderMarkdown(m.content)}</div>
                </div>
              </div>
            {:else}
              {#each parseContentSegments(m.content) as seg}
                {#if seg.type === 'tool_call'}
                  {#if !m.tool_calls?.length}
                    <div class="flex flex-col items-start group">
                      <div class="bubble-tool max-w-[85%] p-3 rounded-2xl text-xs font-mono">
                        <div class="inline-flex items-center gap-2 font-bold mb-1.5 px-2 py-1 rounded-md {toolTone(seg.name)}">
                          <svg class="w-3 h-3" viewBox={ICONS.config.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.config.d} /></svg>
                          <span>{toolLabel(seg.name)}</span>
                        </div>
                        {#if fmtArgs(seg.args)}
                          <div class="bg-white/80 border border-gray-200/50 p-2 rounded-lg text-gray-600 text-[11px] overflow-x-auto whitespace-pre-wrap break-all">{fmtArgs(seg.args)}</div>
                        {/if}
                      </div>
                      <span class="mt-1 ml-1 text-[10px] font-mono text-gray-400 opacity-60 group-hover:opacity-100 transition-opacity">{$t('chat.tool.call')}</span>
                    </div>
                  {/if}
                {:else if seg.content.trim()}
                  <div class="flex flex-col items-start group">
                    <div class="bubble-ai max-w-[88%] px-4 py-3 rounded-2xl text-[13px] text-gray-800 leading-relaxed md-body">{@html renderMarkdown(seg.content.trim())}</div>
                    <div class="mt-1 ml-1 flex items-center gap-2 opacity-60 group-hover:opacity-100 transition-opacity">
                      <span class="text-[10px] font-medium text-gray-500">{$t('chat.assistant.name')}</span>
                      <span class="text-[9px] text-gray-300">•</span>
                      <span class="text-[10px] font-mono text-gray-400">{fmtTime(m.timestamp)}</span>
                    </div>
                  </div>
                {/if}
              {/each}
            {/if}
          {/if}
        {:else if m.role === 'tool'}
          <div class="flex flex-col items-start group">
            <details class="tool-result bubble-tool max-w-[85%] rounded-2xl text-xs font-mono overflow-hidden">
              <summary class="flex items-center gap-2 px-3 py-2 cursor-pointer select-none text-gray-600 hover:text-gray-900 hover:bg-gray-200/50 transition-colors">
                <svg class="caret w-2.5 h-2.5 text-gray-400 transition-transform" viewBox={ICONS.arrowRight.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.arrowRight.d} /></svg>
                <span class="font-medium text-[11px] px-1.5 py-0.5 rounded {toolTone(toolNameFor(msgIdx))}">{$t('chat.tool.result')}</span>
              </summary>
              <div class="p-3 bg-white text-[11px] text-gray-500 font-mono max-h-32 overflow-y-auto whitespace-pre-wrap break-all">{toolResultText(m.tool_result, m.tool_result_key, m.tool_result_args)}</div>
            </details>
          </div>
        {/if}
      {/each}

      {#each pendingTools as tc}
        <div class="flex flex-col items-start group">
          <div class="bubble-tool max-w-[85%] p-3 rounded-2xl text-xs font-mono">
            <div class="inline-flex items-center gap-2 font-bold mb-1 px-2 py-1 rounded-md {toolTone(tc.name)}">
              <svg class="w-3 h-3" viewBox={ICONS.config.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.config.d} /></svg>
              <span>{toolLabel(tc.name)}</span>
            </div>
            {#if tc.status === 'running'}
              <div class="text-amber-600 animate-pulse text-[11px]">{$t('chat.tool.running')}</div>
            {:else if tc.result}
              <div class="bg-white/80 border border-gray-200/50 p-2 rounded-lg text-gray-600 text-[11px] max-h-20 overflow-y-auto whitespace-pre-wrap break-all">{tc.result.length > 200 ? tc.result.slice(0, 200) + '...' : tc.result}</div>
            {/if}
          </div>
          <span class="mt-1 ml-1 text-[10px] font-mono text-gray-400 opacity-60 group-hover:opacity-100 transition-opacity">{$t('chat.tool.call')}</span>
        </div>
      {/each}

      {#if streamingText}
        {#each parseContentSegments(streamingText) as seg}
          {#if seg.type === 'tool_call'}
            <div class="flex flex-col items-start group">
              <div class="bubble-tool max-w-[85%] p-3 rounded-2xl text-xs font-mono">
                <div class="inline-flex items-center gap-2 font-bold mb-1.5 px-2 py-1 rounded-md {toolTone(seg.name)}">
                  <svg class="w-3 h-3" viewBox={ICONS.config.vb} fill="currentColor" aria-hidden="true"><path d={ICONS.config.d} /></svg>
                  <span>{toolLabel(seg.name)}</span>
                </div>
                {#if fmtArgs(seg.args)}
                  <div class="bg-white/80 border border-gray-200/50 p-2 rounded-lg text-gray-600 text-[11px] overflow-x-auto whitespace-pre-wrap break-all">{fmtArgs(seg.args)}</div>
                {/if}
              </div>
              <span class="mt-1 ml-1 text-[10px] font-mono text-gray-400 opacity-60 group-hover:opacity-100 transition-opacity">{$t('chat.tool.call')}</span>
            </div>
          {:else if seg.content.trim()}
            <div class="flex flex-col items-start group">
              <div class="bubble-ai max-w-[88%] px-4 py-3 rounded-2xl text-[13px] text-gray-800 leading-relaxed md-body"><span class="md-body">{@html renderMarkdown(seg.content.trim())}</span><span class="inline-block w-1.5 h-3.5 bg-gray-500/70 animate-pulse ml-0.5 align-text-bottom"></span></div>
              <div class="mt-1 ml-1 flex items-center gap-2 opacity-60">
                <span class="text-[10px] font-medium text-gray-500">{$t('chat.assistant.name')}</span>
              </div>
            </div>
          {/if}
        {/each}
      {/if}
    {/if}
  </div>

  <!-- Failed-task retry -->
  {#if $lastFailedTask && !$taskRunning}
    <div id="ChatFailedTaskBanner" class="border-t border-red-200 bg-red-50 px-3 py-2 flex items-center gap-2 shrink-0">
      <span class="text-[11px] text-red-700">{$lastFailedTask.taskName}{$t('chat.failed.suffix')}</span>
      <div class="flex-1"></div>
      <button id="RetryTaskButton" class="px-2.5 py-1 rounded-md border border-red-300 bg-white text-[11px] font-semibold text-red-700 hover:bg-red-100 transition-colors" on:click={retryTask}>{$t('chat.failed.retry')}</button>
      <button id="IgnoreFailedTaskButton" class="px-2.5 py-1 rounded-md text-[11px] text-gray-500 hover:text-gray-800 transition-colors" on:click={() => lastFailedTask.set(null)}>{$t('chat.failed.ignore')}</button>
    </div>
  {/if}

  <!-- Input area -->
  {#if $currentChatSession}
    <div id="ChatInputArea" class="shrink-0 border-t border-[#EAEAEA] bg-white p-3">
      <div class="relative flex items-end rounded-xl border border-gray-200 focus-within:border-gray-400 focus-within:ring-2 focus-within:ring-gray-100 transition-all">
        <textarea
          id="ChatMessageInput"
          bind:this={inputEl}
          class="w-full pl-3.5 pr-24 py-3 bg-transparent text-sm text-gray-800 placeholder-gray-400 focus:outline-none resize-none max-h-32 leading-relaxed"
          bind:value={chatInput}
          placeholder={$taskRunning ? $t('chat.input.placeholderBusy') : $t('chat.input.placeholder')}
          on:keydown={handleKeydown}
          on:input={autoGrow}
          disabled={$taskRunning}
        ></textarea>
        <div class="absolute right-2 bottom-2 flex items-center gap-1.5">
          <button
            id="SendMessageButton"
            class="px-3.5 py-1.5 bg-black hover:bg-gray-800 active:scale-95 text-white text-xs font-medium rounded-lg transition-all disabled:opacity-40 disabled:pointer-events-none"
            on:click={sendMessage}
            disabled={$taskRunning || !chatInput.trim()}
          >{$t('chat.input.send')}</button>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  /* Chat bubbles: white user / grey assistant, with the small "tail" corner
     from the studio mockup. Flat by design (no shadows). */
  .bubble-user {
    background-color: #fff;
    color: #18181b;
    border: 1px solid #eaeaea;
    border-bottom-right-radius: 4px;
  }
  .bubble-ai,
  .bubble-tool {
    background-color: #f4f4f5;
    border-bottom-left-radius: 4px;
  }
  .tool-result[open] .caret {
    transform: rotate(90deg);
  }
  /* The global :focus-visible ring is coral; inside the chat input the wrapper
     already shows focus, so the textarea must not draw a second line. */
  #ChatMessageInput:focus-visible {
    outline: none;
  }
</style>
