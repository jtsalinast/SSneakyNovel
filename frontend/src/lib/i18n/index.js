// English-only UI catalog. The former Chinese locale was removed; uiLocale is
// kept as a store so components that subscribe to it keep working unchanged.
import { writable, derived, get } from 'svelte/store';
import en from './en.js';

const catalogs = { en };

export const uiLocale = writable('en');

if (typeof document !== 'undefined') {
  try { document.documentElement.lang = 'en'; } catch (e) {}
}

export function setLocale(_lang) {
  // No-op: the UI ships English only. Kept for call-site compatibility.
}

export function getLocale() {
  return get(uiLocale);
}

function interpolatePositional(template, args) {
  if (!args || !args.length) return template;
  return template.replace(/\{(\d+)\}/g, (_, i) => {
    const idx = Number(i);
    return idx < args.length ? String(args[idx]) : `{${i}}`;
  });
}

function lookup(lang, key) {
  const dict = catalogs[lang] || catalogs.en;
  if (Object.prototype.hasOwnProperty.call(dict, key)) return dict[key];
  return key;
}

function interpolateNamed(template, params) {
  if (!params) return template;
  return template.replace(/\{(\w+)\}/g, (_, name) =>
    Object.prototype.hasOwnProperty.call(params, name) ? String(params[name]) : `{${name}}`
  );
}

// Reactive translator. Use in components as `$t('some.key', {name: 'X'})`.
export const t = derived(uiLocale, ($lang) => (key, params) =>
  interpolateNamed(lookup($lang, key), params)
);

// Imperative translate (use outside Svelte components, e.g. in sse.js / api.js).
export function translate(key, params, lang) {
  return interpolateNamed(lookup(lang || get(uiLocale), key), params);
}

// Format server-keyed messages (log.* / agent.*) with positional args {0},{1},…
export function formatKeyedMessage(key, args, lang) {
  return interpolatePositional(lookup(lang || get(uiLocale), key), args || []);
}

export function formatLogEntry(entry, lang) {
  if (!entry) return '';
  const locale = lang || get(uiLocale);
  if (entry.msg_key) {
    return formatKeyedMessage(entry.msg_key, entry.msg_args, locale);
  }
  if (locale === 'en') {
    return entry.msg_en || translateServerMessage(entry.msg, 'en');
  }
  return entry.msg;
}

export function formatToolResult(msg, key, args, lang) {
  if (key) return formatKeyedMessage(key, args, lang);
  return msg || '';
}

// translateServerMessage kept as identity: the backend now emits English-only messages.
export function translateServerMessage(msg, _lang) {
  return msg == null ? msg : String(msg);
}
