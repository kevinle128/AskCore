CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at DATETIME,
  updated_at DATETIME,
  deleted_at DATETIME,
  name TEXT NOT NULL,
  email TEXT NOT NULL UNIQUE
);
CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON users (deleted_at);

CREATE TABLE IF NOT EXISTS posts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at DATETIME,
  updated_at DATETIME,
  deleted_at DATETIME,
  title TEXT NOT NULL,
  content TEXT,
  published NUMERIC NOT NULL DEFAULT false,
  author_id INTEGER NOT NULL REFERENCES users (id)
);
CREATE INDEX IF NOT EXISTS idx_posts_deleted_at ON posts (deleted_at);
CREATE INDEX IF NOT EXISTS idx_posts_author_id ON posts (author_id);
