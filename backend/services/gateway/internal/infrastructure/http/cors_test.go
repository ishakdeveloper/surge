package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The browser half of CORS, which no compiler checks.
//
// Effect's HTTP client adds trace headers to every request, and a preflight
// that does not allow one of them refuses the request outright. The browser
// reports that as "Failed to fetch" and nothing more, which is how every
// signed-in web page came to fail while the same call from curl succeeded.

const webOrigin = "http://localhost:5273"

func preflight(t *testing.T, origin string, reached *bool) *httptest.ResponseRecorder {
	t.Helper()

	handler := CORS([]string{webOrigin})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		*reached = true
	}))

	request := httptest.NewRequest(http.MethodOptions, "/v1/trips", nil)
	request.Header.Set("Origin", origin)
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	request.Header.Set("Access-Control-Request-Headers", "authorization,b3,traceparent")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestCORSAllowsTheHeadersTheWebClientSends(t *testing.T) {
	var reached bool
	response := preflight(t, webOrigin, &reached)

	if response.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if reached {
		t.Fatal("a preflight reached the handler behind CORS")
	}

	allowed := strings.ToLower(response.Header().Get("Access-Control-Allow-Headers"))
	for _, header := range []string{"authorization", "content-type", "idempotency-key", "traceparent", "tracestate", "b3"} {
		if !strings.Contains(allowed, header) {
			t.Errorf("Access-Control-Allow-Headers %q does not allow %q", allowed, header)
		}
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != webOrigin {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, webOrigin)
	}
	if got := response.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Access-Control-Allow-Credentials = %q, want true", got)
	}
}

func TestCORSGrantsNothingToAnOriginNotConfigured(t *testing.T) {
	var reached bool
	response := preflight(t, "https://elsewhere.example", &reached)

	for _, header := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials", "Access-Control-Allow-Headers"} {
		if got := response.Header().Get(header); got != "" {
			t.Errorf("%s = %q for an unconfigured origin, want none", header, got)
		}
	}
}
