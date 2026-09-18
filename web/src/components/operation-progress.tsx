import { useTranslation } from "react-i18next"
import type { TFunction } from "i18next"
import { CheckIcon, CircleIcon, Loader2Icon, MinusIcon, XIcon } from "lucide-react"
import { cn } from "cn"

import { translated } from "@/lib/say"
import type { Operation, OperationStep } from "@/lib/types"

/**
 * Shows a long-running operation step by step.
 *
 * Every step is named in the user's language, carries what it found, and a
 * failed step shows the full explanation inline rather than sending the user to
 * look for it. This is what the add-server flow is watched through.
 */
export function OperationProgress({
  operation,
  logs,
}: {
  operation: Operation
  /** Live output of the running step, if any. */
  logs?: string[]
}) {
  const { t } = useTranslation()
  const steps = operation.steps ?? []

  return (
    <ol className="space-y-1" data-slot="operation-progress">
      {steps.map((step) => (
        <li key={step.id}>
          <div
            className={cn(
              "flex items-start gap-3 rounded-md px-3 py-2.5",
              step.status === "running" && "bg-info/5",
              step.status === "failed" && "bg-destructive/5",
            )}
          >
            <StepIcon status={step.status} />
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span
                  className={cn(
                    "text-sm",
                    step.status === "pending" && "text-muted-foreground",
                    step.status === "failed" && "font-medium text-destructive",
                    step.status === "running" && "font-medium",
                  )}
                >
                  {t(`servers.steps.${step.key}`, { defaultValue: step.key })}
                </span>
              </div>
              {step.message && (
                <p
                  className={cn(
                    "mt-0.5 text-xs",
                    step.status === "failed" ? "text-destructive" : "text-muted-foreground",
                  )}
                >
                  {sayStep(t, step)}
                </p>
              )}
              {step.status === "failed" && step.detail && (
                <pre className="log-output mt-2 max-h-72 overflow-auto rounded-md bg-muted p-3 text-xs">
                  {step.detail}
                </pre>
              )}
              {step.status === "running" && logs && logs.length > 0 && (
                <pre className="log-output mt-2 max-h-48 overflow-auto rounded-md bg-muted p-2.5 text-xs">
                  {logs.slice(-40).join("\n")}
                </pre>
              )}
            </div>
          </div>
        </li>
      ))}
    </ol>
  )
}

/**
 * The line under a step's name, in the reader's language.
 *
 * The name has been translated since the panel shipped, from servers.steps.*;
 * the sentence under it was the English the Go code wrote, so somebody adding a
 * server in Indonesian read a translated heading over "Connected to
 * 203.0.113.10". A step that failed shows its problem's title, which the error
 * catalogue already translates — the "problem:" prefix says to look there.
 */
function sayStep(t: TFunction, step: OperationStep): string {
  const key = step.message_key
  if (!key) return step.message
  if (key.startsWith("problem:")) {
    const code = key.slice("problem:".length).replaceAll(".", "_")
    return translated(t, `errors.catalogue.${code}.title`, step.message, step.message_args)
  }
  return translated(t, `servers.stepMessage.${key}`, step.message, step.message_args)
}

function StepIcon({ status }: { status: OperationStep["status"] }) {
  const base = "mt-0.5 size-4 shrink-0"
  switch (status) {
    case "succeeded":
      return <CheckIcon className={cn(base, "text-success")} />
    case "running":
      return <Loader2Icon className={cn(base, "animate-spin text-info")} />
    case "failed":
      return <XIcon className={cn(base, "text-destructive")} />
    case "skipped":
      return <MinusIcon className={cn(base, "text-muted-foreground")} />
    default:
      return <CircleIcon className={cn(base, "text-muted-foreground/40")} />
  }
}
