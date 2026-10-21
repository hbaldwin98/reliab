package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hbaldwin98/reliab/internal/mutscore"
	"github.com/hbaldwin98/reliab/internal/report"
)

// One heavy run per machine (knowledge/decisions/heavy-run-lock.md): two
// mutation or pre-PR runs at once share the CPUs, their tests time out,
// and timeouts read as kills. A heavy run takes a machine-wide lock first;
// a second waits for it, saying once who holds it. The operating system
// releases the lock when its holder exits, however it exits, so a dead
// holder never leaves a lock to clear by hand.

// lockEnv carries the pid holding the lock to the run's commands, so a
// reliab verify that a profile command starts never waits on its own
// parent.
const lockEnv = "RELIAB_HEAVY_LOCK"

// lockPoll is how often a waiting run tries the lock again.
const lockPoll = 500 * time.Millisecond

// heavy reports whether a run takes the heavy-run lock: a full prepr or
// nightly tier, or any run with a mutation command. An inner run without
// one, or an on-demand run of light commands, never waits.
func heavy(tier string, onDemand bool, run []string) bool {
	if slices.ContainsFunc(run, mutscore.IsMutation) {
		return true
	}
	return !onDemand && (tier == "prepr" || tier == "nightly")
}

// lockDir is where the machine's heavy-run lock lives, or "" for no lock:
// opt.LockDir, else reliab's directory in the user cache. A test binary
// takes no lock unless a test names a directory, so packages testing
// verify in parallel do not queue behind each other or behind a real run.
func lockDir(opt Options) string {
	if opt.LockDir != "" || testing.Testing() {
		return opt.LockDir
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "reliab")
}

// holder is what the lock file says about the run holding it.
type holder struct {
	PID       int    `json:"pid"`
	Repo      string `json:"repo"`
	Tier      string `json:"tier"`
	StartedAt string `json:"startedAt"`
}

func (h holder) String() string {
	return fmt.Sprintf("pid %d, %s in %s, since %s", h.PID, h.Tier, h.Repo, h.StartedAt)
}

// inProcess is this process's hold per lock directory: the lock file and
// how many runs share it. A run in the process that holds the lock never
// waits for it, and the lock is released when the last of them ends.
var inProcess = struct {
	sync.Mutex
	held map[string]*hold
}{held: map[string]*hold{}}

type hold struct {
	n int
	f *os.File
}

// acquireHeavy takes the heavy-run lock in dir and returns the function
// that releases it, and the pid of the process holding it, which the run
// passes to its commands as lockEnv. The lock file records me, stamped
// with now() when the lock is taken, not when the wait began: a waiter
// reads that time to judge whether the holder is hung. It waits while another process holds it, writing to
// log who that is, once per holder, and returns ctx's error if ctx ends
// first. It proceeds without waiting when this process already holds it,
// or when the holder is the run whose command started this one (lockEnv).
func acquireHeavy(ctx context.Context, dir string, me holder, now func() time.Time, log io.Writer, poll time.Duration) (func(), int, error) {
	release := func() {
		inProcess.Lock()
		defer inProcess.Unlock()
		h := inProcess.held[dir]
		if h.n--; h.n == 0 {
			_ = h.f.Truncate(0)
			_ = unlock(h.f)
			_ = h.f.Close()
			delete(inProcess.held, dir)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, 0, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "heavy.lock"), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, 0, err
	}
	var shown *holder
	waitStart := time.Now()
	for {
		// Checking this process's hold and taking the lock happen under one
		// mutex, so two runs in the process never wait on each other.
		inProcess.Lock()
		if h := inProcess.held[dir]; h != nil {
			h.n++
			inProcess.Unlock()
			_ = f.Close()
			return release, os.Getpid(), nil
		}
		ok, err := tryLock(f)
		if ok {
			me.StartedAt = report.Now(now())
			if b, err := json.Marshal(me); err == nil {
				_ = f.Truncate(0)
				_, _ = f.WriteAt(b, 0)
			}
			inProcess.held[dir] = &hold{n: 1, f: f}
		}
		inProcess.Unlock()
		if err != nil {
			_ = f.Close()
			return nil, 0, err
		}
		if ok {
			if shown != nil {
				fmt.Fprintf(log, "heavy-run lock taken after %s\n", time.Since(waitStart).Round(time.Second))
			}
			return release, os.Getpid(), nil
		}
		h, known := readHolder(f)
		if known && os.Getenv(lockEnv) == strconv.Itoa(h.PID) {
			_ = f.Close()
			return func() {}, h.PID, nil
		}
		if shown == nil || (known && h != *shown) {
			if known {
				fmt.Fprintf(log, "waiting for another heavy run to finish (%s); one runs per machine at a time\n", h)
			} else {
				fmt.Fprintln(log, "waiting for another heavy run to finish; one runs per machine at a time")
			}
			shown = &h
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, 0, ctx.Err()
		case <-time.After(poll):
		}
	}
}

// readHolder reads the lock file's holder; known is false while it is
// empty or half written.
func readHolder(f *os.File) (holder, bool) {
	b := make([]byte, 4096)
	n, _ := f.ReadAt(b, 0)
	var h holder
	if err := json.Unmarshal(b[:n], &h); err != nil || h.PID == 0 {
		return holder{}, false
	}
	return h, true
}
