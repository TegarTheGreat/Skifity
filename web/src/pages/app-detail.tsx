import { useSearchParams, useParams } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery } from "@tanstack/react-query"
import {
  BoxIcon,
  ExternalLinkIcon,
  RefreshCwIcon,
  RocketIcon,
} from "lucide-react"

import { AdvancedTab } from "@/components/app/advanced-tab"
import { DeployButton, DeploymentsTab } from "@/components/app/deployments-tab"
import { DomainsTab } from "@/components/app/domains-tab"
import { LogsTab } from "@/components/app/logs-tab"
import { ScalingTab } from "@/components/app/scaling-tab"
import { SettingsTab } from "@/components/app/settings-tab"
import { StorageTab } from "@/components/app/storage-tab"
import { useConfirm } from "@/components/confirm-dialog"
import { ErrorDisplay } from "@/components/error-display"
import { Page, PageHeader } from "@/components/page"
import { StatusBadge } from "@/components/status-badge"
import { VariablesEditor } from "@/components/variables-editor"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useEvents } from "@/hooks/use-events"
import { useSession } from "@/hooks/use-session"
import { api } from "@/lib/api"
import { formatCPU, formatMemory, formatRelative } from "@/lib/format"
import { queryClient } from "@/lib/query"
import type { App, AppStatus } from "@/lib/types"

export function AppDetailPage() {
  const { t } = useTranslation()
  const { appId = "" } = useParams()
  const { team } = useSession()
  const confirm = useConfirm()
  const [params, setParams] = useSearchParams()
  const tab = params.get("tab") ?? "overview"

  const app = useQuery({
    queryKey: ["app", appId],
    queryFn: () => api.get<App>(`/api/apps/${appId}`),
  })

  const status = useQuery({
    queryKey: ["app-status", appId],
    queryFn: () => api.get<AppStatus>(`/api/apps/${appId}/status`),
    // A rollout changes what is running from one second to the next, and the
    // event stream only carries deployment state, not instance state.
    refetchInterval: 10_000,
  })

  useEvents(
    team ? [`team:${team.id}`] : [],
    {
      deployment: () => {
        void queryClient.invalidateQueries({ queryKey: ["app-status", appId] })
        void queryClient.invalidateQueries({ queryKey: ["deployments", appId] })
      },
    },
    Boolean(team),
  )

  const restart = useMutation({
    mutationFn: () => api.post(`/api/apps/${appId}/restart`),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["app-status", appId] }),
  })

  if (app.error) return <ErrorDisplay error={app.error} onRetry={() => void app.refetch()} />
  if (app.isLoading || !app.data) return <Skeleton className="h-96" />

  const current = app.data

  return (
    <Page>
      <PageHeader
        icon={BoxIcon}
        title={current.name}
        badge={
          <StatusBadge
            status={status.data?.phase ?? current.status}
            label={t(`apps.phase.${status.data?.phase ?? current.status}`, {
              defaultValue: status.data?.phase ?? current.status,
            })}
          />
        }
        actions={
          <>
          <Button
            variant="outline"
            disabled={restart.isPending}
            onClick={() => {
              void confirm({
                title: t("apps.restart"),
                description: t("apps.restartConfirm"),
                confirmLabel: t("apps.restart"),
              }).then((yes) => {
                if (yes) restart.mutate()
              })
            }}
          >
            <RefreshCwIcon />
            {t("apps.restart")}
          </Button>
          <DeployButton appId={appId} />
          </>
        }
      />

      {/* The app's addresses belong directly under its name: on this page they
          are the thing people came for. */}
      {(status.data?.urls?.length ?? 0) > 0 && (
        <div className="-mt-3 flex flex-wrap items-center gap-2">
          {status.data?.urls?.map((url) => (
            <Button key={url} variant="outline" size="sm" asChild>
              <a href={url} target="_blank" rel="noreferrer">
                <ExternalLinkIcon />
                {url.replace(/^https?:\/\//, "")}
              </a>
            </Button>
          ))}
        </div>
      )}

      {restart.error != null && <ErrorDisplay error={restart.error} />}

      <Tabs
        value={tab}
        onValueChange={(next) => setParams(next === "overview" ? {} : { tab: next })}
      >
        <TabsList className="flex-wrap">
          <TabsTrigger value="overview">{t("apps.overview")}</TabsTrigger>
          <TabsTrigger value="deployments">{t("apps.deployments")}</TabsTrigger>
          <TabsTrigger value="logs">{t("apps.logs")}</TabsTrigger>
          <TabsTrigger value="variables">{t("apps.variables")}</TabsTrigger>
          <TabsTrigger value="domains">{t("apps.domains")}</TabsTrigger>
          <TabsTrigger value="scaling">{t("apps.scaling")}</TabsTrigger>
          <TabsTrigger value="storage">{t("apps.storage")}</TabsTrigger>
          <TabsTrigger value="settings">{t("apps.settings")}</TabsTrigger>
          <TabsTrigger value="advanced">{t("apps.advanced")}</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="pt-4">
          <Overview app={current} status={status.data} loading={status.isLoading} />
        </TabsContent>
        <TabsContent value="deployments" className="pt-4">
          <DeploymentsTab app={current} />
        </TabsContent>
        <TabsContent value="logs" className="pt-4">
          <LogsTab app={current} />
        </TabsContent>
        <TabsContent value="variables" className="pt-4">
          <VariablesEditor
            base={`/api/apps/${appId}`}
            queryKey={["variables", appId]}
            showBuildTime
          />
        </TabsContent>
        <TabsContent value="domains" className="pt-4">
          <DomainsTab app={current} />
        </TabsContent>
        <TabsContent value="scaling" className="pt-4">
          <ScalingTab app={current} />
        </TabsContent>
        <TabsContent value="storage" className="pt-4">
          <StorageTab app={current} />
        </TabsContent>
        <TabsContent value="settings" className="pt-4">
          <SettingsTab app={current} />
        </TabsContent>
        <TabsContent value="advanced" className="pt-4">
          <AdvancedTab app={current} />
        </TabsContent>
      </Tabs>
    </Page>
  )
}

function Overview({ app, status, loading }: { app: App; status?: AppStatus; loading: boolean }) {
  const { t } = useTranslation()

  if (loading) return <Skeleton className="h-64" />

  const instances = status?.instances ?? []

  return (
    <div className="space-y-6">
      <div className="grid gap-4 sm:grid-cols-3">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-xs font-medium text-muted-foreground">
              {t("apps.instances")}
            </CardTitle>
          </CardHeader>
          <CardContent className="text-2xl font-semibold tabular-nums">
            {status?.ready_replicas ?? 0}
            <span className="text-muted-foreground"> / {status?.desired_replicas ?? 0}</span>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-xs font-medium text-muted-foreground">
              {t("apps.source")}
            </CardTitle>
          </CardHeader>
          <CardContent className="truncate font-mono text-sm">
            {app.source_type === "image" ? app.image : app.repo_url || "—"}
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-xs font-medium text-muted-foreground">
              {t("apps.image")}
            </CardTitle>
          </CardHeader>
          <CardContent className="truncate font-mono text-xs text-muted-foreground">
            {status?.image || "—"}
          </CardContent>
        </Card>
      </div>

      {status?.detail && <p className="text-sm text-muted-foreground">{status.detail}</p>}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("apps.instances")}</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {instances.length === 0 ? (
            <div className="space-y-4 px-6 pb-6 text-sm text-muted-foreground">
              <p>{t("apps.noInstances")}</p>
              <div className="flex">
                <DeployButton appId={app.id} />
              </div>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("apps.instanceName")}</TableHead>
                  <TableHead>{t("common.status")}</TableHead>
                  <TableHead className="hidden sm:table-cell">{t("apps.node")}</TableHead>
                  <TableHead className="hidden md:table-cell">{t("apps.restarts")}</TableHead>
                  <TableHead className="hidden md:table-cell">{t("scaling.cpuReserved")}</TableHead>
                  <TableHead className="hidden md:table-cell">
                    {t("scaling.memoryReserved")}
                  </TableHead>
                  <TableHead className="hidden lg:table-cell">{t("common.created")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {instances.map((instance) => (
                  <TableRow key={instance.name}>
                    <TableCell className="font-mono text-xs">{instance.name}</TableCell>
                    <TableCell>
                      <StatusBadge
                        status={instance.ready ? "running" : instance.status}
                        label={t(`apps.phase.${instance.status}`, {
                          defaultValue: instance.status,
                        })}
                      />
                    </TableCell>
                    <TableCell className="hidden font-mono text-xs sm:table-cell">
                      {instance.node}
                    </TableCell>
                    <TableCell className="hidden tabular-nums md:table-cell">
                      {instance.restarts}
                    </TableCell>
                    <TableCell className="hidden tabular-nums md:table-cell">
                      {instance.cpu_m ? formatCPU(instance.cpu_m) : "—"}
                    </TableCell>
                    <TableCell className="hidden tabular-nums md:table-cell">
                      {instance.memory_mb ? formatMemory(instance.memory_mb) : "—"}
                    </TableCell>
                    <TableCell className="hidden text-xs text-muted-foreground lg:table-cell">
                      {instance.started_at ? formatRelative(instance.started_at) : "—"}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {instances.some((instance) => instance.message) && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <RocketIcon className="size-4" />
              {t("errors.details")}
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-1 text-sm text-muted-foreground">
            {instances
              .filter((instance) => instance.message)
              .map((instance) => (
                <p key={instance.name}>
                  <span className="font-mono text-xs">{instance.name}</span>: {instance.message}
                </p>
              ))}
          </CardContent>
        </Card>
      )}
    </div>
  )
}
