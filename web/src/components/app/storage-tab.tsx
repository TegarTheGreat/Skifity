import { useState } from "react"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery } from "@tanstack/react-query"
import { HardDriveIcon, PlusIcon, Trash2Icon } from "lucide-react"

import { EmptyState } from "@/components/empty-state"
import { ErrorDisplay } from "@/components/error-display"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Field, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api, type List } from "@/lib/api"
import { queryClient } from "@/lib/query"
import type { App, Volume } from "@/lib/types"

export function StorageTab({ app }: { app: App }) {
  const { t } = useTranslation()
  const [adding, setAdding] = useState(false)
  const [name, setName] = useState("data")
  const [mountPath, setMountPath] = useState("/data")
  const [sizeGB, setSizeGB] = useState("5")

  const volumes = useQuery({
    queryKey: ["volumes", app.id],
    queryFn: () => api.get<List<Volume>>(`/api/apps/${app.id}/volumes`),
  })

  const add = useMutation({
    mutationFn: () =>
      api.post(`/api/apps/${app.id}/volumes`, {
        name: name.trim(),
        mount_path: mountPath.trim(),
        size_gb: Number(sizeGB) || 1,
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["volumes", app.id] })
      setAdding(false)
    },
  })

  const remove = useMutation({
    mutationFn: (volumeID: string) => api.delete(`/api/apps/${app.id}/volumes/${volumeID}`),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["volumes", app.id] }),
  })

  if (volumes.isLoading) return <Skeleton className="h-40" />
  if (volumes.error)
    return <ErrorDisplay error={volumes.error} onRetry={() => void volumes.refetch()} />

  const items = volumes.data?.items ?? []

  return (
    <div className="space-y-4">
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
              <div className="grid gap-4 sm:grid-cols-3">
                <Field>
                  <FieldLabel htmlFor="volume-name">{t("common.name")}</FieldLabel>
                  <Input
                    id="volume-name"
                    value={name}
                    onChange={(event) => setName(event.target.value)}
                    className="font-mono"
                    required
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor="volume-path">{t("apps.storage")}</FieldLabel>
                  <Input
                    id="volume-path"
                    value={mountPath}
                    onChange={(event) => setMountPath(event.target.value)}
                    className="font-mono"
                    placeholder="/data"
                    required
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor="volume-size">{t("common.size")}</FieldLabel>
                  <Input
                    id="volume-size"
                    type="number"
                    min={1}
                    value={sizeGB}
                    onChange={(event) => setSizeGB(event.target.value)}
                  />
                </Field>
              </div>
              <p className="text-xs text-muted-foreground">{t("databases.storageHelp")}</p>
              {add.error != null && <ErrorDisplay error={add.error} compact />}
              <div className="flex justify-end gap-2">
                <Button type="button" variant="ghost" onClick={() => setAdding(false)}>
                  {t("common.cancel")}
                </Button>
                <Button type="submit" disabled={add.isPending}>
                  {add.isPending && <Spinner />}
                  {add.isPending ? t("common.saving") : t("common.add")}
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>
      )}

      {items.length === 0 && !adding ? (
        <EmptyState
          icon={HardDriveIcon}
          title={t("apps.storage")}
          description={t("scaling.spreadHelp")}
          action={
            <Button onClick={() => setAdding(true)}>
              <PlusIcon className="size-4" />
              {t("common.add")}
            </Button>
          }
        />
      ) : (
        <>
          <div className="flex justify-end">
            <Button size="sm" onClick={() => setAdding(true)}>
              <PlusIcon className="size-4" />
              {t("common.add")}
            </Button>
          </div>
          {items.length > 0 && (
            <Card>
              <CardContent className="p-0">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t("common.name")}</TableHead>
                      <TableHead>{t("apps.storage")}</TableHead>
                      <TableHead>{t("common.size")}</TableHead>
                      <TableHead className="w-10" />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {items.map((volume) => (
                      <TableRow key={volume.id}>
                        <TableCell className="font-mono text-xs font-medium">
                          {volume.name}
                        </TableCell>
                        <TableCell className="font-mono text-xs">{volume.mount_path}</TableCell>
                        <TableCell className="tabular-nums">{volume.size_gb} GB</TableCell>
                        <TableCell>
                          <Button
                            variant="ghost"
                            size="icon"
                            aria-label={t("common.delete")}
                            disabled={remove.isPending}
                            onClick={() => remove.mutate(volume.id)}
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
          )}
        </>
      )}

      {remove.error != null && <ErrorDisplay error={remove.error} compact />}
    </div>
  )
}
