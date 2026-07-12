package main

import "testing"

// These tests pin the documented numeric contracts of the int8 / Q8_0 quantized
// LLM-inference kernels (peek_i8/peek_u8, dot_i8, dot_q8). The existing happy-path
// tests in mfl_test.go show each kernel works once; these guard the guarantees the
// kernels are actually relied on for in production matmul (machin-colibri):
//   - dot_q8 is numerically identical to the dot_i8 + peek_f32 loop it replaced,
//   - dot_i8's 32-bit accumulator stays exact well past what int8/int16 would hold,
//   - peek_i8/peek_u8 agree at the exact sign boundary (0x80, 0xFF).

// dot_q8 exists purely to collapse a per-group `dot_i8 + two peek_f32` MFL loop
// into one vectorized C call (see #439). That refactor is only safe if the fused
// kernel returns the *same* value as the loop. Here we compute a multi-group,
// mixed-sign inner product both ways in the same program and require byte-identical
// output. Scales are powers of two and the group dots are small, so every product
// is exactly representable and the result is a whole float ("-20"), leaving no room
// for a formatting mismatch to mask a real divergence.
//
//   group0: xq=[1,2,-3,4] wq=[2,1,1,-1] dot=-3 ; xs=0.5  ws=2.0 -> -3*1.0  = -3
//   group1: xq=[5,-6,7,8] wq=[1,1,0,-2] dot=-17; xs=0.25 ws=4.0 -> -17*1.0 = -17
//   total = -20
func TestDotQ8MatchesManualLoop(t *testing.T) {
	// pi8 writes a signed byte (-128..127) into an unsigned-byte cell.
	pi8 := `func pi8(p, i, v) { if v < 0 { v = v + 256 } poke_u8(p, i, v) }`
	// manual is the exact loop dot_q8 replaces: per-group int8 dot times the two
	// per-group fp32 scales, accumulated left-to-right in the same order dot_q8 uses.
	manual := `func manual(xq, xs, wq, ws, n, gs) (acc) {
		acc = 0.0
		g := 0
		off := 0
		for off < n {
			gd := dot_i8(xq + off, wq + off, gs)
			acc = acc + float(gd) * peek_f32(xs, g * 4) * peek_f32(ws, g * 4)
			off = off + gs
			g = g + 1
		}
	}`
	main := `func main() {
		xq := alloc(8)  wq := alloc(8)
		xs := alloc(8)  ws := alloc(8)
		pi8(xq, 0, 1)  pi8(xq, 1, 2)   pi8(xq, 2, -3)  pi8(xq, 3, 4)
		pi8(wq, 0, 2)  pi8(wq, 1, 1)   pi8(wq, 2, 1)   pi8(wq, 3, -1)
		pi8(xq, 4, 5)  pi8(xq, 5, -6)  pi8(xq, 6, 7)   pi8(xq, 7, 8)
		pi8(wq, 4, 1)  pi8(wq, 5, 1)   pi8(wq, 6, 0)   pi8(wq, 7, -2)
		poke_f32(xs, 0, 0.5)  poke_f32(xs, 4, 0.25)
		poke_f32(ws, 0, 2.0)  poke_f32(ws, 4, 4.0)
		fused := dot_q8(xq, xs, wq, ws, 8, 4)
		loop := manual(xq, xs, wq, ws, 8, 4)
		println(str(fused) + " " + str(loop))
		free(xq) free(wq) free(xs) free(ws)
	}`
	out, _ := buildRun(t, main, pi8, manual)
	if out != "-20 -20\n" {
		t.Fatalf("dot_q8 vs manual loop: got %q, want %q", out, "-20 -20\n")
	}
}

// dot_i8's contract is a 32-bit accumulator: exact while |sum| < 2^31 for any count
// (the guide quotes ~133k i8*i8 terms). A naive int8/int16 accumulator would wrap
// long before that. 100000 terms of 127*127 = 16129 sum to 1,612,900,000, which is
// well under 2^31 and must come back exact. A zero count is the empty-group edge and
// must be 0 (an LLM matmul tail group can legitimately be empty).
func TestDotI8AccumulatorExactAndEmpty(t *testing.T) {
	main := `func main() {
		n := 100000
		a := alloc(n)  b := alloc(n)
		i := 0
		for i < n {
			poke_u8(a, i, 127)
			poke_u8(b, i, 127)
			i = i + 1
		}
		println(str(dot_i8(a, b, n)))
		println(str(dot_i8(a, b, 0)))
		free(a) free(b)
	}`
	out, _ := buildRun(t, main)
	if out != "1612900000\n0\n" {
		t.Fatalf("dot_i8 accumulator/empty: got %q, want %q", out, "1612900000\n0\n")
	}
}

// peek_i8 sign-extends, peek_u8 zero-extends. The one byte that separates the two is
// the sign bit: 0x80 must read as -128 (i8) / 128 (u8) and 0xFF as -1 / 255, while
// 0x7F (127) and 0x00 read identically. Quantized weights use the full int8 range,
// so this boundary is exercised on real data.
func TestPeekI8SignBoundary(t *testing.T) {
	main := `func main() {
		p := alloc(4)
		poke_u8(p, 0, 128)
		poke_u8(p, 1, 255)
		poke_u8(p, 2, 127)
		poke_u8(p, 3, 0)
		println(str(peek_i8(p, 0)) + " " + str(peek_i8(p, 1)) + " " + str(peek_i8(p, 2)) + " " + str(peek_i8(p, 3)))
		println(str(peek_u8(p, 0)) + " " + str(peek_u8(p, 1)) + " " + str(peek_u8(p, 2)) + " " + str(peek_u8(p, 3)))
		free(p)
	}`
	out, _ := buildRun(t, main)
	want := "-128 -1 127 0\n128 255 127 0\n"
	if out != want {
		t.Fatalf("peek_i8/u8 sign boundary: got %q, want %q", out, want)
	}
}
