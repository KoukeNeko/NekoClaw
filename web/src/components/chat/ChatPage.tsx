import { useEffect } from "react";
import { useAppStore } from "@/store/appStore";
import { useChat } from "@/hooks/useChat";
import { getDefaultProvider } from "@/api/client";
import { MessageList } from "./MessageList";
import { ChatInput } from "./ChatInput";
import { ChatHeader } from "./ChatHeader";
import { TaskStrip } from "./TaskStrip";
import { ToolApprovalModal } from "./ToolApprovalModal";

/**
 * Main chat page — header, scrollable message list, and input area.
 */
export function ChatPage() {
  const provider = useAppStore((s) => s.provider);
  const setProvider = useAppStore((s) => s.setProvider);
  const setModel = useAppStore((s) => s.setModel);
  const setThinkingMode = useAppStore((s) => s.setThinkingMode);
  const sessionID = useAppStore((s) => s.sessionID);
  const pendingApprovals = useAppStore((s) => s.pendingApprovals);

  const {
    sendMessage,
    sendApprovals,
    cancelStream,
    isStreaming,
    createPlan,
    approvePlan,
    rejectPlan,
    loadPlans,
  } = useChat();

  // Load default provider/model on mount
  useEffect(() => {
    if (!provider) {
      getDefaultProvider()
        .then((dp) => {
          setProvider(dp.provider);
          setModel(dp.model);
          setThinkingMode(dp.thinking_mode ?? "auto");
        })
        .catch(() => {
          /* server not ready */
        });
    }
  }, [provider, setProvider, setModel, setThinkingMode]);

  useEffect(() => {
    loadPlans(sessionID);
  }, [sessionID, loadPlans]);

  return (
    <div className="flex flex-col h-full">
      <ChatHeader />
      <TaskStrip sessionID={sessionID} />

      <div className="flex-1 overflow-hidden">
        <MessageList onApprovePlan={approvePlan} onRejectPlan={rejectPlan} />
      </div>

      <ChatInput
        onSend={sendMessage}
        onPlan={createPlan}
        onCancel={cancelStream}
        isStreaming={isStreaming}
      />

      {pendingApprovals.length > 0 && (
        <ToolApprovalModal onDecide={sendApprovals} />
      )}
    </div>
  );
}
