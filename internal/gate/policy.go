package gate

import (
	"errors"
	"regexp"
	"strings"
)

var shellConstructOpenRE = mustRE(`(?m)^\s*(for|while|until|if|case|select)\b`)

// mustRE compiles a pattern or panics — all patterns here are static.
func mustRE(p string) *regexp.Regexp { return regexp.MustCompile(p) }

// safeTrailingPipeRE strips trailing single-stage pipes that cannot change
// wrap safety (| head -30, | grep foo, ...). Mirrors command_policy.py.
var safeTrailingPipeRE = mustRE(`\s*\|\s*(` +
	`head(\s+-[n]?\s*\d+|\s+-\d+)*` +
	`|tail(\s+[-+]?\d+|\s+-[nf]\s*\d+)*` +
	`|wc(\s+-[lwc])*` +
	`|grep(\s+-[viEcwnHr])*\s+\S+` +
	`|sort(\s+-[rnktu](\s+\d+)?)*` +
	`|uniq(\s+-[cd])*` +
	`|cut(\s+-[fd]\s*\S+)+` +
	`)\s*$`)

// excl is an exclusion pattern with an optional guard. Go RE2 has no
// lookahead, so Python's `(?![^|]*X)` negative lookaheads become guards:
// given the command and the leftmost match, the guard reports whether the
// match still counts as an exclusion.
type excl struct {
	src   string
	re    *regexp.Regexp
	guard func(cmd string, matchEnd int) bool
}

func upToPipeExcludes(cmd string, matchEnd int, needle *regexp.Regexp) bool {
	rest := cmd[matchEnd:]
	if k := strings.IndexByte(rest, '|'); k >= 0 {
		rest = rest[:k]
	}
	return needle.MatchString(rest)
}

var (
	noStreamRE = mustRE(`--no-stream`)
	dashDRE    = mustRE(`\s-d\b`)
	runWordRE  = mustRE(`\brun\b`)
)

// streamingExcluded: commands that never terminate on their own.
func streamingExcluded() []excl {
	return []excl{
		{`^\s*watch\b`, mustRE(`(?m)^\s*watch\b`), nil},
		{`\s--follow(=|\s|$)`, mustRE(`\s--follow(=|\s|$)`), nil},
		{`^\s*tail\b[^|]*\s-[a-zA-Z]*[fF]\b`, mustRE(`(?m)^\s*tail\b[^|]*\s-[a-zA-Z]*[fF]\b`), nil},
		{`^\s*journalctl\b[^|]*\s-[a-zA-Z]*f\b`, mustRE(`(?m)^\s*journalctl\b[^|]*\s-[a-zA-Z]*f\b`), nil},
		{`\b(kubectl|oc)\b[^|]*\blogs\b[^|]*\s-[a-zA-Z]*f\b`, mustRE(`\b(kubectl|oc)\b[^|]*\blogs\b[^|]*\s-[a-zA-Z]*f\b`), nil},
		{`\bdocker\b[^|]*\blogs\b[^|]*\s-[a-zA-Z]*f\b`, mustRE(`\bdocker\b[^|]*\blogs\b[^|]*\s-[a-zA-Z]*f\b`), nil},
		{"^\\s*docker\\s+stats\\b", mustRE(`(?m)^\s*docker\s+stats\b`), func(c string, e int) bool {
			return !upToPipeExcludes(c, e, noStreamRE)
		}},
		{"^\\s*docker(\\s+compose|-compose)\\s+up\\b", mustRE(`(?m)^\s*docker(\s+compose|-compose)\s+up\b`), func(c string, e int) bool {
			return !upToPipeExcludes(c, e, dashDRE)
		}},
		{`\s--watch(=|\s|$)`, mustRE(`\s--watch(=|\s|$)`), nil},
		{`\s--watchAll\b`, mustRE(`\s--watchAll\b`), nil},
		{`^\s*(npx\s+)?vitest\b`, mustRE(`(?m)^\s*(npx\s+)?vitest\b`), func(c string, e int) bool {
			return !upToPipeExcludes(c, e, runWordRE)
		}},
	}
}

const replFlagSrc = `^\s*(python\d?(?:\.\d+)*|ipython|node|ruby|perl|ghci|deno|php|lua|R|bash|sh|zsh)\s+(?:-\S*i\S*|--interactive)(\s|$)`
const replBareSrc = `^\s*(python\d?(?:\.\d+)*|ipython|node|bash|sh|zsh|ruby|irb|pry|gdb|lldb|mongo|mongosh|redis-cli|psql|mysql|sqlite3|php|perl|lua|R)\s*$`
const recursionSrc = `(?:^|\s)(?:\S*/)?save-token(\s|$)`

// wholeCommandExcluded reports whether checkCmd/normCmd hits a whole-command
// exclusion (excluding the chain/pipe checks done earlier by the caller).
func wholeCommandExcluded(checkCmd, normCmd string) (string, bool) {
	pats := []excl{
		{`^\s*(vi|vim|nano|emacs|code)\b`, mustRE(`(?m)^\s*(vi|vim|nano|emacs|code)\b`), nil},
		{`^\s*ssh\s+(?:-\S+\s+)*\S+\s*$`, mustRE(`(?m)^\s*ssh\s+(?:-\S+\s+)*\S+\s*$`), nil},
		{`^\s*rsync\b.*\S+:\S+`, mustRE(`(?m)^\s*rsync\b.*\S+:\S+`), nil},
		{recursionSrc, mustRE(recursionSrc), nil},
		{`<\(`, mustRE(`<\(`), nil},
		{`^\s*sudo\b`, mustRE(`(?m)^\s*sudo\b`), nil},
		{`^\s*env\s+\S+=`, mustRE(`(?m)^\s*env\s+\S+=`), nil},
		{replFlagSrc, mustRE(`(?m)` + replFlagSrc), nil},
	}
	pats = append(pats, streamingExcluded()...)
	return matchExclusions(pats, checkCmd, normCmd)
}

// segmentExcluded: per-segment safety checks for chains.
func segmentExcluded(seg, norm string) (string, bool) {
	if HasUnquotedPipe(seg) || HasUnquotedPipe(norm) {
		return `unquoted pipe`, true
	}
	pats := []excl{
		{`<\(`, mustRE(`<\(`), nil},
		{`^\s*sudo\b`, mustRE(`(?m)^\s*sudo\b`), nil},
		{`^\s*(vi|vim|nano|emacs|code)\b`, mustRE(`(?m)^\s*(vi|vim|nano|emacs|code)\b`), nil},
		{`^\s*ssh\s+(?:-\S+\s+)*\S+\s*$`, mustRE(`(?m)^\s*ssh\s+(?:-\S+\s+)*\S+\s*$`), nil},
		{`^\s*rsync\b.*\S+:\S+`, mustRE(`(?m)^\s*rsync\b.*\S+:\S+`), nil},
		{`^\s*env\s+\S+=`, mustRE(`(?m)^\s*env\s+\S+=`), nil},
		{recursionSrc, mustRE(recursionSrc), nil},
		{replBareSrc, mustRE(`(?m)` + replBareSrc), nil},
		{replFlagSrc, mustRE(`(?m)` + replFlagSrc), nil},
	}
	pats = append(pats, streamingExcluded()...)
	return matchExclusions(pats, seg, norm)
}

func matchExclusions(pats []excl, a, b string) (string, bool) {
	for _, p := range pats {
		for _, s := range []string{a, b} {
			m := p.re.FindStringIndex(s)
			if m == nil {
				continue
			}
			if p.guard == nil || p.guard(s, m[1]) {
				return p.src, true
			}
		}
	}
	return "", false
}

// dangerousConstructs break naive chain splitting / per-segment execution.
var dangerousConstructs = []string{"$(", "`", "<<"}

// --- _normalize_cmd -------------------------------------------------------

var pythonExeRE = mustRE(`python\d?(?:\.\d+)*(?:\.exe)?`)
var pathPrefixRE = mustRE(`^(\S+/)\S`)

// NormalizeCmd strips leading path prefixes and normalises Python launchers
// so exclusion patterns match '/usr/bin/vim', '.venv/bin/pip install', etc.
func NormalizeCmd(cmd string) string {
	stripped := strings.TrimLeft(cmd, " \t\n\v\f\r")
	end := FirstUnquotedTokenEnd(stripped)
	token := stripped[:end]
	if strings.ContainsAny(token, "$`") {
		return cmd
	}
	words, err := posixSplit(token)
	if err != nil || len(words) != 1 {
		return cmd
	}
	executable := strings.ReplaceAll(words[0], "\\", "/")
	if k := strings.LastIndexByte(executable, '/'); k >= 0 {
		executable = executable[k+1:]
	}
	if pythonExeRE.MatchString(executable) {
		return strings.TrimSuffix(executable, ".exe") + stripped[end:]
	}
	if m := pathPrefixRE.FindStringSubmatchIndex(cmd); m != nil {
		return cmd[:m[2]] + cmd[m[3]:]
	}
	return cmd
}

// posixSplit is a minimal POSIX shell word splitter (single quotes literal,
// double quotes honour \\ escapes, bare \ escapes next char). Errors on
// unterminated quotes, matching shlex.split semantics used by the policy.
func posixSplit(s string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord := false
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
			i++
		case c == '\\':
			if i+1 >= len(s) {
				return nil, errors.New("posixSplit: trailing backslash")
			}
			cur.WriteByte(s[i+1])
			inWord = true
			i += 2
		case c == '\'':
			i++
			for i < len(s) && s[i] != '\'' {
				cur.WriteByte(s[i])
				i++
			}
			if i >= len(s) {
				return nil, errors.New("posixSplit: unterminated single quote")
			}
			i++
			inWord = true
		case c == '"':
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				cur.WriteByte(s[i])
				i++
			}
			if i >= len(s) {
				return nil, errors.New("posixSplit: unterminated double quote")
			}
			i++
			inWord = true
		default:
			cur.WriteByte(c)
			inWord = true
			i++
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}

// --- segment safety --------------------------------------------------------

func endsWithLineContinuation(segment string) bool {
	stripped := strings.TrimRight(segment, " \t\n\v\f\r")
	trailing := len(stripped) - len(strings.TrimRight(stripped, "\\"))
	return trailing%2 == 1
}

func isCommentOnly(segment string) bool {
	return strings.HasPrefix(strings.TrimLeft(segment, " \t\n\v\f\r"), "#")
}

func isSegmentSafe(segment string) bool {
	if HasOutputRedirection(segment) {
		return false
	}
	if HasUnquotedBackgroundOperator(segment) {
		return false
	}
	if HasShellConstructOpen(segment) {
		return false
	}
	if endsWithLineContinuation(segment) || isCommentOnly(segment) {
		return false
	}
	if _, ex := segmentExcluded(segment, NormalizeCmd(segment)); ex {
		return false
	}
	return true
}

// --- Policy ----------------------------------------------------------------

// Policy evaluates shell safety against one explicit processor-pattern
// inventory (Python: CommandPolicy).
type Policy struct {
	PatternSources []string
	compiled       []*regexp.Regexp
}

// NewPolicy compiles the supplied hook patterns, dropping invalid ones.
func NewPolicy(patterns []string) *Policy {
	p := &Policy{}
	for _, src := range patterns {
		re, err := regexp.Compile(src)
		if err != nil {
			continue // skip invalid, like compile_patterns()
		}
		p.PatternSources = append(p.PatternSources, src)
		p.compiled = append(p.compiled, re)
	}
	return p
}

// Reason values returned by ExplainDecision. The two "no processor pattern"
// reasons are distinct from exclusions: the command is SAFE, it just has no
// specialized processor. save-token's CLI compresses those with the generic
// processor (unlike the Python hook, which only sees pattern-matched commands).
const (
	ReasonNoPattern    = "no processor pattern matched (safe, generic)"
	ReasonChainNoMatch = "chain all-safe but no compressible segment (safe, generic)"
)

// Decision is the structured explain_decision() record.
type Decision struct {
	Command         string
	Compressible    bool
	Reason          string
	ExcludedBy      string
	MatchedPatterns []string
	IsChain         bool
	// AllSegmentsSafe is true for chains where every segment passed the safety
	// checks even though no segment matched a processor pattern.
	AllSegmentsSafe bool
}

func (p *Policy) matchedCompressible(checkCmd, normCmd string) []string {
	var matched []string
	for i, re := range p.compiled {
		if re.MatchString(checkCmd) || re.MatchString(normCmd) {
			matched = append(matched, p.PatternSources[i])
		}
	}
	return matched
}

// ExplainDecision is the sole evaluator used for interception and explanation.
func (p *Policy) ExplainDecision(command string) Decision {
	res := Decision{Command: command}
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		res.Reason = "empty command"
		return res
	}

	if HasUnquotedOrOr(cmd) {
		res.Reason = "contains '||' (error-recovery chains are not wrapped)"
		res.ExcludedBy = "||"
		return res
	}
	if HasUnquotedNewline(cmd) {
		res.Reason = "contains an unquoted newline (multiple statements)"
		res.ExcludedBy = "unquoted newline"
		return res
	}
	if HasUnquoted(cmd, dangerousConstructs) {
		res.Reason = "contains unquoted $(), backtick, or heredoc"
		res.ExcludedBy = "dangerous shell construct"
		return res
	}

	if HasChainSplit(cmd) {
		res.IsChain = true
		allSafe, hasCompressible := p.chainSafety(cmd)
		res.AllSegmentsSafe = allSafe
		compressible := allSafe && hasCompressible
		res.Compressible = compressible
		seen := map[string]bool{}
		for _, seg := range SplitChain(cmd) {
			checkSeg := safeTrailingPipeRE.ReplaceAllString(seg, "")
			normSeg := NormalizeCmd(checkSeg)
			for _, m := range p.matchedCompressible(checkSeg, normSeg) {
				if !seen[m] {
					seen[m] = true
					res.MatchedPatterns = append(res.MatchedPatterns, m)
				}
			}
		}
		switch {
		case compressible:
			res.Reason = "chain with at least one compressible, all-safe segment"
		case allSafe:
			res.Reason = ReasonChainNoMatch
		default:
			res.Reason = "chain has an unsafe segment"
		}
		return res
	}

	if HasOutputRedirection(cmd) {
		res.Reason = "contains output redirection (>, >>, 2>, &>)"
		res.ExcludedBy = "output redirection"
		return res
	}
	if HasUnquotedBackgroundOperator(cmd) {
		res.Reason = "contains an unquoted '&' (backgrounded command)"
		res.ExcludedBy = "background operator"
		return res
	}

	checkCmd := safeTrailingPipeRE.ReplaceAllString(cmd, "")
	normCmd := NormalizeCmd(checkCmd)
	if HasUnquotedPipe(checkCmd) || HasUnquotedPipe(normCmd) {
		res.Reason = "matched an exclusion pattern"
		res.ExcludedBy = "unquoted pipe"
		return res
	}
	if excl, ok := wholeCommandExcluded(checkCmd, normCmd); ok {
		res.Reason = "matched an exclusion pattern"
		res.ExcludedBy = excl
		return res
	}

	res.MatchedPatterns = p.matchedCompressible(checkCmd, normCmd)
	res.Compressible = len(res.MatchedPatterns) > 0
	if res.Compressible {
		res.Reason = "matched a compressible processor pattern"
	} else {
		res.Reason = ReasonNoPattern
	}
	return res
}

// chainSafety reports (allSegmentsSafe, anySegmentMatchesAPattern).
// Safe trailing pipes are stripped from the last segment. Matches the
// evaluation of Python chain_compression's per-segment safety loop, split so
// callers can distinguish "unsafe" from "safe but unmatched".
func (p *Policy) chainSafety(command string) (allSafe bool, hasMatch bool) {
	segments := SplitChain(command)
	if len(segments) == 0 {
		return false, false
	}
	allSafe = true
	for i, seg := range segments {
		checkSeg := seg
		if i == len(segments)-1 {
			checkSeg = safeTrailingPipeRE.ReplaceAllString(seg, "")
		}
		if !isSegmentSafe(checkSeg) {
			allSafe = false
			continue
		}
		if len(p.matchedCompressible(checkSeg, NormalizeCmd(checkSeg))) > 0 {
			hasMatch = true
		}
	}
	return allSafe, hasMatch
}

// IsCompressible returns eligibility for the command.
func (p *Policy) IsCompressible(command string) bool {
	return p.ExplainDecision(command).Compressible
}

// --- destructive (advisory; not wired into the v1 CLI flow) -----------------

var destructivePatterns = []*regexp.Regexp{
	mustRE(`\bgit\s+push\b[^\n]*\s(?:--force\b|-f\b)`), // guard applied below
	mustRE(`\bgit\s+reset\b[^\n]*\s--hard\b`),
	mustRE(`\bgit\s+clean\b[^\n]*\s-[a-zA-Z]*f`),
	mustRE(`\b(?:kubectl|oc)\b[^\n]*\bdelete\b`),
	mustRE(`\b(?:terraform|tofu)\s+(?:apply|destroy)\b`),
	mustRE(`\bdocker\b[^\n]*\b(?:system\s+prune|volume\s+rm|rmi)\b`),
	mustRE(`\b(?:docker|podman)\s+rm\b`),
	mustRE(`\brm\s+[^\n]*-[a-zA-Z]*[rR][a-zA-Z]*f|\brm\s+[^\n]*-[a-zA-Z]*f[a-zA-Z]*[rR]`),
}

// IsDestructive reports whether auto-approving the command would bypass
// user guard rails (force-push, hard reset, rm -rf, terraform apply, ...).
func IsDestructive(command string) bool {
	norm := NormalizeCmd(command)
	for _, s := range []string{command, norm} {
		for i, re := range destructivePatterns {
			m := re.FindStringIndex(s)
			if m == nil {
				continue
			}
			if i == 0 {
				// --force-with-lease is the safe variant (Python's
				// --force\b(?!-with-lease)); -f is unconditional.
				if s[m[1]-2:] == "-f" {
					return true
				}
				if !strings.HasPrefix(s[m[1]:], "-with-lease") {
					return true
				}
				continue
			}
			return true
		}
	}
	return false
}
