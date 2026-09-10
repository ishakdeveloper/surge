package http

import (
	"context"
	"io"
	"net/http"
	"time"

	paymentspb "github.com/ishakdeveloper/surge/shared/proto/payments"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// maxWebhookBytes bounds a webhook body. Stripe's are a few kilobytes; a
// megabyte is generous, and a bound is what keeps an unauthenticated route from
// being a way to make the gateway buffer anything anyone sends.
const maxWebhookBytes = 1 << 20

// WebhookDeliverer is the payments call a webhook is forwarded through.
type WebhookDeliverer interface {
	DeliverWebhook(ctx context.Context, request *paymentspb.DeliverWebhookRequest, options ...grpc.CallOption) (*paymentspb.DeliverWebhookResponse, error)
}

// StripeWebhooks forwards Stripe's webhooks to payments, byte for byte.
//
// Outside /v1 and its token check, because Stripe has no token to send. What
// authenticates a webhook is its signature, and the gateway deliberately
// cannot check that: the secret lives only in payments, with the key. So the
// body is forwarded untouched — the signature is over its exact bytes — and
// the answer is translated into the status Stripe acts on: 2xx is done, 4xx is
// refused and not retried, 5xx is retried.
func StripeWebhooks(payments WebhookDeliverer, kind paymentspb.WebhookKind) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
		if err != nil {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		_, err = payments.DeliverWebhook(ctx, &paymentspb.DeliverWebhookRequest{
			Kind:      kind,
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
