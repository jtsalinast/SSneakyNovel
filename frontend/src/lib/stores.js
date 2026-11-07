import { writable } from 'svelte/store';

export const apiConfig = writable(null);
export const config = writable(null);
export const progress = writable(null);
export const settings = writable(null);
export const skills = writable([]);
export const taskRunning = writable(false);
export const currentTaskName = writable(null);

export const currentProject = writable(null);
export const projects = writable([]);

// Language of the currently loaded novel project (immutable per project).
// Drives AI prompt language, generated prose language and built-in skill filter.
export const projectLanguage = writable('en');

export const contextPage = writable('config');
export const selectedChapter = writable(-1);

export const streamingContent = writable('');
export const streamingChapterIdx = writable(-1);
// Cumulative tokens for the current task (sent/received), updated via SSE token_usage
export const taskTokenUsage = writable(null);
// Live tokens for the current Brainstorm request set (dedicated SSE channel).
export const brainstormTokens = writable(null);
// Writing's selection toolbar asks the Brainstorm panel to open with a preset
// action/text. nonce makes each request distinct even with equal payloads.
export const brainstormIntent = writable(null);
// Auto-confirm mode: each chapter auto-confirms after generation, then the next begins
export const autoConfirm = writable(false);

export const chatSessions = writable(null);
export const currentChatSession = writable(null);

export const editingCharID = writable(null);
export const editingWvID = writable(null);
export const wvFilter = writable('all');

export const logEntries = writable([]);

// Retry info: records the last failed task
export const lastFailedTask = writable(null);

// LLM connection test result: { ok, model?, error?, snapshot }; snapshot is the tested config JSON.
// Stored in a global store so it survives page switches；when form content and snapshot disagree the UI hides it automatically。
export const apiTestResult = writable(null);

export function addLog(entry) {
  logEntries.update(entries => {
    const next = [...entries, entry];
    return next.length > 500 ? next.slice(-500) : next;
  });
}

export function addToast(msg, type = 'info') {
  const id = Date.now();
  toastStore.update(t => [...t, { id, msg, type }]);
  setTimeout(() => {
    toastStore.update(t => t.filter(x => x.id !== id));
  }, 3000);
}

export const toastStore = writable([]);

export const confirmModal = writable(null);

// Structured unrecoverable save failure from HTTP or background-task SSE.
export const storageError = writable(null);

export const postprocess = writable(null);

export const foreshadowSuggestions = writable([]);
export const foreshadowShowSuggestions = writable(false);

export const outlineCharacterSuggestions = writable([]);
export const outlineCharacterShowSuggestions = writable(false);

// Latest beta-reader progress event ({act_no, stage, index, total, detail}).
// The Writing act-review checklist reads it; it is cleared when a new task
// starts so a stale act can never show another act's progress.
export const betaProgress = writable(null);

export const pendingConfigChanges = writable([]);
export const showConfigChangePanel = writable(false);

// Bumped by the SSE layer when a chat assistant turn finishes (task_end for
// 'chat_message'). ChatPanel subscribes to resolve sendMessageToChat() callers.
export const chatTurnEnd = writable(0);

export function showConfirm(message, onConfirm) {
  confirmModal.set({ message, onConfirm });
}
