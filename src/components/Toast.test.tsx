import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, act, cleanup, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import React from "react";
import { ToastHost, useToasts, TOAST_TIMEOUT } from "./Toast";

const Harness: React.FC<{ timeout?: number }> = ({ timeout }) => {
  const { toasts, push, dismiss } = useToasts(timeout);
  return (
    <>
      <button onClick={() => push("Failed to start browser", "context deadline exceeded")}>push</button>
      <button onClick={() => push("No reason")}>push bare</button>
      <ToastHost toasts={toasts} onDismiss={dismiss} />
    </>
  );
};

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("ToastHost", () => {
  it("renders nothing when there are no toasts", () => {
    render(<ToastHost toasts={[]} onDismiss={vi.fn()} />);
    expect(screen.queryByRole("region")).toBeNull();
  });

  it("renders a title and a reason", () => {
    render(<ToastHost toasts={[{ id: 1, title: "Boom", reason: "because" }]} onDismiss={vi.fn()} />);
    expect(screen.getByText("Boom")).toBeInTheDocument();
    expect(screen.getByText("because")).toBeInTheDocument();
  });

  it("omits the reason when there is none", () => {
    render(<ToastHost toasts={[{ id: 1, title: "Boom" }]} onDismiss={vi.fn()} />);
    expect(document.querySelector(".toast-reason")).toBeNull();
  });

  it("reports a dismissal", async () => {
    const onDismiss = vi.fn();
    render(<ToastHost toasts={[{ id: 7, title: "Boom" }]} onDismiss={onDismiss} />);

    await userEvent.click(screen.getByRole("button", { name: "Dismiss notification" }));
    expect(onDismiss).toHaveBeenCalledWith(7);
  });
});

describe("useToasts", () => {
  it("pushes a toast and hides it once the timeout elapses", () => {
    vi.useFakeTimers();
    render(<Harness />);

    act(() => {
      fireEvent.click(screen.getByRole("button", { name: "push" }));
    });
    expect(screen.getByText("context deadline exceeded")).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(TOAST_TIMEOUT - 1);
    });
    expect(screen.getByText("context deadline exceeded")).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(document.querySelector(".toast")).toBeNull();
  });

  it("stacks several toasts", async () => {
    const user = userEvent.setup();
    render(<Harness />);

    await user.click(screen.getByRole("button", { name: "push" }));
    await user.click(screen.getByRole("button", { name: "push bare" }));

    expect(document.querySelectorAll(".toast")).toHaveLength(2);
  });

  it("dismisses on the close button without waiting for the timeout", async () => {
    const user = userEvent.setup();
    render(<Harness />);

    await user.click(screen.getByRole("button", { name: "push" }));
    await user.click(screen.getByRole("button", { name: "Dismiss notification" }));

    expect(screen.queryByText("context deadline exceeded")).toBeNull();
  });

  it("clears pending timers on unmount", async () => {
    const user = userEvent.setup();
    const { unmount } = render(<Harness />);

    await user.click(screen.getByRole("button", { name: "push" }));

    const clear = vi.spyOn(globalThis, "clearTimeout");
    unmount();

    expect(clear).toHaveBeenCalled();
  });
});
