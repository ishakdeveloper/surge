package domain

import (
	"sync"
	"time"
)

// Registry maps users to their connections.
//
// Sharded by user id rather than one map behind one mutex. At forty thousand
// connections the registry is read on every pushed message and written on every
// connect and disconnect, and a single lock makes it the contended thing in the
// process — which would be an odd way to lose to a problem the rest of the
// system solved by sharding.
type Registry struct {
	shards [registryShards]registryShard
}

const registryShards = 32

type registryShard struct {
	mu sync.RWMutex
	// One connection per user. A second device replaces the first rather than
	// joining it: two sockets for one driver means an offer delivered twice and
	// two answers to reconcile, which is a distributed systems problem invented
	// for no product reason.
	byUser map[string]*Connection
}

func NewRegistry() *Registry {
	registry := &Registry{}
	for i := range registry.shards {
		registry.shards[i].byUser = make(map[string]*Connection)
	}
	return registry
}

// fnv1a, inlined. The registry is on the push path and this is called once per
// message; importing hash/fnv to allocate a hasher per lookup would cost more
// than the hash.
func (r *Registry) shardFor(userID string) *registryShard {
	var hash uint32 = 2166136261
	for i := range len(userID) {
		hash ^= uint32(userID[i])
		hash *= 16777619
	}
	return &r.shards[hash%registryShards]
}

// Add registers a connection, replacing any the user already had.
func (r *Registry) Add(connection *Connection) {
	shard := r.shardFor(connection.UserID)

	shard.mu.Lock()
	previous, existed := shard.byUser[connection.UserID]
	shard.byUser[connection.UserID] = connection
	shard.mu.Unlock()

	if existed && previous != connection {
		previous.Close(EvictionReplaced)
	}
}

// Remove deregisters a connection, but only if it is still the current one.
//
// The check matters: a reconnecting client can register its new socket before
// the old one's reader notices it is gone, and an unconditional delete would
// then remove the connection that is actually live.
func (r *Registry) Remove(connection *Connection) {
	shard := r.shardFor(connection.UserID)

	shard.mu.Lock()
	if current, ok := shard.byUser[connection.UserID]; ok && current == connection {
		delete(shard.byUser, connection.UserID)
	}
	shard.mu.Unlock()
}

// Get returns a user's connection, if they have one.
func (r *Registry) Get(userID string) (*Connection, bool) {
	shard := r.shardFor(userID)

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	connection, ok := shard.byUser[userID]
	return connection, ok
}

// Len is the total connection count.
func (r *Registry) Len() int {
	total := 0
	for i := range r.shards {
		r.shards[i].mu.RLock()
		total += len(r.shards[i].byUser)
		r.shards[i].mu.RUnlock()
	}
	return total
}

// Backlog reports the deepest send queue in the registry.
//
// A max rather than an average, deliberately: the average stays near zero right
// up until the moment something breaks, while the maximum is the early warning.
func (r *Registry) Backlog() int {
	deepest := 0
	for i := range r.shards {
		r.shards[i].mu.RLock()
		for _, connection := range r.shards[i].byUser {
			if backlog := connection.Backlog(); backlog > deepest {
				deepest = backlog
			}
		}
		r.shards[i].mu.RUnlock()
	}
	return deepest
}

// EvictIdle closes connections that have not been heard from.
//
// A TCP connection to a phone that has driven into a tunnel stays open at the
// operating system level long after the phone is gone. Without this the
// registry accumulates ghosts, each holding a goroutine and a buffer, and the
// driver count on the dispatch console slowly becomes fiction.
func (r *Registry) EvictIdle(before time.Time) int {
	evicted := 0

	for i := range r.shards {
		r.shards[i].mu.RLock()
		var stale []*Connection
		for _, connection := range r.shards[i].byUser {
			if connection.LastSeen().Before(before) {
				stale = append(stale, connection)
			}
		}
		r.shards[i].mu.RUnlock()

		for _, connection := range stale {
			connection.Close(EvictionIdle)
			evicted++
		}
	}
	return evicted
}

// CloseAll ends every connection, returning how many.
//
// Needed for shutdown, and the reason is a trap worth naming: http.Server's
// graceful Shutdown waits for active handlers to return, and a WebSocket
// handler does not return until its connection closes. A gateway holding forty
// thousand sockets therefore stops listening, waits for clients that have no
// reason to leave, and hangs — alive, serving nothing, with the listener already
// gone so nothing even reports it as down.
//
// Closing the connections first is what makes the handlers return.
func (r *Registry) CloseAll(reason EvictionReason) int {
	closed := 0

	for i := range r.shards {
		r.shards[i].mu.Lock()
		connections := make([]*Connection, 0, len(r.shards[i].byUser))
		for _, connection := range r.shards[i].byUser {
			connections = append(connections, connection)
		}
		clear(r.shards[i].byUser)
		r.shards[i].mu.Unlock()

		for _, connection := range connections {
			connection.Close(reason)
			closed++
		}
	}
	return closed
}
