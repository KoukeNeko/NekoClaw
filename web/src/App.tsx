import { useEffect, useState } from "react";
import {
  getActivePersona,
  getGeneralConfig,
  getSecurityStatus,
} from "@/api/client";
import { subscribeBrowserAuthEvent } from "@/api/authEvents";
import { useAppStore, type Route } from "@/store/appStore";
import { AppLayout } from "@/layouts/AppLayout";
import { ChatPage } from "@/components/chat/ChatPage";
import { SettingsPage } from "@/components/settings/SettingsPage";
import { LoginPage } from "@/components/auth/LoginPage";
import { SetupPage } from "@/components/auth/SetupPage";
import { detectBrowserTimeZone } from "@/utils/timezone";

// Import highlight.js theme for code blocks
import "highlight.js/styles/github-dark.css";

/**
 * Root application component.
 * Implements a simple hash-based router (no external dependency).
 * Routes: /#/ (chat), /#/settings/provider, /#/settings/auth, etc.
 */

const VALID_ROUTES: Route[] = [
  "login",
  "setup",
  "chat",
  "settings/general",
  "settings/compaction",
  "settings/model-roles",
  "settings/playbooks",
  "settings/automations",
  "settings/permissions",
  "settings/security",
  "settings/backup",
  "settings/provider",
  "settings/persona",
  "settings/auth",
  "settings/memory",
  "settings/usage",
  "settings/mcp",
  "settings/discord",
  "settings/telegram",
  "settings/tools",
  "settings/console",
];

function parseHash(): Route {
  const hash = window.location.hash.replace(/^#\/?/, "");
  const routePath = hash.split("?")[0]?.replace(/^\/?/, "") ?? "";
  const route = routePath as Route;
  if (!routePath || routePath === "/") return "chat";
  if (VALID_ROUTES.includes(route)) return route;
  if (!hash || hash === "/") return "chat";
  return "chat";
}

function setHash(route: Route) {
  let hash = route === "chat" ? "/" : `/${route}`;
  if (route === "setup") {
    const query = window.location.hash.split("?")[1] ?? "";
    if (query) {
      hash += `?${query}`;
    }
  }
  window.location.hash = hash;
}

export function App() {
  const route = useAppStore((s) => s.route);
  const setRoute = useAppStore((s) => s.setRoute);
  const securityStatus = useAppStore((s) => s.securityStatus);
  const setSecurityStatus = useAppStore((s) => s.setSecurityStatus);
  const resetForAuthBoundary = useAppStore((s) => s.resetForAuthBoundary);
  const setActivePersona = useAppStore((s) => s.setActivePersona);
  const setBrowserTimezone = useAppStore((s) => s.setBrowserTimezone);
  const setGeneralTimezoneOverride = useAppStore(
    (s) => s.setGeneralTimezoneOverride,
  );
  const [hashReady, setHashReady] = useState(false);
  const [securityLoading, setSecurityLoading] = useState(true);
  const [securityLoadFailed, setSecurityLoadFailed] = useState(false);

  // Sync hash → store on initial load and popstate
  useEffect(() => {
    function onHashChange() {
      setRoute(parseHash());
    }
    // Initial sync
    setRoute(parseHash());
    setHashReady(true);
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, [setRoute]);

  // Sync store → hash when route changes
  useEffect(() => {
    if (!hashReady) return;
    const currentHash = parseHash();
    if (currentHash !== route) {
      setHash(route);
    }
  }, [hashReady, route]);

  useEffect(() => {
    setBrowserTimezone(detectBrowserTimeZone());
  }, [setBrowserTimezone]);

  useEffect(() => {
    let cancelled = false;

    async function syncSecurityStatus(targetRoute?: Route) {
      try {
        const status = await getSecurityStatus();
        if (cancelled) return;
        setSecurityStatus(status);
        setSecurityLoading(false);
        setSecurityLoadFailed(false);
        if (status.auth_enabled && !status.authenticated) {
          resetForAuthBoundary();
          setRoute(status.setup_required ? "setup" : "login");
          return;
        }
        if (targetRoute && targetRoute !== "login" && targetRoute !== "setup") {
          setRoute(targetRoute);
        }
      } catch {
        if (cancelled) return;
        setSecurityLoading(false);
        setSecurityLoadFailed(true);
      }
    }

    void syncSecurityStatus(parseHash());

    const unsubscribe = subscribeBrowserAuthEvent((reason) => {
      resetForAuthBoundary();
      setRoute(reason === "setup_required" ? "setup" : "login");
      void syncSecurityStatus();
    });

    return () => {
      cancelled = true;
      unsubscribe();
    };
  }, [resetForAuthBoundary, setRoute, setSecurityStatus]);

  useEffect(() => {
    if (securityLoading || !securityStatus) return;
    if (securityStatus.auth_enabled && !securityStatus.authenticated) return;

    let cancelled = false;

    getGeneralConfig()
      .then((config) => {
        if (cancelled) return;
        setGeneralTimezoneOverride(config.timezone ?? "");
      })
      .catch(() => {
        if (cancelled) return;
      });

    getActivePersona()
      .then((persona) => {
        if (cancelled) return;
        setActivePersona(persona?.name ?? "");
      })
      .catch(() => {
        if (cancelled) return;
      });

    return () => {
      cancelled = true;
    };
  }, [
    securityLoading,
    securityStatus,
    setActivePersona,
    setGeneralTimezoneOverride,
  ]);

  useEffect(() => {
    if (securityLoading || !securityStatus) return;
    if (securityStatus.auth_enabled && !securityStatus.authenticated) return;
    if (route === "login" || route === "setup") {
      setRoute("chat");
    }
  }, [route, securityLoading, securityStatus, setRoute]);

  // Global keyboard shortcuts
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      // Ctrl+N: new chat
      if ((e.ctrlKey || e.metaKey) && e.key === "n") {
        e.preventDefault();
        const now = new Date();
        const pad = (n: number) => String(n).padStart(2, "0");
        const id = `chat-${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
        useAppStore.getState().setSessionID(id);
        useAppStore.getState().clearMessages();
        useAppStore.getState().resetUsage();
        setRoute("chat");
      }

      // Ctrl+B: toggle sidebar
      if ((e.ctrlKey || e.metaKey) && e.key === "b") {
        e.preventDefault();
        useAppStore.getState().toggleSidebar();
      }

      // Escape: back to chat from settings
      if (e.key === "Escape" && route.startsWith("settings")) {
        setRoute("chat");
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [route, setRoute]);

  if (securityLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-base-200">
        <div className="card border border-base-300 bg-base-100 shadow-xl">
          <div className="card-body items-center gap-4 px-8 py-10 text-center">
            <span className="loading loading-spinner loading-lg" />
            <div>
              <div className="text-lg font-semibold">檢查後台安全狀態</div>
              <div className="text-sm text-base-content/60">
                正在同步目前 session 與 browser auth 設定。
              </div>
            </div>
          </div>
        </div>
      </div>
    );
  }

  if (securityLoadFailed || !securityStatus) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-base-200 p-6">
        <div className="card max-w-lg border border-base-300 bg-base-100 shadow-xl">
          <div className="card-body gap-4">
            <h1 className="card-title text-2xl">無法讀取安全狀態</h1>
            <p className="text-sm text-base-content/60">
              請確認後端已啟動並重新整理頁面，再試一次。
            </p>
            <div className="card-actions justify-end">
              <button className="btn btn-primary" onClick={() => window.location.reload()}>
                重新整理
              </button>
            </div>
          </div>
        </div>
      </div>
    );
  }

  if (securityStatus.auth_enabled && securityStatus.setup_required) {
    return <SetupPage />;
  }

  if (securityStatus.auth_enabled && !securityStatus.authenticated) {
    return <LoginPage />;
  }

  const effectiveRoute =
    route === "login" || route === "setup" ? "chat" : route;
  const isSettings = effectiveRoute.startsWith("settings");

  return (
    <AppLayout>
      {isSettings ? <SettingsPage /> : <ChatPage />}
    </AppLayout>
  );
}
