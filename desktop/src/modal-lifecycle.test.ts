import { describe, expect, it, vi } from "vitest";
import { ModalLifecycle } from "./modal-lifecycle";

describe("ModalLifecycle", () => {
  it("keeps ordinary modals without a cleanup callback working", () => {
    const modal = new ModalLifecycle();
    expect(() => {
      modal.replace();
      modal.close();
      modal.close();
    }).not.toThrow();
  });

  it("cleans up the current modal once, including reentrant closing", () => {
    const modal = new ModalLifecycle();
    const cleanup = vi.fn(() => modal.close());
    modal.replace(cleanup);
    modal.close();
    modal.close();
    expect(cleanup).toHaveBeenCalledTimes(1);
  });

  it("cleans up the previous modal when another modal replaces it", () => {
    const modal = new ModalLifecycle();
    const first = vi.fn();
    const second = vi.fn();
    modal.replace(first);
    modal.replace(second);
    expect(first).toHaveBeenCalledTimes(1);
    expect(second).not.toHaveBeenCalled();
    modal.close();
    expect(second).toHaveBeenCalledTimes(1);
  });

  it("does not carry auto-select cleanup over into an ordinary modal", () => {
    const modal = new ModalLifecycle();
    const stop = vi.fn();
    modal.replace(stop);
    modal.replace();
    modal.close();
    expect(stop).toHaveBeenCalledTimes(1);
  });
});
