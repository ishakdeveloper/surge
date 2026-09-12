import { request } from "@playwright/test";
import { apiUrl, authUrl } from "./accounts.js";

const webUrl = process.env["E2E_WEB_URL"] ?? "http://localhost:5273";

/**
 * Fails in seconds, naming what is missing, rather than letting every test
 * time out one by one against a stack that never came up.
 */
export default async function globalSetup(): Promise<void> {
  const api = await request.newContext();
  try {
    const needed: ReadonlyArray<readonly [string, string]> = [
      ["web", webUrl],
      ["the gateway", `${apiUrl}/ready`],
      ["auth", `${authUrl}/ready`],
    ];
    for (const [name, url] of needed) {
      const response = await api.get(url, { timeout: 5_000, failOnStatusCode: false })
        .catch(() => undefined);
      if (response?.ok() !== true) {
        throw new Error(
          `${name} is not answering at ${url}. Start the app profile: see deploy/compose/docker-compose.yml`,
        );
      }
    }

    // An open outbox refuses a question with no address; a closed one has no
    // such route at all.
    const outbox = await api.get(`${authUrl}/dev/outbox`, { failOnStatusCode: false });
    if (outbox.status() !== 400) {
      throw new Error(
        "auth is running without AUTH_DEV_OUTBOX=true, so no test can read a sign-in code",
      );
    }
  } finally {
    await api.dispose();
  }
}
