import { useState } from "react"
import { NavLink, Outlet, useNavigate } from "react-router-dom"
import { useTranslation } from "react-i18next"
import {
  ActivityIcon,
  BookOpenIcon,
  BoxesIcon,
  ChevronsUpDownIcon,
  DatabaseIcon,
  FolderIcon,
  LayoutGridIcon,
  LogOutIcon,
  MenuIcon,
  SearchIcon,
  ServerIcon,
  SettingsIcon,
  ShieldAlertIcon,
  UserIcon,
} from "lucide-react"
import { cn } from "cn"

import { CommandPalette } from "@/components/command-palette"
import { LanguageSwitcher } from "@/components/language-switcher"
import { Logo } from "@/components/logo"
import { ThemeToggle } from "@/components/theme-toggle"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Sheet, SheetContent, SheetTitle, SheetTrigger } from "@/components/ui/sheet"
import { useSession } from "@/hooks/use-session"

/** The panel's frame: sidebar, header and the routed page. */
export function AppShell() {
  const { t } = useTranslation()
  const [mobileOpen, setMobileOpen] = useState(false)
  const [paletteOpen, setPaletteOpen] = useState(false)

  return (
    <div className="flex min-h-svh bg-background">
      <aside className="hidden w-60 shrink-0 border-r bg-sidebar lg:flex lg:flex-col">
        <SidebarContent />
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-30 flex h-14 items-center gap-2 border-b bg-background/80 px-4 backdrop-blur">
          <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
            <SheetTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                className="lg:hidden"
                aria-label={t("common.actions")}
              >
                <MenuIcon className="size-4" />
              </Button>
            </SheetTrigger>
            <SheetContent side="left" className="w-64 p-0">
              <SheetTitle className="sr-only">{t("nav.overview")}</SheetTitle>
              <SidebarContent onNavigate={() => setMobileOpen(false)} />
            </SheetContent>
          </Sheet>

          <Button
            variant="outline"
            className="h-9 max-w-md flex-1 justify-start gap-2 px-3 text-muted-foreground"
            onClick={() => setPaletteOpen(true)}
          >
            <SearchIcon className="size-4" />
            <span className="truncate text-sm">{t("nav.commandPalette")}</span>
            <kbd className="ml-auto hidden rounded border bg-muted px-1.5 py-0.5 font-mono text-[10px] sm:inline">
              ⌘K
            </kbd>
          </Button>

          <div className="ml-auto flex items-center gap-1">
            <LanguageSwitcher signedIn />
            <ThemeToggle />
            <AccountMenu />
          </div>
        </header>

        <main className="min-w-0 flex-1 px-4 py-6 sm:px-6 lg:px-8">
          <Outlet />
        </main>
      </div>

      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
    </div>
  )
}

function SidebarContent({ onNavigate }: { onNavigate?: () => void }) {
  const { t } = useTranslation()
  const { team, teams, setTeam, user } = useSession()

  const links = [
    { to: "/", label: t("nav.overview"), icon: LayoutGridIcon, end: true },
    { to: "/projects", label: t("nav.projects"), icon: FolderIcon },
    { to: "/databases", label: t("nav.databases"), icon: DatabaseIcon },
    { to: "/servers", label: t("nav.servers"), icon: ServerIcon },
    { to: "/templates", label: t("nav.templates"), icon: BoxesIcon },
    { to: "/activity", label: t("nav.activity"), icon: ActivityIcon },
  ]

  return (
    <div className="flex h-full flex-col">
      <div className="flex h-14 items-center gap-2.5 border-b px-4">
        <Logo className="size-6 text-primary" title="Skifity" />
        <span className="text-sm font-semibold tracking-tight">Skifity</span>
      </div>

      {teams.length > 0 && (
        <div className="border-b p-2">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" className="h-9 w-full justify-between px-2 text-sm">
                <span className="truncate">{team?.name ?? "—"}</span>
                <ChevronsUpDownIcon className="size-3.5 shrink-0 text-muted-foreground" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="w-56">
              {teams.map((candidate) => (
                <DropdownMenuItem key={candidate.id} onSelect={() => setTeam(candidate)}>
                  <span className="truncate">{candidate.name}</span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      )}

      <nav className="flex-1 space-y-0.5 overflow-y-auto p-2">
        {links.map((link) => (
          <NavLink
            key={link.to}
            to={link.to}
            end={link.end}
            onClick={onNavigate}
            className={({ isActive }) =>
              cn(
                "flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm transition-colors",
                isActive
                  ? "bg-sidebar-accent font-medium text-sidebar-accent-foreground"
                  : "text-muted-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-accent-foreground",
              )
            }
          >
            <link.icon className="size-4 shrink-0" />
            <span className="truncate">{link.label}</span>
          </NavLink>
        ))}
      </nav>

      <div className="space-y-0.5 border-t p-2">
        {user?.is_admin && (
          <NavLink
            to="/settings"
            onClick={onNavigate}
            className={({ isActive }) =>
              cn(
                "flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm transition-colors",
                isActive
                  ? "bg-sidebar-accent font-medium text-sidebar-accent-foreground"
                  : "text-muted-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-accent-foreground",
              )
            }
          >
            <SettingsIcon className="size-4 shrink-0" />
            {t("nav.settings")}
          </NavLink>
        )}
        <a
          href="/docs"
          target="_blank"
          rel="noreferrer"
          className="flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm text-muted-foreground transition-colors hover:bg-sidebar-accent/60 hover:text-sidebar-accent-foreground"
        >
          <BookOpenIcon className="size-4 shrink-0" />
          {t("nav.documentation")}
        </a>
      </div>
    </div>
  )
}

function AccountMenu() {
  const { t } = useTranslation()
  const { user, signOut } = useSession()
  const navigate = useNavigate()

  const initial = (user?.name || user?.email || "?").charAt(0).toUpperCase()

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={t("nav.account")}>
          <span className="flex size-7 items-center justify-center rounded-full bg-primary text-xs font-medium text-primary-foreground">
            {initial}
          </span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-60">
        <DropdownMenuLabel className="font-normal">
          <div className="truncate text-sm font-medium">{user?.name || user?.email}</div>
          {user?.name && <div className="truncate text-xs text-muted-foreground">{user.email}</div>}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => navigate("/account")}>
          <UserIcon className="size-4" />
          {t("nav.account")}
        </DropdownMenuItem>
        {!user?.recovery_saved && (
          <DropdownMenuItem onSelect={() => navigate("/account?recovery=1")}>
            <ShieldAlertIcon className="size-4 text-warning" />
            <span className="text-warning">{t("auth.recoveryKeyReminder")}</span>
          </DropdownMenuItem>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onSelect={() => {
            void signOut()
          }}
        >
          <LogOutIcon className="size-4" />
          {t("nav.signOut")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
