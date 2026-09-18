package main

import (
	"os"
	"testing"
)

// Benchmark for gemm_f32, guarded by MFL_GEMM_BENCH=1 so it never runs in the
// normal `go test` suite. It builds one MFL program that times each shape with
// now_ms() around a timed loop (a warmup call first, so the persistent pthread
// pool's one-time creation is excluded), and prints GFLOP/s = 2·m·n·k·iters /
// (elapsed_ms) / 1e6. Run:  MFL_GEMM_BENCH=1 go test -run TestGemmF32Bench -v
//
// Shapes: 512³ and 1024³ (square), and the three MTLM training shapes
// (8192×288×768 forward, 8192×768×288 backward dX=dY@W, and 288×768×8192 with
// ta=1 for dW=dY^T@X). Target ≥ 50 GFLOP/s multithreaded at 1024³ on an 8-thread
// AVX2/FMA laptop.
func TestGemmF32Bench(t *testing.T) {
	if os.Getenv("MFL_GEMM_BENCH") == "" {
		t.Skip("set MFL_GEMM_BENCH=1 to run the gemm_f32 benchmark")
	}
	bench := `func bench(m, n, k, ta, tb, iters, label) {
	A := alloc(m*k*4) B := alloc(k*n*4) C := alloc(m*n*4)
	i := 0
	while i < m*k { poke_f32(A, i*4, 1.0) i = i+1 }
	i = 0
	while i < k*n { poke_f32(B, i*4, 1.0) i = i+1 }
	gemm_f32(C, A, B, m, n, k, ta, tb, 0)
	t0 := now_ms() it := 0
	while it < iters { gemm_f32(C, A, B, m, n, k, ta, tb, 0) it = it+1 }
	t1 := now_ms()
	gf := 2.0*float(m)*float(n)*float(k)*float(iters)/float(t1-t0)/1000000.0
	println(label + " " + str(gf) + " GFLOP/s")
	free(A) free(B) free(C)
}`
	main := `func main() {
	bench(512, 512, 512, 0, 0, 20, "512^3")
	bench(1024, 1024, 1024, 0, 0, 5, "1024^3")
	bench(8192, 288, 768, 0, 0, 3, "8192x288x768")
	bench(8192, 768, 288, 0, 0, 3, "8192x768x288")
	bench(288, 768, 8192, 1, 0, 3, "288x768x8192 ta=1")
}`
	out, _ := buildRun(t, main, bench)
	t.Log("\n" + out)
	// Surface the numbers on stdout too so they appear in `go test` verbose runs.
	os.Stdout.WriteString(out)
}
