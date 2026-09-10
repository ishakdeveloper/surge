package stripe

import (
	"context"
	"errors"
	"fmt"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/service"
	stripego "github.com/stripe/stripe-go/v86"
)

// manualPayouts stops Stripe paying a driver out on its own schedule, so the
// money waits in their balance until they withdraw it.
//
// Through v1, on a v2 account: v2 has no payout schedule of its own yet, and
// the v1 settings apply to the same account.
func (p *Processor) manualPayouts(ctx context.Context, accountID, idempotencyKey string) error {
	params := &stripego.AccountUpdateParams{
		Settings: &stripego.AccountUpdateSettingsParams{
			Payouts: &stripego.AccountUpdateSettingsPayoutsParams{
				Schedule: &stripego.AccountUpdateSettingsPayoutsScheduleParams{Interval: stripego.String("manual")},
			},
		},
	}
	params.SetIdempotencyKey(idempotencyKey)
	if _, err := p.client.V1Accounts.Update(ctx, accountID, params); err != nil {
		return fmt.Errorf("stripe: set manual payouts on %s: %w", accountID, err)
	}
	return nil
}

// refundReasons are the reasons Stripe takes. Anything else ops writes is kept
// in metadata, where it is not lost, and sent as the customer asking.
var refundReasons = map[string]bool{"duplicate": true, "fraudulent": true, "requested_by_customer": true}

func (p *Processor) Refund(ctx context.Context, request service.RefundRequest) (string, error) {
	params := &stripego.RefundCreateParams{
		PaymentIntent: stripego.String(request.ProcessorPaymentID),
		Amount:        stripego.Int64(request.AmountCents),
		Reason:        stripego.String("requested_by_customer"),
	}
	if refundReasons[request.Reason] {
		params.Reason = stripego.String(request.Reason)
	}
	params.AddMetadata("trip_id", request.TripID)
	params.AddMetadata("reason", request.Reason)
	params.SetIdempotencyKey(request.IdempotencyKey)

	refund, err := p.client.V1Refunds.Create(ctx, params)
	if err != nil {
		return "", fmt.Errorf("stripe: refund %s: %w", request.ProcessorPaymentID, err)
	}
	return refund.ID, nil
}

func (p *Processor) ReverseTransfer(ctx context.Context, request service.ReversalRequest) (string, error) {
	params := &stripego.TransferReversalCreateParams{
		ID:     stripego.String(request.TransferID),
		Amount: stripego.Int64(request.AmountCents),
	}
	params.SetIdempotencyKey(request.IdempotencyKey)

	reversal, err := p.client.V1TransferReversals.Create(ctx, params)
	var stripeErr *stripego.Error
	if errors.As(err, &stripeErr) && stripeErr.Code == stripego.ErrorCode("balance_insufficient") {
		return "", fmt.Errorf("%w: %s", service.ErrReversalRefused, stripeErr.Msg)
	}
	if err != nil {
		return "", fmt.Errorf("stripe: reverse %s: %w", request.TransferID, err)
	}
	return reversal.ID, nil
}

func (p *Processor) Balance(ctx context.Context, accountID string) (service.Balance, error) {
	params := &stripego.BalanceRetrieveParams{}
	params.SetStripeAccount(accountID)

	balance, err := p.client.V1Balance.Retrieve(ctx, params)
	if err != nil {
		return service.Balance{}, fmt.Errorf("stripe: balance of %s: %w", accountID, err)
	}

	// One market's currency. Another would be a second balance, not a sum.
	var held service.Balance
	for _, amount := range balance.Available {
		if string(amount.Currency) == domain.DefaultCurrency {
			held.AvailableCents += amount.Amount
		}
	}
	for _, amount := range balance.Pending {
		if string(amount.Currency) == domain.DefaultCurrency {
			held.PendingCents += amount.Amount
		}
	}
	return held, nil
}

func (p *Processor) Payout(ctx context.Context, request service.PayoutRequest) (service.Payout, error) {
	params := &stripego.PayoutCreateParams{
		Amount:   stripego.Int64(request.AmountCents),
		Currency: stripego.String(request.Currency),
	}
	params.SetStripeAccount(request.AccountID)
	params.SetIdempotencyKey(request.IdempotencyKey)

	payout, err := p.client.V1Payouts.Create(ctx, params)
	// An invalid request here is the account's state, not ours — no bank
	// account in the currency, not enough available — and Stripe's message
	// says which in words a driver can act on.
	var stripeErr *stripego.Error
	if errors.As(err, &stripeErr) && stripeErr.Type == stripego.ErrorTypeInvalidRequest {
		return service.Payout{}, fmt.Errorf("%w: %s", service.ErrPayoutRefused, stripeErr.Msg)
	}
	if err != nil {
		return service.Payout{}, fmt.Errorf("stripe: payout from %s: %w", request.AccountID, err)
	}
	return service.Payout{ID: payout.ID, Status: string(payout.Status)}, nil
}
