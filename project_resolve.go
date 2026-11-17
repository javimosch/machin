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
// or a single file (.mfl or .src). For directories, it recursively gathers all .src files
// across subfolders, composes them into a Program AST, and generates/updates the latest .mfl snapshot.
// It returns the Program AST, the derived output executable name, the path to the resolved .mfl snapshot,
// a boolean indicating if it was a project/src composition, and any error encountered.
func resolveProjectInput(target string) (prog *Program, defaultOut string, snapshotMflPath string, isProject bool, err error) {
	// Follow root symlink if any
	resolvedTarget := target
	if fi, err := os.Lstat(target); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if realPath, err := filepath.EvalSymlinks(target); err == nil {
			resolvedTarget = realPath
		}
	}

	fi, err := os.Stat(resolvedTarget)
	if err != nil {
		return nil, "", "", false, err
	}

	if !fi.IsDir() {
		// Target is a single file (.mfl or .src)
		if strings.HasSuffix(target, ".src") {
			p, mflText, err := composeSources([]string{target})
			if err != nil {
				return nil, "", "", false, err
			}
			mflPath := strings.TrimSuffix(target, ".src") + ".mfl"
			if err := os.WriteFile(mflPath, []byte(mflText), 0644); err != nil {
				return nil, "", "", false, fmt.Errorf("failed to generate %s: %w", mflPath, err)
			}
			outName := strings.TrimSuffix(filepath.Base(target), ".src")
			return p, outName, mflPath, true, nil
		}
		// Regular .mfl file
		p, err := loadMFL(target)
		outName := strings.TrimSuffix(filepath.Base(target), ".mfl")
		return p, outName, target, false, err
	}

	// Target is a directory (e.g. `machin build .` or `machin build ./pkg`)
	// Derive folder name for the project snapshot
	absDir, _ := filepath.Abs(resolvedTarget)
	baseFolderName := filepath.Base(absDir)
	if baseFolderName == "/" || baseFolderName == "." || baseFolderName == "" {
		baseFolderName = "app"
	}

	// Recursively traverse directory and subfolders
	var srcFiles []string
	var mflFiles []string

	err = filepath.WalkDir(resolvedTarget, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := d.Name()

		// Skip hidden folders, caches, git, build, vendor (only when path != resolvedTarget)
		if d.IsDir() {
			if path != resolvedTarget {
				if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
					name == "vendor" || name == "build" || name == "node_modules" {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Skip hidden files, drafts (_), backups (*.bak, *~), and test files (*_test.src)
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
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
		return nil, "", "", true, fmt.Errorf("walk directory %s: %w", target, err)
	}

	// If no .src files, check if an existing .mfl file exists
	if len(srcFiles) == 0 {
		if len(mflFiles) == 1 {
			p, err := loadMFL(mflFiles[0])
			outName := strings.TrimSuffix(filepath.Base(mflFiles[0]), ".mfl")
			return p, outName, mflFiles[0], true, err
		}
		if len(mflFiles) > 1 {
			return nil, "", "", true, fmt.Errorf("multiple .mfl files found in %s; specify which one to build", target)
		}
		return nil, "", "", true, fmt.Errorf("no .src or .mfl source files found in %s", target)
	}

	// Sort files deterministically (alphabetical)
	sort.Strings(srcFiles)

	// Compose all .src files found across subfolders
	p, mflText, err := composeSources(srcFiles)
	if err != nil {
		return nil, "", "", true, err
	}

	// The .mfl snapshot filename is always derived from the directory itself
	mflPath := filepath.Join(target, baseFolderName+".mfl")
	if err := os.WriteFile(mflPath, []byte(mflText), 0644); err != nil {
		return nil, "", "", true, fmt.Errorf("failed to generate %s: %w", mflPath, err)
	}
	fmt.Fprintf(os.Stderr, "generated latest %s (%d files composed)\n", mflPath, len(srcFiles))

	return p, baseFolderName, mflPath, true, nil
}
