import { useEffect } from "react";
import { getActivePersona } from "@/api/client";
import { useAppStore, type Route } from "@/store/appStore";
import { AppLayout } from "@/layouts/AppLayout";
import { ChatPage } from "@/components/chat/ChatPage";
import { SettingsPage } from "@/components/settings/SettingsPage";

// Import highlight.js theme for code blocks
import "highlight.js/styles/github-dark.css";

/**
 * Root application component.
 * Implements a simple hash-based router (no external dependency).
 * Routes: /#/ (chat), /#/settings/provider, /#/settings/auth, etc.
 */

const VALID_ROUTES: Route[] = [
  "chat",
  "settings/provider",
  "settings/persona",
  "settings/auth",
  "settings/sessions",
  "settings/memory",
  "settings/usage",
  "settings/mcp",
  "settings/discord",
  "settings/telegram",
  "settings/tools",
];

function parseHash(): Route {
  const hash = window.location.hash.replace("#/", "").replace("#", "");
  if (!hash || hash === "/") return "chat";
  const route = hash as Route;
  if (VALID_ROUTES.includes(route)) return route;
  return "chat";
}

function setHash(route: Route) {
  const hash = route === "chat" ? "/" : `/${route}`;
  window.location.hash = hash;
}

export function App() {
  const route = useAppStore((s) => s.route);
  const setRoute = useAppStore((s) => s.setRoute);
  const setActivePersona = useAppStore((s) => s.setActivePersona);

  // Sync hash → store on initial load and popstate
  useEffect(() => {
    function onHashChange() {
      setRoute(parseHash());
    }
    // Initial sync
    setRoute(parseHash());
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, [setRoute]);

  // Sync store → hash when route changes
  useEffect(() => {
    const currentHash = parseHash();
    if (currentHash !== route) {
      setHash(route);
    }
  }, [route]);

  useEffect(() => {
    let cancelled = false;

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
  }, [setActivePersona]);

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

  const isSettings = route.startsWith("settings");

  return (
    <AppLayout>
      {isSettings ? <SettingsPage /> : <ChatPage />}
    </AppLayout>
  );
}
