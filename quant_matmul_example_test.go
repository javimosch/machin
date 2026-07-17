package main

import "testing"

// The examples/complex/quant_matmul.mfl demo computes a Q8_0 quantized
// matrix-vector product y = W . x with dot_q8: the activation vector and each
// weight row are int8 codes with one fp32 scale per group of gs elements, laid
// out contiguously and addressed by pointer arithmetic (as a real GGUF
// checkpoint is memory-mapped). This pins the demo's numeric output so the
// shipped example — and the dot_q8 kernel it exercises — cannot silently drift.
//
// Hand-check (gs=2 -> two groups per row; x codes [2,3,1,-2], scales [.5,.25]):
//   row0 w=[4,5,3,1] s=[2,4]: (2*4+3*5)*.5*2 + (1*3-2*1)*.25*4 = 23 + 1  = 24
//   row1 w=[4,5,3,1] s=[4,8]: (23)*.5*4       + (1)*.25*8              = 46 + 2  = 48
//   row2 w=[1,0,0,0] s=[2,2]: (2*1)*.5*2      + 0                      = 2  + 0  = 2
//   sum = 74
func TestQuantMatmulExample(t *testing.T) {
	prog, err := loadMFL("examples/complex/quant_matmul.mfl")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	out, err := RunCaptured(prog)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "y[0] = 24\ny[1] = 48\ny[2] = 2\nsum = 74\n"
	if out != want {
		t.Fatalf("quant_matmul example output:\n got %q\nwant %q", out, want)
	}
}
