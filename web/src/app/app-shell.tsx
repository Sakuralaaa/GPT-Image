import { useLocation } from "react-router-dom";

import { AnimatedRoutes } from "@/app/animated-routes";
import { Sidebar } from "@/components/sidebar";

export function AppShell() {
  const location = useLocation();
  if (location.pathname.startsWith("/login")) {
    return (
      <main className="min-h-screen bg-background text-foreground">
        <AnimatedRoutes />
      </main>
    );
  }
  return (
    <main className="min-h-screen bg-background text-foreground lg:flex">
      <Sidebar />
      <div className="min-w-0 flex-1">
        <div className="mx-auto flex max-w-[1440px] flex-col gap-4 px-3 py-4 sm:px-5 lg:px-8 lg:py-6">
          <AnimatedRoutes />
        </div>
      </div>
    </main>
  );
}
