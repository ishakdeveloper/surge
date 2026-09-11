package domain_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ishakdeveloper/surge/services/gateway/internal/domain"
	"github.com/ishakdeveloper/surge/shared/geo"
	"github.com/ishakdeveloper/surge/shared/wire"
)

// A rider's map of the centre, zoomed in far enough to see cars.
var centre = wire.Viewport{West: 4.85, South: 52.34, East: 4.95, North: 52.40, Zoom: 13}

var secret = []byte("a key for these tests and nothing else")

func framed(drivers ...wire.FleetDriver) *domain.Fleet {
	fleet := domain.NewFleet()
	fleet.Record(wire.FleetFrame{Partition: 1, Instance: "matcher-a", Drivers: drivers}, epoch)
	return fleet
}

func onlyCar(t *testing.T, update wire.CityUpdate) wire.CityCar {
	t.Helper()
	if len(update.Cars) != 1 {
		t.Fatalf("cars = %+v, want exactly one", update.Cars)
	}
	return update.Cars[0]
}

// Only a car free to take a trip is shown, and not by who drives it: a car
// with a passenger in it, or on its way to one, is not another rider's to
// follow.
func TestTheCityShowsFreeCarsAndNotTheirDrivers(t *testing.T) {
	fleet := framed(
		driver(t, "drv-free", 52.3702, 4.8952, wire.StatusIdle, false),
		driver(t, "drv-held", 52.3703, 4.8953, wire.StatusIdle, true),
		driver(t, "drv-carrying", 52.3704, 4.8954, wire.StatusOnTrip, false),
		driver(t, "drv-coming", 52.3705, 4.8955, wire.StatusEnRoutePickup, false),
		driver(t, "drv-off", 52.3706, 4.8956, wire.StatusOffline, false),
	)

	car := onlyCar(t, domain.NewCity(secret).Snapshot(fleet, centre, epoch))

	if car.Lat != 52.3702 || car.Lng != 4.8952 {
		t.Errorf("car at (%f, %f), want the free driver's position", car.Lat, car.Lng)
	}
	if car.Key == "" || strings.Contains(car.Key, "drv") {
		t.Errorf("key %q gives the driver away", car.Key)
	}
}

// A car keeps its key from one frame to the next, so the map can move it
// rather than redraw it; another gateway, with another secret, gives the same
// driver another key.
func TestACarsKeyIsStableAndUnlinkable(t *testing.T) {
	fleet := framed(driver(t, "drv-1", 52.3702, 4.8952, wire.StatusIdle, false))
	city := domain.NewCity(secret)

	first := onlyCar(t, city.Snapshot(fleet, centre, epoch)).Key
	again := onlyCar(t, city.Snapshot(fleet, centre, epoch.Add(500*time.Millisecond))).Key
	elsewhere := onlyCar(t, domain.NewCity([]byte("another gateway")).Snapshot(fleet, centre, epoch)).Key

	if first != again {
		t.Errorf("the same car was keyed %q, then %q", first, again)
	}
	if first == elsewhere {
		t.Error("two gateways keyed the same driver the same way")
	}
}

// Zoomed out, a car is a speck and the whole city's would be the frame.
func TestAZoomedOutMapSeesNoCars(t *testing.T) {
	fleet := framed(driver(t, "drv-1", 52.3702, 4.8952, wire.StatusIdle, false))
	wide := centre
	wide.Zoom = domain.CityCarZoom - 1

	if cars := domain.NewCity(secret).Snapshot(fleet, wide, epoch).Cars; len(cars) != 0 {
		t.Errorf("zoomed out, got %d cars", len(cars))
	}
}

// Past the cap, the cars kept are the ones nearest the middle of the map, so
// the same cars are shown from one second to the next.
func TestTooManyCarsKeepsTheNearest(t *testing.T) {
	midLat, midLng := (centre.South+centre.North)/2, (centre.West+centre.East)/2
	drivers := []wire.FleetDriver{}
	for i := range domain.MaxCityCars {
		drivers = append(drivers, driver(t, fmt.Sprintf("near-%d", i), midLat+float64(i)*1e-5, midLng, wire.StatusIdle, false))
	}
	edge := centre.North - 0.001
	drivers = append(drivers, driver(t, "far", edge, centre.East-0.001, wire.StatusIdle, false))

	cars := domain.NewCity(secret).Snapshot(framed(drivers...), centre, epoch).Cars

	if len(cars) != domain.MaxCityCars {
		t.Fatalf("got %d cars, want the cap of %d", len(cars), domain.MaxCityCars)
	}
	for _, car := range cars {
		if car.Lat == edge {
			t.Fatal("kept the car at the edge over one in the middle")
		}
	}
}

// A booking is shown as the area it came from: near enough to say where the
// city is busy, and never the pickup itself.
func TestABookingIsAnAreaNotAnAddress(t *testing.T) {
	city := domain.NewCity(secret)
	door := geo.Point{Lat: 52.37021, Lng: 4.89517}
	if !city.Booked("trip-1", door, epoch.UnixMilli(), epoch) {
		t.Fatal("refused a real pickup")
	}

	bookings := city.Snapshot(domain.NewFleet(), centre, epoch).Bookings
	if len(bookings) != 1 {
		t.Fatalf("bookings = %+v, want one", bookings)
	}
	shown := geo.Point{Lat: bookings[0].Lat, Lng: bookings[0].Lng}

	if shown == door {
		t.Fatal("the map was sent the rider's pickup")
	}
	cell, err := geo.CellAt(door, domain.BookingRes)
	if err != nil {
		t.Fatalf("cell: %v", err)
	}
	if middle, _ := geo.CellCenter(cell); shown != middle {
		t.Errorf("shown at %+v, want the middle of the pickup's cell, %+v", shown, middle)
	}
	if meters := geo.DistanceMeters(shown, door); meters > 600 {
		t.Errorf("shown %.0f m from the pickup, outside the area it was in", meters)
	}
	if bookings[0].AtMs != epoch.UnixMilli() {
		t.Errorf("atMs = %d", bookings[0].AtMs)
	}
	if bookings[0].Key == "" || strings.Contains(bookings[0].Key, "trip") {
		t.Errorf("key %q gives the trip away", bookings[0].Key)
	}
}

// A booking is a moment: it leaves the feed after a few seconds, and a map
// only hears of the ones in view.
func TestBookingsFadeAndStayInTheirView(t *testing.T) {
	city := domain.NewCity(secret)
	city.Booked("in-view", geo.Point{Lat: 52.37, Lng: 4.90}, epoch.UnixMilli(), epoch)
	city.Booked("schiphol", geo.Point{Lat: 52.31, Lng: 4.76}, epoch.UnixMilli(), epoch)

	if got := city.Snapshot(domain.NewFleet(), centre, epoch).Bookings; len(got) != 1 {
		t.Errorf("in view: %d bookings, want the one in the centre", len(got))
	}
	later := epoch.Add(domain.BookingTTL + time.Second)
	if got := city.Snapshot(domain.NewFleet(), centre, later).Bookings; len(got) != 0 {
		t.Errorf("after the TTL: %d bookings, want none", len(got))
	}
}

// A missing pickup decodes as (0, 0), which is not a place anybody booked from.
func TestAPickupAtNullIslandIsNotABooking(t *testing.T) {
	if domain.NewCity(secret).Booked("trip-1", geo.Point{}, epoch.UnixMilli(), epoch) {
		t.Error("recorded a booking at (0, 0)")
	}
}

// A matcher restoring a partition replays what it had not committed, so one
// trip can arrive twice. It flashes once.
func TestABookingIsShownOnce(t *testing.T) {
	city := domain.NewCity(secret)
	door := geo.Point{Lat: 52.37, Lng: 4.90}
	if !city.Booked("trip-1", door, epoch.UnixMilli(), epoch) {
		t.Fatal("refused the first sighting")
	}
	if city.Booked("trip-1", door, epoch.UnixMilli(), epoch.Add(time.Second)) {
		t.Error("took the replay as a second booking")
	}
	if got := city.Snapshot(domain.NewFleet(), centre, epoch.Add(time.Second)).Bookings; len(got) != 1 {
		t.Errorf("%d bookings, want one", len(got))
	}
}

// A request replayed long after it was made is not a booking that just
// happened, and is not flashed as one.
func TestAnOldRequestIsNotNews(t *testing.T) {
	asked := epoch.Add(-2 * time.Minute).UnixMilli()
	if domain.NewCity(secret).Booked("trip-1", geo.Point{Lat: 52.37, Lng: 4.90}, asked, epoch) {
		t.Error("flashed a request from two minutes ago")
	}
}
