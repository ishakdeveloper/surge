// Package service is chat's logic: opening conversations, sending and reading
// in them, and support's queue. It depends on ports, never on Postgres, Kafka
// or the trip service directly, so every rule here runs in a test that opens
// no socket.
package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ishakdeveloper/surge/services/chat/internal/domain"
	"github.com/ishakdeveloper/surge/shared/authz"
)

// Repository is where conversations live.
type Repository interface {
	// EnsureTrip stores a trip's conversation if there is none, and gives an
	// existing one the closing time if it has none yet. changed reports
	// whether either happened.
	EnsureTrip(ctx context.Context, conversation *domain.Conversation) (stored *domain.Conversation, changed bool, err error)
	Get(ctx context.Context, id string) (*domain.Conversation, error)
	GetByTrip(ctx context.Context, tripID string) (*domain.Conversation, error)
	List(ctx context.Context, filter domain.ListFilter) (domain.Page, error)
	// CreateSupport stores a support conversation with its first message, or
	// returns the one its requester already opened with this key.
	CreateSupport(ctx context.Context, conversation *domain.Conversation, first domain.Message) (stored *domain.Conversation, created bool, err error)

	MessageByClientID(ctx context.Context, conversationID, senderID, clientMessageID string) (*domain.Message, error)
	// Append numbers and stores a message if the conversation is open at its
	// CreatedAt, moves the sender's read marker to it, and owes every other
	// participant a notification at pushAt — all together. A message whose
	// client id is already stored is returned as it was.
	Append(ctx context.Context, message domain.Message, pushAt time.Time) (*domain.Message, error)
	Messages(ctx context.Context, conversationID string, query domain.MessageQuery) (messages []domain.Message, more bool, err error)
	// MarkRead moves a participant's marker forward to seq, never back. It
	// returns where the marker is and whether it moved.
	MarkRead(ctx context.Context, conversationID, userID string, seq int64) (at int64, moved bool, err error)
	// SentSince counts a sender's messages after since, for the rate limit.
	SentSince(ctx context.Context, senderID string, since time.Time) (int, error)

	// Claim makes someone from support the assignee and a participant, or
	// returns ErrClaimed if somebody else already is.
	Claim(ctx context.Context, conversationID string, assignee domain.Participant, now time.Time) (*domain.Conversation, error)
	Resolve(ctx context.Context, conversationID string, now time.Time) (*domain.Conversation, error)

	SavePushToken(ctx context.Context, token domain.PushToken) error
	DeletePushToken(ctx context.Context, userID, token string) error
}

// Trips is what chat needs from the trip service: a trip, as the caller is
// allowed to see it.
type Trips interface {
	// Trip returns ErrTripNotFound for a trip the caller may not see, which is
	// the trip service's rule rather than a copy of it.
	Trip(ctx context.Context, caller authz.Identity, tripID string) (Trip, error)
}

// Trip is the part of a trip a conversation is made from.
type Trip struct {
	ID       string
	RiderID  string
	DriverID string
	// EndedAt is when it completed or was cancelled, and zero while it runs.
	EndedAt time.Time
}

// Change is what a doorbell says about a conversation.
type Change struct {
	ConversationID string
	Kind           domain.Kind
	TripID         string
	LastSeq        int64
	At             time.Time
}

func changeOf(conversation *domain.Conversation, at time.Time) Change {
	return Change{
		ConversationID: conversation.ID, Kind: conversation.Kind, TripID: conversation.TripID,
		LastSeq: conversation.LastSeq, At: at,
	}
}

// Notifier rings the doorbells. Best effort: everything it announces is
// already stored, and a client that misses one reads it on its next look.
type Notifier interface {
	Changed(ctx context.Context, change Change, recipients []string)
	Read(ctx context.Context, conversationID, userID string, seq int64, at time.Time, recipients []string)
	Typing(ctx context.Context, conversationID, userID string, at time.Time, recipients []string)
	// SupportQueueChanged tells everyone from support who is connected.
	SupportQueueChanged(ctx context.Context, change Change)
}

var (
	// ErrTripNotFound is a trip that does not exist or is not the caller's.
	ErrTripNotFound = errors.New("service: unknown trip")
	// ErrNoDriverYet is a trip conversation asked for before anybody accepted
	// the trip: there is nobody to talk to yet.
	ErrNoDriverYet = errors.New("service: the chat opens once a driver accepts")
	ErrRateLimited = errors.New("service: too many messages")
	ErrKeyRequired = errors.New("service: an idempotency key is required")
)

// Defaults for the knobs in Options.
const (
	DefaultCloseGrace  = time.Hour
	DefaultPushDelay   = 8 * time.Second
	DefaultRateLimit   = 20
	DefaultTypingEvery = 3 * time.Second
)

type Options struct {
	Repository Repository
	Trips      Trips
	Notifier   Notifier
	Now        func() time.Time
	NewID      func() string
	// CloseGrace is how long after its trip ends a conversation takes messages.
	CloseGrace time.Duration
	// PushDelay is how long a message waits to be read before its recipient is
	// notified.
	PushDelay time.Duration
	// RateLimit is how many messages one person may send in a minute.
	RateLimit int
	// TypingEvery is the least time between two typing doorbells from one
	// person in one conversation.
	TypingEvery time.Duration
}

type Service struct {
	repo        Repository
	trips       Trips
	notifier    Notifier
	now         func() time.Time
	newID       func() string
	closeGrace  time.Duration
	pushDelay   time.Duration
	rateLimit   int
	typingEvery time.Duration

	typingMu sync.Mutex
	typedAt  map[typingKey]time.Time
}

type typingKey struct{ userID, conversationID string }

func New(options Options) (*Service, error) {
	if options.Repository == nil || options.Trips == nil || options.Notifier == nil {
		return nil, errors.New("service: a repository, trips and a notifier are required")
	}
	s := &Service{
		repo: options.Repository, trips: options.Trips, notifier: options.Notifier,
		now: options.Now, newID: options.NewID,
		closeGrace: options.CloseGrace, pushDelay: options.PushDelay,
		rateLimit: options.RateLimit, typingEvery: options.TypingEvery,
		typedAt: make(map[typingKey]time.Time),
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.newID == nil {
		s.newID = uuid.NewString
	}
	if s.closeGrace <= 0 {
		s.closeGrace = DefaultCloseGrace
	}
	if s.pushDelay <= 0 {
		s.pushDelay = DefaultPushDelay
	}
	if s.rateLimit <= 0 {
		s.rateLimit = DefaultRateLimit
	}
	if s.typingEvery <= 0 {
		s.typingEvery = DefaultTypingEvery
	}
	return s, nil
}

// Now is the service's clock, for the edges that judge a conversation's state.
func (s *Service) Now() time.Time { return s.now() }

// Viewer is the caller as the domain's rules see them.
func Viewer(caller authz.Identity) domain.Viewer {
	return domain.Viewer{UserID: caller.UserID, Support: caller.Role == authz.RoleOps}
}

// RoleOf is the side of a conversation a caller's role puts them on.
func RoleOf(caller authz.Identity) domain.Role {
	switch caller.Role {
	case authz.RoleDriver:
		return domain.RoleDriver
	case authz.RoleOps:
		return domain.RoleSupport
	default:
		return domain.RoleRider
	}
}

// TripConversation is the conversation about a trip, opened here if the
// trip's acceptance has not reached chat yet.
//
// The rider's app hears "accepted" straight from the trip service, and
// usually before the fact has come through the outbox and Kafka; asking the
// trip service is what keeps that race from being a "not found" on the first
// tap.
func (s *Service) TripConversation(ctx context.Context, caller authz.Identity, tripID string) (*domain.Conversation, error) {
	conversation, err := s.repo.GetByTrip(ctx, tripID)
	if errors.Is(err, domain.ErrNotFound) {
		conversation, err = s.openFromTrip(ctx, caller, tripID)
	}
	if err != nil {
		return nil, err
	}
	if !conversation.CanRead(Viewer(caller)) {
		return nil, domain.ErrNotFound
	}
	return conversation, nil
}

func (s *Service) openFromTrip(ctx context.Context, caller authz.Identity, tripID string) (*domain.Conversation, error) {
	trip, err := s.trips.Trip(ctx, caller, tripID)
	if errors.Is(err, ErrTripNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if trip.DriverID == "" {
		return nil, ErrNoDriverYet
	}
	return s.ensure(ctx, trip)
}

// TripChanged applies what the trip service published: a conversation opened
// on acceptance, and a closing time set when the trip ends. Idempotent, and
// indifferent to order, so a redelivered or late fact converges on the same
// conversation.
func (s *Service) TripChanged(ctx context.Context, trip Trip) error {
	_, err := s.ensure(ctx, trip)
	return err
}

func (s *Service) ensure(ctx context.Context, trip Trip) (*domain.Conversation, error) {
	now := s.now()
	conversation := domain.NewTripConversation(s.newID(), trip.ID, trip.RiderID, trip.DriverID, now)
	if !trip.EndedAt.IsZero() {
		conversation.ClosesAt = domain.ClosesAfter(trip.EndedAt, s.closeGrace)
	}

	stored, changed, err := s.repo.EnsureTrip(ctx, conversation)
	if err != nil {
		return nil, err
	}
	if changed {
		s.notifier.Changed(ctx, changeOf(stored, now), stored.Others(""))
	}
	return stored, nil
}

// Conversation is one conversation, for someone who may read it.
func (s *Service) Conversation(ctx context.Context, caller authz.Identity, id string) (*domain.Conversation, error) {
	conversation, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !conversation.CanRead(Viewer(caller)) {
		// NotFound rather than a refusal, as the trip service answers: saying a
		// conversation exists to someone who may not read it is a disclosure.
		return nil, domain.ErrNotFound
	}
	return conversation, nil
}

// List is the caller's conversations. Support asking for support
// conversations gets the queue: all of them, whoever holds them.
func (s *Service) List(ctx context.Context, caller authz.Identity, filter domain.ListFilter) (domain.Page, error) {
	filter.Now = s.now()
	filter.UserID = caller.UserID
	if caller.Role == authz.RoleOps && filter.Kind == domain.KindSupport {
		filter.UserID = ""
	}
	return s.repo.List(ctx, filter)
}

// Messages is a page of a conversation.
func (s *Service) Messages(ctx context.Context, caller authz.Identity, id string, query domain.MessageQuery) ([]domain.Message, bool, error) {
	if _, err := s.Conversation(ctx, caller, id); err != nil {
		return nil, false, err
	}
	return s.repo.Messages(ctx, id, query)
}

// Draft is a message as a client sends it.
type Draft struct {
	Body       string
	QuickReply string
	// ClientMessageID is required, so a retried send cannot send twice.
	ClientMessageID string
}

// Send stores a message and rings the others' doorbells.
func (s *Service) Send(ctx context.Context, caller authz.Identity, id string, draft Draft) (*domain.Message, error) {
	if draft.ClientMessageID == "" {
		return nil, ErrKeyRequired
	}

	conversation, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	now := s.now()
	claim, err := conversation.CheckPost(Viewer(caller), now)
	if err != nil {
		return nil, err
	}
	role := RoleOf(caller)
	code, body, err := domain.Compose(draft.Body, draft.QuickReply, role, conversation.Kind)
	if err != nil {
		return nil, err
	}

	// A retry is answered before the rate limit is asked, so the send that
	// used the last slot in the minute can still be retried.
	existing, err := s.repo.MessageByClientID(ctx, id, caller.UserID, draft.ClientMessageID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	sent, err := s.repo.SentSince(ctx, caller.UserID, now.Add(-time.Minute))
	if err != nil {
		return nil, err
	}
	if sent >= s.rateLimit {
		return nil, ErrRateLimited
	}

	if claim {
		conversation, err = s.repo.Claim(ctx, id,
			domain.Participant{UserID: caller.UserID, Role: domain.RoleSupport}, now)
		if err != nil {
			return nil, err
		}
	}

	message, err := s.repo.Append(ctx, domain.Message{
		ID: s.newID(), ConversationID: id,
		SenderID: caller.UserID, SenderRole: role,
		QuickReply: code, Body: body,
		ClientMessageID: draft.ClientMessageID, CreatedAt: now,
	}, now.Add(s.pushDelay))
	if err != nil {
		return nil, err
	}

	conversation.LastSeq = max(conversation.LastSeq, message.Seq)
	change := changeOf(conversation, now)
	s.notifier.Changed(ctx, change, conversation.Others(caller.UserID))
	if conversation.Kind == domain.KindSupport {
		s.notifier.SupportQueueChanged(ctx, change)
	}
	return message, nil
}

// MarkRead moves the caller's read marker forward, and tells the others.
//
// Support reading a conversation it is not in records nothing: a receipt says
// the person you are talking to has seen it, and support looking at a trip
// conversation is not that person.
func (s *Service) MarkRead(ctx context.Context, caller authz.Identity, id string, seq int64) (int64, error) {
	conversation, err := s.Conversation(ctx, caller, id)
	if err != nil {
		return 0, err
	}
	participant, in := conversation.Participant(caller.UserID)
	if !in {
		return 0, nil
	}

	seq = min(seq, conversation.LastSeq)
	if seq <= participant.LastReadSeq {
		return participant.LastReadSeq, nil
	}

	at, moved, err := s.repo.MarkRead(ctx, id, caller.UserID, seq)
	if err != nil {
		return 0, err
	}
	if moved {
		s.notifier.Read(ctx, id, caller.UserID, at, s.now(), conversation.Others(caller.UserID))
	}
	return at, nil
}

// Typing tells the others the caller is typing, at most once every
// TypingEvery however often a client calls it.
func (s *Service) Typing(ctx context.Context, caller authz.Identity, id string) error {
	conversation, err := s.Conversation(ctx, caller, id)
	if err != nil {
		return err
	}
	if _, in := conversation.Participant(caller.UserID); !in {
		return nil
	}
	now := s.now()
	if !conversation.OpenAt(now) {
		return domain.ErrClosed
	}
	if !s.announceTyping(typingKey{caller.UserID, id}, now) {
		return nil
	}
	s.notifier.Typing(ctx, id, caller.UserID, now, conversation.Others(caller.UserID))
	return nil
}

// announceTyping is the throttle. In memory, per instance: two instances can
// each let one through, which is two "typing" hints a second apart and not
// worth a shared store.
func (s *Service) announceTyping(key typingKey, now time.Time) bool {
	s.typingMu.Lock()
	defer s.typingMu.Unlock()

	if last, ok := s.typedAt[key]; ok && now.Sub(last) < s.typingEvery {
		return false
	}
	s.typedAt[key] = now

	// Forget the stale entries now and then, so the map is bounded by the
	// people typing lately rather than everyone who ever typed.
	if len(s.typedAt) > 10_000 {
		for other, at := range s.typedAt {
			if now.Sub(at) >= s.typingEvery {
				delete(s.typedAt, other)
			}
		}
	}
	return true
}

// SupportDraft is a rider or driver asking support for help.
type SupportDraft struct {
	// TripID is the trip it is about, which must be one the caller was on.
	// Empty for none.
	TripID  string
	Subject string
	Body    string
	Key     string
}

// CreateSupport opens a conversation with support, with its first message.
func (s *Service) CreateSupport(ctx context.Context, caller authz.Identity, draft SupportDraft) (*domain.Conversation, error) {
	if caller.Role == authz.RoleOps {
		return nil, domain.ErrNotAllowed
	}
	if draft.Key == "" {
		return nil, ErrKeyRequired
	}
	subject, err := domain.Subject(draft.Subject)
	if err != nil {
		return nil, err
	}
	role := RoleOf(caller)
	_, body, err := domain.Compose(draft.Body, "", role, domain.KindSupport)
	if err != nil {
		return nil, err
	}

	if draft.TripID != "" {
		// The trip service decides whose trip it is, so a support conversation
		// cannot be pinned to a stranger's ride.
		if _, err := s.trips.Trip(ctx, caller, draft.TripID); errors.Is(err, ErrTripNotFound) {
			return nil, &domain.InvalidError{Reason: "that trip is not one of yours"}
		} else if err != nil {
			return nil, err
		}
	}

	now := s.now()
	conversation := domain.NewSupportConversation(s.newID(), caller.UserID, role, draft.TripID, subject, now)
	stored, created, err := s.repo.CreateSupport(ctx, conversation, domain.Message{
		ID: s.newID(), ConversationID: conversation.ID, Seq: 1,
		SenderID: caller.UserID, SenderRole: role, Body: body,
		ClientMessageID: draft.Key, CreatedAt: now,
	})
	if err != nil {
		return nil, err
	}
	if created {
		s.notifier.SupportQueueChanged(ctx, changeOf(stored, now))
	}
	return stored, nil
}

// Claim makes the caller, support only, the one handling a support
// conversation. Claiming one you already hold is a no-op.
func (s *Service) Claim(ctx context.Context, caller authz.Identity, id string) (*domain.Conversation, error) {
	if caller.Role != authz.RoleOps {
		return nil, domain.ErrNotAllowed
	}
	conversation, err := s.Conversation(ctx, caller, id)
	if err != nil {
		return nil, err
	}
	if conversation.Kind != domain.KindSupport {
		return nil, domain.ErrNotAllowed
	}
	now := s.now()
	if !conversation.OpenAt(now) {
		return nil, domain.ErrClosed
	}
	if conversation.AssigneeID == caller.UserID {
		return conversation, nil
	}

	claimed, err := s.repo.Claim(ctx, id, domain.Participant{UserID: caller.UserID, Role: domain.RoleSupport}, now)
	if err != nil {
		return nil, err
	}
	change := changeOf(claimed, now)
	s.notifier.Changed(ctx, change, claimed.Others(caller.UserID))
	s.notifier.SupportQueueChanged(ctx, change)
	return claimed, nil
}

// Resolve ends a support conversation, by support or by whoever opened it.
// Resolving a resolved conversation is a no-op.
func (s *Service) Resolve(ctx context.Context, caller authz.Identity, id string) (*domain.Conversation, error) {
	conversation, err := s.Conversation(ctx, caller, id)
	if err != nil {
		return nil, err
	}
	if conversation.Kind != domain.KindSupport {
		return nil, domain.ErrNotAllowed
	}
	if caller.Role != authz.RoleOps && caller.UserID != conversation.RequesterID {
		return nil, domain.ErrNotAllowed
	}
	if conversation.Status == domain.StatusResolved {
		return conversation, nil
	}

	now := s.now()
	resolved, err := s.repo.Resolve(ctx, id, now)
	if err != nil {
		return nil, err
	}
	change := changeOf(resolved, now)
	s.notifier.Changed(ctx, change, resolved.Others(caller.UserID))
	s.notifier.SupportQueueChanged(ctx, change)
	return resolved, nil
}
