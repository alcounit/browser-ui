import { describe, it, expect } from "vitest";
import { formatStartTime } from "./utils";

describe("formatStartTime", () => {
  it("returns an empty string without a start time", () => {
    expect(formatStartTime("")).toBe("");
    expect(formatStartTime(null)).toBe("");
    expect(formatStartTime(undefined)).toBe("");
  });

  it("formats the start time as local hours and minutes", () => {
    const ts = "2026-11-17T15:04:05Z";
    expect(formatStartTime(ts)).toBe(
      new Date(ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
    );
  });
});
