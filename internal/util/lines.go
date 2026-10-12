// Package util holds small helpers matching Python semantics used by the
// ported token-saver logic.
package util

import "strings"

// pythonLineBreaks are the additional Unicode line boundaries Python's
// str.splitlines() recognises beyond \n and \r.
var pythonLineBreaks = []rune{
	'\v', '\f', 0x1C, 0x1D, 0x1E, 0x85, 0x2028, 0x2029,
}

// SplitLines mimics Python str.splitlines(): splits on \r\n, \n, \r and the
// extra Unicode boundaries, dropping a trailing empty segment.
func SplitLines(s string) []string {
	var lines []string
	start := 0
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		c := runes[i]
		if c == '\r' {
			lines = append(lines, string(runes[start:i]))
			if i+1 < len(runes) && runes[i+1] == '\n' {
				i++
			}
			i++
			start = i
			continue
		}
		if c == '\n' {
			lines = append(lines, string(runes[start:i]))
			i++
			start = i
			continue
		}
		isBreak := false
		for _, b := range pythonLineBreaks {
			if c == b {
				isBreak = true
				break
			}
		}
		if isBreak {
			lines = append(lines, string(runes[start:i]))
			i++
			start = i
			continue
		}
		i++
	}
	if start < len(runes) {
		lines = append(lines, string(runes[start:]))
	}
	return lines
}

// JoinLines joins lines with "\n" (Python "\n".join semantics).
func JoinLines(lines []string) string {
	return strings.Join(lines, "\n")
}
