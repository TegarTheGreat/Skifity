import { useState } from "react"
import { useTranslation } from "react-i18next"
import { useMutation } from "@tanstack/react-query"
import { PlayIcon, TerminalIcon } from "lucide-react"

import { EmptyState } from "@/components/empty-state"
import { ErrorDisplay } from "@/components/error-display"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Spinner } from "@/components/ui/spinner"
import { api } from "@/lib/api"
import type { App } from "@/lib/types"

/**
 * One-off commands.
 *
 * A migration, a backfill, a look at the data. It runs in the app's own image
 * with the app's own variables, as a job of its own rather than a shell into a
 * running instance — which matters because the moment you most need this is
 * when the app will not start, and then there is nothing to attach to.
 */
export function ConsoleTab({ app }: { app: App }) {
  const { t } = useTranslation()
  const [command, setCommand] = useState("")
  const [output, setOutput] = useState<string[] | null>(null)
  const [ran, setRan] = useState("")

  const run = useMutation({
    mutationFn: async () => {
      const started = await api.post<{ run: string }>(`/api/apps/${app.id}/run`, {
        command: command.trim(),
      })
      return api.get<{ lines: string[] }>(`/api/apps/${app.id}/runs/${started.run}/logs`)
    },
    onSuccess: (result) => {
      setOutput(result.lines)
      setRan(command.trim())
    },
  })

  return (
    <div className="space-y-4">
      <form
        className="space-y-4"
        onSubmit={(event) => {
          event.preventDefault()
          run.mutate()
        }}
      >
        <Field>
          <FieldLabel htmlFor="console-command">{t("apps.runCommand")}</FieldLabel>
          <div className="flex flex-wrap gap-2">
            <Input
              id="console-command"
              value={command}
              onChange={(event) => setCommand(event.target.value)}
              placeholder="npm run migrate"
              className="min-w-64 flex-1 font-mono"
              autoComplete="off"
              disabled={run.isPending}
            />
            <Button type="submit" disabled={!command.trim() || run.isPending}>
              {run.isPending ? <Spinner /> : <PlayIcon />}
              {run.isPending ? t("apps.running") : t("apps.run")}
            </Button>
          </div>
          <FieldDescription>{t("apps.runCommandHelp")}</FieldDescription>
        </Field>
      </form>

      {run.error != null && <ErrorDisplay error={run.error} />}

      {run.isPending && (
        <Alert>
          <Spinner />
          <AlertDescription>{t("apps.runningHelp")}</AlertDescription>
        </Alert>
      )}

      {output === null ? (
        !run.isPending && (
          <EmptyState
            icon={TerminalIcon}
            title={t("apps.runCommand")}
            description={t("apps.runCommandHelp")}
          />
        )
      ) : (
        <div className="space-y-2">
          <p className="font-mono text-xs text-muted-foreground">$ {ran}</p>
          <ScrollArea className="h-80 rounded-md border bg-muted/30">
            <pre className="p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap">
              {output.length > 0 ? output.join("\n") : t("apps.runNoOutput")}
            </pre>
          </ScrollArea>
        </div>
      )}
    </div>
  )
}
