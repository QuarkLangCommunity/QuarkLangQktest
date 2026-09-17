// qktest: QuarkLang 测试运行器（工具链成员）
//
// 用法:
//
//	qktest [-v] [-run regex] [-j N] [-L libdir]... [target...]
//
//	target 可以是测试文件（*.qk）或目录；目录会收集 <dir>/*_test.qk 与 <dir>/tests/*.qk。
//	每个测试在临时目录里执行：测试文件作为 main.qk，配合 -L 目录里的库 .qk（import 用）。
//	判定：退出码非 0、或输出含 "FAIL "、或汇总里 failures 非 0 → 失败。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type testCase struct {
	name string
	file string
}

func collect(targets []string) []testCase {
	var out []testCase
	seen := map[string]bool{}
	add := func(p string) {
		abs, _ := filepath.Abs(p)
		if seen[abs] {
			return
		}
		seen[abs] = true
		out = append(out, testCase{name: strings.TrimSuffix(filepath.Base(p), ".qk"), file: p})
	}
	for _, t := range targets {
		info, err := os.Stat(t)
		if err != nil {
			fmt.Fprintln(os.Stderr, "qktest:", err)
			os.Exit(2)
		}
		if info.IsDir() {
			for _, pat := range []string{"*_test.qk", filepath.Join("tests", "*.qk")} {
				ms, _ := filepath.Glob(filepath.Join(t, pat))
				sort.Strings(ms)
				for _, m := range ms {
					add(m)
				}
			}
			continue
		}
		add(t)
	}
	return out
}

type result struct {
	name   string
	pass   bool
	ms     int64
	detail string
}

func runCase(tc testCase, libDirs []string, quark string, verbose bool) result {
	start := time.Now()
	dir, err := os.MkdirTemp("", "qktest-")
	if err != nil {
		return result{tc.name, false, 0, err.Error()}
	}
	defer os.RemoveAll(dir)

	src, err := os.ReadFile(tc.file)
	if err != nil {
		return result{tc.name, false, 0, err.Error()}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.qk"), src, 0o644); err != nil {
		return result{tc.name, false, 0, err.Error()}
	}
	// 复制库文件（-L 目录里的所有 .qk，main.qk 除外）
	for _, ld := range libDirs {
		ms, _ := filepath.Glob(filepath.Join(ld, "*.qk"))
		for _, m := range ms {
			base := filepath.Base(m)
			if base == "main.qk" {
				continue
			}
			b, err := os.ReadFile(m)
			if err != nil {
				continue
			}
			_ = os.WriteFile(filepath.Join(dir, base), b, 0o644)
		}
	}
	cmd := exec.Command(quark, "main.qk")
	cmd.Dir = dir
	combined, err := cmd.CombinedOutput()
	out := string(combined)
	ms := time.Since(start).Milliseconds()

	pass := err == nil
	if strings.Contains(out, "FAIL ") || regexp.MustCompile("failures=([1-9][0-9]*)").MatchString(out) {
		pass = false
	}
	if !pass {
		var lines []string
		for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			if strings.Contains(l, "FAIL") || strings.Contains(l, "error:") || strings.Contains(l, "failures=") {
				lines = append(lines, l)
			}
		}
		if len(lines) == 0 {
			lines = []string{strings.TrimSpace(out)}
		}
		if len(lines) > 8 {
			lines = lines[:8]
		}
		detail := strings.Join(lines, "\n    ")
		if err != nil && !strings.Contains(detail, "error:") {
			detail += "\n    " + err.Error()
		}
		return result{tc.name, false, ms, detail}
	}
	if verbose {
		return result{tc.name, true, ms, strings.TrimRight(out, "\n")}
	}
	return result{tc.name, true, ms, ""}
}

func main() {
	quark := os.Getenv("QUARK")
	if quark == "" {
		quark = "quark"
	}
	var (
		targets []string
		libDirs []string
		filter  string
		jobs    = runtime.NumCPU()
		verbose bool
	)
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-v":
			verbose = true
		case a == "-run" && i+1 < len(args):
			i++
			filter = args[i]
		case a == "-j" && i+1 < len(args):
			i++
			fmt.Sscanf(args[i], "%d", &jobs)
		case a == "-L" && i+1 < len(args):
			i++
			libDirs = append(libDirs, args[i])
		case strings.HasPrefix(a, "-L") && len(a) > 2:
			libDirs = append(libDirs, a[2:])
		case a == "-h" || a == "--help":
			fmt.Println("usage: qktest [-v] [-run regex] [-j N] [-L libdir]... [target...]")
			return
		default:
			targets = append(targets, a)
		}
	}
	if len(targets) == 0 {
		targets = []string{"."}
	}
	cases := collect(targets)
	if filter != "" {
		re := regexp.MustCompile(filter)
		var kept []testCase
		for _, c := range cases {
			if re.MatchString(c.name) {
				kept = append(kept, c)
			}
		}
		cases = kept
	}
	if len(cases) == 0 {
		fmt.Println("qktest: 没有找到测试（目录下需有 *_test.qk 或 tests/*.qk）")
		os.Exit(1)
	}
	if jobs < 1 {
		jobs = 1
	}
	results := make([]result, len(cases))
	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup
	for i, c := range cases {
		wg.Add(1)
		go func(i int, c testCase) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = runCase(c, libDirs, quark, verbose)
		}(i, c)
	}
	wg.Wait()

	passed, failed := 0, 0
	for _, r := range results {
		if r.pass {
			passed++
			fmt.Printf("PASS %s (%dms)\n", r.name, r.ms)
			if r.detail != "" {
				fmt.Println(r.detail)
			}
		} else {
			failed++
			fmt.Printf("FAIL %s (%dms)\n    %s\n", r.name, r.ms, r.detail)
		}
	}
	fmt.Printf("qktest: %d passed, %d failed\n", passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}
