import { useEffect, useState } from "react";
import { getTasks } from "@/api/client";
import type { TaskList } from "@/api/types";

export function TaskStrip({ sessionID }: { sessionID: string }) {
  const [tasks, setTasks] = useState<TaskList | null>(null);

  useEffect(() => {
    let alive = true;
    getTasks(sessionID)
      .then((next) => {
        if (alive) setTasks(next);
      })
      .catch(() => {
        if (alive) setTasks(null);
      });
    return () => {
      alive = false;
    };
  }, [sessionID]);

  const items = Array.isArray(tasks?.items) ? tasks.items : [];
  if (!tasks || items.length === 0) return null;

  return (
    <div className="border-b border-base-300 bg-base-200/40 px-4 py-2">
      <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-base-content/60">
        Session Tasks
      </div>
      <div className="flex flex-wrap gap-2">
        {items.map((task) => (
          <span
            key={task.id}
            className={`badge badge-outline ${
              task.status === "completed"
                ? "badge-success"
                : task.status === "blocked"
                  ? "badge-error"
                  : task.status === "in_progress"
                    ? "badge-primary"
                    : ""
            }`}
          >
            {task.content}
          </span>
        ))}
      </div>
    </div>
  );
}
