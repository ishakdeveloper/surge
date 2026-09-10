package service

import (
	"context"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
)

// Notifier tells a person that something about their money changed, so a
// screen they have open reads it again instead of polling.
//
// Returns nothing, by contract, as the trip service's does: the change is
// already stored when this runs, a push that fails must not undo it, and the
// REST read is the truth.
type Notifier interface {
	PaymentsChanged(ctx context.Context, userID, tripID string)
}

type silentNotifier struct{}

func (silentNotifier) PaymentsChanged(context.Context, string, string) {}

// apply stores a change, then tells everyone it concerns.
//
// Every write goes through here, so there is no change a rider or driver is
// not told about — the same one-path argument as the trip service's single
// transition function.
func (s *Service) apply(ctx context.Context, change domain.Change) error {
	if err := s.repo.Apply(ctx, change); err != nil {
		return err
	}
	tripID, people := s.concerned(ctx, change)
	s.tell(ctx, tripID, people...)
	return nil
}

// concerned is who a change is about: the rider and driver on its payment,
// the driver of its earning, and for a refund or a dispute — which name a trip,
// not its people — whoever is on that trip.
func (s *Service) concerned(ctx context.Context, change domain.Change) (string, []string) {
	var (
		tripID string
		people []string
	)
	if payment := change.Payment; payment != nil {
		tripID = payment.TripID
		people = append(people, payment.RiderID, payment.DriverID)
	}
	if earning := change.Earning; earning != nil {
		tripID = earning.TripID
		people = append(people, earning.DriverID)
	}

	var named string
	switch {
	case change.Refund != nil:
		named = change.Refund.TripID
	case change.FailedRefund != nil:
		named = change.FailedRefund.TripID
	case change.Dispute != nil:
		named = change.Dispute.TripID
	}
	if named != "" {
		tripID = named
		if payment, err := s.repo.PaymentForTrip(ctx, named); err == nil {
			people = append(people, payment.RiderID, payment.DriverID)
		}
	}
	return tripID, people
}

// tell pushes to each person once.
func (s *Service) tell(ctx context.Context, tripID string, people ...string) {
	seen := make(map[string]bool, len(people))
	for _, person := range people {
		if person == "" || seen[person] {
			continue
		}
		seen[person] = true
		s.notifier.PaymentsChanged(ctx, person, tripID)
	}
}
