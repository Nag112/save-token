package gate

import (
	"testing"
)

var testPatterns = []string{
	`^git\s+(?:-C\s+\S+\s+|--no-pager\s+|-c\s+\S+\s+|--git-dir(?:=|\s+)\S+\s+|--work-tree(?:=|\s+)\S+\s+)*(status|diff|log|show|push|pull|fetch|clone|branch|stash|reflog|remote|blame|cherry-pick|rebase|merge)\b`,
	`^(npm\s+test|yarn\s+test|pnpm\s+test)\b`,
	`^(pytest|py\.test)\b`,
}

func TestIsCompressible(t *testing.T) {
	policy := NewPolicy(testPatterns)
	cases := []struct {
		cmd  string
		want bool
	}{
		{"git status", true},
		{"git status | head -30", true},
		{"/usr/bin/git log --oneline", true},
		{"npm test", true},
		{"pytest -q", true},
		{"git status && npm test", true},
		{"git add . && git commit -m x && git push", true},
		// exclusions
		{"sudo ls", false},
		{"vim file.txt", false},
		{"ssh user@host", false},
		{"save-token ls", false},
		{"./save-token ls", false},
		{"/usr/local/bin/save-token git status", false},
		{"bin/save-token --dry-run pytest", false},
		{"git status | grep foo | wc -l", false}, // two pipes
		{"echo hi > out.txt", false},             // redirection
		{"sleep 5 &", false},                     // background
		{"ls; echo $(whoami)", false},            // command substitution
		{"for f in *.py; do echo $f; done", false},
		{"docker stats", false},
		{"docker stats --no-stream", true}, // guard: excluded only without --no-stream… but no matching pattern
		{"tail -f log.txt", false},
		{"python3", false},
		{"python -i script.py", false},
		{"", false},
		{"echo 'git status | head'", false}, // quoted, no pattern match at start… actually starts with echo
	}
	for _, tc := range cases {
		got := policy.IsCompressible(tc.cmd)
		// docker stats --no-stream has no matching processor pattern → not compressible
		if tc.cmd == "docker stats --no-stream" {
			tc.want = false
		}
		if got != tc.want {
			t.Errorf("IsCompressible(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

func TestQuotedPipesAreSafe(t *testing.T) {
	policy := NewPolicy(testPatterns)
	// pipe inside quotes must not count as a pipe
	if !policy.IsCompressible(`git log --format="%h|%s" | head -5`) {
		t.Error("quoted pipe should not block trailing-pipe allowlist")
	}
	if policy.IsCompressible(`git log --format="%h|%s" | head -5 | cat`) {
		t.Error("second unquoted pipe should block")
	}
}

func TestSplitChain(t *testing.T) {
	segs := SplitChain(`git add . && git commit -m "a && b" ; git push`)
	if len(segs) != 3 {
		t.Fatalf("got %d segments: %q", len(segs), segs)
	}
	if segs[1] != `git commit -m "a && b"` {
		t.Errorf("bad middle segment: %q", segs[1])
	}
}

func TestExtractPrimary(t *testing.T) {
	if got := ExtractPrimaryCommand("git add . && git commit -m x && git push"); got != "git push" {
		t.Errorf("got %q", got)
	}
	if got := ExtractPrimaryCommand("mkdir out && python train.py"); got != "python train.py" {
		t.Errorf("got %q", got)
	}
}

func TestNormalizeCmd(t *testing.T) {
	cases := map[string]string{
		"/usr/bin/git status":                   "git status",
		"./node_modules/.bin/jest":              "jest",
		".venv/bin/pip install foo":             "pip install foo",
		`"C:\\Python312\\python.exe" -m pytest`: "python -m pytest",
	}
	for in, want := range cases {
		if got := NormalizeCmd(in); got != want {
			t.Errorf("NormalizeCmd(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsDestructive(t *testing.T) {
	if !IsDestructive("git push --force origin main") {
		t.Error("force push should be destructive")
	}
	if IsDestructive("git push --force-with-lease origin main") {
		t.Error("force-with-lease should not be destructive")
	}
	if IsDestructive("git push origin main") {
		t.Error("plain push should not be destructive")
	}
}
