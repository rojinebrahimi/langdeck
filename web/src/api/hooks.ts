import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient, keepPreviousData } from "@tanstack/react-query";
import { api } from "./client";
import type {
  AuthMode,
  Agent, AgentConfig, AgentDepGraph, APIKey, AuditEntry, Command, Interrupt, Me, Run, RunEvent, Stats, User,
} from "./types";

export const useMe = (enabled = true) =>
  useQuery({ queryKey: ["me"], queryFn: () => api<Me>("/api/v1/me"), retry: false, staleTime: 60_000, enabled });

export const useAuthInfo = () =>
  useQuery({ queryKey: ["auth-info"], queryFn: () => api<{ mode: AuthMode; demo?: boolean; default_credentials?: string }>("/api/v1/auth/info") });

export const useStats = () => useQuery({ queryKey: ["stats"], queryFn: () => api<Stats>("/api/v1/stats"), refetchInterval: 30_000 });

/** Every agent (up to 500), for pickers such as the Runs filter. Lists use useAgentsPage. */
export const useAgents = () =>
  useQuery({ queryKey: ["agents", "all"], queryFn: () => api<AgentPage>("/api/v1/agents?limit=500").then((r) => r.agents), refetchInterval: 20_000 });

/** The declared dependency graph between agents. */
export const useAgentGraph = () =>
  useQuery({ queryKey: ["agents", "graph"], queryFn: () => api<AgentDepGraph>("/api/v1/agent-graph"), refetchInterval: 20_000 });

export interface AgentPage { agents: Agent[]; total: number; next_after: string | null }
export interface AgentPageQuery { q?: string; status?: string; after?: string; limit?: number }

/** One page of agents, keyset-paginated by name. Keeps the previous page on screen while the next loads. */
export const useAgentsPage = (f: AgentPageQuery) => {
  const qs = toQuery(f);
  return useQuery({
    queryKey: ["agents", "page", f],
    queryFn: () => api<AgentPage>(`/api/v1/agents?${qs}`),
    placeholderData: keepPreviousData,
    refetchInterval: 20_000,
  });
};

function toQuery(f: object) {
  return new URLSearchParams(
    Object.entries(f).filter(([, v]) => v !== undefined && v !== "").map(([k, v]) => [k, String(v)]),
  );
}

export const useAgent = (id: string) =>
  useQuery({ queryKey: ["agents", id], queryFn: () => api<{ agent: Agent; config: AgentConfig | null }>(`/api/v1/agents/${id}`), enabled: !!id });

export const useAgentConfigs = (id: string) =>
  useQuery({ queryKey: ["agents", id, "configs"], queryFn: () => api<{ configs: AgentConfig[] }>(`/api/v1/agents/${id}/configs`).then((r) => r.configs) });

export const useAgentCommands = (id: string) =>
  useQuery({ queryKey: ["agents", id, "commands"], queryFn: () => api<{ commands: Command[] }>(`/api/v1/agents/${id}/commands`).then((r) => r.commands) });

export interface RunFilters { agent_id?: string; status?: string; limit?: number; before?: string }
export interface RunPage { runs: Run[]; next_before: string | null }

/** One page of runs, newest first, keyset-paginated by creation time. */
export const useRunsPage = (f: RunFilters) => {
  const qs = toQuery(f);
  return useQuery({
    queryKey: ["runs", "page", f],
    queryFn: () => api<RunPage>(`/api/v1/runs?${qs}`),
    placeholderData: keepPreviousData,
  });
};

export const useRuns = (f: RunFilters = {}) => {
  const qs = new URLSearchParams(Object.entries(f).filter(([, v]) => v !== undefined && v !== "").map(([k, v]) => [k, String(v)]));
  return useQuery({ queryKey: ["runs", f], queryFn: () => api<{ runs: Run[] }>(`/api/v1/runs?${qs}`).then((r) => r.runs) });
};

export const useRun = (id: string) => useQuery({ queryKey: ["runs", "detail", id], queryFn: () => api<Run>(`/api/v1/runs/${id}`) });

export const useRunEvents = (id: string) =>
  useQuery({ queryKey: ["runs", "detail", id, "events"], queryFn: () => api<{ events: RunEvent[] }>(`/api/v1/runs/${id}/events?limit=5000`).then((r) => r.events) });

export const useInterrupts = (status = "pending") =>
  useQuery({ queryKey: ["interrupts", status], queryFn: () => api<{ interrupts: Interrupt[] }>(`/api/v1/interrupts?status=${status}`).then((r) => r.interrupts) });

export const useAPIKeys = () => useQuery({ queryKey: ["api-keys"], queryFn: () => api<{ api_keys: APIKey[] }>("/api/v1/api-keys").then((r) => r.api_keys) });
export const useUsers = () => useQuery({ queryKey: ["users"], queryFn: () => api<{ users: User[] }>("/api/v1/users").then((r) => r.users) });
export const useAudit = () => useQuery({ queryKey: ["audit"], queryFn: () => api<{ entries: AuditEntry[] }>("/api/v1/audit?limit=200").then((r) => r.entries) });

/** useAction wraps a mutation and refreshes everything once it lands. */
export function useAction<TVars, TOut = unknown>(fn: (v: TVars) => Promise<TOut>) {
  const qc = useQueryClient();
  return useMutation({ mutationFn: fn, onSuccess: () => qc.invalidateQueries() });
}

/**
 * useLiveUpdates subscribes to the server's SSE stream and invalidates the
 * matching queries, so every screen updates without polling.
 */
export function useLiveUpdates(enabled: boolean) {
  const qc = useQueryClient();
  const [connected, setConnected] = useState(false);
  useEffect(() => {
    if (!enabled || typeof EventSource === "undefined") return;
    const es = new EventSource("/api/v1/stream", { withCredentials: true });
    let timer: ReturnType<typeof setTimeout> | undefined;
    const pending = new Set<string>();
    // Coalesce bursts (a busy agent can emit many events per second).
    const flush = () => {
      for (const key of pending) qc.invalidateQueries({ queryKey: [key] });
      pending.clear();
      timer = undefined;
    };
    const mark = (...keys: string[]) => {
      keys.forEach((k) => pending.add(k));
      timer ??= setTimeout(flush, 1500);
    };
    es.onopen = () => setConnected(true);
    es.onerror = () => setConnected(false);
    es.addEventListener("agent.updated", () => mark("agents", "stats"));
    es.addEventListener("run.updated", () => mark("runs", "stats", "agents"));
    es.addEventListener("interrupt.updated", () => mark("interrupts", "runs", "stats"));
    es.addEventListener("command.created", () => mark("agents"));
    return () => {
      es.close();
      if (timer) clearTimeout(timer);
    };
  }, [enabled, qc]);
  return connected;
}
