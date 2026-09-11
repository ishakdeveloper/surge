package wire

// The city feed: what any signed-in map may show of the fleet and of demand.
//
// The console's fleet feed is for ops — every driver by id, busy or not. This
// one is what a rider's map may show, and it is shaped by what it must not give
// away. Only cars free to take a trip, because a car with a passenger in it is
// not something another rider gets to follow. No driver ids, only a key that
// tells one car from another between frames. And bookings as the area they
// were made in, never the address.
//
// Every slice is allocated, never nil, for the reason given in fleet.go.

const (
	// TagCityUpdate is one map's view of the city, pushed once a second while
	// it watches.
	TagCityUpdate = "CityUpdate"

	// TagClientWatchCity asks for city updates for a viewport. Any signed-in
	// caller: the feed is shaped to be safe to show anyone.
	TagClientWatchCity = "ClientWatchCity"
	// TagClientUnwatchCity stops them.
	TagClientUnwatchCity = "ClientUnwatchCity"
)

// CityCar is a car free to take a trip.
type CityCar struct {
	// Key tells one car from another across updates, so the map can move it
	// rather than redraw it, and says nothing else. It is a keyed hash of the
	// driver id under a secret the gateway draws when it starts, so it cannot
	// be turned back into the id, and it changes when the gateway restarts.
	Key     string  `json:"key"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Heading float64 `json:"heading"`
}

// CityBooking is a trip somebody booked, as the area it was booked in.
type CityBooking struct {
	// Key tells one booking from another across the several updates that carry
	// it, so the map flashes it once. Keyed like a car's, from the trip id.
	Key string `json:"key"`
	// Lat and Lng are the middle of the resolution-8 cell the pickup fell in,
	// about half a kilometre across: near enough to say where the city is busy,
	// too coarse to say whose door it was.
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
	AtMs int64   `json:"atMs"`
}

// CityUpdate is one map's view of the city.
type CityUpdate struct {
	AtMs int64 `json:"atMs"`
	// Cars is empty when the map is zoomed out past the point where a car is a
	// speck, and otherwise the free cars in view, nearest its middle first.
	Cars []CityCar `json:"cars"`
	// Bookings are those made in view in the last few seconds.
	Bookings []CityBooking `json:"bookings"`
}
