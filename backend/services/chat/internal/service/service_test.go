package service_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/chat/internal/domain"
	"github.com/ishakdeveloper/surge/services/chat/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/services/chat/internal/service"
	"github.com/ishakdeveloper/surge/shared/authz"
)

var start = time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

var (
	rider    = authz.Identity{UserID: "rider-1", Role: authz.RoleRider}
	driver   = authz.Identity{UserID: "drv-1", Role: authz.RoleDriver}
	stranger = authz.Identity{UserID: "rider-2", Role: authz.RoleRider}
	agent    = authz.Identity{UserID: "ops-1", Role: authz.RoleOps}
	agent2   = authz.Identity{UserID: "ops-2", Role: authz.RoleOps}
)

// fakeTrips answers as the trip service does: the rider, the assigned driver,
// or ops, and a not-found for anybody else.
type fakeTrips struct{ trips map[string]service.Trip }

func (f *fakeTrips) Trip(_ context.Context, caller authz.Identity, id string) (service.Trip, error) {
	trip, ok := f.trips[id]
	visible := caller.UserID == trip.RiderID ||
		(trip.DriverID != "" && caller.UserID == trip.DriverID) ||
		caller.Role == authz.RoleOps
	if !ok || !visible {
		return service.Trip{}, service.ErrTripNotFound
	}
	return trip, nil
}

type delivery struct {
	conversationID string
	userID         string
	seq            int64
	recipients     []string
}

// recorder is the Notifier, remembering every doorbell.
type recorder struct {
	mu      sync.Mutex
	changed []delivery
	reads   []delivery
	typing  []delivery
	queue   []service.Change
}

func (r *recorder) Changed(_ context.Context, change service.Change, recipients []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.changed = append(r.changed, delivery{conversationID: change.ConversationID, seq: change.LastSeq, recipients: recipients})
}

func (r *recorder) Read(_ context.Context, id, userID string, seq int64, _ time.Time, recipients []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads = append(r.reads, delivery{conversationID: id, userID: userID, seq: seq, recipients: recipients})
}

func (r *recorder) Typing(_ context.Context, id, userID string, _ time.Time, recipients []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.typing = append(r.typing, delivery{conversationID: id, userID: userID, recipients: recipients})
}

func (r *recorder) SupportQueueChanged(_ context.Context, change service.Change) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queue = append(r.queue, change)
}

type rig struct {
	chat  *service.Service
	repo  *repository.InMemory
	trips *fakeTrips
	bells *recorder
	now   time.Time
}

func newRig(t *testing.T, options ...func(*service.Options)) *rig {
	t.Helper()
	r := &rig{
		repo: repository.NewInMemory(),
		trips: &fakeTrips{trips: map[string]service.Trip{
			"trip-1": {ID: "trip-1", RiderID: "rider-1", DriverID: "drv-1"},
			"trip-2": {ID: "trip-2", RiderID: "rider-1"},
		}},
		bells: &recorder{},
		now:   start,
	}
	ids := 0
	opts := service.Options{
		Repository: r.repo, Trips: r.trips, Notifier: r.bells,
		Now:   func() time.Time { return r.now },
		NewID: func() string { ids++; return fmt.Sprintf("id-%d", ids) },
	}
	for _, option := range options {
		option(&opts)
	}
	chat, err := service.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	r.chat = chat
	return r
}

func (r *rig) tripConversation(t *testing.T, who authz.Identity) *domain.Conversation {
	t.Helper()
	conversation, err := r.chat.TripConversation(context.Background(), who, "trip-1")
	if err != nil {
		t.Fatalf("open the trip conversation as %s: %v", who.UserID, err)
	}
	return conversation
}

func (r *rig) send(t *testing.T, who authz.Identity, id, body, key string) *domain.Message {
	t.Helper()
	message, err := r.chat.Send(context.Background(), who, id, service.Draft{Body: body, ClientMessageID: key})
	if err != nil {
		t.Fatalf("%s sending %q: %v", who.UserID, body, err)
	}
	return message
}

// The rider hears "accepted" from the trip service before the fact has come
// through the outbox, and taps chat at once. That tap opens the conversation;
// the fact arriving afterwards finds it and changes nothing.
func TestTheFirstTapOpensTheConversationBeforeTheFactArrives(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	opened := r.tripConversation(t, rider)
	if len(opened.Participants) != 2 {
		t.Fatalf("participants %+v, want the rider and the driver", opened.Participants)
	}

	if err := r.chat.TripChanged(ctx, service.Trip{ID: "trip-1", RiderID: "rider-1", DriverID: "drv-1"}); err != nil {
		t.Fatalf("the late fact: %v", err)
	}
	if again := r.tripConversation(t, driver); again.ID != opened.ID {
		t.Errorf("the driver got %s, the rider %s", again.ID, opened.ID)
	}
	if len(r.bells.changed) != 1 {
		t.Errorf("%d doorbells for one conversation opening", len(r.bells.changed))
	}
}

func TestBeforeAcceptanceThereIsNobodyToTalkTo(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	if _, err := r.chat.TripConversation(ctx, rider, "trip-2"); !errors.Is(err, service.ErrNoDriverYet) {
		t.Errorf("before a driver accepted: %v", err)
	}
	if _, err := r.chat.TripConversation(ctx, stranger, "trip-1"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a stranger's tap: %v", err)
	}
}

// Numbered without gaps, so a client asking for everything after the newest
// seq it holds misses nothing; and a retried send is the message it retried.
func TestSeqsAreContiguousAndARetryIsOneMessage(t *testing.T) {
	r := newRig(t)
	id := r.tripConversation(t, rider).ID

	first := r.send(t, rider, id, "I'm at the side entrance", "k1")
	second := r.send(t, driver, id, "Two minutes", "k2")
	retried := r.send(t, driver, id, "Two minutes", "k2")
	third := r.send(t, rider, id, "OK", "k3")

	if first.Seq != 1 || second.Seq != 2 || third.Seq != 3 {
		t.Errorf("seqs %d %d %d, want 1 2 3", first.Seq, second.Seq, third.Seq)
	}
	if retried.ID != second.ID {
		t.Errorf("the retry stored a second message")
	}

	messages, _, err := r.chat.Messages(context.Background(), driver, id, domain.MessageQuery{After: 1})
	if err != nil || len(messages) != 2 || messages[0].Seq != 2 {
		t.Fatalf("after seq 1: %v, %+v", err, messages)
	}

	// One doorbell per stored message, to the other side only.
	last := r.bells.changed[len(r.bells.changed)-1]
	if last.seq != 3 || len(last.recipients) != 1 || last.recipients[0] != "drv-1" {
		t.Errorf("the rider's last message rang %+v", last)
	}
	if pending := r.repo.PendingPushes(); pending != 3 {
		t.Errorf("%d notifications owed, want one per message", pending)
	}
}

func TestAKeylessSendIsRefused(t *testing.T) {
	r := newRig(t)
	id := r.tripConversation(t, rider).ID
	if _, err := r.chat.Send(context.Background(), rider, id, service.Draft{Body: "hi"}); !errors.Is(err, service.ErrKeyRequired) {
		t.Errorf("want ErrKeyRequired, got %v", err)
	}
}

// After the grace window the conversation stops taking messages and keeps
// every one it has; a completion redelivered later does not reopen it.
func TestAClosedConversationStillReads(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	id := r.tripConversation(t, rider).ID
	r.send(t, rider, id, "Thanks!", "k1")

	ended := service.Trip{ID: "trip-1", RiderID: "rider-1", DriverID: "drv-1", EndedAt: start}
	if err := r.chat.TripChanged(ctx, ended); err != nil {
		t.Fatal(err)
	}

	r.now = start.Add(59 * time.Minute)
	r.send(t, rider, id, "I left my umbrella", "k2")

	r.now = start.Add(time.Hour)
	later := ended
	later.EndedAt = start.Add(30 * time.Minute)
	if err := r.chat.TripChanged(ctx, later); err != nil {
		t.Fatal(err)
	}
	if _, err := r.chat.Send(ctx, driver, id, service.Draft{Body: "Found it", ClientMessageID: "k3"}); !errors.Is(err, domain.ErrClosed) {
		t.Errorf("after the grace window: %v", err)
	}

	messages, _, err := r.chat.Messages(ctx, driver, id, domain.MessageQuery{})
	if err != nil || len(messages) != 2 {
		t.Errorf("reading a closed conversation: %v, %d messages", err, len(messages))
	}
}

func TestStrangersSeeNothingAndSupportOnlyReads(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	id := r.tripConversation(t, rider).ID
	r.send(t, rider, id, "hello", "k1")

	if _, err := r.chat.Conversation(ctx, stranger, id); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a stranger reading: %v", err)
	}
	if _, err := r.chat.Send(ctx, stranger, id, service.Draft{Body: "hi", ClientMessageID: "s"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a stranger sending: %v", err)
	}

	if _, _, err := r.chat.Messages(ctx, agent, id, domain.MessageQuery{}); err != nil {
		t.Errorf("support reading a trip conversation: %v", err)
	}
	if _, err := r.chat.Send(ctx, agent, id, service.Draft{Body: "hi", ClientMessageID: "o"}); !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("support writing in a trip conversation: %v", err)
	}
	// Support looking is not the person you are talking to having read it.
	if at, err := r.chat.MarkRead(ctx, agent, id, 1); err != nil || at != 0 || len(r.bells.reads) != 0 {
		t.Errorf("support's read became a receipt: at=%d err=%v reads=%v", at, err, r.bells.reads)
	}
}

func TestReadMarkersOnlyMoveForward(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	id := r.tripConversation(t, rider).ID
	r.send(t, rider, id, "one", "k1")
	r.send(t, rider, id, "two", "k2")

	if at, err := r.chat.MarkRead(ctx, driver, id, 2); err != nil || at != 2 {
		t.Fatalf("read to 2: at=%d err=%v", at, err)
	}
	if at, _ := r.chat.MarkRead(ctx, driver, id, 1); at != 2 {
		t.Errorf("a late receipt moved the marker back to %d", at)
	}
	if at, _ := r.chat.MarkRead(ctx, driver, id, 99); at != 2 {
		t.Errorf("a marker past the newest message: %d", at)
	}

	if len(r.bells.reads) != 1 {
		t.Fatalf("%d receipts, want one: only a move is news", len(r.bells.reads))
	}
	if receipt := r.bells.reads[0]; receipt.seq != 2 || receipt.recipients[0] != "rider-1" {
		t.Errorf("receipt %+v", receipt)
	}

	conversation, _ := r.chat.Conversation(ctx, rider, id)
	if me, _ := conversation.Participant("rider-1"); me.LastReadSeq != 2 {
		t.Errorf("the sender has read their own messages up to %d", me.LastReadSeq)
	}
}

func TestTheRateLimitCountsAMinute(t *testing.T) {
	r := newRig(t, func(o *service.Options) { o.RateLimit = 3 })
	ctx := context.Background()
	id := r.tripConversation(t, rider).ID

	for i := range 3 {
		r.send(t, rider, id, "spam", fmt.Sprintf("k%d", i))
	}
	if _, err := r.chat.Send(ctx, rider, id, service.Draft{Body: "spam", ClientMessageID: "k3"}); !errors.Is(err, service.ErrRateLimited) {
		t.Fatalf("the fourth in a minute: %v", err)
	}
	// The send that took the last slot can still be retried.
	if retried := r.send(t, rider, id, "spam", "k2"); retried.Seq != 3 {
		t.Errorf("a retry at the limit: seq %d", retried.Seq)
	}

	r.now = start.Add(61 * time.Second)
	r.send(t, rider, id, "later", "k4")
}

func TestQuickRepliesBelongToTheirSide(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	id := r.tripConversation(t, rider).ID

	message, err := r.chat.Send(ctx, driver, id, service.Draft{QuickReply: "driver.arrived", ClientMessageID: "q1"})
	if err != nil {
		t.Fatal(err)
	}
	if message.QuickReply != "driver.arrived" || message.Body != "I'm here" {
		t.Errorf("stored %q %q", message.QuickReply, message.Body)
	}

	var invalid *domain.InvalidError
	if _, err := r.chat.Send(ctx, rider, id, service.Draft{QuickReply: "driver.arrived", ClientMessageID: "q2"}); !errors.As(err, &invalid) {
		t.Errorf("a rider sending the driver's reply: %v", err)
	}
}

func TestTypingIsThrottled(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	id := r.tripConversation(t, rider).ID

	for range 5 {
		if err := r.chat.Typing(ctx, rider, id); err != nil {
			t.Fatal(err)
		}
	}
	r.now = start.Add(service.DefaultTypingEvery)
	if err := r.chat.Typing(ctx, rider, id); err != nil {
		t.Fatal(err)
	}

	if len(r.bells.typing) != 2 {
		t.Fatalf("%d typing doorbells for two windows", len(r.bells.typing))
	}
	if to := r.bells.typing[0].recipients; len(to) != 1 || to[0] != "drv-1" {
		t.Errorf("typing went to %v", to)
	}
}

// Support, end to end: a rider asks, the queue hears, the first agent to
// answer holds it, a second cannot barge in, and resolving ends it.
func TestSupportIsClaimedByWhoeverAnswers(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	opened, err := r.chat.CreateSupport(ctx, rider, service.SupportDraft{
		TripID: "trip-1", Subject: "Lost property", Body: "I left my bag in the car", Key: "help-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	again, err := r.chat.CreateSupport(ctx, rider, service.SupportDraft{
		TripID: "trip-1", Subject: "Lost property", Body: "I left my bag in the car", Key: "help-1",
	})
	if err != nil || again.ID != opened.ID {
		t.Fatalf("a retried request opened %v (%v)", again, err)
	}
	if len(r.bells.queue) != 1 {
		t.Errorf("the queue heard %d times about one request", len(r.bells.queue))
	}

	queue, err := r.chat.List(ctx, agent, domain.ListFilter{Kind: domain.KindSupport, State: domain.StateOpen})
	if err != nil || len(queue.Conversations) != 1 {
		t.Fatalf("support's queue: %v, %+v", err, queue)
	}
	if theirs, _ := r.chat.List(ctx, driver, domain.ListFilter{Kind: domain.KindSupport}); len(theirs.Conversations) != 0 {
		t.Errorf("the driver can list the rider's support conversation")
	}

	reply, err := r.chat.Send(ctx, agent2, opened.ID, service.Draft{QuickReply: "support.looking_into_it", ClientMessageID: "a1"})
	if err != nil {
		t.Fatalf("answering claims it: %v", err)
	}
	if reply.SenderRole != domain.RoleSupport {
		t.Errorf("support's reply is labelled %s", reply.SenderRole)
	}
	if _, err := r.chat.Send(ctx, agent, opened.ID, service.Draft{Body: "hi", ClientMessageID: "a2"}); !errors.Is(err, domain.ErrClaimed) {
		t.Errorf("a second agent answering: %v", err)
	}
	if _, err := r.chat.Claim(ctx, agent, opened.ID); !errors.Is(err, domain.ErrClaimed) {
		t.Errorf("a second agent claiming: %v", err)
	}

	resolved, err := r.chat.Resolve(ctx, rider, opened.ID)
	if err != nil || resolved.Status != domain.StatusResolved {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := r.chat.Send(ctx, rider, opened.ID, service.Draft{Body: "one more thing", ClientMessageID: "r2"}); !errors.Is(err, domain.ErrClosed) {
		t.Errorf("writing after resolving: %v", err)
	}
	if _, err := r.chat.Resolve(ctx, driver, opened.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("a stranger resolving: %v", err)
	}
}

func TestSupportIsForRidersAndDriversAboutTheirOwnTrips(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	var invalid *domain.InvalidError
	if _, err := r.chat.CreateSupport(ctx, stranger, service.SupportDraft{TripID: "trip-1", Body: "hi", Key: "k"}); !errors.As(err, &invalid) {
		t.Errorf("about someone else's trip: %v", err)
	}
	if _, err := r.chat.CreateSupport(ctx, agent, service.SupportDraft{Body: "hi", Key: "k"}); !errors.Is(err, domain.ErrNotAllowed) {
		t.Errorf("support opening a support conversation: %v", err)
	}
	if _, err := r.chat.CreateSupport(ctx, rider, service.SupportDraft{Body: "hi"}); !errors.Is(err, service.ErrKeyRequired) {
		t.Errorf("without a key: %v", err)
	}
	if _, err := r.chat.CreateSupport(ctx, driver, service.SupportDraft{Body: "My app froze", Key: "k"}); err != nil {
		t.Errorf("about no trip at all: %v", err)
	}
}

func TestAPushTokenIsExposAndFollowsTheDevice(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	token := "ExponentPushToken[xxxxxxxxxxxxxxxxxxxxxx]"

	if err := r.chat.RegisterPushToken(ctx, rider, "not-a-token", domain.PlatformIOS); !errors.Is(err, service.ErrInvalidPushToken) {
		t.Errorf("a made-up token: %v", err)
	}
	if err := r.chat.RegisterPushToken(ctx, rider, token, domain.PlatformIOS); err != nil {
		t.Fatal(err)
	}
	// The phone is signed in as the driver now: it is theirs.
	if err := r.chat.RegisterPushToken(ctx, driver, token, domain.PlatformIOS); err != nil {
		t.Fatal(err)
	}
	if tokens, _ := r.repo.PushTokens(ctx, "rider-1"); len(tokens) != 0 {
		t.Errorf("the rider still owns the driver's phone")
	}

	if err := r.chat.UnregisterPushToken(ctx, rider, token); err != nil {
		t.Fatal(err)
	}
	if tokens, _ := r.repo.PushTokens(ctx, "drv-1"); len(tokens) != 1 {
		t.Errorf("someone else signed the driver's phone out")
	}
}
