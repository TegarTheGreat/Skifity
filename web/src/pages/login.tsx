import { useState } from "react"
import { useTranslation } from "react-i18next"

import { CenteredLayout } from "@/pages/setup"
import { ErrorDisplay } from "@/components/error-display"
import { Logo } from "@/components/logo"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { api } from "@/lib/api"

/**
 * Sign-in.
 *
 * Two-factor is asked for only after the password is accepted, which keeps the
 * first screen simple and tells nobody whether an account has it enabled.
 */
export function LoginPage({ onSignedIn }: { onSignedIn: () => void }) {
  const { t } = useTranslation()
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [needsCode, setNeedsCode] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<unknown>(null)

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    setSubmitting(true)
    setError(null)
    try {
      const result = await api.anonymous<{ totp_required?: boolean }>("/api/auth/login", {
        method: "POST",
        body: {
          email: email.trim(),
          password,
          totp_code: code.trim() || undefined,
        },
      })
      if (result?.totp_required) {
        setNeedsCode(true)
        return
      }
      onSignedIn()
    } catch (caught) {
      setError(caught)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <CenteredLayout>
      <Card className="w-full max-w-sm">
        <CardHeader className="space-y-3">
          <Logo className="size-9 text-primary" />
          <div>
            <CardTitle className="text-xl">
              {t("auth.signInTitle", { product: "Skifity" })}
            </CardTitle>
            <CardDescription>{t("auth.signInSubtitle")}</CardDescription>
          </div>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit} className="space-y-4">
            {error != null && <ErrorDisplay error={error} compact />}

            <div className="space-y-2">
              <Label htmlFor="email">{t("auth.email")}</Label>
              <Input
                id="email"
                type="email"
                value={email}
                onChange={(event) => setEmail(event.target.value)}
                required
                autoFocus
                autoComplete="username"
                disabled={needsCode}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="password">{t("auth.password")}</Label>
              <Input
                id="password"
                type="password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                required
                autoComplete="current-password"
                disabled={needsCode}
              />
            </div>

            {needsCode && (
              <div className="space-y-2">
                <Label htmlFor="code">{t("auth.twoFactorCode")}</Label>
                <Input
                  id="code"
                  value={code}
                  onChange={(event) => setCode(event.target.value)}
                  required
                  autoFocus
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  maxLength={7}
                  className="text-center font-mono text-lg tracking-[0.4em]"
                />
                <p className="text-xs text-muted-foreground">{t("auth.twoFactorPrompt")}</p>
              </div>
            )}

            <Button type="submit" className="w-full" disabled={submitting}>
              {submitting ? t("auth.signingIn") : t("auth.signIn")}
            </Button>

            <details className="text-xs text-muted-foreground">
              <summary className="cursor-pointer hover:text-foreground">
                {t("auth.forgotPassword")}
              </summary>
              <p className="mt-2">{t("auth.forgotPasswordHelp")}</p>
            </details>
          </form>
        </CardContent>
      </Card>
    </CenteredLayout>
  )
}
