import { useState } from "react"
import { useNavigate, useParams } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery } from "@tanstack/react-query"
import { ContainerIcon, GitBranchIcon, SparklesIcon } from "lucide-react"
import { cn } from "cn"

import { ErrorDisplay } from "@/components/error-display"
import { Page, PageHeader } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { useSession } from "@/hooks/use-session"
import { api, type List } from "@/lib/api"
import { queryClient } from "@/lib/query"
import type { App, Deployment, Detection, GitSource } from "@/lib/types"
import { Spinner } from "@/components/ui/spinner"

type SourceType = "git" | "image"

/**
 * Creating an app asks for as little as possible.
 *
 * A repository URL is enough: the builder is detected, the port comes from the
 * PORT variable the panel sets, and everything else has a safe default that can
 * be changed later from the app's own settings.
 */
export function NewAppPage() {
  const { t } = useTranslation()
  const { envId = "" } = useParams()
  const navigate = useNavigate()

  const [sourceType, setSourceType] = useState<SourceType>("git")
  const [name, setName] = useState("")
  const [repoURL, setRepoURL] = useState("")
  const [branch, setBranch] = useState("")
  const [rootDir, setRootDir] = useState("")
  const [image, setImage] = useState("")
  const [builder, setBuilder] = useState("auto")
  const [dockerfilePath, setDockerfilePath] = useState("")
  const [port, setPort] = useState("")
  const [healthPath, setHealthPath] = useState("")
  const [startCommand, setStartCommand] = useState("")
  const [deployNow, setDeployNow] = useState(true)
  const [advanced, setAdvanced] = useState(false)
  const [gitSourceID, setGitSourceID] = useState("")

  // A public repository needs no account. This picker only appears once one is
  // connected, so the common case stays a single field.
  const { team } = useSession()
  const gitSources = useQuery({
    queryKey: ["git-sources", team?.id],
    queryFn: () => api.get<List<GitSource>>(`/api/teams/${team!.id}/git-sources`),
    enabled: Boolean(team),
  })
  const sources = gitSources.data?.items ?? []

  // Looking at the repository before anything is created.
  //
  // Explicit rather than on every keystroke: a provider's rate limit is shared
  // by the whole panel, and a form that quietly makes requests while somebody
  // types is one that runs out of them on the day it matters.
  const detect = useMutation({
    mutationFn: () =>
      api.post<Detection>(`/api/teams/${team!.id}/detect`, {
        repo_url: repoURL.trim(),
        branch: branch.trim(),
        root_dir: rootDir.trim(),
        git_source_id: gitSourceID,
      }),
    onSuccess: (found) => {
      // Only fields nobody has filled in, and only when the guess is a
      // statement rather than a question. Overwriting what somebody typed
      // because a heuristic disagreed is the behaviour that makes people stop
      // trusting a form.
      if (found.confidence !== "high") return
      if (!port && found.port) setPort(String(found.port))
      if (!healthPath && found.health_path) setHealthPath(found.health_path)
      if (!startCommand && found.start_command) setStartCommand(found.start_command)
      if (builder === "auto" && (found.builder === "dockerfile" || found.builder === "static")) {
        setBuilder(found.builder)
      }
      if (!dockerfilePath && found.dockerfile_path) setDockerfilePath(found.dockerfile_path)
    },
  })
  const found = detect.data

  // "github.com/you/blog" becomes "blog", which is almost always the name the
  // user would have typed anyway.
  const suggestedName = (() => {
    if (sourceType === "image") return image.split("/").pop()?.split(":")[0] ?? ""
    return (
      repoURL
        .replace(/\.git$/, "")
        .split("/")
        .filter(Boolean)
        .pop() ?? ""
    )
  })()

  const create = useMutation({
    mutationFn: () =>
      api.post<App | { app: App; deployment: Deployment }>(`/api/environments/${envId}/apps`, {
        name: (name.trim() || suggestedName).trim(),
        source_type: sourceType,
        repo_url: sourceType === "git" ? repoURL.trim() : "",
        git_source_id: sourceType === "git" ? gitSourceID : "",
        branch: branch.trim(),
        root_dir: rootDir.trim(),
        image: sourceType === "image" ? image.trim() : "",
        builder,
        dockerfile_path: dockerfilePath.trim(),
        port: Number(port) || 0,
        health_path: healthPath.trim(),
        start_command: startCommand.trim(),
        deploy: deployNow,
      }),
    onSuccess: (result) => {
      const app = "app" in result ? result.app : result
      void queryClient.invalidateQueries({ queryKey: ["apps", envId] })
      navigate(`/apps/${app.id}`)
    },
  })

  const ready = sourceType === "git" ? repoURL.trim() !== "" : image.trim() !== ""

  return (
    <Page width="narrow">
      <PageHeader title={t("apps.newApp")} description={t("apps.emptyHelp")} />

      <form
        className="space-y-6"
        onSubmit={(event) => {
          event.preventDefault()
          create.mutate()
        }}
      >
        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("apps.source")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-2 gap-3">
              <SourceChoice
                active={sourceType === "git"}
                icon={GitBranchIcon}
                label={t("apps.sourceGit")}
                onClick={() => setSourceType("git")}
              />
              <SourceChoice
                active={sourceType === "image"}
                icon={ContainerIcon}
                label={t("apps.sourceImage")}
                onClick={() => setSourceType("image")}
              />
            </div>

            {sourceType === "git" ? (
              <>
                <Field>
                  <FieldLabel htmlFor="repo-url">{t("apps.repository")}</FieldLabel>
                  <Input
                    id="repo-url"
                    value={repoURL}
                    onChange={(event) => setRepoURL(event.target.value)}
                    placeholder={t("apps.repositoryPlaceholder")}
                    autoFocus
                    required
                  />
                  <FieldDescription>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="-ml-2 h-7"
                      disabled={!repoURL.trim() || detect.isPending}
                      onClick={() => detect.mutate()}
                    >
                      {detect.isPending ? <Spinner /> : <SparklesIcon className="size-3.5" />}
                      {t("apps.detect")}
                    </Button>
                  </FieldDescription>
                </Field>

                {detect.error != null && <ErrorDisplay error={detect.error} compact />}
                {found && <DetectionSummary detection={found} />}
                {sources.length > 0 && (
                  <Field>
                    <FieldLabel htmlFor="git-source">
                      {t("git.title")}{" "}
                      <span className="text-muted-foreground">({t("common.optional")})</span>
                    </FieldLabel>
                    <Select value={gitSourceID} onValueChange={setGitSourceID}>
                      <SelectTrigger id="git-source">
                        <SelectValue placeholder={t("common.none")} />
                      </SelectTrigger>
                      <SelectContent>
                        {sources.map((source) => (
                          <SelectItem key={source.id} value={source.id}>
                            {source.name}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <FieldDescription>{t("git.help")}</FieldDescription>
                  </Field>
                )}
                <Field>
                  <FieldLabel htmlFor="branch">
                    {t("apps.branch")}{" "}
                    <span className="text-muted-foreground">({t("common.optional")})</span>
                  </FieldLabel>
                  <Input
                    id="branch"
                    value={branch}
                    onChange={(event) => setBranch(event.target.value)}
                    placeholder="main"
                  />
                </Field>
              </>
            ) : (
              <Field>
                <FieldLabel htmlFor="image">{t("apps.image")}</FieldLabel>
                <Input
                  id="image"
                  value={image}
                  onChange={(event) => setImage(event.target.value)}
                  placeholder={t("apps.imagePlaceholder")}
                  className="font-mono"
                  autoFocus
                  required
                />
              </Field>
            )}

            <Field>
              <FieldLabel htmlFor="app-name">{t("apps.appName")}</FieldLabel>
              <Input
                id="app-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder={suggestedName}
              />
            </Field>
          </CardContent>
        </Card>

        <div>
          <Button type="button" variant="ghost" size="sm" onClick={() => setAdvanced(!advanced)}>
            {advanced ? t("common.hideAdvanced") : t("common.showAdvanced")}
          </Button>
        </div>

        {advanced && (
          <Card>
            <CardContent className="space-y-4 pt-6">
              <Field>
                <FieldLabel htmlFor="port">{t("apps.port")}</FieldLabel>
                <Input
                  id="port"
                  type="number"
                  min={1}
                  max={65535}
                  value={port}
                  onChange={(event) => setPort(event.target.value)}
                  placeholder="3000"
                />
                <FieldDescription>{t("apps.portHelp", { product: "Skifity" })}</FieldDescription>
              </Field>

              <Field>
                <FieldLabel htmlFor="health-path">{t("apps.healthPath")}</FieldLabel>
                <Input
                  id="health-path"
                  value={healthPath}
                  onChange={(event) => setHealthPath(event.target.value)}
                  placeholder="/healthz"
                  className="font-mono"
                />
                <FieldDescription>{t("apps.healthPathHelp")}</FieldDescription>
              </Field>

              {sourceType === "git" && (
                <>
                  <Field>
                    <FieldLabel htmlFor="root-dir">{t("apps.rootDirectory")}</FieldLabel>
                    <Input
                      id="root-dir"
                      value={rootDir}
                      onChange={(event) => setRootDir(event.target.value)}
                      placeholder="apps/web"
                      className="font-mono"
                    />
                    <FieldDescription>{t("apps.rootDirectoryHelp")}</FieldDescription>
                  </Field>

                  <Field>
                    <FieldLabel htmlFor="builder">{t("apps.builder")}</FieldLabel>
                    <Select value={builder} onValueChange={setBuilder}>
                      <SelectTrigger id="builder">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="auto">{t("apps.builderAuto")}</SelectItem>
                        <SelectItem value="dockerfile">{t("apps.builderDockerfile")}</SelectItem>
                        <SelectItem value="static">{t("apps.builderStatic")}</SelectItem>
                      </SelectContent>
                    </Select>
                  </Field>

                  {builder === "dockerfile" && (
                    <Field>
                      <FieldLabel htmlFor="dockerfile-path">
                        {t("apps.builderDockerfile")}
                      </FieldLabel>
                      <Input
                        id="dockerfile-path"
                        value={dockerfilePath}
                        onChange={(event) => setDockerfilePath(event.target.value)}
                        placeholder="Dockerfile"
                        className="font-mono"
                      />
                    </Field>
                  )}
                </>
              )}

              <Field>
                <FieldLabel htmlFor="start-command">{t("apps.startCommand")}</FieldLabel>
                <Input
                  id="start-command"
                  value={startCommand}
                  onChange={(event) => setStartCommand(event.target.value)}
                  className="font-mono"
                />
              </Field>
            </CardContent>
          </Card>
        )}

        <label className="flex items-center gap-2.5 text-sm">
          <Checkbox
            checked={deployNow}
            onCheckedChange={(checked) => setDeployNow(checked === true)}
          />
          {t("deploy.deployNow")}
        </label>

        {create.error && <ErrorDisplay error={create.error} />}

        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={() => navigate(-1)}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" disabled={!ready || create.isPending}>
            {create.isPending && <Spinner />}
            {create.isPending ? t("apps.deploying") : t("common.create")}
          </Button>
        </div>
      </form>
    </Page>
  )
}

function SourceChoice({
  active,
  icon: Icon,
  label,
  onClick,
}: {
  active: boolean
  icon: typeof GitBranchIcon
  label: string
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        "flex items-center gap-2.5 rounded-lg border px-3 py-3 text-sm transition-colors",
        active
          ? "border-primary bg-primary/5 font-medium"
          : "text-muted-foreground hover:border-primary/40 hover:text-foreground",
      )}
    >
      <Icon className="size-4 shrink-0" />
      {label}
    </button>
  )
}

/**
 * What the panel worked out about a repository.
 *
 * A guess presented as a guess. High confidence comes from a marker file the
 * repository's author put there — a package.json naming Next.js — and is stated;
 * anything lower is a question, and the fields below are left for the person to
 * answer. The notes are what make it reviewable rather than magic.
 */
function DetectionSummary({ detection }: { detection: Detection }) {
  const { t } = useTranslation()
  const sure = detection.confidence === "high"
  const what = [detection.framework, detection.language].filter(Boolean).join(" · ")

  return (
    <div
      className={cn(
        "rounded-lg border p-3 text-sm",
        sure ? "border-success/40 bg-success/5" : "border-dashed",
      )}
    >
      <p className="font-medium">
        {sure
          ? t("apps.detected", { what: what || detection.builder })
          : t("apps.detectedMaybe", { what: what || detection.builder })}
      </p>
      {detection.notes && detection.notes.length > 0 && (
        <ul className="mt-1.5 space-y-0.5 text-xs text-muted-foreground">
          {detection.notes.map((note) => (
            <li key={note}>{note}</li>
          ))}
        </ul>
      )}
      {detection.truncated && (
        <p className="mt-1.5 text-xs text-muted-foreground">{t("apps.detectedPartly")}</p>
      )}
    </div>
  )
}
