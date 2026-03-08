export const FALLBACK_TIMEZONE = "UTC";

export function isValidTimeZone(value: string): boolean {
  const timezone = value.trim();
  if (!timezone) return false;
  try {
    new Intl.DateTimeFormat("en-US", { timeZone: timezone }).format(new Date());
    return true;
  } catch {
    return false;
  }
}

export function normalizeTimeZoneOverride(value?: string | null): string {
  const timezone = value?.trim() ?? "";
  if (!timezone) return "";
  return isValidTimeZone(timezone) ? timezone : "";
}

export function detectBrowserTimeZone(): string {
  if (typeof Intl === "undefined" || typeof Intl.DateTimeFormat !== "function") {
    return FALLBACK_TIMEZONE;
  }
  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone?.trim() ?? "";
  if (!timezone) return FALLBACK_TIMEZONE;
  return isValidTimeZone(timezone) ? timezone : FALLBACK_TIMEZONE;
}

export function resolveEffectiveTimeZone(
  browserTimeZone?: string | null,
  overrideTimeZone?: string | null,
): string {
  const override = normalizeTimeZoneOverride(overrideTimeZone);
  if (override) return override;

  const browser = normalizeTimeZoneOverride(browserTimeZone);
  if (browser) return browser;

  return FALLBACK_TIMEZONE;
}

export function formatRFC3339MinuteInTimeZone(
  date: Date,
  timeZone: string,
): string {
  const effectiveTimeZone = resolveEffectiveTimeZone(FALLBACK_TIMEZONE, timeZone);
  const formatter = new Intl.DateTimeFormat("en-CA", {
    timeZone: effectiveTimeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
    timeZoneName: "longOffset",
  });
  const parts = formatter.formatToParts(date);
  const get = (type: Intl.DateTimeFormatPartTypes) =>
    parts.find((part) => part.type === type)?.value ?? "";

  const offsetValue = get("timeZoneName");
  const offset = parseOffset(offsetValue);
  return `${get("year")}-${get("month")}-${get("day")}T${get("hour")}:${get("minute")}${offset}`;
}

function parseOffset(value: string): string {
  const trimmed = value.trim();
  if (!trimmed || trimmed === "GMT" || trimmed === "UTC") {
    return "Z";
  }

  const match = trimmed.match(/^(?:GMT|UTC)([+-])(\d{1,2})(?::?(\d{2}))?$/);
  if (!match) {
    return "Z";
  }

  const sign = match[1];
  const hour = match[2].padStart(2, "0");
  const minute = (match[3] ?? "00").padStart(2, "0");
  return `${sign}${hour}:${minute}`;
}
