package gate

import "strings"

// unquotedPositions materialises the byte indices visited by IterUnquoted.
func unquotedPositions(command string) []int {
	var out []int
	IterUnquoted(command, func(i int, _ byte) { out = append(out, i) })
	return out
}

// Segment is one piece of a &&/; chain plus the operator that followed it
// ("" for the last segment).
type Segment struct {
	Text string
	Op   string
}

// SplitChainWithOps splits a command on unquoted && and ;, keeping the
// operators (Python: chain_utils.split_chain_with_ops).
func SplitChainWithOps(command string) []Segment {
	var segs []Segment
	start := 0
	skipTo := -1
	for _, i := range unquotedPositions(command) {
		if i < skipTo {
			continue
		}
		var op string
		width := 0
		switch {
		case hasPrefixAt(command, "&&", i):
			op = "&&"
			width = 2
		case command[i] == ';':
			op = ";"
			width = 1
		default:
			continue
		}
		if seg := command[start:i]; strings.TrimSpace(seg) != "" {
			segs = append(segs, Segment{Text: strings.TrimSpace(seg), Op: op})
		}
		start = i + width
		skipTo = start
	}
	if seg := command[start:]; strings.TrimSpace(seg) != "" {
		segs = append(segs, Segment{Text: strings.TrimSpace(seg), Op: ""})
	}
	return segs
}

// SplitChain returns just the segment texts.
func SplitChain(command string) []string {
	segs := SplitChainWithOps(command)
	out := make([]string, len(segs))
	for i, s := range segs {
		out[i] = s.Text
	}
	return out
}

// silentCmdsRE: commands that produce (almost) no output; used when picking
// the primary command of a chain for processor selection.
var silentCmdsRE = mustRE(`^\s*(?:cd|pushd|popd|mkdir(?:\s+-p)?|cp|mv|rm|touch|chmod|chown|ln|export|unset|source|set|shopt|alias|hash|type|true|false|git\s+(?:add|rm|checkout|switch|reset|clean|config|init|tag|commit|mv|restore))(?:\s|$)`)

// ExtractPrimaryCommand returns the last non-silent segment of a chain (or
// the last segment if all are silent), mirroring chain_utils.
func ExtractPrimaryCommand(command string) string {
	segs := SplitChain(command)
	for i := len(segs) - 1; i >= 0; i-- {
		if !silentCmdsRE.MatchString(segs[i]) {
			return segs[i]
		}
	}
	if len(segs) > 0 {
		return segs[len(segs)-1]
	}
	return command
}
