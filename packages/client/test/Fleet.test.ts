import { AuthToken } from "@/AuthToken.js";
import { Fleet } from "@/Fleet.js";
import { SurgeApi } from "@/SurgeApi.js";
import { describe, expect, it } from "@effect/vitest";
import { VehicleId } from "@surge/domain/api/Primitives";
import { Effect, Layer } from "effect";
import { fakeHttp, type FakeHttpRequest, type FakeHttpResponse } from "./support/fake-http.js";

/**
 * Handing a document over, against a fake gateway and a fake bucket.
 *
 * The property worth testing is the one a type cannot state: the file goes
 * straight to the bucket, on a link signed for exactly that file, and the
 * caller's bearer token does not travel with it. A signature covers the
 * headers it was made with, so an Authorization header nobody signed is how a
 * bucket comes to refuse a driver's certificate for no reason they could
 * understand.
 */
const uploadUrl = "https://documents.surge.test/documents/drv-1/doc-1.pdf?X-Amz-Signature=abc";

const stored = {
  document: {
    id: "doc-1",
    driverId: "drv-1",
    vehicleId: "veh-1",
    kind: "DOCUMENT_KIND_INSURANCE",
    status: "DOCUMENT_STATUS_AWAITING_FILE",
    expiresAt: "",
    extracted: {
      status: "EXTRACTION_STATUS_NONE",
      insurer: "",
      policyNumber: "",
      expiresAt: "",
      quotes: [],
    },
    reviewNote: "",
    reviewedAt: "",
    createdAt: "2026-09-12T12:00:00Z",
    updatedAt: "2026-09-12T12:00:00Z",
  },
};

const gateway = (bucket: (request: FakeHttpRequest) => FakeHttpResponse) =>
  fakeHttp((request) => {
    if (request.method === "POST" && request.path === "/v1/fleet/documents/uploads") {
      return { body: { ...stored, uploadUrl, expiresAt: "2026-09-12T12:15:00Z" } };
    }
    if (request.method === "POST" && request.path === "/v1/fleet/documents/doc-1/finish") {
      return {
        body: { document: { ...stored.document, status: "DOCUMENT_STATUS_SUBMITTED" } },
      };
    }
    return bucket(request);
  });

const harness = (bucket: (request: FakeHttpRequest) => FakeHttpResponse) => {
  const http = gateway(bucket);
  const layer = Fleet.layer.pipe(
    Layer.provideMerge(SurgeApi.layer),
    Layer.provideMerge(Layer.mergeAll(
      http.layer,
      Layer.succeed(AuthToken)({
        get: Effect.succeed("a-token-nothing-here-verifies"),
        identity: Effect.die("no identity is needed to hand over a document"),
        invalidate: Effect.void,
      }),
    )),
  );
  return { http, layer };
};

const certificate = new Uint8Array([37, 80, 68, 70]);

describe("handing over a document", () => {
  it.effect("asks for a link, puts the file at it, and says it arrived", () =>
    Effect.gen(function*() {
      const { http, layer } = harness(() => ({ status: 200, body: {} }));

      const document = yield* Effect.gen(function*() {
        const fleet = yield* Fleet;
        return yield* fleet.hand({
          kind: "DOCUMENT_KIND_INSURANCE",
          vehicleId: VehicleId.make("veh-1"),
          contentType: "application/pdf",
          bytes: certificate,
        });
      }).pipe(Effect.provide(layer));

      expect(document.status).toBe("DOCUMENT_STATUS_SUBMITTED");

      const [asked, put, finished] = http.requests;
      expect(asked?.path).toBe("/v1/fleet/documents/uploads");
      expect(asked?.body).toMatchObject({
        kind: "DOCUMENT_KIND_INSURANCE",
        vehicleId: "veh-1",
        contentType: "application/pdf",
        // Quoted, because proto3 JSON sends every 64-bit number as a string —
        // the generated client encodes it, and this is what leaves the app.
        byteSize: "4",
      });

      // The bucket, not the gateway, and without the caller's token.
      expect(put?.method).toBe("PUT");
      expect(put?.path).toBe("/documents/drv-1/doc-1.pdf");
      expect(put?.authorization).toBeUndefined();

      // The gateway's own calls do carry it.
      expect(asked?.authorization).toBe("Bearer a-token-nothing-here-verifies");
      expect(finished?.path).toBe("/v1/fleet/documents/doc-1/finish");
    }));

  it.effect("keeps the document unfinished when the bucket refuses the file", () =>
    Effect.gen(function*() {
      const { http, layer } = harness(() => ({
        status: 403,
        body: { message: "signature expired" },
      }));

      const refused = yield* Effect.gen(function*() {
        const fleet = yield* Fleet;
        return yield* Effect.flip(fleet.hand({
          kind: "DOCUMENT_KIND_VOG",
          contentType: "application/pdf",
          bytes: certificate,
        }));
      }).pipe(Effect.provide(layer));

      expect(refused).toMatchObject({ _tag: "UploadRefused", status: 403 });
      // Nothing said it arrived, so nobody is asked to review a file that is
      // not there.
      expect(http.requests.some((request) => request.path.endsWith("/finish"))).toBe(false);
    }));
});
