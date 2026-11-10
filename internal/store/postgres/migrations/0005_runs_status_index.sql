-- Status-filtered run lists ordered by newest (the existing status index only covers active runs).
CREATE INDEX runs_status_created_idx ON runs (status, created_at DESC);
CREATE INDEX runs_thread_idx ON runs (thread_id, created_at DESC);
