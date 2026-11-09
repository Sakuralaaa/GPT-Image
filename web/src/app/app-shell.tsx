import { useLocation } from "react-router-dom";

import { AnimatedRoutes } from "@/app/animated-routes";
import { TopNav } from "@/components/top-nav";

export function AppShell() {
  const location = useLocation();
  if (location.pathname.startsWith("/login")) {
    return (
      <main className="min-h-screen text-foreground">
        <AnimatedRoutes />
      </main>
    );
  }
  return (
    <div className="min-h-screen text-foreground">
      <TopNav />
      <main className="mx-auto w-full max-w-[1280px] px-5 pt-10 pb-10 sm:px-8">
        <AnimatedRoutes />
      </main>
    </div>
  );
}
