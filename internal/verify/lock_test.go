package verify

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// knowledge/decisions/heavy-run-lock.md: one heavy run per machine. A
// second heavy run waits and says once who holds the lock; a holder that
// dies frees it; a run never waits on its own process or on the run whose
// command started it; light runs take no lock.

// lockHelperEnv makes TestLockHelperProcess hold the lock in the directory
// it names, as a second process, until it is killed.
const lockHelperEnv = "RELIAB_TEST_LOCK_HELPER"

func TestLockHelperProcess(t *testing.T) {
	dir := os.Getenv(lockHelperEnv)
	if dir == "" {
		t.Skip("helper process for the heavy-run lock tests")
	}
	me := holder{PID: os.Getpid(), Repo: "/src/other-repo", Tier: "prepr"}
	at := func() time.Time { return time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC) }
	if _, _, err := acquireHeavy(context.Background(), dir, me, at, io.Discard, 10*time.Millisecond); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	fmt.Println("held")
	time.Sleep(time.Minute)
	os.Exit(0)
}

// holdInOtherProcess starts a process holding the lock in dir and returns
// it once it holds the lock.
func holdInOtherProcess(t *testing.T, dir string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockHelperProcess$")
	cmd.Env = append(os.Environ(), lockHelperEnv+"="+dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if strings.TrimSpace(line) != "held" {
		t.Fatalf("helper said %q, %v", line, err)
	}
	return cmd
}

// syncBuffer is a log two goroutines may share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestHeavyRunWaitsForAnotherProcessThenTakesOverWhenItDies(t *testing.T) {
	dir := t.TempDir()
	other := holdInOtherProcess(t, dir)

	var log syncBuffer
	type result struct {
		release func()
		pid     int
		err     error
	}
	done := make(chan result, 1)
	go func() {
		release, pid, err := acquireHeavy(context.Background(), dir, holder{PID: os.Getpid(), Repo: "/src/this", Tier: "prepr"}, time.Now, &log, 10*time.Millisecond)
		done <- result{release, pid, err}
	}()
	select {
	case r := <-done:
		t.Fatalf("took the lock while another process held it: %+v", r)
	case <-time.After(300 * time.Millisecond):
	}
	// It says who holds the lock, and only once while that holder waits.
	want := fmt.Sprintf("waiting for another heavy run to finish (pid %d, prepr in /src/other-repo, since 2026-10-20T09:00:00Z)", other.Process.Pid)
	if got := log.String(); !strings.Contains(got, want) || strings.Count(got, "waiting") != 1 {
		t.Fatalf("log = %q, want one line containing %q", got, want)
	}

	// A holder that dies frees the lock; the waiting run takes it.
	if err := other.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.err != nil || r.pid != os.Getpid() {
			t.Fatalf("after the holder died: pid %d, %v", r.pid, r.err)
		}
		r.release()
	case <-time.After(10 * time.Second):
		t.Fatal("still waiting after the holder died")
	}
	if !strings.Contains(log.String(), "heavy-run lock taken after") {
		t.Errorf("log = %q, want it to say when the lock was taken", log.String())
	}
}

// The record a waiter reads says when its holder took the lock, not when
// it began to wait: heavy-run-lock.md has a person judge a hung holder by
// that time, so a run that queued must not look older than it is.
func TestLockRecordsWhenItWasTakenNotWhenTheWaitBegan(t *testing.T) {
	dir := t.TempDir()
	other := holdInOtherProcess(t, dir)
	queued := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	taken := queued.Add(time.Hour)
	var mu sync.Mutex
	clock := queued
	now := func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }

	done := make(chan error, 1)
	go func() {
		release, _, err := acquireHeavy(context.Background(), dir, holder{PID: os.Getpid(), Repo: "/src/this", Tier: "prepr"}, now, io.Discard, 10*time.Millisecond)
		if err == nil {
			t.Cleanup(release)
		}
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	clock = taken
	mu.Unlock()
	if err := other.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("still waiting after the holder died")
	}
	f, err := os.Open(dir + "/heavy.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	h, known := readHolder(f)
	if want := taken.Format(time.RFC3339); !known || h.StartedAt != want {
		t.Errorf("holder = %+v (known %v), want startedAt %s, when the lock was taken", h, known, want)
	}
}

func TestHeavyRunWaitEndsWithItsContext(t *testing.T) {
	dir := t.TempDir()
	holdInOtherProcess(t, dir)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, _, err := acquireHeavy(ctx, dir, holder{PID: os.Getpid()}, time.Now, io.Discard, 10*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the context's", err)
	}
}

// A reliab verify that a heavy run's command starts gets the holder's pid
// in RELIAB_HEAVY_LOCK and goes ahead, passing the same pid on.
func TestRunStartedByTheHolderDoesNotWait(t *testing.T) {
	dir := t.TempDir()
	other := holdInOtherProcess(t, dir)
	t.Setenv(lockEnv, strconv.Itoa(other.Process.Pid))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, pid, err := acquireHeavy(ctx, dir, holder{PID: os.Getpid()}, time.Now, io.Discard, 10*time.Millisecond)
	if err != nil || pid != other.Process.Pid {
		t.Fatalf("pid %d, err %v; want to go ahead under pid %d", pid, err, other.Process.Pid)
	}
	release()
}

// Two heavy runs in one process never wait on each other, and the lock is
// held until the last ends: another process cannot take it before.
func TestRunsInOneProcessShareTheLock(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, _, err := acquireHeavy(ctx, dir, holder{PID: os.Getpid()}, time.Now, io.Discard, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	second, pid, err := acquireHeavy(ctx, dir, holder{PID: os.Getpid()}, time.Now, io.Discard, 10*time.Millisecond)
	if err != nil || pid != os.Getpid() {
		t.Fatalf("second run in the process: pid %d, %v", pid, err)
	}
	first()
	if f, err := os.OpenFile(dir+"/heavy.lock", os.O_RDWR, 0); err != nil {
		t.Fatal(err)
	} else {
		ok, err := tryLock(f)
		if ok {
			_ = unlock(f)
		}
		_ = f.Close()
		if ok || err != nil {
			t.Errorf("lock free while the second run holds it: ok %v, err %v", ok, err)
		}
	}
	second()
	// The last release frees it for anyone.
	third, _, err := acquireHeavy(ctx, dir, holder{PID: os.Getpid()}, time.Now, io.Discard, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	third()
}

func TestHeavy(t *testing.T) {
	tests := []struct {
		tier     string
		onDemand bool
		run      []string
		want     bool
	}{
		{"inner", false, []string{"build", "unit"}, false},
		{"inner", false, []string{"build", "unit", "mutation_changed"}, true},
		{"prepr", false, []string{"build", "unit"}, true},
		{"nightly", false, []string{"build"}, true},
		{"prepr", true, []string{"unit"}, false},
		{"inner", true, []string{"mutation_full"}, true},
	}
	for _, tt := range tests {
		if got := heavy(tt.tier, tt.onDemand, tt.run); got != tt.want {
			t.Errorf("heavy(%s, on-demand %v, %v) = %v, want %v", tt.tier, tt.onDemand, tt.run, got, tt.want)
		}
	}
}

// Through verify: a light inner run goes ahead while another process holds
// the lock; a prepr run waits for it.
func TestVerifyLightRunIgnoresTheLockAndHeavyRunWaits(t *testing.T) {
	dir := t.TempDir()
	holdInOtherProcess(t, dir)
	root := repo(t, "commands:\n  unit: \"true\"\ntiers:\n  inner: { run: [unit] }\n  prepr: { run: [unit] }\n")
	run(t, Options{Root: root, Tier: "inner", LockDir: dir})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var log bytes.Buffer
	if _, _, err := Run(ctx, Options{Root: root, Profile: "reliability-profile.yaml", Tier: "prepr", LockDir: dir, Log: &log}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("prepr while another heavy run holds the lock: err %v, want it to wait until cancelled", err)
	}
	if !strings.Contains(log.String(), "waiting for another heavy run to finish") {
		t.Errorf("log = %q", log.String())
	}
}

// A heavy run's commands get the holder's pid, so a reliab verify one of
// them starts goes ahead instead of waiting on its parent forever.
func TestVerifyPassesTheHolderToCommands(t *testing.T) {
	root := repo(t, "commands:\n  unit: echo \"$RELIAB_HEAVY_LOCK\" > .reliab/out/lock.txt\ntiers:\n  prepr: { run: [unit] }\n")
	t.Setenv(lockEnv, "")
	run(t, Options{Root: root, Tier: "prepr", LockDir: t.TempDir()})
	got, err := os.ReadFile(root + "/.reliab/out/lock.txt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strconv.Itoa(os.Getpid()) {
		t.Errorf("RELIAB_HEAVY_LOCK = %q, want this process's pid %d", got, os.Getpid())
	}
}
