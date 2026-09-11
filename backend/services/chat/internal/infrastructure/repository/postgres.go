// Package repository is where chat's conversations, messages and owed
// notifications live.
package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ishakdeveloper/surge/services/chat/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres is the durable repository, and the only one the service runs on.
type Postgres struct{ pool *pgxpool.Pool }

func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

type scanner interface{ Scan(dest ...any) error }

// querier is a pool or a transaction.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func nullable(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

const conversationColumns = `id, kind, trip_id, status, subject, requester_id, assignee_id,
	last_seq, closes_at, created_at, updated_at`

func scanConversation(row scanner) (*domain.Conversation, error) {
	var (
		conversation domain.Conversation
		kind, status string
		closesAt     *time.Time
	)
	err := row.Scan(&conversation.ID, &kind, &conversation.TripID, &status, &conversation.Subject,
		&conversation.RequesterID, &conversation.AssigneeID, &conversation.LastSeq, &closesAt,
		&conversation.CreatedAt, &conversation.UpdatedAt)
	if err != nil {
		return nil, err
	}
	conversation.Kind = domain.Kind(kind)
	conversation.Status = domain.Status(status)
	if closesAt != nil {
		conversation.ClosesAt = *closesAt
	}
	return &conversation, nil
}

// withParticipants fills in who is in each conversation, in one query.
func withParticipants(ctx context.Context, q querier, conversations ...*domain.Conversation) error {
	if len(conversations) == 0 {
		return nil
	}
	ids := make([]string, len(conversations))
	byID := make(map[string]*domain.Conversation, len(conversations))
	for i, conversation := range conversations {
		ids[i] = conversation.ID
		byID[conversation.ID] = conversation
		conversation.Participants = nil
	}

	rows, err := q.Query(ctx, `
		select conversation_id, user_id, role, last_read_seq
		from chat_participant
		where conversation_id = any($1)
		order by joined_at, user_id`, ids)
	if err != nil {
		return fmt.Errorf("repository: participants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			conversationID, role string
			participant          domain.Participant
		)
		if err := rows.Scan(&conversationID, &participant.UserID, &role, &participant.LastReadSeq); err != nil {
			return fmt.Errorf("repository: participants: %w", err)
		}
		participant.Role = domain.Role(role)
		conversation := byID[conversationID]
		conversation.Participants = append(conversation.Participants, participant)
	}
	return rows.Err()
}

func one(ctx context.Context, q querier, where string, args ...any) (*domain.Conversation, error) {
	conversation, err := scanConversation(q.QueryRow(ctx,
		`select `+conversationColumns+` from chat_conversation where `+where, args...))
	if err != nil {
		return nil, fmt.Errorf("repository: conversation: %w", notFound(err))
	}
	if err := withParticipants(ctx, q, conversation); err != nil {
		return nil, err
	}
	return conversation, nil
}

func (r *Postgres) Get(ctx context.Context, id string) (*domain.Conversation, error) {
	return one(ctx, r.pool, "id = $1", id)
}

func (r *Postgres) GetByTrip(ctx context.Context, tripID string) (*domain.Conversation, error) {
	return one(ctx, r.pool, "kind = 'trip' and trip_id = $1", tripID)
}

func (r *Postgres) EnsureTrip(ctx context.Context, conversation *domain.Conversation) (*domain.Conversation, bool, error) {
	var (
		stored  *domain.Conversation
		changed bool
	)
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// On conflict do nothing rather than read-then-insert: the consumer and
		// a rider's first tap can arrive together, and the unique index is
		// what decides which one created it.
		tag, err := tx.Exec(ctx, `
			insert into chat_conversation (id, kind, trip_id, status, requester_id, closes_at, created_at, updated_at)
			values ($1, 'trip', $2, 'open', $3, $4, $5, $5)
			on conflict (trip_id) where kind = 'trip' do nothing`,
			conversation.ID, conversation.TripID, conversation.RequesterID,
			nullable(conversation.ClosesAt), conversation.CreatedAt)
		if err != nil {
			return err
		}

		if tag.RowsAffected() == 1 {
			changed = true
			for _, participant := range conversation.Participants {
				if _, err := tx.Exec(ctx, `
					insert into chat_participant (conversation_id, user_id, role, joined_at)
					values ($1, $2, $3, $4)
					on conflict do nothing`,
					conversation.ID, participant.UserID, string(participant.Role), conversation.CreatedAt); err != nil {
					return err
				}
			}
		} else if !conversation.ClosesAt.IsZero() {
			// The first end wins: a completion redelivered an hour later must
			// not reopen a conversation that has closed.
			tag, err := tx.Exec(ctx, `
				update chat_conversation set closes_at = $2, updated_at = $3
				where kind = 'trip' and trip_id = $1 and closes_at is null`,
				conversation.TripID, conversation.ClosesAt, conversation.CreatedAt)
			if err != nil {
				return err
			}
			changed = tag.RowsAffected() == 1
		}

		stored, err = one(ctx, tx, "kind = 'trip' and trip_id = $1", conversation.TripID)
		return err
	})
	if err != nil {
		return nil, false, fmt.Errorf("repository: ensure trip conversation: %w", err)
	}
	return stored, changed, nil
}

func (r *Postgres) List(ctx context.Context, filter domain.ListFilter) (domain.Page, error) {
	limit := domain.ConversationPage(filter.Limit)

	var (
		where []string
		args  []any
	)
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if filter.UserID != "" {
		where = append(where, `exists (select 1 from chat_participant p
			where p.conversation_id = c.id and p.user_id = `+arg(filter.UserID)+`)`)
	}
	if filter.Kind != "" {
		where = append(where, "c.kind = "+arg(string(filter.Kind)))
	}
	switch filter.State {
	case domain.StateOpen:
		where = append(where, "c.status = 'open' and (c.closes_at is null or c.closes_at > "+arg(filter.Now)+")")
	case domain.StateClosed:
		where = append(where, "c.status = 'open' and c.closes_at <= "+arg(filter.Now))
	case domain.StateResolved:
		where = append(where, "c.status = 'resolved'")
	}
	if filter.Cursor != "" {
		where = append(where, `(c.created_at, c.id) <
			(select created_at, id from chat_conversation where id = `+arg(filter.Cursor)+`)`)
	}

	query := `select ` + strings.ReplaceAll("c."+conversationColumns, ", ", ", c.") + ` from chat_conversation c`
	if len(where) > 0 {
		query += " where " + strings.Join(where, " and ")
	}
	query += " order by c.created_at desc, c.id desc limit " + arg(limit+1)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return domain.Page{}, fmt.Errorf("repository: list conversations: %w", err)
	}
	conversations, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.Conversation, error) {
		return scanConversation(row)
	})
	if err != nil {
		return domain.Page{}, fmt.Errorf("repository: list conversations: %w", err)
	}

	page := domain.Page{}
	if len(conversations) > limit {
		conversations = conversations[:limit]
		page.NextCursor = conversations[limit-1].ID
	}
	if err := withParticipants(ctx, r.pool, conversations...); err != nil {
		return domain.Page{}, err
	}
	page.Conversations = make([]domain.Conversation, len(conversations))
	for i, conversation := range conversations {
		page.Conversations[i] = *conversation
	}
	return page, nil
}

func (r *Postgres) CreateSupport(ctx context.Context, conversation *domain.Conversation, first domain.Message) (*domain.Conversation, bool, error) {
	id := conversation.ID
	created := false
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			insert into chat_conversation (id, kind, trip_id, status, subject, requester_id,
			  idempotency_key, last_seq, created_at, updated_at)
			values ($1, 'support', $2, 'open', $3, $4, $5, 1, $6, $6)
			on conflict (requester_id, idempotency_key)
			  where kind = 'support' and idempotency_key <> '' do nothing`,
			conversation.ID, conversation.TripID, conversation.Subject, conversation.RequesterID,
			first.ClientMessageID, conversation.CreatedAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return tx.QueryRow(ctx, `
				select id from chat_conversation
				where kind = 'support' and requester_id = $1 and idempotency_key = $2`,
				conversation.RequesterID, first.ClientMessageID).Scan(&id)
		}

		created = true
		for _, participant := range conversation.Participants {
			// The requester has read their own first message.
			if _, err := tx.Exec(ctx, `
				insert into chat_participant (conversation_id, user_id, role, last_read_seq, joined_at)
				values ($1, $2, $3, 1, $4)`,
				conversation.ID, participant.UserID, string(participant.Role), conversation.CreatedAt); err != nil {
				return err
			}
		}
		_, err = insertMessage(ctx, tx, first)
		return err
	})
	if err != nil {
		return nil, false, fmt.Errorf("repository: create support conversation: %w", err)
	}
	stored, err := r.Get(ctx, id)
	return stored, created, err
}

const messageColumns = `id, conversation_id, seq, sender_id, sender_role, quick_reply, body,
	client_message_id, created_at`

func scanMessage(row scanner) (domain.Message, error) {
	var (
		message domain.Message
		role    string
	)
	err := row.Scan(&message.ID, &message.ConversationID, &message.Seq, &message.SenderID, &role,
		&message.QuickReply, &message.Body, &message.ClientMessageID, &message.CreatedAt)
	message.SenderRole = domain.Role(role)
	return message, err
}

// insertMessage stores a message, or reports that its client id already is.
func insertMessage(ctx context.Context, tx pgx.Tx, message domain.Message) (bool, error) {
	tag, err := tx.Exec(ctx, `
		insert into chat_message (conversation_id, seq, id, sender_id, sender_role, quick_reply,
		  body, client_message_id, created_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		on conflict (conversation_id, sender_id, client_message_id)
		  where client_message_id <> '' do nothing`,
		message.ConversationID, message.Seq, message.ID, message.SenderID, string(message.SenderRole),
		message.QuickReply, message.Body, message.ClientMessageID, message.CreatedAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Postgres) MessageByClientID(ctx context.Context, conversationID, senderID, clientMessageID string) (*domain.Message, error) {
	message, err := scanMessage(r.pool.QueryRow(ctx, `
		select `+messageColumns+` from chat_message
		where conversation_id = $1 and sender_id = $2 and client_message_id = $3
		  and client_message_id <> ''`,
		conversationID, senderID, clientMessageID))
	if err != nil {
		return nil, fmt.Errorf("repository: message by client id: %w", notFound(err))
	}
	return &message, nil
}

// errDuplicate unwinds a send whose client id another send stored first, so
// the seq it took is given back with the rest of the transaction.
var errDuplicate = errors.New("repository: message already stored")

func (r *Postgres) Append(ctx context.Context, message domain.Message, pushAt time.Time) (*domain.Message, error) {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// Numbering and the open check in one statement. The row lock it takes
		// is what serializes sends in a conversation, so seqs have no gaps and
		// no message lands after the conversation has closed.
		err := tx.QueryRow(ctx, `
			update chat_conversation set last_seq = last_seq + 1, updated_at = $2
			where id = $1 and status = 'open' and (closes_at is null or closes_at > $2)
			returning last_seq`,
			message.ConversationID, message.CreatedAt).Scan(&message.Seq)
		if errors.Is(err, pgx.ErrNoRows) {
			var exists bool
			if err := tx.QueryRow(ctx,
				`select exists (select 1 from chat_conversation where id = $1)`,
				message.ConversationID).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return domain.ErrClosed
			}
			return domain.ErrNotFound
		}
		if err != nil {
			return err
		}

		inserted, err := insertMessage(ctx, tx, message)
		if err != nil {
			return err
		}
		if !inserted {
			return errDuplicate
		}

		if _, err := tx.Exec(ctx, `
			update chat_participant set last_read_seq = greatest(last_read_seq, $3)
			where conversation_id = $1 and user_id = $2`,
			message.ConversationID, message.SenderID, message.Seq); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			insert into chat_push_job (recipient_id, conversation_id, seq, not_before)
			select user_id, $1, $2, $3 from chat_participant
			where conversation_id = $1 and user_id <> $4`,
			message.ConversationID, message.Seq, pushAt, message.SenderID)
		return err
	})
	switch {
	case errors.Is(err, errDuplicate):
		return r.MessageByClientID(ctx, message.ConversationID, message.SenderID, message.ClientMessageID)
	case err != nil:
		return nil, fmt.Errorf("repository: append: %w", err)
	}
	return &message, nil
}

func (r *Postgres) Messages(ctx context.Context, conversationID string, query domain.MessageQuery) ([]domain.Message, bool, error) {
	limit := domain.MessagePage(query.Limit)

	var (
		rows       pgx.Rows
		err        error
		descending = true
	)
	switch {
	case query.After > 0:
		descending = false
		rows, err = r.pool.Query(ctx, `select `+messageColumns+` from chat_message
			where conversation_id = $1 and seq > $2 order by seq limit $3`,
			conversationID, query.After, limit+1)
	case query.Before > 0:
		rows, err = r.pool.Query(ctx, `select `+messageColumns+` from chat_message
			where conversation_id = $1 and seq < $2 order by seq desc limit $3`,
			conversationID, query.Before, limit+1)
	default:
		rows, err = r.pool.Query(ctx, `select `+messageColumns+` from chat_message
			where conversation_id = $1 order by seq desc limit $2`,
			conversationID, limit+1)
	}
	if err != nil {
		return nil, false, fmt.Errorf("repository: messages: %w", err)
	}
	messages, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Message, error) {
		return scanMessage(row)
	})
	if err != nil {
		return nil, false, fmt.Errorf("repository: messages: %w", err)
	}

	more := len(messages) > limit
	if more {
		messages = messages[:limit]
	}
	if descending {
		slices.Reverse(messages)
	}
	return messages, more, nil
}

func (r *Postgres) MarkRead(ctx context.Context, conversationID, userID string, seq int64) (int64, bool, error) {
	var at int64
	err := r.pool.QueryRow(ctx, `
		update chat_participant set last_read_seq = $3
		where conversation_id = $1 and user_id = $2 and last_read_seq < $3
		returning last_read_seq`,
		conversationID, userID, seq).Scan(&at)
	if err == nil {
		return at, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, fmt.Errorf("repository: mark read: %w", err)
	}

	// Already there or further: a late receipt, which is not an error.
	err = r.pool.QueryRow(ctx, `
		select last_read_seq from chat_participant where conversation_id = $1 and user_id = $2`,
		conversationID, userID).Scan(&at)
	if err != nil {
		return 0, false, fmt.Errorf("repository: read marker: %w", notFound(err))
	}
	return at, false, nil
}

func (r *Postgres) SentSince(ctx context.Context, senderID string, since time.Time) (int, error) {
	var sent int
	err := r.pool.QueryRow(ctx,
		`select count(*) from chat_message where sender_id = $1 and created_at > $2`,
		senderID, since).Scan(&sent)
	if err != nil {
		return 0, fmt.Errorf("repository: sent since: %w", err)
	}
	return sent, nil
}

func (r *Postgres) Claim(ctx context.Context, conversationID string, assignee domain.Participant, now time.Time) (*domain.Conversation, error) {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// A compare-and-set on the assignee, so two agents answering at once
		// cannot both believe they hold it.
		tag, err := tx.Exec(ctx, `
			update chat_conversation set assignee_id = $2, updated_at = $3
			where id = $1 and kind = 'support' and assignee_id in ('', $2)`,
			conversationID, assignee.UserID, now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			var exists bool
			if err := tx.QueryRow(ctx,
				`select exists (select 1 from chat_conversation where id = $1 and kind = 'support')`,
				conversationID).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return domain.ErrClaimed
			}
			return domain.ErrNotFound
		}
		_, err = tx.Exec(ctx, `
			insert into chat_participant (conversation_id, user_id, role, joined_at)
			values ($1, $2, $3, $4)
			on conflict do nothing`,
			conversationID, assignee.UserID, string(assignee.Role), now)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("repository: claim: %w", err)
	}
	return r.Get(ctx, conversationID)
}

func (r *Postgres) Resolve(ctx context.Context, conversationID string, now time.Time) (*domain.Conversation, error) {
	tag, err := r.pool.Exec(ctx, `
		update chat_conversation set status = 'resolved', updated_at = $2
		where id = $1 and kind = 'support'`,
		conversationID, now)
	if err != nil {
		return nil, fmt.Errorf("repository: resolve: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, fmt.Errorf("repository: resolve: %w", domain.ErrNotFound)
	}
	return r.Get(ctx, conversationID)
}

func (r *Postgres) SavePushToken(ctx context.Context, token domain.PushToken) error {
	_, err := r.pool.Exec(ctx, `
		insert into chat_push_token (token, user_id, platform, updated_at)
		values ($1, $2, $3, $4)
		on conflict (token) do update set
		  user_id    = excluded.user_id,
		  platform   = excluded.platform,
		  updated_at = excluded.updated_at`,
		token.Token, token.UserID, string(token.Platform), token.UpdatedAt)
	if err != nil {
		return fmt.Errorf("repository: save push token: %w", err)
	}
	return nil
}

func (r *Postgres) DeletePushToken(ctx context.Context, userID, token string) error {
	if _, err := r.pool.Exec(ctx,
		`delete from chat_push_token where token = $1 and user_id = $2`, token, userID); err != nil {
		return fmt.Errorf("repository: delete push token: %w", err)
	}
	return nil
}

func (r *Postgres) PushTokens(ctx context.Context, userID string) ([]domain.PushToken, error) {
	rows, err := r.pool.Query(ctx, `
		select token, user_id, platform, updated_at from chat_push_token
		where user_id = $1 order by updated_at desc`, userID)
	if err != nil {
		return nil, fmt.Errorf("repository: push tokens: %w", err)
	}
	tokens, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.PushToken, error) {
		var (
			token    domain.PushToken
			platform string
		)
		err := row.Scan(&token.Token, &token.UserID, &platform, &token.UpdatedAt)
		token.Platform = domain.Platform(platform)
		return token, err
	})
	if err != nil {
		return nil, fmt.Errorf("repository: push tokens: %w", err)
	}
	return tokens, nil
}

func (r *Postgres) DeletePushTokens(ctx context.Context, tokens []string) error {
	if _, err := r.pool.Exec(ctx, `delete from chat_push_token where token = any($1)`, tokens); err != nil {
		return fmt.Errorf("repository: prune push tokens: %w", err)
	}
	return nil
}

func (r *Postgres) LeasePushes(ctx context.Context, now, until time.Time, limit int) ([]domain.PushJob, error) {
	// Skip locked, so workers on every instance take disjoint batches; and a
	// lease rather than a transaction held across the send, so a slow push
	// provider holds no database connection.
	rows, err := r.pool.Query(ctx, `
		with due as (
		  select id from chat_push_job
		  where not_before <= $1
		  order by not_before
		  limit $3
		  for update skip locked
		)
		update chat_push_job job set not_before = $2
		from due where job.id = due.id
		returning job.id, job.recipient_id, job.conversation_id, job.seq, job.attempts`,
		now, until, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: lease pushes: %w", err)
	}
	jobs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.PushJob, error) {
		var job domain.PushJob
		err := row.Scan(&job.ID, &job.RecipientID, &job.ConversationID, &job.Seq, &job.Attempts)
		return job, err
	})
	if err != nil {
		return nil, fmt.Errorf("repository: lease pushes: %w", err)
	}
	return jobs, nil
}

func (r *Postgres) FinishPushes(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := r.pool.Exec(ctx, `delete from chat_push_job where id = any($1)`, ids); err != nil {
		return fmt.Errorf("repository: finish pushes: %w", err)
	}
	return nil
}

func (r *Postgres) RetryPushes(ctx context.Context, ids []int64, notBefore time.Time) error {
	if _, err := r.pool.Exec(ctx, `
		update chat_push_job set attempts = attempts + 1, not_before = $2
		where id = any($1)`, ids, notBefore); err != nil {
		return fmt.Errorf("repository: retry pushes: %w", err)
	}
	return nil
}
