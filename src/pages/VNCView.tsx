import React, { useEffect, useRef, useState } from "react";
import { useParams, Link, useNavigate, useLocation } from "react-router-dom";
import RFB from "@novnc/novnc/lib/rfb";
import { formatUptime } from "../utils";

type AnyRFB = any;

export const VNCView: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const location = useLocation();

  const containerRef = useRef<HTMLDivElement | null>(null);
  const rfbRef = useRef<AnyRFB | null>(null);

  const initialStart = (location.state as any)?.sessionStartTime ?? null;

  const [status, setStatus] = useState<"Connecting" | "Connected" | "Disconnected">("Connecting");
  const [isMaximized, setIsMaximized] = useState(false);
  const [sessionStartTime, setSessionStartTime] = useState<string | null>(initialStart);
  const [uptime, setUptime] = useState("0s");

  useEffect(() => {
    if (!sessionStartTime) return;

    const update = () => setUptime(formatUptime(sessionStartTime));
    update();

    const interval = setInterval(() => {
      if (status === "Disconnected") return;
      update();
    }, 1000);

    return () => clearInterval(interval);
  }, [sessionStartTime, status]);

  useEffect(() => {
    if (!id) return;

    if (sessionStartTime) return;

    fetch(`/api/v1/browsers/${id}`)
      .then((r) => {
        if (!r.ok) throw new Error('Failed to fetch session');
        return r.json();
      })
      .then((session: { browserId: string; startTime: string }) => {
        if (session.browserId === id) {
          setSessionStartTime(session.startTime);
        }
      })
      .catch(() => {
      });
  }, [id, sessionStartTime]);

  useEffect(() => {
    if (!id || !containerRef.current) return;
    if (rfbRef.current) return;

    const protocol = window.location.protocol === "https:" ? "wss" : "ws";
    const host = window.location.host;
    const url = `${protocol}://${host}/api/v1/browsers/${id}/vnc`;

    const rfb: AnyRFB = new (RFB as any)(containerRef.current, url, {
      scaleViewport: true,
      resizeSession: false,
    });

    rfb.addEventListener("credentialsrequired", async () => {
      try {
        const resp = await fetch(`/api/v1/browsers/${id}/vnc/settings`);
        if (!resp.ok) throw new Error("failed to get password");
        const { password } = await resp.json();

        if (typeof rfb.sendCredentials === "function") {
          rfb.sendCredentials({ password });
        } else {
          (rfb as any)._rfb_credentials = { password };
        }
      } catch {
        setStatus("Disconnected");
        try { rfb.disconnect(); } catch {}
      }
    });


    rfb.viewOnly = false;
    rfb.localCursor = true;
    rfb.clipViewport = false;
    rfb.viewportDrag = false;

    rfb.addEventListener("connect", () => setStatus("Connected"));
    rfb.addEventListener("disconnect", () => setStatus("Disconnected"));

    rfbRef.current = rfb;

    const resizeCanvas = () => {
      if (!rfbRef.current || !containerRef.current) return;

      const rect = containerRef.current.getBoundingClientRect();
      const w = Math.round(rect.width);
      const h = Math.round(rect.height);

      try {
        if (isMaximized && typeof rfbRef.current.requestDesktopSize === "function") {
          rfbRef.current.requestDesktopSize(w, h);
        } else {
          rfbRef.current.scaleViewport = true;
        }
      } catch { }
    };

    window.addEventListener("resize", resizeCanvas);
    setTimeout(resizeCanvas, 200);

    const handleUnload = () => {
      try {
        rfbRef.current?.disconnect();
      } catch { }
    };

    window.addEventListener("beforeunload", handleUnload);

    return () => {
      window.removeEventListener("resize", resizeCanvas);
      window.removeEventListener("beforeunload", handleUnload);
    };
  }, [id]);

  useEffect(() => {
    if (!rfbRef.current || !containerRef.current) return;

    const rect = containerRef.current.getBoundingClientRect();
    const w = Math.round(rect.width);
    const h = Math.round(rect.height);

    try {
      if (isMaximized && typeof rfbRef.current.requestDesktopSize === "function") {
        rfbRef.current.requestDesktopSize(w, h);
      } else {
        rfbRef.current.scaleViewport = true;
      }
    } catch { }
  }, [isMaximized]);

  const handleClose = () => {
    try {
      rfbRef.current?.disconnect();
    } catch { }

    navigate("/ui/");
  };

  return (
    <>
      <header className="app-header">
        <div className="header-title">SELENOSIS-UI</div>
      </header>

      <div className="vnc-wrapper">
        <section className={`vnc-window ${isMaximized ? "maximized" : ""}`}>
          <header className="window-header">
            <div className="window-controls">
              <button onClick={handleClose} className="control-btn close-btn" title="Close" />
              <button
                onClick={() => setIsMaximized((s) => !s)}
                className={`control-btn maximize-btn fullscreen-btn ${isMaximized ? "fullscreen-active" : ""}`}
                title="Fullscreen"
              />
            </div>
            <h1 className="window-title">VNC Session — {id}</h1>
          </header>

          <div className="vnc-container" ref={containerRef} tabIndex={-1}>
            {status !== "Connected" && (
              <div className="vnc-placeholder">
                {status === "Connecting" ? "Session Loading..." : "Session Disconnected"}
              </div>
            )}
          </div>

          <footer className="vnc-status-bar">
            <div>Uptime: <time>{uptime}</time></div>
            <div className={status === "Connected" ? "status-connected" : "status-disconnected"}>
              {status}
            </div>
          </footer>
        </section>
      </div>
    </>
  );
};

export default VNCView;
