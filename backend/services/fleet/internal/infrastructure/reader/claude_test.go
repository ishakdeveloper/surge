package reader_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/fleet/internal/domain"
	"github.com/ishakdeveloper/surge/services/fleet/internal/infrastructure/reader"
	"github.com/ishakdeveloper/surge/services/fleet/internal/service"
)

// files stands in for the documents bucket: it serves one certificate, and
// hands out the link the reader fetches it with.
type files struct{ url string }

func (f files) Upload(context.Context, string, string, int64, time.Time) (service.Upload, error) {
	return service.Upload{}, nil
}
func (f files) View(string, time.Time) (string, error) { return f.url, nil }

// model answers as Anthropic does: content blocks, of which the text ones are
// the answer.
func model(t *testing.T, text string) (*httptest.Server, *http.Request, *[]byte) {
	t.Helper()
	var (
		seen http.Request
		body []byte
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = *r
		body, _ = json.Marshal(readBody(t, r))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]string{{"type": "text", "text": text}},
		})
	}))
	return server, &seen, &body
}

func readBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return decoded
}

func newReader(t *testing.T, endpoint, documents string) service.Reader {
	t.Helper()
	read, err := reader.New(reader.Options{
		Endpoint: endpoint, Key: "sk-ant-test", Store: files{url: documents},
	})
	if err != nil {
		t.Fatal(err)
	}
	return read
}

func TestReadsTheFieldsAReviewerWouldType(t *testing.T) {
	documents := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("a scanned certificate"))
	}))
	defer documents.Close()

	server, seen, body := model(t, `{"insurer":"Achmea","policyNumber":"NL-8842-11",
		"expiresAt":"2027-03-31","quotes":["geldig tot 31-03-2027"]}`)
	defer server.Close()

	extraction, err := newReader(t, server.URL, documents.URL).
		Read(context.Background(), "documents/drv-1/id-1.pdf", "application/pdf")
	if err != nil {
		t.Fatal(err)
	}

	if extraction.Status != domain.ExtractionDone {
		t.Fatalf("status %s", extraction.Status)
	}
	if extraction.Insurer != "Achmea" || extraction.PolicyNumber != "NL-8842-11" {
		t.Errorf("read %+v", extraction)
	}
	if want := time.Date(2027, 3, 31, 0, 0, 0, 0, time.UTC); !extraction.ExpiresAt.Equal(want) {
		t.Errorf("expiry %v, want %v", extraction.ExpiresAt, want)
	}
	// The quotes are what let a reviewer check the machine rather than trust it.
	if len(extraction.Quotes) != 1 {
		t.Errorf("quotes %v", extraction.Quotes)
	}

	if seen.Header.Get("X-Api-Key") == "" || seen.Header.Get("Anthropic-Version") == "" {
		t.Errorf("headers %v", seen.Header)
	}
	// A PDF travels as a document block, an image as an image block.
	if !containsType(*body, "document") {
		t.Errorf("a PDF was not sent as a document: %s", *body)
	}
}

func TestAnImageIsSentAsAnImage(t *testing.T) {
	documents := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("a photograph"))
	}))
	defer documents.Close()

	server, _, body := model(t, `{"insurer":"","policyNumber":"","expiresAt":"","quotes":[]}`)
	defer server.Close()

	if _, err := newReader(t, server.URL, documents.URL).
		Read(context.Background(), "documents/drv-1/id-2.jpg", "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if !containsType(*body, "image") {
		t.Errorf("a photograph was not sent as an image: %s", *body)
	}
}

// A model that answers with something other than the JSON it was asked for is
// a reading that failed, not an outage: the document still reaches a reviewer,
// who was going to read it anyway.
func TestAnUnreadableAnswerIsAFailedReading(t *testing.T) {
	documents := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("a scan"))
	}))
	defer documents.Close()

	server, _, _ := model(t, "I am afraid I cannot read this certificate.")
	defer server.Close()

	extraction, err := newReader(t, server.URL, documents.URL).
		Read(context.Background(), "documents/drv-1/id-3.pdf", "application/pdf")
	if err != nil {
		t.Fatalf("a failed reading became an error: %v", err)
	}
	if extraction.Status != domain.ExtractionFailed {
		t.Errorf("status %s", extraction.Status)
	}
}

// Models fence their JSON however firmly they are asked not to.
func TestFencedJSONIsStillRead(t *testing.T) {
	documents := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("a scan"))
	}))
	defer documents.Close()

	server, _, _ := model(t, "```json\n{\"insurer\":\"Allianz\",\"policyNumber\":\"\",\"expiresAt\":\"\",\"quotes\":[]}\n```")
	defer server.Close()

	extraction, err := newReader(t, server.URL, documents.URL).
		Read(context.Background(), "documents/drv-1/id-4.pdf", "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if extraction.Status != domain.ExtractionDone || extraction.Insurer != "Allianz" {
		t.Errorf("read %+v", extraction)
	}
}

func containsType(body []byte, kind string) bool {
	var decoded map[string]any
	if json.Unmarshal(body, &decoded) != nil {
		return false
	}
	messages, _ := decoded["messages"].([]any)
	for _, message := range messages {
		entry, _ := message.(map[string]any)
		content, _ := entry["content"].([]any)
		for _, block := range content {
			piece, _ := block.(map[string]any)
			if piece["type"] == kind {
				return true
			}
		}
	}
	return false
}
