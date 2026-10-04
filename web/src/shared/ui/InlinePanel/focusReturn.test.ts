import { describe, expect, it, vi } from "vitest";

import { returnFocusToOpener } from "@/shared/ui/InlinePanel/focusReturn";

// Dismissing or saving an inline panel removes the focused field; keyboard users continue from the control that opened it (WAI-ARIA dialog focus guidance applied to an inline region).
const opener = (isConnected = true) => ({ isConnected, focus: vi.fn() });

describe("returnFocusToOpener", () => {
  it("focuses the opener when the panel held focus", () => {
    const button = opener();
    expect(returnFocusToOpener(button, true)).toBe(true);
    expect(button.focus).toHaveBeenCalledTimes(1);
  });

  it("does not scroll the page while returning focus", () => {
    const button = opener();
    returnFocusToOpener(button, true);
    expect(button.focus).toHaveBeenCalledWith({ preventScroll: true });
  });

  it("returns focus on every close of a reopened panel", () => {
    const button = opener();
    returnFocusToOpener(button, true);
    returnFocusToOpener(button, true);
    expect(button.focus).toHaveBeenCalledTimes(2);
  });

  it("leaves focus alone when the user already moved it elsewhere", () => {
    const button = opener();
    expect(returnFocusToOpener(button, false)).toBe(false);
    expect(button.focus).not.toHaveBeenCalled();
  });

  it("skips an opener that left the document, such as a refreshed table row", () => {
    const button = opener(false);
    expect(returnFocusToOpener(button, true)).toBe(false);
    expect(button.focus).not.toHaveBeenCalled();
  });

  it("does nothing when nothing had focus before the panel opened", () => {
    expect(returnFocusToOpener(undefined, true)).toBe(false);
  });

  it("does nothing for a detached opener the user also moved away from", () => {
    const button = opener(false);
    expect(returnFocusToOpener(button, false)).toBe(false);
    expect(button.focus).not.toHaveBeenCalled();
  });
});
