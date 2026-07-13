import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { PasswordPromptModal, SavePasswordModal } from "./VncPasswordModals";

describe("PasswordPromptModal", () => {
  it("disables Connect until a password is typed, then submits it", async () => {
    const onSubmit = vi.fn();
    const onCancel = vi.fn();
    render(<PasswordPromptModal onSubmit={onSubmit} onCancel={onCancel} />);

    const connect = screen.getByRole("button", { name: "Connect" });
    expect(connect).toBeDisabled();

    await userEvent.type(screen.getByLabelText("VNC password"), "secret");
    expect(connect).toBeEnabled();

    await userEvent.click(connect);
    expect(onSubmit).toHaveBeenCalledWith("secret");
  });

  it("does not submit an empty password via Enter", async () => {
    const onSubmit = vi.fn();
    render(<PasswordPromptModal onSubmit={onSubmit} onCancel={() => {}} />);

    await userEvent.type(screen.getByLabelText("VNC password"), "{Enter}");
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it("calls onCancel when Cancel is clicked", async () => {
    const onCancel = vi.fn();
    render(<PasswordPromptModal onSubmit={() => {}} onCancel={onCancel} />);

    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it("shows the reuse warning and error reason when provided", () => {
    render(
      <PasswordPromptModal
        onSubmit={() => {}}
        onCancel={() => {}}
        warnReused
        errorReason="authentication failed"
      />,
    );

    expect(screen.getByText("This password was already tried.")).toBeInTheDocument();
    expect(screen.getByText("authentication failed")).toBeInTheDocument();
  });
});

describe("SavePasswordModal", () => {
  it("reports the chosen scope for each button", async () => {
    const onChoose = vi.fn();
    render(<SavePasswordModal browserName="chrome" browserVersion="123" onChoose={onChoose} />);

    await userEvent.click(screen.getByRole("button", { name: 'Save for all "chrome"' }));
    await userEvent.click(screen.getByRole("button", { name: 'Only "chrome 123"' }));
    await userEvent.click(screen.getByRole("button", { name: "Don't save" }));

    expect(onChoose.mock.calls).toEqual([["name"], ["version"], [null]]);
  });
});
