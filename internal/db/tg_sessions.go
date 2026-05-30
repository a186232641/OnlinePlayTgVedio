package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type TGSessionStatus string

const (
	TGStatusPending TGSessionStatus = "pending"
	TGStatusActive  TGSessionStatus = "active"
	TGStatusRevoked TGSessionStatus = "revoked"
)

type TGSession struct {
	ID                 int64
	UserID             int64
	Phone              string
	TGUserID           int64
	Label              string
	Status             TGSessionStatus
	DiscoverStatus     string
	DiscoverStartedAt  *time.Time
	DiscoverFinishedAt *time.Time
	DiscoverError      string
	LastUsedAt         *time.Time
}

const tgSessionCols = `
    id, user_id, COALESCE(phone, ''), COALESCE(tg_user_id, 0),
    COALESCE(label, ''), status,
    COALESCE(discover_status, 'idle'), discover_started_at, discover_finished_at,
    COALESCE(discover_error, ''), last_used_at
`

func scanTGSession(row pgx.Row) (*TGSession, error) {
	s := &TGSession{}
	var status string
	if err := row.Scan(
		&s.ID, &s.UserID, &s.Phone, &s.TGUserID,
		&s.Label, &status,
		&s.DiscoverStatus, &s.DiscoverStartedAt, &s.DiscoverFinishedAt,
		&s.DiscoverError, &s.LastUsedAt,
	); err != nil {
		return nil, err
	}
	s.Status = TGSessionStatus(status)
	return s, nil
}

// CreateTGSession inserts a fresh row for a new bind attempt. Returns the
// generated session id, which the caller (tglogin) hands to the gotd session
// storage so blob writes land on this row.
func (d *DB) CreateTGSession(ctx context.Context, userID int64, phone string) (int64, error) {
	row := d.QueryRow(ctx, `
        INSERT INTO tg_sessions (user_id, phone, status, updated_at)
        VALUES ($1, $2, 'pending', NOW())
        RETURNING id
    `, userID, phone)
	var id int64
	if err := row.Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (d *DB) GetTGSessionByID(ctx context.Context, sessionID, userID int64) (*TGSession, error) {
	row := d.QueryRow(ctx, `
        SELECT `+tgSessionCols+`
        FROM tg_sessions WHERE id=$1 AND user_id=$2
    `, sessionID, userID)
	s, err := scanTGSession(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s, nil
}

func (d *DB) ListTGSessions(ctx context.Context, userID int64) ([]TGSession, error) {
	rows, err := d.Query(ctx, `
        SELECT `+tgSessionCols+`
        FROM tg_sessions
        WHERE user_id=$1 AND status <> 'revoked'
        ORDER BY id
    `, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TGSession
	for rows.Next() {
		s, err := scanTGSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (d *DB) LoadTGSessionBlob(ctx context.Context, sessionID int64) ([]byte, error) {
	row := d.QueryRow(ctx, `SELECT encrypted_session FROM tg_sessions WHERE id=$1`, sessionID)
	var blob []byte
	if err := row.Scan(&blob); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return blob, nil
}

func (d *DB) StoreTGSessionBlob(ctx context.Context, sessionID int64, blob []byte) error {
	_, err := d.Exec(ctx, `
        UPDATE tg_sessions
        SET encrypted_session=$1, updated_at=NOW()
        WHERE id=$2
    `, blob, sessionID)
	return err
}

func (d *DB) MarkTGSessionActive(ctx context.Context, sessionID int64, phone string, tgUserID int64) error {
	_, err := d.Exec(ctx, `
        UPDATE tg_sessions
        SET phone=$2, tg_user_id=$3, status='active', last_used_at=NOW(), updated_at=NOW()
        WHERE id=$1
    `, sessionID, phone, tgUserID)
	return err
}

// ActivateAndConsolidateTGSession marks the freshly-authed session active, then
// folds it into any pre-existing session for the same Telegram account (same
// user_id + tg_user_id). Re-logging into an account thus reuses the ORIGINAL
// session row — keeping its channels/videos (which are keyed off tg_session_id)
// attached — instead of stranding them under a throwaway new row. The freshly
// obtained session blob is moved onto the canonical row, the throwaway row is
// deleted, and the canonical id is returned as the one to bring online. When no
// prior session exists for the account it behaves like MarkTGSessionActive and
// returns newSessionID unchanged.
func (d *DB) ActivateAndConsolidateTGSession(ctx context.Context, newSessionID, userID, tgUserID int64, phone string) (int64, error) {
	tx, err := d.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Oldest existing row for this Telegram account (any status, so a
	// previously revoked bind gets resurrected and its channels restored),
	// excluding the throwaway row we just created.
	var canonicalID int64
	err = tx.QueryRow(ctx, `
        SELECT id FROM tg_sessions
        WHERE user_id=$1 AND tg_user_id=$2 AND tg_user_id<>0 AND id<>$3
        ORDER BY id
        LIMIT 1
    `, userID, tgUserID, newSessionID).Scan(&canonicalID)
	if errors.Is(err, pgx.ErrNoRows) {
		// First bind of this account: just activate the new row in place.
		if _, err := tx.Exec(ctx, `
            UPDATE tg_sessions
            SET phone=$2, tg_user_id=$3, status='active', last_used_at=NOW(), updated_at=NOW()
            WHERE id=$1
        `, newSessionID, phone, tgUserID); err != nil {
			return 0, err
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
		return newSessionID, nil
	}
	if err != nil {
		return 0, err
	}

	// Move the freshly authed blob onto the canonical row and reactivate it.
	if _, err := tx.Exec(ctx, `
        UPDATE tg_sessions canon
        SET encrypted_session = src.encrypted_session,
            phone=$3, tg_user_id=$4, status='active',
            last_used_at=NOW(), updated_at=NOW()
        FROM tg_sessions src
        WHERE canon.id=$1 AND src.id=$2
    `, canonicalID, newSessionID, phone, tgUserID); err != nil {
		return 0, err
	}
	// Drop the throwaway row (it has no channels of its own yet).
	if _, err := tx.Exec(ctx, `DELETE FROM tg_sessions WHERE id=$1`, newSessionID); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return canonicalID, nil
}

func (d *DB) UpdateTGSessionLabel(ctx context.Context, sessionID, userID int64, label string) error {
	_, err := d.Exec(ctx, `
        UPDATE tg_sessions
        SET label=NULLIF($3,''), updated_at=NOW()
        WHERE id=$1 AND user_id=$2
    `, sessionID, userID, label)
	return err
}

// ActiveTGSessionRef is the minimum the manager needs to start a client.
type ActiveTGSessionRef struct {
	ID     int64
	UserID int64
}

func (d *DB) ListActiveTGSessions(ctx context.Context) ([]ActiveTGSessionRef, error) {
	rows, err := d.Query(ctx, `
        SELECT id, user_id FROM tg_sessions
        WHERE status='active' AND encrypted_session IS NOT NULL
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActiveTGSessionRef
	for rows.Next() {
		var r ActiveTGSessionRef
		if err := rows.Scan(&r.ID, &r.UserID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RevokeTGSession is a soft-delete: the row stays so favorites/videos
// pointing through this session keep resolving, but it disappears from
// session listings and we wipe the encrypted blob so the gotd client
// can't reconnect.
func (d *DB) RevokeTGSession(ctx context.Context, sessionID, userID int64) error {
	_, err := d.Exec(ctx, `
        UPDATE tg_sessions
        SET status='revoked', encrypted_session=NULL, updated_at=NOW()
        WHERE id=$1 AND user_id=$2
    `, sessionID, userID)
	return err
}

func (d *DB) StartDiscover(ctx context.Context, sessionID int64) error {
	_, err := d.Exec(ctx, `
        UPDATE tg_sessions
        SET discover_status='running', discover_started_at=NOW(),
            discover_finished_at=NULL, discover_error=NULL
        WHERE id=$1
    `, sessionID)
	return err
}

func (d *DB) FinishDiscover(ctx context.Context, sessionID int64, errMsg string) error {
	status := "done"
	if errMsg != "" {
		status = "failed"
	}
	_, err := d.Exec(ctx, `
        UPDATE tg_sessions
        SET discover_status=$2, discover_finished_at=NOW(), discover_error=NULLIF($3,'')
        WHERE id=$1
    `, sessionID, status, errMsg)
	return err
}