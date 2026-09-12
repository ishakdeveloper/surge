// Package identity verifies a driver against their licence, through Stripe
// Identity.
//
// Stripe already knows this platform — payments holds the account that moves
// money — but verifying a driver is not moving money, and the key here is a
// restricted one that can do nothing else. That is the whole reason this lives
// in the fleet rather than in payments: a service that can open a verification
// session should not also be able to issue a refund.
//
// Stripe Connect does collect identity documents of its own, when Stripe's
// risk rules ask for them, to satisfy Stripe's obligations before it pays a
// driver out. That is not the same question as "may this person drive for us
// tonight", it fires on Stripe's schedule rather than at onboarding, and
// forcing it early is invite-only. So the two exist side by side: Connect
// satisfies Stripe, and this satisfies us.
package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ishakdeveloper/surge/services/fleet/internal/domain"
	"github.com/ishakdeveloper/surge/services/fleet/internal/service"
	stripego "github.com/stripe/stripe-go/v86"
)

// Stripe opens verification sessions and reads back what they decided.
type Stripe struct {
	client *stripego.Client
	secret string
}

// New builds the provider. The key should be a restricted key with Identity
// write and nothing else; a secret key works and can do far more than this
// needs.
func New(key, webhookSecret string) *Stripe {
	return &Stripe{client: stripego.NewClient(key), secret: webhookSecret}
}

// Start opens a session and returns the page the driver finishes it on.
//
// A document check with a selfie, and live capture on: a photograph of a
// photograph of a licence is exactly what this is meant to catch. The driver
// id travels as the client reference so a support conversation about a failed
// check can find it in Stripe's dashboard.
func (s *Stripe) Start(ctx context.Context, driverID, returnURL string) (service.Session, error) {
	params := &stripego.IdentityVerificationSessionCreateParams{
		Type:              stripego.String("document"),
		ClientReferenceID: stripego.String(driverID),
		Metadata:          map[string]string{"driver_id": driverID},
		Options: &stripego.IdentityVerificationSessionCreateOptionsParams{
			Document: &stripego.IdentityVerificationSessionCreateOptionsDocumentParams{
				AllowedTypes:          []*string{stripego.String("driving_license")},
				RequireMatchingSelfie: stripego.Bool(true),
				RequireLiveCapture:    stripego.Bool(true),
			},
		},
	}
	if returnURL != "" {
		params.ReturnURL = stripego.String(returnURL)
	}

	session, err := s.client.V1IdentityVerificationSessions.Create(ctx, params)
	if err != nil {
		return service.Session{}, fmt.Errorf("identity: create session: %w", err)
	}
	return service.Session{ID: session.ID, URL: session.URL}, nil
}

// Receive verifies a webhook and says what it means for a driver.
//
// The second return says whether it was ours at all: Stripe sends this
// endpoint whatever the account is subscribed to, and an event about a payment
// is not a failure here, it is somebody else's news.
func (s *Stripe) Receive(ctx context.Context, payload []byte, signature string) (service.Verdict, bool, error) {
	// The API version is not checked against the SDK's, as payments does not
	// check it either: Stripe signs events in the account's version and the
	// handful of fields read here are the same in all of them.
	event, err := stripego.ConstructEvent(payload, signature, s.secret, stripego.WithIgnoreAPIVersionMismatch())
	if err != nil {
		return service.Verdict{}, false, fmt.Errorf("%w: %v", service.ErrInvalidWebhook, err)
	}
	if !strings.HasPrefix(string(event.Type), "identity.verification_session.") {
		return service.Verdict{}, false, nil
	}

	var session stripego.IdentityVerificationSession
	if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
		return service.Verdict{}, false, fmt.Errorf("identity: decode session: %w", err)
	}

	verdict := service.Verdict{SessionID: session.ID}
	switch event.Type {
	case "identity.verification_session.verified":
		return s.verified(ctx, session)
	case "identity.verification_session.processing":
		verdict.Status = domain.IdentityProcessing
	case "identity.verification_session.requires_input":
		verdict.Status = domain.IdentityFailed
		verdict.Reason = refusal(session)
	case "identity.verification_session.canceled":
		verdict.Status = domain.IdentityUnstarted
	default:
		return service.Verdict{}, false, nil
	}
	return verdict, true, nil
}

// verified reads the name and the licence's expiry off the report.
//
// The webhook carries the session, whose last verification report is an id
// rather than the report; the document's expiry lives on the report, so it is
// asked for. Without the expiry a licence that runs out next month would look
// like one that never does.
func (s *Stripe) verified(ctx context.Context, session stripego.IdentityVerificationSession) (service.Verdict, bool, error) {
	verdict := service.Verdict{SessionID: session.ID, Status: domain.IdentityVerified}

	retrieved, err := s.client.V1IdentityVerificationSessions.Retrieve(ctx, session.ID,
		&stripego.IdentityVerificationSessionRetrieveParams{
			Params: stripego.Params{Expand: []*string{stripego.String("last_verification_report")}},
		})
	if err != nil {
		return service.Verdict{}, false, fmt.Errorf("identity: retrieve session: %w", err)
	}

	if outputs := retrieved.VerifiedOutputs; outputs != nil {
		verdict.Name = strings.TrimSpace(outputs.FirstName + " " + outputs.LastName)
	}
	report := retrieved.LastVerificationReport
	if report != nil && report.Document != nil {
		verdict.Document = report.Document.Number
		if verdict.Name == "" {
			verdict.Name = strings.TrimSpace(report.Document.FirstName + " " + report.Document.LastName)
		}
		if expiry := report.Document.ExpirationDate; expiry != nil && expiry.Year > 0 {
			verdict.LicenceExpiresAt = time.Date(
				int(expiry.Year), time.Month(expiry.Month), int(expiry.Day),
				0, 0, 0, 0, time.UTC,
			)
		}
	}
	return verdict, true, nil
}

// refusal is why a check came back needing input, in words a driver can act
// on. Stripe's reason is written for exactly that, so it is passed through
// rather than reworded.
func refusal(session stripego.IdentityVerificationSession) string {
	if session.LastError == nil {
		return "The check could not be completed. Try again."
	}
	if reason := strings.TrimSpace(session.LastError.Reason); reason != "" {
		return reason
	}
	return string(session.LastError.Code)
}
