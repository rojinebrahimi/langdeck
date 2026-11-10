export type Role = "viewer" | "operator" | "admin" | "agent";
export type AgentStatus = "online" | "offline" | "paused";
export type RunStatus = "pending" | "running" | "interrupted" | "succeeded" | "failed" | "cancelled";
export type Json = unknown;

export interface Me {
  kind: "user" | "api_key";
  id: string;
  name: string;
  email: string;
  role: Role;
  csrf_token: string;
  auth_mode: AuthMode;
  must_change_password: boolean;
}

export type AuthMode = "local" | "oidc" | "dev";

export interface GraphNode { id: string; type?: string; data?: unknown }
export interface GraphEdge { source: string; target: string; conditional?: boolean; data?: unknown }
export interface AgentGraph { nodes: GraphNode[]; edges: GraphEdge[] }

export type AccessMode = "read" | "write" | "execute" | "delete" | "admin";
/** A resource an agent says it touches, and how. Self-reported by the SDK, not enforced. */
export interface Access { resource: string; modes: AccessMode[]; description?: string }

/** One agent in the fleet dependency graph; unregistered dependencies come back with registered=false. */
export interface AgentDepNode { id?: string; name: string; status: AgentStatus | "unknown"; registered: boolean; accesses: number }
export interface AgentDepGraph { nodes: AgentDepNode[]; edges: { source: string; target: string }[] }

export interface Agent {
  id: string;
  name: string;
  description: string;
  version: string;
  framework: string;
  graph?: AgentGraph;
  labels: Record<string, string>;
  accesses: Access[];
  depends_on: string[];
  desired_state: "running" | "paused";
  status: AgentStatus;
  instances: number;
  sdk_version: string;
  last_heartbeat?: string;
  created_at: string;
  updated_at: string;
}

export interface AgentConfig {
  agent_id: string;
  version: number;
  config: Record<string, Json>;
  comment: string;
  created_by: string;
  created_at: string;
}

export interface Run {
  id: string;
  agent_id: string;
  agent_name: string;
  thread_id?: string;
  status: RunStatus;
  trigger: string;
  input?: Json;
  output?: Json;
  error?: string;
  prompt_tokens: number;
  completion_tokens: number;
  created_by?: string;
  created_at: string;
  started_at?: string;
  ended_at?: string;
}

export interface RunEvent {
  id: number;
  run_id: string;
  type: string;
  node?: string;
  payload?: Json;
  latency_ms?: number;
  ts: string;
}

export interface Command {
  id: string;
  agent_id: string;
  run_id?: string;
  type: string;
  status: string;
  result?: string;
  created_by: string;
  created_at: string;
  acked_at?: string;
}

export interface Interrupt {
  id: string;
  run_id: string;
  agent_id: string;
  agent_name: string;
  node: string;
  question: string;
  payload?: Json;
  status: "pending" | "approved" | "rejected";
  response?: Json;
  resolved_by?: string;
  created_at: string;
  resolved_at?: string;
}

export interface APIKey {
  id: string;
  name: string;
  prefix: string;
  role: Role;
  agent_name?: string;
  created_by: string;
  created_at: string;
  expires_at?: string;
  last_used_at?: string;
  revoked_at?: string;
}

export interface User {
  id: string;
  email: string;
  name: string;
  role: Role;
  local: boolean;
  created_at: string;
  last_login_at?: string;
}

export interface AuditEntry {
  id: number;
  actor: string;
  action: string;
  target_type: string;
  target_id: string;
  details?: Json;
  ip: string;
  created_at: string;
}

export interface Stats {
  agents_total: number;
  agents_online: number;
  runs_last_24h: number;
  runs_by_status: Partial<Record<RunStatus, number>>;
  failure_rate: number;
  pending_interrupts: number;
  tokens_last_24h: number;
  p50_latency_ms: number;
  p95_latency_ms: number;
  hourly: { hour: string; succeeded: number; failed: number; other: number }[];
  top_agents: { agent_name: string; runs: number; failed: number }[];
}
