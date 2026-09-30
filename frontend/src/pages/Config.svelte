<script>
  import { onMount } from 'svelte';
  import { api } from '../lib/api.js';
  import { apiConfig, config, progress, settings, editingCharID, editingWvID, wvFilter, addToast, showConfirm, taskRunning, apiTestResult } from '../lib/stores.js';
  import { t } from '../lib/i18n/index.js';
  import { resolveChatCompletionsURL } from '../lib/apiUrl.js';
  import ConfigChangePanel from '../components/ConfigChangePanel.svelte';

  export let sendToChat = async () => {};

  function stripNameMarks(name) {
    return (name.startsWith('「') && name.endsWith('」')) ? name.slice(1, -1) : name;
  }

  let showCharForm = false;
  let showWvForm = false;
  let charCollapse = false;
  let wvCollapse = false;

  let charName = '', charAge = '', charAppearance = '', charPersonality = '', charBackground = '', charMotivation = '', charAbilities = '', charNotes = '';
  let wvName = '', wvCategory = 'other', wvDescription = '', wvTags = '';

  // 组织管理
  let showOrgForm = false, orgCollapse = false;
  let orgName = '', orgType = '', orgDescription = '';
  let orgMembers = [];
  let editingOrgID = null;

  // 关系管理
  let showRelForm = false, relCollapse = false;
  let relSource = '', relTarget = '', relLabel = '';
  let editingRelID = null;

  $: cfgBase = $apiConfig?.base_url || '';
  $: cfgModel = $apiConfig?.model || '';
  $: cfgKey = $apiConfig?.api_key || '';
  $: cfgTimeout = $apiConfig?.http_timeout_seconds || 600;

  let localApiCfg = { base_url: '', url_strict: false, model: '', api_key: '', http_timeout_seconds: 600, max_tokens: 32768, context_budget_tokens: 900000 };
  let localStoryCfg = { type: '', parent_genre: '', title: '', subgenre: '', theme: '', tone: '', author: '', story_length: '', structure: '', motif: '', brief: '', conflict_scale: '', conflict_other: '', specific_settings: '', protagonist_type: '', protagonist_other: '', gender_bias: 'random', target_audience: '', locations_enabled: false, target_words_per_chapter: 2500, writing_style: '', writing_pov: '' };
  // Merged Theme+Motif UI field (combined_theme). Derived from theme/motif while
  // the server snapshot hasn't been echoed back yet; written through on edit.
  let combinedTheme = '';
  let combinedThemeDirty = false;
  $: combinedThemeShown = combinedThemeDirty ? combinedTheme : [localStoryCfg.theme, localStoryCfg.motif].map(x => (x || '').trim()).filter(Boolean).join(' / ');
  function onCombinedThemeInput(e) {
    combinedTheme = e.target.value;
    combinedThemeDirty = true;
    const i = combinedTheme.indexOf(' / ');
    if (i >= 0) {
      localStoryCfg.theme = combinedTheme.slice(0, i);
      localStoryCfg.motif = combinedTheme.slice(i + 3);
    } else {
      localStoryCfg.theme = combinedTheme;
      localStoryCfg.motif = '';
    }
  }

// Novel-parameter data fetched from /api/novel-params (hints + parent-genre option maps).
let SUBGENRE_HINTS = {};
let STRUCTURE_HINTS = {};
let PARENT_GENRES = [];            // canonical parent genre keys (backend ParentGenreKeys)
let SUBGENRES_BY_GENRE = {};       // parent key -> subgenre option list
let SETTINGS_BY_GENRE = {};        // parent key -> specific-settings checkbox options
let TONES_BY_GENRE = {};           // parent key -> tone suggestions (editable combobox)
let CONFLICTS_BY_GENRE = {};       // parent key -> conflict-scale suggestions
let PROTAGONISTS_BY_GENRE = {};    // parent key -> protagonist-type suggestions
let AUDIENCE_KEYS = ['kid', 'middle_grade', 'ya', 'new_adult', 'adult', 'all_ages'];
let novelParamsTick = 0;
function subgenreHint(s) { return SUBGENRE_HINTS[(s || '').toLowerCase()] || ''; }
function structHint(k) { return STRUCTURE_HINTS[k] || ''; }

// Novel parameters: fallback genre presets (mirror of backend config.Genre* maps).
// /api/novel-params overrides these at runtime; "other" is always available in the UI.
const GENRE_CONFLICTS = {
fantasy: ['Personal Quest', 'Kingdom-wide', 'World-saving', 'Good vs Evil', 'Political Intrigue'],
scifi: ['Personal', 'Planetary', 'Interstellar', 'Galactic'],
mystery: ['Personal Mystery', 'Community Secret', 'Local Crime', 'Family Mystery', 'Historical Puzzle'],
romance: ['Personal Growth', 'Relationship Obstacles', 'Career vs Love', 'Family Issues', 'Past Trauma'],
thriller: ['International Conspiracy', 'Government Secrets', 'Spy Networks', 'National Security', 'Global Politics'],
horror: ['Personal Haunting', 'Family Curse', 'Supernatural Threat', 'Psychological Terror', 'Ancient Evil'],
historical: ['Tribal Warfare', 'Religious Conflicts', 'Ancient Politics', 'Survival Struggles', 'Civilization Building'],
western: ['Personal Vendetta', 'Town Protection', 'Range War', 'Law vs Lawlessness', 'Civilization vs Wilderness'],
adventure: ['Personal Survival', 'Group Expedition', 'Region-wide', 'Man vs Nature'],
literary: ['Inner Conflict', 'Family Dynamics', 'Community & Society', 'Generational Change'],
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
adventure: ['Seasoned Explorer', 'Reluctant Survivor', 'Optimistic Amateur', 'Grizzled Guide'],
literary: ['Everyman/Everywoman', 'Unreliable Narrator', 'Returning Outsider', 'Observer of Family'],
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
// Fallback parent-genre mapping (mirrors backend MatchParentGenreKey); used only
// to preselect the dropdown for legacy configs before /api/novel-params arrives.
const PARENT_FALLBACK = {
fantasy: 'fantasy', wuxia: 'fantasy', xianxia: 'fantasy', romantasy: 'fantasy', urban: 'fantasy', cozy: 'mystery',
scifi: 'scifi', cyberpunk: 'scifi', solarpunk: 'scifi', space_opera: 'scifi', dystopian: 'scifi', postapo: 'scifi',
steampunk: 'scifi', time_travel: 'scifi', cli_fi: 'scifi', afrofuturism: 'scifi', hard_scifi: 'scifi', superhero: 'scifi', litrpg: 'scifi',
mystery: 'mystery', mystery_police: 'mystery', romance: 'romance', thriller: 'thriller', heist: 'thriller',
horror: 'horror', gothic: 'horror', dark_academia: 'horror', historical: 'historical', western: 'western',
adventure: 'adventure', swashbuckler: 'adventure', nautical: 'adventure', picaresque: 'adventure', military: 'adventure',
ya: 'literary', middle_grade: 'literary', graphic_novel: 'literary',
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
// Effective parent-genre key driving every dependent option list: an explicit
// select choice wins; otherwise derive it from the legacy free-text Type field
// (exact canonical key first, then keyword mapping, mirroring the backend).
let genreKeyDerived = '';
$: genreKeyDerived = (() => {
novelParamsTick; // re-run when /api/novel-params data lands
const pg = (localStoryCfg.parent_genre || '').trim();
if (pg && pg !== 'other') return pg;
const t = (localStoryCfg.type || '').trim().toLowerCase();
if (!t) return '';
if (PARENT_GENRES.includes(t)) return t;
const k = matchGenreKey(t);
return k ? (PARENT_FALLBACK[k] || '') : '';
})();
// Option lists for the combobox/checkbox controls (parent-genre specific).
$: subgenreOpts = (novelParamsTick, genreKeyDerived ? (SUBGENRES_BY_GENRE[genreKeyDerived] || []) : []);
$: conflictOpts = (novelParamsTick, genreKeyDerived ? (CONFLICTS_BY_GENRE[genreKeyDerived] || GENRE_CONFLICTS[genreKeyDerived] || []) : []);
$: protagonistOpts = (novelParamsTick, genreKeyDerived ? (PROTAGONISTS_BY_GENRE[genreKeyDerived] || GENRE_PROTAGONISTS[genreKeyDerived] || []) : []);
$: toneOpts = (novelParamsTick, genreKeyDerived ? (TONES_BY_GENRE[genreKeyDerived] || []) : []);
$: settingOptions = (novelParamsTick, genreKeyDerived ? (SETTINGS_BY_GENRE[genreKeyDerived] || GENRE_SETTINGS[genreKeyDerived] || []) : []);
// Specific settings are stored as one entry per line; expose them as a set for checkboxes.
$: settingLines = (localStoryCfg.specific_settings || '').split('\n').map(x => x.trim()).filter(Boolean);
$: settingSet = new Set(settingLines);
function toggleSetting(s, checked) {
let cur = settingLines.slice();
if (checked) { if (!settingSet.has(s)) cur.push(s); }
else { cur = cur.filter(x => x !== s); }
localStoryCfg.specific_settings = cur.join('\n');
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
if (Array.isArray(p.parent_genres)) PARENT_GENRES = p.parent_genres;
if (p.subgenres_by_genre) SUBGENRES_BY_GENRE = p.subgenres_by_genre;
if (p.setting_options_by_genre) SETTINGS_BY_GENRE = p.setting_options_by_genre;
if (p.tone_options_by_genre) TONES_BY_GENRE = p.tone_options_by_genre;
if (p.conflicts_by_parent_genre) CONFLICTS_BY_GENRE = p.conflicts_by_parent_genre;
if (p.protagonists_by_parent_genre) PROTAGONISTS_BY_GENRE = p.protagonists_by_parent_genre;
if (Array.isArray(p.audience_keys) && p.audience_keys.length) AUDIENCE_KEYS = p.audience_keys;
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

  // AI section generation (based on the story brief)
  let genBusy = {};
  async function pollGenDone(section) {
    try {
      for (;;) {
        await new Promise(r => setTimeout(r, 1500));
        const st = await api('GET', '/api/status').catch(() => null);
        if (!st?.is_task_running) break;
      }
      addToast($t('config.generate.done'), 'success');
    } finally {
      genBusy = { ...genBusy, [section]: false };
      try { config.set(await api('GET', '/api/config')); } catch (e) {}
      try { settings.set(await api('GET', '/api/settings')); } catch (e) {}
    }
  }
  async function generateSection(section) {
    if (genBusy[section] || $taskRunning) return;
    const brief = (localStoryCfg.brief || '').trim() || ($config?.story?.brief || '').trim();
    if (!brief && section !== 'motif' && section !== 'brief') { addToast($t('config.brief.required'), 'error'); return; }
    try {
      await api('POST', '/api/generate/' + section, { brief });
      genBusy = { ...genBusy, [section]: true };
      taskRunning.set(true);
      addToast($t('config.generate.started'), 'info');
      pollGenDone(section);
    } catch (e) {
      addToast(e.message, 'error');
    }
  }
  function genBtnProps(section) {
    return {
      class: 'btn btn-accent btn-xs',
      disabled: $taskRunning || !!genBusy[section],
      onclick: (e) => { e.stopPropagation(); generateSection(section); },
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
      if ($editingCharID) {
        await api('PUT', '/api/characters/' + $editingCharID, data);
      } else {
        await api('POST', '/api/characters', data);
      }
      addToast($t('config.char.saved'), 'success');
      closeCharForm();
      settings.set(await api('GET', '/api/settings'));
    } catch (e) { addToast(e.message, 'error'); }
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

  async function saveWorldview() {
    if (!wvName.trim() || !wvDescription.trim()) { addToast($t('config.wv.requiredFields'), 'error'); return; }
    const data = { name: wvName.trim(), category: wvCategory, description: wvDescription.trim(), tags: wvTags };
    try {
      if ($editingWvID) {
        await api('PUT', '/api/worldview/' + $editingWvID, data);
      } else {
        await api('POST', '/api/worldview', data);
      }
      addToast($t('config.wv.saved'), 'success');
      closeWvForm();
      settings.set(await api('GET', '/api/settings'));
    } catch (e) { addToast(e.message, 'error'); }
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
      if (editingOrgID) {
        await api('PUT', '/api/organizations/' + editingOrgID, data);
      } else {
        await api('POST', '/api/organizations', data);
      }
      addToast($t('config.org.saved'), 'success');
      closeOrgForm();
      settings.set(await api('GET', '/api/settings'));
    } catch (e) { addToast(e.message, 'error'); }
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
      if (editingRelID) {
        await api('PUT', '/api/relations/' + editingRelID, data);
      } else {
        await api('POST', '/api/relations', data);
      }
      addToast($t('config.rel.saved'), 'success');
      closeRelForm();
      settings.set(await api('GET', '/api/settings'));
    } catch (e) { addToast(e.message, 'error'); }
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
  <!-- API + Story Config: side by side -->
  <div class="grid grid-cols-1 @3xl:grid-cols-2 gap-4">
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

    <div class="card bg-base-200">
      <div class="card-body p-4 gap-2">
        <h3 class="card-title text-base">{$t('config.story.title')}</h3>
        {#if hasAccepted}
          <div class="alert alert-warning text-xs py-1.5 px-3">
            <span>{$t('config.story.acceptedHint')}</span>
          </div>
        {/if}
        <div class="grid grid-cols-2 gap-x-3 gap-y-1.5">
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.type')}</span>
            <input type="text" class="input input-sm w-full" bind:value={localStoryCfg.type} placeholder={$t('config.story.type.placeholder')} disabled={$taskRunning} />
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.subgenre')}</span>
            <input type="text" list="subgenre-presets" class="input input-sm w-full" bind:value={localStoryCfg.subgenre} placeholder={$t('config.story.subgenre.placeholder')} disabled={$taskRunning} title={subgenreHint(localStoryCfg.subgenre) || $t('config.tip.subgenre')} />
            <datalist id="subgenre-presets">
              {#each SUBGENRE_PRESETS as s}
                <option value={s}>{subgenreHint(s)}</option>
              {/each}
            </datalist>
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.titleField')}</span>
            <input type="text" class="input input-sm w-full" bind:value={localStoryCfg.title} placeholder={$t('config.story.title.placeholder')} disabled={$taskRunning} />
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.author')}</span>
            <input type="text" class="input input-sm w-full" bind:value={localStoryCfg.author} placeholder={$t('config.story.author.placeholder')} disabled={$taskRunning} />
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.theme')}</span>
            <input type="text" class="input input-sm w-full" bind:value={localStoryCfg.theme} placeholder={$t('config.story.theme.placeholder')} disabled={$taskRunning} />
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.tone')}</span>
            <input type="text" class="input input-sm w-full" bind:value={localStoryCfg.tone} placeholder={$t('config.story.tone.placeholder')} disabled={$taskRunning} />
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
              <select class="select select-sm w-full" bind:value={localStoryCfg.structure} disabled={$taskRunning} title={structHint(localStoryCfg.structure) || $t('config.story.structure')}>
                {#each structOpts as k}
                  <option value={k} title={structHint(k)}>{$t('config.story.structure.' + k)}</option>
                {/each}
              </select>
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
            {#if conflictOpts.length}
              <select class="select select-sm w-full" bind:value={localStoryCfg.conflict_scale} disabled={$taskRunning} title={$t('config.tip.conflict')}>
                <option value="">{$t('config.story.auto')}</option>
                {#each conflictOpts as c}
                  <option value={c}>{c}</option>
                {/each}
                <option value="other">{$t('config.story.other')}</option>
              </select>
              {#if localStoryCfg.conflict_scale === 'other'}
                <input type="text" class="input input-sm w-full mt-1" bind:value={localStoryCfg.conflict_other} placeholder={$t('config.story.conflict.placeholder')} disabled={$taskRunning} />
              {/if}
            {:else}
              <input type="text" class="input input-sm w-full" bind:value={localStoryCfg.conflict_other} placeholder={$t('config.story.conflict.placeholder')} disabled={$taskRunning} title={$t('config.tip.conflict')} />
            {/if}
          </div>
          <div>
            <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.story.protagonist')}</span>
            {#if protagonistOpts.length}
              <select class="select select-sm w-full" bind:value={localStoryCfg.protagonist_type} disabled={$taskRunning} title={$t('config.tip.protagonist')}>
                <option value="">{$t('config.story.auto')}</option>
                {#each protagonistOpts as pt}
                  <option value={pt}>{pt}</option>
                {/each}
                <option value="other">{$t('config.story.other')}</option>
              </select>
              {#if localStoryCfg.protagonist_type === 'other'}
                <input type="text" class="input input-sm w-full mt-1" bind:value={localStoryCfg.protagonist_other} placeholder={$t('config.story.protagonist.placeholder')} disabled={$taskRunning} />
              {/if}
            {:else}
              <input type="text" class="input input-sm w-full" bind:value={localStoryCfg.protagonist_other} placeholder={$t('config.story.protagonist.placeholder')} disabled={$taskRunning} title={$t('config.tip.protagonist')} />
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
            <div class="flex flex-wrap gap-1 mb-1">
              {#each settingSuggestions as s}
                <button type="button" class="badge badge-outline badge-sm cursor-pointer hover:badge-primary" on:click={() => addSetting(s)} disabled={$taskRunning} title={$t('config.tip.specificSettings')}>+ {s}</button>
              {/each}
            </div>
          {/if}
          <textarea class="textarea textarea-sm w-full h-16 text-xs font-mono" bind:value={localStoryCfg.specific_settings} placeholder={$t('config.story.specificSettings.placeholder')} disabled={$taskRunning} title={$t('config.tip.specificSettings')}></textarea>
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
  </div>

  <!-- Literary motif (random-generable theme/motif seed) -->
  <div class="card bg-base-200">
    <div class="card-body p-4 gap-2">
      <div class="flex justify-between items-center">
        <h3 class="card-title text-base">{$t('config.motif.title')}</h3>
        <button {...genBtnProps('motif')}>
          {#if genBusy['motif']}
            <span class="loading loading-spinner loading-xs"></span>{$t('config.generating')}
          {:else}✨ {$t('common.generate')}{/if}
        </button>
      </div>
      <input class="input w-full text-base" bind:value={localStoryCfg.motif} placeholder={$t('config.motif.placeholder')} disabled={$taskRunning} />
      <div class="text-xs opacity-60">{$t('config.motif.hint')}</div>
      <div class="flex justify-end">
        <button class="btn btn-primary btn-xs" on:click={saveStoryConfig} disabled={$taskRunning}>{$t('common.save')}</button>
      </div>
    </div>
  </div>

  <!-- Story brief (AI generation seed) -->
  <div class="card bg-base-200">
    <div class="card-body p-4 gap-2">
      <div class="flex justify-between items-center">
        <h3 class="card-title text-base">{$t('config.brief.title')}</h3>
        <button {...genBtnProps('brief')}>
          {#if genBusy['brief']}
            <span class="loading loading-spinner loading-xs"></span>{$t('config.generating')}
          {:else}✨ {$t('common.generate')}{/if}
        </button>
      </div>
      <textarea class="textarea w-full h-32 text-base" bind:value={localStoryCfg.brief} placeholder={$t('config.brief.placeholder')} disabled={$taskRunning}></textarea>
      <div class="text-xs opacity-60">{$t('config.brief.hint')}</div>
      <div class="flex justify-end">
        <button class="btn btn-primary btn-xs" on:click={saveStoryConfig} disabled={$taskRunning}>{$t('common.save')}</button>
      </div>
    </div>
  </div>

  <!-- Writing Style & POV -->
  <div class="card bg-base-200">
    <div class="card-body p-4 gap-2">
      <div class="flex justify-between items-center">
        <h3 class="card-title text-base">{$t('config.style.title')}</h3>
        <button {...genBtnProps('style')}>
          {#if genBusy['style']}<span class="loading loading-spinner loading-xs"></span>{$t('config.generating')}{:else}✨ {$t('common.generate')}{/if}
        </button>
      </div>
      <div>
        <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.style.label')}</span>
        <textarea class="textarea w-full h-28 text-base" bind:value={localStoryCfg.writing_style} placeholder={$t('config.style.placeholder')} disabled={$taskRunning}></textarea>
      </div>
      <div>
        <span class="text-xs text-base-content/65 mb-0.5 block">{$t('config.pov.label')}</span>
        <textarea class="textarea w-full h-20 text-base" bind:value={localStoryCfg.writing_pov} placeholder={$t('config.pov.placeholder')} disabled={$taskRunning}></textarea>
      </div>
      <div class="flex justify-end">
        <button class="btn btn-primary btn-xs" on:click={saveStoryConfig} disabled={$taskRunning}>{$t('common.save')}</button>
      </div>
    </div>
  </div>

  <!-- Characters -->
  <div class="card bg-base-200">
    <div class="card-body p-4 gap-2">
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div class="flex justify-between items-center cursor-pointer select-none" on:click={() => charCollapse = !charCollapse}>
        <div class="flex items-center gap-2">
          <h3 class="card-title text-base">{$t('config.char.title')} <span class="text-xs font-normal text-base-content/65">({chars.length})</span></h3>
          <button {...genBtnProps('characters')}>
            {#if genBusy['characters']}<span class="loading loading-spinner loading-xs"></span>{$t('config.generating')}{:else}✨ {$t('common.generate')}{/if}
          </button>
        </div>
        <svg class="w-4 h-4 text-base-content/65 transition-transform" class:rotate-180={charCollapse} viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd"/></svg>
      </div>
      {#if !charCollapse}
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
      {/if}
    </div>
  </div>

  <!-- Worldview -->
  <div class="card bg-base-200">
    <div class="card-body p-4 gap-2">
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div class="flex justify-between items-center cursor-pointer select-none" on:click={() => wvCollapse = !wvCollapse}>
        <h3 class="card-title text-base">{$t('config.wv.title')} <span class="text-xs font-normal text-base-content/65">({filteredWvs.length})</span></h3>
        <div class="flex items-center gap-2">
          {#if localStoryCfg.locations_enabled}
            <button {...genBtnProps('locations')} onclick={(e) => { e.stopPropagation(); generateSection('locations'); }}>
              {#if genBusy['locations']}<span class="loading loading-spinner loading-xs"></span>{$t('config.generating')}{:else}✨ {$t('common.generate')}{/if}
            </button>
          {/if}
          <svg class="w-4 h-4 text-base-content/65 transition-transform" class:rotate-180={wvCollapse} viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd"/></svg>
        </div>
      </div>
      {#if !wvCollapse}
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
      {/if}
    </div>
  </div>

  <!-- Organizations -->
  <div class="card bg-base-200">
    <div class="card-body p-4 gap-2">
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div class="flex justify-between items-center cursor-pointer select-none" on:click={() => orgCollapse = !orgCollapse}>
        <div class="flex items-center gap-2">
          <h3 class="card-title text-base">{$t('config.org.title')} <span class="text-xs font-normal text-base-content/65">({orgs.length})</span></h3>
          <button {...genBtnProps('organizations')}>
            {#if genBusy['organizations']}<span class="loading loading-spinner loading-xs"></span>{$t('config.generating')}{:else}✨ {$t('common.generate')}{/if}
          </button>
        </div>
        <svg class="w-4 h-4 text-base-content/65 transition-transform" class:rotate-180={orgCollapse} viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd"/></svg>
      </div>
      {#if !orgCollapse}
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
              <button class="btn btn-ghost btn-xs" on:click={closeOrgForm}>{$t('common.cancel')}</button>
            </div>
          </div>
        {/if}

        <div class="flex gap-1.5">
          <button class="btn btn-primary btn-xs" on:click={requestNewOrg} disabled={$taskRunning}>{$t('config.org.create')}</button>
        </div>
      {/if}
    </div>
  </div>

  <!-- Relations -->
  <div class="card bg-base-200">
    <div class="card-body p-4 gap-2">
      <!-- svelte-ignore a11y-click-events-have-key-events -->
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div class="flex justify-between items-center cursor-pointer select-none" on:click={() => relCollapse = !relCollapse}>
        <div class="flex items-center gap-2">
          <h3 class="card-title text-base">{$t('config.rel.title')} <span class="text-xs font-normal text-base-content/65">({rels.length})</span></h3>
          <button {...genBtnProps('relations')}>
            {#if genBusy['relations']}<span class="loading loading-spinner loading-xs"></span>{$t('config.generating')}{:else}✨ {$t('common.generate')}{/if}
          </button>
        </div>
        <svg class="w-4 h-4 text-base-content/65 transition-transform" class:rotate-180={relCollapse} viewBox="0 0 20 20" fill="currentColor"><path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd"/></svg>
      </div>
      {#if !relCollapse}
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
      {/if}
    </div>
  </div>
</div>
