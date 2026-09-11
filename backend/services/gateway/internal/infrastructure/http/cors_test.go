package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const webOrigin = "http://localhost:5273"

func serve(t *testing.T, method, origin string) (*httptest.ResponseRecorder, bool) {
	t.Helper()

	reached := false
	handler := CORS([]string{webOrigin})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	request := httptest.NewRequest(method, "/v1/simulator", nil)
	if origin != "" {
		request.Header.Set("Origin", origin)
		request.Header.Set("Access-Control-Request-Method", "PUT")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder, reached
}

// The console rescales the simulator with PUT, from another origin, carrying
// credentials: a browser sends a preflight first, and refuses the call unless
// the answer names the method, the exact origin, and credentials.
func TestPreflightAllowsTheConsolesPut(t *testing.T) {
	recorder, reached := serve(t, http.MethodOptions, webOrigin)

	if recorder.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", recorder.Code)
	}
	if reached {
		t.Error("a preflight reached the handler behind it")
	}
	methods := recorder.Header().Get("Access-Control-Allow-Methods")
	for _, method := range []string{"GET", "POST", "PUT"} {
		if !strings.Contains(methods, method) {
			t.Errorf("Access-Control-Allow-Methods = %q, missing %s", methods, method)
		}
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != webOrigin {
		t.Errorf("Access-Control-Allow-Origin = %q, want the exact origin", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Access-Control-Allow-Credentials = %q, want true", got)
	}
}

// Any other origin gets no CORS headers at all, so its browser refuses the
// response — the list of origins is the control.
func TestOtherOriginsAreNotAllowed(t *testing.T) {
	recorder, _ := serve(t, http.MethodOptions, "https://elsewhere.example")

	for _, header := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials"} {
		if got := recorder.Header().Get(header); got != "" {
			t.Errorf("%s = %q for an unlisted origin", header, got)
		}
	}
}

func TestRequestsPassThrough(t *testing.T) {
	recorder, reached := serve(t, http.MethodPut, webOrigin)

	if !reached || recorder.Code != http.StatusOK {
		t.Errorf("reached = %v, status = %d; a real request must reach the handler", reached, recorder.Code)
	}
}
