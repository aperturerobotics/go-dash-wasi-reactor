# go-dash-wasi-reactor

[![GoDoc Widget]][GoDoc] [![Go Report Card Widget]][Go Report Card]

> A Go module that embeds the dash shell WASI WebAssembly runtime (reactor model).

[GoDoc]: https://godoc.org/github.com/aperturerobotics/go-dash-wasi-reactor
[GoDoc Widget]: https://godoc.org/github.com/aperturerobotics/go-dash-wasi-reactor?status.svg
[Go Report Card Widget]: https://goreportcard.com/badge/github.com/aperturerobotics/go-dash-wasi-reactor
[Go Report Card]: https://goreportcard.com/report/github.com/aperturerobotics/go-dash-wasi-reactor

## Related Projects

- [go-quickjs-wasi-reactor](https://github.com/aperturerobotics/go-quickjs-wasi-reactor) - QuickJS JavaScript engine (same reactor pattern)
- [dash](https://github.com/aperturerobotics/dash) - Fork of dash with WASI reactor support

## About Dash

[Dash](https://en.wikipedia.org/wiki/Almquist_shell#dash) is a POSIX-compliant shell derived from the Almquist shell (ash). It is small (~15K lines of C), fast, and widely used as `/bin/sh` on Debian and Ubuntu systems.

This project compiles dash to WebAssembly with WASI support using the **reactor model**, enabling re-entrant shell execution from a host environment. The host calls `dash_init()` once, then `dash_eval()` repeatedly. Shell state (variables, functions, aliases, exit status) persists in WASM linear memory between calls.

## Purpose

This module provides easy access to the dash shell compiled to WebAssembly, embedded directly in the Go module. It uses [wazero](https://wazero.io/) (pure Go, no CGo) as the WASM runtime.

### setjmp/longjmp via Snapshot/Restore

Dash uses `setjmp`/`longjmp` for error recovery. Standard wasi-sdk compiles these using WASM Exception Handling, which wazero does not support. This project implements `setjmp`/`longjmp` using wazero's experimental [snapshot/restore](https://pkg.go.dev/github.com/tetratelabs/wazero/experimental#Snapshotter) mechanism instead:

- **setjmp**: Host captures a snapshot of the WASM execution state plus the C stack memory
- **longjmp**: Host restores the saved snapshot, making `setjmp` return the longjmp value

This approach follows the same pattern used by [go-pgquery](https://github.com/wasilibs/go-pgquery) for PostgreSQL's setjmp/longjmp.

### Reactor Model

Unlike the standard WASI "command" model that blocks in `_start()`, the reactor model exports named functions that the host calls repeatedly:

- `dash_init(argc, argv)` - Initialize the shell runtime
- `dash_eval(cmd, len)` - Evaluate a command string
- `dash_get_exitstatus()` - Get exit status of last command
- `dash_getvar(name)` - Get a shell variable
- `dash_setvar(name, value)` - Set a shell variable
- `dash_destroy()` - Tear down the runtime

**Memory Management:**

- `malloc`, `free`, `realloc`, `calloc` - For host to allocate memory in WASM linear memory

## Packages

### Root Package (`github.com/aperturerobotics/go-dash-wasi-reactor`)

Provides the embedded WASM binary and export constants:

```go
package main

import (
    "fmt"
    dashwasi "github.com/aperturerobotics/go-dash-wasi-reactor"
)

func main() {
    wasmBytes := dashwasi.DashWASM
    fmt.Printf("Dash WASM size: %d bytes\n", len(wasmBytes))
}
```

### Wazero Dash Library (`github.com/aperturerobotics/go-dash-wasi-reactor/wazero-dash`)

High-level Go API for running shell commands with wazero:

```go
package main

import (
    "context"
    "fmt"
    "os"

    dash "github.com/aperturerobotics/go-dash-wasi-reactor/wazero-dash"
    "github.com/tetratelabs/wazero"
)

func main() {
    ctx := context.Background()
    r := wazero.NewRuntime(ctx)
    defer r.Close(ctx)

    config := wazero.NewModuleConfig().
        WithStdout(os.Stdout).
        WithStderr(os.Stderr)

    d, _ := dash.NewDash(ctx, r, config)
    defer d.Close(ctx)

    d.Init(ctx, nil)

    // Evaluate shell commands
    d.Eval(ctx, "echo hello world")

    // Variables persist between calls
    d.Eval(ctx, "FOO=bar")
    val, _ := d.GetVar(ctx, "FOO")
    fmt.Println("FOO =", val) // FOO = bar

    // Set variables from the host
    d.SetVar(ctx, "HOST_VAR", "from_go")
    d.Eval(ctx, "echo $HOST_VAR") // from_go

    // Check exit status
    status, _ := d.Eval(ctx, "false")
    fmt.Println("exit status:", status) // exit status: 1
}
```

## Building the WASM Binary

The WASM binary is built from the [aperturerobotics/dash](https://github.com/aperturerobotics/dash) fork using wasi-sdk:

```bash
cd /path/to/dash

# Native build first (generates parser tables, builtins, etc.)
./autogen.sh && ./configure && make

# WASI reactor build
mkdir -p build-wasi && cd build-wasi
cmake .. \
  -DCMAKE_SYSTEM_NAME=WASI \
  -DCMAKE_C_COMPILER=/path/to/wasi-sdk/bin/clang \
  -DCMAKE_SYSROOT=/path/to/wasi-sdk/share/wasi-sysroot \
  -DDASH_WASI_REACTOR=ON
cmake --build .

# Copy output
cp dash.wasm /path/to/go-dash-wasi-reactor/dash.wasm
```

## Testing

```bash
go test ./wazero-dash/
```

## License

BSD 3-Clause, same as the upstream dash project.
