package dashwasi

import "testing"

func TestDashWASMEmbedded(t *testing.T) {
	if len(DashWASM) == 0 {
		t.Fatal("DashWASM is empty")
	}
	// WASM magic number: \0asm
	if DashWASM[0] != 0x00 || DashWASM[1] != 0x61 || DashWASM[2] != 0x73 || DashWASM[3] != 0x6d {
		t.Fatal("DashWASM does not start with WASM magic number")
	}
	t.Logf("DashWASM size: %d bytes", len(DashWASM))
}
