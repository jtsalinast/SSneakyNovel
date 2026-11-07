package story

import (
	"showmethestory/internal/config"
	"showmethestory/internal/i18n"
)

// ContentLanguage resolves the language every AI-facing report, analysis, beta
// reading and brainstorm prompt must use: the pinned Story output language
// ("Story language") when set, otherwise the project language.
//
// Critiques, beta panels and brainstorm describe the prose the author is
// writing, so they must follow the story language — not the immutable project
// language chosen at project creation. A project can be created in English and
// later pinned to Spanish prose; without this resolution the reports came back
// in English.
func ContentLanguage(cfg *config.Config) string {
	if cfg == nil {
		return i18n.LangEN
	}
	if lang := config.EffectiveOutputLanguage(cfg); lang != "" {
		return lang
	}
	return i18n.NormalizeLanguage(cfg.Language)
}

// ContentLanguageName is the language name used inside prompt text ("write the
// summary in Spanish"), as opposed to the "es"/"en" key.
func ContentLanguageName(lang string) string {
	if normalizeLangEN(lang) {
		return "English"
	}
	return "español"
}

// ReportLanguageRule returns an explicit, localized instruction to write every
// author-facing string in the content language. The shared system prompts are
// intentionally English (model quality), so this tail rule — placed last in the
// user prompt, where small models weigh instructions most — is what keeps each
// report in the story language instead of drifting to English.
func ReportLanguageRule(lang string) string {
	if normalizeLangEN(lang) {
		return "\n\n[OUTPUT LANGUAGE — MANDATORY] Write every author-facing string in English (summary, notes, suggestion title/detail/action/evidence, findings, labels). Never answer in another language, even if some instructions above appear in a different one."
	}
	return "\n\n[IDIOMA DE SALIDA — OBLIGATORIO] Escribe en español TODOS los textos dirigidos al autor (resumen, notas, título/detalle/acción/evidencia de cada sugerencia, hallazgos y etiquetas). No respondas en otro idioma, aunque parte de las instrucciones anteriores aparezca en otro idioma."
}
