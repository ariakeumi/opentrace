package trace

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Manager struct {
	runner   *Runner
	ttl      time.Duration
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewManager(runner *Runner, ttl time.Duration) *Manager {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &Manager{
		runner:   runner,
		ttl:      ttl,
		sessions: make(map[string]*Session),
	}
}

func (m *Manager) Create(_ context.Context, req Request) (*Session, error) {
	normalized, timeout, err := req.Normalize(m.runner.DefaultTimeout())
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	session := newSession(newSessionID(), normalized, cancel)

	m.mu.Lock()
	m.sessions[session.ID()] = session
	m.mu.Unlock()

	go m.runSession(ctx, session)

	return session, nil
}

func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessions[id]
	return session, ok
}

func (m *Manager) runSession(ctx context.Context, session *Session) {
	session.Append(Event{
		Type:      "status",
		Timestamp: time.Now().UTC(),
		SessionID: session.ID(),
		Status:    "running",
	})

	exitCode, err := m.runner.Run(ctx, session.Request(), func(event Event) {
		event.SessionID = session.ID()
		session.Append(event)
	})

	switch {
	case err == nil:
		session.Append(Event{
			Type:      "status",
			Timestamp: time.Now().UTC(),
			SessionID: session.ID(),
			Status:    "completed",
			ExitCode:  exitCode,
		})
		session.Close("completed")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status := "stopped"
		if errors.Is(err, context.DeadlineExceeded) {
			status = "timeout"
		}
		session.Append(Event{
			Type:      "status",
			Timestamp: time.Now().UTC(),
			SessionID: session.ID(),
			Status:    status,
			ExitCode:  exitCode,
			Message:   err.Error(),
		})
		session.Close(status)
	default:
		session.Append(Event{
			Type:      "status",
			Timestamp: time.Now().UTC(),
			SessionID: session.ID(),
			Status:    "failed",
			ExitCode:  exitCode,
			Message:   fmt.Sprintf("nexttrace exited with error: %v", err),
		})
		session.Close("failed")
	}

	time.AfterFunc(m.ttl, func() {
		m.mu.Lock()
		delete(m.sessions, session.ID())
		m.mu.Unlock()
	})
}

type Session struct {
	id      string
	request Request
	cancel  context.CancelFunc

	mu          sync.RWMutex
	status      string
	history     []Event
	subscribers map[chan Event]struct{}
	closed      bool
}

func newSession(id string, req Request, cancel context.CancelFunc) *Session {
	return &Session{
		id:          id,
		request:     req,
		cancel:      cancel,
		status:      "pending",
		history:     make([]Event, 0, 32),
		subscribers: make(map[chan Event]struct{}),
	}
}

func (s *Session) ID() string {
	return s.id
}

func (s *Session) Status() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *Session) Request() Request {
	return s.request
}

func (s *Session) Stop() {
	s.cancel()
}

func (s *Session) Append(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.history = append(s.history, event)
	if event.Type == "status" && event.Status != "" {
		s.status = event.Status
	}

	for ch := range s.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func (s *Session) Close(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.status = status
	if s.closed {
		return
	}
	s.closed = true

	for ch := range s.subscribers {
		close(ch)
		delete(s.subscribers, ch)
	}
}

func (s *Session) ReplayAndSubscribe() ([]Event, <-chan Event, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	history := append([]Event(nil), s.history...)
	if s.closed {
		ch := make(chan Event)
		close(ch)
		return history, ch, func() {}
	}

	ch := make(chan Event, 64)
	s.subscribers[ch] = struct{}{}
	cancel := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.subscribers[ch]; ok {
			delete(s.subscribers, ch)
			close(ch)
		}
	}

	return history, ch, cancel
}

func newSessionID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
