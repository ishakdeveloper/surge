import { describe, expect, it, jest } from "@jest/globals";
import type { FakeRequest } from "@surge/common/testing/fake-platform";
import { act, screen, waitFor } from "@testing-library/react-native";
import type { WebViewMessageEvent, WebViewProps } from "react-native-webview";
import { ConnectBanner } from "../../src/drive/connect-banner";
import { renderScreen } from "./render-screen";

/**
 * The WebView is the boundary: what runs inside it is Stripe's script, which a
 * screen test cannot load. This stand-in keeps the props the banner hands it
 * and the scripts the banner injects back, so a test can play Connect.js's
 * side of the conversation — asking for a session, reporting a height.
 *
 * Named `mock…` so Jest lets the hoisted factory reach it.
 */
const mockWebView: { props: WebViewProps | undefined; injected: Array<string>; } = {
  props: undefined,
  injected: [],
};

jest.mock("react-native-webview", () => {
  const React: typeof import("react") = require("react");
  return {
    WebView: React.forwardRef((props: WebViewProps, ref) => {
      mockWebView.props = props;
      React.useImperativeHandle(ref, () => ({
        injectJavaScript: (script: string) => {
          mockWebView.injected.push(script);
        },
      }));
      return null;
    }),
  };
});

const KEY = "pk_test_banner";
const SESSIONS = "/v1/payments/account-sessions";

/** What Connect.js posts to the app, delivered the way the WebView would. */
const fromStripe = (data: string) =>
  act(() => {
    mockWebView.props?.onMessage?.({ nativeEvent: { data } } as WebViewMessageEvent);
  });

const html = () => {
  const source = mockWebView.props?.source;
  return source !== undefined && "html" in source ? source.html : "";
};

describe("Stripe's notification banner", () => {
  it("loads Stripe's script with the app's publishable key, and asks for nothing yet", async () => {
    mockWebView.injected = [];
    await renderScreen(<ConnectBanner publishableKey={KEY} />, { routes: [] });

    expect(html()).toContain("https://connect-js.stripe.com/v1.0/connect.js");
    expect(html()).toContain(`publishableKey: "${KEY}"`);
    expect(mockWebView.injected).toEqual([]);
  });

  it("answers Connect.js's request for a session with one from the payments service", async () => {
    mockWebView.injected = [];
    const requests: Array<FakeRequest> = [];
    await renderScreen(<ConnectBanner publishableKey={KEY} />, {
      routes: [{ method: "POST", path: SESSIONS, body: { clientSecret: "accs_secret_banner" } }],
      requests,
    });

    await fromStripe("session");

    await waitFor(() => {
      expect(mockWebView.injected).toEqual([`window.surgeSession("accs_secret_banner"); true;`]);
    });
    // Through the shared client, as the signed-in driver.
    expect(requests).toEqual([
      { method: "POST", path: SESSIONS, authorization: "Bearer fake-token", body: {} },
    ]);
  });

  it("answers null when a session is refused, so the banner stays empty rather than waiting", async () => {
    mockWebView.injected = [];
    await renderScreen(<ConnectBanner publishableKey={KEY} />, {
      routes: [{
        method: "POST",
        path: SESSIONS,
        status: 400,
        body: { error: { code: "failed_precondition", message: "set up payouts first" } },
      }],
    });

    await fromStripe("session");

    await waitFor(() => {
      expect(mockWebView.injected).toEqual(["window.surgeSession(null); true;"]);
    });
  });

  it("takes the height of what Stripe draws, which is nothing until it has something to say", async () => {
    await renderScreen(<ConnectBanner publishableKey={KEY} />, { routes: [] });
    expect(JSON.stringify(screen.toJSON())).toContain(`"height":1`);

    await fromStripe("height:84");

    expect(JSON.stringify(screen.toJSON())).toContain(`"height":84`);
  });
});
