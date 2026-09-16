#!/usr/bin/env node
/**
 * Fails the build when a translation is missing, extra, empty, or has the wrong
 * plural forms for its language.
 *
 * This is the one thing Lingui offers over i18next: a missing translation
 * becomes a build failure rather than an English string appearing in the middle
 * of a Russian page. It is about a hundred lines to reproduce, which is a much
 * better trade than a macro-based toolchain.
 */

import { readFileSync, readdirSync } from "node:fs"
import { join, dirname } from "node:path"
import { fileURLToPath } from "node:url"

const here = dirname(fileURLToPath(import.meta.url))
const localesDir = join(here, "..", "src", "locales")

/** The language every other language is checked against. */
const SOURCE = "en"

/**
 * The plural categories each language must provide, from Intl.PluralRules.
 * Getting these wrong is not cosmetic: Russian needs one/few/many, and without
 * "many" the panel says "5 сервера" instead of "5 серверов".
 */
const PLURAL_CATEGORIES = {
  en: ["one", "other"],
  id: ["other"],
  hi: ["one", "other"],
  ru: ["one", "few", "many", "other"],
  "zh-CN": ["other"],
}

const PLURAL_SUFFIXES = ["_zero", "_one", "_two", "_few", "_many", "_other"]

function loadLocale(code) {
  const path = join(localesDir, `${code}.json`)
  try {
    return JSON.parse(readFileSync(path, "utf8"))
  } catch (error) {
    console.error(`\nCannot read ${path}: ${error.message}\n`)
    process.exit(1)
  }
}

/** Flattens a nested object into dotted paths. */
function flatten(object, prefix = "", out = new Map()) {
  for (const [key, value] of Object.entries(object)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (value && typeof value === "object" && !Array.isArray(value)) {
      flatten(value, path, out)
    } else {
      out.set(path, value)
    }
  }
  return out
}

/** Splits a key into its base and plural suffix, if it has one. */
function splitPlural(key) {
  for (const suffix of PLURAL_SUFFIXES) {
    if (key.endsWith(suffix)) {
      return { base: key.slice(0, -suffix.length), category: suffix.slice(1) }
    }
  }
  return { base: key, category: null }
}

/** Returns the interpolation placeholders a string uses, such as {{count}}. */
function placeholders(value) {
  if (typeof value !== "string") return new Set()
  return new Set([...value.matchAll(/\{\{\s*([\w.]+)\s*\}\}/g)].map((match) => match[1]))
}

const available = readdirSync(localesDir)
  .filter((name) => name.endsWith(".json"))
  .map((name) => name.replace(/\.json$/, ""))

const source = flatten(loadLocale(SOURCE))
const problems = []

// Every language named in PLURAL_CATEGORIES must actually exist.
for (const code of Object.keys(PLURAL_CATEGORIES)) {
  if (!available.includes(code)) {
    problems.push(`${code}.json is missing, but the panel ships in that language`)
  }
}
for (const code of available) {
  if (!PLURAL_CATEGORIES[code]) {
    problems.push(`${code}.json exists but is not in the shipped language list`)
  }
}

// Group the source keys by their base, so plural sets are checked as a whole.
const sourceGroups = new Map()
for (const [key, value] of source) {
  const { base, category } = splitPlural(key)
  if (!sourceGroups.has(base)) sourceGroups.set(base, { plural: false, value, categories: [] })
  const group = sourceGroups.get(base)
  if (category) {
    group.plural = true
    group.categories.push(category)
    group.value = value
  }
}

for (const code of available) {
  if (code === SOURCE) {
    // The source still has to be free of empty strings.
    for (const [key, value] of source) {
      if (typeof value !== "string" || value.trim() === "") {
        problems.push(`${SOURCE}: ${key} is empty`)
      }
    }
    continue
  }

  const target = flatten(loadLocale(code))
  const categories = PLURAL_CATEGORIES[code] ?? ["other"]

  for (const [base, group] of sourceGroups) {
    if (group.plural) {
      // A plural key must have exactly the categories this language uses.
      for (const category of categories) {
        const key = `${base}_${category}`
        const value = target.get(key)
        if (value === undefined) {
          problems.push(`${code}: ${key} is missing (${base} is a plural, and ${code} needs ${categories.join(", ")})`)
        } else if (typeof value !== "string" || value.trim() === "") {
          problems.push(`${code}: ${key} is empty`)
        }
      }
      for (const key of target.keys()) {
        const split = splitPlural(key)
        if (split.base === base && split.category && !categories.includes(split.category)) {
          problems.push(`${code}: ${key} uses the plural form "${split.category}", which ${code} does not have`)
        }
      }
      continue
    }

    const value = target.get(base)
    if (value === undefined) {
      problems.push(`${code}: ${base} is missing`)
      continue
    }
    if (typeof value !== "string" || value.trim() === "") {
      problems.push(`${code}: ${base} is empty`)
      continue
    }

    // A translation that drops or invents a placeholder renders as a literal
    // "{{count}}" or silently loses a value.
    const wanted = placeholders(group.value)
    const got = placeholders(value)
    for (const name of wanted) {
      if (!got.has(name)) {
        problems.push(`${code}: ${base} is missing the placeholder {{${name}}}`)
      }
    }
    for (const name of got) {
      if (!wanted.has(name)) {
        problems.push(`${code}: ${base} has an extra placeholder {{${name}}} that ${SOURCE} does not use`)
      }
    }
  }

  // Keys that exist only in a translation are dead weight, and usually a sign
  // that a key was renamed in English and not everywhere else.
  for (const key of target.keys()) {
    const { base } = splitPlural(key)
    if (!sourceGroups.has(base)) {
      problems.push(`${code}: ${key} is not in ${SOURCE}.json`)
    }
  }
}

if (problems.length > 0) {
  console.error(`\n${problems.length} translation problem(s):\n`)
  for (const problem of problems.slice(0, 60)) console.error(`  - ${problem}`)
  if (problems.length > 60) console.error(`  ... and ${problems.length - 60} more`)
  console.error(
    `\nEvery user-visible string must exist in all ${Object.keys(PLURAL_CATEGORIES).length} languages.\n`,
  )
  process.exit(1)
}

const keyCount = sourceGroups.size
console.log(
  `i18n: ${keyCount} keys complete in ${available.length} languages (${available.sort().join(", ")}).`,
)
