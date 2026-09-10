package repository

import (
	"context"
	"fmt"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/jackc/pgx/v5"
)

const refundColumns = `id, trip_id, payment_id, amount_cents, driver_cents, currency, reason, reversal,
	processor_refund_id, processor_reversal_id, idempotency_key, created_at`

// writeRefund inserts a refund and adds it to the payment and the earning in
// place. Each update carries its own bound, so a refund that would pass what
// was captured or earned matches no row and the transaction writes nothing.
func writeRefund(ctx context.Context, tx pgx.Tx, refund *domain.Refund) error {
	tag, err := tx.Exec(ctx, `
		insert into refund (`+refundColumns+`)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		on conflict (trip_id, idempotency_key) do nothing`,
		refund.ID, refund.TripID, refund.PaymentID, refund.AmountCents, refund.DriverCents,
		refund.Currency, refund.Reason, string(refund.Reversal),
		refund.ProcessorRefundID, refund.ProcessorReversalID, refund.IdempotencyKey, refund.CreatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}

	tag, err = tx.Exec(ctx, `
		update payment set
		  refunded_cents = refunded_cents + $2,
		  status = case when refunded_cents + $2 >= captured_cents then 'refunded' else status end,
		  updated_at = $3
		where id = $1 and status = 'captured' and refunded_cents + $2 <= captured_cents`,
		refund.PaymentID, refund.AmountCents, refund.CreatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}

	if !refund.TakesFromEarning() {
		return nil
	}
	tag, err = tx.Exec(ctx, `
		update earning set
		  reversed_cents = reversed_cents + $2,
		  status = case when reversed_cents + $2 >= net_cents then 'reversed' else status end,
		  updated_at = $3
		where trip_id = $1 and reversed_cents + $2 <= net_cents`,
		refund.TripID, refund.DriverCents, refund.CreatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

func (r *Postgres) RefundByKey(ctx context.Context, tripID, key string) (*domain.Refund, error) {
	var (
		refund   domain.Refund
		reversal string
	)
	err := r.pool.QueryRow(ctx,
		`select `+refundColumns+` from refund where trip_id = $1 and idempotency_key = $2`, tripID, key,
	).Scan(&refund.ID, &refund.TripID, &refund.PaymentID, &refund.AmountCents, &refund.DriverCents,
		&refund.Currency, &refund.Reason, &reversal,
		&refund.ProcessorRefundID, &refund.ProcessorReversalID, &refund.IdempotencyKey, &refund.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("repository: refund: %w", notFound(err))
	}
	refund.Reversal = domain.ReversalKind(reversal)
	return &refund, nil
}

const withdrawalColumns = `id, driver_id, amount_cents, currency, status, processor_payout_id,
	failure_reason, idempotency_key, created_at, updated_at`

func scanWithdrawal(row scanner) (*domain.Withdrawal, error) {
	var (
		withdrawal domain.Withdrawal
		status     string
	)
	err := row.Scan(&withdrawal.ID, &withdrawal.DriverID, &withdrawal.AmountCents, &withdrawal.Currency,
		&status, &withdrawal.ProcessorPayoutID, &withdrawal.FailureReason, &withdrawal.IdempotencyKey,
		&withdrawal.CreatedAt, &withdrawal.UpdatedAt)
	if err != nil {
		return nil, err
	}
	withdrawal.Status = domain.WithdrawalStatus(status)
	return &withdrawal, nil
}

func (r *Postgres) SaveWithdrawal(ctx context.Context, withdrawal *domain.Withdrawal) error {
	tag, err := r.pool.Exec(ctx, `
		insert into withdrawal (`+withdrawalColumns+`)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		on conflict (driver_id, idempotency_key) do nothing`,
		withdrawal.ID, withdrawal.DriverID, withdrawal.AmountCents, withdrawal.Currency,
		string(withdrawal.Status), withdrawal.ProcessorPayoutID, withdrawal.FailureReason,
		withdrawal.IdempotencyKey, withdrawal.CreatedAt, withdrawal.UpdatedAt)
	if err != nil {
		return fmt.Errorf("repository: save withdrawal: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrConflict
	}
	return nil
}

func (r *Postgres) UpdateWithdrawal(ctx context.Context, withdrawal *domain.Withdrawal, from domain.WithdrawalStatus) error {
	tag, err := r.pool.Exec(ctx, `
		update withdrawal set status = $2, processor_payout_id = $3, failure_reason = $4, updated_at = $5
		where id = $1 and status = $6`,
		withdrawal.ID, string(withdrawal.Status), withdrawal.ProcessorPayoutID,
		withdrawal.FailureReason, withdrawal.UpdatedAt, string(from))
	if err != nil {
		return fmt.Errorf("repository: update withdrawal: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	var exists bool
	if err := r.pool.QueryRow(ctx, `select exists (select 1 from withdrawal where id = $1)`, withdrawal.ID).Scan(&exists); err != nil {
		return fmt.Errorf("repository: update withdrawal: %w", err)
	}
	if !exists {
		return domain.ErrNotFound
	}
	return domain.ErrConflict
}

func (r *Postgres) WithdrawalByKey(ctx context.Context, driverID, key string) (*domain.Withdrawal, error) {
	withdrawal, err := scanWithdrawal(r.pool.QueryRow(ctx,
		`select `+withdrawalColumns+` from withdrawal where driver_id = $1 and idempotency_key = $2`, driverID, key))
	if err != nil {
		return nil, fmt.Errorf("repository: withdrawal: %w", notFound(err))
	}
	return withdrawal, nil
}

func (r *Postgres) WithdrawalByPayoutID(ctx context.Context, processorPayoutID string) (*domain.Withdrawal, error) {
	withdrawal, err := scanWithdrawal(r.pool.QueryRow(ctx,
		`select `+withdrawalColumns+` from withdrawal where processor_payout_id = $1 and processor_payout_id <> ''`,
		processorPayoutID))
	if err != nil {
		return nil, fmt.Errorf("repository: withdrawal by payout: %w", notFound(err))
	}
	return withdrawal, nil
}

func (r *Postgres) ListWithdrawals(ctx context.Context, filter domain.WithdrawalFilter) (domain.WithdrawalPage, error) {
	limit := filter.Limit + 1
	rows, err := r.pool.Query(ctx, `
		select `+withdrawalColumns+` from withdrawal
		where driver_id = $1
		  and ($2 = '' or (created_at, id) < (select created_at, id from withdrawal where id = $2))
		order by created_at desc, id desc
		limit $3`,
		filter.DriverID, filter.Cursor, limit)
	if err != nil {
		return domain.WithdrawalPage{}, fmt.Errorf("repository: list withdrawals: %w", err)
	}
	defer rows.Close()

	var withdrawals []domain.Withdrawal
	for rows.Next() {
		withdrawal, err := scanWithdrawal(rows)
		if err != nil {
			return domain.WithdrawalPage{}, fmt.Errorf("repository: list withdrawals: %w", err)
		}
		withdrawals = append(withdrawals, *withdrawal)
	}
	if err := rows.Err(); err != nil {
		return domain.WithdrawalPage{}, fmt.Errorf("repository: list withdrawals: %w", err)
	}

	page := domain.WithdrawalPage{}
	if len(withdrawals) == limit {
		withdrawals = withdrawals[:filter.Limit]
		page.NextCursor = withdrawals[len(withdrawals)-1].ID
	}
	page.Withdrawals = withdrawals
	return page, nil
}
