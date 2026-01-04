import React, { useCallback, useEffect, useRef, useState } from "react";
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { formatUptime, getBrowserIcon } from '../utils';

interface Session {
  sessionId: string;
  browserId: string;
  browserName: string;
  browserVersion: string;
  startTime: string;
  phase: 'Running' | 'Pending' | 'Failed' | 'Succeeded';
}

const fetchBrowsers = async (): Promise<Session[]> => {
  const res = await fetch('/api/v1/browsers');
  if (!res.ok) throw new Error('Network response was not ok');
  return res.json();
};

export const Dashboard: React.FC = () => {
  const { data: browsers = [], isLoading } = useQuery({
    queryKey: ['browsers'],
    queryFn: fetchBrowsers,
    refetchInterval: 2000,
  });

  const [now, setNow] = React.useState(Date.now());

  React.useEffect(() => {
    const interval = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(interval);
  }, []);

  const sortedBrowsers = React.useMemo(() => {
    return [...browsers].sort(
      (a, b) => new Date(b.startTime).getTime() - new Date(a.startTime).getTime()
    );
  }, [browsers]);

  const stats = React.useMemo(() => {
    const map = new Map<string, number>();

    browsers.forEach(s => {
      const name = s.browserName;
      map.set(name, (map.get(name) || 0) + 1);
    });

    const sorted = [...map.entries()]
      .sort((a, b) => b[1] - a[1])
      .slice(0, 4);

    return {
      total: browsers.length,
      browsers: sorted
    };
  }, [browsers]);

  return (
    <>
      <header className="app-header">
        <div className="header-title">SELENOSIS-UI</div>
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
            {sortedBrowsers.map((browser) => (
              <div
                key={browser.browserId}
                className={`browser-card ${browser.phase !== 'Running' ? 'disabled' : ''}`}
              >
                <h2 className="browser-uuid" title={browser.browserId}>{browser.browserId}</h2>

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
                  {browser.phase === 'Running' ? (
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
            ))}
          </div>
        )}
      </main>
    </>
  );
};