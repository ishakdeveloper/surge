// Package reader reads what it can off a document, so a reviewer types less.
//
// Insurance certificates are the case that needs it: every insurer lays one
// out differently, and no document-understanding service anywhere ships a
// prebuilt model for them — the identity provider already reads the licence,
// and the vehicle register already knows the car. So this is a model with
// vision, asked for four fields and the sentences it took them from.
//
// It is advice and never a decision. What it reads is shown beside the
// document for a reviewer to agree with, and a reading that fails changes
// nothing: the reviewer reads the document themselves, which is what they were
// going to do anyway.
package reader

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ishakdeveloper/surge/services/fleet/internal/domain"
	"github.com/ishakdeveloper/surge/services/fleet/internal/service"
)

// Endpoint is Anthropic's messages API.
const Endpoint = "https://api.anthropic.com/v1/messages"

// Version is the API version header Anthropic requires.
const Version = "2023-06-01"

// DefaultModel is the cheapest model that reads a scanned certificate
// reliably. Extraction is a bulk, low-judgement job; the money belongs in the
// reviewer's time, not here.
const DefaultModel = "claude-haiku-4-5"

// maxDocumentBytes bounds what is sent to the model.
const maxDocumentBytes = 12 << 20

const instruction = `Read this vehicle insurance certificate and reply with only a JSON object, no prose and no code fence, with these keys:
"insurer": the insurance company's name, or "" if you cannot see one.
"policyNumber": the policy or certificate number, or "".
"expiresAt": the date cover ends, as YYYY-MM-DD, or "" if it is not stated.
"quotes": an array of at most three short quotes, copied exactly from the document, showing where you read those values.
Dutch certificates often say "geldig tot", "vervaldatum" or "einddatum" for the end of cover. If a field is not legible, use "" rather than guessing.`

type Claude struct {
	http     *http.Client
	endpoint string
	key      string
	model    string
	store    service.Store
	now      func() time.Time
}

type Options struct {
	Endpoint string
	Key      string
	Model    string
	// Store is where the file is, and how to get a link to it.
	Store service.Store
	Now   func() time.Time
}

func New(options Options) (*Claude, error) {
	if options.Key == "" || options.Store == nil {
		return nil, fmt.Errorf("reader: an API key and a store are required")
	}
	claude := &Claude{
		http:     &http.Client{Timeout: 60 * time.Second},
		endpoint: options.Endpoint, key: options.Key, model: options.Model,
		store: options.Store, now: options.Now,
	}
	if claude.endpoint == "" {
		claude.endpoint = Endpoint
	}
	if claude.model == "" {
		claude.model = DefaultModel
	}
	if claude.now == nil {
		claude.now = time.Now
	}
	return claude, nil
}

// Read fetches the document and asks the model what is on it.
func (c *Claude) Read(ctx context.Context, key, contentType string) (domain.Extraction, error) {
	document, err := c.fetch(ctx, key)
	if err != nil {
		return domain.Extraction{}, err
	}

	answer, err := c.ask(ctx, document, contentType)
	if err != nil {
		return domain.Extraction{}, err
	}
	return extractionOf(answer), nil
}

// fetch reads the file back out of storage, through the same presigned link a
// reviewer would open.
func (c *Claude) fetch(ctx context.Context, key string) ([]byte, error) {
	link, err := c.store.View(key, c.now())
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, fmt.Errorf("reader: request: %w", err)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("reader: fetch document: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reader: fetch document: status %d", response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, maxDocumentBytes))
}

// block is one piece of content in a message: an image, a PDF, or text.
type block struct {
	Type   string  `json:"type"`
	Source *source `json:"source,omitempty"`
	Text   string  `json:"text,omitempty"`
}

type source struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type request struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string  `json:"role"`
	Content []block `json:"content"`
}

type reply struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// ask sends the document and the instruction, the document first: a model
// reads an instruction about a page better when it has already seen the page.
func (c *Claude) ask(ctx context.Context, document []byte, contentType string) (string, error) {
	kind := "image"
	if contentType == "application/pdf" {
		kind = "document"
	}
	body, err := json.Marshal(request{
		Model:     c.model,
		MaxTokens: 1024,
		Messages: []message{{
			Role: "user",
			Content: []block{
				{Type: kind, Source: &source{
					Type:      "base64",
					MediaType: contentType,
					Data:      base64.StdEncoding.EncodeToString(document),
				}},
				{Type: "text", Text: instruction},
			},
		}},
	})
	if err != nil {
		return "", fmt.Errorf("reader: encode: %w", err)
	}

	post, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("reader: request: %w", err)
	}
	post.Header.Set("Content-Type", "application/json")
	post.Header.Set("X-Api-Key", c.key)
	post.Header.Set("Anthropic-Version", Version)

	response, err := c.http.Do(post)
	if err != nil {
		return "", fmt.Errorf("reader: ask: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("reader: read answer: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("reader: status %d: %s", response.StatusCode, bytes.TrimSpace(raw))
	}

	var decoded reply
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", fmt.Errorf("reader: decode answer: %w", err)
	}
	if decoded.Error != nil {
		return "", fmt.Errorf("reader: %s: %s", decoded.Error.Type, decoded.Error.Message)
	}

	var text strings.Builder
	for _, part := range decoded.Content {
		if part.Type == "text" {
			text.WriteString(part.Text)
		}
	}
	return text.String(), nil
}

// fields is the shape the model was asked for.
type fields struct {
	Insurer      string   `json:"insurer"`
	PolicyNumber string   `json:"policyNumber"`
	ExpiresAt    string   `json:"expiresAt"`
	Quotes       []string `json:"quotes"`
}

// extractionOf turns the model's answer into what a reviewer is shown.
//
// An answer that is not the JSON it was asked for is a failed reading rather
// than an error: the document still reaches the reviewer, who reads it.
func extractionOf(answer string) domain.Extraction {
	text := strings.TrimSpace(answer)
	// Models fence JSON even when asked not to.
	if start := strings.Index(text, "{"); start >= 0 {
		if end := strings.LastIndex(text, "}"); end > start {
			text = text[start : end+1]
		}
	}

	var read fields
	if err := json.Unmarshal([]byte(text), &read); err != nil {
		return domain.Extraction{Status: domain.ExtractionFailed}
	}

	extraction := domain.Extraction{
		Status:       domain.ExtractionDone,
		Insurer:      strings.TrimSpace(read.Insurer),
		PolicyNumber: strings.TrimSpace(read.PolicyNumber),
		Quotes:       read.Quotes,
	}
	if expiry, err := time.ParseInLocation(time.DateOnly, strings.TrimSpace(read.ExpiresAt), time.UTC); err == nil {
		extraction.ExpiresAt = expiry
	}
	return extraction
}
