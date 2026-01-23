import React, { useEffect, useRef, useState } from "react";
import { useParams, Link, useNavigate, useLocation } from "react-router-dom";
import RFB from "@novnc/novnc/lib/rfb";
import { formatUptime } from "../utils";
import copyToVncIcon from "../assets/icons/clipboard-paste.svg";
import copyFromVncIcon from "../assets/icons/clipboard-copy.svg";
import sendIcon from "../assets/icons/send.svg";
import closeIcon from "../assets/icons/circle-x.svg";

type AnyRFB = any;
const buildNumber = __BUILD_NUMBER__;

export const VNCView: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const location = useLocation();

  const containerRef = useRef<HTMLDivElement | null>(null);
  const rfbRef = useRef<AnyRFB | null>(null);
  const clipboardInputRef = useRef<HTMLTextAreaElement | null>(null);

  const initialStart = (location.state as any)?.sessionStartTime ?? null;

  const [status, setStatus] = useState<"Connecting" | "Connected" | "Disconnected">("Connecting");
  const [isMaximized, setIsMaximized] = useState(false);
  const [sessionStartTime, setSessionStartTime] = useState<string | null>(initialStart);
  const [uptime, setUptime] = useState("0s");
  const [lastRemoteClipboard, setLastRemoteClipboard] = useState<string>("");
  const [clipboardHint, setClipboardHint] = useState<string>("");
  const [showPastePrompt, setShowPastePrompt] = useState(false);
  const [pasteText, setPasteText] = useState("");
  const pasteInputRef = useRef<HTMLTextAreaElement | null>(null);

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
    rfb.addEventListener("clipboard", (event: any) => {
      const text = event?.detail?.text ?? "";
      setLastRemoteClipboard(text);
    });

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

  const readFromClipboard = async () => {
    if (!clipboardInputRef.current) return "";
    const input = clipboardInputRef.current;

    if (navigator.clipboard?.readText) {
      try {
        return await navigator.clipboard.readText();
      } catch {
      }
    }

    input.value = "";
    input.focus();
    input.select();
    return new Promise<string>((resolve) => {
      const handlePaste = (event: ClipboardEvent) => {
        const text = event.clipboardData?.getData("text/plain") ?? "";
        input.removeEventListener("paste", handlePaste);
        resolve(text);
      };

      input.addEventListener("paste", handlePaste, { once: true });
    });
  };

  const writeToClipboard = async (text: string) => {
    if (!clipboardInputRef.current) return;
    const input = clipboardInputRef.current;

    if (navigator.clipboard?.writeText) {
      try {
        await navigator.clipboard.writeText(text);
        return;
      } catch {
      }
    }

    input.value = text;
    input.focus();
    input.select();
    try {
      document.execCommand("copy");
    } catch {
    }
  };

  const handleCopyFromClipboard = async () => {
    if (!rfbRef.current) return;
    if (navigator.clipboard?.readText) {
      setClipboardHint("Reading local clipboard...");
      setTimeout(() => setClipboardHint(""), 2000);
      const text = await readFromClipboard();
      if (text) {
        rfbRef.current.clipboardPasteFrom(text);
        try {
          rfbRef.current.sendKey(0xFFE3, "ControlLeft", true);
          rfbRef.current.sendKey(0x0076, "KeyV", true);
          rfbRef.current.sendKey(0x0076, "KeyV", false);
          rfbRef.current.sendKey(0xFFE3, "ControlLeft", false);
        } catch {
        }
        setClipboardHint("Pasted to VNC");
        setTimeout(() => setClipboardHint(""), 2000);
      }
      return;
    }

    setPasteText("");
    setShowPastePrompt(true);
  };

  const handleCopyToClipboard = async () => {
    if (!lastRemoteClipboard) return;
    setClipboardHint("Copied from VNC");
    setTimeout(() => setClipboardHint(""), 2000);
    await writeToClipboard(lastRemoteClipboard);
  };

  useEffect(() => {
    if (!showPastePrompt) return;
    const input = pasteInputRef.current;
    if (!input) return;
    input.focus();
    input.select();
  }, [showPastePrompt]);

  const submitPasteText = (text: string) => {
    const value = text.trim();
    if (value && rfbRef.current?.clipboardPasteFrom) {
      rfbRef.current.clipboardPasteFrom(value);
      try {
        rfbRef.current.sendKey(0xFFE3, "ControlLeft", true);
        rfbRef.current.sendKey(0x0076, "KeyV", true);
        rfbRef.current.sendKey(0x0076, "KeyV", false);
        rfbRef.current.sendKey(0xFFE3, "ControlLeft", false);
      } catch {
      }
      setClipboardHint("Pasted to VNC");
      setTimeout(() => setClipboardHint(""), 2000);
    }
    setShowPastePrompt(false);
    setPasteText("");
  };

  const handlePastePromptChange = (event: React.ChangeEvent<HTMLTextAreaElement>) => {
    const value = event.target.value;
    setPasteText(value);
  };

  const handlePastePromptPaste = (event: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const text = event.clipboardData?.getData("text/plain") ?? "";
    event.preventDefault();
    setPasteText(text);
  };

  const handlePastePromptCancel = () => {
    setShowPastePrompt(false);
    setPasteText("");
  };

  const handlePastePromptCopy = async () => {
    const value = pasteInputRef.current?.value ?? pasteText;
    if (!value) return;
    if (rfbRef.current?.clipboardPasteFrom) {
      rfbRef.current.clipboardPasteFrom(value);
      setClipboardHint("Copied to VNC");
      setTimeout(() => setClipboardHint(""), 2000);
    }
    setShowPastePrompt(false);
    setPasteText("");
  };

  return (
    <>
      <header className="app-header">
        <div className="header-title">
          SELENOSIS-UI <span className="build-version">{buildNumber}</span>
        </div>
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
            <div className="window-actions">
              <button
                type="button"
                className="clipboard-btn"
                onClick={handleCopyFromClipboard}
                disabled={status !== "Connected"}
                aria-label="Copy to VNC"
                title="Copy to VNC"
              >
                <img className="clipboard-icon" src={copyToVncIcon} alt="" />
              </button>
              <button
                type="button"
                className="clipboard-btn"
                onClick={handleCopyToClipboard}
                disabled={status !== "Connected"}
                aria-label="Copy from VNC"
                title="Copy from VNC"
              >
                <img className="clipboard-icon" src={copyFromVncIcon} alt="" />
              </button>
              {showPastePrompt && (
                <div className="clipboard-prompt">
                  <div className="clipboard-prompt-title">Paste text to send to remote</div>
                  <textarea
                    ref={pasteInputRef}
                    className="clipboard-prompt-input"
                    value={pasteText}
                    onChange={handlePastePromptChange}
                    onPaste={handlePastePromptPaste}
                    rows={3}
                  />
                  <div className="clipboard-prompt-actions">
                    <button
                      type="button"
                      className="clipboard-btn"
                      onClick={handlePastePromptCopy}
                      aria-label="Send"
                      title="Send"
                    >
                      <img className="clipboard-icon" src={sendIcon} alt="" />
                    </button>
                    <button
                      type="button"
                      className="clipboard-btn"
                      onClick={handlePastePromptCancel}
                      aria-label="Close"
                      title="Close"
                    >
                      <img className="clipboard-icon" src={closeIcon} alt="" />
                    </button>
                  </div>
                </div>
              )}
            </div>
          </header>

          <div className="vnc-container" ref={containerRef} tabIndex={-1}>
            {status !== "Connected" && (
              <div className="vnc-placeholder">
                {status === "Connecting" ? "Session Loading..." : "Session Disconnected"}
              </div>
            )}
            {clipboardHint && (
              <div className="clipboard-hint">
                {clipboardHint}
              </div>
            )}
          </div>
          <textarea
            ref={clipboardInputRef}
            tabIndex={-1}
            style={{
              position: "absolute",
              left: "-9999px",
              top: "0",
              width: "1px",
              height: "1px",
              opacity: 0,
              pointerEvents: "none",
            }}
          />

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
