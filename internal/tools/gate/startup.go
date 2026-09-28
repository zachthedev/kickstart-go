package main

import (
	"fmt"
	"strconv"
	"strings"
)

// ///////////////////////////////////////////////
// Constants
// ///////////////////////////////////////////////

const (
	// vendor is where go builds dependencies from when the directory exists and
	// no -mod flag says otherwise.
	vendor = "vendor"
	// trackedShown is how many tracked paths under vendor a finding names
	// before it counts the rest.
	trackedShown = 5
	// excerptBytes is how much of a file's text a finding quotes.
	excerptBytes = 200
)

// ///////////////////////////////////////////////
// The checks
// ///////////////////////////////////////////////

// startupFindings refuses a tracked path under a vendor directory at the
// root, which go builds from in place of the module cache unless a -mod flag
// says otherwise, while CI's gate sets -mod=readonly. No shared job reads
// vendor. The shared commits job refuses the other tracked files a program
// reads before any check of its own, before a merge. Names compare through
// fold, because a case-insensitive filesystem opens a tracked Vendor as
// vendor.
func startupFindings(tracked []string) []string {
	var vendored []string
	for _, name := range tracked {
		if key := fold(name); key == fold(vendor) || strings.HasPrefix(key, fold(vendor)+"/") {
			vendored = append(vendored, strconv.Quote(name))
		}
	}
	if len(vendored) == 0 {
		return nil
	}
	return []string{trackedUnder(vendored, vendor, "which go builds from in place of the module cache unless a -mod flag says otherwise")}
}

// trackedUnder names the first few quoted paths under dir and counts the
// rest, so a committed package reads as one finding. why says what reads dir.
func trackedUnder(quoted []string, dir, why string) string {
	shown := quoted[:min(len(quoted), trackedShown)]
	more := ""
	if extra := len(quoted) - len(shown); extra > 0 {
		more = fmt.Sprintf(" and %d more", extra)
	}
	verb := "are"
	if len(quoted) == 1 {
		verb = "is"
	}
	return fmt.Sprintf("%s%s %s tracked under %s, %s. Remove them from the index with git rm -r --cached",
		strings.Join(shown, ", "), more, verb, dir, why)
}

// excerpt quotes the start of a file's text for a finding, so a control
// character cannot reach the terminal and a large file stays one line.
func excerpt(data []byte) string {
	if len(data) <= excerptBytes {
		return strconv.Quote(string(data))
	}
	return strconv.Quote(string(data[:excerptBytes])) + "..."
}
