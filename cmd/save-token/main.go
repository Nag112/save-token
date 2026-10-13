// Command save-token runs a shell command and prints a compressed,
// less-verbose version of its output. Exit code always matches the
// original command; refusals to compress run the command raw.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Nag112/save-token/internal/config"
	"github.com/Nag112/save-token/internal/diffstat"
	"github.com/Nag112/save-token/internal/engine"
	"github.com/Nag112/save-token/internal/exec"
	"github.com/Nag112/save-token/internal/gate"
	"github.com/Nag112/save-token/internal/processors"
)

var version = "0.2.1"

const usage = `save-token <command...>

Run a command and print compressed output (same exit code).

Flags (before the command):
  --dry-run        print compression stats to stderr, compressed output to stdout
  --show-removed   with --dry-run, show a removed-lines breakdown
  --no-compress    run the command with unmodified output
  --timeout N      override the command timeout in seconds (default 300)
  --version        print version and exit
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cfg := config.Get()

	var (
		dryRun      bool
		showRemoved bool
		noCompress  bool
		timeout     = cfg.WrapTimeout
	)

	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			i++
		case a == "--dry-run":
			dryRun = true
		case a == "--show-removed":
			showRemoved = true
		case a == "--no-compress":
			noCompress = true
		case a == "--version":
			fmt.Println("save-token " + version)
			return 0
		case a == "--timeout":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "save-token: --timeout needs a value")
				return 2
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n <= 0 {
				fmt.Fprintf(os.Stderr, "save-token: invalid timeout %q\n", args[i+1])
				return 2
			}
			timeout = n
			i++
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "save-token: unknown flag %s\n\n%s", a, usage)
			return 2
		default:
			goto command
		}
	}
command:
	command := shellJoin(args[i:])
	if strings.TrimSpace(command) == "" {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}

	if noCompress || !cfg.Enabled {
		return exec.Passthrough(command, timeout)
	}

	reg := processors.DefaultRegistry()
	eng := engine.NewEngine(reg)
	policy := gate.NewPolicy(reg.HookPatterns())

	decision := policy.ExplainDecision(command)
	// Deliberate deviation from the Python hook: safe commands that match
	// no processor pattern are still compressed with the generic processor
	// instead of being refused outright.
	compressible := decision.Compressible ||
		decision.Reason == gate.ReasonNoPattern ||
		(decision.IsChain && decision.AllSegmentsSafe)
	if !compressible {
		fmt.Fprintf(os.Stderr, "[save-token] running raw (%s)\n", decision.Reason)
		return exec.Passthrough(command, timeout)
	}

	segments := gate.SplitChainWithOps(command)
	isChain := len(segments) > 1 && exec.ChainSupported()

	var (
		fullOutput string
		exitCode   int
		compressed string
		procName   string
	)

	if isChain {
		rewritten, prefix := exec.InjectMarkers(segments)
		if !exec.ValidSyntax(rewritten) {
			return exec.Passthrough(command, timeout)
		}
		res := exec.Capture(rewritten, timeout, true)
		fullOutput = exec.Cap(res.Stdout, cfg.MaxOutputBytes)
		exitCode = res.ExitCode
		if strings.TrimSpace(fullOutput) == "" {
			return exitCode
		}
		chunks := exec.SplitByMarkers(fullOutput, prefix, len(segments))
		lastIdx := chunks[len(chunks)-1].SegIdx
		var parts []string
		var procs []string
		for _, ch := range chunks {
			ec := 0
			if ch.SegIdx == lastIdx {
				ec = exitCode
			}
			r := engine.Compress(eng, segments[ch.SegIdx].Text, ch.Text, &ec)
			parts = append(parts, r.Compressed)
			procs = append(procs, r.Processor)
		}
		compressed = strings.Join(parts, "\n")
		procName = "chain[" + strings.Join(dedupe(procs), ",") + "]"
	} else {
		res := exec.Capture(command, timeout, false)
		var outParts []string
		if res.Stdout != "" {
			outParts = append(outParts, res.Stdout)
		}
		if res.Stderr != "" {
			outParts = append(outParts, res.Stderr)
		}
		fullOutput = exec.Cap(strings.Join(outParts, "\n"), cfg.MaxOutputBytes)
		exitCode = res.ExitCode
		if strings.TrimSpace(fullOutput) == "" {
			return exitCode
		}
		primary := gate.ExtractPrimaryCommand(command)
		r := engine.Compress(eng, primary, fullOutput, &exitCode)
		compressed = r.Compressed
		procName = r.Processor
	}

	if dryRun {
		printDryRun(procName, fullOutput, compressed, showRemoved, cfg)
	}

	if !strings.HasSuffix(compressed, "\n") {
		compressed += "\n"
	}
	os.Stdout.WriteString(compressed)
	return exitCode
}

func printDryRun(procName, original, compressed string, showRemoved bool, cfg *config.Config) {
	oc := utf8.RuneCountInString(original)
	cc := utf8.RuneCountInString(compressed)
	cpt := cfg.CharsPerToken
	if cpt < 1 {
		cpt = 4
	}
	ot, ct := oc/cpt, cc/cpt
	saved := ot - ct
	ratio := 0.0
	if ot > 0 {
		ratio = float64(saved) / float64(ot) * 100
	}
	fmt.Fprintf(os.Stderr, "[save-token dry-run] processor=%s original=%d tokens compressed=%d tokens saved=%d tokens (%.0f%%)\n",
		procName, ot, ct, saved, ratio)
	if showRemoved {
		fmt.Fprintln(os.Stderr, diffstat.FormatSummary(diffstat.Summarize(original, compressed)))
	}
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// shellJoin reassembles argv into one shell command line. Arguments
// containing whitespace are re-quoted so quoting survives the round
// trip (save-token sh -c 'cat x' must not become `sh -c cat x`), but an
// argument that itself carries shell operators is a deliberate chain
// fragment from the user's shell and reaches sh unquoted.
func shellJoin(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = shellQuote(a)
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\"'\\$`!#&*?[]{}~()<>;|") {
		return s
	}
	if strings.ContainsAny(s, "&;|<>") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
