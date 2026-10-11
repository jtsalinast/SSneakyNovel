// Run: node frontend/src/lib/novelwriterGenres.check.js
// Round 17 regression guard for the single-parent subgenre taxonomy:
//  1. GENRES lists contain no duplicate subgenres (SUBGENRE_PARENT would throw).
//  2. SUBGENRE_DATA.genre agrees with the GENRES parent for every label.
//  3. The Config.svelte "subgenreOptions" cascade, when fed the backend preset
//     catalog (lowercase keys like "mahou shoujo", "cozy mystery"), returns
//     ONLY subgenres whose canonical parent is one of the selected genres —
//     the bug that showed Fantasy entries under Mystery & Thriller.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { GENRES, SUBGENRE_DATA, SUBGENRE_PARENT, parentGenreForSubgenre, canonicalSubgenreLabel } from './novelwriterGenres.js';

const src = readFileSync(new URL('../pages/Config.svelte', import.meta.url), 'utf8');

// —— 1 & 2: static catalog invariants ————————————————————————————————
assert.equal(parentGenreForSubgenre('High Fantasy'), 'fantasy');
assert.equal(parentGenreForSubgenre('mahou shoujo'), 'fantasy'); // lowercase key form
assert.equal(parentGenreForSubgenre('Cozy Mystery'), 'mystery_thriller');
assert.equal(parentGenreForSubgenre('romantasy'), 'romance');
assert.equal(parentGenreForSubgenre('nonexistent genre'), '');
for (const [label, meta] of Object.entries(SUBGENRE_DATA)) {
  const p = SUBGENRE_PARENT[String(label).toLowerCase()];
  assert.ok(!p || p === meta.genre, `parent mismatch for "${label}": GENRES=${p} SUBGENRE_DATA=${meta.genre}`);
}

// —— 3: mirror of the reactive SUBGENRE_INDEX + subgenreOptions cascade ——
// The page merges the static taxonomy with the backend tables (subgenre_parents
// and per-subgenre subgenre_catalogs) into a REACTIVE index rebuilt on every
// novelParamsTick; presets that only exist server-side must resolve to their
// canonical parent there too, and NEVER leak into another genre's dropdown.
const normSub = (s) => String(s || '').trim().toLowerCase().replace(/[_-]+/g, ' ').replace(/\s+/g, ' ');
const titleCasePreset = (s) => String(s || '').replace(/(^|[\s-])(\S)/g, (_m, sep, ch) => sep + ch.toUpperCase());

// Backend payload samples (subset of what /api/novel-params ships).
const SUBGENRE_PRESETS = [
  'epic fantasy', 'urban fantasy', 'dark fantasy', 'wuxia', 'xianxia', 'isekai',
  'mahou shoujo', 'mahou shonen', 'regression', 'villainess', 'otome',
  'cozy mystery', 'noir', 'heist', 'legal thriller', 'psychological thriller',
  'space opera', 'cyberpunk', 'revenge', 'slice of life', 'mecha', 'battle royale',
];
const SUBGENRE_PARENT_TABLE = { mahou_shoujo: 'fantasy', locked_room_impossible_crime: 'mystery_thriller', regression_rebirth: 'fantasy' };
const SUBGENRE_CATALOGS = {
  cozy_mystery: { conflicts: ['Personal Mystery'], protagonists: ['Amateur Detective'], settings: ['small_community'], genre: 'mystery_thriller' },
  isekai: { conflicts: ['World Transplanted'], protagonists: ['Summoned Protagonist'], settings: ['cheat_ability'], genre: 'fantasy' },
};

function buildIndex(backendParents, catalogs, presets) {
  const idx = {};
  const put = (token, label, parent) => {
    const t = normSub(token);
    if (!t || !parent) return;
    if (!idx[t]) idx[t] = { label, parent };
  };
  for (const g of GENRES) for (const s of g.subgenres) put(s, s, g.key);
  for (const [label, meta] of Object.entries(SUBGENRE_DATA)) put(label, label, meta.genre);
  for (const [tok, c] of Object.entries(catalogs)) {
    if (!c || !c.genre) continue;
    put(tok, canonicalSubgenreLabel(tok) || titleCasePreset(tok), c.genre);
  }
  for (const [tok, parent] of Object.entries(backendParents)) put(tok, canonicalSubgenreLabel(tok) || titleCasePreset(tok), parent);
  for (const p of presets) {
    const t = normSub(p);
    if (!t || idx[t]) continue;
    const parent = backendParents[t.replace(/ /g, '_')] || SUBGENRE_PARENT[t] || '';
    if (parent) put(t, canonicalSubgenreLabel(t) || titleCasePreset(p), parent);
  }
  return idx;
}

function canonicalParentFor(idx, token) {
  const t = normSub(token);
  if (!t) return '';
  const hit = idx[t];
  if (hit) return hit.parent;
  const parts = t.split(' ');
  for (let n = parts.length - 1; n >= 1; n--) {
    const p = idx[parts.slice(0, n).join(' ')];
    if (p) return p.parent;
  }
  return '';
}

function subgenreOptions(idx, keys, presets) {
  keys = keys.filter((k) => k !== '__other__');
  const list = [];
  const seen = new Set();
  const push = (s) => { const lc = String(s).toLowerCase(); if (s && !seen.has(lc)) { seen.add(lc); list.push(s); } };
  if (!keys.length) {
    for (const p of presets) push(titleCasePreset(p));
    for (const entry of Object.values(idx)) push(entry.label);
    return list;
  }
  for (const k of keys) {
    const g = GENRES.find((x) => x.key === k);
    if (g) for (const s of g.subgenres) push(s);
  }
  for (const p of presets) {
    const entry = idx[normSub(p)];
    if (entry && keys.includes(entry.parent)) push(entry.label);
  }
  return list;
}

const IDX = buildIndex(SUBGENRE_PARENT_TABLE, SUBGENRE_CATALOGS, SUBGENRE_PRESETS);

// Server-only tokens resolve through the merged index (round-19 bug class).
assert.equal(canonicalParentFor(IDX, 'mahou_shoujo'), 'fantasy');
assert.equal(canonicalParentFor(IDX, 'locked_room_impossible_crime'), 'mystery_thriller'); // prefix fallback
assert.equal(canonicalParentFor(IDX, 'nonexistent_thing'), '');

const mt = subgenreOptions(IDX, ['mystery_thriller'], SUBGENRE_PRESETS);
for (const leaked of ['Wuxia', 'Isekai', 'Mahou Shoujo', 'Epic Fantasy', 'Space Opera', 'Slice Of Life', 'Mecha', 'Battle Royale', 'Cyberpunk']) {
  assert.ok(!mt.some((x) => x.toLowerCase() === leaked.toLowerCase()), `"${leaked}" leaked into Mystery & Thriller`);
}
assert.ok(mt.includes('Cozy Mystery') && mt.includes('Noir') && mt.includes('Heist'));
assert.ok(mt.includes('Psychological Thriller'));
assert.ok(mt.includes('Revenge')); // canonical parent IS mystery_thriller

const fa = subgenreOptions(IDX, ['fantasy'], SUBGENRE_PRESETS);
assert.ok(fa.includes('Mahou Shoujo') && fa.includes('Isekai') && fa.includes('Wuxia'));
assert.ok(!fa.some((x) => x.toLowerCase() === 'cozy mystery'));

// No selection: everything browsable (union), nothing hidden.
const all = subgenreOptions(IDX, [], SUBGENRE_PRESETS);
assert.ok(all.length > mt.length && all.includes('Cozy Mystery') && all.includes('Isekai'));

// —— 4: source-code guards for Config.svelte ————————————————————————————
// Filtering happens ONLY in the frontend from the full backend payload. Since
// round 19 there is exactly ONE taxonomy implementation (lib/subgenreTaxonomy.js)
// and Config.svelte must consume it instead of keeping a second, drifting copy
// of the merge/filter logic (that duplication was the bug).
assert.match(src, /\$: SUBGENRE_INDEX = \(\(\) => \{/);
assert.match(src, /\$: TAX = buildSubgenreIndex\(novelParamsPayload\)/);
assert.match(src, /return subgenresForKeys\(TAX, keys\);/);
assert.match(src, /if \(p\.subgenre_catalogs && typeof p\.subgenre_catalogs === 'object'\) SUBGENRE_CATALOGS = p\.subgenre_catalogs;/);
assert.match(src, /if \(parent && keys\.length && !keys\.includes\(parent\)\) return null; \/\/ foreign\/stale/);
assert.doesNotMatch(src, /SUBGENRE_TOKEN_PARENT/);
assert.doesNotMatch(src, /SUBGENRE_DATA\[titleCasePreset\(s\)\]/);
assert.doesNotMatch(src, /const SUBGENRE_INDEX = \(\(\) => \{/);
// The Subgenre combobox must render through the new GenreSubgenrePicker
// component (active markup), not the legacy ComboSelect path.
assert.match(src, /import GenreSubgenrePicker from '\.\.\/lib\/components\/GenreSubgenrePicker\.svelte';/);
assert.match(src, /<GenreSubgenrePicker[\s\S]*?paramsPayload=\{novelParamsPayload\}[\s\S]*?genreKeys=\{selectedGenreKeys\}/);

console.log('novelwriterGenres checks passed:', GENRES.length, 'genres,', Object.keys(SUBGENRE_PARENT).length, 'unique subgenres');
