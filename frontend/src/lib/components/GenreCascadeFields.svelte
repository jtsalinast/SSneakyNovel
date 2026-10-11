<script>
  // GenreCascadeFields — round 21, written FROM SCRATCH (per user request) as a
  // self-contained replacement for the legacy inline cascade in Config.svelte.
  // The old fields stay in the file untouched; this block renders BELOW them.
  //
  // Design contract:
  //  - The backend (/api/novel-params) ships ALL parameters UNFILTERED.
  //  - This component filters EVERYTHING client-side through
  //    lib/subgenreTaxonomy.js — the single taxonomy implementation:
  //      * Subgenre list  -> subgenresForKeys() (one canonical parent each)
  //      * Specific Settings / Conflict Scale / Protagonist Type
  //                       -> cascadeForSelection() (subgenre catalog when its
  //                          parent is among the selected genres, otherwise the
  //                          union of the SELECTED genres' buckets only).
  //  - A subgenre or bucket from another genre can NEVER leak into these lists:
  //    there is no "unknown parent → show everything" fallback anywhere.
  //
  // It writes into the SAME story-config object as the legacy fields via
  // `bind:cfg`, so saving behaves identically whichever block the user edits.
  import ComboSelect from './ComboSelect.svelte';
  import InfoTip from './InfoTip.svelte';
  import { createEventDispatcher } from 'svelte';
  import { get } from 'svelte/store';
  import { uiLocale, translate } from '../i18n/index.js';
  import { CONFLICT_TERMS_EN, PROTAGONIST_TERMS_EN } from '../termDescriptions.js';
  import { GENRES, SUBGENRE_DATA } from '../novelwriterGenres.js';
  import {
    buildSubgenreIndex, subgenresForKeys, resolveParent, norm, cascadeForSelection,
    splitSubgenreText, subgenreBelongsToAny,
  } from '../subgenreTaxonomy.js';

  const dispatch = createEventDispatcher();

  export let paramsPayload = null;   // raw, unfiltered /api/novel-params blob
  export let genreKeys = [];         // selected frontend GENRES keys (legacy block)
  export let cfg;                    // localStoryCfg (bound by the parent)
  export let disabled = false;
  export let otherLabel = 'Other';
  export let placeholderGenre = '';
  export let placeholderSub = '';
  export let placeholderConflict = '';
  export let placeholderProtag = '';
  export let emptyLabel = 'Auto';
  export let clearLabel = 'Clear';
  export let labels = {};            // localized field labels from i18n
  export let hints = {};             // token -> localized subgenre hint
  export let tips = {};              // ⓘ tooltip texts (config.tip.*) for the side icons

  // —— Rich item lists (round 24) ——————————————————————————————————————
  // Secondary description lines exactly like the legacy comboboxes had:
  //  * Genre rows        -> GENRE_BLURBS (static, EN/ZH, keyed by GENRES key)
  //  * Subgenre rows     -> backend subgenre_hints (`hints` prop)
  //  * Conflict/Protag   -> term-family blurbs (EN/ES), keyword-resolved
  //  * Length/Structure  -> LENGTH_BLURBS / backend structure_hints
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
      comedy: 'Fiction whose primary promise is laughter — parody, farce, sitcom ensembles, dark and absurdist humor, workplace and paranormal comedy.',
      adventure: 'Movement-driven fiction: quests, exploration, survival and treasure hunts where the journey and its dangers are the plot.',
      mystery_thriller: 'Crime, puzzles and suspense: investigations from cozy whodunits to hard-boiled noir, plus chases, conspiracies and high-stakes thrillers.',
    },
    zh: {
      fantasy: '幻想世界：魔法、神话生物或超自然规则——从史诗第二世界到都市隐秘社会。',
      scifi: '以科技、科学或未来社会为根基的推想小说：太空歌剧、赛博朋克、反乌托邦、初次接触……',
      mystery: '以罪案与谜团为核心：调查推动情节，从乡村安乐椅推理到硬汉黑色侦探与劫案故事。',
      romance: '爱情故事即主线情节——情感弧线、关系障碍与令读者满意的结局是这类作品的承诺。',
      thriller: '节奏、危险与悬念：追逐、阴谋与高风险，无论是谍战、律政、家庭还是动作向。',
      horror: '以制造恐惧与不安为目的：超自然惊悚、宇宙性渺小、肉体恐怖或身边的怪物。',
      historical: '扎根于真实历史时期的小说，时代风物、习俗与事件塑造人物命运。',
      western: '边疆故事：无法无天、荣誉准则与旷野——经典、意大利面式、怪异或移植到太空的西部片。',
      drama: '以人物为先的文学小说：家族史诗、成长、心理写实、元小说、存在主义与哲思故事。',
      comedy: '以笑声为首要承诺的小说：戏仿、闹剧、情景喜剧群像、黑色与荒诞幽默、职场与超自然喜剧。',
      adventure: '以行动驱动的虚构：冒险、探索、求生与寻宝——旅程与危险即情节。',
      mystery_thriller: '罪案、谜团与悬念：从安乐椅推理到硬汉黑色侦探，外加追逐、阴谋与高风险惊悚。',
    },
  };
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
    zh: {
      flash: '微型小说：一篇连续完成（约1500字内），聚焦一个瞬间与一次反转。',
      short: '短篇小说（约1500–7500词）：单一线索、少量角色、迅速收束。',
      novelette: '中短篇（约7500–17500词）：主线之外可容纳一条副线与更多成长。',
      novella: '中篇（约17500–40000词）：情节集中、人物精简，具备长篇质感。',
      light_novel: '轻小说：日式连载文体——短章节（约3500词）、对白密集、以篇章推进。',
      anthology: '短篇集：多篇独立故事以主题或世界观串联，各自成篇。',
      novel: '标准长篇（约6–9万词）：完整多章结构，单章约4000词平衡节奏与深度。',
      epic: '史诗长篇（10万词以上）：多视角多支线、世界级冲突，单章约5000词。',
    },
  };
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
  // —— Exact-term description tables (round 26) ————————————————————————————
  // The keyword-family resolvers below are only a generic fallback: the real
  // Conflict-scale / Protagonist-type vocabularies shipped by the backend
  // (config.GenreConflictScales / GenreProtagonistTypes) contain hundreds of
  // specific terms ("Heavenly Tribulation", "Netrunner", "Dungeon Economy"…)
  // that matched no family and therefore rendered WITHOUT any second-line
  // description. CONFLICT_TERMS_EN / PROTAGONIST_TERMS_EN (lib/termDescriptions.js)
  // now cover every term in those catalogs verbatim (case-insensitive exact
  // lookup), so each combobox row always shows its own explanation.

  const CONFLICT_TERMS = { en: CONFLICT_TERMS_EN, es: CONFLICT_TERMS_EN };
  const PROTAGONIST_TERMS = { en: PROTAGONIST_TERMS_EN, es: PROTAGONIST_TERMS_EN };

  function famKey(text) {
    const t = String(text || '').toLowerCase();
    const has = (...ws) => ws.some((w) => t.includes(w));
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
  function itemDesc(text, fams, keyFn, exactTable) {
    const lang = get(uiLocale) === 'zh' ? 'es' : 'en'; // family catalogs ship EN + ES
    const tbl = exactTable ? (exactTable[lang] || exactTable.en) : null;
    if (tbl) {
      const k = String(text || '').trim().toLowerCase();
      if (tbl[k]) return tbl[k];
    }
    const k = keyFn(text);
    return k ? (fams[lang] || fams.en)[k] || '' : '';
  }
  function genreBlurb(key) {
    const lang = get(uiLocale) === 'zh' ? 'zh' : 'en';
    return (GENRE_BLURBS[lang] || GENRE_BLURBS.en)[key] || '';
  }
  function lengthBlurb(k) {
    const lang = get(uiLocale) === 'zh' ? 'zh' : 'en';
    return (LENGTH_BLURBS[lang] || LENGTH_BLURBS.en)[k] || '';
  }
  // Localized option labels for length/structure. Resolved directly through
  // the i18n catalog (reactive on uiLocale) — same keys the legacy comboboxes
  // used ('config.story.length.*' / 'config.story.structure.*'), falling back
  // to the humanized key when missing.
  function locLabel(key, prefix, fallback) {
    const lang = get(uiLocale);
    const v = translate('config.story.' + prefix + key, undefined, lang);
    return (typeof v === 'string' && v && v !== 'config.story.' + prefix + key) ? v : fallback;
  }

  // —— Specific-settings tooltip map (round 24) ———————————————————————————
  // Backend subgenre_hints also carries a blurb per SETTING token (e.g.
  // "magic_school") alongside the subgenre ones — exact-key lookup only, so
  // tokens that have no hint simply show no tooltip.
  $: settingsHintMap = (() => {
    const m = {};
    const src = hints || {};
    for (const s of settingOpts) {
      const k = String(s).toLowerCase();
      if (src[k]) m[s] = src[k];
    }
    return m;
  })();

  // —— Genre combobox (self-contained; no dependency on the legacy block) ——
  // Options come from the STATIC GENRES table, so they render immediately even
  // before /api/novel-params arrives. Multi-select chips, exactly like the
  // legacy Genre field; free text becomes "Other".
  $: genreOpts = GENRES.map((g) => ({ value: g.label, label: g.label, desc: genreBlurb(g.key) }));
  // Reactive local selection keys, synced from cfg.type (so editing either
  // block keeps both in sync — same storage format).
  $: myGenreKeys = (() => {
    const p = String(cfg.type || '').split(',').map((x) => x.trim()).filter(Boolean);
    const keys = [];
    for (const part of p) {
      const g = GENRES.find(
        (x) => x.label.toLowerCase() === part.toLowerCase() || x.key.toLowerCase() === part.toLowerCase(),
      );
      if (g) keys.push(g.key); else keys.push('__other__');
    }
    return keys.length ? keys : (genreKeys || []);
  })();
  // Effective keys driving every filter below: this block's own selection when
  // present, otherwise whatever the parent already knows (legacy block).
  $: effectiveKeys = String(cfg.type || '').trim() ? myGenreKeys : (genreKeys || []);

  function labelToKey(label) {
    return GENRES.find(
      (x) => x.label.toLowerCase() === label.toLowerCase() || x.key.toLowerCase() === label.toLowerCase(),
    )?.key || '__other__';
  }

  function onGenreChange(e) {
    const v = String(e.detail.value || '');
    const parts = v.split(',').map((x) => x.trim()).filter(Boolean);
    const unknown = parts.filter(
      (p) => !GENRES.some((x) => x.label.toLowerCase() === p.toLowerCase() || x.key.toLowerCase() === p.toLowerCase()),
    );
    cfg.type = [...parts.filter((p) => !unknown.includes(p)), ...unknown].join(', ');
    // Point-7 behaviour (round 22, multi-subgenre): keep ONLY the chips that
    // still belong to the new genre union; drop the ones that don't. If every
    // chip is orphaned the field resets to Auto — same semantics as before,
    // but per-chip instead of all-or-nothing.
    const keys = parts.map(labelToKey);
    const union = subgenresForKeys(index, keys);
    const kept = splitSubgenreText(cfg.subgenre).filter(
      (c) => union.some((s) => s.toLowerCase() === c.toLowerCase()) || subgenreBelongsToAny(index, c, keys),
    );
    if (kept.join(', ') !== splitSubgenreText(cfg.subgenre).join(', ')) {
      cfg.subgenre = kept.join(', ');
    }
    dispatch('genreChange', { keys, value: cfg.type });
  }

  // —— Subgenre combobox (MULTI-SELECT, round 22) ————————————————————————
  // Same interaction as the Genre field: chips + free text. The stored format
  // is a comma-separated list of labels ("Cozy Mystery, Noir"), identical to
  // how Type stores multiple genres and to what the backend now understands
  // (config.SplitSubgenreList). Options are filtered by EVERY selected genre's
  // union; already-picked chips stay in the option list even if a later genre
  // change would no longer offer them (they get dropped from the VALUE only
  // when none of them belongs to the new selection anymore — see onGenreChange).
  $: index = buildSubgenreIndex(paramsPayload);
  $: filteredLabels = subgenresForKeys(index, effectiveKeys);
  // Round 27 hint lookup: the backend ships SubgenreHints keyed by display
  // label ("cozy mystery") AND — since this round — a snake_case alias
  // ("cozy_mystery"). Free-text chips may arrive in any spelling, so we try
  // every normalization here instead of relying on the server alone.
  function hintFor(text) {
    const src = hints || {};
    const raw = String(text || '');
    const flat = raw.toLowerCase().replace(/\s+/g, ' ').trim(); // collapse spaces
    const cands = [
      norm(raw),                        // "cozy mystery" (spaces, lowercase)
      flat,                             // "road-trip comedy" (hyphens kept, spaces collapsed)
      flat.replace(/-+/g, ' '),         // "road trip comedy" (hyphens -> spaces)
      flat.replace(/-+/g, '_'),         // "road_trip_comedy"
      norm(raw).replace(/ +/g, '_'),    // "cozy_mystery"
      raw.trim().toLowerCase().replace(/[ -]+/g, '_'),
    ];
    for (const k of cands) { if (src[k]) return src[k]; }
    return '';
  }
  $: subOpts = (() => {
    const opts = filteredLabels.map((s) => ({
      value: s, label: s, desc: hintFor(s),
    }));
    for (const cur of splitSubgenreText(cfg.subgenre)) {
      if (!opts.some((o) => o.value.toLowerCase() === cur.toLowerCase())) {
        opts.push({ value: cur, label: cur, desc: hintFor(cur) });
      }
    }
    return opts;
  })();

  // —— Dependent cascade fields (Specific Settings / Conflict / Protagonist) —
  // Passes the FULL multi-select text: cascadeForSelection resolves each chip
  // and unions their own catalogs when every chip belongs to the selection.
  $: cascade = cascadeForSelection(
    paramsPayload,
    { conflicts: {}, protagonists: {}, settings: {}, subgenreData: SUBGENRE_DATA },
    cfg.subgenre,
    effectiveKeys,
  );
  $: conflictOpts = (cascade.conflicts || []).map((c) => ({ value: c, label: c, desc: itemDesc(c, CONFLICT_FAMILIES, famKey, CONFLICT_TERMS) }));
  $: protagOpts = (cascade.protagonists || []).map((p) => ({ value: p, label: p, desc: itemDesc(p, PROTAGONIST_FAMILIES, protagFamKey, PROTAGONIST_TERMS) }));
  $: settingOpts = cascade.settings || [];

  // Checked-state helpers over cfg.specific_settings (newline-separated tokens,
  // exactly like the legacy block — same storage format, so both stay in sync).
  function currentList() {
    return String(cfg.specific_settings || '').split('\n').map((x) => x.trim()).filter(Boolean);
  }
  function checked(s) { return currentList().includes(s); }
  function toggle(s) {
    const cur = currentList();
    const i = cur.indexOf(s);
    if (i >= 0) cur.splice(i, 1); else cur.push(s);
    cfg.specific_settings = cur.join('\n');
    dispatch('settingsChange', { value: cfg.specific_settings });
  }

  // —— Specific-settings suggestion gating (round 24, requirement #3) ————
  // The checkbox shortcuts and the modifier presets only appear while at least
  // ONE subgenre chip is selected. With no subgenre they would just be the raw
  // genre-union dump, which reads as noise; the free-text box below stays
  // available for fully custom settings either way.
  $: hasSubgenre = splitSubgenreText(cfg.subgenre).length > 0;
  $: showSettingShortcuts = hasSubgenre && settingOpts.length > 0;

  // —— Modifier presets (round 23; hidden when no subgenre is selected —
  // round 24 requirement #3: shortcuts only make sense once a subgenre anchors
  // them) ————————————————————————————————————————————————————————————————
  $: modifierMap = (paramsPayload && paramsPayload.modifier_presets) || {};
  $: modifierGroups = hasSubgenre
    ? Object.keys(modifierMap).sort((a, b) => a.localeCompare(b))
        .map((name) => ({ name, settings: modifierMap[name] || [] }))
        .filter((g) => g.settings.length)
    : [];
  function modifierChecked(name) {
    const list = currentList();
    const s = modifierMap[name] || [];
    return s.length > 0 && s.every((x) => list.includes(x));
  }
  function toggleModifier(name) {
    const s = modifierMap[name] || [];
    if (!s.length) return;
    const cur = currentList();
    if (modifierChecked(name)) {
      cfg.specific_settings = cur.filter((x) => !s.includes(x)).join('\n');
    } else {
      cfg.specific_settings = [...new Set([...cur, ...s])].join('\n');
    }
    dispatch('settingsChange', { value: cfg.specific_settings });
  }

  function onSubChange(e) {
    // Multi-select: normalize the joined chip list ("Cozy Mystery, Noir").
    const chips = splitSubgenreText(String(e.detail.value || ''));
    cfg.subgenre = chips.join(', ');
    const first = chips[0] || '';
    dispatch('subChange', { value: cfg.subgenre, chips, parent: resolveParent(index, first) });
  }
  function onConflictChange(e) { cfg.conflict_scale = String(e.detail.value || '').trim(); }
  function onProtagChange(e) { cfg.protagonist_type = String(e.detail.value || '').trim(); }

  // —— Free-text specific settings ————————————————————————————————————————
  // The textarea holds ONLY the entries that are not part of the current
  // suggestion list. When shortcuts are hidden (no subgenre selected, round-24
  // rule #3) EVERY stored entry must stay visible/editable here, otherwise it
  // would silently disappear from the UI while still being saved.
  let freeSettingsText = '';
  let freeSettingsDirty = false;
  $: if (!freeSettingsDirty) {
    const known = showSettingShortcuts ? new Set(settingOpts) : new Set();
    freeSettingsText = currentList().filter((x) => !known.has(x)).join('\n');
  }
  function applyFreeSettings() {
    freeSettingsDirty = false;
    const known = showSettingShortcuts ? new Set(settingOpts) : new Set();
    const checkedNow = currentList().filter((x) => known.has(x));
    const custom = freeSettingsText.split('\n').map((x) => x.trim()).filter(Boolean);
    cfg.specific_settings = [...new Set([...checkedNow, ...custom])].join('\n');
    dispatch('settingsChange', { value: cfg.specific_settings });
  }

  // —— Tone suggestions ——————————————————————————————————————————————————
  // Free-text tone field + suggestion chips from the resolved subgenre catalog
  // (backend `subgenre_catalogs.tones` / static SUBGENRE_DATA). Same storage
  // format as the legacy block: comma-separated labels in cfg.tone.
  $: toneOpts = cascade.tones || [];
  function toneList(v) {
    return String(v || '').split(',').map((x) => x.trim()).filter(Boolean);
  }
  function toneChecked(t) { return toneList(cfg.tone).includes(t); }
  function toggleTone(t) {
    const cur = toneList(cfg.tone);
    const i = cur.indexOf(t);
    if (i >= 0) cur.splice(i, 1); else cur.push(t);
    cfg.tone = cur.join(', ');
  }

  // —— Story length combobox (round 23) ————————————————————————————————
  // Options come from the backend payload (`length_keys`) with a static
  // fallback; picking a length applies its words-per-chapter preset
  // (`target_words_by_length`), same behaviour as the legacy field.
  const STATIC_LENGTH_KEYS = ['flash', 'short', 'novelette', 'novella', 'light_novel', 'anthology', 'novel', 'epic'];
  const STATIC_TARGET_WORDS = { flash: 1000, short: 2000, novelette: 2500, novella: 2500, light_novel: 3500, anthology: 4000, novel: 4000, epic: 5000 };
  $: lengthOpts = (() => {
    const vals = (paramsPayload && Array.isArray(paramsPayload.length_keys) && paramsPayload.length_keys.length)
      ? paramsPayload.length_keys : STATIC_LENGTH_KEYS;
    return vals.map((k) => ({ value: k, label: locLabel(k, 'length.', String(k).replaceAll('_', ' ')), desc: lengthBlurb(k) }));
  })();
  function onLengthChange(e) {
    const v = String(e.detail.value || '').trim();
    cfg.story_length = v;
    const tw = (paramsPayload && paramsPayload.target_words_by_length) || STATIC_TARGET_WORDS;
    if (tw[v]) cfg.target_words_per_chapter = tw[v];
    dispatch('lengthChange', { value: v });
  }

  // —— Narrative structure combobox (round 23) ——————————————————————————
  // Single-select over the backend `all_structure_keys` payload (static
  // fallback), storing the same plain key as the legacy field (e.g. "three_act").
  const STATIC_STRUCTURE_KEYS = ['three_act', 'six_act', 'fichtean', 'freytag', 'seven_point', 'heros_journey', 'heros_journey_simple', 'save_the_cat', 'story_circle', 'kishotenketsu', 'snowflake', 'quest', 'romance_arc', 'episodic'];
  // Backend structure_hints (token -> beat description), fed through the
  // `hints` prop's sibling: Config passes it as labels.__structHints to keep
  // the props surface small. Falls back to the static payload copy.
  $: structHints = (labels.__structHints) || (paramsPayload && paramsPayload.structure_hints) || {};
  $: structureOpts = (() => {
    const vals = (paramsPayload && Array.isArray(paramsPayload.all_structure_keys) && paramsPayload.all_structure_keys.length)
      ? paramsPayload.all_structure_keys : STATIC_STRUCTURE_KEYS;
    return vals.map((k) => ({ value: k, label: locLabel(k, 'structure.', String(k).replaceAll('_', ' ')), desc: structHints[k] || '' }));
  })();
  function onStructureChange(e) {
    cfg.structure = String(e.detail.value || '').trim();
    dispatch('structureChange', { value: cfg.structure });
  }
</script>

<div class="rounded-md border border-primary/35 bg-base-100/40 p-3 space-y-3">
  <div class="text-xs font-semibold uppercase tracking-wide text-primary/80">
    {labels.sectionTitle || 'Genre-filtered parameters (new)'}
  </div>

  <div class="grid grid-cols-2 gap-3">
    <div>
      <span class="text-xs text-base-content/65 mb-0.5 flex items-center gap-1">
        {labels.genre || 'Genre'}
      </span>
      <ComboSelect
        options={genreOpts}
        multiple={true}
        allowOther={true}
        otherLabel={otherLabel}
        placeholder={placeholderGenre}
        emptyLabel={emptyLabel}
        clearLabel={clearLabel}
        disabled={disabled}
        fieldTip={tips.genre}
        ariaLabel={labels.genre || 'Genre'}
        bind:value={cfg.type}
        on:change={onGenreChange}
      />
    </div>
    <div>
      <span class="text-xs text-base-content/65 mb-0.5 flex items-center gap-1">
        {labels.subgenre || 'Subgenre'}
      </span>
      <ComboSelect
        options={subOpts}
        multiple={true}
        allowOther={true}
        otherLabel={otherLabel}
        placeholder={placeholderSub}
        emptyLabel={emptyLabel}
        clearLabel={clearLabel}
        disabled={disabled}
        fieldTip={tips.subgenre}
        ariaLabel={labels.subgenre || 'Subgenre'}
        bind:value={cfg.subgenre}
        on:change={onSubChange}
      />
    </div>
    <!-- Round 26: the "From genres:" info box under the Genre combobox was
         removed per user request (class rounded border ... opacity-70). -->

    <div>
      <span class="text-xs text-base-content/65 mb-0.5 flex items-center gap-1">
        {labels.conflict || 'Conflict scale'}
        <span class="badge badge-ghost badge-sm font-mono normal-case pointer-events-none">{conflictOpts.length}</span>
      </span>
      <ComboSelect
        options={conflictOpts}
        allowOther={true}
        otherLabel={otherLabel}
        placeholder={placeholderConflict}
        emptyLabel={emptyLabel}
        clearLabel={clearLabel}
        disabled={disabled}
        fieldTip={tips.conflict}
        ariaLabel={labels.conflict || 'Conflict scale'}
        bind:value={cfg.conflict_scale}
        on:change={onConflictChange}
      />
    </div>
    <div>
      <span class="text-xs text-base-content/65 mb-0.5 flex items-center gap-1">
        {labels.protagonist || 'Protagonist type'}
        <span class="badge badge-ghost badge-sm font-mono normal-case pointer-events-none">{protagOpts.length}</span>
      </span>
      <ComboSelect
        options={protagOpts}
        allowOther={true}
        otherLabel={otherLabel}
        placeholder={placeholderProtag}
        emptyLabel={emptyLabel}
        clearLabel={clearLabel}
        disabled={disabled}
        fieldTip={tips.protagonist}
        ariaLabel={labels.protagonist || 'Protagonist type'}
        bind:value={cfg.protagonist_type}
        on:change={onProtagChange}
      />
    </div>
  </div>

  <div>
    <span class="text-xs text-base-content/65 mb-0.5 flex items-center gap-1">
      {labels.settings || 'Specific settings'}
      <InfoTip text={tips.specificSettings} label={labels.settings || 'Specific settings'} />
      {#if showSettingShortcuts}<span class="badge badge-ghost badge-sm font-mono normal-case pointer-events-none">{settingOpts.length}</span>{/if}
    </span>
    {#if showSettingShortcuts}
      <div class="grid grid-cols-2 gap-x-3 gap-y-1">
        {#each settingOpts as s}
          <label class="flex items-center gap-1.5 text-xs cursor-pointer font-mono" title={settingsHintMap[s] || ''}>
            <input type="checkbox" class="checkbox checkbox-xs" checked={checked(s)} on:change={() => toggle(s)} disabled={disabled} />
            {s.replaceAll('_', ' ')}
          </label>
        {/each}
      </div>
    {:else}
      <p class="text-xs text-base-content/45">{labels.pickSubgenre || labels.pickGenre || 'Select a subgenre to populate these checkboxes.'}</p>
    {/if}
    {#if hasSubgenre && modifierGroups.length}
      <details class="mt-1.5" open>
        <summary class="text-xs text-base-content/65 cursor-pointer select-none">{labels.modifiers || 'Modifiers'}</summary>
        <div class="grid grid-cols-3 gap-x-3 gap-y-1 mt-1">
          {#each modifierGroups as grp}
            <label class="flex items-center gap-1.5 text-xs cursor-pointer" title={grp.settings.map((x) => x.replaceAll('_', ' ')).join(', ')}>
              <input type="checkbox" class="checkbox checkbox-xs" checked={modifierChecked(grp.name)} on:change={() => toggleModifier(grp.name)} disabled={disabled} />
              {grp.name}
            </label>
          {/each}
        </div>
      </details>
    {/if}
  </div>

  <!-- Free-text specific settings (newline-separated tokens), kept in the SAME
       storage field as the checkboxes so both stay in sync. -->
  <textarea
    class="textarea textarea-sm w-full h-12 text-xs font-mono"
    bind:value={freeSettingsText}
    on:blur={applyFreeSettings}
    on:input={() => { freeSettingsDirty = true; }}
    placeholder={labels.settingsPlaceholder || 'custom_setting_per_line'}
    disabled={disabled}
  ></textarea>

  <!-- Tone: free text + per-subgenre suggestion chips (from the resolved
       catalog). Same comma-separated storage as the legacy field. -->
  <div>
    <span class="text-xs text-base-content/65 mb-0.5 flex items-center gap-1">
      {labels.tone || 'Tone'}
      <InfoTip text={tips.tone} label={labels.tone || 'Tone'} />
    </span>
    <input type="text" class="input input-sm w-full mb-1" bind:value={cfg.tone} placeholder={labels.tonePlaceholder || ''} disabled={disabled} />
    {#if toneOpts.length}
      <div class="flex flex-wrap gap-x-3 gap-y-1">
        {#each toneOpts as t}
          <label class="flex items-center gap-1.5 text-xs cursor-pointer">
            <input type="checkbox" class="checkbox checkbox-xs" checked={toneChecked(t)} on:change={() => toggleTone(t)} disabled={disabled} />
            {t}
          </label>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Length & structure: same comboboxes as the legacy fields, populated from
       the backend payload. Picking a length applies its word-per-chapter
       preset and a default structure when the current one is empty/auto. -->
  <div class="grid grid-cols-2 gap-3">
    <div>
      <span class="text-xs text-base-content/65 mb-0.5 flex items-center gap-1">
        {labels.length || 'Story length'}
      </span>
      <ComboSelect
        options={lengthOpts}
        allowOther={false}
        placeholder={labels.lengthNone || '—'}
        emptyLabel={labels.lengthNone || '—'}
        clearLabel={clearLabel}
        disabled={disabled}
        fieldTip={tips.length}
        ariaLabel={labels.length || 'Story length'}
        bind:value={cfg.story_length}
        on:change={onLengthChange}
      />
    </div>
    <div>
      <span class="text-xs text-base-content/65 mb-0.5 flex items-center gap-1">
        {labels.structure || 'Narrative structure'}
      </span>
      <ComboSelect
        options={structureOpts}
        allowOther={true}
        otherLabel={otherLabel}
        placeholder={labels.structurePlaceholder || ''}
        emptyLabel={emptyLabel}
        clearLabel={clearLabel}
        disabled={disabled}
        fieldTip={tips.structure}
        ariaLabel={labels.structure || 'Narrative structure'}
        bind:value={cfg.structure}
        on:change={onStructureChange}
      />
    </div>
  </div>
</div>
