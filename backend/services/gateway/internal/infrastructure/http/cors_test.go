package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The browser half of CORS, which no compiler checks.
//
// Both halves here are bugs that reached a browser and nothing else. Effect's
// HTTP client adds trace headers to every request, and a preflight that does
// not allow one of them refuses the request outright — reported as "Failed to
// fetch" and nothing more, which is how every signed-in web page came to fail
// while the same call from curl succeeded. The method list did the same to the
// console's rescale, which `sim.proto` declares as a PUT.

const webOrigin = "http://localhost:5273"

// serve runs one request through the middleware and says whether it reached
// what is behind it.
func serve(t *testing.T, method, path, origin, requested, headers string) (*httptest.ResponseRecorder, bool) {
	t.Helper()

	reached := false
	handler := CORS([]string{webOrigin})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	request := httptest.NewRequest(method, path, nil)
	if origin != "" {
		request.Header.Set("Origin", origin)
		request.Header.Set("Access-Control-Request-Method", requested)
	}
	if headers != "" {
		request.Header.Set("Access-Control-Request-Headers", headers)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder, reached
}

func TestCORSAllowsTheHeadersTheWebClientSends(t *testing.T) {
	response, reached := serve(t, http.MethodOptions, "/v1/trips", webOrigin,
		http.MethodGet, "authorization,b3,traceparent")

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

// The console rescales the simulator with PUT, from another origin, carrying
// credentials: a browser sends a preflight first, and refuses the call unless
// the answer names the method.
func TestPreflightAllowsTheConsolesPut(t *testing.T) {
	response, reached := serve(t, http.MethodOptions, "/v1/simulator", webOrigin, http.MethodPut, "")

	if response.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", response.Code)
	}
	if reached {
		t.Error("a preflight reached the handler behind it")
	}
	methods := response.Header().Get("Access-Control-Allow-Methods")
	for _, method := range []string{"GET", "POST", "PUT"} {
		if !strings.Contains(methods, method) {
			t.Errorf("Access-Control-Allow-Methods = %q, missing %s", methods, method)
		}
	}
}

// Any other origin gets no CORS headers at all, so its browser refuses the
// response — the list of origins is the control.
func TestCORSGrantsNothingToAnOriginNotConfigured(t *testing.T) {
	response, _ := serve(t, http.MethodOptions, "/v1/trips", "https://elsewhere.example",
		http.MethodGet, "authorization,b3,traceparent")

	for _, header := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials", "Access-Control-Allow-Headers"} {
		if got := response.Header().Get(header); got != "" {
			t.Errorf("%s = %q for an unconfigured origin, want none", header, got)
		}
	}
}

func TestRequestsPassThrough(t *testing.T) {
	response, reached := serve(t, http.MethodPut, "/v1/simulator", webOrigin, http.MethodPut, "")

	if !reached || response.Code != http.StatusOK {
		t.Errorf("reached = %v, status = %d; a real request must reach the handler", reached, response.Code)
	}
}
