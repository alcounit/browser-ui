import React from "react";
import { useQuery, useMutation } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router-dom';
import { formatUptime, getBrowserIcon } from '../utils';
import { useAuth } from '../App';
import { SessionTypeBadge } from '../components/SessionTypeBadge';
import { ToastHost, useToasts } from '../components/Toast';
import { readTypeFilter, writeTypeFilter } from '../lib/sessionFilter';
import { buildCatalog, creatableGroups, flattenBrowsers, startBrowserError, type SessionGroup, type SupportedBrowsers } from '../lib/browserCatalog';
import { sessionTypeLabel } from '../lib/sessionType';

const buildNumber = __BUILD_NUMBER__;

interface Session {
  sessionId: string;
  browserId: string;
  browserName: string;
  browserVersion: string;
  startTime: string;
  phase: 'Running' | 'Pending' | 'Failed' | 'Succeeded';
  startedManually: boolean;
  sessionType?: string;
  vnc?: boolean;
}

interface StatusResponse {
  activeSessions: Session[];
  supportedBrowsers: SupportedBrowsers[];
}

const fetchStatus = async (onUnauthorized: () => void): Promise<StatusResponse> => {
  const res = await fetch('/api/v1/status');
  if (res.status === 401) { onUnauthorized(); throw new Error('unauthorized'); }
  if (!res.ok) throw new Error('Network response was not ok');
  return res.json();
};

const startBrowser = async (payload: { browserName: string; browserVersion: string }): Promise<{ browserId: string }> => {
  const res = await fetch('/api/v1/browsers', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!res.ok) throw await startBrowserError(res);
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
  const { authEnabled, onUnauthorized } = useAuth();

  const { data, isLoading } = useQuery({
    queryKey: ['status'],
    queryFn: () => fetchStatus(onUnauthorized),
    refetchInterval: 2000,
  });

  const logoutMutation = useMutation({
    mutationFn: async () => {
      await fetch('/api/v1/auth/logout', { method: 'POST' });
    },
    onSuccess: () => navigate('/ui/login', { replace: true }),
  });
  const browsers = data?.activeSessions ?? [];

  const [, setNow] = React.useState(Date.now());
  const [dropdownOpen, setDropdownOpen] = React.useState(false);
  const [expandedGroup, setExpandedGroup] = React.useState<string | null>(null);
  const [expandedBrowser, setExpandedBrowser] = React.useState<string | null>(null);
  const [startingKey, setStartingKey] = React.useState<string | null>(null);
  const [deletingIds, setDeletingIds] = React.useState<Set<string>>(new Set());
  const wrapperRef = React.useRef<HTMLDivElement>(null);
  const { toasts, push, dismiss } = useToasts();
  const [typeFilter, setTypeFilter] = React.useState<string | null>(() => readTypeFilter());

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
        setExpandedGroup(null);
        setExpandedBrowser(null);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [dropdownOpen]);

  const sessionGroups: SessionGroup[] = React.useMemo(
    () => buildCatalog(data?.supportedBrowsers ?? []),
    [data],
  );

  const browserGroups = React.useMemo(() => flattenBrowsers(sessionGroups), [sessionGroups]);

  const startGroups = React.useMemo(() => creatableGroups(sessionGroups), [sessionGroups]);

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
      setExpandedGroup(null);
      setExpandedBrowser(null);
      navigate(`/session/${session.browserId}`);
    },
    onError: (err: Error, variables) => {
      setStartingKey(null);
      push(`Failed to start ${variables.browserName} ${variables.browserVersion}`, err.message);
    },
  });

  const startMutate = mutation.mutate;

  const handleSelect = React.useCallback((name: string, version: string) => {
    const key = `${name}:${version}`;
    setStartingKey(key);
    startMutate({ browserName: name, browserVersion: version });
  }, [startMutate]);

  const toggleDropdown = React.useCallback(() => {
    setExpandedGroup(null);
    setExpandedBrowser(null);
    setDropdownOpen(o => !o);
  }, []);

  const toggleGroup = React.useCallback((sessionType: string) => {
    setExpandedBrowser(null);
    setExpandedGroup(cur => cur === sessionType ? null : sessionType);
  }, []);

  const toggleBrowser = React.useCallback((key: string) => {
    setExpandedBrowser(cur => cur === key ? null : key);
  }, []);

  // ── stats ────────────────────────────────────────────────────────────────────

  const sortedBrowsers = React.useMemo(() =>
    browsers
      .filter(b => typeFilter === null || sessionTypeLabel(b.sessionType) === typeFilter)
      .sort((a, b) => new Date(b.startTime).getTime() - new Date(a.startTime).getTime()),
    [browsers, typeFilter]);

  const toggleTypeFilter = React.useCallback((sessionType: string) => {
    setTypeFilter(cur => {
      const next = cur === sessionType ? null : sessionType;
      writeTypeFilter(next);
      return next;
    });
  }, []);

  React.useEffect(() => {
    if (typeFilter === null || isLoading) return;
    if (browsers.some(b => sessionTypeLabel(b.sessionType) === typeFilter)) return;
    setTypeFilter(null);
    writeTypeFilter(null);
  }, [browsers, typeFilter, isLoading]);

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
      <ToastHost toasts={toasts} onDismiss={dismiss} />

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
              {startGroups.length === 0 && (
                <div className="create-browser-empty">
                  {sessionGroups.length === 0 ? 'No browsers configured' : 'No Selenium browsers configured'}
                </div>
              )}

              {startGroups.map(sessionGroup => {
                return (
                  <div key={sessionGroup.sessionType}>
                    <button
                      className="session-group-header"
                      onClick={() => toggleGroup(sessionGroup.sessionType)}
                    >
                      <span className="session-group-title">{sessionGroup.sessionType}</span>
                      <span className={`group-chevron ${expandedGroup === sessionGroup.sessionType ? 'open' : ''}`}>▶</span>
                    </button>

                    {expandedGroup === sessionGroup.sessionType && (
                      <>
                        {sessionGroup.browsers.map(browser => {
                          const key = `${sessionGroup.sessionType}:${browser.name}`;

                          return (
                            <div key={key}>
                              <button
                                className="browser-group-header"
                                onClick={() => toggleBrowser(key)}
                              >
                                <span className="create-browser-icon">{getBrowserIcon(browser.name)}</span>
                                <span className="browser-group-name">{browser.name}</span>
                                <span className={`group-chevron ${expandedBrowser === key ? 'open' : ''}`}>▶</span>
                              </button>

                              {expandedBrowser === key && (
                                <VersionList
                                  versions={browser.versions}
                                  browserName={browser.name}
                                  startingKey={startingKey}
                                  onSelect={handleSelect}
                                />
                              )}
                            </div>
                          );
                        })}
                      </>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </div>

        {authEnabled && (
          <button
            className="logout-btn"
            onClick={() => logoutMutation.mutate()}
            disabled={logoutMutation.isPending}
          >
            {logoutMutation.isPending ? 'Signing out…' : 'Sign out'}
          </button>
        )}

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

                <div className="session-type-row">
                  <SessionTypeBadge
                    type={browser.sessionType}
                    active={sessionTypeLabel(browser.sessionType) === typeFilter}
                    onClick={toggleTypeFilter}
                  />
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
                  {browser.vnc === false ? (
                    <button
                      className="vnc-button disabled"
                      disabled
                      title="VNC is not available for this browser"
                    >
                      CONNECT
                    </button>
                  ) : browser.phase === 'Running' && !isDeleting ? (
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
