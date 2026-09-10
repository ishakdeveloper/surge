package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/jackc/pgx/v5"
)

const disputeColumns = `id, processor_dispute_id, trip_id, payment_id, amount_cents, driver_cents, currency,
	reason, reversal, status, processor_reversal_id, processor_restore_transfer_id, created_at, updated_at`

func writeDispute(ctx context.Context, tx pgx.Tx, dispute *domain.Dispute, from domain.DisputeStatus) error {
	if from == "" {
		tag, err := tx.Exec(ctx, `
			insert into dispute (`+disputeColumns+`)
			values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
			-- One row per processor dispute: the created webhook delivered
			-- twice is one dispute, and one take-back from the driver.
			on conflict (processor_dispute_id) do nothing`,
			dispute.ID, dispute.ProcessorDisputeID, dispute.TripID, dispute.PaymentID,
			dispute.AmountCents, dispute.DriverCents, dispute.Currency, dispute.Reason,
			string(dispute.Reversal), string(dispute.Status),
			dispute.ProcessorReversalID, dispute.ProcessorRestoreTransferID, dispute.CreatedAt, dispute.UpdatedAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrConflict
		}
		return nil
	}

	tag, err := tx.Exec(ctx, `
		update dispute set status = $2, processor_restore_transfer_id = $3, updated_at = $4
		where id = $1 and status = $5`,
		dispute.ID, string(dispute.Status), dispute.ProcessorRestoreTransferID, dispute.UpdatedAt, string(from))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return missingOrMoved(ctx, tx, `select exists (select 1 from dispute where id = $1)`, dispute.ID)
	}
	return nil
}

func (r *Postgres) DisputeByProcessorID(ctx context.Context, processorDisputeID string) (*domain.Dispute, error) {
	var (
		dispute          domain.Dispute
		reversal, status string
	)
	err := r.pool.QueryRow(ctx,
		`select `+disputeColumns+` from dispute where processor_dispute_id = $1`, processorDisputeID,
	).Scan(&dispute.ID, &dispute.ProcessorDisputeID, &dispute.TripID, &dispute.PaymentID,
		&dispute.AmountCents, &dispute.DriverCents, &dispute.Currency, &dispute.Reason,
		&reversal, &status, &dispute.ProcessorReversalID, &dispute.ProcessorRestoreTransferID,
		&dispute.CreatedAt, &dispute.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("repository: dispute: %w", notFound(err))
	}
	dispute.Reversal, dispute.Status = domain.ReversalKind(reversal), domain.DisputeStatus(status)
	return &dispute, nil
}

// StalePayments reads through the payment_open index, which covers exactly
// the statuses the sweeper asks about.
func (r *Postgres) StalePayments(ctx context.Context, status domain.Status, before time.Time, limit int) ([]domain.Payment, error) {
	rows, err := r.pool.Query(ctx, `
		select `+paymentColumns+` from payment
		where status = $1 and updated_at < $2
		order by updated_at
		limit $3`,
		string(status), before, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: stale payments: %w", err)
	}
	defer rows.Close()

	var payments []domain.Payment
	for rows.Next() {
		payment, err := scanPayment(rows)
		if err != nil {
			return nil, fmt.Errorf("repository: stale payments: %w", err)
		}
		payments = append(payments, *payment)
	}
	return payments, rows.Err()
}

func (r *Postgres) OwedDrivers(ctx context.Context, limit int) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		select distinct e.driver_id
		from earning e
		join payout_account a on a.driver_id = e.driver_id
		where e.status = 'unpaid' and a.transfers_status = $1
		limit $2`,
		domain.TransfersActive, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: owed drivers: %w", err)
	}
	defer rows.Close()

	var drivers []string
	for rows.Next() {
		var driver string
		if err := rows.Scan(&driver); err != nil {
			return nil, fmt.Errorf("repository: owed drivers: %w", err)
		}
		drivers = append(drivers, driver)
	}
	return drivers, rows.Err()
}
