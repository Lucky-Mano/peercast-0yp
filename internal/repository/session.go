// Package repository wraps all PostgreSQL queries for the PeerCast YP.
package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/titagaki/peercast-0yp/internal/channel"
)

// Session is a row from channel_sessions.
type Session struct {
	ID          int64
	ChannelName string
	ContentType string
	Genre       string
	Description string
	URL         string
	Comment     string
	TrackerIP   string
	StartedAt   time.Time
	EndedAt     *time.Time
	DurationMin int
}

// SessionInterval is a [start, end) pair from channel_sessions.
type SessionInterval struct {
	Start time.Time
	End   time.Time
}

// SessionRepo wraps channel_sessions queries.
type SessionRepo struct {
	db          *sql.DB
	genrePrefix string
}

// NewSessionRepo creates a SessionRepo backed by db.
func NewSessionRepo(db *sql.DB, genrePrefix string) *SessionRepo {
	return &SessionRepo{db: db, genrePrefix: genrePrefix}
}

// CloseStaleSessions closes sessions left open by a previous crash.
func (r *SessionRepo) CloseStaleSessions(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE channel_sessions SET ended_at = NOW() WHERE ended_at IS NULL`)
	return err
}

// trackerIP returns the GlobalAddr IP string of the tracker hit, or "" if none.
func trackerIP(s channel.ChannelState) string {
	for _, h := range s.Hits {
		if h.Tracker {
			return h.GlobalAddr.IP.String()
		}
	}
	return ""
}

// Insert creates a new session row and returns its ID.
func (r *SessionRepo) Insert(ctx context.Context, s channel.ChannelState, now time.Time) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO channel_sessions
			(channel_name, content_type, genre, description, url, comment, tracker_ip, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`,
		s.Info.Name,
		s.Info.ContentType,
		channel.GenreDisplay(r.genrePrefix, s.Info.Genre),
		s.Info.Desc,
		s.Info.URL,
		s.Info.Comment,
		trackerIP(s),
		now,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// Close sets ended_at and updates final metadata on the session row identified by id.
func (r *SessionRepo) Close(ctx context.Context, id int64, s channel.ChannelState, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE channel_sessions
		SET ended_at = $1, genre = $2, description = $3, url = $4, comment = $5
		WHERE id = $6`,
		now,
		channel.GenreDisplay(r.genrePrefix, s.Info.Genre),
		s.Info.Desc,
		s.Info.URL,
		s.Info.Comment,
		id,
	)
	return err
}

// List returns sessions from the past 7 days, up to limit rows starting from offset,
// ordered by started_at DESC.
func (r *SessionRepo) List(ctx context.Context, limit, offset int) ([]Session, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, channel_name, content_type, genre, description, url, comment, tracker_ip,
		       started_at, ended_at,
		       FLOOR(EXTRACT(EPOCH FROM (COALESCE(ended_at, NOW()) - started_at)) / 60)::INTEGER AS duration_min
		FROM channel_sessions
		WHERE started_at >= NOW() - INTERVAL '7 days'
		ORDER BY started_at DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		var endedAt sql.NullTime
		if err := rows.Scan(
			&s.ID, &s.ChannelName,
			&s.ContentType, &s.Genre, &s.Description, &s.URL, &s.Comment, &s.TrackerIP,
			&s.StartedAt, &endedAt, &s.DurationMin,
		); err != nil {
			return nil, err
		}
		if endedAt.Valid {
			t := endedAt.Time
			s.EndedAt = &t
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// ListIntervalsByName returns [start, end) pairs for all sessions of the
// given channel name in the past 365 days, ordered by started_at.
func (r *SessionRepo) ListIntervalsByName(ctx context.Context, name string) ([]SessionInterval, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT started_at, COALESCE(ended_at, NOW())
		FROM channel_sessions
		WHERE channel_name = $1
		  AND started_at >= NOW() - INTERVAL '365 days'
		ORDER BY started_at`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var intervals []SessionInterval
	for rows.Next() {
		var iv SessionInterval
		if err := rows.Scan(&iv.Start, &iv.End); err != nil {
			return nil, err
		}
		intervals = append(intervals, iv)
	}
	return intervals, rows.Err()
}
