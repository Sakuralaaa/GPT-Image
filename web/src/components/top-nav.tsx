import { useEffect, useState } from "react";
import { NavLink, useLocation, useNavigate } from "react-router-dom";
import { Menu, X } from "lucide-react";

import { logout } from "@/lib/api";
import { APP_NAME } from "@/lib/brand";
import { clearVerifiedAuthSession } from "@/lib/session";
import { applyColorTheme, getPreferredColorTheme, saveColorTheme, type ColorTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";

const NAV_ITEMS = [
  { path: "/dashboard", label: "仪表盘" },
  { path: "/accounts", label: "账号" },
  { path: "/keys", label: "API Key" },
  { path: "/recovery", label: "找回任务" },
  { path: "/image", label: "创作台" },
  { path: "/image-manager", label: "图库" },
  { path: "/logs", label: "日志" },
  { path: "/settings", label: "设置" },
  { path: "/register", label: "注册机" },
];

function linkClass(isActive: boolean) {
  return cn(
    "relative py-1 text-[14px] transition-colors",
    "after:absolute after:inset-x-0 after:-bottom-[17px] after:h-[2px] after:bg-transparent after:content-['']",
    isActive ? "text-foreground after:bg-oxblood" : "text-muted-foreground hover:text-foreground",
  );
}

export function TopNav() {
  const navigate = useNavigate();
  const location = useLocation();
  const [theme, setTheme] = useState<ColorTheme>(() => getPreferredColorTheme());
  const [open, setOpen] = useState(false);

  useEffect(() => {
    setOpen(false);
  }, [location.pathname]);

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

  const utilities = (
    <div className="flex items-center gap-4 text-[13px] text-muted-foreground">
      <button type="button" onClick={toggleTheme} className="hover:text-foreground" title="切换主题">
        {theme === "dark" ? "浅色" : "深色"}
      </button>
      <span className="h-3 w-px bg-border" aria-hidden />
      <button type="button" onClick={() => void handleLogout()} className="hover:text-oxblood">
        退出
      </button>
    </div>
  );

  return (
    <header className="sticky top-0 z-40 border-b border-border bg-background">
      <div className="mx-auto flex h-16 max-w-[1280px] items-center gap-8 px-5 sm:px-8">
        <NavLink to="/dashboard" className="font-serif text-[22px] leading-none font-medium tracking-tight whitespace-nowrap text-foreground">
          {APP_NAME}
        </NavLink>
        <nav className="hidden min-w-0 flex-1 items-center gap-6 xl:flex">
          {NAV_ITEMS.map((item) => (
            <NavLink key={item.path} to={item.path} className={({ isActive }) => linkClass(isActive)}>
              {item.label}
            </NavLink>
          ))}
        </nav>
        <div className="ml-auto hidden xl:block">{utilities}</div>
        <button
          type="button"
          className="ml-auto flex items-center gap-2 text-sm text-foreground xl:hidden"
          onClick={() => setOpen((value) => !value)}
          aria-label={open ? "关闭菜单" : "打开菜单"}
        >
          {open ? <X className="size-4" /> : <Menu className="size-4" />}
          {NAV_ITEMS.find((item) => location.pathname.startsWith(item.path))?.label || "菜单"}
        </button>
      </div>
      {open ? (
        <div className="border-t border-border bg-background xl:hidden">
          <nav className="mx-auto grid max-w-[1280px] grid-cols-2 gap-x-8 px-5 py-4 sm:grid-cols-3 sm:px-8">
            {NAV_ITEMS.map((item) => (
              <NavLink
                key={item.path}
                to={item.path}
                className={({ isActive }) =>
                  cn("border-b border-border py-3 font-serif text-lg", isActive ? "text-oxblood" : "text-foreground")
                }
              >
                {item.label}
              </NavLink>
            ))}
          </nav>
          <div className="mx-auto max-w-[1280px] px-5 pb-4 sm:px-8">{utilities}</div>
        </div>
      ) : null}
    </header>
  );
}
