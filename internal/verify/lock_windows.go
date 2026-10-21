//go:build windows

package verify

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockRange is the byte locked: one past 4 GiB, far beyond the holder
// record at the start of the file, so a waiting run can still read who
// holds it (Windows locks are mandatory for the range they cover).
func lockRange() *windows.Overlapped { return &windows.Overlapped{OffsetHigh: 1} }

// tryLock takes an exclusive lock on f without waiting; ok is false while
// another handle holds it. Windows drops the lock when its holder exits.
func tryLock(f *os.File) (ok bool, err error) {
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, lockRange())
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return false, nil
	}
	return err == nil, err
}

func unlock(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, lockRange())
}
