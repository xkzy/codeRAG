package runtime

import (
	"sync"
	"sync/atomic"
	"time"
)

type DropPolicy string

const (
	DropPolicyBlock   DropPolicy = "block"
	DropPolicyDrop   DropPolicy = "drop"
	DropPolicyOldest DropPolicy = "oldest"
)

type QueueConfig struct {
	Capacity       int
	DropPolicy     DropPolicy
	MetricsEnabled bool
}

type ObservationEvent struct {
	ID        string
	ProjectID string
	SessionID string
	ProcessID string
	Source    string
	Stream    string
	Timestamp time.Time
	RawText   []byte
	Metadata  map[string]string
}

type EventQueue struct {
	cfg        QueueConfig
	events     []ObservationEvent
	head       atomic.Int64
	tail       atomic.Int64
	count      atomic.Int64
	dropped    atomic.Int64
	mu         sync.Mutex
	notEmpty   chan struct{}
	closed     atomic.Bool
}

func NewEventQueue(cfg QueueConfig) *EventQueue {
	if cfg.Capacity <= 0 {
		cfg.Capacity = 10000
	}
	if cfg.DropPolicy == "" {
		cfg.DropPolicy = DropPolicyDrop
	}
	q := &EventQueue{
		cfg:      cfg,
		events:  make([]ObservationEvent, cfg.Capacity),
		notEmpty: make(chan struct{}, 1),
	}
	return q
}

func (q *EventQueue) Enqueue(event ObservationEvent) bool {
	if q.closed.Load() {
		return false
	}

	head := q.head.Load()
	tail := q.tail.Load()
	nextTail := (tail + 1) % int64(len(q.events))
	
	if nextTail == head {
		switch q.cfg.DropPolicy {
		case DropPolicyBlock:
			return false
		case DropPolicyDrop:
			q.dropped.Add(1)
			return false
		case DropPolicyOldest:
			q.dropped.Add(1)
			_, _ = q.dequeueOne()
		}
	}

	q.events[tail] = event
	q.tail.Store(nextTail)
	q.count.Add(1)

	select {
	case q.notEmpty <- struct{}{}:
	default:
	}
	return true
}

func (q *EventQueue) dequeueOne() (ObservationEvent, bool) {
	head := q.head.Load()
	tail := q.tail.Load()
	if head == tail {
		return ObservationEvent{}, false
	}
	event := q.events[head]
	q.events[head] = ObservationEvent{}
	q.head.Store((head + 1) % int64(len(q.events)))
	q.count.Add(-1)
	return event, true
}

func (q *EventQueue) DequeueAll() []ObservationEvent {
	q.mu.Lock()
	defer q.mu.Unlock()

	var events []ObservationEvent
	for {
		event, ok := q.dequeueOne()
		if !ok {
			break
		}
		events = append(events, event)
	}

	if len(events) > 0 {
		select {
		case <-q.notEmpty:
		default:
		}
	}
	return events
}

func (q *EventQueue) DequeueBlocking(timeout time.Duration) ([]ObservationEvent, bool) {
	deadline := time.Now().Add(timeout)
	
	for time.Now().Before(deadline) {
		events := q.DequeueAll()
		if len(events) > 0 {
			return events, true
		}
		if q.closed.Load() {
			return nil, false
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil, false
}

func (q *EventQueue) Close() {
	q.closed.Store(true)
	close(q.notEmpty)
}

func (q *EventQueue) Stats() QueueStats {
	return QueueStats{
		Count:     q.count.Load(),
		Capacity:  len(q.events),
		Dropped:   q.dropped.Load(),
		IsClosed:  q.closed.Load(),
	}
}

type QueueStats struct {
	Count    int64
	Capacity int
	Dropped  int64
	IsClosed bool
}

func (q *EventQueue) IsEmpty() bool {
	return q.count.Load() == 0
}
