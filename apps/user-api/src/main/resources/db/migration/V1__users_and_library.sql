CREATE TABLE user_data.users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    clerk_subject text NOT NULL UNIQUE CHECK (length(clerk_subject) > 0),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_data.library_entries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES user_data.users(id) ON DELETE CASCADE,
    game_id uuid NOT NULL,
    platform_id uuid,
    ownership text NOT NULL CHECK (ownership IN ('owned', 'wishlist', 'subscription')),
    snapshot_title text NOT NULL CHECK (length(snapshot_title) > 0),
    snapshot_cover_url text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, game_id)
);
CREATE INDEX library_user_page ON user_data.library_entries(user_id, created_at DESC, id DESC);
CREATE INDEX library_user_filters ON user_data.library_entries(user_id, ownership, platform_id);
