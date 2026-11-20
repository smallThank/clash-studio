package studio

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/mattn/go-sqlite3"
)

const schemaVersion = 1

const schema = `
CREATE TABLE users (
	id TEXT PRIMARY KEY NOT NULL,
	username TEXT NOT NULL,
	username_normalized TEXT NOT NULL UNIQUE,
	password_hash BLOB NOT NULL,
	created_at INTEGER NOT NULL
);

CREATE TABLE sessions (
	session_digest BLOB PRIMARY KEY NOT NULL,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX sessions_by_expiry ON sessions(expires_at);

CREATE TABLE profiles (
	id TEXT PRIMARY KEY NOT NULL,
	owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	target TEXT NOT NULL CHECK(target IN ('openclash', 'verge', 'both')),
	draft_yaml BLOB NOT NULL,
	draft_rev INTEGER NOT NULL CHECK(draft_rev > 0),
	published_version_id TEXT,
	updated_at INTEGER NOT NULL,
	FOREIGN KEY (published_version_id, id)
		REFERENCES profile_versions(id, profile_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX profiles_by_owner ON profiles(owner_id, updated_at DESC, id);

CREATE TABLE profile_versions (
	id TEXT PRIMARY KEY NOT NULL,
	profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
	version INTEGER NOT NULL CHECK(version > 0),
	draft_rev INTEGER NOT NULL CHECK(draft_rev > 0),
	yaml BLOB NOT NULL,
	content_sha256 BLOB NOT NULL,
	created_at INTEGER NOT NULL,
	UNIQUE(id, profile_id),
	UNIQUE(profile_id, version),
	UNIQUE(profile_id, draft_rev)
);

CREATE TABLE subscription_links (
	id TEXT PRIMARY KEY NOT NULL,
	profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
	label TEXT NOT NULL,
	token_digest BLOB NOT NULL UNIQUE,
	token BLOB NOT NULL,
	created_at INTEGER NOT NULL,
	revoked_at INTEGER
);
CREATE INDEX links_by_profile ON subscription_links(profile_id, created_at, id);

CREATE TABLE profile_imports (
	profile_id TEXT PRIMARY KEY NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
	source_url BLOB NOT NULL,
	user_agent BLOB NOT NULL,
	base_yaml BLOB NOT NULL,
	content_sha256 BLOB NOT NULL,
	last_fetched_at INTEGER NOT NULL
);

CREATE TABLE import_stages (
	id TEXT PRIMARY KEY NOT NULL,
	owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	profile_id TEXT REFERENCES profiles(id) ON DELETE CASCADE,
	draft_rev INTEGER,
	base_sha256 BLOB,
	yaml BLOB NOT NULL,
	source_url BLOB,
	user_agent BLOB,
	expires_at INTEGER NOT NULL
);
CREATE INDEX stages_by_expiry ON import_stages(expires_at);
CREATE INDEX stages_by_owner ON import_stages(owner_id, expires_at);
`

func migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start SQLite migration: %w", err)
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read SQLite schema version: %w", err)
	}
	switch version {
	case schemaVersion:
		return tx.Commit()
	case 0:
		if _, err := tx.ExecContext(ctx, schema); err != nil {
			return fmt.Errorf("create SQLite schema: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "PRAGMA user_version = 1"); err != nil {
			return fmt.Errorf("record SQLite schema version: %w", err)
		}
		return tx.Commit()
	default:
		return fmt.Errorf("unsupported SQLite schema version %d", version)
	}
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("the storage encryption key must contain 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

const encryptionVersion byte = 1

func (s *Server) seal(value, purpose string) []byte {
	// A per-value random nonce and purpose binding prevent replay across fields.
	size := s.aead.NonceSize() + 1
	data := make([]byte, size, size+len(value)+s.aead.Overhead())
	data[0] = encryptionVersion
	if _, err := rand.Read(data[1:]); err != nil {
		panic(err)
	}
	return s.aead.Seal(data, data[1:], []byte(value), []byte(purpose))
}

func (s *Server) unseal(data []byte, purpose string) (string, error) {
	start := s.aead.NonceSize() + 1
	if len(data) < start || data[0] != encryptionVersion {
		return "", errors.New("unsupported encrypted value")
	}
	cleartext, err := s.aead.Open(nil, data[1:start], data[start:], []byte(purpose))
	if err != nil {
		return "", err
	}
	return string(cleartext), nil
}

func randomToken(bytes int) string {
	data := make([]byte, bytes)
	if _, err := rand.Read(data); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func digest(value string) []byte {
	hash := sha256.Sum256([]byte(value))
	return hash[:]
}

func sqliteUnique(err error) bool {
	var sqliteErr sqlite3.Error
	return errors.As(err, &sqliteErr) &&
		(sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique ||
			sqliteErr.ExtendedCode == sqlite3.ErrConstraintPrimaryKey)
}
