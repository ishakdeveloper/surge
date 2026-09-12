import { Effect, Layer } from "effect";
import { type HttpBody, HttpClient, HttpClientResponse } from "effect/unstable/http";

/**
 * An `HttpClient` answered by a function, for tests of services over the
 * generated API. The request goes through the generated client and its
 * decoders as it does in production; only the network is replaced.
 */
export interface FakeHttpRequest {
  readonly method: string;
  readonly path: string;
  readonly query: URLSearchParams;
  /** The bearer token, when the caller sent one. Absent on a presigned link. */
  readonly authorization: string | undefined;
  /** The JSON body, when there was one. */
  readonly body: unknown;
}

export interface FakeHttpResponse {
  readonly status?: number;
  readonly body: unknown;
}

/**
 * The JSON body, when there was one.
 *
 * A document upload puts bytes at a presigned link, so not every body is JSON
 * — and a test client that threw on a PDF would be failing the thing it is
 * meant to observe.
 */
const jsonBody = (body: HttpBody.HttpBody): unknown => {
  if (body._tag !== "Uint8Array") return undefined;
  try {
    return JSON.parse(new TextDecoder().decode(body.body));
  } catch {
    return undefined;
  }
};

/** The gateway's answer to a route nobody wrote, so a forgotten route fails the way the app would. */
export const notFound = (request: FakeHttpRequest): FakeHttpResponse => ({
  status: 404,
  body: {
    error: { code: "not_found", message: `no fake route for ${request.method} ${request.path}` },
  },
});

export const fakeHttp = (answer: (request: FakeHttpRequest) => FakeHttpResponse) => {
  const requests: Array<FakeHttpRequest> = [];
  const layer = Layer.succeed(HttpClient.HttpClient)(
    HttpClient.make((request, url) => {
      const received: FakeHttpRequest = {
        authorization: request.headers["authorization"],
        method: request.method,
        path: url.pathname,
        query: url.searchParams,
        body: jsonBody(request.body),
      };
      requests.push(received);
      const response = answer(received);
      return Effect.succeed(
        HttpClientResponse.fromWeb(
          request,
          new Response(JSON.stringify(response.body), {
            status: response.status ?? 200,
            headers: { "content-type": "application/json" },
          }),
        ),
      );
    }),
  );
  return { layer, requests };
};
