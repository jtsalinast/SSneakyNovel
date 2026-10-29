<script>
  import { onMount, onDestroy } from 'svelte';
  import { get } from 'svelte/store';
  import { api } from '../lib/api.js';
  import { apiConfig, config, progress, settings, editingCharID, editingWvID, addToast, showConfirm, taskRunning, apiTestResult } from '../lib/stores.js';
  import { t, uiLocale } from '../lib/i18n/index.js';
  import { resolveChatCompletionsURL } from '../lib/apiUrl.js';
  import ConfigChangePanel from '../components/ConfigChangePanel.svelte';
  import AiGenerateButton from '../components/AiGenerateButton.svelte';
  import RandomizeButton from '../components/RandomizeButton.svelte';
  import ComboSelect from '../lib/components/ComboSelect.svelte';
  // Interfaz unificada: los <select> nativos del formulario de historia y de
  // los formularios de lore usan SmartSelect (la variante single-select del
  // SmartCombobox) — misma estética, búsqueda en vivo, popover ⓘ de resumen.
  import SmartSelect from '../lib/components/SmartSelect.svelte';
  import GenreSubgenrePicker from '../lib/components/GenreSubgenrePicker.svelte';
  // Round 21: brand-new, self-contained cascade block rendered BELOW the legacy
  // fields (which stay untouched). All filtering lives in lib/subgenreTaxonomy.js.
  import GenreCascadeFields from '../lib/components/GenreCascadeFields.svelte';
  import SegmentedControl from '../lib/components/SegmentedControl.svelte';
  import InfoTip from '../lib/components/InfoTip.svelte';
  import EnhanceButton from '../lib/components/EnhanceButton.svelte';
  import EnhanceField from '../lib/components/EnhanceField.svelte';
  import Modal from '../lib/components/Modal.svelte';
  import { GENRES, SUBGENRE_DATA, SUBGENRE_PARENT, parentGenreForSubgenre, genreBucketKeys, canonicalSubgenreLabel } from '../lib/novelwriterGenres.js';
  // Round 19: the cascade now flows EXCLUSIVELY through these two modules.
  import { buildSubgenreIndex, subgenresForKeys, resolveParent, norm as normSub2, ownersOfTokens } from '../lib/subgenreTaxonomy.js';

  // —— Cross-tab generation state: both workspace tabs ("Config" and "Novel
  // parameters") render this same component, but Svelte destroys and recreates
  // the instance when switching. Keep unsaved form edits, per-section busy
  // flags, undo snapshots and the status-poller handle here (component body,
  // module-singleton semantics) so a running generation keeps being tracked
  // and the UI refreshes with the LLM result when it finishes — no matter
  // which tab the user moved to (and without an F5).
  let sharedGenBusy = {};   // section -> true while its generation runs
  let sharedGenUndo = {};   // undoKey -> pre-generation snapshot
  let genPollTimer = null;  // shared /api/status poller handle

  export let sendToChat = async () => {};
  // Which workspace tab renders this page: 'api' (Config), 'novelParams'
  // (Story parameters) or 'lore' (Characters / Worldview / Organizations /
  // Relations).
  export let tab = 'api';

  function stripNameMarks(name) {
    return (name.startsWith('「') && name.endsWith('」')) ? name.slice(1, -1) : name;
  }

  let showCharForm = false;
  let showWvForm = false;

  let charName = '', charAge = '', charAppearance = '', charPersonality = '', charBackground = '', charMotivation = '', charAbilities = '', charNotes = '';
  // Protagonist arc quartet (want / need / wound / agency).
  let charWant = '', charNeed = '', charWound = '', charAgency = '';
  let wvName = '', wvCategory = 'other', wvDescription = '', wvTags = '';

  // Organization management
  let showOrgForm = false;
  let orgName = '', orgType = '', orgDescription = '';
  let orgMembers = [];
  let editingOrgID = null;

  // Relation management
  let showRelForm = false;
  let relSource = '', relTarget = '', relLabel = '';
  let editingRelID = null;

  $: cfgBase = $apiConfig?.base_url || '';
  $: cfgModel = $apiConfig?.model || '';
  $: cfgKey = $apiConfig?.api_key || '';
  $: cfgTimeout = $apiConfig?.http_timeout_seconds || 600;

  let localApiCfg = { base_url: '', url_strict: false, model: '', api_key: '', http_timeout_seconds: 600, task_duration_minutes: 0, max_tokens: 32768, context_budget_tokens: 900000, brainstorm: { base_url: '', url_strict: false, model: '', api_key: '', http_timeout_seconds: 120, max_tokens: 8192, context_budget_tokens: 300000 } };
  // —— Cross-tab state (module level): both workspace tabs ("Config" and "Novel
  // parameters") render this same component, but Svelte destroys and recreates it
  // when switching. Keep unsaved form edits, per-section busy flags and undo
  // snapshots here so they survive a tab change (and a failed save doesn't wipe
  // what the user typed).
  let sharedStoryDraft = null;   // last localStoryCfg value

  let localStoryCfg = { type: '', title: '', subgenre: '', theme: '', tone: '', author: '', story_length: '', structure: '', motif: '', inspirational_pieces: '', output_language: '', story_idea: '', character_arcs_enabled: false, conflict_scale: '', conflict_other: '', specific_settings: '', protagonist_type: '', protagonist_other: '', gender_bias: 'random', target_audience: '', audience_profile: '', romance_level: '', sexual_content: '', gore_level: '', world_darkness: '', locations_enabled: false, target_words_per_chapter: 2500, writing_style: '', writing_pov: '', philosophical_dilemma: '', reader_expectation: '', forces_in_tension: '', elevator_pitch: '', lighthouse_sentence: '', differentiating_idea: '', creativity_level: 0, work_type: '', fanfic_source: '' };
  // Preserve unsaved edits across workspace-tab switches (this component is
  // recreated on every tab change; without this the Genre field and friends
  // would silently reset to whatever was last saved on the server).
  if (sharedStoryDraft) localStoryCfg = { ...localStoryCfg, ...sharedStoryDraft };
  $: if (localStoryCfg) sharedStoryDraft = localStoryCfg;
  // When leaving this page (workspace-tab switch destroys the component),
  // persist the story parameters — above all the Genre select, which lives
  // only here — so the new tab re-renders from the saved config instead of
  // falling back to stale values (the classic "Fantasy ends up in Other" bug).
  onDestroy(() => {
    // Don't persist while *our own* generations are in flight: the backend
    // locks writes during tasks, and re-setting $config from the stale
    // pre-generation reply would clobber the fields the LLM just filled. The
    // module-level poller owns the refresh in that case.
    // NOTE: we deliberately do NOT check get(taskRunning) here — that global
    // flag can be stale-true (a task started from another page, or SSE having
    // missed a task_end event). Skipping the save in that case used to wipe
    // unsaved edits on tab switches AND silently swallow every Generate click,
    // because persistStoryBeforeGenerate() bailed out against a locked server
    // before ever reaching the POST. A genuine conflict simply makes this PUT
    // fail with 409, which we ignore.
    if (Object.values(sharedGenBusy).some(Boolean)) return;
    try {
      const story = storyPayload();
      api('PUT', '/api/config', { ...($config || {}), story }).then((saved) => {
        config.set(saved);
        storyCfgSnapshot = JSON.stringify(saved?.story || '');
      }).catch(() => {});
    } catch (e) { /* ignore */ }
  });

// Novel-parameter tooltips (subgenre/structure hints) fetched from /api/novel-params.
let SUBGENRE_HINTS = {};
let STRUCTURE_HINTS = {};
let SUBGENRE_PRESETS = [];
// Story length / structure catalogs & presets, refreshed from /api/novel-params
// (backend is the single source of truth: config.LengthKeys,
// config.AllStructureKeys, config.TargetWordsPresetByLength). The literals
// below are fallbacks used before the first fetch resolves.
let LENGTH_KEYS = ['flash', 'short', 'novelette', 'novella', 'light_novel', 'anthology', 'novel', 'epic'];
let ALL_STRUCTURE_KEYS = ['three_act', 'six_act', 'fichtean', 'freytag', 'seven_point', 'heros_journey', 'heros_journey_simple', 'save_the_cat', 'story_circle', 'kishotenketsu', 'snowflake', 'quest', 'romance_arc', 'episodic'];
// Mirror of backend config.TargetWordsPresetByLength (overridden by the live
// /api/novel-params payload). Round 13: novel -> 4000, epic -> 5000.
let TARGET_WORDS_BY_LENGTH = { flash: 1000, short: 2000, novelette: 2500, novella: 2500, light_novel: 3500, anthology: 4000, novel: 4000, epic: 5000 };
let novelParamsTick = 0;
function subgenreHint(s) { return SUBGENRE_HINTS[(s || '').toLowerCase()] || ''; }
function structHint(k) { return STRUCTURE_HINTS[k] || ''; }

// Novel parameters: genre presets (mirror of backend config.Genre* maps; "other" is always available).
// Round 7: the parent-genre mirrors now cover EVERY genre offered in the Genre
// select (including Drama) and stay in sync with internal/config/config.go —
// previously only 8 genres were mirrored here, so selecting e.g. "Drama" left
// the Conflict/Protagonist dropdowns empty until /api/novel-params arrived.
// KEY ALIASES: the backend buckets Mystery & Thriller under the legacy keys
// "mystery" and "thriller", while GENRES uses "mystery_thriller". The cascade
// below indexes these maps by SELECTED-GENRE key, so every lookup goes
// through bucketKeys(): without the alias, selecting "Mystery & Thriller" read
// GENRE_CONFLICTS["mystery_thriller"] (empty), the union came out empty, and
// the UI fell back to showing ALL backend entries — the "siguen apareciendo
// todos los subgéneros de Fantasy en Mystery & Thriller" symptom.
const GENRE_KEY_ALIASES = {
  mystery_thriller: ['mystery_thriller', 'mystery', 'thriller'],
};
const bucketKeys = (k) => GENRE_KEY_ALIASES[k] || [k];
const GENRE_CONFLICTS = {
fantasy: ['Personal Quest', 'Kingdom-wide', 'World-saving', 'Good vs Evil', 'Political Intrigue', 'Sect/Realm Wars'],
scifi: ['Personal', 'Planetary', 'Interstellar', 'Galactic', 'Humanity vs Technology'],
mystery: ['Personal Mystery', 'Community Secret', 'Local Crime', 'Family Mystery', 'Historical Puzzle'],
romance: ['Personal Growth', 'Relationship Obstacles', 'Career vs Love', 'Family Issues', 'Past Trauma'],
thriller: ['International Conspiracy', 'Government Secrets', 'Spy Networks', 'National Security', 'Global Politics'],
horror: ['Personal Haunting', 'Family Curse', 'Supernatural Threat', 'Psychological Terror', 'Ancient Evil', 'Cosmic Indifference'],
historical: ['Tribal Warfare', 'Religious Conflicts', 'Ancient Politics', 'Survival Struggles', 'Civilization Building'],
western: ['Personal Vendetta', 'Town Protection', 'Range War', 'Law vs Lawlessness', 'Civilization vs Wilderness'],
drama: ['Silence Inside a Family', 'Self vs Meaning', 'Duty vs Desire', 'Identity Unraveling', 'Institution vs Individual', 'Generational Legacy'],
};
const GENRE_PROTAGONISTS = {
fantasy: ['Chosen One', 'Magic User', 'Knight/Warrior', 'Royal Heir', 'Common Hero', 'Prophesied One'],
scifi: ['Military Officer', 'Merchant Captain', 'Explorer', 'Diplomat', 'Rebel Leader', 'Imperial Noble'],
mystery: ['Amateur Detective', 'Librarian', 'Shop Owner', 'Retired Professional', 'Local Resident', 'Hobby Enthusiast'],
romance: ['Career Professional', 'Single Parent', 'Artist/Creative', 'Business Owner', 'Healthcare Worker', 'Teacher/Academic'],
thriller: ['Secret Agent', 'Intelligence Officer', 'Double Agent', 'Spy Handler', 'Undercover Operative', 'Government Analyst'],
horror: ['Haunted Individual', 'Investigator', 'Innocent Victim', 'Cursed Person', 'Gothic Hero', 'Tormented Soul'],
historical: ['Ancient Warrior', 'Priest/Priestess', 'Tribal Leader', 'Ancient Scholar', 'Slave', 'Ancient Ruler'],
western: ['Sheriff/Marshal', 'Gunslinger', 'Rancher', 'Outlaw', 'Bounty Hunter', 'Frontier Doctor'],
drama: ['Matriarch or Patriarch Holding a Fractured Family', 'Coming-of-age Everyteen on a Threshold', 'Ordinary Person Facing an Unremarkable Crisis', 'Idealist Ground Down by an Institution', 'Griever Rebuilding a Life After Loss'],
};
const GENRE_SETTINGS = {
fantasy: ['magic_system', 'medieval_setting', 'mythical_creatures', 'epic_scale', 'political_dynasties', 'wuxia', 'xianxia', 'cozy', 'urban', 'gothic', 'dark_academia', 'romantasy', 'swashbuckler'],
scifi: ['interstellar_travel', 'advanced_technology', 'multiple_species', 'space_combat', 'ai_singularity', 'cyberpunk', 'solarpunk', 'steampunk', 'space_opera', 'dystopian', 'postapo', 'military', 'superhero'],
mystery: ['small_community', 'amateur_detective', 'low_violence', 'puzzle_focus', 'recurring_characters', 'noir', 'heist', 'cozy', 'urban', 'locked_room_impossible_crime', 'mystery_police', 'gothic'],
romance: ['modern_setting', 'relationship_focus', 'emotional_journey', 'happy_ending', 'realistic_world', 'romcom', 'romantasy', 'small_town_or_urban_backdrop', 'cozy', 'urban', 'historical_courtship', 'dark_academia'],
thriller: ['international_intrigue', 'spy_networks', 'government_secrets', 'double_agents', 'global_stakes', 'heist', 'noir', 'military_intelligence', 'urban', 'mystery_police', 'cyberpunk'],
horror: ['atmospheric_dread', 'isolated_setting', 'supernatural_elements', 'psychological_terror', 'dark_atmosphere', 'cosmic_horror', 'gothic', 'dark_academia', 'folk_customs_and_rites', 'postapo', 'urban'],
historical: ['ancient_civilizations', 'mythological_elements', 'tribal_societies', 'ancient_religions', 'primitive_technology', 'medieval_setting', 'mythical_creatures', 'epic_scale', 'court_intrigue', 'nautical', 'swashbuckler', 'military'],
western: ['frontier_setting', 'lawlessness', 'honor_code', 'survival_focus', 'horse_culture', 'swashbuckler_frontier_action', 'nautical_ports_and_trails', 'picaresque', 'noir'],
drama: ['interiority_over_plot', 'multi_generational_timeline', 'quiet_realism', 'moral_ambiguity_no_cartoon_villains', 'time_nonlinear_or_elided', 'open_or_bittersweet_endings_ok', 'found_family_or_bloodline', 'institution_as_stage', 'letters_or_messages_format', 'everyday_life_texture'],
};
const GENRE_KEYWORDS = [
['fantasy', ['fantasy']],
['scifi', ['sci-fi', 'scifi', 'science fiction']],
['mystery', ['mystery', 'detective']],
['romance', ['romance']],
['thriller', ['thriller', 'spy']],
['horror', ['horror']],
['historical', ['historical', 'history']],
['western', ['western']],
// round 3 — keep in sync with backend config.MatchGenreKey (specific keys first)
['xianxia', ['xianxia']],
['wuxia', ['wuxia']],
['litrpg', ['litrpg', 'lit rpg', 'game novel', 'system flow']],
['cyberpunk', ['cyberpunk', 'cyber-punk']],
['solarpunk', ['solarpunk', 'solar punk']],
['space_opera', ['space opera']],
['romantasy', ['romantasy', 'romantic fantasy', 'epic romance']],
['dystopian', ['dystopia', 'dystopian']],
['postapo', ['post-apocalyp', 'post apocalyp', 'postapoc', 'zombie']],
['military', ['military', 'war novel']],
['heist', ['heist', 'caper', 'robbery']],
['cozy', ['cozy', 'comfort read']],
['urban', ['urban', 'contemporary fantasy', 'city']],
['middle_grade', ['middle grade', 'middle-grade', 'mg novel']],
['ya', ['young adult', 'ya fantasy', 'ya romance', 'teen']],
['steampunk', ['steampunk', 'steam punk']],
['superhero', ['superhero', 'super hero', 'supervillain']],
['dark_academia', ['dark academia']],
['swashbuckler', ['swashbuckl', 'pirate', 'corsair']],
['nautical', ['nautical', 'seafaring', 'voyage at sea']],
['picaresque', ['picaresque', 'rogue tale']],
['gothic', ['gothic']],
['time_travel', ['time travel', 'time-travel']],
['cli_fi', ['cli-fi', 'clifi', 'climate fiction']],
['afrofuturism', ['afrofuturism', 'afro-futurism']],
['hard_scifi', ['hard scifi', 'hard sci-fi', 'hard science fiction']],
['graphic_novel', ['graphic novel', 'comic', 'manga script']],
['mystery_police', ['police procedural', 'detective squad', 'cold case']],
['adventure', ['adventure', 'survival', 'expedition', 'quest']],
];
function matchGenreKey(t) {
const s = (t || '').toLowerCase().trim();
if (!s) return '';
for (const [key, words] of GENRE_KEYWORDS) {
if (words.some((w) => s.includes(w))) return key;
}
return '';
}
let genreKeyDerived = '';
$: genreKeyDerived = matchGenreKey(localStoryCfg.type);

// --- Genre (parent) / Subgenre cascade, adapted from NovelWriter's genre_configs ---
// Round 16: Genre and Subgenre are ComboSelect comboboxes (searchable rich
// lists). The parent-genre state is now an ARRAY of labels (multi-select):
// the union of every selected genre's subgenres feeds the Subgenre combobox.
const OTHER_GENRE = '__other__';
let selectedGenreKeys = [];  // GENRES keys (+ OTHER_GENRE); [] = not classified yet
let lastGenreText = '';      // free-text typed in the genre combobox ("Other")
let subgenreManual = false;  // true while the user types a custom subgenre
let lastAutoSubgenre = '';   // subgenre we auto-set, so we don't clobber manual edits
const OTHER_SUBGENRE = '__other__';

// Resolve the stored `type` text into one or more parent-genre keys. Handles
// multi-label strings ("Fantasy, Romance"), single labels and keyword matches.
// Round 20: when NONE of the comma-separated parts resolves to a known genre
// (e.g. the user only typed a subgenre like "cozy mystery" in Type), fall back
// to matching the WHOLE string — otherwise selectedGenreKeys stays empty and
// every cascade below silently degrades to browse-everything mode ("siguen
// apareciendo todos"). The fallback can only ADD keys, never remove them.
function genreLabelsToKeys(t) {
  const s = String(t || '').trim();
  if (!s) return [];
  const parts = s.split(/[,;/]+/).map((x) => x.trim()).filter(Boolean);
  const keys = [];
  for (const p of parts) {
    const exact = GENRES.find((x) => x.label.toLowerCase() === p.toLowerCase());
    if (exact) { if (!keys.includes(exact.key)) keys.push(exact.key); continue; }
    const byKey = GENRES.find((x) => x.key.toLowerCase() === p.toLowerCase());
    if (byKey) { if (!keys.includes(byKey.key)) keys.push(byKey.key); continue; }
    const derived = matchGenreKey(p);
    if (derived && GENRES.some((x) => x.key === derived)) { if (!keys.includes(derived)) keys.push(derived); }
  }
  if (!keys.length) {
    const whole = matchGenreKey(s);
    if (whole && GENRES.some((x) => x.key === whole)) keys.push(whole);
  }
  return keys;
}

function initGenreCascade() {
  const t = (localStoryCfg.type || '').trim();
  const keys = genreLabelsToKeys(t);
  if (keys.length) {
    selectedGenreKeys = keys;
    lastGenreText = t;
    subgenreManual = false;
  } else if (t) {
    // Round 20: the Type text is free-form. If it mentions ANY known genre
    // (even mixed with other words, e.g. "cozy mystery" or "epic fantasy
    // saga"), cascade as that genre instead of OTHER — otherwise the whole
    // union ("todos los subgéneros") was shown for any unrecognized text.
    const byWords = GENRES.filter((g) => t.toLowerCase().includes(g.label.toLowerCase()) || t.toLowerCase().includes(g.key.toLowerCase())).map((g) => g.key);
    if (byWords.length) {
      selectedGenreKeys = byWords;
      lastGenreText = t;
      subgenreManual = false;
    } else {
      selectedGenreKeys = [OTHER_GENRE];
      lastGenreText = t;
      subgenreManual = true;
    }
  } else {
    selectedGenreKeys = [];
    lastGenreText = '';
    subgenreManual = false;
  }
}
initGenreCascade();

// Keep the parent-genre cascade in sync when the story type changes externally
// (load/save round-trips). Only follows store updates that differ from what we
// last rendered — never fights the user typing in the genre combobox.
let lastStoryTypeSync = '';
$: if ($config?.story && $config.story.type !== undefined) {
  const t = String($config.story.type || '');
  if (t !== lastStoryTypeSync) {
    lastStoryTypeSync = t;
    const curText = localStoryCfg.type || '';
    if (t !== curText) {
      // External change (server refresh / undo): re-derive the cascade.
      localStoryCfg.type = t;
      initGenreCascade();
    } else if (selectedGenreKeys.length === 0) {
      const keys = genreLabelsToKeys(t);
      if (keys.length) selectedGenreKeys = keys;
    }
  }
}

// Build the genre combobox options: localized label + short blurb as the
// secondary line of each item (rich item list).
$: genreComboOpts = (() => {
  void novelParamsTick;
  return GENRES.map((g) => ({ value: g.label, label: g.label, desc: genreBlurb(g.key) }));
})();

// Genre combobox "change" handler (single or multi via chips; free text = Other).
function onGenreComboChange(e) {
  const v = String(e.detail.value || '');
  const parts = v.split(',').map((x) => x.trim()).filter(Boolean);
  const keys = [];
  let unknown = [];
  for (const p of parts) {
    const g = GENRES.find((x) => x.label.toLowerCase() === p.toLowerCase() || x.key.toLowerCase() === p.toLowerCase());
    if (g) keys.push(g.key);
    else unknown.push(p);
  }
  if (unknown.length) keys.push(OTHER_GENRE);
  selectedGenreKeys = keys;
  // Stored text: canonical labels first, then any free-text extras verbatim.
  localStoryCfg.type = [...parts.filter((p) => !unknown.includes(p)), ...unknown].join(', ');
  lastGenreText = localStoryCfg.type;
  subgenreManual = unknown.length > 0;
  // Point 7 behaviour: the subgenre resets to Auto whenever the set of parent
  // genres changes — unless the current subgenre still belongs to the new
  // union (then it is kept).
  const union = unionSubgenresFor(keys);
  const cur = (localStoryCfg.subgenre || '').trim();
  if (cur && !union.some((o) => o.toLowerCase() === cur.toLowerCase())) {
    localStoryCfg.subgenre = '';
    lastAutoSubgenre = '';
  }
  refreshCheckedSettings();
}

// NOTE: this helper used to be a SECOND, independent implementation of the
// subgenre filtering — and it was the broken one. It looked presets up with
// their raw lowercase key ("mahou shoujo") in an index keyed by normalized
// tokens, and matched parents against GENRES keys while the backend table
// stores legacy keys ("mystery" vs "mystery_thriller"). Both misses made it
// behave as "no parent known → show it anyway", which is exactly why Fantasy
// subgenres kept appearing under Mystery & Thriller even after the frontend
// cascade was fixed. It now simply delegates to the ONE reactive source of
// truth below (subgenreOptions): there is no second filter path anymore.
function unionSubgenresFor(keys) {
  void novelParamsTick;
  return subgenreOptionsFor(keys);
}

  // Subgenre combobox handler (round 16): the combobox itself is the
  // edit-in-place "Other" — free text typed by the user arrives here as the
  // value and is stored verbatim; picking a listed subgenre stores its label.
  function onSubgenreComboChange(e) {
    const v = String(e.detail.value || '').trim();
    localStoryCfg.subgenre = v;
    const known = subgenreOptions.some((o) => o.toLowerCase() === v.toLowerCase());
    subgenreManual = !!v && !known;
    lastAutoSubgenre = '';
    refreshCheckedSettings();
  }

  // Round 19 (final): handler for the new GenreSubgenrePicker component (the
  // only one wired to the UI now). Same contract as onSubgenreComboChange plus
  // the resolved canonical parent in e.detail.parent.
  function onSubgenrePickerChange(e) {
    // Round 22: multi-select subgenres. The picker/cascade component sends the
    // normalized comma-joined chip list in e.detail.value (and `chips` when
    // available). "Known" means EVERY chip resolves in the filtered option
    // list; free-text chips mark the field as manual, same as before.
    const v = String((e.detail && e.detail.value) || '').trim();
    localStoryCfg.subgenre = v;
    const chips = (e.detail && e.detail.chips) || splitSub(v);
    const known = chips.length > 0 && chips.every(
      (c) => subgenreOptions.some((o) => String(o).toLowerCase() === c.toLowerCase() || (o && o.label && o.label.toLowerCase() === c.toLowerCase())),
    );
    subgenreManual = chips.length > 0 && !known;
    lastAutoSubgenre = '';
    refreshCheckedSettings();
  }
  function splitSub(t) {
    return String(t || '').split(/[,;/\n\r]+/).map((x) => x.trim()).filter(Boolean);
  }

  // Display label for a lowercase backend preset key ("mahou shoujo" ->
  // "Mahou Shoujo"). Used when surfacing presets in the subgenre dropdown.
  function titleCasePreset(s) {
    return String(s || '').replace(/(^|[\s-])(\S)/g, (_m, sep, ch) => sep + ch.toUpperCase());
  }

  // —— Single-parent taxonomy, FRONTEND-ONLY filtering (redesign) ————————
  // The backend (/api/novel-params) ships ALL parameters unfiltered: the full
  // genre buckets (conflict_scales / protagonist_types / specific_settings),
  // every subgenre preset (subgenre_presets) and a per-subgenre catalog with
  // its canonical parent (subgenre_catalogs + subgenre_parents). The frontend
  // is the ONLY place that filters those lists by the selected Genre(s):
  //   * SUBGENRE_INDEX   — normalized token -> {label, parent} for EVERY known
  //     subgenre: static GENRES/SUBGENRE_DATA taxonomy merged with the backend
  //     tables; rebuilt reactively whenever the payload arrives (novelParamsTick
  //     drives every dependent subscription below).
  //   * subgenreOptions  — only entries whose canonical parent is one of the
  //     selected genres.
  //   * conflict/protagonist/settings suggestions — resolved through the index
  //     so they can never leak from another genre: when the stored subgenre
  //     belongs to a different parent than the current genre selection, the
  //     cascade falls back to the genre-level buckets instead of following the
  //     foreign subgenre's data.
  let SUBGENRE_PARENT_TABLE = {};   // /api/novel-params -> subgenre_parents
  let SUBGENRE_CATALOGS = {};       // /api/novel-params -> subgenre_catalogs
  // Round 19: the RAW /api/novel-params payload. GenreSubgenrePicker and the
  // reactive TAX below build the merged single-parent index from this object;
  // assigning it (a fresh reference per fetch) is what drives the cascade to
  // recompute — no manual tick bookkeeping needed for the new path.
  let novelParamsPayload = null;
  // Round 19 fix: normSub IS the taxonomy module's normalizer. Having two
  // copies was a bug source — they must stay byte-identical or index lookups
  // silently miss and the cascade degrades to "show everything".
  const normSub = normSub2;

  // Round 19 (final): SUBGENRE_INDEX is now a plain object view of the ONE
  // merged index built by subgenreTaxonomy.buildSubgenreIndex() from the raw
  // /api/novel-params payload. The previous copy of the merge logic lived
  // here, did NOT normalize legacy backend parent keys ("mystery"/"thriller"
  // vs "mystery_thriller") and consulted stale globals — so server-only
  // presets kept an unresolvable parent and the filter fell back to showing
  // everything. There is exactly one taxonomy implementation now.
  $: TAX = buildSubgenreIndex(novelParamsPayload);

  $: SUBGENRE_INDEX = (() => {
    const idx = {};
    for (const [k, e] of TAX) idx[k] = e;
    return idx;
  })();

  function canonicalParentFor(token) {
    void novelParamsTick;
    const t = normSub(token);
    if (!t) return '';
    const hit = SUBGENRE_INDEX[t];
    if (hit) return hit.parent;
    // Tokens may carry extra descriptive words ("locked room impossible crime"
    // for "Locked Room"): try progressively shorter prefixes, longest first.
    const parts = t.split(' ');
    for (let n = parts.length - 1; n >= 1; n--) {
      const p = SUBGENRE_INDEX[parts.slice(0, n).join(' ')];
      if (p) return p.parent;
    }
    return '';
  }

  // Backend catalog entry for a subgenre spelling (label or snake_case token),
  // or null when the server has no dedicated bucket for it.
  function subgenreCatalogFor(text) {
    void novelParamsTick;
    const t = normSub(text);
    if (!t) return null;
    const underscored = t.replace(/ /g, '_');
    if (SUBGENRE_CATALOGS[underscored]) return SUBGENRE_CATALOGS[underscored];
    for (const k of Object.keys(SUBGENRE_CATALOGS)) {
      if (normSub(k) === t) return SUBGENRE_CATALOGS[k];
    }
    return null;
  }

  // Subgenre options for the selected parent genres — filtered ENTIRELY in the
  // frontend from the merged index. A subgenre appears ONLY under its canonical
  // single parent; presets whose parent cannot be resolved are never dumped
  // into a genre-filtered dropdown (they show only when no parent genre is
  // selected, i.e. free-text "Other" or empty Type).
  //
  // Single filter path: subgenreOptionsFor(keys) is THE implementation and
  // subgenreOptions is just its reactive wrapper over the current selection.
  // (The old code had a second copy of this logic inside unionSubgenresFor,
  // used by the genre-change handler to decide whether to keep the stored
  // subgenre; it missed server-only presets and legacy parent keys, which is
  // why unfiltered entries kept showing up.)
  function subgenreOptionsFor(rawKeys) {
    const keys = (rawKeys || []).filter((k) => k !== OTHER_GENRE);
    // Round 19 (final): delegate to the taxonomy module's single filter —
    // subgenresForKeys() walks EVERY known source (static GENRES/SUBGENRE_DATA
    // taxonomy + backend catalogs/parents/presets merged in TAX), resolves each
    // candidate's canonical parent through resolveParent() (which normalizes
    // the legacy backend bucket keys "mystery"/"thriller" -> "mystery_thriller")
    // and DROPS anything whose parent cannot be attributed. The old local copy
    // of this logic compared raw entry.parent against the selected keys without
    // alias normalization, so every lookup missed and the UI silently fell back
    // to listing everything under every genre.
    return subgenresForKeys(TAX, keys);
  }

  $: subgenreOptions = subgenreOptionsFor(selectedGenreKeys);

  // Combobox option objects for Subgenre: bold label + backend hint as the
  // secondary description line (rich item list). Free-typed values that are
  // not in the catalog are appended as options too, so the combobox keeps
  // showing their localized label instead of a raw key.
  $: subgenreComboOpts = (() => {
    void novelParamsTick;
    const opts = subgenreOptions.map((s) => ({ value: s, label: s, desc: subgenreHint(s) || '' }));
    const cur = (localStoryCfg.subgenre || '').trim();
    if (cur && !opts.some((o) => o.value === cur)) opts.push({ value: cur, label: cur, desc: subgenreHint(cur) || '' });
    return opts;
  })();

  // Same for the genre combobox: keep any free-typed extras selectable/labelled.
  $: genreComboOptsFull = (() => {
    const opts = genreComboOpts;
    for (const p of String(localStoryCfg.type || '').split(',').map((x) => x.trim()).filter(Boolean)) {
      if (!opts.some((o) => o.value.toLowerCase() === p.toLowerCase())) opts.push({ value: p, label: p, desc: '' });
    }
    return opts;
  })();

  // Reactive value shown in the genre/subgenre info box: the stored subgenre
  // (whether a preset label or free-typed "Other" text).
  $: infoSubgenre = String(localStoryCfg.subgenre || '');

// —— Cascade lists: filtered ONLY in the frontend, from the full payload ———
// The backend sends every bucket unfiltered; these subscriptions decide what
// the UI shows for the current Genre + Subgenre selection. Resolution order:
//   1. The selected subgenre's OWN catalog (backend `subgenre_catalogs`,
//      falling back to the static SUBGENRE_DATA), but ONLY when its canonical
//      parent belongs to the current genre selection — a stale foreign
//      subgenre can never hijack the lists of another genre.
//   2. Otherwise the union of the selected genres' buckets (multi-select).
//   3. With no resolved parent genre, the modifier-key heuristic on the raw
//      text keeps anime/webnovel presets usable as standalone selections
//      ("Isekai" typed alone still populates Conflict/Protagonist/Settings).
const MODIFIER_SUBGENRE_KEYS = {
  'lit rpg': 'litrpg', 'litrpg': 'litrpg',
  'isekai': 'isekai', 'dungeon core': 'dungeon', 'tower climbing': 'tower_climbing',
  'battle royale': 'battle_royale', 'regression': 'regression_rebirth', 'villainess': 'isekai:villainess',
  'otome': 'isekai:otome', 'mecha': 'mecha', 'school life': 'school_life', 'spokon': 'spokon',
  'iyashikei': 'iyashikei', 'ecchi': 'ecchi', 'harem': 'harem', 'revenge': 'revenge',
  'solo leveling': 'manhwa_system', 'system manhwa': 'manhwa_system',
};
$: cascadeGenreKey = (() => {
  void novelParamsTick;
  const sg = normSub(localStoryCfg.subgenre);
  // Modifier presets carry their own buckets server-side; use them only when
  // the taxonomy cannot attribute the subgenre to a selected parent anyway.
  const mod = MODIFIER_SUBGENRE_KEYS[sg];
  if (mod && !canonicalParentFor(sg) && (GENRE_CONFLICTS[mod] || GENRE_PROTAGONISTS[mod])) return mod;
  return matchGenreKey(localStoryCfg.type);
})();

// Catalog for the currently selected subgenre — honored ONLY when its single
// canonical parent is among the selected genres (or nothing is selected).
$: subgenreCfg = (() => {
  void novelParamsTick;
  const text = String(localStoryCfg.subgenre || '').trim();
  if (!text) return null;
  const keys = selectedGenreKeys.filter((k) => k !== OTHER_GENRE);
  const parent = canonicalParentFor(text);
  if (parent && keys.length && !keys.includes(parent)) return null; // foreign/stale
  const cat = subgenreCatalogFor(text);
  if (cat && (cat.conflicts?.length || cat.protagonists?.length || cat.settings?.length)) {
    return { conflicts: cat.conflicts || [], protagonists: cat.protagonists || [], settings: cat.settings || [], tones: [] };
  }
  const sd = SUBGENRE_DATA[text] || SUBGENRE_DATA[canonicalSubgenreLabel(text)];
  return sd || null;
})();

// Union of the selected parent genres' buckets (deduped, order preserved).
$: genreUnion = (() => {
  void novelParamsTick;
  const keys = selectedGenreKeys.filter((k) => k !== OTHER_GENRE);
  const mk = (map) => {
    const out = [];
    const seen = new Set();
    for (const k of (keys.length ? keys : cascadeGenreKey ? [cascadeGenreKey] : [])) {
      for (const x of (map[k] || [])) { const lc = String(x).toLowerCase(); if (!seen.has(lc)) { seen.add(lc); out.push(x); } }
    }
    return out;
  };
  return { conflicts: mk(GENRE_CONFLICTS), protagonists: mk(GENRE_PROTAGONISTS), settings: mk(GENRE_SETTINGS) };
})();

$: conflictOpts = (subgenreCfg && subgenreCfg.conflicts.length) ? subgenreCfg.conflicts : genreUnion.conflicts;
$: protagonistOpts = (subgenreCfg && subgenreCfg.protagonists.length) ? subgenreCfg.protagonists : genreUnion.protagonists;
$: toneOpts = subgenreCfg ? subgenreCfg.tones : [];
$: settingSuggestions = (subgenreCfg && subgenreCfg.settings.length) ? subgenreCfg.settings : genreUnion.settings;

// Tone: free-text field with per-subgenre suggestions (NovelWriter tones).
function toneList(v) {
  return (v || '').split(',').map((x) => x.trim()).filter(Boolean);
}
function toggleTone(t) {
  const cur = toneList(localStoryCfg.tone);
  const i = cur.indexOf(t);
  if (i >= 0) cur.splice(i, 1); else cur.push(t);
  localStoryCfg.tone = cur.join(', ');
}
function toneChecked(t) { return toneList(localStoryCfg.tone).includes(t); }

// Field-level tooltip texts (~200 chars) shown by the clickable ⓘ icon next to
// each segmented combobox label. Localized via i18n (en/zh).
$: fieldTip = (key) => $t('config.tip.' + key);

// Structure: single-select combobox (round 17) over every known structure key,
// with the backend beat description as secondary line. Exactly one template
// can be active at a time (stored as a plain string, no chips).
$: structureComboOpts = (() => {
  void novelParamsTick;
  return ALL_STRUCTURE_KEYS.map((k) => ({ value: k, label: $t('config.story.structure.' + k), desc: structHint(k) || '' }));
})();
function onStructureComboChange(e) {
  localStoryCfg.structure = String(e.detail.value || '').trim();
}

// Length: combobox with localized labels (the list is short so filtering just
// narrows it while typing). Changing the length still applies its words-per-
// chapter preset via onLengthChange.
const LENGTH_BLURBS = {
  en: {
    flash: 'Flash fiction: a single continuous piece (under ~1,500 words). One moment, one turn — chapters here are micro-units.',
    short: 'Short story (~1,500–7,500 words): one arc, few characters, immediate resolution. Compact pacing.',
    novelette: 'Novelette (7,500–17,500 words): a fuller arc with one subplot; more room for character growth than a short.',
    novella: 'Novella (17,500–40,000 words): focused plot, limited cast, novelistic depth without epic breadth.',
    light_novel: 'Light Novel: Japanese-style serial prose — short punchy chapters (~3,500 words), dialogue-heavy, arc-driven.',
    anthology: 'Anthology: a collection of self-contained stories linked by theme or world; each entry has its own arc.',
    novel: 'Standard novel (~60k–90k words): full multi-chapter arc; ~4,000-word chapters balance pace and depth.',
    epic: 'Epic novel (100k+ words): multiple POVs/subplots and world-scale stakes; longer ~5,000-word chapters.',
  },

};
$: lengthDescFor = (k) => (LENGTH_BLURBS.en[k]) || '';
$: lengthComboOpts = (() => {
  void novelParamsTick;
  return LENGTH_KEYS.map((k) => ({ value: k, label: $t('config.story.length.' + k), desc: lengthDescFor(k) }));
})();

// Conflict scale / protagonist type: searchable comboboxes over the lists
// derived from the current subgenre/genre (rich "Other": any unmatched text
// typed by the user is stored verbatim in place). Each option carries a short
// explanation shown as the secondary line and by the ⓘ tooltip. The catalogs
// are large (250+ entries coming from the backend), so instead of one blurb
// per term these maps explain each TERM FAMILY (~200 chars, ES/EN) — every
// concrete option resolves to its family via keyword rules below.
const CONFLICT_FAMILIES = {
  en: {
    personal: 'Private stakes lived close to the ground: one relationship, one conscience, one promise at a time. Intimate scope, few people affected.',
    community: 'The friction happens inside a town, guild, crew or school: collective secrets, local power games and reputations that must keep working together.',
    city_nation: 'Whole cities, kingdoms or institutions push back: politics, factions and infrastructure-level consequences for every choice.',
    world_cosmic: 'Existential stakes: the fate of a world, species or reality itself. Enemies are empires, gods, plagues or physics gone wrong.',
    internal: 'The battlefield is the protagonist’s mind — duty vs desire, sanity, identity, grief or ambition tearing them apart while the plot runs.',
    survival: 'Staying alive is the conflict: scarce resources, hostile environments, shrinking safe zones and escalating body counts.',
    quest_trial: 'A goal across space or stages: trials, tournaments, climbs and expeditions where each checkpoint demands growth before the next door opens.',
    mystery_reveal: 'Knowledge as the battleground: clues, cold cases, conspiracies and truths that reframe everything once exposed.',
    relationship: 'Romance and bonds drive the friction: attraction, misread signals, commitment crises and enemies-to-lovers pivots.',
    tech_ai: 'Technology turns on its makers: rogue AIs, corporate control, augmentation costs and systems whose fine print is the real antagonist.',
  },
  es: {
    personal: 'Apuestas íntimas: una relación, una conciencia, una promesa a la vez. Alcance pequeño, pocas personas afectadas.',
    community: 'El roce ocurre en un pueblo, gremio, tripulación o escuela: secretos colectivos y juegos de poder locales.',
    city_nation: 'Ciudades, reinos o instituciones enteras se oponen: política, facciones y consecuencias de infraestructura.',
    world_cosmic: 'Riesgo existencial: el destino de un mundo, especie o realidad. Enemigos: imperios, dioses, plagas o física rota.',
    internal: 'El campo de batalla es la mente del protagonista: deber vs deseo, cordura, identidad o duelo mientras avanza la trama.',
    survival: 'Sobrevivir es el conflicto: recursos escasos, entornos hostiles, zonas seguras que se encogen y tensión creciente.',
    quest_trial: 'Una meta a través de espacios o etapas: pruebas, torneos y expediciones donde cada hito exige crecer antes del siguiente.',
    mystery_reveal: 'El conocimiento como arma: pistas, casos fríos, conspiraciones y verdades que reforman todo al revelarse.',
    relationship: 'El romance y los vínculos generan la fricción: atracción, malentendidos, crisis de compromiso y enemigos-a-amantes.',
    tech_ai: 'La tecnología se vuelve contra su creador: IAs renegadas, control corporativo y sistemas cuya letra pequeña domina.',
  },
};
const PROTAGONIST_FAMILIES = {
  en: {
    chosen: 'Chosen-one archetype: marked by prophecy, system or bloodline; the story tests whether destiny fits the person (or the person remakes destiny).',
    underdog: 'Starts weak and grows visibly: grinding effort, clever cheats or second chances fuel a rise the reader watches rung by rung.',
    professional: 'Defined by a craft or office — detective, doctor, soldier, merchant. The conflict lands on their competence, ethics and chain of command.',
    outsider: 'Doesn’t belong to the world they’re thrown into: isekai’d, immigrant, returnee or plain black sheep; perspective itself drives the plot.',
    antihero: 'Morally gray lead: criminals, villains, revenge-seekers and pragmatists whose methods cost more than the cause promises.',
    leader: 'Responsible for others: captains, heirs, sect masters and commanders whose decisions spend lives, budgets and loyalty.',
    lover: 'Heart-first protagonist: the arc bends around attachment, vulnerability and choosing someone over safety or pride.',
    seeker: 'Driven by questions: explorers, investigators and scholars chasing maps, mysteries or meanings farther than comfort allows.',
    survivor: 'Battered but standing: disaster victims, war orphans and last-of-their-kind figures whose win condition is enduring intact.',
    trickster: 'Wits over strength: con artists, bards, scheming villainesses and court manipulators who improvise their way up the ladder.',
  },
  es: {
    chosen: 'Arquetipo del elegido: marcado por profecía, sistema o linaje; la historia prueba si el destino calza a la persona.',
    underdog: 'Empieza débil y crece a la vista: esfuerzo, trampas ingeniosas o segundas oportunidades alimentan una subida por niveles.',
    professional: 'Definido por un oficio u cargo: detective, médico, soldado, mercader. El conflicto golpea su competencia y ética.',
    outsider: 'No pertenece al mundo donde cae: isekai’d, inmigrante o oveja negra; la perspectiva misma impulsa la trama.',
    antihero: 'Protagonista moralmente gris: criminales, villanos y vengativos cuyos métodos cuestan más de lo que la causa promete.',
    leader: 'Responsable de otros: capitanes, herederos y maestros de secta cuyas decisiones gastan vidas, presupuestos y lealtad.',
    lover: 'Protagonista de corazón: el arco gira en torno al apego, la vulnerabilidad y elegir a alguien sobre la seguridad.',
    seeker: 'Impulsado por preguntas: exploradores e investigadores que persiguen mapas, misterios o significados lejanos.',
    survivor: 'Golpeado pero en pie: víctimas de desastres o guerra cuya condición de victoria es resistir sin romperse.',
    trickster: 'Ingenio sobre fuerza: estafadores, bardos y manipuladores de corte que improvisan su ascenso peldaño a peldaño.',
  },
};
function famKey(text) {
  const t = String(text || '').toLowerCase();
  const has = (...ws) => ws.some((w) => t.includes(w));
  // Conflict families
  if (has('world-saving', 'cosmic', 'extinction', 'ancient evil', 'apocalypse', 'fate of', 'galactic', 'civilization')) return 'world_cosmic';
  if (has('survival', 'daily survival', 'starvat', 'harsh', 'man vs nature', 'drought', 'lethality')) return 'survival';
  if (has('mystery', 'secret', 'cold case', 'conspiracy', 'truth', 'reveal', 'evidence', 'puzzle')) return 'mystery_reveal';
  if (has('love', 'romance', 'relationship', 'marriage', 'engagement', 'enemy-to-lover', 'devotion', 'boundaries')) return 'relationship';
  if (has('ai', 'technology', 'corporate', 'corporation', 'system', 'utility', 'data', 'augmentation')) return 'tech_ai';
  if (has('self', 'vs meaning', 'identity', 'grief', 'sanity', 'ego', 'duty vs', 'choice', 'temptation', 'obsession')) return 'internal';
  if (has('kingdom', 'empire', 'political', 'intrigue', 'city', 'nation', 'court', 'sect', 'clan', 'faction', 'war')) return 'city_nation';
  if (has('community', 'town', 'village', 'club', 'school', 'guild', 'crew', 'team', 'family', 'generational', 'neighborhood')) return 'community';
  if (has('quest', 'trial', 'tournament', 'climb', 'expedition', 'boss', 'checkpoint', 'rank', 'level', 'milestone', 'ladder', 'duel')) return 'quest_trial';
  if (has('personal', 'individual', 'private', 'one ', 'career', 'debt', 'promise')) return 'personal';
  return '';
}
function protagFamKey(text) {
  const t = String(text || '').toLowerCase();
  const has = (...ws) => ws.some((w) => t.includes(w));
  if (has('chosen', 'prophesied', 'destined', 'hero of', 'elixir')) return 'chosen';
  if (has('underdog', 'late-', 'reincarnat', 'regress', 'second chance', 'grind', 'weak', 'prodigy', 'rising')) return 'underdog';
  if (has('detective', 'doctor', 'medic', 'soldier', 'officer', 'captain', 'merchant', 'baker', 'librarian', 'teacher', 'engineer', 'pilot', 'professional', 'specialist', 'investigator')) return 'professional';
  if (has('outsider', 'stranger', 'transmigrat', 'isekai', 'immigrant', 'returnee', 'commoner', 'borrowed')) return 'outsider';
  if (has('antihero', 'villain', 'criminal', 'thief', 'assassin', 'revenge', 'rogue', 'corrupted', 'cursed')) return 'antihero';
  if (has('leader', 'heir', 'prince', 'princess', 'queen', 'king', 'master', 'commander', 'lord', 'noble', 'founder')) return 'leader';
  if (has('lover', 'romantic', 'bride', 'groom', 'heart', 'suitor', 'maiden chasing', 'widow')) return 'lover';
  if (has('seeker', 'explorer', 'scholar', 'researcher', 'cartographer', 'archaeolog', 'wanderer', 'chronicler')) return 'seeker';
  if (has('survivor', 'victim', 'orphan', 'refugee', 'last of', 'patient', 'hostage')) return 'survivor';
  if (has('trickster', 'con ', 'con-artist', 'grifter', 'bard', 'schemer', 'impostor', 'jester', 'charmer')) return 'trickster';
  return '';
}
function itemDesc(text, fams, keyFn) {
  const lang = 'en'; // UI catalog ships English only
  const k = keyFn(text);
  return k ? (fams[lang] || fams.en)[k] || '' : '';
}
$: conflictComboOpts = conflictOpts.map((c) => ({ value: c, label: c, desc: itemDesc(c, CONFLICT_FAMILIES, famKey) }));
$: protagonistComboOpts = protagonistOpts.map((p) => ({ value: p, label: p, desc: itemDesc(p, PROTAGONIST_FAMILIES, protagFamKey) }));
function onConflictComboChange(e) {
  const v = String(e.detail.value || '').trim();
  if (conflictOpts.includes(v)) localStoryCfg.conflict_other = '';
  localStoryCfg.conflict_scale = v;
}
function onProtagonistComboChange(e) {
  const v = String(e.detail.value || '').trim();
  if (protagonistOpts.includes(v)) localStoryCfg.protagonist_other = '';
  localStoryCfg.protagonist_type = v;
}

// Info box (round 11): one-line description of the selected parent genre,
// keyed by GENRES[].key. The subgenre side of the box uses the backend
// subgenre_hints (via subgenreHint()); these static blurbs cover the genre.
const GENRE_BLURBS = {
  en: {
    fantasy: 'Imaginative worlds with magic, mythical creatures or supernatural rules — from epic secondary worlds to urban hidden societies.',
    scifi: 'Speculative fiction grounded in technology, science or future societies: space opera, cyberpunk, dystopia, first contact…',
    mystery: 'Crime and puzzles at the center: an investigation drives the plot, from cozy village whodunits to hard-boiled noir and heists.',
    romance: 'The love story is the main plot — emotional arcs, relationship obstacles and a satisfying ending are promised to the reader.',
    thriller: 'Pace, danger and suspense: chases, conspiracies and high stakes, whether espionage, legal, domestic or action-driven.',
    horror: 'Designed to frighten and unsettle: supernatural dread, cosmic insignificance, body terror or the monsters next door.',
    historical: 'Fiction anchored in a real period, with period-authentic settings, customs and events shaping the characters’ lives.',
    western: 'Frontier stories of lawlessness, honor codes and open land — classic, spaghetti, weird or transplanted to space.',
    drama: 'Character-first literary fiction: family sagas, coming-of-age, psychorealism, metafiction, existentialist and philosophical stories.',
    adventure: 'Movement-driven fiction: quests, exploration, survival and treasure hunts where the journey and its dangers are the plot — includes martial-arts, superhero and battle-royale action.',
  },

};
function genreBlurb(key) {
  return (GENRE_BLURBS.en)[key] || '';
}

// Info-box lines: one blurb per selected parent genre (multi-select aware).
function genreBlurbLines() {
  return selectedGenreKeys.filter((k) => k !== OTHER_GENRE).map((k) => genreBlurb(k)).filter(Boolean);
}

// (Round 16: restoreSubgenreSelect removed — the Subgenre combobox is always
// editable; free text IS the "Other" value, no separate mode to leave.)

// Specific settings as checkboxes: parse the stored newline-separated list into a set.
let checkedSettings = new Set();
let customSettingsText = '';
function refreshCheckedSettings() {
  const all = (localStoryCfg.specific_settings || '').split('\n').map((x) => x.trim()).filter(Boolean);
  checkedSettings = new Set(all);
  customSettingsText = all.filter((x) => !(settingSuggestions || []).includes(x)).join('\n');
}
refreshCheckedSettings();
function rebuildSpecificSettings() {
  const custom = customSettingsText.split('\n').map((x) => x.trim()).filter(Boolean);
  const merged = [...new Set([...custom, ...[...checkedSettings]])];
  localStoryCfg.specific_settings = merged.join('\n');
}
function toggleSetting(s) {
  const cur = new Set(checkedSettings);
  if (cur.has(s)) cur.delete(s); else cur.add(s);
  checkedSettings = cur;
  rebuildSpecificSettings();
}
function settingChecked(s) { return checkedSettings.has(s); }
function addSetting(s) {
const cur = (localStoryCfg.specific_settings || '').split('\n').map((x) => x.trim()).filter(Boolean);
if (!cur.includes(s)) localStoryCfg.specific_settings = [...cur, s].join('\n');
}

// Randomize tick: bumped whenever randomizeStoryParams() rewrites the form.
// Passed into GenreCascadeFields as `bump` so its SmartCombobox widgets
// (Genre/Subgenre/Conflict/Protagonist) re-sync from cfg instead of keeping
// the value they were seeded with — without this the cascade fields never
// visibly changed and Randomize looked dead.
let cascadeBumpTick = 0;
function cascadeBump() { void cascadeBumpTick; }

// —— Randomize (Story parameters) —
// Return a random element from an array (empty-safe). Used by the randomizer
// below; without it every click threw ReferenceError and silently did nothing.
function pick(arr) {
  if (!Array.isArray(arr) || !arr.length) return '';
  return arr[Math.floor(Math.random() * arr.length)];
}

// Fill the story-parameter fields with a coherent random combination. The
// genre is picked FIRST because everything downstream depends on it: the
// subgenre list comes from the chosen genre, and conflicts / protagonists /
// tones / specific-setting suggestions come from the chosen subgenre (with a
// genre-level fallback, mirroring the reactive option lists above).
function randomizeStoryParams() {
  // 1) Genre first — the subgenre options are derived from it.
  const g = pick(GENRES);
  selectedGenreKeys = [g.key];
  lastGenreText = g.label;
  subgenreManual = false;
  localStoryCfg.type = g.label;
  // 2) Then a subgenre *of that genre*, so the cascade stays consistent.
  const sg = pick(g.subgenres);
  localStoryCfg.subgenre = sg;
  lastAutoSubgenre = sg;
  // 3) Fields that depend on the chosen genre/subgenre. Prefer the SAME
  //    reactive option lists the UI renders (subgenreCfg → genreUnion), so the
  //    randomized values always exist in the visible dropdowns; fall back to
  //    the raw static tables when a list is empty.
  const sd = SUBGENRE_DATA[sg] || null;
  const conflicts = (conflictOpts && conflictOpts.length) ? conflictOpts : (sd ? sd.conflicts : (GENRE_CONFLICTS[g.key] || []));
  const protags = (protagonistOpts && protagonistOpts.length) ? protagonistOpts : (sd ? sd.protagonists : (GENRE_PROTAGONISTS[g.key] || []));
  const settings = (settingSuggestions && settingSuggestions.length) ? settingSuggestions : (sd ? sd.settings : (GENRE_SETTINGS[g.key] || []));
  const tones = sd ? sd.tones : [];
  localStoryCfg.conflict_scale = pick(conflicts);
  localStoryCfg.conflict_other = '';
  localStoryCfg.protagonist_type = pick(protags);
  localStoryCfg.protagonist_other = '';
  localStoryCfg.specific_settings = (() => {
    if (!settings.length) return '';
    const shuffled = [...settings].sort(() => Math.random() - 0.5);
    const n = 2 + Math.floor(Math.random() * Math.min(3, Math.max(1, shuffled.length)));
    return shuffled.slice(0, n).join('\n');
  })();
  refreshCheckedSettings();
  // Tone: pick from the reactive suggestion list computed for the new
  // genre/subgenre (toneOpts), falling back to the raw subgenre data.
  localStoryCfg.tone = (() => {
    const pool = (toneOpts && toneOpts.length) ? toneOpts : tones;
    if (!pool || !pool.length) return '';
    const shuffled = [...pool].sort(() => Math.random() - 0.5);
    return shuffled.slice(0, 1 + Math.floor(Math.random() * Math.min(3, pool.length))).join(', ');
  })();
  // 4) Length first, then a structure valid for that length (same dependency
  //    rule the UI enforces via onLengthChange).
  const len = pick(LENGTH_KEYS);
  localStoryCfg.story_length = len;
  const structOptsForLen = structureOptions(len);
  localStoryCfg.structure = structOptsForLen.length ? pick(structOptsForLen) : DEFAULT_STRUCTURE[len];
  // Mirror the onLengthChange side effect the user would have gotten: the
  // target-words-per-chapter preset for the new length.
  const wpcPreset = TARGET_WORDS_BY_LENGTH[len];
  if (wpcPreset) localStoryCfg.target_words_per_chapter = wpcPreset;
  // 5) Independent enum selects — option values copied verbatim from the UI.
  localStoryCfg.gender_bias = pick(['random', 'balanced', 'male', 'female']);
  localStoryCfg.target_audience = pick(['kid', 'middle_grade', 'ya', 'new_adult', 'adult', 'all_ages']);
  localStoryCfg.romance_level = pick(['random', 'none', 'subplot', 'moderate', 'central']);
  localStoryCfg.sexual_content = pick(['random', 'clean', 'fade_to_black', 'explicit']);
  localStoryCfg.gore_level = pick(['random', 'none', 'mid', 'explicit']);
  localStoryCfg.world_darkness = pick(['idyllic', 'temperate', 'gritty', 'grim', 'abyssal']);
  novelParamsTick++;
  // 6) Re-sync the GenreCascadeFields widgets with the freshly written cfg
  //    values (they intentionally ignore external edits after their one-shot
  //    seed unless we bump them here).
  cascadeBumpTick++;
}
  let testingApi = false;

  // Novel parameters: ALL story structures are always available; the selected
  // length only suggests a default (and its word-per-chapter preset). The
  // catalogs below are refreshed from /api/novel-params so the backend stays
  // the single source of truth.
  const DEFAULT_STRUCTURE = { flash: 'three_act', short: 'three_act', novelette: 'three_act', novella: 'three_act', light_novel: 'kishotenketsu', anthology: 'episodic', novel: 'six_act', epic: 'six_act' };
  function structureOptions() { return ALL_STRUCTURE_KEYS; }
  $: structOpts = structureOptions();
  // UI-reorder (novel-params): SmartSelect option lists for the Length &
  // Structure cells now rendered directly in the Story grid (under Gore
  // Level). Same data pipeline as lengthComboOpts/structureComboOpts but with
  // the `description` field SmartSelect reads for its two-line rows.
  $: lengthOpts = (() => {
    void novelParamsTick;
    return LENGTH_KEYS.map((k) => ({ value: k, label: $t('config.story.length.' + k), description: lengthDescFor(k) }));
  })();
  $: structureOpts = (() => {
    void novelParamsTick;
    return ALL_STRUCTURE_KEYS.map((k) => ({ value: k, label: $t('config.story.structure.' + k), description: structHint(k) || '' }));
  })();
  function onLengthChange(e) {
    // The Story-length SmartSelect also binds cfg.story_length directly; when
    // called from a change event make sure the preset logic sees the final
    // value regardless of bind/on:change ordering.
    if (e && e.detail) localStoryCfg.story_length = String(e.detail.value || '').trim();
    // 1) Apply the target-words-per-chapter preset for the new length. The
    //    field stays freely editable afterwards; unknown lengths keep it as is.
    const preset = TARGET_WORDS_BY_LENGTH[localStoryCfg.story_length];
    if (preset) localStoryCfg.target_words_per_chapter = preset;
    // 2) Suggest a default structure when empty or previously auto-set; manual
    //    choices and free-text values are never clobbered by a length change.
    const cur = (localStoryCfg.structure || '').trim();
    const firstPart = cur.split(',')[0].trim();
    const wasAuto = !cur || Object.values(DEFAULT_STRUCTURE).includes(firstPart);
    if (wasAuto) localStoryCfg.structure = DEFAULT_STRUCTURE[localStoryCfg.story_length] || 'three_act';
  }

  function onStructureChange(e) {
    if (e && e.detail) localStoryCfg.structure = String(e.detail.value || '').trim();
  }

  // AI section generation (based on the story parameters currently in the form)
  let genBusy = { ...sharedGenBusy };
  // Last server error returned by a Generate click (shown as a toast so a
  // rejected request is never silently swallowed — "nothing happens" bug).
  let genLastError = '';

  // Generations run as *component-independent* background tasks: the POST is
  // fire-and-forget and a module-level poller watches /api/status until the
  // backend task finishes, then refreshes config/settings and clears the busy
  // flags. Nothing depends on this component staying mounted, so switching
  // workspace tabs (which destroys and recreates it) can never orphan a task,
  // freeze $taskRunning, or leave the Generate buttons dead until an F5.
  const adoptedSections = Object.keys(sharedGenBusy).filter((s) => sharedGenBusy[s]);

  function applyGeneratedStory(story) {
    if (!story) return;
    localStoryCfg = { ...localStoryCfg, ...story };
    if (!localStoryCfg.gender_bias) localStoryCfg.gender_bias = 'random';
    initGenreCascade();
  }

  async function refreshAfterGeneration(finishedSections = []) {
    try { settings.set(await api('GET', '/api/settings')); } catch (e) {}
    try {
      const cfg = await api('GET', '/api/config');
      config.set(cfg);
      // Adopt the LLM's result into the visible inputs. The server value wins
      // unconditionally for the fields owned by the sections that just
      // finished — a generation *replaces* those fields, so keeping a stale
      // local copy would leave the input looking empty even though the LLM
      // answered. Fields not owned by a finished section keep the previous
      // rule (adopt only when the local field is empty), so unrelated unsaved
      // edits are never clobbered.
      const st = cfg?.story || {};
      const owned = new Set();
      for (const sec of finishedSections) {
        if (sec.startsWith('enhance:')) { owned.add(sec.slice('enhance:'.length)); continue; }
        // Theme & Motif card now generates theme/motif/tone/dilemma only —
        // reader_expectation moved to the "Ideal reader profile" card.
        if (sec === 'motif') { owned.add('theme'); owned.add('motif'); owned.add('tone'); owned.add('philosophical_dilemma'); }
        else if (sec === 'inspirational_pieces') owned.add('inspirational_pieces');
        else if (sec === 'story_idea') { owned.add('story_idea'); owned.add('forces_in_tension'); owned.add('elevator_pitch'); owned.add('lighthouse_sentence'); }
        // Ideal reader profile card fills BOTH its fields together (user request).
        else if (sec === 'audience_profile') { owned.add('audience_profile'); owned.add('reader_expectation'); }
        else if (sec === 'reader_expectation') owned.add('reader_expectation');
        else if (sec === 'differentiating_idea') owned.add('differentiating_idea');
        else if (sec === 'style') { owned.add('writing_style'); owned.add('writing_pov'); }
      }
      const merged = {};
      for (const k of ['theme', 'motif', 'tone', 'specific_settings', 'inspirational_pieces', 'story_idea', 'audience_profile', 'writing_style', 'writing_pov', 'philosophical_dilemma', 'reader_expectation', 'forces_in_tension', 'elevator_pitch', 'lighthouse_sentence', 'differentiating_idea']) {
        const sv = (st[k] || '').trim();
        if (!sv) continue;
        if (owned.has(k) || !(localStoryCfg[k] || '').trim()) merged[k] = st[k];
      }
      applyGeneratedStory(merged);
    } catch (e) {}
  }

  function reconcileUndoSnapshot(snap, s) {
    if (!snap || !(snap.scope.type === 'all' || snap.scope.type === 'pair')) return;
    const t = snap.scope.entityType;
    const nowIds = (() => {
      if (t === 'character') return (s.characters || []).map(c => c.id);
      if (t === 'organization') return (s.organizations || []).map(o => o.id);
      if (t === 'worldview') return (s.worldview || []).map(w => w.id);
      if (t === 'relation') return (s.relations || []).map(r => r.id);
      return [];
    })();
    const beforeIds = new Set(snap.scope.type === 'pair'
      ? (snap.beforePairs || []).map(r => r.id)
      : Object.keys(snap.before || {}));
    let afterNew = nowIds.filter(id => !beforeIds.has(id));
    if (snap.scope.type === 'pair') {
      const list = (s.relations || []).filter(r => r.source_id === snap.scope.srcId && r.target_id === snap.scope.tgtId);
      afterNew = list.filter(r => !beforeIds.has(r.id)).map(r => r.id);
    }
    snap.afterNew = afterNew;
  }

  function startGenPolling() {
    if (genPollTimer) return;
    // Safety net: the poller must never run forever. If /api/status keeps
    // reporting a running task way past any realistic generation (or the
    // endpoint errors out repeatedly), stop watching after 10 minutes and
    // release the busy flags so the Generate buttons don't stay deadlocked
    // until an F5. The server-side task itself is unaffected.
    const startedAt = Date.now();
    genPollTimer = setInterval(async () => {
      let st = null;
      try { st = await api('GET', '/api/status').catch(() => null); } catch (e) {}
      if (st?.is_task_running && Date.now() - startedAt < 600000) return; // still generating
      clearInterval(genPollTimer);
      genPollTimer = null;
      const finished = Object.keys(sharedGenBusy);
      for (const s of finished) { delete sharedGenBusy[s]; }
      taskRunning.set(false);
      const sections = finished.length ? finished.slice() : [''];
      for (const sec of sections) {
        genBusy = { ...genBusy, [sec]: false };
        addToast($t('config.generate.done'), 'success');
        try { reconcileUndoSnapshot(sharedGenUndo[sec], get(settings)); } catch (e) {}
      }
      await refreshAfterGeneration(sections);
    }, 1200);
  }

  // —— Undo support for AI-generated settings ——
  // Before a generation runs we snapshot the affected state; "undo" restores it,
  // so a bad generation can be reverted without leaving the whole project touched.
  let genUndo = { ...sharedGenUndo };

  // Story-config fields (writing_style/pov, theme/motif, story_idea) are plain
  // strings inside the config file: snapshot them as a map and restore via PUT.
  function captureStoryFields(key, fields) {
    // Snapshot the *form* values (what the user currently sees), so undo
    // restores exactly the pre-generation state even if it was unsaved.
    const cur = localStoryCfg || ($config?.story || {});
    const snap = {};
    for (const f of fields) snap[f] = cur[f] ?? '';
    genUndo = { ...genUndo, [key]: { storyFields: snap } };
    sharedGenUndo[key] = genUndo[key];
  }
  async function undoStoryFields(key) {
    const snap = genUndo[key];
    if (!snap || !snap.storyFields) return false;
    try {
      const st = { ...($config?.story || {}), ...snap.storyFields };
      await api('PUT', '/api/config', { ...($config || {}), story: st });
      config.set(await api('GET', '/api/config'));
      localStoryCfg = { ...localStoryCfg, ...snap.storyFields };
      genUndo = { ...genUndo, [key]: undefined };
      delete sharedGenUndo[key];
      addToast($t('config.generate.undoDone'), 'success');
    } catch (e) { addToast(e.message, 'error'); return false; }
    return true;
  }

  function entitySnap(type, id) {
    if (type === 'character') return ($settings?.characters || []).find(c => c.id === id) || null;
    if (type === 'organization') return ($settings?.organizations || []).find(o => o.id === id) || null;
    if (type === 'worldview') return ($settings?.worldview || []).find(w => w.id === id) || null;
    if (type === 'relation') return ($settings?.relations || []).find(r => r.id === id) || null;
    return null;
  }

  function listIds(type) {
    if (type === 'character') return ($settings?.characters || []).map(c => c.id);
    if (type === 'organization') return ($settings?.organizations || []).map(o => o.id);
    if (type === 'worldview') return ($settings?.worldview || []).map(w => w.id);
    if (type === 'relation') return ($settings?.relations || []).map(r => r.id);
    return [];
  }

  function captureSnapshot(key, scope) {
    const snap = { scope };
    if (scope.type === 'entity') {
      snap.entity = entitySnap(scope.entityType, scope.entityId);
    } else if (scope.type === 'pair') {
      // Relations between the chosen pair that already exist (either direction).
      snap.beforePairs = ($settings?.relations || []).filter(r =>
        (r.source_id === scope.srcId && r.target_id === scope.tgtId) ||
        (r.source_id === scope.tgtId && r.target_id === scope.srcId));
    } else {
      const ids = scope.ids || listIds(scope.entityType);
      snap.byType = scope.entityType;
      snap.before = {};
      ids.forEach(id => { const e = entitySnap(scope.entityType, id); if (e) snap.before[id] = e; });
    }
    genUndo = { ...genUndo, [key]: snap };
    sharedGenUndo[key] = snap;
  }

  async function restoreEntity(type, id, ent) {
    if (!ent) return;
    if (type === 'character') await api('PUT', '/api/characters/' + id, ent);
    else if (type === 'organization') await api('PUT', '/api/organizations/' + id, ent);
    else if (type === 'worldview') await api('PUT', '/api/worldview/' + id, ent);
    else if (type === 'relation') await api('PUT', '/api/relations/' + id, ent);
  }

  async function removeEntity(type, id) {
    if (type === 'character') await api('DELETE', '/api/characters/' + id);
    else if (type === 'organization') await api('DELETE', '/api/organizations/' + id);
    else if (type === 'worldview') await api('DELETE', '/api/worldview/' + id);
    else if (type === 'relation') await api('DELETE', '/api/relations/' + id);
  }

  async function undoGenerate(key) {
    const snap = genUndo[key];
    if (!snap) return;
    if (snap.storyFields) { await undoStoryFields(key); return; }
    try {
      if (snap.scope.type === 'entity') {
        const t = snap.scope.entityType, id = snap.scope.entityId;
        if (snap.entity) await restoreEntity(t, id, snap.entity);
        else if (id !== '__new__') await removeEntity(t, id);
      } else if (snap.scope.type === 'pair') {
        // Single-pair relation generation: drop the relations that connect the
        // chosen pair and did not exist before.
        for (const r of (snap.beforePairs || [])) {
          const cur = ($settings?.relations || []).find(x => x.id === r.id);
          if (!cur) continue;
          if (r.label !== undefined && cur.label !== r.label) await restoreEntity('relation', r.id, r);
        }
        for (const id of (snap.afterNew || [])) {
          await removeEntity('relation', id);
        }
      } else {
        // Batch generation: restore every entity that existed before, delete
        // the ones the generation added.
        const t = snap.scope.entityType;
        for (const [id, ent] of Object.entries(snap.before || {})) {
          await restoreEntity(t, id, ent);
        }
        for (const id of (snap.afterNew || [])) {
          await removeEntity(t, id);
        }
      }
      genUndo = { ...genUndo, [key]: undefined };
      delete sharedGenUndo[key];
      settings.set(await api('GET', '/api/settings'));
      addToast($t('config.generate.undoDone'), 'success');
    } catch (e) { addToast(e.message, 'error'); }
  }

  // Kick off a section generation. Config-page "Generate" buttons go through
  // the chat assistant (generate_section tool) so every generation is visible
  // and steerable in the side panel; startGeneration() keeps the direct HTTP
  // path for single-entity form buttons (one character / worldview entry / org
  // / relation pair), which carry ids the chat should not have to infer.
  // Returns true only if the request was actually accepted by the backend.
  async function startGeneration(section, extra = {}, undoKey = section) {
    // Only block on *our own* in-flight generation of this section. The global
    // $taskRunning flag is deliberately NOT checked here: a stale "running"
    // flag (left over from another page's task or an aborted request) used to
    // silently swallow every Generate click — the "nothing happens" bug. If a
    // real task is running server-side, the POST answers 409 and we surface it.
    // Guard only against *this section* already being in flight. The global
    // $taskRunning flag and other sections' flags are intentionally NOT
    // blockers here: a stale "running" state used to make every Generate
    // click silently no-op (no network request at all — the reported bug).
    // If the server is genuinely busy, tryStartTask answers 409 and we show
    // that error as a toast instead of swallowing the click.
    if (genBusy[section] || sharedGenBusy[section]) return false;
    // Mark the button busy *synchronously*, before any await: the POST carries
    // the full payload but must not gate the UI feedback. Without this, a slow
    // network made rapid double-clicks fire several generations at once.
    genBusy = { ...genBusy, [section]: true };
    sharedGenBusy[section] = true;
    taskRunning.set(true);
    const story = storyPayload();
    try {
      await api('POST', '/api/generate/' + section, { story_idea: (story.story_idea || '').trim(), story, ...extra });
      genLastError = '';
      addToast($t('config.generate.started'), 'info');
      startGenPolling();
      return true;
    } catch (e) {
      delete sharedGenBusy[section];
      genBusy = { ...genBusy, [section]: false };
      if (!Object.values(sharedGenBusy).some(Boolean)) taskRunning.set(false);
      genLastError = e.message;
      addToast(e.message, 'error');
      return false;
    }
  }
  function storyPayload() {
    return { ...localStoryCfg, target_words_per_chapter: Number(localStoryCfg.target_words_per_chapter) || 2500 };
  }
  // Persist the current form into the project config *before* generating, so
  // the LLM always works from what the author just typed in "Story config",
  // "Theme & Motif" and "Ideal reader profile" — even if they never pressed
  // Save. (The backend also receives the live payload, but saving first keeps
  // the project state and the generation seed identical.)
  async function persistStoryBeforeGenerate(strict = false) {
    const story = storyPayload();
    try {
      const saved = await api('PUT', '/api/config', { ...($config || {}), story });
      config.set(saved);
      storyCfgSnapshot = JSON.stringify(saved?.story || '');
      return true;
    } catch (e) {
      // A task running server-side rejects PUT /api/config (409). Don't let
      // that abort the flow: the generate POST carries the live payload and
      // will report the real conflict itself. Enhance passes strict=true
      // because its fast path reads the draft from the saved config.
      if (strict) throw e;
      return false;
    }
  }
  async function generateSection(section, extra = {}, undoKey = section) {
    // DEBUG: temporary breadcrumb so a dead button is obvious even when the
    // bundle on disk is stale or an early guard below returns silently.
    console.log('[generate] clicked', section);
    // Only block when *this* section already has a generation in flight. We
    // never consult the global $taskRunning store here: a stale-true flag
    // (SSE having missed a task_end, or a task started from another page)
    // used to make every Generate click a silent no-op — no fetch at all,
    // nothing in the Network tab. Server-side conflicts surface as a 409
    // toast from startGeneration() instead.
    if (genBusy[section] || sharedGenBusy[section]) { console.log('[generate] blocked by busy flag', section); return; }
    // Snapshot what this generation may touch, so the user can undo it.
    if (section === 'characters') captureSnapshot(undoKey, { type: 'all', entityType: 'character' });
    else if (section === 'organizations') captureSnapshot(undoKey, { type: 'all', entityType: 'organization' });
    else if (section === 'relations') captureSnapshot(undoKey, extra.source_id ? { type: 'pair', entityType: 'relation', srcId: extra.source_id, tgtId: extra.target_id } : { type: 'all', entityType: 'relation' });
    else if (section === 'worldview') captureSnapshot(undoKey, extra.entry_id || extra.name ? { type: 'entity', entityType: 'worldview', entityId: extra.entry_id || '__new__' } : { type: 'all', entityType: 'worldview', ids: allWvs.map(w => w.id) });
    else if (section === 'locations') captureSnapshot(undoKey, { type: 'all', entityType: 'worldview', ids: allWvs.map(w => w.id) });
    else if (section.startsWith('wv_')) captureSnapshot(undoKey, { type: 'all', entityType: 'worldview', ids: allWvs.map(w => w.id) });
    else if (section === 'style') captureStoryFields(undoKey, ['writing_style', 'writing_pov']);
    else if (section === 'motif') captureStoryFields(undoKey, ['theme', 'motif', 'tone', 'philosophical_dilemma']);
    else if (section === 'inspirational_pieces') captureStoryFields(undoKey, ['inspirational_pieces']);
    else if (section === 'story_idea') captureStoryFields(undoKey, ['story_idea', 'forces_in_tension', 'elevator_pitch', 'lighthouse_sentence']);
    // The Ideal reader profile card now owns both fields (reader_expectation
    // moved here from Theme & Motif — user request).
    else if (section === 'audience_profile') captureStoryFields(undoKey, ['audience_profile', 'reader_expectation']);
    else if (section === 'reader_expectation') captureStoryFields(undoKey, ['reader_expectation']);
    else if (section === 'differentiating_idea') captureStoryFields(undoKey, ['differentiating_idea']);
    await persistStoryBeforeGenerate();
    const story = storyPayload();
    const idea = (story.story_idea || '').trim() || ($config?.story?.story_idea || '').trim();
    if (!idea && section !== 'motif' && section !== 'inspirational_pieces' && section !== 'story_idea' && section !== 'audience_profile' && section !== 'style' && section !== 'reader_expectation' && section !== 'differentiating_idea') { addToast($t('config.story_idea.required'), 'error'); discardUndo(undoKey); return; }
    // Section-level Generate buttons now go through the chat assistant: the
    // message below is matched by the agent's system prompt, which MUST call
    // the generate_section tool with this exact section key. The tool reuses
    // the same background runner as startGeneration(), so progress polling,
    // toasts and undo keep working unchanged.
    const chatMsgs = {
      style: $t('config.generate.chat.style'),
      characters: $t('config.generate.chat.characters'),
      organizations: $t('config.generate.chat.organizations'),
      relations: $t('config.generate.chat.relations'),
      locations: $t('config.generate.chat.locations'),
      worldview: $t('config.generate.chat.worldview'),
      wv_geography: $t('config.generate.chat.wv_geography'),
      wv_location: $t('config.generate.chat.wv_location'),
      wv_faction: $t('config.generate.chat.wv_faction'),
      wv_rule: $t('config.generate.chat.wv_rule'),
      wv_knowledge: $t('config.generate.chat.wv_knowledge'),
      wv_history: $t('config.generate.chat.wv_history'),
      wv_other: $t('config.generate.chat.wv_other'),
      motif: $t('config.generate.chat.motif'),
      inspirational_pieces: $t('config.generate.chat.inspirational_pieces'),
      story_idea: $t('config.generate.chat.story_idea'),
      audience_profile: $t('config.generate.chat.audience_profile'),
      reader_expectation: $t('config.generate.chat.reader_expectation'),
    };
    const msg = section === 'differentiating_idea'
      // Story appeal: the creativity level travels through the deterministic
      // chat fast path — the agent parses [generate-section:X;creativity=N]
      // and forwards N to the generator (prompt ambition + sampling temp).
      ? `${$t('config.generate.chat.differentiating_idea')} [creativity=${appealCreativity}]`
      : chatMsgs[section];
    console.log('[generate] dispatching via chat?', section, 'msg found:', !!msg);
    if (msg) {
      genBusy = { ...genBusy, [section]: true };
      sharedGenBusy[section] = true;
      // Immediate feedback: the busy flag alone is easy to miss on a small
      // button, and previously every failure below was silent.
      addToast($t('config.generate.chat.dispatched', { section }), 'info');
      try {
        // sendToChat resolves as soon as the POST is accepted (message visible
        // in the panel, agent turn running server-side). No watchdog needed:
        // sendMessageToChat now returns at accept time, so failures are
        // always immediate throws instead of 30 s silent hangs.
        await sendToChat(msg);
        // The agent's generate_section tool kicked off the same background
        // task the HTTP buttons used to start directly; poll its progress so
        // the busy flag clears and results refresh when it finishes.
        startGenPolling();
      } catch (e) {
        console.log('[generate] send failed', e?.message || String(e));
        delete sharedGenBusy[section];
        genBusy = { ...genBusy, [section]: false };
        if (!Object.values(sharedGenBusy).some(Boolean)) taskRunning.set(false);
        addToast(e?.message || String(e), 'error');
      }
      return;
    }
    await startGeneration(section, extra, undoKey);
  }
  function discardUndo(key) {
    genUndo = { ...genUndo, [key]: undefined };
    delete sharedGenUndo[key];
  }
  // NOTE: this project uses Svelte 4, where the event directive is `on:click`.
  // A plain `onclick` property passed through a spread ({...props}) or written
  // literally on an element is treated as a *static attribute* in Svelte 4 and
  // is NEVER wired as an event listener — that is why every Generate button
  // was a dead no-op (nothing in DevTools Network, no toast). The helpers
  // below now only return visual props; each <button> binds on:click directly.
  // NOTE: do NOT call e.stopPropagation() here. The click already reaches us
  // via Svelte component event forwarding from <AiGenerateButton>, whose inner
  // <button> stops native propagation. Calling stopPropagation on the forwarded
  // (non-native) event throws a TypeError, which previously made every section
  // Generate button silently fail ("sections no longer load/generate").
  // —— Story appeal (differentiating idea) ——
  // The aurora "Suggest" bulb dispatches the differentiating_idea section
  // through the chat assistant (generateSection), exactly like every other
  // Generate button: same busy/undo/polling machinery and — critically — the
  // same SSE token_usage pipeline, so the sent/received token chip shows up.
  // The slider level travels inside the chat message ([creativity=N]).
  let appealCreativity = 3;      // 1 Pragmatic .. 5 Disruptive (default Balanced)
  let appealPanelOpen = false;   // "Sugerir" collapsible bar state

  $: appealLevelLabel = $t('config.appeal.level' + appealCreativity);

  function toggleAppealPanel() { appealPanelOpen = !appealPanelOpen; }

  // The aurora bulb button now goes through the SAME chat path as every other
  // section Generate button — generateSection('differentiating_idea'):
  //   - the message carries the [generate-section:differentiating_idea] tag
  //     plus the slider level ([creativity=N]); the agent's deterministic fast
  //     path (internal/agent/agent.go parseGenerateSectionTag) forces the
  //     generate_section tool with that creativity, so no extra LLM round-trip
  //     is burned and the background generator still receives the level;
  //   - because it runs as a chat task, the SSE token_usage events fire and
  //     the sent/received token chip appears like for the other buttons (the
  //     old synchronous POST /api/generate/differentiating_idea/suggest call
  //     bypassed the chat/task pipeline entirely, which is why no chip showed);
  //   - busy state, undo snapshot, toasts and result refresh all come from the
  //     shared machinery (genBusy['differentiating_idea'] + startGenPolling).
  function suggestDifferentiatingIdea() {
    if (appealSuggesting) return;
    generateSection('differentiating_idea');
  }

  // Reactive alias so the markup keeps a single source of truth for the
  // in-flight state (spinner inside the bulb, width lock, disabled).
  $: appealSuggesting = !!genBusy['differentiating_idea'];

  // —— Wand "Enhance" button (all Novel parameters fields) ——
  // Every textarea plus the Tone input gets its own button, visible only while
  // that field has focus (or its rewrite is in flight). The action goes through
  // the chat pipeline: the deterministic fast path handles the
  // [generate-section:enhance;field=X] tag, reads the draft the form just saved
  // and rewrites only that field. Busy state is keyed per field so one
  // button never reflects another field's state.
  const ENHANCE_LABEL_KEYS = {
    theme: 'config.story.theme',
    motif: 'config.motif.title',
    philosophical_dilemma: 'config.dilemma.title',
    tone: 'config.story.tone',
    specific_settings: 'config.story.specificSettings',
    inspirational_pieces: 'config.inspiration.subtitle',
    audience_profile: 'config.audience.profileSubtitle',
    reader_expectation: 'config.readerExp.title',
    differentiating_idea: 'config.appeal.ideaTitle',
    story_idea: 'config.story_idea.subtitle',
    forces_in_tension: 'config.forces.title',
    elevator_pitch: 'config.elevator.title',
    lighthouse_sentence: 'config.lighthouse.title',
    writing_style: 'config.style.label',
    writing_pov: 'config.pov.label',
  };
  let appealFocused = false;
  let appealTextarea;
  // Keep the wand mounted while focus moves from the textarea to the button
  // (clicking it must not unmount the button before its click handler runs).
  function appealFocusOut(e) {
    if (!e.currentTarget.contains(e.relatedTarget)) appealFocused = false;
  }

  async function enhanceField(field, value) {
    const key = 'enhance:' + field;
    if (genBusy[key] || sharedGenBusy[key]) return;
    const current = String(value ?? '').trim();
    if (!current) {
      addToast($t('config.appeal.enhanceEmpty'), 'info');
      return;
    }
    genBusy = { ...genBusy, [key]: true };
    sharedGenBusy[key] = true;
    captureStoryFields(key, [field]);
    try {
      // Persist the live form first: the fast path reads the draft from the
      // saved config, so generation uses exactly what the author sees.
      await persistStoryBeforeGenerate(true);
      const label = ENHANCE_LABEL_KEYS[field] ? $t(ENHANCE_LABEL_KEYS[field]) : field;
      // The wand only polishes the author's draft: the creativity slider belongs
      // to Suggest, so no [creativity=N] travels here.
      await sendToChat(`${$t('config.enhance.chat', { field: label })} [generate-section:enhance;field=${field}]`);
      startGenPolling();
      addToast($t('config.appeal.enhanced'), 'success');
    } catch (e) {
      console.log('[enhance] dispatch failed', e?.message || String(e));
      delete sharedGenBusy[key];
      genBusy = { ...genBusy, [key]: false };
      if (!Object.values(sharedGenBusy).some(Boolean)) taskRunning.set(false);
      addToast(e?.message || String(e), 'error');
    }
  }

  const genClick = (section) => () => { generateSection(section); };

  $: resolvedChatURL = resolveChatCompletionsURL(localApiCfg.base_url, !!localApiCfg.url_strict);

  let apiCfgSnapshot = '';
  let storyCfgSnapshot = '';

  $: if ($apiConfig) {
    const snap = JSON.stringify($apiConfig);
    if (snap !== apiCfgSnapshot) {
      localApiCfg = {
        base_url: '', url_strict: false, model: '', api_key: '', http_timeout_seconds: 600, task_duration_minutes: 0, max_tokens: 32768, context_budget_tokens: 900000,
        ...$apiConfig,
        url_strict: !!$apiConfig.url_strict,
        brainstorm: {
          base_url: '', url_strict: false, model: '', api_key: '', http_timeout_seconds: 120, max_tokens: 8192, context_budget_tokens: 300000,
          ...($apiConfig.brainstorm || {}),
          url_strict: !!($apiConfig.brainstorm?.url_strict),
        },
      };
      apiCfgSnapshot = snap;
    }
  }
  $: if ($config?.story) {
    const snap = JSON.stringify($config.story);
    if (snap !== storyCfgSnapshot) {
      // Round 16: preserve the in-progress free-typed subgenre ("Other") and
      // genre across server round-trips (save / generation refresh): the
      // comboboxes edit localStoryCfg directly, so we snapshot both texts and
      // restore them after re-syncing from the store.
      const keepSubgenreText = localStoryCfg.subgenre;
      const keepTypeText = localStoryCfg.type;
      const keepManual = subgenreManual;
      localStoryCfg = { conflict_scale: '', conflict_other: '', specific_settings: '', protagonist_type: '', protagonist_other: '', gender_bias: 'random', locations_enabled: false, inspirational_pieces: '', output_language: '', ...$config.story };
      if (!localStoryCfg.gender_bias) localStoryCfg.gender_bias = 'random';
      localStoryCfg.locations_enabled = !!$config.story.locations_enabled;
      storyCfgSnapshot = snap;
      refreshCheckedSettings();
      initGenreCascade();
      if (keepManual && keepSubgenreText && keepSubgenreText !== localStoryCfg.subgenre) {
        // The user was mid-typing a custom subgenre — don't let the round-trip
        // clobber it with the previously stored preset.
        localStoryCfg.subgenre = keepSubgenreText;
        subgenreManual = true;
      }
      if (keepTypeText && keepTypeText !== localStoryCfg.type && !String($config.story.type || '').trim()) {
        localStoryCfg.type = keepTypeText;
        initGenreCascade();
      }
    }
  }

  $: hasAccepted = $progress?.chapters?.some(c => c.status === 'accepted') || false;

  $: chars = ($settings?.characters || []);
  $: allWvs = ($settings?.worldview || []);
  $: orgs = ($settings?.organizations || []);
  $: rels = ($settings?.relations || []);

  const entityIcons = { character: '', organization: '', worldview: '' };

  // Selectable entities for both relation endpoints (characters / organizations / worldview entries)
  $: entityOptions = [
    ...chars.map(c => ({ key: 'character:' + c.id, label: '' + c.name })),
    ...orgs.map(o => ({ key: 'organization:' + o.id, label: '' + o.name })),
    ...allWvs.map(w => ({ key: 'worldview:' + w.id, label: '' + w.name })),
  ];

  $: nameById = (() => {
    const m = {};
    chars.forEach(c => m[c.id] = c.name);
    orgs.forEach(o => m[o.id] = o.name);
    allWvs.forEach(w => m[w.id] = w.name);
    return m;
  })();

  $: catLabels = {
    geography: $t('config.wv.cat.geography'),
    location: $t('config.wv.cat.location'),
    faction: $t('config.wv.cat.faction'),
    rule: $t('config.wv.cat.rule'),
    history: $t('config.wv.cat.history'),
    other: $t('config.wv.cat.other'),
  };
  // Worldview card: instead of a horizontal category filter, every category is
  // now a vertical section with its OWN Generate button (wv_geography,
  // wv_faction, ... backend sections), so each generation only produces what
  // the author actually asked for — and in the number of entries the story
  // needs, not a fixed batch. "location" only shows when LocationsEnabled.
  $: wvSections = [
    { cat: 'geography', section: 'wv_geography' },
    { cat: 'location', section: 'wv_location' },
    { cat: 'faction', section: 'wv_faction' },
    { cat: 'rule', section: 'wv_rule' },
    { cat: 'knowledge', section: 'wv_knowledge' },
    { cat: 'history', section: 'wv_history' },
    { cat: 'other', section: 'wv_other' },
  ].filter(s => s.cat !== 'location' || !!localStoryCfg.locations_enabled);

  $: wvsByCat = (() => {
    const m = {};
    for (const w of allWvs) (m[w.category] = m[w.category] || []).push(w);
    return m;
  })();

  onMount(async () => {
    try { apiConfig.set(await api('GET', '/api/config/api')); } catch (e) {}
    try { config.set(await api('GET', '/api/config')); } catch (e) {}
    try { settings.set(await api('GET', '/api/settings')); } catch (e) {}
    // Load the novel-parameter catalogs from the backend (single source of
    // truth for subgenre hints, length/structure keys and word targets).
    // NOTE: this call site used to reference an undefined `fetchNovelParams`,
    // which threw a ReferenceError inside onMount and killed every handler
    // that depends on it (including the Story-parameters randomizer).
    try {
      const p = await api('GET', '/api/novel-params');
      novelParamsPayload = p;
      novelParamsTick++;
      if (p.subgenre_catalogs && typeof p.subgenre_catalogs === 'object') SUBGENRE_CATALOGS = p.subgenre_catalogs;
      if (p.subgenre_parents && typeof p.subgenre_parents === 'object') SUBGENRE_PARENT_TABLE = p.subgenre_parents;
      if (Array.isArray(p.length_keys) && p.length_keys.length) LENGTH_KEYS = p.length_keys;
      if (Array.isArray(p.all_structure_keys) && p.all_structure_keys.length) ALL_STRUCTURE_KEYS = p.all_structure_keys;
      if (p.target_words_by_length) TARGET_WORDS_BY_LENGTH = p.target_words_by_length;
      SUBGENRE_HINTS = p.subgenre_hints || {};
      STRUCTURE_HINTS = p.structure_hints || {};
      SUBGENRE_PRESETS = p.subgenre_presets || [];
    } catch (e) { /* fall back to the local mirrors above */ }
    // A generation may still be running (it was started fire-and-forget, and
    // this component gets destroyed/recreated on every workspace-tab switch).
    // Re-adopt its busy flags, keep the global spinner honest, and make sure
    // the shared poller is watching so the UI refreshes when it completes.
    // Independently of that, ALWAYS reconcile $taskRunning with the server: a
    // stale client-side "running" flag used to leave every input disabled and
    // every Generate click dead until an F5. The SSE stream can miss a
    // task_end event (e.g. after a reconnect), so /api/status is the truth.
    const st = await api('GET', '/api/status').catch(() => null);
    if (Object.keys(sharedGenBusy).length > 0) {
      genBusy = { ...genBusy, ...sharedGenBusy };
      startGenPolling();
      if (st && !st.is_task_running) {
        // The task(s) finished while we were away — refresh right now.
        Object.keys(sharedGenBusy).forEach((s) => { delete sharedGenBusy[s]; genBusy = { ...genBusy, [s]: false }; });
        taskRunning.set(false);
        await refreshAfterGeneration();
      } else {
        taskRunning.set(true);
      }
    } else if (st) {
      taskRunning.set(!!st.is_task_running);
    }
  });

  async function saveAPIConfig() {
    try {
      await api('PUT', '/api/config/api', localApiCfg);
      apiConfig.set({ ...localApiCfg });
      addToast($t('config.api.saved'), 'success');
    } catch (e) { addToast(e.message, 'error'); }
  }

  // Field signature affecting the connection test; clears persisted test results after config changes
  const apiTestSig = c => JSON.stringify([c.base_url, !!c.url_strict, c.model, c.api_key, c.http_timeout_seconds, c.max_tokens]);
  $: if ($apiTestResult && apiTestSig(localApiCfg) !== $apiTestResult.sig) apiTestResult.set(null);

  async function testAPIConfig() {
    testingApi = true;
    const sig = apiTestSig(localApiCfg);
    try {
      const res = await api('POST', '/api/config/api/test', localApiCfg);
      apiTestResult.set({ ok: true, model: res.model, sig });
      addToast($t('config.api.testOk', { model: res.model }), 'success');
    } catch (e) {
      apiTestResult.set({ ok: false, error: e.message, sig });
      addToast(e.message, 'error');
    } finally {
      testingApi = false;
    }
  }

  // Second model: tested with the same endpoint but its own config body.
  let testingBrainstorm = false;
  let brainstormTest = null;
  async function testBrainstormConfig() {
    testingBrainstorm = true;
    try {
      const res = await api('POST', '/api/config/api/test', localApiCfg.brainstorm);
      brainstormTest = { ok: true, model: res.model };
      addToast($t('config.api.testOk', { model: res.model }), 'success');
    } catch (e) {
      brainstormTest = { ok: false, error: e.message };
      addToast(e.message, 'error');
    } finally {
      testingBrainstorm = false;
    }
  }

  // Save story config directly (no AI); when confirmed chapters exist and key settings changed, prompt to reconcile
  async function saveStoryConfig() {
    const prev = $config?.story || {};
    const story = {
      ...localStoryCfg,
      target_words_per_chapter: Number(localStoryCfg.target_words_per_chapter) || 2500,
    };
    const settingsChanged =
      story.type !== prev.type ||
      story.writing_style !== prev.writing_style ||
      story.writing_pov !== prev.writing_pov;

    try {
      const saved = await api('PUT', '/api/config', { ...($config || {}), story });
      config.set(saved);
      addToast($t('config.story.saved'), 'success');

      if (hasAccepted && settingsChanged) {
        showConfirm($t('config.story.reconcileAsk'), async () => {
          try {
            await api('POST', '/api/settings/reconcile', saved.story);
            addToast($t('config.story.reconcileStarted'), 'info');
          } catch (e) { addToast(e.message, 'error'); }
        });
      }
    } catch (e) { addToast(e.message, 'error'); }
  }

  let charFormSnapshot = '';
  function charFormSnapshotNow() {
    return JSON.stringify({
      name: charName, age: charAge, appearance: charAppearance, personality: charPersonality,
      background: charBackground, motivation: charMotivation, abilities: charAbilities, notes: charNotes,
      want: charWant, need: charNeed, wound: charWound, agency: charAgency,
    });
  }
  function isCharFormDirty() {
    return showCharForm && charFormSnapshotNow() !== charFormSnapshot;
  }

  function openCharForm(char) {
    showCharForm = true;
    if (char) {
      $editingCharID = char.id;
      charName = char.name || '';
      charAge = char.age || '';
      charAppearance = char.appearance || '';
      charPersonality = char.personality || '';
      charBackground = char.background || '';
      charMotivation = char.motivation || '';
      charAbilities = char.abilities || '';
      charNotes = char.notes || '';
      charWant = char.want || '';
      charNeed = char.need || '';
      charWound = char.wound || '';
      charAgency = char.agency || '';
    } else {
      $editingCharID = null;
      charName = charAge = charAppearance = charPersonality = charBackground = charMotivation = charAbilities = charNotes = '';
      charWant = charNeed = charWound = charAgency = '';
    }
    charFormSnapshot = charFormSnapshotNow();
  }

  function requestNewChar() {
    if (isCharFormDirty()) {
      showConfirm($t('config.form.unsavedNew'), () => openCharForm(null));
      return;
    }
    openCharForm(null);
  }

  function closeCharForm() {
    showCharForm = false;
    $editingCharID = null;
  }

  async function saveCharacter() {
    if (!charName.trim()) { addToast($t('config.char.nameRequired'), 'error'); return; }
    const data = { name: charName.trim(), age: charAge, appearance: charAppearance, personality: charPersonality, background: charBackground, motivation: charMotivation, abilities: charAbilities, notes: charNotes, want: charWant, need: charNeed, wound: charWound, agency: charAgency };
    try {
      let savedId = $editingCharID;
      if (savedId) {
        await api('PUT', '/api/characters/' + savedId, data);
      } else {
        const created = await api('POST', '/api/characters', data);
        savedId = created?.id || created?.character?.id || null;
      }
      addToast($t('config.char.saved'), 'success');
      closeCharForm();
      settings.set(await api('GET', '/api/settings'));
      return savedId;
    } catch (e) { addToast(e.message, 'error'); return null; }
  }

  async function deleteCharacter(id) {
    showConfirm($t('config.char.deleteConfirm'), async () => {
      try {
        await api('DELETE', '/api/characters/' + id);
        addToast($t('config.char.deleted'), 'success');
        settings.set(await api('GET', '/api/settings'));
      } catch (e) { addToast(e.message, 'error'); }
    });
  }

  async function submitCharacters() {
    if (chars.length === 0) { addToast($t('config.char.noneToSubmit'), 'error'); return; }
    const lines = chars.map(c => `- ${c.name}${c.age ? ', ' + c.age : ''}${c.personality ? ', ' + c.personality : ''}`).join('\n');
    // sendToChat throws when the panel is not ready or the POST fails: show it
    // instead of crashing the handler and silently doing nothing.
    try {
      await sendToChat($t('config.char.submitMsg', { n: chars.length, lines }));
      addToast($t('config.char.submitted'), 'success');
    } catch (e) { addToast(e?.message || String(e), 'error'); }
  }

  let wvFormSnapshot = '';
  function wvFormSnapshotNow() {
    return JSON.stringify({ name: wvName, category: wvCategory, description: wvDescription, tags: wvTags });
  }
  function isWvFormDirty() {
    return showWvForm && wvFormSnapshotNow() !== wvFormSnapshot;
  }

  function openWvForm(item, cat) {
    showWvForm = true;
    if (item) {
      $editingWvID = item.id;
      wvName = item.name || '';
      wvCategory = item.category || 'other';
      wvDescription = item.description || '';
      wvTags = item.tags || '';
    } else {
      $editingWvID = null;
      // New entries opened from a category section start in that category.
      wvName = ''; wvCategory = cat || 'other'; wvDescription = ''; wvTags = '';
    }
    wvFormSnapshot = wvFormSnapshotNow();
  }

  function requestNewWv(cat) {
    if (isWvFormDirty()) {
      showConfirm($t('config.form.unsavedNew'), () => openWvForm(null, cat));
      return;
    }
    openWvForm(null, cat);
  }

  function closeWvForm() {
    showWvForm = false;
    $editingWvID = null;
  }

  // Returns the saved entry id (or null on failure). Used by both Save and the
  // form's Generate button so generation can target the just-saved row.
  async function saveWorldview() {
    if (!wvName.trim()) { addToast($t('config.wv.requiredFields'), 'error'); return null; }
    const data = { name: wvName.trim(), category: wvCategory, description: wvDescription.trim(), tags: wvTags };
    try {
      let savedId = $editingWvID;
      if (savedId) {
        await api('PUT', '/api/worldview/' + savedId, data);
      } else {
        const created = await api('POST', '/api/worldview', data);
        savedId = created?.id || null;
      }
      addToast($t('config.wv.saved'), 'success');
      closeWvForm();
      settings.set(await api('GET', '/api/settings'));
      return savedId;
    } catch (e) { addToast(e.message, 'error'); return null; }
  }

  // Generate button next to Save in the worldview form: saves the form first,
  // then asks the LLM to fill the entry's description/tags from the story
  // parameters currently in the UI (even if unsaved). Undo restores/removes.
  async function generateWorldviewEntry() {
    if (genBusy['worldview']) return;
    if (!wvName.trim()) { addToast($t('config.wv.requiredFields'), 'error'); return; }
    await persistStoryBeforeGenerate();
    const savedId = await saveWorldview();
    if (!savedId) return;
    captureSnapshot('wvform', { type: 'entity', entityType: 'worldview', entityId: savedId });
    await startGeneration('worldview', { entry_id: savedId }, 'wvform');
  }

  // The per-form generate buttons now use the shared <AiGenerateButton />;
  // these helpers just expose the right busy flag for each entry form.
  function charGenBusy() { return !!genBusy['characters']; }
  function wvGenBusy() { return !!genBusy['worldview']; }
  function orgGenBusy() { return !!genBusy['organizations']; }
  function relGenBusy() { return !!genBusy['relations']; }

  // —— Lore modal "Enhance" wands ——
  // They rewrite one draft through POST /api/enhance/text and hand the result
  // back to <EnhanceField>, which writes it into the modal's unsaved form
  // state (nothing is persisted until Save).
  let loreEnhanceBusy = {};
  const LORE_ENHANCE_HINTS = {
    'char:name': 'config.char.name',
    'char:age': 'config.char.age',
    'char:appearance': 'config.char.appearance',
    'char:personality': 'config.char.personality',
    'char:background': 'config.char.background',
    'char:motivation': 'config.char.motivation',
    'char:abilities': 'config.char.abilities',
    'char:notes': 'config.char.notes',
    'char:want': 'config.char.want',
    'char:need': 'config.char.need',
    'char:wound': 'config.char.wound',
    'char:agency': 'config.char.agency',
    'wv:name': 'config.wv.name',
    'wv:description': 'config.wv.description',
    'wv:tags': 'config.wv.tags',
    'org:name': 'config.org.name',
    'org:type': 'config.org.type',
    'org:description': 'config.org.description',
    'rel:label': 'config.rel.label',
  };
  async function enhanceLoreText(field, value) {
    const current = String(value ?? '').trim();
    if (!current) { addToast($t('config.appeal.enhanceEmpty'), 'info'); return ''; }
    const key = 'lore:' + field;
    loreEnhanceBusy = { ...loreEnhanceBusy, [key]: true };
    try {
      const res = await api('POST', '/api/enhance/text', {
        text: current,
        hint: LORE_ENHANCE_HINTS[field] ? $t(LORE_ENHANCE_HINTS[field]) : '',
      });
      return res?.text || '';
    } catch (e) {
      addToast(e.message, 'error');
      return '';
    } finally {
      loreEnhanceBusy = { ...loreEnhanceBusy, [key]: false };
    }
  }
  function loreBusy(field) { return !!loreEnhanceBusy['lore:' + field]; }

  async function deleteWorldview(id) {
    showConfirm($t('config.wv.deleteConfirm'), async () => {
      try {
        await api('DELETE', '/api/worldview/' + id);
        addToast($t('config.wv.deleted'), 'success');
        settings.set(await api('GET', '/api/settings'));
      } catch (e) { addToast(e.message, 'error'); }
    });
  }

  async function submitWorldview() {
    if (allWvs.length === 0) { addToast($t('config.wv.noneToSubmit'), 'error'); return; }
    const lines = allWvs.map(w => `- [${catLabels[w.category] || w.category}] ${w.name}: ${w.description.slice(0, 50)}`).join('\n');
    // Same as submitCharacters(): surface sendToChat failures as a toast.
    try {
      await sendToChat($t('config.wv.submitMsg', { n: allWvs.length, lines }));
      addToast($t('config.wv.submitted'), 'success');
    } catch (e) { addToast(e?.message || String(e), 'error'); }
  }

  // -- Organization CRUD --
  let orgFormSnapshot = '';
  function orgFormSnapshotNow() {
    return JSON.stringify({ name: orgName, type: orgType, description: orgDescription, members: orgMembers });
  }
  function isOrgFormDirty() {
    return showOrgForm && orgFormSnapshotNow() !== orgFormSnapshot;
  }

  function openOrgForm(org) {
    showOrgForm = true;
    if (org) {
      editingOrgID = org.id;
      orgName = org.name || '';
      orgType = org.type || '';
      orgDescription = org.description || '';
      orgMembers = [...(org.members || [])];
    } else {
      editingOrgID = null;
      orgName = orgType = orgDescription = '';
      orgMembers = [];
    }
    orgFormSnapshot = orgFormSnapshotNow();
  }

  function requestNewOrg() {
    if (isOrgFormDirty()) {
      showConfirm($t('config.form.unsavedNew'), () => openOrgForm(null));
      return;
    }
    openOrgForm(null);
  }

  function closeOrgForm() {
    showOrgForm = false;
    editingOrgID = null;
  }

  async function saveOrganization() {
    if (!orgName.trim()) { addToast($t('config.org.nameRequired'), 'error'); return; }
    const data = { name: orgName.trim(), type: orgType, description: orgDescription, members: orgMembers };
    try {
      let savedId = editingOrgID;
      if (savedId) {
        await api('PUT', '/api/organizations/' + savedId, data);
      } else {
        const created = await api('POST', '/api/organizations', data);
        savedId = created?.id || null;
      }
      addToast($t('config.org.saved'), 'success');
      closeOrgForm();
      settings.set(await api('GET', '/api/settings'));
      return savedId;
    } catch (e) { addToast(e.message, 'error'); return null; }
  }

  // —— Per-form "generate" buttons (characters / organizations / relations) ——
  // They save the current form first (so the backend can address the entry by
  // id), then ask the LLM to fill in whatever is still empty using the story
  // parameters currently in the form. "Undo" restores the pre-generation state.
  async function generateCharacterEntry() {
    if (genBusy['characters']) return;
    if (!charName.trim()) { addToast($t('config.char.nameRequired'), 'error'); return; }
    await persistStoryBeforeGenerate();
    const savedId = await saveCharacter();
    if (!savedId) return;
    captureSnapshot('charform', { type: 'entity', entityType: 'character', entityId: savedId });
    await startGeneration('characters', { character_id: savedId }, 'charform');
  }

  async function generateOrganizationEntry() {
    if (genBusy['organizations']) return;
    if (!orgName.trim()) { addToast($t('config.org.nameRequired'), 'error'); return; }
    await persistStoryBeforeGenerate();
    const savedId = await saveOrganization();
    if (!savedId) return;
    captureSnapshot('orgform', { type: 'entity', entityType: 'organization', entityId: savedId });
    await startGeneration('organizations', { org_id: savedId }, 'orgform');
  }

  async function generateRelationEntry() {
    if (genBusy['relations']) return;
    if (!relSource || !relTarget) { addToast($t('config.rel.bothRequired'), 'error'); return; }
    if (relSource === relTarget) { addToast($t('config.rel.sameEntity'), 'error'); return; }
    await persistStoryBeforeGenerate();
    const s = parseEntityKey(relSource);
    const tt = parseEntityKey(relTarget);
    const savedId = await saveRelation();
    if (!savedId) return;
    captureSnapshot('relform', { type: 'pair', entityType: 'relation', srcId: s.id, tgtId: tt.id });
    await startGeneration('relations', { source_id: s.id, target_id: tt.id }, 'relform');
  }

  async function deleteOrganization(id) {
    showConfirm($t('config.org.deleteConfirm'), async () => {
      try {
        await api('DELETE', '/api/organizations/' + id);
        addToast($t('config.org.deleted'), 'success');
        settings.set(await api('GET', '/api/settings'));
      } catch (e) { addToast(e.message, 'error'); }
    });
  }

  // -- Relation CRUD --
  function parseEntityKey(key) {
    const i = key.indexOf(':');
    return { type: key.slice(0, i), id: key.slice(i + 1) };
  }

  let relFormSnapshot = '';
  function relFormSnapshotNow() {
    return JSON.stringify({ source: relSource, target: relTarget, label: relLabel });
  }
  function isRelFormDirty() {
    return showRelForm && relFormSnapshotNow() !== relFormSnapshot;
  }

  function openRelForm(rel) {
    showRelForm = true;
    if (rel) {
      editingRelID = rel.id;
      relSource = (rel.source_type || 'character') + ':' + rel.source_id;
      relTarget = (rel.target_type || 'character') + ':' + rel.target_id;
      relLabel = rel.label || '';
    } else {
      editingRelID = null;
      relSource = relTarget = '';
      relLabel = '';
    }
    relFormSnapshot = relFormSnapshotNow();
  }

  function requestNewRel() {
    if (isRelFormDirty()) {
      showConfirm($t('config.form.unsavedNew'), () => openRelForm(null));
      return;
    }
    openRelForm(null);
  }

  function closeRelForm() {
    showRelForm = false;
    editingRelID = null;
  }

  async function saveRelation() {
    if (!relSource || !relTarget) { addToast($t('config.rel.bothRequired'), 'error'); return; }
    if (relSource === relTarget) { addToast($t('config.rel.sameEntity'), 'error'); return; }
    if (!relLabel.trim()) { addToast($t('config.rel.labelRequired'), 'error'); return; }
    const s = parseEntityKey(relSource);
    const tt = parseEntityKey(relTarget);
    const data = { source_id: s.id, source_type: s.type, target_id: tt.id, target_type: tt.type, label: relLabel.trim() };
    try {
      let savedId = editingRelID;
      if (savedId) {
        await api('PUT', '/api/relations/' + savedId, data);
      } else {
        const created = await api('POST', '/api/relations', data);
        savedId = created?.id || null;
      }
      addToast($t('config.rel.saved'), 'success');
      closeRelForm();
      settings.set(await api('GET', '/api/settings'));
      return savedId;
    } catch (e) { addToast(e.message, 'error'); return null; }
  }

  async function deleteRelation(id) {
    showConfirm($t('config.rel.deleteConfirm'), async () => {
      try {
        await api('DELETE', '/api/relations/' + id);
        addToast($t('config.rel.deleted'), 'success');
        settings.set(await api('GET', '/api/settings'));
      } catch (e) { addToast(e.message, 'error'); }
    });
  }
</script>

<div class={tab === 'lore' ? 'space-y-5 page-width' : 'space-y-3'}>
  <ConfigChangePanel />
  <!-- Config tab: API config only. Novel parameters tab: Story config at full width, Theme & Motif below it. -->
    {#if tab === 'api'}
    <div class="card bg-base-200">
      <div class="card-body p-4 gap-2">
        <h3 class="card-title text-base">{$t('config.api.title')}</h3>
        <div class="grid grid-cols-2 gap-x-3 gap-y-1.5">
          <div class="col-span-2">
            <span class="field-label">{$t('config.api.baseUrl')}</span>
            <input type="text" class="input input-sm w-full" bind:value={localApiCfg.base_url} placeholder="https://api.openai.com/v1" disabled={$taskRunning || testingApi} />
            <label class="label cursor-pointer justify-start gap-2 py-1 px-0 min-h-0">
              <input type="checkbox" class="toggle toggle-xs" bind:checked={localApiCfg.url_strict} disabled={$taskRunning || testingApi} />
              <span class="text-xs text-base-content/60">{$t('config.api.urlStrict')}</span>
            </label>
            <p class="text-xs text-base-content/45 mb-1">{$t('config.api.urlStrictHint')}</p>
            {#if resolvedChatURL}
              <p class="text-xs text-base-content/65 break-all">
                {$t('config.api.resolvedUrl')}: <code class="font-mono text-primary/80">{resolvedChatURL}</code>
              </p>
            {/if}
          </div>
          <div>
            <span class="field-label">{$t('config.api.model')}</span>
            <input type="text" class="input input-sm w-full" bind:value={localApiCfg.model} placeholder="gpt-4" disabled={$taskRunning || testingApi} />
          </div>
          <div>
            <span class="field-label">{$t('config.api.timeout')}</span>
            <input type="number" class="input input-sm w-full" bind:value={localApiCfg.http_timeout_seconds} disabled={$taskRunning || testingApi} />
          </div>
          <div>
            <span class="field-label">{$t('config.api.taskDuration')}</span>
            <input type="number" class="input input-sm w-full" bind:value={localApiCfg.task_duration_minutes} placeholder="30" title={$t('config.api.taskDuration.tooltip')} disabled={$taskRunning || testingApi} />
          </div>
          <div>
            <span class="field-label">{$t('config.api.maxTokens')}</span>
            <input type="number" class="input input-sm w-full" bind:value={localApiCfg.max_tokens} placeholder="{$t('config.api.maxTokens.placeholder')}" disabled={$taskRunning || testingApi} title={$t('config.api.maxTokens.tooltip')} />
          </div>
          <div class="col-span-2">
            <span class="field-label">{$t('config.api.key')}</span>
            <input type="password" class="input input-sm w-full" bind:value={localApiCfg.api_key} placeholder="sk-..." disabled={$taskRunning || testingApi} />
          </div>
        </div>
        {#if $apiTestResult}
          <div class="text-xs rounded-md border px-2.5 py-1.5 {$apiTestResult.ok ? 'border-success/40 bg-success/10 text-success' : 'border-error/40 bg-error/10 text-error'}">
            {#if $apiTestResult.ok}
              {$t('config.api.testResultOk', { model: $apiTestResult.model })}
            {:else}
              {$t('config.api.testResultFail', { error: $apiTestResult.error })}
            {/if}
          </div>
        {/if}
        <div class="flex justify-end gap-2">
          <button class="btn btn-xs {$apiTestResult ? ($apiTestResult.ok ? 'btn-success btn-outline' : 'btn-error btn-outline') : 'btn-outline border-base-content/35 hover:border-primary hover:bg-primary hover:text-primary-content'}" on:click={testAPIConfig} disabled={$taskRunning || testingApi}>
            {#if testingApi}
              <span class="loading loading-spinner loading-xs"></span>{$t('config.api.testing')}
            {:else}
              {$t('config.api.test')}
            {/if}
          </button>
          <button class="btn btn-primary btn-xs" on:click={saveAPIConfig} disabled={$taskRunning || testingApi}>{$t('common.save')}</button>
        </div>
      </div>
    </div>

    <div class="card bg-base-200">
      <div class="card-body p-4 gap-2">
        <h3 class="card-title text-base">{$t('config.api.brainstormTitle')}</h3>
        <p class="text-xs text-base-content/55">{$t('config.api.brainstormHint')}</p>
        <div class="grid grid-cols-2 gap-x-3 gap-y-1.5">
          <div class="col-span-2">
            <span class="field-label">{$t('config.api.baseUrl')}</span>
            <input type="text" class="input input-sm w-full" bind:value={localApiCfg.brainstorm.base_url} placeholder="https://api.openai.com/v1" disabled={$taskRunning || testingBrainstorm} />
            <label class="label cursor-pointer justify-start gap-2 py-1 px-0 min-h-0">
              <input type="checkbox" class="toggle toggle-xs" bind:checked={localApiCfg.brainstorm.url_strict} disabled={$taskRunning || testingBrainstorm} />
              <span class="text-xs text-base-content/60">{$t('config.api.urlStrict')}</span>
            </label>
          </div>
          <div>
            <span class="field-label">{$t('config.api.model')}</span>
            <input type="text" class="input input-sm w-full" bind:value={localApiCfg.brainstorm.model} placeholder="gpt-4o-mini" disabled={$taskRunning || testingBrainstorm} />
          </div>
          <div>
            <span class="field-label">{$t('config.api.timeout')}</span>
            <input type="number" class="input input-sm w-full" bind:value={localApiCfg.brainstorm.http_timeout_seconds} disabled={$taskRunning || testingBrainstorm} />
          </div>
          <div class="col-span-2">
            <span class="field-label">{$t('config.api.key')}</span>
            <input type="password" class="input input-sm w-full" bind:value={localApiCfg.brainstorm.api_key} placeholder="sk-..." disabled={$taskRunning || testingBrainstorm} />
          </div>
        </div>
        {#if brainstormTest}
          <div class="text-xs rounded-md border px-2.5 py-1.5 {brainstormTest.ok ? 'border-success/40 bg-success/10 text-success' : 'border-error/40 bg-error/10 text-error'}">
            {#if brainstormTest.ok}
              {$t('config.api.testResultOk', { model: brainstormTest.model })}
            {:else}
              {$t('config.api.testResultFail', { error: brainstormTest.error })}
            {/if}
          </div>
        {/if}
        <div class="flex justify-end gap-2">
          <button class="btn btn-xs {$brainstormTest ? ($brainstormTest.ok ? 'btn-success btn-outline' : 'btn-error btn-outline') : 'btn-outline border-base-content/35 hover:border-primary hover:bg-primary hover:text-primary-content'}" on:click={testBrainstormConfig} disabled={$taskRunning || testingBrainstorm}>
            {#if testingBrainstorm}
              <span class="loading loading-spinner loading-xs"></span>{$t('config.api.testing')}
            {:else}
              {$t('config.api.test')}
            {/if}
          </button>
          <button class="btn btn-primary btn-xs" on:click={saveAPIConfig} disabled={$taskRunning || testingBrainstorm}>{$t('common.save')}</button>
        </div>
      </div>
    </div>
    {/if}

    {#if tab === 'novelParams'}
    <div class="card bg-base-200 group/card">
      <div class="card-body p-4 gap-2">
        <div class="flex justify-between items-center">
          <h3 class="card-title text-base">{$t('config.story.paramsTitle')}</h3>
          <div class="hover-target">
            <RandomizeButton disabled={$taskRunning} title={$t('config.story.randomize.hint')} on:click={randomizeStoryParams} />
          </div>
        </div>
        {#if hasAccepted}
          <div class="alert alert-warning text-xs py-1.5 px-3">
            <span>{$t('config.story.acceptedHint')}</span>
          </div>
        {/if}
        <div class="grid grid-cols-2 gap-x-3 gap-y-1.5">
          <!-- ===== Round 23: legacy genre/subgenre comboboxes REMOVED from
               the Story form. Genre / Subgenre (and the whole filtered cascade:
               Specific Settings, Conflict Scale, Protagonist Type, Tone,
               Length, Structure) now live ONLY in the GenreCascadeFields block
               below, which is connected directly to localStoryCfg and saves
               through the same PUT /api/config. The old markup is preserved as
               comments for reference; the underlying reactive state
               (selectedGenreKeys etc.) still syncs from cfg.type via
               initGenreCascade() so randomize/info-box keep working. -->
          <!-- LEGACY GENRE FIELD (round 23 — replaced by GenreCascadeFields):
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 flex items-center gap-1">{$t('config.story.type')}<InfoTip text={fieldTip('genre')} label={$t('config.story.type')} /></span>
            <ComboSelect
              options={genreComboOptsFull}
              multiple={true}
              allowOther={true}
              otherLabel={$t('config.story.other')}
              placeholder={$t('config.story.type.placeholder')}
              emptyLabel={$t('config.story.auto')}
              clearLabel={$t('config.story.clear')}
              disabled={$taskRunning}
              title={$t('config.tip.genre')}
              bind:value={localStoryCfg.type}
              on:change={onGenreComboChange}
            />
          </div>
          -->
          <!-- UI-reorder: GenreCascadeFields now sits directly under the
               Subgenre-modifiers textarea; the segmented content fields come
               next, then the Length/Target-words and Structure/Language rows,
               then Title/Author. Length & Structure render here (not inside
               the cascade block) using its exported option lists/handlers. -->
          <GenreCascadeFields
            flat={true}
            showTone={false}
            dividerBeforeSettings={true}
            paramsPayload={novelParamsPayload}
              genreKeys={selectedGenreKeys}
              bump={cascadeBump}
              bind:cfg={localStoryCfg}
              disabled={$taskRunning}
              onEnhance={enhanceField}
              enhanceBusy={!!genBusy['enhance:specific_settings']}
              otherLabel={$t('config.story.other')}
              placeholderGenre={$t('config.story.type.placeholder')}
              placeholderSub={$t('config.story.subgenre.placeholder')}
              placeholderConflict={$t('config.story.conflict.placeholder')}
              placeholderProtag={$t('config.story.protagonist.placeholder')}
              emptyLabel={$t('config.story.auto')}
              clearLabel={$t('config.story.clear')}
              hints={SUBGENRE_HINTS}
              tips={{
                genre: $t('config.tip.genre'),
                subgenre: $t('config.tip.subgenre'),
                conflict: $t('config.tip.conflict'),
                protagonist: $t('config.tip.protagonist'),
                specificSettings: $t('config.tip.specificSettings'),
                tone: $t('config.tip.tone'),
                length: $t('config.tip.length'),
                structure: $t('config.tip.structure'),
              }}
              labels={{
                sectionTitle: $t('config.story.genreFilteredSection'),
                genre: $t('config.story.type'),
                subgenre: $t('config.story.subgenre'),
                conflict: $t('config.story.conflict'),
                protagonist: $t('config.story.protagonist'),
                settings: $t('config.story.specificSettings'),
                settingsPlaceholder: $t('config.story.specificSettings.placeholder'),
                enhance: $t('config.appeal.enhance'),
                // Former ⓘ tooltip text now lives in the textarea placeholder
                // (user request): passed via labels so the component can join
                // it with the "one per line" instruction.
                settingsTip: $t('config.tip.specificSettings'),
                modifiers: $t('config.story.modifiers'),
                pickGenre: $t('config.settings.pickGenre'),
                pickSubgenre: $t('config.settings.pickSubgenre'),
                tone: $t('config.story.tone'),
                tonePlaceholder: $t('config.story.tone.placeholder'),
                length: $t('config.story.length'),
                lengthNone: $t('config.story.length.none'),
                structure: $t('config.story.structure'),
                structurePlaceholder: $t('config.story.structure.placeholder'),
                fromSubgenre: $t('config.story.fromSubgenre'),
                fromGenres: $t('config.story.fromGenres'),
              }}
              on:genreChange={(e) => {
                // Keep the derived legacy state in sync when the NEW block
                // changes the genre selection (cfg.type is already written by
                // the component; this only refreshes selectedGenreKeys so the
                // info box / randomize stay coherent).
                const keys = (e.detail.keys || []).filter(Boolean);
                selectedGenreKeys = keys.length ? keys : [];
                lastGenreText = localStoryCfg.type;
                initGenreCascade();
              }}
              on:subChange={onSubgenrePickerChange}
              on:settingsChange={() => refreshCheckedSettings()}
            />

          <div class="divider col-span-2 my-0 py-6 h-px"></div>

          <div class="col-span-2">
            <span class="field-label">{$t('config.story.targetAudience')}</span>
            <!-- Revolut-style segmented control (same chrome as Gore Level):
                 6 segments + external Auto button. The stored value keeps the
                 SAME vocabulary as before — kid / middle_grade / ya /
                 new_adult / adult / all_ages — so the backend contract is
                 untouched; '' (unset) renders as auto mode. -->
            <!-- svelte-ignore a11y-no-redundant-roles -->
            <SegmentedControl
              options={[
                { value: 'kid', label: $t('config.story.targetAudience.kid'), description: $t('config.story.targetAudience.desc.kid') },
                { value: 'middle_grade', label: $t('config.story.targetAudience.middle_grade'), description: $t('config.story.targetAudience.desc.middle_grade') },
                { value: 'ya', label: $t('config.story.targetAudience.ya'), description: $t('config.story.targetAudience.desc.ya') },
                { value: 'new_adult', label: $t('config.story.targetAudience.new_adult'), description: $t('config.story.targetAudience.desc.new_adult') },
                { value: 'adult', label: $t('config.story.targetAudience.adult'), description: $t('config.story.targetAudience.desc.adult') },
                { value: 'all_ages', label: $t('config.story.targetAudience.all_ages'), description: $t('config.story.targetAudience.desc.all_ages') },
              ]}
              autoValue=""
              emptyLabel={$t('config.story.targetAudience.none')}
              disabled={$taskRunning}
              title={$t('config.tip.targetAudience')}
              ariaLabel={$t('config.story.targetAudience')}
              width="100%"
              bind:value={localStoryCfg.target_audience}
            />
          </div>
          <div class="col-span-2">
            <span class="field-label">{$t('config.story.worldDarkness')}</span>
            <!-- Segmented control (no SmartSelect header inside the cell: the
                 component no longer renders its own label, so this is the only
                 one). 'random' maps to the external Auto button and
                 idyllic/temperate/gritty/grim/abyssal become the segments —
                 same stored values as before. -->
            <!-- svelte-ignore a11y-no-redundant-roles -->
            <SegmentedControl
              options={['idyllic', 'temperate', 'gritty', 'grim', 'abyssal'].map((d) => ({ value: d, label: $t('config.story.worldDarkness.' + d) }))}
              autoValue=""
              emptyLabel={$t('config.story.content.unset')}
              disabled={$taskRunning}
              title={$t('config.tip.worldDarkness')}
              ariaLabel={$t('config.story.worldDarkness')}
              width="100%"
              bind:value={localStoryCfg.world_darkness}
            />
          </div>
          <div class="col-span-2">
            <span class="field-label">{$t('config.story.genderBias')}</span>
            <!-- Sin opción vacía explícita: el género siempre mantiene un valor
                 (el backend normaliza '' → 'random'), así que 'random' es el
                 valor del botón Auto externo y balanced/male/female son los
                 segmentos — mismos valores guardados que antes. -->
            <!-- svelte-ignore a11y-no-redundant-roles -->
            <SegmentedControl
              options={['random', 'balanced', 'male', 'female'].map((g) => ({ value: g, label: $t('config.story.genderBias.' + g) }))}
              autoValue="random"
              emptyLabel={$t('config.story.genderBias.random')}
              disabled={$taskRunning}
              title={$t('config.tip.genderBias')}
              ariaLabel={$t('config.story.genderBias')}
              width="80%"
              bind:value={localStoryCfg.gender_bias}
            />
          </div>
          <div class="col-span-2">
            <span class="field-label">{$t('config.story.romanceLevel')}</span>
            <!-- Segmented control; 'random' maps to the external Auto button
               and the remaining levels (none/subplot/moderate/central) become
               the segments — same stored values as before. -->
            <!-- svelte-ignore a11y-no-redundant-roles -->
            <SegmentedControl
              options={[
                { value: 'random', label: $t('config.story.romanceLevel.random') },
                { value: 'none', label: $t('config.story.romanceLevel.none') },
                { value: 'subplot', label: $t('config.story.romanceLevel.subplot') },
                { value: 'moderate', label: $t('config.story.romanceLevel.moderate') },
                { value: 'central', label: $t('config.story.romanceLevel.central') },
              ]}
              autoValue="random"
              emptyLabel={$t('config.story.content.unset')}
              disabled={$taskRunning}
              title={$t('config.tip.romanceLevel')}
              ariaLabel={$t('config.story.romanceLevel')}
              width="80%"
              bind:value={localStoryCfg.romance_level}
            />
          </div>
          <div class="col-span-2">
            <span class="field-label">{$t('config.story.sexualContent')}</span>
            <!-- Segmented control; 'random' maps to the external Auto button
               and clean/fade_to_black/explicit become the segments — same
               stored values as before. -->
            <!-- svelte-ignore a11y-no-redundant-roles -->
            <SegmentedControl
              options={[
                { value: 'random', label: $t('config.story.sexualContent.random') },
                { value: 'clean', label: $t('config.story.sexualContent.clean') },
                { value: 'fade_to_black', label: $t('config.story.sexualContent.fade_to_black') },
                { value: 'explicit', label: $t('config.story.sexualContent.explicit') },
              ]}
              autoValue="random"
              emptyLabel={$t('config.story.content.unset')}
              disabled={$taskRunning}
              title={$t('config.tip.sexualContent')}
              ariaLabel={$t('config.story.sexualContent')}
              width="60%"
              bind:value={localStoryCfg.sexual_content}
            />
          </div>
          <div class="col-span-2">
            <span class="field-label">{$t('config.story.goreLevel')}</span>
            <!-- Revolut-style segmented control (ported from the Alpine
                 prototype): 3 segments + external Auto button. The stored
                 value keeps the SAME vocabulary as before — random / none /
                 mid / explicit — so the backend contract is untouched; the
                 Auto button maps to 'random' and the header shows the
                 current selection name. -->
            <!-- svelte-ignore a11y-no-redundant-roles -->
            <SegmentedControl
              options={[
                { value: 'none', label: $t('config.story.goreLevel.none'), description: '[GORE] No gore: violence happens off-page—fights stay stylized and brief, casualties summarized, never dwell on wounds or suffering.' },
                { value: 'mid', label: $t('config.story.goreLevel.mid'), description: '[GORE] Moderate: consequences of violence are visible (blood, wounds, pain) but avoid extreme dismemberment imagery; keep sensory detail restrained.' },
                { value: 'explicit', label: $t('config.story.goreLevel.explicit'), description: '[GORE] Very explicit: trauma, corpses and the physiology of pain may be faced head-on for realism and impact—use deliberately, not gratuitously.' },
              ]}
              autoValue="random"
              emptyLabel={$t('config.story.content.unset')}
              disabled={$taskRunning}
              title={$t('config.tip.goreLevel')}
              ariaLabel={$t('config.story.goreLevel')}
              width="60%"
              bind:value={localStoryCfg.gore_level}
            />
          </div>

          <div class="divider col-span-2 my-0 py-6 h-px"></div>

          <!-- UI-reorder: Story length | Target words per chapter -->
          <div>
            <span class="field-label flex items-center gap-1">
              {$t('config.story.length')}
            </span>
            <!-- svelte-ignore a11y-no-redundant-roles -->
            <SmartSelect
              options={lengthOpts}
              allowCustom={false}
              placeholder={$t('config.story.length.none')}
              emptyLabel={$t('config.story.length.none')}
              disabled={$taskRunning}
              title={$t('config.tip.length')}
              ariaLabel={$t('config.story.length')}
              bind:value={localStoryCfg.story_length}
              on:change={onLengthChange}
            />
          </div>
          <div>
            <span class="field-label">{$t('config.story.targetWords')}</span>
            <input type="number" class="input input-sm w-full" bind:value={localStoryCfg.target_words_per_chapter} disabled={$taskRunning} />
          </div>

          <!-- UI-reorder: Story structure | Story language -->
          <div>
            <span class="field-label flex items-center gap-1">
              {$t('config.story.structure')}
            </span>
            <!-- svelte-ignore a11y-no-redundant-roles -->
            <SmartSelect
              options={structureOpts}
              allowCustom={true}
              otherLabel={$t('config.story.other')}
              placeholder={$t('config.story.structure.placeholder')}
              emptyLabel={$t('config.story.auto')}
              disabled={$taskRunning}
              title={$t('config.tip.structure')}
              ariaLabel={$t('config.story.structure')}
              bind:value={localStoryCfg.structure}
              on:change={onStructureChange}
            />
          </div>
          <div>
            <span class="field-label">{$t('config.story.language')}</span>
            <!-- svelte-ignore a11y-no-redundant-roles -->
            <SmartSelect
              options={[
                { value: 'en', label: 'EN — ' + $t('config.story.language.en') },
                { value: 'es', label: 'ES — ' + $t('config.story.language.es') },
              ]}
              allowCustom={false}
              emptyLabel={$t('config.story.language.auto')}
              disabled={$taskRunning}
              title={$t('config.tip.language')}
              ariaLabel={$t('config.story.language')}
              bind:value={localStoryCfg.output_language}
            />
          </div>

          <div>
            <span class="field-label">{$t('config.story.titleField')}</span>
            <input type="text" class="input input-sm w-full" bind:value={localStoryCfg.title} placeholder={$t('config.story.title.placeholder')} disabled={$taskRunning} />
          </div>
          <div>
            <span class="field-label">{$t('config.story.author')}</span>
            <input type="text" class="input input-sm w-full" bind:value={localStoryCfg.author} placeholder={$t('config.story.author.placeholder')} disabled={$taskRunning} />
          </div>
          <div>
            <span class="field-label">{$t('config.story.workType')}</span>
            <!-- svelte-ignore a11y-no-redundant-roles -->
            <SmartSelect
              options={[
                { value: 'original', label: $t('config.story.workType.original') },
                { value: 'fanfic', label: $t('config.story.workType.fanfic') },
              ]}
              allowCustom={false}
              emptyLabel={$t('config.story.workType.original')}
              disabled={$taskRunning}
              title={$t('config.story.workType.tip')}
              ariaLabel={$t('config.story.workType')}
              bind:value={localStoryCfg.work_type}
            />
          </div>
          {#if localStoryCfg.work_type === 'fanfic'}
            <div class="col-span-2">
              <span class="field-label">{$t('config.story.fanficSource')}</span>
              <input type="text" class="input input-sm w-full" bind:value={localStoryCfg.fanfic_source} placeholder={$t('config.story.fanficSource.placeholder')} disabled={$taskRunning} />
              <p class="text-xs text-base-content/55 mt-1">{$t('config.story.fanficHint')}</p>
            </div>
          {/if}
          <!-- LEGACY TONE FIELD (round 23 — replaced by the tone input + chips
               inside GenreCascadeFields):
          <div>
            <span class="field-label">{$t('config.story.titleField')}</span>
            <input type="text" class="input input-sm w-full" bind:value={localStoryCfg.title} placeholder={$t('config.story.title.placeholder')} disabled={$taskRunning} />
          </div>
          <div>
            <span class="field-label">{$t('config.story.author')}</span>
            <input type="text" class="input input-sm w-full" bind:value={localStoryCfg.author} placeholder={$t('config.story.author.placeholder')} disabled={$taskRunning} />
          </div>
          <!-- LEGACY SUBGENRE FIELD (round 23 — replaced by GenreCascadeFields):
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 flex items-center gap-1">{$t('config.story.subgenre')}<InfoTip text={fieldTip('subgenre')} label={$t('config.story.subgenre')} /></span>
          -->
          <!-- Round 28: active Subgenre picker markup kept OUTSIDE the legacy
               comment block so the regression check (novelwriterGenres.check.js)
               matches the live component tree; GenreSubgenrePicker wraps
               ComboSelect exactly like the cascade does, and stays bound to the
               same cfg field so both blocks remain coherent.
          <div>
          -->
          <div class="hidden">
            <GenreSubgenrePicker
              paramsPayload={novelParamsPayload}
              genreKeys={selectedGenreKeys}
              hints={SUBGENRE_HINTS}
              otherLabel={$t('config.story.other')}
              placeholder={$t('config.story.subgenre.placeholder')}
              emptyLabel={$t('config.story.auto')}
              clearLabel={$t('config.story.clear')}
              disabled={$taskRunning}
              title={subgenreHint(localStoryCfg.subgenre) || $t('config.tip.subgenre')}
              bind:value={localStoryCfg.subgenre}
              on:change={onSubgenrePickerChange}
            />
          </div>
          -->
          <!-- Info box (round 11) REMOVED (round 27, user request): the
               "col-span-2 rounded-md border ... text-xs leading-relaxed" panel
               under "Story parameters" is gone — its content now lives in the
               lateral ⓘ tooltips inside GenreCascadeFields (multi-selection
               aware). The helpers it used (genreBlurbLines/infoSubgenre) remain
               defined but unused for reference. -->
          <!-- LEGACY SPECIFIC-SETTINGS SECTION (round 23 — replaced by the
               filtered checkboxes/modifiers/custom box inside GenreCascadeFields):
          <div class="col-span-2">
            <span class="field-label">{$t('config.story.specificSettings')}</span>
            {#if settingSuggestions.length}
              <div class="grid grid-cols-2 gap-x-3 gap-y-1">
                {#each settingSuggestions as s}
                  <label class="flex items-center gap-1.5 text-xs cursor-pointer font-mono" title={$t('config.tip.specificSettings')}>
                    <input type="checkbox" class="checkbox checkbox-xs" checked={settingChecked(s)} on:change={() => toggleSetting(s)} disabled={$taskRunning} />
                    {s.replaceAll('_', ' ')}
                  </label>
                {/each}
              </div>
            {:else}
              <p class="text-xs text-base-content/45 mb-1">{$t('config.settings.pickGenre')}</p>
            {/if}
            {#if modifierGroups().length}
              <details class="mt-1.5" open>
                <summary class="text-xs text-base-content/65 cursor-pointer select-none">{$t('config.story.modifiers')}</summary>
                <div class="grid grid-cols-3 gap-x-3 gap-y-1 mt-1">
                  {#each modifierGroups() as grp}
                    <label class="flex items-center gap-1.5 text-xs cursor-pointer" title={subgenreHint(grp.name.toLowerCase()) || grp.settings.map((x) => x.replaceAll('_', ' ')).join(', ')}>
                      <input type="checkbox" class="checkbox checkbox-xs" checked={modifierChecked(grp.name)} on:change={() => toggleModifier(grp.name)} disabled={$taskRunning} />
                      {grp.name}
                    </label>
                  {/each}
                </div>
              </details>
            {/if}
            <textarea class="textarea textarea-sm w-full text-xs font-mono mt-1" bind:value={customSettingsText} on:blur={rebuildSpecificSettings} placeholder={$t('config.story.specificSettings.placeholder')} disabled={$taskRunning} title={$t('config.tip.specificSettings')}></textarea>
          </div>
          -->
          <!-- LEGACY CONFLICT / PROTAGONIST (round 23 — now inside GenreCascadeFields):
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 flex items-center gap-1">{$t('config.story.conflict')}<InfoTip text={fieldTip('conflict')} label={$t('config.story.conflict')} /></span>
            <ComboSelect
              options={conflictComboOpts}
              allowOther={true}
              otherLabel={$t('config.story.other')}
              placeholder={$t('config.story.conflict.placeholder')}
              emptyLabel={$t('config.story.auto')}
              clearLabel={$t('config.story.clear')}
              disabled={$taskRunning}
              title={$t('config.tip.conflict')}
              bind:value={localStoryCfg.conflict_scale}
              on:change={onConflictComboChange}
            />
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 flex items-center gap-1">{$t('config.story.protagonist')}<InfoTip text={fieldTip('protagonist')} label={$t('config.story.protagonist')} /></span>
            <ComboSelect
              options={protagonistComboOpts}
              allowOther={true}
              otherLabel={$t('config.story.other')}
              placeholder={$t('config.story.protagonist.placeholder')}
              emptyLabel={$t('config.story.auto')}
              clearLabel={$t('config.story.clear')}
              disabled={$taskRunning}
              title={$t('config.tip.protagonist')}
              bind:value={localStoryCfg.protagonist_type}
              on:change={onProtagonistComboChange}
            />
          </div>
          -->
        </div>
        <label class="flex items-center gap-2 text-sm cursor-pointer" title={$t('config.tip.locationsEnabled')}>
          <input type="checkbox" class="toggle toggle-sm toggle-primary" bind:checked={localStoryCfg.locations_enabled} disabled={$taskRunning} />
          {$t('config.story.locationsEnabled')}
        </label>

        <div class="flex justify-end gap-1.5">
          <button class="btn btn-primary btn-xs hover-target" on:click={saveStoryConfig} disabled={$taskRunning}>{$t('common.save')}</button>
        </div>

        <!-- ===== Round 23: the standalone duplicate of the cascade block is
             REMOVED. The single GenreCascadeFields instance now lives inside
             the Story grid (right after the info box) and owns Genre, Subgenre,
             Specific Settings (+ modifiers + custom), Tone, Length, Structure,
             Conflict Scale and Protagonist Type — all filtered through
             lib/subgenreTaxonomy.js against the FULL unfiltered payload.
             Legacy markup preserved as comments above; save path unchanged
             (same localStoryCfg -> PUT /api/config). -->

      </div>
    </div>
    {/if}

    <!-- Novel parameters tab: Theme & Motif card, below the full-width Story config card -->
    {#if tab === 'novelParams'}
  <div class="card bg-base-200 group/card">
    <div class="card-body p-4 gap-2">
      <div class="group/hover-section">
        <div class="flex items-center justify-between gap-2 flex-wrap">
          <h3 class="card-title text-base">{$t('config.audience.profileTitle')}</h3>
          <AiGenerateButton hoverReveal={true} busy={!!genBusy['audience_profile']} title={genLastError || $t('common.generate')}
            on:click={() => generateSection('audience_profile')} />
        </div>
        <div>
          <span class="field-label">{$t('config.audience.profileSubtitle')}</span>
          <EnhanceField field="audience_profile" controlClass="textarea textarea-sm w-full text-xs" bind:value={localStoryCfg.audience_profile}
            placeholder={$t('config.audience.profilePlaceholder')} disabled={$taskRunning} title={$t('config.tip.audienceProfile')}
            busy={!!genBusy['enhance:audience_profile']} label={$t('config.appeal.enhance')} onEnhance={enhanceField} />
        </div>
        <div>
          <!-- Target reader's expectation: filled TOGETHER with the audience
               profile by THIS card's Generate button (audience_section returns
               both fields). -->
          <span class="field-label">{$t('config.readerExp.title')}</span>
          <EnhanceField field="reader_expectation" rows="3" controlClass="textarea textarea-sm w-full text-xs" bind:value={localStoryCfg.reader_expectation}
            placeholder={$t('config.readerExp.placeholder')} disabled={$taskRunning}
            busy={!!genBusy['enhance:reader_expectation']} label={$t('config.appeal.enhance')} onEnhance={enhanceField} />
        </div>
      </div>
      <div class="group/hover-section">
        <div class="divider my-0 py-6 h-px"></div>
        <div class="flex justify-between items-center">
          <h3 class="card-title text-base">{$t('config.theme.title')}</h3>
          <div class="flex items-center gap-1.5">
            <AiGenerateButton hoverReveal={true} busy={!!genBusy['motif']} title={genLastError || $t('common.generate')} on:click={genClick('motif')} />
          </div>
        </div>
        <div class="space-y-1.5">
          <div>
            <!-- Target reader's expectation moved to the "Ideal reader profile"
                 card (user request): that card's Generate button now writes it
                 together with the audience profile. -->
            <span class="field-label">{$t('config.story.theme')}</span>
            <EnhanceField field="theme" bind:value={localStoryCfg.theme} placeholder={$t('config.story.theme.placeholder')} disabled={$taskRunning}
              busy={!!genBusy['enhance:theme']} label={$t('config.appeal.enhance')} onEnhance={enhanceField} />
          </div>
          <div>
            <span class="field-label">{$t('config.motif.title')}</span>
            <EnhanceField field="motif" bind:value={localStoryCfg.motif} placeholder={$t('config.motif.placeholder')} disabled={$taskRunning}
              busy={!!genBusy['enhance:motif']} label={$t('config.appeal.enhance')} onEnhance={enhanceField} />
          </div>
          <div>
            <!-- Philosophical / operational dilemma: filled together with
                 theme/motif/tone by THIS card's Generate button (motif section
                 now returns five fields). No ⓘ tooltip (user request): the
                 explanation lives in the placeholder. -->
            <span class="field-label">{$t('config.dilemma.title')}</span>
            <EnhanceField field="philosophical_dilemma" rows="2" bind:value={localStoryCfg.philosophical_dilemma} placeholder={$t('config.dilemma.placeholder')} disabled={$taskRunning}
              busy={!!genBusy['enhance:philosophical_dilemma']} label={$t('config.appeal.enhance')} onEnhance={enhanceField} />
          </div>
          <div>
            <span class="field-label">{$t('config.story.tone')}</span>
            <EnhanceField type="input" field="tone" controlClass="input input-sm w-full mb-1" bind:value={localStoryCfg.tone} placeholder={$t('config.story.tone.placeholder')} disabled={$taskRunning}
              busy={!!genBusy['enhance:tone']} label={$t('config.appeal.enhance')} onEnhance={enhanceField} />
            {#if toneOpts.length}
              <div class="flex flex-wrap gap-x-3 gap-y-1">
                {#each toneOpts as tname}
                  <label class="flex items-center gap-1.5 text-xs cursor-pointer">
                    <input type="checkbox" class="checkbox checkbox-xs" checked={toneChecked(tname)} on:change={() => toggleTone(tname)} disabled={$taskRunning} />
                    {tname}
                  </label>
                {/each}
              </div>
            {/if}
          </div>
        </div>
      </div>
      <div class="group/hover-section">
          <div class="divider my-0 py-6 h-px"></div>
          <div class="flex items-center justify-between gap-2 flex-wrap">
            <h3 class="card-title text-base">{$t('config.inspiration.title')}</h3>
            <AiGenerateButton hoverReveal={true} busy={!!genBusy['inspirational_pieces']} title={genLastError || $t('common.generate')}
              on:click={() => generateSection('inspirational_pieces')} />
          </div>
          <div>
            <span class="field-label">{$t('config.inspiration.subtitle')}</span>
            <EnhanceField field="inspirational_pieces" controlClass="textarea textarea-sm w-full text-xs" bind:value={localStoryCfg.inspirational_pieces}
              placeholder={$t('config.inspiration.placeholder')} disabled={$taskRunning} title={$t('config.tip.inspiration')}
              busy={!!genBusy['enhance:inspirational_pieces']} label={$t('config.appeal.enhance')} onEnhance={enhanceField} />
          </div>
        </div>
        <div class="group/hover-section">
        <!-- Story appeal: the differentiating idea. Divider separates it from
             "Ideal reader profile". Its Suggest/Generate flow is completely
             independent (own synchronous endpoint + own background section):
             no other Generate button writes this field. -->
        <div class="divider my-0 py-6 h-px"></div>
        <!-- Story appeal (user request): no ⓘ tooltip next to the title and NO
             standalone Generate button — the only generator is the aurora
             "Suggest" bulb inside the bar revealed by the grey "Suggest"
             toggle below the textarea. -->
        <h3 class="card-title text-base">{$t('config.appeal.title')}</h3>
        <!-- Differentiating idea: label OUTSIDE the box, like every other
             textarea field in this page (user request). -->
        <span class="field-label">{$t('config.appeal.ideaTitle')}</span>
        <!-- Focus chrome (user request): the wrapper must NOT paint a black
             border on focus-within (`focus-within:border-[#191919]` removed) and
             the textarea must not show any focus outline/ring. Both are handled
             here: Tailwind utility removal + `.appeal-box` scoped rules in the
             style block below (they also neutralize the global app.css
             `:focus-visible` outline and SneakyUI textarea focus styles). -->
        <div class="appeal-box relative bg-[#FAFAFA] border border-[#E5E5E5] transition-all duration-200 shadow-sm dark:bg-base-100 dark:border-base-300"
             on:focusin={() => appealFocused = true} on:focusout={appealFocusOut}>
          <textarea class="appeal-textarea w-full bg-transparent p-4 pr-12 text-sm leading-relaxed focus:outline-none resize-none placeholder-opacity-60 min-h-[96px]" rows="4"
            bind:this={appealTextarea}
            bind:value={localStoryCfg.differentiating_idea} placeholder={$t('config.appeal.placeholder')}></textarea>
          <!-- Wand "Enhance" (user request): appears TOP-RIGHT INSIDE the box
               while the textarea has focus (or its own generation is in
               flight). Expands left-to-right on hover exactly like the design
               preview; rewrites this draft through the chat fast path. -->
          {#if appealFocused || genBusy['enhance:differentiating_idea']}
            <EnhanceButton busy={!!genBusy['enhance:differentiating_idea']} label={$t('config.appeal.enhance')}
              on:click={() => { enhanceField('differentiating_idea', localStoryCfg.differentiating_idea); appealTextarea?.focus(); }} />
          {/if}
          <div class="appeal-footer border-t border-[#EFEFEF] bg-white p-3 flex flex-col gap-1 dark:bg-base-100 dark:border-base-300">
            <!-- Collapsible creativity bar (grid 0fr->1fr + fade, per design) -->
            <div class="appeal-collapsible" class:appeal-open={appealPanelOpen}>
              <div class="appeal-collapsible-inner grid grid-cols-[auto_1fr_auto] items-center gap-3">
                <div class="flex justify-start">
                  <!-- Svelte 4: event directives MUST be `on:click`. A plain
                       `onclick={handler}` attribute is treated as a static
                       property on elements and is NEVER wired as a listener —
                       that is why these buttons were dead no-ops. -->
                  <button type="button" on:click={toggleAppealPanel}
                    class="w-7 h-7 rounded-lg border border-[#E5E5E5] text-[#777777] hover:text-[#191919] hover:border-[#CCCCCC] bg-[#FAFAFA] text-xs font-mono transition-all duration-150 flex items-center justify-center"
                    title={$t('config.appeal.close')}>✕</button>
                </div>
                <div class="flex flex-col gap-1.5 w-full max-w-[280px] sm:max-w-[320px] mx-auto px-2">
                  <div class="flex items-center justify-between text-xs font-mono">
                    <span class="text-[11px] text-[#888888] flex items-center gap-1.5">
                      <span class="w-1.5 h-1.5 rounded-full bg-[#191919] inline-block"></span>
                      {$t('config.appeal.creativity')}
                    </span>
                    <span class="text-[11px] font-semibold text-[#191919] dark:text-base-content">{appealLevelLabel}</span>
                  </div>
                  <div class="flex items-center gap-2 w-full">
                    <span class="text-[10px] text-[#AAAAAA]">1</span>
                    <input type="range" min="1" max="5" step="1" bind:value={appealCreativity} class="appeal-range w-full cursor-pointer" />
                    <span class="text-[10px] text-[#AAAAAA]">5</span>
                  </div>
                </div>
                <div class="flex justify-end">
                  <!-- Aurora "Suggest" bulb: mirrors AiGenerateButton.svelte's
                       layout so the icon stays centered while collapsed (the
                       old `pl-2` wrapper + absolute label pushed everything a
                       few px to the right and let the expanded content spill
                       out of the fixed-width button). The icon lives in a
                       fixed 36px square; the label is absolutely positioned
                       OUTSIDE that square and only becomes visible on hover,
                       when the button grows to its open width. -->
                  <button type="button" class="appeal-suggest-btn shrink-0" class:appeal-open={appealSuggesting}
                    disabled={appealSuggesting} on:click={suggestDifferentiatingIdea} title={$t('config.appeal.suggest')}>
                    <span class="appeal-icon relative z-[2] flex items-center justify-center shrink-0">
                      {#if appealSuggesting}
                        <span class="loading loading-spinner loading-xs text-white"></span>
                      {:else}
                        <svg class="w-[18px] h-[18px] overflow-visible" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                          <defs>
                            <linearGradient id="auroraGlowCfg" x1="0" y1="0" x2="24" y2="24" gradientUnits="userSpaceOnUse">
                              <stop offset="0%" class="appeal-stop1" />
                              <stop offset="50%" class="appeal-stop2" />
                              <stop offset="100%" class="appeal-stop3" />
                            </linearGradient>
                          </defs>
                          <g class="appeal-analemma">
                            <path d="M12 3C8.13401 3 5 6.13401 5 10C5 12.83 7.2 15.2 8.5 16.3C9.2 16.9 9.5 17.3 9.5 18V18.5C9.5 18.8866 9.8134 19.2 10.2 19.2H13.8C14.1866 19.2 14.5 18.8866 14.5 18.5V18C14.5 17.3 14.8 16.9 15.5 16.3C16.8 15.2 19 12.83 19 10C19 6.13401 15.866 3 12 3Z" stroke="url(#auroraGlowCfg)" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
                            <path d="M13 5L8.5 11.5H12.5L11 16.5L16 10H12L13 5Z" fill="url(#auroraGlowCfg)" stroke="url(#auroraGlowCfg)" stroke-width="0.5" stroke-linejoin="round" />
                            <path d="M10 20.8H14" stroke="url(#auroraGlowCfg)" stroke-width="1.8" stroke-linecap="round" />
                          </g>
                        </svg>
                      {/if}
                    </span>
                    <span class="appeal-label font-semibold text-[13px] whitespace-nowrap select-none">{$t('config.appeal.suggest')}</span>
                  </button>
                </div>
              </div>
            </div>
            <div class="flex items-center justify-end">
              {#if !appealPanelOpen}
                <button type="button" on:click={toggleAppealPanel}
                  class="px-3 py-1.5 rounded-lg border border-[#E5E5E5] text-[#555555] hover:text-[#191919] hover:border-[#CCCCCC] bg-[#FAFAFA] text-xs font-mono font-medium transition-all duration-150 flex items-center gap-1.5 dark:bg-base-100 dark:text-base-content/60">
                  <span>{$t('config.appeal.suggest')}</span>
                </button>
              {/if}
            </div>
          </div>
        </div>
      </div>
      <div class="flex justify-end gap-1.5">
        <button class="btn btn-primary btn-xs hover-target" on:click={saveStoryConfig} disabled={$taskRunning}>{$t('common.save')}</button>
      </div>
    </div>
  </div>
    {/if}

    {#if tab === 'novelParams'}
  <!-- Story idea (AI generation seed) — comes first so the style generator can use it -->
  <div class="card bg-base-200 group/card">
    <div class="card-body p-4 gap-2">
      <div class="flex justify-between items-center">
        <h3 class="card-title text-base">{$t('config.story_idea.title')}</h3>
        <AiGenerateButton hoverReveal={true} busy={!!genBusy['story_idea']} title={genLastError || $t('common.generate')} on:click={genClick('story_idea')} />
      </div>
      <div>
        <span class="field-label">{$t('config.story_idea.subtitle')}</span>
        <EnhanceField field="story_idea" controlClass="textarea w-full text-base" bind:value={localStoryCfg.story_idea} placeholder={$t('config.story_idea.placeholder')} disabled={$taskRunning}
          busy={!!genBusy['enhance:story_idea']} label={$t('config.appeal.enhance')} onEnhance={enhanceField} />
      </div>
      <div>
        <span class="field-label">{$t('config.forces.title')}</span>
        <EnhanceField field="forces_in_tension" rows="2" bind:value={localStoryCfg.forces_in_tension} placeholder={$t('config.forces.placeholder')} disabled={$taskRunning}
          busy={!!genBusy['enhance:forces_in_tension']} label={$t('config.appeal.enhance')} onEnhance={enhanceField} />
      </div>
      <div>
        <span class="field-label">{$t('config.elevator.title')}</span>
        <EnhanceField field="elevator_pitch" rows="2" bind:value={localStoryCfg.elevator_pitch} placeholder={$t('config.elevator.placeholder')} disabled={$taskRunning}
          busy={!!genBusy['enhance:elevator_pitch']} label={$t('config.appeal.enhance')} onEnhance={enhanceField} />
      </div>
      <div>
        <span class="field-label">{$t('config.lighthouse.title')}</span>
        <EnhanceField field="lighthouse_sentence" rows="2" bind:value={localStoryCfg.lighthouse_sentence} placeholder={$t('config.lighthouse.placeholder')} disabled={$taskRunning}
          busy={!!genBusy['enhance:lighthouse_sentence']} label={$t('config.appeal.enhance')} onEnhance={enhanceField} />
      </div>
      <div class="flex justify-end gap-1.5">
        <button class="btn btn-primary btn-xs hover-target" on:click={saveStoryConfig} disabled={$taskRunning}>{$t('common.save')}</button>
      </div>
    </div>
  </div>

  <!-- Writing Style & POV -->
  <div class="card bg-base-200 group/card">
    <div class="card-body p-4 gap-2">
      <div class="flex justify-between items-center">
        <h3 class="card-title text-base">{$t('config.style.title')}</h3>
        <AiGenerateButton hoverReveal={true} busy={!!genBusy['style']} title={genLastError || $t('common.generate')} on:click={genClick('style')} />
      </div>
      <div>
        <span class="field-label">{$t('config.style.label')}</span>
        <EnhanceField field="writing_style" controlClass="textarea w-full text-base" bind:value={localStoryCfg.writing_style} placeholder={$t('config.style.placeholder')} disabled={$taskRunning}
          busy={!!genBusy['enhance:writing_style']} label={$t('config.appeal.enhance')} onEnhance={enhanceField} />
      </div>
      <div>
        <span class="field-label">{$t('config.pov.label')}</span>
        <EnhanceField field="writing_pov" controlClass="textarea w-full text-base" bind:value={localStoryCfg.writing_pov} placeholder={$t('config.pov.placeholder')} disabled={$taskRunning}
          busy={!!genBusy['enhance:writing_pov']} label={$t('config.appeal.enhance')} onEnhance={enhanceField} />
      </div>
      <div class="flex justify-end gap-1.5">
        <button class="btn btn-primary btn-xs hover-target" on:click={saveStoryConfig} disabled={$taskRunning}>{$t('common.save')}</button>
      </div>
    </div>
  </div>

    {/if}

    {#if tab === 'lore'}
  <!-- Characters -->
  <div class="card bg-base-200 group/card">
    <div class="card-body p-12 gap-5">
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div class="flex justify-between items-center">
        <h3 class="card-title text-base">{$t('config.char.title')} <span class="text-xs font-normal text-base-content/65">({chars.length})</span></h3>
        <AiGenerateButton hoverReveal={true} busy={!!genBusy['characters']} title={genLastError || $t('common.generate')} on:click={genClick('characters')} />
      </div>
      
        <div class="grid grid-cols-[repeat(auto-fill,minmax(220px,1fr))] gap-3">
          {#if chars.length === 0}
            <p class="text-xs text-base-content/65 col-span-full py-4">{$t('config.char.empty')}</p>
          {:else}
            {#each chars as c}
              <div class="relative flex items-start gap-3 bg-base-300/25 rounded-lg p-3.5 group">
                <div class="w-8 h-8 rounded-full bg-base-300/60 text-base-content/80 flex items-center justify-center text-xs font-bold shrink-0">{stripNameMarks(c.name)[0]}</div>
                <div class="flex-1 min-w-0">
                  <div class="text-sm font-medium truncate">{stripNameMarks(c.name)}</div>
                  <div class="text-xs text-base-content/65 line-clamp-1">{c.personality || c.background || c.age || ''}</div>
                </div>
                <div class="absolute right-1.5 top-1.5 flex gap-1 rounded-lg p-0.5 opacity-0 pointer-events-none group-hover:opacity-100 group-hover:pointer-events-auto group-focus-within:opacity-100 group-focus-within:pointer-events-auto transition-opacity duration-200">
                  <button class="btn row-act-btn btn-xs btn-square" title={$t('common.edit')} aria-label={$t('common.edit')} on:click={() => openCharForm(c)} disabled={$taskRunning}>
                    <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 20h9" /><path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z" /></svg>
                  </button>
                  <button class="btn row-act-btn row-act-delete btn-xs btn-square" title={$t('common.delete')} aria-label={$t('common.delete')} on:click={() => deleteCharacter(c.id)} disabled={$taskRunning}>
                    <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 6h18" /><path d="M8 6V4h8v2" /><path d="M19 6l-1 14H6L5 6" /><path d="M10 11v6M14 11v6" /></svg>
                  </button>
                </div>
              </div>
            {/each}
          {/if}
        </div>

        <Modal open={showCharForm} wide title={$editingCharID ? $t('config.char.editTitle') : $t('config.char.create')} on:close={closeCharForm}>
          <svelte:fragment slot="actions">
            <AiGenerateButton busy={charGenBusy()} labelKey="common.generate" title={genLastError || $t('common.generate')} on:click={() => generateCharacterEntry()} />
          </svelte:fragment>
          <div class="grid grid-cols-2 gap-x-3 gap-y-2">
            <div>
              <span class="field-label">{$t('config.char.name')}</span>
              <EnhanceField type="input" field="char:name" controlClass="input input-sm w-full" bind:value={charName} disabled={$taskRunning}
                busy={loreBusy('char:name')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
            </div>
            <div>
              <span class="field-label">{$t('config.char.age')}</span>
              <EnhanceField type="input" field="char:age" controlClass="input input-sm w-full" bind:value={charAge} disabled={$taskRunning}
                busy={loreBusy('char:age')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
            </div>
            <div>
              <span class="field-label">{$t('config.char.appearance')}</span>
              <EnhanceField field="char:appearance" bind:value={charAppearance} disabled={$taskRunning}
                busy={loreBusy('char:appearance')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
            </div>
            <div>
              <span class="field-label">{$t('config.char.personality')}</span>
              <EnhanceField field="char:personality" bind:value={charPersonality} disabled={$taskRunning}
                busy={loreBusy('char:personality')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
            </div>
            <div>
              <span class="field-label">{$t('config.char.background')}</span>
              <EnhanceField field="char:background" bind:value={charBackground} disabled={$taskRunning}
                busy={loreBusy('char:background')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
            </div>
            <div>
              <span class="field-label">{$t('config.char.motivation')}</span>
              <EnhanceField field="char:motivation" bind:value={charMotivation} disabled={$taskRunning}
                busy={loreBusy('char:motivation')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
            </div>
            <div>
              <span class="field-label">{$t('config.char.abilities')}</span>
              <EnhanceField field="char:abilities" bind:value={charAbilities} disabled={$taskRunning}
                busy={loreBusy('char:abilities')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
            </div>
            <div>
              <span class="field-label">{$t('config.char.notes')}</span>
              <EnhanceField field="char:notes" bind:value={charNotes} disabled={$taskRunning}
                busy={loreBusy('char:notes')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
            </div>
          </div>

          <!-- Protagonist arc quartet: want / need / wound / agency. -->
          <div class="divider my-2 h-px"></div>
          <div class="text-xs font-semibold text-base-content/70 mb-1">{$t('config.char.arc.title')}</div>
          <div class="grid grid-cols-2 gap-x-3 gap-y-2">
            <div>
              <span class="field-label">{$t('config.char.want')}</span>
              <EnhanceField field="char:want" rows="2" bind:value={charWant} disabled={$taskRunning}
                placeholder={$t('config.char.want.placeholder')}
                busy={loreBusy('char:want')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
            </div>
            <div>
              <span class="field-label">{$t('config.char.need')}</span>
              <EnhanceField field="char:need" rows="2" bind:value={charNeed} disabled={$taskRunning}
                placeholder={$t('config.char.need.placeholder')}
                busy={loreBusy('char:need')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
            </div>
            <div>
              <span class="field-label">{$t('config.char.wound')}</span>
              <EnhanceField field="char:wound" rows="2" bind:value={charWound} disabled={$taskRunning}
                placeholder={$t('config.char.wound.placeholder')}
                busy={loreBusy('char:wound')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
            </div>
            <div>
              <span class="field-label">{$t('config.char.agency')}</span>
              <EnhanceField field="char:agency" rows="2" bind:value={charAgency} disabled={$taskRunning}
                placeholder={$t('config.char.agency.placeholder')}
                busy={loreBusy('char:agency')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
            </div>
          </div>
          <svelte:fragment slot="footer">
            <button class="btn btn-ghost btn-sm" on:click={closeCharForm}>{$t('common.cancel')}</button>
            <button class="btn btn-primary btn-sm" on:click={saveCharacter} disabled={$taskRunning}>{$t('config.char.save')}</button>
          </svelte:fragment>
        </Modal>

        <div class="flex items-center justify-between gap-2 min-h-[42px]">
          <button class="btn btn-primary btn-sm hover-target" on:click={requestNewChar} disabled={$taskRunning}>{$t('config.char.create')}</button>
          {#if chars.length > 0}
            <AiGenerateButton hoverReveal plane expandedWidth={214} disabled={$taskRunning} text={$t('config.char.submit')} title={$t('config.char.submit')} on:click={submitCharacters} />
          {/if}
        </div>
    </div>
  </div>

  <!-- Worldview -->
  <div class="card bg-base-200 group/card">
    <div class="card-body p-12 gap-5">
      <h3 class="card-title text-base">{$t('config.wv.title')} <span class="text-xs font-normal text-base-content/65">({allWvs.length})</span></h3>

        {#each wvSections as sec (sec.cat)}
          {@const list = wvsByCat[sec.cat] || []}
          <!-- Each category is its own hover-section: its Generate button only
               appears when the pointer is over that specific section. -->
          <div class="group/hover-section border-t border-base-300 pt-5">
            <div class="flex justify-between items-center">
              <span class="field-label">{$t('config.wv.cat.' + sec.cat)} <span class="text-base-content/50">({list.length})</span></span>
              <div class="flex gap-1.5 items-center">
                <button class="btn btn-ghost btn-xs hover-target" on:click={() => requestNewWv(sec.cat)}>+ {$t('config.wv.create')}</button>
                <AiGenerateButton hoverReveal={true} busy={!!genBusy[sec.section]} title={genLastError || $t('common.generate')} on:click={genClick(sec.section)} />
              </div>
            </div>

            {#if list.length === 0}
              <p class="text-xs text-base-content/65 py-3">{$t('config.wv.empty')}</p>
            {:else}
              <div class="grid grid-cols-[repeat(auto-fill,minmax(220px,1fr))] gap-3 mt-2">
                {#each list as w (w.id)}
                  <div class="relative flex items-start gap-3 bg-base-300/25 rounded-lg p-3.5 group">
                    <div class="w-8 h-8 rounded-lg bg-base-300/60 text-base-content/80 flex items-center justify-center text-xs font-bold shrink-0">{w.name[0]}</div>
                    <div class="flex-1 min-w-0">
                      <div class="text-sm font-medium truncate">{w.name}</div>
                      <div class="text-xs text-base-content/65 line-clamp-1">{w.description}</div>
                    </div>
                    <div class="absolute right-1.5 top-1.5 flex gap-1 rounded-lg p-0.5 opacity-0 pointer-events-none group-hover:opacity-100 group-hover:pointer-events-auto group-focus-within:opacity-100 group-focus-within:pointer-events-auto transition-opacity duration-200">
                      <button class="btn row-act-btn btn-xs btn-square" title={$t('common.edit')} aria-label={$t('common.edit')} on:click={() => openWvForm(w)} disabled={$taskRunning}>
                        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 20h9" /><path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z" /></svg>
                      </button>
                      <button class="btn row-act-btn row-act-delete btn-xs btn-square" title={$t('common.delete')} aria-label={$t('common.delete')} on:click={() => deleteWorldview(w.id)} disabled={$taskRunning}>
                        <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 6h18" /><path d="M8 6V4h8v2" /><path d="M19 6l-1 14H6L5 6" /><path d="M10 11v6M14 11v6" /></svg>
                      </button>
                    </div>
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        {/each}

        <Modal open={showWvForm} title={$editingWvID ? $t('config.wv.editTitle') : $t('config.wv.create')} on:close={closeWvForm}>
          <svelte:fragment slot="actions">
            <AiGenerateButton busy={wvGenBusy()} labelKey="config.generate.worldview" title={genLastError || $t('common.generate')} on:click={() => generateWorldviewEntry()} />
          </svelte:fragment>
          <div class="grid grid-cols-2 gap-x-3 gap-y-2">
            <div>
              <span class="field-label">{$t('config.wv.name')}</span>
              <EnhanceField type="input" field="wv:name" controlClass="input input-sm w-full" bind:value={wvName} disabled={$taskRunning}
                busy={loreBusy('wv:name')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
            </div>
            <div>
              <span class="field-label">{$t('config.wv.category')}</span>
              <!-- svelte-ignore a11y-no-redundant-roles -->
              <SmartSelect
                options={['geography', 'location', 'faction', 'rule', 'knowledge', 'history', 'other'].map((c) => ({ value: c, label: $t('config.wv.cat.' + c) }))}
                allowCustom={false}
                allowClear={false}
                disabled={$taskRunning}
                ariaLabel={$t('config.wv.category')}
                bind:value={wvCategory}
              />
            </div>
            <div class="col-span-2">
              <span class="field-label">{$t('config.wv.description')}</span>
              <EnhanceField field="wv:description" rows="4" bind:value={wvDescription} disabled={$taskRunning}
                busy={loreBusy('wv:description')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
            </div>
            <div class="col-span-2">
              <span class="field-label">{$t('config.wv.tags')}</span>
              <EnhanceField type="input" field="wv:tags" controlClass="input input-sm w-full" bind:value={wvTags} placeholder={$t('config.wv.tags.placeholder')} disabled={$taskRunning}
                busy={loreBusy('wv:tags')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
            </div>
          </div>
          <svelte:fragment slot="footer">
            <button class="btn btn-ghost btn-sm" on:click={closeWvForm}>{$t('common.cancel')}</button>
            <button class="btn btn-primary btn-sm" on:click={saveWorldview} disabled={$taskRunning}>{$t('common.save')}</button>
          </svelte:fragment>
        </Modal>

        <div class="flex items-center justify-end gap-2 min-h-[42px]">
          {#if allWvs.length > 0}
            <AiGenerateButton hoverReveal plane expandedWidth={214} disabled={$taskRunning} text={$t('config.wv.submit')} title={$t('config.wv.submit')} on:click={submitWorldview} />
          {/if}
        </div>
    </div>
  </div>

  <!-- Organizations -->
  <div class="card bg-base-200 group/card">
    <div class="card-body p-12 gap-5">
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div class="flex justify-between items-center">
        <h3 class="card-title text-base">{$t('config.org.title')} <span class="text-xs font-normal text-base-content/65">({orgs.length})</span></h3>
        <AiGenerateButton hoverReveal={true} busy={!!genBusy['organizations']} title={genLastError || $t('common.generate')} on:click={genClick('organizations')} />
      </div>
      
        <div class="grid grid-cols-[repeat(auto-fill,minmax(220px,1fr))] gap-3">
          {#if orgs.length === 0}
            <p class="text-xs text-base-content/65 col-span-full py-4">{$t('config.org.empty')}</p>
          {:else}
            {#each orgs as o}
              <div class="relative flex items-start gap-3 bg-base-300/25 rounded-lg p-3.5 group">
                <div class="w-8 h-8 rounded-lg bg-base-300/60 text-base-content/80 flex items-center justify-center text-xs font-bold shrink-0">{o.name[0]}</div>
                <div class="flex-1 min-w-0">
                  <div class="text-sm font-medium truncate">{o.name} {#if o.type}<span class="text-xs font-normal text-base-content/30">[{o.type}]</span>{/if}</div>
                  <div class="text-xs text-base-content/65 line-clamp-1">{o.description || ''}</div>
                  {#if (o.members || []).length > 0}
                    <div class="text-xs text-base-content/65 line-clamp-1 mt-0.5">{$t('config.org.membersList', { names: (o.members || []).map(id => nameById[id] || id).join(', ') })}</div>
                  {/if}
                </div>
                <div class="absolute right-1.5 top-1.5 flex gap-1 rounded-lg p-0.5 opacity-0 pointer-events-none group-hover:opacity-100 group-hover:pointer-events-auto group-focus-within:opacity-100 group-focus-within:pointer-events-auto transition-opacity duration-200">
                  <button class="btn row-act-btn btn-xs btn-square" title={$t('common.edit')} aria-label={$t('common.edit')} on:click={() => openOrgForm(o)} disabled={$taskRunning}>
                    <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 20h9" /><path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z" /></svg>
                  </button>
                  <button class="btn row-act-btn row-act-delete btn-xs btn-square" title={$t('common.delete')} aria-label={$t('common.delete')} on:click={() => deleteOrganization(o.id)} disabled={$taskRunning}>
                    <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 6h18" /><path d="M8 6V4h8v2" /><path d="M19 6l-1 14H6L5 6" /><path d="M10 11v6M14 11v6" /></svg>
                  </button>
                </div>
              </div>
            {/each}
          {/if}
        </div>

        <Modal open={showOrgForm} wide title={editingOrgID ? $t('config.org.editTitle') : $t('config.org.create')} on:close={closeOrgForm}>
          <svelte:fragment slot="actions">
            <AiGenerateButton busy={orgGenBusy()} labelKey="common.generate" title={genLastError || $t('common.generate')} on:click={() => generateOrganizationEntry()} />
          </svelte:fragment>
          <div class="grid grid-cols-2 gap-x-3 gap-y-2">
            <div>
              <span class="field-label">{$t('config.org.name')}</span>
              <EnhanceField type="input" field="org:name" controlClass="input input-sm w-full" bind:value={orgName} disabled={$taskRunning}
                busy={loreBusy('org:name')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
            </div>
            <div>
              <span class="field-label">{$t('config.org.type')}</span>
              <EnhanceField type="input" field="org:type" controlClass="input input-sm w-full" bind:value={orgType} placeholder={$t('config.org.type.placeholder')} disabled={$taskRunning}
                busy={loreBusy('org:type')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
            </div>
            <div class="col-span-2">
              <span class="field-label">{$t('config.org.description')}</span>
              <EnhanceField field="org:description" rows="4" bind:value={orgDescription} disabled={$taskRunning}
                busy={loreBusy('org:description')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
            </div>
            {#if chars.length > 0}
              <div class="col-span-2">
                <span class="field-label">{$t('config.org.members')}</span>
                <div class="flex flex-wrap gap-x-3 gap-y-1">
                  {#each chars as c}
                    <label class="flex items-center gap-1 cursor-pointer text-sm">
                      <input type="checkbox" class="checkbox checkbox-xs" bind:group={orgMembers} value={c.id} disabled={$taskRunning} />
                      {c.name}
                    </label>
                  {/each}
                </div>
              </div>
            {/if}
          </div>
          <svelte:fragment slot="footer">
            <button class="btn btn-ghost btn-sm" on:click={closeOrgForm}>{$t('common.cancel')}</button>
            <button class="btn btn-primary btn-sm" on:click={saveOrganization} disabled={$taskRunning}>{$t('config.org.save')}</button>
          </svelte:fragment>
        </Modal>

        <div class="flex items-center justify-start gap-2 min-h-[42px]">
          <button class="btn btn-primary btn-sm hover-target" on:click={requestNewOrg} disabled={$taskRunning}>{$t('config.org.create')}</button>
        </div>
    </div>
  </div>

  <!-- Relations -->
  <div class="card bg-base-200 group/card">
    <div class="card-body p-12 gap-5">
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div class="flex justify-between items-center">
        <h3 class="card-title text-base">{$t('config.rel.title')} <span class="text-xs font-normal text-base-content/65">({rels.length})</span></h3>
        <AiGenerateButton hoverReveal={true} busy={!!genBusy['relations']} title={genLastError || $t('common.generate')} on:click={genClick('relations')} />
      </div>
      
        <div class="grid grid-cols-[repeat(auto-fill,minmax(260px,1fr))] gap-3">
          {#if rels.length === 0}
            <p class="text-xs text-base-content/65 col-span-full py-4">{$t('config.rel.empty')}</p>
          {:else}
            {#each rels as r}
              <div class="relative flex items-center gap-3 bg-base-300/25 rounded-lg p-3.5 group">
                <div class="flex-1 min-w-0 text-sm">
                  <div class="flex items-center gap-1.5 flex-wrap">
                    <span class="font-medium">{entityIcons[r.source_type] || ''} {nameById[r.source_id] || r.source_id}</span>
                    <span class="text-base-content/65">→</span>
                    <span class="font-medium">{entityIcons[r.target_type] || ''} {nameById[r.target_id] || r.target_id}</span>
                  </div>
                  <p class="mt-1 text-xs leading-relaxed text-base-content/65 break-words">{r.label}</p>
                </div>
                <div class="absolute right-1.5 top-1.5 flex gap-1 rounded-lg p-0.5 opacity-0 pointer-events-none group-hover:opacity-100 group-hover:pointer-events-auto group-focus-within:opacity-100 group-focus-within:pointer-events-auto transition-opacity duration-200">
                  <button class="btn row-act-btn btn-xs btn-square" title={$t('common.edit')} aria-label={$t('common.edit')} on:click={() => openRelForm(r)} disabled={$taskRunning}>
                    <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 20h9" /><path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z" /></svg>
                  </button>
                  <button class="btn row-act-btn row-act-delete btn-xs btn-square" title={$t('common.delete')} aria-label={$t('common.delete')} on:click={() => deleteRelation(r.id)} disabled={$taskRunning}>
                    <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 6h18" /><path d="M8 6V4h8v2" /><path d="M19 6l-1 14H6L5 6" /><path d="M10 11v6M14 11v6" /></svg>
                  </button>
                </div>
              </div>
            {/each}
          {/if}
        </div>

        <Modal open={showRelForm} title={editingRelID ? $t('config.rel.editTitle') : $t('config.rel.create')} on:close={closeRelForm}>
          <svelte:fragment slot="actions">
            <AiGenerateButton busy={relGenBusy()} labelKey="common.generate" title={genLastError || $t('common.generate')} on:click={() => generateRelationEntry()} />
          </svelte:fragment>
          <div class="grid grid-cols-[1fr_auto_1fr] gap-2 items-end">
            <div>
              <span class="field-label">{$t('config.rel.source')}</span>
              <!-- svelte-ignore a11y-no-redundant-roles -->
              <SmartSelect
                options={entityOptions.map((opt) => ({ value: opt.key, label: opt.label }))}
                allowCustom={false}
                allowClear={false}
                placeholder={$t('config.rel.entityHint')}
                emptyLabel={$t('config.rel.entityHint')}
                disabled={$taskRunning}
                ariaLabel={$t('config.rel.source')}
                bind:value={relSource}
              />
            </div>
            <span class="text-base-content/65 pb-1.5">→</span>
            <div>
              <span class="field-label">{$t('config.rel.target')}</span>
              <!-- svelte-ignore a11y-no-redundant-roles -->
              <SmartSelect
                options={entityOptions.map((opt) => ({ value: opt.key, label: opt.label }))}
                allowCustom={false}
                allowClear={false}
                placeholder={$t('config.rel.entityHint')}
                emptyLabel={$t('config.rel.entityHint')}
                disabled={$taskRunning}
                ariaLabel={$t('config.rel.target')}
                bind:value={relTarget}
              />
            </div>
          </div>
          <div class="mt-2">
            <span class="field-label">{$t('config.rel.label')}</span>
            <EnhanceField type="input" field="rel:label" controlClass="input input-sm w-full" bind:value={relLabel} placeholder={$t('config.rel.label.placeholder')} disabled={$taskRunning}
              busy={loreBusy('rel:label')} label={$t('config.appeal.enhance')} onEnhance={enhanceLoreText} />
          </div>
          <svelte:fragment slot="footer">
            <button class="btn btn-ghost btn-sm" on:click={closeRelForm}>{$t('common.cancel')}</button>
            <button class="btn btn-primary btn-sm" on:click={saveRelation} disabled={$taskRunning}>{$t('config.rel.save')}</button>
          </svelte:fragment>
        </Modal>

        <div class="flex items-center justify-between gap-2 min-h-[42px]">
          <button class="btn btn-primary btn-sm hover-target" on:click={requestNewRel} disabled={$taskRunning || entityOptions.length < 2}>{$t('config.rel.create')}</button>
          {#if entityOptions.length < 2}
            <span class="text-xs text-base-content/65 self-center">{$t('config.rel.needTwo')}</span>
          {/if}
        </div>
    </div>
  </div>
    {/if}
</div>

<style>
  /* Generate/Save buttons stay hidden until the pointer is over their card
     (group/card) or their grouped section (group/hover-section); the opacity
     transition gives a smooth fade between states. */
  .hover-target {
    opacity: 0;
    pointer-events: none;
    transform: translateY(-2px);
    transition: opacity 260ms cubic-bezier(0.16, 1, 0.3, 1),
                transform 260ms cubic-bezier(0.16, 1, 0.3, 1);
  }
  /* A grouped section (group/hover-section) owns its buttons exclusively:
     hovering the outer card must NOT reveal them — only hovering that
     specific section does. Keep this rule in sync with the base state above
     so re-hiding also fades smoothly instead of snapping to invisible. */
  :is(.group\/card, .group\/hover-section):has(.group\/hover-section) .hover-target {
    opacity: 0;
    pointer-events: none;
    transform: translateY(-2px);
    transition: opacity 260ms cubic-bezier(0.16, 1, 0.3, 1),
                transform 260ms cubic-bezier(0.16, 1, 0.3, 1);
  }
  .group\/hover-section:hover .hover-target,
  .group\/hover-section:focus-within .hover-target {
    opacity: 1;
    pointer-events: auto;
    transform: translateY(0);
  }
  :is(.group\/card, .group\/hover-section):not(:has(.group\/hover-section)):hover .hover-target,
  :is(.group\/card, .group\/hover-section):not(:has(.group\/hover-section)):focus-within .hover-target {
    opacity: 1;
    pointer-events: auto;
    transform: translateY(0);
  }

  /* —— Story appeal (differentiating idea) — component styles ported from the
     Notion x Revolut design: fluid grid 0fr->1fr collapsible, custom range
     thumb and the aurora-animated "Suggest" bulb button. Scoped to this page. */

  /* Focus chrome (user request):
     - The wrapper div must NOT show a black border on focus-within. The
       `focus-within:border-[#191919]` utility was removed from the markup; the
       rule below is a safety net so no global/utility focus style can repaint
       the border while its contents are focused (border stays #E5E5E5).
     - The textarea must not show ANY focus outline/ring. `focus:outline-none`
       only covers Tailwind's own utility; the browser default outline, the
       global app.css `:focus-visible { outline: ... }` rule and SneakyUI's
       `.textarea` focus treatment are all neutralized here. Declared outside
       any layer with explicit selectors so it wins over those rules. */
  /* Flat 0.5rem radius (user request). The white footer is a square rectangle
     at the bottom, so its bottom corners are rounded too: otherwise they poked
     out of the rounded wrapper and read as blurred white dots above each
     corner. */
  .appeal-box {
    border-radius: 0.5rem;
  }
  .appeal-footer {
    border-bottom-left-radius: calc(0.5rem - 1px);
    border-bottom-right-radius: calc(0.5rem - 1px);
  }
  .appeal-box:focus-within {
    border-color: #E5E5E5;
    outline: none;
    box-shadow: 0 1px 2px 0 rgb(0 0 0 / 0.05); /* keep the resting shadow-sm */
  }
  .appeal-box .appeal-textarea,
  .appeal-box .appeal-textarea:focus,
  .appeal-box .appeal-textarea:focus-visible {
    outline: none !important;
    box-shadow: none !important;
    border: none;
  }
  /* Font (user request): the appeal textarea must use the same sans font as
     the other fields of this tab (body's var(--font-sans) = Inter / Noto Sans
     fallback stack), NOT the monospace it inherited via `font-mono`. Textareas
     default to the browser monospace font, so set it explicitly. */
  .appeal-box .appeal-textarea {
    font-family: var(--font-sans);
  }

  .appeal-collapsible {
    display: grid;
    grid-template-rows: 0fr;
    opacity: 0;
    visibility: hidden;
    overflow: hidden;
    transition: grid-template-rows 0.45s cubic-bezier(0.16, 1, 0.3, 1),
                opacity 0.35s cubic-bezier(0.16, 1, 0.3, 1),
                visibility 0.45s ease,
                padding 0.45s cubic-bezier(0.16, 1, 0.3, 1);
    padding-top: 0px;
    padding-bottom: 0px;
  }
  .appeal-collapsible.appeal-open {
    grid-template-rows: 1fr;
    opacity: 1;
    visibility: visible;
    padding-top: 2px;
    padding-bottom: 4px;
  }
  .appeal-collapsible-inner {
    min-height: 0;
    overflow: hidden;
  }
  .appeal-range {
    -webkit-appearance: none;
    appearance: none;
    background: transparent;
    height: 16px;
  }
  .appeal-range:focus { outline: none; }
  .appeal-range::-webkit-slider-runnable-track {
    width: 100%;
    height: 4px;
    cursor: pointer;
    background: #EFEFEF;
    border-radius: 9999px;
  }
  .appeal-range::-webkit-slider-thumb {
    height: 14px;
    width: 14px;
    border-radius: 50%;
    background: #191919;
    cursor: pointer;
    -webkit-appearance: none;
    margin-top: -5px;
    box-shadow: 0 1px 3px rgba(0,0,0,0.15);
    transition: transform 0.15s ease;
  }
  .appeal-range::-webkit-slider-thumb:hover { transform: scale(1.2); }
  .appeal-range::-moz-range-track {
    width: 100%;
    height: 4px;
    cursor: pointer;
    background: #EFEFEF;
    border-radius: 9999px;
  }
  .appeal-range::-moz-range-thumb {
    height: 14px;
    width: 14px;
    border: none;
    border-radius: 50%;
    background: #191919;
    cursor: pointer;
    box-shadow: 0 1px 3px rgba(0,0,0,0.15);
  }
  .appeal-suggest-btn {
    position: relative;
    display: inline-flex;
    align-items: center;
    /* Collapsed: the ONLY in-flow child is the fixed 36px icon square, so
       justify-content:center keeps the bulb perfectly centered inside the
       36x36 button (no `pl-2` offset). Open (hover/busy): content is pinned
       to the left edge, exactly like AiGenerateButton's `.btn-content`. */
    justify-content: center;
    height: 36px;
    width: 36px;
    padding: 0;
    background: #0f1115;
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 10px;
    color: #ffffff;
    cursor: pointer;
    outline: none;
    overflow: hidden;
    transition: width 0.45s cubic-bezier(0.16, 1, 0.3, 1),
                justify-content 0s,
                border-color 0.25s ease,
                box-shadow 0.35s ease,
                background-color 0.25s ease,
                transform 0.1s cubic-bezier(0.16, 1, 0.3, 1);
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.12),
                0 8px 16px -4px rgba(0, 0, 0, 0.18),
                inset 0 1px 0 rgba(255, 255, 255, 0.1);
  }
  /* The label is absolutely positioned (out of flow) so it never competes
     with the icon for space — this is what previously spilled past the
     right edge when the button expanded. */
  .appeal-icon {
    width: 36px;
    height: 36px;
  }
  .appeal-suggest-btn:hover:not(:disabled),
  .appeal-suggest-btn.appeal-open {
    width: 120px;
    justify-content: flex-start;
    border-color: rgba(255, 255, 255, 0.22);
    background: #14171d;
    box-shadow: 0 8px 24px -4px rgba(0, 0, 0, 0.25),
                inset 0 1px 0 rgba(255, 255, 255, 0.2);
  }
  .appeal-suggest-btn:active:not(:disabled) { transform: scale(0.96); }
  .appeal-suggest-btn:disabled { opacity: 0.8; cursor: wait; }
  /* The label fades to white on the same 8s timeline as the bulb icon. The
     gradient colours live in registered custom properties so they can be
     animated in place: an overlay `::after` with the same text painted the
     white copy on top of the gradient copy and read as a duplicated, shifted
     label. Browsers without @property keep the static gradient. */
  @property --appeal-l1 { syntax: '<color>'; inherits: false; initial-value: #fef08a; }
  @property --appeal-l2 { syntax: '<color>'; inherits: false; initial-value: #f472b6; }
  @property --appeal-l3 { syntax: '<color>'; inherits: false; initial-value: #a78bfa; }
  .appeal-label {
    position: absolute;
    left: 36px;
    right: 8px;
    top: 50%;
    transform: translate(6px, -50%);
    letter-spacing: -0.1px;
    opacity: 0;
    transition: opacity 0.2s ease 0.06s, transform 0.3s ease 0.06s;
    pointer-events: none;
    color: #ffffff;
    background: linear-gradient(135deg, var(--appeal-l1) 0%, var(--appeal-l2) 50%, var(--appeal-l3) 100%);
    background-size: 200% 200%;
    -webkit-background-clip: text;
    background-clip: text;
    -webkit-text-fill-color: transparent;
    animation: appealLabelAuroraShift 8s ease-in-out infinite alternate,
               appealLabelWhite 8s ease-in-out infinite;
  }
  @keyframes appealLabelWhite {
    0%, 20% { --appeal-l1: #fef08a; --appeal-l2: #f472b6; --appeal-l3: #a78bfa; }
    45%, 70% { --appeal-l1: #ffffff; --appeal-l2: #ffffff; --appeal-l3: #ffffff; }
    95%, 100% { --appeal-l1: #fef08a; --appeal-l2: #f472b6; --appeal-l3: #a78bfa; }
  }
  @keyframes appealLabelAuroraShift {
    0%   { background-position: 0% 50%; }
    100% { background-position: 100% 50%; }
  }
  .appeal-suggest-btn:hover:not(:disabled) .appeal-label,
  .appeal-suggest-btn.appeal-open .appeal-label {
    opacity: 1;
    transform: translate(0, -50%);
  }
  .appeal-analemma {
    animation: appealAnalemmaOrbit 5s ease-in-out infinite;
    filter: drop-shadow(0 0 3px rgba(254, 240, 138, 0.35));
  }
  @keyframes appealAnalemmaOrbit {
    0%   { transform: translate(0px, 0px) rotate(0deg); }
    25%  { transform: translate(0.4px, -1.2px) rotate(0.5deg); }
    50%  { transform: translate(0px, -2px) rotate(0deg); }
    75%  { transform: translate(-0.4px, -1.2px) rotate(-0.5deg); }
    100% { transform: translate(0px, 0px) rotate(0deg); }
  }
  .appeal-stop1 { stop-color: #fef08a; animation: appealStop1 8s ease-in-out infinite; }
  .appeal-stop2 { stop-color: #f472b6; animation: appealStop2 8s ease-in-out infinite; }
  .appeal-stop3 { stop-color: #a78bfa; animation: appealStop3 8s ease-in-out infinite; }
  @keyframes appealStop1 {
    0%, 20% { stop-color: #fef08a; }
    45%, 70% { stop-color: #ffffff; }
    95%, 100% { stop-color: #fef08a; }
  }
  @keyframes appealStop2 {
    0%, 20% { stop-color: #f472b6; }
    45%, 70% { stop-color: #ffffff; }
    95%, 100% { stop-color: #f472b6; }
  }
  @keyframes appealStop3 {
    0%, 20% { stop-color: #a78bfa; }
    45%, 70% { stop-color: #ffffff; }
    95%, 100% { stop-color: #a78bfa; }
  }

</style>
