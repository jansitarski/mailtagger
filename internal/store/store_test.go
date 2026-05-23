package store

import (
	"testing"
)

// testStore creates an in-memory store for testing.
func testStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(":memory:", 30)
	if err != nil {
		t.Fatalf("failed to open test store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to migrate test store: %v", err)
	}
	return store
}

func TestStore_Open(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	if store.db == nil {
		t.Error("expected db to be initialized")
	}
	if store.retentionDays != 30 {
		t.Errorf("expected retention days to be 30, got %d", store.retentionDays)
	}
}

func TestStore_Migrate(t *testing.T) {
	store, err := Open(":memory:", 30)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	// Run migrations
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Check that tables exist
	tables := []string{"accounts", "label_cache", "processed_messages", "schema_migrations"}
	for _, table := range tables {
		var exists bool
		err := store.db.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM sqlite_master
				WHERE type='table' AND name=?
			)
		`, table).Scan(&exists)
		if err != nil {
			t.Fatalf("failed to check table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s does not exist", table)
		}
	}

	// Run migrations again (should be idempotent)
	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to run migrations again: %v", err)
	}
}

func TestStore_AccountCRUD(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	// Test InsertAccount
	email := "test@example.com"
	token := []byte("encrypted-token-data")
	acc, err := store.InsertAccount(email, token)
	if err != nil {
		t.Fatalf("failed to insert account: %v", err)
	}
	if acc.ID == 0 {
		t.Error("expected account ID to be set")
	}
	if acc.Email != email {
		t.Errorf("expected email %s, got %s", email, acc.Email)
	}
	if string(acc.EncryptedToken) != string(token) {
		t.Error("token mismatch")
	}

	// Test GetAccount
	acc2, err := store.GetAccount(acc.ID)
	if err != nil {
		t.Fatalf("failed to get account: %v", err)
	}
	if acc2.Email != email {
		t.Errorf("expected email %s, got %s", email, acc2.Email)
	}

	// Test GetAccountByEmail
	acc3, err := store.GetAccountByEmail(email)
	if err != nil {
		t.Fatalf("failed to get account by email: %v", err)
	}
	if acc3.ID != acc.ID {
		t.Errorf("expected account ID %d, got %d", acc.ID, acc3.ID)
	}

	// Test ListAccounts
	accounts, err := store.ListAccounts()
	if err != nil {
		t.Fatalf("failed to list accounts: %v", err)
	}
	if len(accounts) != 1 {
		t.Errorf("expected 1 account, got %d", len(accounts))
	}

	// Test UpdateHistoryID
	historyID := "12345"
	if err := store.UpdateHistoryID(acc.ID, historyID); err != nil {
		t.Fatalf("failed to update history ID: %v", err)
	}
	acc4, err := store.GetAccount(acc.ID)
	if err != nil {
		t.Fatalf("failed to get account: %v", err)
	}
	if acc4.HistoryID != historyID {
		t.Errorf("expected history ID %s, got %s", historyID, acc4.HistoryID)
	}
}

func TestStore_LabelCache(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	// Create an account first
	acc, err := store.InsertAccount("test@example.com", []byte("token"))
	if err != nil {
		t.Fatalf("failed to insert account: %v", err)
	}

	// Test UpsertLabel (insert)
	labelName := "Important"
	labelID := "Label_123"
	if err := store.UpsertLabel(acc.ID, labelName, labelID); err != nil {
		t.Fatalf("failed to upsert label: %v", err)
	}

	// Test GetLabel
	label, err := store.GetLabel(acc.ID, labelName)
	if err != nil {
		t.Fatalf("failed to get label: %v", err)
	}
	if label.LabelName != labelName {
		t.Errorf("expected label name %s, got %s", labelName, label.LabelName)
	}
	if label.LabelID != labelID {
		t.Errorf("expected label ID %s, got %s", labelID, label.LabelID)
	}

	// Test UpsertLabel (update)
	newLabelID := "Label_456"
	if err := store.UpsertLabel(acc.ID, labelName, newLabelID); err != nil {
		t.Fatalf("failed to upsert label: %v", err)
	}
	label2, err := store.GetLabel(acc.ID, labelName)
	if err != nil {
		t.Fatalf("failed to get label: %v", err)
	}
	if label2.LabelID != newLabelID {
		t.Errorf("expected label ID %s, got %s", newLabelID, label2.LabelID)
	}

	// Test ListLabels
	if err := store.UpsertLabel(acc.ID, "Work", "Label_789"); err != nil {
		t.Fatalf("failed to upsert second label: %v", err)
	}
	labels, err := store.ListLabels(acc.ID)
	if err != nil {
		t.Fatalf("failed to list labels: %v", err)
	}
	if len(labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(labels))
	}
}

func TestStore_ProcessedMessages(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	// Create an account first
	acc, err := store.InsertAccount("test@example.com", []byte("token"))
	if err != nil {
		t.Fatalf("failed to insert account: %v", err)
	}

	// Test InsertProcessedMessage
	messageID := "msg_123"
	if err := store.InsertProcessedMessage(acc.ID, messageID); err != nil {
		t.Fatalf("failed to insert processed message: %v", err)
	}

	// Test ProcessedMessageExists
	exists, err := store.ProcessedMessageExists(acc.ID, messageID)
	if err != nil {
		t.Fatalf("failed to check processed message: %v", err)
	}
	if !exists {
		t.Error("expected message to exist")
	}

	// Test non-existent message
	exists2, err := store.ProcessedMessageExists(acc.ID, "non-existent")
	if err != nil {
		t.Fatalf("failed to check non-existent message: %v", err)
	}
	if exists2 {
		t.Error("expected message to not exist")
	}

	// Test GarbageCollectProcessedMessages
	// Insert message with old timestamp
	_, err = store.db.Exec(`
		INSERT INTO processed_messages (account_id, message_id, processed_at)
		VALUES (?, ?, datetime('now', '-31 days'))
	`, acc.ID, "old_msg")
	if err != nil {
		t.Fatalf("failed to insert old message: %v", err)
	}

	deleted, err := store.GarbageCollectProcessedMessages(30)
	if err != nil {
		t.Fatalf("failed to garbage collect: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted message, got %d", deleted)
	}

	// Check that recent message still exists
	exists3, err := store.ProcessedMessageExists(acc.ID, messageID)
	if err != nil {
		t.Fatalf("failed to check recent message: %v", err)
	}
	if !exists3 {
		t.Error("expected recent message to still exist")
	}
}

func TestStore_Crypto(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef") // 32 bytes for AES-256
	plaintext := []byte("secret token data")

	// Test Encrypt
	ciphertext, err := EncryptToken(plaintext, key)
	if err != nil {
		t.Fatalf("failed to encrypt: %v", err)
	}
	if len(ciphertext) == 0 {
		t.Error("expected non-empty ciphertext")
	}

	// Test Decrypt
	decrypted, err := DecryptToken(ciphertext, key)
	if err != nil {
		t.Fatalf("failed to decrypt: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Errorf("expected plaintext %s, got %s", string(plaintext), string(decrypted))
	}

	// Test decrypt with wrong key
	wrongKey := []byte("wrongkey0123456789abcdef01234567")
	_, err = DecryptToken(ciphertext, wrongKey)
	if err == nil {
		t.Error("expected decryption to fail with wrong key")
	}

	// Test decrypt with invalid ciphertext
	_, err = DecryptToken([]byte("invalid"), key)
	if err == nil {
		t.Error("expected decryption to fail with invalid ciphertext")
	}
}

func TestStore_GCLoop(t *testing.T) {
	// Create store with short retention for testing
	store, err := Open(":memory:", 1)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	// Create an account
	acc, err := store.InsertAccount("test@example.com", []byte("token"))
	if err != nil {
		t.Fatalf("failed to insert account: %v", err)
	}

	// Insert old message
	_, err = store.db.Exec(`
		INSERT INTO processed_messages (account_id, message_id, processed_at)
		VALUES (?, ?, datetime('now', '-2 days'))
	`, acc.ID, "old_msg")
	if err != nil {
		t.Fatalf("failed to insert old message: %v", err)
	}

	// Manually trigger GC
	store.runGC()

	// Check that old message was deleted
	exists, err := store.ProcessedMessageExists(acc.ID, "old_msg")
	if err != nil {
		t.Fatalf("failed to check message: %v", err)
	}
	if exists {
		t.Error("expected old message to be garbage collected")
	}
}
