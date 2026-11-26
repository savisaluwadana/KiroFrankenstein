/**
 * View-only UI preferences. Engineering Mode reveals tick numbers, hashes and
 * raw panels; it never changes a request or a number. Persisted in
 * localStorage under `parallel.engineering`. Never a source of simulation truth.
 */
import { create } from "zustand";
import { persist } from "zustand/middleware";

export const ENGINEERING_STORAGE_KEY = "parallel.engineering";

export interface UiState {
  engineering: boolean;
  setEngineering: (on: boolean) => void;
  toggleEngineering: () => void;
}

export const useUiStore = create<UiState>()(
  persist(
    (set) => ({
      engineering: false,
      setEngineering: (on) => set({ engineering: on }),
      toggleEngineering: () => set((s) => ({ engineering: !s.engineering })),
    }),
    {
      name: ENGINEERING_STORAGE_KEY,
      partialize: (s) => ({ engineering: s.engineering }),
      // Hydrate after mount so server and first client render agree.
      skipHydration: true,
    },
  ),
);

/** Call once from a client effect (EngineeringToggle does) to load the stored value. */
export function hydrateUiStore(): void {
  void useUiStore.persist.rehydrate();
}
