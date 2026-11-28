/**
 * Write tokens of the worlds this tab created. The server returns a token once
 * (`writeToken` on create responses) and only its hash is stored there, so the
 * tab is the only place that can change the world. Tokens live in
 * sessionStorage (cleared when the tab closes, not shared with other tabs) and
 * are never put in a URL, logged or sent anywhere but the API's
 * `X-Parallel-Write-Token` header.
 *
 * A token is filed under every id it opens (world, branches, duel) so a
 * mutation can find it from the id in its path. A world opened from a shared
 * link has no token, and the studio shows it read-only.
 */
export const WRITE_TOKEN_HEADER = "X-Parallel-Write-Token";

const PREFIX = "parallel.writeToken.";
const fallback = new Map<string, string>(); // when sessionStorage is unavailable

export function rememberWriteToken(token: string | undefined, ...ids: readonly string[]): void {
  if (token === undefined || token === "") return;
  for (const id of ids) {
    try {
      window.sessionStorage.setItem(PREFIX + id, token);
    } catch {
      fallback.set(id, token); // storage unavailable: the in-memory copy serves this page only
    }
  }
}

export function writeTokenFor(id: string): string | null {
  try {
    const t = window.sessionStorage.getItem(PREFIX + id);
    return t !== null && t !== "" ? t : null;
  } catch {
    return fallback.get(id) ?? null;
  }
}

export function hasWriteToken(id: string): boolean {
  return writeTokenFor(id) !== null;
}

type Listener = () => void;
const listeners = new Set<Listener>();

/** Calls `cb` when the server refuses a write token. Returns the unsubscribe function. */
export function onWriteTokenRejected(cb: Listener): () => void {
  listeners.add(cb);
  return () => {
    listeners.delete(cb);
  };
}

export function notifyWriteTokenRejected(): void {
  for (const cb of [...listeners]) cb();
}
