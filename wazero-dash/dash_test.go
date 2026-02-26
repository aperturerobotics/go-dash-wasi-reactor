package dash

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tetratelabs/wazero"
)

func TestDashEval(t *testing.T) {
	ctx := context.Background()
	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	var stdout bytes.Buffer
	config := wazero.NewModuleConfig().
		WithStdout(&stdout).
		WithStderr(&stdout)

	d, err := NewDash(ctx, r, config)
	if err != nil {
		t.Fatal("NewDash:", err)
	}
	defer d.Close(ctx)

	if err := d.Init(ctx, nil); err != nil {
		t.Fatal("Init:", err)
	}

	// Simple echo.
	status, err := d.Eval(ctx, "echo hello")
	if err != nil {
		t.Fatal("Eval echo:", err)
	}
	if status != 0 {
		t.Fatalf("expected exit status 0, got %d", status)
	}
	if got := strings.TrimSpace(stdout.String()); got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}

	// Variable persistence.
	stdout.Reset()
	_, err = d.Eval(ctx, "FOO=bar")
	if err != nil {
		t.Fatal("Eval assignment:", err)
	}
	val, err := d.GetVar(ctx, "FOO")
	if err != nil {
		t.Fatal("GetVar:", err)
	}
	if val != "bar" {
		t.Fatalf("expected FOO=bar, got %q", val)
	}

	// SetVar from host.
	if err := d.SetVar(ctx, "HOST_VAR", "from_go"); err != nil {
		t.Fatal("SetVar:", err)
	}
	stdout.Reset()
	_, err = d.Eval(ctx, "echo $HOST_VAR")
	if err != nil {
		t.Fatal("Eval HOST_VAR:", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "from_go" {
		t.Fatalf("expected 'from_go', got %q", got)
	}

	// Exit status.
	status, err = d.Eval(ctx, "false")
	if err != nil {
		t.Fatal("Eval false:", err)
	}
	if status != 1 {
		t.Fatalf("expected exit status 1 for 'false', got %d", status)
	}

	es, err := d.GetExitStatus(ctx)
	if err != nil {
		t.Fatal("GetExitStatus:", err)
	}
	if es != 1 {
		t.Fatalf("expected GetExitStatus 1, got %d", es)
	}
}
