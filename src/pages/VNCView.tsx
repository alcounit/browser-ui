import React, { useEffect, useRef, useState } from "react";
import { useParams, useNavigate, useLocation } from "react-router-dom";
import RFB from "@novnc/novnc/lib/rfb";
import { formatUptime } from "../utils";
import {
  newAttemptState,
  buildLadder,
  nextCandidate,
  registerFailure,
  isHardCapReached,
} from "../lib/vncAuth";
import {
  getSavedByName,
  setSavedByName,
  getSavedByVersion,
  setSavedByVersion,
} from "../lib/vncStore";
import { PasswordPromptModal, SavePasswordModal, type SaveScope } from "../components/VncPasswordModals";
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

  const browserNameRef = useRef<string>("");
  const browserVersionRef = useRef<string>("");
  const attemptRef = useRef(newAttemptState());
  const ladderRef = useRef<string[]>([]);
  const pendingPwRef = useRef<string>("");
  const manualRef = useRef(false);
  const securityFailedRef = useRef(false);
  const isMaximizedRef = useRef(false);
  const createRfbRef = useRef<(password: string, manual: boolean) => void>(() => {});

  const [showPrompt, setShowPrompt] = useState(false);
  const [promptError, setPromptError] = useState<string | undefined>(undefined);
  const [showSave, setShowSave] = useState(false);
  const [notice, setNotice] = useState<string>("");

  const teardownRfb = () => {
    const current = rfbRef.current;
    rfbRef.current = null;
    if (current) {
      try {
        current.disconnect();
      } catch {}
    }
  };

  createRfbRef.current = (password: string, manual: boolean) => {
    if (!id || !containerRef.current) return;

    teardownRfb();
    manualRef.current = manual;
    pendingPwRef.current = password;
    securityFailedRef.current = false;
    setStatus("Connecting");

    const protocol = window.location.protocol === "https:" ? "wss" : "ws";
    const host = window.location.host;
    const url = `${protocol}://${host}/api/v1/browsers/${id}/vnc`;

    const rfb: AnyRFB = new (RFB as any)(containerRef.current, url, {
      scaleViewport: true,
      resizeSession: false,
    });

    rfb.addEventListener("credentialsrequired", () => {
      const pw = pendingPwRef.current;
      if (typeof rfb.sendCredentials === "function") {
        rfb.sendCredentials({ password: pw });
      } else {
        (rfb as any)._rfb_credentials = { password: pw };
      }
    });

    rfb.viewOnly = false;
    rfb.localCursor = true;
    rfb.clipViewport = false;
    rfb.viewportDrag = false;

    rfb.addEventListener("connect", () => {
      setStatus("Connected");
      setNotice("");
      if (manualRef.current) {
        setShowSave(true);
      }
    });

    rfb.addEventListener("securityfailure", (event: any) => {
      securityFailedRef.current = true;
      const reason = event?.detail?.reason ?? "authentication failed";
      attemptRef.current = registerFailure(attemptRef.current, pendingPwRef.current);
      if (isHardCapReached(attemptRef.current)) {
        setStatus("Disconnected");
        setNotice(`VNC auth failed: ${reason}`);
        return;
      }
      const next = nextCandidate(ladderRef.current, attemptRef.current.tried);
      if (next !== null) {
        createRfbRef.current(next, false);
        return;
      }
      manualRef.current = true;
      setPromptError(`VNC auth failed: ${reason}`);
      setShowPrompt(true);
    });

    rfb.addEventListener("disconnect", (event: any) => {
      if (securityFailedRef.current) return;
      setStatus("Disconnected");
      if (event?.detail?.clean === false) {
        setNotice("Connection lost");
      }
    });

    rfb.addEventListener("clipboard", (event: any) => {
      const text = event?.detail?.text ?? "";
      setLastRemoteClipboard(text);
    });

    rfbRef.current = rfb;
  };

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
    if (!id || !containerRef.current) return;
    if (rfbRef.current) return;

    let cancelled = false;

    const start = async () => {
      try {
        const resp = await fetch(`/api/v1/browsers/${id}`);
        if (resp.ok) {
          const session: { browserId: string; browserName?: string; browserVersion?: string; startTime?: string } = await resp.json();
          if (session.browserId === id) {
            browserNameRef.current = session.browserName ?? "";
            browserVersionRef.current = session.browserVersion ?? "";
            if (!sessionStartTime && session.startTime) {
              setSessionStartTime(session.startTime);
            }
          }
        }
      } catch {}

      if (cancelled) return;

      ladderRef.current = buildLadder(
        getSavedByVersion(browserNameRef.current, browserVersionRef.current),
        getSavedByName(browserNameRef.current),
      );

      const first = nextCandidate(ladderRef.current, attemptRef.current.tried);
      if (first !== null) {
        createRfbRef.current(first, false);
      } else {
        manualRef.current = true;
        setShowPrompt(true);
      }
    };

    start();

    const resizeCanvas = () => {
      if (!rfbRef.current || !containerRef.current) return;

      const rect = containerRef.current.getBoundingClientRect();
      const w = Math.round(rect.width);
      const h = Math.round(rect.height);

      try {
        if (isMaximizedRef.current && typeof rfbRef.current.requestDesktopSize === "function") {
          rfbRef.current.requestDesktopSize(w, h);
        } else {
          rfbRef.current.scaleViewport = true;
        }
      } catch {}
    };

    window.addEventListener("resize", resizeCanvas);
    const resizeTimer = setTimeout(resizeCanvas, 200);

    const handleUnload = () => {
      try {
        rfbRef.current?.disconnect();
      } catch {}
    };

    window.addEventListener("beforeunload", handleUnload);

    return () => {
      cancelled = true;
      clearTimeout(resizeTimer);
      window.removeEventListener("resize", resizeCanvas);
      window.removeEventListener("beforeunload", handleUnload);
      teardownRfb();
    };
  }, [id]);

  useEffect(() => {
    isMaximizedRef.current = isMaximized;
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
    } catch {}
  }, [isMaximized]);

  const handlePromptSubmit = (password: string) => {
    setShowPrompt(false);
    setPromptError(undefined);
    createRfbRef.current(password, true);
  };

  const handlePromptCancel = () => {
    setShowPrompt(false);
    teardownRfb();
    setStatus("Disconnected");
    setNotice("VNC authentication cancelled");
  };

  const handleSaveChoose = (scope: SaveScope) => {
    const pw = pendingPwRef.current;
    if (scope === "name") {
      setSavedByName(browserNameRef.current, pw);
    } else if (scope === "version") {
      setSavedByVersion(browserNameRef.current, browserVersionRef.current, pw);
    }
    setShowSave(false);
  };

  const handleClose = () => {
    teardownRfb();
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
                {status === "Connecting"
                  ? "Session Loading..."
                  : notice || "Session Disconnected"}
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

      {showPrompt && (
        <PasswordPromptModal
          onSubmit={handlePromptSubmit}
          onCancel={handlePromptCancel}
          errorReason={promptError}
        />
      )}
      {showSave && (
        <SavePasswordModal
          browserName={browserNameRef.current}
          browserVersion={browserVersionRef.current}
          onChoose={handleSaveChoose}
        />
      )}
    </>
  );
};

export default VNCView;
