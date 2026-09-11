import { devHost, serviceUrlsFor } from "@/lib/service-urls.js";
import { describe, expect, it } from "vitest";

const unset = { apiUrl: undefined, wsUrl: undefined, authUrl: undefined };

describe("service addresses", () => {
  it("borrows the dev server's host, so a phone on the network reaches the laptop", () => {
    expect(serviceUrlsFor(unset, "192.168.1.20:8081")).toEqual({
      SURGE_API_URL: "http://192.168.1.20:8100",
      SURGE_WS_URL: "ws://192.168.1.20:8100/ws",
      AUTH_BASE_URL: "http://192.168.1.20:3200",
    });
  });

  it("uses a configured address verbatim, one at a time", () => {
    const urls = serviceUrlsFor(
      { ...unset, apiUrl: "https://api.example.com" },
      "192.168.1.20:8081",
    );

    expect(urls.SURGE_API_URL).toBe("https://api.example.com");
    expect(urls.SURGE_WS_URL).toBe("ws://192.168.1.20:8100/ws");
  });

  it("falls back to localhost when no dev server served the bundle", () => {
    expect(serviceUrlsFor(unset, undefined).SURGE_API_URL).toBe("http://localhost:8100");
  });

  it("takes only the host from a hostUri, whatever follows it", () => {
    expect(devHost("10.0.2.2:8081")).toBe("10.0.2.2");
    expect(devHost("abc-anonymous-8081.exp.direct/path")).toBe("abc-anonymous-8081.exp.direct");
    expect(devHost("")).toBe("localhost");
  });
});
