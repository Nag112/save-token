//go:build !windows

package exec

import (
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// shellCommand builds the platform command runner (POSIX shell).
func shellCommand(command string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", command)
}

// ChainSupported reports whether per-segment chain capture (POSIX brace
// groups + echo markers) is available.
func ChainSupported() bool { return true }

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
