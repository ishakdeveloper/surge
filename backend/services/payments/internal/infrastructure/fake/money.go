package fake

import (
	"context"
	"fmt"

	"github.com/ishakdeveloper/surge/services/payments/internal/service"
)

func (p *Processor) Refund(_ context.Context, request service.RefundRequest) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls.Refund++
	if err := p.failing(); err != nil {
		return "", err
	}
	if answer, ok := p.answers[request.IdempotencyKey]; ok {
		return answer.(string), nil
	}

	placed := p.holds[request.ProcessorPaymentID]
	switch {
	case placed == nil || placed.state != stateCaptured:
		return "", fmt.Errorf("fake: %s was never captured", request.ProcessorPaymentID)
	case placed.refunded+request.AmountCents > placed.amount:
		return "", fmt.Errorf("fake: cannot refund %d, %d left", request.AmountCents, placed.amount-placed.refunded)
	}

	placed.refunded += request.AmountCents
	id := p.id("re")
	p.answers[request.IdempotencyKey] = id
	p.refunds[id] = refunded{hold: request.ProcessorPaymentID, amount: request.AmountCents}
	return id, nil
}

// FailRefund is a refund the card would not take: a closed account, a bank
// that refused it. As at Stripe, the amount goes back to the charge and can be
// refunded again. The processor then reports it by webhook, which is the
// test's to deliver.
func (p *Processor) FailRefund(refundID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if record, ok := p.refunds[refundID]; ok {
		if placed := p.holds[record.hold]; placed != nil {
			placed.refunded -= record.amount
		}
		delete(p.refunds, refundID)
	}
}

func (p *Processor) ReverseTransfer(_ context.Context, request service.ReversalRequest) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls.Reverse++
	if err := p.failing(); err != nil {
		return "", err
	}
	if answer, ok := p.answers[request.IdempotencyKey]; ok {
		return answer.(string), nil
	}

	sent, ok := p.transfers[request.TransferID]
	if !ok {
		return "", fmt.Errorf("fake: no transfer %s", request.TransferID)
	}
	// As Stripe does: a reversal comes out of what the account holds now,
	// and an account that has paid it out cannot give it back.
	if p.balances[sent.account] < request.AmountCents {
		return "", fmt.Errorf("%w: %s holds %d", service.ErrReversalRefused, sent.account, p.balances[sent.account])
	}
	p.balances[sent.account] -= request.AmountCents

	id := p.id("trr")
	p.answers[request.IdempotencyKey] = id
	return id, nil
}

func (p *Processor) Balance(_ context.Context, accountID string) (service.Balance, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.failing(); err != nil {
		return service.Balance{}, err
	}
	return service.Balance{AvailableCents: p.balances[accountID]}, nil
}

func (p *Processor) Payout(_ context.Context, request service.PayoutRequest) (service.Payout, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls.Payout++
	if err := p.failing(); err != nil {
		return service.Payout{}, err
	}
	if answer, ok := p.answers[request.IdempotencyKey]; ok {
		return answer.(service.Payout), nil
	}
	if p.balances[request.AccountID] < request.AmountCents {
		return service.Payout{}, fmt.Errorf("%w: insufficient funds in the account", service.ErrPayoutRefused)
	}

	p.balances[request.AccountID] -= request.AmountCents
	payout := service.Payout{ID: p.id("po"), Status: "in_transit"}
	p.answers[request.IdempotencyKey] = payout
	return payout, nil
}

// Drain empties an account, as a driver who withdrew everything has.
func (p *Processor) Drain(accountID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.balances[accountID] = 0
}
