import { useAppStore } from "@/store/appStore";

/**
 * Thinking indicator — uses daisyUI chat-bubble + loading component.
 * Shows active tool name and retry status when available.
 */
export function ThinkingIndicator() {
  const activeToolName = useAppStore((s) => s.activeToolName);
  const retryStatus = useAppStore((s) => s.retryStatus);

  return (
    <div className="chat chat-start">
      <div className="chat-bubble bg-base-200 text-base-content">
        <div className="flex items-center gap-2">
          {/* daisyUI loading dots */}
          <span className="loading loading-dots loading-sm text-primary" />

          {/* Tool status — daisyUI badge */}
          {activeToolName && (
            <span className="badge badge-outline badge-sm gap-1">
              🔧 {activeToolName}
            </span>
          )}
        </div>

        {/* Retry/fallback status — daisyUI alert */}
        {retryStatus && (
          <div className="alert alert-warning alert-sm mt-2 py-1 px-2 text-xs">
            {retryStatus}
          </div>
        )}
      </div>
    </div>
  );
}
