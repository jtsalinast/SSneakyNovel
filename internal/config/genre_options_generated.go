package config

// GENERATED FILE — genre option presets adapted from the NovelWriter project
// (core/config/genre_configs/*), merged per parent genre. Hand-edit with care:
// these lists feed /api/novel-params and the "Novel parameters" UI tab.

// GenreSpecificSettingOptions lists the checkbox options offered per parent genre
// (adapted from NovelWriter implied_settings; first 12 unique per genre).
// The user's own GenreSpecificSettings string always takes precedence over this map.
var GenreSpecificSettingOptions = map[string][]string{
	"fantasy": { "magic_system", "medieval_setting", "mythical_creatures", "epic_scale", "grim_world", "dangerous_magic", "moral_ambiguity", "supernatural_threats", "hidden_magic", "modern_world", "supernatural_society", "masquerade" },
	"scifi": { "interstellar_travel", "advanced_technology", "multiple_species", "space_combat", "scientific_accuracy", "realistic_physics", "near_future_tech", "timeframes", "locations", "tech_levels", "sociopolitical_contexts", "environments" },
	"mystery": { "small_community", "amateur_detective", "low_violence", "puzzle_focus", "recurring_characters", "law_enforcement", "realistic_investigation", "team_work", "forensic_science", "bureaucracy", "noir_atmosphere", "cynical_world" },
	"romance": { "modern_setting", "relationship_focus", "emotional_journey", "happy_ending", "realistic_world", "period_setting", "historical_accuracy", "period_constraints", "social_conventions", "authentic_details", "supernatural_elements", "otherworldly_beings" },
	"thriller": { "international_intrigue", "spy_networks", "government_secrets", "double_agents", "global_stakes", "advanced_technology", "cyber_warfare", "scientific_threats", "high_tech_weapons", "digital_espionage", "government_corruption", "political_intrigue" },
	"horror": { "atmospheric_dread", "isolated_setting", "supernatural_elements", "psychological_terror", "dark_atmosphere", "unknowable_entities", "existential_dread", "cosmic_scale", "sanity_loss", "ancient_powers", "mental_terror", "reality_questioning" },
	"historical": { "ancient_civilizations", "mythological_elements", "tribal_societies", "ancient_religions", "primitive_technology", "feudal_system", "medieval_society", "chivalric_code", "religious_influence", "period_authenticity", "artistic_renaissance", "scientific_revolution" },
	"western": { "frontier_setting", "lawlessness", "honor_code", "survival_focus", "horse_culture", "moral_ambiguity", "violence", "anti_hero", "stylized_action", "cynical_world", "supernatural_elements", "horror_aspects" },
}

// GenreToneOptions lists tone suggestions per parent genre (adapted from
// NovelWriter tones; first 14 unique). The Tone field remains free-text editable.
var GenreToneOptions = map[string][]string{
	"fantasy": { "Epic", "Heroic", "Noble", "Mythic", "Adventure", "Grim", "Morally Ambiguous", "Horror-tinged", "Psychological", "Brutal", "Modern", "Mysterious", "Action-packed", "Detective Noir" },
	"scifi": { "Optimistic", "Dark", "Political", "Adventure-focused", "Character-driven", "Technical", "Philosophical", "Discovery-focused", "Methodical", "Realistic", "Noir", "Gritty", "Anti-establishment", "Dystopian" },
	"mystery": { "Gentle", "Puzzle-focused", "Community-centered", "Cozy", "Character-driven", "Realistic", "Procedural", "Professional", "Methodical", "Team-focused", "Noir", "Cynical", "Gritty", "Dark" },
	"romance": { "Optimistic", "Emotional", "Realistic", "Heartwarming", "Character-driven", "Romantic", "Historical", "Elegant", "Passionate", "Period-appropriate", "Supernatural", "Mysterious", "Dark Romance", "Fantasy-driven" },
	"thriller": { "Sophisticated", "International", "High-stakes", "Political", "Covert", "High-tech", "Fast-paced", "Technical", "Futuristic", "Digital", "Investigative", "Institutional", "Conspiratorial", "Power-focused" },
	"horror": { "Atmospheric", "Psychological", "Brooding", "Mysterious", "Melancholic", "Existential", "Unknowable", "Cosmic", "Dread-filled", "Mind-breaking", "Disturbing", "Mind-bending", "Paranoid", "Introspective" },
	"historical": { "Epic", "Mythological", "Primitive", "Spiritual", "Ancient", "Authentic", "Chivalric", "Religious", "Feudal", "Period-appropriate", "Artistic", "Intellectual", "Political", "Cultural" },
	"western": { "Heroic", "Moral", "Action-packed", "Traditional", "Justice-focused", "Gritty", "Cynical", "Stylized", "Morally Ambiguous", "Violent", "Horror-tinged", "Mysterious", "Supernatural", "Gothic" },
}
