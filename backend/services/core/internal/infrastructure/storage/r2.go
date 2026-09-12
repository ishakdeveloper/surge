// Package storage holds photos: in Cloudflare R2, and in memory for a laptop
// without R2's keys.
package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// R2 is Cloudflare's object store, over its S3-compatible API.
//
// Requests are signed with AWS Signature Version 4 directly rather than
// through the S3 client: three operations on one bucket need the signer and
// net/http, not an SDK's worth of middleware.
//
// The bucket stays private. A photo is read through a presigned link, and the
// link is signed as of the start of the hour and good for two: everyone asking
// within the hour is handed the same link, which a browser can cache, and it
// still works an hour later.
type R2 struct {
	endpoint    string
	bucket      string
	credentials aws.Credentials
	signer      *v4.Signer
	client      *http.Client
}

type R2Options struct {
	// Endpoint is the account's S3 API: https://<account id>.r2.cloudflarestorage.com.
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
}

const (
	// region is what R2 answers to; it has no regions of its own.
	region  = "auto"
	service = "s3"

	linkWindow   = time.Hour
	linkLifetime = 2 * time.Hour
)

func NewR2(options R2Options) (*R2, error) {
	if options.Endpoint == "" || options.Bucket == "" || options.AccessKeyID == "" || options.SecretAccessKey == "" {
		return nil, errors.New("storage: R2 needs an endpoint, a bucket and both keys")
	}
	return &R2{
		endpoint: strings.TrimRight(options.Endpoint, "/"),
		bucket:   options.Bucket,
		credentials: aws.Credentials{
			AccessKeyID:     options.AccessKeyID,
			SecretAccessKey: options.SecretAccessKey,
		},
		signer: v4.NewSigner(func(o *v4.SignerOptions) {
			// S3 signs the path as sent, not escaped a second time.
			o.DisableURIPathEscaping = true
		}),
		client: &http.Client{Timeout: 20 * time.Second},
	}, nil
}

func (r *R2) objectURL(key string) string {
	segments := strings.Split(key, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return r.endpoint + "/" + url.PathEscape(r.bucket) + "/" + strings.Join(segments, "/")
}

func (r *R2) Put(ctx context.Context, key string, body []byte, contentType string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, r.objectURL(key), bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.ContentLength = int64(len(body))
	request.Header.Set("Content-Type", contentType)
	// Content-addressed: the object behind a key never changes.
	request.Header.Set("Cache-Control", "private, max-age=31536000, immutable")
	return r.send(request, sha256.Sum256(body), http.StatusOK)
}

func (r *R2) Delete(ctx context.Context, key string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, r.objectURL(key), nil)
	if err != nil {
		return err
	}
	// Gone already is what a delete wanted.
	return r.send(request, sha256.Sum256(nil), http.StatusNoContent, http.StatusOK, http.StatusNotFound)
}

func (r *R2) send(request *http.Request, payload [32]byte, accepted ...int) error {
	hash := hex.EncodeToString(payload[:])
	request.Header.Set("X-Amz-Content-Sha256", hash)
	if err := r.signer.SignHTTP(request.Context(), r.credentials, request, hash, service, region, time.Now()); err != nil {
		return fmt.Errorf("storage: sign %s: %w", request.Method, err)
	}
	response, err := r.client.Do(request)
	if err != nil {
		return fmt.Errorf("storage: %s %s: %w", request.Method, request.URL.Path, err)
	}
	defer response.Body.Close()
	for _, code := range accepted {
		if response.StatusCode == code {
			return nil
		}
	}
	detail, _ := io.ReadAll(io.LimitReader(response.Body, 512))
	return fmt.Errorf("storage: %s %s answered %d: %s", request.Method, request.URL.Path, response.StatusCode, detail)
}

func (r *R2) URL(key string, now time.Time) (string, error) {
	request, err := http.NewRequest(http.MethodGet, r.objectURL(key), nil)
	if err != nil {
		return "", err
	}
	query := request.URL.Query()
	query.Set("X-Amz-Expires", strconv.Itoa(int(linkLifetime.Seconds())))
	request.URL.RawQuery = query.Encode()

	signed, _, err := r.signer.PresignHTTP(context.Background(), r.credentials, request,
		"UNSIGNED-PAYLOAD", service, region, now.UTC().Truncate(linkWindow))
	if err != nil {
		return "", fmt.Errorf("storage: presign %s: %w", key, err)
	}
	return signed, nil
}
