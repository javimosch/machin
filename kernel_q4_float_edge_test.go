package main

import "testing"

// These tests pin the documented numeric contracts of the int4 / float LLM-inference
// kernels (dot_q4, dot_f32, axpy_f32). The happy-path lines in mfl_test.go show each
// works once; dot_q8/dot_i8 already have dedicated edge tests (kernel_q8_edge_test.go)
// but the q4 and float kernels did not. These guard the guarantees those kernels are
// relied on for in a real decode loop:
//   - dot_q4 (split-nibble int4 weights) is numerically identical to dot_q8 fed the
//     same logical weights as full int8 — the halved-bytes packing must not change the
//     answer, and the exact nibble layout (byte k = w[k] low | w[k+gs/2] high, +8 bias)
//     must be honored,
//   - dot_f32 uses an fp32 accumulator and returns 0 for an empty count,
//   - axpy_f32 *accumulates* into y (y += s*x), respects the n bound, and is a no-op at
//     n=0.

// dot_q4 exists to halve the weight bytes moved in a memory-bound decode by packing two
// int4 weights per byte (see the dot_q4 guide entry). That packing is only safe if it
// returns the *same* value as the equivalent int8 (dot_q8) inner product. Here the same
// logical weights are fed to both kernels in one program — as split-nibble bytes to
// dot_q4 and as plain int8 to dot_q8 — and the outputs must be byte-identical.
//
// The weights are hand-packed to pin the exact layout dot_q4 decodes. With gs=4 (half=2)
// a group's two bytes hold, each value stored as value+8 in 0..15:
//
//	byte0 = (w0+8) | (w2+8)<<4 ,  byte1 = (w1+8) | (w3+8)<<4
//	group0 w=[3,-2,1,0]  -> byte0 = 11 | 9<<4  = 155 , byte1 = 6  | 8<<4  = 134
//	group1 w=[7,-8,-1,2] -> byte2 = 15 | 7<<4  = 127 , byte3 = 0  | 10<<4 = 160
//
// Activations xq (int8): group0 [1,2,3,4], group1 [-5,6,-7,8]. Scales are powers of two
// so every product is exact and the whole-number result ("-58") leaves no room for a
// formatting mismatch to hide a real divergence:
//
//	group0 int32 dot = 1*3+2*-2+3*1+4*0 = 2 ; xs=0.5 ws=2.0 -> 2*1.0  = 2
//	group1 int32 dot = -5*7+6*-8+-7*-1+8*2 = -60 ; xs=0.25 ws=4.0 -> -60*1.0 = -60
//	total = -58
func TestDotQ4MatchesDotQ8(t *testing.T) {
	// pi8 writes a signed byte (-128..127) into an unsigned-byte cell.
	pi8 := `func pi8(p, i, v) { if v < 0 { v = v + 256 } poke_u8(p, i, v) }`
	main := `func main() {
		xq := alloc(8)
		xs := alloc(8)  ws := alloc(8)
		wq4 := alloc(4)  wq8 := alloc(8)
		pi8(xq, 0, 1)  pi8(xq, 1, 2)   pi8(xq, 2, 3)   pi8(xq, 3, 4)
		pi8(xq, 4, -5) pi8(xq, 5, 6)   pi8(xq, 6, -7)  pi8(xq, 7, 8)
		poke_u8(wq4, 0, 155)  poke_u8(wq4, 1, 134)
		poke_u8(wq4, 2, 127)  poke_u8(wq4, 3, 160)
		pi8(wq8, 0, 3)  pi8(wq8, 1, -2)  pi8(wq8, 2, 1)   pi8(wq8, 3, 0)
		pi8(wq8, 4, 7)  pi8(wq8, 5, -8)  pi8(wq8, 6, -1)  pi8(wq8, 7, 2)
		poke_f32(xs, 0, 0.5)  poke_f32(xs, 4, 0.25)
		poke_f32(ws, 0, 2.0)  poke_f32(ws, 4, 4.0)
		q4 := dot_q4(xq, xs, wq4, ws, 8, 4)
		q8 := dot_q8(xq, xs, wq8, ws, 8, 4)
		println(str(q4) + " " + str(q8))
		free(xq) free(xs) free(ws) free(wq4) free(wq8)
	}`
	out, _ := buildRun(t, main, pi8)
	if out != "-58 -58\n" {
		t.Fatalf("dot_q4 vs dot_q8: got %q, want %q", out, "-58 -58\n")
	}
}

// dot_f32 is the vectorized fp32 inner product (attention q.k). Its contract is an fp32
// accumulator over a[k]*b[k] for k<n, and 0 for an empty count (an attention head can be
// asked for a zero-length span). Values are chosen so the products and their sum are
// exactly representable — 1.5*2 + -2*3 + 4*-0.5 = 3 - 6 - 2 = -5 — so a whole-number
// result pins the value with no float-formatting slack. The n=0 call must return 0.
func TestDotF32AccumulatesAndEmpty(t *testing.T) {
	main := `func main() {
		a := alloc(12)  b := alloc(12)
		poke_f32(a, 0, 1.5)  poke_f32(a, 4, -2.0)  poke_f32(a, 8, 4.0)
		poke_f32(b, 0, 2.0)  poke_f32(b, 4, 3.0)   poke_f32(b, 8, -0.5)
		println(str(dot_f32(a, b, 3)) + " " + str(dot_f32(a, b, 0)))
		free(a) free(b)
	}`
	out, _ := buildRun(t, main)
	if out != "-5 0\n" {
		t.Fatalf("dot_f32 accumulate/empty: got %q, want %q", out, "-5 0\n")
	}
}

// axpy_f32 is the attention value accumulation: y[k] += s*x[k] for k<n. Three things
// must hold and are each exercised here: it *accumulates* into the existing y (not
// overwrite), it stops at n (y[3] is left untouched), and n=0 is a no-op. Starting from
// y=[10,20,30,40], s=-2, x=[1,2,3,4], n=3 gives y=[8,16,24,40]; a following n=0 call with
// a different scale must change nothing.
func TestAxpyF32AccumulatesAndBounds(t *testing.T) {
	main := `func main() {
		y := alloc(16)  x := alloc(16)
		poke_f32(y, 0, 10.0)  poke_f32(y, 4, 20.0)  poke_f32(y, 8, 30.0)  poke_f32(y, 12, 40.0)
		poke_f32(x, 0, 1.0)   poke_f32(x, 4, 2.0)   poke_f32(x, 8, 3.0)   poke_f32(x, 12, 4.0)
		axpy_f32(y, -2.0, x, 3)
		axpy_f32(y, 9.0, x, 0)
		println(str(peek_f32(y, 0)) + " " + str(peek_f32(y, 4)) + " " + str(peek_f32(y, 8)) + " " + str(peek_f32(y, 12)))
		free(y) free(x)
	}`
	out, _ := buildRun(t, main)
	if out != "8 16 24 40\n" {
		t.Fatalf("axpy_f32 accumulate/bounds: got %q, want %q", out, "8 16 24 40\n")
	}
}
