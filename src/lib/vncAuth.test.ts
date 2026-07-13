import { describe, it, expect } from "vitest";
import {
  MAX_AUTH_ATTEMPTS,
  newAttemptState,
  buildLadder,
  nextCandidate,
  registerFailure,
  markManual,
  isReused,
  isHardCapReached,
  shouldPrompt,
} from "./vncAuth";

describe("buildLadder", () => {
  it("drops empty and duplicate candidates preserving order", () => {
    expect(buildLadder("a", null, "b", undefined, "a", "", "c")).toEqual(["a", "b", "c"]);
  });

  it("returns empty when no candidates", () => {
    expect(buildLadder(null, undefined, "")).toEqual([]);
  });
});

describe("nextCandidate", () => {
  it("returns first untried candidate", () => {
    const tried = new Set(["a"]);
    expect(nextCandidate(["a", "b", "c"], tried)).toBe("b");
  });

  it("returns null when all tried", () => {
    const tried = new Set(["a", "b"]);
    expect(nextCandidate(["a", "b"], tried)).toBeNull();
  });

  it("returns null for empty ladder", () => {
    expect(nextCandidate([], new Set())).toBeNull();
  });
});

describe("registerFailure", () => {
  it("records the password and increments attempts without mutating input", () => {
    const state = newAttemptState();
    const next = registerFailure(state, "a");
    expect(next.tried.has("a")).toBe(true);
    expect(next.attempts).toBe(1);
    expect(state.tried.has("a")).toBe(false);
    expect(state.attempts).toBe(0);
  });
});

describe("markManual / isReused", () => {
  it("marks manual mode", () => {
    expect(markManual(newAttemptState()).manual).toBe(true);
  });

  it("detects reused passwords", () => {
    const state = registerFailure(newAttemptState(), "a");
    expect(isReused(state, "a")).toBe(true);
    expect(isReused(state, "b")).toBe(false);
  });
});

describe("isHardCapReached", () => {
  it("is false below the cap and true at the cap", () => {
    let state = newAttemptState();
    expect(isHardCapReached(state)).toBe(false);
    for (let i = 0; i < MAX_AUTH_ATTEMPTS; i++) {
      state = registerFailure(state, `p${i}`);
    }
    expect(isHardCapReached(state)).toBe(true);
  });

  it("honors a custom cap", () => {
    const state = registerFailure(newAttemptState(), "a");
    expect(isHardCapReached(state, 1)).toBe(true);
  });
});

describe("shouldPrompt", () => {
  it("is false while auto candidates remain", () => {
    const state = registerFailure(newAttemptState(), "a");
    expect(shouldPrompt(state, ["a", "b"])).toBe(false);
  });

  it("is true when auto candidates are exhausted", () => {
    let state = registerFailure(newAttemptState(), "a");
    state = registerFailure(state, "b");
    expect(shouldPrompt(state, ["a", "b"])).toBe(true);
  });

  it("is false once the hard cap is reached even with candidates left", () => {
    let state = newAttemptState();
    for (let i = 0; i < MAX_AUTH_ATTEMPTS; i++) {
      state = registerFailure(state, `p${i}`);
    }
    expect(shouldPrompt(state, ["fresh"], MAX_AUTH_ATTEMPTS)).toBe(false);
  });
});

describe("no infinite loop when every password fails", () => {
  it("walks the finite auto ladder then asks for manual input exactly once", () => {
    const ladder = buildLadder("byName", "current", "cookie", "default");
    let state = newAttemptState();
    let prompts = 0;

    let candidate = nextCandidate(ladder, state.tried);
    let guard = 0;
    while (candidate !== null) {
      guard++;
      expect(guard).toBeLessThanOrEqual(ladder.length + 1);
      state = registerFailure(state, candidate);
      if (shouldPrompt(state, ladder)) {
        prompts++;
        break;
      }
      candidate = nextCandidate(ladder, state.tried);
    }

    expect(state.attempts).toBe(ladder.length);
    expect(prompts).toBe(1);
    expect(nextCandidate(ladder, state.tried)).toBeNull();
  });

  it("terminates via the hard cap when manual entries keep failing", () => {
    let state = markManual(newAttemptState());
    const ladder: string[] = [];
    let manualSubmits = 0;

    while (!isHardCapReached(state)) {
      manualSubmits++;
      state = registerFailure(state, `manual-${manualSubmits}`);
    }

    expect(isHardCapReached(state)).toBe(true);
    expect(shouldPrompt(state, ladder)).toBe(false);
    expect(manualSubmits).toBe(MAX_AUTH_ATTEMPTS);
  });
});
