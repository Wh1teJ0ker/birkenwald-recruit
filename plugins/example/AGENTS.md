# Agent instructions: Birkenwald evaluation plugin

You are working on a Go plugin for the Birkenwald Recruit evaluation worker.
Read this file before changing or generating plugin code.

## Required package and symbol

- Package must be `main`.
- Export exactly this package-level function:

```go
func Evaluate(ctx context.Context, req pluginapi.Request) (pluginapi.Result, error)
```

- Keep `func main() {}` so the package also passes `go build ./...`. The worker
  does not call `main`.
- Import the contract from
  `github.com/Wh1teJ0ker/birkenwald-recruit/pluginapi`.

## Allowed imports

- Go standard library.
- `github.com/Wh1teJ0ker/birkenwald-recruit/pluginapi`.

The upload compiler rejects cgo (`import "C"` or `#cgo`), third-party modules,
hidden/underscore-prefixed Go files, path traversal, oversized archives and
multiple packages. Do not add `go.mod` or vendored dependencies; the source is
compiled inside the platform module.

## Input

`pluginapi.Request` provides:

- `ProtocolVersion`: always `"1"` for the current worker.
- `JobID`, `TaskID`, `SubmissionID`: identifiers for logs/details.
- `SubmissionPath`: local filesystem path to the student's submitted ZIP.
- `Config`: raw JSON snapshot of the task's plugin configuration.

Treat the submitted ZIP as untrusted. Validate entry names, entry count and
uncompressed sizes. Do not extract to disk unless necessary; prefer
`archive/zip` readers with explicit limits.

## Output

Return a `pluginapi.Result`:

- `ProtocolVersion` must equal `pluginapi.ProtocolVersion` (`"1"`).
- `Score` must be a finite number from 0 to 100.
- `Details` is optional JSON, must be valid and at most 32 KiB.

Return a non-nil error when evaluation cannot complete. Do not return score 0
to represent failure; the platform records failures separately.

## Example behavior

This example reads `required_files` from `req.Config`, opens the submitted ZIP,
and returns the percentage of required entries present:

```json
{"required_files":["README.md","main.go"]}
```

Default required files are `README.md` and `main.go` when the option is absent.
Check `ctx.Err()` before expensive work and before returning.

## Build and test

The supported target is Linux amd64. From the repository root:

```bash
go test ./plugins/example
go build -buildmode=plugin -o plugins/example/example.so ./plugins/example
```

The plugin must use the same Go version, module dependency versions and Linux
architecture as the worker. A Go plugin cannot be unloaded; restart the worker
after replacing a `.so` that is already loaded.

## Upload

Use the administrator plugin page and upload either:

- one `.go` file, or
- a `.zip` containing the plugin source.

Non-Go files in the ZIP are ignored, so `README.md` and this `AGENTS.md` can
stay in the package. A ZIP with one top-level folder is flattened automatically.
Compilation runs on the API host, requires the Go toolchain and a C compiler,
and is not a security sandbox. Only upload trusted code.

## Runtime safety

The compiled plugin runs inside the worker process with the worker's user
permissions and file/network access. There is no container isolation. Honor
`context` cancellation: a timed-out plugin may keep running, and the worker
disables that plugin path until it is restarted.
