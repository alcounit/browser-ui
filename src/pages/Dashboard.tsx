import React from "react";
import { useQuery, useMutation } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router-dom';
import { formatUptime, getBrowserIcon } from '../utils';

const buildNumber = __BUILD_NUMBER__;

interface Session {
  sessionId: string;
  browserId: string;
  browserName: string;
  browserVersion: string;
  startTime: string;
  phase: 'Running' | 'Pending' | 'Failed' | 'Succeeded';
  startedManually: boolean;
}

interface StatusResponse {
  activeSessions: Session[];
  supportedBrowsers: Record<string, string[]>[];
}

interface BrowserGroup {
  name: string;
  versions: string[];
}

const fetchStatus = async (): Promise<StatusResponse> => {
  const res = await fetch('/api/v1/status');
  if (!res.ok) throw new Error('Network response was not ok');
  return res.json();
};

const startBrowser = async (payload: { browserName: string; browserVersion: string }): Promise<{ browserId: string }> => {
  const res = await fetch('/api/v1/browsers', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!res.ok) throw new Error('Failed to start browser');
  return res.json();
};

// ── VersionList ────────────────────────────────────────────────────────────────

const MAX_VISIBLE = 10;
const ITEM_H = 36;

interface VersionListProps {
  versions: string[];
  browserName: string;
  startingKey: string | null;
  onSelect: (name: string, version: string) => void;
}

const VersionList: React.FC<VersionListProps> = ({ versions, browserName, startingKey, onSelect }) => {
  const listRef = React.useRef<HTMLDivElement>(null);
  const timerRef = React.useRef<number | null>(null);

  const startScroll = (dir: number) => {
    timerRef.current = window.setInterval(() => {
      listRef.current?.scrollBy({ top: dir * 20 });
    }, 40);
  };

  const stopScroll = () => {
    if (timerRef.current !== null) {
      clearInterval(timerRef.current);
      timerRef.current = null;
    }
  };

  React.useEffect(() => stopScroll, []);

  const showArrows = versions.length > MAX_VISIBLE;
  const listMaxH = ITEM_H * Math.min(versions.length, MAX_VISIBLE);

  return (
    <div className="versions-container">
      {showArrows && (
        <button
          className="scroll-arrow scroll-arrow-up"
          onMouseEnter={() => startScroll(-1)}
          onMouseLeave={stopScroll}
          tabIndex={-1}
        >▲</button>
      )}

      <div
        ref={listRef}
        className="versions-list"
        style={{ maxHeight: `${listMaxH}px` }}
      >
        {versions.map(version => {
          const key = `${browserName}:${version}`;
          const loading = startingKey === key;
          return (
            <button
              key={version}
              className="version-item"
              disabled={startingKey !== null}
              onClick={() => onSelect(browserName, version)}
            >
              <span className="version-label">{version}</span>
              {loading && <span className="version-spinner" />}
            </button>
          );
        })}
      </div>

      {showArrows && (
        <button
          className="scroll-arrow scroll-arrow-down"
          onMouseEnter={() => startScroll(1)}
          onMouseLeave={stopScroll}
          tabIndex={-1}
        >▼</button>
      )}
    </div>
  );
};

// ── Dashboard ──────────────────────────────────────────────────────────────────

export const Dashboard: React.FC = () => {
  const navigate = useNavigate();

  const { data, isLoading } = useQuery({
    queryKey: ['status'],
    queryFn: fetchStatus,
    refetchInterval: 2000,
  });
  const browsers = data?.activeSessions ?? [];

  const [, setNow] = React.useState(Date.now());
  const [dropdownOpen, setDropdownOpen] = React.useState(false);
  const [expandedBrowser, setExpandedBrowser] = React.useState<string | null>(null);
  const [startingKey, setStartingKey] = React.useState<string | null>(null);
  const [deletingIds, setDeletingIds] = React.useState<Set<string>>(new Set());
  const wrapperRef = React.useRef<HTMLDivElement>(null);

  React.useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  // close on outside click
  React.useEffect(() => {
    if (!dropdownOpen) return;
    const handler = (e: MouseEvent) => {
      if (wrapperRef.current && !wrapperRef.current.contains(e.target as Node)) {
        setDropdownOpen(false);
        setExpandedBrowser(null);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [dropdownOpen]);

  const browserGroups: BrowserGroup[] = React.useMemo(() => {
    const map = new Map<string, Set<string>>();
    (data?.supportedBrowsers ?? []).forEach(cfg => {
      Object.entries(cfg).forEach(([name, versions]) => {
        if (!map.has(name)) map.set(name, new Set());
        versions.forEach(v => map.get(name)!.add(v));
      });
    });
    return [...map.entries()].map(([name, vSet]) => ({
      name,
      versions: [...vSet].sort((a, b) => b.localeCompare(a, undefined, { numeric: true })),
    })).sort((a, b) => a.name.localeCompare(b.name));
  }, [data]);

  const deleteMutation = useMutation({
    mutationFn: async (browserId: string) => {
      const res = await fetch(`/api/v1/browsers/${browserId}`, { method: 'DELETE' });
      if (!res.ok) throw new Error('Failed to delete browser');
    },
    onMutate: (browserId) => {
      setDeletingIds(prev => new Set(prev).add(browserId));
    },
  });

  const mutation = useMutation({
    mutationFn: startBrowser,
    onSuccess: (session) => {
      setStartingKey(null);
      setDropdownOpen(false);
      setExpandedBrowser(null);
      navigate(`/session/${session.browserId}`);
    },
    onError: () => setStartingKey(null),
  });

  const handleSelect = (name: string, version: string) => {
    const key = `${name}:${version}`;
    setStartingKey(key);
    mutation.mutate({ browserName: name, browserVersion: version });
  };

  const toggleDropdown = () => {
    setDropdownOpen(o => {
      if (o) setExpandedBrowser(null);
      return !o;
    });
  };

  const toggleBrowser = (name: string) => {
    setExpandedBrowser(cur => cur === name ? null : name);
  };

  // ── stats ────────────────────────────────────────────────────────────────────

  const sortedBrowsers = React.useMemo(() =>
    [...browsers].sort((a, b) => new Date(b.startTime).getTime() - new Date(a.startTime).getTime()),
    [browsers]);

  const stats = React.useMemo(() => {
    const sessionCount = new Map<string, number>();
    browsers.forEach(s => sessionCount.set(s.browserName, (sessionCount.get(s.browserName) || 0) + 1));
    return {
      total: browsers.length,
      browsers: browserGroups.map(g => [g.name, sessionCount.get(g.name) ?? 0] as [string, number]),
    };
  }, [browsers, browserGroups]);

  // ── render ───────────────────────────────────────────────────────────────────

  return (
    <>
      <header className="app-header">
        <div className="header-title">
          SELENOSIS-UI <span className="build-version">{buildNumber}</span>
        </div>

        <div className="create-browser-wrapper" ref={wrapperRef}>
          <button className="start-browser-btn" onClick={toggleDropdown}>
            START BROWSER
          </button>

          {dropdownOpen && (
            <div className="create-browser-dropdown">
              {browserGroups.length === 0 && (
                <div className="create-browser-empty">No browsers configured</div>
              )}

              {browserGroups.map(group => (
                <div key={group.name}>
                  <button
                    className="browser-group-header"
                    onClick={() => toggleBrowser(group.name)}
                  >
                    <span className="create-browser-icon">{getBrowserIcon(group.name)}</span>
                    <span className="browser-group-name">{group.name}</span>
                    <span className={`group-chevron ${expandedBrowser === group.name ? 'open' : ''}`}>▶</span>
                  </button>

                  {expandedBrowser === group.name && (
                    <VersionList
                      versions={group.versions}
                      browserName={group.name}
                      startingKey={startingKey}
                      onSelect={handleSelect}
                    />
                  )}
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="header-stats">
          <div className="stat-item">BROWSERS TOTAL: <strong>{stats.total}</strong></div>
          <div className="stat-item">
            {stats.browsers.map(([name, count], index) => (
              <span key={name}>
                {name.toUpperCase()}: <strong>{count}</strong>
                {index < stats.browsers.length - 1 ? ' • ' : ''}
              </span>
            ))}
          </div>
        </div>
      </header>

      <main className="main-content">
        {isLoading && browsers.length === 0 ? (
          <div style={{ textAlign: 'center', marginTop: 40, color: '#666' }}>Loading sessions...</div>
        ) : (
          <div className="container-grid">
            {sortedBrowsers.map((browser) => {
              const isDeleting = deletingIds.has(browser.browserId);
              return (
              <div
                key={browser.browserId}
                className={`browser-card ${browser.phase !== 'Running' || isDeleting ? 'disabled' : ''} ${isDeleting ? 'deleting' : ''}`}
              >
                <div className="browser-uuid-row">
                  <h2 className="browser-uuid" title={browser.browserId}>{browser.browserId}</h2>
                  {browser.startedManually && !isDeleting && (
                    <span className="manual-badge-wrapper">
                      <span className="manual-badge-label">manual</span>
                      <button
                        className="manual-badge-delete"
                        onClick={() => deleteMutation.mutate(browser.browserId)}
                      >delete</button>
                    </span>
                  )}
                </div>

                <div className="browser-header-row">
                  <div className="browser-icon">{getBrowserIcon(browser.browserName)}</div>
                  <div>
                    <div className="browser-name">{browser.browserName}</div>
                    <div className="browser-version">{browser.browserVersion}</div>
                  </div>
                </div>

                <div className="browser-details">
                  <div className="detail-row">
                    <span className="detail-label">Session ID</span>
                    <span className="detail-value browser-id" title={browser.sessionId}>
                      {browser.sessionId}
                    </span>
                  </div>
                  <div className="detail-row">
                    <span className="detail-label">Status</span>
                    <span className="detail-value">{browser.phase}</span>
                  </div>
                </div>

                <div className="browser-meta">
                  <div>Uptime: <time>{formatUptime(browser.startTime)}</time></div>
                  {browser.phase === 'Running' && !isDeleting ? (
                    <Link
                      to={`/session/${browser.browserId}`}
                      className="vnc-button"
                      state={{ sessionStartTime: browser.startTime }}
                    >
                      CONNECT
                    </Link>
                  ) : (
                    <button className="vnc-button disabled" disabled>CONNECT</button>
                  )}
                </div>
              </div>
            ); })}
          </div>
        )}
      </main>
    </>
  );
};
