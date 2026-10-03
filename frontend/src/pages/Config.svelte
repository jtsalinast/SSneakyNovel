<script>
  import { onMount, onDestroy } from 'svelte';
  import { get } from 'svelte/store';
  import { api } from '../lib/api.js';
  import { apiConfig, config, progress, settings, editingCharID, editingWvID, wvFilter, addToast, showConfirm, taskRunning, apiTestResult } from '../lib/stores.js';
  import { t } from '../lib/i18n/index.js';
  import { resolveChatCompletionsURL } from '../lib/apiUrl.js';
  import ConfigChangePanel from '../components/ConfigChangePanel.svelte';
  import { GENRES, SUBGENRE_DATA } from '../lib/novelwriterGenres.js';

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
  let wvName = '', wvCategory = 'other', wvDescription = '', wvTags = '';

  // 组织管理
  let showOrgForm = false;
  let orgName = '', orgType = '', orgDescription = '';
  let orgMembers = [];
  let editingOrgID = null;

  // 关系管理
  let showRelForm = false;
  let relSource = '', relTarget = '', relLabel = '';
  let editingRelID = null;

  $: cfgBase = $apiConfig?.base_url || '';
  $: cfgModel = $apiConfig?.model || '';
  $: cfgKey = $apiConfig?.api_key || '';
  $: cfgTimeout = $apiConfig?.http_timeout_seconds || 600;

  let localApiCfg = { base_url: '', url_strict: false, model: '', api_key: '', http_timeout_seconds: 600, max_tokens: 32768, context_budget_tokens: 900000 };
  // —— Cross-tab state (module level): both workspace tabs ("Config" and "Novel
  // parameters") render this same component, but Svelte destroys and recreates it
  // when switching. Keep unsaved form edits, per-section busy flags and undo
  // snapshots here so they survive a tab change (and a failed save doesn't wipe
  // what the user typed).
  let sharedStoryDraft = null;   // last localStoryCfg value

  let localStoryCfg = { type: '', title: '', subgenre: '', theme: '', tone: '', author: '', story_length: '', structure: '', motif: '', story_idea: '', character_arcs_enabled: false, conflict_scale: '', conflict_other: '', specific_settings: '', protagonist_type: '', protagonist_other: '', gender_bias: 'random', target_audience: '', audience_profile: '', romance_level: '', sexual_content: '', gore_level: '', world_darkness: '', locations_enabled: false, target_words_per_chapter: 2500, writing_style: '', writing_pov: '' };
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
let novelParamsTick = 0;
function subgenreHint(s) { return SUBGENRE_HINTS[(s || '').toLowerCase()] || ''; }
function structHint(k) { return STRUCTURE_HINTS[k] || ''; }

// Novel parameters: genre presets (mirror of backend config.Genre* maps; "other" is always available).
const GENRE_CONFLICTS = {
fantasy: ['Personal Quest', 'Kingdom-wide', 'World-saving', 'Good vs Evil', 'Political Intrigue'],
scifi: ['Personal', 'Planetary', 'Interstellar', 'Galactic'],
mystery: ['Personal Mystery', 'Community Secret', 'Local Crime', 'Family Mystery', 'Historical Puzzle'],
romance: ['Personal Growth', 'Relationship Obstacles', 'Career vs Love', 'Family Issues', 'Past Trauma'],
thriller: ['International Conspiracy', 'Government Secrets', 'Spy Networks', 'National Security', 'Global Politics'],
horror: ['Personal Haunting', 'Family Curse', 'Supernatural Threat', 'Psychological Terror', 'Ancient Evil'],
historical: ['Tribal Warfare', 'Religious Conflicts', 'Ancient Politics', 'Survival Struggles', 'Civilization Building'],
western: ['Personal Vendetta', 'Town Protection', 'Range War', 'Law vs Lawlessness', 'Civilization vs Wilderness'],
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
};
const GENRE_SETTINGS = {
fantasy: ['magic_system', 'medieval_setting', 'mythical_creatures', 'epic_scale'],
scifi: ['interstellar_travel', 'advanced_technology', 'multiple_species', 'space_combat'],
mystery: ['small_community', 'amateur_detective', 'low_violence', 'puzzle_focus', 'recurring_characters'],
romance: ['modern_setting', 'relationship_focus', 'emotional_journey', 'happy_ending', 'realistic_world'],
thriller: ['international_intrigue', 'spy_networks', 'government_secrets', 'double_agents', 'global_stakes'],
horror: ['atmospheric_dread', 'isolated_setting', 'supernatural_elements', 'psychological_terror', 'dark_atmosphere'],
historical: ['ancient_civilizations', 'mythological_elements', 'tribal_societies', 'ancient_religions', 'primitive_technology'],
western: ['frontier_setting', 'lawlessness', 'honor_code', 'survival_focus', 'horse_culture'],
};
const GENRE_KEYWORDS = [
['fantasy', ['fantasy', '奇幻', '玄幻', '魔幻']],
['scifi', ['sci-fi', 'scifi', 'science fiction', '科幻']],
['mystery', ['mystery', 'detective', '悬疑', '推理', '侦探']],
['romance', ['romance', '言情', '爱情']],
['thriller', ['thriller', 'spy', '惊悚', '谍战']],
['horror', ['horror', '恐怖', '灵异']],
['historical', ['historical', 'history', '历史']],
['western', ['western', '西部']],
// round 3 — keep in sync with backend config.MatchGenreKey (specific keys first)
['xianxia', ['xianxia', '仙侠', '修仙']],
['wuxia', ['wuxia', '武侠', '江湖']],
['litrpg', ['litrpg', 'lit rpg', 'game novel', 'system flow', '游戏文', '系统流', '无限流']],
['cyberpunk', ['cyberpunk', 'cyber-punk', '赛博朋克', '赛博']],
['solarpunk', ['solarpunk', 'solar punk', '太阳朋克']],
['space_opera', ['space opera', '星际歌剧', '太空歌剧']],
['romantasy', ['romantasy', 'romantic fantasy', 'epic romance', '浪漫奇幻']],
['dystopian', ['dystopia', 'dystopian', '反乌托邦']],
['postapo', ['post-apocalyp', 'post apocalyp', 'postapoc', 'zombie', '末日', '废土', '丧尸']],
['military', ['military', 'war novel', '军旅', '战争', '军事']],
['heist', ['heist', 'caper', 'robbery', '劫案', '千门']],
['cozy', ['cozy', 'comfort read', '治愈系', '温馨推理', '田园']],
['urban', ['urban', 'contemporary fantasy', 'city', '都市', '现代异能']],
['middle_grade', ['middle grade', 'middle-grade', 'mg novel', '儿童文学', '少儿']],
['ya', ['young adult', 'ya fantasy', 'ya romance', '青春', 'teen']],
['steampunk', ['steampunk', 'steam punk', '蒸汽朋克']],
['superhero', ['superhero', 'super hero', 'supervillain', '超级英雄', '超人']],
['dark_academia', ['dark academia', '暗黑学院', '校园秘密社团']],
['swashbuckler', ['swashbuckl', 'pirate', 'corsair', '海盗', '大航海']],
['nautical', ['nautical', 'seafaring', 'voyage at sea', '航海', '海难']],
['picaresque', ['picaresque', 'rogue tale', '流浪汉', '骗子冒险']],
['gothic', ['gothic', '哥特']],
['time_travel', ['time travel', 'time-travel', '穿越时空', '时间旅行']],
['cli_fi', ['cli-fi', 'clifi', 'climate fiction', '气候小说']],
['afrofuturism', ['afrofuturism', 'afro-futurism', '非洲未来主义']],
['hard_scifi', ['hard scifi', 'hard sci-fi', 'hard science fiction', '硬科幻']],
['graphic_novel', ['graphic novel', 'comic', 'manga script', '图像小说', '漫画']],
['mystery_police', ['police procedural', 'detective squad', 'cold case', '警匪剧', '刑侦', '罪案小组']],
['adventure', ['adventure', 'survival', 'expedition', 'quest', '冒险', '求生', '探险']],
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
const OTHER_GENRE = '__other__';
let selectedGenreKey = '';   // one of GENRES keys or OTHER_GENRE; '' = not classified yet
let subgenreManual = false;  // true while the user types a custom subgenre in "Other" mode
let lastAutoSubgenre = '';   // subgenre we auto-set, so we don't clobber manual edits

function initGenreCascade() {
  const t = (localStoryCfg.type || '').trim();
  // Exact label match first: a known genre ("Fantasy") must never fall into the
  // "Other" box just because keyword matching failed on it.
  const exact = GENRES.find((x) => x.label.toLowerCase() === t.toLowerCase());
  if (exact) { selectedGenreKey = exact.key; subgenreManual = false; return; }
  const g = genreKeyDerived ? GENRES.find((x) => x.key === genreKeyDerived) : null;
  if (g) { selectedGenreKey = g.key; subgenreManual = false; }
  else if (t) { selectedGenreKey = OTHER_GENRE; subgenreManual = true; }
  else { selectedGenreKey = ''; subgenreManual = false; }
}
initGenreCascade();

$: if (genreKeyDerived !== undefined) { void genreKeyDerived; }

// Keep the parent-genre select in sync when the story type changes externally (load/save).
$: if (!subgenreManual && genreKeyDerived && genreKeyDerived !== selectedGenreKey && genreKeyDerived !== OTHER_GENRE) {
  selectedGenreKey = genreKeyDerived;
}

function onGenreChange() {
  if (selectedGenreKey === OTHER_GENRE) {
    localStoryCfg.type = subgenreManual ? localStoryCfg.type : '';
    localStoryCfg.subgenre = '';
    subgenreManual = true;
  } else {
    subgenreManual = false;
    const g = GENRES.find((x) => x.key === selectedGenreKey);
    if (g) {
      localStoryCfg.type = g.label;
      const cur = localStoryCfg.subgenre;
      if (!g.subgenres.includes(cur)) {
        // Only auto-change the subgenre if it was empty or something we auto-set before.
        if (!cur || cur === lastAutoSubgenre) {
          localStoryCfg.subgenre = g.subgenres[0];
          lastAutoSubgenre = g.subgenres[0];
        }
      }
    }
  }
}

function onSubgenreChange() {
  const g = GENRES.find((x) => x.key === selectedGenreKey);
  if (g && !g.subgenres.includes(localStoryCfg.subgenre)) {
    selectedGenreKey = OTHER_GENRE;
    subgenreManual = true;
  }
}

$: subgenreOptions = (() => {
  const g = GENRES.find((x) => x.key === selectedGenreKey);
  return g ? g.subgenres : [];
})();

// Options derived from the selected subgenre (NovelWriter per-subgenre configs), falling back to genre-level lists.
$: subgenreCfg = SUBGENRE_DATA[(localStoryCfg.subgenre || '').trim()] || null;
$: conflictOpts = (novelParamsTick, subgenreCfg ? subgenreCfg.conflicts : (genreKeyDerived ? GENRE_CONFLICTS[genreKeyDerived] : []));
$: protagonistOpts = (novelParamsTick, subgenreCfg ? subgenreCfg.protagonists : (genreKeyDerived ? GENRE_PROTAGONISTS[genreKeyDerived] : []));
$: toneOpts = subgenreCfg ? subgenreCfg.tones : [];
$: settingSuggestions = (novelParamsTick, subgenreCfg ? subgenreCfg.settings : (genreKeyDerived ? GENRE_SETTINGS[genreKeyDerived] : []));

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

// Structure: preset select + optional free-text addition.
function structureExtras(v) {
  return toneList(v).filter((x) => !structOpts.includes(x));
}
function onStructureChange(e) {
  const sel = e.target.value;
  const extras = structureExtras(localStoryCfg.structure);
  const parts = [];
  if (sel && sel !== '__custom__') parts.push(sel);
  if (sel === '__custom__' && extras.length) parts.push(extras[0]);
  localStoryCfg.structure = [...parts, ...extras.filter((x) => x !== parts[0])].join(', ');
}
$: structSelValue = (() => {
  const parts = toneList(localStoryCfg.structure);
  const known = parts.find((x) => structOpts.includes(x));
  if (known) return known;
  return parts.length ? '__custom__' : '';
})();

// Conflict scale / protagonist type: select-with-Other pattern.
$: conflictSelKnown = conflictOpts.includes(localStoryCfg.conflict_scale);
$: conflictIsOther = !!localStoryCfg.conflict_scale && !conflictSelKnown;
function onConflictSelect(e) {
  const v = e.target.value;
  if (v === 'other') { localStoryCfg.conflict_scale = 'other'; }
  else { localStoryCfg.conflict_scale = v; localStoryCfg.conflict_other = ''; }
}
$: protSelKnown = protagonistOpts.includes(localStoryCfg.protagonist_type);
$: protIsOther = !!localStoryCfg.protagonist_type && !protSelKnown;
function onProtSelect(e) {
  const v = e.target.value;
  if (v === 'other') { localStoryCfg.protagonist_type = 'other'; }
  else { localStoryCfg.protagonist_type = v; localStoryCfg.protagonist_other = ''; }
}

// Specific settings as checkboxes: parse the stored newline-separated list into a set.
let checkedSettings = new Set();
let customSettingsText = '';
function refreshCheckedSettings() {
  const all = (localStoryCfg.specific_settings || '').split('\n').map((x) => x.trim()).filter(Boolean);
  checkedSettings = new Set(all);
  customSettingsText = all.filter((x) => !settingSuggestions.includes(x)).join('\n');
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
async function fetchNovelParams() {
try {
const p = await api('GET', '/api/novel-params');
if (p && p.conflict_scales) {
for (const k of Object.keys(p.conflict_scales)) GENRE_CONFLICTS[k] = p.conflict_scales[k].filter((x) => x !== 'other');
for (const k of Object.keys(p.protagonist_types)) GENRE_PROTAGONISTS[k] = p.protagonist_types[k].filter((x) => x !== 'other');
if (p.specific_settings) for (const k of Object.keys(p.specific_settings)) GENRE_SETTINGS[k] = p.specific_settings[k];
if (p.subgenre_hints) SUBGENRE_HINTS = p.subgenre_hints;
if (p.structure_hints) STRUCTURE_HINTS = p.structure_hints;
if (Array.isArray(p.subgenre_presets)) SUBGENRE_PRESETS = p.subgenre_presets;
novelParamsTick++;
}
} catch (e) {}
}
  let testingApi = false;

  // Novel parameters: structure options depend on the selected story length
  const LENGTH_KEYS = ['short', 'novella', 'novel', 'epic'];
  const STRUCTURES_BY_LENGTH = {
    short: ['three_act', 'fichtean', 'freytag'],
    novella: ['three_act', 'seven_point', 'heros_journey_simple'],
    novel: ['three_act', 'six_act', 'save_the_cat', 'heros_journey'],
    epic: ['six_act', 'heros_journey', 'save_the_cat', 'episodic'],
  };
  const DEFAULT_STRUCTURE = { short: 'three_act', novella: 'three_act', novel: 'six_act', epic: 'six_act' };
  function structureOptions(len) { return STRUCTURES_BY_LENGTH[len] || []; }
  $: structOpts = structureOptions(localStoryCfg.story_length);
  function onLengthChange() {
    const opts = structureOptions(localStoryCfg.story_length);
    if (opts.length && !opts.includes(localStoryCfg.structure)) {
      localStoryCfg.structure = DEFAULT_STRUCTURE[localStoryCfg.story_length] || opts[0];
    }
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
        if (sec === 'motif') { owned.add('theme'); owned.add('motif'); }
        else if (sec === 'story_idea') owned.add('story_idea');
        else if (sec === 'audience_profile') owned.add('audience_profile');
        else if (sec === 'style') { owned.add('writing_style'); owned.add('writing_pov'); }
      }
      const merged = {};
      for (const k of ['theme', 'motif', 'story_idea', 'audience_profile', 'writing_style', 'writing_pov']) {
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
    genPollTimer = setInterval(async () => {
      let st = null;
      try { st = await api('GET', '/api/status').catch(() => null); } catch (e) {}
      if (st?.is_task_running) return; // still generating
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
  async function persistStoryBeforeGenerate() {
    const story = storyPayload();
    try {
      const saved = await api('PUT', '/api/config', { ...($config || {}), story });
      config.set(saved);
      storyCfgSnapshot = JSON.stringify(saved?.story || '');
      return true;
    } catch (e) {
      // A task running server-side rejects PUT /api/config (409). Don't let
      // that abort the flow: the generate POST carries the live payload and
      // will report the real conflict itself.
      return false;
    }
  }
  async function generateSection(section, extra = {}, undoKey = section) {
    // Only block when *this* section already has a generation in flight. We
    // never consult the global $taskRunning store here: a stale-true flag
    // (SSE having missed a task_end, or a task started from another page)
    // used to make every Generate click a silent no-op — no fetch at all,
    // nothing in the Network tab. Server-side conflicts surface as a 409
    // toast from startGeneration() instead.
    if (genBusy[section] || sharedGenBusy[section]) return;
    // Snapshot what this generation may touch, so the user can undo it.
    if (section === 'characters') captureSnapshot(undoKey, { type: 'all', entityType: 'character' });
    else if (section === 'organizations') captureSnapshot(undoKey, { type: 'all', entityType: 'organization' });
    else if (section === 'relations') captureSnapshot(undoKey, extra.source_id ? { type: 'pair', entityType: 'relation', srcId: extra.source_id, tgtId: extra.target_id } : { type: 'all', entityType: 'relation' });
    else if (section === 'worldview') captureSnapshot(undoKey, { type: 'entity', entityType: 'worldview', entityId: extra.entry_id || '__new__' });
    else if (section === 'locations') captureSnapshot(undoKey, { type: 'all', entityType: 'worldview', ids: allWvs.map(w => w.id) });
    else if (section === 'style') captureStoryFields(undoKey, ['writing_style', 'writing_pov']);
    else if (section === 'motif') captureStoryFields(undoKey, ['theme', 'motif']);
    else if (section === 'story_idea') captureStoryFields(undoKey, ['story_idea']);
    else if (section === 'audience_profile') captureStoryFields(undoKey, ['audience_profile']);
    await persistStoryBeforeGenerate();
    const story = storyPayload();
    const idea = (story.story_idea || '').trim() || ($config?.story?.story_idea || '').trim();
    if (!idea && section !== 'motif' && section !== 'story_idea' && section !== 'audience_profile' && section !== 'style') { addToast($t('config.story_idea.required'), 'error'); discardUndo(undoKey); return; }
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
      motif: $t('config.generate.chat.motif'),
      story_idea: $t('config.generate.chat.story_idea'),
      audience_profile: $t('config.generate.chat.audience_profile'),
    };
    const msg = chatMsgs[section];
    if (msg) {
      genBusy = { ...genBusy, [section]: true };
      sharedGenBusy[section] = true;
      try {
        await sendToChat(msg);
        // The agent's generate_section tool kicks off the same background
        // task the HTTP buttons used to start directly; poll its progress so
        // the busy flag clears and results refresh when it finishes.
        startGenPolling();
      } catch (e) {
        delete sharedGenBusy[section];
        genBusy = { ...genBusy, [section]: false };
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
  function genBtnProps(section) {
    return {
      class: 'btn btn-accent btn-xs',
      disabled: !!genBusy[section],
      title: genBusy[section] ? '' : (genLastError || $t('common.generate')),
      onclick: (e) => { e.stopPropagation(); generateSection(section); },
    };
  }
  function undoBtnProps(key) {
    return {
      class: 'btn btn-ghost btn-xs border border-base-content/25',
      disabled: !$taskRunning && !genUndo[key],
      title: $t('config.generate.undoHint'),
      onclick: (e) => { e.stopPropagation(); undoGenerate(key); },
    };
  }

  $: resolvedChatURL = resolveChatCompletionsURL(localApiCfg.base_url, !!localApiCfg.url_strict);

  let apiCfgSnapshot = '';
  let storyCfgSnapshot = '';

  $: if ($apiConfig) {
    const snap = JSON.stringify($apiConfig);
    if (snap !== apiCfgSnapshot) {
      localApiCfg = {
        base_url: '', url_strict: false, model: '', api_key: '', http_timeout_seconds: 600, max_tokens: 32768, context_budget_tokens: 900000,
        ...$apiConfig,
        url_strict: !!$apiConfig.url_strict,
      };
      apiCfgSnapshot = snap;
    }
  }
  $: if ($config?.story) {
    const snap = JSON.stringify($config.story);
    if (snap !== storyCfgSnapshot) {
      localStoryCfg = { conflict_scale: '', conflict_other: '', specific_settings: '', protagonist_type: '', protagonist_other: '', gender_bias: 'random', locations_enabled: false, ...$config.story };
      if (!localStoryCfg.gender_bias) localStoryCfg.gender_bias = 'random';
      localStoryCfg.locations_enabled = !!$config.story.locations_enabled;
      storyCfgSnapshot = snap;
      refreshCheckedSettings();
      initGenreCascade();
    }
  }

  $: hasAccepted = $progress?.chapters?.some(c => c.status === 'accepted') || false;

  $: chars = ($settings?.characters || []);
  $: allWvs = ($settings?.worldview || []);
  $: filteredWvs = $wvFilter === 'all' ? allWvs : allWvs.filter(w => w.category === $wvFilter);
  $: orgs = ($settings?.organizations || []);
  $: rels = ($settings?.relations || []);

  const entityIcons = { character: '', organization: '', worldview: '' };

  // 关系双方可选实体（角色 / 组织 / 世界观条目）
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
  $: wvTabs = [
    ['all', $t('config.wv.cat.all')],
    ['geography', $t('config.wv.cat.geography')],
    ['location', $t('config.wv.cat.location')],
    ['faction', $t('config.wv.cat.faction')],
    ['rule', $t('config.wv.cat.rule')],
    ['knowledge', $t('config.wv.cat.knowledge')],
    ['history', $t('config.wv.cat.history')],
    ['other', $t('config.wv.cat.other')],
  ];

  onMount(async () => {
    try { apiConfig.set(await api('GET', '/api/config/api')); } catch (e) {}
    try { config.set(await api('GET', '/api/config')); } catch (e) {}
    try { settings.set(await api('GET', '/api/settings')); } catch (e) {}
    fetchNovelParams();
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

  // 影响连接测试的字段签名；配置改动后据此自动清除持久化的测试结果
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

  // 直接保存故事配置（不经过 AI），存在已确认章节且关键设定有变化时提示协调
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
    } else {
      $editingCharID = null;
      charName = charAge = charAppearance = charPersonality = charBackground = charMotivation = charAbilities = charNotes = '';
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
    const data = { name: charName.trim(), age: charAge, appearance: charAppearance, personality: charPersonality, background: charBackground, motivation: charMotivation, abilities: charAbilities, notes: charNotes };
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
    await sendToChat($t('config.char.submitMsg', { n: chars.length, lines }));
    addToast($t('config.char.submitted'), 'success');
  }

  let wvFormSnapshot = '';
  function wvFormSnapshotNow() {
    return JSON.stringify({ name: wvName, category: wvCategory, description: wvDescription, tags: wvTags });
  }
  function isWvFormDirty() {
    return showWvForm && wvFormSnapshotNow() !== wvFormSnapshot;
  }

  function openWvForm(item) {
    showWvForm = true;
    if (item) {
      $editingWvID = item.id;
      wvName = item.name || '';
      wvCategory = item.category || 'other';
      wvDescription = item.description || '';
      wvTags = item.tags || '';
    } else {
      $editingWvID = null;
      wvName = ''; wvCategory = 'other'; wvDescription = ''; wvTags = '';
    }
    wvFormSnapshot = wvFormSnapshotNow();
  }

  function requestNewWv() {
    if (isWvFormDirty()) {
      showConfirm($t('config.form.unsavedNew'), () => openWvForm(null));
      return;
    }
    openWvForm(null);
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

  function wvGenBtnProps() {
    return {
      class: 'btn btn-accent btn-xs',
      disabled: !!genBusy['worldview'],
      onclick: (e) => { e.stopPropagation(); generateWorldviewEntry(); },
    };
  }

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
    await sendToChat($t('config.wv.submitMsg', { n: allWvs.length, lines }));
    addToast($t('config.wv.submitted'), 'success');
  }

  // —— 组织 CRUD ——
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

  function charGenBtnProps() {
    return {
      class: 'btn btn-accent btn-xs',
      disabled: !!genBusy['characters'],
      onclick: (e) => { e.stopPropagation(); generateCharacterEntry(); },
    };
  }
  function orgGenBtnProps() {
    return {
      class: 'btn btn-accent btn-xs',
      disabled: !!genBusy['organizations'],
      onclick: (e) => { e.stopPropagation(); generateOrganizationEntry(); },
    };
  }
  function relGenBtnProps() {
    return {
      class: 'btn btn-accent btn-xs',
      disabled: !!genBusy['relations'],
      onclick: (e) => { e.stopPropagation(); generateRelationEntry(); },
    };
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

  // —— 关系 CRUD ——
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

<div class="space-y-3">
  <ConfigChangePanel />
  <!-- Config tab: API config only. Novel parameters tab: Story config at full width, Theme & Motif below it. -->
    {#if tab === 'api'}
    <div class="card bg-base-200">
      <div class="card-body p-4 gap-2">
        <h3 class="card-title text-base">{$t('config.api.title')}</h3>
        <div class="grid grid-cols-2 gap-x-3 gap-y-1.5">
          <div class="col-span-2">
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.api.baseUrl')}</span>
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
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.api.model')}</span>
            <input type="text" class="input input-sm w-full" bind:value={localApiCfg.model} placeholder="gpt-4" disabled={$taskRunning || testingApi} />
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.api.timeout')}</span>
            <input type="number" class="input input-sm w-full" bind:value={localApiCfg.http_timeout_seconds} disabled={$taskRunning || testingApi} />
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.api.maxTokens')}</span>
            <input type="number" class="input input-sm w-full" bind:value={localApiCfg.max_tokens} placeholder="{$t('config.api.maxTokens.placeholder')}" disabled={$taskRunning || testingApi} title={$t('config.api.maxTokens.tooltip')} />
          </div>
          <div class="col-span-2">
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.api.key')}</span>
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
    {/if}

    {#if tab === 'novelParams'}
    <div class="card bg-base-200">
      <div class="card-body p-4 gap-2">
        <h3 class="card-title text-base">{$t('config.story.paramsTitle')}</h3>
        {#if hasAccepted}
          <div class="alert alert-warning text-xs py-1.5 px-3">
            <span>{$t('config.story.acceptedHint')}</span>
          </div>
        {/if}
        <div class="grid grid-cols-2 gap-x-3 gap-y-1.5">
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.type')}</span>
            {#if selectedGenreKey === OTHER_GENRE}
              <select class="select select-sm w-full" bind:value={selectedGenreKey} on:change={onGenreChange} disabled={$taskRunning} title={$t('config.tip.genre')}>
                <option value="">{$t('config.story.auto')}</option>
                {#each GENRES as g}
                  <option value={g.key}>{g.label}</option>
                {/each}
                <option value={OTHER_GENRE}>{$t('config.story.other')}</option>
              </select>
              <input type="text" class="input input-sm w-full mt-1" bind:value={localStoryCfg.type} placeholder={$t('config.story.type.placeholder')} disabled={$taskRunning} />
            {:else}
              <select class="select select-sm w-full" bind:value={selectedGenreKey} on:change={onGenreChange} disabled={$taskRunning} title={$t('config.tip.genre')}>
                <option value="">{$t('config.story.auto')}</option>
                {#each GENRES as g}
                  <option value={g.key}>{g.label}</option>
                {/each}
                <option value={OTHER_GENRE}>{$t('config.story.other')}</option>
              </select>
            {/if}
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.subgenre')}</span>
            {#if subgenreOptions.length}
              <select class="select select-sm w-full" bind:value={localStoryCfg.subgenre} on:change={onSubgenreChange} disabled={$taskRunning} title={subgenreHint(localStoryCfg.subgenre) || $t('config.tip.subgenre')}>
                {#each subgenreOptions as s}
                  <option value={s}>{s}</option>
                {/each}
              </select>
            {:else}
              <input type="text" list="subgenre-presets" class="input input-sm w-full" bind:value={localStoryCfg.subgenre} placeholder={$t('config.story.subgenre.placeholder')} disabled={$taskRunning} title={subgenreHint(localStoryCfg.subgenre) || $t('config.tip.subgenre')} />
              <datalist id="subgenre-presets">
                {#each [...new Set([...SUBGENRE_PRESETS, ...Object.keys(SUBGENRE_DATA)])] as s}
                  <option value={s}>{subgenreHint(s)}</option>
                {/each}
              </datalist>
            {/if}
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.titleField')}</span>
            <input type="text" class="input input-sm w-full" bind:value={localStoryCfg.title} placeholder={$t('config.story.title.placeholder')} disabled={$taskRunning} />
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.author')}</span>
            <input type="text" class="input input-sm w-full" bind:value={localStoryCfg.author} placeholder={$t('config.story.author.placeholder')} disabled={$taskRunning} />
          </div>
          <div class="col-span-2">
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.tone')}</span>
            <input type="text" class="input input-sm w-full mb-1" bind:value={localStoryCfg.tone} placeholder={$t('config.story.tone.placeholder')} disabled={$taskRunning} />
            {#if toneOpts.length}
              <div class="flex flex-wrap gap-x-3 gap-y-1">
                {#each toneOpts as t}
                  <label class="flex items-center gap-1.5 text-xs cursor-pointer" >
                    <input type="checkbox" class="checkbox checkbox-xs" checked={toneChecked(t)} on:change={() => toggleTone(t)} disabled={$taskRunning} />
                    {t}
                  </label>
                {/each}
              </div>
            {/if}
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.length')}</span>
            <select class="select select-sm w-full" bind:value={localStoryCfg.story_length} on:change={onLengthChange} disabled={$taskRunning} title={$t('config.tip.length')}>
              <option value="">{$t('config.story.length.none')}</option>
              {#each LENGTH_KEYS as k}
                <option value={k}>{$t('config.story.length.' + k)}</option>
              {/each}
            </select>
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.structure')}</span>
            {#if structOpts.length}
              <select class="select select-sm w-full" value={structSelValue} on:change={onStructureChange} disabled={$taskRunning} title={structHint(localStoryCfg.structure) || $t('config.story.structure')}>
                <option value="">{$t('config.story.auto')}</option>
                {#each structOpts as k}
                  <option value={k} title={structHint(k)}>{$t('config.story.structure.' + k)}</option>
                {/each}
                <option value="__custom__">{$t('config.story.other')}</option>
              </select>
              {#if structSelValue === '__custom__'}
                <input type="text" class="input input-sm w-full mt-1" value={structureExtras(localStoryCfg.structure)[0] || ''} on:input={(e) => { const extras = structureExtras(localStoryCfg.structure).slice(1); localStoryCfg.structure = [e.target.value.trim(), ...extras].filter(Boolean).join(', '); }} placeholder={$t('config.story.structure.placeholder')} disabled={$taskRunning} />
              {/if}
            {:else}
              <input type="text" class="input input-sm w-full" bind:value={localStoryCfg.structure} placeholder={$t('config.story.structure.placeholder')} disabled={$taskRunning} />
            {/if}
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.targetWords')}</span>
            <input type="number" class="input input-sm w-full" bind:value={localStoryCfg.target_words_per_chapter} disabled={$taskRunning} />
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.conflict')}</span>
            <select class="select select-sm w-full" value={conflictIsOther ? 'other' : (localStoryCfg.conflict_scale || '')} on:change={onConflictSelect} disabled={$taskRunning} title={$t('config.tip.conflict')}>
              <option value="">{$t('config.story.auto')}</option>
              {#each conflictOpts as c}
                <option value={c}>{c}</option>
              {/each}
              <option value="other">{$t('config.story.other')}</option>
            </select>
            {#if conflictIsOther}
              <input type="text" class="input input-sm w-full mt-1" bind:value={localStoryCfg.conflict_scale} placeholder={$t('config.story.conflict.placeholder')} disabled={$taskRunning} />
            {/if}
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.protagonist')}</span>
            <select class="select select-sm w-full" value={protIsOther ? 'other' : (localStoryCfg.protagonist_type || '')} on:change={onProtSelect} disabled={$taskRunning} title={$t('config.tip.protagonist')}>
              <option value="">{$t('config.story.auto')}</option>
              {#each protagonistOpts as pt}
                <option value={pt}>{pt}</option>
              {/each}
              <option value="other">{$t('config.story.other')}</option>
            </select>
            {#if protIsOther}
              <input type="text" class="input input-sm w-full mt-1" bind:value={localStoryCfg.protagonist_type} placeholder={$t('config.story.protagonist.placeholder')} disabled={$taskRunning} />
            {/if}
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.targetAudience')}</span>
            <select class="select select-sm w-full" bind:value={localStoryCfg.target_audience} disabled={$taskRunning} title={$t('config.tip.targetAudience')}>
              <option value="">{$t('config.story.targetAudience.none')}</option>
              {#each ['kid', 'middle_grade', 'ya', 'new_adult', 'adult', 'all_ages'] as a}
                <option value={a}>{$t('config.story.targetAudience.' + a)}</option>
              {/each}
            </select>
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.romanceLevel')}</span>
            <select class="select select-sm w-full" bind:value={localStoryCfg.romance_level} disabled={$taskRunning} title={$t('config.tip.romanceLevel')}>
              <option value="">{$t('config.story.content.unset')}</option>
              {#each ['random', 'none', 'subplot', 'moderate', 'central'] as r}
                <option value={r}>{$t('config.story.romanceLevel.' + r)}</option>
              {/each}
            </select>
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.sexualContent')}</span>
            <select class="select select-sm w-full" bind:value={localStoryCfg.sexual_content} disabled={$taskRunning} title={$t('config.tip.sexualContent')}>
              <option value="">{$t('config.story.content.unset')}</option>
              {#each ['random', 'clean', 'fade_to_black', 'explicit'] as s}
                <option value={s}>{$t('config.story.sexualContent.' + s)}</option>
              {/each}
            </select>
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.goreLevel')}</span>
            <select class="select select-sm w-full" bind:value={localStoryCfg.gore_level} disabled={$taskRunning} title={$t('config.tip.goreLevel')}>
              <option value="">{$t('config.story.content.unset')}</option>
              {#each ['random', 'none', 'mid', 'explicit'] as g}
                <option value={g}>{$t('config.story.goreLevel.' + g)}</option>
              {/each}
            </select>
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.worldDarkness')}</span>
            <select class="select select-sm w-full" bind:value={localStoryCfg.world_darkness} disabled={$taskRunning} title={$t('config.tip.worldDarkness')}>
              <option value="">{$t('config.story.content.unset')}</option>
              {#each ['idyllic', 'temperate', 'gritty', 'grim', 'abyssal'] as d}
                <option value={d}>{$t('config.story.worldDarkness.' + d)}</option>
              {/each}
            </select>
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.genderBias')}</span>
            <select class="select select-sm w-full" bind:value={localStoryCfg.gender_bias} disabled={$taskRunning} title={$t('config.tip.genderBias')}>
              {#each ['random', 'balanced', 'male', 'female'] as g}
                <option value={g}>{$t('config.story.genderBias.' + g)}</option>
              {/each}
            </select>
          </div>
        </div>
        <div>
          <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.specificSettings')}</span>
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
          <textarea class="textarea textarea-sm w-full h-12 text-xs font-mono mt-1" bind:value={customSettingsText} on:blur={rebuildSpecificSettings} placeholder={$t('config.story.specificSettings.placeholder')} disabled={$taskRunning} title={$t('config.tip.specificSettings')}></textarea>
        </div>
        <label class="flex items-center gap-2 text-sm cursor-pointer" title={$t('config.tip.locationsEnabled')}>
          <input type="checkbox" class="toggle toggle-sm toggle-primary" bind:checked={localStoryCfg.locations_enabled} disabled={$taskRunning} />
          {$t('config.story.locationsEnabled')}
        </label>
        <div class="flex justify-end">
          <button class="btn btn-primary btn-xs" on:click={saveStoryConfig} disabled={$taskRunning}>{$t('common.save')}</button>
        </div>
      </div>
    </div>
    {/if}

    <!-- Novel parameters tab: Theme & Motif card, below the full-width Story config card -->
    {#if tab === 'novelParams'}
  <div class="card bg-base-200">
    <div class="card-body p-4 gap-2">
      <div class="flex justify-between items-center">
        <h3 class="card-title text-base">{$t('config.theme.title')}</h3>
        <div class="flex items-center gap-1.5">
          <button {...genBtnProps('motif')}>
            {#if genBusy['motif']}
              <span class="loading loading-spinner loading-xs"></span>{$t('config.generating')}
            {:else}✨ {$t('common.generate')}{/if}
          </button>
          <button {...undoBtnProps('motif')}>↩ {$t('common.undo')}</button>
        </div>
      </div>
      <div class="space-y-1.5">
        <div>
          <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.theme')}</span>
          <textarea class="textarea textarea-sm w-full h-16 text-sm" bind:value={localStoryCfg.theme} placeholder={$t('config.story.theme.placeholder')} disabled={$taskRunning} title={$t('config.theme.hint')}></textarea>
        </div>
        <div>
          <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.motif.title')}</span>
          <textarea class="textarea textarea-sm w-full h-16 text-sm" bind:value={localStoryCfg.motif} placeholder={$t('config.motif.placeholder')} disabled={$taskRunning}></textarea>
        </div>
      </div>
      <div class="divider my-0.5 py-0 h-px"></div>
      <div class="flex items-center justify-between gap-2 flex-wrap">
        <span class="text-xs text-base-content/65">{$t('config.audience.profileTitle')}</span>
        <div class="flex items-center gap-1.5">
          <button class="btn btn-accent btn-xs" disabled={!!genBusy['audience_profile']}
            onclick={(e) => { e.stopPropagation(); generateSection('audience_profile'); }}>
            {#if genBusy['audience_profile']}
              <span class="loading loading-spinner loading-xs"></span>{$t('config.generating')}
            {:else}✨ {$t('common.generate')}{/if}
          </button>
          <button {...undoBtnProps('audience_profile')}>↩ {$t('common.undo')}</button>
        </div>
      </div>
      <textarea class="textarea textarea-sm w-full h-16 text-xs" bind:value={localStoryCfg.audience_profile}
        placeholder={$t('config.audience.profilePlaceholder')} disabled={$taskRunning} title={$t('config.tip.audienceProfile')}></textarea>
      <div class="text-xs opacity-60">{$t('config.theme.hint')}</div>
      <div class="flex justify-end">
        <button class="btn btn-primary btn-xs" on:click={saveStoryConfig} disabled={$taskRunning}>{$t('common.save')}</button>
      </div>
    </div>
  </div>
    {/if}

    {#if tab === 'novelParams'}
  <!-- Writing Style & POV -->
  <div class="card bg-base-200">
    <div class="card-body p-4 gap-2">
      <div class="flex justify-between items-center">
        <h3 class="card-title text-base">{$t('config.style.title')}</h3>
      </div>
      <div>
        <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.style.label')}</span>
        <textarea class="textarea w-full h-28 text-base" bind:value={localStoryCfg.writing_style} placeholder={$t('config.style.placeholder')} disabled={$taskRunning}></textarea>
      </div>
      <div>
        <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.pov.label')}</span>
        <textarea class="textarea w-full h-20 text-base" bind:value={localStoryCfg.writing_pov} placeholder={$t('config.pov.placeholder')} disabled={$taskRunning}></textarea>
      </div>
      <div class="flex justify-end gap-1.5">
        <button {...genBtnProps('style')}>
          {#if genBusy['style']}<span class="loading loading-spinner loading-xs"></span>{$t('config.generating')}{:else}✨ {$t('common.generate')}{/if}
        </button>
        <button class="btn btn-primary btn-xs" on:click={saveStoryConfig} disabled={$taskRunning}>{$t('common.save')}</button>
      </div>
    </div>
  </div>

  <!-- Story idea (AI generation seed) -->
  <div class="card bg-base-200">
    <div class="card-body p-4 gap-2">
      <div class="flex justify-between items-center">
        <h3 class="card-title text-base">{$t('config.story_idea.title')}</h3>
      </div>
      <textarea class="textarea w-full h-32 text-base" bind:value={localStoryCfg.story_idea} placeholder={$t('config.story_idea.placeholder')} disabled={$taskRunning}></textarea>
      <div class="text-xs opacity-60">{$t('config.story_idea.hint')}</div>
      <div class="flex justify-end gap-1.5">
        <button {...genBtnProps('story_idea')}>
          {#if genBusy['story_idea']}
            <span class="loading loading-spinner loading-xs"></span>{$t('config.generating')}
          {:else}✨ {$t('common.generate')}{/if}
        </button>
        <button class="btn btn-primary btn-xs" on:click={saveStoryConfig} disabled={$taskRunning}>{$t('common.save')}</button>
      </div>
    </div>
  </div>

    {/if}

    {#if tab === 'lore'}
  <!-- Characters -->
  <div class="card bg-base-200">
    <div class="card-body p-4 gap-2">
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div class="flex justify-between items-center">
        <div class="flex items-center gap-2">
          <h3 class="card-title text-base">{$t('config.char.title')} <span class="text-xs font-normal text-base-content/65">({chars.length})</span></h3>
          <button {...genBtnProps('characters')}>
            {#if genBusy['characters']}<span class="loading loading-spinner loading-xs"></span>{$t('config.generating')}{:else}✨ {$t('common.generate')}{/if}
          </button>
        </div>
      </div>
      
        <div class="grid grid-cols-[repeat(auto-fill,minmax(220px,1fr))] gap-2">
          {#if chars.length === 0}
            <p class="text-xs text-base-content/65 col-span-full py-2">{$t('config.char.empty')}</p>
          {:else}
            {#each chars as c}
              <div class="flex items-start gap-2.5 bg-base-300 rounded-lg p-2.5 group">
                <div class="w-8 h-8 rounded-full bg-primary/20 text-primary flex items-center justify-center text-xs font-bold shrink-0">{stripNameMarks(c.name)[0]}</div>
                <div class="flex-1 min-w-0">
                  <div class="text-sm font-medium truncate">{stripNameMarks(c.name)}</div>
                  <div class="text-xs text-base-content/65 line-clamp-1">{c.personality || c.background || c.age || ''}</div>
                </div>
                <div class="flex gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity shrink-0">
                  <button class="btn btn-outline btn-xs px-1" on:click={() => openCharForm(c)} disabled={$taskRunning}>{$t('common.edit')}</button>
                  <button class="btn btn-error btn-outline btn-xs px-1" on:click={() => deleteCharacter(c.id)} disabled={$taskRunning}>{$t('common.delete')}</button>
                </div>
              </div>
            {/each}
          {/if}
        </div>

        {#if showCharForm}
          <div class="bg-base-300 rounded-lg p-3 space-y-2 mt-1">
            <div class="grid grid-cols-2 gap-x-3 gap-y-1.5">
              <div>
                <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.char.name')}</span>
                <input type="text" class="input input-sm w-full" bind:value={charName} disabled={$taskRunning} />
              </div>
              <div>
                <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.char.age')}</span>
                <input type="text" class="input input-sm w-full" bind:value={charAge} disabled={$taskRunning} />
              </div>
            </div>
            <div class="grid grid-cols-2 gap-x-3 gap-y-1.5">
              <div>
                <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.char.appearance')}</span>
                <textarea class="textarea textarea-sm w-full h-14 text-sm" bind:value={charAppearance} disabled={$taskRunning}></textarea>
              </div>
              <div>
                <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.char.personality')}</span>
                <textarea class="textarea textarea-sm w-full h-14 text-sm" bind:value={charPersonality} disabled={$taskRunning}></textarea>
              </div>
              <div>
                <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.char.background')}</span>
                <textarea class="textarea textarea-sm w-full h-14 text-sm" bind:value={charBackground} disabled={$taskRunning}></textarea>
              </div>
              <div>
                <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.char.motivation')}</span>
                <textarea class="textarea textarea-sm w-full h-14 text-sm" bind:value={charMotivation} disabled={$taskRunning}></textarea>
              </div>
              <div>
                <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.char.abilities')}</span>
                <textarea class="textarea textarea-sm w-full h-14 text-sm" bind:value={charAbilities} disabled={$taskRunning}></textarea>
              </div>
              <div>
                <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.char.notes')}</span>
                <textarea class="textarea textarea-sm w-full h-14 text-sm" bind:value={charNotes} disabled={$taskRunning}></textarea>
              </div>
            </div>
            <div class="flex gap-1.5">
              <button class="btn btn-success btn-xs" on:click={saveCharacter} disabled={$taskRunning}>{$t('config.char.save')}</button>
              <button {...charGenBtnProps()}>
                {#if genBusy['characters']}<span class="loading loading-spinner loading-xs"></span>{$t('config.generating')}{:else}✨ {$t('common.generate')}{/if}
              </button>
              <button {...undoBtnProps('charform')}>↩ {$t('common.undo')}</button>
              <button class="btn btn-ghost btn-xs" on:click={closeCharForm}>{$t('common.cancel')}</button>
            </div>
          </div>
        {/if}

        <div class="flex gap-1.5">
          <button class="btn btn-primary btn-xs" on:click={requestNewChar} disabled={$taskRunning}>{$t('config.char.create')}</button>
          {#if chars.length > 0}
            <button class="btn btn-accent btn-xs" on:click={submitCharacters} disabled={$taskRunning}>{$t('config.char.submit')}</button>
          {/if}
        </div>
    </div>
  </div>

  <!-- Worldview -->
  <div class="card bg-base-200">
    <div class="card-body p-4 gap-2">
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div class="flex justify-between items-center">
        <h3 class="card-title text-base">{$t('config.wv.title')} <span class="text-xs font-normal text-base-content/65">({filteredWvs.length})</span></h3>
        <div class="flex items-center gap-2">
          {#if localStoryCfg.locations_enabled}
            <button {...genBtnProps('locations')} onclick={(e) => { e.stopPropagation(); generateSection('locations'); }}>
              {#if genBusy['locations']}<span class="loading loading-spinner loading-xs"></span>{$t('config.generating')}{:else}✨ {$t('common.generate')}{/if}
            </button>
          {/if}
        </div>
      </div>
      
        <div class="tabs tabs-box tabs-xs bg-base-300 w-fit">
          {#each wvTabs as [cat, label]}
            <button class="tab tab-xs {$wvFilter === cat ? 'tab-active' : ''}" on:click={() => wvFilter.set(cat)}>
              {label}
            </button>
          {/each}
        </div>

        <div class="grid grid-cols-[repeat(auto-fill,minmax(220px,1fr))] gap-2">
          {#if filteredWvs.length === 0}
            <p class="text-xs text-base-content/65 col-span-full py-2">{$t('config.wv.empty')}</p>
          {:else}
            {#each filteredWvs as w}
              <div class="flex items-start gap-2.5 bg-base-300 rounded-lg p-2.5 group">
                <div class="w-8 h-8 rounded-lg bg-accent/20 text-accent flex items-center justify-center text-xs font-bold shrink-0">{w.name[0]}</div>
                <div class="flex-1 min-w-0">
                  <div class="text-sm font-medium truncate">{w.name} <span class="text-xs font-normal text-base-content/30">[{catLabels[w.category] || w.category}]</span></div>
                  <div class="text-xs text-base-content/65 line-clamp-1">{w.description}</div>
                </div>
                <div class="flex gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity shrink-0">
                  <button class="btn btn-outline btn-xs px-1" on:click={() => openWvForm(w)} disabled={$taskRunning}>{$t('common.edit')}</button>
                  <button class="btn btn-error btn-outline btn-xs px-1" on:click={() => deleteWorldview(w.id)} disabled={$taskRunning}>{$t('common.delete')}</button>
                </div>
              </div>
            {/each}
          {/if}
        </div>

        {#if showWvForm}
          <div class="bg-base-300 rounded-lg p-3 space-y-2 mt-1">
            <div class="grid grid-cols-2 gap-x-3 gap-y-1.5">
              <div>
                <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.wv.name')}</span>
                <input type="text" class="input input-sm w-full" bind:value={wvName} disabled={$taskRunning} />
              </div>
              <div>
                <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.wv.category')}</span>
                <select class="select select-sm w-full" bind:value={wvCategory} disabled={$taskRunning}>
                  <option value="geography">{$t('config.wv.cat.geography')}</option>
                  <option value="location">{$t('config.wv.cat.location')}</option>
                  <option value="faction">{$t('config.wv.cat.faction')}</option>
                  <option value="rule">{$t('config.wv.cat.rule')}</option>
                  <option value="knowledge">{$t('config.wv.cat.knowledge')}</option>
                  <option value="history">{$t('config.wv.cat.history')}</option>
                  <option value="other">{$t('config.wv.cat.other')}</option>
                </select>
              </div>
            </div>
            <div>
              <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.wv.description')}</span>
              <textarea class="textarea textarea-sm w-full h-16 text-sm" bind:value={wvDescription} disabled={$taskRunning}></textarea>
            </div>
            <div>
              <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.wv.tags')}</span>
              <input type="text" class="input input-sm w-full" bind:value={wvTags} placeholder={$t('config.wv.tags.placeholder')} disabled={$taskRunning} />
            </div>
            <div class="flex gap-1.5">
              <button class="btn btn-success btn-xs" on:click={saveWorldview} disabled={$taskRunning}>{$t('common.save')}</button>
              <button {...wvGenBtnProps()}>✨ {$t('config.generate.worldview')}</button>
              <button {...undoBtnProps('wvform')}>↩ {$t('common.undo')}</button>
              <button class="btn btn-ghost btn-xs" on:click={closeWvForm}>{$t('common.cancel')}</button>
            </div>
          </div>
        {/if}

        <div class="flex gap-1.5">
          <button class="btn btn-primary btn-xs" on:click={requestNewWv} disabled={$taskRunning}>{$t('config.wv.create')}</button>
          {#if allWvs.length > 0}
            <button class="btn btn-accent btn-xs" on:click={submitWorldview} disabled={$taskRunning}>{$t('config.wv.submit')}</button>
          {/if}
        </div>
    </div>
  </div>

  <!-- Organizations -->
  <div class="card bg-base-200">
    <div class="card-body p-4 gap-2">
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div class="flex justify-between items-center">
        <div class="flex items-center gap-2">
          <h3 class="card-title text-base">{$t('config.org.title')} <span class="text-xs font-normal text-base-content/65">({orgs.length})</span></h3>
          <button {...genBtnProps('organizations')}>
            {#if genBusy['organizations']}<span class="loading loading-spinner loading-xs"></span>{$t('config.generating')}{:else}✨ {$t('common.generate')}{/if}
          </button>
        </div>
      </div>
      
        <div class="grid grid-cols-[repeat(auto-fill,minmax(220px,1fr))] gap-2">
          {#if orgs.length === 0}
            <p class="text-xs text-base-content/65 col-span-full py-2">{$t('config.org.empty')}</p>
          {:else}
            {#each orgs as o}
              <div class="flex items-start gap-2.5 bg-base-300 rounded-lg p-2.5 group">
                <div class="w-8 h-8 rounded-lg bg-warning/20 text-warning flex items-center justify-center text-xs font-bold shrink-0">{o.name[0]}</div>
                <div class="flex-1 min-w-0">
                  <div class="text-sm font-medium truncate">{o.name} {#if o.type}<span class="text-xs font-normal text-base-content/30">[{o.type}]</span>{/if}</div>
                  <div class="text-xs text-base-content/65 line-clamp-1">{o.description || ''}</div>
                  {#if (o.members || []).length > 0}
                    <div class="text-xs text-base-content/65 line-clamp-1 mt-0.5">{$t('config.org.membersList', { names: (o.members || []).map(id => nameById[id] || id).join(', ') })}</div>
                  {/if}
                </div>
                <div class="flex gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity shrink-0">
                  <button class="btn btn-outline btn-xs px-1" on:click={() => openOrgForm(o)} disabled={$taskRunning}>{$t('common.edit')}</button>
                  <button class="btn btn-error btn-outline btn-xs px-1" on:click={() => deleteOrganization(o.id)} disabled={$taskRunning}>{$t('common.delete')}</button>
                </div>
              </div>
            {/each}
          {/if}
        </div>

        {#if showOrgForm}
          <div class="bg-base-300 rounded-lg p-3 space-y-2 mt-1">
            <div class="grid grid-cols-2 gap-x-3 gap-y-1.5">
              <div>
                <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.org.name')}</span>
                <input type="text" class="input input-sm w-full" bind:value={orgName} disabled={$taskRunning} />
              </div>
              <div>
                <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.org.type')}</span>
                <input type="text" class="input input-sm w-full" bind:value={orgType} placeholder={$t('config.org.type.placeholder')} disabled={$taskRunning} />
              </div>
            </div>
            <div>
              <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.org.description')}</span>
              <textarea class="textarea textarea-sm w-full h-16 text-sm" bind:value={orgDescription} disabled={$taskRunning}></textarea>
            </div>
            {#if chars.length > 0}
              <div>
                <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.org.members')}</span>
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
            <div class="flex gap-1.5">
              <button class="btn btn-success btn-xs" on:click={saveOrganization} disabled={$taskRunning}>{$t('config.org.save')}</button>
              <button {...orgGenBtnProps()}>
                {#if genBusy['organizations']}<span class="loading loading-spinner loading-xs"></span>{$t('config.generating')}{:else}✨ {$t('common.generate')}{/if}
              </button>
              <button {...undoBtnProps('orgform')}>↩ {$t('common.undo')}</button>
              <button class="btn btn-ghost btn-xs" on:click={closeOrgForm}>{$t('common.cancel')}</button>
            </div>
          </div>
        {/if}

        <div class="flex gap-1.5">
          <button class="btn btn-primary btn-xs" on:click={requestNewOrg} disabled={$taskRunning}>{$t('config.org.create')}</button>
        </div>
    </div>
  </div>

  <!-- Relations -->
  <div class="card bg-base-200">
    <div class="card-body p-4 gap-2">
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div class="flex justify-between items-center">
        <div class="flex items-center gap-2">
          <h3 class="card-title text-base">{$t('config.rel.title')} <span class="text-xs font-normal text-base-content/65">({rels.length})</span></h3>
          <button {...genBtnProps('relations')}>
            {#if genBusy['relations']}<span class="loading loading-spinner loading-xs"></span>{$t('config.generating')}{:else}✨ {$t('common.generate')}{/if}
          </button>
        </div>
      </div>
      
        <div class="grid grid-cols-[repeat(auto-fill,minmax(260px,1fr))] gap-2">
          {#if rels.length === 0}
            <p class="text-xs text-base-content/65 col-span-full py-2">{$t('config.rel.empty')}</p>
          {:else}
            {#each rels as r}
              <div class="flex items-center gap-2 bg-base-300 rounded-lg p-2.5 group">
                <div class="flex-1 min-w-0 text-sm">
                  <div class="flex items-center gap-1.5 flex-wrap">
                    <span class="font-medium">{entityIcons[r.source_type] || ''} {nameById[r.source_id] || r.source_id}</span>
                    <span class="text-base-content/65">→</span>
                    <span class="font-medium">{entityIcons[r.target_type] || ''} {nameById[r.target_id] || r.target_id}</span>
                  </div>
                  <p class="mt-1 text-xs leading-relaxed text-secondary break-words">{r.label}</p>
                </div>
                <div class="flex gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity shrink-0">
                  <button class="btn btn-outline btn-xs px-1" on:click={() => openRelForm(r)} disabled={$taskRunning}>{$t('common.edit')}</button>
                  <button class="btn btn-error btn-outline btn-xs px-1" on:click={() => deleteRelation(r.id)} disabled={$taskRunning}>{$t('common.delete')}</button>
                </div>
              </div>
            {/each}
          {/if}
        </div>

        {#if showRelForm}
          <div class="bg-base-300 rounded-lg p-3 space-y-2 mt-1">
            <div class="grid grid-cols-[1fr_auto_1fr] gap-2 items-end">
              <div>
                <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.rel.source')}</span>
                <select class="select select-sm w-full" bind:value={relSource} disabled={$taskRunning}>
                  <option value="" disabled>{$t('config.rel.entityHint')}</option>
                  {#each entityOptions as opt}
                    <option value={opt.key}>{opt.label}</option>
                  {/each}
                </select>
              </div>
              <span class="text-base-content/65 pb-1.5">→</span>
              <div>
                <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.rel.target')}</span>
                <select class="select select-sm w-full" bind:value={relTarget} disabled={$taskRunning}>
                  <option value="" disabled>{$t('config.rel.entityHint')}</option>
                  {#each entityOptions as opt}
                    <option value={opt.key}>{opt.label}</option>
                  {/each}
                </select>
              </div>
            </div>
            <div>
              <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.rel.label')}</span>
              <input type="text" class="input input-sm w-full" bind:value={relLabel} placeholder={$t('config.rel.label.placeholder')} disabled={$taskRunning} />
            </div>
            <div class="flex gap-1.5">
              <button class="btn btn-success btn-xs" on:click={saveRelation} disabled={$taskRunning}>{$t('config.rel.save')}</button>
              <button {...relGenBtnProps()}>
                {#if genBusy['relations']}<span class="loading loading-spinner loading-xs"></span>{$t('config.generating')}{:else}✨ {$t('common.generate')}{/if}
              </button>
              <button {...undoBtnProps('relform')}>↩ {$t('common.undo')}</button>
              <button class="btn btn-ghost btn-xs" on:click={closeRelForm}>{$t('common.cancel')}</button>
            </div>
          </div>
        {/if}

        <div class="flex gap-1.5">
          <button class="btn btn-primary btn-xs" on:click={requestNewRel} disabled={$taskRunning || entityOptions.length < 2}>{$t('config.rel.create')}</button>
          {#if entityOptions.length < 2}
            <span class="text-xs text-base-content/65 self-center">{$t('config.rel.needTwo')}</span>
          {/if}
        </div>
    </div>
  </div>
    {/if}
</div>
