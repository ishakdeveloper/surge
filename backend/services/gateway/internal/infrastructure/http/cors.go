package http

import (
	"net/http"
	"slices"
)

// CORS allows exactly the configured origins.
//
// A list rather than a wildcard, because these requests carry credentials and
// `Access-Control-Allow-Origin: *` is invalid with them — browsers refuse the
// combination outright, so a wildcard here would not be permissive, it would
// simply be broken.
func CORS(origins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if origin != "" && slices.Contains(origins, origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key")
				// Every method the REST surface can declare. The list of origins
				// is the control; this list only has to not be the reason a
				// browser refuses a call. It was, once: it stopped at POST while
				// sim.proto declares `PUT /v1/simulator`, so the console's rescale
				// failed its preflight in every browser.
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				// Origin varies the response, so a cache that ignored this
				// would serve one origin's headers to another.
				w.Header().Add("Vary", "Origin")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
