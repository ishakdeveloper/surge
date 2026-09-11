import { afterEach, beforeEach, expect, it, jest } from "@jest/globals";
import { fireEvent, screen } from "@testing-library/react-native";
import SignIn from "../../src/app/(auth)/sign-in";
import { renderScreen } from "./render-screen";

/**
 * effect-form, validating on blur, marks every field touched when a form is
 * submitted, and each field then hands its validation the current value — which
 * the validation only computes when next read, and the next read is that
 * field's render. Computing there notifies the field's own subscription in the
 * middle of React's render, and React warns about an update while rendering.
 *
 * `patches/@lucas-barake__effect-form@0.25.0-beta.6.patch` reads the validation
 * as soon as the value is handed over, so it is computed in the submit instead.
 * This is what fails if the patch is ever lost — on an upgrade, or an install
 * that skips it.
 *
 * A file of its own, because React reports this warning once per process: a
 * test before it in the same file could use it up.
 */
let complaints: Array<string> = [];
let spy: ReturnType<typeof jest.spyOn> | undefined;

beforeEach(() => {
  complaints = [];
  spy = jest.spyOn(console, "error").mockImplementation((...args: Array<unknown>) => {
    const message = String(args[0]);
    if (message.includes("while rendering a different component")) complaints.push(message);
  });
  jest.mocked(globalThis.fetch).mockImplementation(async () =>
    new Response(JSON.stringify({ status: true }), {
      status: 200,
      headers: { "content-type": "application/json" },
    })
  );
});

afterEach(() => {
  spy?.mockRestore();
});

it("fails a submit, switches method and sends, without an update in the middle of a render", async () => {
  await renderScreen(<SignIn />, { routes: [] });

  // An empty submit: every field touched, every field validated, all still on screen.
  await fireEvent.press(screen.getByText("Email me a code"));
  await screen.findByText("Enter your email address.");

  await fireEvent.press(screen.getByTestId("method-phone"));
  await fireEvent.press(screen.getByTestId("method-email"));
  await fireEvent.changeText(screen.getByTestId("field-email"), "ada@example.com");
  await fireEvent.press(screen.getByText("Email me a code"));
  await screen.findByText("Continue");

  expect(complaints).toEqual([]);
});
