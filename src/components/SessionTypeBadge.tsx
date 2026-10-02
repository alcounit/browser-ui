import React from "react";
import { sessionTypeLabel } from "../lib/sessionType";

interface SessionTypeBadgeProps {
  type?: string;
  active?: boolean;
  onClick?: (type: string) => void;
}

export const SessionTypeBadge: React.FC<SessionTypeBadgeProps> = ({ type, active, onClick }) => {
  const label = sessionTypeLabel(type);
  const className = `session-type-badge session-type-badge--${label}${active ? " session-type-badge--active" : ""}`;

  if (!onClick) {
    return <span className={className}>{label}</span>;
  }

  return (
    <button
      type="button"
      className={className}
      aria-pressed={!!active}
      onClick={() => onClick(label)}
    >
      {label}
    </button>
  );
};
