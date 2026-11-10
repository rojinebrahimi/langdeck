import { useState } from "react";
import { Bot } from "lucide-react";
import clsx from "clsx";
import type { Agent } from "../api/types";

/** The agent's uploaded picture, or the default bot icon when there is none (or it fails to load). */
export function AgentAvatar({ agent, className }: { agent: Pick<Agent, "id" | "name" | "image_updated_at">; className?: string }) {
  const [broken, setBroken] = useState(false);
  const src = agent.image_updated_at && !broken ? `/api/v1/agents/${agent.id}/image?v=${encodeURIComponent(agent.image_updated_at)}` : "";
  return (
    <div className={clsx("flex shrink-0 items-center justify-center overflow-hidden rounded-xl bg-brand-500/15 text-brand-300 ring-1 ring-brand-400/20", className ?? "h-10 w-10")}>
      {src ? (
        <img src={src} alt={`${agent.name} picture`} loading="lazy" decoding="async" className="h-full w-full object-cover" onError={() => setBroken(true)} />
      ) : (
        <Bot className="h-1/2 w-1/2" aria-hidden />
      )}
    </div>
  );
}
