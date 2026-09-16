import { Link } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { CompassIcon } from "lucide-react"

import { EmptyState } from "@/components/empty-state"
import { Button } from "@/components/ui/button"

export function NotFoundPage() {
  const { t } = useTranslation()

  return (
    <div className="mx-auto max-w-2xl py-12">
      <EmptyState
        icon={CompassIcon}
        title={t("errors.pageNotFound")}
        description={t("errors.pageNotFoundHelp")}
        action={
          <Button asChild>
            <Link to="/">{t("errors.goHome")}</Link>
          </Button>
        }
      />
    </div>
  )
}
