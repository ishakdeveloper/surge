package domain_test

import (
	"testing"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
)

func TestDisputeTransactionsBalance(t *testing.T) {
	dispute := domain.Dispute{ID: "dsp-1", TripID: "trip-1", AmountCents: 1450, DriverCents: 1160, Currency: "eur"}

	txns := []domain.Txn{
		domain.DisputeTxn(dispute, "drv-1"),
		domain.DisputeReversalTxn(dispute, "drv-1"),
		domain.DisputeWonTxn(dispute, "drv-1"),
		domain.DisputeRestoreTxn(dispute, "drv-1"),
	}
	net := map[string]int64{}
	for _, txn := range txns {
		if err := txn.Validate(); err != nil {
			t.Errorf("%s: %v", txn.ID, err)
		}
		for _, entry := range txn.Entries {
			net[entry.Account] += entry.AmountCents
		}
	}

	// Opened, reversed, won and restored: every account back where it began.
	for account, sum := range net {
		if sum != 0 {
			t.Errorf("%s moved %d across a dispute that was won", account, sum)
		}
	}
}
