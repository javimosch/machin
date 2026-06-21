package main

import (
	"fmt"
	"os"
	"os/exec"
)

// ccPath is the C compiler used to turn emitted C into a native binary.
func ccPath() string {
	if cc := os.Getenv("CC"); cc != "" {
		return cc
	}
	return "cc"
}

// BuildBinary compiles the program to a native executable at outPath via cc -O2.
// When safe is set, runtime bounds, division-by-zero, and overflow checks are
// inserted.
func BuildBinary(prog *Program, outPath string, safe bool) error {
	csrc, err := CompileToC(prog, safe)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "mfl-*.c")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(csrc); err != nil {
		return err
	}
	tmp.Close()

	cmd := exec.Command(ccPath(), "-O2", "-std=c11", "-pthread", "-o", outPath, tmp.Name())
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed: %v\n%s", ccPath(), err, out)
	}
	return nil
}

// RunCaptured builds the program to a temp binary, runs it, and returns stdout.
func RunCaptured(prog *Program) (string, error) {
	bin, err := os.CreateTemp("", "mfl-bin-*")
	if err != nil {
		return "", err
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(prog, bin.Name(), false); err != nil {
		return "", err
	}
	out, err := exec.Command(bin.Name()).Output()
	return string(out), err
}
