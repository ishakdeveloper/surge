// Package routing adapts the Valhalla client to what the trip service needs.
//
// A three-line adapter that earns its place: the service declares a Router
// interface with one method, so its tests need a fake with one method rather
// than a routing engine. The narrowing happens here, at the edge.
package routing

import (
	"context"

	"github.com/ishakdeveloper/surge/services/trip/internal/service"
	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/routing"
)

type Valhalla struct{ client *routing.Client }

func NewValhalla(client *routing.Client) *Valhalla { return &Valhalla{client: client} }

func (v *Valhalla) Route(ctx context.Context, from, to geo.Point) (service.RouteResult, error) {
	route, err := v.client.Route(ctx, from, to)
	if err != nil {
		return service.RouteResult{}, err
	}

	return service.RouteResult{
		// Re-encoding the decoded path would lose precision and cost work; the
		// shape is passed through as Valhalla emitted it.
		Polyline6: route.Encoded,
		Meters:    route.Meters,
		Seconds:   int64(route.Duration.Seconds()),
	}, nil
}

// FlatSurge is base price everywhere: for tests, and for a trip service run
// without matchers. The real one is events.SurgeBook, which reads each cell's
// multiplier from surge.cells.
type FlatSurge struct{}

func (FlatSurge) MultiplierAt(context.Context, geo.Point) float64 { return 1.0 }
