package main

import (
	"strings"
	"testing"
)

// dot_q8 is the Q8_0 matmul kernel of the inference north-star: it dequantizes
// two grouped int8 vectors on the fly (each group carries one f32 scale) and
// accumulates their dot product. The edge/GEMV tests pin its raw numeric
// contract; this test pins the property that actually matters downstream — that
// a full quantize -> dot_q8 pipeline stays faithful to the original f32 dot.
//
// The MFL side quantizes two f32 vectors into Q8_0 form the standard way
// (per-group scale = max(|x|)/127, quant = round(x/scale)), runs dot_q8, and
// checks the relative error against the exact f32 dot computed from the same
// data. A positive DC bias keeps |true dot| well away from zero so cancellation
// can't inflate the ratio: real Q8_0 error here is a fraction of a percent, so
// the 2% gate is a wide, platform-stable margin.
func TestDotQ8QuantizationFidelity(t *testing.T) {
	const maxabs = `
func maxabs(x, base, gs) {
	m := 0.0
	k := 0
	for k < gs {
		v := abs(peek_f32(x, (base+k)*4))
		if v > m { m = v }
		k = k + 1
	}
	return m
}`

	// quantize x[0:n] into q (int8 quants) + sc (one f32 scale per group of gs).
	const quantize = `
func quantize(x, n, gs, q, sc) {
	ng := n / gs
	g := 0
	for g < ng {
		m := maxabs(x, g*gs, gs)
		scale := 0.0
		if m > 0.0 { scale = m / 127.0 }
		poke_f32(sc, g*4, scale)
		k := 0
		for k < gs {
			idx := g*gs + k
			qi := 0
			if scale > 0.0 { qi = int(round(peek_f32(x, idx*4) / scale)) }
			poke_u8(q, idx, qi)
			k = k + 1
		}
		g = g + 1
	}
}`

	// exact f32 dot, the reference the quantized kernel is measured against.
	const fdot = `
func fdot(a, b, n) {
	s := 0.0
	i := 0
	for i < n {
		s = s + peek_f32(a, i*4) * peek_f32(b, i*4)
		i = i + 1
	}
	return s
}`

	const mainF = `
func main() {
	n := 64
	gs := 32
	x := alloc(n*4)
	w := alloc(n*4)
	i := 0
	for i < n {
		poke_f32(x, i*4, sin(float(i)*0.3) + 2.5)
		poke_f32(w, i*4, cos(float(i)*0.17)*0.5 + 1.5)
		i = i + 1
	}
	tru := fdot(x, w, n)

	xq := alloc(n)
	wq := alloc(n)
	xs := alloc((n/gs)*4)
	ws := alloc((n/gs)*4)
	quantize(x, n, gs, xq, xs)
	quantize(w, n, gs, wq, ws)
	approx := dot_q8(xq, xs, wq, ws, n, gs)

	rel := abs(approx - tru) / abs(tru)
	if rel < 0.02 {
		println("fidelity=PASS")
	} else {
		println("fidelity=FAIL rel=" + str(rel))
	}

	// A fully-zero group makes the scale collapse to 0. The kernel must return
	// exactly 0 for it, never a NaN from a 0/0 dequant or a stray quant — this
	// is the dead-channel case that shows up in real weight matrices.
	zf := alloc(gs*4)
	j := 0
	for j < gs { poke_f32(zf, j*4, 0.0) j = j + 1 }
	zq := alloc(gs)
	zqw := alloc(gs)
	zsx := alloc(4)
	zsw := alloc(4)
	quantize(zf, gs, gs, zq, zsx)
	quantize(zf, gs, gs, zqw, zsw)
	zres := dot_q8(zq, zsx, zqw, zsw, gs, gs)
	if zres == 0.0 {
		println("zerogroup=PASS")
	} else {
		println("zerogroup=FAIL")
	}

	free(x) free(w) free(xq) free(wq) free(xs) free(ws)
	free(zf) free(zq) free(zqw) free(zsx) free(zsw)
}`

	out, _ := buildRun(t, maxabs, quantize, fdot, mainF)
	if !strings.Contains(out, "fidelity=PASS") {
		t.Fatalf("Q8_0 quantized dot drifted from the f32 reference:\n%s", out)
	}
	if !strings.Contains(out, "zerogroup=PASS") {
		t.Fatalf("zero-scale group did not yield exactly 0:\n%s", out)
	}
}
