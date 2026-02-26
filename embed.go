// Package dashwasi embeds the dash WASI reactor binary and defines
// export constants for the re-entrant shell API.
package dashwasi

import _ "embed"

// DashWASM contains the binary contents of the dash WASI reactor build.
//
// This is a reactor-model WASM that exports a re-entrant shell API.
// The host calls dash_init() once, then dash_eval() repeatedly.
// Shell state (variables, functions, aliases, exit status) persists
// in WASM linear memory between calls.
//
//go:embed dash.wasm
var DashWASM []byte

// DashWASMFilename is the filename for DashWASM.
const DashWASMFilename = "dash.wasm"

// Memory management exports.
const (
	// ExportMalloc allocates memory in WASM linear memory.
	ExportMalloc = "malloc"

	// ExportFree frees memory in WASM linear memory.
	ExportFree = "free"

	// ExportRealloc reallocates memory in WASM linear memory.
	ExportRealloc = "realloc"

	// ExportCalloc allocates zeroed memory in WASM linear memory.
	ExportCalloc = "calloc"
)

// Dash reactor exports.
const (
	// ExportDashInit initializes the dash shell runtime.
	// Performs same initialization as main() up to but not including cmdloop().
	// Signature: dash_init(argc: i32, argv: i32) -> i32
	// Returns: 0 on success, -1 on error.
	ExportDashInit = "dash_init"

	// ExportDashEval evaluates a command string.
	// Primary re-entrant entry point. Shell state persists between calls.
	// Signature: dash_eval(cmd: i32, len: i32) -> i32
	// Returns: exit status of the last command, or -1 on error.
	ExportDashEval = "dash_eval"

	// ExportDashGetExitStatus gets the current exit status.
	// Signature: dash_get_exitstatus() -> i32
	ExportDashGetExitStatus = "dash_get_exitstatus"

	// ExportDashGetVar gets a shell variable value.
	// Signature: dash_getvar(name: i32) -> i32 (char*)
	// Returns: pointer to value string, or NULL if not set.
	ExportDashGetVar = "dash_getvar"

	// ExportDashSetVar sets a shell variable.
	// Signature: dash_setvar(name: i32, value: i32) -> i32
	// Returns: 0 on success, -1 on error.
	ExportDashSetVar = "dash_setvar"

	// ExportDashDestroy destroys the dash runtime.
	// Signature: dash_destroy() -> void
	ExportDashDestroy = "dash_destroy"
)
