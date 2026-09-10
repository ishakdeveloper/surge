package wire

// Facts on `payment.events`: what became of the hold on a trip, for the trip
// service to dispatch or cancel on.
const (
	// FactPaymentAuthorized is a hold in place. The trip may be dispatched.
	FactPaymentAuthorized = "PaymentAuthorized"
	// FactPaymentActionRequired is a hold waiting on the rider — a 3-D Secure
	// challenge. The trip waits with it.
	FactPaymentActionRequired = "PaymentActionRequired"
	// FactPaymentFailed is a hold that will not happen. The trip is cancelled
	// with Reason.
	FactPaymentFailed = "PaymentFailed"
	// FactPaymentCaptured is the ride paid for.
	FactPaymentCaptured = "PaymentCaptured"
)

// Failure reasons, a closed set so they can be metric labels and so the rider
// can be told something specific.
const (
	FailureNoPaymentMethod      = "no_payment_method"
	FailureDeclined             = "declined"
	FailureAuthenticationFailed = "authentication_failed"
	FailureExpired              = "expired"
)

// PaymentFact is the envelope on `payment.events`, keyed by trip id — the same
// key as `trip.lifecycle`, so a trip's facts in both directions are ordered
// per trip.
//
// No client secret, ever. A rider with an authentication step to finish reads
// it from the API, where they are authenticated; a broker is not a place for a
// credential that confirms a payment.
type PaymentFact struct {
	Tag       string `json:"_tag"`
	TripID    string `json:"tripId"`
	RiderID   string `json:"riderId"`
	PaymentID string `json:"paymentId"`

	AmountCents int64  `json:"amountCents"`
	Currency    string `json:"currency"`

	// Reason is set on PaymentFailed and empty otherwise.
	Reason string `json:"reason"`

	AtMs int64 `json:"atMs"`
}

// Valid rejects a fact no consumer could act on.
func (f PaymentFact) Valid() bool {
	if f.TripID == "" || f.PaymentID == "" {
		return false
	}
	switch f.Tag {
	case FactPaymentAuthorized, FactPaymentActionRequired, FactPaymentCaptured:
		return true
	case FactPaymentFailed:
		return f.Reason != ""
	default:
		return false
	}
}
