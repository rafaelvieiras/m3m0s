// Stores the API key in localStorage (D13, item 2 — "paste the API key once,
// keep it in localStorage") and exposes helpers to read/clear it. Deliberately
// not a reactive React context: the only consumer that needs to react to
// login/logout is navigation (the routes' beforeLoad), which already runs
// outside React's render cycle.
const STORAGE_KEY = "m3m0s_api_key";

export function getApiKey(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY);
  } catch {
    return null;
  }
}

export function setApiKey(key: string): void {
  localStorage.setItem(STORAGE_KEY, key);
}

export function clearApiKey(): void {
  localStorage.removeItem(STORAGE_KEY);
}

// A stored API key counts as signed in (the pre-accounts flow, still valid);
// otherwise the session cookie is probed with GET /auth/me (see lib/api.ts).
export function hasStoredApiKey(): boolean {
  return !!getApiKey();
}
