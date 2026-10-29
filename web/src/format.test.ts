import { describe, expect, it } from "vitest";
import { completion, eventText, fitBand, nextWorkdayMorning, relativeDay, toLocalInput, tokens } from "./format";

describe("relativeDay", () => {
  const now = new Date(2026, 9, 28, 9, 0); // Wed Oct 28, local
  it("names nearby days", () => {
    expect(relativeDay(new Date(2026, 9, 28, 23, 0).toISOString(), now)).toBe("today");
    expect(relativeDay(new Date(2026, 9, 27, 1, 0).toISOString(), now)).toBe("yesterday");
    expect(relativeDay(new Date(2026, 9, 29, 8, 0).toISOString(), now)).toBe("tomorrow");
  });
  it("counts calendar days, not 24-hour spans", () => {
    expect(relativeDay(new Date(2026, 9, 21, 23, 59).toISOString(), now)).toBe("7 days ago");
    expect(relativeDay(new Date(2026, 10, 2, 0, 1).toISOString(), now)).toBe("in 5 days");
  });
});

describe("interview defaults", () => {
  it("skips weekends", () => {
    const friday = new Date(2026, 9, 30, 15, 0);
    expect(toLocalInput(nextWorkdayMorning(friday))).toBe("2026-11-02T10:00");
  });
  it("pads the datetime-local value", () => {
    expect(toLocalInput(new Date(2026, 0, 5, 9, 7))).toBe("2026-01-05T09:07");
  });
});

describe("labels", () => {
  it("describes how a pursuit ended", () => {
    expect(completion("rejected", "interview")).toBe("Rejected after interviewing");
    expect(completion("ghosted", "applied")).toBe("Ghosted");
    expect(completion("offer", "offer")).toBe("Offer");
  });
  it("describes events", () => {
    expect(eventText("step", "resume")).toBe("Done: Tailor resume");
    expect(eventText("completed", "rejected went with someone internal")).toBe(
      "Completed: Rejected · went with someone internal",
    );
    expect(eventText("applied", "")).toBe("Applied");
  });
  it("bands fit at the alert and digest bars", () => {
    expect([fitBand(80), fitBand(75), fitBand(60), fitBand(10), fitBand(null)]).toEqual([
      "strong",
      "strong",
      "fair",
      "weak",
      "none",
    ]);
  });
  it("abbreviates token counts", () => {
    expect([tokens(950), tokens(4_100), tokens(2_500_000)]).toEqual(["950", "4.1K", "2.5M"]);
  });
});
