package native

import (
	"context"
	"errors"
	"sync"
)

// CaptureControlAck describes durable audio at an acknowledged device boundary.
// It never substitutes wall-clock elapsed time for captured samples.
type CaptureControlAck struct {
	ControlID       string `json:"controlId"`
	Action          string `json:"action"`
	State           string `json:"state"`
	Bytes           int64  `json:"bytes"`
	CapturedSamples int64  `json:"capturedSamples"`
	DurationMS      int64  `json:"durationMs"`
}
type pendingCaptureControl struct {
	action string
	result chan CaptureControlAck
}

// One controller belongs to one Capture invocation. Unknown outcomes must be
// recovered from the capture journal, never optimistically represented as paused.
type CaptureController struct {
	mu        sync.Mutex
	ready     chan struct{}
	done      chan struct{}
	closed    bool
	requestID string
	write     func(any) error
	pending   map[string]pendingCaptureControl
}

func NewCaptureController() *CaptureController {
	return &CaptureController{ready: make(chan struct{}), done: make(chan struct{}), pending: map[string]pendingCaptureControl{}}
}
func (c *CaptureController) attach(id string, write func(any) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.write != nil {
		return
	}
	c.requestID = id
	c.write = write
	close(c.ready)
}
func (c *CaptureController) finish() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.done)
	}
}
func (c *CaptureController) Command(ctx context.Context, action string) (CaptureControlAck, error) {
	var empty CaptureControlAck
	if action != "pause" && action != "resume" && action != "stop" {
		return empty, errors.New("invalid capture control")
	}
	select {
	case <-ctx.Done():
		return empty, ctx.Err()
	case <-c.done:
		return empty, errors.New("capture has finished")
	case <-c.ready:
	}
	c.mu.Lock()
	if c.closed || len(c.pending) >= 16 {
		c.mu.Unlock()
		return empty, errors.New("capture unavailable or too many pending controls")
	}
	id := NewID()
	result := make(chan CaptureControlAck, 1)
	c.pending[id] = pendingCaptureControl{action: action, result: result}
	write, requestID := c.write, c.requestID
	c.mu.Unlock()
	// A timed-out command stays registered until its acknowledgement arrives.
	// Otherwise a valid late acknowledgement would terminate recoverable capture.
	if err := write(map[string]any{"kind": "control", "requestId": requestID, "controlId": id, "action": action}); err != nil {
		return empty, err
	}
	select {
	case ack := <-result:
		return ack, nil
	case <-ctx.Done():
		return empty, errors.New("capture control outcome unknown; inspect recording before retrying")
	case <-c.done:
		select {
		case ack := <-result:
			return ack, nil
		default:
			return empty, errors.New("capture ended before acknowledging control")
		}
	}
}
func (c *CaptureController) acknowledge(ack CaptureControlAck) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.pending[ack.ControlID]
	states := map[string]string{"pause": "paused", "resume": "recording", "stop": "stopped"}
	if !ok || p.action != ack.Action || states[ack.Action] != ack.State {
		return false
	}
	delete(c.pending, ack.ControlID)
	p.result <- ack
	return true
}

func (c *CaptureController) accepts(ack CaptureControlAck) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.pending[ack.ControlID]
	return ok && p.action == ack.Action && map[string]string{"pause": "paused", "resume": "recording", "stop": "stopped"}[ack.Action] == ack.State
}
