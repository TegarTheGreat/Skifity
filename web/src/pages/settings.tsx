import { useState } from "react"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery } from "@tanstack/react-query"
import {
  CheckCircle2Icon,
  DownloadIcon,
  KeyRoundIcon,
  PackagePlusIcon,
  PlusIcon,
  Trash2Icon,
} from "lucide-react"
import { toast } from "sonner"

import { useConfirm } from "@/components/confirm-dialog"
import { ErrorDisplay } from "@/components/error-display"
import { GitSources } from "@/components/settings/git-sources"
import { NotificationChannels } from "@/components/settings/notification-channels"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
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
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useSession } from "@/hooks/use-session"
import { api, type List } from "@/lib/api"
import { formatDateTime } from "@/lib/format"
import { queryClient } from "@/lib/query"
import type { AuditEvent, Component, Role, Setting, User } from "@/lib/types"

const GROUPS: { key: string; label: string }[] = [
  { key: "general", label: "settings.general" },
  { key: "domains", label: "settings.domains" },
  { key: "git", label: "settings.git" },
  { key: "storage", label: "settings.storage" },
  { key: "dns", label: "settings.dns" },
  { key: "email", label: "settings.email" },
  { key: "notifications", label: "settings.notifications" },
  { key: "registry", label: "settings.registry" },
]

export function SettingsPage() {
  const { t } = useTranslation()

  return (
    <div className="mx-auto max-w-4xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t("settings.title")}</h1>
      </div>

      <Tabs defaultValue="panel">
        <TabsList className="flex-wrap">
          <TabsTrigger value="panel">{t("settings.general")}</TabsTrigger>
          <TabsTrigger value="git">{t("settings.git")}</TabsTrigger>
          <TabsTrigger value="notifications">{t("settings.notifications")}</TabsTrigger>
          <TabsTrigger value="components">{t("settings.components")}</TabsTrigger>
          <TabsTrigger value="members">{t("settings.members")}</TabsTrigger>
          <TabsTrigger value="security">{t("settings.security")}</TabsTrigger>
          <TabsTrigger value="audit">{t("settings.auditLog")}</TabsTrigger>
        </TabsList>

        <TabsContent value="panel" className="space-y-6 pt-4">
          {/* Git has its own tab, where the connected accounts are. */}
          <SettingGroups except={["git"]} />
          <VersionCard />
        </TabsContent>
        <TabsContent value="git" className="space-y-6 pt-4">
          <GitSources />
          <SettingGroups only={["git"]} />
        </TabsContent>
        <TabsContent value="notifications" className="pt-4">
          <NotificationChannels />
        </TabsContent>
        <TabsContent value="components" className="pt-4">
          <ComponentsPanel />
        </TabsContent>
        <TabsContent value="members" className="pt-4">
          <MembersPanel />
        </TabsContent>
        <TabsContent value="security" className="pt-4">
          <SecurityPanel />
        </TabsContent>
        <TabsContent value="audit" className="pt-4">
          <AuditPanel />
        </TabsContent>
      </Tabs>
    </div>
  )
}

function SettingGroups({ only, except }: { only?: string[]; except?: string[] }) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<Record<string, string>>({})

  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: () => api.get<List<Setting>>("/api/settings"),
  })

  const save = useMutation({
    mutationFn: () => api.put("/api/settings", { values: draft }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["settings"] })
      setDraft({})
      toast.success(t("settings.saved"))
    },
  })

  if (settings.isLoading) return <Skeleton className="h-96" />
  if (settings.error) {
    return <ErrorDisplay error={settings.error} onRetry={() => void settings.refetch()} />
  }

  const items = settings.data?.items ?? []
  const dirty = Object.keys(draft).length > 0
  const shown = GROUPS.filter(
    (group) => (!only || only.includes(group.key)) && (!except || !except.includes(group.key)),
  )

  /** The value a control should show: the edit in progress, else what is stored. */
  const valueOf = (setting: Setting) =>
    draft[setting.key] ?? (setting.secret ? "" : (setting.value ?? ""))

  const set = (key: string, value: string) => setDraft({ ...draft, [key]: value })

  return (
    <div className="space-y-6">
      {shown.map((group) => {
        const groupItems = items.filter((setting) => setting.group === group.key)
        if (groupItems.length === 0) return null
        return (
          <Card key={group.key}>
            <CardHeader>
              <CardTitle className="text-base">{t(group.label)}</CardTitle>
            </CardHeader>
            <CardContent>
              <FieldGroup>
                {groupItems.map((setting) => (
                  <SettingField
                    key={setting.key}
                    setting={setting}
                    value={valueOf(setting)}
                    edited={setting.key in draft}
                    onChange={(value) => set(setting.key, value)}
                  />
                ))}
              </FieldGroup>
            </CardContent>
          </Card>
        )
      })}

      {save.error != null && <ErrorDisplay error={save.error} />}

      {/* The bar only appears once something has changed, so it is never a
          button that does nothing. */}
      {dirty && (
        <div className="sticky bottom-4 z-10 flex items-center justify-between gap-3 rounded-lg border bg-card/95 px-4 py-3 shadow-lg backdrop-blur">
          <span className="text-sm text-muted-foreground">
            {t("settings.unsaved", { count: Object.keys(draft).length })}
          </span>
          <div className="flex gap-2">
            <Button variant="ghost" size="sm" onClick={() => setDraft({})} disabled={save.isPending}>
              {t("common.cancel")}
            </Button>
            <Button size="sm" disabled={save.isPending} onClick={() => save.mutate()}>
              {save.isPending && <Spinner />}
              {save.isPending ? t("common.saving") : t("common.save")}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}

/**
 * One setting, drawn as the kind of thing it actually is.
 *
 * Every setting used to be a text box, so "Use STARTTLS" was a field where you
 * had to guess whether it wanted "true", "yes" or "on" — and the server only
 * accepted some of those.
 */
function SettingField({
  setting,
  value,
  edited,
  onChange,
}: {
  setting: Setting
  value: string
  edited: boolean
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  const id = `setting-${setting.key}`
  const placeholder =
    setting.secret && setting.configured ? t("settings.secretStored") : setting.placeholder

  if (setting.kind === "bool") {
    return (
      <Field orientation="horizontal">
        <FieldContent>
          <FieldLabel htmlFor={id}>{setting.label}</FieldLabel>
          <FieldDescription>{setting.help}</FieldDescription>
        </FieldContent>
        <Switch
          id={id}
          checked={value === "true"}
          onCheckedChange={(checked) => onChange(checked ? "true" : "false")}
        />
      </Field>
    )
  }

  return (
    <Field data-edited={edited || undefined}>
      <FieldLabel htmlFor={id}>
        {setting.label}
        {setting.secret && setting.configured && !edited && (
          <Badge variant="outline" className="ml-2 text-[10px]">
            {t("settings.configured")}
          </Badge>
        )}
        {edited && (
          <Badge variant="secondary" className="ml-2 text-[10px]">
            {t("settings.edited")}
          </Badge>
        )}
      </FieldLabel>

      {setting.kind === "choice" ? (
        <Select value={value} onValueChange={onChange}>
          <SelectTrigger id={id}>
            <SelectValue placeholder={setting.placeholder ?? t("common.none")} />
          </SelectTrigger>
          <SelectContent>
            {(setting.options ?? []).map((option) => (
              <SelectItem key={option} value={option}>
                {option}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : setting.multiline ? (
        <Textarea
          id={id}
          value={value}
          rows={6}
          placeholder={placeholder}
          onChange={(event) => onChange(event.target.value)}
          className="font-mono text-xs"
        />
      ) : (
        <Input
          id={id}
          value={value}
          placeholder={placeholder}
          onChange={(event) => onChange(event.target.value)}
          type={inputType(setting)}
          inputMode={setting.kind === "number" ? "numeric" : undefined}
          autoComplete={setting.secret ? "new-password" : "off"}
          spellCheck={false}
        />
      )}

      <FieldDescription>{setting.help}</FieldDescription>
    </Field>
  )
}

function inputType(setting: Setting): string {
  if (setting.secret) return "password"
  switch (setting.kind) {
    case "number":
      return "number"
    case "email":
      return "email"
    case "url":
      return "url"
    default:
      return "text"
  }
}

function VersionCard() {
  const { t } = useTranslation()
  const { meta } = useSession()

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("settings.upgrade")}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        <div className="flex items-center justify-between">
          <span className="text-muted-foreground">{t("settings.currentVersion")}</span>
          <span className="font-mono">{meta?.version ?? "—"}</span>
        </div>
        <p className="text-xs text-muted-foreground">
          {t("settings.noUpdateCheck", { product: meta?.product ?? "Skifity" })}
        </p>
      </CardContent>
    </Card>
  )
}

function ComponentsPanel() {
  const { t } = useTranslation()

  const components = useQuery({
    queryKey: ["components"],
    queryFn: () => api.get<List<Component>>("/api/components"),
  })

  const install = useMutation({
    mutationFn: (name: string) => api.post(`/api/components/${name}/install`),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["components"] }),
  })

  if (components.isLoading) return <Skeleton className="h-64" />
  if (components.error) {
    return <ErrorDisplay error={components.error} onRetry={() => void components.refetch()} />
  }

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">{t("settings.componentsHelp")}</p>
      <div className="space-y-3">
        {components.data?.items.map((component) => (
          <Card key={component.name}>
            <CardContent className="flex flex-wrap items-center gap-4 py-4">
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{component.title}</span>
                  {component.beta && (
                    <Badge variant="outline" className="text-[10px]">
                      {t("common.beta")}
                    </Badge>
                  )}
                  {component.approximate_memory_mb > 0 && (
                    <span className="text-xs text-muted-foreground">
                      {t("settings.componentMemory", { mb: component.approximate_memory_mb })}
                    </span>
                  )}
                </div>
                <p className="mt-0.5 text-sm text-muted-foreground">{component.description}</p>
                {component.detail && (
                  <p className="mt-0.5 text-xs text-muted-foreground">{component.detail}</p>
                )}
              </div>

              {component.status === "installed" ? (
                <span className="flex items-center gap-1.5 text-sm text-success">
                  <CheckCircle2Icon className="size-4" />
                  {t("settings.componentInstalled")}
                </span>
              ) : (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={install.isPending}
                  onClick={() => install.mutate(component.name)}
                >
                  <PackagePlusIcon className="size-4" />
                  {t("settings.componentInstall")}
                </Button>
              )}
            </CardContent>
          </Card>
        ))}
      </div>
      {install.error != null && <ErrorDisplay error={install.error} />}
    </div>
  )
}

function MembersPanel() {
  const { t } = useTranslation()
  const { team } = useSession()
  const [email, setEmail] = useState("")
  const [role, setRole] = useState<Role>("member")

  const members = useQuery({
    queryKey: ["members", team?.id],
    queryFn: () => api.get<List<{ user: User; role: Role }>>(`/api/teams/${team!.id}/members`),
    enabled: Boolean(team),
  })

  const add = useMutation({
    mutationFn: () => api.post(`/api/teams/${team!.id}/members`, { email: email.trim(), role }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["members", team?.id] })
      setEmail("")
    },
  })

  const remove = useMutation({
    mutationFn: (userID: string) => api.delete(`/api/teams/${team!.id}/members/${userID}`),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["members", team?.id] }),
  })

  if (members.isLoading) return <Skeleton className="h-64" />
  if (members.error)
    return <ErrorDisplay error={members.error} onRetry={() => void members.refetch()} />

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">{t("settings.roleHelp")}</p>

      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("common.name")}</TableHead>
                <TableHead>{t("settings.memberRole")}</TableHead>
                <TableHead className="w-10" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {members.data?.items.map((member) => (
                <TableRow key={member.user.id}>
                  <TableCell>
                    <div className="font-medium">{member.user.name || member.user.email}</div>
                    <div className="text-xs text-muted-foreground">{member.user.email}</div>
                  </TableCell>
                  <TableCell>
                    <Badge variant="secondary">
                      {t(
                        `settings.role${member.role.charAt(0).toUpperCase()}${member.role.slice(1)}`,
                        {
                          defaultValue: member.role,
                        },
                      )}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={t("settings.removeMember")}
                      disabled={remove.isPending}
                      onClick={() => remove.mutate(member.user.id)}
                    >
                      <Trash2Icon className="size-4 text-muted-foreground" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("settings.inviteMember")}</CardTitle>
        </CardHeader>
        <CardContent>
          <form
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault()
              add.mutate()
            }}
          >
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="member-email">{t("settings.memberEmail")}</Label>
                <Input
                  id="member-email"
                  type="email"
                  value={email}
                  onChange={(event) => setEmail(event.target.value)}
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="member-role">{t("settings.memberRole")}</Label>
                <Select value={role} onValueChange={(value) => setRole(value as Role)}>
                  <SelectTrigger id="member-role">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="member">{t("settings.roleMember")}</SelectItem>
                    <SelectItem value="admin">{t("settings.roleAdmin")}</SelectItem>
                    <SelectItem value="owner">{t("settings.roleOwner")}</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
            {add.error != null && <ErrorDisplay error={add.error} compact />}
            <div className="flex justify-end">
              <Button type="submit" disabled={!email.trim() || add.isPending}>
                <PlusIcon className="size-4" />
                {add.isPending ? t("common.saving") : t("settings.inviteMember")}
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>

      {remove.error != null && <ErrorDisplay error={remove.error} />}
    </div>
  )
}

function SecurityPanel() {
  const { t } = useTranslation()
  const { meta } = useSession()
  const [recoveryKey, setRecoveryKey] = useState<string | null>(null)
  const confirm = useConfirm()

  const reveal = useMutation({
    mutationFn: () => api.get<{ recovery_key: string }>("/api/security/recovery-key"),
    onSuccess: (data) => setRecoveryKey(data.recovery_key),
  })

  const rotate = useMutation({
    mutationFn: () =>
      api.post<{ rewrapped: number; failed: number; recovery_key: string }>(
        "/api/security/rotate-key",
      ),
    onSuccess: (data) => {
      setRecoveryKey(data.recovery_key)
      // A partial rotation is not a failure: the old key is kept so the
      // secrets that did not rewrap stay readable. Say which one happened.
      if (data.failed > 0) {
        toast.warning(t("settings.rotateKeyPartial"))
      } else {
        toast.success(t("settings.rotateKeyDone"))
      }
    },
  })

  const download = () => {
    if (!recoveryKey) return
    const blob = new Blob([`${recoveryKey}\n`], { type: "text/plain" })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement("a")
    anchor.href = url
    anchor.download = "skifity-recovery-key.txt"
    anchor.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("settings.recoveryKey")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-muted-foreground">
            {t("auth.recoveryKeyIntro", { product: meta?.product ?? "Skifity" })}
          </p>
          {recoveryKey ? (
            <div className="space-y-3">
              <pre className="rounded-md border bg-muted p-3 font-mono text-xs break-all whitespace-pre-wrap">
                {recoveryKey}
              </pre>
              <Alert>
                <AlertTitle>{t("auth.recoveryKeyWarning")}</AlertTitle>
                <AlertDescription>{t("databases.credentialsWarning")}</AlertDescription>
              </Alert>
              <Button variant="outline" size="sm" onClick={download}>
                <DownloadIcon className="size-4" />
                {t("auth.download")}
              </Button>
            </div>
          ) : (
            <Button variant="outline" disabled={reveal.isPending} onClick={() => reveal.mutate()}>
              <KeyRoundIcon className="size-4" />
              {t("settings.recoveryKey")}
            </Button>
          )}
          {reveal.error != null && <ErrorDisplay error={reveal.error} compact />}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("settings.rotateKey")}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-muted-foreground">{t("settings.rotateKeyHelp")}</p>
          {rotate.error != null && <ErrorDisplay error={rotate.error} compact />}
          <Button
            variant="outline"
            disabled={rotate.isPending}
            onClick={() => {
              void confirm({
                title: t("settings.rotateKey"),
                description: t("settings.rotateKeyHelp"),
                confirmLabel: t("settings.rotateKeyConfirm"),
              }).then((yes) => {
                if (yes) rotate.mutate()
              })
            }}
          >
            {rotate.isPending ? t("common.saving") : t("settings.rotateKeyConfirm")}
          </Button>
        </CardContent>
      </Card>
    </div>
  )
}

function AuditPanel() {
  const { t } = useTranslation()
  const { team } = useSession()

  const audit = useQuery({
    queryKey: ["audit", team?.id],
    queryFn: () => api.get<List<AuditEvent>>(`/api/teams/${team!.id}/audit`),
    enabled: Boolean(team),
  })

  if (audit.isLoading) return <Skeleton className="h-64" />
  if (audit.error) return <ErrorDisplay error={audit.error} onRetry={() => void audit.refetch()} />

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">{t("settings.auditLogHelp")}</p>
      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("common.updated")}</TableHead>
                <TableHead>{t("common.actions")}</TableHead>
                <TableHead className="hidden sm:table-cell">{t("common.name")}</TableHead>
                <TableHead className="hidden md:table-cell">IP</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {audit.data?.items.map((event) => (
                <TableRow key={event.id}>
                  <TableCell className="text-xs whitespace-nowrap">
                    {formatDateTime(event.at)}
                  </TableCell>
                  <TableCell>
                    <div className="font-mono text-xs">{event.action}</div>
                    <div className="text-xs text-muted-foreground">{event.actor_label}</div>
                  </TableCell>
                  <TableCell className="hidden text-xs sm:table-cell">
                    {event.target_label || event.target_type}
                  </TableCell>
                  <TableCell className="hidden font-mono text-xs text-muted-foreground md:table-cell">
                    {event.ip}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  )
}
