// Package fake is a push provider that notifies nobody: what a laptop, the
// simulator and the tests run on.
package fake

import (
	"context"
	"log/slog"
	"sync"

	"github.com/ishakdeveloper/surge/services/chat/internal/service"
)

// Pusher logs every notification and keeps it, for a test to read back.
type Pusher struct {
	mu   sync.Mutex
	sent []service.Notification
}

func NewPusher() *Pusher { return &Pusher{} }

func (p *Pusher) Send(_ context.Context, notifications []service.Notification) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, notification := range notifications {
		slog.Info("push (fake)", "to", notification.To, "title", notification.Title, "body", notification.Body)
	}
	p.sent = append(p.sent, notifications...)
	return nil, nil
}

// Sent is every notification so far.
func (p *Pusher) Sent() []service.Notification {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]service.Notification(nil), p.sent...)
}
