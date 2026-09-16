import { useTranslation } from "react-i18next"
import { useQuery } from "@tanstack/react-query"
import { ActivityIcon } from "lucide-react"

import { EmptyState } from "@/components/empty-state"
import { ErrorDisplay } from "@/components/error-display"
import { Page, PageHeader } from "@/components/page"
import { Card, CardContent } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { useEvents } from "@/hooks/use-events"
import { useSession } from "@/hooks/use-session"
import { api, type List } from "@/lib/api"
import { formatDateTime, formatRelative } from "@/lib/format"
import { queryClient } from "@/lib/query"
import type { AuditEvent } from "@/lib/types"

/**
 * What happened in this team, newest first.
 *
 * The same records the audit log shows, presented as a timeline rather than a
 * table: this page answers "what changed?", the audit tab in Settings answers
 * "who did that, and from where?".
 */
export function ActivityPage() {
  const { t } = useTranslation()
  const { team } = useSession()

  const events = useQuery({
    queryKey: ["audit", team?.id],
    queryFn: () => api.get<List<AuditEvent>>(`/api/teams/${team!.id}/audit`),
    enabled: Boolean(team),
  })

  useEvents(
    team ? [`team:${team.id}`] : [],
    {
      operation: () => void queryClient.invalidateQueries({ queryKey: ["audit", team?.id] }),
      deployment: () => void queryClient.invalidateQueries({ queryKey: ["audit", team?.id] }),
    },
    Boolean(team),
  )

  const items = events.data?.items ?? []

  return (
    <Page width="narrow">
      <PageHeader title={t("nav.activity")} description={t("settings.auditLogHelp")} />

      {events.isLoading ? (
        <Skeleton className="h-64" />
      ) : events.error ? (
        <ErrorDisplay error={events.error} onRetry={() => void events.refetch()} />
      ) : items.length === 0 ? (
        <EmptyState
          icon={ActivityIcon}
          title={t("dashboard.nothingYet")}
          description={t("settings.auditLogHelp")}
        />
      ) : (
        <Card>
          <CardContent className="divide-y p-0">
            {items.map((event) => (
              <div
                key={event.id}
                className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-5 py-3"
              >
                <span className="font-mono text-xs text-muted-foreground">{event.action}</span>
                <span className="min-w-0 flex-1 truncate text-sm">
                  {event.target_label || event.target_type}
                </span>
                <span className="text-xs text-muted-foreground">{event.actor_label}</span>
                <time
                  className="text-xs text-muted-foreground"
                  dateTime={event.at}
                  title={formatDateTime(event.at)}
                >
                  {formatRelative(event.at)}
                </time>
              </div>
            ))}
          </CardContent>
        </Card>
      )}
    </Page>
  )
}
