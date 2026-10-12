// Package critical defines the one shared notion of "a line that must never
// be silently dropped". Ported from token-saver src/processors/critical.py.
package critical

import (
	"regexp"
	"strings"

	"github.com/Nag112/save-token/internal/util"
)

// Case-insensitive, human-vocabulary half.
var criticalCI = regexp.MustCompile(
	`(?im)\b(` +
		`error|errors|failed|failure|failures|fatal|panic|panicked|` +
		`exception|traceback|assert|assertion|denied|refused|timeout|timed` +
		`)\b` +
		`|^\s*(FAIL|ERR)\b` +
		`|\[(ERROR|FATAL|CRITICAL)\]` +
		`|\b\w+Error\b` +
		`|\b\w+Exception\b`,
)

// Case-sensitive, symbol-shaped half (errno/signal names, bare E prefixes).
var criticalCS = regexp.MustCompile(
	`(?m)^\s*E\b|\b(E[A-Z]{3,}|SIG[A-Z]{3,})\b`,
)

// IsCritical reports whether a line looks like it reports a failure worth
// preserving.
func IsCritical(line string) bool {
	return criticalCI.MatchString(line) || criticalCS.MatchString(line)
}

// CriticalLines returns the stripped, non-empty lines of text that look
// critical.
func CriticalLines(text string) []string {
	var out []string
	for _, ln := range util.SplitLines(text) {
		s := strings.TrimSpace(ln)
		if s != "" && IsCritical(ln) {
			out = append(out, s)
		}
	}
	return out
}

// MissingCritical returns critical lines of original that do not appear as
// substrings of compressed. Substring containment (not equality) is
// deliberate: processors legitimately re-indent, prefix, or merge lines.
func MissingCritical(original, compressed string) []string {
	if original == "" {
		return nil
	}
	var out []string
	for _, line := range CriticalLines(original) {
		if !strings.Contains(compressed, line) {
			out = append(out, line)
		}
	}
	return out
}
