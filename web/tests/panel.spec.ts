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

  // The installer prints a link with the token in it, because copying a
  // forty-character string out of a terminal is the most annoying minute of
  // installing anything. It travels in the fragment, which a browser never
  // sends to a server, and the page takes it out of the address bar once it
  // has read it — so a bookmark or a screen share does not keep it.
  const token = setupToken()
  await page.goto(`/setup#token=${token}`)
  await expect(page.getByLabel(/setup token/i)).toHaveValue(token)
  expect(new URL(page.url()).hash, "the token is still in the address bar").toBe("")

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

// Every error the panel can show links into this, and an operator whose panel
// is broken may have no other browser and no way out to the internet.
// The catalogue is the first page anybody browses, and it was three hundred
// grey squares with a letter in them. The logos are served out of the binary,
// so a broken one is a file missing from a build rather than a CDN having a bad
// day — which is exactly the kind of thing that is never noticed until somebody
// opens the page.
test("every logo the catalogue claims is actually there", async ({ page }) => {
  await signIn(page)

  const cookies = await page.context().cookies()
  const jar = cookies.map((c) => `${c.name}=${c.value}`).join("; ")
  const headers = { Cookie: jar }

  const list = await (await page.request.get("/api/templates", { headers })).json()
  const withIcon = list.items.filter((template: { icon?: string }) => template.icon)
  expect(withIcon.length, "no template has a logo, so hack/fetch_icons.py has stopped working")
    .toBeGreaterThan(150)

  const broken: string[] = []
  for (const template of withIcon) {
    const response = await page.request.get(`/api/templates/${template.id}/icon`, { headers })
    if (!response.ok() || (await response.body()).length === 0) {
      broken.push(`${template.id} answered ${response.status()}`)
    }
  }
  expect(broken, "logos the catalogue promises and the panel cannot serve").toEqual([])

  // And a template with no logo must not ask for one: an <img> pointing at a
  // 404 on every card is worse than the letter it replaced.
  const without = list.items.find((template: { icon?: string }) => !template.icon)
  if (without) {
    const response = await page.request.get(`/api/templates/${without.id}/icon`, { headers })
    expect(response.status(), "a template with no logo served one anyway").toBe(404)
  }
})

test("the documentation is served from the binary", async ({ page }) => {
  await page.goto("/docs/")
  await expect(page.getByRole("heading", { level: 1 })).toContainText("documentation")

  await page.getByRole("link", { name: "Troubleshooting" }).click()
  await expect(page.getByRole("heading", { name: "Troubleshooting", level: 1 })).toBeVisible()

  // A link written for the repository has to work here too.
  await page.getByRole("link", { name: "Configuration" }).first().click()
  await expect(page.getByRole("heading", { name: "Configuration", level: 1 })).toBeVisible()

  // And the images have to load, not 404.
  await page.goto("/docs/quick-start")
  const image = page.getByRole("img").first()
  await expect(image).toBeVisible()
  expect(await image.evaluate((element: HTMLImageElement) => element.naturalWidth)).toBeGreaterThan(
    0,
  )
})

test("the theme can be changed and is remembered", async ({ page }) => {
  await signIn(page)

  await page.locator('[data-slot="theme-toggle"]').click()
  await page.getByRole("menuitem", { name: /dark/i }).click()
  await expect(page.locator("html")).toHaveClass(/dark/)

  await page.reload()
  await expect(page.locator("html")).toHaveClass(/dark/)
})

// Every page in the navigation, opened.
//
// The Templates page threw on every install from the day the catalogue grew
// past the eight hand-written entries — `"databases": null` for the 157
// templates that have none, iterated, thrown — and rendered the error boundary
// instead of the catalogue. Nothing caught it, because nothing had ever opened
// the page: the Go tests read the catalogue's structure rather than its JSON,
// and the tests here checked the shell and the empty states.
//
// This opens each one and fails on two things: an uncaught error in the page,
// and the error boundary being on screen. It is deliberately not an assertion
// about what each page contains — that would be a second copy of the panel,
// out of date within a week. It asserts only that the page renders at all,
// which is the thing that was not true.
test("every page in the navigation renders", async ({ page }) => {
  await signIn(page)

  const crashes: string[] = []
  page.on("pageerror", (error) => crashes.push(`${page.url()}: ${error.message}`))

  for (const [path, label] of [
    ["/", "Overview"],
    ["/projects", "Projects"],
    ["/databases", "Databases"],
    ["/servers", "Servers"],
    ["/servers/new", "Add a server"],
    ["/templates", "Templates"],
    ["/activity", "Activity"],
    ["/account", "Account"],
    ["/settings", "Settings"],
  ] as const) {
    await page.goto(path)
    // The heading, not networkidle: two of these are lazy chunks behind a
    // Suspense fallback, and an idle network says nothing about whether the
    // chunk rendered.
    await expect(
      page.getByRole("heading", { level: 1 }).or(page.getByRole("heading", { level: 2 })).first(),
      `${label} (${path}) showed no heading`,
    ).toBeVisible({ timeout: 30_000 })
    await expect(
      page.getByText(/this page stopped working/i),
      `${label} (${path}) rendered the error boundary`,
    ).toHaveCount(0)
  }

  expect(crashes, "a page threw while rendering").toEqual([])
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
