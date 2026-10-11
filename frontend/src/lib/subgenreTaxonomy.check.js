// Regression check for the round-19 single-parent cascade module.
// Run: node src/lib/subgenreTaxonomy.check.js  (from frontend/)
import assert from 'node:assert';
import { GENRES } from './novelwriterGenres.js';
import {
  buildSubgenreIndex, subgenresForKeys, resolveParent, subgenreBelongsToAny, norm,
} from './subgenreTaxonomy.js';

const idx = buildSubgenreIndex(null);

// 1) Every genre's filtered list must equal its static list exactly —
//    no foreign entries (the reported bug: Fantasy items under Mystery).
for (const g of GENRES) {
  const list = subgenresForKeys(idx, [g.key]);
  const foreign = list.filter((s) => !g.subgenres.includes(s));
  assert.deepStrictEqual(foreign, [], `foreign subgenres leaked into ${g.label}: ${foreign}`);
  for (const s of g.subgenres) {
    assert.ok(list.some((x) => norm(x) === norm(s)), `${s} missing from ${g.label} list`);
  }
}

// 2) Hard-coded leak guard for the exact user-reported symptom.
const mt = subgenresForKeys(idx, ['mystery_thriller']);
for (const fantasy of ['High Fantasy', 'Epic Fantasy', 'Isekai', 'Xianxia', 'Wuxia']) {
  assert.ok(!mt.some((x) => x.toLowerCase() === fantasy.toLowerCase()),
    `${fantasy} must never appear under Mystery & Thriller`);
}

// 3) Multi-select union stays within the selected genres only.
const both = subgenresForKeys(idx, ['mystery_thriller', 'romance']);
assert.ok(both.includes('Cozy Mystery') && both.includes('Romantasy'));
assert.ok(!both.some((x) => /fantasy$/i.test(x) && x !== 'Romantic Suspense'), 'no fantasy leaks');

// 4) Backend payload with legacy parent keys ("mystery") normalizes to
//    mystery_thriller and server-only presets get attributed correctly.
const payload = {
  subgenre_parents: { cozy_mystery: 'mystery', mahou_shoujo: 'fantasy' },
  subgenre_catalogs: { action_thriller: { genre: 'thriller', conflicts: [], protagonists: [], settings: [] } },
  subgenre_presets: ['mahou shoujo', 'action thriller', 'totally unknown thing'],
};
const idx2 = buildSubgenreIndex(payload);
assert.strictEqual(resolveParent(idx2, 'cozy_mystery'), 'mystery_thriller');
assert.strictEqual(resolveParent(idx2, 'Action Thriller'), 'mystery_thriller');
assert.strictEqual(resolveParent(idx2, 'locked_room_impossible_crime'), 'mystery_thriller');
const mt2 = subgenresForKeys(idx2, ['mystery_thriller']);
assert.ok(mt2.some((x) => norm(x) === 'cozy mystery'));
assert.ok(!mt2.some((x) => /mahou/i.test(x)), 'mahou shoujo stays under fantasy');
const fa2 = subgenresForKeys(idx2, ['fantasy']);
assert.ok(fa2.some((x) => norm(x) === 'mahou shoujo'));
// unresolvable presets are dropped when a genre is selected, never shown everywhere
assert.ok(!mt2.some((x) => norm(x) === 'totally unknown thing'));

// 5) No selection => browse-everything mode still works.
assert.ok(subgenresForKeys(idx, []).length >= 185);

// 6) subgenreBelongsToAny helper.
assert.ok(subgenreBelongsToAny(idx, 'Heist', ['mystery_thriller']));
assert.ok(!subgenreBelongsToAny(idx, 'Heist', ['fantasy']));

console.log('subgenreTaxonomy.check.js: all assertions passed ✓');
