package authz

import (
	"context"
	"net/http"
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Identity crosses a service boundary as gRPC metadata.
//
// The gateway authenticates once, at the edge, and forwards who the caller is;
// services behind it authorise without re-verifying a token. That split is the
// point — one place knows how to check a signature, every place knows who is
// asking.
//
// It rests on the internal network being trusted, which is the usual assumption
// and worth stating rather than leaving implied: any process that can reach the
// trip service on port 8110 can claim to be anyone. In a cluster that is what
// NetworkPolicy and mTLS are for; the gRPC port is deliberately not exposed
// outside it.
const (
	metadataUserID   = "x-surge-user-id"
	metadataEmail    = "x-surge-email"
	metadataRole     = "x-surge-role"
	metadataVerified = "x-surge-email-verified"
)

// Outgoing attaches an identity to a call.
func Outgoing(ctx context.Context, identity Identity) context.Context {
	return metadata.AppendToOutgoingContext(ctx,
		metadataUserID, identity.UserID,
		metadataEmail, identity.Email,
		metadataRole, string(identity.Role),
		metadataVerified, strconv.FormatBool(identity.EmailVerified),
	)
}

// GatewayMetadata copies a verified identity from an HTTP request context into
// the metadata of the gRPC call the REST gateway is about to make.
//
// This is the seam between the two protocols. grpc-gateway calls it per
// request, and without it every call downstream would be anonymous.
func GatewayMetadata(ctx context.Context, request *http.Request) metadata.MD {
	identity := FromContext(request.Context())
	if identity.UserID == "" {
		return nil
	}

	md := metadata.Pairs(
		metadataUserID, identity.UserID,
		metadataEmail, identity.Email,
		metadataRole, string(identity.Role),
		metadataVerified, strconv.FormatBool(identity.EmailVerified),
	)

	// An Idempotency-Key header belongs on the message, not in metadata: it is
	// part of what the caller is asking for, and a service should not have to
	// reach into transport headers to find it.
	if key := request.Header.Get("Idempotency-Key"); key != "" {
		md.Set("x-surge-idempotency-key", key)
	}
	return md
}

// IdempotencyKeyFromMetadata reads the header the gateway forwarded.
func IdempotencyKeyFromMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	if values := md.Get("x-surge-idempotency-key"); len(values) > 0 {
		return values[0]
	}
	return ""
}

// UnaryServerInterceptor turns forwarded metadata back into an Identity.
//
// Applied to every method, so a handler reads the caller from context exactly
// as an HTTP handler does and never thinks about metadata at all.
func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if identity, ok := identityFromMetadata(ctx); ok {
			ctx = WithIdentity(ctx, identity)
		}
		return handler(ctx, request)
	}
}

func identityFromMetadata(ctx context.Context) (Identity, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return Identity{}, false
	}

	first := func(key string) string {
		if values := md.Get(key); len(values) > 0 {
			return values[0]
		}
		return ""
	}

	userID := first(metadataUserID)
	if userID == "" {
		return Identity{}, false
	}

	role := Role(first(metadataRole))
	if !role.Valid() {
		// An unrecognised role is treated as no identity at all rather than as
		// a caller with unknown powers.
		return Identity{}, false
	}

	verified, _ := strconv.ParseBool(first(metadataVerified))

	return Identity{
		UserID:        userID,
		Email:         first(metadataEmail),
		EmailVerified: verified,
		Role:          role,
	}, true
}

// RequireCaller returns the identity, or the gRPC error a handler should
// return when there is none.
//
// Unauthenticated rather than PermissionDenied: the caller has not said who
// they are, which is a different problem from having said and not being
// allowed, and the two want different client behaviour.
func RequireCaller(ctx context.Context) (Identity, error) {
	identity := FromContext(ctx)
	if identity.UserID == "" {
		return Identity{}, status.Error(codes.Unauthenticated, "no caller identity")
	}
	return identity, nil
}
