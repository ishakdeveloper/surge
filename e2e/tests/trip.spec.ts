import { expect, test } from "@playwright/test";
import { newEmail, signIn } from "../support/accounts.js";

/**
 * Where the driver stands: Centraal, at the pickup of the "Centraal →
 * Rijksmuseum" preset, so the nearest idle driver the matcher finds is this
 * one. The app profile starts with no simulated drivers for the same reason.
 */
const CENTRAAL = { latitude: 52.3786, longitude: 4.8996 };

/**
 * One ride through every service, watched from both ends.
 *
 * rider books → payments holds the fare → trip → geo.events → matcher → an
 * offer on ws.push → gateway → the driver's socket → accept → trip → both
 * screens. Then the driver arrives, starts and completes, and the rider sees
 * each step land without asking.
 */
test("a trip, end to end, seen by the rider and the driver at every step", async ({ browser }) => {
  test.setTimeout(180_000);

  const driverContext = await browser.newContext({
    geolocation: CENTRAAL,
    permissions: ["geolocation"],
  });
  const riderContext = await browser.newContext();
  const driver = await driverContext.newPage();
  const rider = await riderContext.newPage();

  // The driver first, and online, before anyone books. The page pings every
  // four seconds once it has a position, so by the time the rider below has
  // signed up, saved a card and priced a route, the matcher has indexed this
  // driver; a request with nobody indexed near it would go unmatched.
  await signIn(driver, newEmail("driver"), "Driver");
  await driver.goto("/drive");
  await driver.getByRole("button", { name: "Use my location" }).click();
  await driver.getByRole("button", { name: "Go online" }).click();
  await expect(driver.getByText("Waiting for a rider nearby…")).toBeVisible();

  // With TRIP_REQUIRE_PAYMENT the fare is held before dispatch, so the rider
  // needs a card. The fake processor's test card.
  await signIn(rider, newEmail("rider"), "Rider");
  await rider.goto("/ride/payment");
  await rider.getByRole("button", { name: "Save the test card" }).click();
  await expect(rider.getByText(/ending 4242/)).toBeVisible();

  await rider.goto("/ride");
  await rider.getByRole("button", { name: "Centraal → Rijksmuseum" }).click();
  // The innermost section headed "Fares": ancestors come first in document
  // order, so the last match is the one holding the fares themselves.
  const fares = rider.locator("section").filter({
    has: rider.getByRole("heading", { name: "Fares" }),
  }).last();
  await fares.getByRole("button").first().click();
  await rider.getByRole("button", { name: "Book this ride" }).click();

  const riderStatus = rider.getByRole("status");
  await expect(riderStatus).toContainText(/Confirming your payment|Finding you a driver/);

  await driver.getByRole("article").getByRole("button", { name: "Accept" }).click();
  await expect(riderStatus).toContainText("Your driver is on the way");
  await expect(driver.getByRole("status")).toContainText("Drive to the pickup");

  await driver.getByRole("button", { name: "I have arrived" }).click();
  await expect(riderStatus).toContainText("Your driver is at the pickup");

  await driver.getByRole("button", { name: "Start the trip" }).click();
  await expect(riderStatus).toContainText("On your way");

  // A completed trip hands the rider back to booking, with the last one noted.
  await driver.getByRole("button", { name: "Complete the trip" }).click();
  await expect(rider.getByText(/Your last trip/)).toBeVisible();

  await driverContext.close();
  await riderContext.close();
});
