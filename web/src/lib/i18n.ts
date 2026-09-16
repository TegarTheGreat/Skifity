import i18n from "i18next"
import LanguageDetector from "i18next-browser-languagedetector"
import { initReactI18next } from "react-i18next"

import en from "@/locales/en.json"
import hi from "@/locales/hi.json"
import id from "@/locales/id.json"
import ru from "@/locales/ru.json"
import zhCN from "@/locales/zh-CN.json"

/**
 * The languages the panel ships in.
 *
 * Each is named in its own language, because someone looking for their language
 * in a list is not reading the one they cannot read.
 */
export const LANGUAGES = [
  { code: "en", name: "English", englishName: "English" },
  { code: "id", name: "Bahasa Indonesia", englishName: "Indonesian" },
  { code: "hi", name: "हिन्दी", englishName: "Hindi" },
  { code: "ru", name: "Русский", englishName: "Russian" },
  { code: "zh-CN", name: "简体中文", englishName: "Chinese (Simplified)" },
] as const

export type LanguageCode = (typeof LANGUAGES)[number]["code"]

const STORAGE_KEY = "skifity-language"

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      en: { translation: en },
      id: { translation: id },
      hi: { translation: hi },
      ru: { translation: ru },
      "zh-CN": { translation: zhCN },
    },
    fallbackLng: "en",
    supportedLngs: LANGUAGES.map((language) => language.code),
    // zh-TW and zh-HK would otherwise fall back past zh-CN to English; a
    // Simplified page is closer than an English one, and the fallback chain
    // handles the rest.
    nonExplicitSupportedLngs: true,
    detection: {
      // The stored choice wins, then the browser's own languages. A user who
      // picked a language keeps it on the next visit.
      order: ["localStorage", "navigator"],
      lookupLocalStorage: STORAGE_KEY,
      caches: ["localStorage"],
    },
    interpolation: {
      // React escapes everything it renders, so escaping here would show
      // &amp; in the middle of a sentence.
      escapeValue: false,
    },
    returnNull: false,
  })

/** Changes the language and remembers the choice. */
export function setLanguage(code: LanguageCode) {
  void i18n.changeLanguage(code)
  try {
    localStorage.setItem(STORAGE_KEY, code)
  } catch {
    // Not being able to remember it is not a reason to refuse the change.
  }
  document.documentElement.lang = code
}

/** The language currently in use, normalised to one we ship. */
export function currentLanguage(): LanguageCode {
  const active = i18n.resolvedLanguage ?? i18n.language ?? "en"
  const match = LANGUAGES.find((language) => language.code === active)
  return match?.code ?? "en"
}

// Keep the document's lang attribute in step, which screen readers and the
// browser's own hyphenation both rely on.
i18n.on("languageChanged", (code) => {
  document.documentElement.lang = code
})

export default i18n
