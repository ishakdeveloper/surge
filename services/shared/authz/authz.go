// Package authz turns a bearer token into an Identity, locally.
//
// This is the Go half of a contract whose other half is TypeScript. The claim
// names below are exactly the fields of `Identity` in
// packages/domain/src/iam/Identity.ts, and apps/auth mints them into the token
// through the better-auth jwt plugin's definePayload. Changing one without the
// other breaks authentication in a way no compiler will catch, so treat this
// struct and that Schema.Class as one edit.
//
// Nothing here talks to the auth service per request. The JWKS is fetched once
// and refreshed on its own schedule, so verification is a signature check
// against a key already in memory: no database lookup, no network hop, and no
// dependency on Node being up for a ride in progress.
package authz

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lestrrat-go/httprc/v3"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

// Role is which surface an account belongs to. It mirrors the `Role` literal
// union in the domain package; there is no dynamic role and no tenancy.
type Role string

const (
	RoleRider  Role = "rider"
	RoleDriver Role = "driver"
	RoleOps    Role = "ops"
)

// Valid reports whether the role is one this system recognises. An unknown role
// is rejected rather than treated as a rider: a token carrying a role we cannot
// reason about is a token we should not act on.
func (r Role) Valid() bool {
	return r == RoleRider || r == RoleDriver || r == RoleOps
}

// Identity is the authenticated caller.
type Identity struct {
	UserID        string
	Email         string
	EmailVerified bool
	Role          Role
}

// ErrUnauthenticated is returned for every rejection: bad signature, expired,
// wrong issuer, missing claim. The reason is wrapped for logs but never
// distinguished to the caller, because telling an attacker which part of their
// token was wrong is free information.
var ErrUnauthenticated = errors.New("unauthenticated")

// Verifier turns a raw token into an Identity.
type Verifier interface {
	Verify(ctx context.Context, token string) (Identity, error)
}

// JWKS verifies tokens signed by apps/auth.
type JWKS struct {
	set      jwk.Set
	issuer   string
	audience string
}

// NewJWKS registers the JWKS URL with a background-refreshing cache and blocks
// until the first fetch succeeds — a service that cannot verify tokens should
// fail to start rather than accept traffic it will reject.
func NewJWKS(ctx context.Context, jwksURL, issuer, audience string) (*JWKS, error) {
	cache, err := jwk.NewCache(ctx, httprc.NewClient())
	if err != nil {
		return nil, fmt.Errorf("authz: jwk cache: %w", err)
	}

	if err := cache.Register(ctx, jwksURL, jwk.WithMinInterval(time.Minute)); err != nil {
		return nil, fmt.Errorf("authz: register %s: %w", jwksURL, err)
	}

	set, err := cache.CachedSet(jwksURL)
	if err != nil {
		return nil, fmt.Errorf("authz: cached set %s: %w", jwksURL, err)
	}

	return &JWKS{set: set, issuer: issuer, audience: audience}, nil
}

func (j *JWKS) Verify(ctx context.Context, token string) (Identity, error) {
	parsed, err := jwt.ParseString(
		token,
		jwt.WithKeySet(j.set),
		jwt.WithValidate(true),
		jwt.WithIssuer(j.issuer),
		jwt.WithAudience(j.audience),
		jwt.WithContext(ctx),
	)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %v", ErrUnauthenticated, err)
	}

	return identityFrom(parsed)
}

func identityFrom(token jwt.Token) (Identity, error) {
	var identity Identity

	if err := token.Get("userId", &identity.UserID); err != nil || identity.UserID == "" {
		return Identity{}, fmt.Errorf("%w: userId claim missing", ErrUnauthenticated)
	}
	// Email and emailVerified are informational — a token without them is still
	// a valid caller, just one we know less about.
	_ = token.Get("email", &identity.Email)
	_ = token.Get("emailVerified", &identity.EmailVerified)

	var role string
	if err := token.Get("role", &role); err != nil {
		return Identity{}, fmt.Errorf("%w: role claim missing", ErrUnauthenticated)
	}

	identity.Role = Role(role)
	if !identity.Role.Valid() {
		return Identity{}, fmt.Errorf("%w: unknown role %q", ErrUnauthenticated, role)
	}

	return identity, nil
}

// BearerToken extracts the credential a request carries.
//
// Two places, because two transports. An HTTP call uses the Authorization
// header; a WebSocket handshake cannot set one from the browser, so the token
// arrives as a query parameter instead. That is not a weakening — it is the
// same token, over TLS, and it is why the token is short-lived.
func BearerToken(r *http.Request) string {
	if header := r.Header.Get("Authorization"); header != "" {
		if after, ok := strings.CutPrefix(header, "Bearer "); ok {
			return strings.TrimSpace(after)
		}
	}
	return r.URL.Query().Get("token")
}
