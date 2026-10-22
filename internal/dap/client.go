// Package dap is reliab's generic Debug Adapter Protocol client: it starts
// the debug adapter the profile names, speaks DAP to it over stdio or a
// loopback TCP port, and drives the non-interactive run, attach, and probe
// sessions (knowledge/decisions/debug-adapter-seam.md). It knows no adapter:
// launch and attach arguments come from the profile verbatim.
package dap

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	godap "github.com/google/go-dap"
)

// DefaultRequestTimeout bounds one request's wait for its response.
const DefaultRequestTimeout = 30 * time.Second

// Event is an adapter event the session handles.
type Event struct {
	Name string
	Body json.RawMessage
}

// handledEvents are the events the client passes on; any other is counted.
var handledEvents = map[string]bool{
	"initialized": true, "stopped": true, "continued": true, "output": true,
	"exited": true, "terminated": true, "capabilities": true,
}

// ReverseRequest records a request the adapter sent the client.
type ReverseRequest struct {
	Command string `json:"command"`
	Seq     int    `json:"seq"`
}

// declinedReverse are the reverse requests phase 1 answers with an error
// and records: how js-debug-style adapters ask for a terminal or a child
// session.
var declinedReverse = map[string]bool{"runInTerminal": true, "startDebugging": true}

// envelope is the union of a request, response, and event.
type envelope struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	Command    string          `json:"command"`
	Event      string          `json:"event"`
	RequestSeq int             `json:"request_seq"`
	Success    bool            `json:"success"`
	Message    string          `json:"message"`
	Body       json.RawMessage `json:"body"`
}

// Response is an adapter's answer to one request.
type Response struct {
	Command string
	Success bool
	Message string
	Body    json.RawMessage
}

// Err returns the response's failure as an error, or nil on success.
func (r *Response) Err() error {
	if r.Success {
		return nil
	}
	var b godap.ErrorResponseBody
	_ = json.Unmarshal(r.Body, &b)
	msg := r.Message
	if b.Error != nil && b.Error.Format != "" {
		msg = b.Error.Format
		for k, v := range b.Error.Variables {
			msg = strings.ReplaceAll(msg, "{"+k+"}", v)
		}
	}
	if msg == "" {
		msg = "failed"
	}
	return fmt.Errorf("%s: %s", r.Command, msg)
}

// Pending is a sent request waiting for its response.
type Pending struct {
	Command string
	ch      chan *Response
	c       *Client
}

// Wait returns the response, or an error when ctx ends, the timeout passes
// (zero means the client's request timeout), or the adapter goes away.
func (p *Pending) Wait(ctx context.Context, timeout time.Duration) (*Response, error) {
	if timeout == 0 {
		timeout = p.c.RequestTimeout
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case r, ok := <-p.ch:
		if !ok {
			return nil, fmt.Errorf("%s: the adapter closed the connection: %w", p.Command, p.c.readErr())
		}
		return r, nil
	case <-t.C:
		return nil, fmt.Errorf("%s: no response within %s", p.Command, timeout)
	case <-ctx.Done():
		return nil, fmt.Errorf("%s: %w", p.Command, ctx.Err())
	}
}

// Client speaks DAP over one connection. Requests are correlated with
// their responses by seq; events arrive on Events in order.
type Client struct {
	// RequestTimeout bounds each request's wait (DefaultRequestTimeout).
	RequestTimeout time.Duration

	conn io.ReadWriteCloser
	wmu  sync.Mutex
	seq  int

	mu       sync.Mutex
	pending  map[int]*Pending
	reverse  []ReverseRequest
	ignored  map[string]int
	err      error
	closed   bool
	events   chan Event
	queue    []Event
	qcond    *sync.Cond
	done     chan struct{}
	stop     chan struct{}
	stopOnce sync.Once
	shutdown func()
	output   *watchWriter
}

// NewClient starts reading DAP messages from conn.
func NewClient(conn io.ReadWriteCloser) *Client {
	c := &Client{
		RequestTimeout: DefaultRequestTimeout,
		conn:           conn,
		pending:        map[int]*Pending{},
		ignored:        map[string]int{},
		events:         make(chan Event),
		done:           make(chan struct{}),
		stop:           make(chan struct{}),
	}
	c.qcond = sync.NewCond(&c.mu)
	go c.read()
	go c.deliver()
	return c
}

// Events delivers the handled events in arrival order; it closes when the
// connection ends and every event before the end was delivered.
func (c *Client) Events() <-chan Event { return c.events }

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.done }

// Send writes a request and returns its pending response.
func (c *Client) Send(command string, args any) (*Pending, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.seq++
	seq := c.seq
	p := &Pending{Command: command, ch: make(chan *Response, 1), c: c}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("%s: the adapter closed the connection: %w", command, c.err)
	}
	c.pending[seq] = p
	c.mu.Unlock()
	msg := map[string]any{"seq": seq, "type": "request", "command": command}
	if args != nil {
		msg["arguments"] = args
	}
	raw, err := json.Marshal(msg)
	if err == nil {
		err = godap.WriteBaseMessage(c.conn, raw)
	}
	if err != nil {
		c.mu.Lock()
		delete(c.pending, seq)
		c.mu.Unlock()
		return nil, fmt.Errorf("%s: %w", command, err)
	}
	return p, nil
}

// Do sends a request, waits for its response, and decodes a successful
// body into body (when not nil). A failed response is an error.
func (c *Client) Do(ctx context.Context, command string, args, body any) error {
	p, err := c.Send(command, args)
	if err != nil {
		return err
	}
	r, err := p.Wait(ctx, 0)
	if err != nil {
		return err
	}
	if err := r.Err(); err != nil {
		return err
	}
	if body != nil && len(r.Body) > 0 {
		if err := json.Unmarshal(r.Body, body); err != nil {
			return fmt.Errorf("%s: decoding the response: %w", command, err)
		}
	}
	return nil
}

// ReverseRequests returns the reverse requests the adapter sent that phase
// 1 declines, in order.
func (c *Client) ReverseRequests() []ReverseRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ReverseRequest(nil), c.reverse...)
}

// Ignored counts the messages the client did not handle, keyed
// "event <name>", "request <command>", or "response <command>".
func (c *Client) Ignored() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]int, len(c.ignored))
	for k, v := range c.ignored {
		out[k] = v
	}
	return out
}

// IgnoredText lists Ignored as "name xN" in name order.
func IgnoredText(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = fmt.Sprintf("%s x%d", k, m[k])
	}
	return out
}

// Close ends the connection and, for a client that started its adapter,
// kills the adapter's process tree.
func (c *Client) Close() error {
	c.stopOnce.Do(func() { close(c.stop) })
	err := c.conn.Close()
	if c.shutdown != nil {
		c.shutdown()
	}
	<-c.done
	return err
}

func (c *Client) readErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		return io.EOF
	}
	return c.err
}

func (c *Client) read() {
	r := bufio.NewReader(c.conn)
	var err error
	for {
		var raw []byte
		raw, err = godap.ReadBaseMessage(r)
		if err != nil {
			break
		}
		var m envelope
		if json.Unmarshal(raw, &m) != nil {
			c.count("malformed message")
			continue
		}
		switch m.Type {
		case "response":
			c.mu.Lock()
			p := c.pending[m.RequestSeq]
			delete(c.pending, m.RequestSeq)
			if p == nil {
				c.ignored["response "+m.Command]++
			}
			c.mu.Unlock()
			if p != nil {
				p.ch <- &Response{Command: m.Command, Success: m.Success, Message: m.Message, Body: m.Body}
			}
		case "event":
			if !handledEvents[m.Event] {
				c.count("event " + m.Event)
				continue
			}
			c.mu.Lock()
			c.queue = append(c.queue, Event{Name: m.Event, Body: m.Body})
			c.qcond.Signal()
			c.mu.Unlock()
		case "request":
			c.mu.Lock()
			if declinedReverse[m.Command] {
				c.reverse = append(c.reverse, ReverseRequest{Command: m.Command, Seq: m.Seq})
			} else {
				c.ignored["request "+m.Command]++
			}
			c.mu.Unlock()
			// Every reverse request gets an answer, so the adapter does not
			// wait on one forever. It is written aside so the reader never
			// blocks on a write while the adapter blocks on its own.
			go c.respondError(m.Seq, m.Command, "reliab does not support "+m.Command+" yet")
		default:
			c.count("message " + m.Type)
		}
	}
	c.mu.Lock()
	c.closed = true
	if !errors.Is(err, io.EOF) {
		c.err = err
	}
	for seq, p := range c.pending {
		close(p.ch)
		delete(c.pending, seq)
	}
	c.qcond.Signal()
	c.mu.Unlock()
	close(c.done)
}

func (c *Client) count(key string) {
	c.mu.Lock()
	c.ignored[key]++
	c.mu.Unlock()
}

func (c *Client) respondError(requestSeq int, command, msg string) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.seq++
	raw, err := json.Marshal(map[string]any{
		"seq": c.seq, "type": "response", "request_seq": requestSeq, "command": command,
		"success": false, "message": msg,
		"body": map[string]any{"error": map[string]any{"id": 1, "format": msg, "showUser": false}},
	})
	if err == nil {
		_ = godap.WriteBaseMessage(c.conn, raw)
	}
}

// deliver moves queued events to the Events channel, so the reader never
// blocks on a slow consumer and no event is dropped.
func (c *Client) deliver() {
	defer close(c.events)
	for {
		c.mu.Lock()
		for len(c.queue) == 0 && !c.closed {
			c.qcond.Wait()
		}
		if len(c.queue) == 0 {
			c.mu.Unlock()
			return
		}
		ev := c.queue[0]
		c.queue = c.queue[1:]
		c.mu.Unlock()
		select {
		case c.events <- ev:
		case <-c.stop:
			return
		}
	}
}
