// The code the auth service "sent" to `TO`, read from its dev outbox — which
// exists only when the service runs with AUTH_DEV_OUTBOX=true. A Maestro flow
// cannot read the service's log, and this is the log's content, served.
const response = http.get(`${AUTH_URL}/dev/outbox?to=${encodeURIComponent(TO)}`);
if (response.status !== 200) {
  throw new Error(`No code was sent to ${TO} (${response.status}). Is AUTH_DEV_OUTBOX=true?`);
}
output.code = json(response.body).code;
