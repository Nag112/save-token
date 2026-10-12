package processors

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Nag112/save-token/internal/config"
	"github.com/Nag112/save-token/internal/util"
)

// GitProcessor summarizes Git status, changes, history, transfers, branches
// (ports processors/git.py GitProcessor).
type GitProcessor struct{ Base }

const gitOpts = `(?:-C\s+\S+\s+|--no-pager\s+|-c\s+\S+\s+` +
	`|--git-dir(?:=|\s+)\S+\s+|--work-tree(?:=|\s+)\S+\s+)*`

const gitSubCmds = `(status|diff|log|show|push|pull|fetch|clone|branch|stash` +
	`|reflog|remote|blame|cherry-pick|rebase|merge)`

var gitCmdRE = regexp.MustCompile(`\bgit\s+` + gitOpts + gitSubCmds + `\b`)

var gitStatusLineRE = regexp.MustCompile(`^([MADRCTU?! ]{1,2})\s+(.+)$`)
var gitDiffFileRE = regexp.MustCompile(`^diff --git a/(.+?) b/`)
var gitNameStatusRE = regexp.MustCompile(`^([MADRCTU])\d*\s+(.+)$`)
var gitStatLineRE = regexp.MustCompile(`^\s*.+?\s+\|\s+\d+`)
var gitStatStripRE = regexp.MustCompile(`^(\s*.+?\s+\|\s+\d+)\s+[+\-]+\s*$`)
var gitStatGroupRE = regexp.MustCompile(`^\s*(.+?)\s+\|\s+(.+)$`)
var gitStatDigitsRE = regexp.MustCompile(`(\d+)`)
var gitBarsRE = regexp.MustCompile(`\s+[+\-]+\s*$`)
var gitGraphLineRE = regexp.MustCompile(`^[|*/\\ ]*[|*/\\]`)
var gitTransferRE = regexp.MustCompile(`^(Receiving|Resolving|Counting|Compressing|` +
	`remote:\s*(Counting|Compressing|Total|Enumerating))`)
var gitPercentRE = regexp.MustCompile(`\d+%`)
var gitFilesChangedRE = regexp.MustCompile(`^\s*\d+ files? changed`)
var gitBlameRE = regexp.MustCompile(`^[0-9a-f]+\s+\((.+?)\s+\d{4}-\d{2}-\d{2}\s+`)
var gitBlameShortRE = regexp.MustCompile(`^\^?[0-9a-f]+\s+\(`)
var gitBlameShort2RE = regexp.MustCompile(`^\^?[0-9a-f]+\s+\((.+?)\s+\d{4}`)
var gitRemoteDupRE = regexp.MustCompile(`\s+\((fetch|push)\)\s*$`)
var gitStashListRE = regexp.MustCompile(`\bstash\s+list\b`)

var gitLockFiles = map[string]bool{
	"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
	"poetry.lock": true, "Pipfile.lock": true, "Cargo.lock": true,
	"composer.lock": true, "Gemfile.lock": true, "go.sum": true,
	"bun.lockb": true,
}

// NewGit returns the git processor.
func NewGit() *GitProcessor { return &GitProcessor{} }

// Name is the routing identifier.
func (*GitProcessor) Name() string { return "git" }

// Priority orders routing.
func (*GitProcessor) Priority() int { return 20 }

// HandlesFailure reports failure output is summarized too.
func (*GitProcessor) HandlesFailure() bool { return true }

// HookPatterns are anchored command matches.
func (*GitProcessor) HookPatterns() []string {
	return []string{`^git\s+` + gitOpts + gitSubCmds + `\b`}
}

// CanHandle reports whether the command invokes a handled git subcommand.
func (*GitProcessor) CanHandle(command string) bool {
	return gitCmdRE.MatchString(command)
}

func (*GitProcessor) subcmd(command string) string {
	if m := gitCmdRE.FindStringSubmatch(command); m != nil {
		return m[1]
	}
	return ""
}

// Process compresses git output by subcommand.
func (g *GitProcessor) Process(command, output string, _ int) string {
	if output == "" || strings.TrimSpace(output) == "" {
		return output
	}
	switch g.subcmd(command) {
	case "status":
		return g.processStatus(output)
	case "diff":
		return g.processDiff(output, command)
	case "log":
		return g.processLog(output, command)
	case "show":
		return g.processShow(output)
	case "push", "pull", "fetch", "clone":
		return g.processTransfer(output)
	case "branch":
		return g.processBranch(output)
	case "stash":
		if gitStashListRE.MatchString(command) {
			return g.processStashList(output)
		}
		return output
	case "reflog":
		return g.processReflog(output)
	case "blame":
		return g.processBlame(output)
	case "remote":
		return g.processRemote(output)
	case "cherry-pick", "rebase", "merge":
		return g.processTransfer(output)
	}
	return output
}

type longStatusKind struct {
	prefix string
	code   string
}

var longStatusKinds = []longStatusKind{
	{"both modified:", "UU"}, {"both added:", "AA"}, {"both deleted:", "DD"},
	{"added by us:", "AU"}, {"added by them:", "UA"}, {"deleted by us:", "DU"},
	{"deleted by them:", "UD"},
	{"modified:", "M"}, {"new file:", "A"}, {"deleted:", "D"},
	{"renamed:", "R"}, {"copied:", "C"}, {"typechange:", "T"},
}

func (g *GitProcessor) processStatus(output string) string {
	lines := util.SplitLines(strings.TrimSpace(output))
	counts := map[string]int{}
	dirOrder := []string{}
	filesByDir := map[string][]string{}
	var headerLines []string
	inUntracked := false

	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			continue
		}
		if strings.HasPrefix(stripped, "## ") {
			branch := strings.SplitN(stripped[3:], "...", 2)[0]
			headerLines = append(headerLines, "On branch "+branch)
			continue
		}
		if hasAnyPrefix(stripped, "On branch", "Your branch", "HEAD detached") {
			headerLines = append(headerLines, stripped)
			inUntracked = false
			continue
		}
		if hasAnyPrefix(stripped, "nothing to commit", "no changes added") {
			headerLines = append(headerLines, stripped)
			inUntracked = false
			continue
		}
		if strings.HasPrefix(stripped, "Untracked files:") {
			inUntracked = true
			continue
		}
		if hasAnyPrefix(stripped, "Changes", "Unmerged") {
			inUntracked = false
			continue
		}
		if strings.HasPrefix(stripped, "(") {
			continue
		}

		code, filepath := "", ""
		matched := false
		for _, k := range longStatusKinds {
			if strings.HasPrefix(stripped, k.prefix) {
				code = k.code
				filepath = strings.TrimSpace(strings.SplitN(stripped, ":", 2)[1])
				matched = true
				break
			}
		}
		if !matched {
			if m := gitStatusLineRE.FindStringSubmatch(stripped); m != nil {
				codeRaw := strings.TrimSpace(m[1])
				filepath = strings.Trim(strings.TrimSpace(m[2]), `"`)
				if codeRaw[0] != ' ' {
					code = string(codeRaw[0])
				} else {
					code = codeRaw[len(codeRaw)-1:]
				}
				matched = true
			} else if inUntracked && !strings.HasPrefix(stripped, "(") {
				code, filepath = "?", stripped
				matched = true
			}
		}
		if !matched {
			continue
		}

		counts[code]++
		dirName, fileName := splitPath(filepath)
		if _, ok := filesByDir[dirName]; !ok {
			dirOrder = append(dirOrder, dirName)
		}
		filesByDir[dirName] = append(filesByDir[dirName], code+" "+fileName)
	}

	var result []string
	if len(headerLines) > 0 {
		result = append(result, strings.Join(headerLines, " | "))
	}
	codes := sortedKeys(counts)
	var summaryParts []string
	total := 0
	for _, k := range codes {
		if counts[k] > 0 {
			summaryParts = append(summaryParts, fmt.Sprintf("%s:%d", k, counts[k]))
			total += counts[k]
		}
	}
	if len(summaryParts) > 0 {
		result = append(result, fmt.Sprintf("Files: %d (%s)", total, strings.Join(summaryParts, ", ")))
	}
	dirs := make([]string, 0, len(filesByDir))
	for d := range filesByDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, dirName := range dirs {
		files := filesByDir[dirName]
		if len(files) > 8 {
			codes := map[string]int{}
			for _, f := range files {
				codes[strings.SplitN(f, " ", 2)[0]]++
			}
			var parts []string
			for _, c := range sortedKeys(codes) {
				parts = append(parts, fmt.Sprintf("%s:%d", c, codes[c]))
			}
			result = append(result, fmt.Sprintf("  %s/ (%d files: %s)", dirName, len(files), strings.Join(parts, ", ")))
		} else {
			for _, f := range files {
				result = append(result, fmt.Sprintf("  %s/%s", dirName, f))
			}
		}
	}
	if len(result) == 0 {
		return output
	}
	return strings.Join(result, "\n")
}

func splitPath(filepath string) (dir, name string) {
	if i := strings.LastIndex(filepath, "/"); i >= 0 {
		return filepath[:i], filepath[i+1:]
	}
	return ".", filepath
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (g *GitProcessor) processDiff(output, command string) string {
	lines := util.SplitLines(output)

	if strings.Contains(command, "--name-only") || strings.Contains(command, "--name-status") {
		return g.processNameList(lines)
	}

	hasDiffHeader := false
	for _, l := range lines {
		if strings.HasPrefix(l, "diff --git") {
			hasDiffHeader = true
			break
		}
	}
	if len(lines) > 0 && !hasDiffHeader {
		return g.processDiffStat(lines)
	}

	var nonLock []string
	var lockSummaries []string
	currentFile := ""
	currentFileLines := 0
	inLockfile := false

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			if inLockfile && currentFile != "" {
				lockSummaries = append(lockSummaries,
					fmt.Sprintf("diff --git %s", currentFile),
					fmt.Sprintf("  (lockfile changed, %d lines)", currentFileLines))
			}
			filename := ""
			if m := gitDiffFileRE.FindStringSubmatch(line); m != nil {
				_, filename = splitPath(m[1])
			}
			inLockfile = gitLockFiles[filename]
			if inLockfile {
				currentFile = filename
				currentFileLines = 0
			} else {
				nonLock = append(nonLock, line)
			}
			continue
		}
		if inLockfile {
			currentFileLines++
			continue
		}
		nonLock = append(nonLock, line)
	}
	if inLockfile && currentFile != "" {
		lockSummaries = append(lockSummaries,
			fmt.Sprintf("diff --git %s", currentFile),
			fmt.Sprintf("  (lockfile changed, %d lines)", currentFileLines))
	}

	cfg := config.Get()
	maxHunk := cfg.MaxDiffHunkLines
	maxContext := cfg.MaxDiffContextLines
	if len(nonLock) < 200 && maxContext > 1 {
		maxContext = 1
	}
	hasDiffHeader = false
	for _, l := range nonLock {
		if strings.HasPrefix(l, "diff --git") {
			hasDiffHeader = true
			break
		}
	}
	if hasDiffHeader {
		result := compressDiff(nonLock, maxHunk, maxContext)
		result = append(result, lockSummaries...)
		return strings.Join(result, "\n")
	}
	if len(lockSummaries) > 0 {
		return strings.Join(lockSummaries, "\n")
	}
	return strings.Join(nonLock, "\n")
}

func (g *GitProcessor) processNameList(lines []string) string {
	if len(lines) <= 20 {
		return strings.Join(lines, "\n")
	}
	byDir := map[string][]string{}
	dirOrder := []string{}
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			continue
		}
		filepath := stripped
		if m := gitNameStatusRE.FindStringSubmatch(stripped); m != nil {
			filepath = m[2]
		}
		dirName, _ := splitPath(filepath)
		if _, ok := byDir[dirName]; !ok {
			dirOrder = append(dirOrder, dirName)
		}
		byDir[dirName] = append(byDir[dirName], stripped)
	}
	total := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			total++
		}
	}
	result := []string{fmt.Sprintf("%d files changed:", total)}
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, dirName := range dirs {
		files := byDir[dirName]
		if len(files) > 5 {
			result = append(result, fmt.Sprintf("  %s/ (%d files)", dirName, len(files)))
		} else {
			for _, f := range files {
				result = append(result, "  "+f)
			}
		}
	}
	return strings.Join(result, "\n")
}

func (g *GitProcessor) processDiffStat(lines []string) string {
	statCount := 0
	for _, line := range lines {
		if gitStatLineRE.MatchString(line) {
			statCount++
		}
	}
	if statCount > 20 {
		return g.groupStatByDir(lines)
	}
	var result []string
	for _, line := range lines {
		if m := gitStatStripRE.FindStringSubmatch(line); m != nil {
			result = append(result, m[1])
		} else {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}

type statEntry struct {
	path  string
	stats string
}

func (g *GitProcessor) groupStatByDir(lines []string) string {
	byDir := map[string][]statEntry{}
	dirOrder := []string{}
	summaryLine := ""

	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if gitFilesChangedRE.MatchString(stripped) {
			summaryLine = stripped
			continue
		}
		if m := gitStatGroupRE.FindStringSubmatch(stripped); m != nil {
			filepath := strings.TrimSpace(m[1])
			stats := strings.TrimSpace(m[2])
			dirName, _ := splitPath(filepath)
			if _, ok := byDir[dirName]; !ok {
				dirOrder = append(dirOrder, dirName)
			}
			byDir[dirName] = append(byDir[dirName], statEntry{filepath, stats})
		}
	}
	if len(byDir) == 0 {
		return strings.Join(lines, "\n")
	}

	// Sort by file count descending (stable to keep encounter order on ties).
	sort.SliceStable(dirOrder, func(i, j int) bool {
		return len(byDir[dirOrder[i]]) > len(byDir[dirOrder[j]])
	})

	var result []string
	for _, dirName := range dirOrder {
		files := byDir[dirName]
		if len(files) > 5 {
			totalChanges := 0
			for _, f := range files {
				if s := gitStatDigitsRE.FindStringSubmatch(f.stats); s != nil {
					n := 0
					fmt.Sscanf(s[1], "%d", &n)
					totalChanges += n
				}
			}
			result = append(result, fmt.Sprintf(" %s/ (%d files, ~%d changes)", dirName, len(files), totalChanges))
		} else {
			for _, f := range files {
				clean := gitBarsRE.ReplaceAllString(f.stats, "")
				result = append(result, fmt.Sprintf(" %s | %s", f.path, clean))
			}
		}
	}
	if summaryLine != "" {
		result = append(result, summaryLine)
	}
	return strings.Join(result, "\n")
}

func (g *GitProcessor) processLog(output, command string) string {
	maxEntries := config.Get().MaxLogEntries
	lines := util.SplitLines(output)

	hasGraph := strings.Contains(command, "--graph")
	if !hasGraph {
		lim := 10
		if len(lines) < lim {
			lim = len(lines)
		}
		for _, line := range lines[:lim] {
			if gitGraphLineRE.MatchString(line) {
				hasGraph = true
				break
			}
		}
	}
	if hasGraph {
		if len(lines) > maxEntries*4 {
			kept := append([]string{}, lines[:maxEntries*4]...)
			kept = append(kept, fmt.Sprintf("... (%d more lines)", len(lines)-maxEntries*4))
			return strings.Join(kept, "\n")
		}
		return output
	}

	if len(lines) > 0 && !strings.HasPrefix(lines[0], "commit ") {
		if len(lines) > maxEntries {
			return strings.Join(lines[:maxEntries], "\n") +
				fmt.Sprintf("\n... (%d more)", len(lines)-maxEntries)
		}
		return output
	}

	var entries [][]string
	var current []string
	for _, line := range lines {
		if strings.HasPrefix(line, "commit ") {
			if len(current) > 0 {
				entries = append(entries, current)
			}
			current = []string{line}
		} else {
			current = append(current, line)
		}
	}
	if len(current) > 0 {
		entries = append(entries, current)
	}

	var result []string
	lim := maxEntries
	if len(entries) < lim {
		lim = len(entries)
	}
	for _, entry := range entries[:lim] {
		commitHash, message := "", ""
		for _, line := range entry {
			if strings.HasPrefix(line, "commit ") {
				f := strings.Fields(line)
				if len(f) > 1 {
					h := f[1]
					if len(h) > 8 {
						h = h[:8]
					}
					commitHash = h
				}
			} else if strings.TrimSpace(line) != "" && message == "" &&
				!hasAnyPrefix(line, "Author:", "Merge:", "Date:") {
				message = strings.TrimSpace(line)
			}
		}
		result = append(result, commitHash+" "+message)
	}
	if len(entries) > maxEntries {
		result = append(result, fmt.Sprintf("... (%d more commits)", len(entries)-maxEntries))
	}
	return strings.Join(result, "\n")
}

func (g *GitProcessor) processShow(output string) string {
	lines := util.SplitLines(output)
	var header []string
	diffStart := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			diffStart = i
			break
		}
		header = append(header, line)
	}
	if diffStart == -1 {
		return output
	}
	compressedDiff := g.processDiff(strings.Join(lines[diffStart:], "\n"), "")

	var compactHeader []string
	for _, line := range header {
		stripped := strings.TrimSpace(line)
		if stripped != "" && !hasAnyPrefix(stripped, "Merge:", "Author:", "Date:") {
			compactHeader = append(compactHeader, line)
		}
	}
	return strings.Join(compactHeader, "\n") + "\n" + compressedDiff
}

func (g *GitProcessor) processTransfer(output string) string {
	lines := util.SplitLines(output)
	var important []string
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			continue
		}
		if gitTransferRE.MatchString(stripped) || gitPercentRE.MatchString(stripped) {
			continue
		}
		important = append(important, stripped)
	}
	if len(important) > 0 {
		return strings.Join(important, "\n")
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return strings.TrimSpace(lines[i])
		}
	}
	return output
}

func (g *GitProcessor) processBranch(output string) string {
	lines := util.SplitLines(strings.TrimSpace(output))
	threshold := config.Get().GitBranchThreshold
	if len(lines) <= threshold {
		return output
	}
	current := ""
	var branches []string
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "* ") {
			current = stripped
		} else {
			branches = append(branches, stripped)
		}
	}
	var result []string
	if current != "" {
		result = append(result, current)
	}
	result = append(result, fmt.Sprintf("(%d other branches)", len(branches)))
	lim := 5
	if len(branches) < lim {
		lim = len(branches)
	}
	for _, b := range branches[:lim] {
		result = append(result, "  "+b)
	}
	if len(branches) > 5 {
		result = append(result, fmt.Sprintf("  ... (%d more)", len(branches)-5))
	}
	return strings.Join(result, "\n")
}

func (g *GitProcessor) processStashList(output string) string {
	lines := util.SplitLines(strings.TrimSpace(output))
	threshold := config.Get().GitStashThreshold
	if len(lines) <= threshold {
		return output
	}
	return strings.Join(lines[:threshold], "\n") +
		fmt.Sprintf("\n... (%d more stashes)", len(lines)-threshold)
}

func (g *GitProcessor) processReflog(output string) string {
	lines := util.SplitLines(strings.TrimSpace(output))
	maxEntries := config.Get().MaxLogEntries
	if len(lines) <= maxEntries {
		return output
	}
	return strings.Join(lines[:maxEntries], "\n") +
		fmt.Sprintf("\n... (%d more entries)", len(lines)-maxEntries)
}

func (g *GitProcessor) processRemote(output string) string {
	lines := util.SplitLines(strings.TrimSpace(output))
	if len(lines) <= 10 {
		return output
	}
	seen := map[string]bool{}
	var result []string
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		key := gitRemoteDupRE.ReplaceAllString(stripped, "")
		if !seen[key] {
			seen[key] = true
			result = append(result, stripped)
		}
	}
	if len(result) < len(lines) {
		result = append(result, fmt.Sprintf("(%d total lines, fetch/push deduplicated)", len(lines)))
	}
	return strings.Join(result, "\n")
}

func (g *GitProcessor) processBlame(output string) string {
	lines := util.SplitLines(strings.TrimSpace(output))
	if len(lines) <= 20 {
		return output
	}
	byAuthor := map[string]int{}
	for _, line := range lines {
		if m := gitBlameRE.FindStringSubmatch(line); m != nil {
			byAuthor[strings.TrimSpace(m[1])]++
		} else if gitBlameShortRE.MatchString(line) {
			if m2 := gitBlameShort2RE.FindStringSubmatch(line); m2 != nil {
				byAuthor[strings.TrimSpace(m2[1])]++
			}
		}
	}
	if len(byAuthor) == 0 {
		if len(lines) > 50 {
			return strings.Join(lines[:40], "\n") +
				fmt.Sprintf("\n... (%d more lines)", len(lines)-40)
		}
		return output
	}

	recent := lines
	if len(recent) > 10 {
		recent = recent[len(recent)-10:]
	}

	type kv struct {
		author string
		count  int
	}
	items := make([]kv, 0, len(byAuthor))
	for a, c := range byAuthor {
		items = append(items, kv{a, c})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].count > items[j].count })

	result := []string{fmt.Sprintf("%d lines, %d authors:", len(lines), len(byAuthor))}
	for _, it := range items {
		pct := it.count * 100 / len(lines)
		result = append(result, fmt.Sprintf("  %s: %d lines (%d%%)", it.author, it.count, pct))
	}
	result = append(result, "", "Last 10 lines:")
	result = append(result, recent...)
	return strings.Join(result, "\n")
}
