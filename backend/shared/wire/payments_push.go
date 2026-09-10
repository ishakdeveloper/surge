package wire

// TagPaymentsChanged is something about a person's money changing: a hold on
// their trip, a card saved, an earning, a withdrawal. Pushed to that person by
// the payments service.
const TagPaymentsChanged = "PaymentsChanged"

// PaymentsChange says only that something changed, and on which trip if one.
//
// Deliberately thin, like TripUpdate. The REST read is the truth; this is the
// doorbell that makes an open screen read it again, and a push carrying the
// payment itself would be a second copy of it to keep correct.
type PaymentsChange struct {
	// TripID is empty when the change is not about one trip — a card saved,
	// an account able to receive money.
	TripID string `json:"tripId"`
	AtMs   int64  `json:"atMs"`
}
