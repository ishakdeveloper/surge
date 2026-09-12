import { newEmail, signIn, signOut } from "../support/accounts.js";
import { expect, test } from "../support/test.js";

test("a stranger is sent to sign in", async ({ page }) => {
  await page.goto("/ride");

  await expect(page).toHaveURL(/\/auth\/sign-in/);
});

test("a rider signs up, signs out, and signs back in as the same rider", async ({ page }) => {
  const email = newEmail("rider");

  await signIn(page, email, "Rider");
  await expect(page.getByRole("link", { name: "Book a ride →" })).toBeVisible();

  await signOut(page);
  await signIn(page, email);

  await expect(page.getByRole("link", { name: "Book a ride →" })).toBeVisible();
});

test("a driver signs up as a driver", async ({ page }) => {
  await signIn(page, newEmail("driver"), "Driver");

  await expect(page.getByRole("link", { name: "Start a shift →" })).toBeVisible();
});
