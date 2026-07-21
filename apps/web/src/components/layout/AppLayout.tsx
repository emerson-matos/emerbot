import { Outlet } from "react-router-dom";
import { useState } from "react";

import Sidebar from "./Sidebar";
import Header from "./Header";
import MobileSidebar from "./MobileSidebar";

import { useTheme } from "@/lib/theme";
import { useAuth } from "@/lib/auth";

export default function AppLayout() {
  const { theme, toggle } = useTheme();
  const auth = useAuth();

  const [mobileOpen, setMobileOpen] = useState(false);

  const userName = localStorage.getItem("user_name") ?? "você";
  const initials = userName.slice(0, 2).toUpperCase();

  function handleLogout() {
    auth.logout();
  }

  return (
    <div className="min-h-screen lg:grid lg:grid-cols-[16rem_1fr]">
      <Sidebar
        userName={userName}
        initials={initials}
        onLogout={handleLogout}
      />

      <MobileSidebar
        open={mobileOpen}
        onClose={() => setMobileOpen(false)}
        onLogout={handleLogout}
      />

      <div className="flex min-w-0 flex-col">
        <Header
          theme={theme}
          onToggleTheme={toggle}
          onOpenMenu={() => setMobileOpen(true)}
        />

        <main className="mx-auto w-full max-w-7xl flex-1 px-4 py-6 sm:px-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
