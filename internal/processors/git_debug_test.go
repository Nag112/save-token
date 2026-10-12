package processors

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// tempGitRepo creates a scratch git repo with one commit and one modified
// file so git commands produce realistic output on any machine.
func tempGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-q", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGitStatusDump(t *testing.T) {
	gp := NewGit()
	repo := tempGitRepo(t)
	cmd := exec.Command("git", "status")
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	res := gp.Process("git status", string(out), 0)
	if err := os.WriteFile("/tmp/git_status_raw.txt", out, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/tmp/git_status_compressed.txt", []byte(res), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGitDebug(t *testing.T) {
	gp := NewGit()
	repo := tempGitRepo(t)
	for _, args := range [][]string{{"status"}, {"log", "--oneline", "-20"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(err)
		}
		full := "git " + strings.Join(args, " ")
		if !gp.CanHandle(full) {
			t.Errorf("CanHandle false for %q", full)
		}
		res := gp.Process(full, string(out), 0)
		fmt.Printf("=== %s ===\n%s\n", full, res)
		if res == string(out) {
			t.Errorf("unchanged output for %q", full)
		}
	}
}
