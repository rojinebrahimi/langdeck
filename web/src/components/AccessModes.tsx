import clsx from "clsx";
import type { AccessMode } from "../api/types";

const tone: Record<AccessMode, string> = {
  read: "bg-sky-500/15 text-sky-300",
  write: "bg-amber-500/15 text-amber-300",
  execute: "bg-brand-500/20 text-brand-300",
  delete: "bg-rose-500/15 text-rose-300",
  admin: "bg-rose-500/25 text-rose-200",
};

/** One chip per access mode, colour-coded so write/delete/admin stand out from read. */
export function AccessModes({ modes }: { modes: AccessMode[] }) {
  return (
    <span className="flex flex-wrap gap-1.5">
      {modes.map((m) => (
        <span key={m} className={clsx("rounded-md px-1.5 py-0.5 font-mono text-xs font-medium", tone[m] ?? "bg-ink-800 text-ink-300")}>{m}</span>
      ))}
    </span>
  );
}
