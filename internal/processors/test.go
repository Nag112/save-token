package processors

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Nag112/save-token/internal/config"
	"github.com/Nag112/save-token/internal/util"
)

// TestProcessor summarizes test-runner output while retaining failures
// and totals (ports test_output.TestOutputProcessor).
type TestProcessor struct{ Base }

// NewTest returns the test processor.
func NewTest() *TestProcessor { return &TestProcessor{} }

// Name is the routing identifier.
func (*TestProcessor) Name() string { return "test" }

// Priority orders routing.
func (*TestProcessor) Priority() int { return 21 }

// HandlesFailure reports failure output is summarized too.
func (*TestProcessor) HandlesFailure() bool { return true }

// HookPatterns are anchored command matches.
func (*TestProcessor) HookPatterns() []string {
	return []string{
		`^(pytest|py\.test|` + PythonCmd + `\s+-m\s+pytest|jest|mocha|` +
			`vitest|cargo\s+test|go\s+test|rspec|phpunit|bun\s+test|` +
			`dotnet\s+test|swift\s+test|mix\s+test)\b`,
		`^(npm\s+test|yarn\s+test|pnpm\s+test)\b`,
		`^(npx\s+(jest|mocha|vitest|playwright)\b|poetry\s+run\s+(pytest|` +
			`py\.test)\b|uv\s+run\s+(pytest|py\.test)\b|pipx\s+run\s+pytest\b|` +
			`bundle\s+exec\s+(rspec|rails\s+test)\b)`,
	}
}

var (
	testCmdRE = regexp.MustCompile(`\b(pytest|py\.test|` + PythonCmd +
		`\s+-m\s+pytest|jest|mocha|cargo\s+test|go\s+test|rspec|phpunit|` +
		`vitest|bun\s+test|npm\s+test|yarn\s+test|pnpm\s+test|` +
		`dotnet\s+test|swift\s+test|mix\s+test|` +
		`npx\s+(jest|mocha|vitest|playwright)|poetry\s+run\s+(pytest|py\.test)|` +
		`uv\s+run\s+(pytest|py\.test)|pipx\s+run\s+pytest|` +
		`bundle\s+exec\s+(rspec|rails\s+test))\b`)
	testPytestRouteRE = regexp.MustCompile(`\bpytest\b|py\.test|` + PythonCmd +
		`\s+-m\s+pytest|\b(poetry|uv|pipx)\s+run\s+pytest`)
	testJestRouteRE = regexp.MustCompile(`\bjest\b|\bvitest\b|\bnpm\s+test\b|` +
		`\byarn\s+test\b|\bpnpm\s+test\b|\bnpx\s+(jest|vitest)\b`)
	testCargoRE    = regexp.MustCompile(`\bcargo\s+test\b`)
	testGoRE       = regexp.MustCompile(`\bgo\s+test\b`)
	testRspecRE    = regexp.MustCompile(`\brspec\b|\bbundle\s+exec\s+rspec\b`)
	testDotnetRE   = regexp.MustCompile(`\bdotnet\s+test\b`)
	testSwiftRE    = regexp.MustCompile(`\bswift\s+test\b`)
	testMixRE      = regexp.MustCompile(`\bmix\s+test\b`)
	pytestCollectR = regexp.MustCompile(`^(collecting|collected)\s`)
	pytestHeaderR  = regexp.MustCompile(`^(platform|rootdir|configfile|plugins|cachedir)[\s:]`)
	pytestFailures = regexp.MustCompile(`^=+ FAILURES =+`)
	pytestWarnHdr  = regexp.MustCompile(`^=+ warnings summary =+`)
	pytestSection  = regexp.MustCompile(`^=+.*=+$`)
	pytestFailHdr  = regexp.MustCompile(`^_+ .+ _+$`)
	pytestEndFailR = regexp.MustCompile(`^=+ (short test summary|warnings summary|\d+ (failed|passed|error))`)
	pytestPassedR  = regexp.MustCompile(`\bPASSED\b`)
	pytestParamOK  = regexp.MustCompile(`^(\S+?)\[(.+)\]\s+PASSED`)
	pytestFailErrR = regexp.MustCompile(`\bFAILED\b|\bERROR\b`)
	pytestParamNG  = regexp.MustCompile(`^(\S+?)\[(.+)\]\s+FAILED`)
	pytestShortR   = regexp.MustCompile(`^(FAILED|ERROR)\s`)
	covStartR      = regexp.MustCompile(`^-+ coverage`)
	covNameR       = regexp.MustCompile(`^Name\s+Stmts\s+Miss`)
	covTotalR      = regexp.MustCompile(`^TOTAL\s+`)
	covRowR        = regexp.MustCompile(`^(\S+)\s+\d+\s+\d+\s+(\d+)%`)
	warningTypeR   = regexp.MustCompile(`(\w+Warning):\s*(.+)`)
	warnLocR       = regexp.MustCompile(`^\s*/`)
	warnLoc2R      = regexp.MustCompile(`^\s+\w+`)
	jestFailR      = regexp.MustCompile(`\bFAIL\b`)
	jestHdrR       = regexp.MustCompile(`^(Tests?|Test Suites?):`)
	jestPassR      = regexp.MustCompile(`\bPASS\b`)
	jestTestsR     = regexp.MustCompile(`\((\d+)\s+tests?\)`)
	jestSummaryR   = regexp.MustCompile(`^(Tests?|Test Suites?|Snapshots?|Time|Ran all):`)
	cargoSkipR     = regexp.MustCompile(`^\s*(Compiling|Downloading|Running|Doc-tests)`)
	goPkgR         = regexp.MustCompile(`^(FAIL|ok)\s+\S+`)
	rspecTotalR    = regexp.MustCompile(`^\d+ examples?, \d+ failures?`)
	rspecDotsR     = regexp.MustCompile(`^[.FE*P]+$`)
	checkmarkR     = regexp.MustCompile(`^\s*(✓|✔)`)
	dotnetSkipR    = regexp.MustCompile(`^\s*(Build|Restore|Determining|Microsoft)`)
	dotnetPassR    = regexp.MustCompile(`\bPassed\b`)
	dotnetFailR    = regexp.MustCompile(`\bFailed\b`)
	dotnetEndR     = regexp.MustCompile(`^(Total|Passed|Failed|Skipped)\s`)
	dotnetSumR     = regexp.MustCompile(`^(Total tests|Passed|Failed|Skipped|Test Run)`)
	swiftSkipR     = regexp.MustCompile(`^\s*(Build|Compile|Link|Fetch|Creating)`)
	swiftErrR      = regexp.MustCompile(`\bfailed\b|\bFailed\b|\berror\b`)
	swiftExecR     = regexp.MustCompile(`^Executed \d+`)
	mixSkipR       = regexp.MustCompile(`^\s*(Compiling|Generated)\s`)
	mixDotsR       = regexp.MustCompile(`^\.+$`)
	mixFailR       = regexp.MustCompile(`(?i)\bfailure\b|\bfailed\b`)
	mixSumR        = regexp.MustCompile(`^\d+\s+(tests?|doctests?)`)
	mixFinR        = regexp.MustCompile(`^Finished in`)
	genericSumR    = regexp.MustCompile(`^\d+\s+(tests?|specs?|examples?)`)
)

// CanHandle reports whether the command invokes a supported test tool.
func (*TestProcessor) CanHandle(command string) bool {
	return testCmdRE.MatchString(command)
}

// Process compresses test output by runner.
func (t *TestProcessor) Process(command, output string, _ int) string {
	if output == "" || strings.TrimSpace(output) == "" {
		return output
	}
	lines := util.SplitLines(output)
	switch {
	case testPytestRouteRE.MatchString(command):
		return t.processPytest(lines)
	case testJestRouteRE.MatchString(command):
		return t.processJest(lines)
	case testCargoRE.MatchString(command):
		return t.processCargoTest(lines)
	case testGoRE.MatchString(command):
		return t.processGoTest(lines)
	case testRspecRE.MatchString(command):
		return t.processRspec(lines)
	case testDotnetRE.MatchString(command):
		return t.processDotnetTest(lines)
	case testSwiftRE.MatchString(command):
		return t.processSwiftTest(lines)
	case testMixRE.MatchString(command):
		return t.processMixTest(lines)
	default:
		return t.processGenericTest(lines)
	}
}

// truncateTraceback truncates a failure/traceback block to the configured
// maximum by keeping the first and last halves around a marker line.
func truncateTraceback(block []string) []string {
	maxLines := config.Get().MaxTracebackLines
	if len(block) <= maxLines {
		return block
	}
	keepHead := maxLines / 2
	keepTail := maxLines - keepHead
	omitted := len(block) - keepHead - keepTail
	out := make([]string, 0, maxLines+1)
	out = append(out, block[:keepHead]...)
	out = append(out, fmt.Sprintf("    ... (%d traceback lines truncated)", omitted))
	return append(out, block[len(block)-keepTail:]...)
}

type paramInfo struct {
	passed int
	failed []string
}

func (t *TestProcessor) processPytest(lines []string) string {
	var result []string
	inFailure := false
	inWarnings := false
	var failureBlock []string
	var warningLines []string
	var summaryLines []string
	passedCount := 0
	// paramTests preserves dict insertion order.
	paramTests := map[string]*paramInfo{}
	var paramOrder []string

	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if pytestCollectR.MatchString(stripped) {
			continue
		}
		if pytestHeaderR.MatchString(stripped) {
			continue
		}

		if pytestFailures.MatchString(line) {
			inFailure = true
			inWarnings = false
			result = append(result, line)
			continue
		}

		if pytestWarnHdr.MatchString(line) {
			inWarnings = true
			inFailure = false
			if len(failureBlock) > 0 {
				result = append(result, truncateTraceback(failureBlock)...)
				failureBlock = nil
			}
			continue
		}

		if inWarnings {
			if pytestSection.MatchString(line) {
				inWarnings = false
				if len(warningLines) > 0 {
					result = append(result, collapseWarnings(warningLines)...)
					warningLines = nil
				}
				summaryLines = append(summaryLines, line)
			} else {
				if stripped != "" && !strings.HasPrefix(stripped, "--") {
					warningLines = append(warningLines, stripped)
				}
			}
			continue
		}

		if inFailure {
			if pytestFailHdr.MatchString(line) {
				if len(failureBlock) > 0 {
					result = append(result, truncateTraceback(failureBlock)...)
					failureBlock = nil
				}
				result = append(result, line)
				continue
			}
			if pytestEndFailR.MatchString(line) {
				inFailure = false
				if len(failureBlock) > 0 {
					result = append(result, truncateTraceback(failureBlock)...)
					failureBlock = nil
				}
				if strings.Contains(line, "warnings summary") {
					inWarnings = true
				} else {
					result = append(result, line)
				}
			} else if pytestSection.MatchString(line) && !strings.Contains(line, "FAILURES") {
				inFailure = false
				if len(failureBlock) > 0 {
					result = append(result, truncateTraceback(failureBlock)...)
					failureBlock = nil
				}
				result = append(result, line)
			} else {
				failureBlock = append(failureBlock, line)
			}
			continue
		}

		if pytestPassedR.MatchString(line) {
			passedCount++
			if m := pytestParamOK.FindStringSubmatch(stripped); m != nil {
				name := m[1]
				if _, ok := paramTests[name]; !ok {
					paramTests[name] = &paramInfo{}
					paramOrder = append(paramOrder, name)
				}
				paramTests[name].passed++
			}
			continue
		}

		if pytestFailErrR.MatchString(line) {
			if m := pytestParamNG.FindStringSubmatch(stripped); m != nil {
				name := m[1]
				if _, ok := paramTests[name]; !ok {
					paramTests[name] = &paramInfo{}
					paramOrder = append(paramOrder, name)
				}
				paramTests[name].failed = append(paramTests[name].failed, m[2])
			} else {
				result = append(result, line)
			}
			continue
		}

		if pytestSection.MatchString(line) && !strings.Contains(line, "test session starts") {
			summaryLines = append(summaryLines, line)
			continue
		}

		if pytestShortR.MatchString(stripped) {
			result = append(result, line)
		}
	}

	if len(warningLines) > 0 {
		result = append(result, collapseWarnings(warningLines)...)
	}
	if len(failureBlock) > 0 {
		result = append(result, truncateTraceback(failureBlock)...)
	}

	for _, name := range paramOrder {
		info := paramTests[name]
		if len(info.failed) > 0 {
			total := info.passed + len(info.failed)
			shown := info.failed
			extra := ""
			if len(shown) > 5 {
				extra = fmt.Sprintf(", ... (%d more)", len(shown)-5)
				shown = shown[:5]
			}
			result = append(result, fmt.Sprintf("%s: %d/%d passed, FAILED: [%s%s]",
				name, info.passed, total, strings.Join(shown, ", "), extra))
		}
	}

	if passedCount > 0 {
		result = append([]string{fmt.Sprintf("[%d tests passed]", passedCount)}, result...)
	}

	if coverageLines := extractCoverage(lines); len(coverageLines) > 0 {
		result = append(result, compressCoverage(coverageLines)...)
	}

	result = append(result, summaryLines...)
	if len(result) == 0 {
		return util.JoinLines(lines)
	}
	return util.JoinLines(result)
}

// extractCoverage extracts the coverage table lines from pytest output.
func extractCoverage(lines []string) []string {
	coverageStart := -1
	coverageEnd := -1
	for i, line := range lines {
		stripped := strings.TrimSpace(line)
		if coverageStart < 0 && (covStartR.MatchString(stripped) || covNameR.MatchString(stripped)) {
			coverageStart = i
		}
		if coverageStart >= 0 && i > coverageStart && covTotalR.MatchString(stripped) {
			coverageEnd = i
			break
		}
	}
	if coverageStart < 0 {
		return nil
	}
	end := len(lines)
	if coverageEnd >= 0 {
		end = coverageEnd + 1
	}
	return lines[coverageStart:end]
}

// compressCoverage keeps the TOTAL line and files below 80%.
func compressCoverage(lines []string) []string {
	var result []string
	totalLine := ""
	var low []string
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "TOTAL") {
			totalLine = stripped
			continue
		}
		if strings.HasPrefix(stripped, "Name") || strings.HasPrefix(stripped, "-") {
			continue
		}
		if m := covRowR.FindStringSubmatch(stripped); m != nil {
			if pct, err := strconv.Atoi(m[2]); err == nil && pct < 80 {
				low = append(low, stripped)
			}
		}
	}
	if totalLine != "" {
		result = append(result, totalLine)
	}
	if len(low) > 0 {
		result = append(result, fmt.Sprintf("Files below 80%% coverage (%d):", len(low)))
		for _, f := range low[:min(10, len(low))] {
			result = append(result, "  "+f)
		}
		if len(low) > 10 {
			result = append(result, fmt.Sprintf("  ... (%d more)", len(low)-10))
		}
	}
	return result
}

// collapseWarnings groups warnings by type and keeps one example.
func collapseWarnings(warningLines []string) []string {
	byType := map[string][]string{}
	var typeOrder []string
	for _, line := range warningLines {
		if m := warningTypeR.FindStringSubmatch(line); m != nil {
			wtype := m[1]
			if _, ok := byType[wtype]; !ok {
				typeOrder = append(typeOrder, wtype)
			}
			byType[wtype] = append(byType[wtype], line)
		} else if warnLocR.MatchString(line) || warnLoc2R.MatchString(line) {
			continue
		} else {
			if _, ok := byType["other"]; !ok {
				typeOrder = append(typeOrder, "other")
			}
			byType["other"] = append(byType["other"], line)
		}
	}
	if len(byType) == 0 {
		return nil
	}

	entries := make([]string, len(typeOrder))
	copy(entries, typeOrder)
	sort.SliceStable(entries, func(i, j int) bool {
		return len(byType[entries[i]]) > len(byType[entries[j]])
	})

	total := 0
	for _, k := range typeOrder {
		total += len(byType[k])
	}
	var parts []string
	for _, k := range entries {
		if k == "other" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s x%d", k, len(byType[k])))
	}
	var result []string
	if len(parts) > 0 {
		result = append(result, fmt.Sprintf("Warnings (%d): %s", total, strings.Join(parts, ", ")))
		// First maximum in insertion order, matching Python's max().
		mostCommon := typeOrder[0]
		for _, k := range typeOrder[1:] {
			if len(byType[k]) > len(byType[mostCommon]) {
				mostCommon = k
			}
		}
		if len(byType[mostCommon]) > 0 {
			result = append(result, "  e.g. "+byType[mostCommon][0])
		}
	}
	return result
}

func (t *TestProcessor) processJest(lines []string) string {
	var result []string
	inFailure := false
	passedSuites := 0
	var failureBuffer []string
	consecutiveBlanks := 0

	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if jestFailR.MatchString(line) && !jestHdrR.MatchString(stripped) {
			inFailure = true
			consecutiveBlanks = 0
			result = append(result, line)
			continue
		}
		if inFailure {
			failureBuffer = append(failureBuffer, line)
			if stripped == "" {
				consecutiveBlanks++
				if consecutiveBlanks >= 2 {
					result = append(result, truncateTraceback(failureBuffer)...)
					failureBuffer = nil
					inFailure = false
					consecutiveBlanks = 0
				}
			} else {
				consecutiveBlanks = 0
			}
			continue
		}
		if jestPassR.MatchString(line) && !jestHdrR.MatchString(stripped) {
			passedSuites++
			continue
		}
		if jestSummaryR.MatchString(stripped) {
			result = append(result, line)
		}
	}
	if len(failureBuffer) > 0 {
		result = append(result, truncateTraceback(failureBuffer)...)
	}
	if passedSuites > 0 {
		result = append([]string{fmt.Sprintf("[%d suites passed]", passedSuites)}, result...)
	}
	if len(result) == 0 {
		return util.JoinLines(lines)
	}
	return util.JoinLines(result)
}

func (t *TestProcessor) processCargoTest(lines []string) string {
	var result []string
	inFailure := false
	okCount := 0
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "test ") && strings.Contains(stripped, "... ok") {
			okCount++
			continue
		}
		if strings.Contains(stripped, "FAILED") {
			inFailure = true
			result = append(result, line)
			continue
		}
		if inFailure {
			result = append(result, line)
			if strings.HasPrefix(stripped, "test result:") {
				inFailure = false
			}
			continue
		}
		if strings.HasPrefix(stripped, "test result:") {
			result = append(result, line)
			continue
		}
		if cargoSkipR.MatchString(stripped) {
			continue
		}
	}
	if okCount > 0 {
		result = append([]string{fmt.Sprintf("[%d tests passed]", okCount)}, result...)
	}
	if len(result) == 0 {
		return util.JoinLines(lines)
	}
	return util.JoinLines(result)
}

func (t *TestProcessor) processGoTest(lines []string) string {
	var result []string
	passed := 0
	inFailure := false
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "--- PASS") {
			passed++
			continue
		}
		if strings.HasPrefix(stripped, "--- FAIL") {
			inFailure = true
			result = append(result, line)
			continue
		}
		if inFailure {
			result = append(result, line)
			if goPkgR.MatchString(stripped) {
				inFailure = false
			}
			continue
		}
		if goPkgR.MatchString(stripped) {
			result = append(result, line)
			continue
		}
	}
	if passed > 0 {
		result = append([]string{fmt.Sprintf("[%d tests passed]", passed)}, result...)
	}
	if len(result) == 0 {
		return util.JoinLines(lines)
	}
	return util.JoinLines(result)
}

func (t *TestProcessor) processRspec(lines []string) string {
	var result []string
	passed := 0
	inFailure := false
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if rspecTotalR.MatchString(stripped) {
			result = append(result, line)
			continue
		}
		if strings.Contains(stripped, "FAILED") || strings.Contains(stripped, "Failure/Error") {
			inFailure = true
			result = append(result, line)
			continue
		}
		if inFailure {
			result = append(result, line)
			if stripped == "" {
				inFailure = false
			}
			continue
		}
		if rspecDotsR.MatchString(stripped) {
			passed += strings.Count(stripped, ".")
			continue
		}
		if checkmarkR.MatchString(stripped) {
			passed++
			continue
		}
	}
	if passed > 0 {
		result = append([]string{fmt.Sprintf("[%d examples passed]", passed)}, result...)
	}
	if len(result) == 0 {
		return util.JoinLines(lines)
	}
	return util.JoinLines(result)
}

func (t *TestProcessor) processDotnetTest(lines []string) string {
	var result []string
	passed := 0
	inFailure := false
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if dotnetSkipR.MatchString(stripped) {
			continue
		}
		if (strings.HasPrefix(stripped, "Passed!") || dotnetPassR.MatchString(stripped)) &&
			!strings.Contains(strings.ToLower(stripped), "test") {
			passed++
			continue
		}
		if dotnetFailR.MatchString(stripped) {
			inFailure = true
			result = append(result, line)
			continue
		}
		if inFailure {
			result = append(result, line)
			if stripped == "" || dotnetEndR.MatchString(stripped) {
				inFailure = false
			}
			continue
		}
		if dotnetSumR.MatchString(stripped) {
			result = append(result, line)
		}
	}
	if passed > 0 {
		result = append([]string{fmt.Sprintf("[%d tests passed]", passed)}, result...)
	}
	if len(result) == 0 {
		return util.JoinLines(lines)
	}
	return util.JoinLines(result)
}

func (t *TestProcessor) processSwiftTest(lines []string) string {
	var result []string
	passed := 0
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if swiftSkipR.MatchString(stripped) {
			continue
		}
		lower := strings.ToLower(stripped)
		if strings.Contains(lower, "passed") && !strings.Contains(lower, "test") {
			passed++
			continue
		}
		if swiftErrR.MatchString(stripped) {
			result = append(result, line)
			continue
		}
		if strings.HasPrefix(stripped, "Test Suite") || swiftExecR.MatchString(stripped) {
			result = append(result, line)
		}
	}
	if passed > 0 {
		result = append([]string{fmt.Sprintf("[%d tests passed]", passed)}, result...)
	}
	if len(result) == 0 {
		return util.JoinLines(lines)
	}
	return util.JoinLines(result)
}

func (t *TestProcessor) processMixTest(lines []string) string {
	var result []string
	passed := 0
	inFailure := false
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if mixSkipR.MatchString(stripped) {
			continue
		}
		if mixDotsR.MatchString(stripped) {
			passed += len(stripped)
			continue
		}
		if mixFailR.MatchString(stripped) {
			inFailure = true
			result = append(result, line)
			continue
		}
		if inFailure {
			result = append(result, line)
			if stripped == "" {
				inFailure = false
			}
			continue
		}
		if mixSumR.MatchString(stripped) {
			result = append(result, line)
		}
		if mixFinR.MatchString(stripped) {
			result = append(result, line)
		}
	}
	if passed > 0 {
		result = append([]string{fmt.Sprintf("[%d tests passed]", passed)}, result...)
	}
	if len(result) == 0 {
		return util.JoinLines(lines)
	}
	return util.JoinLines(result)
}

func (t *TestProcessor) processGenericTest(lines []string) string {
	var result []string
	passed := 0
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "fail") || strings.Contains(lower, "error") ||
			strings.Contains(lower, "assert") || strings.Contains(lower, "exception") ||
			strings.Contains(lower, "traceback") {
			result = append(result, line)
		} else if strings.Contains(lower, "pass") || strings.Contains(lower, "ok ") ||
			strings.Contains(lower, "success") ||
			checkmarkR.MatchString(strings.TrimSpace(line)) {
			passed++
		} else if genericSumR.MatchString(strings.TrimSpace(line)) {
			result = append(result, line)
		}
	}
	if passed > 0 {
		result = append([]string{fmt.Sprintf("[%d tests passed]", passed)}, result...)
	}
	if len(result) == 0 {
		if len(lines) > 10 {
			return util.JoinLines(lines[len(lines)-10:])
		}
		return util.JoinLines(lines)
	}
	return util.JoinLines(result)
}
