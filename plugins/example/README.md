# Birkenwald example plugin

This directory contains a reference Go plugin for Birkenwald Recruit.

## Contract

The worker loads the compiled `.so` with `plugin.Open`, looks up `Evaluate`, and
calls it with the configured timeout:

```go
func Evaluate(ctx context.Context, req pluginapi.Request) (pluginapi.Result, error)
```

`Request` contains the protocol version, job/task/submission IDs, the local path
to the student's ZIP package, and the task's JSON configuration snapshot.
`Result` must contain `ProtocolVersion: "1"` and a finite `Score` between 0 and
100. Optional `Details` must be valid JSON and no larger than 32 KiB.

Return an error when evaluation cannot complete. A failure is shown separately
and must not be converted to a score of zero.

## Configuration

This example accepts:

```json
{"required_files":["README.md","main.go"]}
```

It opens the submitted ZIP and scores the percentage of required files present.
When `required_files` is omitted it uses `README.md` and `main.go`.

## Build

The supported target is Linux amd64. From the repository root, with CGO
enabled and the same Go toolchain as the worker:

```bash
go build -buildmode=plugin -o plugins/example/example.so ./plugins/example
```

The plugin must be compiled with the same Go version, dependency versions and
architecture as the worker on Linux. Go plugins cannot be unloaded; restart the
worker after replacing a `.so` that has already been loaded.

## Upload

The administrator plugin page accepts either a single `.go` file or a `.zip`
containing `.go` files. This directory is already arranged for upload: the ZIP
may contain the `main.go` at its root or inside one top-level directory.
Non-Go files such as this README and `AGENTS.md` are ignored by the compiler.

## Safety

Plugins run in the worker process with the worker's user permissions and file
and network access. There is no container or sandbox isolation. Only trusted,
reviewed code should be loaded. Plugins should honor `ctx` cancellation; a
timed-out plugin may continue running and its path is disabled in that worker
until restart.
