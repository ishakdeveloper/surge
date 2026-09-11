package domain

import "time"

// Platform is the operating system a device notification goes to.
type Platform string

const (
	PlatformIOS     Platform = "ios"
	PlatformAndroid Platform = "android"
)

// PushToken is a device that can be notified, signed in as UserID.
type PushToken struct {
	Token     string
	UserID    string
	Platform  Platform
	UpdatedAt time.Time
}

// PushJob is a notification owed to a recipient about the message at Seq,
// written in the same transaction as the message.
type PushJob struct {
	ID             int64
	RecipientID    string
	ConversationID string
	Seq            int64
	// Attempts is how many sends of it have failed.
	Attempts int
}
