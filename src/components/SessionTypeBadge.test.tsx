import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SessionTypeBadge } from "./SessionTypeBadge";

describe("SessionTypeBadge", () => {
  it.each(["selenium", "playwright", "mcp", "devtools"])(
    "renders the %s badge with its modifier class",
    (type) => {
      const { container } = render(<SessionTypeBadge type={type} />);

      expect(screen.getByText(type)).toBeInTheDocument();
      expect(container.firstChild).toHaveClass(
        "session-type-badge",
        `session-type-badge--${type}`,
      );
    },
  );

  it.each([
    ["undefined", undefined],
    ["empty string", ""],
    ["an unknown type", "cdp"],
  ])("renders the unknown badge for %s", (_label, type) => {
    const { container } = render(<SessionTypeBadge type={type} />);

    expect(screen.getByText("unknown")).toBeInTheDocument();
    expect(container.firstChild).toHaveClass("session-type-badge", "session-type-badge--unknown");
  });

  it("reports the unknown label on click", async () => {
    const onClick = vi.fn();
    render(<SessionTypeBadge type="cdp" onClick={onClick} />);

    await userEvent.click(screen.getByRole("button", { name: "unknown" }));

    expect(onClick).toHaveBeenCalledWith("unknown");
  });

  it("stays a plain span without a click handler", () => {
    render(<SessionTypeBadge type="selenium" />);

    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.getByText("selenium").tagName).toBe("SPAN");
  });

  it("becomes a button once a click handler is given", () => {
    render(<SessionTypeBadge type="selenium" onClick={vi.fn()} />);

    const badge = screen.getByRole("button", { name: "selenium" });
    expect(badge).toHaveAttribute("aria-pressed", "false");
    expect(badge).not.toHaveClass("session-type-badge--active");
  });

  it("marks itself pressed when active", () => {
    render(<SessionTypeBadge type="mcp" active onClick={vi.fn()} />);

    const badge = screen.getByRole("button", { name: "mcp" });
    expect(badge).toHaveAttribute("aria-pressed", "true");
    expect(badge).toHaveClass("session-type-badge--active");
  });

  it("reports its own type on click", async () => {
    const onClick = vi.fn();
    render(<SessionTypeBadge type="playwright" onClick={onClick} />);

    await userEvent.click(screen.getByRole("button", { name: "playwright" }));

    expect(onClick).toHaveBeenCalledWith("playwright");
  });

  it("carries the active class on a non-interactive badge too", () => {
    const { container } = render(<SessionTypeBadge type="mcp" active />);

    expect(container.firstChild).toHaveClass("session-type-badge--active");
  });
});
