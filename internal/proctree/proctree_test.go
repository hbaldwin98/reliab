package proctree

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Cancelling a tracked command kills its whole process tree, not just the
// command (knowledge/decisions/verify-execution.md: "the running command's
// whole process tree is killed"; debug-adapter-seam.md: the adapter's
// process tree is killed on exit).

func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "proctree-child":
			child()
		case "proctree-grandchild":
			grandchild()
		}
	}
	os.Exit(m.Run())
}

// child waits for a line on stdin, then starts a grandchild and passes on
// the port it listens on.
func child() {
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	gc := exec.Command(os.Args[0], "proctree-grandchild")
	out, err := gc.StdoutPipe()
	if err != nil || gc.Start() != nil {
		os.Exit(3)
	}
	line, _ := bufio.NewReader(out).ReadString('\n')
	fmt.Print(line)
	time.Sleep(time.Minute)
	os.Exit(0)
}

// grandchild listens on a loopback port until it is killed.
func grandchild() {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(3)
	}
	fmt.Println(l.Addr().String())
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	time.Sleep(time.Minute)
	os.Exit(0)
}

func TestTrackKillsTheTree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "proctree-child")
	cmd.WaitDelay = 5 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	afterStart, cleanup := Track(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := afterStart(); err != nil {
		t.Fatal(err)
	}
	// Only now, with the child tracked, does it start the grandchild.
	if _, err := fmt.Fprintln(stdin, "go"); err != nil {
		t.Fatal(err)
	}
	addr, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	addr = strings.TrimSpace(addr)
	if c, err := net.DialTimeout("tcp", addr, 5*time.Second); err != nil {
		t.Fatalf("the grandchild is not listening on %s: %v", addr, err)
	} else {
		_ = c.Close()
	}

	cancel()
	_ = cmd.Wait()
	cleanup()
	deadline := time.Now().Add(10 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", addr, time.Second)
		if err != nil {
			return // the grandchild is gone
		}
		_ = c.Close()
		if time.Now().After(deadline) {
			t.Fatalf("the grandchild still listens on %s after the tree was cancelled", addr)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
