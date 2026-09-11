import { describe, expect, it } from "@jest/globals";
import type { FakeRequest } from "@surge/common/testing/fake-platform";
import { fireEvent, screen, waitFor } from "@testing-library/react-native";
import { BookRide } from "../../src/ride/book-ride";
import { renderScreen, tripFixture } from "./render-screen";

const FARE_ID = "deced82b-ee7c-4137-bd72-385eee1c98fe";

const routes = [
  { method: "GET", path: "/v1/trips", body: { trips: [] } },
  {
    method: "GET",
    path: "/v1/payments/method",
    body: { saved: true, card: { brand: "visa", last4: "4242", expMonth: 12, expYear: 2030 } },
  },
  {
    method: "POST",
    path: "/v1/trips:preview",
    body: {
      fares: [{
        fareId: FARE_ID,
        packageSlug: "sedan",
        totalCents: "1627",
        surgeMultiplier: 1,
        expiresAt: "2026-09-10T13:20:52Z",
      }],
      route: { polyline6: "", meters: 6840, seconds: "720" },
    },
  },
  { method: "POST", path: "/v1/trips", body: { trip: tripFixture } },
];

describe("booking a ride", () => {
  it("prices a preset, and books the chosen fare with a key that makes a retry safe", async () => {
    const requests: Array<FakeRequest> = [];
    await renderScreen(<BookRide />, { routes, requests });

    await fireEvent.press(await screen.findByText("Centraal → Rijksmuseum"));
    await fireEvent.press(await screen.findByText("sedan"));
    await fireEvent.press(screen.getByText("Book this ride"));

    await waitFor(() => {
      expect(requests.some((request) => request.method === "POST" && request.path === "/v1/trips"))
        .toBe(true);
    });

    const booking = requests.find((request) =>
      request.path === "/v1/trips" && request.method === "POST"
    );
    expect(booking?.body).toEqual({ fareId: FARE_ID, idempotencyKey: expect.any(String) });
    // Every call went out with the platform's token, attached by the shared client.
    expect(requests.every((request) => request.authorization === "Bearer fake-token")).toBe(true);
  });

  it("holds the Book button back until a fare is chosen", async () => {
    await renderScreen(<BookRide />, { routes });

    await fireEvent.press(await screen.findByText("Centraal → Rijksmuseum"));
    await screen.findByText("sedan");

    expect(screen.getByRole("button", { name: "Book this ride" })).toBeDisabled();
  });
});
