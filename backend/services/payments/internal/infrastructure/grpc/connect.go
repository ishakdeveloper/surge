package grpc

import (
	"context"
	"errors"

	"github.com/ishakdeveloper/surge/services/payments/internal/service"
	"github.com/ishakdeveloper/surge/shared/authz"
	paymentspb "github.com/ishakdeveloper/surge/shared/proto/payments"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (h *Handler) CreateAccountSession(ctx context.Context, _ *paymentspb.CreateAccountSessionRequest) (*paymentspb.CreateAccountSessionResponse, error) {
	caller, err := callerAs(ctx, authz.RoleDriver)
	if err != nil {
		return nil, err
	}
	secret, err := h.service.AccountSession(ctx, caller.UserID)
	switch {
	case errors.Is(err, service.ErrNoPayoutAccount):
		return nil, status.Error(codes.FailedPrecondition, "set up payouts first")
	case err != nil:
		return nil, status.Errorf(codes.Internal, "could not start an account session: %v", err)
	}
	return &paymentspb.CreateAccountSessionResponse{ClientSecret: secret}, nil
}

func (h *Handler) CreateDashboardLink(ctx context.Context, _ *paymentspb.CreateDashboardLinkRequest) (*paymentspb.CreateDashboardLinkResponse, error) {
	caller, err := callerAs(ctx, authz.RoleDriver)
	if err != nil {
		return nil, err
	}
	link, err := h.service.DashboardLink(ctx, caller.UserID)
	switch {
	case errors.Is(err, service.ErrNoPayoutAccount):
		return nil, status.Error(codes.FailedPrecondition, "set up payouts first")
	case err != nil:
		return nil, status.Errorf(codes.Internal, "could not open the dashboard: %v", err)
	}
	return &paymentspb.CreateDashboardLinkResponse{Url: link}, nil
}
