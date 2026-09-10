// Package http is ingest's read-only window onto its in-memory state.
//
// Not a product API. It exists because the position index lives in a process's
// heap rather than a database, and "what does this instance actually think is
// happening" is otherwise unanswerable — you cannot open a psql shell against a
// Go map.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ishakdeveloper/surge/ingest/internal/domain"
)

type Server struct {
	addr  string
	index *domain.Index
}

func NewServer(addr string, index *domain.Index) *Server {
	return &Server{addr: addr, index: index}
}

func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /debug/stats", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, s.index.Stats())
	})

	// The shard load distribution: how evenly geography spreads drivers, which
	// decides whether sharding by cell is balanced or whether the city centre
	// becomes one hot partition.
	mux.HandleFunc("GET /debug/shards", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, s.index.ShardLoad())
	})

	server := &http.Server{Addr: s.addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()

	slog.Info("debug api listening", "addr", s.addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("ingest: debug api: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
