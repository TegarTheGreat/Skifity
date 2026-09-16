import { useMemo, useState } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery } from "@tanstack/react-query"
import {
  ArrowRightIcon,
  BoxIcon,
  DatabaseIcon,
  ExternalLinkIcon,
  MoreHorizontalIcon,
  PlusIcon,
  RocketIcon,
  Trash2Icon,
} from "lucide-react"

import { EmptyState } from "@/components/empty-state"
import { useDeleteConfirm } from "@/components/confirm-dialog"
import { ErrorDisplay } from "@/components/error-display"
import { StatusBadge } from "@/components/status-badge"
import { VariablesEditor } from "@/components/variables-editor"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useSession } from "@/hooks/use-session"
import { api, type List } from "@/lib/api"
import { queryClient } from "@/lib/query"
import type { App, CanvasEdge, CanvasNode, Database, Environment, Project } from "@/lib/types"

export function ProjectDetailPage() {
  const { t } = useTranslation()
  const { projectId = "" } = useParams()
  const navigate = useNavigate()
  const confirmDelete = useDeleteConfirm()
  const { team } = useSession()
  const [chosen, setChosen] = useState<string | null>(null)
  const [newEnvironment, setNewEnvironment] = useState(false)

  const project = useQuery({
    queryKey: ["project", projectId],
    queryFn: () => api.get<Project>(`/api/projects/${projectId}`),
  })

  const environments = useQuery({
    queryKey: ["environments", projectId],
    queryFn: () => api.get<List<Environment>>(`/api/projects/${projectId}/environments`),
  })

  const items = useMemo(() => environments.data?.items ?? [], [environments.data])

  // Which environment is shown is derived, not stored: the first one until the
  // user picks another, and back to the first if the one they picked is
  // deleted from under the page. Keeping it in state would need an effect to
  // repair it, and that effect would render the broken state first.
  const environmentId = items.some((environment) => environment.id === chosen)
    ? chosen!
    : (items[0]?.id ?? "")
  const setEnvironmentId = setChosen

  const remove = useMutation({
    mutationFn: () => api.delete(`/api/projects/${projectId}`),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["projects", team?.id] })
      navigate("/projects")
    },
  })

  if (project.error) {
    return <ErrorDisplay error={project.error} onRetry={() => void project.refetch()} />
  }

  return (
    <div className="mx-auto max-w-6xl space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          {project.isLoading ? (
            <Skeleton className="h-8 w-48" />
          ) : (
            <h1 className="truncate text-2xl font-semibold tracking-tight">{project.data?.name}</h1>
          )}
          {project.data?.description && (
            <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
              {project.data.description}
            </p>
          )}
        </div>

        <div className="flex items-center gap-2">
          {environmentId && (
            <Button asChild>
              <Link to={`/environments/${environmentId}/apps/new`}>
                <PlusIcon className="size-4" />
                {t("apps.newApp")}
              </Link>
            </Button>
          )}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="icon" aria-label={t("common.actions")}>
                <MoreHorizontalIcon className="size-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={() => setNewEnvironment(true)}>
                <PlusIcon className="size-4" />
                {t("projects.newEnvironment")}
              </DropdownMenuItem>
              <DropdownMenuItem
                variant="destructive"
                onSelect={() => {
                  void confirmDelete(
                    project.data?.name ?? "",
                    t("projects.deleteProjectWarning"),
                    t("common.cannotBeUndone"),
                  ).then((yes) => {
                    if (yes) remove.mutate()
                  })
                }}
              >
                <Trash2Icon className="size-4" />
                {t("projects.deleteProject")}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {remove.error && <ErrorDisplay error={remove.error} />}

      {items.length > 1 && (
        <div className="flex items-center gap-2">
          <Label htmlFor="environment" className="text-sm text-muted-foreground">
            {t("projects.environments")}
          </Label>
          <Select value={environmentId} onValueChange={setEnvironmentId}>
            <SelectTrigger id="environment" className="w-56">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {items.map((environment) => (
                <SelectItem key={environment.id} value={environment.id}>
                  {environment.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      )}

      <Tabs defaultValue="services">
        <TabsList>
          <TabsTrigger value="services">{t("nav.apps")}</TabsTrigger>
          <TabsTrigger value="canvas">{t("projects.canvas")}</TabsTrigger>
          <TabsTrigger value="variables">{t("projects.sharedVariables")}</TabsTrigger>
        </TabsList>

        <TabsContent value="services" className="space-y-6 pt-4">
          {environments.isLoading ? (
            <Skeleton className="h-40" />
          ) : environmentId ? (
            <EnvironmentServices environmentId={environmentId} />
          ) : null}
        </TabsContent>

        <TabsContent value="canvas" className="pt-4">
          <ProjectCanvas projectId={projectId} />
        </TabsContent>

        <TabsContent value="variables" className="pt-4">
          <VariablesEditor
            base={`/api/projects/${projectId}`}
            queryKey={["shared-variables", projectId]}
            description={t("projects.sharedVariablesHelp")}
          />
        </TabsContent>
      </Tabs>

      <NewEnvironmentDialog
        projectId={projectId}
        open={newEnvironment}
        onOpenChange={setNewEnvironment}
        onCreated={(environment) => setEnvironmentId(environment.id)}
      />
    </div>
  )
}

function EnvironmentServices({ environmentId }: { environmentId: string }) {
  const { t } = useTranslation()
  const [newDatabase, setNewDatabase] = useState(false)

  const apps = useQuery({
    queryKey: ["apps", environmentId],
    queryFn: () => api.get<List<App>>(`/api/environments/${environmentId}/apps`),
  })
  const databases = useQuery({
    queryKey: ["databases", environmentId],
    queryFn: () => api.get<List<Database>>(`/api/environments/${environmentId}/databases`),
  })

  const noServices =
    (apps.data?.items.length ?? 0) === 0 && (databases.data?.items.length ?? 0) === 0

  if (apps.error) return <ErrorDisplay error={apps.error} onRetry={() => void apps.refetch()} />

  return (
    <div className="space-y-6">
      {apps.isLoading ? (
        <Skeleton className="h-32" />
      ) : noServices ? (
        <EmptyState
          icon={RocketIcon}
          title={t("apps.empty")}
          description={t("apps.emptyHelp")}
          action={
            <div className="flex flex-wrap justify-center gap-2">
              <Button asChild>
                <Link to={`/environments/${environmentId}/apps/new`}>
                  <PlusIcon className="size-4" />
                  {t("apps.newApp")}
                </Link>
              </Button>
              <Button variant="outline" asChild>
                <Link to="/templates">{t("templates.title")}</Link>
              </Button>
            </div>
          }
        />
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {apps.data?.items.map((app) => (
            <Link key={app.id} to={`/apps/${app.id}`} className="group">
              <Card className="h-full transition-colors group-hover:border-primary/50">
                <CardHeader>
                  <div className="flex items-start justify-between gap-2">
                    <CardTitle className="truncate text-base">{app.name}</CardTitle>
                    <StatusBadge status={app.status} />
                  </div>
                  <CardDescription className="truncate font-mono text-xs">
                    {app.source_type === "image" ? app.image : app.repo_url}
                  </CardDescription>
                </CardHeader>
              </Card>
            </Link>
          ))}
          {databases.data?.items.map((database) => (
            <Link key={database.id} to={`/databases/${database.id}`} className="group">
              <Card className="h-full transition-colors group-hover:border-primary/50">
                <CardHeader>
                  <div className="flex items-start justify-between gap-2">
                    <CardTitle className="flex min-w-0 items-center gap-2 text-base">
                      <DatabaseIcon className="size-4 shrink-0 text-muted-foreground" />
                      <span className="truncate">{database.name}</span>
                    </CardTitle>
                    <StatusBadge
                      status={database.status}
                      label={t(`databases.status.${database.status}`, {
                        defaultValue: database.status,
                      })}
                    />
                  </div>
                  <CardDescription className="font-mono text-xs">
                    {database.engine} {database.engine_version}
                  </CardDescription>
                </CardHeader>
              </Card>
            </Link>
          ))}
        </div>
      )}

      <div className="flex flex-wrap gap-2">
        <Button variant="outline" size="sm" onClick={() => setNewDatabase(true)}>
          <DatabaseIcon className="size-4" />
          {t("databases.newDatabase")}
        </Button>
      </div>

      <NewDatabaseDialog
        environmentId={environmentId}
        open={newDatabase}
        onOpenChange={setNewDatabase}
      />
    </div>
  )
}

/**
 * The project canvas: which apps talk to which databases.
 *
 * Deliberately not a draggable graph. What people actually need is to see the
 * shape of a project at a glance and click through, and a layout they cannot
 * accidentally rearrange stays readable on a phone.
 */
function ProjectCanvas({ projectId }: { projectId: string }) {
  const { t } = useTranslation()

  const canvas = useQuery({
    queryKey: ["canvas", projectId],
    queryFn: () =>
      api.get<{ nodes: CanvasNode[]; edges: CanvasEdge[] }>(`/api/projects/${projectId}/canvas`),
  })

  if (canvas.isLoading) return <Skeleton className="h-64" />
  if (canvas.error)
    return <ErrorDisplay error={canvas.error} onRetry={() => void canvas.refetch()} />

  const nodes = canvas.data?.nodes ?? []
  const edges = canvas.data?.edges ?? []
  if (nodes.length === 0) {
    return (
      <EmptyState icon={BoxIcon} title={t("apps.empty")} description={t("projects.canvasHelp")} />
    )
  }

  const byId = new Map(nodes.map((node) => [node.id, node]))
  const environments = [...new Set(nodes.map((node) => node.environment))]

  return (
    <div className="space-y-8">
      <p className="text-sm text-muted-foreground">{t("projects.canvasHelp")}</p>
      {environments.map((environment) => (
        <section key={environment} className="space-y-3">
          <h2 className="text-sm font-medium text-muted-foreground">{environment}</h2>
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {nodes
              .filter((node) => node.environment === environment)
              .map((node) => {
                const outgoing = edges.filter((edge) => edge.from === node.id)
                return (
                  <Card key={node.id}>
                    <CardHeader>
                      <div className="flex items-start justify-between gap-2">
                        <CardTitle className="flex min-w-0 items-center gap-2 text-sm">
                          {node.kind === "database" ? (
                            <DatabaseIcon className="size-4 shrink-0 text-muted-foreground" />
                          ) : (
                            <BoxIcon className="size-4 shrink-0 text-muted-foreground" />
                          )}
                          <Link
                            to={
                              node.kind === "database"
                                ? `/databases/${node.id}`
                                : `/apps/${node.id}`
                            }
                            className="truncate hover:text-primary"
                          >
                            {node.name}
                          </Link>
                        </CardTitle>
                        <StatusBadge status={node.status} />
                      </div>
                      {node.detail && (
                        <CardDescription className="font-mono text-xs">
                          {node.detail}
                        </CardDescription>
                      )}
                    </CardHeader>
                    {(outgoing.length > 0 || (node.urls?.length ?? 0) > 0) && (
                      <CardContent className="space-y-1.5 text-xs">
                        {node.urls?.map((url) => (
                          <a
                            key={url}
                            href={url}
                            target="_blank"
                            rel="noreferrer"
                            className="flex items-center gap-1.5 text-primary hover:underline"
                          >
                            <ExternalLinkIcon className="size-3 shrink-0" />
                            <span className="truncate">{url.replace(/^https?:\/\//, "")}</span>
                          </a>
                        ))}
                        {outgoing.map((edge) => (
                          <div
                            key={`${edge.from}-${edge.to}-${edge.label}`}
                            className="flex items-center gap-1.5 text-muted-foreground"
                          >
                            <ArrowRightIcon className="size-3 shrink-0" />
                            <span className="truncate">{byId.get(edge.to)?.name ?? edge.to}</span>
                            <Badge variant="outline" className="font-mono text-[10px]">
                              {edge.label}
                            </Badge>
                          </div>
                        ))}
                      </CardContent>
                    )}
                  </Card>
                )
              })}
          </div>
        </section>
      ))}
    </div>
  )
}

function NewEnvironmentDialog({
  projectId,
  open,
  onOpenChange,
  onCreated,
}: {
  projectId: string
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: (environment: Environment) => void
}) {
  const { t } = useTranslation()
  const [name, setName] = useState("")

  const create = useMutation({
    mutationFn: () =>
      api.post<Environment>(`/api/projects/${projectId}/environments`, { name: name.trim() }),
    onSuccess: (environment) => {
      void queryClient.invalidateQueries({ queryKey: ["environments", projectId] })
      onCreated(environment)
      setName("")
      onOpenChange(false)
    },
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("projects.newEnvironment")}</DialogTitle>
          <DialogDescription>{t("projects.sharedVariablesHelp")}</DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            create.mutate()
          }}
        >
          <div className="space-y-2">
            <Label htmlFor="environment-name">{t("projects.environmentName")}</Label>
            <Input
              id="environment-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="staging"
              autoFocus
              required
            />
          </div>
          {create.error && <ErrorDisplay error={create.error} compact />}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={!name.trim() || create.isPending}>
              {create.isPending ? t("common.saving") : t("common.create")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function NewDatabaseDialog({
  environmentId,
  open,
  onOpenChange,
}: {
  environmentId: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [name, setName] = useState("")
  const [engine, setEngine] = useState("postgres")
  const [storage, setStorage] = useState("10")

  const create = useMutation({
    mutationFn: () =>
      api.post<Database>(`/api/environments/${environmentId}/databases`, {
        name: name.trim(),
        engine,
        storage_gb: Number(storage) || 10,
      }),
    onSuccess: (database) => {
      void queryClient.invalidateQueries({ queryKey: ["databases", environmentId] })
      onOpenChange(false)
      navigate(`/databases/${database.id}`)
    },
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("databases.newDatabase")}</DialogTitle>
          <DialogDescription>{t("databases.emptyHelp")}</DialogDescription>
        </DialogHeader>
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            create.mutate()
          }}
        >
          <div className="space-y-2">
            <Label htmlFor="database-name">{t("databases.databaseName")}</Label>
            <Input
              id="database-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              autoFocus
              required
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="database-engine">{t("databases.engine")}</Label>
            <Select value={engine} onValueChange={setEngine}>
              <SelectTrigger id="database-engine">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="postgres">{t("databases.postgres")}</SelectItem>
                <SelectItem value="redis">{t("databases.redis")}</SelectItem>
                <SelectItem value="mysql">{t("databases.mysql")}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="database-storage">{t("databases.storage")}</Label>
            <Input
              id="database-storage"
              type="number"
              min={1}
              value={storage}
              onChange={(event) => setStorage(event.target.value)}
            />
            <p className="text-xs text-muted-foreground">{t("databases.storageHelp")}</p>
          </div>
          {create.error && <ErrorDisplay error={create.error} compact />}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={!name.trim() || create.isPending}>
              {create.isPending ? t("common.saving") : t("common.create")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
