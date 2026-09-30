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
		"三幕结构：第1幕(约25%)铺垫与激励事件、进入第二世界；第2幕(约50%)冲突升级、中点反转、最低谷；第3幕(约25%)高潮与结局。",
		"Three-act structure: Act I (~25%) setup and inciting incident; Act II (~50%) rising conflict, midpoint reversal, low point; Act III (~25%) climax and resolution.",
	},
	"six_act": {
		"六幕结构：开始(日常世界)、触发事件、上升行动(计划与试探)、考验/中点转折、高潮前危机、结局。",
		"Six-part structure: beginning (ordinary world), trigger event, rising action, midpoint test/turn, pre-climax crisis, resolution.",
	},
	"fichtean": {
		"菲希特ean五危机结构：以一连串不断升级的危机推进，每个危机含快速铺垫、 Rising 冲突、决断时刻；从高潮倒推布局。",
		"Fichtean five-crisis structure: a chain of escalating crises, each with brief setup, rising conflict and decision moment; plot backwards from the climax.",
	},
	"freytag": {
		"金字塔结构(Freytag)：开端→上升动作→高潮(居中)→下降动作→结局/灾难。",
		"Freytag's pyramid: exposition, rising action, climax at the middle, falling action, denouement/catastrophe.",
	},
	"seven_point": {
		"七点结构：钩子(0%)→第一转折点(15%)→掐点(35%)→中点(50%)→反转(65%)→第二转折点(85%)→结局(100%)。",
		"Seven-point structure: hook (0%), first plot turn (15%), pinch 1 (35%), midpoint (50%), plot turn 2 (65%), pinch 2 (85%), resolution (100%).",
	},
	"heros_journey": {
		"英雄之旅(12阶段)：日常世界→冒险召唤→犹豫→遇见导师→跨越门槛→试炼盟友敌人→接近深洞→磨难→奖赏→归途→复活→携灵药归来。",
		"Hero's journey (12 stages): ordinary world, call to adventure, refusal, mentor, crossing the threshold, tests/allies/enemies, approach, ordeal, reward, road back, resurrection, return with the elixir.",
	},
	"heros_journey_simple": {
		"简化英雄之旅：出发(日常与召唤)→启蒙(试炼与觉醒)→归来(蜕变与回归)，适合中短篇。",
		"Simplified hero's journey: departure (ordinary world & call), initiation (trials & awakening), return (transformation & homecoming); suits novellas.",
	},
	"save_the_cat": {
		"Save the Cat节拍表：开场画面→主题陈述→铺垫→催化剂→争论→进入第二世界→B故事→游戏时间→中点→坏蛋逼近→一无所有→灵魂暗夜→顿悟→高潮→终场画面。",
		"Save the Cat beats: opening image, theme staked, setup, catalyst, debate, break into two, B story, fun and games, midpoint, bad guys close in, all is lost, dark night of the soul, finale, final image.",
	},
	"episodic": {
		"章节式/单元剧结构：每章或每组章节为相对独立的单元（案件/冒险），由一条贯穿主线逐步串联收束。",
		"Episodic structure: each chapter (or group) is a self-contained unit (case/adventure) loosely threaded by an overarching storyline that converges over time.",
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
	case config.LengthShort:
		if en {
			return "short story"
		}
		return "短篇"
	case config.LengthNovella:
		if en {
			return "novella"
		}
		return "中篇"
	case config.LengthNovel:
		if en {
			return "novel"
		}
		return "长篇"
	case config.LengthEpic:
		if en {
			return "epic"
		}
		return "史诗/超长篇"
	}
	if min > 0 && en {
		return fmt.Sprintf("%s (%d-%d chapters suggested)", length, min, max)
	}
	return length
}

// novelParametersBlock builds an optional prompt block from the story config's
// novel parameters (brief, subgenre, theme, tone, author, length, structure).
// It returns "" when nothing is set, so existing prompts stay unchanged.
func novelParametersBlock(cfg *config.Config) string {
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
	if b := strings.TrimSpace(sc.Brief); b != "" {
		if en {
			lines = append(lines, "[STORY BRIEF - authoritative premise]\n"+b)
		} else {
			lines = append(lines, "【故事简介·权威前提】\n"+b)
		}
	}
	if v := strings.TrimSpace(sc.Subgenre); v != "" {
		add("【子类型】"+v, "[SUBGENRE] "+v)
	}
	if v := strings.TrimSpace(sc.EffectiveTheme()); v != "" {
		add("【主题/母题】"+v+"（请将其作为贯穿全书的主题线索与意象，自然织入情节、人物与场景，不要生硬说教）", "[THEME / MOTIF] "+v+" (treat it as the work's through-line theme and imagery; weave it naturally into plot, characters and scenes, never heavy-handed)")
	}
	if v := strings.TrimSpace(sc.Tone); v != "" {
		add("【基调】"+v, "[TONE] "+v)
	}
	if v := strings.TrimSpace(sc.Author); v != "" {
		add("【署名作者】"+v, "[AUTHOR] "+v)
	}
	if v := strings.TrimSpace(sc.StoryLength); v != "" {
		min, max := config.SuggestedChaptersByLength(v)
		lbl := lengthLabel(v, cfg.Language)
		if min > 0 {
			if en {
				add(fmt.Sprintf("【篇幅】%s（建议总章数约 %d-%d 章）", lbl, min, max),
					fmt.Sprintf("[LENGTH] %s (suggested total: ~%d-%d chapters)", lbl, min, max))
			} else {
				add(fmt.Sprintf("【篇幅】%s（建议总章数约 %d-%d 章）", lbl, min, max), "")
			}
		} else {
			add("【篇幅】"+lbl, "[LENGTH] "+lbl)
		}
	}
	if desc := structureDescription(sc.Structure, cfg.Language); desc != "" {
		lbl := structureLabel(sc.Structure, cfg.Language)
		if en {
			lines = append(lines, "[STRUCTURE] "+lbl+": "+desc)
		} else {
			lines = append(lines, "【故事结构】"+lbl+"："+desc)
		}
	}
	if v := strings.TrimSpace(sc.EffectiveConflict()); v != "" {
		add("【冲突规模】"+v+"（建议方向，不必强制）", "[CONFLICT SCALE] "+v+" (a suggestion, not a constraint)")
	}
	if v := strings.TrimSpace(sc.EffectiveProtagonist()); v != "" {
		add("【主角类型】"+v+"（建议方向，不必强制）", "[PROTAGONIST TYPE] "+v+" (a suggestion, not a constraint)")
	}
	if v := strings.TrimSpace(sc.SpecificSettings); v != "" {
		add("【特定设定】\n"+v, "[SPECIFIC SETTINGS]\n"+v)
	}
	if v := config.AudienceGuidanceOrText(sc.TargetAudience, cfg.Language); v != "" {
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
			lines = append(lines, "【类型修饰】本作带有「"+mod+"」的动漫/漫画/游戏系惯例：请体现其标志性桥段、节奏与读者预期（建议方向，不必强制）。")
		}
	}
	// Genre/subgenre flavor: derived from the free-form Type/Subgenre so it also
	// works for genres without presets; suggestions, never constraints.
	if g := strings.TrimSpace(sc.Type); g != "" || strings.TrimSpace(sc.Subgenre) != "" {
		flavor := strings.TrimSpace(g + " " + sc.Subgenre)
		if flavor != "" {
			add("【类型风味】本作属于「"+flavor+"」：请采用该类型/子类型的典型惯例、读者预期与标志性元素（建议方向，不必强制）。",
				"[GENRE FLAVOR] This book is "+flavor+": honor the typical conventions, reader expectations and signature elements of that genre/subgenre (a suggestion, not a constraint).")
		}
	}
	switch bias := strings.TrimSpace(sc.GenderBias); bias {
	case "", "random":
		add("【人物性别】随机自然即可，不要刻意偏向任何性别。",
			"[CHARACTER GENDER] Leave it to chance; do not deliberately skew toward any gender.")
	case "male":
		add("【人物性别】主要角色倾向男性为主。", "[CHARACTER GENDER] Skew main characters toward male.")
	case "female":
		add("【人物性别】主要角色倾向女性为主。", "[CHARACTER GENDER] Skew main characters toward female.")
	case "balanced":
		add("【人物性别】主要角色男女均衡分布。", "[CHARACTER GENDER] Keep main characters gender-balanced.")
	}
	if len(lines) == 0 {
		return ""
	}
	if en {
		return "NOVEL PARAMETERS (plan the whole book accordingly):\n" + strings.Join(lines, "\n")
	}
	return "小说参数（整体规划须遵循）：\n" + strings.Join(lines, "\n")
}

func structureLabel(structure, lang string) string {
	key := strings.TrimSpace(structure)
	if _, ok := structureDescriptions[key]; !ok {
		return key // free-form value: pass through as-is
	}
	en := i18n.NormalizeLanguage(lang) == i18n.LangEN
	switch key {
	case "three_act":
		return label(en, "三幕结构", "Three-act structure")
	case "six_act":
		return label(en, "六幕结构", "Six-part structure")
	case "fichtean":
		return label(en, "菲希特ean五危机", "Fichtean five-crisis")
	case "freytag":
		return label(en, "金字塔结构", "Freytag's pyramid")
	case "seven_point":
		return label(en, "七点结构", "Seven-point structure")
	case "heros_journey":
		return label(en, "英雄之旅", "Hero's journey")
	case "heros_journey_simple":
		return label(en, "简化英雄之旅", "Simplified hero's journey")
	case "save_the_cat":
		return label(en, "Save the Cat 节拍表", "Save the Cat beats")
	case "episodic":
		return label(en, "章节式/单元剧", "Episodic")
	}
	return key
}

func label(en bool, zh, english string) string {
	if en {
		return english
	}
	return zh
}
