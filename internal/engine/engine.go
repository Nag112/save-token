// Package engine ports token-saver's compression engine routing.
package engine

import (
	"fmt"
	"unicode/utf8"

	"github.com/Nag112/save-token/internal/config"
	"github.com/Nag112/save-token/internal/critical"
	"github.com/Nag112/save-token/internal/processors"
	"github.com/Nag112/save-token/internal/util"
)

// Event describes the most recent compression attempt (ports the hook's
// last_event dict). Set is false before the first compress call and after
// the early no-op returns, matching Python's empty-dict state.
type Event struct {
	Set                bool
	AttemptedProcessor string
	ResultProcessor    string
	WasCompressed      bool
	IsMismatch         bool
	OriginalLen        int
	CompressedLen      int
	FailureFallback    bool
	Redacted           bool
}

// Engine routes command output through the processor registry.
type Engine struct {
	registry  *processors.Registry
	lastEvent Event
}

// NewEngine builds an engine over the given registry.
func NewEngine(reg *processors.Registry) *Engine {
	return &Engine{registry: reg}
}

// LastEvent returns metadata about the most recent Compress call.
func (e *Engine) LastEvent() Event { return e.lastEvent }

func (e *Engine) event(attempted, result string, wasCompressed, isMismatch bool, ol, cl int, failureFallback bool) {
	e.lastEvent = Event{
		Set:                true,
		AttemptedProcessor: attempted,
		ResultProcessor:    result,
		WasCompressed:      wasCompressed,
		IsMismatch:         isMismatch,
		OriginalLen:        ol,
		CompressedLen:      cl,
		FailureFallback:    failureFallback,
	}
}

// selectProcessor picks the routing processor (ports _select_processor).
// A failed command routes to generic unless the matched processor declares
// it handles failure output.
func (e *Engine) selectProcessor(command string, exitCode *int) processors.Processor {
	failed := exitCode != nil && *exitCode != 0
	for _, p := range e.registry.List() {
		if !p.CanHandle(command) {
			continue
		}
		if failed && !p.HandlesFailure() && p != e.registry.Generic() {
			return e.registry.Generic()
		}
		return p
	}
	return nil
}

// callProcess invokes p.Process, forwarding exitCode only for processors
// that want it (ports _call_processor).
func callProcess(p processors.Processor, command, output string, exitCode *int) string {
	if p.WantsExitCode() && exitCode != nil {
		return p.Process(command, output, *exitCode)
	}
	return p.Process(command, output, 0)
}

// recoverCritical re-appends error lines a non-failure processor dropped
// (ports _recover_critical_lines). The note prefix uses this project's
// binary name instead of token-saver's.
func recoverCritical(original, compressed string, p processors.Processor) string {
	cfg := config.Get()
	if p.HandlesFailure() || cfg.RecoverCriticalLines <= 0 {
		return compressed
	}
	missing := critical.MissingCritical(original, compressed)
	if len(missing) == 0 {
		return compressed
	}
	kept := missing
	if len(kept) > cfg.RecoverCriticalLines {
		kept = kept[:cfg.RecoverCriticalLines]
	}
	note := fmt.Sprintf("[save-token] %d error line(s) recovered", len(kept))
	if extra := len(missing) - len(kept); extra > 0 {
		note += fmt.Sprintf(" (%d more omitted)", extra)
	}
	return util.JoinLines(append([]string{compressed, note}, kept...))
}

// Compress routes output through the best processor and returns
// (output, processor_name, was_compressed). Ported from
// CompressionEngine.compress; processing panics propagate to the caller's
// Compress wrapper, which falls back to passthrough.
func (e *Engine) Compress(command, output string, exitCode *int) (string, string, bool) {
	e.lastEvent = Event{}
	cfg := config.Get()
	ol := utf8.RuneCountInString(output)

	if !cfg.Enabled {
		return output, "none", false
	}
	if ol < cfg.MinInputLength {
		return output, "none", false
	}

	processor := e.selectProcessor(command, exitCode)
	if processor == nil {
		return output, "none", false
	}
	name := processor.Name()
	failed := exitCode != nil && *exitCode != 0
	failureFallback := failed && processor == e.registry.Generic()

	compressed := callProcess(processor, command, output, exitCode)
	if compressed == output {
		e.event(name, name, false, false, ol, ol, failureFallback)
		return output, name, false
	}

	redacted := processor.RedactedSecrets(command, output)

	// Secondary processors declared via ChainTo, depth-capped and
	// de-duplicated. (Python wraps each chained call in try/except; in Go
	// a processor panic propagates and the Compress wrapper passes the
	// original output through, which is at least as safe.)
	generic := e.registry.Generic()
	visited := map[string]bool{name: true}
	depth := 0
	for _, chainName := range processor.ChainTo() {
		if depth >= cfg.MaxChainDepth {
			break
		}
		if visited[chainName] {
			continue
		}
		secondary := e.registry.Get(chainName)
		if secondary == nil {
			continue
		}
		visited[chainName] = true
		secondaryRedacts := redacted || secondary.RedactedSecrets(command, compressed)
		chained := callProcess(secondary, command, compressed, exitCode)
		redacted = secondaryRedacts
		if chained != compressed {
			compressed = chained
		}
		depth++
	}

	// Light second-pass cleanup for specialized processors. Python's
	// _clean_output swallows Clean failures when redaction happened;
	// Go's Clean implementations cannot fail.
	if processor != generic {
		compressed = generic.Clean(compressed)
	}
	if !redacted {
		compressed = recoverCritical(output, compressed, processor)
	}

	cl := utf8.RuneCountInString(compressed)
	gain := 0.0
	if ol > 0 {
		gain = float64(ol-cl) / float64(ol)
	}
	minRatio := cfg.MinCompressionRatio
	if redacted || (cl < ol && gain >= minRatio) {
		e.event(name, name, true, false, ol, cl, failureFallback)
		e.lastEvent.Redacted = redacted
		return compressed, name, true
	}

	// Specialized processor missed its size target: fall back to generic
	// over the ORIGINAL output (never re-emit after redaction).
	mismatch := processor != generic
	if processor != generic {
		genericCompressed := callProcess(generic, command, output, exitCode)
		genericRedacted := generic.RedactedSecrets(command, output)
		genericCompressed = generic.Clean(genericCompressed)
		if !genericRedacted {
			genericCompressed = recoverCritical(output, genericCompressed, generic)
		}
		gl := utf8.RuneCountInString(genericCompressed)
		gg := 0.0
		if ol > 0 {
			gg = float64(ol-gl) / float64(ol)
		}
		if genericRedacted || (gl < ol && gg >= minRatio) {
			e.event(name, "generic", true, mismatch, ol, gl, failureFallback)
			e.lastEvent.Redacted = genericRedacted
			return genericCompressed, "generic", true
		}
	}

	e.event(name, name, false, mismatch, ol, cl, failureFallback)
	e.lastEvent.Redacted = redacted
	return output, name, false
}

// Result is the outcome of one compression, independent of engine
// internals (ports core.CompressResult).
type Result struct {
	Compressed         string
	Processor          string
	WasCompressed      bool
	IsMismatch         bool
	AttemptedProcessor string
	OriginalLen        int
	CompressedLen      int
}

// Compress runs the engine and converts processor panics into a
// passthrough result so compression failures never break the CLI
// (ports core.compress).
func Compress(eng *Engine, command, output string, exitCode *int) (res Result) {
	defer func() {
		if r := recover(); r != nil {
			res = Result{
				Compressed:         output,
				Processor:          "passthrough",
				AttemptedProcessor: "passthrough",
				OriginalLen:        utf8.RuneCountInString(output),
				CompressedLen:      utf8.RuneCountInString(output),
			}
		}
	}()
	compressed, processorName, wasCompressed := eng.Compress(command, output, exitCode)
	ev := eng.lastEvent
	attempted := processorName
	if ev.Set {
		attempted = ev.AttemptedProcessor
	}
	return Result{
		Compressed:         compressed,
		Processor:          processorName,
		WasCompressed:      wasCompressed,
		IsMismatch:         ev.IsMismatch,
		AttemptedProcessor: attempted,
		OriginalLen:        utf8.RuneCountInString(output),
		CompressedLen:      utf8.RuneCountInString(compressed),
	}
}
