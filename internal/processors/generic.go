package processors

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Nag112/save-token/internal/config"
	"github.com/Nag112/save-token/internal/critical"
	"github.com/Nag112/save-token/internal/util"
)

var (
	ansiRE      = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]|\x1b\\].*?\x07")
	numericRE   = regexp.MustCompile(`\d+(\.\d+)?`)
	progressBlk = regexp.MustCompile(`[━█▓░▒■□●○]{3,}`)
	asciiBarRE  = regexp.MustCompile(`[#=\->]{5,}`)
	progressCtx = regexp.MustCompile(`[%\[\]]|\b\d+/\d+\b|ETA|eta|\d+(\.\d+)?\s*[KMGT]?i?B/s`)
	pctRE       = regexp.MustCompile(`\d+(\.\d+)?%`)
	rateRE      = regexp.MustCompile(`\d+(\.\d+)?\s*(KB|MB|GB|B|kB|MiB|GiB|k|M|G)/s`)
	etaRE       = regexp.MustCompile(`(ETA|eta)\s+\d+`)
	curlTimeRE  = regexp.MustCompile(`--:--:--|(\d+:){2}\d+`)
)

var spinnerChars = map[string]struct{}{
	"⠋": {}, "⠙": {}, "⠹": {}, "⠸": {}, "⠼": {}, "⠴": {}, "⠦": {}, "⠧": {},
	"⠇": {}, "⠏": {}, "⣾": {}, "⣽": {}, "⣻": {}, "⢿": {}, "⡿": {}, "⣟": {},
	"⣯": {}, "⣷": {},
}

// Generic is the universal fallback processor (ports GenericProcessor).
type Generic struct{ Base }

// NewGeneric returns the generic fallback processor.
func NewGeneric() *Generic { return &Generic{} }

// Name is "generic".
func (Generic) Name() string { return "generic" }

// Priority is 999 so generic routes last.
func (Generic) Priority() int { return 999 }

// CanHandle always matches as the fallback.
func (Generic) CanHandle(command string) bool { return true }

// Process applies the universal compression pipeline.
func (g Generic) Process(command, output string, exitCode int) string {
	cfg := config.Get()
	lines := util.SplitLines(output)
	lines = stripANSI(lines)
	lines = stripProgressBars(lines)
	lines = collapseBlankLines(lines)
	lines = collapseRepeatedLines(lines)
	lines = collapseSimilarLines(lines)
	lines = stripTrailingWS(lines)
	if len(lines) > cfg.GenericTruncateThreshold {
		lines = truncateMiddle(lines)
	}
	return strings.Join(lines, "\n")
}

// Clean is the light second pass: ANSI strip, blank collapse, trailing WS.
func (g Generic) Clean(text string) string {
	lines := util.SplitLines(text)
	lines = stripANSI(lines)
	lines = collapseBlankLines(lines)
	lines = stripTrailingWS(lines)
	return strings.Join(lines, "\n")
}

func stripANSI(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansiRE.ReplaceAllString(l, "")
	}
	return out
}

func stripTrailingWS(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(l, " \t")
	}
	return out
}

func stripProgressBars(lines []string) []string {
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if stripped != "" {
			nStr := utf8.RuneCountInString(stripped)
			// Unicode block bars: always progress noise.
			if b := progressBlk.FindString(stripped); float64(utf8.RuneCountInString(b)) > float64(nStr)*0.5 {
				continue
			}
			// ASCII bars only in progress context; bare rules survive.
			if a := asciiBarRE.FindString(stripped); float64(utf8.RuneCountInString(a)) > float64(nStr)*0.5 &&
				progressCtx.MatchString(stripped) {
				continue
			}
		}
		if _, spinner := spinnerChars[stripped]; spinner {
			continue
		}
		result = append(result, line)
	}
	return result
}

func collapseBlankLines(lines []string) []string {
	result := make([]string, 0, len(lines))
	prevBlank := false
	for _, line := range lines {
		isBlank := strings.TrimSpace(line) == ""
		if isBlank && prevBlank {
			continue
		}
		result = append(result, line)
		prevBlank = isBlank
	}
	return result
}

func collapseRepeatedLines(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	result := make([]string, 0, len(lines))
	current := lines[0]
	count := 1
	flush := func(line string, n int) {
		if n > 1 {
			result = append(result, fmt.Sprintf("%s (x%d)", line, n))
		} else {
			result = append(result, line)
		}
	}
	for _, line := range lines[1:] {
		if line == current && strings.TrimSpace(current) != "" {
			count++
		} else {
			flush(current, count)
			current = line
			count = 1
		}
	}
	flush(current, count)
	return result
}

func normalizeNumbers(line string) string {
	return numericRE.ReplaceAllString(strings.TrimSpace(line), "N")
}

func isNumericHeavy(line string) bool {
	stripped := strings.TrimSpace(line)
	if stripped == "" {
		return false
	}
	if pctRE.MatchString(stripped) {
		return true
	}
	if rateRE.MatchString(stripped) {
		return true
	}
	if etaRE.MatchString(stripped) {
		return true
	}
	digits := 0
	for _, r := range stripped {
		if unicode.IsDigit(r) {
			digits++
		}
	}
	return curlTimeRE.MatchString(stripped) && digits >= 5
}

func collapseSimilarLines(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	result := make([]string, 0, len(lines))
	current := lines[0]
	currentNorm := normalizeNumbers(current)
	group := []string{current}
	flush := func(g []string) {
		if len(g) >= 5 {
			result = append(result, g[0])
			result = append(result, fmt.Sprintf("  ... (%d similar lines)", len(g)-2))
			result = append(result, g[len(g)-1])
		} else {
			result = append(result, g...)
		}
	}
	for _, line := range lines[1:] {
		norm := normalizeNumbers(line)
		if norm == currentNorm && strings.TrimSpace(current) != "" &&
			utf8.RuneCountInString(strings.TrimSpace(current)) > 10 && isNumericHeavy(current) {
			group = append(group, line)
		} else {
			flush(group)
			current = line
			currentNorm = norm
			group = []string{line}
		}
	}
	flush(group)
	return result
}

func truncateMiddle(lines []string) []string {
	cfg := config.Get()
	keepHead := cfg.GenericKeepHead
	keepTail := cfg.GenericKeepTail
	total := len(lines)

	var head, tail, middle []string
	if keepHead > 0 {
		head = lines[:min(keepHead, total)]
	}
	if keepTail > 0 {
		tail = lines[max(0, total-keepTail):]
		middle = lines[len(head):max(len(head), total-len(tail))]
	} else {
		middle = lines[len(head):]
	}
	removed := total - len(head) - len(tail)
	if removed <= 0 {
		return lines
	}

	maxKept := cfg.GenericKeepCritical
	var kept []string
	if maxKept > 0 {
		for _, ln := range middle {
			if critical.IsCritical(ln) {
				kept = append(kept, ln)
				if len(kept) >= maxKept {
					break
				}
			}
		}
	}
	dropped := removed - len(kept)
	marker := fmt.Sprintf("... (%d lines truncated, %d total) ...", dropped, total)
	if len(kept) == 0 {
		return append(append(append([]string{}, head...), marker), tail...)
	}
	out := append([]string{}, head...)
	out = append(out, marker)
	out = append(out, kept...)
	out = append(out, fmt.Sprintf("... (%d critical lines kept from the truncated section) ...", len(kept)))
	out = append(out, tail...)
	return out
}
