import { useMemo, useState } from "react"
import { useNavigate } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { useMutation, useQueries, useQuery } from "@tanstack/react-query"
import { toast } from "sonner"
import { BoxesIcon, ExternalLinkIcon, SearchIcon } from "lucide-react"

import { EmptyState } from "@/components/empty-state"
import { ErrorDisplay } from "@/components/error-display"
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
import { useSession } from "@/hooks/use-session"
import { api, type List } from "@/lib/api"
import type { Environment, Project, Template } from "@/lib/types"

export function TemplatesPage() {
  const { t } = useTranslation()
  const [search, setSearch] = useState("")
  const [installing, setInstalling] = useState<Template | null>(null)

  const templates = useQuery({
    queryKey: ["templates"],
    queryFn: () => api.get<List<Template>>("/api/templates"),
  })

  const items = useMemo(() => templates.data?.items ?? [], [templates.data])
  const shown = useMemo(() => {
    const needle = search.trim().toLowerCase()
    if (!needle) return items
    return items.filter(
      (template) =>
        template.name.toLowerCase().includes(needle) ||
        template.description.toLowerCase().includes(needle) ||
        template.category.toLowerCase().includes(needle),
    )
  }, [items, search])

  const categories = useMemo(
    () => [...new Set(shown.map((template) => template.category))].sort(),
    [shown],
  )

  return (
    <div className="mx-auto max-w-6xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t("templates.title")}</h1>
        <p className="text-sm text-muted-foreground">{t("templates.subtitle")}</p>
      </div>

      <div className="relative max-w-sm">
        <SearchIcon className="absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder={t("templates.search")}
          className="pl-9"
        />
      </div>

      {templates.isLoading ? (
        <Skeleton className="h-64" />
      ) : templates.error ? (
        <ErrorDisplay error={templates.error} onRetry={() => void templates.refetch()} />
      ) : shown.length === 0 ? (
        <EmptyState
          icon={BoxesIcon}
          title={t("templates.title")}
          description={t("templates.subtitle")}
        />
      ) : (
        categories.map((category) => (
          <section key={category} className="space-y-3">
            <h2 className="text-sm font-medium text-muted-foreground">{category}</h2>
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {shown
                .filter((template) => template.category === category)
                .map((template) => (
                  <Card key={template.id} className="flex h-full flex-col">
                    <CardHeader>
                      <div className="flex items-start justify-between gap-2">
                        <CardTitle className="truncate text-base">{template.name}</CardTitle>
                        {template.beta && (
                          <Badge variant="outline" className="text-[10px]">
                            {t("common.beta")}
                          </Badge>
                        )}
                      </div>
                      <CardDescription className="line-clamp-3">
                        {template.description}
                      </CardDescription>
                    </CardHeader>
                    <CardContent className="mt-auto flex items-center gap-2">
                      <Button size="sm" onClick={() => setInstalling(template)}>
                        {t("templates.install")}
                      </Button>
                      {template.website && (
                        <Button variant="ghost" size="sm" asChild>
                          <a href={template.website} target="_blank" rel="noreferrer">
                            <ExternalLinkIcon className="size-3.5" />
                            {t("templates.website")}
                          </a>
                        </Button>
                      )}
                    </CardContent>
                  </Card>
                ))}
            </div>
          </section>
        ))
      )}

      {installing && <InstallDialog template={installing} onClose={() => setInstalling(null)} />}
    </div>
  )
}

function InstallDialog({ template, onClose }: { template: Template; onClose: () => void }) {
  const { t } = useTranslation()
  const { team } = useSession()
  const navigate = useNavigate()
  const [environmentId, setEnvironmentId] = useState("")
  const [name, setName] = useState(template.name)
  const [values, setValues] = useState<Record<string, string>>(
    Object.fromEntries((template.inputs ?? []).map((input) => [input.key, input.default ?? ""])),
  )

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

  const install = useMutation({
    mutationFn: () =>
      api.post(`/api/templates/${template.id}/install`, {
        environment_id: environmentId,
        name: name.trim(),
        // A generated value is filled in by the panel, so anything left empty
        // is sent empty rather than as an accidental literal.
        values,
      }),
    onSuccess: () => {
      toast.success(t("templates.installedNote"))
      onClose()
      navigate("/projects")
    },
  })

  // Inputs the panel generates itself do not need to be asked for.
  const asked = (template.inputs ?? []).filter((input) => !input.generate)

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{template.name}</DialogTitle>
          <DialogDescription>{template.description}</DialogDescription>
        </DialogHeader>

        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            install.mutate()
          }}
        >
          <div className="space-y-2">
            <Label htmlFor="install-environment">{t("projects.environments")}</Label>
            {environments.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t("projects.emptyHelp")}</p>
            ) : (
              <Select value={environmentId} onValueChange={setEnvironmentId}>
                <SelectTrigger id="install-environment">
                  <SelectValue placeholder={t("projects.environments")} />
                </SelectTrigger>
                <SelectContent>
                  {environments.map(({ environment, project }) => (
                    <SelectItem key={environment.id} value={environment.id}>
                      {project.name} · {environment.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </div>

          <div className="space-y-2">
            <Label htmlFor="install-name">{t("apps.appName")}</Label>
            <Input
              id="install-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </div>

          {asked.map((input) => (
            <div key={input.key} className="space-y-2">
              <Label htmlFor={`input-${input.key}`}>
                {input.label}
                {!input.required && (
                  <span className="text-muted-foreground"> ({t("common.optional")})</span>
                )}
              </Label>
              <Input
                id={`input-${input.key}`}
                type={input.secret ? "password" : "text"}
                value={values[input.key] ?? ""}
                onChange={(event) => setValues({ ...values, [input.key]: event.target.value })}
                required={input.required}
              />
              {input.help && <p className="text-xs text-muted-foreground">{input.help}</p>}
            </div>
          ))}

          {template.notes && (
            <p className="rounded-md bg-muted p-3 text-xs text-muted-foreground">
              {template.notes}
            </p>
          )}

          {install.error != null && <ErrorDisplay error={install.error} compact />}

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={!environmentId || install.isPending}>
              {install.isPending ? t("templates.installing") : t("templates.install")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
