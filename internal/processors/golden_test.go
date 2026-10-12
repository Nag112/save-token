package processors

import (
	"fmt"
	"strings"
	"testing"
)

// Golden-style checks: canned real-world tool output goes through
// Process() and must produce the documented compressed shape. These are
// the parity fixtures against the Python token-saver processors.

func TestGoldenLintESLint(t *testing.T) {
	in := `/home/user/project/src/app.js
   3:5   error    Unexpected var, use let or const instead  no-var
   4:5   error    Unexpected var, use let or const instead  no-var
   5:5   error    Unexpected var, use let or const instead  no-var
   6:5   error    Unexpected var, use let or const instead  no-var
  10:1   error    Unexpected console statement              no-console
✖ 5 problems (5 errors, 0 warnings)
`
	p := NewLint()
	if !p.CanHandle("eslint src") {
		t.Fatal("lint must handle eslint")
	}
	out := p.Process("eslint src", in, 1)
	if !strings.HasPrefix(out, "5 issues across 2 rules:") {
		t.Errorf("header wrong:\n%s", out)
	}
	if !strings.Contains(out, "no-var: 4 occurrences") {
		t.Errorf("expected no-var grouping:\n%s", out)
	}
	if !strings.Contains(out, "... (2 more)") {
		t.Errorf("expected elision line:\n%s", out)
	}
	if !strings.Contains(out, "✖ 5 problems") {
		t.Errorf("summary line must survive:\n%s", out)
	}
}

func TestGoldenLintRuff(t *testing.T) {
	in := `src/main.py:10:5: E501 Line too long (95 > 88)
src/main.py:11:5: E501 Line too long (102 > 88)
src/utils.py:3:1: F401 ` + "`os`" + ` imported but unused
Found 3 errors.
`
	p := NewLint()
	if !p.CanHandle("ruff check .") {
		t.Fatal("lint must handle ruff check")
	}
	out := p.Process("ruff check .", in, 1)
	if !strings.Contains(out, "3 issues across 2 rules:") {
		t.Errorf("header wrong:\n%s", out)
	}
	if !strings.Contains(out, "Found 3 errors.") {
		t.Errorf("summary must survive:\n%s", out)
	}
}

func TestGoldenLintNoViolations(t *testing.T) {
	p := NewLint()
	out := p.Process("eslint src", "everything is fine\n", 0)
	if out != "everything is fine\n" {
		t.Errorf("no groups must return output unchanged, got %q", out)
	}
}

func TestGoldenTestPytestFail(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("============ test session starts ============\n")
	for i := 0; i < 30; i++ {
		sb.WriteString("tests/test_mod.py::test_ok PASSED                    [ 50%]\n")
	}
	sb.WriteString("FAILED tests/test_mod.py::test_bad - AssertionError\n")
	sb.WriteString("======== 1 failed, 30 passed in 4.20s ========\n")
	p := NewTest()
	if !p.CanHandle("pytest") {
		t.Fatal("test must handle pytest")
	}
	out := p.Process("pytest", sb.String(), 1)
	if !strings.Contains(out, "test_bad") {
		t.Errorf("failure must survive:\n%s", out)
	}
	if strings.Contains(out, "test_ok PASSED") {
		t.Errorf("passed lines should be folded:\n%s", out)
	}
}

func TestGoldenPackageInstallPip(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("Collecting requests\n")
	for i := 0; i < 40; i++ {
		sb.WriteString("     ---------------------------------------- 62.3/62.3 kB 1.2 MB/s\n")
	}
	sb.WriteString("Installing collected packages: requests\n")
	sb.WriteString("Successfully installed requests-2.31.0\n")
	p := NewPackageInstall()
	if !p.CanHandle("pip install requests") {
		t.Fatal("pkg must handle pip install")
	}
	out := p.Process("pip install requests", sb.String(), 0)
	if !strings.Contains(out, "requests-2.31.0") || strings.Contains(out, "Downloading") {
		t.Errorf("success line must survive:\n%s", out)
	}
	if strings.Contains(out, "MB/s") {
		t.Errorf("progress bars must be stripped:\n%s", out)
	}
}

func TestGoldenBuildFail(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 30; i++ {
		sb.WriteString("npm info lifecycle app@" + fmt.Sprintf("%d", i) + ".0~prebuild\n")
	}
	sb.WriteString("src/main.ts(4,5): error TS2322: Type 'string' is not assignable to type 'number'.\n")
	sb.WriteString("npm error code ELIFECYCLE\n")
	p := NewBuild()
	if !p.CanHandle("npm run build") {
		t.Fatal("build must handle npm run build")
	}
	out := p.Process("npm run build", sb.String(), 1)
	if !strings.Contains(out, "TS2322") {
		t.Errorf("error line must survive:\n%s", out)
	}
	if strings.Count(out, "npm info lifecycle") > 1 {
		t.Errorf("progress spam should collapse:\n%s", out)
	}
}

func TestGoldenGitStatus(t *testing.T) {
	in := "On branch main\nYour branch is up to date with 'origin/main'.\n\nnothing to commit, working tree clean\n"
	p := NewGit()
	if !p.CanHandle("git status") {
		t.Fatal("git must handle git status")
	}
	out := p.Process("git status", in, 0)
	if !strings.Contains(out, "working tree clean") {
		t.Errorf("clean status must survive:\n%s", out)
	}
}

func TestRegistryRoutingNames(t *testing.T) {
	reg := DefaultRegistry()
	cases := map[string]string{
		"git status":             "git",
		"pytest":                 "test",
		"pip install requests":   "python_install",
		"npm run build":          "build",
		"cargo build":            "generic", // cargo not ported in v1: generic catch-all
		"eslint src":             "lint",
		"ls -la":                 "generic",
		"echo hello world again": "generic",
	}
	for cmd, want := range cases {
		p := reg.Get(want)
		if p == nil {
			t.Fatalf("registry missing %q", want)
		}
		if !p.CanHandle(cmd) {
			t.Errorf("%q should route to %s (CanHandle=false)", cmd, want)
		}
	}
}
