import { describe, it, expect } from "vitest";
import {
  buildCatalog,
  canStart,
  creatableGroups,
  flattenBrowsers,
  startBrowserError,
  type SupportedBrowsers,
} from "./browserCatalog";

describe("buildCatalog", () => {
  it("returns an empty list for no configs", () => {
    expect(buildCatalog([])).toEqual([]);
  });

  it("groups browsers by session type", () => {
    const cfgs: SupportedBrowsers[] = [
      { selenium: { chrome: ["120"] } },
      { playwright: { "playwright-chromium": ["1.59.1"] } },
    ];

    expect(buildCatalog(cfgs)).toEqual([
      { sessionType: "playwright", browsers: [{ name: "playwright-chromium", versions: ["1.59.1"] }] },
      { sessionType: "selenium", browsers: [{ name: "chrome", versions: ["120"] }] },
    ]);
  });

  it("merges versions of the same browser across configs", () => {
    const cfgs: SupportedBrowsers[] = [
      { selenium: { chrome: ["120", "119"] } },
      { selenium: { chrome: ["121", "120"] } },
    ];

    expect(buildCatalog(cfgs)).toEqual([
      { sessionType: "selenium", browsers: [{ name: "chrome", versions: ["121", "120", "119"] }] },
    ]);
  });

  it("sorts versions numerically descending and browsers by name", () => {
    const cfgs: SupportedBrowsers[] = [
      { selenium: { firefox: ["9", "10", "100"], chrome: ["120"] } },
    ];

    expect(buildCatalog(cfgs)[0].browsers).toEqual([
      { name: "chrome", versions: ["120"] },
      { name: "firefox", versions: ["100", "10", "9"] },
    ]);
  });

  it("orders groups by session type name", () => {
    const cfgs: SupportedBrowsers[] = [
      { unknown: { opera: ["1"] } },
      { devtools: { "devtools-chrome": ["151.0"] } },
      { selenium: { chrome: ["120"] } },
      { mcp: { "playwright-mcp": ["0.0.75"] } },
    ];

    expect(buildCatalog(cfgs).map((g) => g.sessionType)).toEqual(["devtools", "mcp", "selenium", "unknown"]);
  });

  it("tolerates missing nested values", () => {
    const cfgs = [
      null,
      { selenium: null },
      { selenium: { chrome: null } },
    ] as unknown as SupportedBrowsers[];

    expect(buildCatalog(cfgs)).toEqual([
      { sessionType: "selenium", browsers: [{ name: "chrome", versions: [] }] },
    ]);
  });
});

describe("canStart", () => {
  it("allows selenium only", () => {
    expect(canStart("selenium")).toBe(true);
  });

  it.each(["playwright", "mcp", "devtools", "unknown", ""])("blocks %s", (type) => {
    expect(canStart(type)).toBe(false);
  });
});

describe("creatableGroups", () => {
  it("keeps only the selenium group", () => {
    const groups = buildCatalog([
      { selenium: { chrome: ["120"] } },
      { playwright: { "playwright-chromium": ["1.59.1"] } },
      { devtools: { "devtools-chrome": ["151.0"] } },
      { unknown: { opera: ["1"] } },
    ]);

    expect(creatableGroups(groups).map((g) => g.sessionType)).toEqual(["selenium"]);
  });

  it("returns nothing when no selenium browsers are configured", () => {
    expect(creatableGroups(buildCatalog([{ devtools: { "devtools-chrome": ["151.0"] } }]))).toEqual([]);
  });
});

describe("flattenBrowsers", () => {
  it("returns an empty list for no groups", () => {
    expect(flattenBrowsers([])).toEqual([]);
  });

  it("merges the same browser name across session types", () => {
    const groups = buildCatalog([
      { selenium: { chrome: ["120"] } },
      { unknown: { chrome: ["121"], firefox: ["140"] } },
    ]);

    expect(flattenBrowsers(groups)).toEqual([
      { name: "chrome", versions: ["121", "120"] },
      { name: "firefox", versions: ["140"] },
    ]);
  });
});

describe("startBrowserError", () => {
  const res = (body: unknown, statusText = "Bad Gateway"): Response =>
    ({ statusText, json: async () => body } as unknown as Response);

  it("uses the reason when present", async () => {
    const err = await startBrowserError(res({ error: "failed to create browser", reason: "browser did not become ready" }));
    expect(err.message).toBe("browser did not become ready");
  });

  it("falls back to the error field", async () => {
    const err = await startBrowserError(res({ error: "failed to create browser" }));
    expect(err.message).toBe("failed to create browser");
  });

  it("falls back to the status text on an empty payload", async () => {
    const err = await startBrowserError(res({}));
    expect(err.message).toBe("Bad Gateway");
  });

  it("falls back to the status text on an unparsable body", async () => {
    const broken = { statusText: "Bad Gateway", json: async () => { throw new Error("nope"); } } as unknown as Response;
    const err = await startBrowserError(broken);
    expect(err.message).toBe("Bad Gateway");
  });

  it("falls back to a constant when there is no status text", async () => {
    const err = await startBrowserError(res({}, ""));
    expect(err.message).toBe("Failed to start browser");
  });
});
