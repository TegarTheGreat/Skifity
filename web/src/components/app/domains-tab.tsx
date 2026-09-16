import { useState } from "react"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery } from "@tanstack/react-query"
import { ExternalLinkIcon, GlobeIcon, LockIcon, PlusIcon, Trash2Icon } from "lucide-react"

import { ErrorDisplay } from "@/components/error-display"
import { StatusBadge } from "@/components/status-badge"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { api, type List } from "@/lib/api"
import { queryClient } from "@/lib/query"
import type { App, Domain } from "@/lib/types"

export function DomainsTab({ app }: { app: App }) {
  const { t } = useTranslation()
  const [hostname, setHostname] = useState("")
  const [adding, setAdding] = useState(false)

  const domains = useQuery({
    queryKey: ["domains", app.id],
    queryFn: () => api.get<List<Domain>>(`/api/apps/${app.id}/domains`),
  })

  const add = useMutation({
    mutationFn: () => api.post(`/api/apps/${app.id}/domains`, { hostname: hostname.trim() }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["domains", app.id] })
      setHostname("")
      setAdding(false)
    },
  })

  const remove = useMutation({
    mutationFn: (domainID: string) => api.delete(`/api/apps/${app.id}/domains/${domainID}`),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["domains", app.id] }),
  })

  if (domains.isLoading) return <Skeleton className="h-48" />
  if (domains.error)
    return <ErrorDisplay error={domains.error} onRetry={() => void domains.refetch()} />

  const items = domains.data?.items ?? []

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <p className="max-w-xl text-sm text-muted-foreground">
          {t("domains.automaticHelp", { product: "Skifity" })}
        </p>
        <Button size="sm" onClick={() => setAdding(true)}>
          <PlusIcon className="size-4" />
          {t("domains.addDomain")}
        </Button>
      </div>

      {adding && (
        <Card>
          <CardContent className="pt-6">
            <form
              className="space-y-4"
              onSubmit={(event) => {
                event.preventDefault()
                add.mutate()
              }}
            >
              <div className="space-y-2">
                <Label htmlFor="hostname">{t("domains.hostname")}</Label>
                <Input
                  id="hostname"
                  value={hostname}
                  onChange={(event) => setHostname(event.target.value)}
                  placeholder={t("domains.hostnamePlaceholder")}
                  autoFocus
                  required
                />
              </div>
              {add.error != null && <ErrorDisplay error={add.error} compact />}
              <div className="flex justify-end gap-2">
                <Button type="button" variant="ghost" onClick={() => setAdding(false)}>
                  {t("common.cancel")}
                </Button>
                <Button type="submit" disabled={!hostname.trim() || add.isPending}>
                  {add.isPending ? t("common.saving") : t("common.add")}
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>
      )}

      <div className="space-y-3">
        {items.map((domain) => (
          <Card key={domain.id}>
            <CardContent className="space-y-3 py-4">
              <div className="flex flex-wrap items-center gap-3">
                <GlobeIcon className="size-4 shrink-0 text-muted-foreground" />
                <a
                  href={`${domain.tls ? "https" : "http"}://${domain.hostname}`}
                  target="_blank"
                  rel="noreferrer"
                  className="flex min-w-0 items-center gap-1.5 truncate font-medium hover:text-primary"
                >
                  <span className="truncate">{domain.hostname}</span>
                  <ExternalLinkIcon className="size-3.5 shrink-0" />
                </a>
                {domain.tls && (
                  <Badge variant="outline" className="gap-1 text-[10px]">
                    <LockIcon className="size-2.5" />
                    {t("domains.https")}
                  </Badge>
                )}
                {domain.auto && (
                  <Badge variant="secondary" className="text-[10px]">
                    {t("domains.automatic")}
                  </Badge>
                )}
                <StatusBadge
                  status={domain.status}
                  label={t(`domains.status${statusKey(domain.status)}`, {
                    defaultValue: domain.status,
                  })}
                  className="ml-auto"
                />
                {!domain.auto && (
                  <Button
                    variant="ghost"
                    size="icon"
                    aria-label={t("domains.removeDomain")}
                    disabled={remove.isPending}
                    onClick={() => remove.mutate(domain.id)}
                  >
                    <Trash2Icon className="size-4 text-muted-foreground" />
                  </Button>
                )}
              </div>

              {domain.status === "pending" && !domain.auto && (
                <Alert>
                  <AlertTitle>{t("domains.dnsInstructions")}</AlertTitle>
                  <AlertDescription>
                    {domain.status_detail ||
                      t("domains.dnsRecord", {
                        hostname: domain.hostname,
                        ip: t("common.unknown"),
                      })}
                  </AlertDescription>
                </Alert>
              )}
            </CardContent>
          </Card>
        ))}
      </div>

      {remove.error != null && <ErrorDisplay error={remove.error} compact />}
    </div>
  )
}

function statusKey(status: string): string {
  switch (status) {
    case "active":
      return "Active"
    case "failed":
      return "Failed"
    default:
      return "Pending"
  }
}
