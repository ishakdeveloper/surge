package authz_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ishakdeveloper/surge/shared/authz"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// The seam between the two protocols: the gateway authenticates once at the
// edge and forwards who the caller is, and services behind it authorise without
// re-verifying anything.
func TestIdentityCrossesTheGrpcBoundary(t *testing.T) {
	want := authz.Identity{
		UserID: "user-42", Email: "rider@surge.test",
		EmailVerified: true, Role: authz.RoleRider,
	}

	// The gateway's side: a verified caller on an HTTP request becomes metadata.
	request := httptest.NewRequest(http.MethodGet, "/v1/trips", nil)
	request = request.WithContext(authz.WithIdentity(request.Context(), want))
	request.Header.Set("Idempotency-Key", "abc-123")

	md := authz.GatewayMetadata(context.Background(), request)
	if md == nil {
		t.Fatal("no metadata produced; every downstream call would be anonymous")
	}

	// The service's side: metadata becomes an identity again.
	incoming := metadata.NewIncomingContext(context.Background(), md)

	var seen authz.Identity
	handler := func(ctx context.Context, _ any) (any, error) {
		seen = authz.FromContext(ctx)
		if key := authz.IdempotencyKeyFromMetadata(ctx); key != "abc-123" {
			t.Errorf("idempotency key did not cross: %q", key)
		}
		return nil, nil
	}

	if _, err := authz.UnaryServerInterceptor()(incoming, nil, nil, handler); err != nil {
		t.Fatalf("interceptor: %v", err)
	}

	if seen != want {
		t.Errorf("identity changed crossing the boundary:\n  got  %+v\n  want %+v", seen, want)
	}
}

// A call with no forwarded identity must arrive anonymous rather than as a
// zero-value caller that some check mistakes for a real one.
func TestNoMetadataMeansNoCaller(t *testing.T) {
	var seen authz.Identity
	handler := func(ctx context.Context, _ any) (any, error) {
		seen = authz.FromContext(ctx)
		return nil, nil
	}

	if _, err := authz.UnaryServerInterceptor()(context.Background(), nil, nil, handler); err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	if seen.UserID != "" {
		t.Errorf("an anonymous call arrived with an identity: %+v", seen)
	}

	if _, err := authz.RequireCaller(context.Background()); status.Code(err) != codes.Unauthenticated {
		t.Errorf("want Unauthenticated, got %v", err)
	}
}

// A role this system does not recognise is treated as no identity at all,
// rather than as a caller with unknown powers.
func TestUnknownRoleIsRejected(t *testing.T) {
	md := metadata.Pairs(
		"x-surge-user-id", "user-42",
		"x-surge-role", "superuser",
	)

	var seen authz.Identity
	handler := func(ctx context.Context, _ any) (any, error) {
		seen = authz.FromContext(ctx)
		return nil, nil
	}

	_, _ = authz.UnaryServerInterceptor()(metadata.NewIncomingContext(context.Background(), md), nil, nil, handler)

	if seen.UserID != "" {
		t.Errorf("a caller with an unrecognised role was admitted: %+v", seen)
	}
}

// An unauthenticated HTTP request produces no metadata, so nothing downstream
// can mistake it for a caller.
func TestAnonymousRequestForwardsNothing(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/trips", nil)

	if md := authz.GatewayMetadata(context.Background(), request); md != nil {
		t.Errorf("an anonymous request produced metadata: %v", md)
	}
}
