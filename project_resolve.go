package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// resolveProjectInput checks whether the target is a directory (".", "./dir", etc.)
// or a single file. For directories, it recursively gathers all .src files across subfolders,
// composes them into a Program AST, and automatically generates/updates the latest .mfl snapshot.
func resolveProjectInput(target, defaultOutName string) (prog *Program, effectiveOut string, isProject bool, err error) {
	fi, err := os.Stat(target)
	if err != nil {
		return nil, "", false, err
	}

	if !fi.IsDir() {
		// Target is a single file (.mfl or .src)
		if strings.HasSuffix(target, ".src") {
			p, mflText, err := composeSources([]string{target})
			if err != nil {
				return nil, "", false, err
			}
			mflPath := strings.TrimSuffix(target, ".src") + ".mfl"
			if err := os.WriteFile(mflPath, []byte(mflText), 0644); err != nil {
				return nil, "", false, fmt.Errorf("failed to generate %s: %w", mflPath, err)
			}
			outName := defaultOutName
			if outName == "" {
				outName = strings.TrimSuffix(filepath.Base(target), ".src")
			}
			return p, outName, true, nil
		}
		// Regular .mfl file
		p, err := loadMFL(target)
		return p, defaultOutName, false, err
	}

	// Target is a directory (e.g. `machin build .`)
	// Recursively traverse directory and subfolders
	var srcFiles []string
	var mflFiles []string

	err = filepath.WalkDir(target, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := d.Name()

		// Skip hidden folders, caches, git, build, vendor
		if d.IsDir() {
			if path != target && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) ||
				name == "vendor" || name == "build" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip hidden files, drafts (_), backups (*.bak, *~), and test files (*_test.src)
		if path != target && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) ||
			strings.HasSuffix(name, ".bak") || strings.HasSuffix(name, "~") ||
			strings.HasSuffix(name, "_test.src") {
			return nil
		}

		if strings.HasSuffix(name, ".src") {
			srcFiles = append(srcFiles, path)
		} else if strings.HasSuffix(name, ".mfl") {
			mflFiles = append(mflFiles, path)
		}
		return nil
	})

	if err != nil {
		return nil, "", true, fmt.Errorf("walk directory %s: %w", target, err)
	}

	// If no .src files, check if an existing .mfl file exists
	if len(srcFiles) == 0 {
		if len(mflFiles) == 1 {
			p, err := loadMFL(mflFiles[0])
			outName := defaultOutName
			if outName == "" {
				outName = strings.TrimSuffix(filepath.Base(mflFiles[0]), ".mfl")
			}
			return p, outName, true, err
		}
		if len(mflFiles) > 1 {
			return nil, "", true, fmt.Errorf("multiple .mfl files found in %s; specify which one to build", target)
		}
		return nil, "", true, fmt.Errorf("no .src or .mfl source files found in %s", target)
	}

	// Sort files deterministically (alphabetical)
	sort.Strings(srcFiles)

	// Compose all .src files found across subfolders
	p, mflText, err := composeSources(srcFiles)
	if err != nil {
		return nil, "", true, err
	}

	// Determine output binary and .mfl names
	outName := defaultOutName
	if outName == "" {
		absDir, _ := filepath.Abs(target)
		outName = filepath.Base(absDir)
		if outName == "/" || outName == "." || outName == "" {
			outName = "app"
		}
	}

	// Always write the latest .mfl snapshot in the root of the target directory
	mflPath := filepath.Join(target, outName+".mfl")
	if err := os.WriteFile(mflPath, []byte(mflText), 0644); err != nil {
		return nil, "", true, fmt.Errorf("failed to generate %s: %w", mflPath, err)
	}
	fmt.Fprintf(os.Stderr, "generated latest %s (%d files composed)\n", mflPath, len(srcFiles))

	return p, outName, true, nil
}
