import { describe, it, expect } from "vitest";
import { sessionTypeLabel, SESSION_TYPES, UNKNOWN_SESSION_TYPE } from "./sessionType";

describe("sessionTypeLabel", () => {
  it.each([...SESSION_TYPES])("keeps the known type %s", (type) => {
    expect(sessionTypeLabel(type)).toBe(type);
  });

  it.each([
    ["undefined", undefined],
    ["empty string", ""],
    ["an unknown type", "cdp"],
    ["a differently cased type", "Selenium"],
  ])("maps %s to unknown", (_label, type) => {
    expect(sessionTypeLabel(type)).toBe(UNKNOWN_SESSION_TYPE);
  });
});
