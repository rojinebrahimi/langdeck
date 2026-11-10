-- What each agent says it can access, and which other agents it depends on.
-- Nullable like graph: NULL means "not reported yet", and re-registering without them keeps the old value.
ALTER TABLE agents ADD COLUMN accesses JSONB;
ALTER TABLE agents ADD COLUMN depends_on JSONB;
