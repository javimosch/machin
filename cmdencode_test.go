package main

import "testing"

func TestCmdEncodeNoArgs(t *testing.T) {
	err := cmdEncode(nil)
	if err == nil {
		t.Fatal("expected error for missing source file, got nil")
	}
	want := "encode: need at least one source file"
	if err.Error() != want {
		t.Errorf("cmdEncode(nil) error = %q, want %q", err.Error(), want)
	}
}

func TestComposeSourcesMissingFile(t *testing.T) {
	_, _, err := composeSources([]string{"does-not-exist.mfl"})
	if err == nil {
		t.Fatal("expected error for nonexistent source file, got nil")
	}
}
