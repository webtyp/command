package command_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tinywasm/command"
)

func TestRun(t *testing.T) {
	got, err := command.Run("echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q, want %q (output must be trimmed)", got, "hello")
	}
}

func TestRunFailureCarriesCommandAndOutput(t *testing.T) {
	_, err := command.Run("ls", "/nonexistent-path-xyz")
	if err == nil {
		t.Fatal("expected an error from a failing command")
	}
	if !strings.Contains(err.Error(), "ls /nonexistent-path-xyz") {
		t.Errorf("error should name the command, got: %v", err)
	}
}

func TestRunInDir(t *testing.T) {
	dir := realPath(t, t.TempDir())

	got, err := command.RunInDir(dir, "pwd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Errorf("got %q, want %q", got, dir)
	}
}

func TestRunInDirFailureNamesTheDir(t *testing.T) {
	dir := realPath(t, t.TempDir())

	_, err := command.RunInDir(dir, "ls", "nonexistent-xyz")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error should name the working dir, got: %v", err)
	}
}

func TestRunWithStdin(t *testing.T) {
	got, err := command.RunWithStdin("secret payload", "cat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "secret payload" {
		t.Errorf("got %q, want %q", got, "secret payload")
	}
}

// A secret passed on stdin must not leak into the error message, which is the
// whole reason RunWithStdin exists.
func TestRunWithStdinKeepsInputOutOfErrors(t *testing.T) {
	_, err := command.RunWithStdin("s3cr3t", "false")
	if err == nil {
		t.Fatal("expected an error from `false`")
	}
	if strings.Contains(err.Error(), "s3cr3t") {
		t.Errorf("stdin leaked into the error: %v", err)
	}
}

func TestRunWithRetrySucceedsAfterFailures(t *testing.T) {
	dir := realPath(t, t.TempDir())
	script := filepath.Join(dir, "flaky.sh")
	counter := filepath.Join(dir, "attempts")

	// Fails on the first two runs, succeeds on the third.
	body := fmt.Sprintf(`#!/bin/sh
printf x >> %q
n=$(wc -c < %q)
[ "$n" -ge 3 ] || exit 1
echo ok
`, counter, counter)
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}

	got, err := command.RunWithRetry(dir, script, nil, 3, time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Errorf("got %q, want %q", got, "ok")
	}
}

func TestRunWithRetryExhausted(t *testing.T) {
	dir := realPath(t, t.TempDir())

	_, err := command.RunWithRetry(dir, "false", nil, 2, time.Millisecond)
	if err == nil {
		t.Fatal("expected an error after exhausting the attempts")
	}
	if !strings.Contains(err.Error(), "after 2 attempts") {
		t.Errorf("error should report the attempt count, got: %v", err)
	}
}

// The contract of this package: arguments reach the binary as a tokenized argv,
// never through a shell. Shell metacharacters must therefore be inert.
func TestNoShellInterpretation(t *testing.T) {
	injected := "hello; touch /tmp/command-pkg-should-never-exist"

	got, err := command.Run("echo", injected)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != injected {
		t.Errorf("argument was re-parsed by a shell: got %q, want the literal %q", got, injected)
	}
	if _, err := os.Stat("/tmp/command-pkg-should-never-exist"); err == nil {
		os.Remove("/tmp/command-pkg-should-never-exist")
		t.Fatal("a shell executed the injected command — the no-shell guarantee is broken")
	}
}

// Globs are not expanded either: they arrive at the binary verbatim.
func TestNoGlobExpansion(t *testing.T) {
	got, err := command.Run("echo", "*.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "*.go" {
		t.Errorf("glob was expanded: got %q, want the literal %q", got, "*.go")
	}
}

func TestRunShellAsync(t *testing.T) {
	dir := realPath(t, t.TempDir())
	marker := filepath.Join(dir, "done")

	if err := command.RunShellAsync(fmt.Sprintf("touch %q", marker)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// It returns before the command finishes; poll briefly for the side effect.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the async shell command never produced its marker file")
}

// Exec is the seam tests use to intercept execution without running anything.
func TestExecIsReplaceable(t *testing.T) {
	original := command.Exec
	defer func() { command.Exec = original }()

	var gotName string
	var gotArgs []string
	command.Exec = func(name string, args ...string) *exec.Cmd {
		gotName, gotArgs = name, args
		return exec.Command("echo", "intercepted")
	}

	out, err := command.Run("git", "status", "--porcelain")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "intercepted" {
		t.Errorf("got %q, want %q", out, "intercepted")
	}
	if gotName != "git" || strings.Join(gotArgs, " ") != "status --porcelain" {
		t.Errorf("interception missed the call: %s %v", gotName, gotArgs)
	}
}

// Commands run in isolated directories concurrently without interfering:
// RunInDir sets cmd.Dir instead of mutating the process working directory.
func TestConcurrentSafeExecution(t *testing.T) {
	const workers = 10
	root := t.TempDir()

	dirs := make([]string, workers)
	for i := range dirs {
		full := filepath.Join(root, fmt.Sprintf("dir_%d", i))
		if err := os.Mkdir(full, 0755); err != nil {
			t.Fatal(err)
		}
		dirs[i] = realPath(t, full)
	}

	var wg sync.WaitGroup
	failures := make(chan error, workers)

	for i, dir := range dirs {
		wg.Add(1)
		go func() {
			defer wg.Done()

			got, err := command.RunInDir(dir, "pwd")
			if err != nil {
				failures <- fmt.Errorf("worker %d: %v", i, err)
				return
			}
			if got != dir {
				failures <- fmt.Errorf("worker %d raced: ran in %q, expected %q", i, got, dir)
			}
		}()
	}

	wg.Wait()
	close(failures)

	for err := range failures {
		t.Error(err)
	}
}

// realPath resolves symlinks so comparisons against `pwd` output hold on
// systems where TempDir is itself a symlink (e.g. /var → /private/var).
func realPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
