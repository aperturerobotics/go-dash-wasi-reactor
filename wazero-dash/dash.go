// Package dash provides a high-level Go API for running shell commands
// using the dash WASI reactor module with wazero.
//
// setjmp/longjmp are implemented via wazero's experimental snapshot/restore
// mechanism. The dash WASM binary imports __setjmp and __longjmp from the
// "env" module. The host provides these as snapshot (setjmp) and restore
// (longjmp) operations.
package dash

import (
	"context"
	"errors"

	dashwasi "github.com/aperturerobotics/go-dash-wasi-reactor"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// dashStateKey is the context key for checkpoint state.
type dashStateKey struct{}

// checkpoint holds saved execution state for a setjmp call.
type checkpoint struct {
	snapshot     experimental.Snapshot
	stackPointer uint32
	cstack       []byte
}

// dashState holds the setjmp/longjmp checkpoint state shared between
// host functions and the Dash wrapper.
type dashState struct {
	checkpoints []*checkpoint
}

// Dash wraps a dash WASI reactor module providing a high-level API
// for shell command execution.
type Dash struct {
	runtime wazero.Runtime
	mod     api.Module
	state   *dashState

	malloc api.Function
	free   api.Function

	dashInit          api.Function
	dashEval          api.Function
	dashGetExitStatus api.Function
	dashGetVar        api.Function
	dashSetVar        api.Function
	dashDestroy       api.Function

	initialized bool
}

// CompileDash compiles the embedded dash WASM module.
// The compiled module can be reused across multiple Dash instances.
func CompileDash(ctx context.Context, r wazero.Runtime) (wazero.CompiledModule, error) {
	return r.CompileModule(ctx, dashwasi.DashWASM)
}

// NewDash creates a new Dash instance using the embedded WASM reactor.
// Call Close() when done to release resources.
func NewDash(ctx context.Context, r wazero.Runtime, config wazero.ModuleConfig) (*Dash, error) {
	state := &dashState{}

	// Install WASI.
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, r); err != nil {
		return nil, err
	}

	// Install host functions for setjmp/longjmp via snapshot/restore.
	if _, err := r.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(setjmpHost).
		Export("__setjmp").
		NewFunctionBuilder().
		WithFunc(longjmpHost).
		Export("__longjmp").
		Instantiate(ctx); err != nil {
		return nil, err
	}

	compiled, err := CompileDash(ctx, r)
	if err != nil {
		return nil, err
	}

	return newDashFromCompiled(ctx, r, compiled, config, state)
}

// newDashFromCompiled instantiates dash from a pre-compiled module.
func newDashFromCompiled(ctx context.Context, r wazero.Runtime, compiled wazero.CompiledModule, config wazero.ModuleConfig, state *dashState) (*Dash, error) {
	ctx = withDashState(ctx, state)

	mod, err := r.InstantiateModule(ctx, compiled, config.WithName(dashwasi.DashWASMFilename))
	if err != nil {
		return nil, err
	}

	// Call _initialize for WASI reactor startup.
	initFn := mod.ExportedFunction("_initialize")
	if initFn != nil {
		if _, err := initFn.Call(ctx); err != nil {
			_ = mod.Close(ctx)
			return nil, errors.New("_initialize failed: " + err.Error())
		}
	}

	d := &Dash{
		runtime: r,
		mod:     mod,
		state:   state,

		malloc: mod.ExportedFunction(dashwasi.ExportMalloc),
		free:   mod.ExportedFunction(dashwasi.ExportFree),

		dashInit:          mod.ExportedFunction(dashwasi.ExportDashInit),
		dashEval:          mod.ExportedFunction(dashwasi.ExportDashEval),
		dashGetExitStatus: mod.ExportedFunction(dashwasi.ExportDashGetExitStatus),
		dashGetVar:        mod.ExportedFunction(dashwasi.ExportDashGetVar),
		dashSetVar:        mod.ExportedFunction(dashwasi.ExportDashSetVar),
		dashDestroy:       mod.ExportedFunction(dashwasi.ExportDashDestroy),
	}

	if d.malloc == nil {
		_ = mod.Close(ctx)
		return nil, errors.New("missing export: " + dashwasi.ExportMalloc)
	}
	if d.free == nil {
		_ = mod.Close(ctx)
		return nil, errors.New("missing export: " + dashwasi.ExportFree)
	}
	if d.dashInit == nil {
		_ = mod.Close(ctx)
		return nil, errors.New("missing export: " + dashwasi.ExportDashInit)
	}
	if d.dashEval == nil {
		_ = mod.Close(ctx)
		return nil, errors.New("missing export: " + dashwasi.ExportDashEval)
	}
	if d.dashDestroy == nil {
		_ = mod.Close(ctx)
		return nil, errors.New("missing export: " + dashwasi.ExportDashDestroy)
	}

	return d, nil
}

// withDashState returns a context with snapshotter enabled and dash state attached.
func withDashState(ctx context.Context, state *dashState) context.Context {
	ctx = experimental.WithSnapshotter(ctx)
	return context.WithValue(ctx, dashStateKey{}, state)
}

// callCtx returns a context configured for calling exported functions.
func (d *Dash) callCtx(ctx context.Context) context.Context {
	return withDashState(ctx, d.state)
}

// allocString allocates a null-terminated string in WASM memory.
func (d *Dash) allocString(ctx context.Context, s string) (uint32, error) {
	b := []byte(s)
	results, err := d.malloc.Call(ctx, uint64(len(b)+1))
	if err != nil {
		return 0, err
	}
	ptr := uint32(results[0])
	if ptr == 0 {
		return 0, errors.New("malloc returned null")
	}
	if !d.mod.Memory().Write(ptr, append(b, 0)) {
		_, _ = d.free.Call(ctx, uint64(ptr))
		return 0, errors.New("failed to write string to memory")
	}
	return ptr, nil
}

// freePtr frees a pointer in WASM memory.
func (d *Dash) freePtr(ctx context.Context, ptr uint32) {
	if ptr != 0 {
		_, _ = d.free.Call(ctx, uint64(ptr))
	}
}

// Init initializes the dash shell runtime.
// Must be called before Eval. Pass nil args for default initialization.
func (d *Dash) Init(ctx context.Context, args []string) error {
	if d.initialized {
		return errors.New("dash already initialized")
	}

	ctx = d.callCtx(ctx)

	if len(args) == 0 {
		args = []string{"dash"}
	}

	argc := len(args)
	ptrs := make([]uint32, argc)
	for i, arg := range args {
		ptr, err := d.allocString(ctx, arg)
		if err != nil {
			for j := 0; j < i; j++ {
				d.freePtr(ctx, ptrs[j])
			}
			return err
		}
		ptrs[i] = ptr
	}

	// Allocate argv array (4 bytes per pointer in wasm32).
	results, err := d.malloc.Call(ctx, uint64(argc*4))
	if err != nil {
		for _, ptr := range ptrs {
			d.freePtr(ctx, ptr)
		}
		return err
	}
	argv := uint32(results[0])
	if argv == 0 {
		for _, ptr := range ptrs {
			d.freePtr(ctx, ptr)
		}
		return errors.New("malloc returned null for argv")
	}

	// Write argv pointers (little-endian wasm32).
	for i, ptr := range ptrs {
		d.mod.Memory().WriteUint32Le(argv+uint32(i*4), ptr)
	}

	initResults, err := d.dashInit.Call(ctx, uint64(argc), uint64(argv))

	d.freePtr(ctx, argv)
	for _, ptr := range ptrs {
		d.freePtr(ctx, ptr)
	}

	if err != nil {
		return errors.New("dash_init failed: " + err.Error())
	}
	if int32(initResults[0]) != 0 {
		return errors.New("dash_init returned error")
	}

	d.initialized = true
	return nil
}

// Eval evaluates a shell command string.
// Returns the exit status of the last command.
func (d *Dash) Eval(ctx context.Context, cmd string) (int, error) {
	if !d.initialized {
		return -1, errors.New("dash not initialized")
	}

	ctx = d.callCtx(ctx)

	cmdPtr, err := d.allocString(ctx, cmd)
	if err != nil {
		return -1, err
	}
	defer d.freePtr(ctx, cmdPtr)

	results, err := d.dashEval.Call(ctx, uint64(cmdPtr), uint64(len(cmd)))
	if err != nil {
		return -1, errors.New("dash_eval failed: " + err.Error())
	}

	return int(int32(results[0])), nil
}

// GetExitStatus returns the exit status of the last command.
func (d *Dash) GetExitStatus(ctx context.Context) (int, error) {
	if !d.initialized {
		return -1, errors.New("dash not initialized")
	}
	if d.dashGetExitStatus == nil {
		return -1, errors.New("dash_get_exitstatus not available")
	}

	results, err := d.dashGetExitStatus.Call(d.callCtx(ctx))
	if err != nil {
		return -1, err
	}
	return int(int32(results[0])), nil
}

// GetVar returns the value of a shell variable, or empty string if unset.
func (d *Dash) GetVar(ctx context.Context, name string) (string, error) {
	if !d.initialized {
		return "", errors.New("dash not initialized")
	}
	if d.dashGetVar == nil {
		return "", errors.New("dash_getvar not available")
	}

	ctx = d.callCtx(ctx)

	namePtr, err := d.allocString(ctx, name)
	if err != nil {
		return "", err
	}
	defer d.freePtr(ctx, namePtr)

	results, err := d.dashGetVar.Call(ctx, uint64(namePtr))
	if err != nil {
		return "", err
	}

	valPtr := uint32(results[0])
	if valPtr == 0 {
		return "", nil
	}

	return d.readCString(valPtr), nil
}

// SetVar sets a shell variable.
func (d *Dash) SetVar(ctx context.Context, name, value string) error {
	if !d.initialized {
		return errors.New("dash not initialized")
	}
	if d.dashSetVar == nil {
		return errors.New("dash_setvar not available")
	}

	ctx = d.callCtx(ctx)

	namePtr, err := d.allocString(ctx, name)
	if err != nil {
		return err
	}
	defer d.freePtr(ctx, namePtr)

	valPtr, err := d.allocString(ctx, value)
	if err != nil {
		return err
	}
	defer d.freePtr(ctx, valPtr)

	results, err := d.dashSetVar.Call(ctx, uint64(namePtr), uint64(valPtr))
	if err != nil {
		return err
	}
	if int32(results[0]) != 0 {
		return errors.New("dash_setvar failed")
	}
	return nil
}

// readCString reads a null-terminated string from WASM memory.
func (d *Dash) readCString(ptr uint32) string {
	mem := d.mod.Memory()
	var buf []byte
	for i := uint32(0); ; i++ {
		b, ok := mem.ReadByte(ptr + i)
		if !ok || b == 0 {
			break
		}
		buf = append(buf, b)
	}
	return string(buf)
}

// Close destroys the dash runtime and releases resources.
func (d *Dash) Close(ctx context.Context) error {
	if d.initialized {
		_, _ = d.dashDestroy.Call(d.callCtx(ctx))
		d.initialized = false
	}
	return d.mod.Close(ctx)
}

// setjmpHost implements setjmp via wazero snapshot.
//
// Takes a snapshot of the execution state and saves the C stack.
// Writes a checkpoint index to the jmp_buf in WASM memory.
// Returns 0 on first call. When longjmp restores this checkpoint,
// setjmp "returns" the longjmp value instead.
func setjmpHost(ctx context.Context, mod api.Module, bufPtr uint32) int32 {
	state := ctx.Value(dashStateKey{}).(*dashState)

	snap := experimental.GetSnapshotter(ctx).Snapshot()

	// Save C stack: memory from __stack_pointer to __heap_base.
	sp := uint32(mod.ExportedGlobal("__stack_pointer").Get())
	heapBase := uint32(mod.ExportedGlobal("__heap_base").Get())
	var cstack []byte
	if sp < heapBase {
		view, ok := mod.Memory().Read(sp, heapBase-sp)
		if ok {
			cstack = make([]byte, len(view))
			copy(cstack, view)
		}
	}

	idx := len(state.checkpoints)
	state.checkpoints = append(state.checkpoints, &checkpoint{
		snapshot:     snap,
		stackPointer: sp,
		cstack:       cstack,
	})

	// Write checkpoint index to jmp_buf (8 bytes little-endian).
	mod.Memory().WriteUint64Le(bufPtr, uint64(idx))

	return 0
}

// longjmpHost implements longjmp via wazero snapshot restore.
//
// Restores the C stack and execution state saved by a previous setjmp.
// The corresponding setjmp "returns" val. Does not return.
func longjmpHost(ctx context.Context, mod api.Module, bufPtr uint32, val int32) {
	state := ctx.Value(dashStateKey{}).(*dashState)

	idx, _ := mod.Memory().ReadUint64Le(bufPtr)

	// C standard: longjmp(buf, 0) behaves as longjmp(buf, 1).
	if val == 0 {
		val = 1
	}

	cp := state.checkpoints[idx]

	// Restore C stack: reset __stack_pointer and write back saved memory.
	mod.ExportedGlobal("__stack_pointer").(api.MutableGlobal).Set(uint64(cp.stackPointer))
	if len(cp.cstack) > 0 {
		mod.Memory().Write(cp.stackPointer, cp.cstack)
	}

	// Restore execution state. Makes the setjmp host function return val.
	cp.snapshot.Restore([]uint64{uint64(uint32(val))})
}
