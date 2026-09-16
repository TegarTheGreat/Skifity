import { test, expect, type Page } from "@playwright/test"
import { readFileSync } from "node:fs"
import { join, resolve } from "node:path"

/**
 * One pass through the panel, against the real binary.
 *
 * Deliberately small: first-run setup, the recovery key gate, the shell, and
 * the language switcher in all five languages. That last one is the part most
 * likely to break silently, because an untranslated page still renders.
 */

const EMAIL = "owner@example.test"
const PASSWORD = "a reasonable passphrase"

/** The five languages the panel ships, named as the switcher names them. */
const LANGUAGES = [
  { code: "en", name: "English" },
  { code: "id", name: "Bahasa Indonesia" },
  { code: "hi", name: "हिन्दी" },
  { code: "ru", name: "Русский" },
  { code: "zh-CN", name: "简体中文" },
] as const

/** Reads a locale file, so the test never guesses at a translation. */
function localeStrings(code: string): Record<string, Record<string, unknown>> {
  const path = resolve(import.meta.dirname, "..", "src", "locales", `${code}.json`)
  return JSON.parse(readFileSync(path, "utf8"))
}

function setupToken(): string {
  const workdir = readFileSync(resolve(import.meta.dirname, "..", ".e2e-workdir"), "utf8").trim()
  return readFileSync(join(workdir, "setup-token"), "utf8").trim()
}

test.describe.configure({ mode: "serial" })

test("first-run setup creates an account and shows the recovery key once", async ({ page }) => {
  await page.goto("/")

  // With no account, every route is the setup screen.
  await expect(page.getByLabel(/setup token/i)).toBeVisible()

  await page.getByLabel(/setup token/i).fill(setupToken())
  await page.getByLabel(/^email$/i).fill(EMAIL)
  await page.getByLabel(/your name/i).fill("Owner")
  await page.getByLabel(/^password$/i).fill(PASSWORD)
  await page.getByLabel(/team name/i).fill("Acme")
  await page.getByRole("button", { name: /create account/i }).click()

  // The recovery key is shown once, and the panel does not move on until it is
  // acknowledged. That gate is the whole point of the screen.
  const recoveryKey = page.getByText(/^SKIFITY-RECOVERY-v1-/)
  await expect(recoveryKey).toBeVisible()
  expect((await recoveryKey.textContent())?.trim().length).toBeGreaterThan(30)

  const done = page.getByRole("button", { name: /done|continue|finish/i })
  await expect(done).toBeDisabled()

  await page.getByRole("checkbox").check()
  await expect(done).toBeEnabled()
  await done.click()

  await expect(page.getByRole("heading", { name: "Overview", level: 1, exact: true })).toBeVisible()
})

test("the shell is there, and the empty states say what to do next", async ({ page }) => {
  await signIn(page)

  // Every navigation target should exist and land somewhere. The heading is
  // matched at level 1 and exactly: "Servers" would otherwise also match the
  // empty state's "No servers yet".
  for (const name of ["Projects", "Servers", "Databases"] as const) {
    await page.getByRole("link", { name, exact: true }).click()
    await expect(page.getByRole("heading", { name, level: 1, exact: true })).toBeVisible()
  }

  // A panel with no servers must not just say "none": it has to offer the
  // next step.
  await page.getByRole("link", { name: "Servers", exact: true }).click()
  await expect(page.getByRole("link", { name: /add a server/i }).first()).toBeVisible()

  await page
    .getByRole("link", { name: /add a server/i })
    .first()
    .click()
  await expect(page.getByLabel(/IP address or hostname/i)).toBeVisible()
  // The promise that the password is not kept has to be on the screen where
  // the password is typed.
  await expect(page.getByText(/never stored/i)).toBeVisible()
})

test("every language is complete on the pages a new user sees", async ({ page }) => {
  await signIn(page)

  // The expected words come from the locale files themselves. The completeness
  // check already proves those files have every key; what this proves is that
  // the running panel actually resolves to the language that was chosen, which
  // is a different failure and one the files cannot show.
  const languages = LANGUAGES.map((language) => ({
    ...language,
    overview: localeStrings(language.code).dashboard.title as string,
  }))

  for (const language of languages) {
    // Selected by data-slot rather than by name: the button's accessible name
    // is itself translated, so it stops matching the moment it is used.
    await page.locator('[data-slot="language-switcher"]').click()
    await page.getByRole("menuitem", { name: language.name }).click()

    await expect(
      page.getByRole("heading", { name: language.overview, level: 1, exact: true }),
      `the overview heading in ${language.name}`,
    ).toBeVisible()

    // The sidebar must be translated too, not just the page.
    await expect(
      page.locator("nav").getByText(language.overview).first(),
      `the sidebar in ${language.name}`,
    ).toBeVisible()

    // Nothing may be left as a raw key, which is what a missing translation
    // looks like when the fallback is turned off.
    const body = (await page.locator("body").innerText()).trim()
    expect(body, `untranslated key visible in ${language.name}`).not.toMatch(
      /\b(nav|dashboard|common|servers|apps)\.[a-zA-Z]+\b/,
    )
  }

  // The choice has to survive a reload, or it is not a setting. The last
  // language switched to is still selected.
  const last = languages[languages.length - 1]
  await page.reload()
  await expect(
    page.getByRole("heading", { name: last.overview, level: 1, exact: true }),
  ).toBeVisible()
})

test("the theme can be changed and is remembered", async ({ page }) => {
  await signIn(page)

  await page.locator('[data-slot="theme-toggle"]').click()
  await page.getByRole("menuitem", { name: /dark/i }).click()
  await expect(page.locator("html")).toHaveClass(/dark/)

  await page.reload()
  await expect(page.locator("html")).toHaveClass(/dark/)
})

/** Signs in, unless this context already has a session. */
async function signIn(page: Page) {
  await page.goto("/")

  const email = page.getByLabel(/^email$/i)
  const accountMenu = page.locator('[data-slot="account-menu"]')

  // Wait for the app to decide which of the two it is showing. Checking
  // visibility without waiting races the first render and always says "no".
  await expect(email.or(accountMenu).first()).toBeVisible()

  if (await email.isVisible()) {
    await email.fill(EMAIL)
    await page.getByLabel(/^password$/i).fill(PASSWORD)
    await page.getByRole("button", { name: /sign in/i }).click()
  }
  await expect(accountMenu).toBeVisible()
}
