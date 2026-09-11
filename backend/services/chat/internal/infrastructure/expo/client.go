// Package expo sends device notifications through Expo's push service, which
// delivers to APNs and FCM for the app — so chat holds one credential, not an
// Apple key and a Google one.
package expo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/ishakdeveloper/surge/services/chat/internal/service"
)

// Endpoint is Expo's push API.
const Endpoint = "https://exp.host/--/api/v2/push/send"

// batch is the most notifications Expo takes in one request.
const batch = 100

type Client struct {
	http        *http.Client
	endpoint    string
	accessToken string
}

// New sends to endpoint. The access token is optional: Expo requires one only
// once the project turns on enhanced push security.
func New(endpoint, accessToken string) *Client {
	return &Client{
		http:     &http.Client{Timeout: 10 * time.Second},
		endpoint: endpoint, accessToken: accessToken,
	}
}

type message struct {
	To       string            `json:"to"`
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Data     map[string]string `json:"data,omitempty"`
	Sound    string            `json:"sound"`
	Priority string            `json:"priority"`
}

type response struct {
	Data []struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Details struct {
			Error string `json:"error"`
		} `json:"details"`
	} `json:"data"`
	Errors []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

// Send delivers in batches of a hundred.
//
// Expo answers each notification with a ticket, in order. DeviceNotRegistered
// is the one error acted on — the app was uninstalled, and the token will
// never work again. Any other ticket error is one notification lost, which is
// logged rather than retried: resending the rest of the batch to reach it
// would notify everyone else twice.
func (c *Client) Send(ctx context.Context, notifications []service.Notification) ([]string, error) {
	var unregistered []string
	for start := 0; start < len(notifications); start += batch {
		chunk := notifications[start:min(start+batch, len(notifications))]
		gone, err := c.send(ctx, chunk)
		if err != nil {
			return unregistered, err
		}
		unregistered = append(unregistered, gone...)
	}
	return unregistered, nil
}

func (c *Client) send(ctx context.Context, notifications []service.Notification) ([]string, error) {
	messages := make([]message, len(notifications))
	for i, notification := range notifications {
		messages[i] = message{
			To: notification.To, Title: notification.Title, Body: notification.Body,
			Data: notification.Data, Sound: "default", Priority: "high",
		}
	}
	body, err := json.Marshal(messages)
	if err != nil {
		return nil, fmt.Errorf("expo: encode: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("expo: request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if c.accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+c.accessToken)
	}

	reply, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("expo: send: %w", err)
	}
	defer func() { _ = reply.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(reply.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("expo: read: %w", err)
	}
	if reply.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("expo: status %d: %s", reply.StatusCode, bytes.TrimSpace(raw))
	}

	var decoded response
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("expo: decode: %w", err)
	}
	if len(decoded.Errors) > 0 {
		return nil, fmt.Errorf("expo: %s: %s", decoded.Errors[0].Code, decoded.Errors[0].Message)
	}

	var unregistered []string
	for i, ticket := range decoded.Data {
		if ticket.Status != "error" || i >= len(notifications) {
			continue
		}
		if ticket.Details.Error == "DeviceNotRegistered" {
			unregistered = append(unregistered, notifications[i].To)
			continue
		}
		slog.Warn("expo refused a notification", "error", ticket.Details.Error, "message", ticket.Message)
	}
	return unregistered, nil
}
