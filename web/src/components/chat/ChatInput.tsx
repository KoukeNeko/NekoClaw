import { useState, useRef, useCallback, useEffect } from "react";

interface Props {
  onSend: (text: string, images?: { mime_type: string; data: string; file_name?: string }[]) => void;
  onCancel: () => void;
  isStreaming: boolean;
}

/**
 * Chat input textarea with send/cancel button.
 * Supports Enter to send, Shift+Enter for newline.
 * Supports image paste (Ctrl+V) and drag-and-drop.
 */
export function ChatInput({ onSend, onCancel, isStreaming }: Props) {
  const [text, setText] = useState("");
  const [images, setImages] = useState<{ mime_type: string; data: string; file_name: string }[]>([]);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Auto-resize textarea
  useEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`;
  }, [text]);

  // Focus textarea on mount and route change
  useEffect(() => {
    textareaRef.current?.focus();
  }, []);

  const handleSend = useCallback(() => {
    if (isStreaming) return;
    if (!text.trim() && images.length === 0) return;
    onSend(text, images.length > 0 ? images : undefined);
    setText("");
    setImages([]);
    // Reset textarea height
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
    }
  }, [text, images, isStreaming, onSend]);

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && !e.shiftKey && !e.altKey && !e.metaKey) {
      e.preventDefault();
      handleSend();
    }
  }

  function handlePaste(e: React.ClipboardEvent) {
    const items = e.clipboardData?.items;
    if (!items) return;

    for (const item of items) {
      if (item.type.startsWith("image/")) {
        e.preventDefault();
        const file = item.getAsFile();
        if (!file) continue;
        readImageFile(file);
        break;
      }
    }
  }

  function handleDrop(e: React.DragEvent) {
    e.preventDefault();
    const files = e.dataTransfer?.files;
    if (!files) return;
    for (const file of files) {
      if (file.type.startsWith("image/")) {
        readImageFile(file);
      }
    }
  }

  function readImageFile(file: File) {
    const reader = new FileReader();
    reader.onload = () => {
      const base64 = (reader.result as string).split(",")[1];
      setImages((prev) => [
        ...prev,
        {
          mime_type: file.type,
          data: base64,
          file_name: file.name,
        },
      ]);
    };
    reader.readAsDataURL(file);
  }

  function removeImage(index: number) {
    setImages((prev) => prev.filter((_, i) => i !== index));
  }

  return (
    <div className="border-t border-base-300 bg-base-100 p-3">
      <div className="max-w-3xl mx-auto">
        {/* Image preview strip */}
        {images.length > 0 && (
          <div className="flex flex-wrap gap-2 mb-2">
            {images.map((img, i) => (
              <div key={i} className="badge badge-lg gap-1">
                📎 {img.file_name}
                <button
                  className="btn btn-ghost btn-xs btn-circle"
                  onClick={() => removeImage(i)}
                >
                  ✕
                </button>
              </div>
            ))}
          </div>
        )}

        <div className="flex gap-2 items-end">
          <textarea
            ref={textareaRef}
            className="textarea textarea-bordered flex-1 min-h-10 max-h-[200px] resize-none leading-normal focus:outline-0"
            placeholder="輸入訊息... (Enter 送出, Shift+Enter 換行)"
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={handleKeyDown}
            onPaste={handlePaste}
            onDrop={handleDrop}
            onDragOver={(e) => e.preventDefault()}
            rows={1}
            disabled={isStreaming}
          />

          {isStreaming ? (
            <button
              className="btn btn-error btn-sm"
              onClick={onCancel}
              title="取消"
            >
              <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth={2}
                stroke="currentColor"
                className="w-4 h-4"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M6 18 18 6M6 6l12 12"
                />
              </svg>
            </button>
          ) : (
            <button
              className="btn btn-primary btn-sm"
              onClick={handleSend}
              disabled={!text.trim() && images.length === 0}
              title="送出 (Enter)"
            >
              <svg
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth={2}
                stroke="currentColor"
                className="w-4 h-4"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M6 12 3.269 3.125A59.769 59.769 0 0 1 21.485 12 59.768 59.768 0 0 1 3.27 20.875L5.999 12Zm0 0h7.5"
                />
              </svg>
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
