package grpc

import (
	"context"
	"errors"

	"github.com/ishakdeveloper/surge/services/payments/internal/domain"
	"github.com/ishakdeveloper/surge/services/payments/internal/service"
	"github.com/ishakdeveloper/surge/shared/authz"
	paymentspb "github.com/ishakdeveloper/surge/shared/proto/payments"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// idempotencyKey prefers the field, and falls back to the Idempotency-Key
// header the gateway forwards, as the trip service's booking does.
func idempotencyKey(ctx context.Context, field string) (string, error) {
	if field == "" {
		field = authz.IdempotencyKeyFromMetadata(ctx)
	}
	if field == "" {
		return "", status.Error(codes.InvalidArgument,
			"an Idempotency-Key is required so a retry cannot move money twice")
	}
	return field, nil
}

func (h *Handler) GetBalance(ctx context.Context, _ *paymentspb.GetBalanceRequest) (*paymentspb.GetBalanceResponse, error) {
	caller, err := callerAs(ctx, authz.RoleDriver)
	if err != nil {
		return nil, err
	}
	balance, err := h.service.Balance(ctx, caller.UserID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not read the balance: %v", err)
	}
	return &paymentspb.GetBalanceResponse{Balance: &paymentspb.Balance{
		AvailableCents: balance.AvailableCents,
		PendingCents:   balance.PendingCents,
		OwedCents:      balance.OwedCents,
		Currency:       balance.Currency,
		CanWithdraw:    balance.CanWithdraw,
	}}, nil
}

func (h *Handler) CreateWithdrawal(ctx context.Context, request *paymentspb.CreateWithdrawalRequest) (*paymentspb.CreateWithdrawalResponse, error) {
	caller, err := callerAs(ctx, authz.RoleDriver)
	if err != nil {
		return nil, err
	}
	key, err := idempotencyKey(ctx, request.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	if request.GetAmountCents() < 0 {
		return nil, status.Error(codes.InvalidArgument, "an amount cannot be negative")
	}

	withdrawal, err := h.service.Withdraw(ctx, caller.UserID, request.GetAmountCents(), key)
	switch {
	case errors.Is(err, service.ErrNoPayoutAccount):
		return nil, status.Error(codes.FailedPrecondition, "finish setting up payouts before withdrawing")
	case errors.Is(err, service.ErrNothingToWithdraw):
		return nil, status.Error(codes.FailedPrecondition, "there is nothing available to withdraw yet")
	case errors.Is(err, service.ErrOverBalance):
		return nil, status.Error(codes.FailedPrecondition, "that is more than is available to withdraw")
	case errors.Is(err, service.ErrPayoutRefused):
		// The processor's reason, which is the one thing a driver can act on:
		// "add a bank account", in its words.
		return nil, status.Error(codes.FailedPrecondition, withdrawal.FailureReason)
	case err != nil:
		return nil, status.Errorf(codes.Internal, "could not withdraw: %v", err)
	}
	return &paymentspb.CreateWithdrawalResponse{Withdrawal: toWithdrawal(withdrawal)}, nil
}

func (h *Handler) ListWithdrawals(ctx context.Context, request *paymentspb.ListWithdrawalsRequest) (*paymentspb.ListWithdrawalsResponse, error) {
	caller, err := callerAs(ctx, authz.RoleDriver)
	if err != nil {
		return nil, err
	}
	page, err := h.service.Withdrawals(ctx, caller.UserID, int(request.GetPageSize()), request.GetPageToken())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not list withdrawals: %v", err)
	}
	withdrawals := make([]*paymentspb.Withdrawal, 0, len(page.Withdrawals))
	for i := range page.Withdrawals {
		withdrawals = append(withdrawals, toWithdrawal(&page.Withdrawals[i]))
	}
	return &paymentspb.ListWithdrawalsResponse{Withdrawals: withdrawals, NextPageToken: page.NextCursor}, nil
}

// RefundTrip is ops only. A rider asking for their money back is a
// conversation with a person, not an endpoint.
func (h *Handler) RefundTrip(ctx context.Context, request *paymentspb.RefundTripRequest) (*paymentspb.RefundTripResponse, error) {
	if _, err := callerAs(ctx, authz.RoleOps); err != nil {
		return nil, err
	}
	key, err := idempotencyKey(ctx, request.GetIdempotencyKey())
	if err != nil {
		return nil, err
	}
	if request.GetAmountCents() < 0 {
		return nil, status.Error(codes.InvalidArgument, "an amount cannot be negative")
	}

	refund, err := h.service.Refund(ctx, request.GetTripId(), request.GetAmountCents(), request.GetReason(), key)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return nil, status.Error(codes.NotFound, "no payment for that trip")
	case errors.Is(err, service.ErrNotRefundable):
		return nil, status.Error(codes.FailedPrecondition, "only a captured payment can be refunded")
	case errors.Is(err, service.ErrRefundTooLarge):
		return nil, status.Error(codes.InvalidArgument, "that is more than is left to refund")
	case err != nil:
		return nil, status.Errorf(codes.Internal, "could not refund: %v", err)
	}

	payment, err := h.service.Payment(ctx, request.GetTripId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not read the payment: %v", err)
	}
	return &paymentspb.RefundTripResponse{Refund: toRefund(refund), Payment: toPayment(payment, false)}, nil
}

var withdrawalStatuses = map[domain.WithdrawalStatus]paymentspb.WithdrawalStatus{
	domain.WithdrawalRequested: paymentspb.WithdrawalStatus_WITHDRAWAL_STATUS_REQUESTED,
	domain.WithdrawalInTransit: paymentspb.WithdrawalStatus_WITHDRAWAL_STATUS_IN_TRANSIT,
	domain.WithdrawalPaid:      paymentspb.WithdrawalStatus_WITHDRAWAL_STATUS_PAID,
	domain.WithdrawalFailed:    paymentspb.WithdrawalStatus_WITHDRAWAL_STATUS_FAILED,
}

var refundReversals = map[domain.ReversalKind]paymentspb.RefundReversal{
	domain.ReversalNone:     paymentspb.RefundReversal_REFUND_REVERSAL_NONE,
	domain.ReversalDeducted: paymentspb.RefundReversal_REFUND_REVERSAL_DEDUCTED,
	domain.ReversalReversed: paymentspb.RefundReversal_REFUND_REVERSAL_REVERSED,
	domain.ReversalFailed:   paymentspb.RefundReversal_REFUND_REVERSAL_FAILED,
}

func toWithdrawal(withdrawal *domain.Withdrawal) *paymentspb.Withdrawal {
	return &paymentspb.Withdrawal{
		Id:            withdrawal.ID,
		AmountCents:   withdrawal.AmountCents,
		Currency:      withdrawal.Currency,
		Status:        withdrawalStatuses[withdrawal.Status],
		FailureReason: withdrawal.FailureReason,
		CreatedAt:     timestamppb.New(withdrawal.CreatedAt),
		UpdatedAt:     timestamppb.New(withdrawal.UpdatedAt),
	}
}

func toRefund(refund *domain.Refund) *paymentspb.Refund {
	return &paymentspb.Refund{
		Id:            refund.ID,
		TripId:        refund.TripID,
		AmountCents:   refund.AmountCents,
		DriverCents:   refund.DriverCents,
		Currency:      refund.Currency,
		Reason:        refund.Reason,
		Reversal:      refundReversals[refund.Reversal],
		Status:        refundStatuses[refund.Status],
		FailureReason: refund.FailureReason,
		CreatedAt:     timestamppb.New(refund.CreatedAt),
	}
}

var refundStatuses = map[domain.RefundStatus]paymentspb.RefundStatus{
	domain.RefundSucceeded: paymentspb.RefundStatus_REFUND_STATUS_SUCCEEDED,
	domain.RefundFailed:    paymentspb.RefundStatus_REFUND_STATUS_FAILED,
}
