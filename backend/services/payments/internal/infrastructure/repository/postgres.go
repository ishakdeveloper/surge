package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/shared/outbox"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres is the durable repository, and the only one the service runs on:
// a payments service that forgets what it held on restart is not one.
type Postgres struct{ pool *pgxpool.Pool }

func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

// OutboxTable is where payment facts wait for the relay.
var OutboxTable = outbox.NewTable("payments_outbox")

type scanner interface{ Scan(dest ...any) error }

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

const customerColumns = `user_id, processor_customer_id, payment_method_id,
	card_brand, card_last4, card_exp_month, card_exp_year, created_at, updated_at`

func (r *Postgres) customerWhere(ctx context.Context, column, value string) (domain.Customer, error) {
	var customer domain.Customer
	err := r.pool.QueryRow(ctx,
		`select `+customerColumns+` from payment_customer where `+column+` = $1`, value,
	).Scan(&customer.UserID, &customer.ProcessorCustomerID, &customer.PaymentMethodID,
		&customer.Card.Brand, &customer.Card.Last4, &customer.Card.ExpMonth, &customer.Card.ExpYear,
		&customer.CreatedAt, &customer.UpdatedAt)
	if err != nil {
		return domain.Customer{}, fmt.Errorf("repository: customer: %w", notFound(err))
	}
	return customer, nil
}

func (r *Postgres) Customer(ctx context.Context, userID string) (domain.Customer, error) {
	return r.customerWhere(ctx, "user_id", userID)
}

func (r *Postgres) CustomerByProcessorID(ctx context.Context, processorCustomerID string) (domain.Customer, error) {
	return r.customerWhere(ctx, "processor_customer_id", processorCustomerID)
}

func (r *Postgres) SaveCustomer(ctx context.Context, customer domain.Customer) error {
	_, err := r.pool.Exec(ctx, `
		insert into payment_customer (user_id, processor_customer_id, payment_method_id,
		  card_brand, card_last4, card_exp_month, card_exp_year, created_at, updated_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		on conflict (user_id) do update set
		  processor_customer_id = excluded.processor_customer_id,
		  payment_method_id     = excluded.payment_method_id,
		  card_brand            = excluded.card_brand,
		  card_last4            = excluded.card_last4,
		  card_exp_month        = excluded.card_exp_month,
		  card_exp_year         = excluded.card_exp_year,
		  updated_at            = excluded.updated_at`,
		customer.UserID, customer.ProcessorCustomerID, customer.PaymentMethodID,
		customer.Card.Brand, customer.Card.Last4, customer.Card.ExpMonth, customer.Card.ExpYear,
		customer.CreatedAt, customer.UpdatedAt)
	if err != nil {
		return fmt.Errorf("repository: save customer: %w", err)
	}
	return nil
}

const paymentColumns = `id, trip_id, attempt, rider_id, driver_id, status,
	amount_cents, captured_cents, refunded_cents, commission_cents, currency,
	processor_payment_id, processor_charge_id, client_secret, failure_reason,
	created_at, updated_at`

func scanPayment(row scanner) (*domain.Payment, error) {
	var (
		payment domain.Payment
		status  string
	)
	err := row.Scan(&payment.ID, &payment.TripID, &payment.Attempt, &payment.RiderID, &payment.DriverID, &status,
		&payment.AmountCents, &payment.CapturedCents, &payment.RefundedCents, &payment.CommissionCents, &payment.Currency,
		&payment.ProcessorPaymentID, &payment.ProcessorChargeID, &payment.ClientSecret, &payment.FailureReason,
		&payment.CreatedAt, &payment.UpdatedAt)
	if err != nil {
		return nil, err
	}
	payment.Status = domain.Status(status)
	return &payment, nil
}

func (r *Postgres) PaymentForTrip(ctx context.Context, tripID string) (*domain.Payment, error) {
	payment, err := scanPayment(r.pool.QueryRow(ctx,
		`select `+paymentColumns+` from payment where trip_id = $1`, tripID))
	if err != nil {
		return nil, fmt.Errorf("repository: payment for trip: %w", notFound(err))
	}
	return payment, nil
}

func (r *Postgres) PaymentByProcessorID(ctx context.Context, processorPaymentID string) (*domain.Payment, error) {
	payment, err := scanPayment(r.pool.QueryRow(ctx,
		`select `+paymentColumns+` from payment where processor_payment_id = $1 and processor_payment_id <> ''`,
		processorPaymentID))
	if err != nil {
		return nil, fmt.Errorf("repository: payment by processor id: %w", notFound(err))
	}
	return payment, nil
}

func (r *Postgres) accountWhere(ctx context.Context, column, value string) (domain.PayoutAccount, error) {
	var account domain.PayoutAccount
	err := r.pool.QueryRow(ctx, `
		select driver_id, processor_account_id, transfers_status, requirements_due, created_at, updated_at
		from payout_account where `+column+` = $1`, value,
	).Scan(&account.DriverID, &account.ProcessorAccountID, &account.TransfersStatus,
		&account.RequirementsDue, &account.CreatedAt, &account.UpdatedAt)
	if err != nil {
		return domain.PayoutAccount{}, fmt.Errorf("repository: payout account: %w", notFound(err))
	}
	return account, nil
}

func (r *Postgres) PayoutAccount(ctx context.Context, driverID string) (domain.PayoutAccount, error) {
	return r.accountWhere(ctx, "driver_id", driverID)
}

func (r *Postgres) PayoutAccountByProcessorID(ctx context.Context, processorAccountID string) (domain.PayoutAccount, error) {
	return r.accountWhere(ctx, "processor_account_id", processorAccountID)
}

func (r *Postgres) SavePayoutAccount(ctx context.Context, account domain.PayoutAccount) error {
	_, err := r.pool.Exec(ctx, `
		insert into payout_account (driver_id, processor_account_id, transfers_status, requirements_due, created_at, updated_at)
		values ($1, $2, $3, $4, $5, $6)
		on conflict (driver_id) do update set
		  processor_account_id = excluded.processor_account_id,
		  transfers_status     = excluded.transfers_status,
		  requirements_due     = excluded.requirements_due,
		  updated_at           = excluded.updated_at`,
		account.DriverID, account.ProcessorAccountID, account.TransfersStatus,
		account.RequirementsDue, account.CreatedAt, account.UpdatedAt)
	if err != nil {
		return fmt.Errorf("repository: save payout account: %w", err)
	}
	return nil
}

const earningColumns = `trip_id, driver_id, payment_id, gross_cents, commission_cents, net_cents,
	currency, status, processor_transfer_id, created_at, updated_at, reversed_cents`

func scanEarning(row scanner) (*domain.Earning, error) {
	var (
		earning domain.Earning
		status  string
	)
	err := row.Scan(&earning.TripID, &earning.DriverID, &earning.PaymentID,
		&earning.GrossCents, &earning.CommissionCents, &earning.NetCents,
		&earning.Currency, &status, &earning.ProcessorTransferID, &earning.CreatedAt, &earning.UpdatedAt,
		&earning.ReversedCents)
	if err != nil {
		return nil, err
	}
	earning.Status = domain.EarningStatus(status)
	return &earning, nil
}

func (r *Postgres) Earning(ctx context.Context, tripID string) (*domain.Earning, error) {
	earning, err := scanEarning(r.pool.QueryRow(ctx,
		`select `+earningColumns+` from earning where trip_id = $1`, tripID))
	if err != nil {
		return nil, fmt.Errorf("repository: earning: %w", notFound(err))
	}
	return earning, nil
}

func (r *Postgres) UnpaidEarnings(ctx context.Context, driverID string) ([]domain.Earning, error) {
	rows, err := r.pool.Query(ctx,
		`select `+earningColumns+` from earning where driver_id = $1 and status = 'unpaid' order by created_at`,
		driverID)
	if err != nil {
		return nil, fmt.Errorf("repository: unpaid earnings: %w", err)
	}
	defer rows.Close()

	var earnings []domain.Earning
	for rows.Next() {
		earning, err := scanEarning(rows)
		if err != nil {
			return nil, fmt.Errorf("repository: unpaid earnings: %w", err)
		}
		earnings = append(earnings, *earning)
	}
	return earnings, rows.Err()
}

func (r *Postgres) ListEarnings(ctx context.Context, filter domain.EarningFilter) (domain.EarningPage, error) {
	// One more than asked for, which is how a next page is discovered without
	// counting the history. Keyset over (created_at, trip_id), matching the
	// earning_driver_created index, as the trip listing does.
	limit := filter.Limit + 1
	rows, err := r.pool.Query(ctx, `
		select `+earningColumns+` from earning
		where driver_id = $1
		  and ($2 = '' or (created_at, trip_id) < (select created_at, trip_id from earning where trip_id = $2))
		order by created_at desc, trip_id desc
		limit $3`,
		filter.DriverID, filter.Cursor, limit)
	if err != nil {
		return domain.EarningPage{}, fmt.Errorf("repository: list earnings: %w", err)
	}
	defer rows.Close()

	var earnings []domain.Earning
	for rows.Next() {
		earning, err := scanEarning(rows)
		if err != nil {
			return domain.EarningPage{}, fmt.Errorf("repository: list earnings: %w", err)
		}
		earnings = append(earnings, *earning)
	}
	if err := rows.Err(); err != nil {
		return domain.EarningPage{}, fmt.Errorf("repository: list earnings: %w", err)
	}

	page := domain.EarningPage{}
	if len(earnings) == limit {
		earnings = earnings[:filter.Limit]
		page.NextCursor = earnings[len(earnings)-1].TripID
	}
	page.Earnings = earnings
	return page, nil
}

func (r *Postgres) EarningTotals(ctx context.Context, driverID string) (domain.EarningTotals, error) {
	var totals domain.EarningTotals
	err := r.pool.QueryRow(ctx, `
		select coalesce(sum(net_cents - reversed_cents) filter (where status = 'unpaid'), 0),
		       coalesce(sum(net_cents - reversed_cents) filter (where status = 'transferred'), 0)
		from earning where driver_id = $1`, driverID,
	).Scan(&totals.OwedCents, &totals.PaidCents)
	if err != nil {
		return domain.EarningTotals{}, fmt.Errorf("repository: earning totals: %w", err)
	}
	return totals, nil
}

func (r *Postgres) LedgerBalance(ctx context.Context, account string) (int64, error) {
	var balance int64
	if err := r.pool.QueryRow(ctx,
		`select coalesce(sum(amount_cents), 0) from ledger_entry where account = $1`, account,
	).Scan(&balance); err != nil {
		return 0, fmt.Errorf("repository: ledger balance: %w", err)
	}
	return balance, nil
}

func (r *Postgres) EventHandled(ctx context.Context, eventID string) (bool, error) {
	var handled bool
	if err := r.pool.QueryRow(ctx,
		`select exists (select 1 from processor_event where event_id = $1)`, eventID,
	).Scan(&handled); err != nil {
		return false, fmt.Errorf("repository: event handled: %w", err)
	}
	return handled, nil
}

func (r *Postgres) MarkEventHandled(ctx context.Context, eventID, eventType string) error {
	if _, err := r.pool.Exec(ctx,
		`insert into processor_event (event_id, type) values ($1, $2) on conflict (event_id) do nothing`,
		eventID, eventType,
	); err != nil {
		return fmt.Errorf("repository: mark event handled: %w", err)
	}
	return nil
}

// Apply writes a change in one transaction.
func (r *Postgres) Apply(ctx context.Context, change domain.Change) error {
	for _, txn := range change.Txns {
		if err := txn.Validate(); err != nil {
			return err
		}
	}
	messages, err := encodeFacts(change.Facts)
	if err != nil {
		return err
	}

	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if change.Payment != nil {
			if err := writePayment(ctx, tx, change.Payment, change.From); err != nil {
				return err
			}
		}
		if change.Earning != nil {
			if err := writeEarning(ctx, tx, change.Earning, change.EarningFrom); err != nil {
				return err
			}
		}
		if change.Refund != nil {
			if err := writeRefund(ctx, tx, change.Refund); err != nil {
				return err
			}
		}
		if change.Dispute != nil {
			if err := writeDispute(ctx, tx, change.Dispute, change.DisputeFrom); err != nil {
				return err
			}
		}
		if change.FailedRefund != nil {
			if err := writeFailedRefund(ctx, tx, change.FailedRefund); err != nil {
				return err
			}
		}
		for _, txn := range change.Txns {
			if err := writeTxn(ctx, tx, txn); err != nil {
				return err
			}
		}
		return outbox.Write(ctx, tx, OutboxTable, messages...)
	})

	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrNotFound):
		return err
	default:
		return fmt.Errorf("repository: apply: %w", err)
	}
}

func writePayment(ctx context.Context, tx pgx.Tx, payment *domain.Payment, from domain.Status) error {
	if from == "" {
		tag, err := tx.Exec(ctx, `
			insert into payment (`+paymentColumns+`)
			values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
			-- One payment per trip. A second delivery racing the first finds the
			-- row there and backs off, rather than placing a second hold.
			on conflict (trip_id) do nothing`,
			payment.ID, payment.TripID, payment.Attempt, payment.RiderID, payment.DriverID, string(payment.Status),
			payment.AmountCents, payment.CapturedCents, payment.RefundedCents, payment.CommissionCents, payment.Currency,
			payment.ProcessorPaymentID, payment.ProcessorChargeID, payment.ClientSecret, payment.FailureReason,
			payment.CreatedAt, payment.UpdatedAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrConflict
		}
		return nil
	}

	tag, err := tx.Exec(ctx, `
		update payment set
		  attempt = $2, driver_id = $3, status = $4,
		  amount_cents = $5, captured_cents = $6, refunded_cents = $7, commission_cents = $8,
		  processor_payment_id = $9, processor_charge_id = $10, client_secret = $11, failure_reason = $12,
		  updated_at = $13
		-- Compare-and-set on the status the change was decided against: a
		-- webhook and a consumer settling one payment at once must not both
		-- win.
		where id = $1 and status = $14`,
		payment.ID, payment.Attempt, payment.DriverID, string(payment.Status),
		payment.AmountCents, payment.CapturedCents, payment.RefundedCents, payment.CommissionCents,
		payment.ProcessorPaymentID, payment.ProcessorChargeID, payment.ClientSecret, payment.FailureReason,
		payment.UpdatedAt, string(from))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return missingOrMoved(ctx, tx, `select exists (select 1 from payment where id = $1)`, payment.ID)
	}
	return nil
}

func writeEarning(ctx context.Context, tx pgx.Tx, earning *domain.Earning, from domain.EarningStatus) error {
	if from == "" {
		tag, err := tx.Exec(ctx, `
			insert into earning (`+earningColumns+`)
			values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			on conflict (trip_id) do nothing`,
			earning.TripID, earning.DriverID, earning.PaymentID,
			earning.GrossCents, earning.CommissionCents, earning.NetCents,
			earning.Currency, string(earning.Status), earning.ProcessorTransferID,
			earning.CreatedAt, earning.UpdatedAt, earning.ReversedCents)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrConflict
		}
		return nil
	}

	tag, err := tx.Exec(ctx, `
		update earning set status = $2, processor_transfer_id = $3, updated_at = $4, reversed_cents = $6
		where trip_id = $1 and status = $5`,
		earning.TripID, string(earning.Status), earning.ProcessorTransferID, earning.UpdatedAt, string(from),
		earning.ReversedCents)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return missingOrMoved(ctx, tx, `select exists (select 1 from earning where trip_id = $1)`, earning.TripID)
	}
	return nil
}

func writeTxn(ctx context.Context, tx pgx.Tx, txn domain.Txn) error {
	for _, entry := range txn.Entries {
		tag, err := tx.Exec(ctx, `
			insert into ledger_entry (txn_id, kind, account, amount_cents, currency, trip_id)
			values ($1, $2, $3, $4, $5, $6)
			-- A transaction already posted is a conflict, not a second posting:
			-- the ids are derived from what they record, so a repeat is exactly
			-- the double count this index exists to refuse.
			on conflict (txn_id, account) do nothing`,
			txn.ID, string(txn.Kind), entry.Account, entry.AmountCents, txn.Currency, txn.TripID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrConflict
		}
	}
	return nil
}

// missingOrMoved explains a compare-and-set that matched nothing: a 404 and a
// lost race want different answers, and only a second read tells them apart.
func missingOrMoved(ctx context.Context, tx pgx.Tx, exists string, key string) error {
	var found bool
	if err := tx.QueryRow(ctx, exists, key).Scan(&found); err != nil {
		return err
	}
	if !found {
		return domain.ErrNotFound
	}
	return domain.ErrConflict
}
