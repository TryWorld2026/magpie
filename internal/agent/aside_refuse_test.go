package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// refuseWrites makes the file at path refuse a write to it, on either
// platform: its permission bits on Unix, where 0400 refuses one, and the
// read-only attribute on Windows, where a write to such a file answers a
// permission error. A platform a write still goes through on is no answer
// for these checks, so it says so rather than leaving them asserting
// nothing.
func refuseWrites(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o400); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(path, 0o600) })
		return
	}
	refuseWritesWindows(t, path)
}

// refusesNoWrite is whether a write to path is still turned away, which a
// Windows file carrying the read-only attribute alone may not be for a
// process that can take it off.
func refusesNoWrite(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return true
	}
	f.Close()
	return false
}

// refused is whether err is the answer a write to a file that refuses one
// gets, each platform naming it its own way.
func refused(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "permission denied") || strings.Contains(s, "Access is denied")
}

var _ = filepath.Join
