package tgsession

import (
	"context"
	"errors"

	"github.com/gotd/td/session"

	"github.com/hanfeilong/onlineplaytgvideo/internal/db"
)

// Storage is a per-user gotd session.Storage backed by the tg_sessions table.
// Session blobs are encrypted at rest with AES-256-GCM using the master key.
type Storage struct {
	DB        *db.DB
	UserID    int64
	MasterKey []byte
}

var _ session.Storage = (*Storage)(nil)

func (s *Storage) LoadSession(ctx context.Context) ([]byte, error) {
	enc, err := s.DB.LoadTGSessionBlob(ctx, s.UserID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, session.ErrNotFound
		}
		return nil, err
	}
	if len(enc) == 0 {
		return nil, session.ErrNotFound
	}
	plain, err := DecryptAEAD(s.MasterKey, enc)
	if err != nil {
		return nil, err
	}
	return plain, nil
}

func (s *Storage) StoreSession(ctx context.Context, data []byte) error {
	enc, err := EncryptAEAD(s.MasterKey, data)
	if err != nil {
		return err
	}
	return s.DB.StoreTGSessionBlob(ctx, s.UserID, enc)
}
