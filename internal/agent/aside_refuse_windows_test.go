//go:build windows

package agent

import (
	"testing"

	"golang.org/x/sys/windows"
)

// refuseWritesWindows marks path read-only, the one thing a Windows write
// refuses. The probe says whether it really is refused, since a process
// that may take the attribute off is not answered by it.
func refuseWritesWindows(t *testing.T, path string) {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		t.Skipf("cannot read %s's attributes: %v", path, err)
	}
	if err := windows.SetFileAttributes(p, attrs|windows.FILE_ATTRIBUTE_READONLY); err != nil {
		t.Skipf("cannot mark %s read-only: %v", path, err)
	}
	t.Cleanup(func() {
		open, err := windows.GetFileAttributes(p)
		if err == nil {
			windows.SetFileAttributes(p, open&^windows.FILE_ATTRIBUTE_READONLY)
		}
	})
	if !refusesNoWrite(path) {
		t.Skipf("%s marked read-only still takes a write", path)
	}
}