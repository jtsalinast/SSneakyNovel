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
  import SmartCombobox from './SmartCombobox.svelte';
  import SmartSelect from './SmartSelect.svelte';
  import InfoTip from './InfoTip.svelte';
  import { createEventDispatcher } from 'svelte';
  import { get } from 'svelte/store';
  import { uiLocale, translate } from '../i18n/index.js';
  import { CONFLICT_TERMS_EN, PROTAGONIST_TERMS_EN } from '../termDescriptions.js';
  import { GENRES, SUBGENRE_DATA, genreBucketKeys } from '../novelwriterGenres.js';
  import {
    buildSubgenreIndex, subgenresForKeys, resolveParent, norm, cascadeForSelection,
    splitSubgenreText, subgenreBelongsToAny, normalizeBuckets,
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
  // Round 29: flat mode — when true, the component renders ONLY its inner
  // fields (no wrapper card/border/background and no section-title header),
  // so the parent can place them at the same level as the rest of the
  // "Story parameters" content instead of nested inside two extra divs.
  export let flat = false;
  // Escape hatch: the legacy ComboSelect twins of Genre/Subgenre/Conflict/
  // Protagonist are hidden by default (the SmartCombobox widgets are now the
  // primary UI). Set this to true to bring them back — they stay mounted and
  // route through the SAME shared handler bodies, so behaviour is identical
  // either way.
  let showLegacyCombos = false;

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

  // Shared body of the Genre change handler: BOTH the smart combobox and the
  // hidden legacy ComboSelect route through this function, so whichever one
  // becomes visible again keeps identical side-effect behaviour.
  // Point-7 behaviour (round 22, multi-subgenre): keep ONLY the chips that
  // still belong to the new genre union; drop the ones that don't. If every
  // chip is orphaned the field resets to Auto — same semantics as before,
  // but per-chip instead of all-or-nothing. The 'genreChange' dispatch keeps
  // the parent's derived state (selectedGenreKeys, randomize, info box)
  // coherent. NOTE: no echo back into genreSmartValue — the smart widget owns
  // its selection internally and re-parsing labels against the catalog used
  // to drop the most recently added chip (the stale-echo bug).
  function applyGenreSelection(v) {
    const parts = String(v || '').split(',').map((x) => x.trim()).filter(Boolean);
    const unknown = parts.filter(
      (p) => !GENRES.some((x) => x.label.toLowerCase() === p.toLowerCase() || x.key.toLowerCase() === p.toLowerCase()),
    );
    cfg.type = [...parts.filter((p) => !unknown.includes(p)), ...unknown].join(', ');
    const keys = parts.map(labelToKey);
    const union = subgenresForKeys(index, keys);
    const kept = splitSubgenreText(cfg.subgenre).filter(
      (c) => union.some((s) => s.toLowerCase() === c.toLowerCase()) || subgenreBelongsToAny(index, c, keys),
    );
    if (kept.join(', ') !== splitSubgenreText(cfg.subgenre).join(', ')) {
      cfg.subgenre = kept.join(', ');
      // Mirror the purge into the Subgenre smart widget as well, so it shows
      // the surviving chips live.
      subSmartValue = cfg.subgenre;
    }
    dispatch('genreChange', { keys, value: cfg.type });
  }
  function onGenreChange(e) {
    applyGenreSelection(String(e.detail.value || ''));
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

  // Round 28 fix: the "Other" search editor of the Subgenre combo must ALWAYS
  // offer the complete subgenre catalog, unfiltered by the selected Genre(s).
  // subgenresForKeys with no keys returns the full union (browse-everything
  // mode), so we reuse it with an empty key list here. The normal dropdown
  // keeps showing only the genre-filtered options (subOpts).
  $: allSubLabels = subgenresForKeys(index, []);
  $: otherSubOpts = (() => {
    const opts = allSubLabels.map((s) => ({ value: s, label: s, desc: hintFor(s) }));
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

  // Shared body of the Subgenre change handler: BOTH the smart combobox and
  // the hidden legacy ComboSelect route through it, so either widget produces
  // identical side-effects (storage normalization + 'subChange' dispatch that
  // the parent Config.svelte relies on to refresh its derived state).
  function applySubSelection(chips) {
    cfg.subgenre = chips.join(', ');
    const first = chips[0] || '';
    dispatch('subChange', { value: cfg.subgenre, chips, parent: resolveParent(index, first) });
  }
  function onSubChange(e) {
    // Multi-select: normalize the joined chip list ("Cozy Mystery, Noir").
    applySubSelection(splitSubgenreText(String(e.detail.value || '')));
    // Keep the Subgenre smart widget in sync when the legacy combo is the
    // widget the user interacted with (its chips re-adopt the normalized list).
    subSmartValue = cfg.subgenre;
  }
  // Multi-select wire format for conflict_scale / protagonist_type: a
  // comma-separated list ("Personal stakes, Ticking clock"), the same shape
  // cfg.subgenre uses. The backend treats these fields as suggestion strings
  // injected verbatim into prompts, so lists need no extra backend work;
  // EffectiveConflict()/EffectiveProtagonist() resolve every "other" entry
  // against the *_other custom text. splitMultiList parses any stored shape
  // back into chips (single value, comma list, or newline list).
  function splitMultiList(text) {
    const out = [];
    for (const p of String(text || '').split(/[,\n]/)) {
      const s = p.trim();
      if (s && !out.some((o) => o.toLowerCase() === s.toLowerCase())) out.push(s);
    }
    return out;
  }
  // Shared body of the Conflict-scale change handler (smart + hidden legacy).
  function applyConflictSelection(chips) {
    cfg.conflict_scale = (chips || []).join(', ');
  }
  function onConflictChange(e) {
    applyConflictSelection(splitMultiList(String(e.detail.value || '')));
    // Keep the smart widget in sync when the legacy combo is the one the user
    // interacted with (bind:value only updates the smart's bound value; the
    // handler must forward it so its chips re-adopt the picks).
    conflictSmartValue = cfg.conflict_scale;
  }
  // Shared body of the Protagonist-type change handler (smart + hidden legacy).
  function applyProtagSelection(chips) {
    cfg.protagonist_type = (chips || []).join(', ');
  }
  function onProtagChange(e) {
    applyProtagSelection(splitMultiList(String(e.detail.value || '')));
    // Same two-way sync as above for the Protagonist smart widget.
    protagSmartValue = cfg.protagonist_type;
  }

  // Bound values of the Protagonist/Conflict smart comboboxes. Initialized
  // empty; seeded one-shot from the stored cfg field when the catalog is
  // available (see the $: blocks below — first non-empty sync wins,
  // afterwards the user rules).
  let protagSmartValue = '';
  let protagSmartSeeded = false;

  // —— SmartCombobox #2 — "Protagonist type" proof of concept ————————————
  // Same component, now fed with REAL protagonist data through the same
  // single-parent taxonomy the rest of the cascade uses. Three buckets, in
  // this order:
  //  * Suggested — protagonists tied to the selected SUBGENRE(s) (their own
  //    catalogs: backend subgenre_catalogs first, static SUBGENRE_DATA second).
  //  * Related   — everything else belonging to the selected GENRE(s) (union of
  //    the genre-level protagonist_types buckets), minus what Suggested shows.
  //  * Other     — every remaining protagonist available in the full catalog,
  //    regardless of genre/subgenre (backend payload buckets + every static
  //    SUBGENRE_DATA entry + the genres' own union), minus the two above.
  // The component runs in `single` mode (radio): cfg.protagonist_type stores
  // exactly one value, so picking an option replaces the selection and closes
  // the dropdown. This block is a PoC only — it does NOT replace the existing
  // Protagonist-type ComboSelect anywhere.
  $: protagChips = splitSubgenreText(cfg.subgenre);
  $: suggestedProtagonists = (() => {
    const out = [];
    const push = (x) => {
      const s = String(x || '').trim();
      if (s && !out.some((o) => o.toLowerCase() === s.toLowerCase())) out.push(s);
    };
    for (const chip of protagChips) {
      const parent = resolveParent(index, chip);
      if (parent && !effectiveKeys.includes(parent)) continue; // stale chip from another genre
      const tok = norm(chip).replace(/ /g, '_');
      const cat = (paramsPayload && paramsPayload.subgenre_catalogs && paramsPayload.subgenre_catalogs[tok]) || null;
      for (const p of (cat && cat.protagonists) || []) push(p);
      const sd = SUBGENRE_DATA[chip] || null;
      for (const p of (sd && sd.protagonists) || []) push(p);
    }
    return out;
  })();
  $: relatedProtagonists = (() => {
    const bk = normalizeBuckets(paramsPayload && paramsPayload.protagonist_types);
    const out = [];
    const push = (x) => {
      const s = String(x || '').trim();
      if (!s) return;
      if (suggestedProtagonists.some((o) => o.toLowerCase() === s.toLowerCase())) return;
      if (!out.some((o) => o.toLowerCase() === s.toLowerCase())) out.push(s);
    };
    for (const k of effectiveKeys) {
      if (!k || k === '__other__') continue;
      for (const bkKey of genreBucketKeys(k)) for (const p of bk[bkKey] || []) push(p);
      const g = GENRES.find((x) => x.key === k);
      if (g) for (const s of g.subgenres) for (const p of (SUBGENRE_DATA[s] && SUBGENRE_DATA[s].protagonists) || []) push(p);
    }
    return out;
  })();
  $: otherProtagonists = (() => {
    const taken = new Set([...suggestedProtagonists, ...relatedProtagonists].map((x) => x.toLowerCase()));
    const pools = [];
    const bk = paramsPayload && paramsPayload.protagonist_types;
    if (bk && typeof bk === 'object') for (const v of Object.values(bk)) if (Array.isArray(v)) pools.push(v);
    const poolStatic = [];
    for (const sd of Object.values(SUBGENRE_DATA)) for (const p of (sd && sd.protagonists) || []) poolStatic.push(p);
    for (const g of GENRES) for (const s of g.subgenres) for (const p of (SUBGENRE_DATA[s] && SUBGENRE_DATA[s].protagonists) || []) poolStatic.push(p);
    const out = [];
    for (const x of [...pools.flat(), ...poolStatic]) {
      const s = String(x || '').trim();
      if (!s || taken.has(s.toLowerCase())) continue;
      if (!out.some((o) => o.toLowerCase() === s.toLowerCase())) out.push(s);
    }
    return out;
  })();
  const protagItem = (p) => ({
    id: 'pt:' + p,
    label: p,
    description: itemDesc(p, PROTAGONIST_FAMILIES, protagFamKey, PROTAGONIST_TERMS),
  });
  $: smartProtagGroups = (() => {
    const gs = [];
    if (suggestedProtagonists.length) gs.push({ label: 'Sugeridos', items: suggestedProtagonists.map(protagItem) });
    if (relatedProtagonists.length) gs.push({ label: 'Relacionados', items: relatedProtagonists.map(protagItem) });
    if (otherProtagonists.length) gs.push({ label: 'Otros', items: otherProtagonists.map(protagItem) });
    return gs;
  })();
  function onSmartProtagChange(e) {
    // Mirror the FULL chip list into the SAME storage field the real combo
    // uses (multi-select now: comma-separated, same shape as cfg.subgenre).
    // Values come from e.detail.ids ('pt:<label>' — the widget's authoritative
    // id list), never from the possibly-stale reactive `items` snapshot; free
    // 'cx:' chips fall back to the emitted text (split on commas/newlines,
    // kept verbatim, like the legacy "Other" behaviour). Routes through the
    // SAME shared body as the hidden legacy handler so both widgets stay
    // behaviourally identical.
    const ids = e.detail.ids || [];
    const known = ids
      .filter((id) => String(id).startsWith('pt:'))
      .map((id) => String(id).slice(3));
    const custom = ids.filter((id) => String(id).startsWith('cx:')).map((id) => String(id).slice(3));
    const chips = known.length || custom.length
      ? [...known, ...custom]
      : splitMultiList(String(e.detail.value || ''));
    applyProtagSelection(chips);
  }
  // One-shot seed: show the already-stored protagonist picks (if any) as
  // active chips the first time every label is available in the catalog.
  // After that the user owns the widget — external edits to
  // cfg.protagonist_type (e.g. the legacy ComboSelect) intentionally do NOT
  // re-sync this block.
  $: if (!protagSmartSeeded && !protagSmartValue && cfg.protagonist_type) {
    const stored = splitMultiList(cfg.protagonist_type);
    const exists = stored.every((v) =>
      smartProtagGroups.some((g) => g.items.some((it) => it.label === v))
    );
    if (stored.length && exists) {
      protagSmartValue = stored.join(', ');
      protagSmartSeeded = true;
    }
  }

  // —— SmartCombobox #3 — "Conflict scale" proof of concept ————————————————
  // EXACT same three-bucket logic as the Protagonist PoC above, but over the
  // conflict vocabulary: Suggested = conflicts of the selected SUBGENRE(s)
  // (backend subgenre_catalogs.conflicts first, static SUBGENRE_DATA.conflicts
  // second); Related = everything else from the selected GENRE(s) (union of
  // the genre-level conflict_scales buckets + their subgenres' own catalogs),
  // minus Suggested; Other = every remaining conflict in the full global pool,
  // regardless of genre/subgenre, minus the two above. Single-choice mode;
  // mirrors the pick into cfg.conflict_scale (PoC only).
  $: suggestedConflicts = (() => {
    const out = [];
    const push = (x) => {
      const s = String(x || '').trim();
      if (s && !out.some((o) => o.toLowerCase() === s.toLowerCase())) out.push(s);
    };
    for (const chip of protagChips) {
      const parent = resolveParent(index, chip);
      if (parent && !effectiveKeys.includes(parent)) continue; // stale chip from another genre
      const tok = norm(chip).replace(/ /g, '_');
      const cat = (paramsPayload && paramsPayload.subgenre_catalogs && paramsPayload.subgenre_catalogs[tok]) || null;
      for (const c of (cat && cat.conflicts) || []) push(c);
      const sd = SUBGENRE_DATA[chip] || null;
      for (const c of (sd && sd.conflicts) || []) push(c);
    }
    return out;
  })();
  $: relatedConflicts = (() => {
    const bk = normalizeBuckets(paramsPayload && paramsPayload.conflict_scales);
    const out = [];
    const push = (x) => {
      const s = String(x || '').trim();
      if (!s) return;
      if (suggestedConflicts.some((o) => o.toLowerCase() === s.toLowerCase())) return;
      if (!out.some((o) => o.toLowerCase() === s.toLowerCase())) out.push(s);
    };
    for (const k of effectiveKeys) {
      if (!k || k === '__other__') continue;
      for (const bkKey of genreBucketKeys(k)) for (const c of bk[bkKey] || []) push(c);
      const g = GENRES.find((x) => x.key === k);
      if (g) for (const s of g.subgenres) for (const c of (SUBGENRE_DATA[s] && SUBGENRE_DATA[s].conflicts) || []) push(c);
    }
    return out;
  })();
  $: otherConflicts = (() => {
    const taken = new Set([...suggestedConflicts, ...relatedConflicts].map((x) => x.toLowerCase()));
    const pools = [];
    const bk = paramsPayload && paramsPayload.conflict_scales;
    if (bk && typeof bk === 'object') for (const v of Object.values(bk)) if (Array.isArray(v)) pools.push(v);
    const poolStatic = [];
    for (const sd of Object.values(SUBGENRE_DATA)) for (const c of (sd && sd.conflicts) || []) poolStatic.push(c);
    for (const g of GENRES) for (const s of g.subgenres) for (const c of (SUBGENRE_DATA[s] && SUBGENRE_DATA[s].conflicts) || []) poolStatic.push(c);
    const out = [];
    for (const x of [...pools.flat(), ...poolStatic]) {
      const s = String(x || '').trim();
      if (!s || taken.has(s.toLowerCase())) continue;
      if (!out.some((o) => o.toLowerCase() === s.toLowerCase())) out.push(s);
    }
    return out;
  })();
  const conflictItem = (c) => ({
    id: 'cf:' + c,
    label: c,
    description: itemDesc(c, CONFLICT_FAMILIES, famKey, CONFLICT_TERMS),
  });
  $: smartConflictGroups = (() => {
    const gs = [];
    if (suggestedConflicts.length) gs.push({ label: 'Sugeridos', items: suggestedConflicts.map(conflictItem) });
    if (relatedConflicts.length) gs.push({ label: 'Relacionados', items: relatedConflicts.map(conflictItem) });
    if (otherConflicts.length) gs.push({ label: 'Otros', items: otherConflicts.map(conflictItem) });
    return gs;
  })();
  function onSmartConflictChange(e) {
    // Mirror the FULL chip list into the SAME storage field the real combo
    // uses (multi-select now), via e.detail.ids ('cf:<label>' — authoritative,
    // immune to stale reactive snapshots; see onSmartProtagChange). Routes
    // through the SAME shared body as the hidden legacy handler so both
    // widgets stay behaviourally identical.
    const ids = e.detail.ids || [];
    const known = ids
      .filter((id) => String(id).startsWith('cf:'))
      .map((id) => String(id).slice(3));
    const custom = ids.filter((id) => String(id).startsWith('cx:')).map((id) => String(id).slice(3));
    const chips = known.length || custom.length
      ? [...known, ...custom]
      : splitMultiList(String(e.detail.value || ''));
    applyConflictSelection(chips);
  }
  let conflictSmartValue = '';
  let conflictSmartSeeded = false;
  $: if (!conflictSmartSeeded && !conflictSmartValue && cfg.conflict_scale) {
    const stored = splitMultiList(cfg.conflict_scale);
    const exists = stored.every((v) =>
      smartConflictGroups.some((g) => g.items.some((it) => it.label === v))
    );
    if (stored.length && exists) {
      conflictSmartValue = stored.join(', ');
      conflictSmartSeeded = true;
    }
  }

  // —— SmartCombobox #4 — "Subgenre" proof of concept —————————————————————
  // Same component fed with the REAL subgenre taxonomy, bucketed by how close
  // each subgenre is to the current selection:
  //  * Sugeridos   — children of the selected genre(s) (subgenresForKeys):
  //                  exactly what the regular Subgenre field offers.
  //  * Relacionados— children of the closest RELATED parent genre(s). The
  //                  affinity map below declares which genres are siblings
  //                  (e.g. Sci-Fi and Fantasy both lean on Action & Adventure,
  //                  so adventure's children surface here when either is
  //                  selected). Children already shown in Sugeridos never
  //                  repeat.
  //  * Otros       — children of EVERY remaining genre (the rest of the
  //                  taxonomy), minus the two buckets above.
  // Multi-select chips (cfg.subgenre stores a comma-separated list, identical
  // storage format to the real field). PoC only — does NOT replace the real
  // Subgenre ComboSelect.
  const GENRE_AFFINITY = {
    scifi: ['adventure', 'fantasy'],          // sci-fi ↔ action/adventure (+ fantasy for science-fantasy crossover)
    fantasy: ['adventure', 'scifi'],          // fantasy ↔ action/adventure (+ scifi)
    mystery_thriller: ['adventure', 'drama'],
    thriller: ['adventure', 'mystery_thriller'],
    romance: ['drama', 'adventure'],
    horror: ['drama', 'mystery_thriller'],
    adventure: ['fantasy', 'scifi'],
    historical: ['drama', 'adventure'],
    western: ['adventure', 'historical'],
    drama: ['romance', 'historical'],
    comedy: ['romance', 'drama'],
  };
  $: smartSubSuggested = subgenresForKeys(index, effectiveKeys);
  $: smartSubRelated = (() => {
    const relKeys = [...new Set(effectiveKeys.flatMap((k) => GENRE_AFFINITY[k] || []))]
      .filter((k) => !effectiveKeys.includes(k));
    const suggLower = new Set(smartSubSuggested.map((s) => s.toLowerCase()));
    return subgenresForKeys(index, relKeys).filter((s) => !suggLower.has(s.toLowerCase()));
  })();
  $: smartSubOther = (() => {
    const taken = new Set([...smartSubSuggested, ...smartSubRelated].map((s) => s.toLowerCase()));
    return allSubLabels.filter((s) => !taken.has(s.toLowerCase()));
  })();
  const subItem = (s) => ({ id: 'sg:' + s, label: s, description: hintFor(s) });
  $: smartSubGroups = (() => {
    const gs = [];
    if (smartSubSuggested.length) gs.push({ label: 'Sugeridos', items: smartSubSuggested.map(subItem) });
    if (smartSubRelated.length) gs.push({ label: 'Relacionados', items: smartSubRelated.map(subItem) });
    if (smartSubOther.length) gs.push({ label: 'Otros', items: smartSubOther.map(subItem) });
    return gs;
  })();
  function onSmartSubChange(e) {
    // Mirror the chip list into cfg.subgenre. The chips come from
    // e.detail.ids ('sg:<label>' — the widget's authoritative id list, immune
    // to stale reactive snapshots inside click handlers), falling back to the
    // emitted text for free 'cx:' chips (kept verbatim like the legacy
    // "Other". Runs the SAME shared body as the hidden legacy handler — the
    // parent (Config.svelte → onSubgenrePickerChange) relies on its
    // 'subChange' dispatch to refresh derived state.
    const ids = (e.detail.ids || []).filter((id) => String(id).startsWith('sg:')).map((id) => String(id).slice(3));
    const chips = ids.length ? [...new Set(ids)] : splitSubgenreText(String(e.detail.value || ''));
    applySubSelection(chips);
  }
  let subSmartValue = '';
  let subSmartSeeded = false;
  // One-shot seed of the stored subgenre chips (same rule as the other PoCs:
  // first non-empty sync wins; afterwards the widget owns the interaction).
  $: if (!subSmartSeeded && !subSmartValue && cfg.subgenre) {
    const chips = splitSubgenreText(cfg.subgenre);
    if (chips.length) {
      subSmartValue = chips.join(', ');
      subSmartSeeded = true;
    }
  }

  // —— SmartCombobox #5 — "Genre" proof of concept (NO groups) —————————————
  // All ten top-level genres as ONE flat list (grouped={false} hides the
  // accordion headers entirely). Multi-select, mirroring cfg.type with the
  // same comma-separated format the real Genre field stores. PoC only.
  $: smartGenreGroups = [{
    label: 'Géneros',
    items: GENRES.map((g) => ({ id: 'gn:' + g.key, label: g.label, description: genreBlurb(g.key) })),
  }];
  function onSmartGenreChange(e) {
    // Primary path: the keys come from e.detail.ids ('gn:<key>' per chip, 'cx:'
    // free chips → __other__) — the widget's authoritative id list, immune to
    // stale reactive snapshots inside click handlers. cfg.type is rebuilt from
    // the matching catalog labels; everything else (normalization, per-chip
    // purge of orphaned subgenres, genreChange dispatch) runs through the SAME
    // shared body as the legacy handler.
    const ids = e.detail.ids || [];
    if (ids.length || !e.detail.value) {
      let keys = ids
        .map((id) => (String(id).startsWith('gn:') ? String(id).slice(3) : null))
        .filter(Boolean);
      const customCount = ids.filter((id) => String(id).startsWith('cx:')).length;
      if (customCount) keys = [...keys, '__other__'];
      const labelsOfIds = (e.detail.items || []).map((it) => it.label);
      cfg.type = labelsOfIds.length ? labelsOfIds.join(', ') : '';
      const union = subgenresForKeys(index, keys);
      const kept = splitSubgenreText(cfg.subgenre).filter(
        (c) => union.some((s) => s.toLowerCase() === c.toLowerCase()) || subgenreBelongsToAny(index, c, keys),
      );
      if (kept.join(', ') !== splitSubgenreText(cfg.subgenre).join(', ')) {
        cfg.subgenre = kept.join(', ');
        // Mirror the purge into the Subgenre smart widget as well, so it shows
        // the surviving chips live.
        subSmartValue = cfg.subgenre;
      }
      dispatch('genreChange', { keys, value: cfg.type });
      return;
    }
    // Fallback (foreign detail shape / no ids): identical normalization, purge
    // and dispatch as the legacy ComboSelect handler via the shared body.
    applyGenreSelection(String(e.detail.value || ''));
  }
  let genreSmartValue = '';
  let genreSmartSeeded = false;
  $: if (!genreSmartSeeded && !genreSmartValue && cfg.type) {
    const parts = String(cfg.type).split(',').map((x) => x.trim()).filter(Boolean);
    if (parts.length) {
      genreSmartValue = parts.join(', ');
      genreSmartSeeded = true;
    }
  }

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
    // NOTE: the key is `description`, not `desc` — SmartCombobox and
    // SmartSelect read item.description for their two-line dropdown rows and
    // summary popovers; a misnamed field silently drops every second line.
    return vals.map((k) => ({ value: k, label: locLabel(k, 'length.', String(k).replaceAll('_', ' ')), description: lengthBlurb(k) }));
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
    // NOTE: the key is `description`, not `desc` — SmartCombobox and
    // SmartSelect read item.description for their two-line dropdown rows and
    // summary popovers; a misnamed field silently drops every second line.
    return vals.map((k) => ({ value: k, label: locLabel(k, 'structure.', String(k).replaceAll('_', ' ')), description: structHints[k] || '' }));
  })();
  function onStructureChange(e) {
    cfg.structure = String(e.detail.value || '').trim();
    dispatch('structureChange', { value: cfg.structure });
  }
</script>

{#if flat}
  <!-- Round 29: flat mode — the fields render directly as siblings of the
       parent's grid items (no nested wrapper divs, no card border/bg, no
       section-title header). -->
  <div class="col-span-2 grid grid-cols-2 gap-x-3 gap-y-1.5">
    {#if showLegacyCombos}
      <!-- Legacy ComboSelect versions kept behind a flag for A/B comparison;
           they share the exact same handlers (applyGenreSelection etc.) as the
           SmartCombobox replacements below. -->
      <div>
        <span class="field-label flex items-center gap-1">
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
        <span class="field-label flex items-center gap-1">
          {labels.subgenre || 'Subgenre'}
        </span>
        <ComboSelect
          options={subOpts}
          otherSearchOptions={otherSubOpts}
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
    {/if}

    <!-- Smart Genre: all top-level genres as ONE flat list, NO group headers
         (grouped={false}). Multi-select; writes cfg.type and purges orphaned
         subgenres exactly like the legacy combo did. -->
    <div class="col-span-2">
      <span class="field-label flex items-center gap-1">
        {labels.genre || 'Genre'}
      </span>
      <SmartCombobox
        bind:value={genreSmartValue}
        groups={smartGenreGroups}
        grouped={false}
        placeholder="Buscar o elegir géneros..."
        disabled={disabled}
        ariaLabel={labels.genre || 'Genre'}
        on:change={onSmartGenreChange}
      />
    </div>

    <!-- Smart Subgenre: Sugeridos = children of the selected genre(s);
         Relacionados = children of the closest related parent genre(s)
         (e.g. Action & Adventure for Sci-Fi / Fantasy); Otros = children of the
         remaining genres. Multi-select chips; writes cfg.subgenre. -->
    <div class="col-span-2">
      <span class="field-label flex items-center gap-1">
        {labels.subgenre || 'Subgenre'}
      </span>
      <SmartCombobox
        bind:value={subSmartValue}
        groups={smartSubGroups}
        placeholder="Buscar o elegir subgéneros..."
        disabled={disabled}
        ariaLabel={labels.subgenre || 'Subgenre'}
        on:change={onSmartSubChange}
      />
    </div>

    <!-- Smart Conflict scale: three-bucket logic over the real conflict
         vocabulary (Sugeridos = conflicts of the chosen subgenre(s);
         Relacionados = rest of the chosen genre(s); Otros = the rest of the
         global pool). Multi-select chips; writes cfg.conflict_scale as a
         comma-separated list. -->
    <div>
      <span class="field-label flex items-center gap-1">
        {labels.conflict || 'Conflict scale'}
      </span>
      <SmartCombobox
        bind:value={conflictSmartValue}
        groups={smartConflictGroups}
        placeholder="Buscar o elegir escalas de conflicto..."
        disabled={disabled}
        ariaLabel={labels.conflict || 'Conflict scale'}
        on:change={onSmartConflictChange}
      />
    </div>

    <!-- Smart Protagonist type: same component fed with the REAL protagonist
         catalog bucketed as Sugeridos (selected subgenre) / Relacionados (rest
         of the selected genre) / Otros (everything else). Multi-select chips;
         writes cfg.protagonist_type as a comma-separated list. -->
    <div>
      <span class="field-label flex items-center gap-1">
        {labels.protagonist || 'Protagonist type'}
      </span>
      <SmartCombobox
        bind:value={protagSmartValue}
        groups={smartProtagGroups}
        placeholder="Buscar o elegir tipos de protagonista..."
        disabled={disabled}
        ariaLabel={labels.protagonist || 'Protagonist type'}
        on:change={onSmartProtagChange}
      />
    </div>

    {#if showLegacyCombos}
      <div>
        <span class="field-label flex items-center gap-1">
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
        <span class="field-label flex items-center gap-1">
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
    {/if}

    <div class="col-span-2">
      <span class="field-label flex items-center gap-1">
        {labels.settings || 'Specific settings'}
        <InfoTip text={tips.specificSettings} label={labels.settings || 'Specific settings'} />
        {#if showSettingShortcuts}<span class="badge badge-ghost badge-sm font-mono normal-case pointer-events-none">{settingOpts.length}</span>{/if}
      </span>
      {#if showSettingShortcuts}
        <div class="flex flex-wrap gap-2">
          {#each settingOpts as s}
            <label class="chip-selectable chip-pop" title={settingsHintMap[s] || ''}>
              <input type="checkbox" checked={checked(s)} on:change={() => toggle(s)} disabled={disabled} />
              <!-- Check indicator (visible only while selected) -->
              <svg class="chip-check-icon" fill="none" stroke="currentColor" stroke-width="3" viewBox="0 0 24 24" aria-hidden="true">
                <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"></path>
              </svg>
              <span>{s.replaceAll('_', ' ')}</span>
            </label>
          {/each}
        </div>
      {:else}
        <p class="text-xs text-base-content/45">{labels.pickSubgenre || labels.pickGenre || 'Select a subgenre to populate these checkboxes.'}</p>
      {/if}
      {#if hasSubgenre && modifierGroups.length}
        <details class="mt-1.5">
          <summary class="text-xs text-base-content/65 cursor-pointer select-none">{labels.modifiers || 'Modifiers'}</summary>
          <!-- Modifiers listed vertically in 3 content-width columns; the layout
               lives in app.css `.chips-columns` (column-dense flow, each chip
               spans ceil(count/3) rows), so chips never stretch to full width. -->
          <div class="chips-columns mt-1" style="--chip-count: {modifierGroups.length}">
            {#each modifierGroups as grp}
              <label class="chip-selectable chip-pop" title={grp.settings.map((x) => x.replaceAll('_', ' ')).join(', ')}>
                <input type="checkbox" checked={modifierChecked(grp.name)} on:change={() => toggleModifier(grp.name)} disabled={disabled} />
                <svg class="chip-check-icon" fill="none" stroke="currentColor" stroke-width="3" viewBox="0 0 24 24" aria-hidden="true">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"></path>
                </svg>
                <span>{grp.name}</span>
              </label>
            {/each}
          </div>
        </details>
      {/if}
      <!-- Free-text specific settings (newline-separated tokens), kept in the SAME
           storage field as the checkboxes so both stay in sync. -->
      <textarea
        class="textarea textarea-sm w-full h-12 text-xs font-mono mt-1"
        bind:value={freeSettingsText}
        on:blur={applyFreeSettings}
        on:input={() => { freeSettingsDirty = true; }}
        placeholder={labels.settingsPlaceholder || 'custom_setting_per_line'}
        disabled={disabled}
      ></textarea>
    </div>

    <div class="col-span-2">
      <span class="field-label flex items-center gap-1">
        {labels.tone || 'Tone'}
        <InfoTip text={tips.tone} label={labels.tone || 'Tone'} />
      </span>
      <input type="text" class="input input-sm w-full mb-1" bind:value={cfg.tone} placeholder={labels.tonePlaceholder || ''} disabled={disabled} />
      {#if toneOpts.length}
        <div class="flex flex-wrap gap-x-3 gap-y-1">
          {#each toneOpts as t}
            <label class="chip-pop flex items-center gap-1.5 text-xs cursor-pointer">
              <input type="checkbox" class="checkbox checkbox-xs" checked={toneChecked(t)} on:change={() => toggleTone(t)} disabled={disabled} />
              {t}
            </label>
          {/each}
        </div>
      {/if}
    </div>

    <!-- Story length — SmartSelect (unified UI). Same lengthOpts data and
         onLengthChange handler as the old ComboSelect: ONE value, searchable
         two-line rows (label + blurb), ⓘ summary popover and ✕ clear button.
         Optional field → clearing maps to the legacy "—" (empty) state, so
         allowCustom stays off (a free-text length makes no sense here). -->
    <div>
      <span class="field-label flex items-center gap-1">
        {labels.length || 'Story length'}
      </span>
      <!-- svelte-ignore a11y-no-redundant-roles -->
      <SmartSelect
        options={lengthOpts}
        allowCustom={false}
        placeholder={labels.lengthNone || '—'}
        emptyLabel={labels.lengthNone || '—'}
        disabled={disabled}
        title={tips.length || ''}
        ariaLabel={labels.length || 'Story length'}
        bind:value={cfg.story_length}
        on:change={onLengthChange}
      />
    </div>
    <!-- Narrative structure — SmartSelect (unified UI). Same structureOpts /
         onStructureChange as the old ComboSelect; custom values allowed
         ("Other"), matching the legacy allowOther={true} behaviour. -->
    <div>
      <span class="field-label flex items-center gap-1">
        {labels.structure || 'Narrative structure'}
      </span>
      <!-- svelte-ignore a11y-no-redundant-roles -->
      <SmartSelect
        options={structureOpts}
        allowCustom={true}
        otherLabel={otherLabel}
        placeholder={labels.structurePlaceholder || ''}
        emptyLabel={emptyLabel}
        disabled={disabled}
        title={tips.structure || ''}
        ariaLabel={labels.structure || 'Narrative structure'}
        bind:value={cfg.structure}
        on:change={onStructureChange}
      />
    </div>
  </div>
{:else}
<div class="surface-card p-3 space-y-3">
  <div class="text-xs font-semibold uppercase tracking-wide text-primary/80">
    {labels.sectionTitle || 'Genre-filtered parameters (new)'}
  </div>

  <div class="grid grid-cols-2 gap-3">
    {#if showLegacyCombos}
      <!-- Legacy ComboSelect versions kept behind a flag for A/B comparison;
           they share the exact same handlers as the SmartCombobox replacements. -->
      <div>
        <span class="field-label flex items-center gap-1">
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
        <span class="field-label flex items-center gap-1">
          {labels.subgenre || 'Subgenre'}
        </span>
        <ComboSelect
          options={subOpts}
          otherSearchOptions={otherSubOpts}
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
    {/if}

    <!-- Smart Genre (card mode): ONE flat list, no group headers
         (grouped={false}). Multi-select; writes cfg.type. -->
    <div class="col-span-2">
      <span class="field-label flex items-center gap-1">
        {labels.genre || 'Genre'}
      </span>
      <SmartCombobox
        bind:value={genreSmartValue}
        groups={smartGenreGroups}
        grouped={false}
        placeholder="Buscar o elegir géneros..."
        disabled={disabled}
        ariaLabel={labels.genre || 'Genre'}
        on:change={onSmartGenreChange}
      />
    </div>

    <!-- Smart Subgenre (card mode): children of the selected genre(s) /
         of the related parent genre(s) / of the rest. Writes cfg.subgenre. -->
    <div class="col-span-2">
      <span class="field-label flex items-center gap-1">
        {labels.subgenre || 'Subgenre'}
      </span>
      <SmartCombobox
        bind:value={subSmartValue}
        groups={smartSubGroups}
        placeholder="Buscar o elegir subgéneros..."
        disabled={disabled}
        ariaLabel={labels.subgenre || 'Subgenre'}
        on:change={onSmartSubChange}
      />
    </div>

    <!-- Smart Conflict scale (card mode): three buckets over the real
         conflict vocabulary; multi-select chips; writes cfg.conflict_scale
         as a comma-separated list. -->
    <div>
      <span class="field-label flex items-center gap-1">
        {labels.conflict || 'Conflict scale'}
      </span>
      <SmartCombobox
        bind:value={conflictSmartValue}
        groups={smartConflictGroups}
        placeholder="Buscar o elegir escalas de conflicto..."
        disabled={disabled}
        ariaLabel={labels.conflict || 'Conflict scale'}
        on:change={onSmartConflictChange}
      />
    </div>

    <!-- Smart Protagonist type (card mode): Sugeridos / Relacionados / Otros
         over the REAL protagonist catalog. Multi-select chips; writes
         cfg.protagonist_type as a comma-separated list. -->
    <div>
      <span class="field-label flex items-center gap-1">
        {labels.protagonist || 'Protagonist type'}
      </span>
      <SmartCombobox
        bind:value={protagSmartValue}
        groups={smartProtagGroups}
        placeholder="Buscar o elegir tipos de protagonista..."
        disabled={disabled}
        ariaLabel={labels.protagonist || 'Protagonist type'}
        on:change={onSmartProtagChange}
      />
    </div>

    <!-- Round 26: the "From genres:" info box under the Genre combobox was
         removed per user request (class rounded border ... opacity-70). -->

    {#if showLegacyCombos}
      <div>
        <span class="field-label flex items-center gap-1">
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
        <span class="field-label flex items-center gap-1">
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
    {/if}
  </div>

  <div>
    <span class="field-label flex items-center gap-1">
      {labels.settings || 'Specific settings'}
      <InfoTip text={tips.specificSettings} label={labels.settings || 'Specific settings'} />
      {#if showSettingShortcuts}<span class="badge badge-ghost badge-sm font-mono normal-case pointer-events-none">{settingOpts.length}</span>{/if}
    </span>
    {#if showSettingShortcuts}
      <div class="flex flex-wrap gap-2">
        {#each settingOpts as s}
          <label class="chip-selectable chip-pop" title={settingsHintMap[s] || ''}>
            <input type="checkbox" checked={checked(s)} on:change={() => toggle(s)} disabled={disabled} />
            <!-- Check indicator (visible only while selected) -->
            <svg class="chip-check-icon" fill="none" stroke="currentColor" stroke-width="3" viewBox="0 0 24 24" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"></path>
            </svg>
            <span>{s.replaceAll('_', ' ')}</span>
          </label>
        {/each}
      </div>
    {:else}
      <p class="text-xs text-base-content/45">{labels.pickSubgenre || labels.pickGenre || 'Select a subgenre to populate these checkboxes.'}</p>
    {/if}
    {#if hasSubgenre && modifierGroups.length}
      <details class="mt-1.5">
        <summary class="text-xs text-base-content/65 cursor-pointer select-none">{labels.modifiers || 'Modifiers'}</summary>
        <!-- Modifiers in 3 content-width columns (see .chips-columns in app.css). -->
        <div class="chips-columns mt-1" style="--chip-count: {modifierGroups.length}">
          {#each modifierGroups as grp}
            <label class="chip-selectable chip-pop" title={grp.settings.map((x) => x.replaceAll('_', ' ')).join(', ')}>
              <input type="checkbox" checked={modifierChecked(grp.name)} on:change={() => toggleModifier(grp.name)} disabled={disabled} />
              <svg class="chip-check-icon" fill="none" stroke="currentColor" stroke-width="3" viewBox="0 0 24 24" aria-hidden="true">
                <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"></path>
              </svg>
              <span>{grp.name}</span>
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
    <span class="field-label flex items-center gap-1">
      {labels.tone || 'Tone'}
      <InfoTip text={tips.tone} label={labels.tone || 'Tone'} />
    </span>
    <input type="text" class="input input-sm w-full mb-1" bind:value={cfg.tone} placeholder={labels.tonePlaceholder || ''} disabled={disabled} />
    {#if toneOpts.length}
      <div class="flex flex-wrap gap-x-3 gap-y-1">
        {#each toneOpts as t}
          <label class="chip-pop flex items-center gap-1.5 text-xs cursor-pointer">
            <input type="checkbox" class="checkbox checkbox-xs" checked={toneChecked(t)} on:change={() => toggleTone(t)} disabled={disabled} />
            {t}
          </label>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Length & structure: SmartSelect twins of the flat-mode fields above
       (same lengthOpts/structureOpts data and handlers). Picking a length
       applies its word-per-chapter preset; clearing maps to the legacy
       empty/"Auto" states. -->
  <div class="grid grid-cols-2 gap-3">
    <div>
      <span class="field-label flex items-center gap-1">
        {labels.length || 'Story length'}
      </span>
      <!-- svelte-ignore a11y-no-redundant-roles -->
      <SmartSelect
        options={lengthOpts}
        allowCustom={false}
        placeholder={labels.lengthNone || '—'}
        emptyLabel={labels.lengthNone || '—'}
        disabled={disabled}
        title={tips.length || ''}
        ariaLabel={labels.length || 'Story length'}
        bind:value={cfg.story_length}
        on:change={onLengthChange}
      />
    </div>
    <div>
      <span class="field-label flex items-center gap-1">
        {labels.structure || 'Narrative structure'}
      </span>
      <!-- svelte-ignore a11y-no-redundant-roles -->
      <SmartSelect
        options={structureOpts}
        allowCustom={true}
        otherLabel={otherLabel}
        placeholder={labels.structurePlaceholder || ''}
        emptyLabel={emptyLabel}
        disabled={disabled}
        title={tips.structure || ''}
        ariaLabel={labels.structure || 'Narrative structure'}
        bind:value={cfg.structure}
        on:change={onStructureChange}
      />
    </div>
  </div>
</div>
{/if}
