import { useMemo } from "react";
import type { PlanRecord } from "@/api/types";
import { renderMarkdown } from "@/utils/markdown";

interface Props {
  plan: PlanRecord;
  isStreaming: boolean;
  onApprove: (planID: string) => void;
  onReject: (planID: string) => void;
}

const STATUS_LABELS: Record<PlanRecord["status"], string> = {
  pending_approval: "待核准",
  approved: "已核准",
  rejected: "已拒絕",
  executing: "執行中",
  completed: "已完成",
  failed: "失敗",
  superseded: "已取代",
};

export function PlanCard({ plan, isStreaming, onApprove, onReject }: Props) {
  const html = useMemo(() => renderMarkdown(plan.plan_markdown), [plan.plan_markdown]);
  const pending = plan.status === "pending_approval";

  return (
    <div className="rounded-box border border-base-300 bg-base-100 p-4 shadow-sm">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div>
          <div className="text-xs uppercase tracking-[0.2em] text-base-content/50">Plan</div>
          <div className="font-medium">{plan.plan_summary || plan.request_prompt}</div>
        </div>
        <div className="badge badge-outline">{STATUS_LABELS[plan.status]}</div>
      </div>

      <div
        className="prose prose-sm max-w-none text-inherit prose-headings:text-inherit prose-strong:text-inherit prose-a:text-primary prose-pre:bg-base-200 prose-pre:rounded-box prose-code:bg-base-200 prose-code:rounded-btn prose-code:px-1.5 prose-code:py-0.5 prose-code:text-[0.875em]"
        dangerouslySetInnerHTML={{ __html: html }}
      />

      {plan.last_error && (
        <div className="mt-3 rounded-btn bg-error/10 px-3 py-2 text-sm text-error">
          {plan.last_error}
        </div>
      )}

      {pending && (
        <div className="mt-4 flex flex-wrap gap-2">
          <button
            className="btn btn-primary btn-sm"
            onClick={() => onApprove(plan.id)}
            disabled={isStreaming}
          >
            Approve
          </button>
          <button
            className="btn btn-ghost btn-sm"
            onClick={() => onReject(plan.id)}
            disabled={isStreaming}
          >
            Reject
          </button>
        </div>
      )}
    </div>
  );
}
