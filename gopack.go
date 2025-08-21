package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
	"golang.org/x/mod/zip"
)

const (
	dirPerm  = 0o755
	filePerm = 0o644
)

type info struct {
	Version string    `json:"Version"`
	Time    time.Time `json:"Time"`
}

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// flags
	srcDir := flag.String("src", "", "path to module source directory (worktree root with go.mod)")
	version := flag.String("version", "", "version tag to publish, e.g. v1.0.3")
	outRoot := flag.String("out", "", "GOPROXY root dir, e.g. /tmp/goproxy")
	flag.Parse()

	if *srcDir == "" || *version == "" || *outRoot == "" {
		flag.Usage()
		return fmt.Errorf("required flags: -src, -version, -out")
	}
	if !semver.IsValid(*version) {
		return fmt.Errorf("invalid version %q (want semver like v1.2.3)", *version)
	}

	absSrc, err := filepath.Abs(*srcDir)
	if err != nil {
		return err
	}

	// read go.mod → module path
	goModPath := filepath.Join(absSrc, "go.mod")
	goModBytes, err := os.ReadFile(goModPath)
	if err != nil {
		return fmt.Errorf("read go.mod: %w", err)
	}
	modf, err := modfile.Parse("go.mod", goModBytes, nil)
	if err != nil {
		return fmt.Errorf("parse go.mod: %w", err)
	}
	if modf.Module == nil || modf.Module.Mod.Path == "" {
		return fmt.Errorf("cannot determine module path from go.mod")
	}
	modPath := modf.Module.Mod.Path

	// proxy layout: <out>/<escaped module>/@v/<escaped version>.{mod,info,zip} and <out>/<escaped module>/@v/list
	escPath, err := module.EscapePath(modPath)
	if err != nil {
		return fmt.Errorf("escape module path: %w", err)
	}
	escVer, err := module.EscapeVersion(*version)
	if err != nil {
		return fmt.Errorf("escape version: %w", err)
	}

	modDir := filepath.Join(*outRoot, escPath)
	atV := filepath.Join(modDir, "@v")
	if err := os.MkdirAll(atV, dirPerm); err != nil {
		return err
	}

	// .mod
	if err := os.WriteFile(filepath.Join(atV, escVer+".mod"), goModBytes, filePerm); err != nil {
		return err
	}

	// .info
	ib, err := json.Marshal(info{Version: *version, Time: time.Now().UTC()})
	if err != nil {
		return err
	}
	ib = append(ib, '\n')
	if err := os.WriteFile(filepath.Join(atV, escVer+".info"), ib, filePerm); err != nil {
		return err
	}

	// .zip (canonical via x/mod/zip)
	tmp, err := os.CreateTemp(atV, "zip-*")
	if err != nil {
		return err
	}
	tmpZip := tmp.Name()
	defer func() { _ = os.Remove(tmpZip) }()
	defer tmp.Close()

	if err := zip.CreateFromDir(tmp, module.Version{Path: modPath, Version: *version}, absSrc); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	zipFile := filepath.Join(atV, escVer+".zip")
	if err := os.Rename(tmpZip, zipFile); err != nil {
		return err
	}

	// update @v/list
	if err := updateListFile(atV, *version); err != nil {
		return fmt.Errorf("update list: %w", err)
	}

	fmt.Printf("Wrote:\n  %s\n  %s\n  %s\n  %s\n",
		filepath.Join(atV, escVer+".mod"),
		filepath.Join(atV, escVer+".info"),
		zipFile,
		filepath.Join(atV, "list"),
	)
	return nil
}

// updateListFile updates the @v/list file (newline-separated unescaped versions).
// It ensures unique entries, semver-sorted ascending, and writes atomically.
func updateListFile(atVDir string, newVer string) error {
	listPath := filepath.Join(atVDir, "list")

	// Read existing versions (if any).
	existing := make([]string, 0, 16)
	f, err := os.Open(listPath)
	if err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				continue
			}
			// Keep only valid semver-looking lines (defensive).
			if semver.IsValid(line) {
				existing = append(existing, line)
			}
		}
		if err := sc.Err(); err != nil {
			return fmt.Errorf("scan list: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	// Insert new version if missing.
	seen := make(map[string]struct{}, len(existing)+1)
	for _, v := range existing {
		seen[v] = struct{}{}
	}
	if _, ok := seen[newVer]; !ok {
		existing = append(existing, newVer)
	}

	// Sort using semver.Compare (ascending).
	sort.Slice(existing, func(i, j int) bool {
		return semver.Compare(existing[i], existing[j]) < 0
	})

	// Compose content with trailing newline.
	var out []byte
	for _, v := range existing {
		out = append(out, []byte(v+"\n")...)
	}

	// Atomic write: same-dir temp + rename.
	return writeFileAtomic(atVDir, "list", out, filePerm)
}

// writeFileAtomic writes data to dir/baseName atomically.
func writeFileAtomic(dir, baseName string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(dir, baseName+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, filepath.Join(dir, baseName))
}
