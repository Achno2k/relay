CREATE TABLE sessions (
    id            TEXT PRIMARY KEY,
    agent_name    TEXT NOT NULL,
    kind          TEXT NOT NULL,
    repo          TEXT NOT NULL,
    branch        TEXT NOT NULL,
    worktree_path TEXT NOT NULL,
    slack_channel TEXT NOT NULL DEFAULT '',
    thread_ts     TEXT NOT NULL DEFAULT '',
    last_sent_ts  TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL,
    attached_by   TEXT NOT NULL DEFAULT '',
    attached_at   INTEGER,
    created_at    INTEGER NOT NULL,
    last_active   INTEGER NOT NULL
);

CREATE UNIQUE INDEX sessions_agent_name ON sessions (agent_name);

-- One thread is one session. Sessions started from the terminal have no thread,
-- so the index is partial and many of them can coexist.
CREATE UNIQUE INDEX sessions_thread
    ON sessions (slack_channel, thread_ts)
    WHERE slack_channel <> '' AND thread_ts <> '';

CREATE INDEX sessions_status ON sessions (status);
