package processors

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Nag112/save-token/internal/util"
)

// Case-sensitive on purpose: 'error'/'Error'/'ERROR' as literal tokens.
// IGNORECASE would fold this into the same trap CRITICAL_RE's global
// IGNORECASE has — matching 'Extracting', 'entry', '[emitted]' — which is
// exactly the noise this processor's whole job is to filter out.
var (
	boErrorRe       = regexp.MustCompile(`\b(error|Error|ERROR)\b`)
	boZeroErrorsRe  = regexp.MustCompile(`\b0 errors?\b`)
	boBuildFailRe   = regexp.MustCompile(`\bFAIL\b|\bFailure\b|\bfatal\b|ninja: build stopped|Rollup failed|compiled with \d+ errors?|✗`)
	boTscNoEmitRe   = regexp.MustCompile(`\btsc\b.*--noEmit`)
	boAuditRe       = regexp.MustCompile(`\b(npm|yarn|pnpm)\s+audit\b`)
	boDockerBuildRe = regexp.MustCompile(`\bdocker\s+(build|compose\s+build)\b`)
	// Build package-listing / install / jvm exclusions (other processors).
	boListRe    = regexp.MustCompile(`\b(pip3?\s+(list|freeze)|npm\s+(ls|list)|conda\s+list)\b`)
	boInstallRe = regexp.MustCompile(`\b(pip3?\s+install|poetry\s+(install|update|add)|uv\s+(pip\s+install|sync))\b`)
	boJvmRe     = regexp.MustCompile(`\b(mvn|mvnw|gradle|gradlew)\b`)
	boAnyRe     = regexp.MustCompile(
		`\b(npm\s+(run|install|ci|build|audit)|yarn\s+(run|install|` +
			`build|add|audit)|pnpm\s+(run|install|build|add|audit)|` +
			`make\b|cmake\b|ant\b|` +
			`tsc\b|webpack\b|vite(\s+build)?|esbuild\b|rollup\b|` +
			`next\s+build|nuxt\s+build|` +
			`docker\s+(build|compose\s+build)|` +
			`turbo\s+(run|build)|nx\s+(run|build)|bazel\s+build|sbt\b|` +
			`mix\s+compile|` +
			`bun\s+(install|build|run)|` +
			`npx\s+(webpack|vite|esbuild|tsc|next\s+build|nuxt\s+build|` +
			`turbo\s+run))\b`)
	// _extract_errors
	boWarnNoteRe     = regexp.MustCompile(`\b(warning|Warning|note|Note|help|Help)\b`)
	boCtxNumPipeRe   = regexp.MustCompile(`^\d+\s*\|`)
	boCtxIndentRe    = regexp.MustCompile(`^\s+\d+:\d+`)
	boCountSummaryRe = regexp.MustCompile(`\d+\s+(errors?|warnings?|problems?)`)
	// _summarize_success
	boWarnWordRe = regexp.MustCompile(`(?i)\bwarn(ing)?\b`)
	// _process_docker_build
	boDkStepRe  = regexp.MustCompile(`^(Step \d+/\d+|#\d+\s|\[\d+/\d+\])`)
	boDkErrRe   = regexp.MustCompile(`\b(error|Error|ERROR|failed|FAILED)\b`)
	boDkFinalRe = regexp.MustCompile(`(?i)(Successfully (built|tagged)|naming to |writing image|DONE)`)
	boDkSkip1Re = regexp.MustCompile(`^(Running in |Removing intermediate| ---> |sha256:)`)
	boDkSkip2Re = regexp.MustCompile(`^(Sending build context|Downloading|Extracting|Pulling)`)
	boPercentRe = regexp.MustCompile(`\d+(\.\d+)?%`)
	// _process_audit
	boSevRe      = regexp.MustCompile(`(?i)\b(critical|high|moderate|low)\b`)
	boPkgAngleRe = regexp.MustCompile(`^(\S+)\s+[<>=]`)
	boPkgWordRe  = regexp.MustCompile(`^Package\s+(\S+)`)
	boAuditSumRe = regexp.MustCompile(`(?i)\d+\s+(vulnerabilit|package)`)
	boAuditFixRe = regexp.MustCompile(`(?i)(npm audit fix|run .* to fix|breaking change)`)
	// _process_tsc_typecheck
	boTsFmt1Re  = regexp.MustCompile(`^(.+?)\(\d+,\d+\):\s+error\s+(TS\d+):\s+(.+)$`)
	boTsFmt2Re  = regexp.MustCompile(`^(.+?):\d+:\d+\s+-\s+error\s+(TS\d+):\s+(.+)$`)
	boTsFoundRe = regexp.MustCompile(`^Found \d+ error`)
)

// boIsErrorLine reports whether an already-trimmed line reports a build
// failure (ports _is_error_line).
func boIsErrorLine(stripped string) bool {
	if boZeroErrorsRe.MatchString(stripped) {
		return false
	}
	return boErrorRe.MatchString(stripped) || boBuildFailRe.MatchString(stripped)
}

// BuildOutputProcessor removes build progress while retaining errors and
// result summaries (ports BuildOutputProcessor).
type BuildOutputProcessor struct {
	Base
}

// NewBuild returns the build-output processor.
func NewBuild() *BuildOutputProcessor { return &BuildOutputProcessor{} }

// Name is the stable routing identifier.
func (BuildOutputProcessor) Name() string { return "build" }

// Priority routes builds after tests and package installs.
func (BuildOutputProcessor) Priority() int { return 25 }

// HandlesFailure declares the processor summarizes failure output.
func (BuildOutputProcessor) HandlesFailure() bool { return true }

// WantsExitCode: a bare non-zero exit is the only failure signal for some
// builds, so the engine must forward it.
func (BuildOutputProcessor) WantsExitCode() bool { return true }

// HookPatterns are the anchored command patterns for the gate policy.
func (BuildOutputProcessor) HookPatterns() []string {
	return []string{
		`^(npm\s+(run|install|build|ci|audit)|yarn\s+(run|install|build|` +
			`add|audit)|pnpm\s+(run|install|build|add|audit))\b`,
		`^(make|cmake|ant)\b`,
		`^(tsc|webpack|vite(\s+build)?|esbuild|rollup|next\s+build|` +
			`nuxt\s+build)\b`,
		`^(turbo\s+run|turbo\s+build|nx\s+(run|build)|bazel\s+build|sbt\b|` +
			`mix\s+compile)\b`,
		`^docker\s+(build|compose\s+build)\b`,
		`^bun\s+(install|build|run)\b`,
		`^npx\s+(webpack|vite|esbuild|tsc|next\s+build|nuxt\s+build|` +
			`turbo\s+run)\b`,
	}
}

// CanHandle reports whether this processor supports the command.
func (BuildOutputProcessor) CanHandle(command string) bool {
	// Package listings and installs belong to other processors.
	if boListRe.MatchString(command) ||
		boInstallRe.MatchString(command) ||
		boJvmRe.MatchString(command) {
		return false
	}
	return boAnyRe.MatchString(command)
}

// Process compresses build output; exitCode is forwarded by the engine
// (WantsExitCode). A known failure prevents a success summary.
func (p BuildOutputProcessor) Process(command, output string, exitCode int) string {
	if strings.TrimSpace(output) == "" {
		return output
	}

	// tsc --noEmit is a type-check (lint), not a build — group by code.
	if boTscNoEmitRe.MatchString(command) {
		return p.processTSCTypecheck(output)
	}

	// Piped output may be partial — skip aggressive summarization to avoid
	// claiming "Build succeeded" when errors were piped away.
	if strings.Contains(command, "|") {
		return output
	}

	if boAuditRe.MatchString(command) {
		return p.processAudit(output)
	}
	if boDockerBuildRe.MatchString(command) {
		return p.processDockerBuild(output)
	}

	lines := util.SplitLines(output)

	hasError := false
	for _, line := range lines {
		// Python tests is_error_line on the RAW line (no token depends on
		// leading whitespace), and is_progress_line on the stripped line.
		if boIsErrorLine(line) && !p.isProgressLine(strings.TrimSpace(line)) {
			hasError = true
			break
		}
	}

	// A known non-zero exit is decisive: never print "Build succeeded."
	// over a run whose output has no recognized error vocabulary.
	failedByExitCode := exitCode != 0

	if hasError || failedByExitCode {
		return p.extractErrors(lines, failedByExitCode && !hasError)
	}
	return p.summarizeSuccess(lines)
}

// extractErrors keeps error blocks with context (ports _extract_errors).
func (p BuildOutputProcessor) extractErrors(lines []string, forced bool) string {
	var result []string
	inErrorBlock := false
	blankCount := 0

	for _, line := range lines {
		stripped := strings.TrimSpace(line)

		if p.isProgressLine(stripped) {
			continue
		}

		// Error start
		if boIsErrorLine(stripped) {
			inErrorBlock = true
			blankCount = 0
			result = append(result, line)
			continue
		}

		if inErrorBlock {
			if stripped == "" {
				blankCount++
				// For TypeScript/multi-file errors: tolerate single blank
				// lines, end block only after 2+ consecutive blanks.
				if blankCount >= 2 {
					inErrorBlock = false
				} else {
					result = append(result, line)
				}
				continue
			}
			blankCount = 0
			// Context lines (stack trace, code pointers, etc.)
			if hasAnyPrefix(stripped, "at ", "-->", "  |", "   |", ">", "~~", "^^") ||
				boCtxNumPipeRe.MatchString(stripped) ||
				boCtxIndentRe.MatchString(stripped) {
				result = append(result, line)
			} else if boWarnNoteRe.MatchString(stripped) {
				result = append(result, line)
				inErrorBlock = false
			} else {
				result = append(result, line)
			}
			continue
		}

		// Keep summary lines
		if boCountSummaryRe.MatchString(strings.ToLower(stripped)) {
			result = append(result, line)
		}
	}

	if len(result) == 0 {
		tail := util.JoinLines(tailLines(lines, 30))
		if forced {
			// The exit code is the only evidence of failure. Say so
			// explicitly rather than silently falling back to a raw tail
			// that could still read as a successful build.
			return "[save-token] command exited non-zero; no error markers recognized\n" + tail
		}
		return tail
	}
	return util.JoinLines(result)
}

// summarizeSuccess retains result details and summarizes routine progress
// (ports _summarize_success).
func (p BuildOutputProcessor) summarizeSuccess(lines []string) string {
	var result []string
	warningCount := 0
	var warningSamples []string
	var outputLines []string

	for _, line := range lines {
		stripped := strings.TrimSpace(line)

		if p.isProgressLine(stripped) {
			continue
		}

		if boWarnWordRe.MatchString(stripped) {
			warningCount++
			if len(warningSamples) < 5 {
				warningSamples = append(warningSamples, stripped)
			}
			continue
		}

		// Keep meaningful output lines
		lower := strings.ToLower(stripped)
		for _, kw := range []string{
			"built", "compiled", "success", "done", "complete", "finish",
			"written", "created", "generated", "output", "bundle", "size",
			"gzip", "chunk", "cached", "remote:", "tasks",
		} {
			if strings.Contains(lower, kw) {
				outputLines = append(outputLines, stripped)
				break
			}
		}
	}

	summary := "Build succeeded."
	if warningCount > 0 {
		summary += fmt.Sprintf(" (%d warnings)", warningCount)
	}

	result = append(result, summary)
	for _, sample := range warningSamples {
		result = append(result, "  "+sample)
	}
	// Keep last few meaningful lines (often contain size/timing info).
	if len(outputLines) > 0 {
		result = append(result, tailLines(outputLines, 3)...)
	}

	return util.JoinLines(result)
}

// processDockerBuild keeps steps, errors, and final result
// (ports _process_docker_build).
func (p BuildOutputProcessor) processDockerBuild(output string) string {
	lines := util.SplitLines(output)
	var result []string

	for _, line := range lines {
		stripped := strings.TrimSpace(line)

		// Keep step headers
		if boDkStepRe.MatchString(stripped) {
			result = append(result, stripped)
			continue
		}

		// Always keep errors
		if boDkErrRe.MatchString(stripped) {
			result = append(result, stripped)
			continue
		}

		// Keep final image/tag info
		if boDkFinalRe.MatchString(stripped) {
			result = append(result, stripped)
			continue
		}

		// Skip noise: intermediate containers, sha256 hashes, RUN output
		if boDkSkip1Re.MatchString(stripped) || boDkSkip2Re.MatchString(stripped) {
			continue
		}
		if boPercentRe.MatchString(stripped) {
			continue
		}
	}

	if len(result) == 0 {
		return util.JoinLines(tailLines(lines, 10))
	}
	return util.JoinLines(result)
}

// processAudit groups npm/yarn audit vulnerabilities by severity
// (ports _process_audit).
func (p BuildOutputProcessor) processAudit(output string) string {
	lines := util.SplitLines(output)
	severities := map[string]int{}
	packages := map[string][]string{} // severity -> package names
	var summaryLines []string
	seenSummary := map[string]bool{}
	currentPackage := ""

	for _, line := range lines {
		stripped := strings.TrimSpace(line)

		sevMatch := boSevRe.FindStringSubmatch(stripped)

		// Package name in vulnerability blocks
		pkgMatch := boPkgAngleRe.FindStringSubmatch(stripped)
		if pkgMatch == nil {
			pkgMatch = boPkgWordRe.FindStringSubmatch(stripped)
		}
		if pkgMatch != nil {
			currentPackage = pkgMatch[1]
		}

		if sevMatch != nil {
			sev := strings.ToLower(sevMatch[1])
			severities[sev]++
			if currentPackage != "" {
				list := packages[sev]
				found := false
				for _, pk := range list {
					if pk == currentPackage {
						found = true
						break
					}
				}
				if !found {
					packages[sev] = append(list, currentPackage)
				}
			}
		}

		// Keep summary/total lines
		if boAuditSumRe.MatchString(stripped) || boAuditFixRe.MatchString(stripped) {
			if !seenSummary[stripped] {
				summaryLines = append(summaryLines, stripped)
				seenSummary[stripped] = true
			}
		}
	}

	if len(severities) == 0 {
		// Could not parse — fall through unchanged (as Python re-joins).
		return util.JoinLines(lines)
	}

	var result []string
	total := 0
	for _, n := range severities {
		total += n
	}
	result = append(result, fmt.Sprintf("%d vulnerabilities found:", total))
	for _, sev := range []string{"critical", "high", "moderate", "low"} {
		if _, ok := severities[sev]; !ok {
			continue
		}
		pkgs := packages[sev]
		pkgStr := ""
		if len(pkgs) > 0 {
			pkgStr = " (" + strings.Join(head(pkgs, 5), ", ") + ")"
		}
		if len(pkgs) > 5 {
			pkgStr = fmt.Sprintf(" (%s +%d more)", strings.Join(head(pkgs, 5), ", "), len(pkgs)-5)
		}
		result = append(result, fmt.Sprintf("  %s: %d%s", sev, severities[sev], pkgStr))
	}

	// Deduplicated summary lines (deduped while collecting, same order).
	result = append(result, summaryLines...)

	return util.JoinLines(result)
}

// processTSCTypecheck groups tsc --noEmit errors by TS code
// (ports _process_tsc_typecheck).
func (p BuildOutputProcessor) processTSCTypecheck(output string) string {
	lines := util.SplitLines(output)
	byCode := map[string][]string{}
	var codes []string // insertion order, for Python-dict stable sorting
	summaryLine := ""

	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		// TS error format: src/file.ts(10,5): error TS2322: message
		m := boTsFmt1Re.FindStringSubmatch(stripped)
		if m == nil {
			// Also match: src/file.ts:10:5 - error TS2322: message
			m = boTsFmt2Re.FindStringSubmatch(stripped)
		}
		if m != nil {
			code := m[2]
			if _, ok := byCode[code]; !ok {
				codes = append(codes, code)
			}
			byCode[code] = append(byCode[code], stripped)
			continue
		}
		// Summary line: Found N errors in M files.
		if boTsFoundRe.MatchString(stripped) {
			summaryLine = stripped
		}
	}

	if len(byCode) == 0 {
		return output
	}

	total := 0
	for _, v := range byCode {
		total += len(v)
	}
	// stable sort by count desc over insertion-ordered codes
	sort.SliceStable(codes, func(i, j int) bool {
		return len(byCode[codes[i]]) > len(byCode[codes[j]])
	})

	result := []string{fmt.Sprintf("%d type errors across %d codes:", total, len(byCode))}
	for _, code := range codes {
		violations := byCode[code]
		count := len(violations)
		if count > 3 {
			result = append(result, fmt.Sprintf("  %s: %d occurrences", code, count))
			for _, v := range violations[:2] {
				result = append(result, "    "+v)
			}
			result = append(result, fmt.Sprintf("    ... (%d more)", count-2))
		} else {
			for _, v := range violations {
				result = append(result, "  "+v)
			}
		}
	}

	if summaryLine != "" {
		result = append(result, summaryLine)
	}
	return util.JoinLines(result)
}

// boProgressPatterns are the routine build-progress patterns (re.match
// semantics; all anchored, so MatchString is exact parity).
var boProgressPatterns = compileAll(
	`^\s*(Downloading|Installing|Fetching|Resolving|Unpacking|Linking|Extracting)`,
	`^\s*added \d+ packages?`,
	`^\s*\d+ packages? are looking`,
	`^\s*(GET|fetch)\s+http`,
	`^\s*npm\s+(WARN|notice|warn)\b`,
	`^\s*\d+(\.\d+)?\s*%`,
	`^\s*(⠋|⠙|⠹|⠸|⠼|⠴|⠦|⠧|⠇|⠏|⣾|⣽|⣻|⢿|⡿|⣟|⣯|⣷)`,
	`^\s*\[\d+/\d+\]`,                          // [1/5] progress indicators
	`^\s*(Compiling|Updating|Preparing)\s+\S+`, // cargo
	`^\s*Already up to date`,
	`^\s*Using\s+(cached|version)\b`,
	`^\s*Collecting\s+\S+`, // pip
	`^\s*━`,                // pip progress bar
	// Yarn Berry v2+.
	`^\s*➤?\s*YN\d+:.*\b(Resolution|Fetch|Link)\s+step\b`,
	// pnpm resolved/reused/downloaded statistics.
	`^\s*Progress:\s+resolved\s+\d+`,
	// pnpm content-addressable store.
	`^\s*[Pp]ackages?\s+(are|is)\s+hard linked`,
)

// isProgressLine reports routine build progress or decoration.
func (BuildOutputProcessor) isProgressLine(line string) bool {
	if line == "" {
		return false
	}
	for _, re := range boProgressPatterns {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// compileAll compiles anchored patterns at init (panics are programmer
// errors, caught immediately in tests).
func compileAll(pats ...string) []*regexp.Regexp {
	outs := make([]*regexp.Regexp, 0, len(pats))
	for _, p := range pats {
		outs = append(outs, regexp.MustCompile(p))
	}
	return outs
}

// tailLines returns the last n lines of s.
func tailLines(lines []string, n int) []string {
	if len(lines) > n {
		return lines[len(lines)-n:]
	}
	return lines
}

// head returns the first n elements of s.
func head(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
