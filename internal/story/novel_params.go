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
var structureDescriptions = map[string][2]string{ // [es, en]
	"three_act": {
		"Estructura en tres actos: Acto I (~25%) planteamiento e incidente incitador; Acto II (~50%) conflicto creciente, punto de giro central y punto bajo; Acto III (~25%) clímax y desenlace.",
		"Three-act structure: Act I (~25%) setup and inciting incident; Act II (~50%) rising conflict, midpoint reversal, low point; Act III (~25%) climax and resolution.",
	},
	"six_act": {
		"Estructura en seis partes: inicio (mundo ordinario), evento detonante, acción creciente, prueba/giro a mitad, crisis previa al clímax, resolución.",
		"Six-part structure: beginning (ordinary world), trigger event, rising action, midpoint test/turn, pre-climax crisis, resolution.",
	},
	"fichtean": {
		"Estructura Fichtean de cinco crisis: una cadena de crisis escalantes, cada una con planteamiento breve, conflicto creciente y momento de decisión; planifica la trama hacia atrás desde el clímax.",
		"Fichtean five-crisis structure: a chain of escalating crises, each with brief setup, rising conflict and decision moment; plot backwards from the climax.",
	},
	"freytag": {
		"Pirámide de Freytag: exposición, acción creciente, clímax en el centro, acción descendente, desenlace/catástrofe.",
		"Freytag's pyramid: exposition, rising action, climax at the middle, falling action, denouement/catastrophe.",
	},
	"seven_point": {
		"Estructura de siete puntos: gancho (0%), primer giro argumental (15%), presión 1 (35%), punto medio (50%), segundo giro (65%), presión 2 (85%), resolución (100%).",
		"Seven-point structure: hook (0%), first plot turn (15%), pinch 1 (35%), midpoint (50%), plot turn 2 (65%), pinch 2 (85%), resolution (100%).",
	},
	"heros_journey": {
		"El viaje del héroe (12 etapas): mundo ordinario, llamada a la aventura, negativa, mentor, cruce del umbral, pruebas/aliados/enemigos, aproximación, calvario, recompensa, camino de regreso, resurrección, retorno con el elixir.",
		"Hero's journey (12 stages): ordinary world, call to adventure, refusal, mentor, crossing the threshold, tests/allies/enemies, approach, ordeal, reward, road back, resurrection, return with the elixir.",
	},
	"heros_journey_simple": {
		"Viaje del héroe simplificado: partida (mundo ordinario y llamada), iniciación (pruebas y despertar), retorno (transformación y regreso a casa); adecuado para novela corta.",
		"Simplified hero's journey: departure (ordinary world & call), initiation (trials & awakening), return (transformation & homecoming); suits novellas.",
	},
	"save_the_cat": {
		"Beats de Save the Cat: imagen inicial, tema enunciado, planteamiento, catalizador, debate, entrada al mundo nuevo, historia B, diversión y juego, punto medio, se ciernen los villanos, todo está perdido, noche oscura del alma, clímax, imagen final.",
		"Save the Cat beats: opening image, theme staked, setup, catalyst, debate, break into two, B story, fun and games, midpoint, bad guys close in, all is lost, dark night of the soul, finale, final image.",
	},
	"episodic": {
		"Estructura episódica: cada capítulo (o grupo) es una unidad autocontenida (caso/aventura) unida de forma suelta por una trama general que converge con el tiempo.",
		"Episodic structure: each chapter (or group) is a self-contained unit (case/adventure) loosely threaded by an overarching storyline that converges over time.",
	},
	"story_circle": {
		"Círculo narrativo (Dan Harmon): tú → necesitas → entras → te adaptas → buscas → pagas el precio → regresas → cambias. Un bucle de ocho pasos que funciona igual en historias episódicas o serializadas.",
		"Story circle (Dan Harmon): you → need → go → adapt → search → take → return → change. An eight-step loop that works for episodic and serialized stories alike.",
	},
	"kishotenketsu": {
		"Kishōtenketsu (estructura eastasiática de cuatro actos): introducción → desarrollo → giro/sorpresa → reconciliación. El conflicto es opcional; adecuado para slice-of-life, light novels y antologías.",
		"Kishōtenketsu (four-act East-Asian structure): introduction → development → surprise/twist → reconciliation. Conflict is optional; suits slice-of-life, light novels and anthologies.",
	},
	"snowflake": {
		"Método copo de nieve: expande una premisa de una frase por capas (párrafo → lista de escenas → personajes → outline); la densidad de capítulos crece con cada iteración, ideal para epopeyas y libros multiperspectiva.",
		"Snowflake method: expand a one-sentence premise in layers (paragraph → scene list → characters → outline); chapter density grows with each iteration, ideal for epics and multi-POV books.",
	},
	"quest": {
		"Estructura de búsqueda (Quest): un objetivo claro impulsa al protagonista a través de espacios/etapas; las pruebas en el camino revelan la verdad detrás del objetivo y alcanzarlo produce una transformación.",
		"Quest structure: a clear goal drives the protagonist across spaces/stages; trials along the way reveal the truth behind the goal, and reaching it brings transformation.",
	},
	"romance_arc": {
		"Arco romántico: encuentro/atracción → cercanía y malentendidos → crisis de compromiso → separación y punto bajo → despertar y búsqueda → final feliz (o desenlace agridulce); los beats giran en torno a la relación.",
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
		return "microrrelato"
	case config.LengthShort:
		if en {
			return "short story"
		}
		return "relato corto"
	case config.LengthNovelette:
		if en {
			return "novelette"
		}
		return "novelette"
	case config.LengthNovella:
		if en {
			return "novella"
		}
		return "novela corta"
	case config.LengthLightNovel:
		if en {
			return "light novel"
		}
		return "light novel"
	case config.LengthAnthology:
		if en {
			return "anthology"
		}
		return "antología"
	case config.LengthNovel:
		if en {
			return "novel"
		}
		return "novela"
	case config.LengthEpic:
		if en {
			return "epic"
		}
		return "novela épica"
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
		add("[MOTIVO LITERARIO] "+m+" (téjelo como hilo temático/imagen recurrente a lo largo de la trama, los personajes y las escenas; nunca de forma pesada)", "[LITERARY MOTIF] "+m+" (weave it as a recurring image/thematic thread through plot, characters and scenes; never heavy-handed)")
	}
	if b := strings.TrimSpace(sc.StoryIdea); b != "" {
		if en {
			lines = append(lines, "[STORY IDEA - authoritative premise]\n"+b)
		} else {
			lines = append(lines, "[IDEA DE LA HISTORIA - premisa autorizada]\n"+b)
		}
	}
	if v := strings.TrimSpace(sc.ForcesInTension); v != "" {
		add("[FUERZAS EN TENSIÓN] "+v+" (dramatiza estas fuerzas a favor y en contra del tema/tesis de la obra en el conflicto, los bandos y los puntos de giro; ninguna fuerza debe quedar sin oposición)",
			"[FORCES IN TENSION] "+v+" (dramatize these for/against forces of the work's thesis in conflict, factions and turning points; no force should go unanswered)")
	}
	if v := strings.TrimSpace(sc.ElevatorPitch); v != "" {
		add("[ELEVATOR PITCH] "+v+" (gancho north-star de alto concepto: la historia debe poder venderse con esta promesa; conserva su giro central al planificar y escribir)",
			"[ELEVATOR PITCH] "+v+" (north-star high-concept hook: the story must be sellable with this promise; keep its central twist intact when planning and writing)")
	}
	if v := strings.TrimSpace(sc.LighthouseSentence); v != "" {
		add("[FRASE FARO] \""+v+"\" (punto fijo inmutable de la obra: cada arco y capítulo debe navegar hacia ella o alejarse de ella de forma intencional; nunca la contradigas sin que la trama lo pague)",
			"[LIGHTHOUSE SENTENCE] \""+v+"\" (the work's immutable fixed point: every arc and chapter must navigate toward or deliberately away from it; never contradict it without paying for it in plot)")
	}
	if v := strings.TrimSpace(sc.Subgenre); v != "" {
		// Multi-select support: the stored value may be a comma-separated list
		// ("Cozy Mystery, Noir"); every entry is surfaced to the AI.
		for _, part := range config.SplitSubgenreList(v) {
			add("[SUBGÉNERO] "+part, "[SUBGENRE] "+part)
		}
	}
	if v := strings.TrimSpace(sc.Theme); v != "" {
		add("[TEMÁTICA] "+v, "[THEME] "+v)
	}
	if v := strings.TrimSpace(sc.Tone); v != "" {
		add("[TONO] "+v, "[TONE] "+v)
	}
	if v := strings.TrimSpace(sc.PhilosophicalDilemma); v != "" {
		add("[DILEMA FILOSÓFICO/OPERACIONAL] "+v+" (es la tensión central de la obra: haz que personajes, decisiones y consecuencias lo encarnen y lo exploren sin resolverlo de forma simplista)",
			"[PHILOSOPHICAL/OPERATIONAL DILEMMA] "+v+" (this is the work's central tension: make characters, decisions and consequences embody and explore it without a simplistic resolution)")
	}
	if v := strings.TrimSpace(sc.ReaderExpectation); v != "" {
		add("[EXPECTATIVA DEL LECTOR OBJETIVO] "+v+" (cumple las convenciones de género y los motivos obligatorios aquí listados; son compromisos con el lector, no opciones)",
			"[TARGET READER'S EXPECTATION] "+v+" (honor the genre conventions and mandatory motifs listed here; they are promises to the reader, not options)")
	}
	if v := strings.TrimSpace(sc.DifferentiatingIdea); v != "" {
		add("[IDEA DIFERENCIADORA] "+v+" (es el ángulo único que distingue esta obra de su estante de género: protégelo en premisa, giros y voz; evita tropos que lo diluyan)",
			"[DIFFERENTIATING IDEA] "+v+" (this is the unique angle that sets the work apart from its genre shelf: protect it in premise, twists and voice; avoid tropes that dilute it)")
	}
	if v := strings.TrimSpace(sc.InspirationalPieces); v != "" {
		add("[OBRAS DE INSPIRACIÓN] "+v+" (úsalas como referencia de estilo/atmósfera/oficio: toma su tono y técnicas, pero nunca copies sus tramas, personajes ni configuraciones propias)",
			"[INSPIRATIONAL PIECES] "+v+" (use them as style/atmosphere/craft reference points: borrow their tone and techniques, never copy their plots, characters or proprietary settings)")
	}
	if strings.EqualFold(strings.TrimSpace(sc.WorkType), "fanfic") {
		src := strings.TrimSpace(sc.FanficSource)
		fromES, fromEN := "", ""
		if src != "" {
			fromES = " basada en \"" + src + "\""
			fromEN = " based on \"" + src + "\""
		}
		add("[FAN FICTION] Esta obra es fan fiction"+fromES+". Trata los personajes, las reglas del mundo y el canon de la obra original como fijos: mantenlos en carácter, no contradigas el canon sin una razón explícita dentro de la historia y no reproduzcas texto con derechos de autor de forma literal. La trama debe ser original.",
			"[FAN FICTION] This is a fan work"+fromEN+". Treat the source's established characters, world rules and canon as fixed: keep them in character, never contradict canon without an explicit in-story reason, and never reproduce copyrighted text verbatim. The plot must be original.")
	}
	if v := strings.TrimSpace(sc.Author); v != "" {
		add("[AUTOR/AUTORÍA] "+v, "[AUTHOR] "+v)
	}
	if v := strings.TrimSpace(sc.StoryLength); v != "" {
		min, max := config.SuggestedChaptersByLength(v)
		lbl := lengthLabel(v, cfg.Language)
		if min > 0 {
			if en {
				add(fmt.Sprintf("[EXTENSIÓN] %s (total sugerido: ~%d-%d capítulos)", lbl, min, max),
					fmt.Sprintf("[LENGTH] %s (suggested total: ~%d-%d chapters)", lbl, min, max))
			} else {
				add(fmt.Sprintf("[EXTENSIÓN] %s (total sugerido: ~%d-%d capítulos)", lbl, min, max), "")
			}
		} else {
			add("[EXTENSIÓN] "+lbl, "[LENGTH] "+lbl)
		}
	}
	if desc := structureDescription(sc.Structure, cfg.Language); desc != "" {
		lbl := structureLabel(sc.Structure, cfg.Language)
		if en {
			lines = append(lines, "[STRUCTURE] "+lbl+": "+desc)
		} else {
			lines = append(lines, "[ESTRUCTURA] "+lbl+": "+desc)
		}
	}
	if v := strings.TrimSpace(sc.EffectiveConflict()); v != "" {
		add("[ESCALA DEL CONFLICTO] "+v+" (una sugerencia, no una restricción)", "[CONFLICT SCALE] "+v+" (a suggestion, not a constraint)")
	}
	if v := strings.TrimSpace(sc.EffectiveProtagonist()); v != "" {
		add("[TIPO DE PROTAGONISTA] "+v+" (una sugerencia, no una restricción)", "[PROTAGONIST TYPE] "+v+" (a suggestion, not a constraint)")
	}
	if v := strings.TrimSpace(sc.SpecificSettings); v != "" {
		add("[AJUSTES ESPECÍFICOS]\n"+v, "[SPECIFIC SETTINGS]\n"+v)
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
			lines = append(lines, "[MODIFICADOR DE GÉNERO] Esta historia usa el conjunto de convenciones anime/manhwa/juego \""+mod+"\": respeta sus tropos característicos, su ritmo y las expectativas del lector (una sugerencia, no una restricción).")
		}
	}
	// Genre/subgenre flavor: derived from the free-form Type/Subgenre so it also
	// works for genres without presets; suggestions, never constraints.
	if g := strings.TrimSpace(sc.Type); g != "" || strings.TrimSpace(sc.Subgenre) != "" {
		flavor := strings.TrimSpace(g + " " + sc.Subgenre)
		if flavor != "" {
			add("[SABOR DE GÉNERO] Este libro es "+flavor+": respeta las convenciones típicas, las expectativas del lector y los elementos característicos de ese género/subgénero (una sugerencia, no una restricción).",
				"[GENRE FLAVOR] This book is "+flavor+": honor the typical conventions, reader expectations and signature elements of that genre/subgenre (a suggestion, not a constraint).")
		}
	}
	switch bias := strings.TrimSpace(sc.GenderBias); bias {
	case "", "random":
		add("[GÉNERO DE PERSONAJES] Déjalo al azar; no te inclines deliberadamente hacia ningún género.",
			"[CHARACTER GENDER] Leave it to chance; do not deliberately skew toward any gender.")
	case "male":
		add("[GÉNERO DE PERSONAJES] Inclina a los personajes principales hacia el masculino.", "[CHARACTER GENDER] Skew main characters toward male.")
	case "female":
		add("[GÉNERO DE PERSONAJES] Inclina a los personajes principales hacia el femenino.", "[CHARACTER GENDER] Skew main characters toward female.")
	case "balanced":
		add("[GÉNERO DE PERSONAJES] Mantén un reparto principal equilibrado en género.", "[CHARACTER GENDER] Keep main characters gender-balanced.")
	}
	if len(lines) == 0 {
		return ""
	}
	block := "NOVEL PARAMETERS (plan the whole book accordingly):\n" + strings.Join(lines, "\n")
	if !en {
		block = "PARÁMETROS DE LA NOVELA (planifica todo el libro según ellos):\n" + strings.Join(lines, "\n")
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
		return label(en, "Estructura en tres actos", "Three-act structure")
	case "six_act":
		return label(en, "Estructura en seis partes", "Six-part structure")
	case "fichtean":
		return label(en, "Fichtean de cinco crisis", "Fichtean five-crisis")
	case "freytag":
		return label(en, "Pirámide de Freytag", "Freytag's pyramid")
	case "seven_point":
		return label(en, "Estructura de siete puntos", "Seven-point structure")
	case "heros_journey":
		return label(en, "El viaje del héroe", "Hero's journey")
	case "heros_journey_simple":
		return label(en, "Viaje del héroe simplificado", "Simplified hero's journey")
	case "save_the_cat":
		return label(en, "Beats de Save the Cat", "Save the Cat beats")
	case "episodic":
		return label(en, "Episódica", "Episodic")
	case "story_circle":
		return label(en, "Círculo narrativo", "Story circle")
	case "kishotenketsu":
		return label(en, "Kishōtenketsu", "Kishōtenketsu")
	case "snowflake":
		return label(en, "Método copo de nieve", "Snowflake method")
	case "quest":
		return label(en, "Estructura de búsqueda", "Quest structure")
	case "romance_arc":
		return label(en, "Arco romántico", "Romance arc")
	}
	return key
}

func label(en bool, zh, english string) string {
	if en {
		return english
	}
	return zh
}
