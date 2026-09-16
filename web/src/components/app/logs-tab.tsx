import { useEffect, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { PauseIcon, PlayIcon, ScrollTextIcon } from "lucide-react"

import { EmptyState } from "@/components/empty-state"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import type { App } from "@/lib/types"

/** How many lines are kept in memory. A chatty app must not grow the tab forever. */
const MAX_LINES = 2000

/**
 * Live application logs.
 *
 * This reads the app's own stream rather than the shared event hub, because the
 * panel scrubs each line as it passes through and a log stream is long-lived in
 * a way the hub's fan-out buffer is not meant for.
 */
export function LogsTab({ app }: { app: App }) {
  const { t } = useTranslation()
  const [lines, setLines] = useState<string[]>([])
  const [following, setFollowing] = useState(true)
  const [filter, setFilter] = useState("")
  const bottom = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!following) return

    const source = new EventSource(`/api/apps/${app.id}/logs?follow=true&tail=200`)
    source.addEventListener("log", (message) => {
      try {
        const line = JSON.parse((message as MessageEvent).data) as string
        setLines((previous) => {
          const next = [...previous, line]
          return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next
        })
      } catch {
        // A frame we cannot parse is not worth tearing the stream down for.
      }
    })
    return () => source.close()
  }, [app.id, following])

  const shown = useMemo(() => {
    if (!filter.trim()) return lines
    const needle = filter.toLowerCase()
    return lines.filter((line) => line.toLowerCase().includes(needle))
  }, [lines, filter])

  useEffect(() => {
    if (following) bottom.current?.scrollIntoView({ block: "nearest" })
  }, [shown.length, following])

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          value={filter}
          onChange={(event) => setFilter(event.target.value)}
          placeholder={t("common.search")}
          className="h-9 max-w-xs"
        />
        <Button variant="outline" size="sm" onClick={() => setFollowing(!following)}>
          {following ? <PauseIcon className="size-3.5" /> : <PlayIcon className="size-3.5" />}
          {following ? t("common.off") : t("common.on")}
        </Button>
        <span className="text-xs text-muted-foreground">
          {shown.length} / {lines.length}
        </span>
      </div>

      {lines.length === 0 ? (
        <EmptyState
          icon={ScrollTextIcon}
          title={t("apps.logs")}
          description={t("apps.noInstances")}
        />
      ) : (
        <ScrollArea className="h-[28rem] rounded-md border bg-muted/30">
          <pre className="p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap">
            {shown.join("\n")}
            <div ref={bottom} />
          </pre>
        </ScrollArea>
      )}
    </div>
  )
}
