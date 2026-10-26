import { get } from 'svelte/store';
import { addLog, addToast, config, progress, skills, taskRunning, streamingContent, streamingChapterIdx, taskTokenUsage, currentChatSession, settings, chatSessions, lastFailedTask, currentTaskName, logEntries, postprocess, foreshadowSuggestions, foreshadowShowSuggestions, outlineCharacterSuggestions, outlineCharacterShowSuggestions, pendingConfigChanges, showConfigChangePanel, storageError, chatTurnEnd } from './stores.js';
import { api } from './api.js';
import { getLocale, translate, formatLogEntry, formatToolResult } from './i18n/index.js';
import { TOKEN_POLL_INTERVAL_MS } from './tokenPoll.js';

let eventSource = null;
let reconnectTimer = null;
let tokenPollTimer = null;
let taskEventVersion = 0;
let statusRequest = 0;

// —— Streaming-output throttle buffer + tail window ——
const FLUSH_INTERVAL = 150;
const TAIL_MAX = 3000;

let contentBuf = '';
let contentFull = '';
let contentIdx = -1;
let contentTimer = null;

function flushContentBuf() {
  if (contentTimer) { clearTimeout(contentTimer); contentTimer = null; }
  if (!contentBuf) return;
  const text = contentBuf;
  contentBuf = '';
  contentFull += text;
  streamingChapterIdx.set(contentIdx);
  streamingContent.set(contentFull.length > TAIL_MAX ? contentFull.slice(-TAIL_MAX) : contentFull);
}

function resetContentStream(idx) {
  contentBuf = '';
  contentFull = '';
  if (contentTimer) { clearTimeout(contentTimer); contentTimer = null; }
  contentIdx = idx;
  streamingChapterIdx.set(idx);
  streamingContent.set('');
}

let progressFetchTimer = null;

// Concurrency guard for /api/progress. During long LLM tasks the backend
// emits progress_update events in bursts (each SaveProgress/broadcast); every
// event used to fire an independent GET, and those requests queued behind the
// busy server on the same origin. With HTTP/1 that saturates the browser's
// per-host connection pool: chapter content loads and button-triggered calls
// stall forever ("los botones dejan de servir", "capítulo cargando sin
// mostrar info"). Now at most one fetch is ever in flight and further
// refreshes coalesce into a single trailing fetch.
let progressInFlight = false;
let progressQueuedImmediate = false;

function doRefreshProgress() {
  if (progressInFlight) {
    // An immediate request must not be lost just because a poll is running:
    // schedule exactly one trailing refresh when it settles.
    if (!progressFetchTimer) {
      progressFetchTimer = setTimeout(() => {
        progressFetchTimer = null;
        doRefreshProgress();
      }, 500);
    }
    return;
  }
  progressInFlight = true;
  api('GET', '/api/progress')
    .then(p => progress.set(p))
    .catch(() => {})
    .finally(() => {
      progressInFlight = false;
      if (progressQueuedImmediate) {
        progressQueuedImmediate = false;
        doRefreshProgress();
      }
    });
}

function refreshProgress(immediate = false) {
  if (immediate) {
    if (progressFetchTimer) { clearTimeout(progressFetchTimer); progressFetchTimer = null; }
    if (progressInFlight) { progressQueuedImmediate = true; return; }
    doRefreshProgress();
    return;
  }
  if (progressFetchTimer) return;
  progressFetchTimer = setTimeout(() => {
    progressFetchTimer = null;
    doRefreshProgress();
  }, 500);
}

function refreshTokenUsage() {
  if (!get(taskRunning)) return;
  syncTaskFromStatus();
}

function startTokenPoll() {
  stopTokenPoll();
  refreshTokenUsage();
  tokenPollTimer = setInterval(refreshTokenUsage, TOKEN_POLL_INTERVAL_MS);
}

function stopTokenPoll() {
  if (tokenPollTimer) { clearInterval(tokenPollTimer); tokenPollTimer = null; }
}

let chatBuf = '';
let chatSessionId = null;
let chatTimer = null;

function flushChatBuf() {
  if (chatTimer) { clearTimeout(chatTimer); chatTimer = null; }
  if (!chatBuf) return;
  const text = chatBuf;
  const sid = chatSessionId;
  chatBuf = '';
  currentChatSession.update(s => {
    if (!s || s.id !== sid) return s;
    return { ...s, streaming_text: (s.streaming_text || '') + text };
  });
}

function clearChatBuf() {
  chatBuf = '';
  if (chatTimer) { clearTimeout(chatTimer); chatTimer = null; }
}

async function syncTaskFromStatus() {
  const version = taskEventVersion;
  const request = ++statusRequest;
  try {
    const s = await api('GET', '/api/status');
    // A newer event or request makes this snapshot obsolete.
    if (version !== taskEventVersion || request !== statusRequest) return;
    if (s?.is_task_running) {
      taskRunning.set(true);
      if (s.token_usage) taskTokenUsage.set(s.token_usage);
      if (s.current_task) {
        currentTaskName.set(translate(`task.${s.current_task}`) || s.current_task);
      }
      if (!tokenPollTimer) tokenPollTimer = setInterval(refreshTokenUsage, TOKEN_POLL_INTERVAL_MS);
    } else {
      const wasRunning = get(taskRunning);
      taskRunning.set(false);
      currentTaskName.set(null);
      taskTokenUsage.set(null);
      resetContentStream(-1);
      clearChatBuf();
      stopTokenPoll();
      if (wasRunning) {
        refreshProgress(true);
        api('GET', '/api/settings').then(settings.set).catch(() => {});
        api('GET', '/api/chat/sessions').then(chatSessions.set).catch(() => {});
        const session = get(currentChatSession);
        if (session) api('GET', '/api/chat/sessions/' + session.id).then(s => {
          if (get(currentChatSession)?.id === session.id) currentChatSession.set(s);
        }).catch(() => {});
      }
    }
  } catch (_) { /* ignore */ }
}

// Safety net against a stuck "task running" state. The UI disables most action
// buttons while `taskRunning` is true, so if the SSE connection drops exactly
// when the backend emits `task_end` — or the broadcaster's TaskStart/TaskEnd
// pairing ever desyncs — the whole interface would freeze ("si interactúo
// mucho dejan de servir los botones"). While we believe a task runs, poll
// /api/status periodically: it reports the server-side truth (activeWork
// counter), so a missed task_end is healed within one interval instead of
// lasting until a manual page reload.
const STUCK_TASK_CHECK_INTERVAL_MS = 5000;
let stuckTaskTimer = null;

function startStuckTaskWatch() {
  if (stuckTaskTimer) return;
  stuckTaskTimer = setInterval(() => {
    if (!get(taskRunning)) {
      clearInterval(stuckTaskTimer);
      stuckTaskTimer = null;
      return;
    }
    syncTaskFromStatus();
  }, STUCK_TASK_CHECK_INTERVAL_MS);
}

export function connectSSE() {
  if (eventSource) eventSource.close();
  const locale = getLocale();
  eventSource = new EventSource(`/api/events?locale=${encodeURIComponent(locale)}`);

  eventSource.addEventListener('open', () => {
    syncTaskFromStatus();
  });

  eventSource.addEventListener('error', () => {
    // The browser auto-reconnects EventSource; meanwhile keep reconciling the
    // task state from /api/status so buttons never stay disabled during the gap.
    if (get(taskRunning)) startStuckTaskWatch();
  });

  eventSource.addEventListener('log', e => {
    const d = JSON.parse(e.data);
    d.msg = formatLogEntry(d, locale);
    addLog(d);
  });

  eventSource.addEventListener('progress_update', () => {
    refreshProgress();
  });

  eventSource.addEventListener('storage_error', e => {
    storageError.set(JSON.parse(e.data));
  });

  function taskLabel(task) {
    return translate(`task.${task}`) || task;
  }

  eventSource.addEventListener('task_start', e => {
    const d = JSON.parse(e.data);
    taskEventVersion++;
    taskRunning.set(true);
    // Reconcile against /api/status while this task runs: if its `task_end`
    // is ever missed (SSE blip, reconnect), the watch clears the stuck
    // "running" state that otherwise disables every action button.
    startStuckTaskWatch();
    resetContentStream(-1);
    clearChatBuf();
    taskTokenUsage.set(null);
    currentTaskName.set(taskLabel(d.task));
    logEntries.set([]);
    lastFailedTask.set(null);
    // A new task started while a sendMessageToChat() caller (Generate button)
    // is waiting: the chat turn itself is over — the agent invoked its tool and
    // the background generation took over. Unblock the caller so it can switch
    // to progress polling instead of hanging until the safety timeout.
    chatTurnEnd.update(n => n + 1);
    startTokenPoll();
  });

  eventSource.addEventListener('task_end', e => {
    const d = JSON.parse(e.data);
    taskEventVersion++;
    syncTaskFromStatus();
    refreshProgress(true);

    if (d.success) {
      const name = taskLabel(d.task);
      addToast(translate('toast.taskDone', { name }), 'success');
    } else if (d.task === 'chapter_generation') {
      api('GET', '/api/progress').then(p => {
        progress.set(p);
        if (!p?.pending_writing_conflict) {
          lastFailedTask.set({ task: d.task, taskName: taskLabel(d.task) });
        }
      }).catch(() => {
        lastFailedTask.set({ task: d.task, taskName: taskLabel(d.task) });
      });
    } else {
      lastFailedTask.set({ task: d.task, taskName: taskLabel(d.task) });
    }

      if (d.task === 'proofread_analyze' || d.task === 'proofread_apply') {
        api('GET', '/api/proofread').then(p => postprocess.set({ book_complete: true, state: p })).catch(() => {});
    }

    if (d.task === 'skill_validation' || d.task === 'skill_optimization') {
      api('GET', '/api/skill-library').then(s => skills.set(s)).catch(() => {});
    }

    if (d.task === 'chat_message') {
      // Notify ChatPanel so sendMessageToChat() callers (Generate buttons)
      // can unblock once the assistant turn is over.
      chatTurnEnd.update(n => n + 1);
      // async tools may still have subtasks；must cancel here chat_chunk delayed flush，
      // otherwise the reloaded messages and streaming_text would each show the same reply twice.
      clearChatBuf();
      let sessionId = null;
      currentChatSession.update(s => {
        if (!s) return s;
        sessionId = s.id;
        return { ...s, streaming_text: '' };
      });
      if (sessionId) {
        api('GET', '/api/chat/sessions/' + sessionId).then(s => {
          currentChatSession.set(s);
        }).catch(() => {});
      }
      api('GET', '/api/chat/sessions').then(s => chatSessions.set(s)).catch(() => {});
      api('GET', '/api/config').then(c => config.set(c)).catch(() => {});
      api('GET', '/api/settings').then(s => settings.set(s)).catch(() => {});
    }
  });

  eventSource.addEventListener('token_usage', e => {
    const d = JSON.parse(e.data);
    taskTokenUsage.set(d);
  });

  eventSource.addEventListener('stream_start', e => {
    const d = JSON.parse(e.data);
    resetContentStream(d.chapter_idx);
  });

  eventSource.addEventListener('content_chunk', e => {
    const d = JSON.parse(e.data);
    if (d.chapter_idx !== contentIdx) {
      flushContentBuf();
      resetContentStream(d.chapter_idx);
    }
    contentBuf += d.text;
    if (!contentTimer) contentTimer = setTimeout(flushContentBuf, FLUSH_INTERVAL);
  });

  eventSource.addEventListener('settings_reconciled', e => {
    const d = JSON.parse(e.data);
    api('GET', '/api/config').then(c => {
      config.set(c);
    }).catch(() => {});
    api('GET', '/api/progress').then(p => progress.set(p)).catch(() => {});
    addToast(translate('toast.settingsReconciled', { detail: d.explanation || '' }), 'success');
  });

  eventSource.addEventListener('settings_updated', () => {
    api('GET', '/api/settings').then(s => settings.set(s)).catch(() => {});
    api('GET', '/api/config').then(c => config.set(c)).catch(() => {});
  });

  eventSource.addEventListener('foreshadow_suggestions', e => {
    const d = JSON.parse(e.data);
    const items = (d || []).map(s => ({ ...s, _selected: true }));
    foreshadowSuggestions.set(items);
    foreshadowShowSuggestions.set(true);
    addToast(translate('toast.foreshadowReady', { n: items.length }), 'info');
  });

  eventSource.addEventListener('outline_character_suggestions', e => {
    const d = JSON.parse(e.data);
    const items = (d || []).map(s => ({ ...s, _selected: true }));
    outlineCharacterSuggestions.set(items);
    outlineCharacterShowSuggestions.set(true);
    addToast(translate('toast.outlineCharacterReady', { n: items.length }), 'info');
  });

  eventSource.addEventListener('config_change_proposal', e => {
    const d = JSON.parse(e.data);
    const items = (d || []).map(c => ({ ...c, _selected: true }));
    pendingConfigChanges.set(items);
    showConfigChangePanel.set(items.length > 0);
    if (items.length > 0) {
      addToast(translate('toast.configChangeProposal', { n: items.length }), 'info');
    }
  });

  eventSource.addEventListener('foreshadow_outline_conflicts', e => {
    const d = JSON.parse(e.data);
    refreshProgress(true);
    addToast(translate('toast.foreshadowOutlineConflict', { n: (d.conflicts || []).length }), 'warning');
  });

  eventSource.addEventListener('writing_conflict', e => {
    const d = JSON.parse(e.data);
    refreshProgress(true);
    addToast(translate('toast.writingConflict'), 'warning');
  });

  eventSource.addEventListener('chat_chunk', e => {
    const d = JSON.parse(e.data);
    if (d.session_id !== chatSessionId) {
      flushChatBuf();
      chatSessionId = d.session_id;
    }
    chatBuf += d.text;
    if (!chatTimer) chatTimer = setTimeout(flushChatBuf, FLUSH_INTERVAL);
  });

  eventSource.addEventListener('tool_call_start', e => {
    const d = JSON.parse(e.data);
    flushChatBuf();
    currentChatSession.update(s => {
      if (!s) return s;
      const toolCalls = [...(s.pending_tool_calls || []), { name: d.tool_name, status: 'running', args: d.args }];
      return { ...s, pending_tool_calls: toolCalls };
    });
  });

  eventSource.addEventListener('tool_call_end', e => {
    const d = JSON.parse(e.data);
    currentChatSession.update(s => {
      if (!s) return s;
      const display = formatToolResult(d.result, d.result_key, d.result_args, locale);
      const toolCalls = (s.pending_tool_calls || []).map(tc =>
        tc.name === d.tool_name && tc.status === 'running'
          ? { ...tc, status: 'done', result: display, result_key: d.result_key, result_args: d.result_args }
          : tc
      );
      return { ...s, pending_tool_calls: toolCalls };
    });
    api('GET', '/api/config').then(c => config.set(c)).catch(() => {});
    api('GET', '/api/settings').then(s => settings.set(s)).catch(() => {});
    api('GET', '/api/progress').then(p => progress.set(p)).catch(() => {});
  });

  eventSource.onerror = () => {
    eventSource.close();
    clearTimeout(reconnectTimer);
    reconnectTimer = setTimeout(connectSSE, 3000);
  };
}
