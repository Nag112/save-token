package processors

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Nag112/save-token/internal/config"
)

// Lint command routing (gate hook patterns, anchored) and the unanchored
// can-handle catch-all (ports lint_output.py). The Python
// diagnostics-snapshot machinery is deliberately not ported.
var (
	lnFileHeaderRe = regexp.MustCompile(`^/?[\w./_-]+\.\w+$`)
	lnColonDigitRe = regexp.MustCompile(`:\d+`)
	lnSummaryRe    = regexp.MustCompile(`^\s*\d+\s+(error|warning|problem)`)
	lnFoundRe      = regexp.MustCompile(`(Found|Total|All checks)\s+\d+`)
	lnErrPrefixRe  = regexp.MustCompile(`(?i)^(error|warning):`)
	lnCrossRe      = regexp.MustCompile(`^\s*✖\s+\d+\s+problem`)
	lnImportantRe  = regexp.MustCompile(`(?i)\b(error|fatal|cannot|failed)\b`)

	// _parse_violation patterns, in original order.
	lnESLintIndentRe = regexp.MustCompile(`^\s*(\d+):(\d+)\s+(error|warning)\s+(.+?)\s{2,}(\S+)\s*$`)
	lnESLintInlineRe = regexp.MustCompile(`^(.+?):(\d+):\d+:\s+.+\((\S+)\)\s*$`)
	lnESLintAltRe    = regexp.MustCompile(`^(.+?):(\d+):\d+\s+(error|warning)\s+.+?\s{2,}(\S+)\s*$`)
	lnRuffRe         = regexp.MustCompile(`^(.+?):(\d+):\d+:\s+([A-Z]\w?\d+)\s+`)
	lnPylintRe       = regexp.MustCompile(`^(.+?):(\d+):\d+:\s+\w+:\s+.+\((\S+)\)\s*$`)
	lnMypyRe         = regexp.MustCompile(`^(.+?):(\d+):\s+(error|warning|note):\s+.+\[(\S+)\]\s*$`)
	lnClippyRe       = regexp.MustCompile(`^(warning|error)\[(\S+)\]`)
	lnClippyTailRe   = regexp.MustCompile(`\[([a-z][a-z0-9_-]+)\]\s*$`)
	lnClippyStartRe  = regexp.MustCompile(`^(warning|error):`)
	lnShellcheckRe   = regexp.MustCompile(`^In (.+?) line (\d+):`)
	lnShellcheck2Re  = regexp.MustCompile(`^(.+?):(\d+):\d+:\s+(warning|error|info|style)\s*-\s*(SC\d+)`)
	lnHadolintRe     = regexp.MustCompile(`^(.+?):(\d+)\s+(DL\d+|SC\d+)\s+`)
	lnBiomeRe        = regexp.MustCompile(`^(.+?):(\d+):\d+\s+(lint/\S+)\s+`)
	lnGolangciRe     = regexp.MustCompile(`^(.+?\.go):(\d+):\d+:\s+.+\(([a-zA-Z][\w-]*)\)\s*$`)
	lnRubocopRe      = regexp.MustCompile(`^(.+?\.rb):(\d+):\d+:\s+[CWEFR]:\s+(\S+?):\s+`)
)

// LintOutputProcessor groups lint violations by rule while retaining
// example locations (ports LintOutputProcessor).
type LintOutputProcessor struct {
	Base
}

// NewLint returns the lint-output processor.
func NewLint() *LintOutputProcessor { return &LintOutputProcessor{} }

// Name is the stable routing identifier.
func (LintOutputProcessor) Name() string { return "lint" }

// Priority routes lint after builds.
func (LintOutputProcessor) Priority() int { return 27 }

// HandlesFailure declares the processor summarizes failure output.
func (LintOutputProcessor) HandlesFailure() bool { return true }

// HookPatterns are the anchored command patterns for the gate policy.
func (LintOutputProcessor) HookPatterns() []string {
	return []string{
		`^(eslint|ruff(\s+check)?|flake8|pylint|rubocop|golangci-lint|` +
			`stylelint|biome\s+(check|lint))\b`,
		`^` + PythonCmd + `\s+-m\s+(flake8|pylint|ruff|mypy)\b`,
		`^(mypy|prettier\s+--check|shellcheck|hadolint|tflint|ktlint|` +
			`swiftlint)\b`,
		`^(oxlint|deno\s+lint)\b`,
		`^(npx\s+(eslint|prettier|stylelint|biome)\b|poetry\s+run\s+(flake8|` +
			`pylint|ruff|mypy)\b|uv\s+run\s+(flake8|pylint|ruff|mypy|` +
			`ruff\s+check)\b|bundle\s+exec\s+rubocop\b)`,
	}
}

var lnCanHandleRe = regexp.MustCompile(
	`\b(eslint|ruff(\s+check)?|flake8|pylint|clippy|rubocop|` +
		`golangci-lint|stylelint|prettier\s+--check|biome\s+(check|lint)|` +
		PythonCmd + `\s+-m\s+(flake8|pylint|ruff|mypy)|mypy|` +
		`shellcheck|hadolint|tflint|ktlint|swiftlint|cargo\s+clippy|` +
		`oxlint|deno\s+lint|` +
		`npx\s+(eslint|prettier|stylelint|biome)|` +
		`poetry\s+run\s+(flake8|pylint|ruff|mypy)|` +
		`uv\s+run\s+(flake8|pylint|ruff|mypy|ruff\s+check)|` +
		`bundle\s+exec\s+rubocop)\b`)

// CanHandle reports whether this processor supports the command.
func (LintOutputProcessor) CanHandle(command string) bool {
	return lnCanHandleRe.MatchString(command)
}

// lnViolation is one parsed lint violation.
type lnViolation struct {
	rule string
	path string
	line string
}

// parseViolation extracts (rule, filepath) from a violation line (ports
// _parse_violation). Patterns are tried in the original order.
func parseViolation(line, currentFile string) (rule, path string, ok bool) {
	if m := lnESLintIndentRe.FindStringSubmatch(line); m != nil {
		return m[5], currentFile, true
	}
	if m := lnESLintInlineRe.FindStringSubmatch(line); m != nil {
		return m[3], m[1], true
	}
	if m := lnESLintAltRe.FindStringSubmatch(line); m != nil {
		return m[4], m[1], true
	}
	if m := lnRuffRe.FindStringSubmatch(line); m != nil {
		return m[3], m[1], true
	}
	if m := lnPylintRe.FindStringSubmatch(line); m != nil {
		return m[3], m[1], true
	}
	if m := lnMypyRe.FindStringSubmatch(line); m != nil {
		return m[4], m[1], true
	}
	if m := lnClippyRe.FindStringSubmatch(line); m != nil {
		return m[2], "", true
	}
	if m := lnClippyTailRe.FindStringSubmatch(line); m != nil &&
		lnClippyStartRe.MatchString(line) {
		return m[1], "", true
	}
	if m := lnShellcheckRe.FindStringSubmatch(line); m != nil {
		return "shellcheck", m[1], true
	}
	if m := lnShellcheck2Re.FindStringSubmatch(line); m != nil {
		return m[4], m[1], true
	}
	if m := lnHadolintRe.FindStringSubmatch(line); m != nil {
		return m[3], m[1], true
	}
	if m := lnBiomeRe.FindStringSubmatch(line); m != nil {
		return m[3], m[1], true
	}
	if m := lnGolangciRe.FindStringSubmatch(line); m != nil {
		return m[3], m[1], true
	}
	if m := lnRubocopRe.FindStringSubmatch(line); m != nil {
		return m[3], m[1], true
	}
	return "", "", false
}

// Process compresses lint output by grouping violations per rule (ports
// process).
func (LintOutputProcessor) Process(command, output string, exitCode int) string {
	if strings.TrimSpace(output) == "" {
		return output
	}

	type ruleGroup struct {
		rule       string
		violations []string
		fileset    map[string]bool
		fileCt     int
	}
	var groups []*ruleGroup
	byRule := map[string]*ruleGroup{}
	var ungrouped, summaryLines []string
	currentFile := ""

	for _, raw := range strings.Split(output, "\n") {
		stripped := strings.TrimSpace(raw)
		if stripped == "" {
			continue
		}

		// ESLint file header line: a path without colon/digits.
		if lnFileHeaderRe.MatchString(stripped) && !lnColonDigitRe.MatchString(stripped) {
			currentFile = stripped
			continue
		}

		if rule, path, ok := parseViolation(stripped, currentFile); ok {
			g, seen := byRule[rule]
			if !seen {
				g = &ruleGroup{rule: rule, fileset: map[string]bool{}}
				byRule[rule] = g
				groups = append(groups, g)
			}
			g.violations = append(g.violations, stripped)
			if path != "" && !g.fileset[path] {
				g.fileset[path] = true
				g.fileCt++
			}
			continue
		}

		lower := strings.ToLower(stripped)
		if lnSummaryRe.MatchString(stripped) ||
			lnFoundRe.MatchString(stripped) ||
			lnErrPrefixRe.MatchString(lower) ||
			lnCrossRe.MatchString(stripped) {
			summaryLines = append(summaryLines, stripped)
		} else {
			ungrouped = append(ungrouped, stripped)
		}
	}

	if len(groups) == 0 {
		return output
	}

	cfg := config.Get()
	exampleCount := cfg.LintExampleCount
	groupThreshold := cfg.LintGroupThreshold

	// Stable sort by descending violation count; Python's sorted() keeps
	// insertion order for ties, so SliceStable over insertion-ordered
	// groups matches.
	sort.SliceStable(groups, func(i, j int) bool {
		return len(groups[i].violations) > len(groups[j].violations)
	})

	totalViolations := 0
	for _, g := range groups {
		totalViolations += len(g.violations)
	}
	result := []string{
		fmt.Sprintf("%d issues across %d rules:", totalViolations, len(groups)),
	}

	for _, g := range groups {
		count := len(g.violations)
		if count > groupThreshold {
			loc := ""
			if g.fileCt > 1 {
				loc = fmt.Sprintf(" in %d files", g.fileCt)
			}
			result = append(result,
				fmt.Sprintf("  %s: %d occurrences%s", g.rule, count, loc))
			for _, v := range head(g.violations, exampleCount) {
				result = append(result, "    "+v)
			}
			if count > exampleCount {
				result = append(result,
					fmt.Sprintf("    ... (%d more)", count-exampleCount))
			}
		} else {
			for _, v := range g.violations {
				result = append(result, "  "+v)
			}
		}
	}

	result = append(result, summaryLines...)

	// Ungrouped lines that look like real errors are worth keeping.
	var important []string
	for _, line := range ungrouped {
		if lnImportantRe.MatchString(line) {
			important = append(important, line)
		}
	}
	result = append(result, head(important, 5)...)

	return strings.Join(result, "\n")
}
