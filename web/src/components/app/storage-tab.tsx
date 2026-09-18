import { useState } from "react"
import { useTranslation } from "react-i18next"
import { useMutation, useQuery } from "@tanstack/react-query"
import { DownloadIcon, HardDriveIcon, PlusIcon, Trash2Icon } from "lucide-react"
import { toast } from "sonner"

import { useDeleteConfirm } from "@/components/confirm-dialog"
import { EmptyState } from "@/components/empty-state"
import { ErrorDisplay, toProblem } from "@/components/error-display"
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
import { formatRelative } from "@/lib/format"
import type { App, Backup, Volume } from "@/lib/types"

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

  // The panel makes somebody type the name before it deletes an app, a
  // project, a database or a server. This was one click on a bin icon, and it
  // is the one that destroys data nothing can get back: an app can be deployed
  // again from the same commit, a disk cannot.
  const confirmDelete = useDeleteConfirm()
  const askThenRemove = (volume: Volume) => {
    void confirmDelete(volume.name, t("apps.deleteVolumeConfirm", { path: volume.mount_path }), t("apps.deleteVolumeConsequence")).then(
      (yes) => {
        if (yes) remove.mutate(volume.id)
      },
    )
  }

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
                  <FieldLabel htmlFor="volume-path">{t("apps.mountPath")}</FieldLabel>
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
                  <FieldLabel htmlFor="volume-size">{t("apps.sizeGB")}</FieldLabel>
                  <Input
                    id="volume-size"
                    type="number"
                    min={1}
                    value={sizeGB}
                    onChange={(event) => setSizeGB(event.target.value)}
                  />
                </Field>
              </div>
              <p className="text-xs text-muted-foreground">{t("apps.volumeHelp")}</p>
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
          title={t("apps.noVolumes")}
          // It used to borrow the scaling tab's sentence about spreading
          // instances across servers, which is about something else entirely.
          description={t("apps.noVolumesHelp")}
          action={
            <Button onClick={() => setAdding(true)}>
              <PlusIcon className="size-4" />
              {t("apps.addVolume")}
            </Button>
          }
        />
      ) : (
        <>
          <div className="flex justify-end">
            <Button size="sm" onClick={() => setAdding(true)}>
              <PlusIcon className="size-4" />
              {t("apps.addVolume")}
            </Button>
          </div>
          {items.length > 0 && (
            <Card>
              <CardContent className="p-0">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t("common.name")}</TableHead>
                      <TableHead>{t("apps.mountPath")}</TableHead>
                      <TableHead>{t("common.size")}</TableHead>
                      <TableHead>{t("apps.lastBackup")}</TableHead>
                      <TableHead className="w-24" />
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
                        <LastBackup appID={app.id} volumeID={volume.id} />
                        <TableCell>
                          <div className="flex justify-end gap-1">
                            <BackUpNow appID={app.id} volumeID={volume.id} />
                            <Button
                              variant="ghost"
                              size="icon"
                              aria-label={t("common.delete")}
                              disabled={remove.isPending}
                              onClick={() => askThenRemove(volume)}
                            >
                              <Trash2Icon className="size-4 text-muted-foreground" />
                            </Button>
                          </div>
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

/**
 * When this volume was last copied to storage.
 *
 * A volume with no backup says so rather than showing an empty cell: "never"
 * is the thing somebody needs to see, and a blank column reads as a column
 * that has not loaded.
 */
function LastBackup({ appID, volumeID }: { appID: string; volumeID: string }) {
  const { t } = useTranslation()
  const backups = useQuery({
    queryKey: ["volume-backups", volumeID],
    queryFn: () => api.get<List<Backup>>(`/api/apps/${appID}/volumes/${volumeID}/backups?limit=1`),
  })

  const latest = backups.data?.items?.[0]
  return (
    <TableCell className="text-xs text-muted-foreground">
      {backups.isLoading ? (
        <Skeleton className="h-4 w-20" />
      ) : latest ? (
        <span title={latest.status}>
          {formatRelative(latest.created_at)}
          {latest.status === "failed" && ` · ${t("databases.status.failed", { defaultValue: "failed" })}`}
        </span>
      ) : (
        t("common.never")
      )}
    </TableCell>
  )
}

/** Copies a volume to storage now. */
function BackUpNow({ appID, volumeID }: { appID: string; volumeID: string }) {
  const { t } = useTranslation()
  const run = useMutation({
    mutationFn: () => api.post(`/api/apps/${appID}/volumes/${volumeID}/backups`, {}),
    onSuccess: () => {
      toast.success(t("databases.backupStarted"))
      void queryClient.invalidateQueries({ queryKey: ["volume-backups", volumeID] })
    },
    onError: (error) => toast.error(toProblem(error)?.title ?? t("errors.somethingWentWrong")),
  })

  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label={t("databases.backupNow")}
      title={t("databases.backupNow")}
      disabled={run.isPending}
      onClick={() => run.mutate()}
    >
      {run.isPending ? <Spinner /> : <DownloadIcon className="size-4 text-muted-foreground" />}
    </Button>
  )
}
