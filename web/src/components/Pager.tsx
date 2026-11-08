import { useCallback, useState } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";

/**
 * Keyset pagination state. The server hands back an opaque cursor for the next
 * page; we keep the cursors we have visited so "Previous" works without offsets.
 * Call reset() whenever the filters change.
 */
export function useCursorPager() {
  const [stack, setStack] = useState<string[]>([""]);
  const page = stack.length;
  const cursor = stack[stack.length - 1];
  const next = useCallback((c: string) => setStack((s) => [...s, c]), []);
  const prev = useCallback(() => setStack((s) => (s.length > 1 ? s.slice(0, -1) : s)), []);
  const reset = useCallback(() => setStack([""]), []);
  return { page, cursor, next, prev, reset };
}

export function Pager({
  page,
  pageSize,
  shown,
  total,
  nextCursor,
  onNext,
  onPrev,
  busy,
  noun = "items",
}: {
  page: number;
  pageSize: number;
  shown: number;
  total?: number;
  nextCursor: string | null | undefined;
  onNext: (cursor: string) => void;
  onPrev: () => void;
  busy?: boolean;
  noun?: string;
}) {
  if (page === 1 && !nextCursor) {
    // Everything fits on one page: a count is enough.
    return total !== undefined && total > 0 ? (
      <p className="mt-4 text-center text-xs text-ink-400">
        {total} {noun}
      </p>
    ) : null;
  }
  const from = (page - 1) * pageSize + (shown ? 1 : 0);
  const to = (page - 1) * pageSize + shown;
  return (
    <nav aria-label="Pagination" className="mt-4 flex flex-wrap items-center justify-between gap-3 text-sm">
      <span className="text-ink-400" aria-live="polite">
        {shown ? `${from}–${to}` : "0"}
        {total !== undefined ? ` of ${total}` : ""} {noun}
        <span className="text-ink-500"> · page {page}</span>
      </span>
      <div className="flex gap-2">
        <button type="button" className="btn-ghost" onClick={onPrev} disabled={page === 1 || busy} aria-label="Previous page">
          <ChevronLeft className="h-4 w-4" /> Previous
        </button>
        <button
          type="button"
          className="btn-ghost"
          onClick={() => nextCursor && onNext(nextCursor)}
          disabled={!nextCursor || busy}
          aria-label="Next page"
        >
          Next <ChevronRight className="h-4 w-4" />
        </button>
      </div>
    </nav>
  );
}
