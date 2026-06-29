package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// On a bare install (binary only, no repo checkout) `machin encode
// framework/machweb.src app.src` must still work — the framework module resolves
// from the embedded copy. This is the quickstart's first build command; before the
// embed it failed with "no such file: framework/machweb.src".
func TestEncodeResolvesEmbeddedFramework(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	dir := t.TempDir()
	app := `func main() { serve(8080, func(req) { return ok_text("hi") }) }`
	if err := os.WriteFile(filepath.Join(dir, "app.src"), []byte(app), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil { // a dir with NO local framework/
		t.Fatal(err)
	}

	// cmdEncode prints the result to stdout; silence it for a clean test log.
	old := os.Stdout
	devnull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	os.Stdout = devnull
	err = cmdEncode([]string{"framework/machweb.src", "app.src"})
	os.Stdout = old
	if devnull != nil {
		devnull.Close()
	}
	if err != nil {
		t.Fatalf("encode should resolve the embedded framework on a bare box, got: %v", err)
	}
}

// A local framework copy must win over the embedded one (vendoring is respected).
func TestReadModulePrefersLocal(t *testing.T) {
	dir := t.TempDir()
	fwdir := filepath.Join(dir, "framework")
	if err := os.MkdirAll(fwdir, 0o755); err != nil {
		t.Fatal(err)
	}
	local := []byte("// a local override\n")
	if err := os.WriteFile(filepath.Join(fwdir, "machweb.src"), local, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readModule(filepath.Join(fwdir, "machweb.src"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, local) {
		t.Fatalf("local module should win; got %q", got)
	}
}

// The embedded set covers the real modules an app composes against.
func TestFrameworkModulesEmbedded(t *testing.T) {
	mods := frameworkModules()
	for _, want := range []string{"machweb.src", "postgres.src", "smtp.src", "ws.src"} {
		found := false
		for _, m := range mods {
			if m == want {
				found = true
			}
		}
		if !found {
			t.Errorf("embedded framework missing %s (have %v)", want, mods)
		}
	}
}
