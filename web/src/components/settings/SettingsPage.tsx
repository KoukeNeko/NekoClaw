import { useAppStore, type Route } from "@/store/appStore";
import { GeneralPanel } from "./GeneralPanel";
import { CompactionPanel } from "./CompactionPanel";
import { ModelRolesPanel } from "./ModelRolesPanel";
import { PlaybooksPanel } from "./PlaybooksPanel";
import { AutomationsPanel } from "./AutomationsPanel";
import { PermissionsPanel } from "./PermissionsPanel";
import { SecurityPanel } from "./SecurityPanel";
import { BackupPanel } from "./BackupPanel";
import { PersonaPanel } from "./PersonaPanel";
import { ProviderPanel } from "./ProviderPanel";
import { DiscordPanel } from "./DiscordPanel";
import { TelegramPanel } from "./TelegramPanel";
import { AuthPanel } from "./AuthPanel";
import { ToolsPanel } from "./ToolsPanel";
import { MemoryPanel } from "./MemoryPanel";
import { MCPPanel } from "./MCPPanel";
import { UsagePanel } from "./UsagePanel";
import { ConsolePanel } from "./ConsolePanel";

interface TabDef {
  route: Route;
  label: string;
}

const TABS: TabDef[] = [
  { route: "settings/general", label: "一般" },
  { route: "settings/compaction", label: "壓縮" },
  { route: "settings/model-roles", label: "Model Roles" },
  { route: "settings/playbooks", label: "Playbooks" },
  { route: "settings/automations", label: "Automations" },
  { route: "settings/permissions", label: "Permissions" },
  { route: "settings/security", label: "安全" },
  { route: "settings/backup", label: "備份" },
  { route: "settings/provider", label: "Provider" },
  { route: "settings/persona", label: "Persona" },
  { route: "settings/auth", label: "認證" },
  { route: "settings/memory", label: "記憶" },
  { route: "settings/usage", label: "用量" },
  { route: "settings/mcp", label: "MCP" },
  { route: "settings/discord", label: "Discord" },
  { route: "settings/telegram", label: "Telegram" },
  { route: "settings/tools", label: "工具" },
  { route: "settings/console", label: "Console" },
];

/** Resolve active route to the corresponding panel component. */
function renderPanel(route: Route) {
  switch (route) {
    case "settings/general":
      return <GeneralPanel />;
    case "settings/compaction":
      return <CompactionPanel />;
    case "settings/model-roles":
      return <ModelRolesPanel />;
    case "settings/playbooks":
      return <PlaybooksPanel />;
    case "settings/automations":
      return <AutomationsPanel />;
    case "settings/permissions":
      return <PermissionsPanel />;
    case "settings/security":
      return <SecurityPanel />;
    case "settings/backup":
      return <BackupPanel />;
    case "settings/provider":
      return <ProviderPanel />;
    case "settings/persona":
      return <PersonaPanel />;
    case "settings/auth":
      return <AuthPanel />;
    case "settings/memory":
      return <MemoryPanel />;
    case "settings/usage":
      return <UsagePanel />;
    case "settings/mcp":
      return <MCPPanel />;
    case "settings/discord":
      return <DiscordPanel />;
    case "settings/telegram":
      return <TelegramPanel />;
    case "settings/tools":
      return <ToolsPanel />;
    case "settings/console":
      return <ConsolePanel />;
    default: {
      const label = TABS.find((t) => t.route === route)?.label ?? "";
      return <PlaceholderPanel name={label} />;
    }
  }
}

export function SettingsPage() {
  const route = useAppStore((s) => s.route);
  const setRoute = useAppStore((s) => s.setRoute);
  const panelWidth = "max-w-[1600px]";

  return (
    <div className="flex h-full min-h-0 flex-col">
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

      <div className="border-b border-base-300 bg-base-200/35">
        <div className={`mx-auto w-full ${panelWidth}`}>
          <div
            role="tablist"
            aria-label="設定分類"
            className="flex gap-2 overflow-x-auto px-2 py-2 sm:px-4 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
          >
            {TABS.map((tab) => {
              const active = route === tab.route;
              return (
                <button
                  key={tab.route}
                  type="button"
                  role="tab"
                  aria-selected={active}
                  className={`btn btn-sm shrink-0 rounded-full border transition-colors ${
                    active
                      ? "border-primary/50 bg-base-100 text-base-content shadow-sm"
                      : "border-transparent bg-transparent text-base-content/65 hover:border-base-300 hover:bg-base-100/70 hover:text-base-content"
                  }`}
                  onClick={() => setRoute(tab.route)}
                >
                  {tab.label}
                </button>
              );
            })}
          </div>
        </div>
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto bg-base-100 [scrollbar-width:thin]">
        <div className={`mx-auto w-full px-4 py-4 sm:px-6 sm:py-6 ${panelWidth}`}>
          {renderPanel(route)}
        </div>
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
