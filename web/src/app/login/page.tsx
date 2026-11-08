"use client";

import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { ArrowRight, KeyRound, LoaderCircle, MoonStar, Sun, UserRound } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { APP_NAME } from "@/lib/brand";
import { login } from "@/lib/api";
import { authSessionFromLoginResponse, setVerifiedAuthSession } from "@/lib/session";
import { applyColorTheme, getPreferredColorTheme, saveColorTheme, type ColorTheme } from "@/lib/theme";
import { useRedirectIfAuthenticated } from "@/lib/use-auth-guard";
import { getDefaultRouteForSession } from "@/store/auth";

const loginBackgroundClass =
  "bg-[radial-gradient(rgba(20,86,240,0.10)_1px,transparent_1px),linear-gradient(145deg,#f8faff_0%,#ffffff_48%,#f4f8ff_100%)] [background-size:14px_14px,cover] dark:bg-[radial-gradient(rgba(96,165,250,0.14)_1px,transparent_1px),linear-gradient(145deg,#080b13_0%,#101827_52%,#070b12_100%)]";

export default function LoginPage() {
  const navigate = useNavigate();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [theme, setTheme] = useState<ColorTheme>(() => getPreferredColorTheme());
  const { isCheckingAuth } = useRedirectIfAuthenticated();

  const handleSubmit = async () => {
    if (!username.trim()) {
      toast.error("请输入用户名");
      return;
    }
    if (!password) {
      toast.error("请输入密码");
      return;
    }
    setIsSubmitting(true);
    try {
      const data = await login(username.trim(), password);
      const token = String(data.token || "").trim();
      if (!token) {
        throw new Error("登录会话签发失败");
      }
      const session = authSessionFromLoginResponse(data, token);
      await setVerifiedAuthSession(session);
      toast.success("登录成功");
      navigate(getDefaultRouteForSession(session), { replace: true });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "登录失败");
    } finally {
      setIsSubmitting(false);
    }
  };

  const toggleTheme = () => {
    const next = theme === "dark" ? "light" : "dark";
    applyColorTheme(next);
    saveColorTheme(next);
    setTheme(next);
  };

  return (
    <div className={`${loginBackgroundClass} fixed inset-0 z-50 flex min-h-svh items-center justify-center px-4`}>
      <button
        type="button"
        onClick={toggleTheme}
        className="fixed right-4 top-4 rounded-full border border-border/60 bg-background/80 p-2 shadow-sm backdrop-blur"
        aria-label="切换主题"
      >
        {theme === "dark" ? <Sun className="size-4" /> : <MoonStar className="size-4" />}
      </button>
      {isCheckingAuth ? (
        <LoaderCircle className="size-5 animate-spin text-muted-foreground" />
      ) : (
        <form
          className="w-full max-w-sm rounded-2xl border border-border/60 bg-background/90 p-8 shadow-xl backdrop-blur"
          onSubmit={(event) => {
            event.preventDefault();
            void handleSubmit();
          }}
        >
          <div className="mb-6 flex flex-col items-center gap-2 text-center">
            <img src="/logo-mark.svg" alt="" className="size-10" />
            <h1 className="text-2xl font-semibold tracking-tight">{APP_NAME}</h1>
            <p className="text-sm text-muted-foreground">管理员登录</p>
          </div>
          <div className="flex flex-col gap-3">
            <label className="relative">
              <UserRound className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input className="h-11 pl-9" placeholder="用户名" autoComplete="username" value={username} onChange={(event) => setUsername(event.target.value)} />
            </label>
            <label className="relative">
              <KeyRound className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input className="h-11 pl-9" type="password" placeholder="密码" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} />
            </label>
            <Button type="submit" className="h-11" disabled={isSubmitting}>
              {isSubmitting ? <LoaderCircle className="size-4 animate-spin" /> : <ArrowRight className="size-4" />}
              登录
            </Button>
          </div>
        </form>
      )}
    </div>
  );
}
