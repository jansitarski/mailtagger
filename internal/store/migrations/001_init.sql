-- accounts table stores Gmail account credentials and state
CREATE TABLE accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    encrypted_token BLOB NOT NULL,
    history_id TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- label_cache stores Gmail label mappings (name -> ID)
CREATE TABLE label_cache (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id INTEGER NOT NULL,
    label_name TEXT NOT NULL,
    label_id TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    UNIQUE (account_id, label_name)
);

CREATE INDEX idx_label_cache_account_id ON label_cache(account_id);

-- processed_messages tracks which messages have been processed to prevent duplicates
CREATE TABLE processed_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id INTEGER NOT NULL,
    message_id TEXT NOT NULL,
    processed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    UNIQUE (account_id, message_id)
);

CREATE INDEX idx_processed_messages_account_id ON processed_messages(account_id);
CREATE INDEX idx_processed_messages_processed_at ON processed_messages(processed_at);
