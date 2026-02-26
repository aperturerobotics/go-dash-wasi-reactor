// Command dash-wasi runs an interactive dash shell inside a WASI WASM runtime.
//
// Usage:
//
//	dash-wasi              # interactive REPL
//	dash-wasi -c 'echo hi' # execute a command string
//	dash-wasi script.sh    # execute a script file
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"

	dash "github.com/aperturerobotics/go-dash-wasi-reactor/wazero-dash"
	"github.com/tetratelabs/wazero"
)

func main() {
	ctx := context.Background()

	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	config := wazero.NewModuleConfig().
		WithStdin(os.Stdin).
		WithStdout(os.Stdout).
		WithStderr(os.Stderr)

	d, err := dash.NewDash(ctx, r, config)
	if err != nil {
		log.Fatalf("failed to create dash: %v", err)
	}
	defer d.Close(ctx)

	if err := d.Init(ctx, nil); err != nil {
		log.Fatalf("failed to init dash: %v", err)
	}

	// -c flag: execute command and exit.
	if len(os.Args) >= 3 && os.Args[1] == "-c" {
		status, err := d.Eval(ctx, os.Args[2])
		if err != nil {
			log.Fatalf("eval error: %v", err)
		}
		os.Exit(status)
	}

	// File argument: read and execute.
	if len(os.Args) >= 2 && os.Args[1] != "-" {
		code, err := os.ReadFile(os.Args[1])
		if err != nil {
			log.Fatalf("failed to read %s: %v", os.Args[1], err)
		}
		status, err := d.Eval(ctx, string(code))
		if err != nil {
			log.Fatalf("eval error: %v", err)
		}
		os.Exit(status)
	}

	// Interactive REPL.
	runREPL(ctx, d)
}

func runREPL(ctx context.Context, d *dash.Dash) {
	fmt.Fprintln(os.Stderr, "dash-wasi (POSIX shell in WASM, type 'exit' or Ctrl+D to quit)")

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Fprint(os.Stderr, "$ ")
		if !scanner.Scan() {
			fmt.Fprintln(os.Stderr)
			break
		}

		line := scanner.Text()
		if line == "exit" || line == "quit" {
			break
		}
		if line == "" {
			continue
		}

		status, err := d.Eval(ctx, line)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}
		_ = status
	}
}
