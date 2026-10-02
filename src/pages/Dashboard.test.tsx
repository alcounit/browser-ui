import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, screen, waitFor, cleanup, fireEvent, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Dashboard } from "./Dashboard";
import type { SupportedBrowsers } from "../lib/browserCatalog";
import { SESSION_TYPE_FILTER_KEY } from "../lib/sessionFilter";

const navigateMock = vi.fn();

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual<typeof import("react-router-dom")>("react-router-dom");
  return { ...actual, useNavigate: () => navigateMock };
});

const auth = vi.hoisted(() => ({ authEnabled: false, onUnauthorized: vi.fn() }));

vi.mock("../App", () => ({
  useAuth: () => auth,
}));

function mockStatus(supportedBrowsers: SupportedBrowsers[], activeSessions: unknown[] = []) {
  global.fetch = vi.fn(async (_url: any, init?: any) => {
    if (init?.method === "POST") {
      return { ok: true, json: async () => ({ browserId: "browser-1" }) } as any;
    }
    return {
      ok: true,
      status: 200,
      json: async () => ({ activeSessions, supportedBrowsers }),
    } as any;
  }) as any;
}

function renderDashboard() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <Dashboard />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const openDropdown = async () => {
  await userEvent.click(screen.getByRole("button", { name: "START BROWSER" }));
};

const runningSession = {
  sessionId: "s-1",
  browserId: "b-1",
  browserName: "chrome",
  browserVersion: "120",
  startTime: "2020-01-01T00:00:00Z",
  phase: "Running",
  startedManually: true,
};

const session = (id: string, sessionType?: string) => ({
  sessionId: `s-${id}`,
  browserId: id,
  browserName: "chrome",
  browserVersion: "120",
  startTime: "2020-01-01T00:00:00Z",
  phase: "Running",
  startedManually: false,
  sessionType,
});

const cardIds = () =>
  [...document.querySelectorAll(".browser-uuid")].map(el => el.textContent);

beforeEach(() => {
  navigateMock.mockReset();
  auth.authEnabled = false;
  auth.onUnauthorized.mockReset();
  sessionStorage.clear();
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("Dashboard browser catalog", () => {
  it("reports when nothing is configured", async () => {
    mockStatus([]);
    renderDashboard();

    await openDropdown();
    expect(await screen.findByText("No browsers configured")).toBeInTheDocument();
  });

  it("offers only selenium browsers", async () => {
    mockStatus([
      { mcp: { "playwright-mcp": ["0.0.75"] } },
      { selenium: { chrome: ["120"] } },
      { devtools: { "devtools-chrome": ["151.0"] } },
      { unknown: { opera: ["1"] } },
    ]);
    renderDashboard();

    await waitFor(() => expect(global.fetch).toHaveBeenCalled());
    await openDropdown();

    await screen.findByText("selenium", { selector: ".session-group-title" });
    expect(
      [...document.querySelectorAll(".session-group-title")].map((el) => el.textContent),
    ).toEqual(["selenium"]);
  });

  it("explains an empty menu when no selenium browsers are configured", async () => {
    mockStatus([{ devtools: { "devtools-chrome": ["151.0"] } }, { unknown: { chrome: ["120"] } }]);
    renderDashboard();

    await waitFor(() => expect(global.fetch).toHaveBeenCalled());
    await openDropdown();

    expect(await screen.findByText("No Selenium browsers configured")).toBeInTheDocument();
    expect(document.querySelectorAll(".session-group-title")).toHaveLength(0);
  });

  it("expands a group to reveal its browsers and versions", async () => {
    mockStatus([{ selenium: { chrome: ["120", "121"] } }]);
    renderDashboard();

    await waitFor(() => expect(global.fetch).toHaveBeenCalled());
    await openDropdown();

    await userEvent.click(await screen.findByText("selenium", { selector: ".session-group-title" }));
    await userEvent.click(await screen.findByText("chrome", { selector: ".browser-group-name" }));

    expect(screen.getByText("121")).toBeInTheDocument();
    expect(screen.getByText("120")).toBeInTheDocument();
  });

  it("collapses a group on a second click", async () => {
    mockStatus([{ selenium: { chrome: ["120"] } }]);
    renderDashboard();

    await waitFor(() => expect(global.fetch).toHaveBeenCalled());
    await openDropdown();

    const header = await screen.findByText("selenium", { selector: ".session-group-title" });
    await userEvent.click(header);
    expect(screen.getByText("chrome", { selector: ".browser-group-name" })).toBeInTheDocument();

    await userEvent.click(header);
    expect(screen.queryByText("chrome", { selector: ".browser-group-name" })).toBeNull();
  });

  it("starts the picked version and navigates to the session", async () => {
    mockStatus([{ selenium: { chrome: ["120"] } }]);
    renderDashboard();

    await waitFor(() => expect(global.fetch).toHaveBeenCalled());
    await openDropdown();

    await userEvent.click(await screen.findByText("selenium", { selector: ".session-group-title" }));
    await userEvent.click(await screen.findByText("chrome", { selector: ".browser-group-name" }));
    await userEvent.click(screen.getByText("120"));

    await waitFor(() => expect(navigateMock).toHaveBeenCalledWith("/session/browser-1"));

    const post = (global.fetch as any).mock.calls.find((c: any[]) => c[1]?.method === "POST");
    expect(JSON.parse(post[1].body)).toEqual({ browserName: "chrome", browserVersion: "120" });
  });

  it("counts sessions per browser name across groups", async () => {
    mockStatus(
      [
        { selenium: { chrome: ["120"] } },
        { unknown: { firefox: ["140"] } },
      ],
      [
        {
          sessionId: "s-1",
          browserId: "b-1",
          browserName: "chrome",
          browserVersion: "120",
          startTime: "2020-01-01T00:00:00Z",
          phase: "Running",
          startedManually: false,
        },
      ],
    );
    renderDashboard();

    await screen.findByText("b-1");

    await waitFor(() => {
      const stats = document.querySelectorAll(".stat-item");
      const text = stats[stats.length - 1].textContent ?? "";
      expect(text).toContain("CHROME: 1");
      expect(text).toContain("FIREFOX: 0");
    });
  });
});

describe("Dashboard dropdown behaviour", () => {
  it("closes on an outside click and forgets the expansion", async () => {
    mockStatus([{ selenium: { chrome: ["120"] } }]);
    renderDashboard();

    await waitFor(() => expect(global.fetch).toHaveBeenCalled());
    await openDropdown();
    await userEvent.click(await screen.findByText("selenium", { selector: ".session-group-title" }));

    await userEvent.click(document.body);

    expect(screen.queryByText("selenium", { selector: ".session-group-title" })).toBeNull();

    await openDropdown();
    expect(screen.queryByText("chrome", { selector: ".browser-group-name" })).toBeNull();
  });

  it("resets the expansion when the dropdown is toggled shut", async () => {
    mockStatus([{ selenium: { chrome: ["120"] } }]);
    renderDashboard();

    await waitFor(() => expect(global.fetch).toHaveBeenCalled());
    await openDropdown();
    await userEvent.click(await screen.findByText("selenium", { selector: ".session-group-title" }));
    await openDropdown();
    await openDropdown();

    expect(screen.queryByText("chrome", { selector: ".browser-group-name" })).toBeNull();
  });

  it("collapsing the group also collapses the expanded browser", async () => {
    mockStatus([{ selenium: { chrome: ["120"] } }]);
    renderDashboard();

    await waitFor(() => expect(global.fetch).toHaveBeenCalled());
    await openDropdown();

    const header = await screen.findByText("selenium", { selector: ".session-group-title" });
    await userEvent.click(header);
    await userEvent.click(screen.getByText("chrome", { selector: ".browser-group-name" }));
    expect(screen.getByText("120")).toBeInTheDocument();

    await userEvent.click(header);
    await userEvent.click(header);
    expect(screen.getByText("chrome", { selector: ".browser-group-name" })).toBeInTheDocument();
    expect(screen.queryByText("120")).toBeNull();
  });

  it("scrolls a long version list with the arrows", async () => {
    const versions = Array.from({ length: 12 }, (_, i) => `1${i}0`);
    mockStatus([{ selenium: { chrome: versions } }]);
    renderDashboard();

    await waitFor(() => expect(global.fetch).toHaveBeenCalled());
    await openDropdown();
    await userEvent.click(await screen.findByText("selenium", { selector: ".session-group-title" }));
    await userEvent.click(screen.getByText("chrome", { selector: ".browser-group-name" }));

    const list = document.querySelector(".versions-list") as HTMLDivElement;
    list.scrollBy = vi.fn();

    const up = document.querySelector(".scroll-arrow-up") as HTMLElement;
    const down = document.querySelector(".scroll-arrow-down") as HTMLElement;
    expect(up).toBeTruthy();

    vi.useFakeTimers();
    try {
      fireEvent.mouseEnter(down);
      vi.advanceTimersByTime(120);
      fireEvent.mouseLeave(down);

      fireEvent.mouseEnter(up);
      vi.advanceTimersByTime(120);
      fireEvent.mouseLeave(up);
    } finally {
      vi.useRealTimers();
    }

    expect(list.scrollBy).toHaveBeenCalled();
  });

  it("shows the hub reason when a start fails", async () => {
    global.fetch = vi.fn(async (_url: any, init?: any) => {
      if (init?.method === "POST") {
        return {
          ok: false,
          statusText: "Bad Gateway",
          json: async () => ({ error: "failed to create browser", reason: "browser did not become ready" }),
        } as any;
      }
      return {
        ok: true,
        status: 200,
        json: async () => ({ activeSessions: [], supportedBrowsers: [{ selenium: { chrome: ["120"] } }] }),
      } as any;
    }) as any;

    renderDashboard();

    await waitFor(() => expect(global.fetch).toHaveBeenCalled());
    await openDropdown();
    await userEvent.click(await screen.findByText("selenium", { selector: ".session-group-title" }));
    await userEvent.click(screen.getByText("chrome", { selector: ".browser-group-name" }));
    await userEvent.click(screen.getByText("120"));

    const toast = await screen.findByRole("alert");
    expect(within(toast).getByText("Failed to start chrome 120")).toBeInTheDocument();
    expect(within(toast).getByText("browser did not become ready")).toBeInTheDocument();
    expect(screen.getByText("120").closest("button")).toBeEnabled();
    expect(navigateMock).not.toHaveBeenCalled();
  });
});

describe("Dashboard sessions", () => {
  it("shows a loading state before the first response", () => {
    mockStatus([]);
    renderDashboard();
    expect(screen.getByText("Loading sessions...")).toBeInTheDocument();
  });

  it("signals an unauthorized status response", async () => {
    global.fetch = vi.fn(async () => ({ ok: false, status: 401, json: async () => ({}) })) as any;
    renderDashboard();

    await waitFor(() => expect(auth.onUnauthorized).toHaveBeenCalled());
  });

  it("deletes a manually started session", async () => {
    mockStatus([], [runningSession]);
    renderDashboard();

    await screen.findByText("b-1");
    expect(screen.getByText("manual")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "delete" }));

    await waitFor(() => {
      const call = (global.fetch as any).mock.calls.find((c: any[]) => c[1]?.method === "DELETE");
      expect(call[0]).toBe("/api/v1/browsers/b-1");
    });
    expect(screen.queryByText("manual")).toBeNull();
  });

  it("disables CONNECT while a session is not running", async () => {
    mockStatus([], [{ ...runningSession, phase: "Pending", startedManually: false }]);
    renderDashboard();

    await screen.findByText("b-1");
    expect(screen.getByRole("button", { name: "CONNECT" })).toBeDisabled();
    expect(screen.queryByText("manual")).toBeNull();
  });

  it("links to the session while it is running", async () => {
    mockStatus([], [runningSession]);
    renderDashboard();

    await screen.findByText("b-1");
    expect(screen.getByRole("link", { name: "CONNECT" })).toHaveAttribute("href", "/session/b-1");
  });

  it("signs out when auth is enabled", async () => {
    auth.authEnabled = true;
    mockStatus([]);
    renderDashboard();

    await userEvent.click(screen.getByRole("button", { name: "Sign out" }));

    await waitFor(() => expect(navigateMock).toHaveBeenCalledWith("/ui/login", { replace: true }));
  });
});


describe("Dashboard session type filter", () => {
  const mixed = [
    session("b-sel", "selenium"),
    session("b-pw", "playwright"),
    session("b-mcp", "mcp"),
    session("b-none"),
  ];

  it("keeps only the clicked type, and restores everything on a second click", async () => {
    mockStatus([], mixed);
    renderDashboard();

    await screen.findByText("b-sel");
    expect(cardIds()).toHaveLength(4);

    await userEvent.click(screen.getByRole("button", { name: "playwright" }));
    expect(cardIds()).toEqual(["b-pw"]);

    await userEvent.click(screen.getByRole("button", { name: "playwright" }));
    expect(cardIds()).toHaveLength(4);
  });

  it("labels sessions without a known type as unknown and filters them", async () => {
    mockStatus([], [...mixed, session("b-cdp", "cdp")]);
    renderDashboard();

    await screen.findByText("b-none");
    expect(screen.getAllByRole("button", { name: "unknown" })).toHaveLength(2);

    await userEvent.click(screen.getAllByRole("button", { name: "unknown" })[0]);
    expect(cardIds().sort()).toEqual(["b-cdp", "b-none"]);
    screen.getAllByRole("button", { name: "unknown" }).forEach((badge) =>
      expect(badge).toHaveAttribute("aria-pressed", "true"),
    );
  });

  it("hides sessions without a type while a filter is on", async () => {
    mockStatus([], mixed);
    renderDashboard();

    await screen.findByText("b-sel");
    await userEvent.click(screen.getByRole("button", { name: "selenium" }));

    expect(cardIds()).toEqual(["b-sel"]);
    expect(screen.queryByText("b-none")).toBeNull();
  });

  it("switches straight from one type to another", async () => {
    mockStatus([], mixed);
    renderDashboard();

    await screen.findByText("b-sel");
    await userEvent.click(screen.getByRole("button", { name: "mcp" }));
    expect(cardIds()).toEqual(["b-mcp"]);

    await userEvent.click(screen.getByRole("button", { name: "mcp" }));
    await userEvent.click(screen.getByRole("button", { name: "selenium" }));
    expect(cardIds()).toEqual(["b-sel"]);
  });

  it("marks the active badge as pressed", async () => {
    mockStatus([], mixed);
    renderDashboard();

    await screen.findByText("b-sel");
    await userEvent.click(screen.getByRole("button", { name: "mcp" }));

    expect(screen.getByRole("button", { name: "mcp" })).toHaveAttribute("aria-pressed", "true");
  });

  it("survives leaving the dashboard and coming back", async () => {
    mockStatus([], mixed);
    const first = renderDashboard();

    await screen.findByText("b-sel");
    await userEvent.click(screen.getByRole("button", { name: "playwright" }));
    expect(cardIds()).toEqual(["b-pw"]);

    first.unmount();

    renderDashboard();
    await screen.findByText("b-pw");

    expect(cardIds()).toEqual(["b-pw"]);
    expect(screen.getByRole("button", { name: "playwright" })).toHaveAttribute("aria-pressed", "true");
  });

  it("restores a filter stored before the first render", async () => {
    sessionStorage.setItem(SESSION_TYPE_FILTER_KEY, "mcp");
    mockStatus([], mixed);
    renderDashboard();

    await screen.findByText("b-mcp");
    expect(cardIds()).toEqual(["b-mcp"]);
  });

  it("clears itself when the filtered sessions are gone", async () => {
    sessionStorage.setItem(SESSION_TYPE_FILTER_KEY, "playwright");
    mockStatus([], [session("b-sel", "selenium")]);
    renderDashboard();

    await screen.findByText("b-sel");

    expect(cardIds()).toEqual(["b-sel"]);
    expect(sessionStorage.getItem(SESSION_TYPE_FILTER_KEY)).toBeNull();
  });
});

describe("Dashboard vnc availability", () => {
  it("disables CONNECT for a browser without vnc", async () => {
    mockStatus([], [{ ...session("b-novnc", "devtools"), vnc: false }]);
    renderDashboard();

    await screen.findByText("b-novnc");
    const connect = screen.getByRole("button", { name: "CONNECT" });
    expect(connect).toBeDisabled();
    expect(connect).toHaveAttribute("title", "VNC is not available for this browser");
    expect(screen.queryByRole("link", { name: "CONNECT" })).toBeNull();
  });

  it("keeps CONNECT as a link when vnc is available", async () => {
    mockStatus([], [{ ...session("b-vnc", "selenium"), vnc: true }]);
    renderDashboard();

    await screen.findByText("b-vnc");
    expect(screen.getByRole("link", { name: "CONNECT" })).toHaveAttribute("href", "/session/b-vnc");
  });
});
