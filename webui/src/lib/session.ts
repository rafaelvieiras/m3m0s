// Session helpers for the route guards (they run outside React). A stored API
// key is the pre-accounts way in and keeps working; otherwise the session
// cookie is probed with GET /auth/me.
import { getMe } from "./api";
import { hasStoredApiKey } from "./auth";

export async function isSignedIn(): Promise<boolean> {
  if (hasStoredApiKey()) return true;
  try {
    return (await getMe()) !== null;
  } catch {
    return false;
  }
}
