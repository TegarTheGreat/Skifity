import { useState } from "react"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery } from "@tanstack/react-query"
import { BellIcon, PlusIcon, SendIcon, Trash2Icon } from "lucide-react"
import { toast } from "sonner"

import { EmptyState } from "@/components/empty-state"
import { ErrorDisplay } from "@/components/error-display"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { useSession } from "@/hooks/use-session"
import { api, type List } from "@/lib/api"
import { queryClient } from "@/lib/query"
import type { NotificationChannel } from "@/lib/types"

/** The events a channel can subscribe to, matching internal/notify. */
const EVENTS = [
  "deploy.succeeded",
  "deploy.failed",
  "app.unhealthy",
  "server.added",
  "server.lost",
  "backup.failed",
  "certificate.failed",
] as const

/** What each kind needs, so the form asks for exactly that and nothing else. */
const KINDS = {
  telegram: [
    { key: "bot_token", label: "Bot token", secret: true },
    { key: "chat_id", label: "Chat id", secret: false },
  ],
  discord: [{ key: "webhook_url", label: "Webhook URL", secret: true }],
  webhook: [{ key: "url", label: "URL", secret: false }],
  email: [{ key: "to", label: "To", secret: false }],
} as const

type Kind = keyof typeof KINDS

export function NotificationChannels() {
  const { t } = useTranslation()
  const { team } = useSession()
  const [adding, setAdding] = useState(false)

  const channels = useQuery({
    queryKey: ["notifications", team?.id],
    queryFn: () => api.get<List<NotificationChannel>>(`/api/teams/${team!.id}/notifications`),
    enabled: Boolean(team),
  })

  const remove = useMutation({
    mutationFn: (channelID: string) =>
      api.delete(`/api/teams/${team!.id}/notifications/${channelID}`),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["notifications", team?.id] }),
  })

  const sendTest = useMutation({
    mutationFn: (channelID: string) =>
      api.post(`/api/teams/${team!.id}/notifications/${channelID}/test`),
    onSuccess: () => toast.success(t("notifications.testSent")),
  })

  if (channels.isLoading) return <Skeleton className="h-48" />
  if (channels.error) {
    return <ErrorDisplay error={channels.error} onRetry={() => void channels.refetch()} />
  }

  const items = channels.data?.items ?? []

  return (
    <div className="space-y-4">
      <p className="max-w-2xl text-sm text-muted-foreground">{t("notifications.help")}</p>

      {items.length === 0 && !adding ? (
        <EmptyState
          icon={BellIcon}
          title={t("notifications.empty")}
          description={t("notifications.help")}
          action={
            <Button onClick={() => setAdding(true)}>
              <PlusIcon className="size-4" />
              {t("notifications.add")}
            </Button>
          }
        />
      ) : (
        <div className="space-y-3">
          {items.map((channel) => (
            <Card key={channel.id}>
              <CardContent className="flex flex-wrap items-center gap-3 py-4">
                <BellIcon className="size-4 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="truncate font-medium">{channel.name}</span>
                    <Badge variant="secondary" className="text-[10px]">
                      {t(`notifications.kind.${channel.kind}`, { defaultValue: channel.kind })}
                    </Badge>
                  </div>
                  <p className="truncate text-xs text-muted-foreground">
                    {channel.events
                      .split(",")
                      .filter(Boolean)
                      .map((event) => t(`notifications.event.${event}`, { defaultValue: event }))
                      .join(" · ") || t("common.none")}
                  </p>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={sendTest.isPending}
                  onClick={() => sendTest.mutate(channel.id)}
                >
                  <SendIcon className="size-4" />
                  {t("notifications.test")}
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={t("common.remove")}
                  disabled={remove.isPending}
                  onClick={() => remove.mutate(channel.id)}
                >
                  <Trash2Icon className="size-4 text-muted-foreground" />
                </Button>
              </CardContent>
            </Card>
          ))}

          {!adding && (
            <Button variant="outline" size="sm" onClick={() => setAdding(true)}>
              <PlusIcon className="size-4" />
              {t("notifications.add")}
            </Button>
          )}
        </div>
      )}

      {adding && <ChannelForm onDone={() => setAdding(false)} />}
      {remove.error != null && <ErrorDisplay error={remove.error} />}
      {sendTest.error != null && <ErrorDisplay error={sendTest.error} />}
    </div>
  )
}

function ChannelForm({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation()
  const { team } = useSession()
  const [kind, setKind] = useState<Kind>("telegram")
  const [name, setName] = useState("")
  const [config, setConfig] = useState<Record<string, string>>({})
  // Failures are what people actually want to be told about; successes are
  // opt-in so the channel does not become noise nobody reads.
  const [events, setEvents] = useState<string[]>(["deploy.failed", "app.unhealthy", "server.lost"])

  const create = useMutation({
    mutationFn: () =>
      api.post(`/api/teams/${team!.id}/notifications`, {
        kind,
        name: name.trim() || kind,
        config,
        events,
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["notifications", team?.id] })
      onDone()
    },
  })

  const fields = KINDS[kind]
  const complete = fields.every((field) => (config[field.key] ?? "").trim() !== "")

  return (
    <Card>
      <CardContent className="pt-6">
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault()
            create.mutate()
          }}
        >
          <Field>
            <FieldLabel htmlFor="channel-kind">{t("notifications.channelKind")}</FieldLabel>
            <Select
              value={kind}
              onValueChange={(next) => {
                setKind(next as Kind)
                // The fields are different per kind; keeping the old values
                // would send a Discord URL as a Telegram token.
                setConfig({})
              }}
            >
              <SelectTrigger id="channel-kind">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(Object.keys(KINDS) as Kind[]).map((candidate) => (
                  <SelectItem key={candidate} value={candidate}>
                    {t(`notifications.kind.${candidate}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>

          <Field>
            <FieldLabel htmlFor="channel-name">{t("notifications.channelName")}</FieldLabel>
            <Input
              id="channel-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder={t(`notifications.kind.${kind}`)}
            />
          </Field>

          {fields.map((field) => (
            <Field key={field.key}>
              <FieldLabel htmlFor={`channel-${field.key}`}>{field.label}</FieldLabel>
              <Input
                id={`channel-${field.key}`}
                type={field.secret ? "password" : "text"}
                autoComplete="off"
                value={config[field.key] ?? ""}
                onChange={(event) => setConfig({ ...config, [field.key]: event.target.value })}
                required
              />
            </Field>
          ))}

          <FieldSet>
            <FieldLegend variant="label">{t("notifications.events")}</FieldLegend>
            <FieldDescription>{t("notifications.eventsHelp")}</FieldDescription>
            <FieldGroup className="grid gap-2 pt-1 sm:grid-cols-2">
              {EVENTS.map((event) => (
                <FieldLabel key={event} htmlFor={`event-${event}`} className="font-normal">
                  <Checkbox
                    id={`event-${event}`}
                    checked={events.includes(event)}
                    onCheckedChange={(checked) =>
                      setEvents(
                        checked === true
                          ? [...events, event]
                          : events.filter((candidate) => candidate !== event),
                      )
                    }
                  />
                  {t(`notifications.event.${event}`)}
                </FieldLabel>
              ))}
            </FieldGroup>
          </FieldSet>

          {create.error != null && <ErrorDisplay error={create.error} compact />}

          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={onDone}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={!complete || create.isPending}>
              {create.isPending && <Spinner />}
              {create.isPending ? t("common.saving") : t("common.add")}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}
