CREATE TABLE user_data.game_statuses (
    library_entry_id uuid PRIMARY KEY REFERENCES user_data.library_entries(id) ON DELETE CASCADE,
    status text NOT NULL CHECK (status IN ('backlog', 'playing', 'paused', 'completed', 'dropped')),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX game_status_filter ON user_data.game_statuses(status, library_entry_id);
INSERT INTO user_data.game_statuses(library_entry_id, status)
SELECT id, 'backlog' FROM user_data.library_entries;

CREATE TABLE user_data.favorites (
    library_entry_id uuid PRIMARY KEY REFERENCES user_data.library_entries(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_data.ratings (
    library_entry_id uuid PRIMARY KEY REFERENCES user_data.library_entries(id) ON DELETE CASCADE,
    value integer NOT NULL CHECK (value BETWEEN 1 AND 10),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_data.notes (
    library_entry_id uuid PRIMARY KEY REFERENCES user_data.library_entries(id) ON DELETE CASCADE,
    text text NOT NULL CHECK (length(text) <= 10000),
    updated_at timestamptz NOT NULL DEFAULT now()
);
