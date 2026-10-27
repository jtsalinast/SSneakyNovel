import { getLocale, translate, translateServerMessage } from './i18n/index.js';
import { storageError } from './stores.js';

// In-flight GET dedup. While an LLM task runs, the SSE stream fires bursts of
// progress_update / tool_call_end events; each one used to launch its own
// GET /api/progress (and /api/config, /api/settings...). Those identical
// requests queued behind the busy server on the same HTTP/1 origin and
// starved the browser's per-host connection pool — chapter content loads and
// button-triggered POSTs then stalled forever ("los botones dejan de servir",
// "capítulo cargando sin mostrar info"). Identical in-flight GETs now share a
// single promise instead of stacking duplicates.
const inflightGets = new Map();

export function apiFetch(url, opts = {}) {
  const method = (opts.method || 'GET').toUpperCase();
  if (method === 'GET') {
    const existing = inflightGets.get(url);
    if (existing) return existing;
    const p = apiFetchOnce(url, opts).finally(() => inflightGets.delete(url));
    inflightGets.set(url, p);
    return p;
  }
  return apiFetchOnce(url, opts);
}

async function apiFetchOnce(url, opts) {
  const locale = getLocale();
  // Timeout guard: a hung request must eventually settle so spinners and
  // disabled buttons recover. The default is deliberately generous for cheap
  // GETs (they may sit queued behind long LLM work on this single-origin
  // HTTP/1 server — aborting them early just triggers retry storms that make
  // the saturation worse). Callers that are latency-critical or genuinely
  // long-running pass an explicit `timeoutMs`.
  const timeoutMs = opts.timeoutMs ?? 120000;
  let signal = opts.signal;
  let timer = null;
  let timedOut = false;
  if (!signal && timeoutMs > 0) {
    const ctrl = new AbortController();
    signal = ctrl.signal;
    timer = setTimeout(() => { timedOut = true; ctrl.abort(); }, timeoutMs);
  }
  let r;
  try {
    r = await fetch(url, {
      ...opts,
      signal,
      headers: {
        ...opts.headers,
        'X-UI-Locale': locale,
        'Accept-Language': locale === 'en' ? 'en-US,en;q=0.9' : 'zh-CN,zh;q=0.9',
      },
    });
  } catch (e) {
    if (timedOut) throw new Error(`request_timeout:${url}`);
    throw e;
  } finally {
    if (timer) clearTimeout(timer);
  }
  // Clear the in-flight marker as soon as headers arrive so the NEXT caller
  // starts a fresh request instead of sharing a possibly stale/cached body.
  if ((opts.method || 'GET').toUpperCase() === 'GET') inflightGets.delete(url);
  // noBody: the caller consumes the Response itself (blob/text downloads).
  const noBody = opts.noBody === true;
  let data;
  if (!r.ok) {
    try { data = await r.json(); } catch (_) { data = {}; }
    const raw = data.error || translate('common.requestFailed', null, locale);
    if (data.code === 'storage_save_failed' && data.storage_error) {
      storageError.set(data.storage_error);
    }
    // Backend mostly responds in Chinese today; translate known strings on the client.
    throw new Error(translateServerMessage(raw, locale));
  }
  if (noBody) return r;
  // Cache the parsed JSON per Response and drain the body eagerly, so every
  // deduped GET settles and releases its connection immediately. A second
  // `.json()` on an already-consumed body would throw "body stream already
  // read"; callers therefore share the cached parse below.
  const parsed = r.json().catch(e => ({ __apiParseError: e }));
  jsonCache.set(r, parsed);
  return r;
}

// GET responses are parsed to a fresh object per caller (the shared in-flight
// promise above would otherwise hand the SAME object to several consumers).
const jsonCache = new WeakMap();

export async function api(method, url, body, headers = {}, timeoutMs) {
  const opts = { method, headers: { 'Content-Type': 'application/json', ...headers } };
  if (body) opts.body = JSON.stringify(body);
  if (timeoutMs !== undefined) opts.timeoutMs = timeoutMs;
  const r = await apiFetch(url, opts);
  let p = jsonCache.get(r);
  if (p === undefined) {
    // Non-deduped path (POST/PUT/DELETE): parse directly.
    return r.json();
  }
  p = await p;
  if (p && p.__apiParseError) throw p.__apiParseError;
  return p;
}
