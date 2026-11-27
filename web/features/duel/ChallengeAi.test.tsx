// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useUiStore } from "@/stores/ui";
import { ChallengeAi } from "./ChallengeAi";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push }) }));

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function install(llmAvailable: boolean) {
  const calls: { path: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const path = new URL(url).pathname.replace(/^\/api\/v1/, "");
      calls.push({ path, body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined });
      if (path === "/managers") {
        return json(200, {
          managers: [
            { kind: "human", name: "You", description: "", available: true },
            { kind: "ai_rule_based", name: "Rule-based AI", description: "", available: true },
            llmAvailable
              ? { kind: "ai_llm", name: "LLM AI", description: "", available: true, model: "m-1" }
              : { kind: "ai_llm", name: "LLM AI", description: "", available: false, unavailableReason: "not configured" },
          ],
        });
      }
      if (path === "/duels") return json(201, { duel: { id: "d9" }, world: {}, status: {} });
      return json(404, { title: "nope", status: 404 });
    }),
  );
  return calls;
}

beforeEach(() => {
  push.mockReset();
  useUiStore.setState({ engineering: false });
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("ChallengeAi", () => {
  it("omits the LLM option when the server says it is unavailable, and starts a Rule-based duel with the demo seed", async () => {
    const user = userEvent.setup();
    const calls = install(false);
    render(<ChallengeAi />);
    await waitFor(() => expect(calls.some((c) => c.path === "/managers")).toBe(true));
    await screen.findByText(/You will play against the Rule-based AI/);
    expect(screen.queryByRole("radio")).toBeNull();
    expect(screen.queryByText(/LLM AI/)).toBeNull();
    await user.click(screen.getByRole("button", { name: "Challenge the AI manager" }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/duels/d9"));
    expect(calls.find((c) => c.path === "/duels")?.body).toEqual({ seed: 2026, aiKind: "ai_rule_based" });
  });

  it("explains the missing LLM manager in Engineering Mode", async () => {
    useUiStore.setState({ engineering: true });
    install(false);
    render(<ChallengeAi />);
    expect((await screen.findByTestId("llm-note")).textContent).toContain("LLM manager not configured on this server");
  });

  it("offers the LLM AI with its model when available", async () => {
    const user = userEvent.setup();
    const calls = install(true);
    render(<ChallengeAi />);
    await user.click(await screen.findByRole("radio", { name: "LLM AI (m-1)" }));
    await user.click(screen.getByRole("button", { name: "Challenge the AI manager" }));
    await waitFor(() => expect(push).toHaveBeenCalled());
    expect(calls.find((c) => c.path === "/duels")?.body).toEqual({ seed: 2026, aiKind: "ai_llm" });
  });
});
