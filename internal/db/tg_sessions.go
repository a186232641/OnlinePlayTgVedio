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
	UserID     int64
	Phone      string
	TGUserID   int64
	Status     TGSessionStatus
	LastUsedAt *time.Time
}

func (d *DB) GetTGSession(ctx context.Context, userID int64) (*TGSession, error) {
	row := d.QueryRow(ctx, `
        SELECT user_id, COALESCE(phone, ''), COALESCE(tg_user_id, 0), status, last_used_at
        FROM tg_sessions WHERE user_id=$1
    `, userID)
	s := &TGSession{}
	var status string
	if err := row.Scan(&s.UserID, &s.Phone, &s.TGUserID, &status, &s.LastUsedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s.Status = TGSessionStatus(status)
	return s, nil
}

func (d *DB) LoadTGSessionBlob(ctx context.Context, userID int64) ([]byte, error) {
	row := d.QueryRow(ctx, `SELECT encrypted_session FROM tg_sessions WHERE user_id=$1`, userID)
	var blob []byte
	if err := row.Scan(&blob); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return blob, nil
}

func (d *DB) StoreTGSessionBlob(ctx context.Context, userID int64, blob []byte) error {
	_, err := d.Exec(ctx, `
        INSERT INTO tg_sessions (user_id, encrypted_session, updated_at)
        VALUES ($1, $2, NOW())
        ON CONFLICT (user_id) DO UPDATE SET
            encrypted_session = EXCLUDED.encrypted_session,
            updated_at = NOW()
    `, userID, blob)
	return err
}

func (d *DB) MarkTGSessionActive(ctx context.Context, userID int64, phone string, tgUserID int64) error {
	_, err := d.Exec(ctx, `
        INSERT INTO tg_sessions (user_id, phone, tg_user_id, status, last_used_at, updated_at)
        VALUES ($1, $2, $3, 'active', NOW(), NOW())
        ON CONFLICT (user_id) DO UPDATE SET
            phone = EXCLUDED.phone,
            tg_user_id = EXCLUDED.tg_user_id,
            status = 'active',
            last_used_at = NOW(),
            updated_at = NOW()
    `, userID, phone, tgUserID)
	return err
}

func (d *DB) ListActiveTGSessionUsers(ctx context.Context) ([]int64, error) {
	rows, err := d.Query(ctx, `SELECT user_id FROM tg_sessions WHERE status='active' AND encrypted_session IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (d *DB) RevokeTGSession(ctx context.Context, userID int64) error {
	_, err := d.Exec(ctx, `UPDATE tg_sessions SET status='revoked', encrypted_session=NULL, updated_at=NOW() WHERE user_id=$1`, userID)
	return err
}
