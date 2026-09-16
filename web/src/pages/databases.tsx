import { useMemo, useState } from "react"
import { Link } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { useQueries, useQuery } from "@tanstack/react-query"
import { DatabaseIcon, PlusIcon } from "lucide-react"

import { EmptyState } from "@/components/empty-state"
import { ErrorDisplay } from "@/components/error-display"
import { Page, PageHeader } from "@/components/page"
import { NewDatabaseDialog } from "@/pages/project-detail"
import { StatusBadge } from "@/components/status-badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { useSession } from "@/hooks/use-session"
import { api, type List } from "@/lib/api"
import { formatRelative } from "@/lib/format"
import type { Database, Environment, Project } from "@/lib/types"

/**
 * Every database in the team, whichever project it belongs to.
 *
 * Databases live in an environment, but people look for "my database" without
 * remembering which project that was, so the panel gathers them here and says
 * where each one lives.
 */
export function DatabasesPage() {
  const { t } = useTranslation()
  const { team } = useSession()
  const [creating, setCreating] = useState<string | null>(null)

  const projects = useQuery({
    queryKey: ["projects", team?.id],
    queryFn: () => api.get<List<Project>>(`/api/teams/${team!.id}/projects`),
    enabled: Boolean(team),
  })

  const projectItems = useMemo(() => projects.data?.items ?? [], [projects.data])

  const environmentQueries = useQueries({
    queries: projectItems.map((project) => ({
      queryKey: ["environments", project.id],
      queryFn: () => api.get<List<Environment>>(`/api/projects/${project.id}/environments`),
    })),
  })

  const environments = useMemo(() => {
    const out: { environment: Environment; project: Project }[] = []
    environmentQueries.forEach((query, index) => {
      for (const environment of query.data?.items ?? []) {
        out.push({ environment, project: projectItems[index] })
      }
    })
    return out
  }, [environmentQueries, projectItems])

  const databaseQueries = useQueries({
    queries: environments.map(({ environment }) => ({
      queryKey: ["databases", environment.id],
      queryFn: () => api.get<List<Database>>(`/api/environments/${environment.id}/databases`),
    })),
  })

  const rows = useMemo(() => {
    const out: { database: Database; environment: Environment; project: Project }[] = []
    databaseQueries.forEach((query, index) => {
      for (const database of query.data?.items ?? []) {
        out.push({ database, ...environments[index] })
      }
    })
    return out
  }, [databaseQueries, environments])

  const loading =
    projects.isLoading ||
    environmentQueries.some((query) => query.isLoading) ||
    databaseQueries.some((query) => query.isLoading)

  if (projects.error) {
    return <ErrorDisplay error={projects.error} onRetry={() => void projects.refetch()} />
  }

  return (
    <Page>
      <PageHeader
        title={t("databases.title")}
        description={t("databases.emptyHelp")}
        actions={
          environments.length > 0 && (
            <Select value={creating ?? ""} onValueChange={setCreating}>
              <SelectTrigger className="w-56">
                <SelectValue placeholder={t("databases.newDatabase")} />
              </SelectTrigger>
              <SelectContent>
                {environments.map(({ environment, project }) => (
                  <SelectItem key={environment.id} value={environment.id}>
                    {project.name} · {environment.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )
        }
      />

      {loading ? (
        <Skeleton className="h-48" />
      ) : rows.length === 0 ? (
        <EmptyState
          icon={DatabaseIcon}
          title={t("databases.empty")}
          description={
            projectItems.length === 0 ? t("projects.emptyHelp") : t("databases.emptyHelp")
          }
          action={
            projectItems.length === 0 ? (
              <Button asChild>
                <Link to="/projects">
                  <PlusIcon className="size-4" />
                  {t("projects.newProject")}
                </Link>
              </Button>
            ) : (
              <Button onClick={() => setCreating(environments[0]?.environment.id ?? null)}>
                <PlusIcon className="size-4" />
                {t("databases.newDatabase")}
              </Button>
            )
          }
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("common.name")}</TableHead>
                  <TableHead>{t("databases.engine")}</TableHead>
                  <TableHead className="hidden sm:table-cell">{t("nav.projects")}</TableHead>
                  <TableHead>{t("common.status")}</TableHead>
                  <TableHead className="hidden md:table-cell">{t("databases.storage")}</TableHead>
                  <TableHead className="hidden lg:table-cell">{t("common.created")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map(({ database, environment, project }) => (
                  <TableRow key={database.id}>
                    <TableCell className="font-medium">
                      <Link to={`/databases/${database.id}`} className="hover:text-primary">
                        {database.name}
                      </Link>
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {database.engine} {database.engine_version}
                    </TableCell>
                    <TableCell className="hidden text-xs text-muted-foreground sm:table-cell">
                      {project.name} · {environment.name}
                    </TableCell>
                    <TableCell>
                      <StatusBadge
                        status={database.status}
                        label={t(`databases.status.${database.status}`, {
                          defaultValue: database.status,
                        })}
                      />
                    </TableCell>
                    <TableCell className="hidden tabular-nums md:table-cell">
                      {database.storage_gb} GB
                    </TableCell>
                    <TableCell className="hidden text-xs text-muted-foreground lg:table-cell">
                      {formatRelative(database.created_at)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      {creating && (
        <NewDatabaseDialog
          environmentId={creating}
          open
          onOpenChange={(open) => {
            if (!open) setCreating(null)
          }}
        />
      )}
    </Page>
  )
}
