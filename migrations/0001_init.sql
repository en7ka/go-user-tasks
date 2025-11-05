-- 0001_init.sql
CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    points BIGINT NOT NULL DEFAULT 0,
    referrer_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS tasks (
    code TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    points BIGINT NOT NULL CHECK (points >= 0)
);

CREATE TABLE IF NOT EXISTS user_tasks (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    task_code TEXT NOT NULL REFERENCES tasks(code) ON DELETE CASCADE,
    completed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, task_code)
);

CREATE TABLE IF NOT EXISTS referrals (
    id BIGSERIAL PRIMARY KEY,
    referrer_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    referred_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bonus_referrer BIGINT NOT NULL,
    bonus_referred BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (referrer_id, referred_id)
);

-- seed some tasks
INSERT INTO tasks (code, title, points) VALUES
    ('subscribe_telegram', 'Subscribe to Telegram channel', 20),
    ('subscribe_twitter', 'Follow on Twitter/X', 20),
    ('enter_referral_code', 'Enter referral code', 10),
    ('complete_profile', 'Complete profile info', 15),
    ('daily_checkin', 'Daily check-in', 5)
ON CONFLICT (code) DO NOTHING;
