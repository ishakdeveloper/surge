package service_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/fake"
	"github.com/ishakdeveloper/surge/services/payments/internal/infrastructure/repository"
	"github.com/ishakdeveloper/surge/services/payments/internal/service"
)

type recorder struct {
	mu    sync.Mutex
	heard []string
}

func (r *recorder) PaymentsChanged(_ context.Context, userID, tripID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.heard = append(r.heard, userID+"@"+tripID)
}

func (r *recorder) count(entry string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, heard := range r.heard {
		if heard == entry {
			n++
		}
	}
	return n
}

// The people a change is about hear about it: the rider when their hold is
// placed, both of them when the ride is paid for, and the driver when their
// share leaves for their account — which is what keeps both screens live
// without either polling.
func TestEveryoneAChangeConcernsIsTold(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewInMemory()
	told := &recorder{}
	ids := 0
	payments, err := service.New(service.Options{
		Repository: repo, Processor: fake.New(), CommissionBps: 2000, Notifier: told,
		Now:   func() time.Time { return now },
		NewID: func() string { ids++; return fmt.Sprintf("pay-%d", ids) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveCustomer(ctx, domain.Customer{UserID: "rider-1", ProcessorCustomerID: "cus_1", PaymentMethodID: fake.CardVisa}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SavePayoutAccount(ctx, domain.PayoutAccount{DriverID: "drv-1", ProcessorAccountID: "acct_1", TransfersStatus: domain.TransfersActive}); err != nil {
		t.Fatal(err)
	}

	if err := payments.TripRequested(ctx, trip("trip-1")); err != nil {
		t.Fatal(err)
	}
	if told.count("rider-1@trip-1") == 0 {
		t.Error("the rider was not told their hold was placed")
	}

	if err := payments.TripCompleted(ctx, trip("trip-1")); err != nil {
		t.Fatal(err)
	}
	if told.count("drv-1@trip-1") < 2 {
		t.Errorf("the driver heard %d times; want the capture and the transfer", told.count("drv-1@trip-1"))
	}

	// A card saved is about no one trip, and still reaches the rider.
	if _, err := payments.SetupCard(ctx, "rider-1", "rider@example.com"); err != nil {
		t.Fatal(err)
	}
	if told.count("rider-1@") == 0 {
		t.Error("the rider was not told their card was saved")
	}
}
