// Package markers inventories the template's own annotations.
//
// A clone absorbs kickstart by working through every directive marked with
// the TODO form of the scope below, and deleting each one. The NOTE form is
// the other half of the convention: a clone leaves one where it deliberately
// diverges from template content, so a diff against kickstart carries the
// reason next to the change.
//
// The inventory is generated: [Scan] is the reader and internal/generate
// renders what it returns. Neither marker is spelled literally in this file,
// so the scanner does not count itself.
package markers

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// ///////////////////////////////////////////////
// Types
// ///////////////////////////////////////////////

// File records how many markers of each kind one file carries.
type File struct {
	// Path is slash-separated and relative to the scanned root.
	Path string
	// TODO counts directives a clone must act on.
	TODO int
	// NOTE counts deliberate divergences a clone has recorded.
	NOTE int
}

// ///////////////////////////////////////////////
// Constants
// ///////////////////////////////////////////////

// TODOMarker and NOTEMarker are the two annotations this package counts.
// They are assembled at run time so that this file, and the inventory
// generated from it, do not count themselves.
const (
	scope      = "(kickstart):"
	TODOMarker = "TODO" + scope
	NOTEMarker = "NOTE" + scope
)

// ///////////////////////////////////////////////
// Variables
// ///////////////////////////////////////////////

// SkipDirs are directory names never scanned: version control, build output
// and installed dependencies, none of which a clone edits.
var SkipDirs = []string{".git", "dist", "node_modules", "vendor"}

// ///////////////////////////////////////////////
// Scan
// ///////////////////////////////////////////////

// Scan walks root and returns one [File] per file carrying at least one
// marker, sorted by path. Binary files, the paths in skip, the directories
// SkipDirs names and every directory below root holding a .git entry are
// ignored; pass the generated inventory's own path in skip so it does not
// count itself.
func Scan(root string, skip ...string) ([]File, error) {
	skipSet := make(map[string]struct{}, len(skip))
	for _, p := range skip {
		skipSet[filepath.ToSlash(p)] = struct{}{}
	}

	// Reads go through an os.Root so a symlink inside the tree cannot walk
	// the scanner out of it between the walk and the read.
	sandbox, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", root, err)
	}
	defer sandbox.Close()

	var found []File
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		slashed := filepath.ToSlash(rel)

		if d.IsDir() {
			if slashed != "." && (isSkippedDir(slashed) || holdsWorkTree(sandbox, rel)) {
				return fs.SkipDir
			}
			return nil
		}
		if _, ok := skipSet[slashed]; ok {
			return nil
		}
		// A link is not a site: its target is, and the walk reaches the
		// target on its own. Counting both would report one marker twice.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		data, readErr := sandbox.ReadFile(rel)
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", slashed, readErr)
		}
		if isBinary(data) {
			return nil
		}
		todo := strings.Count(string(data), TODOMarker)
		note := strings.Count(string(data), NOTEMarker)
		if todo == 0 && note == 0 {
			return nil
		}
		found = append(found, File{Path: slashed, TODO: todo, NOTE: note})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning %s: %w", root, err)
	}

	slices.SortFunc(found, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return found, nil
}

// Totals sums the markers across every scanned file.
func Totals(files []File) (todo, note int) {
	for _, f := range files {
		todo += f.TODO
		note += f.NOTE
	}
	return todo, note
}

// ///////////////////////////////////////////////
// Internal helpers
// ///////////////////////////////////////////////

// isSkippedDir reports whether a slash-separated relative directory matches
// an entry in SkipDirs, either by its own name or by its full relative path.
func isSkippedDir(rel string) bool {
	base := path.Base(rel)
	for _, skip := range SkipDirs {
		if skip == rel || skip == base {
			return true
		}
	}
	return false
}

// holdsWorkTree reports whether the directory at rel holds a .git entry: a
// nested repository, a linked worktree such as one under .claude/worktrees,
// or a submodule checkout. Each is another work tree with its own copy of the
// markers, and git grep does not enter an untracked one either.
func holdsWorkTree(sandbox *os.Root, rel string) bool {
	_, err := sandbox.Lstat(filepath.Join(rel, ".git"))
	return err == nil
}

// isBinary reports whether data looks like a binary file. A NUL byte in the
// first 8 KB is the same heuristic git uses.
func isBinary(data []byte) bool {
	head := data
	if len(head) > 8000 {
		head = head[:8000]
	}
	return bytes.IndexByte(head, 0) >= 0
}
