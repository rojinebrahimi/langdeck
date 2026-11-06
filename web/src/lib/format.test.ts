import { compact, duration, formatMs, shortId, timeAgo } from "./format";

describe("format", () => {
  const now = new Date("2026-01-01T12:00:00Z").getTime();
  it("timeAgo", () => {
    expect(timeAgo(undefined)).toBe("never");
    expect(timeAgo("2026-01-01T11:59:58Z", now)).toBe("just now");
    expect(timeAgo("2026-01-01T11:59:30Z", now)).toBe("30s ago");
    expect(timeAgo("2026-01-01T11:30:00Z", now)).toBe("30m ago");
    expect(timeAgo("2025-12-29T12:00:00Z", now)).toBe("3d ago");
  });
  it("formatMs", () => {
    expect(formatMs(250)).toBe("250ms");
    expect(formatMs(1500)).toBe("1.5s");
    expect(formatMs(125_000)).toBe("2m 5s");
  });
  it("duration", () => {
    expect(duration(undefined)).toBe("—");
    expect(duration("2026-01-01T00:00:00Z", "2026-01-01T00:00:02Z")).toBe("2.0s");
  });
  it("compact and shortId", () => {
    expect(compact(1500)).toBe("1.5K");
    expect(shortId("0123456789abcdef")).toBe("01234567…");
    expect(shortId("short")).toBe("short");
  });
});
