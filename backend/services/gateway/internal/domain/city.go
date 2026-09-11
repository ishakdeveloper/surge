package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/wire"
)

const (
	// BookingTTL is how long a booking stays in the city feed: long enough to
	// reach a map through a dropped frame or two. The map flashes each booking
	// once, however many frames carry it.
	BookingTTL = 6 * time.Second

	// BookingRes is the grain a booking is shown at. Resolution 8 is about
	// 0.74 km², with a 460 m edge: where the city is busy, and not whose door.
	BookingRes = 8

	// CityCarZoom is where a map starts to see cars. Further out a car is a
	// speck, and every free car in the city would be the whole frame.
	CityCarZoom = 12.0

	// MaxCityCars caps a frame's cars, nearest the middle of the map first, so
	// a rider's map stays a map and its frame stays a few kilobytes.
	MaxCityCars = 200

	// bookingStale is how old a request may be and still be news. A partition
	// that changes hands replays the records since its last commit, and a
	// request from a minute ago flashing now would be a booking that did not
	// just happen.
	bookingStale = time.Minute

	// maxBookings bounds memory if demand floods. Past it the oldest go first,
	// which the TTL would drop in a moment anyway.
	maxBookings = 1000
)

// City is what any signed-in map may see of the fleet and of demand.
//
// The console's Fleet is for ops: every driver, by id, busy or not. This is
// what a rider's map may see, and it is shaped by what it must not give away.
// Only cars free to take a trip — a car with a passenger in it is not something
// another rider gets to follow. No driver ids, only a key that tells one car
// from another between frames. And bookings as the area they were made in,
// never the address.
type City struct {
	secret []byte

	mu sync.Mutex
	// bookings in the order they were heard, which is what lets pruning drop
	// a prefix.
	bookings []heardBooking
}

type heardBooking struct {
	booking wire.CityBooking
	heard   time.Time
}

// NewCity keys cars and bookings under secret. A gateway draws a fresh one when
// it starts, so no key outlives the process that made it.
func NewCity(secret []byte) *City {
	return &City{secret: secret}
}

// Booked records a trip booked at pickup, as the middle of the area around it,
// and reports whether it was news.
//
// Not news: a pickup that is not a place — (0, 0) is what a missing one decodes
// as — a request too old to have just happened, and a trip already shown. A
// matcher that restores a partition replays what it had not committed, so the
// same trip can arrive twice.
func (c *City) Booked(tripID string, pickup geo.Point, atMs int64, now time.Time) bool {
	if !pickup.Valid() || (pickup.Lat == 0 && pickup.Lng == 0) {
		return false
	}
	if now.Sub(time.UnixMilli(atMs)) > bookingStale {
		return false
	}
	cell, err := geo.CellAt(pickup, BookingRes)
	if err != nil {
		return false
	}
	center, err := geo.CellCenter(cell)
	if err != nil {
		return false
	}

	key := c.key("trip", tripID)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.prune(now)
	for _, heard := range c.bookings {
		if heard.booking.Key == key {
			return false
		}
	}
	if len(c.bookings) >= maxBookings {
		c.bookings = c.bookings[1:]
	}
	c.bookings = append(c.bookings, heardBooking{
		booking: wire.CityBooking{Key: key, Lat: center.Lat, Lng: center.Lng, AtMs: atMs},
		heard:   now,
	})
	return true
}

// Snapshot is one map's view: the free cars in it, from the fleet, when it is
// zoomed in far enough to see them, and the bookings of the last few seconds.
func (c *City) Snapshot(fleet *Fleet, view wire.Viewport, now time.Time) wire.CityUpdate {
	update := wire.CityUpdate{AtMs: now.UnixMilli(), Cars: []wire.CityCar{}, Bookings: []wire.CityBooking{}}

	// Read before taking this lock, so the two are never held together.
	if view.Zoom >= CityCarZoom {
		for _, driver := range fleet.Free(view, now, MaxCityCars) {
			update.Cars = append(update.Cars, wire.CityCar{
				Key: c.key("car", driver.ID), Lat: driver.Lat, Lng: driver.Lng, Heading: driver.Heading,
			})
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.prune(now)
	for _, heard := range c.bookings {
		booking := heard.booking
		if booking.Lat >= view.South && booking.Lat <= view.North &&
			booking.Lng >= view.West && booking.Lng <= view.East {
			update.Bookings = append(update.Bookings, booking)
		}
	}
	return update
}

// prune drops bookings older than the TTL. Held under c.mu.
func (c *City) prune(now time.Time) {
	cutoff := now.Add(-BookingTTL)
	keep := sort.Search(len(c.bookings), func(i int) bool { return !c.bookings[i].heard.Before(cutoff) })
	c.bookings = c.bookings[keep:]
}

// key is an id as the city feed may show it: the same for the same id under
// this gateway's secret, and useless for finding the id again. The kind is
// part of the input, so a car and a trip that shared an id would not share
// a key.
func (c *City) key(kind, id string) string {
	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte(kind))
	mac.Write([]byte{0})
	mac.Write([]byte(id))
	return hex.EncodeToString(mac.Sum(nil))[:16]
}
