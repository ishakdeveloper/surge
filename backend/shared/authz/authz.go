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
	"github.com/lestrrat-go/jwx/v3/jwa"
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

// HMAC verifies the simulator's own tokens.
//
// Ten thousand simulated drivers cannot each have a better-auth account: signing
// them up would be ten thousand rows and ten thousand password hashes to
// generate a load test. So the simulator mints its own tokens with a shared
// secret, and the gateway accepts them only when explicitly enabled.
//
// The gate is the important part. This verifier is constructed only when
// SIM_ENABLED is true, so a deployment that does not set it cannot be handed a
// forged driver identity even if the secret leaks.
type HMAC struct {
	secret   []byte
	issuer   string
	audience string
}

func NewHMAC(secret, issuer, audience string) (*HMAC, error) {
	if len(secret) < 32 {
		// A short secret on a symmetric signature is the whole attack. Refusing
		// at construction beats discovering it in a log.
		return nil, fmt.Errorf("authz: sim token secret must be at least 32 characters")
	}
	return &HMAC{secret: []byte(secret), issuer: issuer, audience: audience}, nil
}

func (h *HMAC) Verify(ctx context.Context, token string) (Identity, error) {
	key, err := jwk.Import(h.secret)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %v", ErrUnauthenticated, err)
	}

	parsed, err := jwt.ParseString(
		token,
		jwt.WithKey(jwa.HS256(), key),
		jwt.WithValidate(true),
		jwt.WithIssuer(h.issuer),
		jwt.WithAudience(h.audience),
		jwt.WithContext(ctx),
	)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %v", ErrUnauthenticated, err)
	}

	return identityFrom(parsed)
}

// Sign mints a token. Used by the simulator, and by tests.
func (h *HMAC) Sign(identity Identity, lifetime time.Duration) (string, error) {
	now := time.Now()

	token, err := jwt.NewBuilder().
		Issuer(h.issuer).
		Audience([]string{h.audience}).
		Subject(identity.UserID).
		IssuedAt(now).
		Expiration(now.Add(lifetime)).
		Claim("userId", identity.UserID).
		Claim("email", identity.Email).
		Claim("emailVerified", identity.EmailVerified).
		Claim("role", string(identity.Role)).
		Build()
	if err != nil {
		return "", fmt.Errorf("authz: build sim token: %w", err)
	}

	key, err := jwk.Import(h.secret)
	if err != nil {
		return "", fmt.Errorf("authz: import secret: %w", err)
	}

	signed, err := jwt.Sign(token, jwt.WithKey(jwa.HS256(), key))
	if err != nil {
		return "", fmt.Errorf("authz: sign sim token: %w", err)
	}
	return string(signed), nil
}

// Chain tries each verifier in turn.
//
// Order matters only for cost: real user tokens are the common case and JWKS
// verification is a local signature check either way. What matters is that a
// token rejected by all of them is rejected, with one indistinguishable error —
// telling a caller which issuer nearly accepted their token is free information.
type Chain []Verifier

func (c Chain) Verify(ctx context.Context, token string) (Identity, error) {
	for _, verifier := range c {
		identity, err := verifier.Verify(ctx, token)
		if err == nil {
			return identity, nil
		}
	}
	return Identity{}, ErrUnauthenticated
}

// contextKey is unexported so nothing outside this package can put a forged
// Identity into a context. That is the whole reason for the type: with a string
// key, any package could write the value the gateway trusts.
type contextKey struct{}

// WithIdentity attaches a verified caller to a context.
func WithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, identity)
}

// FromContext returns the caller a request was authenticated as.
//
// The zero Identity when there is none, and that is safe by construction: an
// empty UserID matches no rider's trips and the zero Role is not one of the
// three valid ones, so an unauthenticated caller is denied by the same checks
// that authorise everyone else rather than by a special case.
func FromContext(ctx context.Context) Identity {
	identity, _ := ctx.Value(contextKey{}).(Identity)
	return identity
}

// Middleware authenticates a request and attaches the caller.
//
// Rejects rather than passing anonymous requests through. An endpoint that
// wants to be public should not be behind this.
func Middleware(verifier Verifier, onRejected func(reason string)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := BearerToken(r)
			if token == "" {
				if onRejected != nil {
					onRejected("missing_token")
				}
				http.Error(w, `{"error":{"code":"unauthenticated","message":"missing token"}}`, http.StatusUnauthorized)
				return
			}

			identity, err := verifier.Verify(r.Context(), token)
			if err != nil {
				if onRejected != nil {
					onRejected("invalid_token")
				}
				http.Error(w, `{"error":{"code":"unauthenticated","message":"invalid token"}}`, http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), identity)))
		})
	}
}

// RequireRole gates an endpoint on a role. Used for the dispatch console, which
// riders and drivers have no business reading.
func RequireRole(role Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if FromContext(r.Context()).Role != role {
				http.Error(w, `{"error":{"code":"forbidden","message":"insufficient role"}}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
