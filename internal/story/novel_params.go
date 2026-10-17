package story

import (
	"fmt"
	"strings"

	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
)

// structureDescriptions maps a novel-structure key to short beat descriptions,
// adapted from NovelWriter's structure frameworks. Used to guide outline
// generation when the user picks a story structure in the config.
var structureDescriptions = map[string][2]string{ // [zh, en]
	"three_act": {
		"three: #1(approx25%)laymatanditem, advanceenter#two; #2(approx50%)riselevel, middledotrotate, mostlow; #3(approx25%)highand. ",
		"Three-act structure: Act I (~25%) setup and inciting incident; Act II (~50%) rising conflict, midpoint reversal, low point; Act III (~25%) climax and resolution.",
	},
	"six_act": {
		"six: open(sun), tentaclehairitem, upperrisetravelmove(countand), /middledotrotate, highfrontmachine, . ",
		"Six-part structure: beginning (ordinary world), trigger event, rising action, midpoint test/turn, pre-climax crisis, resolution.",
	},
	"fichtean": {
		"eanfivemachine: byoneconnectnot disconnectriselevel machineadvance, eachmachinefastlaymat, Rising , disconnecthourengrave; fromhighpushcloth. ",
		"Fichtean five-crisis structure: a chain of escalating crises, each with brief setup, rising conflict and decision moment; plot backwards from the climax.",
	},
	"freytag": {
		"goldcharacter-count(Freytag): open→upperrisemovemake→high(middle)→lowerdescendmovemake→/disaster. ",
		"Freytag's pyramid: exposition, rising action, climax at the middle, falling action, denouement/catastrophe.",
	},
	"seven_point": {
		"sevendot: hook(0%)→#onerotatedot(15%)→dot(35%)→middledot(50%)→rotate(65%)→#tworotatedot(85%)→(100%). ",
		"Seven-point structure: hook (0%), first plot turn (15%), pinch 1 (35%), midpoint (50%), plot turn 2 (65%), pinch 2 (85%), resolution (100%).",
	},
	"heros_journey": {
		"of(12stepsparagraph): sun→risk→→meetsee→acrosscrossdoor→alliancefriendenemy→connectneardeep→→reward→→alive→spiritualmedicinecome. ",
		"Hero's journey (12 stages): ordinary world, call to adventure, refusal, mentor, crossing the threshold, tests/allies/enemies, approach, ordeal, reward, road back, resurrection, return with the elixir.",
	},
	"heros_journey_simple": {
		"of: exithair(sunand)→enable(andfeelawake)→come(andreturn), joinmiddleshortpiece. ",
		"Simplified hero's journey: departure (ordinary world & call), initiation (trials & awakening), return (transformation & homecoming); suits novellas.",
	},
	"save_the_cat": {
		"Save the Catsectiontable: opendrawnoodles→→laymat→→→advanceenter#two→B→swimhour→middledot→badeggnear→onenohas→spiritualsouldarknight→realize→high→drawnoodles. ",
		"Save the Cat beats: opening image, theme staked, setup, catalyst, debate, break into two, B story, fun and games, midpoint, bad guys close in, all is lost, dark night of the soul, finale, final image.",
	},
	"episodic": {
		"chaptersection/billyuan: eachchapteroreachgroupchaptersectionforto billyuan(item/risk), onestriprun throughthreadunioncollect. ",
		"Episodic structure: each chapter (or group) is a self-contained unit (case/adventure) loosely threaded by an overarching storyline that converges over time.",
	},
	"story_circle": {
		"circle(Dan Harmon): you(district)→want to→advanceenterlife→should→seek→ topayexitprice→return→. eightring, billyuanandlongpieceuse. ",
		"Story circle (Dan Harmon): you → need → go → adapt → search → take → return → change. An eight-step loop that works for episodic and serialized stories alike.",
	},
	"kishotenketsu": {
		"(fourparagraph): (enter)→(hairlay)→(intentoutsiderotate)→(collectcallshould), nostrongmanufactureto, joinsunis, lightsmallsayandshortpiece. ",
		"Kishōtenketsu (four-act East-Asian structure): introduction → development → surprise/twist → reconciliation. Conflict is optional; suits slice-of-life, light novels and anthologies.",
	},
	"snowflake": {
		"snowflower: fromonesentence premise layer(paragraphpassage→rowtable→→big), chaptersectionsecretdegreealong with, joinandmultithread. ",
		"Snowflake method: expand a one-sentence premise in layers (paragraph → scene list → characters → outline); chapter density grows with each iteration, ideal for epics and multi-POV books.",
	},
	"quest": {
		"chaseseek(Quest): bright metricmovehornacrosscrossempty/stepsparagraphfrontadvance, alongmetricbacklater true, doti.e.. ",
		"Quest structure: a clear goal drives the protagonist across spaces/stages; trials along the way reveal the truth behind the goal, and reaching it brings transformation.",
	},
	"romance_arc": {
		"feelthread: meet/→nearanduntie→machine→divideleavelow→feelawakeand→full(or), sectionrevolve aroundcloseisadvance. ",
		"Romance arc structure: meet/attract → closeness & misreading → commitment crisis → separation low point → awakening & pursuit → happy ending (or bittersweet release); beats turn on the relationship.",
	},
}

// StructureDescriptions exposes the narrative-structure beat summaries
// (key -> [zh, en]) so the HTTP layer can send them to the frontend for
// tooltips. The map values are treated as read-only by callers.
func StructureDescriptions() map[string][2]string { return structureDescriptions }

func structureDescription(key, lang string) string {
	d, ok := structureDescriptions[strings.TrimSpace(key)]
	if !ok {
		return ""
	}
	if i18n.NormalizeLanguage(lang) == i18n.LangEN {
		return d[1]
	}
	return d[0]
}

func lengthLabel(length, lang string) string {
	min, max := config.SuggestedChaptersByLength(length)
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	switch strings.TrimSpace(length) {
	case config.LengthFlash:
		if en {
			return "flash fiction"
		}
		return "smallsay"
	case config.LengthShort:
		if en {
			return "short story"
		}
		return "shortpiece"
	case config.LengthNovelette:
		if en {
			return "novelette"
		}
		return "shortmiddlepiece"
	case config.LengthNovella:
		if en {
			return "novella"
		}
		return "middlepiece"
	case config.LengthLightNovel:
		if en {
			return "light novel"
		}
		return "lightsmallsay"
	case config.LengthAnthology:
		if en {
			return "anthology"
		}
		return "shortpiecesmallsay"
	case config.LengthNovel:
		if en {
			return "novel"
		}
		return "longpiece"
	case config.LengthEpic:
		if en {
			return "epic"
		}
		return "/longpiece"
	}
	if min > 0 && en {
		return fmt.Sprintf("%s (%d-%d chapters suggested)", length, min, max)
	}
	return length
}

// It returns "" when nothing is set, so existing prompts stay unchanged.
// NovelParametersBlock builds the authoritative "novel parameters" prompt block
// (story idea, motif, subgenre, theme, tone, length, structure, conflict,
// protagonist, audience, content guidance...) from the story config. It is
// shared by outline, writing and section generation so every AI feature sees
// the same complete parameter set.
func NovelParametersBlock(cfg *config.Config) string {
	sc := cfg.Story
	en := i18n.NormalizeLanguage(cfg.Language) == i18n.LangEN
	var lines []string
	add := func(zh, english string) {
		if english != "" || zh != "" {
			if en {
				lines = append(lines, english)
			} else {
				lines = append(lines, zh)
			}
		}
	}
	if m := sc.EffectiveMotif(); m != "" {
		add("[learn]"+m+"(pleaseBAitsmakeforrun through intentelephantandthreadcord, weaveentersection, and, not want tolifehardsay)", "[LITERARY MOTIF] "+m+" (weave it as a recurring image/thematic thread through plot, characters and scenes; never heavy-handed)")
	}
	if b := strings.TrimSpace(sc.StoryIdea); b != "" {
		if en {
			lines = append(lines, "[STORY IDEA - authoritative premise]\n"+b)
		} else {
			lines = append(lines, "[think·front]\n"+b)
		}
	}
	if v := strings.TrimSpace(sc.Subgenre); v != "" {
		// Multi-select support: the stored value may be a comma-separated list
		// ("Cozy Mystery, Noir"); every entry is surfaced to the AI.
		for _, part := range config.SplitSubgenreList(v) {
			add("[class]"+part, "[SUBGENRE] "+part)
		}
	}
	if v := strings.TrimSpace(sc.Theme); v != "" {
		add("[]"+v, "[THEME] "+v)
	}
	if v := strings.TrimSpace(sc.Tone); v != "" {
		add("[]"+v, "[TONE] "+v)
	}
	if v := strings.TrimSpace(sc.InspirationalPieces); v != "" {
		add("[spiritualfeelmake]"+v+"(makeforwindgrid, andhandupper sitmetric: borrowitsqiand, butnot want toperitssection, orhas)",
			"[INSPIRATIONAL PIECES] "+v+" (use them as style/atmosphere/craft reference points: borrow their tone and techniques, never copy their plots, characters or proprietary settings)")
	}
	if v := strings.TrimSpace(sc.Author); v != "" {
		add("[make]"+v, "[AUTHOR] "+v)
	}
	if v := strings.TrimSpace(sc.StoryLength); v != "" {
		min, max := config.SuggestedChaptersByLength(v)
		lbl := lengthLabel(v, cfg.Language)
		if min > 0 {
			if en {
				add(fmt.Sprintf("[piece]%s(chapternumberapprox %d-%d chapter)", lbl, min, max),
					fmt.Sprintf("[LENGTH] %s (suggested total: ~%d-%d chapters)", lbl, min, max))
			} else {
				add(fmt.Sprintf("[piece]%s(chapternumberapprox %d-%d chapter)", lbl, min, max), "")
			}
		} else {
			add("[piece]"+lbl, "[LENGTH] "+lbl)
		}
	}
	if desc := structureDescription(sc.Structure, cfg.Language); desc != "" {
		lbl := structureLabel(sc.Structure, cfg.Language)
		if en {
			lines = append(lines, "[STRUCTURE] "+lbl+": "+desc)
		} else {
			lines = append(lines, "[]"+lbl+": "+desc)
		}
	}
	if v := strings.TrimSpace(sc.EffectiveConflict()); v != "" {
		add("[]"+v+"(squaredirection, not strongmanufacture)", "[CONFLICT SCALE] "+v+" (a suggestion, not a constraint)")
	}
	if v := strings.TrimSpace(sc.EffectiveProtagonist()); v != "" {
		add("[hornclass]"+v+"(squaredirection, not strongmanufacture)", "[PROTAGONIST TYPE] "+v+" (a suggestion, not a constraint)")
	}
	if v := strings.TrimSpace(sc.SpecificSettings); v != "" {
		add("[]\n"+v, "[SPECIFIC SETTINGS]\n"+v)
	}
	if v := config.AudienceGuidance(sc.TargetAudience, cfg.Language); v != "" {
		lines = append(lines, v)
	}
	// Anime/manhwa/game modifier conventions: when the text matches a modifier
	// preset (isekai, mecha, slice of life, LitRPG-style system flows...) but no
	// classic genre key resolved, still surface its signature conventions so the
	// outline/writing honor them — as suggestions, never constraints.
	if mod := config.MatchAnimeModifierKey(sc.Type + " " + sc.Subgenre); mod != "" {
		if en {
			lines = append(lines, "[GENRE MODIFIER] This story uses the \""+mod+"\" anime/manhwa/game convention set: honor its signature tropes, pacing and reader expectations (a suggestion, not a constraint).")
		} else {
			lines = append(lines, "[classaccessory]thismakebelthas\""+mod+"\" move/draw/swimisproportion: pleasebodyitsmetricambitionbridgeparagraph, sectionandreadexpect(squaredirection, not strongmanufacture). ")
		}
	}
	// Genre/subgenre flavor: derived from the free-form Type/Subgenre so it also
	// works for genres without presets; suggestions, never constraints.
	if g := strings.TrimSpace(sc.Type); g != "" || strings.TrimSpace(sc.Subgenre) != "" {
		flavor := strings.TrimSpace(g + " " + sc.Subgenre)
		if flavor != "" {
			add("[classwindflavor]thismakebelongs toin\""+flavor+"\": pleaseusetheclass/class proportion, readexpectandmetricambitionyuanplain(squaredirection, not strongmanufacture). ",
				"[GENRE FLAVOR] This book is "+flavor+": honor the typical conventions, reader expectations and signature elements of that genre/subgenre (a suggestion, not a constraint).")
		}
	}
	switch bias := strings.TrimSpace(sc.GenderBias); bias {
	case "", "random":
		add("[other]along withmachinei.e.can, not want toengraveintentdirectionhowother. ",
			"[CHARACTER GENDER] Leave it to chance; do not deliberately skew toward any gender.")
	case "male":
		add("[other]want tohorndirectionfor. ", "[CHARACTER GENDER] Skew main characters toward male.")
	case "female":
		add("[other]want tohorndirectionfor. ", "[CHARACTER GENDER] Skew main characters toward female.")
	case "balanced":
		add("[other]want tohornalldividecloth. ", "[CHARACTER GENDER] Keep main characters gender-balanced.")
	}
	if len(lines) == 0 {
		return ""
	}
	block := "NOVEL PARAMETERS (plan the whole book accordingly):\n" + strings.Join(lines, "\n")
	if !en {
		block = "smallsaynumber(bodywhiskerfollow): \n" + strings.Join(lines, "\n")
	}
	// Story output language (EN/ES): when the author pins a generation language
	// in "Story parameters", every AI text must stay in it — this hard rule is
	// appended to the same authoritative block used by outline, writing and
	// section generation, so mid-generation language switches stop happening.
	switch lang := config.NormalizeOutputLanguage(sc.OutputLanguage); lang {
	case "en":
		block += "\n[OUTPUT LANGUAGE - HARD RULE] Write ALL generated content (story idea, sections, characters, worldview, outline summaries, chapter titles and prose) exclusively in English. Never switch to Spanish, Chinese or any other language mid-output."
	case "es":
		block += "\n[IDIOMA DE SALIDA - REGLA OBLIGATORIA] Todo el contenido generado por la IA (idea de la historia, secciones, personajes, visión del mundo, esquemas, títulos de capítulos y prosa) debe escribirse EXCLUSIVAMENTE en español. Nunca cambies a inglés, chino ni a otro idioma a mitad de la generación."
	}
	return block
}

func structureLabel(structure, lang string) string {
	key := strings.TrimSpace(structure)
	if _, ok := structureDescriptions[key]; !ok {
		return key // free-form value: pass through as-is
	}
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	switch key {
	case "three_act":
		return label(en, "three", "Three-act structure")
	case "six_act":
		return label(en, "six", "Six-part structure")
	case "fichtean":
		return label(en, "eanfivemachine", "Fichtean five-crisis")
	case "freytag":
		return label(en, "goldcharacter-count", "Freytag's pyramid")
	case "seven_point":
		return label(en, "sevendot", "Seven-point structure")
	case "heros_journey":
		return label(en, "of", "Hero's journey")
	case "heros_journey_simple":
		return label(en, "of", "Simplified hero's journey")
	case "save_the_cat":
		return label(en, "Save the Cat sectiontable", "Save the Cat beats")
	case "episodic":
		return label(en, "chaptersection/billyuan", "Episodic")
	case "story_circle":
		return label(en, "circle", "Story circle")
	case "kishotenketsu":
		return label(en, "", "Kishōtenketsu")
	case "snowflake":
		return label(en, "snowflower", "Snowflake method")
	case "quest":
		return label(en, "chaseseek", "Quest structure")
	case "romance_arc":
		return label(en, "feelthread", "Romance arc")
	}
	return key
}

func label(en bool, zh, english string) string {
	if en {
		return english
	}
	return zh
}
