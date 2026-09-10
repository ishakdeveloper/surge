package domain_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/gateway/internal/domain"
)

var now = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

// The property that keeps one bad client from taking down the gateway.
//
// A phone in a lift stops reading. Its queue fills. The only options are to
// block the fanout goroutine (harming all 40,000 other connections), to grow
// the queue (harming the process), or to drop this one client. Dropping must be
// immediate and must not block.
func TestSlowConsumerIsEvictedRatherThanBlocking(t *testing.T) {
	connection := domain.NewConnection("c1", "user-1", "driver", now)

	// Fill the queue without anybody reading.
	for i := range domain.SendBuffer {
		if err := connection.Send([]byte("message")); err != nil {
			t.Fatalf("send %d failed early: %v", i, err)
		}
	}

	done := make(chan error, 1)
	go func() { done <- connection.Send([]byte("one too many")) }()

	select {
	case err := <-done:
		if !errors.Is(err, domain.ErrSlowConsumer) {
			t.Fatalf("want ErrSlowConsumer, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Send blocked on a full queue; one stalled client would stall the fanout")
	}

	if connection.Reason() != domain.EvictionSlowConsumer {
		t.Errorf("eviction reason %q, want %q", connection.Reason(), domain.EvictionSlowConsumer)
	}

	select {
	case <-connection.Closed():
	default:
		t.Error("the connection was not closed")
	}
}

// Close is raced by the reader, the writer and the registry whenever a client
// disappears, so it has to be safe from all of them and record only the first
// reason.
func TestCloseIsIdempotentUnderRace(t *testing.T) {
	connection := domain.NewConnection("c1", "user-1", "driver", now)

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			connection.Close(domain.EvictionClientClosed)
		}()
	}
	wg.Wait()

	if connection.Reason() != domain.EvictionClientClosed {
		t.Errorf("reason %q", connection.Reason())
	}
}

// One connection per user: a second device replaces the first rather than
// joining it. Two sockets for one driver means an offer delivered twice and two
// answers to reconcile.
func TestReconnectReplacesTheOldConnection(t *testing.T) {
	registry := domain.NewRegistry()

	first := domain.NewConnection("c1", "user-1", "driver", now)
	second := domain.NewConnection("c2", "user-1", "driver", now)

	registry.Add(first)
	registry.Add(second)

	if registry.Len() != 1 {
		t.Fatalf("registry holds %d connections for one user", registry.Len())
	}

	current, ok := registry.Get("user-1")
	if !ok || current != second {
		t.Fatal("the newer connection did not win")
	}

	select {
	case <-first.Closed():
		if first.Reason() != domain.EvictionReplaced {
			t.Errorf("reason %q, want %q", first.Reason(), domain.EvictionReplaced)
		}
	default:
		t.Error("the replaced connection was left open")
	}
}

// A reconnecting client can register its new socket before the old one's reader
// notices it is gone. An unconditional delete would then remove the live one.
func TestRemoveOnlyDropsTheCurrentConnection(t *testing.T) {
	registry := domain.NewRegistry()

	old := domain.NewConnection("c1", "user-1", "driver", now)
	fresh := domain.NewConnection("c2", "user-1", "driver", now)

	registry.Add(old)
	registry.Add(fresh)

	// The old connection's reader finally notices and cleans up.
	registry.Remove(old)

	current, ok := registry.Get("user-1")
	if !ok {
		t.Fatal("cleaning up the stale connection removed the live one")
	}
	if current != fresh {
		t.Error("the wrong connection survived")
	}
}

// A TCP connection to a phone that drove into a tunnel stays open long after
// the phone is gone. Without idle eviction the registry fills with ghosts and
// the driver count becomes fiction.
func TestEvictIdle(t *testing.T) {
	registry := domain.NewRegistry()

	live := domain.NewConnection("c1", "user-1", "driver", now)
	ghost := domain.NewConnection("c2", "user-2", "driver", now.Add(-time.Hour))

	registry.Add(live)
	registry.Add(ghost)

	if evicted := registry.EvictIdle(now.Add(-time.Minute)); evicted != 1 {
		t.Fatalf("evicted %d, want 1", evicted)
	}

	if ghost.Reason() != domain.EvictionIdle {
		t.Errorf("ghost reason %q", ghost.Reason())
	}
	select {
	case <-live.Closed():
		t.Error("the live connection was evicted")
	default:
	}
}

// Backlog reports the deepest queue, not the average: the average stays near
// zero right up until something breaks.
func TestBacklogReportsTheWorstConnection(t *testing.T) {
	registry := domain.NewRegistry()

	quiet := domain.NewConnection("c1", "user-1", "driver", now)
	struggling := domain.NewConnection("c2", "user-2", "driver", now)
	registry.Add(quiet)
	registry.Add(struggling)

	_ = quiet.Send([]byte("one"))
	for range 10 {
		_ = struggling.Send([]byte("message"))
	}

	if got := registry.Backlog(); got != 10 {
		t.Errorf("backlog %d, want the deepest queue (10)", got)
	}
}

func TestSendToAClosedConnection(t *testing.T) {
	connection := domain.NewConnection("c1", "user-1", "driver", now)
	connection.Close(domain.EvictionShutdown)

	if err := connection.Send([]byte("hello")); !errors.Is(err, domain.ErrGone) {
		t.Errorf("want ErrGone, got %v", err)
	}
}

// The shutdown trap, pinned.
//
// http.Server.Shutdown waits for active handlers to return, and a WebSocket
// handler returns only when its connection closes. A gateway that shuts down
// without closing its connections first stops listening and then waits forever
// for clients who have no reason to leave — alive, unreachable, and with the
// listener already gone so nothing reports it as down. That happened, at five
// thousand connections.
func TestCloseAllReleasesEveryHandler(t *testing.T) {
	registry := domain.NewRegistry()

	const held = 200
	connections := make([]*domain.Connection, held)
	for i := range held {
		connections[i] = domain.NewConnection(
			"c"+string(rune('a'+i%26))+string(rune('0'+i/26)),
			"user-"+string(rune('a'+i%26))+string(rune('0'+i/26)),
			"driver", now)
		registry.Add(connections[i])
	}

	if registry.Len() != held {
		t.Fatalf("setup: registry holds %d, want %d", registry.Len(), held)
	}

	if closed := registry.CloseAll(domain.EvictionShutdown); closed != held {
		t.Errorf("closed %d, want %d", closed, held)
	}

	for i, connection := range connections {
		select {
		case <-connection.Closed():
		default:
			t.Fatalf("connection %d was left open; its handler would never return", i)
		}
		if connection.Reason() != domain.EvictionShutdown {
			t.Fatalf("connection %d closed for %q", i, connection.Reason())
		}
	}

	if registry.Len() != 0 {
		t.Errorf("registry still holds %d connections", registry.Len())
	}
}
