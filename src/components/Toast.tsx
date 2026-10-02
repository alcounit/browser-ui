import React from "react";

export const TOAST_TIMEOUT = 6000;

export interface Toast {
  id: number;
  title: string;
  reason?: string;
}

export const useToasts = (timeout = TOAST_TIMEOUT) => {
  const [toasts, setToasts] = React.useState<Toast[]>([]);
  const nextId = React.useRef(0);
  const timers = React.useRef<number[]>([]);

  React.useEffect(() => {
    const pending = timers.current;
    return () => pending.forEach((id) => clearTimeout(id));
  }, []);

  const dismiss = React.useCallback((id: number) => {
    setToasts((prev) => prev.filter((toast) => toast.id !== id));
  }, []);

  const push = React.useCallback((title: string, reason?: string) => {
    const id = nextId.current++;
    setToasts((prev) => [...prev, { id, title, reason }]);

    const timer = window.setTimeout(() => dismiss(id), timeout);
    timers.current.push(timer);
  }, [dismiss, timeout]);

  return { toasts, push, dismiss };
};

interface ToastHostProps {
  toasts: Toast[];
  onDismiss: (id: number) => void;
}

export const ToastHost: React.FC<ToastHostProps> = ({ toasts, onDismiss }) => {
  if (toasts.length === 0) {
    return null;
  }

  return (
    <div className="toast-host" role="region" aria-label="Notifications">
      {toasts.map((toast) => (
        <div key={toast.id} className="toast" role="alert">
          <div className="toast-body">
            <span className="toast-title">{toast.title}</span>
            {toast.reason && <span className="toast-reason">{toast.reason}</span>}
          </div>
          <button
            type="button"
            className="toast-close"
            aria-label="Dismiss notification"
            onClick={() => onDismiss(toast.id)}
          >
            ×
          </button>
        </div>
      ))}
    </div>
  );
};
