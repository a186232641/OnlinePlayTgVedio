package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type IndexJob struct {
	UserID         int64
	Status         string
	ChannelsTotal  int
	ChannelsDone   int
	VideosFound    int
	LastError      string
	StartedAt      *time.Time
	FinishedAt     *time.Time
}

func (d *DB) StartIndexJob(ctx context.Context, userID int64) error {
	_, err := d.Exec(ctx, `
        INSERT INTO index_jobs (user_id, status, channels_total, channels_done, videos_found, started_at, finished_at, last_error)
        VALUES ($1, 'running', 0, 0, 0, NOW(), NULL, NULL)
        ON CONFLICT (user_id) DO UPDATE SET
            status='running', channels_total=0, channels_done=0, videos_found=0,
            started_at=NOW(), finished_at=NULL, last_error=NULL
    `, userID)
	return err
}

func (d *DB) UpdateIndexProgress(ctx context.Context, userID int64, channelsTotal, channelsDone, videosFound int) error {
	_, err := d.Exec(ctx, `
        UPDATE index_jobs SET channels_total=$2, channels_done=$3, videos_found=$4 WHERE user_id=$1
    `, userID, channelsTotal, channelsDone, videosFound)
	return err
}

func (d *DB) FinishIndexJob(ctx context.Context, userID int64, errMsg string) error {
	status := "done"
	if errMsg != "" {
		status = "failed"
	}
	_, err := d.Exec(ctx, `
        UPDATE index_jobs SET status=$2, finished_at=NOW(), last_error=NULLIF($3,'') WHERE user_id=$1
    `, userID, status, errMsg)
	return err
}

func (d *DB) GetIndexJob(ctx context.Context, userID int64) (*IndexJob, error) {
	row := d.QueryRow(ctx, `
        SELECT user_id, status, channels_total, channels_done, videos_found,
               COALESCE(last_error,''), started_at, finished_at
        FROM index_jobs WHERE user_id=$1
    `, userID)
	j := &IndexJob{}
	if err := row.Scan(&j.UserID, &j.Status, &j.ChannelsTotal, &j.ChannelsDone, &j.VideosFound,
		&j.LastError, &j.StartedAt, &j.FinishedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return j, nil
}
