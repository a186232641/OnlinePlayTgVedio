package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type User struct {
	ID           int64
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

var ErrNotFound = errors.New("not found")

func (d *DB) CreateUser(ctx context.Context, email, passwordHash string) (*User, error) {
	u := &User{Email: email, PasswordHash: passwordHash}
	row := d.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id, created_at`,
		email, passwordHash,
	)
	if err := row.Scan(&u.ID, &u.CreatedAt); err != nil {
		return nil, err
	}
	return u, nil
}

func (d *DB) UserByEmail(ctx context.Context, email string) (*User, error) {
	u := &User{}
	row := d.QueryRow(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE email=$1`,
		email,
	)
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return u, nil
}

func (d *DB) UserByID(ctx context.Context, id int64) (*User, error) {
	u := &User{}
	row := d.QueryRow(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE id=$1`,
		id,
	)
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return u, nil
}
