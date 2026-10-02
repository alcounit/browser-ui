import React from "react";
import { useQuery, useMutation } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { getBrowserIcon } from "../utils";
import { SessionTypeBadge } from "../components/SessionTypeBadge";
import { ToastHost, useToasts } from "../components/Toast";
import { buildCatalog, creatableGroups, startBrowserError, type SessionGroup, type SupportedBrowsers } from "../lib/browserCatalog";

interface Session {
  browserId: string;
}

interface StatusResponse {
  activeSessions: unknown[];
  supportedBrowsers: SupportedBrowsers[];
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
  if (!res.ok) throw await startBrowserError(res);
  return res.json();
};

export const StartBrowser: React.FC = () => {
  const navigate = useNavigate();
  const { data, isLoading } = useQuery({
    queryKey: ["status"],
    queryFn: fetchStatus,
  });

  const allGroups: SessionGroup[] = React.useMemo(
    () => buildCatalog(data?.supportedBrowsers ?? []),
    [data],
  );

  const groups = React.useMemo(() => creatableGroups(allGroups), [allGroups]);

  const [selected, setSelected] = React.useState<Record<string, string>>({});
  const [startingBrowser, setStartingBrowser] = React.useState<string | null>(null);
  const { toasts, push, dismiss } = useToasts();

  const getVersion = React.useCallback(
    (key: string, versions: string[]) => selected[key] ?? versions[0] ?? "",
    [selected],
  );

  const mutation = useMutation({
    mutationFn: startBrowser,
    onSuccess: (session) => navigate(`/session/${session.browserId}`),
    onError: (err: Error, variables) =>
      push(`Failed to start ${variables.browserName} ${variables.browserVersion}`, err.message),
    onSettled: () => setStartingBrowser(null),
  });

  const startMutate = mutation.mutate;

  const handleStart = React.useCallback((key: string, browserName: string, browserVersion: string) => {
    setStartingBrowser(key);
    startMutate({ browserName, browserVersion });
  }, [startMutate]);

  if (isLoading) {
    return (
      <div className="start-browser-page">
        <ToastHost toasts={toasts} onDismiss={dismiss} />

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
      <ToastHost toasts={toasts} onDismiss={dismiss} />

      <header className="app-header">
        <div className="header-title">START BROWSER</div>
        <Link to="/ui/" className="header-back-link">BACK</Link>
      </header>

      <main className="main-content">
        {groups.map((group) => {
          return (
            <section key={group.sessionType} className="start-browser-section">
              <div className="start-browser-section-header">
                <span className="start-browser-section-title">{group.sessionType}</span>
                <SessionTypeBadge type={group.sessionType} />
              </div>

              <div className="start-browser-grid">
                {group.browsers.map((browser) => {
                  const key = `${group.sessionType}:${browser.name}`;
                  const version = getVersion(key, browser.versions);

                  return (
                    <div key={key} className="browser-card">
                      <div className="browser-header-row">
                        <div className="browser-icon">{getBrowserIcon(browser.name)}</div>
                        <div>
                          <div className="browser-name">{browser.name}</div>
                          <div className="browser-version">{browser.versions.length} version{browser.versions.length !== 1 ? "s" : ""}</div>
                        </div>
                      </div>

                      <div className="start-browser-version-row">
                        <label className="start-browser-label" htmlFor={`version-${key}`}>Version</label>
                        <select
                          id={`version-${key}`}
                          className="start-browser-select"
                          value={version}
                          onChange={(e) => setSelected((prev) => ({ ...prev, [key]: e.target.value }))}
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
                          onClick={() => handleStart(key, browser.name, version)}
                        >
                          {startingBrowser === key ? "STARTING…" : "START"}
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            </section>
          );
        })}

        {groups.length === 0 && (
          <div style={{ color: "#666", marginTop: 8 }}>
            {allGroups.length === 0 ? "No browsers configured." : "No Selenium browsers configured."}
          </div>
        )}
      </main>
    </div>
  );
};
