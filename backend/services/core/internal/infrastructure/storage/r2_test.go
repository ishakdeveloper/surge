package storage_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/core/internal/infrastructure/storage"
)

type seen struct {
	method, path, auth, hash, contentType string
	body                                  []byte
}

func r2(t *testing.T, status int) (*storage.R2, *seen) {
	t.Helper()
	var got seen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = seen{
			method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"),
			hash: r.Header.Get("X-Amz-Content-Sha256"), contentType: r.Header.Get("Content-Type"), body: body,
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	store, err := storage.NewR2(storage.R2Options{
		Endpoint: server.URL, Bucket: "surge", AccessKeyID: "key-id", SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatalf("r2: %v", err)
	}
	return store, &got
}

// A photo is sent to the bucket's path, signed the way S3 checks — the body's
// hash in the signed headers — with the type it is.
func TestAPhotoIsPutSignedForS3(t *testing.T) {
	store, got := r2(t, http.StatusOK)
	if err := store.Put(context.Background(), "avatars/u1/abc.jpg", []byte("jpeg"), "image/jpeg"); err != nil {
		t.Fatalf("put: %v", err)
	}
	sum := sha256.Sum256([]byte("jpeg"))
	if got.method != http.MethodPut || got.path != "/surge/avatars/u1/abc.jpg" || string(got.body) != "jpeg" {
		t.Errorf("sent %s %s %q", got.method, got.path, got.body)
	}
	if !strings.HasPrefix(got.auth, "AWS4-HMAC-SHA256 Credential=key-id/") || !strings.Contains(got.auth, "/auto/s3/aws4_request") {
		t.Errorf("authorization = %q", got.auth)
	}
	if got.hash != hex.EncodeToString(sum[:]) || got.contentType != "image/jpeg" {
		t.Errorf("hash %q, type %q", got.hash, got.contentType)
	}
}

func TestARefusedPutIsAnError(t *testing.T) {
	store, _ := r2(t, http.StatusForbidden)
	if err := store.Put(context.Background(), "avatars/u1/abc.jpg", []byte("jpeg"), "image/jpeg"); err == nil {
		t.Error("a 403 was taken as stored")
	}
}

// Deleting a photo that is already gone is what deleting wanted.
func TestDeletingAMissingPhotoIsFine(t *testing.T) {
	store, got := r2(t, http.StatusNotFound)
	if err := store.Delete(context.Background(), "avatars/u1/abc.jpg"); err != nil {
		t.Errorf("delete: %v", err)
	}
	if got.method != http.MethodDelete {
		t.Errorf("sent %s", got.method)
	}
}

// Everyone asking within an hour gets the same link, which a browser caches;
// it outlives the hour; and the next hour's link is a different one.
func TestALinkIsTheSameWithinTheHour(t *testing.T) {
	store, _ := r2(t, http.StatusOK)
	at := func(minute int) string {
		link, err := store.URL("avatars/u1/abc.jpg", time.Date(2026, 9, 11, 12, minute, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("url: %v", err)
		}
		return link
	}

	early, late := at(5), at(55)
	if early != late {
		t.Errorf("two links in one hour:\n%s\n%s", early, late)
	}
	next, err := store.URL("avatars/u1/abc.jpg", time.Date(2026, 9, 11, 13, 1, 0, 0, time.UTC))
	if err != nil || next == early {
		t.Errorf("the next hour's link is the same: %v", err)
	}

	link, err := url.Parse(early)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	query := link.Query()
	if link.Path != "/surge/avatars/u1/abc.jpg" || query.Get("X-Amz-Expires") != "7200" ||
		query.Get("X-Amz-Date") != "20260911T120000Z" || query.Get("X-Amz-Signature") == "" {
		t.Errorf("link = %s", early)
	}
}
