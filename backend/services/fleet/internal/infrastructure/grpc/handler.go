// Package grpc is the fleet's synchronous edge.
//
// A translation layer, as trip's and payments' are: proto in, service call,
// proto out, and the mapping from what went wrong to a status a client can act
// on.
package grpc

import (
	"context"
	"errors"
	"time"

	"github.com/ishakdeveloper/surge/services/fleet/internal/domain"
	"github.com/ishakdeveloper/surge/services/fleet/internal/service"
	"github.com/ishakdeveloper/surge/shared/authz"
	fleetpb "github.com/ishakdeveloper/surge/shared/proto/fleet"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Handler struct {
	fleetpb.UnimplementedFleetServiceServer
	service *service.Service
}

func NewHandler(fleet *service.Service) *Handler { return &Handler{service: fleet} }

// statusOf is the one place a fleet error becomes a status.
func statusOf(err error, action string) error {
	var invalid *domain.InvalidError
	switch {
	case errors.As(err, &invalid):
		return status.Error(codes.InvalidArgument, invalid.Reason)
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, "not found")
	case errors.Is(err, domain.ErrNotAllowed):
		return status.Error(codes.PermissionDenied, "that is not available to your role")
	case errors.Is(err, domain.ErrPlateTaken):
		return status.Error(codes.FailedPrecondition, "that plate is already registered to a driver")
	case errors.Is(err, domain.ErrConflict):
		return status.Error(codes.Aborted, "that changed while you were working on it, please try again")
	case errors.Is(err, service.ErrInvalidWebhook):
		return status.Error(codes.InvalidArgument, "the webhook signature does not check out")
	default:
		return status.Errorf(codes.Internal, "could not %s: %v", action, err)
	}
}

func (h *Handler) GetMyDriver(ctx context.Context, _ *fleetpb.GetMyDriverRequest) (*fleetpb.GetMyDriverResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.service.Papers(ctx, caller)
	if err != nil {
		return nil, statusOf(err, "read your standing")
	}
	return &fleetpb.GetMyDriverResponse{
		Driver:    toDriver(view.Driver, view.Outstanding),
		Vehicles:  toVehicles(view.Vehicles),
		Documents: toDocuments(view.Documents),
	}, nil
}

func (h *Handler) StartIdentityCheck(ctx context.Context, request *fleetpb.StartIdentityCheckRequest) (*fleetpb.StartIdentityCheckResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	session, driver, err := h.service.StartIdentity(ctx, caller, request.GetReturnUrl())
	if err != nil {
		return nil, statusOf(err, "start the identity check")
	}
	return &fleetpb.StartIdentityCheckResponse{Url: session.URL, Status: identities[driver.Identity]}, nil
}

func (h *Handler) AddVehicle(ctx context.Context, request *fleetpb.AddVehicleRequest) (*fleetpb.AddVehicleResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	vehicle, _, err := h.service.AddVehicle(ctx, caller, request.GetPlate(), request.GetPackageSlug())
	if err != nil {
		return nil, statusOf(err, "add the car")
	}
	return &fleetpb.AddVehicleResponse{Vehicle: toVehicle(vehicle)}, nil
}

func (h *Handler) ListVehicles(ctx context.Context, _ *fleetpb.ListVehiclesRequest) (*fleetpb.ListVehiclesResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.service.Papers(ctx, caller)
	if err != nil {
		return nil, statusOf(err, "list your cars")
	}
	return &fleetpb.ListVehiclesResponse{Vehicles: toVehicles(view.Vehicles)}, nil
}

func (h *Handler) RetireVehicle(ctx context.Context, request *fleetpb.RetireVehicleRequest) (*fleetpb.RetireVehicleResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	vehicle, err := h.service.RetireVehicle(ctx, caller, request.GetVehicleId())
	if err != nil {
		return nil, statusOf(err, "retire the car")
	}
	return &fleetpb.RetireVehicleResponse{Vehicle: toVehicle(vehicle)}, nil
}

func (h *Handler) StartUpload(ctx context.Context, request *fleetpb.StartUploadRequest) (*fleetpb.StartUploadResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	document, upload, err := h.service.StartUpload(ctx, caller,
		kindOf(request.GetKind()), request.GetVehicleId(), request.GetContentType(), request.GetByteSize())
	if err != nil {
		return nil, statusOf(err, "start the upload")
	}
	return &fleetpb.StartUploadResponse{
		Document:  toDocument(document),
		UploadUrl: upload.URL,
		ExpiresAt: rfc3339(upload.ExpiresAt),
	}, nil
}

func (h *Handler) FinishUpload(ctx context.Context, request *fleetpb.FinishUploadRequest) (*fleetpb.FinishUploadResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	document, err := h.service.FinishUpload(ctx, caller, request.GetDocumentId())
	if err != nil {
		return nil, statusOf(err, "finish the upload")
	}
	return &fleetpb.FinishUploadResponse{Document: toDocument(document)}, nil
}

func (h *Handler) DeclareAuthorityDocument(ctx context.Context, request *fleetpb.DeclareAuthorityDocumentRequest) (*fleetpb.DeclareAuthorityDocumentResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	document, err := h.service.DeclareAuthority(ctx, caller, kindOf(request.GetKind()))
	if err != nil {
		return nil, statusOf(err, "record the application")
	}
	return &fleetpb.DeclareAuthorityDocumentResponse{Document: toDocument(document)}, nil
}

func (h *Handler) ListDocuments(ctx context.Context, _ *fleetpb.ListDocumentsRequest) (*fleetpb.ListDocumentsResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	view, err := h.service.Papers(ctx, caller)
	if err != nil {
		return nil, statusOf(err, "list your documents")
	}
	return &fleetpb.ListDocumentsResponse{Documents: toDocuments(view.Documents)}, nil
}

func (h *Handler) ListReviewQueue(ctx context.Context, request *fleetpb.ListReviewQueueRequest) (*fleetpb.ListReviewQueueResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	page, err := h.service.Queue(ctx, caller, int(request.GetPageSize()), request.GetPageToken())
	if err != nil {
		return nil, statusOf(err, "read the queue")
	}

	items := make([]*fleetpb.ReviewItem, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, &fleetpb.ReviewItem{
			Driver:       toDriver(item.Driver, nil),
			Waiting:      int32(item.Waiting),
			WaitingSince: rfc3339(item.Since),
		})
	}
	return &fleetpb.ListReviewQueueResponse{Items: items, NextPageToken: page.NextCursor}, nil
}

func (h *Handler) GetReviewItem(ctx context.Context, request *fleetpb.GetReviewItemRequest) (*fleetpb.GetReviewItemResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	review, err := h.service.ReviewItem(ctx, caller, request.GetDriverId())
	if err != nil {
		return nil, statusOf(err, "read the case")
	}

	documents := make([]*fleetpb.ReviewDocument, 0, len(review.Documents))
	for _, item := range review.Documents {
		documents = append(documents, &fleetpb.ReviewDocument{
			Document: toDocument(item.Document),
			FileUrl:  item.FileURL,
		})
	}
	return &fleetpb.GetReviewItemResponse{
		Driver:    toDriver(review.Driver, review.Outstanding),
		Vehicles:  toVehicles(review.Vehicles),
		Documents: documents,
	}, nil
}

func (h *Handler) DecideDocument(ctx context.Context, request *fleetpb.DecideDocumentRequest) (*fleetpb.DecideDocumentResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	expires, err := readDate(request.GetExpiresAt())
	if err != nil {
		return nil, statusOf(err, "read the expiry date")
	}

	document, driver, err := h.service.DecideDocument(ctx, caller, request.GetDocumentId(), domain.Decision{
		Approve:   request.GetApprove(),
		ExpiresAt: expires,
		Note:      request.GetNote(),
	})
	if err != nil {
		return nil, statusOf(err, "decide the document")
	}
	return &fleetpb.DecideDocumentResponse{
		Document: toDocument(document),
		Driver:   toDriver(driver, nil),
	}, nil
}

func (h *Handler) DecideVehicle(ctx context.Context, request *fleetpb.DecideVehicleRequest) (*fleetpb.DecideVehicleResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	vehicle, driver, err := h.service.DecideVehicle(ctx, caller, request.GetVehicleId(), request.GetApprove(), request.GetNote())
	if err != nil {
		return nil, statusOf(err, "decide the car")
	}
	return &fleetpb.DecideVehicleResponse{
		Vehicle: toVehicle(vehicle),
		Driver:  toDriver(driver, nil),
	}, nil
}

func (h *Handler) BlockDriver(ctx context.Context, request *fleetpb.BlockDriverRequest) (*fleetpb.BlockDriverResponse, error) {
	caller, err := authz.RequireCaller(ctx)
	if err != nil {
		return nil, err
	}
	driver, err := h.service.Block(ctx, caller, request.GetDriverId(), request.GetReason())
	if err != nil {
		return nil, statusOf(err, "block the driver")
	}
	return &fleetpb.BlockDriverResponse{Driver: toDriver(driver, nil)}, nil
}

// DeliverWebhook needs no caller, unlike everything else here: the gateway
// forwards it without one, and the signature is the authentication.
func (h *Handler) DeliverWebhook(ctx context.Context, request *fleetpb.DeliverWebhookRequest) (*fleetpb.DeliverWebhookResponse, error) {
	if err := h.service.ReceiveWebhook(ctx, request.GetPayload(), request.GetSignature()); err != nil {
		return nil, statusOf(err, "apply the webhook")
	}
	return &fleetpb.DeliverWebhookResponse{}, nil
}

// --- translation --------------------------------------------------------------

var statuses = map[domain.Status]fleetpb.DriverStatus{
	domain.StatusOnboarding: fleetpb.DriverStatus_DRIVER_STATUS_ONBOARDING,
	domain.StatusApproved:   fleetpb.DriverStatus_DRIVER_STATUS_APPROVED,
	domain.StatusBlocked:    fleetpb.DriverStatus_DRIVER_STATUS_BLOCKED,
}

var identities = map[domain.IdentityStatus]fleetpb.IdentityStatus{
	domain.IdentityUnstarted:  fleetpb.IdentityStatus_IDENTITY_STATUS_UNSTARTED,
	domain.IdentityPending:    fleetpb.IdentityStatus_IDENTITY_STATUS_PENDING,
	domain.IdentityProcessing: fleetpb.IdentityStatus_IDENTITY_STATUS_PROCESSING,
	domain.IdentityVerified:   fleetpb.IdentityStatus_IDENTITY_STATUS_VERIFIED,
	domain.IdentityFailed:     fleetpb.IdentityStatus_IDENTITY_STATUS_FAILED,
}

var requirements = map[domain.RequirementKind]fleetpb.RequirementKind{
	domain.NeedIdentity:        fleetpb.RequirementKind_REQUIREMENT_KIND_IDENTITY,
	domain.NeedVehicle:         fleetpb.RequirementKind_REQUIREMENT_KIND_VEHICLE,
	domain.NeedInsurance:       fleetpb.RequirementKind_REQUIREMENT_KIND_INSURANCE,
	domain.NeedVOG:             fleetpb.RequirementKind_REQUIREMENT_KIND_VOG,
	domain.NeedChauffeurskaart: fleetpb.RequirementKind_REQUIREMENT_KIND_CHAUFFEURSKAART,
}

var vehicleStatuses = map[domain.VehicleStatus]fleetpb.VehicleStatus{
	domain.VehiclePending:  fleetpb.VehicleStatus_VEHICLE_STATUS_PENDING,
	domain.VehicleApproved: fleetpb.VehicleStatus_VEHICLE_STATUS_APPROVED,
	domain.VehicleRejected: fleetpb.VehicleStatus_VEHICLE_STATUS_REJECTED,
	domain.VehicleRetired:  fleetpb.VehicleStatus_VEHICLE_STATUS_RETIRED,
}

var kinds = map[domain.DocumentKind]fleetpb.DocumentKind{
	domain.KindInsurance:       fleetpb.DocumentKind_DOCUMENT_KIND_INSURANCE,
	domain.KindRegistration:    fleetpb.DocumentKind_DOCUMENT_KIND_REGISTRATION,
	domain.KindVOG:             fleetpb.DocumentKind_DOCUMENT_KIND_VOG,
	domain.KindChauffeurskaart: fleetpb.DocumentKind_DOCUMENT_KIND_CHAUFFEURSKAART,
}

var documentStatuses = map[domain.DocumentStatus]fleetpb.DocumentStatus{
	domain.AwaitingFile:      fleetpb.DocumentStatus_DOCUMENT_STATUS_AWAITING_FILE,
	domain.AwaitingAuthority: fleetpb.DocumentStatus_DOCUMENT_STATUS_AWAITING_AUTHORITY,
	domain.Submitted:         fleetpb.DocumentStatus_DOCUMENT_STATUS_SUBMITTED,
	domain.Approved:          fleetpb.DocumentStatus_DOCUMENT_STATUS_APPROVED,
	domain.Rejected:          fleetpb.DocumentStatus_DOCUMENT_STATUS_REJECTED,
	domain.Expired:           fleetpb.DocumentStatus_DOCUMENT_STATUS_EXPIRED,
}

var extractions = map[domain.ExtractionStatus]fleetpb.ExtractionStatus{
	domain.ExtractionNone:    fleetpb.ExtractionStatus_EXTRACTION_STATUS_NONE,
	domain.ExtractionPending: fleetpb.ExtractionStatus_EXTRACTION_STATUS_PENDING,
	domain.ExtractionDone:    fleetpb.ExtractionStatus_EXTRACTION_STATUS_DONE,
	domain.ExtractionFailed:  fleetpb.ExtractionStatus_EXTRACTION_STATUS_FAILED,
}

// kindOf reads a document kind from the wire. An unknown one is left empty and
// refused by the domain, which has the words for why.
func kindOf(kind fleetpb.DocumentKind) domain.DocumentKind {
	for domainKind, protoKind := range kinds {
		if protoKind == kind {
			return domainKind
		}
	}
	return ""
}

// rfc3339 renders a time as the API's string, and an unset one as empty —
// every unset field this API sends is empty rather than absent.
func rfc3339(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

// readDate reads a date a reviewer typed. Both spellings are accepted: a date
// on its own is what a person reads off a certificate, and a timestamp is what
// a date picker sends.
func readDate(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339, time.DateOnly} {
		if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, &domain.InvalidError{Reason: "a date is YYYY-MM-DD"}
}

func toDriver(driver domain.Driver, outstanding []domain.Requirement) *fleetpb.Driver {
	needs := make([]*fleetpb.Requirement, 0, len(outstanding))
	for _, requirement := range outstanding {
		needs = append(needs, &fleetpb.Requirement{
			Kind:   requirements[requirement.Kind],
			Detail: requirement.Detail,
		})
	}
	return &fleetpb.Driver{
		DriverId:         driver.ID,
		Status:           statuses[driver.Status],
		BlockedReason:    driver.BlockedReason,
		Identity:         identities[driver.Identity],
		VerifiedName:     driver.VerifiedName,
		LicenceExpiresAt: rfc3339(driver.LicenceExpiresAt),
		Outstanding:      needs,
		ApprovedAt:       rfc3339(driver.ApprovedAt),
		CreatedAt:        timestamppb.New(driver.CreatedAt),
		UpdatedAt:        timestamppb.New(driver.UpdatedAt),
	}
}

func toVehicle(vehicle domain.Vehicle) *fleetpb.Vehicle {
	return &fleetpb.Vehicle{
		Id:                vehicle.ID,
		DriverId:          vehicle.DriverID,
		Plate:             vehicle.Plate,
		Make:              vehicle.Make,
		Model:             vehicle.Model,
		Colour:            vehicle.Colour,
		Seats:             int32(vehicle.Seats),
		PackageSlug:       vehicle.PackageSlug,
		Status:            vehicleStatuses[vehicle.Status],
		RejectedReason:    vehicle.RejectedReason,
		ApkExpiresAt:      rfc3339(vehicle.APKExpiresAt),
		TaxiRegistered:    vehicle.TaxiRegistered,
		Insured:           vehicle.Insured,
		FirstRegisteredAt: rfc3339(vehicle.FirstRegisteredAt),
		RegisterCheckedAt: rfc3339(vehicle.RegisterCheckedAt),
		CreatedAt:         timestamppb.New(vehicle.CreatedAt),
		UpdatedAt:         timestamppb.New(vehicle.UpdatedAt),
	}
}

func toVehicles(vehicles []domain.Vehicle) []*fleetpb.Vehicle {
	out := make([]*fleetpb.Vehicle, 0, len(vehicles))
	for _, vehicle := range vehicles {
		out = append(out, toVehicle(vehicle))
	}
	return out
}

func toDocument(document domain.Document) *fleetpb.Document {
	return &fleetpb.Document{
		Id:         document.ID,
		DriverId:   document.DriverID,
		VehicleId:  document.VehicleID,
		Kind:       kinds[document.Kind],
		Status:     documentStatuses[document.Status],
		ExpiresAt:  rfc3339(document.ExpiresAt),
		Extracted:  toExtraction(document.Extracted),
		ReviewNote: document.ReviewNote,
		ReviewedAt: rfc3339(document.ReviewedAt),
		CreatedAt:  timestamppb.New(document.CreatedAt),
		UpdatedAt:  timestamppb.New(document.UpdatedAt),
	}
}

func toDocuments(documents []domain.Document) []*fleetpb.Document {
	out := make([]*fleetpb.Document, 0, len(documents))
	for _, document := range documents {
		out = append(out, toDocument(document))
	}
	return out
}

func toExtraction(extraction domain.Extraction) *fleetpb.Extraction {
	return &fleetpb.Extraction{
		Status:       extractions[extraction.Status],
		Insurer:      extraction.Insurer,
		PolicyNumber: extraction.PolicyNumber,
		ExpiresAt:    rfc3339(extraction.ExpiresAt),
		Quotes:       extraction.Quotes,
	}
}
