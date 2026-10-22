package dap

import (
	"bytes"
	"context"
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hbaldwin98/reliab/internal/dap/daptest"
)

// Two transports: stdio, and tcp on a loopback port reliab picks, waiting
// for the ready pattern or retrying the connection for up to five seconds
// (knowledge/decisions/debug-adapter-seam.md, "Two transports").

// self starts this test binary as the fake adapter.
func self(args ...string) []string {
	return append([]string{os.Args[0], daptest.ChildArg}, args...)
}

func runStarted(t *testing.T, cfg AdapterConfig) *Transcript {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	tr := &Transcript{}
	err = Session(ctx, c, tr, baseOptions())
	done := make(chan struct{})
	go func() {
		_ = c.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Close did not end the adapter")
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Stops()) != 2 || tr.ExitCode != 0 {
		t.Fatalf("stops %d, end %+v, exit %d", len(tr.Stops()), tr.End, tr.ExitCode)
	}
	return tr
}

func TestStartStdio(t *testing.T) {
	var log bytes.Buffer
	runStarted(t, AdapterConfig{Argv: self("stdio", "basic"), Transport: TransportStdio, Log: &log})
	if !strings.Contains(log.String(), "fake adapter starting") {
		t.Errorf("the adapter's stderr did not reach the log: %q", log.String())
	}
}

func TestStartTCPWithReady(t *testing.T) {
	port, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	runStarted(t, AdapterConfig{
		Argv:      self("tcp", strconv.Itoa(port), "basic", "DAP server listening at 127.0.0.1"),
		Transport: TransportTCP, Port: port, Ready: regexp.MustCompile("DAP server listening"),
	})
}

func TestStartTCPRetriesWithoutReady(t *testing.T) {
	port, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	runStarted(t, AdapterConfig{Argv: self("tcp", strconv.Itoa(port), "basic"), Transport: TransportTCP, Port: port})
}

func TestStartFailureCarriesTheAdapterOutput(t *testing.T) {
	port, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for name, cfg := range map[string]AdapterConfig{
		"exits before ready":  {Argv: self("tcp", strconv.Itoa(port), "missing", "ready"), Transport: TransportTCP, Port: port, Ready: regexp.MustCompile("ready")},
		"exits before accept": {Argv: self("tcp", strconv.Itoa(port), "missing"), Transport: TransportTCP, Port: port},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Start(ctx, cfg)
			var se *StartError
			if !errors.As(err, &se) || !strings.Contains(se.Output, "bad tcp arguments") {
				t.Fatalf("err = %v, want a StartError with the adapter's output", err)
			}
		})
	}
	if _, err := Start(ctx, AdapterConfig{Argv: []string{"reliab-no-such-adapter-xyz"}, Transport: TransportStdio}); err == nil || !strings.Contains(err.Error(), "reliab-no-such-adapter-xyz") {
		t.Errorf("missing adapter: %v", err)
	}
	if _, err := Start(ctx, AdapterConfig{Argv: self("stdio", "basic"), Transport: "pipe"}); err == nil || !strings.Contains(err.Error(), "stdio or tcp") {
		t.Errorf("unknown transport: %v", err)
	}
	if _, err := Start(ctx, AdapterConfig{Transport: TransportStdio}); err == nil {
		t.Error("an empty command started")
	}
}

func TestStartReadyTimeout(t *testing.T) {
	port, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = Start(context.Background(), AdapterConfig{
		Argv:      self("tcp", strconv.Itoa(port), "basic", "something else"),
		Transport: TransportTCP, Port: port, Ready: regexp.MustCompile("never printed"), ReadyTimeout: 500 * time.Millisecond,
	})
	if err == nil || !strings.Contains(err.Error(), "did not print") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("took %s", time.Since(start))
	}
}

// The ready pattern may arrive split across writes, or without a newline.
func TestWatchWriterMatchesAcrossWrites(t *testing.T) {
	w := &watchWriter{re: regexp.MustCompile(`listening at: 127\.0\.0\.1:\d+`), ready: make(chan struct{})}
	_, _ = w.Write([]byte("API server listen"))
	_, _ = w.Write([]byte("ing at: 127.0"))
	select {
	case <-w.ready:
		t.Fatal("ready before the pattern was complete")
	default:
	}
	_, _ = w.Write([]byte(".0.1:4711"))
	select {
	case <-w.ready:
	default:
		t.Fatal("not ready after the pattern")
	}
	_, _ = w.Write([]byte("\nmore listening at: 127.0.0.1:1\n"))
	if !strings.HasSuffix(w.String(), "more listening at: 127.0.0.1:1") {
		t.Errorf("tail = %q", w.String())
	}
}
