// Package domain is what the gateway is: a registry of who is connected and a
// discipline for writing to them.
//
// The interesting problem is not the socket. It is that a gateway holds tens of
// thousands of connections, some of which are on a phone in a lift, and one
// stalled reader must not be allowed to consume the memory or the goroutine
// that the other 39,999 need.
package domain

import (
	"errors"
	"sync"
	"time"
)

// ErrSlowConsumer means a connection could not keep up and was given up on.
var ErrSlowConsumer = errors.New("gateway: slow consumer")

// ErrGone means the connection has already been closed.
var ErrGone = errors.New("gateway: connection is gone")

// EvictionReason is why a connection was closed, as a closed set so it can be a
// metric label.
type EvictionReason string

const (
	EvictionSlowConsumer EvictionReason = "slow_consumer"
	EvictionClientClosed EvictionReason = "client_closed"
	EvictionShutdown     EvictionReason = "shutdown"
	EvictionReplaced     EvictionReason = "replaced"
	EvictionIdle         EvictionReason = "idle"
)

// Connection is one client.
type Connection struct {
	ID     string
	UserID string
	Role   string

	// send is bounded, and the bound is the design.
	//
	// An unbounded queue turns a stalled reader into unbounded memory: the
	// gateway keeps accepting messages for a phone that will never read them
	// until the process dies. A bounded one turns the same situation into a
	// decision — drop this client — which is a choice the system can make in
	// microseconds and survive.
	send chan []byte

	closeOnce sync.Once
	closed    chan struct{}

	mu       sync.Mutex
	reason   EvictionReason
	lastSeen time.Time
}

// SendBuffer is how many messages may be outstanding for one connection.
//
// Sized for a burst, not a backlog. A driver receives an offer, a trip update
// and a handful of nearby-driver frames; sixty-four is generous for that and
// small enough that a client which has stopped reading is recognised within a
// second or two rather than a minute.
const SendBuffer = 64

func NewConnection(id, userID, role string, now time.Time) *Connection {
	return &Connection{
		ID:       id,
		UserID:   userID,
		Role:     role,
		send:     make(chan []byte, SendBuffer),
		closed:   make(chan struct{}),
		lastSeen: now,
	}
}

// Send queues a message, or reports that the client cannot keep up.
//
// Non-blocking on purpose. The alternative — blocking until there is room — is
// how one slow phone stops the goroutine that is fanning out to everybody else,
// and the failure spreads from one client to all of them.
func (c *Connection) Send(message []byte) error {
	select {
	case <-c.closed:
		return ErrGone
	default:
	}

	select {
	case c.send <- message:
		return nil
	case <-c.closed:
		return ErrGone
	default:
		// The queue is full. This client is not keeping up, and the only
		// options are to block (harming everyone), to grow (harming the
		// process) or to drop them (harming only them). We drop them.
		c.Close(EvictionSlowConsumer)
		return ErrSlowConsumer
	}
}

// Outbound is the queue the writer goroutine reads.
func (c *Connection) Outbound() <-chan []byte { return c.send }

// Closed fires once, when the connection is finished.
func (c *Connection) Closed() <-chan struct{} { return c.closed }

// Close ends the connection, recording why. Safe to call repeatedly and from
// several goroutines — the reader, the writer and the registry all race to it
// when a client disappears.
func (c *Connection) Close(reason EvictionReason) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.reason = reason
		c.mu.Unlock()
		close(c.closed)
	})
}

// Reason reports why the connection ended.
func (c *Connection) Reason() EvictionReason {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reason
}

// Touch records client activity, for idle eviction.
func (c *Connection) Touch(now time.Time) {
	c.mu.Lock()
	c.lastSeen = now
	c.mu.Unlock()
}

func (c *Connection) LastSeen() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastSeen
}

// Backlog is how many messages are waiting. The gauge that says whether the
// fleet is keeping up before anything is actually dropped.
func (c *Connection) Backlog() int { return len(c.send) }
