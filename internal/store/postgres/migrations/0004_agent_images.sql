-- Optional picture per agent. Kept out of the agents table so listing agents never reads the bytes.
ALTER TABLE agents ADD COLUMN image_updated_at TIMESTAMPTZ;
CREATE TABLE agent_images (
    agent_id     UUID PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    content_type TEXT NOT NULL,
    data         BYTEA NOT NULL
);
