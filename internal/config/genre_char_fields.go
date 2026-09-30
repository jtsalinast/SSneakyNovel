package config

import "strings"

// Genre-specific character fields, borrowed from NovelWriter's per-genre lore
// sheets (core/config/genre_configs/*.py). They are *suggestions* injected into
// the AI generation prompt for the "characters" section: the model may fill
// them when relevant and leave them empty otherwise, so they guide generation
// without forcing it. Keys use snake_case to stay stable in settings.json.

// Subgenre overrides take precedence over the parent-genre fields when the
// subgenre text matches (case-insensitive substring), so e.g. "space opera"
// yields ship/rank instead of the generic scifi set.
var subgenreCharacterExtraFields = map[string][]string{
	"epic fantasy":         {"magic_school", "affiliation", "lineage"},
	"urban fantasy":        {"magic_school", "cover_life"},
	"dark fantasy":         {"magic_cost", "affiliation"},
	"wuxia":                {"sect", "cultivation_stage", "signature_technique"},
	"xianxia":              {"sect", "cultivation_stage", "dantian_element"},
	"space opera":          {"ship_id", "rank", "faction"},
	"cyberpunk":            {"implants", "corp_affiliation", "street_name"},
	"post-apocalyptic":     {"scavenger_skill", "settlement", "radiation_tolerance"},
	"cozy mystery":         {"alibi", "small_town_role"},
	"noir":                 {"alibi", "vice_or_secret"},
	"heist":                {"crew_role", "cover_identity"},
	"legal thriller":       {"case_role", "conflict_of_interest"},
	"psychological horror": {"trauma_trigger", "denial_pattern"},
	"supernatural horror":  {"haunting_ties", "ritual_knowledge"},
	"historical romance":   {"social_class", "courtship_constraint"},
	"contemporary romance": {"career", "attachment_style"},
	"gold rush":            {"trade", "claim_ownership"},
	"solarpunk":            {"guild_or_coop", "rewilding_project", "solar-tech_role"},
	"romantasy":            {"bond_or_curse", "court_role", "magic_price"},
	"lit rpg":              {"class_build", "level_progression", "system_privilege"},
	"dystopian":            {"caste_record", "resistance_ties", "ration_status"},
	"military scifi":       {"unit_assignment", "mos_specialty"},
	"cosmic horror":        {"forbidden_knowledge", "sanity_state", "cult_ties"},
	// new subgenre presets (round 3)
	"progression fantasy":    {"power_milestone", "bottleneck_or_limit", "training_regimen"},
	"isekai":                 {"cheat_ability", "summoning_conditions", "return_home_stakes"},
	"dungeon core":           {"core_room_layout", "monster_roster", "floor_progression"},
	"portal fantasy":         {"threshold_rules", "first_contact_mishap", "home_vs_away_pull"},
	"biopunk":                {"gene_edit_lineage", "biohack_rig", "contagion_exposure"},
	"cli-fi":                 {"water_rights_portfolio", "drought_year_count", "dam_or_aquifer_tie"},
	"afrofuturism":           {"ancestral_technique", "nation_or_clan", "diaspora_connection"},
	"sword and sorcery":      {"mercenary_contract", "cursed_relic", "rival_champion"},
	"domestic thriller":      {"household_secret", "suspect_lie", "escape_plan"},
	"techno thriller":        {"zero_day_asset", "corporate_backer", "blowup_radius"},
	"police procedural":      {"case_file_role", "partner_history", "chain_of_command_friction"},
	"cold case":              {"retired_evidence_access", "personal_stake", "statute_pressure"},
	"locked room":            {"access_log", "key_holder_map", "staged_innocence"},
	"suspense romance":       {"danger_bond_origin", "protection_mandate", "trust_ledger"},
	"mafia romance":          {"family_oath", "rival_blood_debt", "exit_cost"},
	"sports romance":         {"team_league_role", "season_deadline", "rivalry_game"},
	"historical fiction":     {"era_occupation", "class_constraint", "real_event_proximity"},
	"nautical":               {"ship_station_duty", "seasickness_or_scars", "mutiny_stance"},
	"picaresque":             {"grift_of_the_week", "patron_or_pursuer", "disguise_alias"},
	"gothic":                 {"estate_curse", "family_sin", "madness_marker"},
	"folk horror":            {"local_custom_obligation", "outsider_status", "harvest_debt"},
	"psychological thriller": {"unreliable_memory", "therapy_or_medication", "gaslighting_vector"},
	"space western":          {"frontier_freighter_claim", "border_authority_warrant", "oxygen_debt"},
	"military fantasy":       {"regiment_and_rank", "war_magic_doctrine", "desertion_price"},
	"cozy sci-fi":            {"station_or_colony_post", "caretaking_duty", "tool_or_companion_ai"},
	"hard scifi":             {"physics_constraint_role", "life_support_dependency", "experiment_stakes"},
	"time travel":            {"temporal_anchor_device", "paradox_exposure", "fixed_point_obsession"},
	"zombie apocalypse":      {"immunity_status", "safehouse_route", "loss_that_keeps_them_going"},
	"superhero":              {"power_limitation", "public_identity_layer", "rogues_gallery_ties"},
	"steampunk":              {"gearwork_implant_or_gadget", "guild_apprenticeship", "airship_or_factory_tie"},
	"dark academia":          {"secret_society_membership", "advisor_dependence", "transcript_scandal"},
	"swashbuckler":           {"dueling_record", "corsair_letter_or_brand", "crew_loyalty"},
	"expedition survival":    {"ration_count", "route_expertise", "turnback_rule"},
	"wilderness survival":    {"foraging_skill", "injury_or_infection", "bear_with_it_metaphor"},
	"epic space fantasy":     {"dynasty_starhouse", "psionic_or_engine_gift", "throne_claim"},
	// anime / manga / manhwa / webnovel & game-genre modifiers (round 5).
	// These refine character sheets AND (via animeModifierPresets below) conflict
	// scales, protagonist types and specific settings — suggestions, never forced.
	"mecha":              {"pilot_neural_sync", "unit_designation", "g_for_pilot_limit"},
	"mahou shoujo":       {"transformation_trigger", "familiar_companion", "purification_cost"},
	"magical girl":       {"transformation_trigger", "familiar_companion", "purification_cost"},
	"slice of life":      {"daily_routine_anchor", "small_wish_of_the_day"},
	"iyashikei":          {"healing_ritual", "lingering_old_wound"},
	"school life":        {"class_year_seat", "club_activity", "academic_pressure_point"},
	"spokon":             {"training_regimen", "personal_best_or_record", "rival_athlete"},
	"harem":              {"route_attachment_style", "jealousy_flashpoint"},
	"ecchi":              {"fan_service_self_awareness", "embarrassing_power_side_effect"},
	"shounen":            {"nakama_bond", "never_give_up_trigger", "power_ceiling_obsession"},
	"battle shonen":      {"signature_move_name", "power_gap_vs_current_strongest", "training_arc_stage"},
	"mahou shounen":      {"spell_specialization", "mana_pool_quirk", "magic_society_registration"},
	"otome isekai":       {"breakable_heroine_flag", "curse_deadline", "capture_target_role"},
	"revenge":            {"grudge_origin_event", "escalation_ladder_step", "collateral_blindness"},
	"dungeon":            {"floor_clearance_record", "party_role", "loot_ethics"},
	"battle royale":      {"loadout_confession", "alliance_breakpoint", "arena_zone_memory"},
	"tower climbing":     {"current_floor", "floor_gate_trial", "clan_or_solo_status"},
	"regression rebirth": {"future_knowledge_edge", "regret_list_item", "butterfly_risk"},
	"villainess":         {"doomed_script_deviation", "court_alliance_web", "redemption_ledger"},
	// round-4 parent genres that were missing subgenre-keyed char-field entries
	"graphic novel": {"visual_motif_note", "panel_voice_note", "signature_prop"},
	"romcom":        {"meet_cute_story", "embarrassment_superpower", "blind_spot_about_themselves"},
}

var genreCharacterExtraFields = map[string][]string{
	"fantasy":     {"magic_school", "affiliation"},
	"scifi":       {"ship_id", "rank", "implants"},
	"mystery":     {"alibi", "secret_knowledge"},
	"romance":     {"love_history", "attachment_style"},
	"thriller":    {"handler_role", "cover_identity"},
	"horror":      {"haunting_ties", "survival_odds"},
	"historical":  {"social_class", "faction"},
	"western":     {"trade", "reputation"},
	"litrpg":      {"class_build", "level_progression"},
	"urban":       {"cover_life", "hidden_society_ties"},
	"military":    {"unit_assignment", "mos_specialty"},
	"postapo":     {"scavenger_skill", "settlement"},
	"adventure":   {"expedition_role", "specialty_skill"},
	"wuxia":       {"sect", "cultivation_stage"},
	"xianxia":     {"sect", "cultivation_stage"},
	"cozy":        {"small_town_role", "hobby_or_trade"},
	"heist":       {"crew_role", "cover_identity"},
	"cyberpunk":   {"implants", "corp_affiliation"},
	"solarpunk":   {"coop_role", "rewilding_project"},
	"romantasy":   {"bond_or_curse", "court_role"},
	"dystopian":   {"caste_record", "resistance_ties"},
	"space_opera": {"ship_id", "rank", "faction"},
	// new parent genres (round 3)
	"graphic_novel":             {"visual_motif", "panel_voice_note", "signature_prop"},
	"scifi:superhero":           {"power_limitation", "public_identity_layer"},
	"scifi:steampunk":           {"gearwork_gadget", "guild_apprenticeship"},
	"horror:dark_academia":      {"secret_society_membership", "advisor_dependence"},
	"adventure:swashbuckler":    {"dueling_record", "crew_loyalty"},
	"adventure:nautical":        {"ship_station_duty", "mutiny_stance"},
	"adventure:picaresque":      {"grift_of_the_week", "disguise_alias"},
	"horror:gothic":             {"estate_curse", "family_sin"},
	"scifi:time_travel":         {"temporal_anchor_device", "paradox_exposure"},
	"scifi:cli_fi":              {"water_rights_portfolio", "drought_year_count"},
	"scifi:afrofuturism":        {"ancestral_technique", "diaspora_connection"},
	"scifi:hard":                {"physics_constraint_role", "life_support_dependency"},
	"mystery:police_procedural": {"case_file_role", "partner_history"},
	"literary":                  {"wound_or_obsession", "self_deception", "what_they_wont_say"},
	"romance:romcom":            {"meet_cute_story", "embarrassment_superpower", "blind_spot_about_themselves"},
}

var genreCharacterExtraDesc = map[string][2]string{
	"fantasy": {
		"可补充 magic_school（学派的魔法体系/流派）与 affiliation（所属组织或阵营）。",
		"Optionally add magic_school (their magical discipline/school) and affiliation (organization or faction).",
	},
	"scifi": {
		"可补充 ship_id（隶属舰船/基地）、rank（军衔或职级）与 implants（义体/植入物）。",
		"Optionally add ship_id (assigned ship/base), rank (military or corporate rank) and implants (cybernetics).",
	},
	"mystery": {
		"可补充 alibi（典型不在场证明/日常行踪）与 secret_knowledge（其掌握的隐藏信息）。",
		"Optionally add alibi (their usual whereabouts/alibi) and secret_knowledge (hidden information they hold).",
	},
	"romance": {
		"可补充 love_history（情感史）与 attachment_style（依恋类型，如安全型/焦虑型）。",
		"Optionally add love_history (past relationships) and attachment_style (secure/anxious/avoidant...).",
	},
	"thriller": {
		"可补充 handler_role（情报职务，如线人/行动官）与 cover_identity（对外伪装身份）。",
		"Optionally add handler_role (intelligence function, e.g. case officer/informant) and cover_identity.",
	},
	"horror": {
		"可补充 haunting_ties（与恐怖源头的关联）与 survival_odds（其求生的优势/弱点简述）。",
		"Optionally add haunting_ties (link to the horror's origin) and survival_odds (what keeps them alive).",
	},
	"historical": {
		"可补充 social_class（社会阶层）与 faction（所属家族/派系）。",
		"Optionally add social_class (period social standing) and faction (family/party allegiance).",
	},
	"western": {
		"可补充 trade（职业营生）与 reputation（在边境的名声/传闻）。",
		"Optionally add trade (occupation) and reputation (how they are known on the frontier).",
	},
	"litrpg": {
		"可补充 class_build（职业/构筑）与 level_progression（当前等级与成长路线）。",
		"Optionally add class_build (class/build) and level_progression (current level & growth path).",
	},
	"urban": {
		"可补充 cover_life（表面身份/日常生活）与 hidden_society_ties（与隐秘社会的联系）。",
		"Optionally add cover_life (daytime ordinary identity) and hidden_society_ties (links to the masquerade world).",
	},
	"military": {
		"可补充 unit_assignment（所属部队）与 mos_specialty（专业兵种/技能）。",
		"Optionally add unit_assignment (unit/platoon) and mos_specialty (job specialty).",
	},
	"postapo": {
		"可补充 scavenger_skill（拾荒/生存专长）与 settlement（所属聚居地）。",
		"Optionally add scavenger_skill (salvage/survival trade) and settlement (home camp).",
	},
	"adventure": {
		"可补充 expedition_role（探险队职责）与 specialty_skill（关键专业技能）。",
		"Optionally add expedition_role (role in the party/expedition) and specialty_skill.",
	},
	"wuxia": {
		"可补充 sect（门派）、cultivation_stage（内功境界）与 signature_technique（成名绝技）。",
		"Optionally add sect, cultivation_stage (internal-energy realm) and signature_technique.",
	},
	"xianxia": {
		"可补充 sect（宗门）、cultivation_stage（修行境界）与 dantian_element（灵根属性）。",
		"Optionally add sect, cultivation_stage (realm) and dantian_element (spirit root).",
	},
	"cozy": {
		"可补充 small_town_role（小镇中的角色/声望）与 hobby_or_trade（爱好或营生）。",
		"Optionally add small_town_role and hobby_or_trade (baking, gardening...).",
	},
	"heist": {
		"可补充 crew_role（团队分工，如撬锁/车手）与 cover_identity（作案伪装身份）。",
		"Optionally add crew_role (lockpicker, driver...) and cover_identity.",
	},
	"cyberpunk": {
		"可补充 implants（义体改造）、corp_affiliation（效忠企业）与 street_name（街头代号）。",
		"Optionally add implants, corp_affiliation and street_name (handle).",
	},
	"solarpunk": {
		"可补充 coop_role（合作社职务）与 rewilding_project（参与的生态修复项目）。",
		"Optionally add coop_role (cooperative job) and rewilding_project.",
	},
	"romantasy": {
		"可补充 bond_or_curse（羁绊或诅咒）与 court_role（宫廷身份）。",
		"Optionally add bond_or_curse and court_role (fae/god court position).",
	},
	"dystopian": {
		"可补充 caste_record（种姓/档案等级）与 resistance_ties（与抵抗组织的联系）。",
		"Optionally add caste_record (assigned caste/file status) and resistance_ties.",
	},
	"space_opera": {
		"可补充 ship_id（任职舰船）、rank（军衔/商阶）与 faction（星际派系）。",
		"Optionally add ship_id, rank and faction (interstellar house/empire).",
	},
	// new parent genres (round 3) — descriptions for the extra-field hints
	"graphic_novel": {
		"可补充 visual_motif（视觉母题/象征色）、panel_voice_note（旁白语气）与 signature_prop（标志性道具）。",
		"Optionally add visual_motif (recurring symbol/color), panel_voice_note (narration voice) and signature_prop.",
	},
	"superhero": {
		"可补充 power_limitation（能力代价/弱点）、public_identity_layer（公开身份伪装）与 rogues_gallery_ties（宿敌渊源）。",
		"Optionally add power_limitation (cost/weakness), public_identity_layer and rogues_gallery_ties (archenemy history).",
	},
	"scifi:superhero": {
		"可补充 power_cost_or_limitation（代价/弱点）、collateral_damage_debt（附带损害债务）与 public_opinion_metronome（舆论压力）。",
		"Optionally add power_cost_or_limitation, collateral_damage_debt and public_opinion_metronome.",
	},
	"scifi:steampunk": {
		"可补充 gearwork_gadget（蒸汽机关装置）、guild_apprenticeship（行会师承）与 airship_or_factory_tie（飞艇/工厂关联）。",
		"Optionally add gearwork_gadget, guild_apprenticeship and airship_or_factory_tie.",
	},
	"horror:dark_academia": {
		"可补充 secret_society_membership（秘密社团席位）、advisor_dependence（导师依附关系）与 transcript_scandal（学籍丑闻）。",
		"Optionally add secret_society_membership, advisor_dependence and transcript_scandal.",
	},
	"adventure:swashbuckler": {
		"可补充 dueling_record（决斗战绩）、corsair_letter_or_brand（私掠状或海盗烙印）与 crew_loyalty（对船员的忠诚）。",
		"Optionally add dueling_record, corsair_letter_or_brand and crew_loyalty.",
	},
	"adventure:nautical": {
		"可补充 ship_station_duty（船上值守职责）、seasickness_or_scars（晕船/旧伤）与 mutiny_stance（对哗变的态度）。",
		"Optionally add ship_station_duty, seasickness_or_scars and mutiny_stance.",
	},
	"adventure:picaresque": {
		"可补充 grift_of_the_week（本周骗局）、patron_or_pursuer（靠山或追兵）与 disguise_alias（伪装化名）。",
		"Optionally add grift_of_the_week (current con), patron_or_pursuer and disguise_alias.",
	},
	"horror:gothic": {
		"可补充 estate_curse（宅邸诅咒）、family_sin（家族罪孽）与 madness_marker（理智崩解的征兆）。",
		"Optionally add estate_curse, family_sin and madness_marker (signs of unraveling sanity).",
	},
	"scifi:time_travel": {
		"可补充 temporal_anchor_device（时间锚定装置）、paradox_exposure（已触发的悖论）与 fixed_point_obsession（执念中的固定时间点）。",
		"Optionally add temporal_anchor_device, paradox_exposure and fixed_point_obsession.",
	},
	"scifi:cli_fi": {
		"可补充 water_rights_portfolio（水权资产）、drought_year_count（经历的旱灾年数）与 dam_or_aquifer_tie（与水坝/含水层的关联）。",
		"Optionally add water_rights_portfolio, drought_year_count and dam_or_aquifer_tie.",
	},
	"scifi:afrofuturism": {
		"可补充 ancestral_technique（祖传技术/技艺）、nation_or_clan（所属民族或氏族）与 diaspora_connection（与流散故土的联系）。",
		"Optionally add ancestral_technique, nation_or_clan and diaspora_connection.",
	},
	"scifi:hard": {
		"可补充 physics_constraint_role（受物理约束的职能）、life_support_dependency（生命维持依赖度）与 experiment_stakes（实验赌注）。",
		"Optionally add physics_constraint_role, life_support_dependency and experiment_stakes.",
	},
	"mystery:police_procedural": {
		"可补充 case_file_role（案件分工）、partner_history（搭档渊源）与 chain_of_command_friction（与上级的摩擦）。",
		"Optionally add case_file_role, partner_history and chain_of_command_friction.",
	},
	"literary": {
		"可补充 wound_or_obsession（旧伤或执念）、self_deception（自我欺骗）与 what_they_wont_say（绝不肯说出口的事）。",
		"Optionally add wound_or_obsession, self_deception and what_they_wont_say.",
	},
	"romance:romcom": {
		"可补充 meet_cute_story（相遇糗事）、embarrassment_superpower（社死体质）与 blind_spot_about_themselves（对自己感情的盲区）。",
		"Optionally add meet_cute_story, embarrassment_superpower and blind_spot_about_themselves.",
	},
}

// GenreCharacterExtraFields returns the extra JSON keys and a bilingual
// description ("zh", "en") for the given genre key. Empty genreKey (no match)
// yields no extras — behavior identical to before this feature existed.
func GenreCharacterExtraFields(genreKey string, zh bool) (keys []string, desc string) {
	keys = genreCharacterExtraFields[genreKey]
	descKey := genreKey
	if len(keys) == 0 {
		// composite "parent:subgenre" key without its own entry -> fall back to parent
		if i := strings.IndexByte(genreKey, ':'); i >= 0 {
			parent := genreKey[:i]
			if pk := genreCharacterExtraFields[parent]; len(pk) > 0 {
				keys, descKey = pk, parent
			}
		}
		if len(keys) == 0 {
			return nil, ""
		}
	}
	pair := genreCharacterExtraDesc[descKey]
	if zh {
		return keys, pair[0]
	}
	return keys, pair[1]
}

// SubgenreCharacterExtraFields returns character extra fields suggested by the
// free-form subgenre text (case-insensitive substring match against known
// subgenre presets). Returns nil when nothing matches, so callers fall back to
// the parent-genre fields — suggestions never force anything.
func SubgenreCharacterExtraFields(subgenre string) []string {
	t := strings.ToLower(strings.TrimSpace(subgenre))
	if t == "" {
		return nil
	}
	for key, fields := range subgenreCharacterExtraFields {
		if strings.Contains(t, key) {
			return fields
		}
	}
	return nil
}

// —— Anime / manhwa / game-genre modifier presets (round 5) ——————————————————
// Single source of truth per modifier: [conflict scales..., "|", protagonists..., "|", specific settings...].

// ParentGenreKeys lists the selectable parent genres for the Story-type select.
var ParentGenreKeys = []string{
	"fantasy", "scifi", "mystery", "romance", "thriller", "horror",
	"historical", "western", "adventure", "literary",
}

// MatchParentGenreKey resolves a free-form story Type (or an explicit parent
// genre key coming from the UI select) to one of ParentGenreKeys, or "" when
// nothing matches. Exact key match wins first (the UI stores canonical keys),
// then keyword matching via MatchGenreKey mapped back to a parent.
func MatchParentGenreKey(storyType string) string {
	t := strings.ToLower(strings.TrimSpace(storyType))
	if t == "" {
		return ""
	}
	for _, k := range ParentGenreKeys {
		if t == k {
			return k
		}
	}
	// "literary fiction" style aliases and legacy composite keys ("scifi:hard")
	if i := strings.Index(t, ":"); i > 0 {
		t = t[:i]
	}
	for _, k := range ParentGenreKeys {
		if t == k {
			return k
		}
	}
	switch k := MatchGenreKey(storyType); k {
	case "fantasy", "wuxia", "xianxia", "romantasy", "urban", "cozy":
		return "fantasy"
	case "scifi", "cyberpunk", "solarpunk", "space_opera", "dystopian", "postapo", "steampunk", "time_travel", "cli_fi", "afrofuturism", "hard_scifi", "superhero", "litrpg":
		return "scifi"
	case "mystery", "mystery_police":
		return "mystery"
	case "romance":
		return "romance"
	case "thriller", "heist", "spy":
		return "thriller"
	case "horror", "gothic", "dark_academia":
		return "horror"
	case "historical":
		return "historical"
	case "western":
		return "western"
	case "adventure", "swashbuckler", "nautical", "picaresque", "military":
		return "adventure"
	case "ya", "middle_grade", "graphic_novel":
		return "literary"
	}
	return ""
}

// GenreListKey resolves the story Type (+ optional explicit parent key coming
// from the UI select) to the map key used by the per-genre option lists
// (GenreConflictScales, GenreProtagonistTypes, GenreSpecificSettings). It
// prefers the parent-genre key when one matches, then falls back to the
// specific MatchGenreKey (litrpg, cyberpunk, ...) so legacy keys keep working.
func GenreListKey(storyType, parentKey string) string {
	if p := strings.TrimSpace(parentKey); p != "" {
		for _, k := range ParentGenreKeys {
			if k == strings.ToLower(p) {
				return k
			}
		}
	}
	if k := MatchParentGenreKey(storyType); k != "" {
		return k
	}
	return MatchGenreKey(storyType)
}

// SubgenresByParentGenre maps each parent genre key to its subgenre options
// (adapted from NovelWriter's populate_subgenres). The UI adds a free-text
// "other" escape hatch on top of these lists.
var SubgenresByParentGenre = map[string][]string{
	"fantasy":   {"High Fantasy", "Dark Fantasy", "Urban Fantasy", "Sword and Sorcery", "Mythic Fantasy", "Fairy Tale", "Epic Fantasy", "Progression Fantasy", "Portal Fantasy", "Romantasy", "Wuxia", "Xianxia", "Isekai", "LitRPG", "Dungeon Core"},
	"scifi":     {"Space Opera", "Hard Sci-Fi", "Cyberpunk", "Time Travel", "Post-Apocalyptic", "Biopunk", "Solarpunk", "Steampunk", "Cli-Fi", "Afrofuturism", "Dystopian", "Superhero", "First Contact"},
	"mystery":   {"Cozy Mystery", "Hard-boiled Detective", "Police Procedural", "Amateur Sleuth", "Legal Thriller", "Forensic Mystery", "Noir", "Cold Case", "Locked Room", "Historical Puzzle"},
	"romance":   {"Contemporary Romance", "Historical Romance", "Paranormal Romance", "Romantic Suspense", "Regency Romance", "Romantic Comedy", "Sports Romance", "Mafia Romance", "Fantasy Romance"},
	"thriller":  {"Espionage Thriller", "Psychological Thriller", "Action Thriller", "Techno-Thriller", "Medical Thriller", "Legal Thriller", "Domestic Thriller", "Political Thriller", "Heist"},
	"horror":    {"Gothic Horror", "Psychological Horror", "Supernatural Horror", "Body Horror", "Cosmic Horror", "Slasher", "Folk Horror", "Haunted House", "Monster Horror"},
	"historical": {"Ancient History", "Medieval", "Renaissance", "Colonial America", "Civil War Era", "World War Era", "Gold Rush", "Dynastic China"},
	"western":   {"Traditional Western", "Weird Western", "Space Western", "Modern Western", "Outlaw Western", "Cattle Drive Western", "Frontier Romance"},
	"adventure": {"Survival", "Expedition", "Swashbuckler", "Nautical", "Picaresque", "Tomb Raiding", "Man vs Nature", "Quest"},
	"literary":  {"Family Saga", "Coming-of-age", "Historical Literary", "Psychorealism", "Metafiction", "Magical Realism"},
}

// The "|" separators are stripped at init; every list ends with "other" so the
// UI always offers a free-form escape hatch and nothing is ever forced.
var animeModifierPresets = map[string][]string{
	"isekai": {
		"Return-Home vs New World Ties", "Summoner's Contract Debt", "World Rules vs Cheated Knowledge", "Kingdom Saved From Scripted Doom", "other", "|",
		"Reincarnated Office Worker", "Trapped Game Developer", "Average Student With One Weird Perk", "Summoned Chosen One Who Refuses the Role", "other", "|",
		"summoning_contract_rules", "cheat_ability_with_cost", "new_world_language_customs_gap", "return_home_option_live", "local_politics_knowledge_gap", "other",
	},
	"isekai:villainess": {
		"Scripted Doom vs Free Will", "Court Intrigue Survival", "Fiance Annulment Pressure", "Family Honor vs Personal Truth", "other", "|",
		"Reborn Villainess Rewriting Her Fate", "Side Character Who Knows the Ending", "Maid Turned Power Broker", "Exiled Heir Building a Quiet Base", "other", "|",
		"otome_game_script_known_events", "duelist_and_ballroom_etiquette", "curse_or_engagement_deadline", "servant_network_intelligence", "other",
	},
	"isekai:otome": {
		"Capture Target Affection Race", "Breakable Heroine Flag", "Game Logic vs Real Hearts", "Villain Route Temptation", "other", "|",
		"Player Trapped as the Heroine", "Dev Who QA'd This Exact Route", "Handwritten-Route Speedrunner", "Love-Interest Aware They Are a Character", "other", "|",
		"affection_meter_visible_or_implied", "event_flags_reset_on_death", "capture_targets_have_agendas", "bad_end_costs_real", "other",
	},
	"manhwa_system": {
		"System Demands vs Player Autonomy", "Solo Ranking Climb", "Guild Politics on Top Rankers", "World Patching Itself Against the MC", "other", "|",
		"Solo Leveler Type Hunter", "Reluctant System Host", "Ranker Burned Out by Grinding", "NPC Who Gained a Player Window", "other", "|",
		"status_window_rules", "penalty_quest_enforcement", "hunter_rank_public_registry", "hidden_skill_market", "other",
	},
	"regression_rebirth": {
		"Future Knowledge vs Butterfly Drift", "Second-Chance Debt Repayment", "Old Enemies Young Bodies", "Prophecy That Already Failed Once", "other", "|",
		"Regressor One Life Ahead", "Veteran Trapped in Rookie Days", "Martyr Given Another Timeline", "Villain Who Remembers Dying", "other", "|",
		"fixed_past_events_catalogue", "butterfly_effect_budget", "aging_body_young_face_tension", "insider_trading_knowledge_edge", "other",
	},
	"revenge": {
		"Long Con vs Immediate Justice", "Institutional Corruption Wall", "Collateral Damage Creep", "Target Becomes Mirror of Self", "other", "|",
		"Patient Architect of Ruin", "Wrongly Convicted Returnee", "Bodyguard Turned Avenger", "Heir Dispossessed and Rebuilt", "other", "|",
		"evidence_chain_over_years", "identity_change_after_fall", "moral_line_defined_early", "allies_gathered_one_debt_at_a_time", "other",
	},
	"mecha": {
		"Pilot vs Machine Cost", "Fleet War Attrition", "Military Chain vs Conscience", "Colony vs Earth Sovereignty", "AI Cockpit Ghost", "other", "|",
		"Reluctant Child Pilot", "Test Pilot Borrowed Time", "Enemy Ace With Family Back Home", "Mechanic Who Ends Up In the Cockpit", "other", "|",
		"mech_sync_cost_to_pilot", "production_line_logistics", "mobile_suit_vs_frame_doctrine", "war_broadcast_propaganda", "other",
	},
	"mahou_shoujo": {
		"Secret Identity vs Friendship", "Monster-of-the-Week Escalation", "Witch-Knight Rivalry", "Corrupted Mentor Revelation", "Cute Aesthetic Dark Stakes", "other", "|",
		"Ordinary Middle-Schooler Recruited", "Retired Magical Girl Mentoring", "Rival Witch With Sympathetic Goal", "Contract Familiar With Hidden Agenda", "other", "|",
		"transformation_sequence_rules", "wish_granting_contract_cost", "team_color_coded_roles", "city_masked_by_magical_filter", "other",
	},
	"slice_of_life": {
		"Small Change vs Comfort Zone", "Seasonal Deadline Warmth", "Friend Group Drifting Apart", "Newcomer Adapting to Town", "Quiet Personal Standard", "other", "|",
		"New Kid Finding Their People", "Overworked Clerk Learning to Rest", "Club President Holding a Fading Club Together", "Grandchild Running a Family Shop", "other", "|",
		"low_stakes_no_world_threat", "food_weather_and_routine_texture", "found_family_slow_burn", "episodic_season_structure", "other",
	},
	"iyashikei": {
		"Healing vs Lingering Loss", "Stranger Trust After Hurt", "Letting Go of an Old Post", "Community Acceptance Gap", "other", "|",
		"Burnout City Worker in a Rural Post", "Grieving Baker Reopening the Shop", "Wandering Repair-Person", "Retired Adventurer Keeping an Inn", "other", "|",
		"gentle_narrative_no_villain", "nature_hot_spring_food_motifs", "small_regulars_cast_roster", "seasonal_ritual_structure", "other",
	},
	"school_life": {
		"Exam Hierarchy Pressure", "Club Competition Stakes", "Rumor Mill vs Reputation", "First Love Timing Farce", "Class Divide Inside One School", "other", "|",
		"Delinquent With Secret Good Grades", "Student Council Idealist", "Transferred-in Observer", "Club Manager Not On the Team", "other", "|",
		"term_calendar_drives_plot", "uniform_and_seating_codes", "cultural_festival_arc_beats", "teacher_administration_friction", "other",
	},
	"spokon": {
		"Talent vs Discipline", "Rival Generation Handoff", "Body Breaking Before Dream", "Team Ego vs Common Goal", "other", "|",
		"Untaught Country Transfer", "Third-Year Last Season", "Walk-on Bench Grinder", "Former Pro-Turned-Coach Failure", "other", "|",
		"training_montage_discipline", "match_by_match_bracket_structure", "sport_rule_authenticity", "physical_limits_plot_driver", "other",
	},
	"harem": {
		"Oblivious Heart vs Many Claims", "Rival Confessions Pile-Up", "Group Cohesion Under Jealousy", "Chosen One Duty vs Chosen Partner", "other", "|",
		"Accidentally-Lucky Everyman", "Kind Oblivious Protagonist", "Fixer Who Attracts Without Trying", "Reluctant Center of a Found Polycule", "other", "|",
		"cast_archetype_spread", "flag_reading_comedy", "jealousy_without_villainy", "relationship_negotiation_rules", "other",
	},
	"ecchi": {
		"Dignity vs Fan-Service Gravity", "Misreading Every Signal", "Rival Tease Escalation", "Serious Mission Absurd Interruptions", "other", "|",
		"Pervert With a Hidden Code", "Straight-Man Surrounded by Chaos", "Unwitting Harem Magnet", "Professional Who Cannot Take a Joke", "other", "|",
		"comic_timing_over_logic", "boundaries_consent_humor_rules", "serious_core_underneath_gags", "running_gag_inventory", "other",
	},
	"battle_shonen": {
		"Power Ceiling Ladder", "Nakama Bond vs Lone Wolf Path", "Tournament Bracket Wars", "Legacy Rivalry Across Generations", "Dark Past Redemption Match", "other", "|",
		"Underdog With a Weird Power Rule", "Rival Prodigy Chasing the MC", "Tournament Champion Slipping Down", "Mentor Losing Their Prime", "other", "|",
		"power_system_named_moves", "friendship_as_buff_not_crutch", "arc_by_arc_enemy_ladder", "training_time_skip_rules", "tournament_arc_beat", "other",
	},
	"mahou_shonen": {
		"Spellcraft Ethics vs Raw Talent", "Magic Society Audit", "Mana Exhaustion Real Cost", "Forbidden Spell Temptation", "other", "|",
		"Self-Taught Street Mage", "Academy Dropout Practicing Alone", "Support-Mage Strategist", "Inheritor of a Dying School", "other", "|",
		"spell_formula_learning_curve", "magic_law_enforcement_body", "mana_battery_physical_cost", "dueling_protocol_rules", "other",
	},
	"dungeon": {
		"Floor-by-Floor Descent", "Party Trust Under Loot Pressure", "Dungeon Ecology Turns Hostile", "Core Heart Ownership War", "other", "|",
		"Porter Turned Delver", "Floor Guide With Secrets", "Trap Specialist Paranoid Genius", "Monster Whisperer Party Oddball", "other", "|",
		"floor_ecology_tables", "safe_room_checkpoint_rules", "party_composition_meta", "boss_pattern_telegraphing", "other",
	},
	"battle_royale": {
		"Trust Alliances Hour by Hour", "Zone Clock vs Morality", "Spectator Economy Pressure", "Last Two Former Friends", "other", "|",
		"Volunteer Entering to Win Money for Family", "Disgraced Veteran Entered by Force", "Troller Playing Both Sides", "Analyst Predicting the Meta Live", "other", "|",
		"shrinking_map_timer", "supply_drop_schedule", "audience_vote_power", "loadout_balance_rules", "other",
	},
	"tower_climbing": {
		"Gate Trial Difficulty Spike", "Climber Guild Cartel", "Floor Guardians Moral Gray", "Summit Myth vs Cost Below", "other", "|",
		"Solo Climber Starting at Floor One", "Porter Scouting Ahead of Rankers", "Regression-Clear Speedrunner", "Architect Who Designed a Floor", "other", "|",
		"floor_theme_rotation", "gatekeeper_boss_contract", "checkpoint_resurrection_rules", "vertical_city_politics", "other",
	},
}

func buildAnimeModifierPresets() {
	for key, raw := range animeModifierPresets {
		var conflicts, protags, settings []string
		section := 0
		for _, item := range raw {
			if item == "|" {
				section++
				continue
			}
			switch section {
			case 0:
				conflicts = append(conflicts, item)
			case 1:
				protags = append(protags, item)
			default:
				settings = append(settings, item)
			}
		}
		GenreConflictScales[key] = conflicts
		GenreProtagonistTypes[key] = protags
		GenreSpecificSettings[key] = settings
	}
}

func init() { buildAnimeModifierPresets() }

// AnimeModifierMatchWords maps each animeModifierPresets key to lowercase
// keywords matched against Type+Subgenre text (any language). Order matters
// only within a key; keys are checked longest-first by MatchAnimeModifierKey.
var AnimeModifierMatchWords = map[string][]string{
	"shounen_generic":    {"shonen", "shounen", "少年", "소년"},
	"isekai:villainess":  {"villainess", "恶毒女配", "悪役令嬢", "빌런"},
	"isekai:otome":       {"otome", "乙女", "オトメ", "레디메이드"},
	"manhwa_system":      {"solo leveling", "hunter", "헌터", "시스템", "system flow manhwa"},
	"regression_rebirth": {"regression", "rebirth flow", "환생", "重生流", "回档"},
	"mahou_shoujo":       {"magical girl", "mahou shoujo", "magic girl", "魔法少女", "마법소녀"},
	"mahou_shonen":       {"mahou shounen", "magic boy", "魔法少年"},
	"battle_shonen":      {"battle shonen", "battle shounen", "战斗少年", "热血格斗"},
	"slice_of_life":      {"slice of life", "slice-of-life", "日常系", "일상물"},
	"iyashikei":          {"iyashikei", "healing story", "治愈系动漫", "힐링물"},
	"school_life":        {"school life", "校园恋爱", "学園", "학교생활"},
	"tower_climbing":     {"tower climb", "tower of god", "登塔", "타워"},
	"battle_royale":      {"battle royale", "大逃杀", "배틀로얄"},
	"dungeon":            {"dungeon", "던전", "地牢", "副本"},
	"litrpg_dedupe":      {}, // placeholder never used
	"mecha":              {"mecha", "gundam", "evangelion", "机甲", "robot anime", "MS"},
	"harem":              {"harem", "后宫", "ハーレム", "하렘"},
	"ecchi":              {"ecchi", "service", "エッチ"},
	"spokon":             {"spokon", "sports anime", "运动番", "スポ根", "体育会"},
	"revenge":            {"revenge", "复仇", "復讐", "복수"},
	"isekai":             {"isekai", "異世界", "transmigration", "other world", "穿越异世", "이세계"},
}

// MatchAnimeModifierKey returns the modifier preset key whose keywords appear
// in the given free-form genre/subgenre text, or "" when nothing matches.
func MatchAnimeModifierKey(text string) string {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return ""
	}
	// check longer keys first so villainess/otome win over plain isekai
	order := []string{"isekai:villainess", "isekai:otome", "manhwa_system", "regression_rebirth",
		"mahou_shoujo", "mahou_shonen", "battle_shonen", "slice_of_life", "iyashikei",
		"school_life", "tower_climbing", "battle_royale", "dungeon", "mecha", "harem",
		"ecchi", "spokon", "revenge", "shounen_generic", "isekai"}
	for _, k := range order {
		for _, w := range AnimeModifierMatchWords[k] {
			if w != "" && strings.Contains(t, strings.ToLower(w)) {
				return k
			}
		}
	}
	return ""
}
