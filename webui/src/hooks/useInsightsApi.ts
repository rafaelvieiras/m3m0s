// TanStack Query hooks for the memory-health dashboard (D37). The overview is
// cheap and read on demand; the analysis status is polled only while a run is
// in flight.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import * as api from "@/lib/api";

const RUNNING_REFETCH_MS = 2000;
const IDLE_REFETCH_MS = 30000;

export function useInsightsOverview() {
  return useQuery({
    queryKey: ["insights-overview"],
    queryFn: api.getInsightsOverview,
    refetchInterval: IDLE_REFETCH_MS,
  });
}

export function useHygiene() {
  return useQuery({
    queryKey: ["insights-hygiene"],
    queryFn: api.getHygiene,
    refetchInterval: (q) => (q.state.data?.status.running ? RUNNING_REFETCH_MS : false),
  });
}

function useInvalidateHygiene() {
  const qc = useQueryClient();
  return () => {
    qc.invalidateQueries({ queryKey: ["insights-hygiene"] });
    qc.invalidateQueries({ queryKey: ["jobs"] });
  };
}

export function useStartHygiene() {
  const invalidate = useInvalidateHygiene();
  return useMutation({ mutationFn: api.startHygiene, onSuccess: invalidate });
}

export function useCancelHygiene() {
  const invalidate = useInvalidateHygiene();
  return useMutation({ mutationFn: api.cancelHygiene, onSuccess: invalidate });
}
