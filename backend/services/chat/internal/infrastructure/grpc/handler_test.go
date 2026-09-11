package grpc_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/chat/internal/infrastructure/grpc"
	"github.com/ishakdeveloper/surge/services/chat/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/services/chat/internal/service"
	"github.com/ishakdeveloper/surge/shared/authz"
	chatpb "github.com/ishakdeveloper/surge/shared/proto/chat"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var start = time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

var (
	rider  = authz.Identity{UserID: "rider-1", Role: authz.RoleRider}
	driver = authz.Identity{UserID: "drv-1", Role: authz.RoleDriver}
	nobody = authz.Identity{UserID: "rider-2", Role: authz.RoleRider}
	agent  = authz.Identity{UserID: "ops-1", Role: authz.RoleOps}
)

type trips struct{}

func (trips) Trip(_ context.Context, caller authz.Identity, id string) (service.Trip, error) {
	switch {
	case id == "trip-1" && (caller.UserID == "rider-1" || caller.UserID == "drv-1"):
		return service.Trip{ID: id, RiderID: "rider-1", DriverID: "drv-1"}, nil
	case id == "trip-2" && caller.UserID == "rider-1":
		return service.Trip{ID: id, RiderID: "rider-1"}, nil
	default:
		return service.Trip{}, service.ErrTripNotFound
	}
}

type silent struct{}

func (silent) Changed(context.Context, service.Change, []string)                {}
func (silent) Read(context.Context, string, string, int64, time.Time, []string) {}
func (silent) Typing(context.Context, string, string, time.Time, []string)      {}
func (silent) SupportQueueChanged(context.Context, service.Change)              {}

type rig struct {
	handler *grpc.Handler
	chat    *service.Service
	now     time.Time
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{now: start}
	chat, err := service.New(service.Options{
		Repository: repository.NewInMemory(), Trips: trips{}, Notifier: silent{},
		Now: func() time.Time { return r.now }, RateLimit: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.chat, r.handler = chat, grpc.NewHandler(chat)
	return r
}

func as(caller authz.Identity) context.Context {
	return authz.WithIdentity(context.Background(), caller)
}

func (r *rig) open(t *testing.T) string {
	t.Helper()
	response, err := r.handler.GetTripConversation(as(rider), &chatpb.GetTripConversationRequest{TripId: "trip-1"})
	if err != nil {
		t.Fatal(err)
	}
	return response.GetConversation().GetId()
}

func (r *rig) send(caller authz.Identity, id, body, key string) error {
	_, err := r.handler.SendMessage(as(caller), &chatpb.SendMessageRequest{
		ConversationId: id, Body: body, ClientMessageId: key,
	})
	return err
}

func wantCode(t *testing.T, what string, err error, want codes.Code) {
	t.Helper()
	if got := status.Code(err); got != want {
		t.Errorf("%s: %s (%v), want %s", what, got, err, want)
	}
}

// Every failure a client can act on arrives as a status it can branch on:
// wait, fix the input, slow down, or stop asking.
func TestFailuresBecomeStatusesAClientCanActOn(t *testing.T) {
	r := newRig(t)
	id := r.open(t)

	_, err := r.handler.GetConversation(context.Background(), &chatpb.GetConversationRequest{ConversationId: id})
	wantCode(t, "no caller", err, codes.Unauthenticated)

	_, err = r.handler.GetTripConversation(as(rider), &chatpb.GetTripConversationRequest{TripId: "trip-2"})
	wantCode(t, "before a driver accepts", err, codes.FailedPrecondition)

	_, err = r.handler.GetConversation(as(nobody), &chatpb.GetConversationRequest{ConversationId: id})
	wantCode(t, "someone else's conversation", err, codes.NotFound)

	wantCode(t, "no idempotency key", r.send(rider, id, "hi", ""), codes.InvalidArgument)
	wantCode(t, "too long", r.send(rider, id, strings.Repeat("a", 1001), "long"), codes.InvalidArgument)
	wantCode(t, "support in a trip", r.send(agent, id, "hi", "ops"), codes.PermissionDenied)

	for i, key := range []string{"k1", "k2", "k3"} {
		if err := r.send(rider, id, "hi", key); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	wantCode(t, "over the rate limit", r.send(rider, id, "hi", "k4"), codes.ResourceExhausted)

	_, err = r.handler.ListMessages(as(rider), &chatpb.ListMessagesRequest{ConversationId: id, AfterSeq: 1, BeforeSeq: 3})
	wantCode(t, "both directions at once", err, codes.InvalidArgument)

	_, err = r.handler.RegisterPushToken(as(rider), &chatpb.RegisterPushTokenRequest{Token: "ExponentPushToken[x]"})
	wantCode(t, "no platform", err, codes.InvalidArgument)
	_, err = r.handler.RegisterPushToken(as(rider), &chatpb.RegisterPushTokenRequest{
		Token: "apns-token", Platform: chatpb.PushPlatform_PUSH_PLATFORM_IOS,
	})
	wantCode(t, "not an Expo token", err, codes.InvalidArgument)

	if err := r.chat.TripChanged(context.Background(), service.Trip{
		ID: "trip-1", RiderID: "rider-1", DriverID: "drv-1", EndedAt: start,
	}); err != nil {
		t.Fatal(err)
	}
	r.now = start.Add(2 * time.Hour)
	wantCode(t, "after the close", r.send(driver, id, "found your umbrella", "late"), codes.FailedPrecondition)
}

// A conversation is shown as the caller sees it: their unread count, and the
// quick replies they could send there now.
func TestAConversationIsShownAsTheCallerSeesIt(t *testing.T) {
	r := newRig(t)
	id := r.open(t)
	if err := r.send(rider, id, "Blue coat", "k1"); err != nil {
		t.Fatal(err)
	}
	if err := r.send(rider, id, "By the flower stall", "k2"); err != nil {
		t.Fatal(err)
	}

	view := func(caller authz.Identity) *chatpb.Conversation {
		t.Helper()
		response, err := r.handler.GetConversation(as(caller), &chatpb.GetConversationRequest{ConversationId: id})
		if err != nil {
			t.Fatal(err)
		}
		return response.GetConversation()
	}

	driverView := view(driver)
	if driverView.GetUnread() != 2 || driverView.GetLastSeq() != 2 {
		t.Errorf("the driver's unread %d of %d", driverView.GetUnread(), driverView.GetLastSeq())
	}
	if driverView.GetStatus() != chatpb.ConversationStatus_CONVERSATION_STATUS_OPEN || driverView.GetClosesAt() != "" {
		t.Errorf("a running trip's conversation: %s closing %q", driverView.GetStatus(), driverView.GetClosesAt())
	}
	if replies := driverView.GetQuickReplies(); len(replies) == 0 || !strings.HasPrefix(replies[0].GetCode(), "driver.") {
		t.Errorf("the driver is offered %v", replies)
	}
	if riderView := view(rider); riderView.GetUnread() != 0 {
		t.Errorf("the rider has %d unread of their own", riderView.GetUnread())
	}
	if agentView := view(agent); agentView.GetUnread() != 0 || len(agentView.GetQuickReplies()) != 0 {
		t.Errorf("support looking in: unread %d, replies %v", agentView.GetUnread(), agentView.GetQuickReplies())
	}

	if err := r.chat.TripChanged(context.Background(), service.Trip{
		ID: "trip-1", RiderID: "rider-1", DriverID: "drv-1", EndedAt: start,
	}); err != nil {
		t.Fatal(err)
	}
	if closing := view(driver).GetClosesAt(); closing != "2026-09-11T13:00:00Z" {
		t.Errorf("closes at %q, want the end plus an hour", closing)
	}
	r.now = start.Add(2 * time.Hour)
	closed := view(driver)
	if closed.GetStatus() != chatpb.ConversationStatus_CONVERSATION_STATUS_CLOSED || len(closed.GetQuickReplies()) != 0 {
		t.Errorf("after the close: %s, replies %v", closed.GetStatus(), closed.GetQuickReplies())
	}

	messages, err := r.handler.ListMessages(as(driver), &chatpb.ListMessagesRequest{ConversationId: id})
	if err != nil {
		t.Fatal(err)
	}
	first := messages.GetMessages()[0]
	if first.GetSeq() != 1 || first.GetSenderRole() != chatpb.ParticipantRole_PARTICIPANT_ROLE_RIDER || first.GetClientMessageId() != "k1" {
		t.Errorf("first message %+v", first)
	}
}
