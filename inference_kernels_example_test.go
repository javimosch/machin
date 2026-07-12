package main

import (
	"os"
	"strings"
	"testing"
)

// examples/complex/inference_kernels.mfl is the Tier-2 half of the inference
// north star (docs/NORTH-STAR-INFERENCE.md): the elementwise/reduction ops that
// wrap the int8 GEMV kernel into a transformer block — RMSNorm, SiLU, GELU,
// softmax, argmax — written in *pure MFL* over peek_f32/poke_f32 fp32 buffers,
// no new builtin. This test builds the committed example end-to-end (parse ->
// cc -> run) and pins its output, so the numeric contract of each kernel is
// locked and the example is proven to still compile and run.
//
// The reference values (activations are printed as int(round(v*1000)), i.e. in
// milli-units, to keep the pinned output free of float-formatting noise):
//
//	silu(x)    = x*sigmoid(x):    silu(-2)=-0.238, silu(0)=0, silu(2)=1.762
//	gelu(x)    tanh approx:       gelu(-1)=-0.159, gelu(0)=0, gelu(1)=0.841
//	rmsnorm([1,2,3], w=1):        ss=14/3=4.667, inv=1/sqrt(ss)=0.4629
//	                              -> [0.463, 0.926, 1.389]
//	softmax([1,2,3]):             [0.0900, 0.2447, 0.6652] (sums to 1.0)
//	argmax([1,2,3])            = 2
func TestInferenceKernelsExample(t *testing.T) {
	src, err := os.ReadFile("examples/complex/inference_kernels.mfl")
	if err != nil {
		t.Fatalf("read example: %v", err)
	}
	funcs, err := splitFunctions(string(src))
	if err != nil {
		t.Fatalf("split example into functions: %v", err)
	}

	out, _ := buildRun(t, funcs...)

	want := strings.Join([]string{
		"silu -238 0 1762",
		"gelu -159 0 841",
		"rmsnorm 463 926 1389",
		"softmax 90 245 665",
		"argmax 2",
		"",
	}, "\n")
	if out != want {
		t.Fatalf("inference_kernels.mfl output:\n got %q\nwant %q", out, want)
	}
}

// TestInferenceKernelsSoftmaxIsNormalized independently pins the defining
// property of softmax — the outputs form a probability distribution — rather
// than only the rounded per-element values above. It runs the same softmax
// kernel over a deliberately skewed logit vector (a large value that would
// overflow exp() without the max-subtraction stabilization the kernel does) and
// checks the milli-unit probabilities sum to 1000 and the argmax dominates.
func TestInferenceKernelsSoftmaxIsNormalized(t *testing.T) {
	// Reuse the library kernels verbatim from the example, with a fresh main
	// that stresses numerical stability (logit 50 => exp(50) overflows f32; the
	// kernel subtracts the row max first, so exp() sees <= 0 and stays finite).
	src, err := os.ReadFile("examples/complex/inference_kernels.mfl")
	if err != nil {
		t.Fatalf("read example: %v", err)
	}
	funcs, err := splitFunctions(string(src))
	if err != nil {
		t.Fatalf("split example into functions: %v", err)
	}
	// Drop the example's main(); supply our own driver over the same kernels.
	var kernels []string
	for _, f := range funcs {
		if strings.HasPrefix(strings.TrimSpace(f), "func main(") {
			continue
		}
		kernels = append(kernels, f)
	}
	driver := `func main(){n:=4 x:=alloc(n*4)out:=alloc(n*4)` +
		`load(x,[]float{0.0-1.0,3.0,50.0,2.0})softmax(x,out,n)` +
		`s:=0 i:=0 while i<n{s=s+mi(getf(out,i))i=i+1}` +
		`println("sum",str(s))println("argmax",str(argmax(x,n)))` +
		`println("p2",str(mi(getf(out,2))))free(x)free(out)}`
	kernels = append(kernels, driver)

	out, _ := buildRun(t, kernels...)

	// exp(50-50)=1 dominates the row; the other three logits are >=47 below the
	// max, so their probabilities round to 0 milli-units and index 2 gets ~1000.
	want := "sum 1000\nargmax 2\np2 1000\n"
	if out != want {
		t.Fatalf("stabilized softmax output:\n got %q\nwant %q", out, want)
	}
}
