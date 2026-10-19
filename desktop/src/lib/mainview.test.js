import { beforeEach, describe, expect, it, vi } from "vitest";

const focusField = vi.fn();
vi.mock("./stores/composer.svelte.js", () => ({ focusField }));

let mv;
let ui;

beforeEach(async () => {
  vi.resetModules();
  focusField.mockClear();
  mv = await import("./mainview.js");
  ({ ui } = await import("./stores/ui.svelte.js"));
});

describe("the main column's view (desktop-design-5-6 6.4)", () => {
  it("shows one view at a time and gives it the focus", () => {
    expect(mv.mainView.current).toBe(null);
    mv.showMain({ kind: "helper", component: "H", props: { sessionId: "c1" } });
    expect(mv.mainView.current).toEqual({ kind: "helper", component: "H", props: { sessionId: "c1" } });
    expect(ui.focus).toBe("main");
    mv.showMain({ kind: "terminal", component: "T" });
    expect(mv.mainView.current).toEqual({ kind: "terminal", component: "T", props: {} });
  });

  it("closes through its handle only while it is the one shown, and gives the focus back to the typing box", () => {
    const helper = mv.showMain({ kind: "helper", component: "H" });
    const terminal = mv.showMain({ kind: "terminal", component: "T" });
    expect(helper.close()).toBe(false);
    expect(mv.mainView.current.kind).toBe("terminal");
    expect(focusField).not.toHaveBeenCalled();
    expect(terminal.close()).toBe(true);
    expect(mv.mainView.current).toBe(null);
    expect(focusField).toHaveBeenCalledTimes(1);
  });

  it("closeMain closes whatever shows, and does nothing when nothing does", () => {
    expect(mv.closeMain()).toBe(false);
    mv.showMain({ kind: "helper", component: "H" });
    expect(mv.closeMain()).toBe(true);
    expect(mv.mainView.current).toBe(null);
    expect(focusField).toHaveBeenCalledTimes(1);
  });
});
