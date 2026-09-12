package domain

import (
	"fmt"
	"time"
)

// DocumentKind is a paper a driver owes.
//
// The driving licence is not one of them: the identity check reads it from
// the driver's hand, and keeping a photograph of it as well would be a copy
// of the most sensitive thing here that nobody needs.
type DocumentKind string

const (
	// KindInsurance is the certificate for one car.
	KindInsurance DocumentKind = "insurance"
	// KindRegistration is the kentekenbewijs, for a car whose keeper is
	// somebody other than the driver.
	KindRegistration DocumentKind = "registration"
	// KindVOG is the declaration Justis issues in one to four weeks. There is
	// no API for it anywhere, by design.
	KindVOG DocumentKind = "vog"
	// KindChauffeurskaart is the smartcard Kiwa posts about four weeks after a
	// complete application. There is no API for it either.
	KindChauffeurskaart DocumentKind = "chauffeurskaart"
)

// DocumentStatus is where a document has got to.
type DocumentStatus string

const (
	// AwaitingFile is a link handed out and nothing put to it yet.
	AwaitingFile DocumentStatus = "awaiting_file"
	// AwaitingAuthority is a driver waiting on Justis or Kiwa, which takes
	// weeks and cannot be hurried by anything here.
	AwaitingAuthority DocumentStatus = "awaiting_authority"
	// Submitted is with a person to look at.
	Submitted DocumentStatus = "submitted"
	Approved  DocumentStatus = "approved"
	Rejected  DocumentStatus = "rejected"
	// Expired is approved once, and past its date.
	Expired DocumentStatus = "expired"
)

// ExtractionStatus is whether a machine has read the file yet.
type ExtractionStatus string

const (
	ExtractionNone    ExtractionStatus = "none"
	ExtractionPending ExtractionStatus = "pending"
	ExtractionDone    ExtractionStatus = "done"
	ExtractionFailed  ExtractionStatus = "failed"
)

// Extraction is what a machine read off a document, and where it read it.
//
// Advice to a reviewer, never a decision: the reviewer types the expiry they
// can see on the page, and this only saves them typing it. The quotes are the
// sentences it was taken from, so the reviewer can check the machine instead
// of trusting it.
type Extraction struct {
	Status       ExtractionStatus
	Insurer      string
	PolicyNumber string
	ExpiresAt    time.Time
	Quotes       []string
}

// Document is a paper a driver owes, whether or not a file has arrived.
type Document struct {
	ID       string
	DriverID string
	// VehicleID is set for the papers that belong to a car.
	VehicleID string
	Kind      DocumentKind
	Status    DocumentStatus

	ObjectKey   string
	ContentType string
	ByteSize    int64

	ExpiresAt time.Time
	Extracted Extraction

	ReviewerID string
	ReviewNote string
	ReviewedAt time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// MaxUploadBytes bounds an upload. A photograph of a certificate is a couple
// of megabytes; a scanned PDF can be larger, and this is generous for both
// without being a way to fill a bucket.
const MaxUploadBytes = 20 << 20

// UploadTypes are what a document may be, and the extension its key gets.
var UploadTypes = map[string]string{
	"image/jpeg":      "jpg",
	"image/png":       "png",
	"image/webp":      "webp",
	"application/pdf": "pdf",
}

// FromAuthority reports whether this kind is issued by somebody who answers in
// weeks and has no API.
func (k DocumentKind) FromAuthority() bool {
	return k == KindVOG || k == KindChauffeurskaart
}

// AboutAVehicle reports whether this kind belongs to a car rather than a
// person.
func (k DocumentKind) AboutAVehicle() bool {
	return k == KindInsurance || k == KindRegistration
}

// Expires reports whether a date has to be read off this kind when approving
// it. A registration document does not expire; everything else here does.
func (k DocumentKind) Expires() bool { return k != KindRegistration }

// KnownKind reports whether this is a document the fleet asks for.
func KnownKind(kind DocumentKind) bool {
	switch kind {
	case KindInsurance, KindRegistration, KindVOG, KindChauffeurskaart:
		return true
	default:
		return false
	}
}

// CheckUpload is whether a file may be put at all, before a link is handed out.
func CheckUpload(kind DocumentKind, contentType string, size int64) error {
	if !KnownKind(kind) {
		return &InvalidError{Reason: "that is not a document the fleet asks for"}
	}
	if _, ok := UploadTypes[contentType]; !ok {
		return &InvalidError{Reason: "a document is a JPEG, PNG, WebP or PDF"}
	}
	switch {
	case size <= 0:
		return &InvalidError{Reason: "a document cannot be empty"}
	case size > MaxUploadBytes:
		return &InvalidError{Reason: fmt.Sprintf("a document is at most %d MB", MaxUploadBytes>>20)}
	}
	return nil
}

// ValidAt reports whether the document counts today: approved, and either
// undated or not yet past its date.
func (d Document) ValidAt(now time.Time) bool {
	if d.Status != Approved {
		return false
	}
	return d.ExpiresAt.IsZero() || now.Before(d.ExpiresAt)
}

// Decision is a reviewer's answer about one document.
type Decision struct {
	Approve   bool
	ExpiresAt time.Time
	Note      string
	Reviewer  string
}

// CheckDecision is whether a reviewer may decide this document now, and
// whether they have said enough to.
func CheckDecision(document Document, decision Decision, now time.Time) error {
	switch document.Status {
	case Submitted, Approved, Expired, Rejected:
	default:
		return &InvalidError{Reason: "there is nothing to look at on that document yet"}
	}

	if !decision.Approve {
		if decision.Note == "" {
			return &InvalidError{Reason: "say why, so the driver knows what to send instead"}
		}
		return nil
	}
	if document.Kind.Expires() {
		switch {
		case decision.ExpiresAt.IsZero():
			return &InvalidError{Reason: "read the expiry date off the document"}
		case !now.Before(decision.ExpiresAt):
			return &InvalidError{Reason: "that document has already expired"}
		}
	}
	return nil
}

// Decided is the document as the reviewer left it.
func Decided(document Document, decision Decision, now time.Time) Document {
	document.Status = Rejected
	if decision.Approve {
		document.Status = Approved
		document.ExpiresAt = decision.ExpiresAt
	}
	document.ReviewNote = decision.Note
	document.ReviewerID = decision.Reviewer
	document.ReviewedAt = now
	document.UpdatedAt = now
	return document
}
