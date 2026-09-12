package http

import (
	"context"
	"io"
	"net/http"
	"time"

	fleetpb "github.com/ishakdeveloper/surge/shared/proto/fleet"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// IdentityDeliverer is the fleet call an identity webhook is forwarded through.
type IdentityDeliverer interface {
	DeliverWebhook(ctx context.Context, request *fleetpb.DeliverWebhookRequest, options ...grpc.CallOption) (*fleetpb.DeliverWebhookResponse, error)
}

// IdentityWebhooks forwards an identity provider's webhooks to the fleet, byte
// for byte.
//
// Its own endpoint rather than the payments one, because the two are signed
// with different secrets held by different services: payments has the key that
// moves money, the fleet has a restricted key that can only verify people. A
// gateway that forwarded both to one service would have to hold a secret
// itself, which is exactly what neither of them wants.
func IdentityWebhooks(fleet IdentityDeliverer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
		if err != nil {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		_, err = fleet.DeliverWebhook(ctx, &fleetpb.DeliverWebhookRequest{
			Payload:   body,
			Signature: r.Header.Get("Stripe-Signature"),
		})
		switch status.Code(err) {
		case codes.OK:
			w.WriteHeader(http.StatusOK)
		case codes.InvalidArgument:
			http.Error(w, "invalid signature", http.StatusBadRequest)
		default:
			http.Error(w, "not applied", http.StatusInternalServerError)
		}
	})
}
