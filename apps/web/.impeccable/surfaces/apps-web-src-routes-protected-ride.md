---
version: 1
slug: "apps-web-src-routes-protected-ride"
primary_target: "apps/web/src/routes/_protected/ride"
related_targets: ["apps/mobile/src/ride","apps/web/src/routes/auth","apps/web/src/routes/_protected/-components"]
---

# Rider booking flow

## Scope and mode

Operate. The rider's surface on web (`apps/web/src/routes/_protected/ride`,
including `/ride/payment`) and mobile (`apps/mobile/src/ride`): find places,
see the quote, choose a class, confirm the card, book, follow the live trip,
see the receipt. It is designed phone first, and the web page at desktop width
is the same sheet floating over a larger map.

## Audience and job

A rider in Amsterdam, on a phone, often outdoors in flat grey daylight. They
want to know the price before committing, book in a few taps, see the car
coming, and not think about payment.

## Constraints

- Every fact on screen comes from the system: the classes are Surge, Surge XL
  and Surge Black from the quote, and the card is the saved card. No car
  renders, ratings or perks.
- Places come from address search (Photon over OpenStreetMap, set with
  `VITE_PLACES_URL`) with reverse lookup for picked points. Map picking and the
  popular trips stay as fallbacks.
- No toasts. Trip status is announced politely as it changes.
- Anti-references from the owner: an Uber clone, anything toy-like, a Dribbble
  shot that falls apart with real states.
- Owner steer, 2026-09-11, after the first build: clean and rounded, a whiter
  ground that is not pure white, greyish cards, better From/To inputs, subtle
  micro-animations, clean inline edits. The reference was a rounded
  near-white app with grey inputs, yellow for the chosen thing and pill
  actions.

## Direction contract

THESIS: The trip as a signposted route, in the colour logic of Schiphol and NS
wayfinding: every card's colour says what kind of thing it is, and the next
step is always the yellow one. It refuses the category default of a floating
white sheet with car renders and a black Confirm button.

OWN-WORLD: The ground is a near-white #F7F7F5, and the rider's sheet is white.
Choices sit on grey tiles (#F0F0ED, #E8E8E4 on hover) with 16px corners. The
sheet has 24px corners, and every action is a pill. Colour is fixed by
meaning:

- Sign yellow #FFD200 with ink #1A1A1A text is the route, the chosen class,
  and the next step.
- Black #1A1A1A with white text is a service: the card, the account, a receipt.
- Exit green #067A3E is done.
- Red #C8312A is refused.

The map is desaturated Positron. The route is yellow on an ink casing. The
stops are a yellow disc ringed in ink (pickup) and an ink square with a yellow
heart (dropoff), the same marks on the map and on the From/To rail. Icons come
from lucide, in one stroke weight. The pickup and dropoff marks are authored.
Type is SF Pro Rounded, semibold and bold in sentence case, with tabular
figures and prices flush right.

STORY: The rider opens on their pickup already set and edits either stop in
place: the row lifts into a white field and the places found rise in beneath
it. They see the route in yellow across a grey city and the three classes
priced, with Surge preselected. They see that the fare will be held on the
card shown, not charged, and tap Go. From then on a status card at the top of
the sheet changes with the trip until the receipt.

FIRST VIEWPORT: On a 390 × 844 phone, the map fills the top 42%. A white sheet
with a grabber rises over its lower edge, overlapping it by 24px. In the
sheet:

1. A grey From/To card: two rows on a dotted rail, a swap button, and
   "12 min · 6.8 km" flush right.
2. The popular trips as pill chips, until a destination is set.
3. The three class tiles, each with a radio mark that pops a check when
   chosen; the chosen tile turns yellow.
4. The black card tile.

Pinned under the sheet in the thumb zone is a yellow "Go · Surge · €16,27"
pill, with its arrow in an ink well and one line on the hold. Without a saved
card it is "Add a card to book" and leads to the payment page. At desktop
width the same sheet floats over a full-height map, 452px wide, footer
included.

The signature interaction is inline editing. While a stop is being edited,
the rest of the sheet steps aside, and it rises back in once a place is
chosen. The micro-motion, each 150–280ms with a soft ease-out and reduced to
colour changes under reduced motion:

- presses settle slightly
- the class check pops in
- results rise in, staggered
- the swap arrows turn
- the Go arrow nudges on hover
- the status card wipes in from the left when the trip moves on

FORM: Wayfinding Yellow, position 1 on the ordered list, seed key f9eee30a.
Refined to rounded cards by the owner's steer.

FINISH: unreviewed and undocumented is unfinished; this build ends with the
finish review, the verdict, DESIGN.md, and every shipping raster carrying its
provenance

## Decisions from the first finish review

- Go is pinned in the thumb zone. The no-card Go leads to the payment page.
- `ActionError` is a refused card.
- `/ride/payment` is in the world.
- The map refits when it resizes, until the rider moves it.
- Focus is ink on light surfaces and yellow on dark ones (`data-surface`).
- The authored pictogram set was set aside by the owner's steer in favour of
  one lucide family. The class tiles carry a radio mark instead of three
  identical cars.

## Unresolved

- A self-hosted Photon for deployments (`VITE_PLACES_URL`). The public one
  answers some clients with a 503.
- The mobile port of this steer, and the mobile font build (expo-font needs a
  new dev client).

## Round three: components, shell, sign-in, onboarding (owner steer, 2026-09-11)

Owner's words: "polish the ui components, cards, inputs, login / register
pages and a proper onboarding flow. the borders etc are really bad. use the
new ride page design as a reference. use motion/react. also the navbar looks
bad. make it floating, animated, maybe white or keep black." Then: no
email/phone tabs, email by default with a quiet phone link, a real OTP input
with keyboard navigation (rareui's as the reference), the aside links more
subtle with more space above.

Decisions:

- Every shared component is re-skinned in the sheet's grammar: tone instead
  of borders, 16px tiles and 24px cards, pill actions, grey-tile inputs that
  lift to white with an ink ring on focus. Hairlines remain only in tables.
- The navigation is a black floating pill (max 768px wide, 12px from the top),
  with the current page a yellow pill that slides between links (motion
  `layoutId`), and an account menu on a round initial button. Full-bleed
  pages run under it; the rider's sheet starts 76px down at desktop width.
- Sign-in is the same sheet over the live map: email first, "Text it to your
  phone" as an aside, a phone step that mirrors it, and a code step with
  `OtpInput`: six inputs, sliding ring, digits pop in, Backspace clears then
  steps back, arrows, paste/autofill, auto-submit on the sixth digit, red
  shake when refused. Asides are 13px muted text, coloured in on hover.
- Onboarding replaces the debug home page: welcome (how it works on the
  dotted rail), card (riders) or payouts (drivers), location, ready; each step
  skippable, progress in segments, remembered per account in localStorage.
- `motion/react` is the motion library on web, under `MotionConfig
  reducedMotion="user"`.
- The breadcrumb trail is gone: the bar says where you are.

## Round four: the live city (owner steer, 2026-09-11)

Owner's words: "lets switch to mapbox, kepler.gl and deck.gl... show active
drivers on the map, and show like a little dot flash in a place where someone
bought an uber." Then: "Now the new map with deck.gl".

Decisions:

- MapLibre + deck.gl stays; no Mapbox (no key, same renderer family) and no
  kepler.gl (an analysis app, not a rider layer).
- Riders get a new gateway feed, `CityUpdate`, separate from the ops-only
  fleet: only cars free to take a trip, keyed by an HMAC under a per-process
  secret (no driver ids), and bookings snapped to the centre of their H3
  res-8 cell (~460 m edge), never the pickup. Cars appear from zoom 12, at
  most 200, nearest the map's middle.
- Bookings come from the matcher's frames (every ride that reaches dispatch,
  after the fare hold; the simulator's riders too), deduplicated by trip and
  ignored when older than a minute.
- On the map: free cars as ink arrowheads on white discs that glide between
  reports; a booking as a yellow dot with two spreading rings, 2.4 s, the dot
  alone under reduced motion. On /ride, onboarding, and (bookings only) the
  console.
