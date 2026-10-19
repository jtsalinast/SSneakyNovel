package prose

import "unicode"

// CountProseUnits counts chapter prose length in WORDS for every script:
// Latin words +1 each (contiguous alnum tokens with internal . , - # glue);
// Each CJK ideograph counts as one word — the standard convention used by
// Word/LibreOffice and Chinese editors ("字数"). Punctuation and whitespace
// break tokens and are not counted. Fullwidth alnum normalizes to halfwidth.
func CountProseUnits(s string) int {
	count := 0
	inToken := false
	cjkRun := 0
	flushCJK := func() {
		if cjkRun > 0 {
			// One CJK ideograph counts as one word — the standard convention used
			// by Word/LibreOffice and Chinese editors ("字数"). The previous
			// heuristic (2 chars ≈ 1 word) undercounted CJK prose.
			count += cjkRun
			cjkRun = 0
		}
	}
	for _, r := range s {
		r = normalizeWideAlnum(r)
		switch {
		case isCJK(r):
			if inToken {
				count++
				inToken = false
			}
			cjkRun++
		case isLatinAlnum(r):
			flushCJK()
			inToken = true
		case isTokenGlue(r):
			if inToken {
				continue
			}
			flushCJK()
		case isWhitespace(r), isBreakPunct(r):
			if inToken {
				count++
				inToken = false
			}
			flushCJK()
		default:
			if inToken {
				count++
				inToken = false
			}
			flushCJK()
		}
	}
	if inToken {
		count++
	}
	flushCJK()
	return count
}

func normalizeWideAlnum(r rune) rune {
	switch {
	case r >= '０' && r <= '９':
		return r - '０' + '0'
	case r >= 'Ａ' && r <= 'Ｚ':
		return r - 'Ａ' + 'A'
	case r >= 'ａ' && r <= 'ｚ':
		return r - 'ａ' + 'a'
	default:
		return r
	}
}

func isLatinAlnum(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

func isTokenGlue(r rune) bool {
	return r == '.' || r == ',' || r == '-' || r == '#'
}

func isWhitespace(r rune) bool {
	return unicode.IsSpace(r)
}

func isBreakPunct(r rune) bool {
	if isCJKPunct(r) {
		return true
	}
	return isLatinBreakPunct(r)
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

func isCJKPunct(r rune) bool {
	switch r {
	case '。', '，', '、', '；', '：', '！', '？', '…', '—', '「', '」', '『', '』', '（', '）', '《', '》', '【', '】', '“', '”', '‘', '’', '·':
		return true
	}
	return false
}

func isLatinBreakPunct(r rune) bool {
	switch r {
	case '.', ',', ';', ':', '!', '?', '(', ')', '[', ']', '{', '}', '"', '\'', '/', '\\', '@', '$', '%', '^', '&', '*', '+', '=', '|', '~', '`', '<', '>':
		return true
	}
	return false
}
