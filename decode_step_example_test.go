package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// examples/complex/decode_step.mfl is the non-matmul half of a quantized-LLM
// decode step: RMS normalization, a numerically-stable softmax, and an argmax,
// all as plain MFL loops over the same raw fp32 buffers (alloc + peek_f32 /
// poke_f32) that dot_q8 reads. This builds the shipped example end-to-end and
// pins its output so the three kernels' arithmetic contract can't drift, and
// separately re-derives the softmax invariants from that output so the golden
// string can't be "fixed" to a wrong-but-stable value.
func TestDecodeStepExample(t *testing.T) {
	prog, err := loadMFL("examples/complex/decode_step.mfl")
	if err != nil {
		t.Fatalf("load example: %v", err)
	}
	bin, err := os.CreateTemp("", "mfl-decode-*")
	if err != nil {
		t.Fatal(err)
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(prog, bin.Name(), false); err != nil {
		t.Fatalf("build example: %v", err)
	}
	raw, err := exec.Command(bin.Name()).Output()
	if err != nil {
		t.Fatalf("run example: %v", err)
	}
	out := string(raw)

	// rmsnorm of [1,2,3,4] with unit weights: divide by sqrt(mean(x^2))=sqrt(7.5),
	// so the outputs are 1..4 / 2.7386. softmax of logits [1,3,2,0] is stable
	// (max-subtracted) and argmax picks index 1 (logit 3.0).
	const want = "rmsnorm: 0.365148 0.730296 1.09544 1.46059\n" +
		"greedy token: 1 p: 0.643914\n" +
		"probs: 0.0871443 0.643914 0.236883 0.0320586\n"
	if out != want {
		t.Fatalf("decode_step output:\n got %q\nwant %q", out, want)
	}

	// Re-derive the invariants from the emitted probs line: it must be a valid
	// distribution (sums to 1) whose argmax is the greedy token the example
	// reported. This catches a golden string edited to a non-softmax value.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	probs := parseFloats(t, strings.TrimPrefix(lines[2], "probs:"))
	sum, best, bestIdx := 0.0, probs[0], 0
	for i, p := range probs {
		if p < 0 {
			t.Fatalf("probability %d is negative: %v", i, p)
		}
		sum += p
		if p > best {
			best, bestIdx = p, i
		}
	}
	if sum < 0.999 || sum > 1.001 {
		t.Fatalf("softmax probs do not sum to 1: got %v (sum %v)", probs, sum)
	}
	if bestIdx != 1 {
		t.Fatalf("argmax of probs is index %d, want the greedy token 1", bestIdx)
	}
}

func parseFloats(t *testing.T, s string) []float64 {
	t.Helper()
	var out []float64
	for _, f := range strings.Fields(s) {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			t.Fatalf("parse float %q: %v", f, err)
		}
		out = append(out, v)
	}
	return out
}
