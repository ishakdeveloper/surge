// Package http is the gateway's REST edge for riders.
//
// A translation layer: JSON in, gRPC out, JSON back. The gateway owns the
// public shape of the API and the services behind it own their own contracts,
// which is what lets a proto change without breaking a browser and a browser
// convenience be added without touching a service.
package http

import (
	"encoding/json"
	"net/http"
	"strings"

	gatewaygrpc "github.com/ishakdeveloper/surge/gateway/internal/infrastructure/grpc"
	"github.com/ishakdeveloper/surge/shared/authz"
	commonpb "github.com/ishakdeveloper/surge/shared/proto/common"
	trippb "github.com/ishakdeveloper/surge/shared/proto/trip"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Response is the envelope every endpoint uses.
//
// One shape for success and failure, borrowed from the reference: a client
// writes one unwrapping function rather than branching on whether a body
// happens to have an `error` key.
type Response struct {
	Data  any    `json:"data,omitempty"`
	Error *Error `json:"error,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type API struct {
	clients *gatewaygrpc.Clients
}

func NewAPI(clients *gatewaygrpc.Clients) *API { return &API{clients: clients} }

// Routes mounts the rider-facing API. Auth is applied by the caller, so this
// stays a description of the surface rather than of the policy.
func (a *API) Routes(mux *http.ServeMux, guard func(http.Handler) http.Handler) {
	mux.Handle("POST /v1/trips/preview", guard(http.HandlerFunc(a.previewTrip)))
	mux.Handle("POST /v1/trips", guard(http.HandlerFunc(a.createTrip)))
	mux.Handle("GET /v1/trips/{id}", guard(http.HandlerFunc(a.getTrip)))
	mux.Handle("POST /v1/trips/{id}/cancel", guard(http.HandlerFunc(a.cancelTrip)))
}

type coordinate struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type previewRequest struct {
	Pickup  coordinate `json:"pickup"`
	Dropoff coordinate `json:"dropoff"`
}

func (a *API) previewTrip(w http.ResponseWriter, r *http.Request) {
	identity := authz.FromContext(r.Context())

	var body previewRequest
	if !decode(w, r, &body) {
		return
	}

	ctx, cancel := gatewaygrpc.WithTimeout(r.Context())
	defer cancel()

	response, err := a.clients.Trip.PreviewTrip(ctx, &trippb.PreviewTripRequest{
		// The rider is the token's subject, never the body's. A rider id taken
		// from the request would let anyone quote — and then book — as somebody
		// else.
		RiderId: identity.UserID,
		Pickup:  &commonpb.Coordinate{Lat: body.Pickup.Lat, Lng: body.Pickup.Lng},
		Dropoff: &commonpb.Coordinate{Lat: body.Dropoff.Lat, Lng: body.Dropoff.Lng},
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, Response{Data: response})
}

type createRequest struct {
	FareID string `json:"fareId"`
}

func (a *API) createTrip(w http.ResponseWriter, r *http.Request) {
	identity := authz.FromContext(r.Context())

	var body createRequest
	if !decode(w, r, &body) {
		return
	}

	// The idempotency key comes from the client, because only the client knows
	// that its retry is a retry. A key minted here would be different on every
	// attempt and would make the whole mechanism decorative.
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeJSON(w, http.StatusBadRequest, Response{Error: &Error{
			Code:    "idempotency_key_required",
			Message: "send an Idempotency-Key header so a retry cannot book twice",
		}})
		return
	}

	ctx, cancel := gatewaygrpc.WithTimeout(r.Context())
	defer cancel()

	response, err := a.clients.Trip.CreateTrip(ctx, &trippb.CreateTripRequest{
		RiderId:        identity.UserID,
		FareId:         body.FareID,
		IdempotencyKey: key,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, Response{Data: response.GetTrip()})
}

func (a *API) getTrip(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := gatewaygrpc.WithTimeout(r.Context())
	defer cancel()

	response, err := a.clients.Trip.GetTrip(ctx, &trippb.GetTripRequest{TripId: r.PathValue("id")})
	if err != nil {
		writeGRPCError(w, err)
		return
	}

	// A rider may only read their own trips. The trip service does not enforce
	// this because it has no notion of who is asking — the gateway is where an
	// identity exists, so it is where the check belongs.
	identity := authz.FromContext(r.Context())
	if trip := response.GetTrip(); trip.GetRiderId() != identity.UserID && identity.Role != authz.RoleOps {
		// Not found rather than forbidden: confirming a trip exists to someone
		// who may not see it is itself a disclosure.
		writeJSON(w, http.StatusNotFound, Response{Error: &Error{Code: "not_found", Message: "unknown trip"}})
		return
	}

	writeJSON(w, http.StatusOK, Response{Data: response.GetTrip()})
}

type cancelRequest struct {
	Reason string `json:"reason"`
}

func (a *API) cancelTrip(w http.ResponseWriter, r *http.Request) {
	var body cancelRequest
	_ = json.NewDecoder(r.Body).Decode(&body)

	ctx, cancel := gatewaygrpc.WithTimeout(r.Context())
	defer cancel()

	response, err := a.clients.Trip.CancelTrip(ctx, &trippb.CancelTripRequest{
		TripId: r.PathValue("id"),
		Reason: body.Reason,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, Response{Data: response.GetTrip()})
}

func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	// Bounded, because the body arrives from the internet. Without a limit a
	// single request can ask the gateway to allocate whatever it likes.
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(into); err != nil {
		writeJSON(w, http.StatusBadRequest, Response{Error: &Error{
			Code: "invalid_body", Message: err.Error(),
		}})
		return false
	}
	return true
}

// grpcToHTTP maps status codes at the boundary.
//
// Explicit rather than a default of 500: the difference between "your fare
// expired, get a new quote" and "we are broken" is the difference between a
// client that recovers and one that retries into a wall.
var grpcToHTTP = map[codes.Code]int{
	codes.InvalidArgument:    http.StatusBadRequest,
	codes.NotFound:           http.StatusNotFound,
	codes.FailedPrecondition: http.StatusConflict,
	codes.PermissionDenied:   http.StatusForbidden,
	codes.Unauthenticated:    http.StatusUnauthorized,
	codes.DeadlineExceeded:   http.StatusGatewayTimeout,
	codes.Unavailable:        http.StatusServiceUnavailable,
	codes.ResourceExhausted:  http.StatusTooManyRequests,
}

func writeGRPCError(w http.ResponseWriter, err error) {
	state, ok := status.FromError(err)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, Response{Error: &Error{
			Code: "internal", Message: "something went wrong",
		}})
		return
	}

	code, mapped := grpcToHTTP[state.Code()]
	if !mapped {
		code = http.StatusInternalServerError
	}

	message := state.Message()
	if code == http.StatusInternalServerError {
		// Internal messages can carry connection strings and table names.
		// Clients get the status; the details stay in the trace.
		message = "something went wrong"
	}

	writeJSON(w, code, Response{Error: &Error{
		Code:    strings.ToLower(state.Code().String()),
		Message: message,
	}})
}

// writeJSON renders the envelope, using protojson for protobuf payloads.
//
// encoding/json on a generated struct produces the proto *field* names —
// `package_slug`, enums as integers, int64 as a number that loses precision
// past 2^53. protojson produces canonical proto3 JSON: camelCase, enums by
// name, int64 as a string. That is the documented mapping every gRPC ecosystem
// agrees on, so a client generated from the same .proto lines up with what this
// gateway actually sends.
func writeJSON(w http.ResponseWriter, code int, body Response) {
	w.Header().Set("Content-Type", "application/json")

	message, isProto := body.Data.(proto.Message)
	if !isProto {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(body)
		return
	}

	encoded, err := protojson.MarshalOptions{
		// Zero values are meaningful here: a trip with no driver yet must say
		// so rather than omitting the field and leaving a client to guess
		// whether it is unassigned or the server forgot.
		EmitUnpopulated: true,
	}.Marshal(message)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(Response{Error: &Error{Code: "internal", Message: "could not encode response"}})
		return
	}

	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(struct {
		Data json.RawMessage `json:"data"`
	}{Data: encoded})
}
