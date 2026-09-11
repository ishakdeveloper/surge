import { DevOutbox } from "@/dev/DevOutbox.js";
import { DevOutboxHttp } from "@/dev/DevOutboxHttp.js";
import { ConfigProvider, Effect, Layer } from "effect";
import { HttpRouter } from "effect/unstable/http";
import { afterAll, describe, expect, it } from "vitest";

/**
 * One outbox for every request, rather than its layer: a route's requirements
 * are supplied per request, and an outbox made for each request would be empty
 * by the time a test reads from it.
 */
const outbox = Effect.runSync(
  Effect.gen(function*() {
    return yield* DevOutbox;
  }).pipe(Effect.provide(DevOutbox.layer)),
);

const handlerFor = (enabled: boolean) =>
  HttpRouter.toWebHandler(
    DevOutboxHttp.pipe(
      HttpRouter.provideRequest(Layer.succeed(DevOutbox)(outbox)),
      Layer.provide(
        ConfigProvider.layer(
          ConfigProvider.fromEnv({ env: { AUTH_DEV_OUTBOX: String(enabled) } }),
        ),
      ),
    ),
    { disableLogger: true },
  );

const get = (handler: ReturnType<typeof handlerFor>, path: string) =>
  handler.handler(new Request(`http://localhost${path}`));

/**
 * The outbox serves sign-in codes to whoever asks, so the part that matters
 * most is that it is not there unless asked for. The rest is what the tests
 * and the end-to-end flows that read it rely on: a closed outbox and an open
 * one answer differently, and a code comes back for the address it was sent
 * to, however that address was capitalised.
 */
describe("the dev outbox", () => {
  const closed = handlerFor(false);
  const open = handlerFor(true);
  afterAll(async () => {
    await closed.dispose();
    await open.dispose();
  });

  it("does not exist unless AUTH_DEV_OUTBOX is true", async () => {
    Effect.runSync(outbox.record("closed@example.com", "Your code is 111111."));

    expect((await get(closed, "/dev/outbox?to=closed@example.com")).status).toBe(404);
    expect((await get(closed, "/dev/outbox")).status).toBe(404);
  });

  it("refuses a question with no address, which is how a test knows it is open", async () => {
    const response = await get(open, "/dev/outbox");

    expect(response.status).toBe(400);
  });

  it("says when nothing has been sent to an address", async () => {
    const response = await get(open, "/dev/outbox?to=nobody@example.com");

    expect(response.status).toBe(404);
    expect(await response.text()).toBe("Nothing has been sent there.");
  });

  it("serves the latest code sent to an address, whatever its case", async () => {
    Effect.runSync(outbox.record("Ada@Example.com", "Your Surge code is 123456."));
    Effect.runSync(outbox.record("ada@example.com", "Your Surge code is 654321."));

    const response = await get(open, "/dev/outbox?to=ADA%40example.com");

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({
      to: "ada@example.com",
      code: "654321",
      text: "Your Surge code is 654321.",
      atMs: expect.any(Number),
    });
  });
});
