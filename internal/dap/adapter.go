package dap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hbaldwin98/reliab/internal/proctree"
)

// Transports.
const (
	TransportStdio = "stdio"
	TransportTCP   = "tcp"
)

// ConnectRetry is how long a tcp adapter without a ready pattern has to
// accept a connection.
const ConnectRetry = 5 * time.Second

// AdapterConfig says how to start a debug adapter.
type AdapterConfig struct {
	// Argv is the expanded command line; Argv[0] is looked up on PATH.
	Argv []string
	// Transport is stdio or tcp.
	Transport string
	// Port is the loopback port a tcp adapter listens on.
	Port int
	// Ready, for tcp, matches the adapter's output once it listens.
	Ready *regexp.Regexp
	// ReadyTimeout bounds the wait for Ready (default 30s).
	ReadyTimeout time.Duration
	// Dir is the adapter's working directory.
	Dir string
	// Log, when set, receives the adapter's stderr (and stdout, for tcp).
	Log io.Writer
}

// outputCap is how much of the adapter's own output a client keeps to show
// when the adapter fails.
const outputCap = 64 << 10

// Start starts the adapter, connects to it, and returns a client whose
// Close kills the adapter's process tree.
func Start(ctx context.Context, cfg AdapterConfig) (*Client, error) {
	if len(cfg.Argv) == 0 {
		return nil, errors.New("debug.adapter.run is empty")
	}
	if cfg.ReadyTimeout == 0 {
		cfg.ReadyTimeout = 30 * time.Second
	}
	// The adapter outlives ctx's request deadlines; Close ends it.
	pctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	cmd := exec.CommandContext(pctx, cfg.Argv[0], cfg.Argv[1:]...)
	cmd.Dir = cfg.Dir
	cmd.WaitDelay = 2 * time.Second
	out := &watchWriter{log: cfg.Log, re: cfg.Ready, ready: make(chan struct{})}
	cmd.Stderr = out

	var stdin io.WriteCloser
	var stdout io.ReadCloser
	var err error
	switch cfg.Transport {
	case TransportStdio:
		if stdin, err = cmd.StdinPipe(); err != nil {
			cancel()
			return nil, err
		}
		if stdout, err = cmd.StdoutPipe(); err != nil {
			cancel()
			return nil, err
		}
	case TransportTCP:
		cmd.Stdout = out
	default:
		cancel()
		return nil, fmt.Errorf("debug.adapter.transport %q: want stdio or tcp", cfg.Transport)
	}

	afterStart, cleanup := proctree.Track(cmd)
	if err := cmd.Start(); err != nil {
		cancel()
		cleanup()
		return nil, fmt.Errorf("starting the adapter %s: %w", cfg.Argv[0], err)
	}
	if err := afterStart(); err != nil {
		fmt.Fprintf(out, "reliab: cannot track the adapter's process tree: %v\n", err)
	}
	exited := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(exited)
	}()
	stop := func() {
		// A well-behaved adapter exits after disconnect; give it a moment
		// before killing the tree.
		select {
		case <-exited:
		case <-time.After(2 * time.Second):
		}
		cancel()
		<-exited
		cleanup()
	}
	fail := func(err error) (*Client, error) {
		stop()
		return nil, &StartError{Err: err, Output: out.String()}
	}

	var conn io.ReadWriteCloser
	switch cfg.Transport {
	case TransportStdio:
		conn = &pipeConn{r: stdout, w: stdin}
	case TransportTCP:
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Port))
		if cfg.Ready != nil {
			t := time.NewTimer(cfg.ReadyTimeout)
			select {
			case <-out.ready:
				t.Stop()
			case <-exited:
				t.Stop()
				return fail(fmt.Errorf("the adapter exited before printing %q: %v", cfg.Ready, waitErr))
			case <-t.C:
				return fail(fmt.Errorf("the adapter did not print %q within %s", cfg.Ready, cfg.ReadyTimeout))
			case <-ctx.Done():
				t.Stop()
				return fail(ctx.Err())
			}
			// Ready means listening; allow a second for the accept loop.
			conn, err = dial(ctx, addr, time.Second, exited)
		} else {
			conn, err = dial(ctx, addr, ConnectRetry, exited)
		}
		if err != nil {
			return fail(err)
		}
	}
	c := NewClient(conn)
	c.shutdown = stop
	c.output = out
	return c, nil
}

// dial retries a loopback connection until it succeeds, the adapter exits,
// or the window passes.
func dial(ctx context.Context, addr string, window time.Duration, exited <-chan struct{}) (net.Conn, error) {
	deadline := time.Now().Add(window)
	var d net.Dialer
	for {
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			return conn, nil
		}
		select {
		case <-exited:
			return nil, fmt.Errorf("the adapter exited before accepting a connection on %s", addr)
		default:
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return nil, fmt.Errorf("cannot connect to the adapter on %s within %s: %w", addr, window, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// FreePort returns a loopback port nothing listens on right now.
func FreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// StartError is a failure to start or reach the adapter, with what the
// adapter printed.
type StartError struct {
	Err    error
	Output string
}

func (e *StartError) Error() string { return e.Err.Error() }
func (e *StartError) Unwrap() error { return e.Err }

// AdapterOutput returns the tail of what the adapter printed outside the
// protocol (its stderr, and its stdout for tcp).
func (c *Client) AdapterOutput() string {
	if c.output == nil {
		return ""
	}
	return c.output.String()
}

// pipeConn joins an adapter's stdout and stdin.
type pipeConn struct {
	r io.ReadCloser
	w io.WriteCloser
}

func (p *pipeConn) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *pipeConn) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *pipeConn) Close() error {
	werr := p.w.Close()
	rerr := p.r.Close()
	return errors.Join(werr, rerr)
}

// watchWriter keeps the tail of the adapter's output, copies it to the log,
// and closes ready the first time the ready pattern matches a line.
type watchWriter struct {
	mu    sync.Mutex
	log   io.Writer
	re    *regexp.Regexp
	ready chan struct{}
	once  sync.Once
	tail  []byte
	line  []byte
}

func (w *watchWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.log != nil {
		_, _ = w.log.Write(b)
	}
	w.tail = append(w.tail, b...)
	if len(w.tail) > outputCap {
		w.tail = w.tail[len(w.tail)-outputCap:]
	}
	if w.re != nil {
		w.line = append(w.line, b...)
		for {
			i := bytes.IndexByte(w.line, '\n')
			if i < 0 {
				break
			}
			w.match(w.line[:i])
			w.line = w.line[i+1:]
		}
		// An adapter may print its ready line without a newline.
		w.match(w.line)
		if len(w.line) > outputCap {
			w.line = w.line[len(w.line)-outputCap:]
		}
	}
	return len(b), nil
}

func (w *watchWriter) match(line []byte) {
	if w.re.Match(bytes.TrimRight(line, "\r")) {
		w.once.Do(func() { close(w.ready) })
	}
}

func (w *watchWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.TrimRight(string(w.tail), "\n")
}

// SplitCommand splits a command line into words. Whitespace separates
// words; single or double quotes group them. A backslash is an ordinary
// character, so Windows paths need no escaping.
func SplitCommand(line string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord := false
	var quote rune
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
			inWord = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unclosed %c quote in %q", quote, line)
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}
