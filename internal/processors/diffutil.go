package processors

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Shared helpers ported from token-saver src/processors/utils.py.

var defaultErrorRE = regexp.MustCompile(
	`\b(error|Error|ERROR|exception|Exception|EXCEPTION|` +
		`fatal|Fatal|FATAL|panic|Panic|PANIC|traceback|Traceback)\b`)

var diffStatLineRE = regexp.MustCompile(`^\s*\d+ files? changed`)

// compressDiff compresses a unified diff (ports utils.compress_diff).
func compressDiff(lines []string, maxHunk, maxContext int) []string {
	var result []string
	hunkLineCount := 0
	hunkTruncated := false
	statLine := ""
	var leadingBuffer []string
	trailingRemaining := 0
	inHunk := false

	marker := fmt.Sprintf("  ... (truncated after %d lines)", maxHunk)

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git"):
			leadingBuffer = nil
			trailingRemaining = 0
			inHunk = false
			if hunkTruncated {
				result = append(result, marker)
			}
			result = append(result, line)
			hunkLineCount = 0
			hunkTruncated = false
		case strings.HasPrefix(line, "@@"):
			leadingBuffer = nil
			trailingRemaining = 0
			inHunk = true
			if hunkTruncated {
				result = append(result, marker)
			}
			result = append(result, line)
			hunkLineCount = 0
			hunkTruncated = false
		case !inHunk && (strings.HasPrefix(line, "index ") ||
			strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ")):
			// skip file-header bookkeeping lines
		case !inHunk && hasAnyPrefix(line, "Binary files", "rename ", "copy ",
			"similarity ", "dissimilarity ", "new file mode", "deleted file mode",
			"old mode", "new mode"):
			// Preserve metadata for diffs with no hunk body (binary, renames).
			result = append(result, line)
		case strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-"):
			hunkLineCount++
			if hunkLineCount <= maxHunk {
				if len(leadingBuffer) > 0 {
					if len(leadingBuffer) > maxContext {
						leadingBuffer = leadingBuffer[len(leadingBuffer)-maxContext:]
					}
					result = append(result, leadingBuffer...)
					leadingBuffer = nil
				}
				result = append(result, line)
				trailingRemaining = maxContext
			} else if !hunkTruncated {
				hunkTruncated = true
			}
		case strings.HasPrefix(line, " "):
			hunkLineCount++
			if hunkLineCount <= maxHunk {
				if trailingRemaining > 0 {
					result = append(result, line)
					trailingRemaining--
				} else {
					leadingBuffer = append(leadingBuffer, line)
				}
			} else if !hunkTruncated {
				hunkTruncated = true
			}
		case diffStatLineRE.MatchString(line):
			statLine = line
		}
	}

	if hunkTruncated {
		result = append(result, marker)
	}
	if statLine != "" {
		result = append(result, statLine)
	}
	return result
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// groupPathsByDir buckets paths by parent directory, preserving encounter
// order (ports utils.group_paths_by_dir). Callers need deterministic order,
// so this returns a slice of pairs instead of a map.
type dirGroup struct {
	Dir   string
	Files []string
}

func groupPathsByDir(paths []string) []dirGroup {
	var order []string
	byDir := map[string][]string{}
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		dir := "."
		name := path
		if i := strings.LastIndex(path, "/"); i >= 0 {
			dir = path[:i]
			name = path[i+1:]
		}
		if _, ok := byDir[dir]; !ok {
			order = append(order, dir)
		}
		byDir[dir] = append(byDir[dir], name)
	}
	groups := make([]dirGroup, 0, len(order))
	for _, d := range order {
		groups = append(groups, dirGroup{Dir: d, Files: byDir[d]})
	}
	return groups
}

// formatDirGroup renders one directory bucket (ports utils.format_dir_group).
func formatDirGroup(dirPath string, files []string, extThreshold int) []string {
	if len(files) > extThreshold {
		exts := map[string]int{}
		for _, f := range files {
			ext := "(none)"
			if i := strings.LastIndex(f, "."); i >= 0 {
				ext = f[i+1:]
			}
			exts[ext]++
		}
		type kv struct {
			ext string
			n   int
		}
		items := make([]kv, 0, len(exts))
		for e, n := range exts {
			items = append(items, kv{e, n})
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].n > items[j].n })
		if len(items) > 4 {
			items = items[:4]
		}
		parts := make([]string, 0, len(items))
		for _, it := range items {
			parts = append(parts, fmt.Sprintf("*.%s:%d", it.ext, it.n))
		}
		return []string{fmt.Sprintf("  %s/ (%d files: %s)", dirPath, len(files), strings.Join(parts, ", "))}
	}
	if len(files) > 5 {
		return []string{fmt.Sprintf("  %s/ (%d files): %s ...", dirPath, len(files), strings.Join(files[:3], ", "))}
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, fmt.Sprintf("  %s/%s", dirPath, f))
	}
	return out
}

// compressLogLines keeps head/tail plus error-context lines in the middle
// (ports utils.compress_log_lines). errorRE nil uses the shared default.
func compressLogLines(lines []string, keepHead, keepTail int, errorRE *regexp.Regexp, contextLines, maxErrorLines int) string {
	if len(lines) <= keepHead+keepTail {
		return strings.Join(lines, "\n")
	}
	errRE := errorRE
	if errRE == nil {
		errRE = defaultErrorRE
	}
	head := lines[:keepHead]
	tail := lines[len(lines)-keepTail:]
	middle := lines[keepHead : len(lines)-keepTail]

	errorSet := map[int]bool{}
	for idx, line := range middle {
		if errRE.MatchString(line) {
			for c := idx - contextLines; c <= idx+contextLines; c++ {
				if c >= 0 && c < len(middle) {
					errorSet[c] = true
				}
			}
		}
	}

	result := append([]string{}, head...)

	if len(middle) > 0 {
		if len(errorSet) > 0 {
			sorted := make([]int, 0, len(errorSet))
			for i := range errorSet {
				sorted = append(sorted, i)
			}
			sort.Ints(sorted)
			result = append(result, fmt.Sprintf("\n... (%d total lines, showing errors) ...\n", len(lines)))
			prev := -2
			for _, idx := range sorted {
				if idx > prev+1 && prev >= 0 {
					gap := idx - prev - 1
					result = append(result, fmt.Sprintf("  ... (%d lines skipped)", gap))
				}
				result = append(result, middle[idx])
				prev = idx
			}
			// Cap error output (Python slices the result list, then appends marker).
			if len(sorted) > maxErrorLines {
				result = result[:keepHead+1+maxErrorLines]
				result = append(result, fmt.Sprintf("  ... (%d more error lines)", len(sorted)-maxErrorLines))
			}
		} else {
			result = append(result, fmt.Sprintf("\n... (%d lines truncated) ...\n", len(lines)-keepHead-keepTail))
		}
	}

	result = append(result, tail...)
	return strings.Join(result, "\n")
}
