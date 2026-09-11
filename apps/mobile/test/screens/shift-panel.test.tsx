import { describe, expect, it } from "@jest/globals";
import { fireEvent, screen } from "@testing-library/react-native";
import { ShiftPanel } from "../../src/drive/shift-panel";
import { renderScreen } from "./render-screen";

const routes = [{ method: "GET", path: "/v1/trips", body: { trips: [] } }];

describe("a driver's shift", () => {
  it("cannot go online until the driver is somewhere", async () => {
    await renderScreen(<ShiftPanel />, { routes, role: "driver" });

    expect(await screen.findByRole("button", { name: "Go online" })).toBeDisabled();

    await fireEvent.press(screen.getByText("Centraal"));

    expect(screen.getByRole("button", { name: "Go online" })).toBeEnabled();
  });

  it("shows the matcher's status as it is sent", async () => {
    await renderScreen(<ShiftPanel />, { routes, role: "driver" });

    expect(await screen.findByText("offline")).toBeTruthy();

    await fireEvent.press(screen.getByText("Centraal"));
    await fireEvent.press(screen.getByText("Go online"));

    expect(await screen.findByText("idle")).toBeTruthy();
  });
});
