package authz_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/shared/authz"
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
	if !outboxOpen(client, base) {
		t.Skipf("auth service at %s has no dev outbox to read a code from; start it with AUTH_DEV_OUTBOX=true", base)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	email := fmt.Sprintf("authz-%d@surge.test", time.Now().UnixNano())
	cookies := signInWithCode(t, ctx, client, base, email, "driver")

	tokenRequest, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/auth/token", nil)
	for _, cookie := range cookies {
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

// outboxOpen reports whether the auth service serves the dev outbox that
// signInWithCode reads a code from. An open one refuses a question with no
// address; a closed one has no such route.
func outboxOpen(client *http.Client, base string) bool {
	response, err := client.Get(base + "/dev/outbox")
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusBadRequest
}

// signInWithCode signs in the way a person does: a code sent to the address,
// read back from the dev outbox rather than an inbox, and typed in. The first
// time makes the account, with the role asked for.
func signInWithCode(t *testing.T, ctx context.Context, client *http.Client, base, email, role string) []*http.Cookie {
	t.Helper()

	since := time.Now().UnixMilli()
	postJSON(t, ctx, client, base+"/api/auth/email-otp/send-verification-otp", map[string]string{
		"email": email,
		"type":  "sign-in",
	})

	signedIn := postJSON(t, ctx, client, base+"/api/auth/sign-in/email-otp", map[string]string{
		"email": email,
		"otp":   codeSentTo(t, ctx, client, base, email, since),
		"role":  role,
	})
	return signedIn.Cookies()
}

func postJSON(t *testing.T, ctx context.Context, client *http.Client, endpoint string, body map[string]string) *http.Response {
	t.Helper()

	encoded, _ := json.Marshal(body)
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("POST %s: %v", endpoint, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("POST %s returned %d", endpoint, response.StatusCode)
	}
	return response
}

// codeSentTo is the code sent to email at or after since. Polled, because
// better-auth may answer before its sender has run.
func codeSentTo(t *testing.T, ctx context.Context, client *http.Client, base, email string, since int64) string {
	t.Helper()

	for attempt := 0; attempt < 25; attempt++ {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/dev/outbox?to="+url.QueryEscape(email), nil)
		if response, err := client.Do(request); err == nil {
			var sent struct {
				Code *string `json:"code"`
				AtMs int64   `json:"atMs"`
			}
			decodeErr := json.NewDecoder(response.Body).Decode(&sent)
			response.Body.Close()
			if response.StatusCode == http.StatusOK && decodeErr == nil && sent.Code != nil && sent.AtMs >= since {
				return *sent.Code
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	t.Fatalf("no code reached the outbox for %s", email)
	return ""
}

// The simulator's own tokens, and the gate on them.
func TestHMACRoundTrip(t *testing.T) {
	const secret = "a-secret-long-enough-to-be-worth-something"

	signer, err := authz.NewHMAC(secret, "surge-sim", "surge")
	if err != nil {
		t.Fatalf("new hmac: %v", err)
	}

	token, err := signer.Sign(authz.Identity{
		UserID: "drv-000042", Email: "drv-000042@sim.surge", Role: authz.RoleDriver,
	}, time.Minute)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	identity, err := signer.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if identity.UserID != "drv-000042" || identity.Role != authz.RoleDriver {
		t.Errorf("identity did not survive the round trip: %+v", identity)
	}
}

// A short secret on a symmetric signature is the whole attack, so it is refused
// at construction rather than discovered later.
func TestHMACRefusesAWeakSecret(t *testing.T) {
	if _, err := authz.NewHMAC("short", "surge-sim", "surge"); err == nil {
		t.Error("a five-character signing secret was accepted")
	}
}

// A token signed by one issuer must not be accepted by another's verifier —
// this is what keeps simulated drivers out of a deployment that has not opted
// into them.
func TestHMACRejectsAnotherIssuersToken(t *testing.T) {
	const secret = "a-secret-long-enough-to-be-worth-something"

	mine, _ := authz.NewHMAC(secret, "surge-sim", "surge")
	theirs, _ := authz.NewHMAC("a-completely-different-secret-of-good-length", "surge-sim", "surge")

	token, err := theirs.Sign(authz.Identity{UserID: "u", Role: authz.RoleOps}, time.Minute)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if _, err := mine.Verify(context.Background(), token); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("a foreign signature was accepted: %v", err)
	}
}

// The chain accepts what any member accepts and rejects everything else with
// one indistinguishable error.
func TestChain(t *testing.T) {
	const secret = "a-secret-long-enough-to-be-worth-something"
	signer, _ := authz.NewHMAC(secret, "surge-sim", "surge")

	chain := authz.Chain{signer}

	token, _ := signer.Sign(authz.Identity{UserID: "drv-1", Role: authz.RoleDriver}, time.Minute)
	if _, err := chain.Verify(context.Background(), token); err != nil {
		t.Errorf("the chain rejected a token one of its members signed: %v", err)
	}

	if _, err := chain.Verify(context.Background(), "not-a-token"); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("want ErrUnauthenticated, got %v", err)
	}

	// An empty chain accepts nothing, which is the safe default for a
	// deployment that configured no verifier at all.
	if _, err := (authz.Chain{}).Verify(context.Background(), token); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Error("an empty chain accepted a token")
	}
}
