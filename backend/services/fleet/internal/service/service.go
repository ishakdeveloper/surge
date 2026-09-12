// Package service is the fleet's logic: what a driver must do before they can
// be offered work, and what a reviewer may do about it.
//
// It depends on ports — a register, an identity provider, object storage, a
// reader — so every rule here runs in a test that opens no socket and holds no
// key.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/ishakdeveloper/surge/services/fleet/internal/domain"
	"github.com/ishakdeveloper/surge/shared/authz"
)

var (
	// ErrNoSuchPlate is a plate the register does not have.
	ErrNoSuchPlate = errors.New("service: the register has no such plate")
	// ErrInvalidWebhook is a webhook whose signature does not check out.
	ErrInvalidWebhook = errors.New("service: webhook signature is not valid")
)

// Fact is what the rest of the system hears when a driver's standing changes.
//
// Keyed by driver on a compacted topic, so a matcher starting cold reads the
// current standing of every driver rather than a history of decisions.
type Fact struct {
	DriverID    string
	Status      domain.Status
	Plate       string
	PackageSlug string
	At          time.Time
}

// Repository is where the fleet's state lives.
type Repository interface {
	// EnsureDriver returns the driver's row, creating it the first time they
	// start onboarding.
	EnsureDriver(ctx context.Context, driverID string, now time.Time) (domain.Driver, error)
	Driver(ctx context.Context, driverID string) (domain.Driver, error)
	// DriverBySession finds whoever a provider's webhook is about.
	DriverBySession(ctx context.Context, sessionID string) (domain.Driver, error)
	// SaveDriver stores the driver and, when a fact is given, the news of it,
	// together.
	SaveDriver(ctx context.Context, driver domain.Driver, fact *Fact) error
	// Papers is everything a driver's standing is worked out from.
	Papers(ctx context.Context, driverID string) (domain.Papers, error)

	Vehicle(ctx context.Context, vehicleID string) (domain.Vehicle, error)
	// SaveVehicle stores a vehicle, refusing a plate another driver still
	// offers with ErrPlateTaken.
	SaveVehicle(ctx context.Context, vehicle domain.Vehicle) error

	Document(ctx context.Context, documentID string) (domain.Document, error)
	SaveDocument(ctx context.Context, document domain.Document) error

	// Queue is what is waiting for a reviewer, oldest first.
	Queue(ctx context.Context, limit int, cursor string) (QueuePage, error)
	// ExpireDocuments moves approved documents past their date to expired, and
	// returns whose they were.
	ExpireDocuments(ctx context.Context, now time.Time) ([]string, error)
}

// QueuePage is one page of the review queue.
type QueuePage struct {
	Items      []QueueItem
	NextCursor string
}

// QueueItem is one driver waiting, and since when.
type QueueItem struct {
	Driver  domain.Driver
	Waiting int
	Since   time.Time
}

// Session is a verification the driver finishes at a provider.
type Session struct {
	ID  string
	URL string
}

// Verdict is what the provider decided.
type Verdict struct {
	SessionID        string
	Status           domain.IdentityStatus
	Name             string
	Document         string
	LicenceExpiresAt time.Time
	// Reason is why a failed check failed, in the provider's words.
	Reason string
}

// Identity opens verification sessions.
type Identity interface {
	Start(ctx context.Context, driverID, returnURL string) (Session, error)
}

// Webhooks verifies what an identity provider sends afterwards, and says what
// it means. The second return is false for an event that was not about a
// verification at all.
type Webhooks interface {
	Receive(ctx context.Context, payload []byte, signature string) (Verdict, bool, error)
}

// Register is the vehicle register.
type Register interface {
	Lookup(ctx context.Context, plate string) (domain.Registration, error)
}

// Upload is a link to put one file to.
type Upload struct {
	URL       string
	ExpiresAt time.Time
}

// Store is where documents live.
type Store interface {
	// Upload is a link the app puts the file to itself, so a scan never
	// travels through this API as base64.
	Upload(ctx context.Context, key, contentType string, size int64, now time.Time) (Upload, error)
	// View is a link a reviewer can open, good for as long as a review takes.
	View(key string, now time.Time) (string, error)
}

// Reader reads what it can off a document.
type Reader interface {
	Read(ctx context.Context, key, contentType string) (domain.Extraction, error)
}

// Hooks are the observability seams.
type Hooks struct {
	// OnStandingChanged counts a driver's standing moving, by the status they
	// moved to.
	OnStandingChanged func(status domain.Status)
	// OnExtraction counts a document read, by outcome.
	OnExtraction func(outcome string)
}

type Options struct {
	Repository Repository
	Identity   Identity
	Register   Register
	Store      Store
	Reader     Reader
	// Webhooks is absent for a provider that sends none, and DeliverWebhook
	// then refuses rather than pretending to have applied something.
	Webhooks Webhooks
	Now      func() time.Time
	NewID    func() string
	// ReadTimeout bounds one document reading.
	ReadTimeout time.Duration
	Hooks       Hooks
}

// DefaultReadTimeout is how long a machine may spend reading one document.
const DefaultReadTimeout = 45 * time.Second

type Service struct {
	repo        Repository
	identity    Identity
	register    Register
	store       Store
	reader      Reader
	webhooks    Webhooks
	now         func() time.Time
	newID       func() string
	readTimeout time.Duration
	hooks       Hooks
}

func New(options Options) (*Service, error) {
	if options.Repository == nil || options.Identity == nil || options.Register == nil || options.Store == nil {
		return nil, errors.New("service: a repository, an identity provider, a register and a store are required")
	}
	s := &Service{
		repo: options.Repository, identity: options.Identity, register: options.Register,
		store: options.Store, reader: options.Reader, webhooks: options.Webhooks,
		now: options.Now, newID: options.NewID, readTimeout: options.ReadTimeout, hooks: options.Hooks,
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.newID == nil {
		s.newID = uuid.NewString
	}
	if s.readTimeout <= 0 {
		s.readTimeout = DefaultReadTimeout
	}
	return s, nil
}

// Now is the service's clock, for the edges that render a standing.
func (s *Service) Now() time.Time { return s.now() }

// View is a driver's own papers, and what is still outstanding.
type View struct {
	Driver      domain.Driver
	Outstanding []domain.Requirement
	Vehicles    []domain.Vehicle
	Documents   []domain.Document
}

// driverOnly is the rule for everything a driver does about themselves. Ops
// read a driver's papers through the review queue, which says who they are
// looking at.
func driverOnly(caller authz.Identity) error {
	if caller.Role != authz.RoleDriver {
		return domain.ErrNotAllowed
	}
	return nil
}

func reviewerOnly(caller authz.Identity) error {
	if caller.Role != authz.RoleOps {
		return domain.ErrNotAllowed
	}
	return nil
}

// Papers is the driver's own standing and everything behind it.
func (s *Service) Papers(ctx context.Context, caller authz.Identity) (View, error) {
	if err := driverOnly(caller); err != nil {
		return View{}, err
	}
	now := s.now()
	if _, err := s.repo.EnsureDriver(ctx, caller.UserID, now); err != nil {
		return View{}, err
	}
	papers, err := s.repo.Papers(ctx, caller.UserID)
	if err != nil {
		return View{}, err
	}
	return View{
		Driver:      papers.Driver,
		Outstanding: domain.Outstanding(papers, now),
		Vehicles:    papers.Vehicles,
		Documents:   papers.Documents,
	}, nil
}

// StartIdentity opens a verification session, or returns the one already open.
//
// A session costs money and a driver taps twice, so an open one is handed back
// rather than replaced.
func (s *Service) StartIdentity(ctx context.Context, caller authz.Identity, returnURL string) (Session, domain.Driver, error) {
	if err := driverOnly(caller); err != nil {
		return Session{}, domain.Driver{}, err
	}
	now := s.now()
	driver, err := s.repo.EnsureDriver(ctx, caller.UserID, now)
	if err != nil {
		return Session{}, domain.Driver{}, err
	}
	if driver.Identity == domain.IdentityVerified {
		return Session{}, driver, &domain.InvalidError{Reason: "your identity is already verified"}
	}

	session, err := s.identity.Start(ctx, caller.UserID, returnURL)
	if err != nil {
		return Session{}, domain.Driver{}, err
	}

	driver.Identity = domain.IdentityPending
	driver.IdentitySessionID = session.ID
	driver.BlockedReason = ""
	driver.UpdatedAt = now
	if err := s.repo.SaveDriver(ctx, driver, nil); err != nil {
		return Session{}, domain.Driver{}, err
	}
	return session, driver, nil
}

// ApplyVerdict records what the provider decided, and works the driver's
// standing out again.
//
// Idempotent: the same verdict twice is the same driver. A verdict about a
// session nobody here started is not an error — it is somebody else's webhook.
func (s *Service) ApplyVerdict(ctx context.Context, verdict Verdict) error {
	driver, err := s.repo.DriverBySession(ctx, verdict.SessionID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	now := s.now()
	driver.Identity = verdict.Status
	driver.UpdatedAt = now
	switch verdict.Status {
	case domain.IdentityVerified:
		driver.VerifiedName = verdict.Name
		driver.VerifiedDocument = verdict.Document
		driver.LicenceExpiresAt = verdict.LicenceExpiresAt
		driver.BlockedReason = ""
	case domain.IdentityFailed:
		driver.BlockedReason = verdict.Reason
	}
	if err := s.repo.SaveDriver(ctx, driver, nil); err != nil {
		return err
	}
	_, err = s.restand(ctx, driver.ID, now)
	return err
}

// ReceiveWebhook verifies one webhook and applies what it decided.
//
// An event the provider sends that is about something else is not an error:
// one endpoint receives whatever the account is subscribed to.
func (s *Service) ReceiveWebhook(ctx context.Context, payload []byte, signature string) error {
	if s.webhooks == nil {
		return &domain.InvalidError{Reason: "this identity provider sends no webhooks"}
	}
	verdict, relevant, err := s.webhooks.Receive(ctx, payload, signature)
	if err != nil || !relevant {
		return err
	}
	return s.ApplyVerdict(ctx, verdict)
}

// AddVehicle registers a car from what the register says about its plate.
//
// The notes are what a reviewer should weigh — a car with no taxi
// registration, an inspection due next month. What cannot be argued with —
// no such plate, a lapsed inspection, no insurance, too few seats — is
// refused here, with the reason, rather than queued for a person to refuse.
func (s *Service) AddVehicle(ctx context.Context, caller authz.Identity, plate, packageSlug string) (domain.Vehicle, []string, error) {
	if err := driverOnly(caller); err != nil {
		return domain.Vehicle{}, nil, err
	}
	normalized, err := domain.NormalizePlate(plate)
	if err != nil {
		return domain.Vehicle{}, nil, err
	}
	class, ok := domain.ClassBySlug(packageSlug)
	if !ok {
		return domain.Vehicle{}, nil, &domain.InvalidError{Reason: "that is not a ride class Surge offers"}
	}

	now := s.now()
	if _, err := s.repo.EnsureDriver(ctx, caller.UserID, now); err != nil {
		return domain.Vehicle{}, nil, err
	}

	registration, err := s.register.Lookup(ctx, normalized)
	if errors.Is(err, ErrNoSuchPlate) {
		return domain.Vehicle{}, nil, &domain.InvalidError{Reason: "the register has no car with that plate"}
	}
	if err != nil {
		return domain.Vehicle{}, nil, err
	}

	notes, err := domain.CheckRegistration(registration, class, now)
	if err != nil {
		return domain.Vehicle{}, nil, err
	}

	vehicle := domain.NewVehicle(s.newID(), caller.UserID, registration, class, now)
	if err := s.repo.SaveVehicle(ctx, vehicle); err != nil {
		return domain.Vehicle{}, nil, err
	}
	return vehicle, notes, nil
}

// RetireVehicle takes a car off the road, freeing its plate for whoever owns
// it next.
func (s *Service) RetireVehicle(ctx context.Context, caller authz.Identity, vehicleID string) (domain.Vehicle, error) {
	if err := driverOnly(caller); err != nil {
		return domain.Vehicle{}, err
	}
	vehicle, err := s.repo.Vehicle(ctx, vehicleID)
	if err != nil {
		return domain.Vehicle{}, err
	}
	if vehicle.DriverID != caller.UserID {
		return domain.Vehicle{}, domain.ErrNotFound
	}

	now := s.now()
	vehicle.Status = domain.VehicleRetired
	vehicle.UpdatedAt = now
	if err := s.repo.SaveVehicle(ctx, vehicle); err != nil {
		return domain.Vehicle{}, err
	}
	if _, err := s.restand(ctx, caller.UserID, now); err != nil {
		return domain.Vehicle{}, err
	}
	return vehicle, nil
}

// StartUpload hands out a link to put one file to, and the document it will
// belong to.
//
// The document that kind already has is reused, so a driver who sends a
// clearer photograph of the same certificate has one document with one
// history rather than two a reviewer has to reconcile.
func (s *Service) StartUpload(
	ctx context.Context, caller authz.Identity,
	kind domain.DocumentKind, vehicleID, contentType string, size int64,
) (domain.Document, Upload, error) {
	if err := driverOnly(caller); err != nil {
		return domain.Document{}, Upload{}, err
	}
	if err := domain.CheckUpload(kind, contentType, size); err != nil {
		return domain.Document{}, Upload{}, err
	}

	now := s.now()
	if _, err := s.repo.EnsureDriver(ctx, caller.UserID, now); err != nil {
		return domain.Document{}, Upload{}, err
	}
	if kind.AboutAVehicle() {
		vehicle, err := s.repo.Vehicle(ctx, vehicleID)
		if err != nil || vehicle.DriverID != caller.UserID {
			return domain.Document{}, Upload{}, &domain.InvalidError{Reason: "that document belongs to a car of yours"}
		}
	} else {
		vehicleID = ""
	}

	papers, err := s.repo.Papers(ctx, caller.UserID)
	if err != nil {
		return domain.Document{}, Upload{}, err
	}

	document, found := existing(papers.Documents, kind, vehicleID)
	if !found {
		document = domain.Document{
			ID: s.newID(), DriverID: caller.UserID, VehicleID: vehicleID, Kind: kind,
			CreatedAt: now,
		}
	}
	document.Status = domain.AwaitingFile
	document.ContentType = contentType
	document.ByteSize = size
	document.ObjectKey = fmt.Sprintf("documents/%s/%s.%s", caller.UserID, document.ID, domain.UploadTypes[contentType])
	document.Extracted = domain.Extraction{Status: domain.ExtractionNone}
	document.UpdatedAt = now

	upload, err := s.store.Upload(ctx, document.ObjectKey, contentType, size, now)
	if err != nil {
		return domain.Document{}, Upload{}, err
	}
	if err := s.repo.SaveDocument(ctx, document); err != nil {
		return domain.Document{}, Upload{}, err
	}
	return document, upload, nil
}

// FinishUpload is the app saying the file arrived.
//
// The document goes to a person either way; a machine reads it first only to
// save the reviewer typing, and a reading that fails changes nothing about
// what happens next.
func (s *Service) FinishUpload(ctx context.Context, caller authz.Identity, documentID string) (domain.Document, error) {
	if err := driverOnly(caller); err != nil {
		return domain.Document{}, err
	}
	document, err := s.repo.Document(ctx, documentID)
	if err != nil {
		return domain.Document{}, err
	}
	if document.DriverID != caller.UserID {
		return domain.Document{}, domain.ErrNotFound
	}
	if document.ObjectKey == "" {
		return domain.Document{}, &domain.InvalidError{Reason: "ask for an upload link first"}
	}

	now := s.now()
	document.Status = domain.Submitted
	document.UpdatedAt = now
	if s.reader != nil && document.Kind == domain.KindInsurance {
		document.Extracted = domain.Extraction{Status: domain.ExtractionPending}
	}
	if err := s.repo.SaveDocument(ctx, document); err != nil {
		return domain.Document{}, err
	}

	if document.Extracted.Status == domain.ExtractionPending {
		s.readInBackground(ctx, document)
	}
	return document, nil
}

// readInBackground reads a document after the driver's request has returned.
//
// Detached from that request for the reason a push is: the driver's phone has
// gone back to a list, and a reading that takes ten seconds must not be
// abandoned because of it.
func (s *Service) readInBackground(ctx context.Context, document domain.Document) {
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.readTimeout)
		defer cancel()

		extraction, err := s.reader.Read(ctx, document.ObjectKey, document.ContentType)
		if err != nil {
			slog.Warn("could not read a document", "document", document.ID, "error", err)
			extraction = domain.Extraction{Status: domain.ExtractionFailed}
		}
		if s.hooks.OnExtraction != nil {
			s.hooks.OnExtraction(string(extraction.Status))
		}

		stored, err := s.repo.Document(ctx, document.ID)
		if err != nil {
			return
		}
		// A reviewer may have decided it while the machine was reading. Their
		// decision stands.
		if stored.Status != domain.Submitted {
			return
		}
		stored.Extracted = extraction
		stored.UpdatedAt = s.now()
		if err := s.repo.SaveDocument(ctx, stored); err != nil {
			slog.Warn("could not store what was read", "document", document.ID, "error", err)
		}
	}()
}

// DeclareAuthority records that the driver has applied for a VOG or a
// chauffeurskaart.
//
// Neither has an API anywhere: Justis answers in one to four weeks and Kiwa
// posts a card about four weeks after a complete application. So the document
// waits in the open, where the driver and a reviewer can both see what it is
// waiting for, and becomes a file when one arrives.
func (s *Service) DeclareAuthority(ctx context.Context, caller authz.Identity, kind domain.DocumentKind) (domain.Document, error) {
	if err := driverOnly(caller); err != nil {
		return domain.Document{}, err
	}
	if !kind.FromAuthority() {
		return domain.Document{}, &domain.InvalidError{Reason: "that document is not one an authority issues"}
	}

	now := s.now()
	if _, err := s.repo.EnsureDriver(ctx, caller.UserID, now); err != nil {
		return domain.Document{}, err
	}
	papers, err := s.repo.Papers(ctx, caller.UserID)
	if err != nil {
		return domain.Document{}, err
	}

	document, found := existing(papers.Documents, kind, "")
	if found && document.Status != domain.Rejected {
		return document, nil
	}
	if !found {
		document = domain.Document{ID: s.newID(), DriverID: caller.UserID, Kind: kind, CreatedAt: now}
	}
	document.Status = domain.AwaitingAuthority
	document.ReviewNote = ""
	document.UpdatedAt = now
	if err := s.repo.SaveDocument(ctx, document); err != nil {
		return domain.Document{}, err
	}
	return document, nil
}

// Queue is what is waiting for a reviewer.
func (s *Service) Queue(ctx context.Context, caller authz.Identity, limit int, cursor string) (QueuePage, error) {
	if err := reviewerOnly(caller); err != nil {
		return QueuePage{}, err
	}
	return s.repo.Queue(ctx, limit, cursor)
}

// Review is one driver's whole case, with a link to each file.
type Review struct {
	Driver      domain.Driver
	Outstanding []domain.Requirement
	Vehicles    []domain.Vehicle
	Documents   []ReviewDocument
}

// ReviewDocument is a document and where to read it.
type ReviewDocument struct {
	Document domain.Document
	FileURL  string
}

// ReviewItem is everything a reviewer needs about one driver.
func (s *Service) ReviewItem(ctx context.Context, caller authz.Identity, driverID string) (Review, error) {
	if err := reviewerOnly(caller); err != nil {
		return Review{}, err
	}
	papers, err := s.repo.Papers(ctx, driverID)
	if err != nil {
		return Review{}, err
	}

	now := s.now()
	documents := make([]ReviewDocument, 0, len(papers.Documents))
	for _, document := range papers.Documents {
		item := ReviewDocument{Document: document}
		if document.ObjectKey != "" {
			link, err := s.store.View(document.ObjectKey, now)
			if err != nil {
				return Review{}, err
			}
			item.FileURL = link
		}
		documents = append(documents, item)
	}

	return Review{
		Driver:      papers.Driver,
		Outstanding: domain.Outstanding(papers, now),
		Vehicles:    papers.Vehicles,
		Documents:   documents,
	}, nil
}

// DecideDocument is a reviewer approving or rejecting one document.
func (s *Service) DecideDocument(
	ctx context.Context, caller authz.Identity, documentID string, decision domain.Decision,
) (domain.Document, domain.Driver, error) {
	if err := reviewerOnly(caller); err != nil {
		return domain.Document{}, domain.Driver{}, err
	}
	document, err := s.repo.Document(ctx, documentID)
	if err != nil {
		return domain.Document{}, domain.Driver{}, err
	}

	now := s.now()
	decision.Reviewer = caller.UserID
	if err := domain.CheckDecision(document, decision, now); err != nil {
		return domain.Document{}, domain.Driver{}, err
	}

	document = domain.Decided(document, decision, now)
	if err := s.repo.SaveDocument(ctx, document); err != nil {
		return domain.Document{}, domain.Driver{}, err
	}
	driver, err := s.restand(ctx, document.DriverID, now)
	return document, driver, err
}

// DecideVehicle is a reviewer approving or rejecting a car.
func (s *Service) DecideVehicle(
	ctx context.Context, caller authz.Identity, vehicleID string, approve bool, note string,
) (domain.Vehicle, domain.Driver, error) {
	if err := reviewerOnly(caller); err != nil {
		return domain.Vehicle{}, domain.Driver{}, err
	}
	vehicle, err := s.repo.Vehicle(ctx, vehicleID)
	if err != nil {
		return domain.Vehicle{}, domain.Driver{}, err
	}
	if !approve && note == "" {
		return domain.Vehicle{}, domain.Driver{}, &domain.InvalidError{
			Reason: "say why, so the driver knows what to do about it",
		}
	}

	now := s.now()
	vehicle.Status = domain.VehicleRejected
	vehicle.RejectedReason = note
	if approve {
		vehicle.Status = domain.VehicleApproved
		vehicle.RejectedReason = ""
	}
	vehicle.UpdatedAt = now
	if err := s.repo.SaveVehicle(ctx, vehicle); err != nil {
		return domain.Vehicle{}, domain.Driver{}, err
	}
	driver, err := s.restand(ctx, vehicle.DriverID, now)
	return vehicle, driver, err
}

// Block stops a driver being offered work, or lifts the block. An empty reason
// lifts it, and their standing is worked out from their papers again.
func (s *Service) Block(ctx context.Context, caller authz.Identity, driverID, reason string) (domain.Driver, error) {
	if err := reviewerOnly(caller); err != nil {
		return domain.Driver{}, err
	}
	driver, err := s.repo.Driver(ctx, driverID)
	if err != nil {
		return domain.Driver{}, err
	}

	now := s.now()
	driver.BlockedReason = reason
	driver.Status = domain.StatusBlocked
	if reason == "" {
		driver.Status = domain.StatusOnboarding
	}
	driver.UpdatedAt = now
	if err := s.repo.SaveDriver(ctx, driver, nil); err != nil {
		return domain.Driver{}, err
	}
	return s.restand(ctx, driverID, now)
}

// Sweep expires the documents whose dates have passed, and works out again
// what their drivers may do.
//
// Every instance runs it; expiring a document twice is the same document, and
// the standing that follows is derived rather than toggled.
func (s *Service) Sweep(ctx context.Context) (int, error) {
	now := s.now()
	drivers, err := s.repo.ExpireDocuments(ctx, now)
	if err != nil {
		return 0, err
	}
	for _, driverID := range drivers {
		if _, err := s.restand(ctx, driverID, now); err != nil {
			return 0, err
		}
	}
	return len(drivers), nil
}

// restand works a driver's standing out from their papers and stores it if it
// moved. The news goes out in the same transaction as the change.
func (s *Service) restand(ctx context.Context, driverID string, now time.Time) (domain.Driver, error) {
	papers, err := s.repo.Papers(ctx, driverID)
	if err != nil {
		return domain.Driver{}, err
	}

	driver := papers.Driver
	status := domain.StandingOf(papers, now)
	if status == driver.Status {
		return driver, nil
	}

	driver.Status = status
	driver.UpdatedAt = now
	if status == domain.StatusApproved && driver.ApprovedAt.IsZero() {
		driver.ApprovedAt = now
	}

	fact := Fact{DriverID: driverID, Status: status, At: now}
	if vehicle, ok := domain.UsableVehicle(papers.Vehicles, now); ok {
		fact.Plate = vehicle.Plate
		fact.PackageSlug = vehicle.PackageSlug
	}
	if err := s.repo.SaveDriver(ctx, driver, &fact); err != nil {
		return domain.Driver{}, err
	}
	if s.hooks.OnStandingChanged != nil {
		s.hooks.OnStandingChanged(status)
	}
	return driver, nil
}

// existing finds the document of a kind a driver already has, so a second
// upload replaces a first rather than queueing beside it.
func existing(documents []domain.Document, kind domain.DocumentKind, vehicleID string) (domain.Document, bool) {
	for _, document := range documents {
		if document.Kind == kind && document.VehicleID == vehicleID {
			return document, true
		}
	}
	return domain.Document{}, false
}
