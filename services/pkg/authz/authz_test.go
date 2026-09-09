package authz_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/pkg/authz"
)

func TestRoleValid(t *testing.T) {
	for _, role := range []authz.Role{authz.RoleRider, authz.RoleDriver, authz.RoleOps} {
		if !role.Valid() {
			t.Errorf("%q should be valid", role)
		}
	}

	// An unknown role must not be quietly downgraded to rider: acting on a role
	// we cannot reason about is worse than refusing the request.
	for _, role := range []authz.Role{"", "admin", "owner", "Rider", "ops "} {
		if role.Valid() {
			t.Errorf("%q should not be valid", role)
		}
	}
}

func TestBearerToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		query  string
		want   string
	}{
		{"authorization header", "Bearer abc.def.ghi", "", "abc.def.ghi"},
		{"query parameter for websockets", "", "abc.def.ghi", "abc.def.ghi"},
		{"header wins over query", "Bearer from-header", "from-query", "from-header"},
		{"wrong scheme falls through to query", "Basic dXNlcjpwYXNz", "from-query", "from-query"},
		{"nothing at all", "", "", ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			url := "/ws"
			if test.query != "" {
				url += "?token=" + test.query
			}

			request := httptest.NewRequest(http.MethodGet, url, nil)
			if test.header != "" {
				request.Header.Set("Authorization", test.header)
			}

			if got := authz.BearerToken(request); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

// The contract test.
//
// Everything else about authentication can be right in isolation and still be
// wrong across the language boundary: the claim names, the signing algorithm,
// the issuer and audience, and the role clamp all have to line up between
// better-auth's definePayload and identityFrom. Nothing but running both can
// tell us they do, so this signs a real user up against a real auth service and
// verifies the token it hands back.
//
// It skips rather than fails when the service is not running, so `go test ./...`
// stays useful on a machine with nothing else up.
func TestVerifyAgainstLiveAuthService(t *testing.T) {
	base := os.Getenv("AUTH_BASE_URL")
	if base == "" {
		base = "http://localhost:3200"
	}

	client := &http.Client{Timeout: 5 * time.Second}
	if _, err := client.Get(base + "/health"); err != nil {
		t.Skipf("auth service not running at %s: %v", base, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	email := fmt.Sprintf("authz-%d@surge.test", time.Now().UnixNano())
	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": "correct-horse-battery",
		"name":     "Authz Test",
		"role":     "driver",
	})

	signUp, err := client.Post(base+"/api/auth/sign-up/email", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("sign up: %v", err)
	}
	defer signUp.Body.Close()

	if signUp.StatusCode != http.StatusOK {
		t.Fatalf("sign up returned %d", signUp.StatusCode)
	}

	tokenRequest, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/auth/token", nil)
	for _, cookie := range signUp.Cookies() {
		tokenRequest.AddCookie(cookie)
	}

	tokenResponse, err := client.Do(tokenRequest)
	if err != nil {
		t.Fatalf("fetch token: %v", err)
	}
	defer tokenResponse.Body.Close()

	var minted struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(tokenResponse.Body).Decode(&minted); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if minted.Token == "" {
		t.Fatal("auth service returned no token")
	}

	verifier, err := authz.NewJWKS(ctx, base+"/api/auth/jwks", base, "surge")
	if err != nil {
		t.Fatalf("build verifier: %v", err)
	}

	identity, err := verifier.Verify(ctx, minted.Token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	if identity.UserID == "" {
		t.Error("identity carries no user id")
	}
	if identity.Email != email {
		t.Errorf("email: got %q, want %q", identity.Email, email)
	}
	if identity.Role != authz.RoleDriver {
		t.Errorf("role: got %q, want %q", identity.Role, authz.RoleDriver)
	}

	t.Run("rejects a token it did not sign", func(t *testing.T) {
		// Same shape, different signature. This is the case that would silently
		// pass if the key set were ignored.
		forged := minted.Token[:len(minted.Token)-6] + "AAAAAA"

		if _, err := verifier.Verify(ctx, forged); !errors.Is(err, authz.ErrUnauthenticated) {
			t.Errorf("forged token: got %v, want ErrUnauthenticated", err)
		}
	})

	t.Run("rejects rubbish", func(t *testing.T) {
		if _, err := verifier.Verify(ctx, "not-a-token"); !errors.Is(err, authz.ErrUnauthenticated) {
			t.Errorf("garbage token: got %v, want ErrUnauthenticated", err)
		}
	})
}
