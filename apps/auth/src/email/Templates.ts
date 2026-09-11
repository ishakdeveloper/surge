/**
 * Transactional message bodies as pure functions.
 *
 * Deliberately plain strings rather than React — nothing in the server package
 * should need a renderer. Swap in `@react-email/components` here if templates
 * outgrow this.
 *
 * Only codes are sent. Signing in is an email or phone one-time code, so there
 * are no passwords to reset, no links to follow and no addresses to confirm
 * separately — entering the code is the confirmation.
 */
export interface RenderedEmail {
  readonly subject: string;
  readonly html: string;
  readonly text: string;
}

const layout = (heading: string, body: string) =>
  `<!doctype html><html><body style="font-family:system-ui,sans-serif;line-height:1.5">`
  + `<h1 style="font-size:18px">${heading}</h1>${body}</body></html>`;

export const emailOtp = (code: string): RenderedEmail => ({
  subject: `${code} is your Surge code`,
  html: layout(
    "Your sign-in code",
    `<p style="font-size:24px;letter-spacing:4px"><strong>${code}</strong></p>`
      + `<p>It expires in five minutes. If you did not ask for it, ignore this email.</p>`,
  ),
  text: `Your Surge sign-in code is ${code}. It expires in five minutes.`,
});

/**
 * One line, code first: a lock screen shows the start of a message, and the
 * `@domain #code` suffix is what lets iOS and Android offer the code for
 * autofill without the person opening the message at all.
 */
export const smsOtp = (code: string, domain: string): string =>
  `${code} is your Surge code. It expires in five minutes.\n\n@${domain} #${code}`;
