package service

import (
	"context"
	"errors"
	"time"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
)

// SweepPolicy is how long each kind of stuck payment is left before the
// sweeper acts on it.
type SweepPolicy struct {
	// Authorizing is a hold asked for and never answered: a crash between
	// storing the ask and storing the answer, with no redelivery coming.
	Authorizing time.Duration
	// Action is a rider who never finished 3-D Secure. Past it the hold is
	// released and the trip cancelled, rather than waiting in payment_pending
	// for someone who has closed the app.
	Action time.Duration
	// Hold is an authorized hold whose trip has not ended long after any real
	// trip would have. Released well inside Stripe's seven days, so the
	// rider's money is not held by a trip that is never going to finish.
	Hold time.Duration
}

// DefaultSweepPolicy is used for any duration left zero.
var DefaultSweepPolicy = SweepPolicy{Authorizing: 2 * time.Minute, Action: 15 * time.Minute, Hold: 24 * time.Hour}

// SweepResult counts what a sweep did.
type SweepResult struct {
	Resumed, Expired, Released, DriversPaid int
}

// sweepBatch bounds one pass. Whatever is left is the next pass's.
const sweepBatch = 100

// Sweep finishes, expires or releases stuck payments, and pays drivers whose
// accounts can now take what they are owed.
//
// Safe to run on every instance at once. Each change is a compare-and-set and
// each processor call carries the key the original would have, so two sweeps
// acting on one payment make one change and one call's worth of effect.
func (s *Service) Sweep(ctx context.Context) (SweepResult, error) {
	var (
		result SweepResult
		errs   []error
	)
	now := s.now()

	stuck, err := s.repo.StalePayments(ctx, domain.StatusAuthorizing, now.Add(-s.sweep.Authorizing), sweepBatch)
	errs = append(errs, err)
	for i := range stuck {
		// Asked again under the same key: the processor answers with the
		// hold it placed, if it placed one, rather than placing another.
		if err := s.authorize(ctx, &stuck[i]); err != nil {
			errs = append(errs, err)
			continue
		}
		result.Resumed++
	}

	waiting, err := s.repo.StalePayments(ctx, domain.StatusRequiresAction, now.Add(-s.sweep.Action), sweepBatch)
	errs = append(errs, err)
	for i := range waiting {
		if err := s.expire(ctx, &waiting[i]); err != nil {
			errs = append(errs, err)
			continue
		}
		result.Expired++
	}

	held, err := s.repo.StalePayments(ctx, domain.StatusAuthorized, now.Add(-s.sweep.Hold), sweepBatch)
	errs = append(errs, err)
	for i := range held {
		if err := s.release(ctx, &held[i]); err != nil {
			errs = append(errs, err)
			continue
		}
		result.Released++
	}

	drivers, err := s.repo.OwedDrivers(ctx, sweepBatch)
	errs = append(errs, err)
	for _, driver := range drivers {
		if err := s.PayOutstanding(ctx, driver); err != nil {
			errs = append(errs, err)
			continue
		}
		result.DriversPaid++
	}

	return result, errors.Join(errs...)
}

// expire gives up on a rider who never authenticated: the hold is let go at
// the processor first, so a rider finishing the challenge late cannot bring
// it back, and then failed — which cancels the trip still waiting on it.
func (s *Service) expire(ctx context.Context, payment *domain.Payment) error {
	if err := s.processor.Release(ctx, payment.ProcessorPaymentID, payment.IdempotencyKey("release")); err != nil {
		return err
	}
	return s.settle(ctx, payment, Authorization{Outcome: OutcomeDeclined, DeclineReason: domain.FailureExpired})
}
