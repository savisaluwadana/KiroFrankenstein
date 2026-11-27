// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { InjectDisruption } from "./InjectDisruption";

const fetchMock = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>();

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function renderForm(extra: { onInjected?: () => void; onEngineMismatch?: () => void } = {}) {
  render(
    <InjectDisruption
      branchId="b1"
      headTick={10}
      horizonTicks={120}
      startClock="Thu 00:00"
      skus={[{ index: 0, id: "speaker" }]}
      {...extra}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Add a surprise" }));
}

describe("InjectDisruption", () => {
  it("is collapsed by default and offers only hours after the latest computed hour", () => {
    render(<InjectDisruption branchId="b1" headTick={10} horizonTicks={120} startClock="Thu 00:00" skus={[]} />);
    expect(screen.queryByLabelText("Starts at")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Add a surprise" }));
    const options = (screen.getByLabelText("Starts at") as HTMLSelectElement).options;
    expect(options.length).toBe(110);
    expect(options[0]?.value).toBe("11");
  });

  it("posts the request with an idempotency key and the expected head, then confirms", async () => {
    fetchMock.mockResolvedValue(
      json(200, { seq: 1, effectiveTick: 11, kind: "demand_surge", durationTicks: 6, magnitudeBps: 5000, target: -1, provenance: "injected", duplicate: false }),
    );
    const onInjected = vi.fn();
    renderForm({ onInjected });
    fireEvent.change(screen.getByLabelText("What happens"), { target: { value: "demand_surge" } });
    fireEvent.change(screen.getByLabelText("Lasts (hours)"), { target: { value: "6" } });
    fireEvent.click(screen.getByRole("button", { name: "Add this surprise" }));
    await waitFor(() => expect(screen.getByTestId("inject-confirmation")).toBeTruthy());
    const [url, init] = fetchMock.mock.calls[0] ?? [];
    expect(String(url)).toContain("/branches/b1/disruptions");
    expect(JSON.parse(String(init?.body))).toEqual({
      kind: "demand_surge",
      startTick: 11,
      durationTicks: 6,
      target: -1,
      magnitudeBps: 5000,
      expectedHeadTick: 10,
    });
    expect((init?.headers as Record<string, string>)["Idempotency-Key"]).toBeTruthy();
    expect(screen.getByTestId("inject-confirmation").textContent).toContain("demand surge");
    expect(onInjected).toHaveBeenCalledTimes(1);
  });

  it("shows the server's allowed range next to the field it names", async () => {
    fetchMock.mockResolvedValue(
      json(422, {
        type: "urn:parallel:problem:validation",
        title: "Validation failed",
        status: 422,
        issues: [{ field: "durationTicks", message: "Choose a length from 1 to 120 hours." }],
      }),
    );
    renderForm();
    fireEvent.change(screen.getByLabelText("Lasts (hours)"), { target: { value: "999" } });
    fireEvent.click(screen.getByRole("button", { name: "Add this surprise" }));
    await waitFor(() => expect(screen.getByTestId("issue-durationTicks")).toBeTruthy());
    expect(screen.getByTestId("issue-durationTicks").textContent).toBe("Choose a length from 1 to 120 hours.");
    expect(screen.queryByTestId("inject-confirmation")).toBeNull();
  });

  it("reports an engine-version mismatch to the studio", async () => {
    fetchMock.mockResolvedValue(
      json(409, { type: "urn:parallel:problem:engine-version-mismatch", title: "Engine version mismatch", status: 409 }),
    );
    const onEngineMismatch = vi.fn();
    renderForm({ onEngineMismatch });
    fireEvent.click(screen.getByRole("button", { name: "Add this surprise" }));
    await waitFor(() => expect(onEngineMismatch).toHaveBeenCalled());
  });
});
