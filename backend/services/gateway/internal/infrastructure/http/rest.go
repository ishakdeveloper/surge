// Package http is the gateway's REST edge.
//
// It is generated, not written. Each RPC in proto/trip.proto carries a
// google.api.http annotation naming its HTTP verb and path, and grpc-gateway
// produces the reverse proxy from that — so the REST surface, the gRPC contract
// and the OpenAPI document in docs/api are one source of truth rather than
// three that drift.
//
// What is written here is the part codegen cannot know: which requests need a
// caller, how a gRPC status becomes an HTTP status, and what an error looks
// like on the wire.
package http

import (
	"context"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/ishakdeveloper/surge/shared/authz"
	commonpb "github.com/ishakdeveloper/surge/shared/proto/common"
	simpb "github.com/ishakdeveloper/surge/shared/proto/sim"
	trippb "github.com/ishakdeveloper/surge/shared/proto/trip"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

// NewMux builds the REST surface over a gRPC connection.
func NewMux(ctx context.Context, trip, simulator *grpc.ClientConn) (*runtime.ServeMux, error) {
	mux := runtime.NewServeMux(
		// Canonical proto3 JSON: camelCase, enums by name, int64 as a string.
		// EmitUnpopulated because a zero value is meaningful — a trip with no
		// driver must say so rather than omitting the field and leaving a
		// client to guess whether it is unassigned or the server forgot.
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{
				EmitUnpopulated: true,
				UseProtoNames:   false,
			},
			UnmarshalOptions: protojson.UnmarshalOptions{
				// Reject unknown fields. A typo'd field name silently ignored
				// is a request that did something other than what the caller
				// asked, which is worse than a 400.
				DiscardUnknown: false,
			},
		}),
		// The seam between the two protocols: this is what carries the verified
		// caller from the HTTP request into the gRPC call's metadata. Without
		// it every call downstream is anonymous.
		runtime.WithMetadata(authz.GatewayMetadata),
		runtime.WithErrorHandler(writeError),
	)

	if err := trippb.RegisterTripServiceHandler(ctx, mux, trip); err != nil {
		return nil, err
	}
	if err := simpb.RegisterSimulatorServiceHandler(ctx, mux, simulator); err != nil {
		return nil, err
	}
	return mux, nil
}

// httpStatus maps gRPC codes at the boundary.
//
// Explicit rather than defaulting to 500: the difference between "your fare
// expired, get a new quote" and "we are broken" is the difference between a
// client that recovers and one that retries into a wall.
// EmitUnpopulated for the same reason the response marshaler uses it: a client
// decoding `{"error":{"code":...}}` with no `message` key has to treat the field
// as optional, and then every error handler carries a branch for a case that
// only exists because of an encoder setting.
var errorMarshaler = protojson.MarshalOptions{EmitUnpopulated: true}

var httpStatus = map[codes.Code]int{
	codes.InvalidArgument:    http.StatusBadRequest,
	codes.NotFound:           http.StatusNotFound,
	codes.AlreadyExists:      http.StatusConflict,
	codes.FailedPrecondition: http.StatusConflict,
	// A write that lost a race to another one. 409 like a precondition, but
	// its own code, because the right response differs: a precondition means
	// "this cannot happen now", aborted means "ask again against fresh state".
	codes.Aborted:           http.StatusConflict,
	codes.PermissionDenied:  http.StatusForbidden,
	codes.Unauthenticated:   http.StatusUnauthorized,
	codes.DeadlineExceeded:  http.StatusGatewayTimeout,
	codes.Unavailable:       http.StatusServiceUnavailable,
	codes.ResourceExhausted: http.StatusTooManyRequests,
	codes.Unimplemented:     http.StatusNotImplemented,
}

func writeError(_ context.Context, _ *runtime.ServeMux, marshaler runtime.Marshaler, w http.ResponseWriter, _ *http.Request, err error) {
	state := status.Convert(err)

	code, mapped := httpStatus[state.Code()]
	if !mapped {
		code = http.StatusInternalServerError
	}

	message := state.Message()
	if code == http.StatusInternalServerError {
		// Internal messages carry table names, connection strings and file
		// paths. The client gets the status; the detail stays in the trace.
		message = "something went wrong"
	}

	// commonpb.ErrorBody rather than a struct declared here, because the shape
	// is part of the contract: proto/common.proto declares it, the published
	// document describes it from that declaration, and the TypeScript client is
	// generated from the document. A hand-written struct beside all three would
	// be the fourth place to change and the first to be forgotten.
	body, err := errorMarshaler.Marshal(&commonpb.ErrorBody{
		Error: &commonpb.Error{Code: codeName(state.Code()), Message: message},
	})
	if err != nil {
		body = []byte(`{"error":{"code":"internal","message":"something went wrong"}}`)
	}

	w.Header().Set("Content-Type", marshaler.ContentType(nil))
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

// codeName is the stable string clients branch on. Derived from the gRPC code
// rather than invented, so adding a status to a service does not require
// touching the gateway.
func codeName(code codes.Code) string {
	switch code {
	case codes.InvalidArgument:
		return "invalid_argument"
	case codes.NotFound:
		return "not_found"
	case codes.AlreadyExists:
		return "already_exists"
	case codes.FailedPrecondition:
		return "failed_precondition"
	case codes.Aborted:
		return "aborted"
	case codes.PermissionDenied:
		return "permission_denied"
	case codes.Unauthenticated:
		return "unauthenticated"
	case codes.DeadlineExceeded:
		return "deadline_exceeded"
	case codes.Unavailable:
		return "unavailable"
	case codes.ResourceExhausted:
		return "rate_limited"
	default:
		return "internal"
	}
}
