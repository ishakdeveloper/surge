package http

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	paymentspb "github.com/ishakdeveloper/surge/shared/proto/payments"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type deliverer struct {
	got    *paymentspb.DeliverWebhookRequest
	answer error
}

func (d *deliverer) DeliverWebhook(_ context.Context, request *paymentspb.DeliverWebhookRequest, _ ...grpc.CallOption) (*paymentspb.DeliverWebhookResponse, error) {
	d.got = request
	return &paymentspb.DeliverWebhookResponse{}, d.answer
}

// The body reaches payments exactly as Stripe sent it, because the signature
// is over those bytes; and payments' answer becomes the status Stripe acts on.
func TestWebhooksAreForwardedUntouched(t *testing.T) {
	body := []byte("{\"id\":\"evt_1\",  \"type\":\"payment_intent.succeeded\"}\n")

	cases := []struct {
		answer error
		want   int
	}{
		{nil, http.StatusOK},
		{status.Error(codes.InvalidArgument, "bad signature"), http.StatusBadRequest},
		// Anything else is worth Stripe retrying.
		{status.Error(codes.Internal, "database down"), http.StatusInternalServerError},
		{status.Error(codes.Unavailable, "payments down"), http.StatusInternalServerError},
	}

	for _, c := range cases {
		payments := &deliverer{answer: c.answer}
		request := httptest.NewRequest(http.MethodPost, "/webhooks/stripe", bytes.NewReader(body))
		request.Header.Set("Stripe-Signature", "t=1,v1=abc")
		recorder := httptest.NewRecorder()

		StripeWebhooks(payments, paymentspb.WebhookKind_WEBHOOK_KIND_SNAPSHOT).ServeHTTP(recorder, request)

		if recorder.Code != c.want {
			t.Errorf("payments answered %v: status %d, want %d", c.answer, recorder.Code, c.want)
		}
		if !bytes.Equal(payments.got.GetPayload(), body) || payments.got.GetSignature() != "t=1,v1=abc" {
			t.Errorf("forwarded %q with %q; the body must arrive byte for byte", payments.got.GetPayload(), payments.got.GetSignature())
		}
	}
}
