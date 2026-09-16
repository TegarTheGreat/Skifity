import { test, expect } from "@playwright/test"
import { readFileSync, mkdirSync } from "node:fs"
import { join, resolve } from "node:path"

/**
 * Captures the screenshots in the README.
 *
 * It is a test rather than a script so that it runs against the same real
 * binary as everything else, and so a screenshot can never show a screen that
 * no longer exists. Run it with:
 *
 *   npx playwright test screenshots --grep-invert nothing
 *
 * It is skipped by default because a README image changing on every run makes
 * for a noisy diff.
 */

const EMAIL = "owner@example.test"
const PASSWORD = "a reasonable passphrase"
const OUT = resolve(import.meta.dirname, "..", "..", "docs", "images")

test.skip(!process.env.SKIFITY_SCREENSHOTS, "set SKIFITY_SCREENSHOTS=1 to capture")
test.describe.configure({ mode: "serial" })
test.use({ viewport: { width: 1440, height: 900 } })

function setupToken(): string {
  const workdir = readFileSync(resolve(import.meta.dirname, "..", ".e2e-workdir"), "utf8").trim()
  return readFileSync(join(workdir, "setup-token"), "utf8").trim()
}

test("capture", async ({ page }) => {
  mkdirSync(OUT, { recursive: true })

  // The setup screen, before anything exists.
  await page.goto("/")
  await expect(page.getByLabel(/setup token/i)).toBeVisible()
  await page.screenshot({ path: join(OUT, "setup.png") })

  await page.getByLabel(/setup token/i).fill(setupToken())
  await page.getByLabel(/^email$/i).fill(EMAIL)
  await page.getByLabel(/your name/i).fill("Owner")
  await page.getByLabel(/^password$/i).fill(PASSWORD)
  await page.getByLabel(/team name/i).fill("Acme")
  await page.getByRole("button", { name: /create account/i }).click()

  // The recovery key, blurred: a screenshot of a real key would be a bad
  // example even though this one is throwaway.
  await expect(page.getByText(/^SKIFITY-RECOVERY-v1-/)).toBeVisible()
  const blur = await page.addStyleTag({
    content: "pre, code { filter: blur(6px) !important; }",
  })
  await page.screenshot({ path: join(OUT, "recovery-key.png") })
  // Removed rather than reloaded: the recovery key is shown once, and a reload
  // would lose the screen this test still has to click through.
  await blur.evaluate((element) => element.remove())

  await page.getByRole("checkbox").check()
  await page.getByRole("button", { name: /done|continue|finish/i }).click()

  // The overview, empty, which is what a new install looks like.
  await expect(page.getByRole("heading", { name: "Overview", level: 1 })).toBeVisible()
  await page.screenshot({ path: join(OUT, "overview.png") })

  // Dark, because that is what most people will see it in.
  await page.locator('[data-slot="theme-toggle"]').click()
  await page.getByRole("menuitem", { name: /dark/i }).click()
  await expect(page.locator("html")).toHaveClass(/dark/)
  await page.screenshot({ path: join(OUT, "overview-dark.png") })

  // Adding a server: the form that is the whole of what Skifity asks for.
  await page.getByRole("link", { name: "Servers", exact: true }).click()
  await page.getByRole("link", { name: /add a server/i }).first().click()
  await expect(page.getByLabel(/IP address or hostname/i)).toBeVisible()
  await page.screenshot({ path: join(OUT, "add-server.png") })

  // Another language, to show that this is not a token gesture.
  await page.locator('[data-slot="language-switcher"]').click()
  await page.getByRole("menuitem", { name: "Русский" }).click()
  await page.goto("/")
  await page.waitForTimeout(300)
  await page.screenshot({ path: join(OUT, "overview-russian.png") })
})
