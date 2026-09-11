---
name: Surge
description: Ride-hailing for Amsterdam, read the way a Schiphol sign is read; every colour says one thing, and the next step is always the yellow one.
colors:
  sign-yellow: "#ffd200"
  sign-yellow-pressed: "#f5c900"
  ink: "#1a1a1a"
  white-sheet: "#ffffff"
  near-white-ground: "#f7f7f5"
  grey-tile: "#f0f0ed"
  grey-tile-hover: "#e8e8e4"
  grey-accent: "#ebebe7"
  hairline: "#e3e3df"
  muted-ink: "#5f636a"
  exit-green: "#067a3e"
  refused-red: "#c8312a"
typography:
  display:
    fontFamily: "SF Pro Rounded, ui-rounded, system-ui, sans-serif"
    fontSize: "28px"
    fontWeight: 700
    lineHeight: 1.15
  headline:
    fontFamily: "SF Pro Rounded, ui-rounded, system-ui, sans-serif"
    fontSize: "24px"
    fontWeight: 700
    lineHeight: 1.25
  title:
    fontFamily: "SF Pro Rounded, ui-rounded, system-ui, sans-serif"
    fontSize: "20px"
    fontWeight: 700
    lineHeight: 1.25
  price:
    fontFamily: "SF Pro Rounded, ui-rounded, system-ui, sans-serif"
    fontSize: "22px"
    fontWeight: 700
    lineHeight: 1
    fontFeature: "tnum"
  card-title:
    fontFamily: "SF Pro Rounded, ui-rounded, system-ui, sans-serif"
    fontSize: "18px"
    fontWeight: 700
    lineHeight: 1.375
  name:
    fontFamily: "SF Pro Rounded, ui-rounded, system-ui, sans-serif"
    fontSize: "16px"
    fontWeight: 600
    lineHeight: 1.5
  body:
    fontFamily: "SF Pro Rounded, ui-rounded, system-ui, sans-serif"
    fontSize: "15px"
    fontWeight: 400
    lineHeight: 1.5
  action:
    fontFamily: "SF Pro Rounded, ui-rounded, system-ui, sans-serif"
    fontSize: "15px"
    fontWeight: 600
    lineHeight: 1
  label:
    fontFamily: "SF Pro Rounded, ui-rounded, system-ui, sans-serif"
    fontSize: "13px"
    fontWeight: 600
    lineHeight: 1.4
  caption:
    fontFamily: "SF Pro Rounded, ui-rounded, system-ui, sans-serif"
    fontSize: "12px"
    fontWeight: 400
    lineHeight: 1.375
  mono:
    fontFamily: "ui-monospace, SF Mono, monospace"
    fontSize: "12px"
    fontWeight: 400
rounded:
  sm: "8px"
  md: "12px"
  lg: "16px"
  xl: "24px"
  pill: "9999px"
spacing:
  2xs: "4px"
  xs: "6px"
  sm: "8px"
  md: "12px"
  lg: "16px"
  xl: "20px"
  2xl: "24px"
components:
  button-primary:
    backgroundColor: "{colors.sign-yellow}"
    textColor: "{colors.ink}"
    typography: "{typography.action}"
    rounded: "{rounded.pill}"
    height: "44px"
    padding: "0 20px"
  button-primary-hover:
    backgroundColor: "{colors.sign-yellow-pressed}"
    textColor: "{colors.ink}"
  button-primary-disabled:
    backgroundColor: "{colors.grey-tile}"
    textColor: "{colors.muted-ink}"
  button-secondary:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.white-sheet}"
    typography: "{typography.action}"
    rounded: "{rounded.pill}"
    height: "44px"
    padding: "0 20px"
  button-quiet:
    backgroundColor: "{colors.grey-tile}"
    textColor: "{colors.ink}"
    typography: "{typography.action}"
    rounded: "{rounded.pill}"
    height: "44px"
    padding: "0 20px"
  button-quiet-hover:
    backgroundColor: "{colors.grey-tile-hover}"
    textColor: "{colors.ink}"
  button-ghost:
    backgroundColor: "transparent"
    textColor: "{colors.ink}"
    typography: "{typography.action}"
    rounded: "{rounded.pill}"
    height: "44px"
    padding: "0 20px"
  button-ghost-hover:
    backgroundColor: "{colors.grey-tile}"
    textColor: "{colors.ink}"
  button-danger:
    backgroundColor: "transparent"
    textColor: "{colors.refused-red}"
    typography: "{typography.action}"
    rounded: "{rounded.pill}"
    height: "44px"
    padding: "0 20px"
  button-block:
    backgroundColor: "{colors.sign-yellow}"
    textColor: "{colors.ink}"
    typography: "{typography.action}"
    rounded: "{rounded.pill}"
    height: "48px"
    width: "100%"
    padding: "0 24px"
  button-sm:
    backgroundColor: "{colors.sign-yellow}"
    textColor: "{colors.ink}"
    rounded: "{rounded.pill}"
    height: "32px"
    padding: "0 14px"
  go-pill:
    backgroundColor: "{colors.sign-yellow}"
    textColor: "{colors.ink}"
    rounded: "{rounded.pill}"
    height: "56px"
    width: "100%"
    padding: "8px 8px 8px 24px"
  input:
    backgroundColor: "{colors.grey-tile}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    height: "48px"
    padding: "0 16px"
  input-hover:
    backgroundColor: "{colors.grey-tile-hover}"
    textColor: "{colors.ink}"
  input-focus:
    backgroundColor: "{colors.white-sheet}"
    textColor: "{colors.ink}"
  sign-direction:
    backgroundColor: "{colors.sign-yellow}"
    textColor: "{colors.ink}"
    rounded: "{rounded.lg}"
    padding: "14px 16px"
  sign-choice:
    backgroundColor: "{colors.grey-tile}"
    textColor: "{colors.ink}"
    rounded: "{rounded.lg}"
    padding: "14px 16px"
  sign-choice-hover:
    backgroundColor: "{colors.grey-tile-hover}"
    textColor: "{colors.ink}"
  sign-service:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.white-sheet}"
    rounded: "{rounded.lg}"
    padding: "14px 16px"
  sign-done:
    backgroundColor: "{colors.exit-green}"
    textColor: "{colors.white-sheet}"
    rounded: "{rounded.lg}"
    padding: "14px 16px"
  sign-refused:
    backgroundColor: "{colors.refused-red}"
    textColor: "{colors.white-sheet}"
    rounded: "{rounded.lg}"
    padding: "14px 16px"
  sheet:
    backgroundColor: "{colors.white-sheet}"
    textColor: "{colors.ink}"
    rounded: "{rounded.xl}"
    padding: "12px"
  card:
    backgroundColor: "{colors.white-sheet}"
    textColor: "{colors.ink}"
    rounded: "{rounded.xl}"
    padding: "20px"
  badge:
    backgroundColor: "{colors.sign-yellow}"
    textColor: "{colors.ink}"
    typography: "{typography.caption}"
    rounded: "{rounded.pill}"
    height: "24px"
    padding: "0 10px"
  chip:
    backgroundColor: "{colors.grey-tile}"
    textColor: "{colors.ink}"
    rounded: "{rounded.pill}"
    padding: "8px 14px"
  top-bar:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.white-sheet}"
    rounded: "{rounded.pill}"
    height: "56px"
    padding: "8px 8px 8px 10px"
  top-bar-current:
    backgroundColor: "{colors.sign-yellow}"
    textColor: "{colors.ink}"
    rounded: "{rounded.pill}"
    padding: "8px 14px"
  icon-bubble:
    backgroundColor: "{colors.white-sheet}"
    textColor: "{colors.ink}"
    rounded: "{rounded.pill}"
    size: "40px"
---

# Design System: Surge

## Overview

**Creative North Star: "The Signposted Route"**

Surge borrows its colour logic from Schiphol and NS wayfinding. Every card's colour says what kind of thing it is and nothing is coloured for emphasis: sign yellow is the route, the chosen class and the next step; black is a service (the card, the account, a receipt); green is done; red is refused; everything else is a grey tile to pick from or read. The world is light because a rider reads it outdoors on a phone in flat Amsterdam daylight, and rounded because the face is rounded: tiles at 16px, the sheet at 24px, every action a pill.

The surface is one white sheet rising over a desaturated map. Its cards are set apart by tone, not by lines: there are no borders anywhere except table hairlines, and the few shadows exist only to lift a floating thing (the sheet, the black navigation pill, a menu, a dialog) off the page. The typeface is SF Pro Rounded throughout, in sentence case, semibold for names and bold for prices, with tabular figures and prices flush right. Motion is small and only where something changed: presses settle, a check pops, results rise in, the status card wipes in; each between 150 and 280ms on a soft ease-out, and reduced to colour changes when the rider asks for reduced motion.

The system refuses the category default: no floating white sheet with car renders and a black Confirm button, no ratings, no perks, no gradients, no glass, no toasts. What the system cannot back is not shown.

**Key Characteristics:**

- Colour is fixed by meaning: yellow next step, black service, green done, red refused, grey to choose from.
- One white sheet over a pale map; tone does the work borders and shadows would.
- Pill actions; 16px tiles; 24px sheet and cards.
- SF Pro Rounded only, sentence case, tabular figures, prices flush right.
- One lucide icon family in one stroke weight, plus two authored stop marks and the yellow arrow mark.
- Motion 150–280ms, soft ease-out, `motion-safe` everywhere, `MotionConfig reducedMotion="user"`.
- Focus is a 2px ring clear of the element: ink on light, sign yellow inside anything dark.

## Colors

A near-white ground, a white sheet and grey tiles on it, with four signal colours that each mean exactly one thing.

### Primary

- **Sign Yellow** (`{colors.sign-yellow}`, ink text): the route on the map, the chosen class tile, the Go pill, the current page in the navigation, the selection highlight, the mark's tile. It is the next step, and only the next step. Hover darkens it to **Sign Yellow Pressed** (`{colors.sign-yellow-pressed}`).

### Secondary

- **Ink** (`{colors.ink}`, white text): a service. The floating navigation pill, the saved-card tile, the receipt, the payment-pending status card, the driver's disc on the map, the route's casing. Also every glyph, every heading and every focus ring on a light surface.

### Tertiary

- **Exit Green** (`{colors.exit-green}`, white text): done. The status card when the driver has arrived, the OTP ring when a code is accepted, the idle-fleet dots on the console map.
- **Refused Red** (`{colors.refused-red}`, white text at full strength; red text on a 10% wash for alerts and destructive pills): not going to happen. `ActionError`, invalid inputs, the danger pill, the OTP shake.

### Neutral

- **Near-White Ground** (`{colors.near-white-ground}`): the page under everything. Not pure white, so the white sheet reads as a sheet.
- **White Sheet** (`{colors.white-sheet}`): the rider's sheet, dialogs, popovers, menus, the icon bubble on a grey or yellow tile, and an input once it has focus.
- **Grey Tile** (`{colors.grey-tile}`): a choice or something to read; the resting state of every input; the disabled Go pill. Hover deepens it to **Grey Tile Hover** (`{colors.grey-tile-hover}`); the swap button's hover uses **Grey Accent** (`{colors.grey-accent}`).
- **Muted Ink** (`{colors.muted-ink}`): labels, asides, captions, placeholder text, disabled text, table headers.
- **Hairline** (`{colors.hairline}`): table row separators, the "or" divider on sign-in, the sheet's footer rule on a phone. Nowhere else.
- **Translucent inks**: on a dark sign, the icon bubble is white at 10–15%, secondary text is the sign's own ink at 70–80% opacity, and the navigation's inactive links are white at 70%. The class radio's empty ring is ink at 20%; the dotted rail is ink at 25%; the grabber is ink at 15%.

### Named Rules

**The One Meaning Rule.** A colour is chosen by what the element says, never for emphasis. Yellow is the next step, black is a service, green is done, red is refused. Two yellow things on one screen means two next steps, which is a bug.

**The Tone Not Line Rule.** Adjacent surfaces are told apart by tone (ground, sheet, tile) and never by a border. The only hairlines in the system are table row separators, the sign-in "or" divider and the phone sheet's footer rule.

**The Ink On Dark Rule.** Anything that turns black, green or red marks itself `data-surface="dark"` so the focus ring turns yellow inside it; a light card nested inside (the account menu) marks itself `data-surface="light"` to go back to ink.

## Typography

**Display Font:** SF Pro Rounded (with ui-rounded, system-ui, sans-serif)
**Body Font:** SF Pro Rounded (same family)
**Label/Mono Font:** ui-monospace, SF Mono, monospace, for trip ids and raw causes only

**Character:** One rounded face at four weights (400, 500, 600, 700), self-hosted as woff2. Sentence case everywhere; no uppercase, no letterspacing, no kickers. Hierarchy comes from weight and size, and numbers are tabular so prices and times line up flush right.

### Hierarchy

- **Display** (700, 28px, 1.15): the sign-in card's title only.
- **Headline** (700, 24px, tight): onboarding step titles and page titles under the bar.
- **Title** (700, 20px, tight): the trip status card's headline.
- **Price** (700, 22px, 1, tabular): a fare on a class tile or the Fare row; flush right.
- **Card Title** (700, 18px, snug): `CardTitle` and `DialogTitle`.
- **Name** (600, 16px): a stop's name, a class name, a row's subject.
- **Body** (400, 15px): prose, descriptions, asides; muted ink when secondary. Dialog descriptions and pill labels are 15px.
- **Action** (600, 15px): every pill's label; 17px bold on the Go pill; 14px on small pills.
- **Label** (600, 13px): "Popular trips", From/To labels (12px), the class tile's second line, table headers; muted ink or the sign's ink at 70%.
- **Caption** (400, 12px): the hold explanation under Go, the trip id line, step counters.

### Named Rules

**The Flush-Right Figures Rule.** Every price, duration and distance is set with tabular figures and right-aligned against the card's edge, so a column of tiles reads as a fare table.

**The Sentence Case Rule.** No uppercase labels, no tracked-out eyebrows, no kickers. A section is introduced by a 13px semibold muted label in sentence case, or by nothing.

## Layout

Phone first. The rider's page is a map band across the top 42% of the viewport (`42dvh`) with a white sheet rising over its lower edge by 24px, a 40×4px grabber, and the cards stacked 8px apart inside 12px of padding. The one action that moves the trip on is pinned in a footer under the thumb, above the safe area; it is not part of the scroll. From 1024px the map fills the page and the same sheet floats over its left side as a 452px column (420px card plus 16px gutters), starting 76px down under the floating bar, footer included. The map keeps its fitted route clear of the sheet (`inset` of 96/64/64/476 wide, 84/40/64/40 narrow) and refits on resize until the rider moves it.

Sign-in is the same arrangement: a sheet over the live map, 520px wide at desktop, with a `max-w-md` card inside that hangs from the top of its column rather than centring, so a step of another height moves only the card's foot. Every other signed-in page runs below the floating bar with 88px of top padding and 16px (32px from `md`) of side padding, its content on a white 24px page card.

The navigation pill is full width on a phone and, from `md`, as wide as what it holds (at least 416px), so it reads as a control floating over the page rather than a black band.

Rhythm inside a card: 16px sides and 14px top and bottom on a sign; 12px between the icon bubble and the text; 6px of padding around the stops card's rows; 20px card spacing on a page-scale `Card` (16px for the small size); 24px in a dialog. Between cards: 8px. Between form fields: 20px.

Breakpoints that matter: `sm` (640px) for dialog width and the wordmark, `md` (768px) for page padding, `lg` (1024px) for the sheet floating over the map.

## Elevation & Depth

Depth is tonal by default: ground under sheet under tile, with no shadow between them and no border. A shadow appears only on a thing that actually floats over the page, and it is always soft, wide and low-contrast, never a hard offset.

### Shadow Vocabulary

- **Sheet edge** (`box-shadow: 0 -10px 30px -18px rgb(0 0 0 / 0.35)`): the top edge of the phone sheet where it rises over the map.
- **Floating sheet** (`box-shadow: 0 18px 44px -18px rgb(0 0 0 / 0.35)`): the desktop rider sheet and sign-in card over the map.
- **Menu / popover** (`box-shadow: 0 18px 44px -16px rgb(0 0 0 / 0.35–0.4)`): the account menu, select popups.
- **Navigation** (`box-shadow: 0 14px 34px -14px rgb(0 0 0 / 0.55)`): the black bar, darker because it sits over the map.
- **Dialog / drawer** (`box-shadow: 0 24px 60px -20px rgb(0 0 0 / 0.45)`): modal surfaces, over a plain 35% black dim with no blur.
- **Page card** (`box-shadow: 0 1px 2px rgb(0 0 0 / 0.04), 0 12px 32px -18px rgb(0 0 0 / 0.22)`): a white `Card` on the ground at page scale.
- **Button lift** (`box-shadow: 0 1px 2px rgb(0 0 0 / 0.08)`): the swap button, a white disc on a grey tile.
- **Inset ring** (`box-shadow: inset 0 0 0 2px var(--foreground | --destructive | --success)`): not depth but state; the focused input, the invalid input, the OTP box.

### Named Rules

**The Floating Only Rule.** A shadow is evidence that a thing floats over the page. Tiles inside a sheet never carry one; two surfaces on the same plane are told apart by tone.

**The No Glass Rule.** The dialog and drawer backdrop is a plain dim (`rgb(0 0 0 / 0.35)`). Nothing is blurred, frosted or translucent for effect.

## Shapes

Rounded, like the face, on a strict palette. Four radii and a pill: 8px (`sm`) for the mark's tile and small selects, 12px (`md`) for inputs, OTP boxes, menu rows and select items, 16px (`lg`) for every sign tile and alert, 24px (`xl`) for the sheet, page cards, dialogs and drawers. Every action, chip, badge, icon bubble, avatar and the navigation itself is a full pill.

No borders. An input has none at rest and takes a 2px inset ring on focus; a checkbox has a 1.5px inset ring of ink at 25%; the class radio is a 24px disc with a 2px inset ring that fills with ink and a yellow check. The stops rail is a 2px dotted line of ink at 25% joining the two marks. The status card wipes in with a 16px-rounded clip.

The two authored marks are geometric: the pickup is a yellow disc ringed in ink with an ink centre (16px on the rail, 30px on the map on a white halo), the dropoff an ink rounded square with a yellow square heart. The map's driver is an ink disc with a white car glyph. The Surge mark is a yellow 8px-rounded tile with an ink up-right arrow.

The live city under the rider's map (and the onboarding map) adds two marks, both quieter than the trip's own. A free car is a 22px white disc with a faint ink halo and an ink arrowhead turned to its heading: a wayfinding arrow rather than a car drawn from above, and plainer than the rider's driver, which stays the only car with a sign. A booking is a 5px yellow dot ringed in ink that pops in, holds and fades over 2.4s, with two yellow rings spreading to 34px behind it; under reduced motion it is the dot alone. Both draw beneath the route and the stops. Cars glide at constant speed between the gateway's one-second reports, and the map animates only while something is moving. The console shows the bookings over its fleet; it has its own dots for the cars.

## Components

### Buttons

Every button is a pill and its colour says what it does; it gives a little under the pointer and only changes colour under reduced motion.

- **Shape:** full pill (`9999px`), 44px tall by default, 48px for `lg`/block, 36px for `sm`, 32px for the `actionVariants` small pill, 28px for `xs`; 20px side padding at default, 24px on block, 14–16px on small.
- **Primary:** sign yellow with ink text, 15px semibold; hover to `#f5c900`; disabled becomes a grey tile with muted text (on the Go pill) or 45% opacity (on `Button`).
- **Hover / Focus:** `transition-[background-color,color,transform] 150ms ease-out`; `active:scale-[0.97]` (0.985 on wide pills); focus is the global 2px ring offset 2px.
- **Secondary:** ink with white text, hover to 85%.
- **Quiet / Outline:** grey tile with ink text, hover to tile-hover; the secondary choice.
- **Ghost:** no fill, ink text, tile fill on hover; the close X in a dialog.
- **Danger / Destructive:** red text on nothing (`actionVariants` danger) or a 10% red wash (`Button` destructive), hover to 15%; "Cancel trip" in the thumb zone.
- **Link:** 12px radius, ink text underlined in ink at 30%, offset 4px, full ink on hover.

### Go Pill (signature)

The next step, pinned under the thumb: a 56px yellow pill, 17px bold label at the left with 24px of padding, and at the right the price in tabular figures beside a 40px ink well holding a yellow arrow. The arrow nudges 2px right on hover over 200ms. Disabled it is a grey tile, the well ink at 10% and the arrow muted. Without a saved card the same pill is a link reading "Add a card to book". Under it, one 12px muted line says what the hold means.

### Sign Tiles (signature)

The one container the rider's screens are built from: `flex gap-3 rounded-2xl px-4 py-3.5` with the colour fixed by tone.

- **direction:** yellow, ink text; the chosen class, the status card while the trip moves toward the rider.
- **choice:** grey tile, ink text; a class to pick, a fare row, a loading row, the last trip; hover to tile-hover when it is a control.
- **service:** ink, white text; the saved card, the payment-pending status.
- **done:** green, white text; the driver has arrived.
- **refused:** red, white text; `ActionError`, with a triangle-alert glyph and the server's words.
  Tiles stack 8px apart. A tile that is a control adds `pressable` (150ms, `active:scale-[0.985]`). Dark tones set `data-surface="dark"`.

### Icon Bubble

A 40px white disc with a 20px lucide glyph, at the left of a sign. On a dark sign it is white at 10–15%. `SignGlyph` is the bare-glyph alternative: a 28px column with a 24px icon.

### Class Tile (signature)

A `choice` sign as a `role="radio"` button: a 24px radio disc (ink ring at 20% empty; ink fill with a 3px-stroke yellow check that pops in when chosen), the class name at 16px semibold with a 13px second line at 70%, and the price at 22px bold tabular flush right. The chosen tile turns `direction` yellow.

### Stops Card (signature)

A grey tile with 6px padding holding the From and To rows (12px sides, 10px vertical, 12px gap), each with its authored mark in a 20px column, a 12px semibold muted label and the name at 16px semibold. A 2px dotted ink rail at 25% joins the marks and fades while a stop is edited. A 36px white swap disc with a `0 1px 2px` shadow sits at the right; its arrows turn 180° over 300ms per swap. The route's duration and distance sit under the rows at 14px semibold tabular, flush right.

### Chips

- **Style:** grey tile pill, 14px sides and 8px vertical, 14px semibold ink text; hover to tile-hover, `active:scale-95`. The popular trips before a destination is set.
- **State:** no selected state; a chip is a shortcut, not a filter.

### Badges

A 24px pill at 12px semibold naming a state in the colour that state means: yellow, ink, red wash with red text, grey tile, or plain text.

### Cards / Containers

- **Corner Style:** 24px on the sheet, page `Card`, dialogs and drawers; 16px on tiles and alerts.
- **Background:** white on the near-white ground; grey tiles inside white.
- **Shadow Strategy:** see Elevation; a `Card` on the ground carries the page-card shadow, tiles inside the sheet carry none.
- **Border:** none.
- **Internal Padding:** 20px (`Card`), 16px (`Card` small), 24px (dialog), 12px (sheet), 16×14px (sign).

### Inputs / Fields

- **Style:** a grey tile, not a box: 48px tall, 12px radius, 16px side padding, 16px text, no border; placeholder in muted ink; hover to tile-hover. Textareas are the same at a 96px minimum. Selects are the same at 44px with 15px medium text and a 16px-radius white popup whose items take a tile on focus and mark the chosen one with an ink disc.
- **Focus:** the tile lifts to white and takes a 2px inset ink ring (`inset 0 0 0 2px var(--foreground)`), the same lift a stop makes when it is edited; 150ms on background and shadow.
- **Error / Disabled:** invalid is a 5% red wash with a 2px inset red ring; disabled is 50% opacity. Validation is on submit, inline, with `aria-invalid` and `aria-describedby`.
- **Checkbox:** a 20px 6px-radius grey tile with a 1.5px inset ring of ink at 25%; checked it fills with ink and shows a yellow check.
- **OTP:** six 56px boxes on a 12px radius, 24px bold tabular digits that pop in; an ink ring slides between boxes on a spring; the ring turns red and the boxes shake once (360ms) when refused, green with a 40ms stagger when accepted.

### Alerts

A message in place, 16px radius, 16px sides and 12px vertical, no border: a grey tile for information, a 10% red wash with red text for a problem. The title is semibold, the description 14px muted. `ActionError` is the stronger form: a full `refused` sign with `role="alert"`.

### Navigation

A black pill floating over the page: 56px tall, up to 768px wide, 12px from the top, 8px inner padding, with the yellow mark and "Surge" (wordmark hidden below `sm`) at the left. Links are 15px semibold, white at 70% resting and white on hover, 14px sides and 8px vertical; the current page is marked by a yellow pill that slides between links on a spring (`layoutId`). The account is a 40px white-at-12% disc with the initial in white; it opens a 256px white 16px-radius menu with 6px padding, menu rows at 12px radius taking a tile on hover. The bar settles in from 20px above on arrival. Real links throughout; no breadcrumbs; full-bleed pages run under it.

### Dialogs and Drawers

A white 24px card lifted off a 35% black dim: 24px padding, 20px gaps, 18px bold title, 15px muted description, ghost close in the corner. Opens at 95% scale and fades in over 200ms on the soft ease-out. Drawers slide 40px from their edge over 250ms, rounded 24px on the leading corners.

### Tables

The one place hairlines belong: rows separated by the hairline at 70%, a 40px header at 13px semibold muted, 12px cell sides and 10px vertical, a 70% tile under the hovered row and a full tile under a selected one.

### Onboarding

Step progress as 6px pill segments in grey tile filling with ink over 400ms, a 12px "Step n of m" counter, a 24px bold title, and the "how it works" list on the same dotted rail as the stops card with 24px numbered white discs haloed by 4px of tile.

### Map

OpenFreeMap Positron: pale greys so the route is the loudest thing. The route is sign yellow at 5px on an ink casing at 9px with round caps and joins. Stops and the driver are the authored marks at 30px on a white halo; console fleet dots are 2.5–6px unoutlined; other markers are 7px discs with a 2px ink stroke. Surging cells are red, busy cells amber. Attribution is set in SF Pro Rounded at 11px.

### Native (apps/mobile)

The Expo app carries the same token names and values as RGB channels (`global.css`, `tailwind.config.js`) and the same radii (8/12/16/24), with `theme.ts` holding the hex for the few places that take a colour rather than a class. `Sign`, `SignButton`, `IconBubble` and `Action` mirror the web primitives with the same tones and sizes; pressed states use `active:` scale and opacity, and motion runs on Reanimated. Native icons are Ionicons at 20–24px rather than lucide, and the pressed yellow is `#f0c400` rather than the web's `#f5c900`; both are noted here as the build's divergences, not as a second system.

## Do's and Don'ts

### Do:

- **Do** choose a surface's colour by what it says: yellow for the next step and the chosen thing, ink for a service, green for done, red for refused, grey tile for a choice or a note.
- **Do** make every action a pill (`9999px`), 44px tall by default and 48–56px in the thumb zone, 15px semibold in sentence case.
- **Do** build inputs as grey tiles (`#f0f0ed`, 12px radius, no border) that lift to white with a 2px inset ink ring on focus, and a 2px inset red ring on `aria-invalid`.
- **Do** set prices, times and distances in tabular figures, bold, flush right.
- **Do** use lucide icons in one stroke weight, sized 16–24px, and the two authored stop marks for pickup and dropoff on both the rail and the map.
- **Do** keep motion between 150 and 280ms on `cubic-bezier(0.22, 1, 0.36, 1)`, opted in with `motion-safe:` or under `MotionConfig reducedMotion="user"`, and only where something changed.
- **Do** mark any ink, green or red surface `data-surface="dark"` so its focus ring turns yellow; nest `data-surface="light"` to return to ink.
- **Do** announce trip status through an `aria-live="polite"` region and show failures in place as a `refused` sign with `role="alert"`.

### Don't:

- **Don't** draw a border on a card, tile, input or button. Hairlines belong to table rows, the sign-in "or" divider and the phone sheet's footer rule, and nowhere else.
- **Don't** put a shadow on anything that does not float over the page; tiles inside a sheet carry none, and no shadow is a hard offset.
- **Don't** use gradients, blur, frosted glass or translucent chrome; the dialog backdrop is a plain 35% dim.
- **Don't** use a toast. Messages are inline, a banner, or a dialog.
- **Don't** render cars, ratings, perks, driver avatars or any social proof; a class tile carries a radio mark, a name, seats and a price.
- **Don't** set text in uppercase, add letterspacing, or write kickers and eyebrows; sentence case throughout, with a 13px semibold muted label where a section needs one.
- **Don't** put two yellow surfaces on one screen; yellow is the one next step.
- **Don't** use a second typeface or a system display face; SF Pro Rounded at 400/500/600/700 is the whole ramp, with the monospace stack reserved for trip ids and raw causes.
