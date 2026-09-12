// Package storage is where driver documents live: an S3-compatible bucket,
// reached with presigned links so a scan never travels through the API.
//
// The same shape as the avatar store the core service uses — the AWS v4 signer
// over net/http rather than the S3 SDK, because signing two kinds of request
// is all that is needed and the SDK is thirty megabytes of everything else.
// What differs is the direction: avatars are put by the server, documents are
// put by the phone, so this presigns the upload as well as the read.
package storage

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/ishakdeveloper/surge/services/fleet/internal/service"
)

const (
	region   = "auto"
	service_ = "s3"
	// unsignedPayload is what a presigned link signs instead of the body: the
	// bytes are not known when the link is made, which is the whole point.
	unsignedPayload = "UNSIGNED-PAYLOAD"
	// uploadWindow is how long a driver has to put the file. Long enough for a
	// slow phone on a train, short enough that a link in a log is stale.
	uploadWindow = 15 * time.Minute
	// viewWindow is how long a reviewer's link to a document lasts.
	viewWindow = 30 * time.Minute
	// viewGrain is how coarsely a view link's signing time is rounded, so a
	// reviewer refreshing the page gets the same URL rather than a new one
	// their browser must fetch again.
	viewGrain = 5 * time.Minute
)

type R2 struct {
	endpoint    string
	bucket      string
	credentials aws.Credentials
	signer      *v4.Signer
}

type Options struct {
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
}

func NewR2(options Options) (*R2, error) {
	if options.Endpoint == "" || options.Bucket == "" || options.AccessKeyID == "" || options.SecretAccessKey == "" {
		return nil, fmt.Errorf("storage: an endpoint, a bucket and credentials are required")
	}
	return &R2{
		endpoint: strings.TrimRight(options.Endpoint, "/"),
		bucket:   options.Bucket,
		credentials: aws.Credentials{
			AccessKeyID:     options.AccessKeyID,
			SecretAccessKey: options.SecretAccessKey,
		},
		// Path escaping off, as S3 signing requires for path-style addressing.
		signer: v4.NewSigner(func(signer *v4.SignerOptions) { signer.DisableURIPathEscaping = true }),
	}, nil
}

// objectURL addresses one object, path-style: the endpoint, the bucket, then
// the key with each segment escaped on its own so slashes survive.
func (r *R2) objectURL(key string) string {
	segments := strings.Split(key, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return r.endpoint + "/" + url.PathEscape(r.bucket) + "/" + strings.Join(segments, "/")
}

// Upload is a link to PUT one file to, with the type and length it must have.
//
// Content-Type and Content-Length are signed, so the link is good for the file
// that was described and not for anything else: a driver cannot be handed a
// link for a 2 MB certificate and put a gigabyte of something else at it.
func (r *R2) Upload(ctx context.Context, key, contentType string, size int64, now time.Time) (service.Upload, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, r.objectURL(key), nil)
	if err != nil {
		return service.Upload{}, fmt.Errorf("storage: upload request: %w", err)
	}
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Content-Length", strconv.FormatInt(size, 10))
	request.ContentLength = size

	expire(request, uploadWindow)

	signed, _, err := r.signer.PresignHTTP(ctx, r.credentials, request,
		unsignedPayload, service_, region, now.UTC(),
		func(options *v4.SignerOptions) { options.DisableURIPathEscaping = true },
	)
	if err != nil {
		return service.Upload{}, fmt.Errorf("storage: presign upload: %w", err)
	}
	return service.Upload{URL: signed, ExpiresAt: now.Add(uploadWindow)}, nil
}

// View is a link a reviewer can open.
//
// The signing time is rounded down, so every reviewer who opens the same
// document within the same few minutes gets the same URL — one their browser
// can cache, and one that appears once in a log rather than once per refresh.
func (r *R2) View(key string, now time.Time) (string, error) {
	request, err := http.NewRequest(http.MethodGet, r.objectURL(key), nil)
	if err != nil {
		return "", fmt.Errorf("storage: view request: %w", err)
	}

	// The window covers the rounding as well, so a link made at the end of one
	// grain still lasts a reviewer's whole read.
	expire(request, viewWindow+viewGrain)

	signed, _, err := r.signer.PresignHTTP(context.Background(), r.credentials, request,
		unsignedPayload, service_, region, now.UTC().Truncate(viewGrain),
		func(options *v4.SignerOptions) { options.DisableURIPathEscaping = true },
	)
	if err != nil {
		return "", fmt.Errorf("storage: presign view: %w", err)
	}
	return signed, nil
}

// expire says how long a presigned link lasts.
//
// S3 carries this as a query parameter rather than a signer option, and the
// signer signs whatever is on the URL — so it has to be set before signing,
// not after.
func expire(request *http.Request, window time.Duration) {
	query := request.URL.Query()
	query.Set("X-Amz-Expires", strconv.FormatInt(int64(window.Seconds()), 10))
	request.URL.RawQuery = query.Encode()
}
