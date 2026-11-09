"use client";

import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { LoaderCircle } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { APP_NAME } from "@/lib/brand";
import { login } from "@/lib/api";
import { authSessionFromLoginResponse, setVerifiedAuthSession } from "@/lib/session";
import { applyColorTheme, getPreferredColorTheme, saveColorTheme, type ColorTheme } from "@/lib/theme";
import { useRedirectIfAuthenticated } from "@/lib/use-auth-guard";
import { getDefaultRouteForSession } from "@/store/auth";

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
    <div className="fixed inset-0 z-50 flex min-h-svh flex-col bg-background text-foreground">
      <header className="flex h-16 items-center justify-between border-b border-border px-6 sm:px-10">
        <span className="font-serif text-[22px] font-medium tracking-tight">{APP_NAME}</span>
        <button type="button" onClick={toggleTheme} className="text-[13px] text-muted-foreground hover:text-foreground" aria-label="切换主题">
          {theme === "dark" ? "浅色" : "深色"}
        </button>
      </header>
      <div className="flex flex-1 items-center justify-center px-6">
        {isCheckingAuth ? (
          <LoaderCircle className="size-5 animate-spin text-muted-foreground" />
        ) : (
          <div className="grid w-full max-w-4xl gap-12 md:grid-cols-[1.1fr_1fr] md:gap-16">
            <div className="flex flex-col justify-end gap-4 md:border-r md:border-border md:pr-16">
              <span className="kicker">Admin Console</span>
              <h1 className="font-serif text-5xl leading-[1.05] font-medium tracking-tight sm:text-6xl">
                图像，
                <br />
                由此生成。
              </h1>
              <p className="max-w-sm text-[15px] leading-7 text-muted-foreground">
                ChatGPT 网页能力的 OpenAI 兼容网关。管理号池、API Key 与创作记录。
              </p>
            </div>
            <form
              className="flex flex-col justify-end"
              onSubmit={(event) => {
                event.preventDefault();
                void handleSubmit();
              }}
            >
              <div className="border-b border-rule-strong pb-2 font-serif text-xl">管理员登录</div>
              <label className="mt-6 flex flex-col gap-2">
                <span className="text-xs text-muted-foreground">用户名</span>
                <Input className="h-9" autoComplete="username" value={username} onChange={(event) => setUsername(event.target.value)} />
              </label>
              <label className="mt-4 flex flex-col gap-2">
                <span className="text-xs text-muted-foreground">密码</span>
                <Input className="h-9" type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} />
              </label>
              <Button type="submit" className="mt-6 h-9" disabled={isSubmitting}>
                {isSubmitting ? <LoaderCircle className="size-4 animate-spin" /> : null}
                登录
              </Button>
            </form>
          </div>
        )}
      </div>
      <footer className="border-t border-border px-6 py-4 text-xs text-muted-foreground sm:px-10">非官方项目 · 仅供学习研究</footer>
    </div>
  );
}
