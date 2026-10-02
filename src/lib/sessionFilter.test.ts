import { describe, it, expect, beforeEach } from "vitest";
import { readTypeFilter, writeTypeFilter, SESSION_TYPE_FILTER_KEY } from "./sessionFilter";

const throwingStorage = (): Storage =>
  ({
    getItem: () => { throw new Error("blocked"); },
    setItem: () => { throw new Error("blocked"); },
    removeItem: () => { throw new Error("blocked"); },
  } as unknown as Storage);

beforeEach(() => {
  sessionStorage.clear();
});

describe("sessionFilter", () => {
  it("round-trips a value through the injected storage", () => {
    writeTypeFilter("playwright", sessionStorage);
    expect(sessionStorage.getItem(SESSION_TYPE_FILTER_KEY)).toBe("playwright");
    expect(readTypeFilter(sessionStorage)).toBe("playwright");
  });

  it("returns null when nothing is stored", () => {
    expect(readTypeFilter(sessionStorage)).toBeNull();
  });

  it("clears the entry when written with null", () => {
    writeTypeFilter("mcp", sessionStorage);
    writeTypeFilter(null, sessionStorage);

    expect(sessionStorage.getItem(SESSION_TYPE_FILTER_KEY)).toBeNull();
    expect(readTypeFilter(sessionStorage)).toBeNull();
  });

  it("defaults to sessionStorage", () => {
    writeTypeFilter("selenium");
    expect(readTypeFilter()).toBe("selenium");
    writeTypeFilter(null);
    expect(readTypeFilter()).toBeNull();
  });

  it("returns null when reading throws", () => {
    expect(readTypeFilter(throwingStorage())).toBeNull();
  });

  it("swallows a throwing write", () => {
    expect(() => writeTypeFilter("mcp", throwingStorage())).not.toThrow();
    expect(() => writeTypeFilter(null, throwingStorage())).not.toThrow();
  });

  it("falls back to no storage when sessionStorage itself throws", () => {
    const original = Object.getOwnPropertyDescriptor(globalThis, "sessionStorage");

    Object.defineProperty(globalThis, "sessionStorage", {
      configurable: true,
      get() { throw new Error("blocked"); },
    });

    try {
      expect(readTypeFilter()).toBeNull();
      expect(() => writeTypeFilter("mcp")).not.toThrow();
    } finally {
      if (original) Object.defineProperty(globalThis, "sessionStorage", original);
    }
  });

  it("does nothing when there is no storage at all", () => {
    expect(readTypeFilter(null)).toBeNull();
    expect(() => writeTypeFilter("mcp", null)).not.toThrow();
  });
});
