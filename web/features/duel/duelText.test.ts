import { describe, expect, it } from "vitest";
import { fallbackSentence } from "./duelText";

describe("fallbackSentence", () => {
  it("uses plain wording for every fallback reason", () => {
    expect(fallbackSentence("timeout")).toBe("The AI service took too long, so the rule-based manager decided instead.");
    expect(fallbackSentence("budget_exhausted")).toMatch(/^The AI call limit for this game was reached/);
    expect(fallbackSentence("invalid_json")).toMatch(/^The AI's answer wasn't in a usable format/);
    for (const code of ["http_error", "schema_violation", "llm_unavailable", "something_new"]) {
      expect(fallbackSentence(code)).toMatch(/rule-based manager decided instead\.$/);
      expect(fallbackSentence(code)).not.toMatch(/LLM|json|schema/i);
    }
  });
});
