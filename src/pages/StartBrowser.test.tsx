import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, screen, waitFor, cleanup, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StartBrowser } from "./StartBrowser";
import type { SupportedBrowsers } from "../lib/browserCatalog";

const navigateMock = vi.fn();

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual<typeof import("react-router-dom")>("react-router-dom");
  return { ...actual, useNavigate: () => navigateMock };
});

function mockStatus(supportedBrowsers: SupportedBrowsers[], startOk = true) {
  global.fetch = vi.fn(async (url: any, init?: any) => {
    if (init?.method === "POST") {
      return {
        ok: startOk,
        statusText: startOk ? "OK" : "Bad Gateway",
        json: async () =>
          startOk
            ? { browserId: "browser-1" }
            : { error: "failed to create browser", reason: "404 page not found" },
      } as any;
    }
    return {
      ok: true,
      json: async () => ({ activeSessions: [], supportedBrowsers }),
    } as any;
  }) as any;
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <StartBrowser />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const section = (name: string) =>
  screen.getByText(name, { selector: ".start-browser-section-title" }).closest("section") as HTMLElement;

beforeEach(() => {
  navigateMock.mockReset();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("StartBrowser", () => {
  it("shows a loading state first", async () => {
    mockStatus([]);
    renderPage();
    expect(screen.getByText("Loading...")).toBeInTheDocument();
    await screen.findByText("No browsers configured.");
  });

  it("reports when nothing is configured", async () => {
    mockStatus([]);
    renderPage();
    expect(await screen.findByText("No browsers configured.")).toBeInTheDocument();
  });

  it("shows only the selenium section", async () => {
    mockStatus([
      { selenium: { chrome: ["120"] } },
      { playwright: { "playwright-chromium": ["1.59.1"] } },
      { mcp: { "playwright-mcp": ["0.0.75"] } },
      { devtools: { "devtools-chrome": ["151.0"] } },
      { unknown: { opera: ["1"] } },
    ]);
    renderPage();

    await screen.findByText("selenium", { selector: ".start-browser-section-title" });

    expect(
      [...document.querySelectorAll(".start-browser-section-title")].map((el) => el.textContent),
    ).toEqual(["selenium"]);
    expect(within(section("selenium")).getByRole("button", { name: "START" })).toBeEnabled();
  });

  it("explains an empty page when no selenium browsers are configured", async () => {
    mockStatus([{ devtools: { "devtools-chrome": ["151.0"] } }, { unknown: { chrome: ["120"] } }]);
    renderPage();

    expect(await screen.findByText("No Selenium browsers configured.")).toBeInTheDocument();
    expect(document.querySelectorAll(".start-browser-section")).toHaveLength(0);
  });

  it("starts the selected version and navigates to the session", async () => {
    mockStatus([{ selenium: { chrome: ["120", "121"] } }]);
    renderPage();

    await screen.findByText("selenium", { selector: ".start-browser-section-title" });

    await userEvent.selectOptions(screen.getByLabelText("Version"), "120");
    await userEvent.click(screen.getByRole("button", { name: "START" }));

    await waitFor(() => expect(navigateMock).toHaveBeenCalledWith("/session/browser-1"));

    const post = (global.fetch as any).mock.calls.find((c: any[]) => c[1]?.method === "POST");
    expect(JSON.parse(post[1].body)).toEqual({ browserName: "chrome", browserVersion: "120" });
  });

  it("starts the newest version when none is picked", async () => {
    mockStatus([{ selenium: { chrome: ["120", "121"] } }]);
    renderPage();

    await screen.findByText("selenium", { selector: ".start-browser-section-title" });
    await userEvent.click(screen.getByRole("button", { name: "START" }));

    await waitFor(() => expect(navigateMock).toHaveBeenCalled());

    const post = (global.fetch as any).mock.calls.find((c: any[]) => c[1]?.method === "POST");
    expect(JSON.parse(post[1].body).browserVersion).toBe("121");
  });

  it("surfaces a start failure", async () => {
    mockStatus([{ selenium: { chrome: ["120"] } }], false);
    renderPage();

    await screen.findByText("selenium", { selector: ".start-browser-section-title" });
    await userEvent.click(screen.getByRole("button", { name: "START" }));

    const toast = await screen.findByRole("alert");
    expect(within(toast).getByText("Failed to start chrome 120")).toBeInTheDocument();
    expect(within(toast).getByText("404 page not found")).toBeInTheDocument();
    expect(navigateMock).not.toHaveBeenCalled();

    await userEvent.click(within(toast).getByRole("button", { name: "Dismiss notification" }));
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("names the picked version in the failure toast, not the default one", async () => {
    mockStatus([{ selenium: { chrome: ["120", "121"] } }], false);
    renderPage();

    await screen.findByText("selenium", { selector: ".start-browser-section-title" });

    await userEvent.selectOptions(screen.getByLabelText("Version"), "120");
    await userEvent.click(screen.getByRole("button", { name: "START" }));

    const toast = await screen.findByRole("alert");
    expect(within(toast).getByText("Failed to start chrome 120")).toBeInTheDocument();
  });

  it("labels the version count per browser", async () => {
    mockStatus([{ selenium: { chrome: ["120", "121"], firefox: ["140"] } }]);
    renderPage();

    await screen.findByText("selenium", { selector: ".start-browser-section-title" });
    expect(screen.getByText("2 versions")).toBeInTheDocument();
    expect(screen.getByText("1 version")).toBeInTheDocument();
  });
});

describe("StartBrowser edge cases", () => {
  it("renders nothing when the status request fails", async () => {
    global.fetch = vi.fn(async () => ({ ok: false, json: async () => ({}) })) as any;
    renderPage();

    await waitFor(() => expect(screen.queryByText("Loading...")).toBeNull());
    expect(screen.getByText("No browsers configured.")).toBeInTheDocument();
  });

  it("falls back to an empty version for a browser without versions", async () => {
    mockStatus([{ selenium: { chrome: [] } }]);
    renderPage();

    await screen.findByText("selenium", { selector: ".start-browser-section-title" });
    expect(screen.getByText("0 versions")).toBeInTheDocument();
    expect((screen.getByLabelText("Version") as HTMLSelectElement).value).toBe("");
  });
});
