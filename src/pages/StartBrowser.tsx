import React from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { getBrowserIcon } from "../utils";

interface Session {
  browserId: string;
}

interface StatusResponse {
  activeSessions: unknown[];
  supportedBrowsers: Record<string, string[]>[];
}

interface BrowserEntry {
  name: string;
  versions: string[];
}

const fetchStatus = async (): Promise<StatusResponse> => {
  const res = await fetch("/api/v1/status");
  if (!res.ok) throw new Error("Failed to fetch status");
  return res.json();
};

const startBrowser = async (payload: { browserName: string; browserVersion: string }): Promise<Session> => {
  const res = await fetch("/api/v1/browsers", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (!res.ok) throw new Error("Failed to start browser");
  return res.json();
};

export const StartBrowser: React.FC = () => {
  const navigate = useNavigate();
  const { data, isLoading } = useQuery({
    queryKey: ["status"],
    queryFn: fetchStatus,
  });

  const browsers: BrowserEntry[] = React.useMemo(() => {
    const map = new Map<string, Set<string>>();
    (data?.supportedBrowsers ?? []).forEach((cfg) => {
      Object.entries(cfg).forEach(([name, versions]) => {
        if (!map.has(name)) map.set(name, new Set());
        versions.forEach((v) => map.get(name)!.add(v));
      });
    });
    return [...map.entries()].map(([name, vSet]) => ({
      name,
      versions: [...vSet].sort((a, b) => b.localeCompare(a, undefined, { numeric: true })),
    }));
  }, [data]);

  const [selected, setSelected] = React.useState<Record<string, string>>({});
  const [startingBrowser, setStartingBrowser] = React.useState<string | null>(null);

  const getVersion = (name: string) =>
    selected[name] ?? browsers.find((b) => b.name === name)?.versions[0] ?? "";

  const mutation = useMutation({
    mutationFn: startBrowser,
    onSuccess: (session) => navigate(`/session/${session.browserId}`),
    onSettled: () => setStartingBrowser(null),
  });

  const handleStart = (browserName: string) => {
    setStartingBrowser(browserName);
    mutation.mutate({ browserName, browserVersion: getVersion(browserName) });
  };

  if (isLoading) {
    return (
      <div className="start-browser-page">
        <header className="app-header">
          <div className="header-title">START BROWSER</div>
          <Link to="/ui/" className="header-back-link">BACK</Link>
        </header>
        <main className="main-content">
          <div style={{ textAlign: "center", marginTop: 40, color: "#666" }}>Loading...</div>
        </main>
      </div>
    );
  }

  return (
    <div className="start-browser-page">
      <header className="app-header">
        <div className="header-title">START BROWSER</div>
        <Link to="/ui/" className="header-back-link">BACK</Link>
      </header>

      <main className="main-content">
        {mutation.isError && (
          <div className="start-browser-error">Failed to start browser. Please try again.</div>
        )}

        <div className="start-browser-grid">
          {browsers.map((browser) => (
            <div key={browser.name} className="browser-card">
              <div className="browser-header-row">
                <div className="browser-icon">{getBrowserIcon(browser.name)}</div>
                <div>
                  <div className="browser-name">{browser.name}</div>
                  <div className="browser-version">{browser.versions.length} version{browser.versions.length !== 1 ? "s" : ""}</div>
                </div>
              </div>

              <div className="start-browser-version-row">
                <label className="start-browser-label">Version</label>
                <select
                  className="start-browser-select"
                  value={getVersion(browser.name)}
                  onChange={(e) => setSelected((prev) => ({ ...prev, [browser.name]: e.target.value }))}
                >
                  {browser.versions.map((v) => (
                    <option key={v} value={v}>{v}</option>
                  ))}
                </select>
              </div>

              <div className="browser-meta">
                <span />
                <button
                  className="vnc-button"
                  disabled={startingBrowser !== null}
                  onClick={() => handleStart(browser.name)}
                >
                  {startingBrowser === browser.name ? "STARTING…" : "START"}
                </button>
              </div>
            </div>
          ))}

          {browsers.length === 0 && (
            <div style={{ color: "#666", marginTop: 8 }}>No browsers configured.</div>
          )}
        </div>
      </main>
    </div>
  );
};
