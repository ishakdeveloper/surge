// Package grpc is payments' synchronous edge: what riders and drivers ask
// about money, answered from what this service has stored.
//
// A translation layer, as the trip service's is: proto in, service call,
// proto out, and the decisions about who may see what.
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

type Handler struct {
	paymentspb.UnimplementedPaymentsServiceServer
	service *service.Service
}

func NewHandler(payments *service.Service) *Handler { return &Handler{service: payments} }

// callerAs returns the caller when they hold one of the roles. A driver asking
// for a rider's card is not a caller who forgot a field; it is a request this
// API does not answer for them.
func callerAs(ctx context.Context, roles ...authz.Role) (authz.Identity, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return authz.Identity{}, err
	}
	for _, role := range roles {
		if caller.Role == role {
			return caller, nil
		}
	}
	return authz.Identity{}, status.Error(codes.PermissionDenied, "that is not available to your role")
}

func (h *Handler) CreateSetupIntent(ctx context.Context, _ *paymentspb.CreateSetupIntentRequest) (*paymentspb.CreateSetupIntentResponse, error) {
	caller, err := callerAs(ctx, authz.RoleRider)
	if err != nil {
		return nil, err
	}
	secret, err := h.service.SetupCard(ctx, caller.UserID, caller.Email)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not start saving a card: %v", err)
	}
	return &paymentspb.CreateSetupIntentResponse{ClientSecret: secret}, nil
}

func (h *Handler) GetPaymentMethod(ctx context.Context, _ *paymentspb.GetPaymentMethodRequest) (*paymentspb.GetPaymentMethodResponse, error) {
	caller, err := callerAs(ctx, authz.RoleRider)
	if err != nil {
		return nil, err
	}
	customer, err := h.service.Customer(ctx, caller.UserID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not read the card: %v", err)
	}
	return &paymentspb.GetPaymentMethodResponse{
		Saved: customer.CanPay(),
		Card: &paymentspb.Card{
			Brand:    customer.Card.Brand,
			Last4:    customer.Card.Last4,
			ExpMonth: int32(customer.Card.ExpMonth),
			ExpYear:  int32(customer.Card.ExpYear),
		},
	}, nil
}

func (h *Handler) GetTripPayment(ctx context.Context, request *paymentspb.GetTripPaymentRequest) (*paymentspb.GetTripPaymentResponse, error) {
	caller, err := callerAs(ctx, authz.RoleRider, authz.RoleOps)
	if err != nil {
		return nil, err
	}

	payment, err := h.service.Payment(ctx, request.GetTripId())
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return nil, status.Error(codes.NotFound, "no payment for that trip")
	case err != nil:
		return nil, status.Errorf(codes.Internal, "could not read the payment: %v", err)
	}

	// Another rider's payment is NotFound rather than PermissionDenied, as in
	// the trip service: saying it exists is itself a disclosure.
	owner := payment.RiderID == caller.UserID
	if !owner && caller.Role != authz.RoleOps {
		return nil, status.Error(codes.NotFound, "no payment for that trip")
	}
	return &paymentspb.GetTripPaymentResponse{Payment: toPayment(payment, owner)}, nil
}

func (h *Handler) GetPayoutAccount(ctx context.Context, _ *paymentspb.GetPayoutAccountRequest) (*paymentspb.GetPayoutAccountResponse, error) {
	caller, err := callerAs(ctx, authz.RoleDriver)
	if err != nil {
		return nil, err
	}
	account, found, err := h.service.PayoutAccount(ctx, caller.UserID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not read the payout account: %v", err)
	}
	return &paymentspb.GetPayoutAccountResponse{Account: toPayoutAccount(account, found)}, nil
}

func (h *Handler) StartOnboarding(ctx context.Context, _ *paymentspb.StartOnboardingRequest) (*paymentspb.StartOnboardingResponse, error) {
	caller, err := callerAs(ctx, authz.RoleDriver)
	if err != nil {
		return nil, err
	}
	link, err := h.service.StartOnboarding(ctx, caller.UserID, caller.Email)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not start onboarding: %v", err)
	}
	return &paymentspb.StartOnboardingResponse{Url: link}, nil
}

func (h *Handler) ListEarnings(ctx context.Context, request *paymentspb.ListEarningsRequest) (*paymentspb.ListEarningsResponse, error) {
	caller, err := callerAs(ctx, authz.RoleDriver)
	if err != nil {
		return nil, err
	}
	page, totals, err := h.service.Earnings(ctx, caller.UserID, int(request.GetPageSize()), request.GetPageToken())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not list earnings: %v", err)
	}

	earnings := make([]*paymentspb.Earning, 0, len(page.Earnings))
	for _, earning := range page.Earnings {
		earnings = append(earnings, &paymentspb.Earning{
			TripId:          earning.TripID,
			GrossCents:      earning.GrossCents,
			CommissionCents: earning.CommissionCents,
			NetCents:        earning.NetCents,
			Currency:        earning.Currency,
			Status:          earningStatuses[earning.Status],
			CreatedAt:       timestamppb.New(earning.CreatedAt),
		})
	}
	return &paymentspb.ListEarningsResponse{
		Earnings:      earnings,
		NextPageToken: page.NextCursor,
		OwedCents:     totals.OwedCents,
		PaidCents:     totals.PaidCents,
		Currency:      domain.DefaultCurrency,
	}, nil
}

// paymentStatuses is the one place a payment status becomes a wire enum.
var paymentStatuses = map[domain.Status]paymentspb.PaymentStatus{
	domain.StatusAuthorizing:    paymentspb.PaymentStatus_PAYMENT_STATUS_AUTHORIZING,
	domain.StatusRequiresAction: paymentspb.PaymentStatus_PAYMENT_STATUS_REQUIRES_ACTION,
	domain.StatusAuthorized:     paymentspb.PaymentStatus_PAYMENT_STATUS_AUTHORIZED,
	domain.StatusCaptured:       paymentspb.PaymentStatus_PAYMENT_STATUS_CAPTURED,
	domain.StatusReleased:       paymentspb.PaymentStatus_PAYMENT_STATUS_RELEASED,
	domain.StatusFailed:         paymentspb.PaymentStatus_PAYMENT_STATUS_FAILED,
	domain.StatusRefunded:       paymentspb.PaymentStatus_PAYMENT_STATUS_REFUNDED,
}

var earningStatuses = map[domain.EarningStatus]paymentspb.EarningStatus{
	domain.EarningUnpaid:      paymentspb.EarningStatus_EARNING_STATUS_UNPAID,
	domain.EarningTransferred: paymentspb.EarningStatus_EARNING_STATUS_TRANSFERRED,
	domain.EarningReversed:    paymentspb.EarningStatus_EARNING_STATUS_REVERSED,
}

// toPayment shows the client secret only to the rider it belongs to. Ops can
// read a payment; they cannot finish a rider's authentication for them.
func toPayment(payment *domain.Payment, owner bool) *paymentspb.Payment {
	secret := ""
	if owner {
		secret = payment.ClientSecret
	}
	return &paymentspb.Payment{
		Id:            payment.ID,
		TripId:        payment.TripID,
		Status:        paymentStatuses[payment.Status],
		AmountCents:   payment.AmountCents,
		CapturedCents: payment.CapturedCents,
		RefundedCents: payment.RefundedCents,
		Currency:      payment.Currency,
		FailureReason: payment.FailureReason,
		ClientSecret:  secret,
		CreatedAt:     timestamppb.New(payment.CreatedAt),
		UpdatedAt:     timestamppb.New(payment.UpdatedAt),
	}
}

func toPayoutAccount(account domain.PayoutAccount, found bool) *paymentspb.PayoutAccount {
	switch {
	case !found:
		return &paymentspb.PayoutAccount{Status: paymentspb.PayoutAccountStatus_PAYOUT_ACCOUNT_STATUS_NOT_STARTED}
	case account.CanReceive():
		return &paymentspb.PayoutAccount{
			Status:          paymentspb.PayoutAccountStatus_PAYOUT_ACCOUNT_STATUS_ACTIVE,
			RequirementsDue: account.RequirementsDue,
		}
	default:
		return &paymentspb.PayoutAccount{
			Status:          paymentspb.PayoutAccountStatus_PAYOUT_ACCOUNT_STATUS_PENDING,
			RequirementsDue: account.RequirementsDue,
		}
	}
}
