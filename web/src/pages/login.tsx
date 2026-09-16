import { useState } from "react"
import { useTranslation } from "react-i18next"
import { ArrowLeftIcon } from "lucide-react"

import { ErrorDisplay } from "@/components/error-display"
import { Logo } from "@/components/logo"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { InputOTP, InputOTPGroup, InputOTPSlot } from "@/components/ui/input-otp"
import { Spinner } from "@/components/ui/spinner"
import { api } from "@/lib/api"
import { CenteredLayout } from "@/pages/setup"

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

  const signIn = async (totpCode: string) => {
    setSubmitting(true)
    setError(null)
    try {
      const result = await api.anonymous<{ totp_required?: boolean }>("/api/auth/login", {
        method: "POST",
        body: { email: email.trim(), password, totp_code: totpCode.trim() || undefined },
      })
      if (result?.totp_required) {
        setNeedsCode(true)
        return
      }
      onSignedIn()
    } catch (caught) {
      setError(caught)
      // A wrong code is almost always a typo or a clock drift, and leaving the
      // digits in place means fixing one of them rather than retyping six.
      setCode("")
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <CenteredLayout>
      <Card className="w-full max-w-sm">
        <CardHeader className="space-y-3">
          <Logo className="size-9 text-primary" />
          <div className="space-y-1">
            <CardTitle className="text-xl">
              {needsCode ? t("auth.twoFactorCode") : t("auth.signInTitle", { product: "Skifity" })}
            </CardTitle>
            <CardDescription>
              {needsCode ? t("auth.twoFactorPrompt") : t("auth.signInSubtitle")}
            </CardDescription>
          </div>
        </CardHeader>

        <CardContent>
          <form
            onSubmit={(event) => {
              event.preventDefault()
              void signIn(code)
            }}
          >
            <FieldGroup>
              {error != null && <ErrorDisplay error={error} compact />}

              {needsCode ? (
                <>
                  <Field className="items-center">
                    <InputOTP
                      maxLength={6}
                      value={code}
                      autoFocus
                      onChange={(value) => {
                        setCode(value)
                        // Six digits is the whole answer, so pressing a button
                        // afterwards is a step with no decision in it.
                        if (value.length === 6 && !submitting) void signIn(value)
                      }}
                    >
                      <InputOTPGroup>
                        {[0, 1, 2, 3, 4, 5].map((slot) => (
                          <InputOTPSlot key={slot} index={slot} />
                        ))}
                      </InputOTPGroup>
                    </InputOTP>
                  </Field>

                  {submitting && (
                    <p className="flex items-center justify-center gap-2 text-sm text-muted-foreground">
                      <Spinner />
                      {t("auth.signingIn")}
                    </p>
                  )}

                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      setNeedsCode(false)
                      setCode("")
                      setError(null)
                    }}
                  >
                    <ArrowLeftIcon />
                    {t("common.back")}
                  </Button>
                </>
              ) : (
                <>
                  <Field>
                    <FieldLabel htmlFor="email">{t("auth.email")}</FieldLabel>
                    <Input
                      id="email"
                      type="email"
                      value={email}
                      onChange={(event) => setEmail(event.target.value)}
                      required
                      autoFocus
                      autoComplete="username"
                    />
                  </Field>

                  <Field>
                    <FieldLabel htmlFor="password">{t("auth.password")}</FieldLabel>
                    <Input
                      id="password"
                      type="password"
                      value={password}
                      onChange={(event) => setPassword(event.target.value)}
                      required
                      autoComplete="current-password"
                    />
                  </Field>

                  <Field>
                    <Button type="submit" className="w-full" disabled={submitting}>
                      {submitting && <Spinner />}
                      {submitting ? t("auth.signingIn") : t("auth.signIn")}
                    </Button>
                    <FieldDescription className="text-center">
                      <Collapsible>
                        <CollapsibleTrigger className="hover:text-foreground">
                          {t("auth.forgotPassword")}
                        </CollapsibleTrigger>
                        <CollapsibleContent>
                          <p className="pt-2 text-left">{t("auth.forgotPasswordHelp")}</p>
                        </CollapsibleContent>
                      </Collapsible>
                    </FieldDescription>
                  </Field>
                </>
              )}
            </FieldGroup>
          </form>
        </CardContent>
      </Card>
    </CenteredLayout>
  )
}
