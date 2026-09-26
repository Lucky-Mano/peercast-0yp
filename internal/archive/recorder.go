// Package archive records channel session and snapshot data to PostgreSQL.
package archive

import (
	"context"
	"log/slog"
	"time"

	pcp "github.com/titagaki/peercast-pcp/pcp"

	"github.com/titagaki/peercast-0yp/internal/channel"
	"github.com/titagaki/peercast-0yp/internal/repository"
)

// Recorder polls channel.Store and writes sessions/snapshots to PostgreSQL.
type Recorder struct {
	sessions  *repository.SessionRepo
	snapshots *repository.SnapshotRepo
	store     *channel.Store
	log       *slog.Logger

	active   map[pcp.GnuID]sessionRecord // channel_id → open session
	lastSnap time.Time                   // last time snapshots were written
}

type sessionRecord struct {
	id        int64
	startedAt time.Time
	lastState channel.ChannelState
}

// New returns a Recorder. Call Start to begin polling.
func New(sessions *repository.SessionRepo, snapshots *repository.SnapshotRepo, store *channel.Store, log *slog.Logger) *Recorder {
	return &Recorder{
		sessions:  sessions,
		snapshots: snapshots,
		store:     store,
		log:       log,
		active:    make(map[pcp.GnuID]sessionRecord),
	}
}

// Start runs the poll loop until ctx is cancelled.
// It first closes any sessions left open from a previous crash.
func (r *Recorder) Start(ctx context.Context) {
	if err := r.sessions.CloseStaleSessions(ctx); err != nil {
		r.log.Error("archive: closeStaleSessions", "err", err)
	}

	r.lastSnap = time.Now().Truncate(10 * time.Minute)

	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			r.closeAllSessions()
			return
		case <-tick.C:
			r.poll(ctx)
		}
	}
}

// closeAllSessions closes all active sessions on graceful shutdown.
func (r *Recorder) closeAllSessions() {
	ctx := context.Background()
	now := time.Now()
	for id, sess := range r.active {
		if err := r.sessions.Close(ctx, sess.id, sess.lastState, now); err != nil {
			r.log.Error("archive: closeSession (shutdown)", "session_id", sess.id, "err", err)
		}
		delete(r.active, id)
	}
}

func (r *Recorder) poll(ctx context.Context) {
	now := time.Now()
	states := r.store.SnapshotOrdered()

	// Build set of currently active channel IDs.
	currentIDs := make(map[pcp.GnuID]struct{}, len(states))
	for _, s := range states {
		currentIDs[s.Info.ID] = struct{}{}
	}

	// New channels → INSERT channel_sessions.
	// Active channels → update lastState.
	for _, s := range states {
		id := s.Info.ID
		if _, exists := r.active[id]; !exists {
			sessionID, err := r.sessions.Insert(ctx, s, now)
			if err != nil {
				r.log.Error("archive: insertSession", "channel", s.Info.Name, "err", err)
				continue
			}
			r.active[id] = sessionRecord{id: sessionID, startedAt: now, lastState: s}
			// Record an initial snapshot at the start of the current 10-minute window.
			if err := r.snapshots.Insert(ctx, sessionID, s, now.Truncate(10*time.Minute)); err != nil {
				r.log.Error("archive: insertSnapshot (initial)", "channel", s.Info.Name, "err", err)
			}
		} else {
			rec := r.active[id]
			rec.lastState = s
			r.active[id] = rec
		}
	}

	// Gone channels → UPDATE ended_at and final metadata.
	for id, sess := range r.active {
		if _, exists := currentIDs[id]; !exists {
			if err := r.sessions.Close(ctx, sess.id, sess.lastState, now); err != nil {
				r.log.Error("archive: closeSession", "session_id", sess.id, "err", err)
			}
			delete(r.active, id)
		}
	}

	// Every 10 minutes: INSERT snapshots for all active channels.
	if now.Sub(r.lastSnap) >= 10*time.Minute {
		snapTime := now.Truncate(10 * time.Minute)
		for _, s := range states {
			sess, ok := r.active[s.Info.ID]
			if !ok {
				continue
			}
			if err := r.snapshots.Insert(ctx, sess.id, s, snapTime); err != nil {
				r.log.Error("archive: insertSnapshot", "channel", s.Info.Name, "err", err)
			}
		}
		r.lastSnap = now
	}
}
