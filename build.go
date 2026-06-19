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
func BuildBinary(funcs []*FuncDecl, outPath string) error {
	csrc, err := CompileToC(funcs)
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
func RunCaptured(funcs []*FuncDecl) (string, error) {
	bin, err := os.CreateTemp("", "mfl-bin-*")
	if err != nil {
		return "", err
	}
	bin.Close()
	defer os.Remove(bin.Name())
	if err := BuildBinary(funcs, bin.Name()); err != nil {
		return "", err
	}
	out, err := exec.Command(bin.Name()).Output()
	return string(out), err
}
