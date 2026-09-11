package repository

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/ishakdeveloper/surge/services/chat/internal/domain"
)

// InMemory is the repository the service's tests run on. It mirrors the
// Postgres one's rules — one conversation per trip, gapless seqs, the open
// check at append, monotonic read markers, the claim compare-and-set — so a
// test that passes here is about the service, not about a map.
type InMemory struct {
	mu            sync.Mutex
	conversations map[string]*domain.Conversation
	messages      map[string][]domain.Message
	jobs          []job
	nextJob       int64
	tokens        map[string]domain.PushToken
}

type job struct {
	domain.PushJob
	notBefore time.Time
}

func NewInMemory() *InMemory {
	return &InMemory{
		conversations: make(map[string]*domain.Conversation),
		messages:      make(map[string][]domain.Message),
		tokens:        make(map[string]domain.PushToken),
	}
}

func clone(conversation *domain.Conversation) *domain.Conversation {
	copied := *conversation
	copied.Participants = slices.Clone(conversation.Participants)
	return &copied
}

func (m *InMemory) byTrip(tripID string) *domain.Conversation {
	for _, conversation := range m.conversations {
		if conversation.Kind == domain.KindTrip && conversation.TripID == tripID {
			return conversation
		}
	}
	return nil
}

func (m *InMemory) EnsureTrip(_ context.Context, conversation *domain.Conversation) (*domain.Conversation, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing := m.byTrip(conversation.TripID)
	if existing == nil {
		m.conversations[conversation.ID] = clone(conversation)
		return clone(conversation), true, nil
	}
	if existing.ClosesAt.IsZero() && !conversation.ClosesAt.IsZero() {
		existing.ClosesAt = conversation.ClosesAt
		existing.UpdatedAt = conversation.CreatedAt
		return clone(existing), true, nil
	}
	return clone(existing), false, nil
}

func (m *InMemory) Get(_ context.Context, id string) (*domain.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conversation, ok := m.conversations[id]
	if !ok {
		return nil, fmt.Errorf("repository: conversation: %w", domain.ErrNotFound)
	}
	return clone(conversation), nil
}

func (m *InMemory) GetByTrip(_ context.Context, tripID string) (*domain.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conversation := m.byTrip(tripID)
	if conversation == nil {
		return nil, fmt.Errorf("repository: conversation: %w", domain.ErrNotFound)
	}
	return clone(conversation), nil
}

// newestFirst is the list order: created_at, then id, descending.
func newestFirst(a, b *domain.Conversation) int {
	if order := b.CreatedAt.Compare(a.CreatedAt); order != 0 {
		return order
	}
	return cmp.Compare(b.ID, a.ID)
}

func (m *InMemory) List(_ context.Context, filter domain.ListFilter) (domain.Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var matched []*domain.Conversation
	for _, conversation := range m.conversations {
		if filter.UserID != "" {
			if _, in := conversation.Participant(filter.UserID); !in {
				continue
			}
		}
		if filter.Kind != "" && conversation.Kind != filter.Kind {
			continue
		}
		if filter.State != "" && conversation.StateAt(filter.Now) != filter.State {
			continue
		}
		matched = append(matched, conversation)
	}
	slices.SortFunc(matched, newestFirst)

	if filter.Cursor != "" {
		cursor, ok := m.conversations[filter.Cursor]
		if !ok {
			return domain.Page{}, nil
		}
		matched = slices.DeleteFunc(matched, func(c *domain.Conversation) bool { return newestFirst(cursor, c) >= 0 })
	}

	limit := domain.ConversationPage(filter.Limit)
	page := domain.Page{}
	if len(matched) > limit {
		matched = matched[:limit]
		page.NextCursor = matched[limit-1].ID
	}
	for _, conversation := range matched {
		page.Conversations = append(page.Conversations, *clone(conversation))
	}
	return page, nil
}

func (m *InMemory) CreateSupport(_ context.Context, conversation *domain.Conversation, first domain.Message) (*domain.Conversation, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, existing := range m.conversations {
		if existing.Kind != domain.KindSupport || existing.RequesterID != conversation.RequesterID {
			continue
		}
		if messages := m.messages[existing.ID]; len(messages) > 0 && messages[0].ClientMessageID == first.ClientMessageID {
			return clone(existing), false, nil
		}
	}

	stored := clone(conversation)
	stored.LastSeq = 1
	for i := range stored.Participants {
		stored.Participants[i].LastReadSeq = 1
	}
	m.conversations[stored.ID] = stored
	m.messages[stored.ID] = []domain.Message{first}
	return clone(stored), true, nil
}

func (m *InMemory) messageByClientID(conversationID, senderID, clientMessageID string) (domain.Message, bool) {
	for _, message := range m.messages[conversationID] {
		if message.SenderID == senderID && message.ClientMessageID == clientMessageID && clientMessageID != "" {
			return message, true
		}
	}
	return domain.Message{}, false
}

func (m *InMemory) MessageByClientID(_ context.Context, conversationID, senderID, clientMessageID string) (*domain.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	message, ok := m.messageByClientID(conversationID, senderID, clientMessageID)
	if !ok {
		return nil, fmt.Errorf("repository: message by client id: %w", domain.ErrNotFound)
	}
	return &message, nil
}

func (m *InMemory) Append(_ context.Context, message domain.Message, pushAt time.Time) (*domain.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	conversation, ok := m.conversations[message.ConversationID]
	if !ok {
		return nil, fmt.Errorf("repository: append: %w", domain.ErrNotFound)
	}
	if !conversation.OpenAt(message.CreatedAt) {
		return nil, fmt.Errorf("repository: append: %w", domain.ErrClosed)
	}
	if existing, ok := m.messageByClientID(message.ConversationID, message.SenderID, message.ClientMessageID); ok {
		return &existing, nil
	}

	conversation.LastSeq++
	conversation.UpdatedAt = message.CreatedAt
	message.Seq = conversation.LastSeq
	m.messages[message.ConversationID] = append(m.messages[message.ConversationID], message)

	for i, participant := range conversation.Participants {
		if participant.UserID == message.SenderID {
			conversation.Participants[i].LastReadSeq = max(participant.LastReadSeq, message.Seq)
			continue
		}
		m.nextJob++
		m.jobs = append(m.jobs, job{
			PushJob: domain.PushJob{
				ID: m.nextJob, RecipientID: participant.UserID,
				ConversationID: message.ConversationID, Seq: message.Seq,
			},
			notBefore: pushAt,
		})
	}
	return &message, nil
}

func (m *InMemory) Messages(_ context.Context, conversationID string, query domain.MessageQuery) ([]domain.Message, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	all := m.messages[conversationID]
	var window []domain.Message
	for _, message := range all {
		if query.After > 0 && message.Seq <= query.After {
			continue
		}
		if query.Before > 0 && message.Seq >= query.Before {
			continue
		}
		window = append(window, message)
	}

	limit := domain.MessagePage(query.Limit)
	more := len(window) > limit
	if !more {
		return window, false, nil
	}
	if query.After > 0 {
		return slices.Clone(window[:limit]), true, nil
	}
	return slices.Clone(window[len(window)-limit:]), true, nil
}

func (m *InMemory) MarkRead(_ context.Context, conversationID, userID string, seq int64) (int64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	conversation, ok := m.conversations[conversationID]
	if !ok {
		return 0, false, fmt.Errorf("repository: mark read: %w", domain.ErrNotFound)
	}
	for i, participant := range conversation.Participants {
		if participant.UserID != userID {
			continue
		}
		if participant.LastReadSeq >= seq {
			return participant.LastReadSeq, false, nil
		}
		conversation.Participants[i].LastReadSeq = seq
		return seq, true, nil
	}
	return 0, false, fmt.Errorf("repository: mark read: %w", domain.ErrNotFound)
}

func (m *InMemory) SentSince(_ context.Context, senderID string, since time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sent := 0
	for _, messages := range m.messages {
		for _, message := range messages {
			if message.SenderID == senderID && message.CreatedAt.After(since) {
				sent++
			}
		}
	}
	return sent, nil
}

func (m *InMemory) Claim(_ context.Context, conversationID string, assignee domain.Participant, now time.Time) (*domain.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	conversation, ok := m.conversations[conversationID]
	if !ok || conversation.Kind != domain.KindSupport {
		return nil, fmt.Errorf("repository: claim: %w", domain.ErrNotFound)
	}
	if conversation.AssigneeID != "" && conversation.AssigneeID != assignee.UserID {
		return nil, fmt.Errorf("repository: claim: %w", domain.ErrClaimed)
	}
	conversation.AssigneeID = assignee.UserID
	conversation.UpdatedAt = now
	if _, in := conversation.Participant(assignee.UserID); !in {
		conversation.Participants = append(conversation.Participants, assignee)
	}
	return clone(conversation), nil
}

func (m *InMemory) Resolve(_ context.Context, conversationID string, now time.Time) (*domain.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	conversation, ok := m.conversations[conversationID]
	if !ok || conversation.Kind != domain.KindSupport {
		return nil, fmt.Errorf("repository: resolve: %w", domain.ErrNotFound)
	}
	conversation.Status = domain.StatusResolved
	conversation.UpdatedAt = now
	return clone(conversation), nil
}

func (m *InMemory) SavePushToken(_ context.Context, token domain.PushToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[token.Token] = token
	return nil
}

func (m *InMemory) DeletePushToken(_ context.Context, userID, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.tokens[token]; ok && existing.UserID == userID {
		delete(m.tokens, token)
	}
	return nil
}

func (m *InMemory) PushTokens(_ context.Context, userID string) ([]domain.PushToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var tokens []domain.PushToken
	for _, token := range m.tokens {
		if token.UserID == userID {
			tokens = append(tokens, token)
		}
	}
	slices.SortFunc(tokens, func(a, b domain.PushToken) int { return cmp.Compare(a.Token, b.Token) })
	return tokens, nil
}

func (m *InMemory) DeletePushTokens(_ context.Context, tokens []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, token := range tokens {
		delete(m.tokens, token)
	}
	return nil
}

func (m *InMemory) LeasePushes(_ context.Context, now, until time.Time, limit int) ([]domain.PushJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	slices.SortStableFunc(m.jobs, func(a, b job) int { return a.notBefore.Compare(b.notBefore) })
	var leased []domain.PushJob
	for i := range m.jobs {
		if len(leased) == limit {
			break
		}
		if m.jobs[i].notBefore.After(now) {
			continue
		}
		m.jobs[i].notBefore = until
		leased = append(leased, m.jobs[i].PushJob)
	}
	return leased, nil
}

func (m *InMemory) FinishPushes(_ context.Context, ids []int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs = slices.DeleteFunc(m.jobs, func(j job) bool { return slices.Contains(ids, j.ID) })
	return nil
}

func (m *InMemory) RetryPushes(_ context.Context, ids []int64, notBefore time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.jobs {
		if slices.Contains(ids, m.jobs[i].ID) {
			m.jobs[i].Attempts++
			m.jobs[i].notBefore = notBefore
		}
	}
	return nil
}

// PendingPushes is how many notifications are still owed, for tests.
func (m *InMemory) PendingPushes() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.jobs)
}
