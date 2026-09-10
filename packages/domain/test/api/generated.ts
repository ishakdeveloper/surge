/**
 * GENERATED — do not edit. `make proto` regenerates it.
 *
 * `@effect/openapi-generator` reading the Swagger 2.0 document grpc-gateway
 * emits from `proto/trip.proto`. It is deliberately not the client:
 * `src/api/SurgeApi.ts` is, and it is hand-written because this output cannot
 * express the three things that make a client worth having — proto3 leaves
 * every field optional in Swagger, ids arrive as bare strings rather than
 * branded, and `int64` stays the quoted string it is on the wire.
 *
 * What it is for is `ApiContract.test.ts`, which diffs the two. A path, method,
 * parameter or field that moves in the proto appears here on the next
 * `make proto`, and the test fails until the hand-written contract catches up.
 */
import * as Schema from "effect/Schema";
import { HttpApi, HttpApiEndpoint, HttpApiGroup, OpenApi } from "effect/unstable/httpapi";
// non-recursive definitions
export type V1ListTripsResponse = {
  readonly "trips"?: ReadonlyArray<
    {
      readonly "id"?: string;
      readonly "riderId"?: string;
      readonly "driverId"?: string;
      readonly "status"?:
        | "TRIP_STATUS_UNSPECIFIED"
        | "TRIP_STATUS_REQUESTED"
        | "TRIP_STATUS_OFFERED"
        | "TRIP_STATUS_ACCEPTED"
        | "TRIP_STATUS_ARRIVED"
        | "TRIP_STATUS_IN_PROGRESS"
        | "TRIP_STATUS_COMPLETED"
        | "TRIP_STATUS_CANCELLED"
        | "TRIP_STATUS_UNMATCHED";
      readonly "pickup"?: { readonly "lat"?: number; readonly "lng"?: number; };
      readonly "dropoff"?: { readonly "lat"?: number; readonly "lng"?: number; };
      readonly "route"?: {
        readonly "polyline6"?: string;
        readonly "meters"?: number;
        readonly "seconds"?: string;
      };
      readonly "totalCents"?: string;
      readonly "createdAt"?: string;
      readonly "updatedAt"?: string;
    }
  >;
  readonly "nextPageToken"?: string;
};
export const V1ListTripsResponse = Schema.Struct({
  "trips": Schema.optionalKey(Schema.Array(Schema.Struct({
    "id": Schema.optionalKey(Schema.String),
    "riderId": Schema.optionalKey(Schema.String),
    "driverId": Schema.optionalKey(Schema.String),
    "status": Schema.optionalKey(
      Schema.Literals([
        "TRIP_STATUS_UNSPECIFIED",
        "TRIP_STATUS_REQUESTED",
        "TRIP_STATUS_OFFERED",
        "TRIP_STATUS_ACCEPTED",
        "TRIP_STATUS_ARRIVED",
        "TRIP_STATUS_IN_PROGRESS",
        "TRIP_STATUS_COMPLETED",
        "TRIP_STATUS_CANCELLED",
        "TRIP_STATUS_UNMATCHED",
      ]).annotate({
        "description":
          "TripStatus is the state machine, and the wire is the place it is written\ndown once. UNSPECIFIED is reserved by convention so an unset field is not\nsilently a valid state.\n\n - TRIP_STATUS_UNMATCHED: No driver was found before the request expired. Distinct from cancelled:\nnobody chose it, and the rider should be offered a retry rather than an\napology.",
        "default": "TRIP_STATUS_UNSPECIFIED",
      }),
    ),
    "pickup": Schema.optionalKey(
      Schema.Struct({
        "lat": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
        "lng": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
      }).annotate({
        "description":
          "Coordinate is WGS84. `lng` rather than `lon`, matching H3, Valhalla and the\nrest of this codebase — one spelling everywhere is worth more than any\nargument about which.",
      }),
    ),
    "dropoff": Schema.optionalKey(
      Schema.Struct({
        "lat": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
        "lng": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
      }).annotate({
        "description":
          "Coordinate is WGS84. `lng` rather than `lon`, matching H3, Valhalla and the\nrest of this codebase — one spelling everywhere is worth more than any\nargument about which.",
      }),
    ),
    "route": Schema.optionalKey(
      Schema.Struct({
        "polyline6": Schema.optionalKey(
          Schema.String.annotate({
            "description":
              "Polyline encoded at precision 6, as Valhalla emits it. Not a repeated\nCoordinate: a city route is hundreds of points, and the encoded form is\nroughly a tenth the size on a connection a phone is paying for.",
          }),
        ),
        "meters": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
        "seconds": Schema.optionalKey(Schema.String.annotate({ "format": "int64" })),
      }).annotate({ "description": "Route is a driveable path with its cost." }),
    ),
    "totalCents": Schema.optionalKey(Schema.String.annotate({ "format": "int64" })),
    "createdAt": Schema.optionalKey(Schema.String.annotate({ "format": "date-time" })),
    "updatedAt": Schema.optionalKey(Schema.String.annotate({ "format": "date-time" })),
  }))),
  "nextPageToken": Schema.optionalKey(
    Schema.String.annotate({ "description": "Empty when there are no more." }),
  ),
}).annotate({ "identifier": "v1ListTripsResponse" });
export type RpcStatus = {
  readonly "code"?: number;
  readonly "message"?: string;
  readonly "details"?: ReadonlyArray<
    { readonly "@type"?: string; readonly [x: string]: Schema.Json; }
  >;
};
export const RpcStatus = Schema.Struct({
  "code": Schema.optionalKey(
    Schema.Number.annotate({ "format": "int32" }).check(
      Schema.isInt().annotate({ "expected": "an integer" }),
    ),
  ),
  "message": Schema.optionalKey(Schema.String),
  "details": Schema.optionalKey(
    Schema.Array(
      Schema.StructWithRest(Schema.Struct({ "@type": Schema.optionalKey(Schema.String) }), [
        Schema.Record(Schema.String, Schema.Json.annotate({ "expected": "JSON value" })),
      ]),
    ),
  ),
}).annotate({ "identifier": "rpcStatus" });
export type V1CreateTripRequest = {
  readonly "fareId"?: string;
  readonly "idempotencyKey"?: string;
};
export const V1CreateTripRequest = Schema.Struct({
  "fareId": Schema.optionalKey(Schema.String),
  "idempotencyKey": Schema.optionalKey(
    Schema.String.annotate({
      "description":
        "Generated client-side, so a retry over a flaky connection is recognised\nrather than booked twice. Over REST this also arrives as an\n`Idempotency-Key` header, which the gateway copies into the field.",
    }),
  ),
}).annotate({ "identifier": "v1CreateTripRequest" });
export type V1CreateTripResponse = {
  readonly "trip"?: {
    readonly "id"?: string;
    readonly "riderId"?: string;
    readonly "driverId"?: string;
    readonly "status"?:
      | "TRIP_STATUS_UNSPECIFIED"
      | "TRIP_STATUS_REQUESTED"
      | "TRIP_STATUS_OFFERED"
      | "TRIP_STATUS_ACCEPTED"
      | "TRIP_STATUS_ARRIVED"
      | "TRIP_STATUS_IN_PROGRESS"
      | "TRIP_STATUS_COMPLETED"
      | "TRIP_STATUS_CANCELLED"
      | "TRIP_STATUS_UNMATCHED";
    readonly "pickup"?: { readonly "lat"?: number; readonly "lng"?: number; };
    readonly "dropoff"?: { readonly "lat"?: number; readonly "lng"?: number; };
    readonly "route"?: {
      readonly "polyline6"?: string;
      readonly "meters"?: number;
      readonly "seconds"?: string;
    };
    readonly "totalCents"?: string;
    readonly "createdAt"?: string;
    readonly "updatedAt"?: string;
  };
};
export const V1CreateTripResponse = Schema.Struct({
  "trip": Schema.optionalKey(Schema.Struct({
    "id": Schema.optionalKey(Schema.String),
    "riderId": Schema.optionalKey(Schema.String),
    "driverId": Schema.optionalKey(Schema.String),
    "status": Schema.optionalKey(
      Schema.Literals([
        "TRIP_STATUS_UNSPECIFIED",
        "TRIP_STATUS_REQUESTED",
        "TRIP_STATUS_OFFERED",
        "TRIP_STATUS_ACCEPTED",
        "TRIP_STATUS_ARRIVED",
        "TRIP_STATUS_IN_PROGRESS",
        "TRIP_STATUS_COMPLETED",
        "TRIP_STATUS_CANCELLED",
        "TRIP_STATUS_UNMATCHED",
      ]).annotate({
        "description":
          "TripStatus is the state machine, and the wire is the place it is written\ndown once. UNSPECIFIED is reserved by convention so an unset field is not\nsilently a valid state.\n\n - TRIP_STATUS_UNMATCHED: No driver was found before the request expired. Distinct from cancelled:\nnobody chose it, and the rider should be offered a retry rather than an\napology.",
        "default": "TRIP_STATUS_UNSPECIFIED",
      }),
    ),
    "pickup": Schema.optionalKey(
      Schema.Struct({
        "lat": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
        "lng": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
      }).annotate({
        "description":
          "Coordinate is WGS84. `lng` rather than `lon`, matching H3, Valhalla and the\nrest of this codebase — one spelling everywhere is worth more than any\nargument about which.",
      }),
    ),
    "dropoff": Schema.optionalKey(
      Schema.Struct({
        "lat": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
        "lng": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
      }).annotate({
        "description":
          "Coordinate is WGS84. `lng` rather than `lon`, matching H3, Valhalla and the\nrest of this codebase — one spelling everywhere is worth more than any\nargument about which.",
      }),
    ),
    "route": Schema.optionalKey(
      Schema.Struct({
        "polyline6": Schema.optionalKey(
          Schema.String.annotate({
            "description":
              "Polyline encoded at precision 6, as Valhalla emits it. Not a repeated\nCoordinate: a city route is hundreds of points, and the encoded form is\nroughly a tenth the size on a connection a phone is paying for.",
          }),
        ),
        "meters": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
        "seconds": Schema.optionalKey(Schema.String.annotate({ "format": "int64" })),
      }).annotate({ "description": "Route is a driveable path with its cost." }),
    ),
    "totalCents": Schema.optionalKey(Schema.String.annotate({ "format": "int64" })),
    "createdAt": Schema.optionalKey(Schema.String.annotate({ "format": "date-time" })),
    "updatedAt": Schema.optionalKey(Schema.String.annotate({ "format": "date-time" })),
  })),
}).annotate({ "identifier": "v1CreateTripResponse" });
export type V1GetTripResponse = {
  readonly "trip"?: {
    readonly "id"?: string;
    readonly "riderId"?: string;
    readonly "driverId"?: string;
    readonly "status"?:
      | "TRIP_STATUS_UNSPECIFIED"
      | "TRIP_STATUS_REQUESTED"
      | "TRIP_STATUS_OFFERED"
      | "TRIP_STATUS_ACCEPTED"
      | "TRIP_STATUS_ARRIVED"
      | "TRIP_STATUS_IN_PROGRESS"
      | "TRIP_STATUS_COMPLETED"
      | "TRIP_STATUS_CANCELLED"
      | "TRIP_STATUS_UNMATCHED";
    readonly "pickup"?: { readonly "lat"?: number; readonly "lng"?: number; };
    readonly "dropoff"?: { readonly "lat"?: number; readonly "lng"?: number; };
    readonly "route"?: {
      readonly "polyline6"?: string;
      readonly "meters"?: number;
      readonly "seconds"?: string;
    };
    readonly "totalCents"?: string;
    readonly "createdAt"?: string;
    readonly "updatedAt"?: string;
  };
};
export const V1GetTripResponse = Schema.Struct({
  "trip": Schema.optionalKey(Schema.Struct({
    "id": Schema.optionalKey(Schema.String),
    "riderId": Schema.optionalKey(Schema.String),
    "driverId": Schema.optionalKey(Schema.String),
    "status": Schema.optionalKey(
      Schema.Literals([
        "TRIP_STATUS_UNSPECIFIED",
        "TRIP_STATUS_REQUESTED",
        "TRIP_STATUS_OFFERED",
        "TRIP_STATUS_ACCEPTED",
        "TRIP_STATUS_ARRIVED",
        "TRIP_STATUS_IN_PROGRESS",
        "TRIP_STATUS_COMPLETED",
        "TRIP_STATUS_CANCELLED",
        "TRIP_STATUS_UNMATCHED",
      ]).annotate({
        "description":
          "TripStatus is the state machine, and the wire is the place it is written\ndown once. UNSPECIFIED is reserved by convention so an unset field is not\nsilently a valid state.\n\n - TRIP_STATUS_UNMATCHED: No driver was found before the request expired. Distinct from cancelled:\nnobody chose it, and the rider should be offered a retry rather than an\napology.",
        "default": "TRIP_STATUS_UNSPECIFIED",
      }),
    ),
    "pickup": Schema.optionalKey(
      Schema.Struct({
        "lat": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
        "lng": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
      }).annotate({
        "description":
          "Coordinate is WGS84. `lng` rather than `lon`, matching H3, Valhalla and the\nrest of this codebase — one spelling everywhere is worth more than any\nargument about which.",
      }),
    ),
    "dropoff": Schema.optionalKey(
      Schema.Struct({
        "lat": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
        "lng": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
      }).annotate({
        "description":
          "Coordinate is WGS84. `lng` rather than `lon`, matching H3, Valhalla and the\nrest of this codebase — one spelling everywhere is worth more than any\nargument about which.",
      }),
    ),
    "route": Schema.optionalKey(
      Schema.Struct({
        "polyline6": Schema.optionalKey(
          Schema.String.annotate({
            "description":
              "Polyline encoded at precision 6, as Valhalla emits it. Not a repeated\nCoordinate: a city route is hundreds of points, and the encoded form is\nroughly a tenth the size on a connection a phone is paying for.",
          }),
        ),
        "meters": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
        "seconds": Schema.optionalKey(Schema.String.annotate({ "format": "int64" })),
      }).annotate({ "description": "Route is a driveable path with its cost." }),
    ),
    "totalCents": Schema.optionalKey(Schema.String.annotate({ "format": "int64" })),
    "createdAt": Schema.optionalKey(Schema.String.annotate({ "format": "date-time" })),
    "updatedAt": Schema.optionalKey(Schema.String.annotate({ "format": "date-time" })),
  })),
}).annotate({ "identifier": "v1GetTripResponse" });
export type TripServiceCancelTripBody = { readonly "reason"?: string; };
export const TripServiceCancelTripBody = Schema.Struct({
  "reason": Schema.optionalKey(Schema.String),
}).annotate({ "identifier": "TripServiceCancelTripBody" });
export type V1CancelTripResponse = {
  readonly "trip"?: {
    readonly "id"?: string;
    readonly "riderId"?: string;
    readonly "driverId"?: string;
    readonly "status"?:
      | "TRIP_STATUS_UNSPECIFIED"
      | "TRIP_STATUS_REQUESTED"
      | "TRIP_STATUS_OFFERED"
      | "TRIP_STATUS_ACCEPTED"
      | "TRIP_STATUS_ARRIVED"
      | "TRIP_STATUS_IN_PROGRESS"
      | "TRIP_STATUS_COMPLETED"
      | "TRIP_STATUS_CANCELLED"
      | "TRIP_STATUS_UNMATCHED";
    readonly "pickup"?: { readonly "lat"?: number; readonly "lng"?: number; };
    readonly "dropoff"?: { readonly "lat"?: number; readonly "lng"?: number; };
    readonly "route"?: {
      readonly "polyline6"?: string;
      readonly "meters"?: number;
      readonly "seconds"?: string;
    };
    readonly "totalCents"?: string;
    readonly "createdAt"?: string;
    readonly "updatedAt"?: string;
  };
};
export const V1CancelTripResponse = Schema.Struct({
  "trip": Schema.optionalKey(Schema.Struct({
    "id": Schema.optionalKey(Schema.String),
    "riderId": Schema.optionalKey(Schema.String),
    "driverId": Schema.optionalKey(Schema.String),
    "status": Schema.optionalKey(
      Schema.Literals([
        "TRIP_STATUS_UNSPECIFIED",
        "TRIP_STATUS_REQUESTED",
        "TRIP_STATUS_OFFERED",
        "TRIP_STATUS_ACCEPTED",
        "TRIP_STATUS_ARRIVED",
        "TRIP_STATUS_IN_PROGRESS",
        "TRIP_STATUS_COMPLETED",
        "TRIP_STATUS_CANCELLED",
        "TRIP_STATUS_UNMATCHED",
      ]).annotate({
        "description":
          "TripStatus is the state machine, and the wire is the place it is written\ndown once. UNSPECIFIED is reserved by convention so an unset field is not\nsilently a valid state.\n\n - TRIP_STATUS_UNMATCHED: No driver was found before the request expired. Distinct from cancelled:\nnobody chose it, and the rider should be offered a retry rather than an\napology.",
        "default": "TRIP_STATUS_UNSPECIFIED",
      }),
    ),
    "pickup": Schema.optionalKey(
      Schema.Struct({
        "lat": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
        "lng": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
      }).annotate({
        "description":
          "Coordinate is WGS84. `lng` rather than `lon`, matching H3, Valhalla and the\nrest of this codebase — one spelling everywhere is worth more than any\nargument about which.",
      }),
    ),
    "dropoff": Schema.optionalKey(
      Schema.Struct({
        "lat": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
        "lng": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
      }).annotate({
        "description":
          "Coordinate is WGS84. `lng` rather than `lon`, matching H3, Valhalla and the\nrest of this codebase — one spelling everywhere is worth more than any\nargument about which.",
      }),
    ),
    "route": Schema.optionalKey(
      Schema.Struct({
        "polyline6": Schema.optionalKey(
          Schema.String.annotate({
            "description":
              "Polyline encoded at precision 6, as Valhalla emits it. Not a repeated\nCoordinate: a city route is hundreds of points, and the encoded form is\nroughly a tenth the size on a connection a phone is paying for.",
          }),
        ),
        "meters": Schema.optionalKey(
          Schema.Number.annotate({ "format": "double" }).check(
            Schema.isFinite().annotate({ "expected": "a finite number" }),
          ),
        ),
        "seconds": Schema.optionalKey(Schema.String.annotate({ "format": "int64" })),
      }).annotate({ "description": "Route is a driveable path with its cost." }),
    ),
    "totalCents": Schema.optionalKey(Schema.String.annotate({ "format": "int64" })),
    "createdAt": Schema.optionalKey(Schema.String.annotate({ "format": "date-time" })),
    "updatedAt": Schema.optionalKey(Schema.String.annotate({ "format": "date-time" })),
  })),
}).annotate({ "identifier": "v1CancelTripResponse" });
export type V1PreviewTripRequest = {
  readonly "pickup"?: { readonly "lat"?: number; readonly "lng"?: number; };
  readonly "dropoff"?: { readonly "lat"?: number; readonly "lng"?: number; };
};
export const V1PreviewTripRequest = Schema.Struct({
  "pickup": Schema.optionalKey(
    Schema.Struct({
      "lat": Schema.optionalKey(
        Schema.Number.annotate({ "format": "double" }).check(
          Schema.isFinite().annotate({ "expected": "a finite number" }),
        ),
      ),
      "lng": Schema.optionalKey(
        Schema.Number.annotate({ "format": "double" }).check(
          Schema.isFinite().annotate({ "expected": "a finite number" }),
        ),
      ),
    }).annotate({
      "description":
        "Coordinate is WGS84. `lng` rather than `lon`, matching H3, Valhalla and the\nrest of this codebase — one spelling everywhere is worth more than any\nargument about which.",
    }),
  ),
  "dropoff": Schema.optionalKey(
    Schema.Struct({
      "lat": Schema.optionalKey(
        Schema.Number.annotate({ "format": "double" }).check(
          Schema.isFinite().annotate({ "expected": "a finite number" }),
        ),
      ),
      "lng": Schema.optionalKey(
        Schema.Number.annotate({ "format": "double" }).check(
          Schema.isFinite().annotate({ "expected": "a finite number" }),
        ),
      ),
    }).annotate({
      "description":
        "Coordinate is WGS84. `lng` rather than `lon`, matching H3, Valhalla and the\nrest of this codebase — one spelling everywhere is worth more than any\nargument about which.",
    }),
  ),
}).annotate({
  "description":
    "The caller is NOT a field on any request.\n\nIt arrives as gRPC metadata, put there by the gateway from a verified token.\nThis matters more now that the REST body maps straight onto the message: a\n`rider_id` field would be client-supplied, and a client that can name the\nrider can quote, book and read rides as somebody else.",
  "identifier": "v1PreviewTripRequest",
});
export type V1PreviewTripResponse = {
  readonly "fares"?: ReadonlyArray<
    {
      readonly "fareId"?: string;
      readonly "packageSlug"?: string;
      readonly "totalCents"?: string;
      readonly "surgeMultiplier"?: number;
      readonly "expiresAt"?: string;
    }
  >;
  readonly "route"?: {
    readonly "polyline6"?: string;
    readonly "meters"?: number;
    readonly "seconds"?: string;
  };
};
export const V1PreviewTripResponse = Schema.Struct({
  "fares": Schema.optionalKey(
    Schema.Array(Schema.Struct({
      "fareId": Schema.optionalKey(Schema.String),
      "packageSlug": Schema.optionalKey(
        Schema.String.annotate({
          "description":
            "Sedan, van and so on. A string rather than an enum because the catalogue is\noperator configuration, not a protocol change.",
        }),
      ),
      "totalCents": Schema.optionalKey(Schema.String.annotate({ "format": "int64" })),
      "surgeMultiplier": Schema.optionalKey(
        Schema.Number.annotate({
          "description":
            "The multiplier applied for demand in the pickup cell at quote time. Carried\nexplicitly so a rider can be shown why a fare is what it is.",
          "format": "double",
        }).check(Schema.isFinite().annotate({ "expected": "a finite number" })),
      ),
      "expiresAt": Schema.optionalKey(Schema.String.annotate({ "format": "date-time" })),
    })).annotate({ "description": "Quotes for each vehicle class, priced from the same route." }),
  ),
  "route": Schema.optionalKey(
    Schema.Struct({
      "polyline6": Schema.optionalKey(
        Schema.String.annotate({
          "description":
            "Polyline encoded at precision 6, as Valhalla emits it. Not a repeated\nCoordinate: a city route is hundreds of points, and the encoded form is\nroughly a tenth the size on a connection a phone is paying for.",
        }),
      ),
      "meters": Schema.optionalKey(
        Schema.Number.annotate({ "format": "double" }).check(
          Schema.isFinite().annotate({ "expected": "a finite number" }),
        ),
      ),
      "seconds": Schema.optionalKey(Schema.String.annotate({ "format": "int64" })),
    }).annotate({ "description": "Route is a driveable path with its cost." }),
  ),
}).annotate({ "identifier": "v1PreviewTripResponse" });
// schemas
export type TripServiceListTripsParams = {
  readonly "pageSize"?: number;
  readonly "pageToken"?: string;
  readonly "status"?: string;
};
export const TripServiceListTripsParams = Schema.Struct({
  "pageSize": Schema.optionalKey(
    Schema.Number.annotate({ "format": "int32" }).check(
      Schema.isInt().annotate({ "expected": "an integer" }),
    ),
  ),
  "pageToken": Schema.optionalKey(Schema.String),
  "status": Schema.optionalKey(Schema.String),
});
export type TripServiceListTripsQuery = {
  readonly "pageSize"?: number;
  readonly "pageToken"?: string;
  readonly "status"?: string;
};
export const TripServiceListTripsQuery = Schema.Struct({
  "pageSize": Schema.optionalKey(
    Schema.Number.annotate({ "format": "int32" }).check(
      Schema.isInt().annotate({ "expected": "an integer" }),
    ),
  ),
  "pageToken": Schema.optionalKey(Schema.String),
  "status": Schema.optionalKey(Schema.String),
});
export type TripServiceListTrips200 = V1ListTripsResponse;
export const TripServiceListTrips200 = V1ListTripsResponse;
export type TripServiceListTripsdefault = RpcStatus;
export const TripServiceListTripsdefault = RpcStatus;
export type TripServiceCreateTripRequestJson = V1CreateTripRequest;
export const TripServiceCreateTripRequestJson = V1CreateTripRequest;
export type TripServiceCreateTrip200 = V1CreateTripResponse;
export const TripServiceCreateTrip200 = V1CreateTripResponse;
export type TripServiceCreateTripdefault = RpcStatus;
export const TripServiceCreateTripdefault = RpcStatus;
export type TripServiceGetTripPathParams = { readonly "tripId": string; };
export const TripServiceGetTripPathParams = Schema.Struct({ "tripId": Schema.String });
export type TripServiceGetTrip200 = V1GetTripResponse;
export const TripServiceGetTrip200 = V1GetTripResponse;
export type TripServiceGetTripdefault = RpcStatus;
export const TripServiceGetTripdefault = RpcStatus;
export type TripServiceCancelTrip2PathParams = { readonly "tripId": string; };
export const TripServiceCancelTrip2PathParams = Schema.Struct({ "tripId": Schema.String });
export type TripServiceCancelTrip2RequestJson = TripServiceCancelTripBody;
export const TripServiceCancelTrip2RequestJson = TripServiceCancelTripBody;
export type TripServiceCancelTrip2200 = V1CancelTripResponse;
export const TripServiceCancelTrip2200 = V1CancelTripResponse;
export type TripServiceCancelTrip2default = RpcStatus;
export const TripServiceCancelTrip2default = RpcStatus;
export type TripServiceCancelTripPathParams = { readonly "tripId": string; };
export const TripServiceCancelTripPathParams = Schema.Struct({ "tripId": Schema.String });
export type TripServiceCancelTripRequestJson = TripServiceCancelTripBody;
export const TripServiceCancelTripRequestJson = TripServiceCancelTripBody;
export type TripServiceCancelTrip200 = V1CancelTripResponse;
export const TripServiceCancelTrip200 = V1CancelTripResponse;
export type TripServiceCancelTripdefault = RpcStatus;
export const TripServiceCancelTripdefault = RpcStatus;
export type TripServicePreviewTripRequestJson = V1PreviewTripRequest;
export const TripServicePreviewTripRequestJson = V1PreviewTripRequest;
export type TripServicePreviewTrip200 = V1PreviewTripResponse;
export const TripServicePreviewTrip200 = V1PreviewTripResponse;
export type TripServicePreviewTripdefault = RpcStatus;
export const TripServicePreviewTripdefault = RpcStatus;

class TripServiceGroup extends HttpApiGroup.make("TripService")
  .add(
    HttpApiEndpoint.get("TripServiceListTrips", "/v1/trips", {
      query: TripServiceListTripsQuery,
      success: TripServiceListTrips200,
      error: TripServiceListTripsdefault,
    })
      .annotate(OpenApi.Identifier, "TripService_ListTrips")
      .annotate(OpenApi.Summary, "ListTrips is a rider's history, newest first."),
    HttpApiEndpoint.post("TripServiceCreateTrip", "/v1/trips", {
      payload: TripServiceCreateTripRequestJson,
      success: TripServiceCreateTrip200,
      error: TripServiceCreateTripdefault,
    })
      .annotate(OpenApi.Identifier, "TripService_CreateTrip")
      .annotate(
        OpenApi.Summary,
        "Create commits a previewed fare to a real trip and starts matching.",
      )
      .annotate(
        OpenApi.Description,
        "Takes an idempotency key because the caller is a browser on a phone\nnetwork: a retried request must return the original trip rather than\nbooking a second one.",
      ),
    HttpApiEndpoint.get("TripServiceGetTrip", "/v1/trips/:tripId", {
      params: TripServiceGetTripPathParams,
      success: TripServiceGetTrip200,
      error: TripServiceGetTripdefault,
    })
      .annotate(OpenApi.Identifier, "TripService_GetTrip")
      .annotate(OpenApi.Summary, "Get returns a trip's current state."),
    HttpApiEndpoint.post("TripServiceCancelTrip2", "/v1/trips/:tripId/cancel", {
      params: TripServiceCancelTrip2PathParams,
      payload: TripServiceCancelTrip2RequestJson,
      success: TripServiceCancelTrip2200,
      error: TripServiceCancelTrip2default,
    })
      .annotate(OpenApi.Identifier, "TripService_CancelTrip2")
      .annotate(OpenApi.Summary, "Cancel ends a trip before completion.")
      .annotate(
        OpenApi.Description,
        "A custom method rather than DELETE: cancelling is a state transition that\nreturns the trip, not a deletion — the row stays, and a rider can still\nread what happened to it.",
      ),
    HttpApiEndpoint.post("TripServiceCancelTrip", "/v1/trips/:tripId:cancel", {
      params: TripServiceCancelTripPathParams,
      payload: TripServiceCancelTripRequestJson,
      success: TripServiceCancelTrip200,
      error: TripServiceCancelTripdefault,
    })
      .annotate(OpenApi.Identifier, "TripService_CancelTrip")
      .annotate(OpenApi.Summary, "Cancel ends a trip before completion.")
      .annotate(
        OpenApi.Description,
        "A custom method rather than DELETE: cancelling is a state transition that\nreturns the trip, not a deletion — the row stays, and a rider can still\nread what happened to it.",
      ),
    HttpApiEndpoint.post("TripServicePreviewTrip", "/v1/trips:preview", {
      payload: TripServicePreviewTripRequestJson,
      success: TripServicePreviewTrip200,
      error: TripServicePreviewTripdefault,
    })
      .annotate(OpenApi.Identifier, "TripService_PreviewTrip")
      .annotate(
        OpenApi.Summary,
        "Preview quotes a trip without committing to it. Read-only, so a rider\ndragging a pin can call it repeatedly.",
      )
      .annotate(
        OpenApi.Description,
        "POST rather than GET despite being read-only: the request carries two\ncoordinate pairs, and a GET would put a rider's exact pickup and dropoff\ninto every access log and browser history along the way.",
      ),
  )
  .annotate(
    OpenApi.Description,
    "TripService owns the trip lifecycle and is the only writer of trip state.\n\nSynchronous on purpose, and the contrast with the rest of the system is the\npoint. Location and matching are event-driven because they are streams: a\nposition is one of thousands a second and nobody waits for it. A rider\npressing \"request ride\" is waiting, and wants an answer — a fare, a trip id,\nor a reason it cannot happen. Modelling that as an event and correlating a\nreply would be an RPC with extra steps and worse error handling.\nThe HTTP mapping lives here, next to the RPC it maps, and grpc-gateway\ngenerates the REST surface from it. That is the point: hand-written handlers\nmean the REST API and the gRPC contract are two sources of truth, and the\nfirst thing they do is drift — the reference this project borrows from has a\nTypeScript contract file and a Go one that already disagree about which\nevents exist.\n\nPaths follow Google's API design guide: plural collections, and custom\nmethods as `:verb` rather than as invented sub-resources.",
  )
{}

export class SurgeApi extends HttpApi.make("SurgeApi")
  .annotate(OpenApi.Title, "trip.proto")
  .annotate(OpenApi.Version, "version not set")
  .add(TripServiceGroup)
{}
