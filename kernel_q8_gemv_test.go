package main

import "testing"

// Q8_0 GEMV/GEMM integration: compose the kernel builtins (alloc/poke_u8/
// poke_f32/dot_q8/dot_i8/peek_f32) into the operation an LLM decode step
// actually runs — a quantized weight matrix times one or more quantized
// activation vectors. Two things are pinned at once:
//
//  1. Hand-computed correctness. A 3x4 int8 weight matrix (group size 2, so two
//     per-group fp32 scales per row) is multiplied by two int8 activation
//     vectors. Every product is worked out by hand in the comment block below,
//     so the printed output "7 -10 1" / "6 18 3" is an external oracle, not a
//     value captured from a prior run.
//
//  2. Fused == manual equivalence. dot_q8 is the fused form of the per-group
//     `dot_i8 + two peek_f32` loop it was introduced to replace (#435 -> #439).
//     The runtime does both for every (row, activation) pair and asserts bitwise
//     equality: the group int32 reduction and the `acc * xscale * wscale`
//     product order are identical between the C kernel and the MFL composition,
//     so the two must agree exactly, not merely approximately. If they ever
//     diverge (e.g. a codegen change reorders the scale multiply) the program
//     prints FUSED!=MANUAL and the test fails.
//
// This complements the per-kernel numeric-contract unit tests: those pin a
// single call's output; this pins the whole matmul inner loop the kernels exist
// to serve, plus weight reuse across a batch (the GEMM path).
//
// Weights (row-major, int8), group scales ws (fp32, two per row):
//   row0 = [ 1, 1, 1, 1]  ws=[1.0, 1.0]
//   row1 = [ 2, 0,-1, 4]  ws=[0.5, 2.0]
//   row2 = [-2, 3, 5,-1]  ws=[2.0, 0.5]
//
// Activation batch (int8) xq, group scales xs (fp32, two per vector):
//   vec0 = [ 2,-1, 3, 0]  xs=[1.0, 2.0]
//   vec1 = [ 1, 1, 0, 2]  xs=[2.0, 1.0]
//
// Per group g the contribution is (sum_k xq*wq over the group) * xs[g] * ws[g].
// vec0:
//   row0: (2*1 + -1*1)*1*1 + (3*1 + 0*1)*2*1     =   1 +  6 =   7
//   row1: (2*2 + -1*0)*1*0.5 + (3*-1 + 0*4)*2*2  =   2 + -12 = -10
//   row2: (2*-2 + -1*3)*1*2 + (3*5 + 0*-1)*2*0.5 = -14 + 15 =   1   -> "7 -10 1"
// vec1:
//   row0: (1*1 + 1*1)*2*1 + (0*1 + 2*1)*1*1      =   4 +  2 =   6
//   row1: (1*2 + 1*0)*2*0.5 + (0*-1 + 2*4)*1*2   =   2 + 16 =  18
//   row2: (1*-2 + 1*3)*2*2 + (0*5 + 2*-1)*1*0.5  =   4 + -1 =   3   -> "6 18 3"
func TestKernelQ8GemvFusedEqualsManual(t *testing.T) {
	// One row of the manual (unfused) inner product: for each length-gs group,
	// a signed int8 dot times the two per-group fp32 scales, accumulated in a
	// double. This is exactly what dot_q8 collapses into one call.
	manual := `func gemv_manual_row(xq, xs, wq, ws, n, gs) (acc) {
	acc = 0.0
	ng := n / gs
	g := 0
	while g < ng {
		d := dot_i8(xq + g * gs, wq + g * gs, gs)
		acc = acc + float(d) * peek_f32(xs, g * 4) * peek_f32(ws, g * 4)
		g = g + 1
	}
}`

	main := `func main() {
	n := 4
	gs := 2
	rows := 3
	batch := 2
	ng := n / gs

	// weight matrix (rows*n int8) and its group scales (rows*ng fp32).
	wq := alloc(rows * n)
	ws := alloc(rows * ng * 4)
	poke_u8(wq, 0, 1)   poke_u8(wq, 1, 1)   poke_u8(wq, 2, 1)   poke_u8(wq, 3, 1)
	poke_u8(wq, 4, 2)   poke_u8(wq, 5, 0)   poke_u8(wq, 6, 255) poke_u8(wq, 7, 4)
	poke_u8(wq, 8, 254) poke_u8(wq, 9, 3)   poke_u8(wq, 10, 5)  poke_u8(wq, 11, 255)
	poke_f32(ws, 0, 1.0)  poke_f32(ws, 4, 1.0)
	poke_f32(ws, 8, 0.5)  poke_f32(ws, 12, 2.0)
	poke_f32(ws, 16, 2.0) poke_f32(ws, 20, 0.5)

	// activation batch (batch*n int8) and its group scales (batch*ng fp32).
	xq := alloc(batch * n)
	xs := alloc(batch * ng * 4)
	poke_u8(xq, 0, 2) poke_u8(xq, 1, 255) poke_u8(xq, 2, 3) poke_u8(xq, 3, 0)
	poke_u8(xq, 4, 1) poke_u8(xq, 5, 1)   poke_u8(xq, 6, 0) poke_u8(xq, 7, 2)
	poke_f32(xs, 0, 1.0)  poke_f32(xs, 4, 2.0)
	poke_f32(xs, 8, 2.0)  poke_f32(xs, 12, 1.0)

	ok := true
	b := 0
	while b < batch {
		xrow := xq + b * n
		xsrow := xs + b * ng * 4
		line := ""
		r := 0
		while r < rows {
			wrow := wq + r * n
			wsrow := ws + r * ng * 4
			fused := dot_q8(xrow, xsrow, wrow, wsrow, n, gs)
			ref := gemv_manual_row(xrow, xsrow, wrow, wsrow, n, gs)
			if fused != ref { ok = false }
			line = line + str(fused)
			if r < rows - 1 { line = line + " " }
			r = r + 1
		}
		println(line)
		b = b + 1
	}
	if ok { println("OK") } else { println("FUSED!=MANUAL") }

	free(wq) free(ws) free(xq) free(xs)
}`

	out, _ := buildRun(t, main, manual)
	const want = "7 -10 1\n6 18 3\nOK\n"
	if out != want {
		t.Fatalf("Q8 GEMV: got %q, want %q", out, want)
	}
}
