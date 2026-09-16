import { useState } from "react"
import { useNavigate, useParams } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery } from "@tanstack/react-query"
import { ArrowUpCircleIcon, RefreshCwIcon, Trash2Icon } from "lucide-react"

import { ErrorDisplay } from "@/components/error-display"
import { OperationProgress } from "@/components/operation-progress"
import { StatusBadge } from "@/components/status-badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"
import { useEvents } from "@/hooks/use-events"
import { useSession } from "@/hooks/use-session"
import { api } from "@/lib/api"
import { formatCPU, formatDateTime, formatMemory, formatRelative } from "@/lib/format"
import { queryClient } from "@/lib/query"
import type { NodeInfo, Operation, Server } from "@/lib/types"

export function ServerDetailPage() {
  const { t } = useTranslation()
  const { serverId = "" } = useParams()
  const navigate = useNavigate()
  const { team } = useSession()
  const [wipe, setWipe] = useState(true)
  const [name, setName] = useState<string | null>(null)
  const [operationId, setOperationId] = useState<string | null>(null)

  const server = useQuery({
    queryKey: ["server", serverId],
    queryFn: () => api.get<Server>(`/api/servers/${serverId}`),
  })

  const metrics = useQuery({
    queryKey: ["server-metrics", serverId],
    queryFn: () => api.get<NodeInfo>(`/api/servers/${serverId}/metrics`),
    enabled: server.data?.status === "ready",
    refetchInterval: 15_000,
    retry: false,
  })

  useEvents(
    team ? [`team:${team.id}`] : [],
    { operation: () => void queryClient.invalidateQueries({ queryKey: ["server", serverId] }) },
    Boolean(team),
  )

  const rename = useMutation({
    mutationFn: (next: string) => api.patch(`/api/servers/${serverId}`, { name: next }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["server", serverId] })
      setName(null)
    },
  })

  const retry = useMutation({
    mutationFn: () => api.post<Operation>(`/api/servers/${serverId}/retry`),
    onSuccess: (operation) => setOperationId(operation.id),
  })

  const promote = useMutation({
    mutationFn: () => api.post<Operation>(`/api/servers/${serverId}/promote`),
    onSuccess: (operation) => setOperationId(operation.id),
  })

  const remove = useMutation({
    mutationFn: () => api.delete<Operation>(`/api/servers/${serverId}?wipe=${wipe}`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["servers", team?.id] })
      navigate("/servers")
    },
  })

  if (server.error)
    return <ErrorDisplay error={server.error} onRetry={() => void server.refetch()} />
  if (server.isLoading || !server.data) return <Skeleton className="h-96" />

  const current = server.data

  return (
    <div className="mx-auto max-w-4xl space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          {name === null ? (
            <button
              type="button"
              className="truncate text-left text-2xl font-semibold tracking-tight hover:text-primary"
              onClick={() => setName(current.name)}
            >
              {current.name}
            </button>
          ) : (
            <form
              className="flex items-center gap-2"
              onSubmit={(event) => {
                event.preventDefault()
                rename.mutate(name.trim())
              }}
            >
              <Input value={name} onChange={(event) => setName(event.target.value)} autoFocus />
              <Button type="submit" size="sm" disabled={rename.isPending}>
                {t("common.save")}
              </Button>
              <Button type="button" size="sm" variant="ghost" onClick={() => setName(null)}>
                {t("common.cancel")}
              </Button>
            </form>
          )}
          <p className="mt-1 font-mono text-sm text-muted-foreground">
            {current.ssh_user}@{current.host}:{current.ssh_port}
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <StatusBadge
            status={current.status}
            label={t(`servers.status.${current.status}`, { defaultValue: current.status })}
          />
          {current.status === "failed" && (
            <Button variant="outline" disabled={retry.isPending} onClick={() => retry.mutate()}>
              <RefreshCwIcon className="size-4" />
              {t("common.retry")}
            </Button>
          )}
          {current.role === "worker" && current.status === "ready" && (
            <Button
              variant="outline"
              disabled={promote.isPending}
              onClick={() => {
                if (window.confirm(t("servers.promoteHelp"))) promote.mutate()
              }}
            >
              <ArrowUpCircleIcon className="size-4" />
              {t("servers.promote")}
            </Button>
          )}
        </div>
      </div>

      {current.status_detail && (
        <p className="text-sm text-muted-foreground">{current.status_detail}</p>
      )}

      {retry.error != null && <ErrorDisplay error={retry.error} />}
      {promote.error != null && <ErrorDisplay error={promote.error} />}
      {rename.error != null && <ErrorDisplay error={rename.error} />}

      {operationId && <LiveOperation operationId={operationId} />}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("servers.requirements")}</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-x-8 gap-y-3 sm:grid-cols-2">
          <Fact
            label={t("common.type")}
            value={t(`servers.role.${current.role}`, { defaultValue: current.role })}
          />
          <Fact
            label={t("servers.operatingSystem")}
            value={current.os_info || t("common.unknown")}
          />
          <Fact
            label={t("servers.cpu")}
            value={current.cpu_cores ? String(current.cpu_cores) : "—"}
          />
          <Fact
            label={t("servers.memory")}
            value={current.memory_mb ? formatMemory(current.memory_mb) : "—"}
          />
          <Fact label={t("servers.disk")} value={current.disk_gb ? `${current.disk_gb} GB` : "—"} />
          <Fact label="Architecture" value={current.arch || "—"} />
          <Fact label={t("apps.node")} value={current.node_name || "—"} mono />
          <Fact
            label={t("servers.lastSeen")}
            value={current.last_seen_at ? formatRelative(current.last_seen_at) : t("common.never")}
          />
          <Fact label={t("common.created")} value={formatDateTime(current.created_at)} />
        </CardContent>
      </Card>

      {metrics.data && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("dashboard.clusterHealth")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-5">
            <Usage
              label={t("dashboard.cpuUsed")}
              used={metrics.data.cpu_used_m}
              total={metrics.data.cpu_capacity_m}
              render={formatCPU}
            />
            <Usage
              label={t("dashboard.memoryUsed")}
              used={metrics.data.memory_used_mb}
              total={metrics.data.memory_capacity_mb}
              render={formatMemory}
            />
            <Fact label={t("apps.instances")} value={String(metrics.data.pod_count)} />
            <Fact label="Kubelet" value={metrics.data.kubelet_version} mono />
          </CardContent>
        </Card>
      )}

      <Card className="border-destructive/30">
        <CardHeader>
          <CardTitle className="text-base text-destructive">{t("servers.removeServer")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-muted-foreground">{t("servers.removeServerWarning")}</p>
          <label className="flex items-center gap-2.5 text-sm">
            <Checkbox checked={wipe} onCheckedChange={(checked) => setWipe(checked === true)} />
            {t("servers.wipeServer")}
          </label>
          {remove.error != null && <ErrorDisplay error={remove.error} compact />}
          <Button
            variant="destructive"
            disabled={remove.isPending}
            onClick={() => {
              if (window.confirm(t("servers.removeServerWarning"))) remove.mutate()
            }}
          >
            <Trash2Icon className="size-4" />
            {t("servers.removeServer")}
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}

function LiveOperation({ operationId }: { operationId: string }) {
  const [operation, setOperation] = useState<Operation | null>(null)

  const query = useQuery({
    queryKey: ["operation", operationId],
    queryFn: () => api.get<Operation>(`/api/operations/${operationId}`),
  })

  useEvents([`operation:${operationId}`], {
    operation: (data) => setOperation(data as Operation),
  })

  const current = operation ?? query.data
  if (!current) return null

  return (
    <Card>
      <CardContent className="pt-6">
        <OperationProgress operation={current} />
      </CardContent>
    </Card>
  )
}

function Fact({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={mono ? "font-mono text-sm" : "text-sm"}>{value}</dd>
    </div>
  )
}

function Usage({
  label,
  used,
  total,
  render,
}: {
  label: string
  used: number
  total: number
  render: (value: number) => string
}) {
  const percent = total > 0 ? Math.min(100, Math.round((used / total) * 100)) : 0
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between text-sm">
        <span>{label}</span>
        <span className="tabular-nums text-muted-foreground">
          {render(used)} / {render(total)}
        </span>
      </div>
      <Progress value={percent} />
    </div>
  )
}
