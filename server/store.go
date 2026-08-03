package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const (
	defaultLocationName = "Asia/Shanghai"
	maxMessageBatch     = 500
)

// Store owns the SQLite connection and the time zone used for day derivation.
type Store struct {
	db  *sql.DB
	loc *time.Location
}

// MessageInput is the wire shape accepted by the ingestion endpoint.
type MessageInput struct {
	ClientMsgID string `json:"client_msg_id"`
	App         string `json:"app"`
	AppName     string `json:"app_name"`
	Chat        string `json:"chat"`
	Sender      string `json:"sender"`
	Content     string `json:"content"`
	TS          int64  `json:"ts"`
}

// MessageRecord is the wire shape returned by the messages endpoint.
type MessageRecord struct {
	ID          int64  `json:"id"`
	ClientMsgID string `json:"client_msg_id"`
	App         string `json:"app"`
	AppName     string `json:"app_name"`
	Chat        string `json:"chat"`
	Sender      string `json:"sender"`
	Content     string `json:"content"`
	TS          int64  `json:"ts"`
	Day         string `json:"day"`
}

// AppSummary is one grouped row returned by the apps endpoint.
type AppSummary struct {
	App     string `json:"app"`
	AppName string `json:"app_name"`
	Count   int64  `json:"count"`
	LastTS  int64  `json:"last_ts"`
}

var (
	ErrUserExists            = errors.New("user already exists")
	ErrUserNotFound          = errors.New("user not found")
	ErrAuthorizationNotFound = errors.New("authorization code not found")
	ErrAuthorizationExpired  = errors.New("authorization code expired")
	ErrAuthorizationUsed     = errors.New("authorization code already used")
	ErrAuthorizationClient   = errors.New("authorization client mismatch")
	ErrAuthorizationRedirect = errors.New("authorization redirect mismatch")
	ErrAuthorizationPKCE     = errors.New("authorization PKCE verification failed")
	ErrDeviceNotFound        = errors.New("device not found")
	ErrDeviceSecretInvalid   = errors.New("device secret is invalid")
)

const (
	sessionTTL                  = 7 * 24 * time.Hour
	accessTokenTTL              = 7 * 24 * time.Hour
	authorizationCodeTTL        = 5 * time.Minute
	authorizationCodeByteLength = 16
	accessTokenByteLength       = 16
	sessionByteLength           = 16
)

// User is an account that owns bound devices and query credentials.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

// OpenStore opens path, creates the schema if needed, and derives days in loc.
func OpenStore(path string, loc *time.Location) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is required")
	}
	if loc == nil {
		loc = time.FixedZone(defaultLocationName, 8*60*60)
	}
	if path != ":memory:" {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// A single connection keeps :memory: databases and migrations deterministic,
	// while SQLite still serializes writes correctly for the file-backed server.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db, loc: loc}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS devices (
    device_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    secret TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    user_id INTEGER,
    bound_at INTEGER
);
CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    client_msg_id TEXT NOT NULL UNIQUE,
    device_id TEXT NOT NULL,
    app TEXT NOT NULL,
    app_name TEXT NOT NULL,
    chat TEXT NOT NULL,
    sender TEXT NOT NULL,
    content TEXT NOT NULL,
    ts INTEGER NOT NULL,
    day TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_day_device_app
    ON messages(day, device_id, app);
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE TABLE IF NOT EXISTS tokens (
    token_hash TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tokens_user_id ON tokens(user_id);
CREATE TABLE IF NOT EXISTS oauth_codes (
    code_hash TEXT PRIMARY KEY,
    client_id TEXT NOT NULL,
    user_id INTEGER NOT NULL,
    redirect_uri TEXT NOT NULL,
    code_challenge TEXT NOT NULL,
    expires_at INTEGER NOT NULL,
    used_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_oauth_codes_expires_at ON oauth_codes(expires_at);
DROP TABLE IF EXISTS device_grants;
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
    content,
    sender,
    chat,
    content='messages',
    content_rowid='id'
);
`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("migrate sqlite: %w", err)
	}

	columns, err := s.deviceColumns()
	if err != nil {
		return err
	}
	for name, definition := range map[string]string{
		"user_id":  "INTEGER",
		"bound_at": "INTEGER",
	} {
		if columns[name] {
			continue
		}
		if _, err := s.db.Exec("ALTER TABLE devices ADD COLUMN " + name + " " + definition); err != nil {
			return fmt.Errorf("add devices.%s: %w", name, err)
		}
	}

	const ftsTriggers = `
CREATE TRIGGER IF NOT EXISTS messages_fts_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, content, sender, chat)
    VALUES (new.id, new.content, new.sender, new.chat);
END;
CREATE TRIGGER IF NOT EXISTS messages_fts_ad AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, content, sender, chat)
    VALUES ('delete', old.id, old.content, old.sender, old.chat);
END;
CREATE TRIGGER IF NOT EXISTS messages_fts_au AFTER UPDATE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, content, sender, chat)
    VALUES ('delete', old.id, old.content, old.sender, old.chat);
    INSERT INTO messages_fts(rowid, content, sender, chat)
    VALUES (new.id, new.content, new.sender, new.chat);
END;
`
	if _, err := s.db.Exec(ftsTriggers); err != nil {
		return fmt.Errorf("create messages FTS triggers: %w", err)
	}
	if _, err := s.db.Exec(`INSERT INTO messages_fts(messages_fts) VALUES ('rebuild')`); err != nil {
		return fmt.Errorf("backfill messages FTS: %w", err)
	}
	return nil
}

func (s *Store) deviceColumns() (map[string]bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(devices)`)
	if err != nil {
		return nil, fmt.Errorf("inspect devices schema: %w", err)
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var (
			cid        int
			name       string
			dataType   string
			notNull    int
			defaultV   sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultV, &primaryKey); err != nil {
			return nil, fmt.Errorf("scan devices schema: %w", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate devices schema: %w", err)
	}
	return columns, nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// RegisterDevice creates or replaces a device's name and secret.
func (s *Store) RegisterDevice(deviceID, name, secret string) error {
	_, err := s.db.Exec(`
INSERT INTO devices(device_id, name, secret, created_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(device_id) DO UPDATE SET name = excluded.name, secret = excluded.secret
`, deviceID, name, secret, time.Now().UTC().Unix())
	if err != nil {
		return fmt.Errorf("register device: %w", err)
	}
	return nil
}

// AuthenticateDevice reports whether secret is the current secret for deviceID.
func (s *Store) AuthenticateDevice(deviceID, secret string) (bool, error) {
	var stored string
	err := s.db.QueryRow(`SELECT secret FROM devices WHERE device_id = ?`, deviceID).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("authenticate device: %w", err)
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(secret)) == 1, nil
}

// RegisterUser hashes password and creates a unique account.
func (s *Store) RegisterUser(username, password string) (User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return User{}, errors.New("username is required")
	}
	if len(password) < 8 {
		return User{}, errors.New("password must be at least 8 characters")
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}
	now := time.Now().UTC()
	result, err := s.db.Exec(`
INSERT INTO users(username, password_hash, created_at)
VALUES (?, ?, ?)
ON CONFLICT(username) DO NOTHING
`, username, string(passwordHash), now.Unix())
	if err != nil {
		return User{}, fmt.Errorf("register user: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return User{}, fmt.Errorf("count user registration: %w", err)
	}
	if affected == 0 {
		return User{}, ErrUserExists
	}
	id, err := result.LastInsertId()
	if err != nil {
		return User{}, fmt.Errorf("read user id: %w", err)
	}
	return User{ID: id, Username: username, PasswordHash: string(passwordHash), CreatedAt: now}, nil
}

// FindUserByUsername returns an account and its bcrypt password hash.
func (s *Store) FindUserByUsername(username string) (User, error) {
	var (
		user      User
		createdAt int64
	)
	err := s.db.QueryRow(`
SELECT id, username, password_hash, created_at
FROM users
WHERE username = ?
`, strings.TrimSpace(username)).Scan(&user.ID, &user.Username, &user.PasswordHash, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("find user: %w", err)
	}
	user.CreatedAt = time.Unix(createdAt, 0).UTC()
	return user, nil
}

// FindUserByID returns an account by its database id.
func (s *Store) FindUserByID(id int64) (User, error) {
	var (
		user      User
		createdAt int64
	)
	err := s.db.QueryRow(`
SELECT id, username, password_hash, created_at
FROM users
WHERE id = ?
`, id).Scan(&user.ID, &user.Username, &user.PasswordHash, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("find user by id: %w", err)
	}
	user.CreatedAt = time.Unix(createdAt, 0).UTC()
	return user, nil
}

// CreateSession creates a raw cookie token while storing only its SHA-256 hash.
func (s *Store) CreateSession(userID int64, requestedTTL ...time.Duration) (string, error) {
	ttl := sessionTTL
	if len(requestedTTL) > 0 {
		ttl = requestedTTL[0]
	}
	return s.issueCredential("sessions", userID, ttl, sessionByteLength)
}

// ValidateSession resolves an unexpired session cookie to its account.
func (s *Store) ValidateSession(rawToken string) (User, bool, error) {
	if rawToken == "" {
		return User{}, false, nil
	}
	var (
		user      User
		createdAt int64
	)
	err := s.db.QueryRow(`
SELECT u.id, u.username, u.password_hash, u.created_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = ? AND s.expires_at > ?
`, hashCredential(rawToken), time.Now().UTC().Unix()).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, fmt.Errorf("validate session: %w", err)
	}
	user.CreatedAt = time.Unix(createdAt, 0).UTC()
	return user, true, nil
}

// IssueAccessToken creates a raw API token while storing only its SHA-256 hash.
func (s *Store) IssueAccessToken(userID int64, ttl time.Duration) (string, error) {
	return s.issueCredential("tokens", userID, ttl, accessTokenByteLength)
}

func (s *Store) issueCredential(table string, userID int64, ttl time.Duration, byteLength int) (string, error) {
	for attempt := 0; attempt < 5; attempt++ {
		raw, err := randomHex(byteLength)
		if err != nil {
			return "", fmt.Errorf("generate credential: %w", err)
		}
		_, err = s.db.Exec(
			"INSERT INTO "+table+"(token_hash, user_id, expires_at) VALUES (?, ?, ?)",
			hashCredential(raw),
			userID,
			time.Now().UTC().Add(ttl).Unix(),
		)
		if err == nil {
			return raw, nil
		}
		if !strings.Contains(strings.ToLower(err.Error()), "unique") {
			return "", fmt.Errorf("store credential: %w", err)
		}
	}
	return "", errors.New("could not generate unique credential")
}

// ValidateAccessToken returns the owner of an unexpired API token.
func (s *Store) ValidateAccessToken(rawToken string) (int64, bool, error) {
	if rawToken == "" {
		return 0, false, nil
	}
	var userID int64
	err := s.db.QueryRow(`
SELECT user_id
FROM tokens
WHERE token_hash = ? AND expires_at > ?
`, hashCredential(rawToken), time.Now().UTC().Unix()).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("validate access token: %w", err)
	}
	return userID, true, nil
}

// CreateAuthorizationCode issues a single-use authorization code, storing
// only its SHA-256 hash along with the bindings it was issued for.
func (s *Store) CreateAuthorizationCode(clientID string, userID int64, redirectURI, codeChallenge string, ttl time.Duration) (string, error) {
	for attempt := 0; attempt < 5; attempt++ {
		raw, err := randomHex(authorizationCodeByteLength)
		if err != nil {
			return "", fmt.Errorf("generate authorization code: %w", err)
		}
		_, err = s.db.Exec(`
INSERT INTO oauth_codes(code_hash, client_id, user_id, redirect_uri, code_challenge, expires_at)
VALUES (?, ?, ?, ?, ?, ?)
`, hashCredential(raw), clientID, userID, redirectURI, codeChallenge, time.Now().UTC().Add(ttl).Unix())
		if err == nil {
			return raw, nil
		}
		if !strings.Contains(strings.ToLower(err.Error()), "unique") {
			return "", fmt.Errorf("store authorization code: %w", err)
		}
	}
	return "", errors.New("could not generate unique authorization code")
}

// ConsumeAuthorizationCode validates an authorization code against the client,
// redirect URI, and PKCE verifier, then atomically marks it used. The code can
// only be redeemed once within its TTL and by the client it was issued to.
func (s *Store) ConsumeAuthorizationCode(rawCode, codeVerifier, clientID, redirectURI string, now time.Time) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin authorization code redemption: %w", err)
	}
	defer tx.Rollback()
	var (
		userID              int64
		storedClientID      string
		storedRedirectURI   string
		storedCodeChallenge string
		expiresAt           int64
		usedAt              sql.NullInt64
	)
	err = tx.QueryRow(`
SELECT user_id, client_id, redirect_uri, code_challenge, expires_at, used_at
FROM oauth_codes
WHERE code_hash = ?
`, hashCredential(rawCode)).Scan(
		&userID, &storedClientID, &storedRedirectURI, &storedCodeChallenge, &expiresAt, &usedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrAuthorizationNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("read authorization code: %w", err)
	}
	if !now.UTC().Before(time.Unix(expiresAt, 0).UTC()) {
		return 0, ErrAuthorizationExpired
	}
	if usedAt.Valid {
		return 0, ErrAuthorizationUsed
	}
	if storedClientID != clientID {
		return 0, ErrAuthorizationClient
	}
	if storedRedirectURI != redirectURI {
		return 0, ErrAuthorizationRedirect
	}
	if !verifyPKCE(codeVerifier, storedCodeChallenge) {
		return 0, ErrAuthorizationPKCE
	}
	result, err := tx.Exec(`
UPDATE oauth_codes SET used_at = ?
WHERE code_hash = ? AND used_at IS NULL
`, now.UTC().Unix(), hashCredential(rawCode))
	if err != nil {
		return 0, fmt.Errorf("consume authorization code: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count authorization code consumption: %w", err)
	}
	if affected == 0 {
		return 0, ErrAuthorizationUsed
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit authorization code redemption: %w", err)
	}
	return userID, nil
}

// verifyPKCE compares the S256 challenge of a code verifier with the stored
// challenge using a constant-time comparison.
func verifyPKCE(codeVerifier, codeChallenge string) bool {
	if codeVerifier == "" || codeChallenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(codeVerifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(challenge), []byte(codeChallenge)) == 1
}

// BindDevice proves possession of a device secret and assigns the device to a user.
func (s *Store) BindDevice(deviceID, secret string, userID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin device binding: %w", err)
	}
	defer tx.Rollback()
	var stored string
	err = tx.QueryRow(`SELECT secret FROM devices WHERE device_id = ?`, deviceID).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDeviceNotFound
	}
	if err != nil {
		return fmt.Errorf("read device for binding: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(secret)) != 1 {
		return ErrDeviceSecretInvalid
	}
	if _, err := tx.Exec(`
UPDATE devices
SET user_id = ?, bound_at = ?
WHERE device_id = ?
`, userID, time.Now().UTC().Unix(), deviceID); err != nil {
		return fmt.Errorf("bind device: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit device binding: %w", err)
	}
	return nil
}

// DeviceBinding reports whether a device is bound and, when present, its owner.
func (s *Store) DeviceBinding(deviceID string) (userID int64, bound bool, boundAt time.Time, err error) {
	var (
		storedUserID sql.NullInt64
		storedAt     sql.NullInt64
	)
	err = s.db.QueryRow(`
SELECT user_id, bound_at
FROM devices
WHERE device_id = ?
`, deviceID).Scan(&storedUserID, &storedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, time.Time{}, ErrDeviceNotFound
	}
	if err != nil {
		return 0, false, time.Time{}, fmt.Errorf("read device binding: %w", err)
	}
	if !storedUserID.Valid {
		return 0, false, time.Time{}, nil
	}
	if storedAt.Valid {
		boundAt = time.Unix(storedAt.Int64, 0).UTC()
	}
	return storedUserID.Int64, true, boundAt, nil
}

// IsDeviceBound reports whether upload should be allowed for a device.
func (s *Store) IsDeviceBound(deviceID string) (bool, error) {
	_, bound, _, err := s.DeviceBinding(deviceID)
	if errors.Is(err, ErrDeviceNotFound) {
		return false, nil
	}
	return bound, err
}

func randomHex(byteLength int) (string, error) {
	raw := make([]byte, byteLength)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func hashCredential(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// InsertMessages atomically inserts one batch and counts client-message duplicates.
func (s *Store) InsertMessages(deviceID string, messages []MessageInput) (inserted, duplicates int, err error) {
	if len(messages) > maxMessageBatch {
		return 0, 0, fmt.Errorf("message batch exceeds %d", maxMessageBatch)
	}
	for _, message := range messages {
		if message.ClientMsgID == "" {
			return 0, 0, errors.New("client_msg_id is required")
		}
		if message.App == "" {
			return 0, 0, errors.New("app is required")
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("begin message transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
INSERT INTO messages(
    client_msg_id, device_id, app, app_name, chat, sender, content, ts, day
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(client_msg_id) DO NOTHING
`)
	if err != nil {
		return 0, 0, fmt.Errorf("prepare message insert: %w", err)
	}
	defer stmt.Close()

	for _, message := range messages {
		day := time.Unix(message.TS, 0).In(s.loc).Format("2006-01-02")
		result, execErr := stmt.Exec(
			message.ClientMsgID,
			deviceID,
			message.App,
			message.AppName,
			message.Chat,
			message.Sender,
			message.Content,
			message.TS,
			day,
		)
		if execErr != nil {
			return 0, 0, fmt.Errorf("insert message: %w", execErr)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return 0, 0, fmt.Errorf("count message insert: %w", rowsErr)
		}
		if rows == 1 {
			inserted++
		} else {
			duplicates++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit messages: %w", err)
	}
	return inserted, duplicates, nil
}

type messageCursor struct {
	TS int64
	ID int64
}

type messageQuery struct {
	Day      string
	DeviceID string
	App      string
	UserID   int64
	Cursor   *messageCursor
	Limit    int
}

// QueryMessages applies filters and returns a deterministic page.
func (s *Store) QueryMessages(query messageQuery) ([]MessageRecord, string, error) {
	if query.Limit < 1 {
		return nil, "", errors.New("limit must be at least 1")
	}
	if query.Limit > maxMessageBatch {
		query.Limit = maxMessageBatch
	}

	where := make([]string, 0, 5)
	args := make([]any, 0, 6)
	selectSQL := `SELECT id, client_msg_id, app, app_name, chat, sender, content, ts, day`
	fromSQL := " FROM messages"
	columnPrefix := ""
	if query.UserID > 0 {
		selectSQL = `SELECT m.id, m.client_msg_id, m.app, m.app_name, m.chat, m.sender, m.content, m.ts, m.day`
		fromSQL = " FROM messages m JOIN devices d ON d.device_id = m.device_id"
		columnPrefix = "m."
		where = append(where, "d.user_id = ?")
		args = append(args, query.UserID)
	}
	if query.Day != "" {
		where = append(where, columnPrefix+"day = ?")
		args = append(args, query.Day)
	}
	if query.DeviceID != "" {
		where = append(where, columnPrefix+"device_id = ?")
		args = append(args, query.DeviceID)
	}
	if query.App != "" {
		where = append(where, columnPrefix+"app = ?")
		args = append(args, query.App)
	}

	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + joinConditions(where)
	}

	// With no day filter, the contract asks for the most recent window. It is
	// returned in the same ascending order as day-scoped queries.
	if query.Day == "" {
		if query.Cursor != nil {
			where = append(where, columnPrefix+"id < ?")
			args = append(args, query.Cursor.ID)
			whereSQL = " WHERE " + joinConditions(where)
		}
		sqlQuery := selectSQL + fromSQL + whereSQL + " ORDER BY " + columnPrefix + "ts DESC, " + columnPrefix + "id DESC LIMIT ?"
		rows, err := s.db.Query(sqlQuery, append(args, query.Limit)...)
		if err != nil {
			return nil, "", fmt.Errorf("query recent messages: %w", err)
		}
		defer rows.Close()
		result, err := scanMessages(rows)
		if err != nil {
			return nil, "", err
		}
		reverseMessages(result)
		// The omitted-day view is a bounded recent window rather than a full
		// historical traversal, so it has no continuation cursor.
		return result, "", nil
	}

	if query.Cursor != nil {
		where = append(where, "("+columnPrefix+"ts > ? OR ("+columnPrefix+"ts = ? AND "+columnPrefix+"id > ?))")
		args = append(args, query.Cursor.TS, query.Cursor.TS, query.Cursor.ID)
		whereSQL = " WHERE " + joinConditions(where)
	}
	sqlQuery := selectSQL + fromSQL + whereSQL + " ORDER BY " + columnPrefix + "ts ASC, " + columnPrefix + "id ASC LIMIT ?"
	rows, err := s.db.Query(sqlQuery, append(args, query.Limit+1)...)
	if err != nil {
		return nil, "", fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()
	result, err := scanMessages(rows)
	if err != nil {
		return nil, "", err
	}
	var nextCursor string
	if len(result) > query.Limit {
		result = result[:query.Limit]
		last := result[len(result)-1]
		nextCursor = fmt.Sprintf("%d:%d", last.TS, last.ID)
	}
	return result, nextCursor, nil
}

// QueryApps returns one aggregate per app for the requested filters.
func (s *Store) QueryApps(day, deviceID string) ([]AppSummary, error) {
	return s.queryApps(day, deviceID, 0)
}

// QueryAppsForUser limits aggregates to devices bound to one account.
func (s *Store) QueryAppsForUser(userID int64, day, deviceID string) ([]AppSummary, error) {
	return s.queryApps(day, deviceID, userID)
}

func (s *Store) queryApps(day, deviceID string, userID int64) ([]AppSummary, error) {
	where := make([]string, 0, 3)
	args := make([]any, 0, 3)
	fromSQL := " FROM messages"
	columnPrefix := ""
	if userID > 0 {
		fromSQL = " FROM messages m JOIN devices d ON d.device_id = m.device_id"
		columnPrefix = "m."
		where = append(where, "d.user_id = ?")
		args = append(args, userID)
	}
	if day != "" {
		where = append(where, columnPrefix+"day = ?")
		args = append(args, day)
	}
	if deviceID != "" {
		where = append(where, columnPrefix+"device_id = ?")
		args = append(args, deviceID)
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + joinConditions(where)
	}
	rows, err := s.db.Query(`
SELECT `+columnPrefix+`app, MAX(`+columnPrefix+`app_name), COUNT(*), MAX(`+columnPrefix+`ts)`+fromSQL+whereSQL+`
GROUP BY `+columnPrefix+`app
ORDER BY `+columnPrefix+`app ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query apps: %w", err)
	}
	defer rows.Close()
	result := make([]AppSummary, 0)
	for rows.Next() {
		var summary AppSummary
		if err := rows.Scan(&summary.App, &summary.AppName, &summary.Count, &summary.LastTS); err != nil {
			return nil, fmt.Errorf("scan app summary: %w", err)
		}
		result = append(result, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate app summaries: %w", err)
	}
	return result, nil
}

func joinConditions(conditions []string) string {
	result := conditions[0]
	for _, condition := range conditions[1:] {
		result += " AND " + condition
	}
	return result
}

func scanMessages(rows *sql.Rows) ([]MessageRecord, error) {
	result := make([]MessageRecord, 0)
	for rows.Next() {
		var message MessageRecord
		if err := rows.Scan(
			&message.ID,
			&message.ClientMsgID,
			&message.App,
			&message.AppName,
			&message.Chat,
			&message.Sender,
			&message.Content,
			&message.TS,
			&message.Day,
		); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		result = append(result, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}
	return result, nil
}

func reverseMessages(messages []MessageRecord) {
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
}

const historyPageSize = 50

// historyQuery selects one OFFSET page of newest-first messages for an account.
type historyQuery struct {
	UserID   int64
	Day      string
	App      string
	DeviceID string
	Search   string
	Page     int
	PageSize int
}

// QueryHistory returns one page of history plus the total match count. A
// non-empty Search is matched as a single FTS5 phrase across content, sender,
// and chat; an empty search returns everything.
func (s *Store) QueryHistory(query historyQuery) ([]MessageRecord, int64, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 || query.PageSize > historyPageSize {
		query.PageSize = historyPageSize
	}
	where := []string{"d.user_id = ?"}
	args := []any{query.UserID}
	if query.Day != "" {
		where = append(where, "m.day = ?")
		args = append(args, query.Day)
	}
	if query.App != "" {
		where = append(where, "m.app = ?")
		args = append(args, query.App)
	}
	if query.DeviceID != "" {
		where = append(where, "m.device_id = ?")
		args = append(args, query.DeviceID)
	}
	if query.Search != "" {
		where = append(where, "m.id IN (SELECT rowid FROM messages_fts WHERE messages_fts MATCH ?)")
		args = append(args, ftsPhrase(query.Search))
	}
	baseSQL := " FROM messages m JOIN devices d ON d.device_id = m.device_id WHERE " + joinConditions(where)
	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*)`+baseSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count history: %w", err)
	}
	sqlQuery := `SELECT m.id, m.client_msg_id, m.app, m.app_name, m.chat, m.sender, m.content, m.ts, m.day` +
		baseSQL + ` ORDER BY m.ts DESC, m.id DESC LIMIT ? OFFSET ?`
	rows, err := s.db.Query(sqlQuery, append(args, query.PageSize, (query.Page-1)*query.PageSize)...)
	if err != nil {
		return nil, 0, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()
	result, err := scanMessages(rows)
	if err != nil {
		return nil, 0, err
	}
	return result, total, nil
}

// ftsPhrase wraps a raw search term as one FTS5 phrase, doubling embedded
// double quotes so operators and punctuation match literally.
func ftsPhrase(search string) string {
	return `"` + strings.ReplaceAll(search, `"`, `""`) + `"`
}

// TrendPoint is one day of the seven-day overview trend with a precomputed
// bar height percentage for server-side rendering.
type TrendPoint struct {
	Day   string
	Count int64
	Pct   int
}

// AppShareRow is one app's share of the overview message volume.
type AppShareRow struct {
	App   string
	Count int64
	Pct   int
}

// OverviewStats holds the account-level dashboard aggregates.
type OverviewStats struct {
	Today    int64
	Week     int64
	Agents   int64
	Devices  int64
	Uplink   bool
	Trend    []TrendPoint
	AppShare []AppShareRow
}

// Overview aggregates today's and this week's messages, authorized agents,
// bound-device count, uplink health, a seven-day trend, and app shares.
func (s *Store) Overview(userID int64) (OverviewStats, error) {
	now := time.Now().In(s.loc)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc)
	weekStart := dayStart.AddDate(0, 0, -6)
	todayDay := dayStart.Format("2006-01-02")
	weekStartDay := weekStart.Format("2006-01-02")
	stats := OverviewStats{}
	base := " FROM messages m JOIN devices d ON d.device_id = m.device_id WHERE d.user_id = ?"
	if err := s.db.QueryRow(`SELECT COUNT(*)`+base+` AND m.day = ?`, userID, todayDay).Scan(&stats.Today); err != nil {
		return stats, fmt.Errorf("count today messages: %w", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*)`+base+` AND m.day BETWEEN ? AND ?`, userID, weekStartDay, todayDay).Scan(&stats.Week); err != nil {
		return stats, fmt.Errorf("count week messages: %w", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(DISTINCT client_id) FROM oauth_codes WHERE user_id = ?`, userID).Scan(&stats.Agents); err != nil {
		return stats, fmt.Errorf("count authorized agents: %w", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM devices WHERE user_id = ? AND bound_at IS NOT NULL`, userID).Scan(&stats.Devices); err != nil {
		return stats, fmt.Errorf("count bound devices: %w", err)
	}
	var uplink int64
	if err := s.db.QueryRow(`SELECT COUNT(*)`+base+` AND m.ts >= ?`, userID, now.Add(-24*time.Hour).Unix()).Scan(&uplink); err != nil {
		return stats, fmt.Errorf("count uplink messages: %w", err)
	}
	stats.Uplink = uplink > 0

	rows, err := s.db.Query(`SELECT m.day, COUNT(*)`+base+` AND m.day BETWEEN ? AND ? GROUP BY m.day ORDER BY m.day`, userID, weekStartDay, todayDay)
	if err != nil {
		return stats, fmt.Errorf("query seven-day trend: %w", err)
	}
	defer rows.Close()
	counts := make(map[string]int64, 7)
	peak := int64(1)
	for rows.Next() {
		var day string
		var count int64
		if err := rows.Scan(&day, &count); err != nil {
			return stats, fmt.Errorf("scan trend day: %w", err)
		}
		counts[day] = count
		if count > peak {
			peak = count
		}
	}
	if err := rows.Err(); err != nil {
		return stats, fmt.Errorf("iterate trend days: %w", err)
	}
	for offset := 0; offset < 7; offset++ {
		day := dayStart.AddDate(0, 0, offset-6).Format("2006-01-02")
		count := counts[day]
		height := int(count * 100 / peak)
		if height < 3 {
			height = 3
		}
		stats.Trend = append(stats.Trend, TrendPoint{Day: day[5:], Count: count, Pct: height})
	}

	appRows, err := s.db.Query(`SELECT m.app, COUNT(*)`+base+` GROUP BY m.app ORDER BY COUNT(*) DESC, m.app ASC`, userID)
	if err != nil {
		return stats, fmt.Errorf("query app share: %w", err)
	}
	defer appRows.Close()
	type appCount struct {
		app   string
		count int64
	}
	shares := make([]appCount, 0, 8)
	var total int64
	for appRows.Next() {
		var share appCount
		if err := appRows.Scan(&share.app, &share.count); err != nil {
			return stats, fmt.Errorf("scan app share: %w", err)
		}
		shares = append(shares, share)
		total += share.count
	}
	if err := appRows.Err(); err != nil {
		return stats, fmt.Errorf("iterate app share: %w", err)
	}
	if total > 0 {
		for _, share := range shares {
			stats.AppShare = append(stats.AppShare, AppShareRow{
				App:   share.app,
				Count: share.count,
				Pct:   int(share.count * 100 / total),
			})
		}
	}
	return stats, nil
}

// BoundDevice is one device bound to an account.
type BoundDevice struct {
	DeviceID string
	Name     string
	BoundAt  time.Time
}

// BoundDevicesForUser lists the devices bound to one account, newest first.
func (s *Store) BoundDevicesForUser(userID int64) ([]BoundDevice, error) {
	rows, err := s.db.Query(`
SELECT device_id, name, bound_at
FROM devices
WHERE user_id = ? AND bound_at IS NOT NULL
ORDER BY bound_at DESC
`, userID)
	if err != nil {
		return nil, fmt.Errorf("query bound devices: %w", err)
	}
	defer rows.Close()
	result := make([]BoundDevice, 0)
	for rows.Next() {
		var device BoundDevice
		var boundAt int64
		if err := rows.Scan(&device.DeviceID, &device.Name, &boundAt); err != nil {
			return nil, fmt.Errorf("scan bound device: %w", err)
		}
		device.BoundAt = time.Unix(boundAt, 0).UTC()
		result = append(result, device)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bound devices: %w", err)
	}
	return result, nil
}

// DeviceIDsForUser lists the distinct devices that have messages for an account.
func (s *Store) DeviceIDsForUser(userID int64) ([]string, error) {
	rows, err := s.db.Query(`
SELECT DISTINCT m.device_id
FROM messages m
JOIN devices d ON d.device_id = m.device_id
WHERE d.user_id = ?
ORDER BY m.device_id
`, userID)
	if err != nil {
		return nil, fmt.Errorf("query history device ids: %w", err)
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var deviceID string
		if err := rows.Scan(&deviceID); err != nil {
			return nil, fmt.Errorf("scan history device id: %w", err)
		}
		result = append(result, deviceID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate history device ids: %w", err)
	}
	return result, nil
}
