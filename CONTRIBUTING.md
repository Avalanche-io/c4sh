# Contributing to c4sh

Thank you for your interest in contributing to c4sh! This document provides guidelines for contributors.

## Project Structure

c4sh is a shell integration tool that makes c4m files behave like directories. It depends on the [c4](https://github.com/Avalanche-io/c4) library for c4m parsing, encoding, and content store operations.

```
main.go              — command dispatch
c4mutil.go           — shared c4m and store helpers
cmd_*.go             — one file per command (cd, ls, cat, cp, mv, rm, mkdir, pool, ingest, rsync)
cmd_shellinit.go     — shell integration scripts (bash/zsh/PowerShell)
cmd_complete.go      — tab completion
platform_unix.go     — Unix exec and command lookup
platform_windows.go  — Windows exec and command lookup
internal/ctx/        — shell context (environment variable management)
```

## Development

```bash
git clone https://github.com/Avalanche-io/c4sh.git
cd c4sh
go build .
go vet ./...
```

During development, `go.mod` uses a `replace` directive to reference a local checkout of the c4 library. For releases, this is replaced with a tagged version.

## Code Style

- Follow standard Go conventions (`gofmt`)
- Keep functions focused — c4sh is a thin layer of c4m text manipulation
- Match flag conventions of real Unix counterparts (`-l`, `-a`, `-rf`, `-p`)
- Commands that don't involve c4m paths should fall through to the real system command

## Pull Requests

1. Create your branch from `main`
2. Make clear, concise commits (single-line messages)
3. Ensure `go build .` and `go vet ./...` pass
4. Submit PR against `main`

## Bugs

Report issues at https://github.com/Avalanche-io/c4sh/issues

## License

By contributing to c4sh, you agree that your contributions will be licensed under the Apache License. See [LICENSE](LICENSE) for details.
