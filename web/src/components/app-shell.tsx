import { Fragment } from "react"
import { Link, NavLink, Outlet, useLocation, useNavigate } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { useQuery } from "@tanstack/react-query"
import {
  ActivityIcon,
  BookOpenIcon,
  BoxesIcon,
  ChevronsUpDownIcon,
  DatabaseIcon,
  FolderIcon,
  LayoutGridIcon,
  LogOutIcon,
  SearchIcon,
  ServerIcon,
  SettingsIcon,
  ShieldAlertIcon,
  UserIcon,
} from "lucide-react"

import { CommandPalette } from "@/components/command-palette"
import { ErrorBoundary } from "@/components/error-boundary"
import { LanguageSwitcher } from "@/components/language-switcher"
import { Logo } from "@/components/logo"
import { ThemeToggle } from "@/components/theme-toggle"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Badge } from "@/components/ui/badge"
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Kbd, KbdGroup } from "@/components/ui/kbd"
import { Separator } from "@/components/ui/separator"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  SidebarTrigger,
} from "@/components/ui/sidebar"
import { usePaletteShortcut } from "@/hooks/use-palette"
import { useSession } from "@/hooks/use-session"
import { api, type List } from "@/lib/api"
import type { Project, Server } from "@/lib/types"

/**
 * The panel's frame.
 *
 * Built on shadcn's Sidebar rather than a hand-rolled aside, which is what
 * gives it a collapsible rail, keyboard control, a mobile drawer and a
 * remembered open state without any of that being written here.
 */
export function AppShell() {
  const { t } = useTranslation()
  const { paletteOpen, setPaletteOpen } = usePaletteShortcut()
  const location = useLocation()

  return (
    <SidebarProvider>
      {/*
        Visible only once it has focus. Without it, a keyboard reaches the page
        by tabbing through every link in the sidebar, on every navigation — the
        sidebar is the same on all of them, so it is the same twenty presses
        every time.
      */}
      <a
        href="#content"
        className="sr-only z-50 rounded-md bg-primary px-4 py-2 text-primary-foreground focus:not-sr-only focus:absolute focus:top-3 focus:left-3"
      >
        {t("common.skipToContent")}
      </a>
      <AppSidebar />
      <SidebarInset>
        <Header onOpenPalette={() => setPaletteOpen(true)} />
        <main id="content" tabIndex={-1} className="min-w-0 flex-1 p-4 sm:p-6 lg:p-8">
          {/*
            Inside the shell rather than around it, so a page that throws
            leaves the sidebar, the header and the command palette working and
            the person can simply go somewhere else. The path is the reset key:
            navigating away is what clears it.
          */}
          <ErrorBoundary resetKey={location.pathname}>
            <Outlet />
          </ErrorBoundary>
        </main>
      </SidebarInset>
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
    </SidebarProvider>
  )
}

function AppSidebar() {
  const { t } = useTranslation()
  const { team, teams, setTeam, user } = useSession()

  // Counts next to the navigation, so the sidebar says something about the
  // state of the system rather than being a list of nouns.
  const servers = useQuery({
    queryKey: ["servers", team?.id],
    queryFn: () => api.get<List<Server>>(`/api/teams/${team!.id}/servers`),
    enabled: Boolean(team),
  })
  const projects = useQuery({
    queryKey: ["projects", team?.id],
    queryFn: () => api.get<List<Project>>(`/api/teams/${team!.id}/projects`),
    enabled: Boolean(team),
  })

  const unhealthy = (servers.data?.items ?? []).filter(
    (server) => server.status === "failed" || server.status === "not_ready",
  ).length

  const links = [
    { to: "/", label: t("nav.overview"), icon: LayoutGridIcon, end: true },
    { to: "/projects", label: t("nav.projects"), icon: FolderIcon, count: projects.data?.total },
    { to: "/databases", label: t("nav.databases"), icon: DatabaseIcon },
    {
      to: "/servers",
      label: t("nav.servers"),
      icon: ServerIcon,
      count: servers.data?.total,
      alert: unhealthy > 0,
    },
    { to: "/templates", label: t("nav.templates"), icon: BoxesIcon },
    { to: "/activity", label: t("nav.activity"), icon: ActivityIcon },
  ]

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton asChild size="lg" tooltip="Skifity">
              <Link to="/">
                <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground">
                  <Logo className="size-4" />
                </div>
                <div className="grid flex-1 text-left leading-tight">
                  <span className="truncate font-semibold">Skifity</span>
                  <span className="truncate text-xs text-muted-foreground">
                    {team?.name ?? "—"}
                  </span>
                </div>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>

          {teams.length > 1 && (
            <SidebarMenuItem>
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <SidebarMenuButton tooltip={t("settings.team")}>
                    <ChevronsUpDownIcon />
                    <span className="truncate">{team?.name ?? "—"}</span>
                  </SidebarMenuButton>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="start" className="w-56">
                  <DropdownMenuLabel>{t("settings.team")}</DropdownMenuLabel>
                  {teams.map((candidate) => (
                    <DropdownMenuItem key={candidate.id} onSelect={() => setTeam(candidate)}>
                      <span className="truncate">{candidate.name}</span>
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
            </SidebarMenuItem>
          )}
        </SidebarMenu>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>{t("nav.overview")}</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {links.map((link) => (
                <SidebarMenuItem key={link.to}>
                  <SidebarMenuButton asChild tooltip={link.label}>
                    <NavLink to={link.to} end={link.end}>
                      {({ isActive }) => (
                        <>
                          <link.icon data-active={isActive} />
                          <span>{link.label}</span>
                        </>
                      )}
                    </NavLink>
                  </SidebarMenuButton>
                  {link.alert ? (
                    <SidebarMenuBadge className="text-destructive">{unhealthy}</SidebarMenuBadge>
                  ) : link.count ? (
                    <SidebarMenuBadge>{link.count}</SidebarMenuBadge>
                  ) : null}
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter>
        <SidebarMenu>
          {user?.is_admin && (
            <SidebarMenuItem>
              <SidebarMenuButton asChild tooltip={t("nav.settings")}>
                <NavLink to="/settings">
                  <SettingsIcon />
                  <span>{t("nav.settings")}</span>
                </NavLink>
              </SidebarMenuButton>
            </SidebarMenuItem>
          )}
          <SidebarMenuItem>
            <SidebarMenuButton asChild tooltip={t("nav.documentation")}>
              <a href="/docs/" target="_blank" rel="noreferrer">
                <BookOpenIcon />
                <span>{t("nav.documentation")}</span>
              </a>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>

      <SidebarRail />
    </Sidebar>
  )
}

function Header({ onOpenPalette }: { onOpenPalette: () => void }) {
  const { t } = useTranslation()

  return (
    <header className="sticky top-0 z-30 flex h-14 shrink-0 items-center gap-2 border-b bg-background/80 px-4 backdrop-blur">
      <SidebarTrigger className="-ml-1" />
      <Separator orientation="vertical" className="mr-1 h-4" />

      <Crumbs />

      <Button
        variant="outline"
        size="sm"
        onClick={onOpenPalette}
        className="ml-auto gap-2 text-muted-foreground"
      >
        <SearchIcon className="size-4" />
        <span className="hidden sm:inline">{t("nav.commandPalette")}</span>
        <KbdGroup className="hidden sm:flex">
          <Kbd>⌘</Kbd>
          <Kbd>K</Kbd>
        </KbdGroup>
      </Button>

      <LanguageSwitcher signedIn />
      <ThemeToggle />
      <AccountMenu />
    </header>
  )
}

/**
 * Where you are, from the URL.
 *
 * Only the segments that name something are shown: an id in the middle of a
 * path is noise, and the page itself already says what it is looking at.
 */
function Crumbs() {
  const { t } = useTranslation()
  const location = useLocation()

  const segments = location.pathname.split("/").filter(Boolean)
  const named = segments.filter((segment) => !segment.includes("_"))

  if (named.length === 0) {
    return (
      <Breadcrumb>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbPage>{t("nav.overview")}</BreadcrumbPage>
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>
    )
  }

  return (
    <Breadcrumb className="min-w-0">
      <BreadcrumbList>
        <BreadcrumbItem className="hidden sm:block">
          <BreadcrumbLink asChild>
            <Link to="/">{t("nav.overview")}</Link>
          </BreadcrumbLink>
        </BreadcrumbItem>
        {named.map((segment, index) => {
          const last = index === named.length - 1
          const label = t(`nav.${segment}`, { defaultValue: humanise(segment) })
          return (
            <Fragment key={segment + index}>
              <BreadcrumbSeparator className="hidden sm:block" />
              <BreadcrumbItem className="min-w-0">
                {last ? (
                  <BreadcrumbPage className="truncate">{label}</BreadcrumbPage>
                ) : (
                  <BreadcrumbLink asChild>
                    <Link to={"/" + named.slice(0, index + 1).join("/")}>{label}</Link>
                  </BreadcrumbLink>
                )}
              </BreadcrumbItem>
            </Fragment>
          )
        })}
      </BreadcrumbList>
    </Breadcrumb>
  )
}

function humanise(segment: string): string {
  return segment.charAt(0).toUpperCase() + segment.slice(1).replace(/-/g, " ")
}

function AccountMenu() {
  const { t } = useTranslation()
  const { user, signOut } = useSession()
  const navigate = useNavigate()

  const initial = (user?.name || user?.email || "?").charAt(0).toUpperCase()

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          aria-label={t("nav.account")}
          data-slot="account-menu"
          className="relative"
        >
          <Avatar className="size-7">
            <AvatarFallback className="bg-primary text-xs font-medium text-primary-foreground">
              {initial}
            </AvatarFallback>
          </Avatar>
          {!user?.recovery_saved && (
            <span className="absolute top-1 right-1 size-2 rounded-full bg-warning ring-2 ring-background" />
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        <DropdownMenuLabel className="font-normal">
          <div className="flex items-center gap-2">
            <Avatar className="size-8">
              <AvatarFallback className="bg-primary text-xs font-medium text-primary-foreground">
                {initial}
              </AvatarFallback>
            </Avatar>
            <div className="min-w-0">
              <div className="truncate text-sm font-medium">{user?.name || user?.email}</div>
              {user?.name && (
                <div className="truncate text-xs text-muted-foreground">{user.email}</div>
              )}
            </div>
          </div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => navigate("/account")}>
          <UserIcon />
          {t("nav.account")}
        </DropdownMenuItem>
        {!user?.recovery_saved && (
          <DropdownMenuItem onSelect={() => navigate("/account?recovery=1")}>
            <ShieldAlertIcon className="text-warning" />
            <span className="text-warning">{t("auth.recoveryKeyReminder")}</span>
            <Badge variant="outline" className="ml-auto border-warning/40 text-warning">
              !
            </Badge>
          </DropdownMenuItem>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onSelect={() => {
            void signOut()
          }}
        >
          <LogOutIcon />
          {t("nav.signOut")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
