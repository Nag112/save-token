package exec

import (
	"strings"
	"testing"

	"github.com/Nag112/save-token/internal/gate"
)

func TestCaptureExitCodePassthrough(t *testing.T) {
	res := Capture("echo hello", 30, false)
	if res.ExitCode != 0 {
		t.Errorf("exit = %d, want 0 (stderr=%q)", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "hello") {
		t.Errorf("stdout = %q, want to contain hello", res.Stdout)
	}
}

func TestCaptureNonZeroExit(t *testing.T) {
	res := Capture("echo boom; exit 7", 30, true)
	if res.ExitCode != 7 {
		t.Errorf("exit = %d, want 7", res.ExitCode)
	}
	if !strings.Contains(res.Stdout, "boom") {
		t.Errorf("stdout = %q", res.Stdout)
	}
}

func TestCaptureStderrSeparate(t *testing.T) {
	res := Capture("echo out; echo err >&2", 30, false)
	if !strings.Contains(res.Stdout, "out") || strings.Contains(res.Stdout, "err") {
		t.Errorf("stdout = %q, want only 'out'", res.Stdout)
	}
	if !strings.Contains(res.Stderr, "err") {
		t.Errorf("stderr = %q, want to contain err", res.Stderr)
	}
}

func TestCaptureTimeout(t *testing.T) {
	res := Capture("sleep 10", 1, false)
	if !res.TimedOut {
		t.Error("expected TimedOut")
	}
	if res.ExitCode != 124 {
		t.Errorf("exit = %d, want 124 on timeout", res.ExitCode)
	}
}

func TestCaptureExecFailure(t *testing.T) {
	res := Capture("definitely-not-a-command-xyz", 30, true)
	if res.ExitCode == 0 {
		t.Error("expected non-zero exit for missing command")
	}
}

func TestMarkerRoundTrip(t *testing.T) {
	segments := gate.SplitChainWithOps("echo one && echo two && echo three")
	if len(segments) != 3 {
		t.Fatalf("split into %d segments, want 3", len(segments))
	}
	rewritten, prefix := InjectMarkers(segments)
	if !ValidSyntax(rewritten) {
		t.Fatalf("rewritten command rejected by sh -n:\n%s", rewritten)
	}
	res := Capture(rewritten, 30, true)
	if res.ExitCode != 0 {
		t.Fatalf("exit = %d, stderr=%q", res.ExitCode, res.Stderr)
	}
	chunks := SplitByMarkers(res.Stdout, prefix, len(segments))
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3 (stdout=%q)", len(chunks), res.Stdout)
	}
	want := []string{"one", "two", "three"}
	for _, c := range chunks {
		if strings.TrimSpace(c.Text) != want[c.SegIdx] {
			t.Errorf("chunk %d = %q, want %q", c.SegIdx, c.Text, want[c.SegIdx])
		}
	}
}
