package native

import (
	"context"
	"sync"
)

type Priority int

const (
	PriorityRecording Priority = iota
	PriorityChat
	PriorityTranscription
	PriorityBackground
)

type waitSlot struct {
	priority Priority
	ready    chan struct{}
	cancel   context.CancelFunc
}

// Scheduler admits one heavy worker. Recording preempts lower-priority work;
// editing suppresses background work. The durable job owner classifies preemption
// as interruption and retries from its checkpoint instead of committing late output.
type Scheduler struct {
	mu      sync.Mutex
	active  *waitSlot
	waiting []*waitSlot
	editing bool
}

func (s *Scheduler) dispatch() {
	if s.active != nil {
		return
	}
	best := -1
	for i, w := range s.waiting {
		if s.editing && w.priority >= PriorityBackground {
			continue
		}
		if best < 0 || w.priority < s.waiting[best].priority {
			best = i
		}
	}
	if best < 0 {
		return
	}
	w := s.waiting[best]
	s.waiting = append(s.waiting[:best], s.waiting[best+1:]...)
	s.active = w
	close(w.ready)
}
func (s *Scheduler) Enter(ctx context.Context, p Priority) (context.Context, func(), error) {
	run, cancel := context.WithCancel(ctx)
	w := &waitSlot{p, make(chan struct{}), cancel}
	s.mu.Lock()
	s.waiting = append(s.waiting, w)
	if p == PriorityRecording && s.active != nil && s.active.priority > p {
		s.active.cancel()
	}
	s.dispatch()
	s.mu.Unlock()
	select {
	case <-w.ready:
		if e := ctx.Err(); e != nil {
			s.release(w)
			return nil, nil, e
		}
		return run, func() { s.release(w) }, nil
	case <-ctx.Done():
		s.mu.Lock()
		for i, item := range s.waiting {
			if item == w {
				s.waiting = append(s.waiting[:i], s.waiting[i+1:]...)
				break
			}
		}
		if s.active == w {
			s.active = nil
		}
		cancel()
		s.dispatch()
		s.mu.Unlock()
		return nil, nil, ctx.Err()
	}
}
func (s *Scheduler) release(w *waitSlot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.cancel()
	if s.active == w {
		s.active = nil
	}
	s.dispatch()
}
func (s *Scheduler) SetEditing(busy bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.editing = busy
	if busy && s.active != nil && s.active.priority >= PriorityBackground {
		s.active.cancel()
	}
	s.dispatch()
}
