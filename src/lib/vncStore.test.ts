import { describe, it, expect } from "vitest";
import {
  NAME_PREFIX,
  VERSION_PREFIX,
  getSavedByName,
  setSavedByName,
  getSavedByVersion,
  setSavedByVersion,
} from "./vncStore";

function memoryStorage(): Storage {
  const map = new Map<string, string>();
  return {
    get length() {
      return map.size;
    },
    clear: () => map.clear(),
    getItem: (k: string) => (map.has(k) ? map.get(k)! : null),
    key: (i: number) => Array.from(map.keys())[i] ?? null,
    removeItem: (k: string) => map.delete(k),
    setItem: (k: string, v: string) => {
      map.set(k, v);
    },
  } as Storage;
}

describe("saved-by-name", () => {
  it("writes and reads under the name prefix", () => {
    const s = memoryStorage();
    setSavedByName("chrome", "secret", s);
    expect(s.getItem(NAME_PREFIX + "chrome")).toBe("secret");
    expect(getSavedByName("chrome", s)).toBe("secret");
  });

  it("returns null for missing or empty name", () => {
    const s = memoryStorage();
    expect(getSavedByName("firefox", s)).toBeNull();
    expect(getSavedByName("", s)).toBeNull();
    setSavedByName("", "x", s);
    expect(s.length).toBe(0);
  });
});

describe("saved-by-version", () => {
  it("writes and reads under the version prefix keyed by name@version", () => {
    const s = memoryStorage();
    setSavedByVersion("chrome", "123", "pw", s);
    expect(s.getItem(VERSION_PREFIX + "chrome@123")).toBe("pw");
    expect(getSavedByVersion("chrome", "123", s)).toBe("pw");
  });

  it("returns null and skips writes when name or version is empty", () => {
    const s = memoryStorage();
    expect(getSavedByVersion("chrome", "999", s)).toBeNull();
    expect(getSavedByVersion("", "123", s)).toBeNull();
    expect(getSavedByVersion("chrome", "", s)).toBeNull();
    setSavedByVersion("", "123", "x", s);
    setSavedByVersion("chrome", "", "x", s);
    expect(s.length).toBe(0);
  });
});

describe("default browser storage", () => {
  it("uses localStorage when no storage is injected", () => {
    localStorage.clear();

    setSavedByName("chrome", "byname", localStorage);
    expect(getSavedByName("chrome")).toBe("byname");

    setSavedByVersion("chrome", "123", "byver", localStorage);
    expect(getSavedByVersion("chrome", "123")).toBe("byver");
  });
});
