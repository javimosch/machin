package main

import (
	"fmt"
	"testing"
)

// gemm_f32 is the multithreaded, cache-blocked fp32 GEMM for the MTLM trainer:
//   C[m×n] (+)= op(A)[m×k] @ op(B)[k×n], row-major,
//   ta=1 -> A stored [k×m] (used transposed), tb=1 -> B stored [n×k],
//   accumulate=1 adds into C, else overwrites. These tests pin the contract:
//
//   - all four (ta,tb) transpose combos produce the same answer as a naive
//     MFL triple loop (the indexing/transpose logic is correct), on odd sizes
//     that exercise the cache-blocked MC/KC/NC tiling and the 4×8 micro-kernel's
//     edge handling (m/n/k not multiples of MR/NR/MC/KC/NC),
//   - accumulate=0 overwrites and accumulate=1 adds (C = R then C = 2R),
//   - empty dims (m=0, n=0, k=0) are no-ops (C untouched).
//
// Values are ((idx%17)-8)*0.0625 — multiples of 1/16 in [-0.5,0.5], all exactly
// representable in fp32, so products and (for k≤768) the k-length sums are exact.
// That makes the naive reference and the AVX2/FMA GEMM agree bit-for-bit (fma and
// mul+add of exact values round identically), so the 1e-4 relative tolerance is
// comfortable headroom rather than a rounding budget. The accumulation ORDER is
// in fact the same (k ascending), so this is a strict correctness pin.

// gemmProgram builds the MFL program for one (ta,tb,m,n,k) case. The op(A)/op(B)
// offset formulas are baked in per ta/tb so the naive reference inner loop has no
// per-iteration branching.
func gemmProgram(m, n, k, ta, tb int) string {
	aOff := "i*k+p" // ta=0: A is [m,k] row-major
	if ta == 1 {
		aOff = "p*m+i" // A stored [k,m], op(A)[i][p] = A[p*m+i]
	}
	bOff := "p*n+j" // tb=0: B is [k,n] row-major
	if tb == 1 {
		bOff = "j*k+p" // B stored [n,k], op(B)[p][j] = B[j*k+p]
	}
	return fmt.Sprintf(`func main() {
	m := %d n := %d k := %d
	A := alloc(m*k*4) B := alloc(k*n*4) C := alloc(m*n*4) Cref := alloc(m*n*4)
	i := 0 j := 0 p := 0 v := 0
	acc := 0.0 av := 0.0 bv := 0.0 cv := 0.0 rv := 0.0 d := 0.0 den := 0.0 r := 0.0
	maxrel := 0.0 maxrel2 := 0.0
	i = 0
	while i < m*k { v = (i-(i/17)*17)-8 poke_f32(A, i*4, float(v)*0.0625) i = i+1 }
	i = 0
	while i < k*n { v = (i-(i/17)*17)-8 poke_f32(B, i*4, float(v)*0.0625) i = i+1 }
	gemm_f32(C, A, B, m, n, k, %d, %d, 0)
	i = 0
	while i < m {
		j = 0
		while j < n {
			acc = 0.0 p = 0
			while p < k {
				av = peek_f32(A, (%s)*4) bv = peek_f32(B, (%s)*4)
				acc = acc+av*bv p = p+1
			}
			poke_f32(Cref, (i*n+j)*4, acc) j = j+1
		}
		i = i+1
	}
	i = 0
	while i < m*n {
		cv = peek_f32(C, i*4) rv = peek_f32(Cref, i*4)
		d = cv-rv if d < 0 { d = -d }
		den = rv if den < 0 { den = -den } if den < 1.0 { den = 1.0 }
		r = d/den if r > maxrel { maxrel = r }
		i = i+1
	}
	gemm_f32(C, A, B, m, n, k, %d, %d, 1)
	i = 0
	while i < m*n {
		cv = peek_f32(C, i*4) rv = peek_f32(Cref, i*4)
		d = cv-rv*2.0 if d < 0 { d = -d }
		den = rv*2.0 if den < 0 { den = -den } if den < 1.0 { den = 1.0 }
		r = d/den if r > maxrel2 { maxrel2 = r }
		i = i+1
	}
	if maxrel < 0.0001 { if maxrel2 < 0.0001 { println("PASS") } else { println("FAIL-ACC") } } else { println("FAIL") }
	free(A) free(B) free(C) free(Cref)
}`, m, n, k, ta, tb, aOff, bOff, ta, tb)
}

// TestGemmF32AllTransposeCombos runs every (ta,tb) × accumulate combination against
// a naive MFL reference on three odd sizes — tiny (7×13×5), medium (33×65×17), and
// a training-shaped 300×288×768 — and requires the GEMM to match within 1e-4
// relative. The large size is the one that exercises multithreading (m*n*k=66M ≫
// the 64k single-thread threshold) and the full MC/KC/NC block nest.
func TestGemmF32AllTransposeCombos(t *testing.T) {
	sizes := [][3]int{{7, 13, 5}, {33, 65, 17}, {300, 288, 768}}
	for _, sz := range sizes {
		m, n, k := sz[0], sz[1], sz[2]
		for ta := 0; ta <= 1; ta++ {
			for tb := 0; tb <= 1; tb++ {
				name := fmt.Sprintf("m%dn%dk%d_ta%d_tb%d", m, n, k, ta, tb)
				t.Run(name, func(t *testing.T) {
					out, _ := buildRun(t, gemmProgram(m, n, k, ta, tb))
					if out != "PASS\n" {
						t.Fatalf("gemm_f32 %s: got %q, want PASS", name, out)
					}
				})
			}
		}
	}
}

// TestGemmF32EmptyDimsNoOp confirms an empty dimension (m=0, n=0, or k=0) is a
// no-op: a sentinel pre-written into C is left untouched. The C buffer is one
// float; gemm_f32 must return immediately without writing it.
func TestGemmF32EmptyDimsNoOp(t *testing.T) {
	cases := []struct{ m, n, k int }{
		{0, 4, 4}, {4, 0, 4}, {4, 4, 0},
	}
	for _, c := range cases {
		name := fmt.Sprintf("m%dn%dk%d", c.m, c.n, c.k)
		t.Run(name, func(t *testing.T) {
			main := fmt.Sprintf(`func main() {
	A := alloc(16) B := alloc(16) C := alloc(16)
	poke_f32(A, 0, 1.0) poke_f32(A, 4, 2.0) poke_f32(A, 8, 3.0) poke_f32(A, 12, 4.0)
	poke_f32(B, 0, 5.0) poke_f32(B, 4, 6.0) poke_f32(B, 8, 7.0) poke_f32(B, 12, 8.0)
	poke_f32(C, 0, 123.0)
	gemm_f32(C, A, B, %d, %d, %d, 0, 0, 0)
	println(str(peek_f32(C, 0)))
	free(A) free(B) free(C)
}`, c.m, c.n, c.k)
			out, _ := buildRun(t, main)
			if out != "123\n" {
				t.Fatalf("gemm_f32 empty %s: C touched, got %q, want 123 (sentinel)", name, out)
			}
		})
	}
}
