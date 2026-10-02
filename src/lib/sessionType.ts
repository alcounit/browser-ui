export const SESSION_TYPES = ["selenium", "playwright", "devtools", "mcp"] as const;

export const UNKNOWN_SESSION_TYPE = "unknown";

export type SessionTypeLabel = (typeof SESSION_TYPES)[number] | typeof UNKNOWN_SESSION_TYPE;

export const sessionTypeLabel = (type?: string): SessionTypeLabel =>
  SESSION_TYPES.includes(type as (typeof SESSION_TYPES)[number])
    ? (type as SessionTypeLabel)
    : UNKNOWN_SESSION_TYPE;
