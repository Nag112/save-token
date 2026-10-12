// Package diffstat summarises what compression removed, for --dry-run /
// --show-removed. Mirrors token-saver src/diffstat.py output shape using an
// LCS line diff instead of difflib.SequenceMatcher (dry-run tool only, so
// minor grouping differences are acceptable).
package diffstat

import (
	"fmt"
	"strings"

	"github.com/Nag112/save-token/internal/util"
)

// Summary holds the removed/added line and char counts plus samples.
type Summary struct {
	OriginalLines   int
	CompressedLines int
	LinesRemoved    int
	LinesAdded      int
	CharsRemoved    int
	RemovedSamples  []string
	AddedSamples    []string
}

const maxSamples = 5

// Summarize compares original vs compressed line lists.
func Summarize(original, compressed string) Summary {
	a := util.SplitLines(original)
	b := util.SplitLines(compressed)

	s := Summary{OriginalLines: len(a), CompressedLines: len(b)}
	s.CharsRemoved = runeLen(original) - runeLen(compressed)
	if s.CharsRemoved < 0 {
		s.CharsRemoved = 0
	}

	for _, op := range lcsOps(a, b) {
		switch op.kind {
		case opDel:
			s.LinesRemoved += op.count()
			for _, ln := range a[op.a1:op.a2] {
				if strings.TrimSpace(ln) != "" && len(s.RemovedSamples) < maxSamples {
					s.RemovedSamples = append(s.RemovedSamples, truncate(ln, 100))
				}
			}
		case opIns:
			s.LinesAdded += op.count()
			for _, ln := range b[op.b1:op.b2] {
				if strings.TrimSpace(ln) != "" && len(s.AddedSamples) < maxSamples {
					s.AddedSamples = append(s.AddedSamples, truncate(ln, 100))
				}
			}
		}
	}
	return s
}

// FormatSummary renders the "Removed breakdown:" block like Python's
// format_summary.
func FormatSummary(s Summary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Removed breakdown:\n")
	fmt.Fprintf(&b, "  Lines: %d removed, %d added\n", s.LinesRemoved, s.LinesAdded)
	fmt.Fprintf(&b, "  Chars: %d removed\n", s.CharsRemoved)
	if len(s.RemovedSamples) > 0 {
		fmt.Fprintf(&b, "  Sample removed (-):\n")
		for _, ln := range s.RemovedSamples {
			fmt.Fprintf(&b, "    - %s\n", ln)
		}
	}
	if len(s.AddedSamples) > 0 {
		fmt.Fprintf(&b, "  Sample added (+):\n")
		for _, ln := range s.AddedSamples {
			fmt.Fprintf(&b, "    + %s\n", ln)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func runeLen(s string) int { return len([]rune(s)) }

func truncate(s string, n int) string {
	r := []rune(strings.TrimRight(s, " \t\r"))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n])
}

// --- minimal LCS diff (Myers-light via DP on smaller inputs; guarded) ---

type opKind int

const (
	opEqual opKind = iota
	opDel
	opIns
)

type op struct {
	kind           opKind
	a1, a2, b1, b2 int
}

func (o op) count() int {
	if o.kind == opDel {
		return o.a2 - o.a1
	}
	return o.b2 - o.b1
}

// lcsOps computes equal/delete/insert opcodes over line slices. Inputs are
// capped: for very large outputs we degrade to a single replace block rather
// than an O(n*m) table (diffstat is diagnostic-only).
func lcsOps(a, b []string) []op {
	const cap = 4000
	if len(a)*len(b) > cap*cap/4 {
		ops := []op{}
		if len(a) > 0 {
			ops = append(ops, op{kind: opDel, a1: 0, a2: len(a)})
		}
		if len(b) > 0 {
			ops = append(ops, op{kind: opIns, b1: 0, b2: len(b)})
		}
		return ops
	}

	n, m := len(a), len(b)
	// DP table
	table := make([][]int, n+1)
	for i := range table {
		table[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}

	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		if a[i] == b[j] {
			i++
			j++
			continue
		}
		di, dj := i, j
		for i < n && j < m && a[i] != b[j] {
			if table[i+1][j] >= table[i][j+1] {
				i++
			} else {
				j++
			}
		}
		if i > di {
			ops = append(ops, op{kind: opDel, a1: di, a2: i, b1: dj, b2: j})
		}
		if j > dj {
			ops = append(ops, op{kind: opIns, a1: di, a2: i, b1: dj, b2: j})
		}
	}
	if n > i {
		ops = append(ops, op{kind: opDel, a1: i, a2: n, b1: j, b2: m})
	}
	if m > j {
		ops = append(ops, op{kind: opIns, a1: i, a2: n, b1: j, b2: m})
	}
	return ops
}
