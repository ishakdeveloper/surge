package sim

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/ishakdeveloper/surge/shared/authz"
	"github.com/ishakdeveloper/surge/shared/wire"
)

// WSTransport puts the fleet on real WebSockets.
//
// This is the swap the Transport interface existed for. Phase 1's simulator
// produced straight to Kafka so a load rig could exist before the product did;
// this one opens a connection per driver through the actual gateway, which is
// the only way to exercise the things the gateway does — the connection
// registry, the bounded send queues, slow-consumer eviction, and the difference
// between forty thousand sockets and forty thousand map entries.
//
// The driver behaviour is unchanged. Same routes, same speeds, same accept rate
// and thinking time, because otherwise a difference in the numbers could not be
// attributed to the transport.
type WSTransport struct {
	url    string
	signer *authz.HMAC
	policy *OfferPolicy
	hooks  WSHooks

	// dialing bounds how many connections are being established at once.
	//
	// Ten thousand simultaneous handshakes is a synthetic thundering herd that
	// measures the gateway's accept path rather than its steady state — and it
	// is not what a real fleet does, since drivers come online over hours.
	dialing chan struct{}

	mu    sync.RWMutex
	conns map[string]*driverConn
}

type WSHooks struct {
	OnConnected func()
	// OnDisconnected carries the underlying error, not just a label. A reason
	// of "read_failed" says nothing about whether the peer went away, the
	// deadline passed, or a limit was hit.
	OnDisconnected func(reason string, err error)
	OnDialFailed   func()
	OnOffer        func()
	OnReply        func(accepted bool)
	OnDuplicate    func(tripID string)
}

// driverConn is one driver's socket.
type driverConn struct {
	socket *websocket.Conn
	cancel context.CancelFunc

	// writes are serialised: the driver's own goroutine sends pings while the
	// reader's goroutine sends offer replies, and a WebSocket permits one
	// writer at a time.
	write sync.Mutex

	closeOnce sync.Once
	closed    chan struct{}
}

func NewWSTransport(url, secret, audience string, policy *OfferPolicy, dialConcurrency int, hooks WSHooks) (*WSTransport, error) {
	signer, err := authz.NewHMAC(secret, "surge-sim", audience)
	if err != nil {
		return nil, err
	}
	if dialConcurrency < 1 {
		dialConcurrency = 1
	}

	return &WSTransport{
		url:     url,
		signer:  signer,
		policy:  policy,
		hooks:   hooks,
		dialing: make(chan struct{}, dialConcurrency),
		conns:   make(map[string]*driverConn),
	}, nil
}

// Ping sends a position, connecting first if this driver has no socket yet.
//
// Connect-on-first-ping rather than up front, so the fleet comes online at the
// rate the simulator scales rather than all at once — which is both gentler on
// the gateway and closer to how drivers actually start their shift.
func (t *WSTransport) Ping(ctx context.Context, ping wire.DriverPing) error {
	conn, err := t.connectionFor(ctx, ping.DriverID)
	if err != nil {
		return err
	}

	message, err := json.Marshal(wire.ClientMessage{Tag: wire.TagClientPing, Ping: &ping})
	if err != nil {
		return fmt.Errorf("sim: encode ping: %w", err)
	}

	return t.send(ctx, conn, ping.DriverID, message)
}

func (t *WSTransport) send(ctx context.Context, conn *driverConn, driverID string, message []byte) error {
	conn.write.Lock()
	defer conn.write.Unlock()

	select {
	case <-conn.closed:
		return fmt.Errorf("sim: connection for %s is closed", driverID)
	default:
	}

	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := conn.socket.Write(writeCtx, websocket.MessageText, message); err != nil {
		t.drop(driverID, conn, "write_failed", err)
		return fmt.Errorf("sim: write for %s: %w", driverID, err)
	}
	return nil
}

func (t *WSTransport) connectionFor(ctx context.Context, driverID string) (*driverConn, error) {
	t.mu.RLock()
	existing, ok := t.conns[driverID]
	t.mu.RUnlock()

	if ok {
		select {
		case <-existing.closed:
			// Dropped since; fall through and redial. A driver whose socket
			// died should come back, exactly as a phone reconnecting would.
		default:
			return existing, nil
		}
	}

	select {
	case t.dialing <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-t.dialing }()

	// Somebody else may have connected this driver while we waited for a slot.
	t.mu.RLock()
	existing, ok = t.conns[driverID]
	t.mu.RUnlock()
	if ok {
		select {
		case <-existing.closed:
		default:
			return existing, nil
		}
	}

	return t.dial(ctx, driverID)
}

func (t *WSTransport) dial(ctx context.Context, driverID string) (*driverConn, error) {
	// Every simulated driver gets its own identity, minted rather than
	// registered: ten thousand better-auth accounts to run a load test would be
	// ten thousand password hashes for no benefit. The gateway accepts these
	// only when SIM_ENABLED is set.
	token, err := t.signer.Sign(authz.Identity{
		UserID: driverID,
		Email:  driverID + "@sim.surge",
		Role:   authz.RoleDriver,
	}, time.Hour)
	if err != nil {
		return nil, err
	}

	dialCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	socket, _, err := websocket.Dial(dialCtx, t.url+"?token="+token, &websocket.DialOptions{
		HTTPClient: &http.Client{Timeout: 20 * time.Second},
	})
	if err != nil {
		if t.hooks.OnDialFailed != nil {
			t.hooks.OnDialFailed()
		}
		return nil, fmt.Errorf("sim: dial for %s: %w", driverID, err)
	}

	// A driver receives an offer, a trip update, and little else. The default
	// 32KiB is generous for that, and a large limit would let one misbehaving
	// gateway response allocate freely across ten thousand connections.
	socket.SetReadLimit(64 << 10)

	readCtx, cancelRead := context.WithCancel(ctx)
	conn := &driverConn{socket: socket, cancel: cancelRead, closed: make(chan struct{})}

	t.mu.Lock()
	t.conns[driverID] = conn
	t.mu.Unlock()

	if t.hooks.OnConnected != nil {
		t.hooks.OnConnected()
	}

	go t.read(readCtx, conn, driverID)
	return conn, nil
}

// read handles everything the gateway pushes to this driver.
func (t *WSTransport) read(ctx context.Context, conn *driverConn, driverID string) {
	for {
		_, data, err := conn.socket.Read(ctx)
		if err != nil {
			t.drop(driverID, conn, "read_failed", err)
			return
		}

		var message wire.ServerMessage
		if err := json.Unmarshal(data, &message); err != nil {
			continue
		}
		if message.Tag != wire.TagOffer || message.Offer == nil {
			continue
		}

		if t.hooks.OnOffer != nil {
			t.hooks.OnOffer()
		}

		offer := *message.Offer
		decision := t.policy.Decide(offer)

		if decision.Duplicate {
			if t.hooks.OnDuplicate != nil {
				t.hooks.OnDuplicate(offer.TripID)
			}
			go t.answer(ctx, conn, driverID, offer, false, 0)
			continue
		}

		go t.answer(ctx, conn, driverID, offer, decision.Accept, decision.Delay)
	}
}

func (t *WSTransport) answer(ctx context.Context, conn *driverConn, driverID string, offer wire.Offer, accept bool, delay time.Duration) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(delay):
	}

	message, err := json.Marshal(wire.ClientMessage{
		Tag: wire.TagClientOfferReply,
		// Echoed back, not chosen: only the shard that made the reservation can
		// resolve it, and by now the driver may be in a different cell owned by
		// a different instance.
		ReplyCell: offer.ReplyCell,
		Reply: &wire.OfferRepliedPayload{
			TripID:   offer.TripID,
			DriverID: driverID,
			Accepted: accept,
		},
	})
	if err != nil {
		return
	}

	if err := t.send(ctx, conn, driverID, message); err != nil {
		// The reply never left. Deliberately no Commit: the matcher will not
		// learn of this acceptance, will time the offer out, and will re-offer
		// the trip to somebody else — which is correct behaviour, not a double
		// dispatch. Recording the acceptance here would make that correct retry
		// look like the bug this metric exists to catch.
		return
	}

	if accept && !t.policy.Commit(offer) {
		// Committed only once the reply is on the wire. Somebody took this trip
		// while the driver was thinking, and both answers are now in flight —
		// which is the race worth counting.
		if t.hooks.OnDuplicate != nil {
			t.hooks.OnDuplicate(offer.TripID)
		}
	}

	if t.hooks.OnReply != nil {
		t.hooks.OnReply(accept)
	}
}

func (t *WSTransport) drop(driverID string, conn *driverConn, reason string, cause error) {
	conn.closeOnce.Do(func() {
		close(conn.closed)
		conn.cancel()
		_ = conn.socket.Close(websocket.StatusNormalClosure, reason)

		t.mu.Lock()
		// Only if it is still the current one: the driver may already have
		// redialled, and removing the live socket would strand them.
		if current, ok := t.conns[driverID]; ok && current == conn {
			delete(t.conns, driverID)
		}
		t.mu.Unlock()

		if t.hooks.OnDisconnected != nil {
			t.hooks.OnDisconnected(reason, cause)
		}
	})
}

// Connections is how many sockets are currently open.
func (t *WSTransport) Connections() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.conns)
}

// Flush is a no-op: a WebSocket write has already left by the time it returns.
func (t *WSTransport) Flush(context.Context) error { return nil }

func (t *WSTransport) Close() {
	t.mu.Lock()
	conns := make(map[string]*driverConn, len(t.conns))
	for id, conn := range t.conns {
		conns[id] = conn
	}
	t.mu.Unlock()

	for id, conn := range conns {
		t.drop(id, conn, "shutdown", nil)
	}
}
