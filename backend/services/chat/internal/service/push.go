package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ishakdeveloper/surge/services/chat/internal/domain"
	"github.com/ishakdeveloper/surge/shared/authz"
)

// ErrInvalidPushToken is a token that is not Expo's.
var ErrInvalidPushToken = errors.New("service: not an Expo push token")

// validPushToken accepts Expo's two spellings and nothing that could be
// mistaken for them.
func validPushToken(token string) bool {
	return len(token) <= 256 && strings.HasSuffix(token, "]") &&
		(strings.HasPrefix(token, "ExponentPushToken[") || strings.HasPrefix(token, "ExpoPushToken["))
}

// RegisterPushToken lets the caller's device be notified. A token already
// registered to someone else moves to the caller: it is one phone, and it is
// theirs now.
func (s *Service) RegisterPushToken(ctx context.Context, caller authz.Identity, token string, platform domain.Platform) error {
	if !validPushToken(token) {
		return ErrInvalidPushToken
	}
	return s.repo.SavePushToken(ctx, domain.PushToken{
		Token: token, UserID: caller.UserID, Platform: platform, UpdatedAt: s.now(),
	})
}

// UnregisterPushToken stops notifying a device, and only one the caller holds.
func (s *Service) UnregisterPushToken(ctx context.Context, caller authz.Identity, token string) error {
	return s.repo.DeletePushToken(ctx, caller.UserID, token)
}

// Notification is one device notification.
type Notification struct {
	// To is the device's push token.
	To    string
	Title string
	Body  string
	// Data travels with the notification, for the app to open the right
	// conversation when it is tapped.
	Data map[string]string
}

// Pusher delivers notifications to devices.
type Pusher interface {
	// Send delivers what it can. It returns the tokens the provider says are
	// gone for good, and an error only when the whole batch should be retried.
	Send(ctx context.Context, notifications []Notification) (unregistered []string, err error)
}

// PushStore is the queue of notifications owed, and what deciding one needs.
type PushStore interface {
	// LeasePushes takes up to limit jobs due at now, and hides them from other
	// workers until until. A worker that dies holding a lease loses nothing:
	// the jobs come due again when it runs out.
	LeasePushes(ctx context.Context, now, until time.Time, limit int) ([]domain.PushJob, error)
	FinishPushes(ctx context.Context, ids []int64) error
	RetryPushes(ctx context.Context, ids []int64, notBefore time.Time) error
	PushTokens(ctx context.Context, userID string) ([]domain.PushToken, error)
	DeletePushTokens(ctx context.Context, tokens []string) error

	Get(ctx context.Context, id string) (*domain.Conversation, error)
	Messages(ctx context.Context, conversationID string, query domain.MessageQuery) ([]domain.Message, bool, error)
}

// PushHooks are the observability seams.
type PushHooks struct {
	// OnSent counts notifications handed to the provider.
	OnSent func(n int)
	// OnSuppressed counts jobs dropped because the message was read in time,
	// or because the recipient has no device.
	OnSuppressed func(reason string, n int)
	// OnFailed counts batches the provider refused, to be retried.
	OnFailed func()
}

type PushOptions struct {
	Store  PushStore
	Pusher Pusher
	Now    func() time.Time
	// Lease is how long a leased job is hidden: longer than one send can take.
	Lease time.Duration
	// Batch is how many jobs one pass leases.
	Batch int
	// MaxAttempts is how many failed sends a job gets before it is dropped. A
	// notification an hour late about a pickup is noise, not news.
	MaxAttempts int
	Hooks       PushHooks
}

// PushWorker sends the notifications messages owe, once they have gone
// unread for the push delay.
//
// Every instance runs one. Leasing with skip-locked is what makes that safe:
// two workers never hold the same job, and neither has to know the other
// exists.
type PushWorker struct {
	store       PushStore
	pusher      Pusher
	now         func() time.Time
	lease       time.Duration
	batch       int
	maxAttempts int
	hooks       PushHooks
}

func NewPushWorker(options PushOptions) *PushWorker {
	w := &PushWorker{
		store: options.Store, pusher: options.Pusher, now: options.Now,
		lease: options.Lease, batch: options.Batch, maxAttempts: options.MaxAttempts,
		hooks: options.Hooks,
	}
	if w.now == nil {
		w.now = time.Now
	}
	if w.lease <= 0 {
		w.lease = 30 * time.Second
	}
	if w.batch <= 0 {
		w.batch = 100
	}
	if w.maxAttempts <= 0 {
		w.maxAttempts = 5
	}
	return w
}

// Run drains the queue every tick until ctx ends.
func (w *PushWorker) Run(ctx context.Context, every time.Duration) error {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.Drain(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("push pass did not finish; the next one retries", "error", err)
			}
		}
	}
}

// Drain sends everything due now, a batch at a time.
func (w *PushWorker) Drain(ctx context.Context) error {
	for {
		leased, err := w.Once(ctx)
		if err != nil || leased < w.batch {
			return err
		}
	}
}

// group is one recipient's jobs in one conversation: one notification,
// however many messages it is about.
type group struct {
	recipientID    string
	conversationID string
	seq            int64
	attempts       int
	ids            []int64
}

// Once handles one batch, and returns how many jobs it leased.
func (w *PushWorker) Once(ctx context.Context) (int, error) {
	now := w.now()
	jobs, err := w.store.LeasePushes(ctx, now, now.Add(w.lease), w.batch)
	if err != nil || len(jobs) == 0 {
		return 0, err
	}

	var groups []*group
	byKey := make(map[[2]string]*group)
	for _, job := range jobs {
		key := [2]string{job.RecipientID, job.ConversationID}
		g, ok := byKey[key]
		if !ok {
			g = &group{recipientID: job.RecipientID, conversationID: job.ConversationID}
			byKey[key] = g
			groups = append(groups, g)
		}
		g.seq = max(g.seq, job.Seq)
		g.attempts = max(g.attempts, job.Attempts)
		g.ids = append(g.ids, job.ID)
	}

	var (
		done          []int64
		sending       []*group
		notifications []Notification
	)
	for _, g := range groups {
		prepared, reason, err := w.prepare(ctx, g)
		switch {
		case err != nil:
			// Left leased: it comes due again when the lease runs out.
			slog.Warn("push could not be prepared", "conversation", g.conversationID, "error", err)
		case reason != "":
			done = append(done, g.ids...)
			if w.hooks.OnSuppressed != nil {
				w.hooks.OnSuppressed(reason, len(g.ids))
			}
		default:
			sending = append(sending, g)
			notifications = append(notifications, prepared...)
		}
	}

	if len(notifications) > 0 {
		unregistered, err := w.pusher.Send(ctx, notifications)
		if err != nil {
			if w.hooks.OnFailed != nil {
				w.hooks.OnFailed()
			}
			slog.Warn("push provider refused a batch; retrying", "notifications", len(notifications), "error", err)
			for _, g := range sending {
				if g.attempts+1 >= w.maxAttempts {
					done = append(done, g.ids...)
					continue
				}
				if err := w.store.RetryPushes(ctx, g.ids, now.Add(backoff(g.attempts))); err != nil {
					return len(jobs), fmt.Errorf("service: retry pushes: %w", err)
				}
			}
		} else {
			for _, g := range sending {
				done = append(done, g.ids...)
			}
			if w.hooks.OnSent != nil {
				w.hooks.OnSent(len(notifications))
			}
			if len(unregistered) > 0 {
				if err := w.store.DeletePushTokens(ctx, unregistered); err != nil {
					return len(jobs), fmt.Errorf("service: prune push tokens: %w", err)
				}
			}
		}
	}

	if err := w.store.FinishPushes(ctx, done); err != nil {
		return len(jobs), fmt.Errorf("service: finish pushes: %w", err)
	}
	return len(jobs), nil
}

// prepare decides one group: the notifications to send, or why none are.
func (w *PushWorker) prepare(ctx context.Context, g *group) ([]Notification, string, error) {
	conversation, err := w.store.Get(ctx, g.conversationID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, "gone", nil
	}
	if err != nil {
		return nil, "", err
	}
	recipient, in := conversation.Participant(g.recipientID)
	if !in {
		return nil, "gone", nil
	}
	if recipient.LastReadSeq >= g.seq {
		// Read within the delay: they were looking, and a notification about
		// what they have seen is noise.
		return nil, "read", nil
	}

	tokens, err := w.store.PushTokens(ctx, g.recipientID)
	if err != nil {
		return nil, "", err
	}
	if len(tokens) == 0 {
		return nil, "no_device", nil
	}

	latest, _, err := w.store.Messages(ctx, g.conversationID, domain.MessageQuery{Before: g.seq + 1, Limit: 1})
	if err != nil {
		return nil, "", err
	}
	if len(latest) == 0 {
		return nil, "gone", nil
	}

	title, body := compose(latest[0], g.seq-recipient.LastReadSeq)
	notifications := make([]Notification, 0, len(tokens))
	for _, token := range tokens {
		notifications = append(notifications, Notification{
			To: token.Token, Title: title, Body: body,
			Data: map[string]string{
				"conversationId": conversation.ID,
				"kind":           string(conversation.Kind),
				"tripId":         conversation.TripID,
			},
		})
	}
	return notifications, "", nil
}

// compose is the notification's words. The sender is named by their side of
// the conversation, which is all chat knows about them and all a lock screen
// needs.
func compose(latest domain.Message, unread int64) (title, body string) {
	switch latest.SenderRole {
	case domain.RoleDriver:
		title = "Your driver"
	case domain.RoleRider:
		title = "Your rider"
	default:
		title = "Surge Support"
	}
	if unread > 1 {
		return title, fmt.Sprintf("%d new messages", unread)
	}
	return title, truncate(latest.Body, 140)
}

func truncate(text string, most int) string {
	if utf8.RuneCountInString(text) <= most {
		return text
	}
	runes := []rune(text)
	return string(runes[:most-1]) + "…"
}

// backoff is how long a failed send waits before its next try.
func backoff(attempts int) time.Duration {
	return min(15*time.Second<<attempts, 5*time.Minute)
}
