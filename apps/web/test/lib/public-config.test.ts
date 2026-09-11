import { fromEnvironment, type PublicConfig, publicConfigScript } from "@/lib/public-config.js";
import { describe, expect, it } from "vitest";

/** A server's environment, as a deployment would set it. */
const env = {
  SURGE_API_URL: "https://api.surge.example",
  SURGE_WS_URL: "wss://api.surge.example/ws",
  AUTH_BASE_URL: "https://auth.surge.example",
  STRIPE_PUBLISHABLE_KEY: "pk_test_123",
} as const;

const config: PublicConfig = env;

/** What the browser's `window.__SURGE_CONFIG__` holds once the script has run. */
const parsed = (script: string): unknown =>
  JSON.parse(script.slice(script.indexOf("=") + 1)) as unknown;

describe("fromEnvironment", () => {
  it("takes each address from the server's environment", () => {
    expect(fromEnvironment(env)).toEqual(config);
  });

  it("treats an empty variable as unset and falls back to the build's value", () => {
    const resolved = fromEnvironment({ ...env, STRIPE_PUBLISHABLE_KEY: "" });

    expect(resolved.STRIPE_PUBLISHABLE_KEY).toBe(import.meta.env.VITE_STRIPE_PUBLISHABLE_KEY);
    expect(resolved.SURGE_API_URL).toBe(env.SURGE_API_URL);
  });
});

describe("publicConfigScript", () => {
  it("hands the browser exactly what the server resolved", () => {
    expect(parsed(publicConfigScript(config))).toEqual(config);
  });

  /**
   * The values are operator-set, not visitor-set, but a script that a stray
   * `</script>` in an environment variable can break out of is a script that
   * one typo in a deployment turns into markup injection.
   */
  it("cannot be closed early by a value containing a closing tag", () => {
    const hostile = { ...config, SURGE_API_URL: "https://x</script><script>alert(1)</script>" };
    const script = publicConfigScript(hostile);

    expect(script).not.toContain("</script");
    expect(parsed(script)).toEqual(hostile);
  });
});
