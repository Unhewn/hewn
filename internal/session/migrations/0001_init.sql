-- sessions, messages, and tool_calls per HEWN.md §3's v0.1 schema.
--
-- The approvals table from that section is deliberately not created here:
-- there is no code yet that reads or writes cross-session approval memory
-- (tool.Policy's allow-session state is still in-process only), and an
-- unused table is dead schema, not a documented seam. It becomes its own
-- forward-only migration when that feature is actually built.

CREATE TABLE sessions (
  id           TEXT PRIMARY KEY,             -- ULID
  parent_id    TEXT REFERENCES sessions(id), -- forking, unused in v0.1
  fork_point   INTEGER,                      -- message seq forked from
  title        TEXT,
  cwd          TEXT NOT NULL,
  provider     TEXT NOT NULL,
  model        TEXT NOT NULL,
  created_at   INTEGER NOT NULL,             -- unix millis
  updated_at   INTEGER NOT NULL              -- unix millis
);

CREATE INDEX idx_sessions_updated_at ON sessions(updated_at DESC);

CREATE TABLE messages (
  id            TEXT PRIMARY KEY,
  session_id    TEXT NOT NULL REFERENCES sessions(id),
  seq           INTEGER NOT NULL,
  role          TEXT NOT NULL,               -- user|assistant|tool|system
  content       TEXT NOT NULL,               -- JSON: array of content blocks
  input_tokens  INTEGER,
  output_tokens INTEGER,
  created_at    INTEGER NOT NULL,            -- unix millis
  UNIQUE(session_id, seq)
);

CREATE TABLE tool_calls (
  id          TEXT PRIMARY KEY,
  message_id  TEXT NOT NULL REFERENCES messages(id),
  tool        TEXT NOT NULL,
  params      TEXT NOT NULL,
  result      TEXT,
  is_error    INTEGER NOT NULL DEFAULT 0,    -- addition beyond HEWN.md's literal schema: tool.Result.IsError
                                              -- is real, useful data (agent.ToolCallResult already carries
                                              -- it) that the given schema had nowhere to put
  approved    INTEGER,                       -- 0 denied, 1 once, 2 session-wide; NULL = not gated (read-only tool)
  duration_ms INTEGER,
  created_at  INTEGER NOT NULL               -- unix millis
);

CREATE INDEX idx_tool_calls_message_id ON tool_calls(message_id);
