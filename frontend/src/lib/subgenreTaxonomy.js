// subgenreTaxonomy.js — Round 19: SINGLE SOURCE OF TRUTH for the genre ->
// subgenre cascade. Written from scratch (per user request) as a pure,
// dependency-free module so its logic can be unit-tested in isolation and is
// immune to Svelte reactivity subtleties. The old inline filtering code in
// Config.svelte remains in the file but is no longer used; everything the UI
// renders now flows through this module.
//
// Contract:
//   * The backend (/api/novel-params) ships ALL parameters UNFILTERED.
//   * buildSubgenreIndex(payload) merges every source into one table that
//     assigns each subgenre EXACTLY ONE canonical parent genre key.
//   * subgenresForKeys(index, keys) returns ONLY the subgenres whose canonical
//     parent is among `keys`. Entries whose parent cannot be resolved are
//     DROPPED when a real genre is selected (they never "belong everywhere" —
//     that fallback was the original bug that made Fantasy subgenres appear
//     under Mystery & Thriller).
//   * resolveParent() understands display labels ("Cozy Mystery"), snake_case
//     tokens ("cozy_mystery") and descriptive token suffixes
//     ("locked_room_impossible_crime" -> "Locked Room").

import { GENRES, SUBGENRE_DATA } from './novelwriterGenres.js';

export const norm = (s) =>
  String(s || '').trim().toLowerCase().replace(/[_-]+/g, ' ').replace(/\s+/g, ' ');

const titleCase = (s) =>
  String(s || '').replace(/(^|[\s-])(\S)/g, (_m, sep, ch) => sep + ch.toUpperCase());

// Legacy backend bucket keys -> frontend GENRES key. The backend still buckets
// "Mystery & Thriller" under the legacy keys "mystery"/"thriller"; parents
// arriving from the server must be normalized before comparison or nothing
// ever matches and every filter silently degrades to "show all".
const PARENT_ALIASES = { mystery: 'mystery_thriller', thriller: 'mystery_thriller' };
const normParent = (p) => PARENT_ALIASES[p] || p;

// Static taxonomy assembled once from the frontend catalogs. Both sources are
// already validated at module load by novelwriterGenres.js (it throws on any
// duplicate parent), so we trust them here.
const STATIC_TAXONOMY = (() => {
  const map = new Map(); // normKey -> { label, parent }
  for (const g of GENRES) {
    for (const s of g.subgenres) {
      if (!map.has(norm(s))) map.set(norm(s), { label: s, parent: g.key });
    }
  }
  for (const [label, meta] of Object.entries(SUBGENRE_DATA)) {
    const k = norm(label);
    if (!k) continue;
    const parent = normParent(meta && meta.genre);
    const hit = map.get(k);
    if (hit) {
      // Consistency guard: mirrors the invariant enforced in novelwriterGenres.
      if (parent && hit.parent !== parent) {
        throw new Error(
          `subgenreTaxonomy: "${label}" belongs to both "${hit.parent}" and "${parent}"`);
      }
    } else if (parent) {
      map.set(k, { label, parent });
    }
  }
  return map;
})();

export const staticTaxonomy = () => new Map(STATIC_TAXONOMY);

// Keys of the static taxonomy, sorted longest-first — used for substring
// resolution of free-text / compound-token values.
const STATIC_KEYS_SORTED = [...STATIC_TAXONOMY.keys()].sort((a, b) => b.length - a.length);

/**
 * Canonical single parent-genre key for ANY subgenre spelling, or '' when it
 * cannot be attributed to a genre. Resolution order:
 *   1. exact match in the merged index (labels + tokens, case/hyphen-insensitive)
 *   2. progressive token prefixes ("locked room impossible crime" -> "locked room")
 *   3. longest static-taxonomy label contained in the text
 */
export function resolveParent(index, text) {
  const t = norm(text);
  if (!t) return '';
  const idx = index || STATIC_TAXONOMY;
  const hit = idx.get ? idx.get(t) : undefined;
  if (hit && hit.parent) return hit.parent;
  const parts = t.split(' ');
  for (let n = parts.length - 1; n >= 1; n--) {
    const p = idx.get ? idx.get(parts.slice(0, n).join(' ')) : undefined;
    if (p && p.parent) return p.parent;
  }
  for (const k of STATIC_KEYS_SORTED) {
    if (t.includes(k)) {
      const e = STATIC_TAXONOMY.get(k);
      if (e) return e.parent;
    }
  }
  return '';
}

/**
 * Merge the static frontend taxonomy with every table the backend ships in the
 * /api/novel-params payload. Backend entries only FILL GAPS (a subgenre that
 * exists in the static catalog keeps its static parent — the static catalog is
 * the reviewed single-parent source of truth); brand-new server-only presets
 * are added with their advertised parent. Unresolvable parents are dropped.
 */
export function buildSubgenreIndex(payload) {
  const idx = new Map(STATIC_TAXONOMY);
  const put = (token, label, parent) => {
    const t = norm(token);
    const p = normParent(String(parent || '').trim());
    if (!t || !p) return false;
    if (!idx.has(t)) idx.set(t, { label: label || titleCase(t.replace(/ /g, ' ')), parent: p });
    return true;
  };
  if (!payload || typeof payload !== 'object') return idx;

  const catalogs = payload.subgenre_catalogs || {};
  for (const [tok, c] of Object.entries(catalogs)) {
    if (!c) continue;
    put(tok, titleCase(String(tok).replace(/_/g, ' ')), c.genre);
  }
  const parents = payload.subgenre_parents || {};
  for (const [tok, parent] of Object.entries(parents)) {
    put(tok, titleCase(String(tok).replace(/_/g, ' ')), parent);
  }
  const presets = Array.isArray(payload.subgenre_presets) ? payload.subgenre_presets : [];
  for (const p of presets) {
    const t = norm(p);
    if (!t || idx.has(t)) continue;
    // Try the raw preset string against the backend parent table first, then
    // against the whole index via prefix/substring resolution.
    let parent = normParent(String(parents[p.toLowerCase()] || parents[t.replace(/ /g, '_')] || ''));
    if (!parent) parent = resolveParent(idx, p);
    put(p, titleCase(p), parent);
  }
  return idx;
}

/**
 * THE filter. Returns the display labels of every subgenre whose canonical
 * parent is one of `genreKeys` (frontend GENRES keys; OTHER/free-text keys are
 * ignored). Candidates come from EVERY source (static taxonomy + all backend
 * tables) so server-only presets are included — but an entry is kept ONLY when
 * resolveParent attributes it to exactly one of the selected genres. When no
 * genre is selected, returns the full union (browse-everything mode).
 */
export function subgenresForKeys(index, genreKeys) {
  const keys = (genreKeys || []).filter((k) => k && k !== '__other__').map(normParent);
  const out = [];
  const seen = new Set();
  const push = (label) => {
    const lc = norm(label);
    if (label && !seen.has(lc)) { seen.add(lc); out.push(label); }
  };
  const candidates = new Set([...STATIC_TAXONOMY.keys(), ...(index ? index.keys() : [])]);
  if (!keys.length) {
    for (const [, e] of index) push(e.label);
    return out;
  }
  const keySet = new Set(keys);
  for (const cand of candidates) {
    const parent = resolveParent(index, cand);
    if (parent && keySet.has(parent)) {
      const e = index.get(cand) || STATIC_TAXONOMY.get(cand);
      push(e ? e.label : titleCase(cand.replace(/ /g, ' ')));
    }
  }
  return out;
}

/** Genre keys that own at least one of the given stored settings tokens —
 *  used to keep previously-checked Specific Settings visible after reload. */
export function ownersOfTokens(index, tokens) {
  const owners = new Set();
  for (const tk of tokens || []) {
    // Only consider tokens that ARE subgenre names (backend registers each
    // subgenre's own snake_case token inside its parent's settings bucket).
    const p = resolveParent(index, tk);
    if (p) owners.add(p);
  }
  return owners;
}

/** True when `text` resolves to a subgenre owned by one of `genreKeys`. */
export function subgenreBelongsToAny(index, text, genreKeys) {
  const p = resolveParent(index, text);
  if (!p) return false;
  return (genreKeys || []).map(normParent).includes(p);
}
