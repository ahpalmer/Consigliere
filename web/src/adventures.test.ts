import { describe, expect, it } from "vitest";
import { when } from "./pages/Adventures";

describe("when", () => {
  it("says today and tomorrow, with the time", () => {
    expect(when("2026-12-05", "", "2026-12-05")).toBe("Today");
    expect(when("2026-12-06", "19:30", "2026-12-05")).toBe("Tomorrow at 7:30pm");
    expect(when("2026-12-06", "09:00", "2026-12-05")).toBe("Tomorrow at 9am");
    expect(when("2026-12-06", "00:15", "2026-12-05")).toBe("Tomorrow at 12:15am");
  });
  it("names other days", () => {
    expect(when("2026-12-12", "", "2026-12-05")).toMatch(/Dec 12/);
    expect(when("2027-01-02", "", "2026-12-05")).toMatch(/2027/);
  });
});
