// Package ws is the gateway's stateful edge.
//
// Two goroutines per connection and no more: one reading, one writing. The
// writer exists so that a slow client blocks only itself — writing to a socket
// from whichever goroutine happens to have a message is how a fanout ends up
// waiting on a phone in a lift.
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/ishakdeveloper/surge/services/gateway/internal/domain"
	"github.com/ishakdeveloper/surge/shared/authz"
	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/kafkax"
	trippb "github.com/ishakdeveloper/surge/shared/proto/trip"
	"github.com/ishakdeveloper/surge/shared/tracing"
	"github.com/ishakdeveloper/surge/shared/wire"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Hooks are the observability seams.
type Hooks struct {
	OnConnect    func(role string)
	OnDisconnect func(role string, reason domain.EvictionReason)
	OnInbound    func(tag string)
	OnRejected   func(reason string)
	OnPushed     func()
	OnPushFailed func(reason string)
	// OnProduceError carries the real error. A "produce" label told us twelve
	// thousand records had failed and nothing about why.
	OnProduceError func(topic string, err error)
}

type Hub struct {
	registry *domain.Registry
	verifier authz.Verifier
	producer *kgo.Client
	hooks    Hooks

	// origins the browser may connect from. Empty means same-origin only.
	origins []string

	// fleet is the picture the console draws; trips is how a follow request
	// is checked against who is asking.
	fleet *domain.Fleet
	trips trippb.TripServiceClient
	// eta predicts how long a followed driver is from the pickup.
	eta *domain.EtaModel

	// Subscriptions, by connection id. Held here rather than on the connection
	// because they are what the fan-out iterates; a connection that closes
	// takes its subscriptions with it, in forget.
	mu        sync.Mutex
	watchers  map[string]watcher
	followers map[string]follower
}

type watcher struct {
	connection *domain.Connection
	viewport   wire.Viewport
}

type follower struct {
	connection *domain.Connection
	tripID     string
	driverID   string
	pickup     geo.Point
}

func NewHub(
	registry *domain.Registry, verifier authz.Verifier, producer *kgo.Client, origins []string,
	fleet *domain.Fleet, trips trippb.TripServiceClient, eta *domain.EtaModel, hooks Hooks,
) *Hub {
	return &Hub{
		registry: registry, verifier: verifier, producer: producer, origins: origins, hooks: hooks,
		fleet: fleet, trips: trips, eta: eta,
		watchers: map[string]watcher{}, followers: map[string]follower{},
	}
}

// heartbeat bounds how long a dead connection can look alive.
const (
	heartbeatInterval = 20 * time.Second
	idleTimeout       = 90 * time.Second
	writeTimeout      = 10 * time.Second
)

// Handler accepts an upgrade.
func (h *Hub) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Authenticated before the upgrade, not after. Accepting the socket
		// first and closing it on a bad token means an unauthenticated client
		// can hold a connection slot for as long as it takes them to not send
		// a token.
		identity, err := h.verifier.Verify(r.Context(), authz.BearerToken(r))
		if err != nil {
			if h.hooks.OnRejected != nil {
				h.hooks.OnRejected("unauthenticated")
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		socket, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: h.origins,
			// Compression off: these are small JSON frames many times a second,
			// and per-message deflate costs more CPU than it saves bytes at
			// this size.
			CompressionMode: websocket.CompressionDisabled,
		})
		if err != nil {
			return
		}

		h.serve(r.Context(), socket, identity)
	}
}

func (h *Hub) serve(ctx context.Context, socket *websocket.Conn, identity authz.Identity) {
	connection := domain.NewConnection(uuid.NewString(), identity.UserID, string(identity.Role), time.Now())

	h.registry.Add(connection)
	if h.hooks.OnConnect != nil {
		h.hooks.OnConnect(string(identity.Role))
	}

	defer func() {
		h.forget(connection.ID)
		h.registry.Remove(connection)
		reason := connection.Reason()
		if reason == "" {
			reason = domain.EvictionClientClosed
		}
		if h.hooks.OnDisconnect != nil {
			h.hooks.OnDisconnect(string(identity.Role), reason)
		}
		// A slow consumer is closed with a status that says so, rather than
		// looking to the client like a network blip it should retry into.
		status := websocket.StatusNormalClosure
		if reason == domain.EvictionSlowConsumer {
			status = websocket.StatusPolicyViolation
		}
		_ = socket.Close(status, string(reason))
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go h.write(ctx, socket, connection)

	welcome, _ := json.Marshal(wire.ServerMessage{Tag: wire.TagServerWelcome})
	_ = connection.Send(welcome)

	h.read(ctx, socket, connection, identity)
}

// read pumps inbound frames until the client goes away.
func (h *Hub) read(ctx context.Context, socket *websocket.Conn, connection *domain.Connection, identity authz.Identity) {
	for {
		// A read deadline is what turns a phone that drove into a tunnel from a
		// goroutine held forever into a disconnect.
		readCtx, cancel := context.WithTimeout(ctx, idleTimeout)
		_, data, err := socket.Read(readCtx)
		cancel()

		if err != nil {
			connection.Close(domain.EvictionClientClosed)
			return
		}

		connection.Touch(time.Now())

		var message wire.ClientMessage
		if err := json.Unmarshal(data, &message); err != nil {
			if h.hooks.OnRejected != nil {
				h.hooks.OnRejected("malformed")
			}
			continue
		}

		if h.hooks.OnInbound != nil {
			h.hooks.OnInbound(message.Tag)
		}

		h.dispatch(ctx, connection, message, identity)

		select {
		case <-connection.Closed():
			return
		default:
		}
	}
}

// dispatch turns a client message into a record on the bus.
func (h *Hub) dispatch(ctx context.Context, connection *domain.Connection, message wire.ClientMessage, identity authz.Identity) {
	switch message.Tag {
	case wire.TagClientHeartbeat:
		// Touch already happened; nothing else to do.

	case wire.TagClientPing:
		if message.Ping == nil {
			return
		}
		// The driver id comes from the token, never from the payload. A client
		// that could name its own driver id could report positions for someone
		// else — and be dispatched their rides.
		ping := *message.Ping
		ping.DriverID = identity.UserID

		if !ping.Valid() {
			if h.hooks.OnRejected != nil {
				h.hooks.OnRejected("invalid_ping")
			}
			return
		}

		h.produce(ctx, kafkax.TopicLocPing, identity.UserID, ping)

	case wire.TagClientWatchFleet:
		// The whole fleet, every driver's position, is for ops. Checked here
		// rather than trusted from the page, because the page is not the
		// security boundary — this socket is.
		if identity.Role != authz.RoleOps {
			h.reject(connection, "not_ops", "the fleet is visible to ops only")
			return
		}
		if message.Viewport == nil {
			return
		}
		h.mu.Lock()
		h.watchers[connection.ID] = watcher{connection: connection, viewport: *message.Viewport}
		h.mu.Unlock()

	case wire.TagClientUnwatchFleet:
		h.mu.Lock()
		delete(h.watchers, connection.ID)
		h.mu.Unlock()

	case wire.TagClientFollowTrip:
		if message.TripID == "" {
			return
		}
		// Checked against the trip service as the caller, so the rule that
		// decides who may read a trip decides who may watch its driver: its
		// rider, its driver, or ops. A rider who could follow any trip id could
		// follow anybody's driver.
		lookup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		response, err := h.trips.GetTrip(authz.Outgoing(lookup, identity), &trippb.GetTripRequest{TripId: message.TripID})
		if err != nil || response.GetTrip().GetDriverId() == "" {
			h.reject(connection, "not_followable", "that trip has no driver to follow")
			return
		}
		h.mu.Lock()
		h.followers[connection.ID] = follower{
			connection: connection, tripID: message.TripID, driverID: response.GetTrip().GetDriverId(),
			pickup: geo.Point{Lat: response.GetTrip().GetPickup().GetLat(), Lng: response.GetTrip().GetPickup().GetLng()},
		}
		h.mu.Unlock()

	case wire.TagClientUnfollowTrip:
		h.mu.Lock()
		delete(h.followers, connection.ID)
		h.mu.Unlock()

	case wire.TagClientOfferReply:
		if message.Reply == nil || message.ReplyCell == "" {
			return
		}
		reply := *message.Reply
		reply.DriverID = identity.UserID

		h.produce(ctx, kafkax.TopicGeoEvents, message.ReplyCell, wire.GeoEvent{
			Tag:     wire.TagOfferReplied,
			Cell:    message.ReplyCell,
			AtMs:    time.Now().UnixMilli(),
			Replied: &reply,
		})
	}
}

func (h *Hub) produce(ctx context.Context, topic, key string, message any) {
	payload, err := json.Marshal(message)
	if err != nil {
		if h.hooks.OnPushFailed != nil {
			h.hooks.OnPushFailed("encode")
		}
		return
	}

	// Detached from the connection's context: a record must not be abandoned
	// because the client that sent it disconnected a millisecond later. Its
	// position is still true.
	tracing.Produce(context.WithoutCancel(ctx), h.producer, &kgo.Record{
		Topic: topic,
		Key:   []byte(key),
		Value: payload,
	}, func(_ *kgo.Record, err error) {
		if err == nil {
			return
		}
		if h.hooks.OnPushFailed != nil {
			h.hooks.OnPushFailed("produce")
		}
		if h.hooks.OnProduceError != nil {
			h.hooks.OnProduceError(topic, err)
		}
	})
}

// write drains the connection's queue onto the socket.
func (h *Hub) write(ctx context.Context, socket *websocket.Conn, connection *domain.Connection) {
	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			connection.Close(domain.EvictionShutdown)
			return

		case <-connection.Closed():
			return

		case message := <-connection.Outbound():
			writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := socket.Write(writeCtx, websocket.MessageText, message)
			cancel()

			if err != nil {
				// A write that times out is a client that is not reading, which
				// is the same condition the bounded queue catches — recorded
				// the same way so the metric tells one story.
				connection.Close(domain.EvictionSlowConsumer)
				return
			}

		case <-heartbeat.C:
			pingCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := socket.Ping(pingCtx)
			cancel()

			if err != nil {
				connection.Close(domain.EvictionClientClosed)
				return
			}
		}
	}
}

// Push delivers a message to a user, if they are connected here.
//
// Returns false when they are not, which is not an error: the gateway is one of
// several, and a driver connected to a different instance is that instance's
// concern.
func (h *Hub) Push(userID string, message []byte) bool {
	connection, ok := h.registry.Get(userID)
	if !ok {
		return false
	}

	switch err := connection.Send(message); {
	case err == nil:
		if h.hooks.OnPushed != nil {
			h.hooks.OnPushed()
		}
		return true
	case errors.Is(err, domain.ErrSlowConsumer):
		slog.Warn("evicted a slow consumer", "user", userID)
		if h.hooks.OnPushFailed != nil {
			h.hooks.OnPushFailed("slow_consumer")
		}
	default:
		if h.hooks.OnPushFailed != nil {
			h.hooks.OnPushFailed("gone")
		}
	}
	return false
}

// forget drops a closed connection's subscriptions.
func (h *Hub) forget(connectionID string) {
	h.mu.Lock()
	delete(h.watchers, connectionID)
	delete(h.followers, connectionID)
	h.mu.Unlock()
}

// reject counts a refused request and tells the client why, so a console
// opened by the wrong role says so instead of sitting on an empty map.
func (h *Hub) reject(connection *domain.Connection, reason, message string) {
	if h.hooks.OnRejected != nil {
		h.hooks.OnRejected(reason)
	}
	if payload, err := json.Marshal(wire.ServerMessage{Tag: wire.TagServerError, Error: message}); err == nil {
		_ = connection.Send(payload)
	}
}

// RunFleet sends every watching console its view, and every following rider
// their driver's position, once a second.
//
// Through each connection's bounded send queue like everything else, so a
// console that stops reading is evicted rather than allowed to hold the
// fan-out up — the same rule that protects the drivers' offers.
func (h *Hub) RunFleet(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			h.fanOut(now)
		}
	}
}

func (h *Hub) fanOut(now time.Time) {
	h.mu.Lock()
	watchers := make([]watcher, 0, len(h.watchers))
	for _, w := range h.watchers {
		watchers = append(watchers, w)
	}
	followers := make([]follower, 0, len(h.followers))
	for _, f := range h.followers {
		followers = append(followers, f)
	}
	h.mu.Unlock()

	for _, w := range watchers {
		update := h.fleet.Snapshot(w.viewport, now)
		if payload, err := json.Marshal(wire.ServerMessage{Tag: wire.TagFleetUpdate, Fleet: &update}); err == nil {
			_ = w.connection.Send(payload)
		}
	}

	for _, f := range followers {
		driver, found := h.fleet.Driver(f.driverID, now)
		if !found {
			continue
		}
		position := wire.DriverPosition{
			TripID: f.tripID, DriverID: driver.ID,
			Lat: driver.Lat, Lng: driver.Lng, Heading: driver.Heading, AtMs: now.UnixMilli(),
		}
		// A wait only means something while the driver is on the way.
		if driver.Status == wire.StatusEnRoutePickup {
			meters := geo.DistanceMeters(geo.Point{Lat: driver.Lat, Lng: driver.Lng}, f.pickup)
			if seconds, ok := h.eta.Predict(meters); ok {
				position.EtaSeconds = math.Round(seconds)
			}
		}
		if payload, err := json.Marshal(wire.ServerMessage{Tag: wire.TagDriverPosition, Position: &position}); err == nil {
			_ = f.connection.Send(payload)
		}
	}
}
