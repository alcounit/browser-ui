import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, screen, waitFor, act, cleanup, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Routes, Route } from "react-router-dom";
import { VNCView } from "./VNCView";
import { NAME_PREFIX, VERSION_PREFIX } from "../lib/vncStore";

const navigateMock = vi.fn();

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual<typeof import("react-router-dom")>("react-router-dom");
  return { ...actual, useNavigate: () => navigateMock };
});

const rfb = vi.hoisted(() => {
  const instances: any[] = [];
  class FakeRFB {
    url: string;
    options: any;
    listeners: Record<string, Array<(e: any) => void>> = {};
    sendCredentials = vi.fn();
    disconnect = vi.fn();
    clipboardPasteFrom = vi.fn();
    sendKey = vi.fn();
    requestDesktopSize = vi.fn();
    scaleViewport = false;
    viewOnly = false;
    localCursor = false;
    clipViewport = false;
    viewportDrag = false;
    constructor(_target: any, url: string, options: any) {
      this.url = url;
      this.options = options;
      instances.push(this);
    }
    addEventListener(type: string, cb: (e: any) => void) {
      (this.listeners[type] ||= []).push(cb);
    }
    emit(type: string, detail?: any) {
      (this.listeners[type] || []).forEach((cb) => cb({ detail }));
    }
  }
  return { instances, FakeRFB };
});

vi.mock("@novnc/novnc/lib/rfb", () => ({ default: rfb.FakeRFB }));
vi.mock("../assets/icons/clipboard-paste.svg", () => ({ default: "paste.svg" }));
vi.mock("../assets/icons/clipboard-copy.svg", () => ({ default: "copy.svg" }));
vi.mock("../assets/icons/send.svg", () => ({ default: "send.svg" }));
vi.mock("../assets/icons/circle-x.svg", () => ({ default: "x.svg" }));

function mockFetch(opts: { browserName?: string | undefined; browserVersion?: string; startTime?: string; password?: string | null; sessionOk?: boolean }) {
  if (opts.password && opts.browserName) {
    localStorage.setItem(NAME_PREFIX + opts.browserName, opts.password);
  }
  global.fetch = vi.fn(async () => {
    return {
      ok: opts.sessionOk !== false,
      json: async () => ({ browserId: "id-1", browserName: opts.browserName, browserVersion: opts.browserVersion ?? "123", startTime: opts.startTime ?? "2020-01-01T00:00:00Z" }),
    } as any;
  }) as any;
}

function renderView(id = "id-1") {
  return render(
    <MemoryRouter initialEntries={[`/ui/sessions/${id}`]}>
      <Routes>
        <Route path="/ui/sessions/:id" element={<VNCView />} />
      </Routes>
    </MemoryRouter>,
  );
}

const latest = () => rfb.instances[rfb.instances.length - 1];
const emit = (type: string, detail?: any) => act(() => latest().emit(type, detail));

beforeEach(() => {
  rfb.instances.length = 0;
  navigateMock.mockReset();
  localStorage.clear();
  sessionStorage.clear();
  vi.restoreAllMocks();
});

afterEach(() => {
  cleanup();
});

describe("auto-connect with a saved password", () => {
  it("sends the saved-by-name password and reports connected", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    renderView();

    await waitFor(() => expect(rfb.instances.length).toBe(1));
    expect(latest().url).toContain("/api/v1/browsers/id-1/vnc");

    await emit("credentialsrequired");
    expect(latest().sendCredentials).toHaveBeenCalledWith({ password: "secret" });

    await emit("connect");
    expect(screen.getByText("Connected")).toBeInTheDocument();
  });

  it("uses _rfb_credentials when sendCredentials is unavailable", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));

    (latest() as any).sendCredentials = undefined;
    await emit("credentialsrequired");
    expect((latest() as any)._rfb_credentials).toEqual({ password: "secret" });
  });
});

describe("saved-password retry ladder", () => {
  it("tries the version-saved password first, then falls through to the name-saved one on securityfailure", async () => {
    localStorage.setItem(VERSION_PREFIX + "chrome@123", "byversion");
    localStorage.setItem(NAME_PREFIX + "chrome", "byname");
    mockFetch({ browserName: "chrome", browserVersion: "123" });
    renderView();

    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("credentialsrequired");
    expect(latest().sendCredentials).toHaveBeenCalledWith({ password: "byversion" });

    await emit("securityfailure", { reason: "bad" });
    await waitFor(() => expect(rfb.instances.length).toBe(2));

    await emit("credentialsrequired");
    expect(latest().sendCredentials).toHaveBeenCalledWith({ password: "byname" });
  });
});

describe("fetch failures", () => {
  it("prompts for manual entry when the session lookup fails", async () => {
    mockFetch({ browserName: "chrome", sessionOk: false, startTime: "" });
    renderView();

    await waitFor(() => expect(screen.getByLabelText("VNC password")).toBeInTheDocument());
    expect(rfb.instances.length).toBe(0);
  });
});

describe("manual entry", () => {
  it("prompts when no candidate exists, connects, and saves for all by name", async () => {
    mockFetch({ browserName: "chrome", password: null });
    renderView();

    await waitFor(() => expect(screen.getByLabelText("VNC password")).toBeInTheDocument());
    expect(rfb.instances.length).toBe(0);

    await userEvent.type(screen.getByLabelText("VNC password"), "typed");
    await userEvent.click(screen.getByRole("button", { name: "Connect" }));

    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("credentialsrequired");
    expect(latest().sendCredentials).toHaveBeenCalledWith({ password: "typed" });

    await emit("connect");
    await userEvent.click(screen.getByRole("button", { name: 'Save for all "chrome"' }));
    expect(localStorage.getItem(NAME_PREFIX + "chrome")).toBe("typed");
  });

  it("saves for the current browser version", async () => {
    mockFetch({ browserName: "chrome", browserVersion: "123", password: null });
    renderView();
    await waitFor(() => expect(screen.getByLabelText("VNC password")).toBeInTheDocument());

    await userEvent.type(screen.getByLabelText("VNC password"), "typed");
    await userEvent.click(screen.getByRole("button", { name: "Connect" }));
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");

    await userEvent.click(screen.getByRole("button", { name: 'Only "chrome 123"' }));
    expect(localStorage.getItem(VERSION_PREFIX + "chrome@123")).toBe("typed");
  });

  it("prefers the version-saved password over the name-saved one", async () => {
    localStorage.setItem(NAME_PREFIX + "chrome", "byname");
    localStorage.setItem(VERSION_PREFIX + "chrome@123", "byversion");
    mockFetch({ browserName: "chrome", browserVersion: "123" });
    renderView();

    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("credentialsrequired");
    expect(latest().sendCredentials).toHaveBeenCalledWith({ password: "byversion" });
  });

  it("does not persist when the user declines to save", async () => {
    mockFetch({ browserName: "chrome", password: null });
    renderView();
    await waitFor(() => expect(screen.getByLabelText("VNC password")).toBeInTheDocument());

    await userEvent.type(screen.getByLabelText("VNC password"), "typed");
    await userEvent.click(screen.getByRole("button", { name: "Connect" }));
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");

    await userEvent.click(screen.getByRole("button", { name: "Don't save" }));
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
  });

  it("cancelling the prompt disconnects with a notice", async () => {
    mockFetch({ browserName: "chrome", password: null });
    renderView();
    await waitFor(() => expect(screen.getByLabelText("VNC password")).toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.getByText("VNC authentication cancelled")).toBeInTheDocument();
  });

  it("does not require a browser name to fall back to manual save scope name", async () => {
    mockFetch({ browserName: undefined, password: null });
    renderView();
    await waitFor(() => expect(screen.getByLabelText("VNC password")).toBeInTheDocument());

    await userEvent.type(screen.getByLabelText("VNC password"), "typed");
    await userEvent.click(screen.getByRole("button", { name: "Connect" }));
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");

    await userEvent.click(screen.getByRole("button", { name: 'Save for all ""' }));
    expect(localStorage.length).toBe(0);
  });
});

describe("securityfailure handling", () => {
  it("exhausting the auto ladder opens the manual prompt with the failure reason", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));

    await emit("securityfailure", { reason: "wrong password" });
    await waitFor(() => expect(screen.getByText("VNC auth failed: wrong password")).toBeInTheDocument());
  });

  it("stops at the hard cap when manual entries keep failing", async () => {
    mockFetch({ browserName: "chrome", password: "p0" });
    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));

    await emit("securityfailure");
    await waitFor(() => expect(screen.getByLabelText("VNC password")).toBeInTheDocument());

    for (let i = 1; i <= 4; i++) {
      await userEvent.type(screen.getByLabelText("VNC password"), `m${i}`);
      await userEvent.click(screen.getByRole("button", { name: "Connect" }));
      await waitFor(() => expect(rfb.instances.length).toBe(i + 1));
      await emit("securityfailure", { reason: "nope" });
    }

    await waitFor(() => expect(screen.getByText("VNC auth failed: nope")).toBeInTheDocument());
    expect(screen.queryByLabelText("VNC password")).not.toBeInTheDocument();
  });
});

describe("disconnect handling", () => {
  it("reports connection lost on an unclean disconnect", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));

    await emit("disconnect", { clean: false });
    expect(screen.getByText("Connection lost")).toBeInTheDocument();
  });

  it("ignores the disconnect that follows a securityfailure", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));

    await emit("securityfailure", { reason: "bad" });
    const firstInstance = rfb.instances[0];
    await act(() => firstInstance.emit("disconnect", { clean: false }));
    expect(screen.queryByText("Connection lost")).not.toBeInTheDocument();
  });

  it("shows a clean disconnect as disconnected", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");
    await emit("disconnect", { clean: true });
    expect(screen.getAllByText("Disconnected").length).toBeGreaterThan(0);
  });
});

describe("window controls and clipboard", () => {
  it("disconnects and navigates home on close", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    const inst = latest();

    await userEvent.click(screen.getByTitle("Close"));
    expect(inst.disconnect).toHaveBeenCalled();
    expect(navigateMock).toHaveBeenCalledWith("/ui/");
  });

  it("requests desktop size when maximized", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");

    await userEvent.click(screen.getByTitle("Fullscreen"));
    expect(latest().requestDesktopSize).toHaveBeenCalled();
  });

  it("scales the viewport on window resize when not maximized", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");

    latest().scaleViewport = false;
    await act(() => {
      window.dispatchEvent(new Event("resize"));
    });
    expect(latest().scaleViewport).toBe(true);
  });

  it("copies remote clipboard to local when available", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });

    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");
    await emit("clipboard", { text: "remote-text" });

    await userEvent.click(screen.getByTitle("Copy from VNC"));
    expect(writeText).toHaveBeenCalledWith("remote-text");
  });

  it("pastes local clipboard into the session when readText is available", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    const readText = vi.fn().mockResolvedValue("local-text");
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { readText } });

    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");

    await userEvent.click(screen.getByTitle("Copy to VNC"));
    await waitFor(() => expect(latest().clipboardPasteFrom).toHaveBeenCalledWith("local-text"));
  });

  it("falls back to the paste prompt when readText is unavailable", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: {} });

    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");

    await userEvent.click(screen.getByTitle("Copy to VNC"));
    const prompt = screen.getByText("Paste text to send to remote");
    expect(prompt).toBeInTheDocument();

    const input = document.querySelector(".clipboard-prompt-input") as HTMLTextAreaElement;
    await userEvent.type(input, "manual paste");
    await userEvent.click(screen.getByTitle("Send"));
    expect(latest().clipboardPasteFrom).toHaveBeenCalledWith("manual paste");
  });

  it("cancels the paste prompt", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: {} });

    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");

    await userEvent.click(screen.getByTitle("Copy to VNC"));
    const promptActions = document.querySelector(".clipboard-prompt-actions") as HTMLElement;
    await userEvent.click(within(promptActions).getByTitle("Close"));
    await waitFor(() => expect(screen.queryByText("Paste text to send to remote")).not.toBeInTheDocument());
  });

  it("does nothing when copying from an empty remote clipboard", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    const writeText = vi.fn();
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });

    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");

    await userEvent.click(screen.getByTitle("Copy from VNC"));
    expect(writeText).not.toHaveBeenCalled();
  });

  it("writes to the clipboard via execCommand fallback when writeText is unavailable", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: {} });
    const execCommand = vi.fn().mockReturnValue(true);
    (document as any).execCommand = execCommand;

    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");
    await emit("clipboard", { text: "remote-text" });

    await userEvent.click(screen.getByTitle("Copy from VNC"));
    await waitFor(() => expect(execCommand).toHaveBeenCalledWith("copy"));
  });

  it("falls back to execCommand when writeText rejects", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    const writeText = vi.fn().mockRejectedValue(new Error("denied"));
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    const execCommand = vi.fn(() => {
      throw new Error("execCommand unsupported");
    });
    (document as any).execCommand = execCommand;

    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");
    await emit("clipboard", { text: "remote-text" });

    await userEvent.click(screen.getByTitle("Copy from VNC"));
    await waitFor(() => expect(execCommand).toHaveBeenCalledWith("copy"));
  });

  it("reads via the textarea fallback when readText rejects, tolerating sendKey errors", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    const readText = vi.fn().mockRejectedValue(new Error("denied"));
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { readText } });

    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");
    latest().sendKey = vi.fn(() => {
      throw new Error("boom");
    });

    const hidden = document.querySelector('textarea[tabindex="-1"]') as HTMLTextAreaElement;
    let pasteHandler: ((e: any) => void) | undefined;
    const origAdd = hidden.addEventListener.bind(hidden);
    vi.spyOn(hidden, "addEventListener").mockImplementation((type: string, handler: any, opts?: any) => {
      if (type === "paste") pasteHandler = handler;
      return origAdd(type, handler, opts);
    });

    void userEvent.click(screen.getByTitle("Copy to VNC"));
    await waitFor(() => expect(pasteHandler).toBeDefined());

    await act(async () => {
      pasteHandler!({ clipboardData: { getData: () => "pasted-text" } });
    });

    await waitFor(() => expect(latest().clipboardPasteFrom).toHaveBeenCalledWith("pasted-text"));
  });

  it("does not paste when the local clipboard is empty", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    const readText = vi.fn().mockResolvedValue("");
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { readText } });

    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");

    await userEvent.click(screen.getByTitle("Copy to VNC"));
    await waitFor(() => expect(readText).toHaveBeenCalled());
    expect(latest().clipboardPasteFrom).not.toHaveBeenCalled();
  });

  it("captures pasted text in the paste prompt textarea", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: {} });

    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");

    await userEvent.click(screen.getByTitle("Copy to VNC"));
    const input = document.querySelector(".clipboard-prompt-input") as HTMLTextAreaElement;
    await act(async () => {
      const pasteEvent = new Event("paste", { bubbles: true }) as any;
      pasteEvent.clipboardData = { getData: () => "from-paste" };
      input.dispatchEvent(pasteEvent);
    });
    expect((input as HTMLTextAreaElement).value).toBe("from-paste");
  });

  it("toggling fullscreen off scales the viewport again", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");

    await userEvent.click(screen.getByTitle("Fullscreen"));
    latest().scaleViewport = false;
    await userEvent.click(screen.getByTitle("Fullscreen"));
    expect(latest().scaleViewport).toBe(true);
  });

  it("requests desktop size on window resize while maximized", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");

    await userEvent.click(screen.getByTitle("Fullscreen"));
    latest().requestDesktopSize.mockClear();
    await act(async () => {
      window.dispatchEvent(new Event("resize"));
    });
    expect(latest().requestDesktopSize).toHaveBeenCalled();
  });

  it("disconnects the session on page unload", async () => {
    mockFetch({ browserName: "chrome", password: "secret" });
    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    const inst = latest();

    await act(async () => {
      window.dispatchEvent(new Event("beforeunload"));
    });
    expect(inst.disconnect).toHaveBeenCalled();
  });

  it("keeps ticking the uptime while connected", async () => {
    mockFetch({ browserName: "chrome", password: "secret", startTime: new Date(Date.now() - 5000).toISOString() });
    renderView();
    await waitFor(() => expect(rfb.instances.length).toBe(1));
    await emit("connect");

    await act(async () => {
      await new Promise((r) => setTimeout(r, 1100));
    });
    expect(screen.getByText(/Uptime:/)).toBeInTheDocument();
  });
});
