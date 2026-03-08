import { useAppStore } from "@/store/appStore";
import { Sidebar } from "@/components/sidebar/Sidebar";

interface AppLayoutProps {
  children: React.ReactNode;
}

/**
 * Root layout shell — daisyUI responsive collapsible drawer.
 * Desktop (lg+): sidebar always visible, toggles between icon-only (w-14) and full (w-64).
 * Mobile (<lg): drawer overlay with full sidebar.
 */
export function AppLayout({ children }: AppLayoutProps) {
  const sidebarOpen = useAppStore((s) => s.sidebarOpen);
  const setSidebarOpen = useAppStore((s) => s.setSidebarOpen);

  return (
    <div className="drawer lg:drawer-open h-screen">
      <input
        id="app-drawer"
        type="checkbox"
        className="drawer-toggle"
        checked={sidebarOpen}
        onChange={(e) => setSidebarOpen(e.target.checked)}
      />

      {/* Main content area */}
      <div className="drawer-content flex flex-col h-screen overflow-hidden">
        {children}
      </div>

      {/* Sidebar drawer — collapsible on desktop */}
      <div className="drawer-side is-drawer-close:overflow-visible z-40 h-screen">
        <label
          htmlFor="app-drawer"
          aria-label="close sidebar"
          className="drawer-overlay"
        />
        <div className="flex h-full flex-col bg-base-200 is-drawer-close:w-14 is-drawer-open:w-64 transition-[width] duration-200 overflow-hidden">
          <Sidebar />
        </div>
      </div>
    </div>
  );
}
