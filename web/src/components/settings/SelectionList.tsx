import type { ReactNode } from "react";

type SelectionListProps = {
  children: ReactNode;
};

type SelectionListItemProps = {
  children: ReactNode;
  onClick: () => void;
  selected: boolean;
};

export function SelectionList({ children }: SelectionListProps) {
  return (
    <ul className="space-y-2 rounded-box border border-base-300 bg-base-100 p-2">
      {children}
    </ul>
  );
}

export function SelectionListItem({
  children,
  onClick,
  selected,
}: SelectionListItemProps) {
  return (
    <li className="w-full">
      <button
        className={`flex w-full min-w-0 items-start rounded-box border border-base-300 px-4 py-3 text-left transition-colors ${
          selected
            ? "border-primary/60 bg-base-300"
            : "bg-base-100 hover:bg-base-200"
        }`}
        onClick={onClick}
        type="button"
      >
        {children}
      </button>
    </li>
  );
}
