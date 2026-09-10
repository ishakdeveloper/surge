// Package wirestatus is the one table between the domain's state names and the
// wire enum.
//
// Two things need it — the gRPC handler returning a trip, and the notifier
// pushing a status change to a rider — and two copies of a mapping is how a
// state gets spelled one way in a response and another in a push. An explicit
// table rather than a string cast, so renaming a domain constant is a compile
// error here instead of a silently UNSPECIFIED status on the wire.
package wirestatus

import (
	"github.com/ishakdeveloper/surge/services/trip/internal/domain"
	trippb "github.com/ishakdeveloper/surge/shared/proto/trip"
)

var statuses = map[domain.Status]trippb.TripStatus{
	domain.StatusRequested:  trippb.TripStatus_TRIP_STATUS_REQUESTED,
	domain.StatusOffered:    trippb.TripStatus_TRIP_STATUS_OFFERED,
	domain.StatusAccepted:   trippb.TripStatus_TRIP_STATUS_ACCEPTED,
	domain.StatusArrived:    trippb.TripStatus_TRIP_STATUS_ARRIVED,
	domain.StatusInProgress: trippb.TripStatus_TRIP_STATUS_IN_PROGRESS,
	domain.StatusCompleted:  trippb.TripStatus_TRIP_STATUS_COMPLETED,
	domain.StatusCancelled:  trippb.TripStatus_TRIP_STATUS_CANCELLED,
	domain.StatusUnmatched:  trippb.TripStatus_TRIP_STATUS_UNMATCHED,
}

// Of is the wire enum for a domain status.
func Of(status domain.Status) trippb.TripStatus { return statuses[status] }
