// Package grpc is core's gRPC edge: the caller from the gateway's verified
// token, a domain error as a status someone can act on.
package grpc

import (
	"context"
	"errors"

	"github.com/ishakdeveloper/surge/services/core/internal/domain"
	"github.com/ishakdeveloper/surge/services/core/internal/service"
	"github.com/ishakdeveloper/surge/shared/authz"
	profilepb "github.com/ishakdeveloper/surge/shared/proto/profile"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Handler struct {
	profilepb.UnimplementedProfileServiceServer
	profiles *service.Service
}

func NewHandler(profiles *service.Service) *Handler { return &Handler{profiles: profiles} }

func (h *Handler) GetMyProfile(ctx context.Context, _ *profilepb.GetMyProfileRequest) (*profilepb.GetMyProfileResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.profiles.Get(ctx, caller.UserID)
	if err != nil {
		return nil, failure(err, "read your profile")
	}
	return &profilepb.GetMyProfileResponse{Profile: toProto(view)}, nil
}

func (h *Handler) UpdateMyProfile(ctx context.Context, request *profilepb.UpdateMyProfileRequest) (*profilepb.UpdateMyProfileResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.profiles.Rename(ctx, caller.UserID, request.GetDisplayName())
	if err != nil {
		return nil, failure(err, "save your name")
	}
	return &profilepb.UpdateMyProfileResponse{Profile: toProto(view)}, nil
}

func (h *Handler) UploadAvatar(ctx context.Context, request *profilepb.UploadAvatarRequest) (*profilepb.UploadAvatarResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.profiles.SetAvatar(ctx, caller.UserID, request.GetImage())
	if err != nil {
		return nil, failure(err, "save your photo")
	}
	return &profilepb.UploadAvatarResponse{Profile: toProto(view)}, nil
}

func (h *Handler) RemoveAvatar(ctx context.Context, _ *profilepb.RemoveAvatarRequest) (*profilepb.RemoveAvatarResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.profiles.RemoveAvatar(ctx, caller.UserID)
	if err != nil {
		return nil, failure(err, "remove your photo")
	}
	return &profilepb.RemoveAvatarResponse{Profile: toProto(view)}, nil
}

// GetProfile is anyone's, to any signed-in caller: an id reaches someone only
// through a trip or a conversation they share, and a first name and a photo
// are what that person sees of them anyway.
func (h *Handler) GetProfile(ctx context.Context, request *profilepb.GetProfileRequest) (*profilepb.GetProfileResponse, error) {
	if _, err := authz.RequireCaller(ctx); err != nil {
		return nil, err
	}
	view, err := h.profiles.Get(ctx, request.GetUserId())
	if err != nil {
		return nil, failure(err, "read that profile")
	}
	return &profilepb.GetProfileResponse{Profile: toProto(view)}, nil
}

func toProto(view service.View) *profilepb.Profile {
	return &profilepb.Profile{
		UserId:      view.Profile.UserID,
		DisplayName: view.Profile.DisplayName,
		AvatarUrl:   view.AvatarURL,
	}
}

// failure says what went wrong in words the person can act on; anything else
// is ours.
func failure(err error, action string) error {
	switch {
	case errors.Is(err, domain.ErrNameEmpty):
		return status.Error(codes.InvalidArgument, "Enter the first name riders and drivers will see.")
	case errors.Is(err, domain.ErrNameTooLong):
		return status.Errorf(codes.InvalidArgument, "Keep it to %d characters.", domain.MaxNameLength)
	case errors.Is(err, domain.ErrNameInvalid):
		return status.Error(codes.InvalidArgument, "That name has characters we cannot show.")
	case errors.Is(err, domain.ErrNotAnImage):
		return status.Error(codes.InvalidArgument, "That file is not a JPEG, PNG or WebP photo.")
	case errors.Is(err, domain.ErrTooLarge):
		return status.Error(codes.InvalidArgument, "That photo is too large. Choose one under 5 MB.")
	case errors.Is(err, domain.ErrInvalidUser):
		return status.Error(codes.InvalidArgument, "That is not a user id.")
	default:
		return status.Errorf(codes.Internal, "could not %s: %v", action, err)
	}
}
