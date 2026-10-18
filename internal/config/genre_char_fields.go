package config

import (
	"fmt"
	"sort"
	"strings"
)

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

var genreCharacterExtraDesc = map[string]string{
	"fantasy":     "Optionally add magic_school (their magical discipline/school) and affiliation (organization or faction).",
	"scifi":       "Optionally add ship_id (assigned ship/base), rank (military or corporate rank) and implants (cybernetics).",
	"mystery":     "Optionally add alibi (their usual whereabouts/alibi) and secret_knowledge (hidden information they hold).",
	"romance":     "Optionally add love_history (past relationships) and attachment_style (secure/anxious/avoidant...).",
	"thriller":    "Optionally add handler_role (intelligence function, e.g. case officer/informant) and cover_identity.",
	"horror":      "Optionally add haunting_ties (link to the horror's origin) and survival_odds (what keeps them alive).",
	"historical":  "Optionally add social_class (period social standing) and faction (family/party allegiance).",
	"western":     "Optionally add trade (occupation) and reputation (how they are known on the frontier).",
	"litrpg":      "Optionally add class_build (class/build) and level_progression (current level & growth path).",
	"urban":       "Optionally add cover_life (daytime ordinary identity) and hidden_society_ties (links to the masquerade world).",
	"military":    "Optionally add unit_assignment (unit/platoon) and mos_specialty (job specialty).",
	"postapo":     "Optionally add scavenger_skill (salvage/survival trade) and settlement (home camp).",
	"adventure":   "Optionally add expedition_role (role in the party/expedition) and specialty_skill.",
	"wuxia":       "Optionally add sect, cultivation_stage (internal-energy realm) and signature_technique.",
	"xianxia":     "Optionally add sect, cultivation_stage (realm) and dantian_element (spirit root).",
	"cozy":        "Optionally add small_town_role and hobby_or_trade (baking, gardening...).",
	"heist":       "Optionally add crew_role (lockpicker, driver...) and cover_identity.",
	"cyberpunk":   "Optionally add implants, corp_affiliation and street_name (handle).",
	"solarpunk":   "Optionally add coop_role (cooperative job) and rewilding_project.",
	"romantasy":   "Optionally add bond_or_curse and court_role (fae/god court position).",
	"dystopian":   "Optionally add caste_record (assigned caste/file status) and resistance_ties.",
	"space_opera": "Optionally add ship_id, rank and faction (interstellar house/empire).",
	// new parent genres (round 3) — descriptions for the extra-field hints
	"graphic_novel":             "Optionally add visual_motif (recurring symbol/color), panel_voice_note (narration voice) and signature_prop.",
	"superhero":                 "Optionally add power_limitation (cost/weakness), public_identity_layer and rogues_gallery_ties (archenemy history).",
	"scifi:superhero":           "Optionally add power_cost_or_limitation, collateral_damage_debt and public_opinion_metronome.",
	"scifi:steampunk":           "Optionally add gearwork_gadget, guild_apprenticeship and airship_or_factory_tie.",
	"horror:dark_academia":      "Optionally add secret_society_membership, advisor_dependence and transcript_scandal.",
	"adventure:swashbuckler":    "Optionally add dueling_record, corsair_letter_or_brand and crew_loyalty.",
	"adventure:nautical":        "Optionally add ship_station_duty, seasickness_or_scars and mutiny_stance.",
	"adventure:picaresque":      "Optionally add grift_of_the_week (current con), patron_or_pursuer and disguise_alias.",
	"horror:gothic":             "Optionally add estate_curse, family_sin and madness_marker (signs of unraveling sanity).",
	"scifi:time_travel":         "Optionally add temporal_anchor_device, paradox_exposure and fixed_point_obsession.",
	"scifi:cli_fi":              "Optionally add water_rights_portfolio, drought_year_count and dam_or_aquifer_tie.",
	"scifi:afrofuturism":        "Optionally add ancestral_technique, nation_or_clan and diaspora_connection.",
	"scifi:hard":                "Optionally add physics_constraint_role, life_support_dependency and experiment_stakes.",
	"mystery:police_procedural": "Optionally add case_file_role, partner_history and chain_of_command_friction.",
	"literary":                  "Optionally add wound_or_obsession, self_deception and what_they_wont_say.",
	"romance:romcom":            "Optionally add meet_cute_story, embarrassment_superpower and blind_spot_about_themselves.",
}

// GenreCharacterExtraFields returns the extra JSON keys and an English
// description for the given genre key. Empty genreKey (no match)
// yields no extras — behavior identical to before this feature existed.
func GenreCharacterExtraFields(genreKey string) (keys []string, desc string) {
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
	return keys, genreCharacterExtraDesc[descKey]
}

// SubgenreCharacterExtraFields returns character extra fields suggested by the
// free-form subgenre text. The text may hold a COMMA-SEPARATED LIST of
// subgenres ("Cozy Mystery, Noir"): each part is matched independently and the
// union of their fields is returned, deduplicated in first-seen order.
// Matching is case-insensitive; a part also matches when it contains a known
// preset key as a phrase ("locked room impossible crime" -> "cozy mystery"
// style keys stay exact-ish via substring). Returns nil when nothing matches,
// so callers fall back to the parent-genre fields — suggestions never force
// anything.
func SubgenreCharacterExtraFields(subgenre string) []string {
	parts := splitSubgenreList(subgenre)
	if len(parts) == 0 {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, t := range parts {
		for key, fields := range subgenreCharacterExtraFields {
			if strings.Contains(t, key) {
				for _, f := range fields {
					if !seen[f] {
						seen[f] = true
						out = append(out, f)
					}
				}
			}
		}
	}
	return out
}

// SplitSubgenreList normalizes a stored subgenre value into its individual
// entries: comma/semicolon/slash/newline separated, lowercased, trimmed,
// empties dropped. Exported so prompt builders can iterate every selected
// subgenre (multi-select support).
func SplitSubgenreList(subgenre string) []string { return splitSubgenreList(subgenre) }

func splitSubgenreList(subgenre string) []string {
	raw := strings.FieldsFunc(strings.ToLower(subgenre), func(r rune) bool {
		return r == ',' || r == ';' || r == '/' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, p := range raw {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// SubgenreExtraFieldsSnapshot exposes the raw subgenre-label -> extra-fields
// table for API serialization (/api/novel-params -> subgenre_catalogs). The
// HTTP layer ships ALL parameters and lets the frontend filter them per
// selected genre; this copy keeps the internal map unexported-safe.
func SubgenreExtraFieldsSnapshot() map[string][]string {
	out := make(map[string][]string, len(subgenreCharacterExtraFields))
	for k, v := range subgenreCharacterExtraFields {
		out[k] = append([]string{}, v...)
	}
	return out
}

// —— Anime / manhwa / game-genre modifier presets (round 5) ——————————————————
// Single source of truth per modifier: [conflict scales..., "|", protagonists..., "|", specific settings...].
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
	// round 10: erotica as a first-class subgenre (was only a preset key, so
	// it never appeared in the modifier panel and left conflict/protagonist
	// dropdowns empty when selected).
	"erotica": {
		"Desire vs Consequence", "Boundaries Negotiated in Real Time", "Intimacy vs Reputation Risk", "Fantasy Colliding With Feelings", "other", "|",
		"Confident Adult Owning Their Wants", "Couple Redefining an Old Agreement", "Newcomer Exploring Consent First", "Estranged Partners Rekindling", "other", "|",
		"consent_checkpoints_as_beats", "adult_characters_only", "intimacy_drives_plot", "aftermath_and_emotional_texture", "other",
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
		GenreSpecificSettings[key] = ensureTrailingOther(settings)
	}
}

// ensureTrailingOther guarantees the round-5 invariant that every preset list
// ends with the "other" escape hatch. The raw preset tables already place
// "other" last, but later passes (buildSubgenreLabelPresets,
// enforceSingleParentSettingTokens) can drop it when the sentinel token is
// treated as a duplicate owned by another genre key — this restores it.
func ensureTrailingOther(list []string) []string {
	if len(list) == 0 || list[len(list)-1] != "other" {
		list = append(list, "other")
	}
	return list
}

// dramaSubgenreKeys lists the Drama parent-genre subgenres that need dedicated
// conflict/protagonist/settings presets (round 13 audit). Their content mirrors
// SUBGENRE_DATA in frontend/src/lib/novelwriterGenres.js so both sides stay in
// sync. Kept as a slice (not a map) because Go forbids referencing other maps
// during package initialization — buildDramaSubgenrePresets runs from init().
var dramaSubgenreKeys = []string{
	"family saga", "coming-of-age", "psychorealism", "metafiction",
	"existentialist fiction", "philosophical", "historical saga", "legal drama",
	"medical drama", "sports drama", "war drama", "courtroom drama",
	"domestic drama", "epistolary", "tragedy", "melodrama", "satire", "allegory",
	"bildungsroman",
}

var dramaSubgenrePresets = map[string][]string{
	"family saga": {
		"Silence Inside a Family", "Inheritance Torn Apart", "Old Wounds Surfacing Now", "Duty vs Personal Truth", "other", "|",
		"Matriarch Holding the Line Together", "Black Sheep Returning", "Eldest Child Bearing Expectations", "Adopted Member Seeking Origin", "other", "|",
		"multi_generational_timeline", "inheritance_and_estate", "secrets_across_eras", "found_family_or_bloodline", "other",
	},
	"coming-of-age": {
		"Identity Unraveling", "Friendship Fraying Under Change", "Expectation vs Self", "First Real Consequence", "other", "|",
		"Teen Testing Adulthood", "Recent Graduate Adrift", "Young Protege Outgrowing a Mentor", "Quiet Kid Finding a Voice", "other", "|",
		"threshold_season", "first_autonomy", "peer_and_mentor_influence", "small_world_losing_borders", "other",
	},
	"psychorealism": {
		"Self vs Meaning", "Small Compromise Becoming Defining", "Loneliness in Plain Sight", "Truth Avoided for Years", "other", "|",
		"Everyday Professional Near a Breaking Point", "Observer Reading a Room", "Caregiver Losing and Finding Themselves", "Neighbor Living an Unremarkable Crisis", "other", "|",
		"ordinary_life_texture", "interior_perception_over_plot", "quiet_revelations", "social_detail_precision", "other",
	},
	"metafiction": {
		"Control vs the Story's Autonomy", "Life Imitating the Draft", "Which Layer Is Real", "Finishing What Should Stay Open", "other", "|",
		"Writer Trapped Inside Their Premise", "Character Sensing the Page", "Editor Holding Two Truths", "Reader Who Becomes a Source", "other", "|",
		"story_about_story_making", "author_surrogate_present", "layered_narrative_levels", "reader_awareness_devices", "other",
	},
	"existentialist fiction": {
		"Self vs Meaning", "Bad Faith vs Authentic Act", "Grief Without Explanation", "Freedom's Nausea", "other", "|",
		"Stranger to Their Own Life", "Man Cornered by Contingency", "Woman Choosing Without Grounds", "Intellectual Facing the Void", "other", "|",
		"absurd_everyday", "radical_freedom_weight", "meaning_without_promises", "choice_defines_self", "other",
	},
	"philosophical": {
		"Truth vs Comfort", "Ethics Colliding With Loyalty", "Two Right Answers", "Knowledge That Cannot Be Un-known", "other", "|",
		"Teacher Facing Their Own Doctrine", "Debater Changing Mid-Argument", "Thinker Living the Hypothesis", "Skeptic Won by Experience", "other", "|",
		"idea_as_plot_driver", "dialogue_and_debate", "thought_experiment_frames", "ethics_under_pressure", "other",
	},
	"historical saga": {
		"Civilization Building", "Family Divisions Across Generations", "Tradition vs Upheaval", "Legacy Weighing on the Young", "other", "|",
		"Patriarch or Matriarch Anchoring Eras", "Heir Carrying a Name Across Wars", "Emigrant Rebuilding Elsewhere", "Servant Witnessing Great Houses Fall", "other", "|",
		"era_defining_events", "family_across_nations", "social_change_as_backdrop", "multi_generational_timeline", "other",
	},
	"legal drama": {
		"Justice Corruption", "Client vs Counsel Interests", "Duty vs Conscience", "Winning vs Doing Right", "other", "|",
		"Defense Attorney Betting Reputation", "Public Defender Overloaded", "Prosecutor Choosing Justice or Record", "Junior Lawyer Given Impossible File", "other", "|",
		"law_firm_internal_politics", "ethics_bar_pressure", "client_truth_shifting", "courtroom_drama", "other",
	},
	"medical drama": {
		"Scarce Resources Real People", "Protocol vs Mercy", "Career vs Patient Truth", "Burnout at the Bedside", "other", "|",
		"Resident Learning Limits", "Attending Haunted by One Case", "Nurse Holding the Ward Together", "Surgeon Facing Their Own Error", "other", "|",
		"hospital_hierarchy", "triage_ethics", "shift_based_pacing", "patient_story_arcs", "other",
	},
	"sports drama": {
		"Talent vs Discipline", "Team Ego vs Common Goal", "Dream vs Body's Limits", "Rival Generation Handoff", "other", "|",
		"Veteran Last Season", "Untaught Rookie", "Coach Redeeming Through a Team", "Athlete Body Betraying Them", "other", "|",
		"season_structure", "training_regimen", "team_egos_and_roles", "career_window_pressure", "other",
	},
	"war drama": {
		"Unit Survival", "Chain-of-Command Conflict", "War vs Conscience", "Insurgency & Occupation", "other", "|",
		"Junior Officer", "Sergeant NCO", "Medic", "Conscript Turned Veteran", "Civilian Under Occupation", "other", "|",
		"front_and_home_front", "chain_of_command", "moral_attrition", "survivor_guilt", "other",
	},
	"courtroom drama": {
		"Reasonable Doubt vs Belief", "Truth Suppressed by Procedure", "One Verdict Many Lives", "Fair Trial Under Pressure", "other", "|",
		"Lead Counsel", "Holdout Juror", "Expert Witness With a Stake", "Court Clerk Seeing Everything", "other", "|",
		"trial_procedure", "jury_and_testimony", "objection_timing", "public_opinion_pressure", "other",
	},
	"domestic drama": {
		"Routine Cracking Under News", "Loyalty Between Partners", "Money Stress Rewriting Roles", "What a Household Pretends", "other", "|",
		"Parent Holding Two Jobs and a Secret", "Spouse Rebalancing After Loss", "Sibling Forced Into Guardian Role", "Adult Child Moving Back Home", "other", "|",
		"single_household_stage", "everyday_financial_pressure", "caregiving_load", "unspoken_arrangements", "other",
	},
	"epistolary": {
		"Distance Kept by Words", "Misreading Written Tone", "The Letter Never Sent", "Truth Emerging Out of Order", "other", "|",
		"Correspondent Writing Twice Before Sending", "Archivist Ordering the Letters", "Diarist Hiding From Themselves", "Forum Poster Revealed by Timestamps", "other", "|",
		"letters_or_messages_format", "documents_as_scenes", "missing_pieces_between_pages", "multiple_voices_no_narrator", "other",
	},
	"tragedy": {
		"Fate vs Character", "Right Act Too Late", "Pride Preceding the Fall", "Love That Cannot Save", "other", "|",
		"Capable Person With One Fatal Blind Spot", "Leader Choosing Pride Once Too Often", "Lover Whose Devotion Becomes Ruin", "Inheritor Repeating a Parent's Mistake", "other", "|",
		"flaw_driven_downfall", "inevitability_build", "high_stakes_personal", "catharsis_ending", "other",
	},
	"melodrama": {
		"Forbidden Desire vs Duty", "Exposed Hypocrisy", "Reunion After Long Suffering", "Vindication Against Odds", "other", "|",
		"Wronged Innocent Fighting Back", "Passionate Rebel Against a Household", "Secret Heir Restored", "Sacrificial Caregiver Finally Seen", "other", "|",
		"heightened_emotion", "clear_villainy_or_wrong", "twists_and_reversals", "sensory_spectacle", "other",
	},
	"satire": {
		"Hypocrisy vs Reality", "Punching at Authority", "Self-destruction of a Movement", "Decency in a Corrupt Game", "other", "|",
		"Naive Participant in a Ridiculous System", "Cynic Commentating From Inside", "True Believer Whose Faith Breaks Comedy", "Bureaucrat Perfecting Absurdity", "other", "|",
		"institutional_absurdity", "exaggerated_types", "ironic_narrative_distance", "targets_of_power", "other",
	},
	"allegory": {
		"Virtue vs Institutional Sin", "Path Chosen vs Path Assigned", "Truth Under a Thin Disguise", "Freedom Against Structure", "other", "|",
		"Figure Carrying an Idea", "Everyman on a Designed Road", "Representative of a Class or Nation", "Seeker in a Modeled World", "other", "|",
		"symbolic_world_logic", "second_reading_meaning", "types_not_only_individuals", "moral_geometry", "other",
	},
	"bildungsroman": {
		"Inherited Beliefs Questioned", "Ambition vs Integrity", "Belonging on One's Own Terms", "Education by Failure", "other", "|",
		"Orphan Making Their Own Rules", "Provincial Arriving in the City", "Apprentice Surpassing a Master", "Quiet Child Becoming a Witness", "other", "|",
		"formative_years_span", "mentors_and_bad_examples", "self_made_identity", "society_shaping_the_person", "other",
	},
}

// buildDramaSubgenrePresets splits the drama subgenre presets into the same
// three Genre* maps used by /api/novel-params, so selecting e.g. "Medical
// Drama" fills the Conflict/Protagonist/Specific-settings dropdowns exactly
// like any anime modifier preset does.
func buildDramaSubgenrePresets() {
	for key, raw := range dramaSubgenrePresets {
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
		GenreSpecificSettings[key] = ensureTrailingOther(settings)
	}
}

// —— Round 13 audit: first-class subgenres promoted from cross-cutting
// modifiers ————————————————————————————————————————————————————————————————
// The user asked these to be selectable directly as Subgenres (wuxia/xianxia/
// romantasy/isekai/revenge/mahou shoujo/mahou shonen/spokon already covered by
// earlier rounds; this adds the remaining ones requested: dungeon core via the
// "dungeon" key, battle royale, slice of life, ecchi, mecha, erotica...). Each
// entry registers its title-cased display label in SubgenrePresetKeys and
// merges its signature setting tokens into its ONE canonical parent genre
// (subgenreLabelParents). Single-parent rule (round 17): every subgenre belongs
// to exactly one parent, mirroring the frontend GENRES lists in
// frontend/src/lib/novelwriterGenres.js — hybrid/crossover labels no longer
// repeat across parents.
var subgenreLabelParents = map[string][]string{
	"Wuxia":                  {"fantasy"},
	"Xianxia":                {"fantasy"},
	"LitRPG":                 {"fantasy"},
	"Isekai":                 {"fantasy"},
	"Dungeon Core":           {"fantasy"},
	"Tower Climbing":         {"fantasy"},
	"Regression":             {"fantasy"},
	"Villainess":             {"fantasy"},
	"Otome":                  {"fantasy"},
	"Mahou Shoujo":           {"fantasy"},
	"Mahou Shonen":           {"fantasy"},
	"Battle Royale":          {"scifi"},
	"Mecha":                  {"scifi"},
	"Superhero":              {"scifi"},
	"Romantasy":              {"romance"},
	"Erotica":                {"romance"},
	"Harem":                  {"romance"},
	"Revenge":                {"mystery_thriller"},
	"Slice of Life":          {"drama"},
	"School Life":            {"drama"},
	"Spokon":                 {"drama"},
	"Iyashikei":              {"drama"},
	"Ecchi":                  {"drama"},
	"Cyberpunk":              {"scifi"},
	"Steampunk":              {"scifi"},
	"Solarpunk":              {"scifi"},
	"Space Opera":            {"scifi"},
	"Dystopian":              {"scifi"},
	"Post-Apocalyptic":       {"scifi"},
	"Time Travel":            {"scifi"},
	"First Contact":          {"scifi"},
	"Heist":                  {"mystery_thriller"},
	"Noir":                   {"mystery_thriller"},
	"Gothic":                 {"horror"},
	"Dark Academia":          {"horror"},
	"Swashbuckler":           {"adventure"},
	"Nautical":               {"adventure"},
	"Picaresque":             {"adventure"},
	"Rom-Com":                {"romance"},
	"Cozy":                   {"mystery_thriller"},
	"Urban Fantasy":          {"fantasy"},
	"Magical Realism":        {"fantasy"},
	"Progression Fantasy":    {"fantasy"},
	"Epic Fantasy":           {"fantasy"},
	"High Fantasy":           {"fantasy"},
	"Dark Fantasy":           {"fantasy"},
	"Sword and Sorcery":      {"fantasy"},
	"Hard Sci-Fi":            {"scifi"},
	"Biopunk":                {"scifi"},
	"Cli-Fi":                 {"scifi"},
	"Afrofuturism":           {"scifi"},
	"Folk Horror":            {"horror"},
	"Cosmic Horror":          {"horror"},
	"Haunted House":          {"horror"},
	"Zombie Apocalypse":      {"scifi"},
	"Police Procedural":      {"mystery_thriller"},
	"Locked Room":            {"mystery_thriller"},
	"Hard-Boiled":            {"mystery_thriller"},
	"Espionage Thriller":     {"mystery_thriller"},
	"Techno-Thriller":        {"mystery_thriller"},
	"Political Thriller":     {"mystery_thriller"},
	"Legal Thriller":         {"mystery_thriller"},
	"Domestic Thriller":      {"mystery_thriller"},
	"Psychological Thriller": {"mystery_thriller"},
	"Action Thriller":        {"mystery_thriller"},
	"Psychological Mystery":  {"mystery_thriller"},
	"Historical Mystery":     {"mystery_thriller"},
	"Amateur Detective":      {"mystery_thriller"},
	"Cozy Mystery":           {"mystery_thriller"},
	"Historical Romance":     {"romance"},
	"Contemporary Romance":   {"romance"},
	"Paranormal Romance":     {"romance"},
	"Romantic Suspense":      {"romance"},
	"Small Town Romance":     {"romance"},
	"Erotic Romance":         {"romance"},
	"Sports Romance":         {"romance"},
	"Mafia Romance":          {"romance"},
	"Suspense Romance":       {"romance"},
	"Alien Invasion":         {"scifi"},
	"Space Western":          {"western"},
	"Weird Western":          {"western"},
	"Classic Western":        {"western"},
	"Outlaw Western":         {"western"},
	"Cattle Drive Western":   {"western"},
	"Modern Western":         {"western"},
	"Traditional Western":    {"western"},
	"Spaghetti Western":      {"western"},
	"Ancient History":        {"historical"},
	"Medieval":               {"historical"},
	"Renaissance":            {"historical"},
	"Colonial America":       {"historical"},
	"Civil War Era":          {"historical"},
	"World War Era":          {"historical"},
	"Regency":                {"historical"},
	"Gold Rush":              {"western"},
	"Military":               {"military"},
	"Military Fantasy":       {"fantasy"},
	"Portal Fantasy":         {"fantasy"},
	"Mythic Fantasy":         {"fantasy"},
	"Fairy Tale":             {"fantasy"},
	"Noblebright":            {"fantasy"},
	"Grimdark":               {"fantasy"},
	"Body Horror":            {"horror"},
	"Slasher Horror":         {"horror"},
	"Supernatural Horror":    {"horror"},
	"Psychological Horror":   {"horror"},
	"Gothic Horror":          {"horror"},
	"Space Adventure":        {"scifi"},
	"Lost World":             {"adventure"},
	"Survival":               {"adventure"},
	"Exploration":            {"adventure"},
	"Disaster":               {"adventure"},
	"War Epic":               {"military"},
	"Sea Adventure":          {"adventure"},
	"Treasure Hunt":          {"adventure"},
	"Jungle Expedition":      {"adventure"},
	"Mountain Climb":         {"adventure"},
	"Desert Trek":            {"adventure"},
	"Polar Expedition":       {"adventure"},
	"Sky Pirate":             {"adventure"},
	"Road Trip":              {"adventure"},
	"Coming-of-age":          {"drama"},
	"Family Saga":            {"drama"},
	"Psychorealism":          {"drama"},
	"Metafiction":            {"drama"},
	"Existentialist Fiction": {"drama"},
	"Philosophical":          {"drama"},
	"Historical Saga":        {"drama"},
	"Legal Drama":            {"drama"},
	"Medical Drama":          {"drama"},
	"Sports Drama":           {"drama"},
	"War Drama":              {"drama"},
	"Courtroom Drama":        {"drama"},
	"Domestic Drama":         {"drama"},
	"Epistolary":             {"drama"},
	"Tragedy":                {"drama"},
	"Melodrama":              {"drama"},
	"Satire":                 {"drama"},
	"Allegory":               {"drama"},
	"Bildungsroman":          {"drama"},
}

// buildSubgenreLabelPresets registers each display label above in the genre
// preset maps: appends the subgenre's own snake_case token to every compatible
// parent genre's specific-settings list (so the checkbox appears there), and
// ensures the label itself is a known subgenre preset. Runs after
// buildAnimeModifierPresets so labels can also point at those keys.
func buildSubgenreLabelPresets() {
	for label, parents := range subgenreLabelParents {
		token := strings.ToLower(strings.ReplaceAll(label, " ", "_"))
		for _, parent := range parents {
			// Only merge into genres that actually exist as presets; unknown
			// parent names are silently skipped (they act as free-form tags).
			list := GenreSpecificSettings[parent]
			if len(list) == 0 {
				continue
			}
			found := false
			for _, x := range list {
				if strings.EqualFold(x, token) {
					found = true
					break
				}
			}
			if !found {
				// Keep the trailing "other" escape hatch last (round-5 invariant).
				if len(list) > 0 && list[len(list)-1] == "other" {
					merged := make([]string, 0, len(list)+1)
					merged = append(merged, list[:len(list)-1]...)
					merged = append(merged, token, "other")
					GenreSpecificSettings[parent] = merged
				} else {
					GenreSpecificSettings[parent] = append(list, token)
				}
			}
		}
	}
}

func init() {
	buildAnimeModifierPresets()
	buildDramaSubgenrePresets()
	buildSubgenreLabelPresets()
	buildCanonicalSubgenreParents()
	enforceSingleParentSettingTokens()
}

// canonicalSubgenreParents maps every frontend GENRES subgenre label (see
// frontend/src/lib/novelwriterGenres.js) to its EXACTLY ONE parent genre key.
// It is generated at init() from subgenreLabelParents plus frontendOnlySubgenre
// entries for labels that exist only in the frontend catalog; duplicates are a
// hard error (panic) so the taxonomy can never silently regress. The frontend
// derives SUBGENRE_PARENT from its own copy of the same lists and validates it
// at module load with an equivalent invariant.
var canonicalSubgenreParents = map[string]string{}

// frontendOnlySubgenre returns the canonical parent for labels present in the
// frontend GENRES catalog but absent from subgenreLabelParents (which only
// covers labels with backend presets). Keys are lowercase display labels.
func frontendOnlySubgenre(label string) (string, bool) {
	p, ok := frontendOnlySubgenreParents[strings.ToLower(label)]
	return p, ok
}

// frontendOnlySubgenreParents completes the taxonomy for frontend catalog
// labels without dedicated backend presets. Values mirror the `genre` field of
// SUBGENRE_DATA in novelwriterGenres.js (the two catalogs share the same 185
// labels; this map covers the ones subgenreLabelParents does not name).
var frontendOnlySubgenreParents = map[string]string{
	// fantasy
	"high fantasy": "fantasy", "epic fantasy": "fantasy", "sword and sorcery": "fantasy",
	"dark fantasy": "fantasy", "grimdark": "fantasy", "noblebright": "fantasy",
	"mythic fantasy": "fantasy", "fairy tale": "fantasy", "fairy tale retelling": "fantasy",
	"magical realism": "fantasy", "progression fantasy": "fantasy", "portal fantasy": "fantasy",
	"military fantasy": "fantasy", "science fantasy": "fantasy", "technomagic": "fantasy",
	"cyberfantasy": "fantasy", "gamelit": "fantasy", "mahou shoujo": "fantasy",
	"mahou shonen": "fantasy", "arthurian legend": "fantasy", "norse mythology": "fantasy",
	"greek mythology": "fantasy", "egyptian mythology": "fantasy", "slavic folklore": "fantasy",
	"asian folklore": "fantasy", "gods among us": "fantasy", "legend / epic poem": "fantasy",
	// scifi
	"alien invasion": "scifi", "steampunk": "scifi", "biopunk": "scifi", "dieselpunk": "scifi",
	"cli-fi": "scifi", "afrofuturism": "scifi", "cozy sci-fi": "scifi", "system apocalypse": "scifi",
	"vrmmo": "scifi", "base building": "scifi", "kingdom building": "scifi",
	// mystery_thriller
	"hard-boiled": "mystery_thriller", "cold case": "mystery_thriller",
	"organized crime / mafia": "mystery_thriller", "gangster epic": "mystery_thriller",
	"detective noir": "mystery_thriller", "legal crime drama": "mystery_thriller",
	"true-crime style": "mystery_thriller", "psychological thriller": "mystery_thriller",
	"domestic thriller": "mystery_thriller", "legal thriller": "mystery_thriller",
	"political thriller": "mystery_thriller", "espionage thriller": "mystery_thriller",
	"techno-thriller": "mystery_thriller", "medical thriller": "mystery_thriller",
	"eco-thriller": "mystery_thriller", "conspiracy thriller": "mystery_thriller",
	"action thriller": "mystery_thriller",
	// romance
	"contemporary romance": "romance", "historical romance": "romance", "regency romance": "romance",
	"romantic suspense": "romance", "paranormal romance": "romance", "small town romance": "romance",
	"sports romance": "romance", "mafia romance": "romance", "erotic romance": "romance",
	// adventure
	"adventure quest": "adventure", "lost world": "adventure", "treasure hunt": "adventure",
	"swashbuckler": "adventure", "picaresque": "adventure", "nautical / sea voyage": "adventure",
	"expedition / exploration": "adventure", "jungle expedition": "adventure",
	"desert crossing": "adventure", "mountain climbing": "adventure", "polar exploration": "adventure",
	"underground / subterranean": "adventure", "road trip": "adventure", "man vs nature": "adventure",
	"disaster": "adventure", "martial arts": "adventure", "spy action": "adventure",
	// horror
	"gothic horror": "horror", "cosmic horror": "horror", "psychological horror": "horror",
	"body horror": "horror", "slasher horror": "horror", "supernatural horror": "horror",
	"folk horror": "horror", "haunted house": "horror", "vampire": "horror", "werewolf": "horror",
	"monster horror": "horror", "occult ritual": "horror",
	// historical
	"ancient history": "historical", "medieval": "historical", "renaissance": "historical",
	"colonial america": "historical", "civil war era": "historical", "world war era": "historical",
	"regency": "historical", "victorian": "historical", "interwar": "historical",
	"cold war": "historical", "dynastic china": "historical", "feudal japan": "historical",
	"viking age": "historical",
	// western
	"classic western": "western", "traditional western": "western", "modern western": "western",
	"spaghetti western": "western", "weird western": "western", "space western": "western",
	"outlaw western": "western", "cattle drive western": "western", "gold rush": "western",
	"frontier noir": "western",
	// drama
	"coming-of-age": "drama", "family saga": "drama", "historical saga": "drama",
	"psychorealism": "drama", "domestic drama": "drama", "legal drama": "drama",
	"medical drama": "drama", "sports drama": "drama", "war drama": "drama", "tragedy": "drama",
	"melodrama": "drama", "existentialist fiction": "drama", "philosophical": "drama",
	"metafiction": "drama", "epistolary": "drama", "slice of life": "drama",
	"school life": "drama", "iyashikei": "drama",
	// comedy
	"parody": "comedy", "farce": "comedy", "sitcom-style ensemble": "comedy",
	"dark comedy": "comedy", "absurdist humor": "comedy", "workplace comedy": "comedy",
	"road-trip comedy": "comedy", "paranormal comedy": "comedy",
}

// buildCanonicalSubgenreParents assembles the full label->parent taxonomy and
// panics on any conflicting/duplicate assignment (single-parent invariant).
// Backend preset buckets use the legacy key "mystery" for the merged
// "Mystery & Thriller" parent (config.MatchGenreKey maps both "mystery" and
// "thriller" to it); canonical parents are normalized to the frontend GENRES
// keys so callers can compare against selectedGenreKeys directly.
func buildCanonicalSubgenreParents() {
	normalize := func(p string) string {
		if p == "mystery" || p == "thriller" {
			return "mystery_thriller"
		}
		// The backend preset bucket "military" (war stories) is not one of the
		// frontend GENRES parent keys; its canonical parent is Drama, which owns
		// "War Drama" and the other conflict-driven war subgenres.
		if p == "military" || p == "war" {
			return "drama"
		}
		return p
	}
	for label, parents := range subgenreLabelParents {
		if len(parents) != 1 {
			panic(fmt.Sprintf("config: subgenre %q has %d parents; each subgenre must belong to exactly one parent genre", label, len(parents)))
		}
		lc := strings.ToLower(label)
		p := normalize(parents[0])
		if prev, dup := canonicalSubgenreParents[lc]; dup && prev != p {
			panic(fmt.Sprintf("config: subgenre %q assigned to both %q and %q", label, prev, p))
		}
		canonicalSubgenreParents[lc] = p
	}
	for lc, parent := range frontendOnlySubgenreParents {
		p := normalize(parent)
		if prev, dup := canonicalSubgenreParents[lc]; dup {
			if prev != p {
				panic(fmt.Sprintf("config: subgenre %q assigned to both %q and %q", lc, prev, p))
			}
			continue
		}
		canonicalSubgenreParents[lc] = p
	}
}

// CanonicalParentForSubgenre returns the single canonical parent-genre key for
// a subgenre display label or snake_case token ("Cozy Mystery", "isekai",
// "locked_room_impossible_crime"), or "" when the label is unknown. This is the
// authoritative cross-check used by GetNovelParams to guarantee the UI never
// surfaces a subgenre under more than one parent genre.
func CanonicalParentForSubgenre(s string) string {
	key := normalizeGenreKeyString(s)
	if key == "" {
		return ""
	}
	if p, ok := canonicalSubgenreParents[key]; ok {
		return p
	}
	// Tokens may carry extra descriptive words ("locked_room_impossible_crime"
	// for "Locked Room"); try progressively shorter prefixes of the token.
	parts := strings.Split(key, "_")
	for n := len(parts) - 1; n >= 1; n-- {
		if p, ok := canonicalSubgenreParents[strings.Join(parts[:n], "_")]; ok {
			return p
		}
	}
	return ""
}

// CanonicalSubgenreParents returns a copy of the full label/token -> canonical
// parent-genre map (lowercase display labels and their snake_case variants).
// Shipped to the frontend via /api/novel-params -> subgenre_parents.
func CanonicalSubgenreParents() map[string]string {
	out := make(map[string]string, len(canonicalSubgenreParents)*2)
	for lc, parent := range canonicalSubgenreParents {
		out[lc] = parent
		out[normalizeGenreKeyString(lc)] = parent
	}
	return out
}

func normalizeGenreKeyString(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
			lastUnderscore = false
		default: // spaces, hyphens, slashes, punctuation...
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

// enforceSingleParentSettingTokens guarantees the single-parent rule on the
// data the frontend actually consumes (/api/novel-params -> specific_settings):
// a subgenre's snake_case setting token may live under exactly ONE parent genre
// key. Earlier rounds merged tokens into several parents for hybrid labels, so
// selecting e.g. "Mystery & Thriller" surfaced Fantasy subgenres in the
// dropdown. When a token appears under multiple parents, it is kept only under
// its canonical one (canonicalSubgenreParents; otherwise the first
// alphabetically) and removed from the rest. Runs after all preset builders in
// init().
func enforceSingleParentSettingTokens() {
	keys := make([]string, 0, len(GenreSpecificSettings))
	for k := range GenreSpecificSettings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	owners := map[string][]string{} // token -> sorted genre keys holding it
	for _, k := range keys {
		for _, tok := range GenreSpecificSettings[k] {
			owners[tok] = append(owners[tok], k)
		}
	}
	for _, k := range keys {
		cleaned := make([]string, 0, len(GenreSpecificSettings[k]))
		for _, tok := range GenreSpecificSettings[k] {
			holders := owners[tok]
			if len(holders) <= 1 {
				cleaned = append(cleaned, tok)
				continue
			}
			want := CanonicalParentForSubgenre(tok)
			if want == "" || !containsString(holders, want) {
				want = holders[0] // keys iterated sorted: first alphabetically wins
			}
			if k == want {
				cleaned = append(cleaned, tok)
			}
			// else: foreign duplicate of a token owned by another genre — drop it.
		}
		GenreSpecificSettings[k] = ensureTrailingOther(cleaned)
	}
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// AnimeModifierMatchWords maps each animeModifierPresets key to lowercase
// keywords matched against Type+Subgenre text (any language). Order matters
// only within a key; keys are checked longest-first by MatchAnimeModifierKey.
var AnimeModifierMatchWords = map[string][]string{
	"shounen_generic":    {"shonen", "shounen", "소년"},
	"isekai:villainess":  {"villainess", "빌런"},
	"isekai:otome":       {"otome", "オトメ", "레디메이드"},
	"manhwa_system":      {"solo leveling", "hunter", "헌터", "시스템", "system flow manhwa"},
	"regression_rebirth": {"regression", "rebirth flow", "환생"},
	"mahou_shoujo":       {"magical girl", "mahou shoujo", "magic girl", "마법소녀"},
	"mahou_shonen":       {"mahou shounen", "magic boy"},
	"battle_shonen":      {"battle shonen", "battle shounen"},
	"slice_of_life":      {"slice of life", "slice-of-life", "일상물"},
	"iyashikei":          {"iyashikei", "healing story", "힐링물"},
	"school_life":        {"school life", "학교생활"},
	"tower_climbing":     {"tower climb", "tower of god", "타워"},
	"battle_royale":      {"battle royale", "배틀로얄"},
	"dungeon":            {"dungeon", "던전"},
	"litrpg_dedupe":      {}, // placeholder never used
	"mecha":              {"mecha", "gundam", "evangelion", "robot anime", "MS"},
	"harem":              {"harem", "ハーレム", "하렘"},
	"ecchi":              {"ecchi", "service", "エッチ"},
	"spokon":             {"spokon", "sports anime"},
	"revenge":            {"revenge", "복수"},
	"isekai":             {"isekai", "transmigration", "other world", "이세계"},
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
