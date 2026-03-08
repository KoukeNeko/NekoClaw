import { useEffect } from "react";
import { useAppStore } from "@/store/appStore";
import { useChat } from "@/hooks/useChat";
import { getDefaultProvider } from "@/api/client";
import { MessageList } from "./MessageList";
import { ChatInput } from "./ChatInput";
import { ChatHeader } from "./ChatHeader";
import { ToolApprovalModal } from "./ToolApprovalModal";

/**
 * Main chat page — header, scrollable message list, and input area.
 */
export function ChatPage() {
  const provider = useAppStore((s) => s.provider);
  const setProvider = useAppStore((s) => s.setProvider);
  const setModel = useAppStore((s) => s.setModel);
  const pendingApprovals = useAppStore((s) => s.pendingApprovals);

  const { sendMessage, sendApprovals, cancelStream, isStreaming } = useChat();

  // Load default provider/model on mount
  useEffect(() => {
    if (!provider) {
      getDefaultProvider()
        .then((dp) => {
          setProvider(dp.provider);
          setModel(dp.model);
        })
        .catch(() => {
          /* server not ready */
        });
    }
  }, [provider, setProvider, setModel]);

  return (
    <div className="flex flex-col h-full">
      <ChatHeader />

      <div className="flex-1 overflow-hidden">
        <MessageList />
      </div>

      <ChatInput
        onSend={sendMessage}
        onCancel={cancelStream}
        isStreaming={isStreaming}
      />

      {pendingApprovals.length > 0 && (
        <ToolApprovalModal onDecide={sendApprovals} />
      )}
    </div>
  );
}
