import { beforeEach, describe, expect, it, jest } from "@jest/globals";
import { fireEvent, screen, waitFor } from "@testing-library/react-native";
import SignIn from "../../src/app/(auth)/sign-in";
import { renderScreen } from "./render-screen";

/**
 * better-auth is the boundary here, and it speaks `fetch` — replaced for every
 * test in `setup.js`, before better-auth's client captured it. Each test here
 * answers as the auth service does when it accepts, and reads what the screen
 * asked of it.
 *
 * The dev outbox answers too, with the code it holds, and is not counted among
 * what the screen asked better-auth for: a development build's code step reads
 * it to show the code, and Jest's `__DEV__` is a development build.
 */
const fetchMock = jest.mocked(globalThis.fetch);
let calls: Array<{ readonly path: string; readonly body: unknown; }> = [];

const json = (body: unknown) =>
  new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" },
  });

beforeEach(() => {
  calls = [];
  fetchMock.mockImplementation(async (input, init) => {
    const url = new URL(
      typeof input === "string" ? input : input instanceof URL ? input.href : input.url,
    );
    if (url.pathname === "/dev/outbox") {
      return json({ to: url.searchParams.get("to"), code: "123456", text: "…", atMs: 1 });
    }
    calls.push({
      path: url.pathname,
      body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined,
    });
    return json({ status: true, success: true });
  });
});

describe("signing in with a code", () => {
  it("sends an email code, then verifies it with the role a new account gets", async () => {
    await renderScreen(<SignIn />, { routes: [] });

    await fireEvent.changeText(screen.getByTestId("field-email"), "ada@example.com");
    await fireEvent.press(screen.getByTestId("role-driver"));
    await fireEvent.press(screen.getByText("Email me a code"));

    await screen.findByText("Continue");
    expect(calls.at(-1)).toEqual({
      path: "/api/auth/email-otp/send-verification-otp",
      body: { email: "ada@example.com", type: "sign-in" },
    });

    await fireEvent.changeText(screen.getByTestId("field-code"), "123456");
    await fireEvent.press(screen.getByText("Continue"));

    await waitFor(() => {
      expect(calls.at(-1)).toEqual({
        path: "/api/auth/sign-in/email-otp",
        body: { email: "ada@example.com", otp: "123456", role: "driver" },
      });
    });
  });

  it("asks for the country code before texting anything", async () => {
    await renderScreen(<SignIn />, { routes: [] });

    await fireEvent.press(screen.getByTestId("method-phone"));
    await fireEvent.changeText(screen.getByTestId("field-phone"), "06 12345678");
    await fireEvent.press(screen.getByText("Text me a code"));

    await screen.findByText("Include the country code, like +31 6 12345678.");
    expect(calls).toEqual([]);
  });

  /** What lets a demo sign in by phone with no SMS provider behind it. */
  it("shows the code it texted, in a development build", async () => {
    await renderScreen(<SignIn />, { routes: [] });

    await fireEvent.press(screen.getByTestId("method-phone"));
    await fireEvent.changeText(screen.getByTestId("field-phone"), "+31 6 12345678");
    await fireEvent.press(screen.getByText("Text me a code"));

    await screen.findByText("123456");
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining("/dev/outbox?to=%2B31612345678"),
    );
  });
});
