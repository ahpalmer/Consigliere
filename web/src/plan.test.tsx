import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { inline } from "./pages/CodePlan";

describe("inline", () => {
  it("renders bold, code and link text", () => {
    expect(renderToStaticMarkup(<p>{inline("A **Code** page in `internal/devbox` ([dev-box.md](dev-box.md))")}</p>)).toBe(
      "<p>A <strong>Code</strong> page in <code>internal/devbox</code> (dev-box.md)</p>",
    );
  });
});
