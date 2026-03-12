import { useEffect, useState } from "react";
import {
  ApiError,
  addAIStudioKey,
  addAnthropicAPIKey,
  addAnthropicToken,
  addOpenAIKey,
  addOpenAICodexToken,
  cancelAnthropicBrowserLogin,
  cancelOpenAICodexBrowserLogin,
  completeAnthropicBrowserManual,
  completeGeminiOAuthManual,
  completeOpenAICodexBrowserManual,
  deleteAIStudioProfile,
  deleteAnthropicProfile,
  deleteOpenAIProfile,
  deleteOpenAICodexProfile,
  getAnthropicBrowserJob,
  getOpenAICodexBrowserJob,
  listAIStudioProfiles,
  listAnthropicProfiles,
  listGeminiProfiles,
  listOpenAICodexProfiles,
  listOpenAIProfiles,
  startAnthropicBrowserLogin,
  startGeminiOAuth,
  startOpenAICodexBrowserLogin,
  updateGeminiProfileConfig,
  useAIStudioProfile,
  useAnthropicProfile,
  useGeminiProfile,
  useOpenAIProfile,
  useOpenAICodexProfile,
} from "@/api/client";
import type {
  AIStudioProfile,
  AnthropicBrowserJobResponse,
  AnthropicBrowserStartResponse,
  AnthropicProfile,
  GeminiAuthProfile,
  GeminiExecutionMode,
  GeminiAuthStartResponse,
  OpenAICodexBrowserJobResponse,
  OpenAICodexBrowserStartResponse,
  OpenAIProfile,
} from "@/api/types";

type StatusTone = "success" | "error" | "info";
type StatusState = { tone: StatusTone; message: string } | null;
type AuthGroupKey = "gemini" | "aiStudio" | "anthropic" | "openAI";
type SelectionState = Record<AuthGroupKey, string>;
type CredentialDraft = { secret: string; displayName: string };

interface NavBadge {
  label: string;
  className: string;
}

interface NavProfile {
  key: string;
  title: string;
  subtitle: string;
  badges: NavBadge[];
}

const EMPTY_SELECTIONS: SelectionState = {
  gemini: "",
  aiStudio: "",
  anthropic: "",
  openAI: "",
};

function statusClass(tone: StatusTone): string {
  switch (tone) {
    case "success":
      return "alert-success";
    case "error":
      return "alert-error";
    default:
      return "alert-info";
  }
}

function fallback(value?: string | null, defaultValue = "-"): string {
  const trimmed = value?.trim();
  return trimmed ? trimmed : defaultValue;
}

function isLocalBrowserHost(hostname: string): boolean {
  const normalized = hostname.trim().toLowerCase();
  return (
    normalized === "localhost" ||
    normalized === "127.0.0.1" ||
    normalized === "::1" ||
    normalized === "[::1]"
  );
}

function buildGeminiOAuthRequest(): { mode: "auto" | "remote"; redirect_uri?: string } {
  if (typeof window === "undefined") {
    return { mode: "auto" };
  }
  if (isLocalBrowserHost(window.location.hostname)) {
    return { mode: "auto" };
  }
  return { mode: "remote" };
}

function geminiCallbackExample(redirectURI?: string | null): string {
  const trimmed = redirectURI?.trim();
  const base = trimmed || "http://127.0.0.1:8085/oauth2callback";
  return `${base}?code=...&state=...`;
}

function formatDateTime(value?: string | null): string {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString("zh-TW", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function formatLongDateTime(value?: string | null): string {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString("zh-TW", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function normalizeUntilTimestamp(value?: string | null): string | undefined {
  const trimmed = value?.trim();
  if (!trimmed) return undefined;

  const date = new Date(trimmed);
  if (Number.isNaN(date.getTime())) return undefined;
  if (date.getUTCFullYear() <= 1) return undefined;
  if (date.getTime() <= Date.now()) return undefined;

  return trimmed;
}

function geminiProfileKey(profile: GeminiAuthProfile): string {
  return profile.profile_id;
}

function aiStudioProfileKey(profile: AIStudioProfile): string {
  return profile.profile_id;
}

function anthropicProfileKey(profile: AnthropicProfile): string {
  return profile.profile_id;
}

function openAIProfileKey(profile: OpenAIProfile): string {
  return `${profile.provider}:${profile.profile_id}`;
}

function sortGeminiProfiles(incoming: GeminiAuthProfile[]): GeminiAuthProfile[] {
  return [...incoming].sort((left, right) => {
    if (left.preferred !== right.preferred) return left.preferred ? -1 : 1;
    return left.profile_id.localeCompare(right.profile_id, "en", {
      sensitivity: "base",
    });
  });
}

function sortAIStudioProfiles(incoming: AIStudioProfile[]): AIStudioProfile[] {
  return [...incoming].sort((left, right) => {
    if (left.preferred !== right.preferred) return left.preferred ? -1 : 1;
    return left.profile_id.localeCompare(right.profile_id, "en", {
      sensitivity: "base",
    });
  });
}

function sortAnthropicProfiles(incoming: AnthropicProfile[]): AnthropicProfile[] {
  return [...incoming].sort((left, right) => {
    if (left.preferred !== right.preferred) return left.preferred ? -1 : 1;
    return left.profile_id.localeCompare(right.profile_id, "en", {
      sensitivity: "base",
    });
  });
}

function sortOpenAIProfiles(incoming: OpenAIProfile[]): OpenAIProfile[] {
  return [...incoming].sort((left, right) => {
    if (left.preferred !== right.preferred) return left.preferred ? -1 : 1;
    const byProvider = left.provider.localeCompare(right.provider, "en", {
      sensitivity: "base",
    });
    if (byProvider !== 0) return byProvider;
    return left.profile_id.localeCompare(right.profile_id, "en", {
      sensitivity: "base",
    });
  });
}

function reconcileSelection<T>(
  currentKey: string,
  items: T[],
  getKey: (item: T) => string,
): string {
  if (currentKey && items.some((item) => getKey(item) === currentKey)) {
    return currentKey;
  }
  return items[0] ? getKey(items[0]) : "";
}

function availabilityBadge(
  available: boolean,
  cooldownUntil?: string,
  disabledUntil?: string,
): NavBadge {
  const activeDisabledUntil = normalizeUntilTimestamp(disabledUntil);
  const activeCooldownUntil = normalizeUntilTimestamp(cooldownUntil);

  if (!available && activeDisabledUntil) {
    return {
      label: `Disabled ${formatDateTime(activeDisabledUntil)}`,
      className: "badge-error",
    };
  }
  if (!available && activeCooldownUntil) {
    return {
      label: `Cooldown ${formatDateTime(activeCooldownUntil)}`,
      className: "badge-warning",
    };
  }
  return {
    label: available ? "Available" : "Unavailable",
    className: available ? "badge-success" : "badge-ghost",
  };
}

function profileBadges(profile: {
  preferred: boolean;
  available: boolean;
  cooldown_until?: string;
  disabled_until?: string;
}): NavBadge[] {
  const badges = [availabilityBadge(profile.available, profile.cooldown_until, profile.disabled_until)];
  if (profile.preferred) {
    badges.unshift({ label: "Preferred", className: "badge-primary" });
  }
  return badges;
}

function geminiExecutionModeLabel(mode: GeminiExecutionMode): string {
  return mode === "cli_headless" ? "CLI Headless" : "Internal API";
}

function upsertGeminiProfile(
  profiles: GeminiAuthProfile[],
  nextProfile: GeminiAuthProfile,
): GeminiAuthProfile[] {
  const exists = profiles.some((profile) => profile.profile_id === nextProfile.profile_id);
  const next = exists
    ? profiles.map((profile) =>
        profile.profile_id === nextProfile.profile_id ? nextProfile : profile,
      )
    : [...profiles, nextProfile];
  return sortGeminiProfiles(next);
}

function geminiNavProfiles(profiles: GeminiAuthProfile[]): NavProfile[] {
  return profiles.map((profile) => {
    const badges = profileBadges(profile);
    if (!profile.project_ready) {
      badges.push({ label: "Project missing", className: "badge-warning" });
    }
    return {
      key: geminiProfileKey(profile),
      title: fallback(profile.email, profile.profile_id),
      subtitle: `${profile.profile_id} · project ${fallback(profile.project_id)}`,
      badges,
    };
  });
}

function aiStudioNavProfiles(profiles: AIStudioProfile[]): NavProfile[] {
  return profiles.map((profile) => ({
    key: aiStudioProfileKey(profile),
    title: fallback(profile.display_name, profile.profile_id),
    subtitle: fallback(profile.key_hint, profile.profile_id),
    badges: profileBadges(profile),
  }));
}

function anthropicNavProfiles(profiles: AnthropicProfile[]): NavProfile[] {
  return profiles.map((profile) => ({
    key: anthropicProfileKey(profile),
    title: fallback(profile.display_name, profile.profile_id),
    subtitle: `${fallback(profile.type)} · ${fallback(profile.key_hint)}`,
    badges: profileBadges(profile),
  }));
}

function openAINavProfiles(profiles: OpenAIProfile[]): NavProfile[] {
  return profiles.map((profile) => ({
    key: openAIProfileKey(profile),
    title: fallback(profile.display_name, profile.profile_id),
    subtitle: `${profile.provider} · ${fallback(profile.type)} · ${fallback(profile.key_hint)}`,
    badges: profileBadges(profile),
  }));
}

function findSelected<T>(
  items: T[],
  currentKey: string,
  getKey: (item: T) => string,
): T | null {
  return items.find((item) => getKey(item) === currentKey) ?? items[0] ?? null;
}

function isTerminalJobError(error: unknown): boolean {
  if (!(error instanceof ApiError)) return false;
  switch (error.code.trim()) {
    case "job_not_found":
    case "job_expired":
    case "job_cancelled":
    case "job_completed":
      return true;
    default:
      return false;
  }
}

function jobNeedsManualIntervention(
  job: { status: string; manual_hint?: string } | null,
): boolean {
  if (!job) return false;
  return job.status === "manual_required" || (
    job.status === "failed" && !!job.manual_hint?.trim()
  );
}

function anthropicJobFromStart(
  response: AnthropicBrowserStartResponse,
): AnthropicBrowserJobResponse {
  return {
    job_id: response.job_id,
    provider: response.provider,
    mode: response.mode,
    status: response.status,
    events: [],
    expires_at: response.expires_at,
    message: response.message,
    manual_hint: response.manual_hint,
  };
}

function openAIJobFromStart(
  response: OpenAICodexBrowserStartResponse,
): OpenAICodexBrowserJobResponse {
  return {
    job_id: response.job_id,
    provider: response.provider,
    mode: response.mode,
    status: response.status,
    events: [],
    expires_at: response.expires_at,
    message: response.message,
    manual_hint: response.manual_hint,
  };
}

function selectedProviderTitle(
  group: AuthGroupKey,
  selected:
    | GeminiAuthProfile
    | AIStudioProfile
    | AnthropicProfile
    | OpenAIProfile
    | null,
): string {
  if (!selected) {
    switch (group) {
      case "gemini":
        return "Gemini OAuth";
      case "aiStudio":
        return "AI Studio Keys";
      case "anthropic":
        return "Anthropic";
      default:
        return "OpenAI / Codex";
    }
  }

  switch (group) {
    case "gemini":
      return fallback((selected as GeminiAuthProfile).email, selected.profile_id);
    case "aiStudio":
    case "anthropic":
    case "openAI":
      return fallback(
        (selected as AIStudioProfile | AnthropicProfile | OpenAIProfile)
          .display_name,
        selected.profile_id,
      );
  }
}

function ProviderNavCard(props: {
  title: string;
  description: string;
  active: boolean;
  profileCount: number;
  selectedLabel: string;
  stateBadge: NavBadge;
  profiles: NavProfile[];
  selectedKey: string;
  onActivate: () => void;
  onSelectProfile: (key: string) => void;
}) {
  const {
    title,
    description,
    active,
    profileCount,
    selectedLabel,
    stateBadge,
    profiles,
    selectedKey,
    onActivate,
    onSelectProfile,
  } = props;

  return (
    <div
      className={`card min-w-0 border shadow-sm transition-colors ${
        active
          ? "border-primary bg-base-200"
          : "border-base-300 bg-base-200/80"
      }`}
    >
      <div className="card-body gap-4 p-4">
        <button
          type="button"
          className="flex w-full min-w-0 items-start justify-between gap-3 text-left"
          onClick={onActivate}
        >
          <div className="min-w-0 flex-1 space-y-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-semibold">{title}</span>
              <div className={`badge badge-sm ${stateBadge.className}`}>
                {stateBadge.label}
              </div>
            </div>
            <p className="text-sm text-base-content/60">{description}</p>
          </div>
          <div className="badge badge-outline badge-sm shrink-0 whitespace-nowrap">
            {profileCount}
          </div>
        </button>

        <div className="rounded-box border border-base-300 bg-base-100 px-3 py-2">
          <div className="text-xs uppercase tracking-wide text-base-content/45">
            Current Selection
          </div>
          <div className="mt-1 break-all text-sm font-medium">{selectedLabel}</div>
        </div>

        {profiles.length === 0 ? (
          <div className="rounded-box border border-dashed border-base-300 bg-base-100/70 px-3 py-4 text-sm text-base-content/55">
            尚無 profiles。切換到右側面板新增或啟動登入流程。
          </div>
        ) : (
          <div className="max-h-80 space-y-2 overflow-y-auto pr-1 [scrollbar-width:thin]">
            {profiles.map((profile) => (
              <button
                key={profile.key}
                type="button"
                className={`card w-full border text-left shadow-sm transition-colors ${
                  selectedKey === profile.key
                    ? "border-primary bg-primary/8"
                    : "border-base-300 bg-base-100"
                }`}
                onClick={() => onSelectProfile(profile.key)}
              >
                <div className="card-body gap-3 p-3">
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0 flex-1">
                      <div className="truncate font-medium">{profile.title}</div>
                      <div className="mt-1 truncate text-xs text-base-content/55">
                        {profile.subtitle}
                      </div>
                    </div>
                    {selectedKey === profile.key && (
                      <div className="badge badge-primary badge-sm shrink-0 whitespace-nowrap leading-none">
                        選取中
                      </div>
                    )}
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {profile.badges.map((badge) => (
                      <div
                        key={`${profile.key}-${badge.label}`}
                        className={`badge badge-sm ${badge.className}`}
                      >
                        {badge.label}
                      </div>
                    ))}
                  </div>
                </div>
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function SecretField(props: {
  label: string;
  placeholder: string;
  value: string;
  onChange: (value: string) => void;
  hint?: string;
  disabled?: boolean;
  autoComplete?: string;
}) {
  const {
    label,
    placeholder,
    value,
    onChange,
    hint,
    disabled,
    autoComplete = "off",
  } = props;
  const [visible, setVisible] = useState(false);

  return (
    <label className="fieldset">
      <legend className="fieldset-legend">{label}</legend>
      <div className="join join-vertical w-full sm:join-horizontal">
        <input
          type={visible ? "text" : "password"}
          className="input input-bordered join-item w-full font-mono outline-none focus:outline-none focus:ring-0 focus-visible:outline-none focus:shadow-none"
          placeholder={placeholder}
          autoComplete={autoComplete}
          spellCheck={false}
          value={value}
          onChange={(event) => onChange(event.target.value)}
          disabled={disabled}
        />
        <button
          type="button"
          className="btn join-item"
          onClick={() => setVisible((current) => !current)}
          disabled={disabled}
        >
          {visible ? "隱藏" : "顯示"}
        </button>
      </div>
      {hint && <p className="label text-base-content/55">{hint}</p>}
    </label>
  );
}

function LoadingPanel() {
  return (
    <div className="space-y-6">
      <div className="card border border-base-300 bg-base-200 shadow-sm">
        <div className="card-body gap-4">
          <div className="flex gap-2">
            <div className="skeleton h-5 w-20" />
            <div className="skeleton h-5 w-16" />
          </div>
          <div className="skeleton h-8 w-60" />
          <div className="skeleton h-4 w-96 max-w-full" />
          <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
            {Array.from({ length: 4 }).map((_, index) => (
              <div key={index} className="stat">
                <div className="skeleton h-4 w-24" />
                <div className="mt-3 skeleton h-8 w-28" />
                <div className="mt-3 skeleton h-4 w-36" />
              </div>
            ))}
          </div>
        </div>
      </div>

      <div className="grid gap-6 xl:grid-cols-[minmax(320px,0.95fr)_minmax(0,1.65fr)]">
        <div className="space-y-4">
          {Array.from({ length: 4 }).map((_, index) => (
            <div
              key={index}
              className="card border border-base-300 bg-base-200 shadow-sm"
            >
              <div className="card-body gap-4 p-4">
                <div className="skeleton h-6 w-40" />
                <div className="skeleton h-4 w-full" />
                <div className="space-y-2">
                  {Array.from({ length: 2 }).map((_, row) => (
                    <div
                      key={row}
                      className="rounded-box border border-base-300 bg-base-100 p-3"
                    >
                      <div className="skeleton h-4 w-40" />
                      <div className="mt-2 skeleton h-3 w-full" />
                    </div>
                  ))}
                </div>
              </div>
            </div>
          ))}
        </div>

        <div className="space-y-6">
          <div className="card border border-base-300 bg-base-200 shadow-sm">
            <div className="card-body gap-4">
              <div className="skeleton h-8 w-72" />
              <div className="skeleton h-4 w-96 max-w-full" />
              <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
                {Array.from({ length: 3 }).map((_, index) => (
                  <div key={index} className="stat">
                    <div className="skeleton h-4 w-20" />
                    <div className="mt-3 skeleton h-8 w-24" />
                    <div className="mt-3 skeleton h-4 w-32" />
                  </div>
                ))}
              </div>
            </div>
          </div>
          <div className="card border border-base-300 bg-base-200 shadow-sm">
            <div className="card-body gap-4">
              <div className="skeleton h-6 w-48" />
              <div className="skeleton h-28 w-full" />
              <div className="skeleton h-10 w-full" />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

export function AuthPanel() {
  const [geminiProfiles, setGeminiProfiles] = useState<GeminiAuthProfile[]>([]);
  const [aiStudioProfiles, setAIStudioProfiles] = useState<AIStudioProfile[]>([]);
  const [anthropicProfiles, setAnthropicProfiles] = useState<AnthropicProfile[]>(
    [],
  );
  const [openAIProfiles, setOpenAIProfiles] = useState<OpenAIProfile[]>([]);
  const [openAICodexProfiles, setOpenAICodexProfiles] = useState<
    OpenAIProfile[]
  >([]);
  const [selectedGroup, setSelectedGroup] = useState<AuthGroupKey>("gemini");
  const [selectedKeys, setSelectedKeys] = useState<SelectionState>(
    EMPTY_SELECTIONS,
  );
  const [geminiFlow, setGeminiFlow] = useState<GeminiAuthStartResponse | null>(
    null,
  );
  const [anthropicJob, setAnthropicJob] =
    useState<AnthropicBrowserJobResponse | null>(null);
  const [openAIBrowserJob, setOpenAIBrowserJob] =
    useState<OpenAICodexBrowserJobResponse | null>(null);
  const [geminiManualInput, setGeminiManualInput] = useState("");
  const [aiStudioDraft, setAIStudioDraft] = useState<CredentialDraft>({
    secret: "",
    displayName: "",
  });
  const [anthropicTokenDraft, setAnthropicTokenDraft] = useState<CredentialDraft>(
    {
      secret: "",
      displayName: "",
    },
  );
  const [anthropicKeyDraft, setAnthropicKeyDraft] = useState<CredentialDraft>({
    secret: "",
    displayName: "",
  });
  const [openAIKeyDraft, setOpenAIKeyDraft] = useState<CredentialDraft>({
    secret: "",
    displayName: "",
  });
  const [openAICodexDraft, setOpenAICodexDraft] = useState<CredentialDraft>({
    secret: "",
    displayName: "",
  });
  const [anthropicManualSecret, setAnthropicManualSecret] = useState("");
  const [openAIManualSecret, setOpenAIManualSecret] = useState("");
  const [loading, setLoading] = useState(true);
  const [busyAction, setBusyAction] = useState("");
  const [status, setStatus] = useState<StatusState>(null);

  const combinedOpenAIProfiles = sortOpenAIProfiles([
    ...openAIProfiles,
    ...openAICodexProfiles,
  ]);
  const selectedGemini = findSelected(
    geminiProfiles,
    selectedKeys.gemini,
    geminiProfileKey,
  );
  const selectedAIStudio = findSelected(
    aiStudioProfiles,
    selectedKeys.aiStudio,
    aiStudioProfileKey,
  );
  const selectedAnthropic = findSelected(
    anthropicProfiles,
    selectedKeys.anthropic,
    anthropicProfileKey,
  );
  const selectedOpenAI = findSelected(
    combinedOpenAIProfiles,
    selectedKeys.openAI,
    openAIProfileKey,
  );

  async function syncProfiles(showLoading = false) {
    if (showLoading) setLoading(true);

    try {
      const [
        loadedGemini,
        loadedAIStudio,
        loadedAnthropic,
        loadedOpenAI,
        loadedOpenAICodex,
      ] = await Promise.all([
        listGeminiProfiles(),
        listAIStudioProfiles(),
        listAnthropicProfiles(),
        listOpenAIProfiles(),
        listOpenAICodexProfiles(),
      ]);

      const sortedGemini = sortGeminiProfiles(loadedGemini);
      const sortedAIStudio = sortAIStudioProfiles(loadedAIStudio);
      const sortedAnthropic = sortAnthropicProfiles(loadedAnthropic);
      const sortedOpenAI = sortOpenAIProfiles(loadedOpenAI);
      const sortedOpenAICodex = sortOpenAIProfiles(loadedOpenAICodex);
      const sortedCombinedOpenAI = sortOpenAIProfiles([
        ...sortedOpenAI,
        ...sortedOpenAICodex,
      ]);

      setGeminiProfiles(sortedGemini);
      setAIStudioProfiles(sortedAIStudio);
      setAnthropicProfiles(sortedAnthropic);
      setOpenAIProfiles(sortedOpenAI);
      setOpenAICodexProfiles(sortedOpenAICodex);
      setSelectedKeys((current) => ({
        gemini: reconcileSelection(current.gemini, sortedGemini, geminiProfileKey),
        aiStudio: reconcileSelection(
          current.aiStudio,
          sortedAIStudio,
          aiStudioProfileKey,
        ),
        anthropic: reconcileSelection(
          current.anthropic,
          sortedAnthropic,
          anthropicProfileKey,
        ),
        openAI: reconcileSelection(
          current.openAI,
          sortedCombinedOpenAI,
          openAIProfileKey,
        ),
      }));
      return true;
    } catch {
      setStatus({ tone: "error", message: "無法載入認證設定" });
      return false;
    } finally {
      if (showLoading) setLoading(false);
    }
  }

  useEffect(() => {
    void syncProfiles(true);
  }, []);

  useEffect(() => {
    if (!status) return undefined;
    const timer = window.setTimeout(() => setStatus(null), 2400);
    return () => window.clearTimeout(timer);
  }, [status]);

  useEffect(() => {
    if (!anthropicJob?.job_id || anthropicJob.status !== "running") {
      return undefined;
    }

    let cancelled = false;
    let timer = 0;

    const poll = async () => {
      try {
        const snapshot = await getAnthropicBrowserJob(anthropicJob.job_id);
        if (cancelled) return;

        if (snapshot.status === "completed") {
          setAnthropicJob(null);
          const ok = await syncProfiles(false);
          if (!cancelled && ok) {
            setStatus({ tone: "success", message: "Anthropic Browser Login 完成" });
          }
          return;
        }

        if (snapshot.status === "failed" && !snapshot.manual_hint?.trim()) {
          setAnthropicJob(null);
          const ok = await syncProfiles(false);
          if (!cancelled) {
            setStatus({
              tone: "error",
              message:
                snapshot.error_message ||
                snapshot.message ||
                "Anthropic Browser Login 失敗",
            });
          }
          if (!cancelled && ok) return;
          return;
        }

        setAnthropicJob(snapshot);
        if (jobNeedsManualIntervention(snapshot) && snapshot.message?.trim()) {
          setStatus({ tone: "info", message: snapshot.message.trim() });
          return;
        }

        timer = window.setTimeout(() => {
          void poll();
        }, 1000);
      } catch (error) {
        if (cancelled) return;
        if (isTerminalJobError(error)) {
          setAnthropicJob(null);
          const ok = await syncProfiles(false);
          if (ok) {
            setStatus({
              tone: "info",
              message: "Anthropic Browser Login 工作已結束，已重新同步 profiles",
            });
          }
          return;
        }

        setStatus({
          tone: "error",
          message: "Anthropic Browser Login 狀態查詢失敗",
        });
        timer = window.setTimeout(() => {
          void poll();
        }, 2000);
      }
    };

    timer = window.setTimeout(() => {
      void poll();
    }, 1000);

    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [anthropicJob?.job_id, anthropicJob?.status]);

  useEffect(() => {
    if (!openAIBrowserJob?.job_id || openAIBrowserJob.status !== "running") {
      return undefined;
    }

    let cancelled = false;
    let timer = 0;

    const poll = async () => {
      try {
        const snapshot = await getOpenAICodexBrowserJob(openAIBrowserJob.job_id);
        if (cancelled) return;

        if (snapshot.status === "completed") {
          setOpenAIBrowserJob(null);
          const ok = await syncProfiles(false);
          if (!cancelled && ok) {
            setStatus({
              tone: "success",
              message: "OpenAI Codex Browser Login 完成",
            });
          }
          return;
        }

        if (snapshot.status === "failed" && !snapshot.manual_hint?.trim()) {
          setOpenAIBrowserJob(null);
          const ok = await syncProfiles(false);
          if (!cancelled) {
            setStatus({
              tone: "error",
              message:
                snapshot.error_message ||
                snapshot.message ||
                "OpenAI Codex Browser Login 失敗",
            });
          }
          if (!cancelled && ok) return;
          return;
        }

        setOpenAIBrowserJob(snapshot);
        if (jobNeedsManualIntervention(snapshot) && snapshot.message?.trim()) {
          setStatus({ tone: "info", message: snapshot.message.trim() });
          return;
        }

        timer = window.setTimeout(() => {
          void poll();
        }, 1000);
      } catch (error) {
        if (cancelled) return;
        if (isTerminalJobError(error)) {
          setOpenAIBrowserJob(null);
          const ok = await syncProfiles(false);
          if (ok) {
            setStatus({
              tone: "info",
              message: "OpenAI Codex Browser Login 工作已結束，已重新同步 profiles",
            });
          }
          return;
        }

        setStatus({
          tone: "error",
          message: "OpenAI Codex Browser Login 狀態查詢失敗",
        });
        timer = window.setTimeout(() => {
          void poll();
        }, 2000);
      }
    };

    timer = window.setTimeout(() => {
      void poll();
    }, 1000);

    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [openAIBrowserJob?.job_id, openAIBrowserJob?.status]);

  function selectGroup(group: AuthGroupKey) {
    setSelectedGroup(group);
  }

  function selectProfile(group: AuthGroupKey, key: string) {
    setSelectedGroup(group);
    setSelectedKeys((current) => ({ ...current, [group]: key }));
  }

  async function handleRefresh() {
    setBusyAction("reload");
    try {
      const ok = await syncProfiles(false);
      if (ok) {
        setStatus({ tone: "info", message: "已重新同步認證設定" });
      }
    } finally {
      setBusyAction("");
    }
  }

  async function handleStartGeminiOAuth() {
    setBusyAction("gemini-start");
    try {
      const request = buildGeminiOAuthRequest();
      const response = await startGeminiOAuth(request);
      setGeminiFlow(response);
      setGeminiManualInput("");

      const shouldAutoOpen = response.mode === "loopback" || request.mode === "remote";
      const opened = shouldAutoOpen
        ? window.open(response.auth_url, "_blank", "noopener,noreferrer")
        : null;

      if (response.mode === "loopback") {
        setStatus({
          tone: "info",
          message: opened
            ? "已開啟 Gemini 授權頁面。若最後停在 127.0.0.1 callback，請把該網址或授權 code 貼回此頁。"
            : "Gemini OAuth 已啟動，請手動開啟授權網址完成登入",
        });
      } else if (request.mode === "remote") {
        setStatus({
          tone: "info",
          message: opened
            ? "已開啟 Gemini 授權頁面。Gemini CLI OAuth 只接受 127.0.0.1 loopback callback；完成後請把瀏覽器最後的 callback URL 或 code 貼回此頁。"
            : "Gemini OAuth 已啟動，請手動開啟授權網址完成登入",
        });
      } else {
        setStatus({
          tone: "info",
          message: "Gemini OAuth 已啟動，完成授權後貼上 127.0.0.1 callback URL 或 code",
        });
      }
    } catch {
      setStatus({ tone: "error", message: "啟動 Gemini OAuth 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleCompleteGeminiOAuth() {
    if (!geminiFlow || !geminiManualInput.trim()) return;
    setBusyAction("gemini-complete");
    try {
      await completeGeminiOAuthManual({
        state: geminiFlow.state,
        callback_url_or_code: geminiManualInput.trim(),
      });
      setGeminiFlow(null);
      setGeminiManualInput("");
      const ok = await syncProfiles(false);
      if (ok) {
        setStatus({ tone: "success", message: "Gemini OAuth 已完成" });
      }
    } catch {
      setStatus({ tone: "error", message: "Gemini OAuth 完成失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleUseGeminiProfile() {
    if (!selectedGemini) return;
    setBusyAction("gemini-use");
    try {
      await useGeminiProfile(selectedGemini.profile_id);
      const ok = await syncProfiles(false);
      if (ok) {
        setStatus({
          tone: "success",
          message: `已選用 ${fallback(selectedGemini.email, selectedGemini.profile_id)}`,
        });
      }
    } catch {
      setStatus({ tone: "error", message: "切換 Gemini profile 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleSetGeminiExecutionMode(mode: GeminiExecutionMode) {
    if (!selectedGemini || selectedGemini.execution_mode === mode) return;
    setBusyAction("gemini-execution-mode");
    try {
      const updated = await updateGeminiProfileConfig({
        profile_id: selectedGemini.profile_id,
        execution_mode: mode,
      });
      setGeminiProfiles((current) => upsertGeminiProfile(current, updated));
      setSelectedKeys((current) => ({ ...current, gemini: updated.profile_id }));
      setStatus({
        tone: "success",
        message:
          mode === "cli_headless"
            ? "Gemini profile 已切換為 CLI headless 模式"
            : "Gemini profile 已切回 internal API 模式",
      });
    } catch {
      setStatus({ tone: "error", message: "更新 Gemini execution mode 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleAddAIStudioKey() {
    if (!aiStudioDraft.secret.trim()) return;
    setBusyAction("ai-studio-add");
    try {
      const response = await addAIStudioKey({
        api_key: aiStudioDraft.secret.trim(),
        display_name: aiStudioDraft.displayName.trim() || undefined,
      });
      setAIStudioDraft({ secret: "", displayName: "" });
      setSelectedGroup("aiStudio");
      setSelectedKeys((current) => ({
        ...current,
        aiStudio: response.profile_id,
      }));
      const ok = await syncProfiles(false);
      if (ok) {
        setStatus({ tone: "success", message: "AI Studio API key 已新增" });
      }
    } catch {
      setStatus({ tone: "error", message: "新增 AI Studio API key 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleUseAIStudioProfile() {
    if (!selectedAIStudio) return;
    setBusyAction("ai-studio-use");
    try {
      await useAIStudioProfile(selectedAIStudio.profile_id);
      const ok = await syncProfiles(false);
      if (ok) {
        setStatus({
          tone: "success",
          message: `已選用 ${fallback(selectedAIStudio.display_name, selectedAIStudio.profile_id)}`,
        });
      }
    } catch {
      setStatus({ tone: "error", message: "切換 AI Studio profile 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleDeleteAIStudioProfile() {
    if (!selectedAIStudio) return;
    setBusyAction("ai-studio-delete");
    try {
      await deleteAIStudioProfile(selectedAIStudio.profile_id);
      const ok = await syncProfiles(false);
      if (ok) {
        setStatus({ tone: "info", message: "AI Studio profile 已刪除" });
      }
    } catch {
      setStatus({ tone: "error", message: "刪除 AI Studio profile 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleAddAnthropicToken() {
    if (!anthropicTokenDraft.secret.trim()) return;
    setBusyAction("anthropic-token");
    try {
      const response = await addAnthropicToken({
        setup_token: anthropicTokenDraft.secret.trim(),
        display_name: anthropicTokenDraft.displayName.trim() || undefined,
      });
      setAnthropicTokenDraft({ secret: "", displayName: "" });
      setSelectedGroup("anthropic");
      setSelectedKeys((current) => ({
        ...current,
        anthropic: response.profile_id,
      }));
      const ok = await syncProfiles(false);
      if (ok) {
        setStatus({ tone: "success", message: "Anthropic setup-token 已新增" });
      }
    } catch {
      setStatus({ tone: "error", message: "新增 Anthropic setup-token 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleAddAnthropicAPIKey() {
    if (!anthropicKeyDraft.secret.trim()) return;
    setBusyAction("anthropic-key");
    try {
      const response = await addAnthropicAPIKey({
        api_key: anthropicKeyDraft.secret.trim(),
        display_name: anthropicKeyDraft.displayName.trim() || undefined,
      });
      setAnthropicKeyDraft({ secret: "", displayName: "" });
      setSelectedGroup("anthropic");
      setSelectedKeys((current) => ({
        ...current,
        anthropic: response.profile_id,
      }));
      const ok = await syncProfiles(false);
      if (ok) {
        setStatus({ tone: "success", message: "Anthropic API key 已新增" });
      }
    } catch {
      setStatus({ tone: "error", message: "新增 Anthropic API key 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleUseAnthropicProfile() {
    if (!selectedAnthropic) return;
    setBusyAction("anthropic-use");
    try {
      await useAnthropicProfile(selectedAnthropic.profile_id);
      const ok = await syncProfiles(false);
      if (ok) {
        setStatus({
          tone: "success",
          message: `已選用 ${fallback(selectedAnthropic.display_name, selectedAnthropic.profile_id)}`,
        });
      }
    } catch {
      setStatus({ tone: "error", message: "切換 Anthropic profile 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleDeleteAnthropicProfile() {
    if (!selectedAnthropic) return;
    setBusyAction("anthropic-delete");
    try {
      await deleteAnthropicProfile(selectedAnthropic.profile_id);
      const ok = await syncProfiles(false);
      if (ok) {
        setStatus({ tone: "info", message: "Anthropic profile 已刪除" });
      }
    } catch {
      setStatus({ tone: "error", message: "刪除 Anthropic profile 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleStartAnthropicBrowserLogin() {
    if (openAIBrowserJob?.status === "running") {
      setStatus({
        tone: "info",
        message: "已有進行中的 OpenAI Codex Browser Login，請先完成或取消。",
      });
      return;
    }
    if (anthropicJob?.status === "running") {
      setStatus({
        tone: "info",
        message: "Anthropic Browser Login 已在進行中。",
      });
      return;
    }

    setBusyAction("anthropic-browser-start");
    try {
      const response = await startAnthropicBrowserLogin({ mode: "auto" });
      setAnthropicManualSecret("");

      if (response.status === "completed") {
        setAnthropicJob(null);
        const ok = await syncProfiles(false);
        if (ok) {
          setStatus({
            tone: "success",
            message: response.message?.trim() || "Anthropic Browser Login 完成",
          });
        }
      } else if (response.status === "failed" && !response.manual_hint?.trim()) {
        setAnthropicJob(null);
        setStatus({
          tone: "error",
          message: response.message?.trim() || "Anthropic Browser Login 失敗",
        });
      } else {
        setAnthropicJob(anthropicJobFromStart(response));
        setStatus({
          tone: "info",
          message: response.message?.trim() || "Anthropic Browser Login 已啟動",
        });
      }
    } catch {
      setStatus({ tone: "error", message: "啟動 Anthropic Browser Login 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleCompleteAnthropicBrowserManual() {
    if (!anthropicManualSecret.trim()) return;
    setBusyAction("anthropic-browser-complete");
    try {
      if (anthropicJob?.job_id) {
        await completeAnthropicBrowserManual({
          job_id: anthropicJob.job_id,
          setup_token: anthropicManualSecret.trim(),
        });
      } else {
        await addAnthropicToken({ setup_token: anthropicManualSecret.trim() });
      }
      setAnthropicManualSecret("");
      setAnthropicJob(null);
      const ok = await syncProfiles(false);
      if (ok) {
        setStatus({ tone: "success", message: "Anthropic credential 已完成寫入" });
      }
    } catch {
      setStatus({ tone: "error", message: "Anthropic manual fallback 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleCancelAnthropicBrowserLogin() {
    if (!anthropicJob?.job_id) {
      setAnthropicJob(null);
      return;
    }

    setBusyAction("anthropic-browser-cancel");
    try {
      await cancelAnthropicBrowserLogin(anthropicJob.job_id);
      setAnthropicJob(null);
      setAnthropicManualSecret("");
      setStatus({ tone: "info", message: "Anthropic Browser Login 已取消" });
    } catch (error) {
      if (isTerminalJobError(error)) {
        setAnthropicJob(null);
        setAnthropicManualSecret("");
        setStatus({
          tone: "info",
          message: "Anthropic Browser Login 已結束，已清除本地狀態",
        });
      } else {
        setStatus({ tone: "error", message: "取消 Anthropic Browser Login 失敗" });
      }
    } finally {
      setBusyAction("");
    }
  }

  async function handleAddOpenAIKey() {
    if (!openAIKeyDraft.secret.trim()) return;
    setBusyAction("openai-key");
    try {
      const response = await addOpenAIKey({
        api_key: openAIKeyDraft.secret.trim(),
        display_name: openAIKeyDraft.displayName.trim() || undefined,
      });
      setOpenAIKeyDraft({ secret: "", displayName: "" });
      setSelectedGroup("openAI");
      setSelectedKeys((current) => ({
        ...current,
        openAI: `openai:${response.profile_id}`,
      }));
      const ok = await syncProfiles(false);
      if (ok) {
        setStatus({ tone: "success", message: "OpenAI API key 已新增" });
      }
    } catch {
      setStatus({ tone: "error", message: "新增 OpenAI API key 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleAddOpenAICodexToken() {
    if (!openAICodexDraft.secret.trim()) return;
    setBusyAction("openai-codex-token");
    try {
      const response = await addOpenAICodexToken({
        token: openAICodexDraft.secret.trim(),
        display_name: openAICodexDraft.displayName.trim() || undefined,
      });
      setOpenAICodexDraft({ secret: "", displayName: "" });
      setSelectedGroup("openAI");
      setSelectedKeys((current) => ({
        ...current,
        openAI: `openai-codex:${response.profile_id}`,
      }));
      const ok = await syncProfiles(false);
      if (ok) {
        setStatus({ tone: "success", message: "OpenAI Codex token 已新增" });
      }
    } catch {
      setStatus({ tone: "error", message: "新增 OpenAI Codex token 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleUseOpenAIProfile() {
    if (!selectedOpenAI) return;
    setBusyAction("openai-use");
    try {
      if (selectedOpenAI.provider === "openai-codex") {
        await useOpenAICodexProfile(selectedOpenAI.profile_id);
      } else {
        await useOpenAIProfile(selectedOpenAI.profile_id);
      }

      const ok = await syncProfiles(false);
      if (ok) {
        setStatus({
          tone: "success",
          message: `已選用 ${fallback(selectedOpenAI.display_name, selectedOpenAI.profile_id)}`,
        });
      }
    } catch {
      setStatus({ tone: "error", message: "切換 OpenAI profile 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleDeleteOpenAIProfile() {
    if (!selectedOpenAI) return;
    setBusyAction("openai-delete");
    try {
      if (selectedOpenAI.provider === "openai-codex") {
        await deleteOpenAICodexProfile(selectedOpenAI.profile_id);
      } else {
        await deleteOpenAIProfile(selectedOpenAI.profile_id);
      }

      const ok = await syncProfiles(false);
      if (ok) {
        setStatus({ tone: "info", message: "OpenAI profile 已刪除" });
      }
    } catch {
      setStatus({ tone: "error", message: "刪除 OpenAI profile 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleStartOpenAIBrowserLogin() {
    if (anthropicJob?.status === "running") {
      setStatus({
        tone: "info",
        message: "已有進行中的 Anthropic Browser Login，請先完成或取消。",
      });
      return;
    }
    if (openAIBrowserJob?.status === "running") {
      setStatus({
        tone: "info",
        message: "OpenAI Codex Browser Login 已在進行中。",
      });
      return;
    }

    setBusyAction("openai-browser-start");
    try {
      const response = await startOpenAICodexBrowserLogin({ mode: "auto" });
      setOpenAIManualSecret("");

      if (response.status === "completed") {
        setOpenAIBrowserJob(null);
        const ok = await syncProfiles(false);
        if (ok) {
          setStatus({
            tone: "success",
            message: response.message?.trim() || "OpenAI Codex Browser Login 完成",
          });
        }
      } else if (response.status === "failed" && !response.manual_hint?.trim()) {
        setOpenAIBrowserJob(null);
        setStatus({
          tone: "error",
          message: response.message?.trim() || "OpenAI Codex Browser Login 失敗",
        });
      } else {
        setOpenAIBrowserJob(openAIJobFromStart(response));
        setStatus({
          tone: "info",
          message: response.message?.trim() || "OpenAI Codex Browser Login 已啟動",
        });
      }
    } catch {
      setStatus({ tone: "error", message: "啟動 OpenAI Codex Browser Login 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleCompleteOpenAIBrowserManual() {
    if (!openAIManualSecret.trim()) return;
    setBusyAction("openai-browser-complete");
    try {
      if (openAIBrowserJob?.job_id) {
        await completeOpenAICodexBrowserManual({
          job_id: openAIBrowserJob.job_id,
          token: openAIManualSecret.trim(),
        });
      } else {
        await addOpenAICodexToken({ token: openAIManualSecret.trim() });
      }
      setOpenAIManualSecret("");
      setOpenAIBrowserJob(null);
      const ok = await syncProfiles(false);
      if (ok) {
        setStatus({ tone: "success", message: "OpenAI Codex credential 已完成寫入" });
      }
    } catch {
      setStatus({ tone: "error", message: "OpenAI Codex manual fallback 失敗" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleCancelOpenAIBrowserLogin() {
    if (!openAIBrowserJob?.job_id) {
      setOpenAIBrowserJob(null);
      return;
    }

    setBusyAction("openai-browser-cancel");
    try {
      await cancelOpenAICodexBrowserLogin(openAIBrowserJob.job_id);
      setOpenAIBrowserJob(null);
      setOpenAIManualSecret("");
      setStatus({ tone: "info", message: "OpenAI Codex Browser Login 已取消" });
    } catch (error) {
      if (isTerminalJobError(error)) {
        setOpenAIBrowserJob(null);
        setOpenAIManualSecret("");
        setStatus({
          tone: "info",
          message: "OpenAI Codex Browser Login 已結束，已清除本地狀態",
        });
      } else {
        setStatus({
          tone: "error",
          message: "取消 OpenAI Codex Browser Login 失敗",
        });
      }
    } finally {
      setBusyAction("");
    }
  }

  if (loading) {
    return <LoadingPanel />;
  }

  const geminiNav = geminiNavProfiles(geminiProfiles);
  const aiStudioNav = aiStudioNavProfiles(aiStudioProfiles);
  const anthropicNav = anthropicNavProfiles(anthropicProfiles);
  const openAINav = openAINavProfiles(combinedOpenAIProfiles);
  const configuredProviders = [
    geminiProfiles,
    aiStudioProfiles,
    anthropicProfiles,
    combinedOpenAIProfiles,
  ].filter((profiles) => profiles.length > 0).length;
  const totalProfiles =
    geminiProfiles.length +
    aiStudioProfiles.length +
    anthropicProfiles.length +
    combinedOpenAIProfiles.length;
  const runningJobs =
    Number(anthropicJob?.status === "running") +
    Number(openAIBrowserJob?.status === "running");
  const manualFlows =
    Number(!!geminiFlow) +
    Number(jobNeedsManualIntervention(anthropicJob)) +
    Number(jobNeedsManualIntervention(openAIBrowserJob));

  const navCards = [
    {
      group: "gemini" as const,
      title: "Gemini OAuth",
      description: "OAuth profiles、project 狀態與 callback/manual fallback 管理。",
      profiles: geminiNav,
      selectedKey: selectedGemini ? geminiProfileKey(selectedGemini) : "",
      selectedLabel: selectedProviderTitle("gemini", selectedGemini),
      stateBadge:
        geminiProfiles.length > 0
          ? { label: "Configured", className: "badge-primary" }
          : { label: "Setup required", className: "badge-ghost" },
    },
    {
      group: "aiStudio" as const,
      title: "AI Studio Keys",
      description: "管理 google-ai-studio API keys，快速切換可用 profile。",
      profiles: aiStudioNav,
      selectedKey: selectedAIStudio ? aiStudioProfileKey(selectedAIStudio) : "",
      selectedLabel: selectedProviderTitle("aiStudio", selectedAIStudio),
      stateBadge:
        aiStudioProfiles.length > 0
          ? { label: "Configured", className: "badge-primary" }
          : { label: "Setup required", className: "badge-ghost" },
    },
    {
      group: "anthropic" as const,
      title: "Anthropic",
      description: "setup-token、API key 與 browser login 的完整管理。",
      profiles: anthropicNav,
      selectedKey: selectedAnthropic ? anthropicProfileKey(selectedAnthropic) : "",
      selectedLabel: selectedProviderTitle("anthropic", selectedAnthropic),
      stateBadge:
        anthropicJob?.status === "running"
          ? { label: "Job running", className: "badge-info" }
          : anthropicProfiles.length > 0
            ? { label: "Configured", className: "badge-primary" }
            : { label: "Setup required", className: "badge-ghost" },
    },
    {
      group: "openAI" as const,
      title: "OpenAI / Codex",
      description: "openai 與 openai-codex 合併檢視，保留 provider 差異。",
      profiles: openAINav,
      selectedKey: selectedOpenAI ? openAIProfileKey(selectedOpenAI) : "",
      selectedLabel: selectedProviderTitle("openAI", selectedOpenAI),
      stateBadge:
        openAIBrowserJob?.status === "running"
          ? { label: "Job running", className: "badge-info" }
          : combinedOpenAIProfiles.length > 0
            ? { label: "Configured", className: "badge-primary" }
            : { label: "Setup required", className: "badge-ghost" },
    },
  ];

  return (
    <div className="space-y-6">
      <div className="card border border-base-300 bg-base-200 shadow-sm">
        <div className="card-body gap-4">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
            <div className="min-w-0 flex-1 space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                <div className="badge badge-outline badge-sm">Auth</div>
                <div className="badge badge-info badge-sm">Profiles</div>
                {manualFlows > 0 && (
                  <div className="badge badge-warning badge-sm">
                    {manualFlows} 個流程待處理
                  </div>
                )}
              </div>
              <div>
                <h2 className="card-title text-2xl">認證管理</h2>
                <p className="text-sm text-base-content/60">
                  使用與 Persona 設定相同的管理語言，集中管理 Gemini、AI
                  Studio、Anthropic、OpenAI 與 OpenAI Codex 認證。
                </p>
              </div>
            </div>

            <div className="w-full shrink-0 sm:w-auto">
              <div className="join join-vertical w-full sm:join-horizontal">
                <button
                  className="btn btn-sm join-item"
                  onClick={handleRefresh}
                  disabled={busyAction !== ""}
                >
                  {busyAction === "reload" && (
                    <span className="loading loading-spinner loading-xs" />
                  )}
                  重新整理
                </button>
              </div>
            </div>
          </div>

          <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
            <div className="stat">
              <div className="stat-title">Configured Providers</div>
              <div className="stat-value text-lg">{configuredProviders}</div>
              <div className="stat-desc">4 組認證來源中已設定的 provider 數量</div>
            </div>
            <div className="stat">
              <div className="stat-title">Profiles</div>
              <div className="stat-value text-lg">{totalProfiles}</div>
              <div className="stat-desc">所有已載入的 credential / OAuth profiles</div>
            </div>
            <div className="stat">
              <div className="stat-title">Browser Jobs</div>
              <div className="stat-value text-lg">{runningJobs}</div>
              <div className="stat-desc">Anthropic / OpenAI Codex 進行中的 browser login</div>
            </div>
            <div className="stat">
              <div className="stat-title">Manual Action</div>
              <div className="stat-value text-lg">{manualFlows}</div>
              <div className="stat-desc">需要貼上 callback、setup-token 或 OAuth token 的流程</div>
            </div>
          </div>
        </div>
      </div>

      <div className="grid gap-6 xl:grid-cols-[minmax(320px,0.95fr)_minmax(0,1.65fr)]">
        <div className="min-w-0 space-y-4">
          {navCards.map((card) => (
            <ProviderNavCard
              key={card.group}
              title={card.title}
              description={card.description}
              active={selectedGroup === card.group}
              profileCount={card.profiles.length}
              selectedLabel={card.selectedLabel}
              stateBadge={card.stateBadge}
              profiles={card.profiles}
              selectedKey={card.selectedKey}
              onActivate={() => selectGroup(card.group)}
              onSelectProfile={(key) => selectProfile(card.group, key)}
            />
          ))}
        </div>

        <div className="min-w-0 space-y-6">
          {selectedGroup === "gemini" && (
            <>
              <div className="card border border-base-300 bg-base-200 shadow-sm">
                <div className="card-body gap-4">
                  <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
                    <div className="min-w-0 flex-1 space-y-2">
                      <div className="flex flex-wrap items-center gap-2">
                        <div className="badge badge-outline badge-sm">Gemini OAuth</div>
                        {selectedGemini?.preferred && (
                          <div className="badge badge-primary badge-sm">
                            Preferred
                          </div>
                        )}
                        {selectedGemini && (
                          <div
                            className={`badge badge-sm ${
                              selectedGemini.available
                                ? "badge-success"
                                : "badge-ghost"
                            }`}
                          >
                            {selectedGemini.available ? "Available" : "Unavailable"}
                          </div>
                        )}
                        {geminiFlow && (
                          <div className="badge badge-info badge-sm">OAuth pending</div>
                        )}
                      </div>
                      <div>
                        <h2 className="card-title text-2xl">
                          {selectedProviderTitle("gemini", selectedGemini)}
                        </h2>
                        <p className="text-sm text-base-content/60">
                          管理 `google-gemini-cli` 的 OAuth profiles，包含 project
                          onboarding、callback/manual fallback 與 runtime profile 切換。
                        </p>
                      </div>
                    </div>

                    <div className="max-w-full overflow-x-auto lg:shrink-0">
                      <div className="join join-vertical w-full sm:w-max sm:join-horizontal">
                        <button
                          className="btn btn-sm join-item"
                          onClick={handleStartGeminiOAuth}
                          disabled={busyAction !== ""}
                        >
                          {busyAction === "gemini-start" && (
                            <span className="loading loading-spinner loading-xs" />
                          )}
                          開始 OAuth
                        </button>
                        <button
                          className="btn btn-ghost btn-sm join-item"
                          onClick={handleRefresh}
                          disabled={busyAction !== ""}
                        >
                          重新整理
                        </button>
                        <button
                          className="btn btn-primary btn-sm join-item"
                          onClick={handleUseGeminiProfile}
                          disabled={!selectedGemini || busyAction !== ""}
                        >
                          {busyAction === "gemini-use" && (
                            <span className="loading loading-spinner loading-xs" />
                          )}
                          使用此 profile
                        </button>
                      </div>
                    </div>
                  </div>

                  <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
                    <div className="stat">
                      <div className="stat-title">Profiles</div>
                      <div className="stat-value text-lg">{geminiProfiles.length}</div>
                      <div className="stat-desc">Gemini OAuth credentials</div>
                    </div>
                    <div className="stat">
                      <div className="stat-title">Available</div>
                      <div className="stat-value text-lg">
                        {geminiProfiles.filter((profile) => profile.available).length}
                      </div>
                      <div className="stat-desc">目前可直接選用的 profiles</div>
                    </div>
                    <div className="stat">
                      <div className="stat-title">Project</div>
                      <div className="stat-value text-lg">
                        {selectedGemini?.project_ready ? "Ready" : "Check"}
                      </div>
                      <div className="stat-desc">
                        {fallback(selectedGemini?.project_id)}
                      </div>
                    </div>
                    <div className="stat">
                      <div className="stat-title">Flow</div>
                      <div className="stat-value text-lg">
                        {geminiFlow ? "Pending" : "Idle"}
                      </div>
                      <div className="stat-desc">
                        {geminiFlow?.mode ? `mode=${geminiFlow.mode}` : "尚未啟動 OAuth"}
                      </div>
                    </div>
                  </div>
                </div>
              </div>

              <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(320px,0.92fr)]">
                <div className="card border border-base-300 bg-base-200 shadow-sm">
                  <div className="card-body gap-4">
                    <div className="flex items-center justify-between gap-3">
                      <h3 className="card-title text-lg">Profile 詳情</h3>
                      <div className="badge badge-outline badge-sm">
                        {selectedGemini ? "Selected" : "No profile"}
                      </div>
                    </div>

                    {selectedGemini ? (
                      <>
                        <ul className="list rounded-box border border-base-300 bg-base-100">
                          <li className="list-row">
                            <div>
                              <div className="text-xs uppercase tracking-wide text-base-content/50">
                                Profile ID
                              </div>
                              <div className="break-all font-mono text-sm">
                                {selectedGemini.profile_id}
                              </div>
                            </div>
                          </li>
                          <li className="list-row">
                            <div>
                              <div className="text-xs uppercase tracking-wide text-base-content/50">
                                Email
                              </div>
                              <div className="text-sm">{fallback(selectedGemini.email)}</div>
                            </div>
                          </li>
                          <li className="list-row">
                            <div>
                              <div className="text-xs uppercase tracking-wide text-base-content/50">
                                Project ID
                              </div>
                              <div className="break-all font-mono text-sm">
                                {fallback(selectedGemini.project_id)}
                              </div>
                            </div>
                          </li>
                          <li className="list-row">
                            <div>
                              <div className="text-xs uppercase tracking-wide text-base-content/50">
                                Endpoint
                              </div>
                              <div className="break-all font-mono text-sm">
                                {fallback(selectedGemini.endpoint)}
                              </div>
                            </div>
                          </li>
                          <li className="list-row">
                            <div className="flex w-full flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                              <div className="min-w-0">
                                <div className="text-xs uppercase tracking-wide text-base-content/50">
                                  Execution Mode
                                </div>
                                <div className="text-sm">
                                  {geminiExecutionModeLabel(selectedGemini.execution_mode)}
                                </div>
                                <div className="text-xs text-base-content/50">
                                  開啟後，主聊天且未啟用工具時會改走 `gemini -p`
                                  headless subprocess。
                                </div>
                              </div>
                              <label className="flex items-center gap-3 self-start lg:self-center">
                                <span className="text-xs font-medium text-base-content/60">
                                  CLI Headless
                                </span>
                                <input
                                  type="checkbox"
                                  className="toggle toggle-primary"
                                  checked={
                                    selectedGemini.execution_mode === "cli_headless"
                                  }
                                  disabled={busyAction !== ""}
                                  onChange={(event) => {
                                    void handleSetGeminiExecutionMode(
                                      event.target.checked
                                        ? "cli_headless"
                                        : "internal_api",
                                    );
                                  }}
                                />
                              </label>
                            </div>
                          </li>
                          <li className="list-row">
                            <div>
                              <div className="text-xs uppercase tracking-wide text-base-content/50">
                                Updated
                              </div>
                              <div className="text-sm">
                                {formatLongDateTime(selectedGemini.updated_at)}
                              </div>
                            </div>
                          </li>
                        </ul>

                        {!selectedGemini.project_ready && (
                          <div className="alert alert-warning">
                            <span>
                              這個 profile 的 project 尚未準備完成。請重新執行 Gemini
                              OAuth，並確認 gemini CLI provisioning 已完成。
                            </span>
                          </div>
                        )}

                        {!!selectedGemini.unavailable_reason?.trim() && (
                          <div className="alert alert-info">
                            <span>
                              unavailable_reason: {selectedGemini.unavailable_reason}
                            </span>
                          </div>
                        )}

                        {!!selectedGemini.disabled_reason?.trim() && (
                          <div className="alert alert-warning">
                            <span>
                              disabled_reason: {selectedGemini.disabled_reason}
                            </span>
                          </div>
                        )}
                      </>
                    ) : (
                      <div className="alert alert-info">
                        <span>尚無 Gemini OAuth profile。點擊「開始 OAuth」建立授權流程。</span>
                      </div>
                    )}
                  </div>
                </div>

                <div className="space-y-6">
                  <div className="card border border-base-300 bg-base-200 shadow-sm">
                    <div className="card-body gap-4">
                      <div className="flex items-center justify-between gap-3">
                        <h3 className="card-title text-lg">OAuth 流程</h3>
                        <div
                          className={`badge badge-sm ${
                            geminiFlow ? "badge-info" : "badge-ghost"
                          }`}
                        >
                          {geminiFlow ? "Pending" : "Idle"}
                        </div>
                      </div>

                      {geminiFlow ? (
                        <>
                          <label className="fieldset">
                            <legend className="fieldset-legend">授權網址</legend>
                            <textarea
                              className="textarea textarea-bordered h-32 w-full font-mono text-xs"
                              readOnly
                              value={geminiFlow.auth_url}
                            />
                          </label>

                          <ul className="list rounded-box border border-base-300 bg-base-100">
                            <li className="list-row">
                              <div>
                                <div className="text-xs uppercase tracking-wide text-base-content/50">
                                  Mode
                                </div>
                                <div className="font-medium">{geminiFlow.mode}</div>
                              </div>
                            </li>
                            <li className="list-row">
                              <div>
                                <div className="text-xs uppercase tracking-wide text-base-content/50">
                                  OAuth Mode
                                </div>
                                <div className="font-medium">
                                  {fallback(geminiFlow.oauth_mode)}
                                </div>
                              </div>
                            </li>
                            <li className="list-row">
                              <div>
                                <div className="text-xs uppercase tracking-wide text-base-content/50">
                                  Redirect URI
                                </div>
                                <div className="font-mono text-xs break-all">
                                  {geminiFlow.redirect_uri}
                                </div>
                              </div>
                            </li>
                            <li className="list-row">
                              <div>
                                <div className="text-xs uppercase tracking-wide text-base-content/50">
                                  Expires
                                </div>
                                <div className="text-sm">
                                  {formatLongDateTime(geminiFlow.expires_at)}
                                </div>
                              </div>
                            </li>
                          </ul>

                          <div className="join join-vertical w-full sm:join-horizontal">
                            <button
                              className="btn btn-sm join-item"
                              onClick={() =>
                                window.open(
                                  geminiFlow.auth_url,
                                  "_blank",
                                  "noopener,noreferrer",
                                )
                              }
                            >
                              開啟授權頁面
                            </button>
                            <button
                              className="btn btn-ghost btn-sm join-item"
                              onClick={() => {
                                setGeminiFlow(null);
                                setGeminiManualInput("");
                              }}
                              disabled={busyAction !== ""}
                            >
                              清除流程
                            </button>
                          </div>

                          <label className="fieldset">
                            <legend className="fieldset-legend">
                              Callback URL 或 Code
                            </legend>
                            <textarea
                              className="textarea textarea-bordered h-24 w-full"
                              placeholder={geminiCallbackExample(geminiFlow.redirect_uri)}
                              value={geminiManualInput}
                              onChange={(event) =>
                                setGeminiManualInput(event.target.value)
                              }
                              disabled={busyAction !== ""}
                            />
                            <p className="label text-base-content/55">
                              <>
                                Gemini CLI OAuth 目前只接受{" "}
                                <code>127.0.0.1</code> loopback callback；遠端站點或
                                popup 被阻擋時，請把最後的 callback URL 或授權 code
                                貼回這裡。
                              </>
                            </p>
                          </label>

                          <button
                            className="btn btn-primary"
                            onClick={handleCompleteGeminiOAuth}
                            disabled={!geminiManualInput.trim() || busyAction !== ""}
                          >
                            {busyAction === "gemini-complete" && (
                              <span className="loading loading-spinner loading-xs" />
                            )}
                            完成 Gemini OAuth
                          </button>
                        </>
                      ) : (
                        <div className="space-y-3">
                          <div className="alert alert-info">
                            <span>
                              點擊「開始 OAuth」後會開啟 Gemini 授權頁。Gemini CLI OAuth
                              只接受 <code>127.0.0.1</code> loopback callback；若從公網站點發起或無法自動回呼，請把最後的 callback URL 或 code 貼回這裡。
                            </span>
                          </div>
                          <ul className="list rounded-box border border-base-300 bg-base-100">
                            <li className="list-row">
                              <div>
                                <div className="font-medium">Loopback callback</div>
                                <div className="text-sm text-base-content/65">
                                  預設使用 <code>http://127.0.0.1:8085/oauth2callback</code>，本機啟動時可直接完成回呼。
                                </div>
                              </div>
                            </li>
                            <li className="list-row">
                              <div>
                                <div className="font-medium">Manual fallback</div>
                                <div className="text-sm text-base-content/65">
                                  遠端站點或 popup 被阻擋時，請手動貼上最後的 <code>127.0.0.1</code> callback URL 或授權 code。
                                </div>
                              </div>
                            </li>
                          </ul>
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              </div>
            </>
          )}

          {selectedGroup === "aiStudio" && (
            <>
              <div className="card border border-base-300 bg-base-200 shadow-sm">
                <div className="card-body gap-4">
                  <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
                    <div className="min-w-0 flex-1 space-y-2">
                      <div className="flex flex-wrap items-center gap-2">
                        <div className="badge badge-outline badge-sm">AI Studio</div>
                        {selectedAIStudio?.preferred && (
                          <div className="badge badge-primary badge-sm">
                            Preferred
                          </div>
                        )}
                        {selectedAIStudio && (
                          <div
                            className={`badge badge-sm ${
                              selectedAIStudio.available
                                ? "badge-success"
                                : "badge-ghost"
                            }`}
                          >
                            {selectedAIStudio.available ? "Available" : "Unavailable"}
                          </div>
                        )}
                      </div>
                      <div>
                        <h2 className="card-title text-2xl">
                          {selectedProviderTitle("aiStudio", selectedAIStudio)}
                        </h2>
                        <p className="text-sm text-base-content/60">
                          管理 `google-ai-studio` API keys，儲存後即可在 Provider 頁切換模型。
                        </p>
                      </div>
                    </div>

                    <div className="max-w-full overflow-x-auto lg:shrink-0">
                      <div className="join join-vertical w-full sm:w-max sm:join-horizontal">
                        <button
                          className="btn btn-sm join-item"
                          onClick={handleRefresh}
                          disabled={busyAction !== ""}
                        >
                          重新整理
                        </button>
                        <button
                          className="btn btn-ghost btn-sm join-item"
                          onClick={handleDeleteAIStudioProfile}
                          disabled={!selectedAIStudio || busyAction !== ""}
                        >
                          {busyAction === "ai-studio-delete" && (
                            <span className="loading loading-spinner loading-xs" />
                          )}
                          刪除
                        </button>
                        <button
                          className="btn btn-primary btn-sm join-item"
                          onClick={handleUseAIStudioProfile}
                          disabled={!selectedAIStudio || busyAction !== ""}
                        >
                          {busyAction === "ai-studio-use" && (
                            <span className="loading loading-spinner loading-xs" />
                          )}
                          使用此 profile
                        </button>
                      </div>
                    </div>
                  </div>

                  <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
                    <div className="stat">
                      <div className="stat-title">Keys</div>
                      <div className="stat-value text-lg">{aiStudioProfiles.length}</div>
                      <div className="stat-desc">AI Studio profiles</div>
                    </div>
                    <div className="stat">
                      <div className="stat-title">Available</div>
                      <div className="stat-value text-lg">
                        {aiStudioProfiles.filter((profile) => profile.available).length}
                      </div>
                      <div className="stat-desc">目前可直接使用的 keys</div>
                    </div>
                    <div className="stat">
                      <div className="stat-title">Selected</div>
                      <div className="stat-value text-lg">
                        {selectedAIStudio ? "Ready" : "None"}
                      </div>
                      <div className="stat-desc">{fallback(selectedAIStudio?.key_hint)}</div>
                    </div>
                  </div>
                </div>
              </div>

              <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(320px,0.92fr)]">
                <div className="card border border-base-300 bg-base-200 shadow-sm">
                  <form
                    className="card-body gap-4"
                    onSubmit={(event) => {
                      event.preventDefault();
                      void handleAddAIStudioKey();
                    }}
                  >
                    <div className="flex items-center justify-between gap-3">
                      <h3 className="card-title text-lg">新增 API key</h3>
                      <div className="badge badge-outline badge-sm">google-ai-studio</div>
                    </div>

                    <SecretField
                      label="API Key"
                      placeholder="AIza..."
                      value={aiStudioDraft.secret}
                      onChange={(value) =>
                        setAIStudioDraft((current) => ({ ...current, secret: value }))
                      }
                      disabled={busyAction !== ""}
                      hint="API key 會儲存在 auth store，Web 端只負責送出新增請求。"
                    />

                    <label className="fieldset">
                      <legend className="fieldset-legend">Display Name</legend>
                      <input
                        type="text"
                        className="input input-bordered w-full"
                        placeholder="main"
                        value={aiStudioDraft.displayName}
                        onChange={(event) =>
                          setAIStudioDraft((current) => ({
                            ...current,
                            displayName: event.target.value,
                          }))
                        }
                        disabled={busyAction !== ""}
                      />
                    </label>

                    <div className="card-actions justify-end">
                      <button
                        type="button"
                        className="btn"
                        onClick={() =>
                          setAIStudioDraft({ secret: "", displayName: "" })
                        }
                        disabled={busyAction !== ""}
                      >
                        清空
                      </button>
                      <button
                        type="submit"
                        className="btn btn-primary"
                        disabled={!aiStudioDraft.secret.trim() || busyAction !== ""}
                      >
                        {busyAction === "ai-studio-add" && (
                          <span className="loading loading-spinner loading-xs" />
                        )}
                        新增 AI Studio key
                      </button>
                    </div>
                  </form>
                </div>

                <div className="card border border-base-300 bg-base-200 shadow-sm">
                  <div className="card-body gap-4">
                    <div className="flex items-center justify-between gap-3">
                      <h3 className="card-title text-lg">Selected Profile</h3>
                      <div className="badge badge-ghost badge-sm">
                        {selectedAIStudio ? "Loaded" : "Empty"}
                      </div>
                    </div>

                    {selectedAIStudio ? (
                      <ul className="list rounded-box border border-base-300 bg-base-100">
                        <li className="list-row">
                          <div>
                            <div className="text-xs uppercase tracking-wide text-base-content/50">
                              Profile ID
                            </div>
                            <div className="break-all font-mono text-sm">
                              {selectedAIStudio.profile_id}
                            </div>
                          </div>
                        </li>
                        <li className="list-row">
                          <div>
                            <div className="text-xs uppercase tracking-wide text-base-content/50">
                              Display Name
                            </div>
                            <div className="text-sm">
                              {fallback(selectedAIStudio.display_name)}
                            </div>
                          </div>
                        </li>
                        <li className="list-row">
                          <div>
                            <div className="text-xs uppercase tracking-wide text-base-content/50">
                              Key Hint
                            </div>
                            <div className="break-all font-mono text-sm">
                              {fallback(selectedAIStudio.key_hint)}
                            </div>
                          </div>
                        </li>
                        <li className="list-row">
                          <div>
                            <div className="text-xs uppercase tracking-wide text-base-content/50">
                              Updated
                            </div>
                            <div className="text-sm">
                              {formatLongDateTime(selectedAIStudio.updated_at)}
                            </div>
                          </div>
                        </li>
                      </ul>
                    ) : (
                      <div className="alert alert-info">
                        <span>尚無 AI Studio key。使用左側表單新增第一組憑證。</span>
                      </div>
                    )}
                  </div>
                </div>
              </div>
            </>
          )}

          {selectedGroup === "anthropic" && (
            <>
              <div className="card border border-base-300 bg-base-200 shadow-sm">
                <div className="card-body gap-4">
                  <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
                    <div className="min-w-0 flex-1 space-y-2">
                      <div className="flex flex-wrap items-center gap-2">
                        <div className="badge badge-outline badge-sm">Anthropic</div>
                        {selectedAnthropic?.preferred && (
                          <div className="badge badge-primary badge-sm">
                            Preferred
                          </div>
                        )}
                        {selectedAnthropic && (
                          <div
                            className={`badge badge-sm ${
                              selectedAnthropic.available
                                ? "badge-success"
                                : "badge-ghost"
                            }`}
                          >
                            {selectedAnthropic.available
                              ? "Available"
                              : "Unavailable"}
                          </div>
                        )}
                        {anthropicJob && (
                          <div className="badge badge-info badge-sm">
                            {anthropicJob.status}
                          </div>
                        )}
                      </div>
                      <div>
                        <h2 className="card-title text-2xl">
                          {selectedProviderTitle("anthropic", selectedAnthropic)}
                        </h2>
                        <p className="text-sm text-base-content/60">
                          Claude setup-token、API key 與 browser login
                          在同一頁管理，保留 job timeline 與 manual fallback。
                        </p>
                      </div>
                    </div>

                    <div className="max-w-full overflow-x-auto lg:shrink-0">
                      <div className="join join-vertical w-full sm:w-max sm:join-horizontal">
                        <button
                          className="btn btn-sm join-item"
                          onClick={handleRefresh}
                          disabled={busyAction !== ""}
                        >
                          重新整理
                        </button>
                        <button
                          className="btn btn-ghost btn-sm join-item"
                          onClick={handleDeleteAnthropicProfile}
                          disabled={!selectedAnthropic || busyAction !== ""}
                        >
                          {busyAction === "anthropic-delete" && (
                            <span className="loading loading-spinner loading-xs" />
                          )}
                          刪除
                        </button>
                        <button
                          className="btn btn-primary btn-sm join-item"
                          onClick={handleUseAnthropicProfile}
                          disabled={!selectedAnthropic || busyAction !== ""}
                        >
                          {busyAction === "anthropic-use" && (
                            <span className="loading loading-spinner loading-xs" />
                          )}
                          使用此 profile
                        </button>
                      </div>
                    </div>
                  </div>

                  <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
                    <div className="stat">
                      <div className="stat-title">Credentials</div>
                      <div className="stat-value text-lg">
                        {anthropicProfiles.length}
                      </div>
                      <div className="stat-desc">Anthropic profiles</div>
                    </div>
                    <div className="stat">
                      <div className="stat-title">Available</div>
                      <div className="stat-value text-lg">
                        {anthropicProfiles.filter((profile) => profile.available).length}
                      </div>
                      <div className="stat-desc">目前可直接使用的 credentials</div>
                    </div>
                    <div className="stat">
                      <div className="stat-title">Browser Job</div>
                      <div className="stat-value text-lg">
                        {anthropicJob ? anthropicJob.status : "Idle"}
                      </div>
                      <div className="stat-desc">
                        {anthropicJob?.job_id || "尚未啟動 browser login"}
                      </div>
                    </div>
                  </div>
                </div>
              </div>

              <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(340px,0.95fr)]">
                <div className="space-y-6">
                  <div className="card border border-base-300 bg-base-200 shadow-sm">
                    <form
                      className="card-body gap-4"
                      onSubmit={(event) => {
                        event.preventDefault();
                        void handleAddAnthropicToken();
                      }}
                    >
                      <div className="flex items-center justify-between gap-3">
                        <h3 className="card-title text-lg">新增 setup-token</h3>
                        <div className="badge badge-outline badge-sm">token</div>
                      </div>

                      <SecretField
                        label="Setup Token"
                        placeholder="sk-ant-oat01-..."
                        value={anthropicTokenDraft.secret}
                        onChange={(value) =>
                          setAnthropicTokenDraft((current) => ({
                            ...current,
                            secret: value,
                          }))
                        }
                        disabled={busyAction !== ""}
                      />

                      <label className="fieldset">
                        <legend className="fieldset-legend">Display Name</legend>
                        <input
                          type="text"
                          className="input input-bordered w-full"
                          placeholder="token-main"
                          value={anthropicTokenDraft.displayName}
                          onChange={(event) =>
                            setAnthropicTokenDraft((current) => ({
                              ...current,
                              displayName: event.target.value,
                            }))
                          }
                          disabled={busyAction !== ""}
                        />
                      </label>

                      <div className="card-actions justify-end">
                        <button
                          type="button"
                          className="btn"
                          onClick={() =>
                            setAnthropicTokenDraft({ secret: "", displayName: "" })
                          }
                          disabled={busyAction !== ""}
                        >
                          清空
                        </button>
                        <button
                          type="submit"
                          className="btn btn-primary"
                          disabled={
                            !anthropicTokenDraft.secret.trim() || busyAction !== ""
                          }
                        >
                          {busyAction === "anthropic-token" && (
                            <span className="loading loading-spinner loading-xs" />
                          )}
                          新增 setup-token
                        </button>
                      </div>
                    </form>
                  </div>

                  <div className="card border border-base-300 bg-base-200 shadow-sm">
                    <form
                      className="card-body gap-4"
                      onSubmit={(event) => {
                        event.preventDefault();
                        void handleAddAnthropicAPIKey();
                      }}
                    >
                      <div className="flex items-center justify-between gap-3">
                        <h3 className="card-title text-lg">新增 API key</h3>
                        <div className="badge badge-outline badge-sm">api_key</div>
                      </div>

                      <SecretField
                        label="API Key"
                        placeholder="sk-ant-api03-..."
                        value={anthropicKeyDraft.secret}
                        onChange={(value) =>
                          setAnthropicKeyDraft((current) => ({
                            ...current,
                            secret: value,
                          }))
                        }
                        disabled={busyAction !== ""}
                      />

                      <label className="fieldset">
                        <legend className="fieldset-legend">Display Name</legend>
                        <input
                          type="text"
                          className="input input-bordered w-full"
                          placeholder="api-main"
                          value={anthropicKeyDraft.displayName}
                          onChange={(event) =>
                            setAnthropicKeyDraft((current) => ({
                              ...current,
                              displayName: event.target.value,
                            }))
                          }
                          disabled={busyAction !== ""}
                        />
                      </label>

                      <div className="card-actions justify-end">
                        <button
                          type="button"
                          className="btn"
                          onClick={() =>
                            setAnthropicKeyDraft({ secret: "", displayName: "" })
                          }
                          disabled={busyAction !== ""}
                        >
                          清空
                        </button>
                        <button
                          type="submit"
                          className="btn btn-primary"
                          disabled={!anthropicKeyDraft.secret.trim() || busyAction !== ""}
                        >
                          {busyAction === "anthropic-key" && (
                            <span className="loading loading-spinner loading-xs" />
                          )}
                          新增 API key
                        </button>
                      </div>
                    </form>
                  </div>
                </div>

                <div className="space-y-6">
                  <div className="card border border-base-300 bg-base-200 shadow-sm">
                    <div className="card-body gap-4">
                      <div className="flex items-center justify-between gap-3">
                        <h3 className="card-title text-lg">Selected Profile</h3>
                        <div className="badge badge-ghost badge-sm">
                          {selectedAnthropic ? "Loaded" : "Empty"}
                        </div>
                      </div>

                      {selectedAnthropic ? (
                        <>
                          <ul className="list rounded-box border border-base-300 bg-base-100">
                            <li className="list-row">
                              <div>
                                <div className="text-xs uppercase tracking-wide text-base-content/50">
                                  Profile ID
                                </div>
                                <div className="break-all font-mono text-sm">
                                  {selectedAnthropic.profile_id}
                                </div>
                              </div>
                            </li>
                            <li className="list-row">
                              <div>
                                <div className="text-xs uppercase tracking-wide text-base-content/50">
                                  Type
                                </div>
                                <div className="text-sm">
                                  {fallback(selectedAnthropic.type)}
                                </div>
                              </div>
                            </li>
                            <li className="list-row">
                              <div>
                                <div className="text-xs uppercase tracking-wide text-base-content/50">
                                  Key Hint
                                </div>
                                <div className="break-all font-mono text-sm">
                                  {fallback(selectedAnthropic.key_hint)}
                                </div>
                              </div>
                            </li>
                            <li className="list-row">
                              <div>
                                <div className="text-xs uppercase tracking-wide text-base-content/50">
                                  Updated
                                </div>
                                <div className="text-sm">
                                  {formatLongDateTime(selectedAnthropic.updated_at)}
                                </div>
                              </div>
                            </li>
                          </ul>

                          {!!selectedAnthropic.disabled_reason?.trim() && (
                            <div className="alert alert-warning">
                              <span>
                                disabled_reason: {selectedAnthropic.disabled_reason}
                              </span>
                            </div>
                          )}
                        </>
                      ) : (
                        <div className="alert alert-info">
                          <span>尚無 Anthropic credential。可新增 token、API key 或使用 browser login。</span>
                        </div>
                      )}
                    </div>
                  </div>

                  <div className="card border border-base-300 bg-base-200 shadow-sm">
                    <div className="card-body gap-4">
                      <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
                        <div className="min-w-0 flex-1">
                          <h3 className="card-title text-lg">Browser Login</h3>
                          <p className="text-sm text-base-content/60">
                            啟動外部登入流程並輪詢狀態；若需要 manual fallback，會在此頁顯示 setup-token 輸入區塊。
                          </p>
                        </div>
                        <div className="max-w-full overflow-x-auto lg:shrink-0">
                          <div className="join join-vertical w-full sm:w-max sm:join-horizontal">
                            <button
                              className="btn btn-sm join-item"
                              onClick={handleStartAnthropicBrowserLogin}
                              disabled={busyAction !== ""}
                            >
                              {busyAction === "anthropic-browser-start" && (
                                <span className="loading loading-spinner loading-xs" />
                              )}
                              啟動 Browser Login
                            </button>
                            <button
                              className="btn btn-ghost btn-sm join-item"
                              onClick={handleCancelAnthropicBrowserLogin}
                              disabled={!anthropicJob || busyAction !== ""}
                            >
                              {busyAction === "anthropic-browser-cancel" && (
                                <span className="loading loading-spinner loading-xs" />
                              )}
                              取消
                            </button>
                          </div>
                        </div>
                      </div>

                      {anthropicJob ? (
                        <>
                          <ul className="list rounded-box border border-base-300 bg-base-100">
                            <li className="list-row">
                              <div>
                                <div className="text-xs uppercase tracking-wide text-base-content/50">
                                  Job ID
                                </div>
                                <div className="font-mono text-xs break-all">
                                  {anthropicJob.job_id}
                                </div>
                              </div>
                            </li>
                            <li className="list-row">
                              <div>
                                <div className="text-xs uppercase tracking-wide text-base-content/50">
                                  Mode / Status
                                </div>
                                <div className="font-medium">
                                  {anthropicJob.mode} · {anthropicJob.status}
                                </div>
                              </div>
                            </li>
                            <li className="list-row">
                              <div>
                                <div className="text-xs uppercase tracking-wide text-base-content/50">
                                  Expires
                                </div>
                                <div className="text-sm">
                                  {formatLongDateTime(anthropicJob.expires_at)}
                                </div>
                              </div>
                            </li>
                          </ul>

                          {anthropicJob.events && anthropicJob.events.length > 0 && (
                            <div className="space-y-2">
                              <div className="text-sm font-medium">Recent Events</div>
                              <ul className="list rounded-box border border-base-300 bg-base-100">
                                {anthropicJob.events.slice(-5).map((event) => (
                                  <li
                                    key={`${event.at}-${event.message}`}
                                    className="list-row"
                                  >
                                    <div>
                                      <div className="text-xs text-base-content/50">
                                        {formatLongDateTime(event.at)}
                                      </div>
                                      <div className="text-sm">{event.message}</div>
                                    </div>
                                  </li>
                                ))}
                              </ul>
                            </div>
                          )}

                          {(anthropicJob.message?.trim() ||
                            anthropicJob.manual_hint?.trim()) && (
                            <div className="alert alert-info">
                              <span>
                                {anthropicJob.message?.trim() ||
                                  anthropicJob.manual_hint?.trim()}
                              </span>
                            </div>
                          )}

                          {jobNeedsManualIntervention(anthropicJob) && (
                            <div className="space-y-4">
                              <SecretField
                                label="Manual Fallback Setup Token"
                                placeholder="sk-ant-oat01-..."
                                value={anthropicManualSecret}
                                onChange={setAnthropicManualSecret}
                                disabled={busyAction !== ""}
                                hint={
                                  anthropicJob.manual_hint?.trim() ||
                                  "若 browser login 失敗，可直接貼上 setup-token 完成寫入。"
                                }
                              />
                              <button
                                className="btn btn-primary"
                                onClick={handleCompleteAnthropicBrowserManual}
                                disabled={!anthropicManualSecret.trim() || busyAction !== ""}
                              >
                                {busyAction === "anthropic-browser-complete" && (
                                  <span className="loading loading-spinner loading-xs" />
                                )}
                                完成 Manual Fallback
                              </button>
                            </div>
                          )}
                        </>
                      ) : (
                        <div className="alert alert-info">
                          <span>尚未啟動 browser login。需要自動化登入時再開始即可。</span>
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              </div>
            </>
          )}

          {selectedGroup === "openAI" && (
            <>
              <div className="card border border-base-300 bg-base-200 shadow-sm">
                <div className="card-body gap-4">
                  <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
                    <div className="min-w-0 flex-1 space-y-2">
                      <div className="flex flex-wrap items-center gap-2">
                        <div className="badge badge-outline badge-sm shrink-0 whitespace-nowrap">
                          OpenAI / Codex
                        </div>
                        {selectedOpenAI && (
                          <div className="badge badge-accent badge-sm shrink-0 whitespace-nowrap">
                            {selectedOpenAI.provider}
                          </div>
                        )}
                        {selectedOpenAI?.preferred && (
                          <div className="badge badge-primary badge-sm">
                            Preferred
                          </div>
                        )}
                        {selectedOpenAI && (
                          <div
                            className={`badge badge-sm ${
                              selectedOpenAI.available
                                ? "badge-success"
                                : "badge-ghost"
                            }`}
                          >
                            {selectedOpenAI.available ? "Available" : "Unavailable"}
                          </div>
                        )}
                        {openAIBrowserJob && (
                          <div className="badge badge-info badge-sm">
                            {openAIBrowserJob.status}
                          </div>
                        )}
                      </div>
                      <div>
                        <h2 className="card-title text-2xl">
                          {selectedProviderTitle("openAI", selectedOpenAI)}
                        </h2>
                        <p className="text-sm text-base-content/60">
                          合併管理 `openai` 與 `openai-codex` profiles，並保留 provider
                          badge 與對應的 browser login/manual fallback。
                        </p>
                      </div>
                    </div>

                    <div className="max-w-full overflow-x-auto lg:shrink-0">
                      <div className="join join-vertical w-full sm:w-max sm:join-horizontal">
                        <button
                          className="btn btn-sm join-item"
                          onClick={handleRefresh}
                          disabled={busyAction !== ""}
                        >
                          重新整理
                        </button>
                        <button
                          className="btn btn-ghost btn-sm join-item"
                          onClick={handleDeleteOpenAIProfile}
                          disabled={!selectedOpenAI || busyAction !== ""}
                        >
                          {busyAction === "openai-delete" && (
                            <span className="loading loading-spinner loading-xs" />
                          )}
                          刪除
                        </button>
                        <button
                          className="btn btn-primary btn-sm join-item"
                          onClick={handleUseOpenAIProfile}
                          disabled={!selectedOpenAI || busyAction !== ""}
                        >
                          {busyAction === "openai-use" && (
                            <span className="loading loading-spinner loading-xs" />
                          )}
                          使用此 profile
                        </button>
                      </div>
                    </div>
                  </div>

                  <div className="stats stats-vertical border border-base-300 bg-base-100 shadow-sm lg:stats-horizontal">
                    <div className="stat">
                      <div className="stat-title">OpenAI Keys</div>
                      <div className="stat-value text-lg">{openAIProfiles.length}</div>
                      <div className="stat-desc">`openai` provider credentials</div>
                    </div>
                    <div className="stat">
                      <div className="stat-title">Codex Tokens</div>
                      <div className="stat-value text-lg">
                        {openAICodexProfiles.length}
                      </div>
                      <div className="stat-desc">`openai-codex` provider credentials</div>
                    </div>
                    <div className="stat">
                      <div className="stat-title">Browser Job</div>
                      <div className="stat-value text-lg">
                        {openAIBrowserJob ? openAIBrowserJob.status : "Idle"}
                      </div>
                      <div className="stat-desc">
                        {openAIBrowserJob?.job_id || "尚未啟動 Codex browser login"}
                      </div>
                    </div>
                  </div>
                </div>
              </div>

              <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(340px,0.95fr)]">
                <div className="space-y-6">
                  <div className="card border border-base-300 bg-base-200 shadow-sm">
                    <form
                      className="card-body gap-4"
                      onSubmit={(event) => {
                        event.preventDefault();
                        void handleAddOpenAIKey();
                      }}
                    >
                      <div className="flex items-center justify-between gap-3">
                        <h3 className="card-title text-lg">新增 OpenAI API key</h3>
                        <div className="badge badge-outline badge-sm shrink-0 whitespace-nowrap">
                          openai
                        </div>
                      </div>

                      <SecretField
                        label="API Key"
                        placeholder="sk-proj-..."
                        value={openAIKeyDraft.secret}
                        onChange={(value) =>
                          setOpenAIKeyDraft((current) => ({
                            ...current,
                            secret: value,
                          }))
                        }
                        disabled={busyAction !== ""}
                      />

                      <label className="fieldset">
                        <legend className="fieldset-legend">Display Name</legend>
                        <input
                          type="text"
                          className="input input-bordered w-full"
                          placeholder="openai-main"
                          value={openAIKeyDraft.displayName}
                          onChange={(event) =>
                            setOpenAIKeyDraft((current) => ({
                              ...current,
                              displayName: event.target.value,
                            }))
                          }
                          disabled={busyAction !== ""}
                        />
                      </label>

                      <div className="card-actions justify-end">
                        <button
                          type="button"
                          className="btn"
                          onClick={() =>
                            setOpenAIKeyDraft({ secret: "", displayName: "" })
                          }
                          disabled={busyAction !== ""}
                        >
                          清空
                        </button>
                        <button
                          type="submit"
                          className="btn btn-primary"
                          disabled={!openAIKeyDraft.secret.trim() || busyAction !== ""}
                        >
                          {busyAction === "openai-key" && (
                            <span className="loading loading-spinner loading-xs" />
                          )}
                          新增 OpenAI key
                        </button>
                      </div>
                    </form>
                  </div>

                  <div className="card border border-base-300 bg-base-200 shadow-sm">
                    <form
                      className="card-body gap-4"
                      onSubmit={(event) => {
                        event.preventDefault();
                        void handleAddOpenAICodexToken();
                      }}
                    >
                      <div className="flex items-center justify-between gap-3">
                        <h3 className="card-title text-lg">新增 OpenAI Codex token</h3>
                        <div className="badge badge-outline badge-sm shrink-0 whitespace-nowrap">
                          openai-codex
                        </div>
                      </div>

                      <SecretField
                        label="OAuth Token"
                        placeholder="codex-oauth-token"
                        value={openAICodexDraft.secret}
                        onChange={(value) =>
                          setOpenAICodexDraft((current) => ({
                            ...current,
                            secret: value,
                          }))
                        }
                        disabled={busyAction !== ""}
                      />

                      <label className="fieldset">
                        <legend className="fieldset-legend">Display Name</legend>
                        <input
                          type="text"
                          className="input input-bordered w-full"
                          placeholder="codex-main"
                          value={openAICodexDraft.displayName}
                          onChange={(event) =>
                            setOpenAICodexDraft((current) => ({
                              ...current,
                              displayName: event.target.value,
                            }))
                          }
                          disabled={busyAction !== ""}
                        />
                      </label>

                      <div className="card-actions justify-end">
                        <button
                          type="button"
                          className="btn"
                          onClick={() =>
                            setOpenAICodexDraft({ secret: "", displayName: "" })
                          }
                          disabled={busyAction !== ""}
                        >
                          清空
                        </button>
                        <button
                          type="submit"
                          className="btn btn-primary"
                          disabled={!openAICodexDraft.secret.trim() || busyAction !== ""}
                        >
                          {busyAction === "openai-codex-token" && (
                            <span className="loading loading-spinner loading-xs" />
                          )}
                          新增 Codex token
                        </button>
                      </div>
                    </form>
                  </div>
                </div>

                <div className="space-y-6">
                  <div className="card border border-base-300 bg-base-200 shadow-sm">
                    <div className="card-body gap-4">
                      <div className="flex items-center justify-between gap-3">
                        <h3 className="card-title text-lg">Selected Profile</h3>
                        <div className="badge badge-ghost badge-sm">
                          {selectedOpenAI ? "Loaded" : "Empty"}
                        </div>
                      </div>

                      {selectedOpenAI ? (
                        <ul className="list rounded-box border border-base-300 bg-base-100">
                          <li className="list-row">
                            <div>
                              <div className="text-xs uppercase tracking-wide text-base-content/50">
                                Provider
                              </div>
                              <div className="font-medium">
                                {selectedOpenAI.provider}
                              </div>
                            </div>
                          </li>
                          <li className="list-row">
                            <div>
                              <div className="text-xs uppercase tracking-wide text-base-content/50">
                                Profile ID
                              </div>
                              <div className="break-all font-mono text-sm">
                                {selectedOpenAI.profile_id}
                              </div>
                            </div>
                          </li>
                          <li className="list-row">
                            <div>
                              <div className="text-xs uppercase tracking-wide text-base-content/50">
                                Type / Key Hint
                              </div>
                              <div className="text-sm">
                                {fallback(selectedOpenAI.type)} · {fallback(selectedOpenAI.key_hint)}
                              </div>
                            </div>
                          </li>
                          <li className="list-row">
                            <div>
                              <div className="text-xs uppercase tracking-wide text-base-content/50">
                                Updated
                              </div>
                              <div className="text-sm">
                                {formatLongDateTime(selectedOpenAI.updated_at)}
                              </div>
                            </div>
                          </li>
                        </ul>
                      ) : (
                        <div className="alert alert-info">
                          <span>尚無 OpenAI / Codex profile。可新增 API key、OAuth token 或啟動 browser login。</span>
                        </div>
                      )}
                    </div>
                  </div>

                  <div className="card border border-base-300 bg-base-200 shadow-sm">
                    <div className="card-body gap-4">
                      <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
                        <div className="min-w-0 flex-1">
                          <h3 className="card-title text-lg">Codex Browser Login</h3>
                          <p className="text-sm text-base-content/60">
                            啟動 `openai-codex` browser login job，保留 recent events 與 manual fallback token 輸入。
                          </p>
                        </div>
                        <div className="max-w-full overflow-x-auto lg:shrink-0">
                          <div className="join join-vertical w-full sm:w-max sm:join-horizontal">
                            <button
                              className="btn btn-sm join-item"
                              onClick={handleStartOpenAIBrowserLogin}
                              disabled={busyAction !== ""}
                            >
                              {busyAction === "openai-browser-start" && (
                                <span className="loading loading-spinner loading-xs" />
                              )}
                              啟動 Browser Login
                            </button>
                            <button
                              className="btn btn-ghost btn-sm join-item"
                              onClick={handleCancelOpenAIBrowserLogin}
                              disabled={!openAIBrowserJob || busyAction !== ""}
                            >
                              {busyAction === "openai-browser-cancel" && (
                                <span className="loading loading-spinner loading-xs" />
                              )}
                              取消
                            </button>
                          </div>
                        </div>
                      </div>

                      {openAIBrowserJob ? (
                        <>
                          <ul className="list rounded-box border border-base-300 bg-base-100">
                            <li className="list-row">
                              <div>
                                <div className="text-xs uppercase tracking-wide text-base-content/50">
                                  Job ID
                                </div>
                                <div className="font-mono text-xs break-all">
                                  {openAIBrowserJob.job_id}
                                </div>
                              </div>
                            </li>
                            <li className="list-row">
                              <div>
                                <div className="text-xs uppercase tracking-wide text-base-content/50">
                                  Mode / Status
                                </div>
                                <div className="font-medium">
                                  {openAIBrowserJob.mode} · {openAIBrowserJob.status}
                                </div>
                              </div>
                            </li>
                            <li className="list-row">
                              <div>
                                <div className="text-xs uppercase tracking-wide text-base-content/50">
                                  Expires
                                </div>
                                <div className="text-sm">
                                  {formatLongDateTime(openAIBrowserJob.expires_at)}
                                </div>
                              </div>
                            </li>
                          </ul>

                          {openAIBrowserJob.events && openAIBrowserJob.events.length > 0 && (
                            <div className="space-y-2">
                              <div className="text-sm font-medium">Recent Events</div>
                              <ul className="list rounded-box border border-base-300 bg-base-100">
                                {openAIBrowserJob.events.slice(-5).map((event) => (
                                  <li
                                    key={`${event.at}-${event.message}`}
                                    className="list-row"
                                  >
                                    <div>
                                      <div className="text-xs text-base-content/50">
                                        {formatLongDateTime(event.at)}
                                      </div>
                                      <div className="text-sm">{event.message}</div>
                                    </div>
                                  </li>
                                ))}
                              </ul>
                            </div>
                          )}

                          {(openAIBrowserJob.message?.trim() ||
                            openAIBrowserJob.manual_hint?.trim()) && (
                            <div className="alert alert-info">
                              <span>
                                {openAIBrowserJob.message?.trim() ||
                                  openAIBrowserJob.manual_hint?.trim()}
                              </span>
                            </div>
                          )}

                          {jobNeedsManualIntervention(openAIBrowserJob) && (
                            <div className="space-y-4">
                              <SecretField
                                label="Manual Fallback OAuth Token"
                                placeholder="codex-oauth-token"
                                value={openAIManualSecret}
                                onChange={setOpenAIManualSecret}
                                disabled={busyAction !== ""}
                                hint={
                                  openAIBrowserJob.manual_hint?.trim() ||
                                  "若 browser login 失敗，可直接貼上 OpenAI Codex token。"
                                }
                              />
                              <button
                                className="btn btn-primary"
                                onClick={handleCompleteOpenAIBrowserManual}
                                disabled={!openAIManualSecret.trim() || busyAction !== ""}
                              >
                                {busyAction === "openai-browser-complete" && (
                                  <span className="loading loading-spinner loading-xs" />
                                )}
                                完成 Manual Fallback
                              </button>
                            </div>
                          )}
                        </>
                      ) : (
                        <div className="alert alert-info">
                          <span>尚未啟動 Codex browser login。需要登入工作站帳號時再啟動即可。</span>
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              </div>
            </>
          )}
        </div>
      </div>

      {status && (
        <div className="toast toast-end toast-bottom z-50">
          <div className={`alert shadow-lg ${statusClass(status.tone)}`}>
            <span>{status.message}</span>
          </div>
        </div>
      )}
    </div>
  );
}
