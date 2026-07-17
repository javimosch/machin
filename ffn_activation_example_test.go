package main

import (
	"strings"
	"testing"
)

// TestFFNActivationExample builds and runs examples/complex/ffn_activation.mfl and
// pins its output. The example is the pure-MFL feed-forward activation block of a
// transformer — the SiLU gate, a tanh-form GELU, and the SwiGLU gate*up Hadamard
// product — the elementwise glue between the (quantized) FFN matmuls. Pinning the
// numbers guards both that the example keeps compiling and that the float math
// stays correct:
//   - SiLU(0)=0 and SiLU is bounded-negative for x<0,
//   - GELU(1)≈0.841 matches the standard tanh approximation,
//   - SwiGLU(gate,up) = silu(gate) elementwise-times up.
func TestFFNActivationExample(t *testing.T) {
	prog, err := loadMFL("examples/complex/ffn_activation.mfl")
	if err != nil {
		t.Fatalf("load example: %v", err)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	for _, want := range []string{
		"silu(-2): -0.238",
		"silu(-1): -0.269",
		"silu(0): 0",
		"silu(1): 0.731",
		"silu(2): 1.762",
		"gelu(-1): -0.159",
		"gelu(0): 0",
		"gelu(1): 0.841",
		"gelu(2): 1.955",
		"swiglu:  1.462  0.881  0  -1.076",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
