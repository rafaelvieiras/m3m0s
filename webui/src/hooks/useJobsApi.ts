// TanStack Query hooks for in-process jobs (GET/POST/PATCH /jobs, D27) —
// same convention as useMemoryApi.ts: the single data access point for
// components, no raw fetch outside here / src/lib/api.ts.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import * as api from "@/lib/api";

// Short refetchInterval — m3m0s has no SSE/websocket infrastructure (D23),
// so polling is the only way to reflect "running now"/completion of a run
// without a manual F5.
const JOBS_REFETCH_INTERVAL_MS = 5000;

export function useJobs() {
  return useQuery({
    queryKey: ["jobs"],
    queryFn: api.listJobs,
    refetchInterval: JOBS_REFETCH_INTERVAL_MS,
  });
}

export function useTriggerJob() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api.runJobNow(name),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["jobs"] });
    },
  });
}

export function useSetJobInterval() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ name, seconds }: { name: string; seconds: number }) =>
      api.setJobInterval(name, seconds),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["jobs"] });
    },
  });
}
