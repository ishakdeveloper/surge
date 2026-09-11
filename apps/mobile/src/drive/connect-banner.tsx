import { colors } from "@/lib/theme.js";
import { useAtomSet } from "@effect/atom-react";
import { createAccountSession } from "@surge/common/atom/payment-atoms";
import * as React from "react";
import { View } from "react-native";
import { WebView, type WebViewMessageEvent } from "react-native-webview";

/**
 * Stripe's notification banner for a connected account — a document to upload,
 * a detail to confirm — which Stripe requires wherever a connected account's
 * health is shown. The web renders it with `@stripe/react-connect-js`.
 *
 * There is no native equivalent, so it is Connect.js in a WebView. The page
 * asks for an account session by posting `"session"`; the app fetches one
 * through the shared `createAccountSession` and hands the client secret back
 * in. The secret goes from our server to Stripe's script and nowhere else.
 *
 * The view is sized to what the banner draws, which is nothing at all when
 * Stripe has nothing to say — the usual case, and the reason this is not a
 * fixed-height box.
 */
const page = (publishableKey: string) =>
  `<!doctype html><html><head>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>html,body{margin:0;background:transparent}</style>
</head><body><div id="banner"></div>
<script src="https://connect-js.stripe.com/v1.0/connect.js" async></script>
<script>
  var waiting = [];
  var send = function (message) { window.ReactNativeWebView.postMessage(message); };
  window.surgeSession = function (secret) {
    var resolve = waiting; waiting = [];
    resolve.forEach(function (done) { done(secret); });
  };
  window.StripeConnect = window.StripeConnect || {};
  StripeConnect.onLoad = function () {
    var connect = StripeConnect.init({
      publishableKey: ${JSON.stringify(publishableKey)},
      fetchClientSecret: function () {
        return new Promise(function (resolve) { waiting.push(resolve); send("session"); });
      },
      appearance: { variables: {
        colorBackground: ${JSON.stringify(colors.card)},
        colorText: ${JSON.stringify(colors.foreground)},
        colorPrimary: ${JSON.stringify(colors.primary)},
        fontFamily: "-apple-system, system-ui, sans-serif"
      } }
    });
    document.getElementById("banner").appendChild(connect.create("notification-banner"));
    new ResizeObserver(function () {
      send("height:" + Math.ceil(document.body.scrollHeight));
    }).observe(document.body);
  };
</script></body></html>`;

export const ConnectBanner = (props: { readonly publishableKey: string; }) => {
  const createSession = useAtomSet(createAccountSession, { mode: "promise" });
  const view = React.useRef<WebView>(null);
  // One point rather than zero: a WebView with no size may not load at all.
  const [height, setHeight] = React.useState(1);

  const answer = (secret: string | null) => {
    view.current?.injectJavaScript(`window.surgeSession(${JSON.stringify(secret)}); true;`);
  };

  const onMessage = (event: WebViewMessageEvent) => {
    const message = event.nativeEvent.data;
    if (message === "session") {
      // A refused session leaves the banner empty, which is what Connect.js
      // shows for an account it cannot load; the rest of the screen stands.
      void createSession().then(answer, () => {
        answer(null);
      });
    } else if (message.startsWith("height:")) {
      setHeight(Math.max(1, Number(message.slice("height:".length))));
    }
  };

  return (
    <View style={{ height }} className="overflow-hidden rounded-lg">
      <WebView
        ref={view}
        // A localhost origin, which Stripe accepts for embedded components in
        // test mode; the page itself is inline and loads only Stripe's script.
        source={{ html: page(props.publishableKey), baseUrl: "https://localhost" }}
        originWhitelist={["https://*"]}
        onMessage={onMessage}
        scrollEnabled={false}
        style={{ backgroundColor: "transparent" }}
      />
    </View>
  );
};
