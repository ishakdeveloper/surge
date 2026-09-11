// Package grpc is chat's synchronous edge.
//
// A translation layer, as the trip service's is: proto in, service call, proto
// out, and the mapping from what went wrong to a status a client can act on.
package grpc

import (
	"context"
	"errors"
	"time"

	"github.com/ishakdeveloper/surge/services/chat/internal/domain"
	"github.com/ishakdeveloper/surge/services/chat/internal/service"
	"github.com/ishakdeveloper/surge/shared/authz"
	chatpb "github.com/ishakdeveloper/surge/shared/proto/chat"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Handler struct {
	chatpb.UnimplementedChatServiceServer
	service *service.Service
}

func NewHandler(chat *service.Service) *Handler { return &Handler{service: chat} }

// statusOf is the one place a chat error becomes a status.
func statusOf(err error, action string) error {
	var invalid *domain.InvalidError
	switch {
	case errors.As(err, &invalid):
		return status.Error(codes.InvalidArgument, invalid.Reason)
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, "unknown conversation")
	case errors.Is(err, service.ErrNoDriverYet):
		// FailedPrecondition: asked a moment too early, and right a moment
		// later. The client waits for the trip to be accepted.
		return status.Error(codes.FailedPrecondition, "the chat opens once a driver accepts the trip")
	case errors.Is(err, domain.ErrClosed):
		return status.Error(codes.FailedPrecondition, "this conversation is closed")
	case errors.Is(err, domain.ErrClaimed):
		return status.Error(codes.FailedPrecondition, "someone else from support is handling this")
	case errors.Is(err, domain.ErrNotAllowed):
		return status.Error(codes.PermissionDenied, "that is not available to your role")
	case errors.Is(err, service.ErrRateLimited):
		return status.Error(codes.ResourceExhausted, "too many messages in the last minute, wait a moment")
	case errors.Is(err, service.ErrKeyRequired):
		return status.Error(codes.InvalidArgument, "an Idempotency-Key is required so a retry cannot send twice")
	case errors.Is(err, service.ErrInvalidPushToken):
		return status.Error(codes.InvalidArgument, "that is not an Expo push token")
	default:
		return status.Errorf(codes.Internal, "could not %s: %v", action, err)
	}
}

// key is an idempotency key from the request, or from the Idempotency-Key
// header the gateway forwards as metadata.
func key(ctx context.Context, field string) string {
	if field != "" {
		return field
	}
	return authz.IdempotencyKeyFromMetadata(ctx)
}

func (h *Handler) GetTripConversation(ctx context.Context, request *chatpb.GetTripConversationRequest) (*chatpb.GetTripConversationResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	conversation, err := h.service.TripConversation(ctx, caller, request.GetTripId())
	if err != nil {
		return nil, statusOf(err, "open the conversation")
	}
	return &chatpb.GetTripConversationResponse{Conversation: h.toConversation(conversation, caller)}, nil
}

func (h *Handler) ListConversations(ctx context.Context, request *chatpb.ListConversationsRequest) (*chatpb.ListConversationsResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	page, err := h.service.List(ctx, caller, domain.ListFilter{
		Kind:   kindOf(request.GetKind()),
		State:  stateOf(request.GetStatus()),
		Limit:  int(request.GetPageSize()),
		Cursor: request.GetPageToken(),
	})
	if err != nil {
		return nil, statusOf(err, "list conversations")
	}

	conversations := make([]*chatpb.Conversation, 0, len(page.Conversations))
	for i := range page.Conversations {
		conversations = append(conversations, h.toConversation(&page.Conversations[i], caller))
	}
	return &chatpb.ListConversationsResponse{Conversations: conversations, NextPageToken: page.NextCursor}, nil
}

func (h *Handler) GetConversation(ctx context.Context, request *chatpb.GetConversationRequest) (*chatpb.GetConversationResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	conversation, err := h.service.Conversation(ctx, caller, request.GetConversationId())
	if err != nil {
		return nil, statusOf(err, "read the conversation")
	}
	return &chatpb.GetConversationResponse{Conversation: h.toConversation(conversation, caller)}, nil
}

func (h *Handler) ListMessages(ctx context.Context, request *chatpb.ListMessagesRequest) (*chatpb.ListMessagesResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	after, before := request.GetAfterSeq(), request.GetBeforeSeq()
	switch {
	case after < 0 || before < 0:
		return nil, status.Error(codes.InvalidArgument, "a seq cannot be negative")
	case after > 0 && before > 0:
		return nil, status.Error(codes.InvalidArgument, "read after a seq or before one, not both")
	}

	messages, more, err := h.service.Messages(ctx, caller, request.GetConversationId(), domain.MessageQuery{
		After: int64(after), Before: int64(before), Limit: int(request.GetPageSize()),
	})
	if err != nil {
		return nil, statusOf(err, "read the messages")
	}

	out := make([]*chatpb.Message, 0, len(messages))
	for i := range messages {
		out = append(out, toMessage(&messages[i]))
	}
	return &chatpb.ListMessagesResponse{Messages: out, HasMore: more}, nil
}

func (h *Handler) SendMessage(ctx context.Context, request *chatpb.SendMessageRequest) (*chatpb.SendMessageResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	message, err := h.service.Send(ctx, caller, request.GetConversationId(), service.Draft{
		Body:            request.GetBody(),
		QuickReply:      request.GetQuickReply(),
		ClientMessageID: key(ctx, request.GetClientMessageId()),
	})
	if err != nil {
		return nil, statusOf(err, "send the message")
	}
	return &chatpb.SendMessageResponse{Message: toMessage(message)}, nil
}

func (h *Handler) MarkRead(ctx context.Context, request *chatpb.MarkReadRequest) (*chatpb.MarkReadResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	if request.GetSeq() < 0 {
		return nil, status.Error(codes.InvalidArgument, "a seq cannot be negative")
	}
	at, err := h.service.MarkRead(ctx, caller, request.GetConversationId(), int64(request.GetSeq()))
	if err != nil {
		return nil, statusOf(err, "mark the conversation read")
	}
	return &chatpb.MarkReadResponse{LastReadSeq: int32(at)}, nil
}

func (h *Handler) Typing(ctx context.Context, request *chatpb.TypingRequest) (*chatpb.TypingResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.service.Typing(ctx, caller, request.GetConversationId()); err != nil {
		return nil, statusOf(err, "say you are typing")
	}
	return &chatpb.TypingResponse{}, nil
}

func (h *Handler) CreateSupportConversation(ctx context.Context, request *chatpb.CreateSupportConversationRequest) (*chatpb.CreateSupportConversationResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	conversation, err := h.service.CreateSupport(ctx, caller, service.SupportDraft{
		TripID:  request.GetTripId(),
		Subject: request.GetSubject(),
		Body:    request.GetBody(),
		Key:     key(ctx, request.GetIdempotencyKey()),
	})
	if err != nil {
		return nil, statusOf(err, "contact support")
	}
	return &chatpb.CreateSupportConversationResponse{Conversation: h.toConversation(conversation, caller)}, nil
}

func (h *Handler) ClaimSupportConversation(ctx context.Context, request *chatpb.ClaimSupportConversationRequest) (*chatpb.ClaimSupportConversationResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	conversation, err := h.service.Claim(ctx, caller, request.GetConversationId())
	if err != nil {
		return nil, statusOf(err, "claim the conversation")
	}
	return &chatpb.ClaimSupportConversationResponse{Conversation: h.toConversation(conversation, caller)}, nil
}

func (h *Handler) ResolveSupportConversation(ctx context.Context, request *chatpb.ResolveSupportConversationRequest) (*chatpb.ResolveSupportConversationResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	conversation, err := h.service.Resolve(ctx, caller, request.GetConversationId())
	if err != nil {
		return nil, statusOf(err, "resolve the conversation")
	}
	return &chatpb.ResolveSupportConversationResponse{Conversation: h.toConversation(conversation, caller)}, nil
}

func (h *Handler) RegisterPushToken(ctx context.Context, request *chatpb.RegisterPushTokenRequest) (*chatpb.RegisterPushTokenResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	platform, ok := platforms[request.GetPlatform()]
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "the platform must be iOS or Android")
	}
	if err := h.service.RegisterPushToken(ctx, caller, request.GetToken(), platform); err != nil {
		return nil, statusOf(err, "register the device")
	}
	return &chatpb.RegisterPushTokenResponse{}, nil
}

func (h *Handler) UnregisterPushToken(ctx context.Context, request *chatpb.UnregisterPushTokenRequest) (*chatpb.UnregisterPushTokenResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.service.UnregisterPushToken(ctx, caller, request.GetToken()); err != nil {
		return nil, statusOf(err, "unregister the device")
	}
	return &chatpb.UnregisterPushTokenResponse{}, nil
}

var platforms = map[chatpb.PushPlatform]domain.Platform{
	chatpb.PushPlatform_PUSH_PLATFORM_IOS:     domain.PlatformIOS,
	chatpb.PushPlatform_PUSH_PLATFORM_ANDROID: domain.PlatformAndroid,
}

var kinds = map[domain.Kind]chatpb.ConversationKind{
	domain.KindTrip:    chatpb.ConversationKind_CONVERSATION_KIND_TRIP,
	domain.KindSupport: chatpb.ConversationKind_CONVERSATION_KIND_SUPPORT,
}

func kindOf(kind chatpb.ConversationKind) domain.Kind {
	for domainKind, protoKind := range kinds {
		if protoKind == kind {
			return domainKind
		}
	}
	return ""
}

var states = map[domain.State]chatpb.ConversationStatus{
	domain.StateOpen:     chatpb.ConversationStatus_CONVERSATION_STATUS_OPEN,
	domain.StateClosed:   chatpb.ConversationStatus_CONVERSATION_STATUS_CLOSED,
	domain.StateResolved: chatpb.ConversationStatus_CONVERSATION_STATUS_RESOLVED,
}

func stateOf(status chatpb.ConversationStatus) domain.State {
	for state, protoStatus := range states {
		if protoStatus == status {
			return state
		}
	}
	return ""
}

var roles = map[domain.Role]chatpb.ParticipantRole{
	domain.RoleRider:   chatpb.ParticipantRole_PARTICIPANT_ROLE_RIDER,
	domain.RoleDriver:  chatpb.ParticipantRole_PARTICIPANT_ROLE_DRIVER,
	domain.RoleSupport: chatpb.ParticipantRole_PARTICIPANT_ROLE_SUPPORT,
}

// toConversation is a conversation as the caller sees it: their unread count,
// and the quick replies they could send in it now.
func (h *Handler) toConversation(conversation *domain.Conversation, caller authz.Identity) *chatpb.Conversation {
	now := h.service.Now()

	participants := make([]*chatpb.Participant, 0, len(conversation.Participants))
	for _, participant := range conversation.Participants {
		participants = append(participants, &chatpb.Participant{
			UserId:      participant.UserID,
			Role:        roles[participant.Role],
			LastReadSeq: int32(participant.LastReadSeq),
		})
	}

	var unread int64
	if me, in := conversation.Participant(caller.UserID); in {
		unread = conversation.LastSeq - me.LastReadSeq
	}

	quickReplies := []*chatpb.QuickReply{}
	if _, err := conversation.CheckPost(service.Viewer(caller), now); err == nil {
		for _, reply := range domain.QuickRepliesFor(service.RoleOf(caller), conversation.Kind) {
			quickReplies = append(quickReplies, &chatpb.QuickReply{Code: reply.Code, Text: reply.Text})
		}
	}

	closesAt := ""
	if !conversation.ClosesAt.IsZero() {
		closesAt = conversation.ClosesAt.UTC().Format(time.RFC3339)
	}

	return &chatpb.Conversation{
		Id:           conversation.ID,
		Kind:         kinds[conversation.Kind],
		TripId:       conversation.TripID,
		Status:       states[conversation.StateAt(now)],
		Subject:      conversation.Subject,
		RequesterId:  conversation.RequesterID,
		AssigneeId:   conversation.AssigneeID,
		LastSeq:      int32(conversation.LastSeq),
		Unread:       int32(unread),
		ClosesAt:     closesAt,
		Participants: participants,
		QuickReplies: quickReplies,
		CreatedAt:    timestamppb.New(conversation.CreatedAt),
		UpdatedAt:    timestamppb.New(conversation.UpdatedAt),
	}
}

func toMessage(message *domain.Message) *chatpb.Message {
	return &chatpb.Message{
		Id:              message.ID,
		ConversationId:  message.ConversationID,
		Seq:             int32(message.Seq),
		SenderId:        message.SenderID,
		SenderRole:      roles[message.SenderRole],
		QuickReply:      message.QuickReply,
		Body:            message.Body,
		ClientMessageId: message.ClientMessageID,
		CreatedAt:       timestamppb.New(message.CreatedAt),
	}
}
