package main

import (
	"fmt"
	"strings"
	"testing"
)

// The MTLM training kernels (attn_causal_fwd/bwd_f32, rmsnorm_fwd/bwd_f32,
// silu_mul(_bwd)_f32, softmax_xent_f32) are multithreaded C builtins that replace
// scalar MFL loops. Each test fills deterministic buffers, runs the builtin, runs a
// naive MFL port of the reference loops (copied from mtlm src/model.src), and pins
// max |diff| < 1e-5. Values are small multiples of 1/16 so fp32 rounding is tame.

// buildRun takes one function per source string.
const kernFillFn = `func kfill(p, n, seed) { i := 0  v := 0  while i < n { v = ((i*7 + seed*13) - (((i*7 + seed*13)/17)*17)) - 8  poke_f32(p, i*4, float(v)*0.0625)  i = i+1 } }`
const kernMaxdiffFn = `func kmaxdiff(a, b, n) (d) { d = 0.0  i := 0  while i < n { x := peek_f32(a, i*4) - peek_f32(b, i*4)  if x < 0.0 { x = 0.0 - x }  if x > d { d = x }  i = i+1 } }`

func attnProgram(B, T, dim, kvh, heads int) string {
	return fmt.Sprintf(`
func main() {
	B := %d  T := %d  dim := %d  heads := %d  kvh := %d
	hs := dim / heads  kvdim := dim * kvh / heads  bt := B*T  kvm := heads / kvh
	q := alloc(bt*dim*4)  k := alloc(bt*kvdim*4)  v := alloc(bt*kvdim*4)  dout := alloc(bt*dim*4)
	kfill(q, bt*dim, 1)  kfill(k, bt*kvdim, 2)  kfill(v, bt*kvdim, 3)  kfill(dout, bt*dim, 4)
	probs := alloc(B*heads*T*T*4)  out := alloc(bt*dim*4)
	probs2 := alloc(B*heads*T*T*4)  out2 := alloc(bt*dim*4)
	dq := alloc(bt*dim*4)  dk := alloc(bt*kvdim*4)  dv := alloc(bt*kvdim*4)
	dq2 := alloc(bt*dim*4)  dk2 := alloc(bt*kvdim*4)  dv2 := alloc(bt*kvdim*4)
	attn_causal_fwd_f32(q, k, v, probs, out, B, T, dim, kvdim, heads, kvh)
	attn_causal_bwd_f32(q, k, v, probs, dout, dq, dk, dv, B, T, dim, kvdim, heads, kvh)
	// ---- naive reference (mtlm m_attn_fwd_chunk / m_attn_back_chunk) ----
	inv := 1.0 / sqrt(float(hs))
	dsc := alloc(T*4)
	idx := 0
	while idx < B*heads {
		b := idx / heads  h := idx - b*heads  kr := (h / kvm) * hs
		t := 0
		while t < T {
			sr := (b*heads+h)*T + t  qr := (b*T+t)*dim + h*hs  mx := -1e30  s := 0
			while s <= t {
				kr2 := (b*T+s)*kvdim + kr  dot := 0.0  j := 0
				while j < hs { dot = dot + peek_f32(q, (qr+j)*4) * peek_f32(k, (kr2+j)*4)  j = j+1 }
				dot = dot * inv  poke_f32(probs2, (sr*T+s)*4, dot)  if dot > mx { mx = dot }  s = s+1
			}
			sum := 0.0  s = 0
			while s <= t { e := exp(peek_f32(probs2, (sr*T+s)*4) - mx)  poke_f32(probs2, (sr*T+s)*4, e)  sum = sum + e  s = s+1 }
			s = 0
			while s <= t { poke_f32(probs2, (sr*T+s)*4, peek_f32(probs2, (sr*T+s)*4) / sum)  s = s+1 }
			j := 0  while j < hs { poke_f32(out2, (qr+j)*4, 0.0)  j = j+1 }
			s = 0
			while s <= t {
				vr := (b*T+s)*kvdim + kr  a := peek_f32(probs2, (sr*T+s)*4)  j = 0
				while j < hs { poke_f32(out2, (qr+j)*4, peek_f32(out2, (qr+j)*4) + a*peek_f32(v, (vr+j)*4))  j = j+1 }
				s = s+1
			}
			// backward for this (b,h,t)
			s = 0
			while s <= t {
				vr := (b*T+s)*kvdim + kr  a := peek_f32(probs2, (sr*T+s)*4)  sm := 0.0  j = 0
				while j < hs {
					dov := peek_f32(dout, (qr+j)*4)
					poke_f32(dv2, (vr+j)*4, peek_f32(dv2, (vr+j)*4) + a*dov)
					sm = sm + dov * peek_f32(v, (vr+j)*4)  j = j+1
				}
				poke_f32(dsc, s*4, sm)  s = s+1
			}
			psum := 0.0  s = 0
			while s <= t { psum = psum + peek_f32(probs2, (sr*T+s)*4) * peek_f32(dsc, s*4)  s = s+1 }
			s = 0
			while s <= t { p := peek_f32(probs2, (sr*T+s)*4)  poke_f32(dsc, s*4, p * (peek_f32(dsc, s*4) - psum))  s = s+1 }
			s = 0
			while s <= t {
				kr2 := (b*T+s)*kvdim + kr  ds := peek_f32(dsc, s*4) * inv  j = 0
				while j < hs {
					poke_f32(dq2, (qr+j)*4, peek_f32(dq2, (qr+j)*4) + ds*peek_f32(k, (kr2+j)*4))
					poke_f32(dk2, (kr2+j)*4, peek_f32(dk2, (kr2+j)*4) + ds*peek_f32(q, (qr+j)*4))
					j = j+1
				}
				s = s+1
			}
			t = t+1
		}
		idx = idx+1
	}
	d1 := kmaxdiff(out, out2, bt*dim)  d2 := kmaxdiff(dq, dq2, bt*dim)  d3 := kmaxdiff(dk, dk2, bt*kvdim)  d4 := kmaxdiff(dv, dv2, bt*kvdim)
	if d1 < 0.00001 && d2 < 0.00001 && d3 < 0.00001 && d4 < 0.00001 { println("PASS") } else { println("FAIL out=" + str(d1) + " dq=" + str(d2) + " dk=" + str(d3) + " dv=" + str(d4)) }
}
`, B, T, dim, heads, kvh)
}

func TestAttnCausalFwdBwdF32(t *testing.T) {
	cases := []struct{ B, T, dim, kvh, heads int }{{2, 8, 16, 1, 2}, {1, 5, 12, 3, 3}, {3, 33, 32, 2, 4}}
	for _, c := range cases {
		name := fmt.Sprintf("B%dT%ddim%dh%dkv%d", c.B, c.T, c.dim, c.heads, c.kvh)
		t.Run(name, func(t *testing.T) {
			out, _ := buildRun(t, kernFillFn, kernMaxdiffFn, attnProgram(c.B, c.T, c.dim, c.kvh, c.heads))
			if strings.TrimSpace(out) != "PASS" {
				t.Fatalf("attention %s: got %q", name, out)
			}
		})
	}
}

func TestRmsnormSiluSoftmaxF32(t *testing.T) {
	prog := `func main() {
	n := 37  rows := 53  eps := 0.00001
	x := alloc(rows*n*4)  w := alloc(n*4)  dout := alloc(rows*n*4)
	kfill(x, rows*n, 5)  kfill(w, n, 6)  kfill(dout, rows*n, 7)
	i := 0  while i < n { poke_f32(w, i*4, peek_f32(w, i*4) + 1.0)  i = i+1 }
	out := alloc(rows*n*4)  nm := alloc(rows*n*4)  out2 := alloc(rows*n*4)  nm2 := alloc(rows*n*4)
	dx := alloc(rows*n*4)  dw := alloc(n*4)  dx2 := alloc(rows*n*4)  dw2 := alloc(n*4)
	kfill(dx, rows*n, 8)  kfill(dx2, rows*n, 8)   // dx accumulates onto a residual grad
	rmsnorm_fwd_f32(out, x, w, nm, n, rows, eps)
	rmsnorm_bwd_f32(dx, dout, w, x, nm, dw, n, rows, eps)
	r := 0
	while r < rows {
		xo := r*n  ss := 0.0  i = 0
		while i < n { v := peek_f32(x, (xo+i)*4)  ss = ss + v*v  i = i+1 }
		rms := 1.0 / sqrt(ss / float(n) + eps)
		i = 0
		while i < n { nv := peek_f32(x, (xo+i)*4) * rms  poke_f32(nm2, (xo+i)*4, nv)  poke_f32(out2, (xo+i)*4, peek_f32(w, i*4) * nv)  i = i+1 }
		i = 0
		while i < n { poke_f32(dw2, i*4, peek_f32(dw2, i*4) + peek_f32(dout, (xo+i)*4)*peek_f32(nm2, (xo+i)*4))  i = i+1 }
		gm := 0.0  i = 0
		while i < n { gm = gm + peek_f32(dout, (xo+i)*4)*peek_f32(w, i*4)*peek_f32(nm2, (xo+i)*4)  i = i+1 }
		gm = gm / float(n)  i = 0
		while i < n { dn := peek_f32(dout, (xo+i)*4) * peek_f32(w, i*4)  poke_f32(dx2, (xo+i)*4, peek_f32(dx2, (xo+i)*4) + rms * (dn - gm * peek_f32(nm2, (xo+i)*4)))  i = i+1 }
		r = r+1
	}
	e1 := kmaxdiff(out, out2, rows*n)  e2 := kmaxdiff(nm, nm2, rows*n)  e3 := kmaxdiff(dx, dx2, rows*n)  e4 := kmaxdiff(dw, dw2, n)
	// silu_mul
	m := 3001
	h1 := alloc(m*4)  h3 := alloc(m*4)  g := alloc(m*4)  g2 := alloc(m*4)  dg := alloc(m*4)
	dh1 := alloc(m*4)  dh3 := alloc(m*4)  dh1b := alloc(m*4)  dh3b := alloc(m*4)
	kfill(h1, m, 9)  kfill(h3, m, 10)  kfill(dg, m, 11)
	silu_mul_f32(g, h1, h3, m)
	silu_mul_bwd_f32(dh1, dh3, dg, h1, h3, m)
	i = 0
	while i < m {
		xv := peek_f32(h1, i*4)  sg := 1.0 / (1.0 + exp(0.0 - xv))  sl := xv * sg
		poke_f32(g2, i*4, sl * peek_f32(h3, i*4))
		poke_f32(dh1b, i*4, peek_f32(dg, i*4) * peek_f32(h3, i*4) * (sg + xv*sg*(1.0-sg)))
		poke_f32(dh3b, i*4, peek_f32(dg, i*4) * sl)
		i = i+1
	}
	e5 := kmaxdiff(g, g2, m)  e6 := kmaxdiff(dh1, dh1b, m)  e7 := kmaxdiff(dh3, dh3b, m)
	// softmax_xent
	rw := 7  vocab := 33
	lg := alloc(rw*vocab*4)  tg := alloc(rw*4)  pr := alloc(rw*vocab*4)  dl := alloc(rw*vocab*4)  pr2 := alloc(rw*vocab*4)  dl2 := alloc(rw*vocab*4)
	kfill(lg, rw*vocab, 12)
	r = 0  while r < rw { poke_i32(tg, r*4, (r*5) - (((r*5)/vocab)*vocab))  r = r+1 }
	loss := softmax_xent_f32(lg, tg, pr, dl, rw, vocab)
	loss2 := 0.0  r = 0
	while r < rw {
		lo := r*vocab  mx := peek_f32(lg, lo*4)  c := 1
		while c < vocab { v := peek_f32(lg, (lo+c)*4)  if v > mx { mx = v }  c = c+1 }
		sum := 0.0  c = 0
		while c < vocab { e := exp(peek_f32(lg, (lo+c)*4) - mx)  poke_f32(pr2, (lo+c)*4, e)  sum = sum + e  c = c+1 }
		c = 0
		while c < vocab { poke_f32(pr2, (lo+c)*4, peek_f32(pr2, (lo+c)*4) / sum)  c = c+1 }
		tid := peek_i32(tg, r*4)  p := peek_f32(pr2, (lo+tid)*4)  loss2 = loss2 - log(p)
		c = 0
		while c < vocab { oh := 0.0  if c == tid { oh = 1.0 }  poke_f32(dl2, (lo+c)*4, (peek_f32(pr2, (lo+c)*4) - oh) / float(rw))  c = c+1 }
		r = r+1
	}
	loss2 = loss2 / float(rw)
	e8 := kmaxdiff(pr, pr2, rw*vocab)  e9 := kmaxdiff(dl, dl2, rw*vocab)  e10 := loss - loss2  if e10 < 0.0 { e10 = 0.0 - e10 }
	tol := 0.00001
	if e1 < tol && e2 < tol && e3 < tol && e4 < 0.0001 && e5 < tol && e6 < tol && e7 < tol && e8 < tol && e9 < tol && e10 < tol { println("PASS") } else {
		println("FAIL rms_out=" + str(e1) + " normed=" + str(e2) + " dx=" + str(e3) + " dw=" + str(e4) + " silu=" + str(e5) + " dh1=" + str(e6) + " dh3=" + str(e7) + " probs=" + str(e8) + " dlogits=" + str(e9) + " loss=" + str(e10))
	}
}
`
	out, _ := buildRun(t, kernFillFn, kernMaxdiffFn, prog)
	if strings.TrimSpace(out) != "PASS" {
		t.Fatalf("rmsnorm/silu/softmax kernels: got %q", out)
	}
}
