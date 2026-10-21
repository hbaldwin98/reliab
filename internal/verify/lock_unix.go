//go:build !windows

package verify

import (
	"errors"
	"os"
	"syscall"
)

// tryLock takes an exclusive lock on f without waiting; ok is false while
// another open file holds it. The kernel drops the lock when its holder
// exits.
func tryLock(f *os.File) (ok bool, err error) {
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

func unlock(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
