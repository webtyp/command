// Package command runs external programs.
//
// Every function except RunShellAsync executes the binary directly through
// os/exec with an already-tokenized argv — there is NO shell in between. That
// is the point of this package, not an implementation detail: arguments are
// never re-parsed, so globbing, quoting, $VAR expansion, `;`, `|` and `&&` have
// no meaning and command injection is impossible.
//
//	command.Run("git", "commit", "-m", userInput) // safe: userInput is one argv entry
//
// If you genuinely need a shell, RunShellAsync says so in its name. Never build
// a command line by concatenating untrusted input into it.
//
// Zero dependencies.
package command

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Exec builds the *exec.Cmd every function here runs. Tests replace it to
// intercept execution; production code never touches it.
var Exec = exec.Command

// Run executes name with args and returns its combined output, trimmed.
// A non-zero exit is an error whose message carries the command and its output.
func Run(name string, args ...string) (string, error) {
	return run(Exec(name, args...), "", name, args)
}

// RunInDir executes name with args inside dir.
func RunInDir(dir, name string, args ...string) (string, error) {
	cmd := Exec(name, args...)
	cmd.Dir = dir
	return run(cmd, dir, name, args)
}

// RunWithStdin executes name with args, piping input to its stdin.
// Use it for secrets: an argv is visible in the process table and in the error
// messages of this package, stdin is not.
func RunWithStdin(input, name string, args ...string) (string, error) {
	cmd := Exec(name, args...)
	cmd.Stdin = strings.NewReader(input)
	return run(cmd, "", name, args)
}

// RunWithRetry executes name in dir, retrying up to attempts times, waiting
// delay between tries. It returns as soon as one attempt succeeds.
func RunWithRetry(dir, name string, args []string, attempts int, delay time.Duration) (string, error) {
	var (
		output string
		err    error
	)

	for i := range attempts {
		if output, err = RunInDir(dir, name, args...); err == nil {
			return output, nil
		}
		if i < attempts-1 {
			time.Sleep(delay)
		}
	}

	return output, fmt.Errorf("command %s failed in %s after %d attempts: %w", name, dir, attempts, err)
}

// RunShellAsync starts commandLine in the platform shell (sh -c, or cmd.exe /C
// on Windows) and returns immediately without waiting for it to finish.
//
// This is the ONLY function here that opens a shell, so commandLine IS parsed:
// never build it from untrusted input.
func RunShellAsync(commandLine string) error {
	shell, flag := "sh", "-c"
	if runtime.GOOS == "windows" {
		shell, flag = "cmd.exe", "/C"
	}
	return exec.Command(shell, flag, commandLine).Start()
}

// run executes cmd and normalizes its result: output is trimmed, and a failure
// reports the command, the wrapped error and whatever the command printed.
func run(cmd *exec.Cmd, dir, name string, args []string) (string, error) {
	outputBytes, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(outputBytes))

	if err == nil {
		return output, nil
	}

	line := strings.TrimSpace(name + " " + strings.Join(args, " "))
	if dir != "" {
		return output, fmt.Errorf("command failed in %s: %s\nError: %w\nOutput: %s", dir, line, err, output)
	}
	return output, fmt.Errorf("command failed: %s\nError: %w\nOutput: %s", line, err, output)
}
