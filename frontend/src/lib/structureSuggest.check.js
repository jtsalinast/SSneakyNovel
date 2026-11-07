// Run: node frontend/src/lib/structureSuggest.check.js
import assert from 'node:assert/strict';
import { suggestStructure, STRUCTURE_ORDER } from './structureSuggest.js';

// Empty parameters fall back to the safe baseline.
assert.equal(suggestStructure({}, STRUCTURE_ORDER), 'three_act');
assert.equal(suggestStructure(null, STRUCTURE_ORDER), 'three_act');

// Genre/subgenre drive the pick.
assert.equal(suggestStructure({ genre: 'Romance', romanceLevel: 'central' }, STRUCTURE_ORDER), 'romance_arc');
assert.equal(suggestStructure({ genre: 'Mystery & Thriller' }, STRUCTURE_ORDER), 'seven_point');
assert.equal(suggestStructure({ subgenre: 'Slice of Life', length: 'light_novel' }, STRUCTURE_ORDER), 'kishotenketsu');
assert.equal(suggestStructure({ length: 'anthology' }, STRUCTURE_ORDER), 'episodic');
assert.equal(suggestStructure({ length: 'epic', genre: 'Fantasy' }, STRUCTURE_ORDER), 'snowflake');
assert.equal(suggestStructure({ length: 'flash' }, STRUCTURE_ORDER), 'three_act');

// Protagonist/conflict feed the quest & hero paths.
assert.equal(suggestStructure({ genre: 'Adventure' }, STRUCTURE_ORDER), 'quest');
assert.equal(
  suggestStructure({ genre: 'Adventure', protagonist: 'Chosen One', conflict: 'World-saving' }, STRUCTURE_ORDER),
  'heros_journey',
);

// The returned key is always one of the known structures.
for (const p of [{}, { genre: 'Drama' }, { genre: 'Comedy', length: 'light_novel' }]) {
  assert.ok(STRUCTURE_ORDER.includes(suggestStructure(p, STRUCTURE_ORDER)));
}

console.log('structureSuggest.check.js: ok');
