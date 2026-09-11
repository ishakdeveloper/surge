package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/chat/internal/domain"
	"github.com/ishakdeveloper/surge/services/chat/internal/infrastructure/fake"
	"github.com/ishakdeveloper/surge/services/chat/internal/service"
)

const driverPhone = "ExponentPushToken[driver-phone]"

// flaky is a provider that refuses every batch, then forgets a device.
type flaky struct {
	fail       bool
	unregister []string
	attempts   int
	lastBatch  []service.Notification
}

func (f *flaky) Send(_ context.Context, notifications []service.Notification) ([]string, error) {
	f.attempts++
	f.lastBatch = notifications
	if f.fail {
		return nil, errors.New("expo is down")
	}
	return f.unregister, nil
}

func (r *rig) worker(pusher service.Pusher) *service.PushWorker {
	return service.NewPushWorker(service.PushOptions{
		Store: r.repo, Pusher: pusher, Now: func() time.Time { return r.now },
	})
}

func (r *rig) withDriverPhone(t *testing.T) string {
	t.Helper()
	if err := r.chat.RegisterPushToken(context.Background(), driver, driverPhone, domain.PlatformAndroid); err != nil {
		t.Fatal(err)
	}
	return r.tripConversation(t, rider).ID
}

// Someone looking at the conversation needs no notification about it, and
// nobody had to know they were looking: they read it within the delay.
func TestAMessageReadInTimeIsNeverPushed(t *testing.T) {
	r := newRig(t)
	id := r.withDriverPhone(t)
	pusher := fake.NewPusher()
	worker := r.worker(pusher)

	r.send(t, rider, id, "At the door", "k1")
	if _, err := r.chat.MarkRead(context.Background(), driver, id, 1); err != nil {
		t.Fatal(err)
	}

	r.now = start.Add(service.DefaultPushDelay)
	if err := worker.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sent := pusher.Sent(); len(sent) != 0 {
		t.Errorf("notified about a message already read: %+v", sent)
	}
	if pending := r.repo.PendingPushes(); pending != 0 {
		t.Errorf("%d jobs left behind", pending)
	}
}

// Three messages unread is one buzz, not three.
func TestAnUnreadBurstIsOneNotification(t *testing.T) {
	r := newRig(t)
	id := r.withDriverPhone(t)
	pusher := fake.NewPusher()
	worker := r.worker(pusher)

	r.send(t, rider, id, "Hi", "k1")
	if err := worker.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(pusher.Sent()) != 0 {
		t.Fatal("notified before the delay was up")
	}

	r.send(t, rider, id, "I'm by the flower stall", "k2")
	r.send(t, rider, id, "Blue coat", "k3")
	r.now = start.Add(service.DefaultPushDelay)
	if err := worker.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}

	sent := pusher.Sent()
	if len(sent) != 1 {
		t.Fatalf("%d notifications for one burst", len(sent))
	}
	got := sent[0]
	if got.To != driverPhone || got.Title != "Your rider" || got.Body != "3 new messages" {
		t.Errorf("notification %+v", got)
	}
	if got.Data["conversationId"] != id || got.Data["tripId"] != "trip-1" {
		t.Errorf("a tap would not open the conversation: %v", got.Data)
	}
}

func TestOneUnreadMessageIsShownAsItIs(t *testing.T) {
	r := newRig(t)
	id := r.withDriverPhone(t)
	pusher := fake.NewPusher()

	r.send(t, rider, id, "I'm at the side entrance", "k1")
	r.now = start.Add(service.DefaultPushDelay)
	if err := r.worker(pusher).Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sent := pusher.Sent(); len(sent) != 1 || sent[0].Body != "I'm at the side entrance" {
		t.Errorf("sent %+v", sent)
	}
}

func TestNobodyWithoutADeviceIsOwedAnything(t *testing.T) {
	r := newRig(t)
	id := r.tripConversation(t, rider).ID
	pusher := fake.NewPusher()

	r.send(t, rider, id, "Hello?", "k1")
	r.now = start.Add(service.DefaultPushDelay)
	if err := r.worker(pusher).Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(pusher.Sent()) != 0 || r.repo.PendingPushes() != 0 {
		t.Errorf("sent %d, %d left", len(pusher.Sent()), r.repo.PendingPushes())
	}
}

// A provider outage is retried with backoff, and given up on: a notification
// an hour late about a pickup is noise.
func TestAFailedSendBacksOffThenGivesUp(t *testing.T) {
	r := newRig(t)
	id := r.withDriverPhone(t)
	provider := &flaky{fail: true}
	worker := r.worker(provider)

	r.send(t, rider, id, "Hello?", "k1")
	r.now = start.Add(service.DefaultPushDelay)
	if err := worker.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if provider.attempts != 1 || r.repo.PendingPushes() != 1 {
		t.Fatalf("attempts %d, pending %d", provider.attempts, r.repo.PendingPushes())
	}

	// Not due again yet.
	r.now = r.now.Add(10 * time.Second)
	_ = worker.Drain(context.Background())
	if provider.attempts != 1 {
		t.Errorf("retried before the backoff: %d attempts", provider.attempts)
	}

	for range 10 {
		r.now = r.now.Add(5 * time.Minute)
		_ = worker.Drain(context.Background())
	}
	if provider.attempts != 5 {
		t.Errorf("%d attempts, want five and then no more", provider.attempts)
	}
	if pending := r.repo.PendingPushes(); pending != 0 {
		t.Errorf("%d jobs still owed after giving up", pending)
	}
}

// An uninstalled app's token never works again, and is forgotten.
func TestAGoneDeviceIsForgotten(t *testing.T) {
	r := newRig(t)
	id := r.withDriverPhone(t)
	provider := &flaky{unregister: []string{driverPhone}}

	r.send(t, rider, id, "Hello?", "k1")
	r.now = start.Add(service.DefaultPushDelay)
	if err := r.worker(provider).Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tokens, _ := r.repo.PushTokens(context.Background(), "drv-1"); len(tokens) != 0 {
		t.Errorf("still notifying %v", tokens)
	}
}
