# command
<img src="docs/img/badges.svg">

Process execution helpers over `os/exec`. **No shell, no injection. Zero dependencies.**

```go
import "webtyp.com/command"
```

## The guarantee

Every function except `RunShellAsync` executes the binary **directly**, with an already
tokenized `argv`. There is no shell in between — and that is the point of this package, not an
implementation detail.

```go
command.Run("git", "commit", "-m", userInput) // safe, whatever userInput contains
```

Because arguments are never re-parsed, shell metacharacters are inert: `;`, `|`, `&&`, `$VAR`,
quotes and globs reach the binary **verbatim**. Command injection is impossible by
construction, and `command.Run("echo", "*.go")` prints a literal `*.go`. Both properties are
locked by tests (`TestNoShellInterpretation`, `TestNoGlobExpansion`).

If you genuinely need a shell, `RunShellAsync` says so in its name — and it is the one place
where you must never concatenate untrusted input.

## API

| Function | Purpose |
|---|---|
| `Run(name, args...)` | Run a binary; returns its combined output, trimmed. |
| `RunInDir(dir, name, args...)` | Same, inside `dir`. Sets `cmd.Dir`, so it is **safe under concurrency** — it never mutates the process working directory. |
| `RunWithStdin(input, name, args...)` | Pipes `input` to stdin. Use it for **secrets**: an argv is visible in the process table and in this package's error messages; stdin is not. |
| `RunWithRetry(dir, name, args, attempts, delay)` | Retries a flaky command, returning as soon as one attempt succeeds. |
| `RunShellAsync(commandLine)` | The **only** function that opens a shell (`sh -c`, or `cmd.exe /C` on Windows). Returns immediately, without waiting. |

A non-zero exit is an error whose message carries the command, the wrapped error and whatever
the command printed:

```
command failed in /home/me/repo: git push
Error: exit status 128
Output: fatal: could not read from remote repository
```

## Testing

`Exec` is the seam. Replace it to intercept execution without running anything:

```go
original := command.Exec
defer func() { command.Exec = original }()

command.Exec = func(name string, args ...string) *exec.Cmd {
    return exec.Command("echo", "intercepted")
}
```

## License

MIT
