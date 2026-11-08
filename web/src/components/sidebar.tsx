import { useState } from "react";
import { NavLink, useLocation, useNavigate } from "react-router-dom";
import {
  Bot,
  Gauge,
  Images,
  KeyRound,
  LifeBuoy,
  LogOut,
  Menu,
  MoonStar,
  ScrollText,
  Settings,
  Sparkles,
  Sun,
  UsersRound,
  X,
} from "lucide-react";

import { logout } from "@/lib/api";
import { clearVerifiedAuthSession } from "@/lib/session";
import { applyColorTheme, getPreferredColorTheme, saveColorTheme, type ColorTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";

import { APP_NAME } from "@/lib/brand";

const NAV_ITEMS = [
  { path: "/dashboard", label: "仪表盘", icon: Gauge },
  { path: "/accounts", label: "账号", icon: UsersRound },
  { path: "/keys", label: "API Key", icon: KeyRound },
  { path: "/recovery", label: "找回任务", icon: LifeBuoy },
  { path: "/image", label: "创作台", icon: Sparkles },
  { path: "/image-manager", label: "图库", icon: Images },
  { path: "/logs", label: "日志", icon: ScrollText },
  { path: "/settings", label: "设置", icon: Settings },
  { path: "/register", label: "注册机", icon: Bot },
];

function NavList({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <nav className="flex flex-col gap-1">
      {NAV_ITEMS.map(({ path, label, icon: Icon }) => (
        <NavLink
          key={path}
          to={path}
          onClick={onNavigate}
          className={({ isActive }) =>
            cn(
              "flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
              isActive
                ? "bg-primary/10 text-primary dark:bg-primary/20"
                : "text-muted-foreground hover:bg-muted hover:text-foreground",
            )
          }
        >
          <Icon className="size-4 shrink-0" />
          <span>{label}</span>
        </NavLink>
      ))}
    </nav>
  );
}

export function Sidebar() {
  const navigate = useNavigate();
  const location = useLocation();
  const [theme, setTheme] = useState<ColorTheme>(() => getPreferredColorTheme());
  const [mobileOpen, setMobileOpen] = useState(false);

  const toggleTheme = () => {
    const next = theme === "dark" ? "light" : "dark";
    applyColorTheme(next);
    saveColorTheme(next);
    setTheme(next);
  };

  const handleLogout = async () => {
    try {
      await logout();
    } catch {
      // The local session is cleared even when the server cookie is gone.
    }
    await clearVerifiedAuthSession();
    navigate("/login", { replace: true });
  };

  const footer = (
    <div className="flex items-center gap-1 border-t border-border/60 pt-3">
      <button
        type="button"
        onClick={toggleTheme}
        className="flex flex-1 items-center gap-2 rounded-lg px-3 py-2 text-sm text-muted-foreground hover:bg-muted hover:text-foreground"
        title={theme === "dark" ? "浅色模式" : "深色模式"}
      >
        {theme === "dark" ? <Sun className="size-4" /> : <MoonStar className="size-4" />}
        {theme === "dark" ? "浅色" : "深色"}
      </button>
      <button
        type="button"
        onClick={() => void handleLogout()}
        className="flex flex-1 items-center gap-2 rounded-lg px-3 py-2 text-sm text-muted-foreground hover:bg-rose-50 hover:text-rose-600 dark:hover:bg-rose-950/30"
      >
        <LogOut className="size-4" />
        退出
      </button>
    </div>
  );

  const brand = (
    <div className="flex items-center gap-2 px-2 pb-4">
      <img src="/logo-mark.svg" alt="" className="size-7" />
      <span className="text-base font-semibold tracking-tight">{APP_NAME}</span>
    </div>
  );

  const current = NAV_ITEMS.find((item) => location.pathname.startsWith(item.path));

  return (
    <>
      <aside className="sticky top-0 hidden h-screen w-56 shrink-0 flex-col border-r border-border/60 bg-card/60 px-3 py-4 backdrop-blur lg:flex">
        {brand}
        <div className="flex-1 overflow-y-auto">
          <NavList />
        </div>
        {footer}
      </aside>
      <header className="sticky top-0 z-40 flex items-center justify-between border-b border-border/60 bg-background/90 px-4 py-2 backdrop-blur lg:hidden">
        <div className="flex items-center gap-2">
          <img src="/logo-mark.svg" alt="" className="size-6" />
          <span className="text-sm font-semibold">{APP_NAME}</span>
          {current ? <span className="text-xs text-muted-foreground">/ {current.label}</span> : null}
        </div>
        <button type="button" className="rounded-lg p-2 hover:bg-muted" onClick={() => setMobileOpen(true)} aria-label="打开菜单">
          <Menu className="size-5" />
        </button>
      </header>
      {mobileOpen ? (
        <div className="fixed inset-0 z-50 lg:hidden">
          <div className="absolute inset-0 bg-black/40" onClick={() => setMobileOpen(false)} />
          <div className="absolute inset-y-0 left-0 flex w-64 flex-col bg-background px-3 py-4 shadow-xl">
            <div className="flex items-start justify-between">
              {brand}
              <button type="button" className="rounded-lg p-1.5 hover:bg-muted" onClick={() => setMobileOpen(false)} aria-label="关闭菜单">
                <X className="size-4" />
              </button>
            </div>
            <div className="flex-1 overflow-y-auto">
              <NavList onNavigate={() => setMobileOpen(false)} />
            </div>
            {footer}
          </div>
        </div>
      ) : null}
    </>
  );
}
