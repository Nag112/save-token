package engine

import (
	"strings"
	"testing"

	"github.com/Nag112/save-token/internal/processors"
)

func newTestEngine() *Engine {
	return NewEngine(processors.DefaultRegistry())
}

func ptr(n int) *int { return &n }

func TestRoutingGit(t *testing.T) {
	// Realistic long git status: many modified files, so the git
	// processor genuinely compresses and no mismatch fallback happens.
	var sb strings.Builder
	sb.WriteString("On branch feature/x\n")
	sb.WriteString("Your branch is up to date with 'origin/feature/x'.\n\n")
	sb.WriteString("Changes not staged for commit:\n")
	for i := 0; i < 40; i++ {
		sb.WriteString("\tmodified:   internal/some/deep/path/module_")
		sb.WriteByte(byte('a' + i%26))
		sb.WriteString(".go\n")
	}
	sb.WriteString("\nno changes added to commit (use \"git add\" and/or \"git commit -a\")\n")
	out, name, compressed := newTestEngine().Compress("git status", sb.String(), ptr(0))
	if name != "git" {
		t.Fatalf("git status routed to %q, want git (out=%q)", name, out)
	}
	if !compressed {
		t.Errorf("expected compression for 40-entry git status, got raw:\n%s", out)
	}
}

func TestRoutingLint(t *testing.T) {
	in := `/home/user/project/src/app.js
   3:5   error  'x' is not defined   no-undef
   4:5   error  'y' is not defined   no-undef
   5:5   error  'z' is not defined   no-undef
   6:5   error  'w' is not defined   no-undef
✖ 4 problems (4 errors, 0 warnings)
`
	out, name, compressed := newTestEngine().Compress("eslint src", in, ptr(1))
	if name != "lint" {
		t.Fatalf("eslint routed to %q, want lint (out=%q)", name, out)
	}
	if !compressed || !strings.Contains(out, "4 issues across") {
		t.Errorf("expected lint grouping, got:\n%s", out)
	}
}

func TestShortOutputNotCompressed(t *testing.T) {
	// Routing still reports the matched processor, but the output is
	// returned unchanged because processing gains nothing.
	out, _, compressed := newTestEngine().Compress("git status", "ok\n", ptr(0))
	if compressed {
		t.Errorf("short output must not compress, got %q", out)
	}
	if out != "ok\n" {
		t.Errorf("output changed: %q", out)
	}
}

func TestNoProcessorMatched(t *testing.T) {
	// A command matching no specialized pattern still routes to generic;
	// with repetitive filler generic compresses.
	in := strings.Repeat("some repeated filler line\n", 30)
	out, name, compressed := newTestEngine().Compress("ls -la /tmp", in, ptr(0))
	if name != "generic" {
		t.Fatalf("routed to %q, want generic", name)
	}
	if !compressed {
		t.Errorf("expected collapse of repeats, got:\n%s", out)
	}
}

func TestMismatchFallback(t *testing.T) {
	// The build processor matches the command but the output has no
	// build markers; generic wins on the original and the event records
	// the mismatch (engine.py mismatch-fallback parity).
	original := strings.Repeat("noise line without markers\n", 20)
	eng := newTestEngine()
	out, name, compressed := eng.Compress("npm run build", original, ptr(3))
	ev := eng.LastEvent()
	if ev.AttemptedProcessor != "build" {
		t.Errorf("attempted = %q, want build", ev.AttemptedProcessor)
	}
	if !ev.IsMismatch {
		t.Errorf("expected IsMismatch event, got %+v (out=%q)", ev, out)
	}
	if name != "generic" || !compressed {
		t.Errorf("mismatch fallback should yield generic compressed, got (%q,%v)", name, compressed)
	}
}

// quietProcessor compresses nothing and declares no failure handling —
// used to exercise the engine's failure-fallback-to-generic path.
type quietProcessor struct {
	processors.Base
}

func (quietProcessor) Name() string                       { return "quiet" }
func (quietProcessor) Priority() int                      { return 1 }
func (quietProcessor) CanHandle(string) bool              { return true }
func (quietProcessor) Process(string, string, int) string { return "" }

func TestFailureFallbackToGeneric(t *testing.T) {
	// "quiet" matches every command but returns empty output; on failure
	// the engine must bypass non-HandlesFailure processors and use
	// generic instead.
	reg := processors.NewRegistry(quietProcessor{}, processors.NewGeneric())
	eng := NewEngine(reg)
	in := strings.Repeat("line of failure spam\n", 30)
	out, name, _ := eng.Compress("whatever", in, ptr(2))
	ev := eng.LastEvent()
	// selectProcessor substitutes generic before processing, so the
	// event records generic as the attempted processor with the
	// failure-fallback flag set.
	if ev.AttemptedProcessor != "generic" {
		t.Errorf("attempted = %q, want generic", ev.AttemptedProcessor)
	}
	if !ev.FailureFallback {
		t.Errorf("expected FailureFallback event, got %+v", ev)
	}
	if name != "generic" {
		t.Errorf("failed command routed to %q, want generic", name)
	}
	_ = out
}
