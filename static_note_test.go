package main

import (
	"strings"
	"testing"
)

func TestStaticGlibcNote(t *testing.T) {
	ld := "x.c:(.text+0x1): warning: Using 'getaddrinfo' in statically linked applications requires at runtime the shared libraries from the glibc version used for linking"
	if !strings.Contains(staticGlibcNote(true, ld), "may not be portable") {
		t.Fatal("the linker warning was not surfaced")
	}
	if staticGlibcNote(false, ld) != "" || staticGlibcNote(true, "") != "" {
		t.Fatal("warned without cause")
	}
}
