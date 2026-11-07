// Deterministic "Suggest" for Story structure. Scores every known structure
// from the current novel parameters (genre, subgenre, protagonist type, conflict
// scale, target audience, length and romance level) and returns the best fit.
// Pure and side-effect free so the Outline page can call it from a button and
// the check script can exercise it without a browser.

const has = (text, ...words) => {
  const t = String(text || '').toLowerCase();
  return words.some((w) => t.includes(w));
};

// p: { genre, subgenre, protagonist, conflict, targetAudience, length, romanceLevel }
// keys: optional canonical structure list (order breaks score ties; default
// prefers three_act, which carries the baseline score).
export function suggestStructure(p, keys) {
  const params = p || {};
  const genre = params.genre || '';
  const sub = params.subgenre || '';
  const both = genre + ' ' + sub;
  const protag = params.protagonist || '';
  const conflict = params.conflict || '';
  const length = params.length || '';
  const audience = params.targetAudience || '';
  const romance = params.romanceLevel || '';

  const s = {};
  const add = (k, v) => { s[k] = (s[k] || 0) + v; };

  // Baseline: every story can be told in three acts.
  add('three_act', 1);

  // Romance.
  if (has(genre, 'romance')) add('romance_arc', 5);
  if (has(both, 'romance', 'romcom', 'rom-com', 'love', 'romantasy')) add('romance_arc', 3);
  if (romance === 'central') add('romance_arc', 3);
  else if (romance === 'moderate') add('romance_arc', 2);

  // Mystery / thriller.
  if (has(genre, 'mystery')) add('seven_point', 5);
  if (has(genre, 'thriller')) { add('seven_point', 4); add('fichtean', 4); }
  if (has(both, 'mystery', 'detective', 'noir', 'crime', 'procedural', 'suspense')) add('seven_point', 2);
  if (has(conflict, 'mystery', 'secret', 'conspiracy', 'evidence', 'puzzle', 'reveal')) add('seven_point', 2);
  if (has(protag, 'detective', 'investigator', 'forensic', 'profiler')) add('seven_point', 2);
  if (has(both, 'thriller', 'horror', 'suspense')) add('fichtean', 2);
  if (has(conflict, 'clock', 'ticking', 'escape', 'threat', 'psychological')) add('fichtean', 1);

  // Adventure / quest / hero.
  if (has(genre, 'adventure', 'fantasy', 'scifi', 'science fiction')) add('quest', 2);
  if (has(both, 'adventure', 'quest', 'epic', 'isekai', 'wuxia', 'xianxia', 'expedition', 'journey')) add('quest', 3);
  if (has(protag, 'chosen one', 'explorer', 'cartographer', 'archaeolog', 'wanderer', 'adventurer')) add('quest', 3);
  if (has(conflict, 'quest', 'expedition', 'journey', 'race against time', 'frontier')) add('quest', 2);
  if (has(protag, 'chosen one', 'hero', 'prophesied')) add('heros_journey', 4);
  if (has(conflict, 'world-saving', 'world saving', 'fate', 'good vs evil', 'ancient evil', 'prophecy')) add('heros_journey', 2);
  if (has(genre, 'fantasy', 'adventure')) add('heros_journey', 2);

  // Slice of life / school / cozy / comedy.
  if (length === 'light_novel') add('kishotenketsu', 4);
  if (has(both, 'slice of life', 'school', 'cozy', 'iyashikei', 'daily')) add('kishotenketsu', 3);
  if (length === 'anthology') add('episodic', 4);
  if (has(both, 'slice of life', 'episodic', 'road trip', 'sitcom', 'monster of the week')) add('episodic', 3);
  if (has(both, 'coming of age', 'coming-of-age', 'bildungsroman', 'school')) add('story_circle', 3);
  if (has(genre, 'comedy')) { add('story_circle', 1); add('kishotenketsu', 1); }

  // Drama / tragedy / historical.
  if (has(genre, 'drama', 'historical', 'literary')) add('freytag', 3);
  if (has(both, 'tragedy', 'melodrama', 'saga', 'literary', 'drama')) add('freytag', 2);
  if (has(conflict, 'fate', 'tragedy', 'character')) add('freytag', 1);

  // Long-form / epic / many threads.
  if (length === 'epic') { add('snowflake', 5); add('six_act', 3); }
  if (length === 'novel') { add('snowflake', 1); add('six_act', 2); }
  if (has(both, 'epic', 'grand', 'saga', 'multiple pov', 'multigenerational', 'generational')) add('snowflake', 2);
  if (has(genre, 'scifi', 'science fiction', 'fantasy')) add('six_act', 1);

  // Short forms.
  if (length === 'flash' || length === 'short') { add('three_act', 3); add('freytag', 2); }
  if (length === 'novelette' || length === 'novella') add('heros_journey_simple', 3);

  // Audience & mainstream shape.
  if (audience === 'ya') { add('story_circle', 1); add('kishotenketsu', 1); }
  if (has(genre, 'action')) add('save_the_cat', 2);
  if (has(genre, 'comedy', 'romance')) add('save_the_cat', 1);

  const order = (Array.isArray(keys) && keys.length) ? keys : Object.keys(s);
  let best = 'three_act';
  let bestScore = -Infinity;
  for (const k of order) {
    const sc = s[k] || 0;
    if (sc > bestScore) { bestScore = sc; best = k; }
  }
  return best;
}

// Canonical order used by the UI when no explicit key list is supplied.
export const STRUCTURE_ORDER = [
  'three_act', 'six_act', 'fichtean', 'freytag', 'seven_point', 'heros_journey',
  'heros_journey_simple', 'save_the_cat', 'story_circle', 'kishotenketsu',
  'snowflake', 'quest', 'romance_arc', 'episodic',
];
