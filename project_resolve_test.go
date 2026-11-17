package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectResolution_Directory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create subfolder with helper.src
	subDir := filepath.Join(tmpDir, "pkg")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	helperSrc := `func greet(name) (r) { r = "hello " + name }`
	if err := os.WriteFile(filepath.Join(subDir, "helper.src"), []byte(helperSrc), 0644); err != nil {
		t.Fatal(err)
	}

	// Create root main.src
	mainSrc := `func main() { msg := greet("world") println(msg) }`
	if err := os.WriteFile(filepath.Join(tmpDir, "main.src"), []byte(mainSrc), 0644); err != nil {
		t.Fatal(err)
	}

	// Resolve project directory
	prog, outName, isProject, err := resolveProjectInput(tmpDir, "")
	if err != nil {
		t.Fatalf("resolveProjectInput failed: %v", err)
	}
	if !isProject {
		t.Errorf("expected isProject to be true")
	}
	if outName == "" {
		t.Errorf("expected non-empty outName")
	}
	if prog == nil {
		t.Fatalf("expected non-nil Program AST")
	}

	// Verify generated .mfl file exists in tmpDir
	mflFiles, err := filepath.Glob(filepath.Join(tmpDir, "*.mfl"))
	if err != nil || len(mflFiles) != 1 {
		t.Errorf("expected exactly 1 generated .mfl file, got %v", mflFiles)
	}
}

func TestProjectResolution_SingleSrcFile(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "app.src")
	srcContent := `func main() { println("single file") }`
	if err := os.WriteFile(srcPath, []byte(srcContent), 0644); err != nil {
		t.Fatal(err)
	}

	prog, outName, isProject, err := resolveProjectInput(srcPath, "")
	if err != nil {
		t.Fatalf("resolveProjectInput failed: %v", err)
	}
	if !isProject {
		t.Errorf("expected isProject to be true")
	}
	if outName != "app" {
		t.Errorf("expected outName 'app', got %q", outName)
	}
	if prog == nil {
		t.Fatalf("expected non-nil Program AST")
	}

	expectedMfl := filepath.Join(tmpDir, "app.mfl")
	if _, err := os.Stat(expectedMfl); os.IsNotExist(err) {
		t.Errorf("expected generated .mfl at %s", expectedMfl)
	}
}
