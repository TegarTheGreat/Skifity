import { Link } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { useQuery } from "@tanstack/react-query"
import { PlusIcon, ServerIcon } from "lucide-react"

import { EmptyState } from "@/components/empty-state"
import { ErrorDisplay } from "@/components/error-display"
import { StatusBadge } from "@/components/status-badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { useEvents } from "@/hooks/use-events"
import { useSession } from "@/hooks/use-session"
import { api, type List } from "@/lib/api"
import { formatMemory, formatRelative } from "@/lib/format"
import { queryClient } from "@/lib/query"
import type { Server } from "@/lib/types"

export function ServersPage() {
  const { t } = useTranslation()
  const { team } = useSession()

  const servers = useQuery({
    queryKey: ["servers", team?.id],
    queryFn: () => api.get<List<Server>>(`/api/teams/${team!.id}/servers`),
    enabled: Boolean(team),
  })

  useEvents(
    team ? [`team:${team.id}`] : [],
    { operation: () => void queryClient.invalidateQueries({ queryKey: ["servers", team?.id] }) },
    Boolean(team),
  )

  return (
    <div className="mx-auto max-w-6xl space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">{t("servers.title")}</h1>
          <p className="text-sm text-muted-foreground">{t("servers.requirementsList")}</p>
        </div>
        <Button asChild>
          <Link to="/servers/new">
            <PlusIcon className="size-4" />
            {t("servers.addServer")}
          </Link>
        </Button>
      </div>

      {servers.isLoading ? (
        <Skeleton className="h-48" />
      ) : servers.error ? (
        <ErrorDisplay error={servers.error} onRetry={() => void servers.refetch()} />
      ) : (servers.data?.items.length ?? 0) === 0 ? (
        <EmptyState
          icon={ServerIcon}
          title={t("servers.empty")}
          description={t("servers.emptyHelp")}
          action={
            <Button asChild>
              <Link to="/servers/new">
                <PlusIcon className="size-4" />
                {t("servers.addServer")}
              </Link>
            </Button>
          }
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("common.name")}</TableHead>
                  <TableHead>{t("servers.host")}</TableHead>
                  <TableHead>{t("common.type")}</TableHead>
                  <TableHead>{t("common.status")}</TableHead>
                  <TableHead className="hidden md:table-cell">{t("servers.cpu")}</TableHead>
                  <TableHead className="hidden md:table-cell">{t("servers.memory")}</TableHead>
                  <TableHead className="hidden lg:table-cell">{t("servers.lastSeen")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {servers.data?.items.map((server) => (
                  <TableRow key={server.id} className="cursor-pointer">
                    <TableCell className="font-medium">
                      <Link to={`/servers/${server.id}`} className="hover:text-primary">
                        {server.name}
                      </Link>
                    </TableCell>
                    <TableCell className="font-mono text-xs">{server.host}</TableCell>
                    <TableCell className="text-xs">
                      {t(`servers.role.${server.role}`, { defaultValue: server.role })}
                    </TableCell>
                    <TableCell>
                      <StatusBadge
                        status={server.status}
                        label={t(`servers.status.${server.status}`, {
                          defaultValue: server.status,
                        })}
                      />
                    </TableCell>
                    <TableCell className="hidden tabular-nums md:table-cell">
                      {server.cpu_cores || "—"}
                    </TableCell>
                    <TableCell className="hidden tabular-nums md:table-cell">
                      {server.memory_mb ? formatMemory(server.memory_mb) : "—"}
                    </TableCell>
                    <TableCell className="hidden text-xs text-muted-foreground lg:table-cell">
                      {server.last_seen_at
                        ? formatRelative(server.last_seen_at)
                        : t("common.never")}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
