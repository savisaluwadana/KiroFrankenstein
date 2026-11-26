// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { ENGINEERING_STORAGE_KEY, hydrateUiStore, useUiStore } from "./ui";

describe("ui store", () => {
  beforeEach(() => {
    window.localStorage.clear();
    useUiStore.setState({ engineering: false });
  });

  it("defaults off and toggles", () => {
    expect(useUiStore.getState().engineering).toBe(false);
    useUiStore.getState().toggleEngineering();
    expect(useUiStore.getState().engineering).toBe(true);
  });

  it("persists to localStorage and rehydrates", async () => {
    useUiStore.getState().setEngineering(true);
    const raw = window.localStorage.getItem(ENGINEERING_STORAGE_KEY);
    expect(raw).not.toBeNull();
    expect(JSON.parse(raw ?? "{}").state.engineering).toBe(true);
    useUiStore.setState({ engineering: false });
    window.localStorage.setItem(ENGINEERING_STORAGE_KEY, JSON.stringify({ state: { engineering: true }, version: 0 }));
    hydrateUiStore();
    await useUiStore.persist.rehydrate();
    expect(useUiStore.getState().engineering).toBe(true);
  });
});
