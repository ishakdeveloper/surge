import type { DocumentId, VehicleId } from "@surge/domain/api/Primitives";
import type { Document, DocumentKind } from "@surge/domain/fleet/Fleet";
import { Context, Effect, Layer, Schema } from "effect";
import { HttpClient, HttpClientRequest } from "effect/unstable/http";
import { SurgeApi, type SurgeApiClient } from "./SurgeApi.js";

/**
 * Handing a document over, in the three steps the fleet asks for.
 *
 * The file never passes through the API: the fleet returns a link signed for
 * exactly this file, the app puts the bytes straight at the bucket, and then
 * says it arrived. That is the whole reason this exists as a service rather
 * than three calls at a call site — the three have to happen in order, and the
 * middle one must not carry the caller's token.
 */

/** The bucket refused the file. */
export class UploadRefused extends Schema.TaggedError<UploadRefused>()("UploadRefused", {
  status: Schema.Number,
  reason: Schema.String,
}) {
  override get message(): string {
    return `the documents bucket refused the file (${this.status})`;
  }
}

type FleetClient = SurgeApiClient["fleet"];

export type UploadError =
  | Effect.Error<ReturnType<FleetClient["startUpload"]>>
  | Effect.Error<ReturnType<FleetClient["finishUpload"]>>
  | UploadRefused;

/** A file to hand over, as bytes: each app turns its own picker's result into these. */
export interface Handover {
  readonly kind: DocumentKind;
  /** The car an insurance certificate or registration belongs to. */
  readonly vehicleId?: VehicleId;
  /** image/jpeg, image/png, image/webp or application/pdf. */
  readonly contentType: string;
  readonly bytes: Uint8Array;
}

export interface FleetService {
  /**
   * Ask for a link, put the file at it, and say it arrived. The document comes
   * back submitted, which is when a reviewer sees it.
   */
  readonly hand: (handover: Handover) => Effect.Effect<Document, UploadError>;
}

export class Fleet extends Context.Service<Fleet, FleetService>()("Fleet") {
  static layer: Layer.Layer<Fleet, never, SurgeApi | HttpClient.HttpClient> = Layer.effect(Fleet)(
    Effect.gen(function*() {
      const api = yield* SurgeApi;
      // The platform's own client, deliberately not the API's: a presigned
      // link is signed for a particular request, and an Authorization header
      // the signature does not cover is how a bucket comes to refuse a file
      // for no reason a driver could understand.
      const http = yield* HttpClient.HttpClient;

      const hand = Effect.fnUntraced(function*(handover: Handover) {
        const started = yield* api.fleet.startUpload({
          payload: {
            kind: handover.kind,
            vehicleId: (handover.vehicleId ?? "") as VehicleId,
            contentType: handover.contentType,
            byteSize: handover.bytes.length,
          },
        });

        const response = yield* http.execute(
          HttpClientRequest.put(started.uploadUrl).pipe(
            // Exactly the type the link was signed for. Anything else is a
            // signature over a request nobody made.
            HttpClientRequest.bodyUint8Array(handover.bytes, handover.contentType),
          ),
        ).pipe(
          Effect.catchTag(
            "HttpClientError",
            (error) => Effect.fail(new UploadRefused({ status: 0, reason: error.message })),
          ),
        );
        if (response.status < 200 || response.status >= 300) {
          return yield* Effect.fail(
            new UploadRefused({ status: response.status, reason: "the bucket refused it" }),
          );
        }

        const finished = yield* api.fleet.finishUpload({
          params: { documentId: started.document.id as DocumentId },
          payload: {},
        });
        return finished.document;
      });

      return { hand };
    }),
  );
}
