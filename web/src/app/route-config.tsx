import type { ReactNode } from "react";

import AccountsPage from "@/app/accounts/page";
import DashboardPage from "@/app/dashboard/page";
import ImagePage from "@/app/image/page";
import ImageManagerPage from "@/app/image-manager/page";
import KeysPage from "@/app/keys/page";
import HomePage from "@/app/page";
import LoginPage from "@/app/login/page";
import LogsPage from "@/app/logs/page";
import RecoveryPage from "@/app/recovery/page";
import RegisterPage from "@/app/register/page";
import SettingsPage from "@/app/settings/page";

export type AppRouteConfig = {
  path: string;
  element: ReactNode;
  requiredPath?: string;
};

export const appRoutes: AppRouteConfig[] = [
  { path: "/", element: <HomePage /> },
  { path: "/login", element: <LoginPage /> },
  { path: "/dashboard", element: <DashboardPage />, requiredPath: "/dashboard" },
  { path: "/accounts", element: <AccountsPage />, requiredPath: "/accounts" },
  { path: "/keys", element: <KeysPage />, requiredPath: "/keys" },
  { path: "/recovery", element: <RecoveryPage />, requiredPath: "/recovery" },
  { path: "/register", element: <RegisterPage />, requiredPath: "/register" },
  { path: "/image-manager", element: <ImageManagerPage />, requiredPath: "/image-manager" },
  { path: "/logs", element: <LogsPage />, requiredPath: "/logs" },
  { path: "/settings", element: <SettingsPage />, requiredPath: "/settings" },
  { path: "/image", element: <ImagePage />, requiredPath: "/image" },
  { path: "*", element: <HomePage /> },
];
