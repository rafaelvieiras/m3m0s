// TanStack Query hooks for the embedding configuration and the re-embedding
// job (D36). Progress is polled (the server has no push channel, D23) only
// while a run is in flight.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import * as api from "@/lib/api";
import type { EmbeddingConfigPayload, ReembedEstimatePayload } from "@/lib/schemas";

const IDLE_REFETCH_MS = 15000;
const RUNNING_REFETCH_MS = 2000;

export function useEmbeddingConfig() {
  return useQuery({
    queryKey: ["embedding-config"],
    queryFn: api.getEmbeddingConfig,
    refetchInterval: (q) => (q.state.data?.reembed.running ? RUNNING_REFETCH_MS : IDLE_REFETCH_MS),
  });
}

export function useReembedStatus(enabled: boolean) {
  return useQuery({
    queryKey: ["embedding-reembed"],
    queryFn: api.getReembedStatus,
    enabled,
    refetchInterval: (q) => (q.state.data?.running ? RUNNING_REFETCH_MS : false),
  });
}

function useInvalidateEmbedding() {
  const qc = useQueryClient();
  return () => {
    qc.invalidateQueries({ queryKey: ["embedding-config"] });
    qc.invalidateQueries({ queryKey: ["embedding-reembed"] });
    qc.invalidateQueries({ queryKey: ["jobs"] });
    qc.invalidateQueries({ queryKey: ["stats"] });
  };
}

export function useSaveEmbeddingConfig() {
  const invalidate = useInvalidateEmbedding();
  return useMutation({
    mutationFn: (p: EmbeddingConfigPayload) => api.putEmbeddingConfig(p),
    onSuccess: invalidate,
  });
}

export function useEstimateReembed() {
  return useMutation({ mutationFn: (p: ReembedEstimatePayload) => api.estimateReembed(p) });
}

export function useStartReembed() {
  const invalidate = useInvalidateEmbedding();
  return useMutation({ mutationFn: api.startReembed, onSuccess: invalidate });
}

export function useCancelReembed() {
  const invalidate = useInvalidateEmbedding();
  return useMutation({ mutationFn: api.cancelReembed, onSuccess: invalidate });
}
