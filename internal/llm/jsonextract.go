// LLM output helpers: locate the first complete JSON object/array in free-form
// model output (string-aware bracket matching), with repair of truncated tails.
package llm

import "strings"

// ponytail: byte walk outside JSON strings only; truncating mid-\\ or \\u may miscount.
// Used by ExtractJSON for string-aware object boundary detection.
func WalkJSONStructure(s string, onStruct func(i int, c byte)) {
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		onStruct(i, c)
	}
}

// endsInString reports whether s stops inside an unterminated JSON string
// literal (models cut off by max tokens often truncate mid-value).
func endsInString(s string) bool {
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
		}
	}
	return inString
}

// extractBalanced scans s from the first occurrence of open and returns the
// substring up to (and including) the matching close, using string-aware
// bracket counting. If the output was truncated before the value closed, the
// tail is repaired (dangling string closed, missing brackets appended):
// models frequently emit otherwise-valid JSON whose final closers were cut
// off by max tokens, and salvaging it beats discarding the whole generation.
func extractBalanced(s string, open, close byte) string {
	start := strings.IndexByte(s, open)
	if start == -1 {
		return ""
	}
	sub := s[start:]
	depth := 0
	end := -1
	WalkJSONStructure(sub, func(i int, c byte) {
		if c == open {
			depth++
		} else if c == close {
			depth--
			if depth == 0 && end == -1 {
				end = i + 1
			}
		}
	})
	if end != -1 {
		return sub[:end]
	}
	if depth <= 0 {
		return ""
	}
	tail := sub
	if endsInString(tail) {
		tail += `"`
	}
	return tail + strings.Repeat(string(close), depth)
}

func ExtractJSON(content string) string {
	return extractBalanced(content, '{', '}')
}

// ExtractJSONArray locates the first complete top-level JSON array in
// free-form model output (with the same truncation repair as ExtractJSON).
func ExtractJSONArray(content string) string {
	return extractBalanced(content, '[', ']')
}
