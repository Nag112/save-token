// Package processors ports the token-saver compression processors to Go.
package processors

// Processor is the compression backend contract (ports base.Processor).
//
// Priority bands: 10-19 reserved overrides, 20-29 core v1 processors,
// 50 default, 999 generic fallback (always last).
type Processor interface {
	// Name is the stable routing identifier.
	Name() string
	// Priority orders routing; lower runs first.
	Priority() int
	// HookPatterns are anchored (^...) regex sources matching the
	// normalized command; used to build the gate's processor policy.
	HookPatterns() []string
	// ChainTo names secondary processors applied after this one.
	ChainTo() []string
	// HandlesFailure declares the processor safely summarizes failure
	// output; otherwise a non-zero exit routes to generic instead.
	HandlesFailure() bool
	// WantsExitCode declares the processor consumes the exit status.
	WantsExitCode() bool
	// CanHandle reports whether this processor supports the command.
	CanHandle(command string) bool
	// Process transforms captured output. exitCode is forwarded only for
	// processors with WantsExitCode; others receive 0 and must ignore it.
	Process(command, output string, exitCode int) string
	// Clean is the light second-pass cleanup (ANSI/blank/whitespace) the
	// engine runs after specialized processors. Identity by default.
	Clean(text string) string
	// RedactedSecrets reports whether Process would remove secrets from
	// this output; true forbids engine fallbacks that re-emit the original.
	RedactedSecrets(command, output string) bool
}

// Base supplies the default method implementations (ports base.Processor
// defaults). Embed it and override what a processor actually changes.
type Base struct{}

// Priority is the default routing priority.
func (Base) Priority() int { return 50 }

// HookPatterns returns no anchored patterns by default.
func (Base) HookPatterns() []string { return nil }

// ChainTo declares no secondary processors.
func (Base) ChainTo() []string { return nil }

// HandlesFailure is false by default: failures route to generic.
func (Base) HandlesFailure() bool { return false }

// WantsExitCode is false by default.
func (Base) WantsExitCode() bool { return false }

// CanHandle must be overridden; nothing matches by default.
func (Base) CanHandle(command string) bool { return false }

// Process must be overridden; identity by default.
func (Base) Process(command, output string, exitCode int) string { return output }

// Clean is identity by default.
func (Base) Clean(text string) string { return text }

// RedactedSecrets is false by default (no redaction).
func (Base) RedactedSecrets(command, output string) bool { return false }

// PythonCmd is the shared python-executable prefix pattern
// (ports base.PYTHON_CMD).
const PythonCmd = `(?:\S+/)?python[23]?(?:\.\d+)?(?:\.exe)?`
