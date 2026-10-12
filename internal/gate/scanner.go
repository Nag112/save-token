// Package gate decides, quote-aware, whether a shell command may be safely
// wrapped and compressed. Ported from token-saver src/shell_syntax.py,
// src/chain_utils.py and src/command_policy.py.
package gate

// IterUnquoted calls fn for every byte outside a quoted region (and outside
// backslash-escaped pairs). Byte-level scanning is UTF-8 safe: all delimiters
// scanned here are ASCII, and UTF-8 continuation bytes are >= 0x80.
//
// Semantics (matching Python iter_unquoted):
//   - a backslash outside quotes consumes itself and the next char as a pair;
//     neither is yielded (this also correctly skips a line-continuation \n);
//   - a single-quoted region ends at the next ' with no escape handling;
//   - a double-quoted region honours backslash escapes;
//   - an unterminated quote swallows the rest of the string (safe direction).
func IterUnquoted(command string, fn func(i int, ch byte)) {
	n := len(command)
	i := 0
	for i < n {
		ch := command[i]
		if ch == '\\' && i+1 < n {
			i += 2
			continue
		}
		if ch == '\'' || ch == '"' {
			quote := ch
			escapes := quote == '"'
			i++
			for i < n && command[i] != quote {
				if escapes && command[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				i++
			}
			i++ // past the closing quote (or past the end)
			continue
		}
		fn(i, ch)
		i++
	}
}

// HasUnquoted reports whether any construct starts at an unquoted position.
func HasUnquoted(command string, constructs []string) bool {
	found := false
	IterUnquoted(command, func(i int, _ byte) {
		if found {
			return
		}
		for _, c := range constructs {
			if hasPrefixAt(command, c, i) {
				found = true
				return
			}
		}
	})
	return found
}

func hasPrefixAt(s, prefix string, i int) bool {
	return i+len(prefix) <= len(s) && s[i:i+len(prefix)] == prefix
}

func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') || b == '_'
}

// isFDDup reports whether command[i:] starts with a file-descriptor
// duplication: >&digits or >&-, followed by a non-identifier boundary
// (Python: >&(\d+|-)(?![\w./-]) anchored at the '>').
func isFDDup(command string, i int) bool {
	if !hasPrefixAt(command, ">&", i) {
		return false
	}
	j := i + 2
	if j >= len(command) {
		return false
	}
	if command[j] == '-' {
		j++
	} else {
		start := j
		for j < len(command) && command[j] >= '0' && command[j] <= '9' {
			j++
		}
		if j == start {
			return false
		}
	}
	if j < len(command) {
		b := command[j]
		if isWordByte(b) || b == '.' || b == '/' || b == '-' {
			return false
		}
	}
	return true
}

// HasOutputRedirection detects an unquoted '>' that writes to a file:
// '->' and '=>' are not redirections, and FD dups (2>&1, >&2, >&-) are not.
func HasOutputRedirection(command string) bool {
	found := false
	IterUnquoted(command, func(i int, ch byte) {
		if found || ch != '>' {
			return
		}
		prev := byte(0)
		if i > 0 {
			prev = command[i-1]
		}
		if prev == '-' || prev == '=' {
			return
		}
		if isFDDup(command, i) {
			return
		}
		found = true
	})
	return found
}

// HasUnquotedBackgroundOperator detects a bare '&' (backgrounding), excluding
// '&&' and redirection forms ('>&', '&>').
func HasUnquotedBackgroundOperator(command string) bool {
	n := len(command)
	found := false
	IterUnquoted(command, func(i int, ch byte) {
		if found || ch != '&' {
			return
		}
		var prev, next byte
		if i > 0 {
			prev = command[i-1]
		}
		if i+1 < n {
			next = command[i+1]
		}
		if prev == '&' || next == '&' {
			return // part of &&
		}
		if prev == '>' || next == '>' {
			return // part of N>&M or &>file
		}
		found = true
	})
	return found
}

// HasUnquotedPipe reports any unquoted '|' (Go replacement for the Python
// lookaround-based pipe pattern; fail-safe in the refuse direction).
func HasUnquotedPipe(command string) bool {
	found := false
	IterUnquoted(command, func(_ int, ch byte) {
		if ch == '|' {
			found = true
		}
	})
	return found
}

// HasUnquotedOrOr reports an unquoted '||'.
func HasUnquotedOrOr(command string) bool {
	return HasUnquoted(command, []string{"||"})
}

// HasChainSplit reports an unquoted '&&' or ';'.
func HasChainSplit(command string) bool {
	return HasUnquoted(command, []string{"&&", ";"})
}

// HasShellConstructOpen reports a segment beginning with a POSIX compound
// statement reserved word (for/while/until/if/case/select).
func HasShellConstructOpen(segment string) bool {
	return shellConstructOpenRE.MatchString(segment)
}

// HasUnquotedNewline reports an unquoted newline that is not a line
// continuation (not preceded by an odd run of backslashes).
func HasUnquotedNewline(command string) bool {
	found := false
	IterUnquoted(command, func(i int, ch byte) {
		if found || ch != '\n' {
			return
		}
		before := command[:i]
		tb := 0
		for k := len(before) - 1; k >= 0 && before[k] == '\\'; k-- {
			tb++
		}
		if tb%2 == 1 {
			return // line continuation
		}
		found = true
	})
	return found
}

// FirstUnquotedTokenEnd returns the index just past the first token, where
// token ends at the first unquoted whitespace. Returns len(command) if the
// whole string is one token (or fully quoted).
func FirstUnquotedTokenEnd(command string) int {
	end := len(command)
	IterUnquoted(command, func(i int, ch byte) {
		if end == len(command) && (ch == ' ' || ch == '\t' || ch == '\n') {
			end = i
		}
	})
	return end
}
