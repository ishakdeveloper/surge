// Package routing is the Valhalla client: real road geometry, and the
// time/distance matrix that batched assignment needs.
//
// Valhalla rather than OSRM because osrm-backend publishes amd64 images only,
// and emulating one on Apple silicon makes tile preprocessing painful. Valhalla
// is native arm64, and `/sources_to_targets` gives the matrix that OSRM's
// `/table` would otherwise provide.
package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ishakdeveloper/surge/pkg/geo"
)

// Client talks to a Valhalla instance.
type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				// The simulator asks for thousands of routes on startup, all at
				// once. The default of 2 idle connections per host turns that
				// into connection churn against a service on localhost.
				MaxIdleConns:        200,
				MaxIdleConnsPerHost: 200,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// Route is a driveable path between two points.
type Route struct {
	Path     *geo.Path
	Duration time.Duration
	Meters   float64
	// Encoded is the shape as Valhalla emitted it, precision 6.
	//
	// Kept alongside the decoded path because the two have different consumers:
	// the simulator walks the path, while anything that sends a route to a
	// browser wants the encoded form — a city route is hundreds of points, and
	// encoded it is roughly a tenth the bytes on a connection a phone pays for.
	Encoded string
}

type location struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

func at(p geo.Point) location { return location{Lat: p.Lat, Lon: p.Lng} }

type routeResponse struct {
	Trip struct {
		Legs []struct {
			Shape string `json:"shape"`
		} `json:"legs"`
		Summary struct {
			Length float64 `json:"length"`
			Time   float64 `json:"time"`
		} `json:"summary"`
	} `json:"trip"`
	Error string `json:"error"`
}

// NoRouteError means Valhalla could not connect the two points — usually
// because one of them landed in water or on a pedestrian-only street.
//
// A distinct type because it is an expected outcome, not a failure: the
// simulator drops random points into a bounding box that includes the IJ, and
// the right response is to pick another destination rather than to retry or to
// log an error.
type NoRouteError struct {
	From, To geo.Point
	Detail   string
}

func (e *NoRouteError) Error() string {
	return fmt.Sprintf("routing: no route from %+v to %+v: %s", e.From, e.To, e.Detail)
}

// Route asks for a driveable route between two points.
func (c *Client) Route(ctx context.Context, from, to geo.Point) (*Route, error) {
	body, err := json.Marshal(map[string]any{
		"locations": []location{at(from), at(to)},
		"costing":   "auto",
		"directions_options": map[string]string{
			"units": "kilometers",
		},
		// The shape is the only part of the response we use; skipping the
		// turn-by-turn narrative cuts the payload substantially at 10k routes.
		"directions_type": "none",
	})
	if err != nil {
		return nil, fmt.Errorf("routing: encode request: %w", err)
	}

	var decoded routeResponse
	if err := c.post(ctx, "/route", body, &decoded); err != nil {
		var status *statusError
		if ok := asStatus(err, &status); ok && status.code == http.StatusBadRequest {
			return nil, &NoRouteError{From: from, To: to, Detail: status.body}
		}
		return nil, err
	}

	if len(decoded.Trip.Legs) == 0 {
		return nil, &NoRouteError{From: from, To: to, Detail: "no legs in response"}
	}

	points, err := geo.DecodePolyline6(decoded.Trip.Legs[0].Shape)
	if err != nil {
		return nil, fmt.Errorf("routing: decode shape: %w", err)
	}

	path, err := geo.NewPath(points)
	if err != nil {
		// A route so short it has no distinct vertices is not usable as a path.
		return nil, &NoRouteError{From: from, To: to, Detail: err.Error()}
	}

	return &Route{
		Path:     path,
		Duration: time.Duration(decoded.Trip.Summary.Time * float64(time.Second)),
		Meters:   decoded.Trip.Summary.Length * 1000,
		Encoded:  decoded.Trip.Legs[0].Shape,
	}, nil
}

// MatrixCell is one source-to-target pair.
type MatrixCell struct {
	Duration time.Duration
	Meters   float64
	// Reachable is false when Valhalla found no path. A batched assignment must
	// treat those as infinite cost rather than as zero.
	Reachable bool
}

type matrixResponse struct {
	SourcesToTargets [][]struct {
		Time     *float64 `json:"time"`
		Distance *float64 `json:"distance"`
	} `json:"sources_to_targets"`
}

// Matrix returns the travel time and distance from every source to every
// target — the cost matrix a global assignment needs.
//
// This is the endpoint that makes batched matching possible at all. Greedy
// matching needs one route per candidate; solving an assignment problem over a
// 2-second window of requests needs the whole R x D grid, and asking for it as
// R x D separate /route calls would cost more than the batching saves.
func (c *Client) Matrix(ctx context.Context, sources, targets []geo.Point) ([][]MatrixCell, error) {
	if len(sources) == 0 || len(targets) == 0 {
		return nil, fmt.Errorf("routing: matrix needs at least one source and one target")
	}

	from := make([]location, len(sources))
	for i, point := range sources {
		from[i] = at(point)
	}
	to := make([]location, len(targets))
	for i, point := range targets {
		to[i] = at(point)
	}

	body, err := json.Marshal(map[string]any{
		"sources": from,
		"targets": to,
		"costing": "auto",
	})
	if err != nil {
		return nil, fmt.Errorf("routing: encode matrix request: %w", err)
	}

	var decoded matrixResponse
	if err := c.post(ctx, "/sources_to_targets", body, &decoded); err != nil {
		return nil, err
	}

	if len(decoded.SourcesToTargets) != len(sources) {
		return nil, fmt.Errorf("routing: matrix returned %d rows for %d sources",
			len(decoded.SourcesToTargets), len(sources))
	}

	matrix := make([][]MatrixCell, len(sources))
	for i, row := range decoded.SourcesToTargets {
		matrix[i] = make([]MatrixCell, len(targets))
		for j, cell := range row {
			if j >= len(targets) {
				break
			}
			// Valhalla returns null rather than omitting the pair when a target
			// is unreachable, which is why these are pointers.
			if cell.Time == nil || cell.Distance == nil {
				continue
			}
			matrix[i][j] = MatrixCell{
				Duration:  time.Duration(*cell.Time * float64(time.Second)),
				Meters:    *cell.Distance * 1000,
				Reachable: true,
			}
		}
	}

	return matrix, nil
}

// Healthy reports whether Valhalla is up and serving tiles.
func (c *Client) Healthy(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/status", nil)
	if err != nil {
		return err
	}

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("routing: valhalla unreachable at %s: %w", c.baseURL, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("routing: valhalla status %d", response.StatusCode)
	}
	return nil
}

type statusError struct {
	code int
	body string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("routing: valhalla returned %d: %s", e.code, e.body)
}

func asStatus(err error, target **statusError) bool {
	status, ok := err.(*statusError)
	if ok {
		*target = status
	}
	return ok
}

func (c *Client) post(ctx context.Context, path string, body []byte, into any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("routing: build request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("routing: %s: %w", path, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		snippet := make([]byte, 256)
		n, _ := response.Body.Read(snippet)
		return &statusError{code: response.StatusCode, body: string(snippet[:n])}
	}

	if err := json.NewDecoder(response.Body).Decode(into); err != nil {
		return fmt.Errorf("routing: decode %s response: %w", path, err)
	}
	return nil
}
