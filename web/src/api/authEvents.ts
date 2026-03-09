export type BrowserAuthReason =
  | "setup_required"
  | "auth_required"
  | "session_expired";

type BrowserAuthEventDetail = {
  reason: BrowserAuthReason;
};

const AUTH_EVENT_NAME = "nekoclaw:browser-auth";

export function dispatchBrowserAuthEvent(reason: BrowserAuthReason) {
  window.dispatchEvent(
    new CustomEvent<BrowserAuthEventDetail>(AUTH_EVENT_NAME, {
      detail: { reason },
    }),
  );
}

export function subscribeBrowserAuthEvent(
  listener: (reason: BrowserAuthReason) => void,
): () => void {
  const handler = (event: Event) => {
    const detail = (event as CustomEvent<BrowserAuthEventDetail>).detail;
    if (!detail?.reason) return;
    listener(detail.reason);
  };
  window.addEventListener(AUTH_EVENT_NAME, handler);
  return () => window.removeEventListener(AUTH_EVENT_NAME, handler);
}
