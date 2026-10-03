import { QueryClient } from "@tanstack/react-query";

// Single instance shared between the QueryClientProvider (main.tsx) and any
// non-component code that needs to read/write the cache (e.g. seeding
// `['entry', id]` from search/recent results, used by the detail/edit
// screens — see src/hooks/useMemoryApi.ts).
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 1,
    },
  },
});
