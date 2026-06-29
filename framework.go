package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The framework modules (machweb, the DB drivers, sso, ws, smtp, reactive, …) are
// MFL libraries an app composes against — `machin encode framework/machweb.src
// app.src`. They are embedded here so a binary-only install (curl|sh, no repo
// checkout) can resolve them: without this, the very first quickstart command fails
// with "no such file: framework/machweb.src".
//
//go:embed framework/*.src
var frameworkFS embed.FS

// readModule reads a source file for `machin encode`, falling back to an embedded
// framework module when the local file is absent. A local copy always wins (so a
// vendored or edited module is respected); the embed only fills the gap on a bare
// install. Tries the path as given, then framework/<basename>.
func readModule(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, err // a real error (permissions, a directory, …) — surface it
	}
	if d, e := frameworkFS.ReadFile(path); e == nil {
		return d, nil
	}
	if d, e := frameworkFS.ReadFile("framework/" + filepath.Base(path)); e == nil {
		return d, nil
	}
	return nil, err // the original not-found error
}

// frameworkModules lists the embedded module file names (e.g. "machweb.src").
func frameworkModules() []string {
	entries, err := frameworkFS.ReadDir("framework")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// cmdFramework gives explicit access to the embedded framework modules:
//
//	machin framework list            list the embedded modules
//	machin framework machweb         print one to stdout (machweb.src)
//	machin framework --vendor [dir]  write them all into ./framework (or dir)
func cmdFramework(args []string) error {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "list", "-h", "--help":
		for _, m := range frameworkModules() {
			fmt.Println(m)
		}
		return nil
	case "--vendor", "vendor":
		dir := "framework"
		if len(args) > 1 {
			dir = args[1]
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		mods := frameworkModules()
		for _, m := range mods {
			data, err := frameworkFS.ReadFile("framework/" + m)
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, m), data, 0o644); err != nil {
				return err
			}
		}
		fmt.Fprintf(os.Stderr, "vendored %d framework modules -> %s/\n", len(mods), dir)
		return nil
	default:
		name := sub
		if !strings.HasSuffix(name, ".src") {
			name += ".src"
		}
		data, err := frameworkFS.ReadFile("framework/" + filepath.Base(name))
		if err != nil {
			return fmt.Errorf("no framework module %q — run `machin framework list`", sub)
		}
		fmt.Print(string(data))
		return nil
	}
}
