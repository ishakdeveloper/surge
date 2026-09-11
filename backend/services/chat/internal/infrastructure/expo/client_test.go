package expo_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ishakdeveloper/surge/services/chat/internal/infrastructure/expo"
	"github.com/ishakdeveloper/surge/services/chat/internal/service"
)

// expoStub answers the way Expo's push API does: 200 with one ticket per
// notification, in order, whatever became of each.
type expoStub struct {
	mu      sync.Mutex
	batches []int
	auth    []string
	gone    map[string]bool
	status  int
}

func (s *expoStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var messages []struct {
		To string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&messages); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.batches = append(s.batches, len(messages))
	s.auth = append(s.auth, r.Header.Get("Authorization"))
	s.mu.Unlock()

	if s.status != 0 {
		http.Error(w, "upstream unavailable", s.status)
		return
	}
	var tickets []map[string]any
	for _, m := range messages {
		if s.gone[m.To] {
			tickets = append(tickets, map[string]any{
				"status": "error", "message": "not a registered push notification recipient",
				"details": map[string]string{"error": "DeviceNotRegistered"},
			})
			continue
		}
		tickets = append(tickets, map[string]any{"status": "ok", "id": "ticket-" + m.To})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": tickets})
}

func notifications(n int) []service.Notification {
	out := make([]service.Notification, n)
	for i := range out {
		out[i] = service.Notification{To: fmt.Sprintf("ExponentPushToken[%d]", i), Title: "Your driver", Body: "I'm here"}
	}
	return out
}

func TestSendsInBatchesAndReportsGoneDevices(t *testing.T) {
	stub := &expoStub{gone: map[string]bool{"ExponentPushToken[150]": true}}
	server := httptest.NewServer(stub)
	defer server.Close()

	unregistered, err := expo.New(server.URL, "secret").Send(context.Background(), notifications(250))
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(stub.batches) != "[100 100 50]" {
		t.Errorf("batches %v, want Expo's limit of a hundred", stub.batches)
	}
	if stub.auth[0] != "Bearer secret" {
		t.Errorf("authorization %q", stub.auth[0])
	}
	if len(unregistered) != 1 || unregistered[0] != "ExponentPushToken[150]" {
		t.Errorf("unregistered %v: the ticket's position is the notification's", unregistered)
	}
}

func TestAnOutageIsAnErrorToRetry(t *testing.T) {
	server := httptest.NewServer(&expoStub{status: http.StatusServiceUnavailable})
	defer server.Close()

	if _, err := expo.New(server.URL, "").Send(context.Background(), notifications(1)); err == nil {
		t.Fatal("a 503 was taken for a delivery")
	}
}
