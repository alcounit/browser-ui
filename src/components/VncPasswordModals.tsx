import React, { useEffect, useRef, useState } from "react";

export type SaveScope = "name" | "version" | null;

interface PasswordPromptModalProps {
  onSubmit: (password: string) => void;
  onCancel: () => void;
  warnReused?: boolean;
  errorReason?: string;
}

export const PasswordPromptModal: React.FC<PasswordPromptModalProps> = ({
  onSubmit,
  onCancel,
  warnReused,
  errorReason,
}) => {
  const [value, setValue] = useState("");
  const inputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  const handleSubmit = (event: React.FormEvent) => {
    event.preventDefault();
    onSubmit(value);
  };

  return (
    <div className="vnc-modal-overlay" role="dialog" aria-label="VNC password required">
      <form className="vnc-modal" onSubmit={handleSubmit}>
        <div className="vnc-modal-title">VNC password required</div>
        {errorReason && <div className="vnc-modal-error">{errorReason}</div>}
        {warnReused && (
          <div className="vnc-modal-warn">This password was already tried.</div>
        )}
        <input
          ref={inputRef}
          className="vnc-modal-input"
          type="password"
          autoComplete="off"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          aria-label="VNC password"
        />
        <div className="vnc-modal-actions">
          <button type="submit" className="vnc-modal-btn" disabled={!value}>
            Connect
          </button>
          <button type="button" className="vnc-modal-btn" onClick={onCancel}>
            Cancel
          </button>
        </div>
      </form>
    </div>
  );
};

interface SavePasswordModalProps {
  browserName: string;
  browserVersion: string;
  onChoose: (scope: SaveScope) => void;
}

export const SavePasswordModal: React.FC<SavePasswordModalProps> = ({ browserName, browserVersion, onChoose }) => {
  return (
    <div className="vnc-modal-overlay" role="dialog" aria-label="Save VNC password">
      <div className="vnc-modal">
        <div className="vnc-modal-title">Save this password?</div>
        <div className="vnc-modal-actions">
          <button type="button" className="vnc-modal-btn" onClick={() => onChoose("name")}>
            Save for all "{browserName}"
          </button>
          <button type="button" className="vnc-modal-btn" onClick={() => onChoose("version")}>
            Only "{browserName} {browserVersion}"
          </button>
          <button type="button" className="vnc-modal-btn" onClick={() => onChoose(null)}>
            Don't save
          </button>
        </div>
      </div>
    </div>
  );
};
