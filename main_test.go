package main

// Tests for the behaviour README.md documents. Until now this repository had no test at
// all: CI ran `go build ./... && go vet ./...` on Linux only, so the runner's two
// documented contracts were unverified.
//
//   1. target collection: a directory contributes its `*_test.qk` and its `tests/*.qk`
//      ("目录（收集 *_test.qk 与 tests/*.qk）")
//   2. the verdict rules: a test fails when the interpreter exits non-zero, when its output
//      contains "FAIL ", or when its summary reports `failures=N` with N > 0
//      ("判定：退出码非 0、或输出含 "FAIL "、或汇总里 failures 非 0 → 失败")
//   3. no tests found is an error, not a silent pass
//
// The verdict rules are exercised end to end against a stub interpreter compiled on the fly,
// so they are not re-implemented (and therefore not re-asserted) inside this file.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestCollectDiscoversTheDocumentedPatterns(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a_test.qk"), "program main;\n")
	write(t, filepath.Join(dir, "plain.qk"), "program main;\n")
	write(t, filepath.Join(dir, "tests", "b.qk"), "program main;\n")
	write(t, filepath.Join(dir, "tests", "notes.txt"), "ignored\n")

	got := names(collect([]string{dir}))
	want := []string{"a_test", "b"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("collect(dir) = %v, want %v", got, want)
	}

	// A directory plus one of the files it already contributed must not run that file twice.
	got = names(collect([]string{dir, filepath.Join(dir, "a_test.qk")}))
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("collect(dir, a_test.qk) = %v, want the same %v (de-duplicated)", got, want)
	}

	// An explicitly named file is collected even though it is not named *_test.qk.
	got = names(collect([]string{filepath.Join(dir, "plain.qk")}))
	if strings.Join(got, ",") != "plain" {
		t.Errorf("collect(plain.qk) = %v, want [plain]", got)
	}
}

func TestRunAppliesTheDocumentedVerdictRules(t *testing.T) {
	cases := []struct {
		name       string
		output     string
		quarkExit  int
		wantPassed bool
	}{
		{"clean-pass", "PASS demo (1 assertions)\nfailures=0\n", 0, true},
		{"fail-marker-with-zero-exit", "FAIL 故意失败示例 got=[x] want=[y]\nfailures=1\n", 0, false},
		// The FAIL marker alone must fail the run: a harness that prints FAIL but then reports
		// failures=0 is still a failing test (README: 输出含 "FAIL " → 失败).
		{"fail-marker-despite-zero-count", "FAIL 故意失败示例 got=[x] want=[y]\nfailures=0\n", 0, false},
		{"failures-count-with-zero-exit", "failures=2\n", 0, false},
		{"non-zero-interpreter-exit", "something went wrong\n", 3, false},
	}

	qktest := buildBinary(t, "qktest", ".")
	fake := buildFakeQuark(t)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, "sample_test.qk"), "program main;\n")

			out, code := run(t, qktest, []string{"-j", "1", dir}, []string{
				"QUARK=" + fake,
				"QKT_FAKE_OUTPUT=" + tc.output,
				"QKT_FAKE_EXIT=" + itoa(tc.quarkExit),
			})

			passed := code == 0
			if passed != tc.wantPassed {
				t.Errorf("exit code = %d, want passed=%v\noutput:\n%s", code, tc.wantPassed, out)
			}
			if !strings.Contains(out, "qktest: ") {
				t.Errorf("summary line missing from output:\n%s", out)
			}
			if tc.wantPassed && !strings.Contains(out, "PASS sample") {
				t.Errorf("expected a PASS line, got:\n%s", out)
			}
			if !tc.wantPassed && !strings.Contains(out, "FAIL sample") {
				t.Errorf("expected a FAIL line, got:\n%s", out)
			}
		})
	}
}

func TestNoTestsFoundIsAnError(t *testing.T) {
	qktest := buildBinary(t, "qktest", ".")
	out, code := run(t, qktest, []string{t.TempDir()}, nil)
	if code == 0 {
		t.Errorf("an empty directory exited 0, want non-zero\noutput:\n%s", out)
	}
}

func names(cases []testCase) []string {
	out := make([]string, 0, len(cases))
	for _, c := range cases {
		out = append(out, c.name)
	}
	sort.Strings(out)
	return out
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	if neg {
		d = append([]byte{'-'}, d...)
	}
	return string(d)
}

func binaryName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// buildBinary compiles a package of this module into a temporary directory.
func buildBinary(t *testing.T, name, pkg string) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), binaryName(name))
	cmd := exec.Command("go", "build", "-o", exe, pkg)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, out)
	}
	return exe
}

// buildFakeQuark compiles a stub interpreter that prints QKT_FAKE_OUTPUT and exits with
// QKT_FAKE_EXIT, so the verdict rules can be driven from the outside.
func buildFakeQuark(t *testing.T) string {
	t.Helper()
	src := `package main

import (
	"fmt"
	"os"
	"strconv"
)

func main() {
	fmt.Print(os.Getenv("QKT_FAKE_OUTPUT"))
	code, _ := strconv.Atoi(os.Getenv("QKT_FAKE_EXIT"))
	os.Exit(code)
}
`
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module fakequark\n\ngo 1.21\n")
	write(t, filepath.Join(dir, "main.go"), src)
	exe := filepath.Join(dir, binaryName("fakequark"))
	cmd := exec.Command("go", "build", "-o", exe, ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build fake quark: %v\n%s", err, out)
	}
	return exe
}

func run(t *testing.T, exe string, args, extraEnv []string) (string, int) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running %s %v: %v", exe, args, err)
		}
		code = exitErr.ExitCode()
	}
	return string(out), code
}
