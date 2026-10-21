// TanStack Query hooks for the account endpoints (D35, docs/api-contract.md,
// "Accounts"). Same rule as useMemoryApi: components never call fetch.
import { useMutation, useQuery } from "@tanstack/react-query";
import * as api from "@/lib/api";

export function useAuthStatus() {
  return useQuery({ queryKey: ["auth-status"], queryFn: api.getAuthStatus, retry: false });
}

export function useResetInfo(token: string) {
  return useQuery({
    queryKey: ["reset-info", token],
    queryFn: () => api.getResetInfo(token),
    enabled: token.length > 0,
    retry: false,
  });
}

export function useLogin() {
  return useMutation({
    mutationFn: (v: { username: string; password: string }) => api.login(v.username, v.password),
  });
}

export function useOnboard() {
  return useMutation({
    mutationFn: async (v: { username: string; password: string; confirm: string }) => {
      await api.onboard(v.username, v.password, v.confirm);
    },
  });
}

export function useResetPassword() {
  return useMutation({
    mutationFn: (v: { token: string; password: string; confirm: string }) =>
      api.resetPassword(v.token, v.password, v.confirm),
  });
}
