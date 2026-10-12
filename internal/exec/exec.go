// Package exec runs shell commands with output capture, marker-based
// chain splitting, process-group signal handling, and output capping
// (ports wrap.py's execution helpers).
package exec

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Nag112/save-token/internal/gate"
)

// Result holds one captured command execution. When the command was run
// with mergeStderr, all output is in Stdout.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	TimedOut bool
}

func markerPrefix() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "__ST_MARK_000000000000_"
	}
	return fmt.Sprintf("__ST_MARK_%s_", hex.EncodeToString(b))
}

// InjectMarkers rewrites a chain so each segment prints a unique marker
// before it runs (ports _inject_markers). Brace groups keep cd/export
// effects for later segments; a newline before the closing brace defends
// against segments ending in '#'.
func InjectMarkers(segments []gate.Segment) (rewritten, prefix string) {
	prefix = markerPrefix()
	var sb strings.Builder
	for i, seg := range segments {
		if i > 0 {
			sb.WriteString(" " + segments[i-1].Op + " ")
			sb.WriteString("{\necho '" + prefix + strconv.Itoa(i) + "';\n")
		} else {
			sb.WriteString("{\n")
		}
		sb.WriteString(seg.Text)
		sb.WriteString("\n}")
	}
	return sb.String(), prefix
}

// Chunk is one segment's captured output.
type Chunk struct {
	SegIdx int
	Text   string
}

// SplitByMarkers divides merged output into per-segment chunks (ports
// _split_output_by_markers). Markers can be glued mid-line (when a
// segment produces output without a trailing newline), so every line is
// searched for every marker token. Missing markers are fine — their
// segments simply produce no output.
func SplitByMarkers(output, prefix string, n int) []Chunk {
	tokens := map[string]int{}
	for i := 1; i < n; i++ {
		tokens[prefix+strconv.Itoa(i)] = i
	}
	var chunks []Chunk
	cur := 0
	var buf []string
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		rest := line
		for {
			bestPos, bestTok := -1, ""
			for tok := range tokens {
				if p := strings.Index(rest, tok); p >= 0 && (bestPos == -1 || p < bestPos) {
					bestPos, bestTok = p, tok
				}
			}
			if bestPos == -1 {
				buf = append(buf, rest)
				break
			}
			buf = append(buf, rest[:bestPos])
			chunks = append(chunks, Chunk{SegIdx: cur, Text: strings.Trim(strings.Join(buf, "\n"), "\n")})
			cur = tokens[bestTok]
			buf = nil
			rest = rest[bestPos+len(bestTok):]
			if rest == "" {
				break
			}
		}
	}
	chunks = append(chunks, Chunk{SegIdx: cur, Text: strings.Trim(strings.Join(buf, "\n"), "\n")})
	return chunks
}

// ValidSyntax reports whether sh -n accepts the rewritten command
// (ports _rewrite_is_syntactically_valid). Inconclusive checks (e.g. the
// check itself timed out) count as valid — fail open.
func ValidSyntax(rewritten string) bool {
	cmd := exec.Command("sh", "-n", "-c", rewritten)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return true
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err == nil
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		return true
	}
}

func exitCodeOf(err error, timedOut bool) int {
	if timedOut {
		return 124
	}
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if e, ok := err.(*exec.ExitError); ok {
		ee = e
	} else {
		return 127 // could not execute
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok {
		if ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ws.ExitStatus()
	}
	return ee.ExitCode()
}

// start wires a command in its own process group with signal forwarding
// and a timeout watchdog that kills the whole group.
func start(cmd *exec.Cmd, timeoutSec int) (timedOut *bool, stop func()) {
	t := false
	timedOut = &t
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	var once sync.Once
	go func() {
		for {
			select {
			case sig := <-sigs:
				if cmd.Process != nil {
					if s, ok := sig.(syscall.Signal); ok {
						_ = syscall.Kill(-cmd.Process.Pid, s)
					}
				}
			case <-done:
				return
			}
		}
	}()
	var timer *time.Timer
	if timeoutSec > 0 {
		timer = time.AfterFunc(time.Duration(timeoutSec)*time.Second, func() {
			if cmd.Process != nil {
				t = true
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		})
	}
	stop = func() {
		once.Do(func() {
			if timer != nil {
				timer.Stop()
			}
			signal.Stop(sigs)
			close(done)
		})
	}
	return timedOut, stop
}

// Capture runs the command via sh -c with piped output. With
// mergeStderr, stderr interleaves into stdout as in the real terminal.
func Capture(command string, timeoutSec int, mergeStderr bool) Result {
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Stdin = os.Stdin // Python's subprocess inherits stdin; Go defaults to /dev/null.
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	if mergeStderr {
		cmd.Stderr = &outBuf
	} else {
		cmd.Stderr = &errBuf
	}

	timedOut, stop := start(cmd, timeoutSec)
	err := cmd.Run()
	stop()

	res := Result{Stdout: outBuf.String(), Stderr: errBuf.String()}
	if *timedOut {
		res.TimedOut = true
		res.Stdout += fmt.Sprintf("\n[save-token] Command timed out after %ds (partial output shown)\n", timeoutSec)
	}
	res.ExitCode = exitCodeOf(err, res.TimedOut)
	if err != nil && !res.TimedOut {
		if _, ok := err.(*exec.ExitError); !ok {
			// exec itself failed (no shell): mirror Python's 127.
			res.ExitCode = 127
		}
	}
	return res
}

// Passthrough runs the command with inherited stdio for commands that
// must not be captured (refusals, raw mode). Signals are forwarded to the
// child's process group since the terminal only delivers them to us.
func Passthrough(command string, timeoutSec int) int {
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	timedOut, stop := start(cmd, timeoutSec)
	err := cmd.Run()
	stop()
	if *timedOut {
		fmt.Fprintf(os.Stderr, "[save-token] Command timed out after %ds\n", timeoutSec)
		return 124
	}
	return exitCodeOf(err, false)
}

// Cap truncates text to cap characters (Python parity: characters, not
// bytes) and notes the removal (ports _cap_output).
func Cap(text string, cap int) string {
	if cap <= 0 {
		return text
	}
	r := []rune(text)
	if len(r) <= cap {
		return text
	}
	return fmt.Sprintf("%s\n[save-token] Output truncated at %s chars (was %s)\n",
		string(r[:cap]), comma(cap), comma(len(r)))
}

// comma formats n with thousands separators.
func comma(n int) string {
	s := strconv.Itoa(n)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	var b []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			b = append(b, ',')
		}
		b = append(b, c)
	}
	return sign + string(b)
}
