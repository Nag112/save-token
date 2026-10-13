//go:build windows

package exec

import (
	"os/exec"
	"sync"
	"time"
)

// shellCommand builds the platform command runner (cmd.exe).
func shellCommand(command string) *exec.Cmd {
	return exec.Command("cmd.exe", "/c", command)
}

// ChainSupported reports whether per-segment chain capture is available.
// The brace-group marker rewrite is POSIX shell syntax, so on Windows
// chains run as one raw command instead.
func ChainSupported() bool { return false }

// ValidSyntax has no cmd.exe syntax check; fail open.
func ValidSyntax(rewritten string) bool { return true }

func exitCodeOf(err error, timedOut bool) int {
	if timedOut {
		return 124
	}
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return 127 // could not execute
}

// start runs the command with a timeout watchdog. Windows has no POSIX
// process groups here, so a timeout kills the child directly; console
// Ctrl+C already reaches the child through the shared console group.
func start(cmd *exec.Cmd, timeoutSec int) (timedOut *bool, stop func()) {
	t := false
	timedOut = &t
	var timer *time.Timer
	if timeoutSec > 0 {
		timer = time.AfterFunc(time.Duration(timeoutSec)*time.Second, func() {
			if cmd.Process != nil {
				t = true
				_ = cmd.Process.Kill()
			}
		})
	}
	var once sync.Once
	stop = func() {
		once.Do(func() {
			if timer != nil {
				timer.Stop()
			}
		})
	}
	return timedOut, stop
}
