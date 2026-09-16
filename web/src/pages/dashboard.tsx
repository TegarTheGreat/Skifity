import { Link } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { useQuery } from "@tanstack/react-query"
import {
  ActivityIcon,
  CpuIcon,
  LayoutGridIcon,
  MemoryStickIcon,
  PlusIcon,
  ServerIcon,
  ShieldCheckIcon,
} from "lucide-react"

import { EmptyState } from "@/components/empty-state"
import { ErrorDisplay } from "@/components/error-display"
import { StatusBadge } from "@/components/status-badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"
import { useEvents } from "@/hooks/use-events"
import { useSession } from "@/hooks/use-session"
import { api, type List } from "@/lib/api"
import { formatCPU, formatMemory, formatRelative } from "@/lib/format"
import type { AuditEvent, ClusterSummary, Project, Server } from "@/lib/types"
import { queryClient } from "@/lib/query"

export function DashboardPage() {
  const { t } = useTranslation()
  const { team, user } = useSession()

  const cluster = useQuery({
    queryKey: ["cluster", team?.id],
    queryFn: () => api.get<ClusterSummary>(`/api/teams/${team!.id}/cluster`),
    enabled: Boolean(team),
    refetchInterval: 15_000,
  })

  const servers = useQuery({
    queryKey: ["servers", team?.id],
    queryFn: () => api.get<List<Server>>(`/api/teams/${team!.id}/servers`),
    enabled: Boolean(team),
  })

  const projects = useQuery({
    queryKey: ["projects", team?.id],
    queryFn: () => api.get<List<Project>>(`/api/teams/${team!.id}/projects`),
    enabled: Boolean(team),
  })

  const audit = useQuery({
    queryKey: ["audit", team?.id, "recent"],
    queryFn: () => api.get<List<AuditEvent>>(`/api/teams/${team!.id}/audit?limit=8`),
    enabled: Boolean(team) && Boolean(user?.is_admin),
  })

  // Live updates: anything that changes in this team refreshes the lists
  // without the user reloading the page.
  useEvents(
    team ? [`team:${team.id}`] : [],
    {
      operation: () => void queryClient.invalidateQueries({ queryKey: ["servers", team?.id] }),
      "project.created": () =>
        void queryClient.invalidateQueries({ queryKey: ["projects", team?.id] }),
      "app.created": () => void queryClient.invalidateQueries({ queryKey: ["projects", team?.id] }),
      audit: () => void queryClient.invalidateQueries({ queryKey: ["audit", team?.id, "recent"] }),
    },
    Boolean(team),
  )

  const hasServers = (servers.data?.items.length ?? 0) > 0

  return (
    <div className="mx-auto max-w-6xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t("dashboard.title")}</h1>
        <p className="text-sm text-muted-foreground">
          {t("dashboard.welcome")}
          {user?.name ? `, ${user.name}` : ""}.
        </p>
      </div>

      {cluster.isLoading ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {[0, 1, 2, 3].map((index) => (
            <Skeleton key={index} className="h-28" />
          ))}
        </div>
      ) : cluster.data?.reachable === false ? (
        <Card className="border-warning/40 bg-warning/5">
          <CardHeader>
            <CardTitle className="text-base">{t("dashboard.clusterUnreachable")}</CardTitle>
            <CardDescription>
              {t("dashboard.clusterUnreachableHelp", { product: "Skifity" })}
            </CardDescription>
          </CardHeader>
          {cluster.data.message && (
            <CardContent>
              <p className="log-output text-xs text-muted-foreground">{cluster.data.message}</p>
            </CardContent>
          )}
        </Card>
      ) : (
        <ClusterCards summary={cluster.data} />
      )}

      {!hasServers && !servers.isLoading && (
        <EmptyState
          icon={ServerIcon}
          title={t("dashboard.noServers")}
          description={t("dashboard.noServersHelp", { product: "Skifity" })}
          action={
            <Button asChild>
              <Link to="/servers/new">
                <PlusIcon className="size-4" />
                {t("dashboard.addFirstServer")}
              </Link>
            </Button>
          }
        />
      )}

      <div className="grid gap-6 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <div>
              <CardTitle className="text-base">{t("nav.projects")}</CardTitle>
              <CardDescription>{t("projects.emptyHelp")}</CardDescription>
            </div>
            <Button size="sm" variant="outline" asChild>
              <Link to="/projects">{t("common.viewAll")}</Link>
            </Button>
          </CardHeader>
          <CardContent>
            {projects.isLoading ? (
              <Skeleton className="h-24" />
            ) : projects.error ? (
              <ErrorDisplay error={projects.error} onRetry={() => void projects.refetch()} />
            ) : (projects.data?.items.length ?? 0) === 0 ? (
              <EmptyState
                icon={LayoutGridIcon}
                title={t("dashboard.noApps")}
                description={t("dashboard.noAppsHelp")}
                action={
                  <Button asChild size="sm">
                    <Link to="/projects?new=1">
                      <PlusIcon className="size-4" />
                      {t("projects.newProject")}
                    </Link>
                  </Button>
                }
                className="border-0 py-8"
              />
            ) : (
              <ul className="divide-y">
                {projects.data?.items.map((project) => (
                  <li key={project.id}>
                    <Link
                      to={`/projects/${project.id}`}
                      className="flex items-center justify-between py-3 text-sm hover:text-primary"
                    >
                      <span className="font-medium">{project.name}</span>
                      <span className="text-xs text-muted-foreground">
                        {formatRelative(project.created_at)}
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("nav.servers")}</CardTitle>
          </CardHeader>
          <CardContent>
            {servers.isLoading ? (
              <Skeleton className="h-24" />
            ) : !hasServers ? (
              <p className="text-sm text-muted-foreground">{t("servers.empty")}</p>
            ) : (
              <ul className="space-y-3">
                {servers.data?.items.slice(0, 6).map((server) => (
                  <li key={server.id}>
                    <Link
                      to={`/servers/${server.id}`}
                      className="flex items-center justify-between gap-2 text-sm hover:text-primary"
                    >
                      <span className="min-w-0 truncate">
                        <span className="font-medium">{server.name}</span>
                        <span className="ml-2 text-xs text-muted-foreground">{server.host}</span>
                      </span>
                      <StatusBadge
                        status={server.status}
                        label={t(`servers.status.${server.status}`, {
                          defaultValue: server.status,
                        })}
                      />
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>
      </div>

      {user?.is_admin && (audit.data?.items.length ?? 0) > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("dashboard.recentActivity")}</CardTitle>
          </CardHeader>
          <CardContent>
            <ul className="divide-y text-sm">
              {audit.data?.items.map((event) => (
                <li key={event.id} className="flex items-center justify-between gap-3 py-2.5">
                  <span className="min-w-0 truncate">
                    <span className="font-medium">{event.action}</span>
                    {event.target_label && (
                      <span className="ml-2 text-muted-foreground">{event.target_label}</span>
                    )}
                  </span>
                  <span className="shrink-0 text-xs text-muted-foreground">
                    {formatRelative(event.at)}
                  </span>
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}
    </div>
  )
}

function ClusterCards({ summary }: { summary?: ClusterSummary }) {
  const { t } = useTranslation()
  if (!summary) return null

  const cpuPercent = summary.total_cpu_m > 0 ? (summary.used_cpu_m / summary.total_cpu_m) * 100 : 0
  const memoryPercent =
    summary.total_memory_mb > 0 ? (summary.used_memory_mb / summary.total_memory_mb) * 100 : 0

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      <MetricCard
        icon={ServerIcon}
        label={t("nav.servers")}
        value={`${summary.ready_nodes} / ${summary.nodes.length}`}
        detail={t("common.instance", {
          count: summary.nodes.reduce((sum, node) => sum + node.pod_count, 0),
        })}
      />
      <MetricCard
        icon={CpuIcon}
        label={t("dashboard.cpuUsed")}
        value={`${Math.round(cpuPercent)}%`}
        detail={`${formatCPU(summary.used_cpu_m)} / ${formatCPU(summary.total_cpu_m)}`}
        progress={cpuPercent}
      />
      <MetricCard
        icon={MemoryStickIcon}
        label={t("dashboard.memoryUsed")}
        value={`${Math.round(memoryPercent)}%`}
        detail={`${formatMemory(summary.used_memory_mb)} / ${formatMemory(summary.total_memory_mb)}`}
        progress={memoryPercent}
      />
      <MetricCard
        icon={summary.high_availability ? ShieldCheckIcon : ActivityIcon}
        label={t("dashboard.clusterHealth")}
        value={
          summary.high_availability
            ? t("dashboard.highAvailability")
            : t("dashboard.notHighlyAvailable")
        }
        detail={summary.high_availability ? undefined : t("dashboard.notHighlyAvailableHelp")}
        small
      />
    </div>
  )
}

function MetricCard({
  icon: Icon,
  label,
  value,
  detail,
  progress,
  small,
}: {
  icon: typeof ServerIcon
  label: string
  value: string
  detail?: string
  progress?: number
  small?: boolean
}) {
  return (
    <Card>
      <CardContent className="space-y-2 pt-5">
        <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
          <Icon className="size-3.5" />
          {label}
        </div>
        <div className={small ? "text-sm font-medium" : "text-2xl font-semibold tabular-nums"}>
          {value}
        </div>
        {progress !== undefined && <Progress value={Math.min(100, progress)} className="h-1.5" />}
        {detail && <p className="text-xs text-muted-foreground">{detail}</p>}
      </CardContent>
    </Card>
  )
}
