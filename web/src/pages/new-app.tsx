import { useState } from "react"
import { useNavigate, useParams } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { useMutation } from "@tanstack/react-query"
import { ContainerIcon, GitBranchIcon } from "lucide-react"
import { cn } from "cn"

import { ErrorDisplay } from "@/components/error-display"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { api } from "@/lib/api"
import { queryClient } from "@/lib/query"
import type { App, Deployment } from "@/lib/types"

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
    <div className="mx-auto max-w-2xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t("apps.newApp")}</h1>
        <p className="text-sm text-muted-foreground">{t("apps.emptyHelp")}</p>
      </div>

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
                <div className="space-y-2">
                  <Label htmlFor="repo-url">{t("apps.repository")}</Label>
                  <Input
                    id="repo-url"
                    value={repoURL}
                    onChange={(event) => setRepoURL(event.target.value)}
                    placeholder={t("apps.repositoryPlaceholder")}
                    autoFocus
                    required
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="branch">
                    {t("apps.branch")}{" "}
                    <span className="text-muted-foreground">({t("common.optional")})</span>
                  </Label>
                  <Input
                    id="branch"
                    value={branch}
                    onChange={(event) => setBranch(event.target.value)}
                    placeholder="main"
                  />
                </div>
              </>
            ) : (
              <div className="space-y-2">
                <Label htmlFor="image">{t("apps.image")}</Label>
                <Input
                  id="image"
                  value={image}
                  onChange={(event) => setImage(event.target.value)}
                  placeholder={t("apps.imagePlaceholder")}
                  className="font-mono"
                  autoFocus
                  required
                />
              </div>
            )}

            <div className="space-y-2">
              <Label htmlFor="app-name">{t("apps.appName")}</Label>
              <Input
                id="app-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder={suggestedName}
              />
            </div>
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
              <div className="space-y-2">
                <Label htmlFor="port">{t("apps.port")}</Label>
                <Input
                  id="port"
                  type="number"
                  min={1}
                  max={65535}
                  value={port}
                  onChange={(event) => setPort(event.target.value)}
                  placeholder="3000"
                />
                <p className="text-xs text-muted-foreground">
                  {t("apps.portHelp", { product: "Skifity" })}
                </p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="health-path">{t("apps.healthPath")}</Label>
                <Input
                  id="health-path"
                  value={healthPath}
                  onChange={(event) => setHealthPath(event.target.value)}
                  placeholder="/healthz"
                  className="font-mono"
                />
                <p className="text-xs text-muted-foreground">{t("apps.healthPathHelp")}</p>
              </div>

              {sourceType === "git" && (
                <>
                  <div className="space-y-2">
                    <Label htmlFor="root-dir">{t("apps.rootDirectory")}</Label>
                    <Input
                      id="root-dir"
                      value={rootDir}
                      onChange={(event) => setRootDir(event.target.value)}
                      placeholder="apps/web"
                      className="font-mono"
                    />
                    <p className="text-xs text-muted-foreground">{t("apps.rootDirectoryHelp")}</p>
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="builder">{t("apps.builder")}</Label>
                    <Select value={builder} onValueChange={setBuilder}>
                      <SelectTrigger id="builder">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="auto">{t("apps.builderAuto")}</SelectItem>
                        <SelectItem value="dockerfile">{t("apps.builderDockerfile")}</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>

                  {builder === "dockerfile" && (
                    <div className="space-y-2">
                      <Label htmlFor="dockerfile-path">{t("apps.builderDockerfile")}</Label>
                      <Input
                        id="dockerfile-path"
                        value={dockerfilePath}
                        onChange={(event) => setDockerfilePath(event.target.value)}
                        placeholder="Dockerfile"
                        className="font-mono"
                      />
                    </div>
                  )}
                </>
              )}

              <div className="space-y-2">
                <Label htmlFor="start-command">{t("apps.startCommand")}</Label>
                <Input
                  id="start-command"
                  value={startCommand}
                  onChange={(event) => setStartCommand(event.target.value)}
                  className="font-mono"
                />
              </div>
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
            {create.isPending ? t("apps.deploying") : t("common.create")}
          </Button>
        </div>
      </form>
    </div>
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
