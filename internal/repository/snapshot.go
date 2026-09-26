package repository

import (
	"context"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"

	"github.com/titagaki/peercast-0yp/internal/channel"
)

// SnapshotRow is a row from channel_snapshots with change detection applied.
type SnapshotRow struct {
	RecordedAt  time.Time
	Listeners   int
	Relays      int
	Hidden      bool
	Changed     bool
	Name        string
	Genre       string
	Description string
	URL         string
	Comment     string
	TrackTitle  string
	TrackArtist string
}

// SnapshotRepo wraps channel_snapshots queries.
type SnapshotRepo struct {
	db *sql.DB
}

// NewSnapshotRepo creates a SnapshotRepo backed by db.
func NewSnapshotRepo(db *sql.DB) *SnapshotRepo {
	return &SnapshotRepo{db: db}
}

// Insert writes a snapshot row for the given session.
func (r *SnapshotRepo) Insert(ctx context.Context, sessionID int64, s channel.ChannelState, t time.Time) error {
	hidden := strings.Contains(s.Info.Genre, "?")
	var age uint32
	for _, h := range s.Hits {
		if h.Tracker {
			age = h.UpTime
			break
		}
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO channel_snapshots
			(session_id, channel_id, recorded_at,
			 listeners, relays, age,
			 name, bitrate, genre, url, description, comment, content_type,
			 hidden_listeners,
			 track_title, track_artist, track_contact, track_album)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT (recorded_at, name) DO UPDATE SET
			session_id       = EXCLUDED.session_id,
			channel_id       = EXCLUDED.channel_id,
			listeners        = EXCLUDED.listeners,
			relays           = EXCLUDED.relays,
			age              = EXCLUDED.age,
			bitrate          = EXCLUDED.bitrate,
			genre            = EXCLUDED.genre,
			url              = EXCLUDED.url,
			description      = EXCLUDED.description,
			comment          = EXCLUDED.comment,
			content_type     = EXCLUDED.content_type,
			hidden_listeners = EXCLUDED.hidden_listeners,
			track_title      = EXCLUDED.track_title,
			track_artist     = EXCLUDED.track_artist,
			track_contact    = EXCLUDED.track_contact,
			track_album      = EXCLUDED.track_album`,
		sessionID,
		hex.EncodeToString(s.Info.ID[:]),
		t,
		s.Listeners,
		s.Relays,
		age,
		s.Info.Name,
		s.Info.Bitrate,
		stripYPPrefix(s.Info.Genre),
		s.Info.URL,
		s.Info.Desc,
		s.Info.Comment,
		s.Info.ContentType,
		hidden,
		s.Info.Track.Title,
		s.Info.Track.Artist,
		s.Info.Track.Contact,
		s.Info.Track.Album,
	)
	return err
}

// ListByNameAndDate returns snapshot rows for the given channel name and date range,
// with Changed set to true when any metadata field differs from the previous snapshot.
func (r *SnapshotRepo) ListByNameAndDate(ctx context.Context, name string, dayStart, dayEnd time.Time) ([]SnapshotRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			ch.recorded_at, ch.listeners, ch.relays, ch.hidden_listeners,
			ch.name, ch.genre, ch.description, ch.url, ch.comment, ch.track_title, ch.track_artist,
			LAG(ch.name)         OVER w AS prev_name,
			LAG(ch.genre)        OVER w AS prev_genre,
			LAG(ch.description)  OVER w AS prev_description,
			LAG(ch.url)          OVER w AS prev_url,
			LAG(ch.comment)      OVER w AS prev_comment,
			LAG(ch.track_title)  OVER w AS prev_track_title,
			LAG(ch.track_artist) OVER w AS prev_track_artist
		FROM channel_snapshots ch
		JOIN channel_sessions cs ON ch.session_id = cs.id
		WHERE cs.channel_name = $1 AND ch.recorded_at >= $2 AND ch.recorded_at < $3
		WINDOW w AS (PARTITION BY ch.session_id ORDER BY ch.recorded_at)
		ORDER BY ch.recorded_at`,
		name, dayStart, dayEnd,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SnapshotRow
	for rows.Next() {
		var recordedAt time.Time
		var listeners, relays int
		var hidden bool
		var name, genre, desc, url, comment, trackTitle, trackArtist string
		var prevName, prevGenre, prevDesc, prevURL, prevComment, prevTrackTitle, prevTrackArtist sql.NullString

		if err := rows.Scan(
			&recordedAt, &listeners, &relays, &hidden,
			&name, &genre, &desc, &url, &comment, &trackTitle, &trackArtist,
			&prevName, &prevGenre, &prevDesc, &prevURL, &prevComment, &prevTrackTitle, &prevTrackArtist,
		); err != nil {
			return nil, err
		}

		changed := !prevName.Valid ||
			name != prevName.String ||
			genre != prevGenre.String ||
			desc != prevDesc.String ||
			comment != prevComment.String ||
			trackTitle != prevTrackTitle.String ||
			trackArtist != prevTrackArtist.String

		row := SnapshotRow{
			RecordedAt: recordedAt,
			Listeners:  listeners,
			Relays:     relays,
			Hidden:     hidden,
			Changed:    changed,
		}
		if changed {
			row.Name = name
			row.Genre = genre
			row.Description = desc
			row.URL = url
			row.Comment = comment
			row.TrackTitle = trackTitle
			row.TrackArtist = trackArtist
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// PageSnapshotRow is a SnapshotRow augmented with the session start time,
// used to compute elapsed broadcast duration for the getgmt page.
type PageSnapshotRow struct {
	SessionStartedAt time.Time
	SnapshotRow
}

// ListByNameAndDateForPage is like ListByNameAndDate but also includes
// the session started_at, needed to compute broadcast duration.
func (r *SnapshotRepo) ListByNameAndDateForPage(ctx context.Context, name string, dayStart, dayEnd time.Time) ([]PageSnapshotRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			cs.started_at,
			ch.recorded_at, ch.listeners, ch.relays,
			ch.name, ch.genre, ch.description, ch.url, ch.comment, ch.track_title, ch.track_artist,
			LAG(ch.name)         OVER w AS prev_name,
			LAG(ch.genre)        OVER w AS prev_genre,
			LAG(ch.description)  OVER w AS prev_description,
			LAG(ch.url)          OVER w AS prev_url,
			LAG(ch.comment)      OVER w AS prev_comment,
			LAG(ch.track_title)  OVER w AS prev_track_title,
			LAG(ch.track_artist) OVER w AS prev_track_artist
		FROM channel_snapshots ch
		JOIN channel_sessions cs ON ch.session_id = cs.id
		WHERE cs.channel_name = $1 AND ch.recorded_at >= $2 AND ch.recorded_at < $3
		WINDOW w AS (PARTITION BY ch.session_id ORDER BY ch.recorded_at)
		ORDER BY ch.recorded_at`,
		name, dayStart, dayEnd,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PageSnapshotRow
	for rows.Next() {
		var sessionStartedAt time.Time
		var recordedAt time.Time
		var listeners, relays int
		var name, genre, desc, url, comment, trackTitle, trackArtist string
		var prevName, prevGenre, prevDesc, prevURL, prevComment, prevTrackTitle, prevTrackArtist sql.NullString

		if err := rows.Scan(
			&sessionStartedAt,
			&recordedAt, &listeners, &relays,
			&name, &genre, &desc, &url, &comment, &trackTitle, &trackArtist,
			&prevName, &prevGenre, &prevDesc, &prevURL, &prevComment, &prevTrackTitle, &prevTrackArtist,
		); err != nil {
			return nil, err
		}

		changed := !prevName.Valid ||
			name != prevName.String ||
			genre != prevGenre.String ||
			desc != prevDesc.String ||
			comment != prevComment.String ||
			trackTitle != prevTrackTitle.String ||
			trackArtist != prevTrackArtist.String

		row := PageSnapshotRow{
			SessionStartedAt: sessionStartedAt,
			SnapshotRow: SnapshotRow{
				RecordedAt: recordedAt,
				Listeners:  listeners,
				Relays:     relays,
				Changed:    changed,
			},
		}
		if changed {
			row.SnapshotRow.Name = name
			row.SnapshotRow.Genre = genre
			row.SnapshotRow.Description = desc
			row.SnapshotRow.URL = url
			row.SnapshotRow.Comment = comment
			row.SnapshotRow.TrackTitle = trackTitle
			row.SnapshotRow.TrackArtist = trackArtist
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
