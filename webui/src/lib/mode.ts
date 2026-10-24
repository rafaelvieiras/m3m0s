// Simple / Advanced mode. Simple (the default) shows only Home, Search, Add and
// Connect agent; Advanced adds Recent, Stats, Insights, Jobs, Embedding,
// Preview and Trash plus the raw filters. The choice is remembered in localStorage, and
// deep links to advanced screens keep working in either mode.
import { useSyncExternalStore } from "react";

const STORAGE_KEY = "m3m0s.advanced";

function read(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === "1";
  } catch {
    return false; // storage blocked: stay in Simple mode
  }
}

let current = read();
const listeners = new Set<() => void>();

export function isAdvancedMode(): boolean {
  return current;
}

export function setAdvancedMode(on: boolean) {
  if (on === current) return;
  current = on;
  try {
    localStorage.setItem(STORAGE_KEY, on ? "1" : "0");
  } catch {
    // ignore: the choice just will not survive a reload
  }
  listeners.forEach((fn) => fn());
}

function subscribe(fn: () => void) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** [advanced, setAdvanced]: re-renders when the mode changes anywhere. */
export function useAdvancedMode(): [boolean, (on: boolean) => void] {
  const advanced = useSyncExternalStore(subscribe, isAdvancedMode, isAdvancedMode);
  return [advanced, setAdvancedMode];
}

/** Routes that only appear in the Advanced nav. */
export const ADVANCED_ROUTES = ["/recent", "/stats", "/insights", "/jobs", "/embedding", "/preview", "/trash"] as const;
