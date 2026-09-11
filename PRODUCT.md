# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

`apps/web` is the web target. `apps/mobile` is an Expo app for iOS and Android
that shares the web app's token names and carries the same Surge language on
both operating systems rather than adapting per OS. It is a separate target
when it is designed.

## Users

**Riders** in Amsterdam who need to get somewhere now. Usually on a phone,
often outdoors, sometimes in a hurry. Their job is to book a car, know what it
will cost before committing, see the driver coming, and pay without thinking
about it.

**Drivers** working a shift in Amsterdam. The phone is mounted in the car and
glanced at between junctions. Their job is to go on shift, take or pass on an
offer quickly, run the trip from pickup to drop-off, and see what they earned
and when it pays out.

**Operators** run the market from the console. Their job is to watch the
fleet and the matcher, and to drive the simulator.

The frontend is built for real riders and drivers, as if Surge launches
publicly in Amsterdam. People evaluating the project are incidental and are not
the audience to design for.

## Product Purpose

Surge is ride-hailing for one market, Amsterdam. A rider books a trip from a
fare quote, the fare is held on their card, and dispatch sends the trip to a
nearby driver. The trip then runs through a state machine from request to
completion, and the driver is paid out.

Success is a rider who books in a few taps and trusts the price, and a driver
who can act on an offer at a glance without taking their attention off the
road.

## Positioning

The backend makes promises most ride-hailing clones only claim:

- A driver is never double-booked. Each part of the city has exactly one
  matcher that assigns drivers there, so two riders cannot both be given the
  same car.
- The fare is quoted and held on the card before dispatch. With payment
  required, a trip reaches the matcher only once the fare is held.
- Every processor call is idempotent and the ledger is double entry, so a
  retry never charges twice.

## Operating Context

- Rider flow: sign in, set pickup and drop-off, get a fare quote, add or
  confirm a card, hold the fare, book, follow the live trip, pay.
- Driver flow: sign in, start a shift (position is reported to dispatch while
  on shift, including in the background on mobile), receive offers, accept or
  pass, run the trip, see earnings, set up payouts through Stripe.
- Console flow: simulator controls, fleet statistics, and the shard table.
  Fed by a driver simulator that puts tens of thousands of cars on real
  Amsterdam roads.
- Sign-in is a one-time code sent by email or text, with a rider or driver
  role chosen at sign-up.
- Maps show real Amsterdam streets. Routes come from Valhalla.
- Payments go through Stripe: card entry, fare holds, and driver payouts.

## Capabilities and Constraints

- Web: TanStack Start in `apps/web`, Tailwind 4, shadcn-style components in
  `apps/web/src/components/ui`.
- Mobile: Expo with NativeWind in `apps/mobile`. Tailwind 3 under NativeWind,
  and colours are RGB channels because React Native has no `oklch()`.
- The REST client is generated from `proto/trip.proto`. The UI must not
  declare endpoints by hand.
- One market, Amsterdam. Currency is euros, with amounts in cents.
- Terminology in the product: trip, fare quote, hold, offer, shift, earnings,
  payout.
- Ride classes are operator data in the trip service's catalogue: **Surge**
  (sedan, 4 seats), **Surge XL** (van, 6 seats) and **Surge Black** (luxury,
  4 seats). A quote prices every class for the route, and surge multiplies the
  metered fare above a per-class minimum.
- Riders book on their phones first. The web rider surface is designed phone
  first and must still work on a desktop browser.
- Places are found by address search, which needs a geocoder over
  OpenStreetMap data (Photon or Nominatim). It is decided but not built: today
  a rider picks points on the map or from three presets, and they are shown as
  raw coordinates.
- Undecided: payment methods beyond cards, scheduled rides, and shared rides.
  None exist in the backend today and none should be shown as if they do.

## Brand Commitments

- Name: **Surge**.
- Typeface: **SF Pro Rounded**, decided by the owner. The files are
  self-hosted from the owner's machine (`/Library/Fonts/SF-Pro-Rounded-*.otf`).
  The owner chose this knowing Apple's licence does not permit web embedding
  or redistribution.
- The existing data-dense, low-chrome dashboard look is discarded. The
  `dashboard-ui` rules that required it have been removed.

## Evidence on Hand

- Measured backend numbers in `docs/benchmarks`: 10,000 GPS writes per second
  sustained at 40,000 simulated drivers, p99 end-to-end age of 50 ms.
- There are no real riders, drivers, testimonials, ratings, reviews, press, or
  city launch. None may be fabricated. Driver names, ratings and vehicles
  shown in the UI must come from the system, not be invented as decoration.

## Product Principles

1. **The price before the promise.** A rider sees and holds the fare before
   anything is dispatched. The UI never hides the cost behind a commitment.
2. **Glanceable for the driver.** Driver screens are read in a moving car. One
   decision per screen, large targets, and state that is readable in a second.
3. **The live state is the truth.** Trip, offer and driver positions are
   real-time. The UI shows what the system knows now, and says so when it is
   waiting or out of date.
4. **Show nothing the system cannot back.** No invented ride classes, perks,
   ratings or social proof.

## Accessibility & Inclusion

- No toasts. Messages are inline, banners, or dialogs, per
  `knowledge/rules/accessible-notifications-and-messages.md`.
- Form validation follows `knowledge/rules/form-validation-message.md`:
  validate on submit, use `aria-invalid` and `aria-describedby`, and never
  the browser's native validation UI.
- Navigation uses real links, never buttons.
- Driver screens are used while driving, so targets must be large and the key
  state must be readable at a glance in daylight.
