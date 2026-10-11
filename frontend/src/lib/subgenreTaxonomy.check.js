// Regression check for the round-19 single-parent cascade module.
// Run: node src/lib/subgenreTaxonomy.check.js  (from frontend/)
import assert from 'node:assert';
import { GENRES } from './novelwriterGenres.js';
import {
  buildSubgenreIndex, subgenresForKeys, resolveParent, subgenreBelongsToAny, norm,
  splitSubgenreText,
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

// —— Round 21: cascadeForSelection + normalizeBuckets ————————
{
  const { cascadeForSelection, normalizeBuckets } = await import('./subgenreTaxonomy.js');
  const PAYLOAD = {
    conflict_scales: {
      mystery: ['mystery_crime', 'other'],
      thriller: ['global_stakes', 'other'],
      fantasy: ['epic_conflict', 'other'],
    },
    protagonist_types: {
      mystery: ['amateur_detective'],
      thriller: ['spy_agent'],
      fantasy: ['chosen_one'],
    },
    specific_settings: {
      mystery: ['small_community', 'puzzle_focus'],
      thriller: ['international_intrigue', 'noir'],
      fantasy: ['magic_system', 'medieval_setting'],
    },
    subgenre_parents: {},
    subgenre_catalogs: {
      cozy_mystery: { conflicts: ['puzzle_focus'], protagonists: ['amateur_detective'], settings: ['small_community', 'low_violence'] },
    },
    subgenre_presets: [],
  };
  // Legacy backend keys must fold into the frontend genre key.
  const nb = normalizeBuckets(PAYLOAD.specific_settings);
  assert.deepEqual(nb['mystery_thriller'].sort(), ['international_intrigue', 'noir', 'puzzle_focus', 'small_community'].sort());

  // Mystery & Thriller selected, no subgenre -> ONLY mystery/thriller buckets.
  const c1 = cascadeForSelection(PAYLOAD, {}, '', ['mystery_thriller']);
  assert.ok(!c1.settings.includes('magic_system'), 'fantasy setting leaked into Mystery & Thriller');
  assert.ok(c1.settings.includes('small_community'));
  assert.ok(!c1.conflicts.includes('epic_conflict'));
  assert.ok(!c1.protagonists.includes('chosen_one'));

  // Fantasy selected -> no mystery entries.
  const c2 = cascadeForSelection(PAYLOAD, {}, '', ['fantasy']);
  assert.deepEqual(c2.settings, ['magic_system', 'medieval_setting']);

  // Cozy Mystery (owned by mystery) + Mystery selection -> own catalog wins.
  const c3 = cascadeForSelection(PAYLOAD, {}, 'Cozy Mystery', ['mystery_thriller']);
  assert.equal(c3.source, 'subgenre');
  assert.ok(c3.settings.includes('low_violence'));

  // FOREIGN stale subgenre (High Fantasy under a Mystery selection) must NOT
  // hijack the lists — falls back to the genre union only.
  const c4 = cascadeForSelection(PAYLOAD, {}, 'High Fantasy', ['mystery_thriller']);
  assert.notEqual(c4.source, 'subgenre');
  assert.ok(!c4.settings.includes('magic_system') || c4.source === 'all');
  assert.ok(!c4.settings.includes('magic_system'), 'fantasy bucket leaked via foreign subgenre');

  // Nothing selected -> browse-everything union.
  const c5 = cascadeForSelection(PAYLOAD, {}, '', []);
  assert.ok(c5.settings.includes('magic_system') && c5.settings.includes('small_community'));
}
console.log('round 21 cascade assertions passed ✓');

// —— Round 22: multi-select subgenre storage ————————————————
// splitSubgenreText must mirror backend config.SplitSubgenreList: commas,
// semicolons, slashes and newlines all separate chips; blanks are dropped.
assert.deepEqual(splitSubgenreText('Cozy Mystery, Noir; Heist/Thriller\nSpy'), ['Cozy Mystery', 'Noir', 'Heist', 'Thriller', 'Spy']);
assert.deepEqual(splitSubgenreText(''), []);
assert.deepEqual(splitSubgenreText(null), []);
// A multi-chip string belongs to a genre set when ANY chip matches.
assert.ok(subgenreBelongsToAny(idx, 'High Fantasy, Heist', ['mystery_thriller']), 'multi-chip membership failed');
assert.ok(!subgenreBelongsToAny(idx, 'High Fantasy, Epic Quest', ['mystery_thriller']), 'foreign multi-chip wrongly accepted');
console.log('round 22 multi-subgenre assertions passed ✓');
