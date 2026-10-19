import { describe, expect, it } from "vitest";
import { humanizeTime, inProject, infoText } from "./match.js";

// The Sessions sheet's info text and project rule (desktop-design-5-6 2.5):
// a slip shows wrong ages or opens a chat in the wrong window.
const NOW = 1_700_000_000_000; // ms
const at = (secondsAgo) => NOW / 1000 - secondsAgo;

describe("humanizeTime", () => {
  it("words ages as go-humanize does, at the edges of each step", () => {
    expect(humanizeTime(at(0), NOW)).toBe("now");
    expect(humanizeTime(at(1), NOW)).toBe("1 second ago");
    expect(humanizeTime(at(59), NOW)).toBe("59 seconds ago");
    expect(humanizeTime(at(119), NOW)).toBe("1 minute ago");
    expect(humanizeTime(at(3 * 3600 + 5), NOW)).toBe("3 hours ago");
    expect(humanizeTime(at(13 * 86400), NOW)).toBe("1 week ago");
    expect(humanizeTime(at(-120), NOW)).toBe("2 minutes from now");
  });
});

describe("rows", () => {
  it("shows the short folder and the age", () => {
    expect(infoText({ Directory: "/home/me/dev/app", UpdatedAt: at(7200) }, "/home/me", NOW)).toBe("~/dev/app · 2 hours ago");
    expect(infoText({ UpdatedAt: at(30) }, "/home/me", NOW)).toBe("30 seconds ago");
  });

  it("keeps a chat in this window only when its folder is this project", () => {
    expect(inProject({ Directory: "/p/app/" }, "/p/app")).toBe(true);
    expect(inProject({ Directory: "/p/other" }, "/p/app")).toBe(false);
    expect(inProject({}, "/p/app")).toBe(true);
  });
});
