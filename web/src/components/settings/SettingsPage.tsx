import { Fragment } from "react";
import { useAppStore, type Route } from "@/store/appStore";
import { ProviderPanel } from "./ProviderPanel";

/**
 * Settings page — daisyUI tabs (official radio + tab-content pattern).
 * Tabs structure matches https://daisyui.com/components/tab/ exactly.
 */

interface TabDef {
  route: Route;
  label: string;
}

const TABS: TabDef[] = [
  { route: "settings/provider", label: "Provider" },
  { route: "settings/persona", label: "Persona" },
  { route: "settings/auth", label: "認證" },
  { route: "settings/sessions", label: "對話" },
  { route: "settings/memory", label: "記憶" },
  { route: "settings/usage", label: "用量" },
  { route: "settings/mcp", label: "MCP" },
  { route: "settings/discord", label: "Discord" },
  { route: "settings/telegram", label: "Telegram" },
  { route: "settings/tools", label: "工具" },
];

/** Resolve active route to the corresponding panel component. */
function renderPanel(route: Route) {
  switch (route) {
    case "settings/provider":
      return <ProviderPanel />;
    default: {
      const label = TABS.find((t) => t.route === route)?.label ?? "";
      return <PlaceholderPanel name={label} />;
    }
  }
}

export function SettingsPage() {
  const route = useAppStore((s) => s.route);
  const setRoute = useAppStore((s) => s.setRoute);

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <header className="navbar bg-base-200/50 backdrop-blur-sm border-b border-base-300 min-h-12 px-3">
        <div className="flex-none">
          <label
            htmlFor="app-drawer"
            aria-label="toggle sidebar"
            className="btn btn-ghost btn-sm btn-square"
          >
            <svg
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 24 24"
              strokeLinejoin="round"
              strokeLinecap="round"
              strokeWidth="2"
              fill="none"
              stroke="currentColor"
              className="size-4"
            >
              <path d="M4 4m0 2a2 2 0 0 1 2 -2h12a2 2 0 0 1 2 2v12a2 2 0 0 1 -2 2h-12a2 2 0 0 1 -2 -2z" />
              <path d="M9 4v16" />
              <path d="M14 10l2 2l-2 2" />
            </svg>
          </label>
        </div>
        <div className="flex-1">
          <span className="text-sm font-semibold">設定</span>
        </div>
        <button
          className="btn btn-ghost btn-sm"
          onClick={() => setRoute("chat")}
        >
          返回對話
        </button>
      </header>

      {/* daisyUI tabs — official pattern */}
      <div className="tabs tabs-border flex-1">
        {TABS.map((tab) => (
          <Fragment key={tab.route}>
            <input
              type="radio"
              name="settings-tabs"
              className="tab"
              aria-label={tab.label}
              checked={route === tab.route}
              onChange={() => setRoute(tab.route)}
            />
            <div className="tab-content border-base-300 bg-base-100 p-6">
              <div className="max-w-2xl mx-auto">
                {route === tab.route && renderPanel(tab.route)}
              </div>
            </div>
          </Fragment>
        ))}
      </div>
    </div>
  );
}

/** Placeholder for settings panels not yet implemented — daisyUI skeleton. */
function PlaceholderPanel({ name }: { name: string }) {
  return (
    <div className="space-y-6 py-4">
      <div className="text-center text-base-content/40 mb-4">
        <p className="text-lg font-semibold">{name}</p>
        <p className="text-sm mt-1">即將推出</p>
      </div>
      <fieldset className="fieldset" disabled>
        <legend className="fieldset-legend">
          <div className="skeleton h-4 w-24" />
        </legend>
        <div className="skeleton h-10 w-full rounded-btn" />
      </fieldset>
      <fieldset className="fieldset" disabled>
        <legend className="fieldset-legend">
          <div className="skeleton h-4 w-32" />
        </legend>
        <div className="skeleton h-10 w-full rounded-btn" />
      </fieldset>
      <fieldset className="fieldset" disabled>
        <legend className="fieldset-legend">
          <div className="skeleton h-4 w-20" />
        </legend>
        <div className="space-y-2">
          <div className="skeleton h-8 w-full rounded-btn" />
          <div className="skeleton h-8 w-full rounded-btn" />
          <div className="skeleton h-8 w-3/4 rounded-btn" />
        </div>
      </fieldset>
    </div>
  );
}
