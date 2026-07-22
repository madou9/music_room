package repository

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"music-room/internal/model"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PlaylistRepository interface {
	Create(ctx context.Context, ownerID uuid.UUID, req model.CreatePlaylistRequest) (*model.Playlist, error)
	List(ctx context.Context, callerID uuid.UUID, f model.PlaylistListFilter) ([]model.Playlist, error)
	GetAccessible(ctx context.Context, playlistID, callerID uuid.UUID) (*model.Playlist, error)
	GetByIDForOwner(ctx context.Context, playlistID, ownerID uuid.UUID) (*model.Playlist, error)
	Update(ctx context.Context, playlistID uuid.UUID, req model.UpdatePlaylistRequest) (*model.Playlist, error)
	Delete(ctx context.Context, playlistID uuid.UUID) error
	AddInvite(ctx context.Context, playlistID, userID uuid.UUID) error
	ListTracks(ctx context.Context, playlistID uuid.UUID) ([]model.PlaylistTrack, error)
	IsInvited(ctx context.Context, playlistID, userID uuid.UUID) (bool, error)
	AddTrack(ctx context.Context, playlistID, callerID uuid.UUID, req model.AddTrackRequest) (*model.PlaylistTrack, error)
	RemoveTrack(ctx context.Context, playlistID, trackID uuid.UUID) error
	MoveTrack(ctx context.Context, playlistID, trackID uuid.UUID, newPos int) error
}

type playlistRepository struct {
	pool *pgxpool.Pool
}

func NewPlaylistRepository(pool *pgxpool.Pool) PlaylistRepository {
	return &playlistRepository{pool: pool}
}

const playlistCols = `id, owner_id, name, visibility, license, created_at`

func scanPlaylist(row interface {
	Scan(...any) error
}) (*model.Playlist, error) {
	var p model.Playlist
	err := row.Scan(&p.ID, &p.OwnerID, &p.Name, &p.Visibility, &p.License, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *playlistRepository) Create(ctx context.Context, ownerID uuid.UUID, req model.CreatePlaylistRequest) (*model.Playlist, error) {
	query := fmt.Sprintf(`
		INSERT INTO playlists (owner_id, name, visibility, license)
		VALUES ($1, $2, $3, $4)
		RETURNING %s`, playlistCols)

	return scanPlaylist(r.pool.QueryRow(ctx, query, ownerID, req.Name, req.Visibility, req.License))
}

func (r *playlistRepository) List(ctx context.Context, callerID uuid.UUID, f model.PlaylistListFilter) ([]model.Playlist, error) {
	// Base visibility filter: public OR owned by caller OR caller is invited.
	baseWhere := `
		(p.visibility = 'public' OR p.owner_id = $1
		 OR EXISTS (SELECT 1 FROM playlist_invites pi WHERE pi.playlist_id = p.id AND pi.user_id = $1))`

	args := []any{callerID}
	paramIdx := 2

	if f.Q != "" {
		baseWhere += fmt.Sprintf(` AND p.name ILIKE $%d`, paramIdx)
		args = append(args, "%"+f.Q+"%")
		paramIdx++
	}

	query := fmt.Sprintf(`SELECT %s FROM playlists p WHERE %s ORDER BY p.created_at DESC`, playlistCols, baseWhere)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var playlists []model.Playlist
	for rows.Next() {
		p, err := scanPlaylist(rows)
		if err != nil {
			return nil, err
		}
		playlists = append(playlists, *p)
	}
	return playlists, rows.Err()
}

// GetAccessible returns the playlist only if the caller can see it
// (public, owner, or invited). Returns pgx.ErrNoRows if not accessible.
func (r *playlistRepository) GetAccessible(ctx context.Context, playlistID, callerID uuid.UUID) (*model.Playlist, error) {
	query := fmt.Sprintf(`
		SELECT %s FROM playlists p
		WHERE p.id = $1
		AND (p.visibility = 'public' OR p.owner_id = $2
		     OR EXISTS (SELECT 1 FROM playlist_invites pi WHERE pi.playlist_id = p.id AND pi.user_id = $2))`,
		playlistCols)

	return scanPlaylist(r.pool.QueryRow(ctx, query, playlistID, callerID))
}

// GetByIDForOwner returns the playlist only if callerID is the owner.
// Used for write operations (update, delete, invite).
func (r *playlistRepository) GetByIDForOwner(ctx context.Context, playlistID, ownerID uuid.UUID) (*model.Playlist, error) {
	query := fmt.Sprintf(`SELECT %s FROM playlists p WHERE p.id = $1 AND p.owner_id = $2`, playlistCols)
	return scanPlaylist(r.pool.QueryRow(ctx, query, playlistID, ownerID))
}

func (r *playlistRepository) Update(ctx context.Context, playlistID uuid.UUID, req model.UpdatePlaylistRequest) (*model.Playlist, error) {
	query := fmt.Sprintf(`
		UPDATE playlists SET
			name       = COALESCE($2, name),
			visibility = COALESCE($3, visibility),
			license    = COALESCE($4, license)
		WHERE id = $1
		RETURNING %s`, playlistCols)

	return scanPlaylist(r.pool.QueryRow(ctx, query, playlistID, req.Name, req.Visibility, req.License))
}

func (r *playlistRepository) Delete(ctx context.Context, playlistID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM playlists WHERE id = $1`, playlistID)
	return err
}

func (r *playlistRepository) AddInvite(ctx context.Context, playlistID, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO playlist_invites (playlist_id, user_id) VALUES ($1, $2)`,
		playlistID, userID,
	)
	return err
}

func (r *playlistRepository) ListTracks(ctx context.Context, playlistID uuid.UUID) ([]model.PlaylistTrack, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, playlist_id, external_id, title, artist, position, added_by, created_at
		FROM playlist_tracks
		WHERE playlist_id = $1
		ORDER BY position ASC`, playlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []model.PlaylistTrack
	for rows.Next() {
		var t model.PlaylistTrack
		err := rows.Scan(
			&t.ID, &t.PlaylistID, &t.ExternalID, &t.Title,
			&t.Artist, &t.Position, &t.AddedBy, &t.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

func (r *playlistRepository) IsInvited(ctx context.Context, playlistID, userID uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM playlist_invites WHERE playlist_id = $1 AND user_id = $2)`,
		playlistID, userID,
	).Scan(&exists)
	return exists, err
}

func scanPlaylistTrack(row interface{ Scan(...any) error }) (*model.PlaylistTrack, error) {
	var t model.PlaylistTrack
	err := row.Scan(&t.ID, &t.PlaylistID, &t.ExternalID, &t.Title, &t.Artist, &t.Position, &t.AddedBy, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *playlistRepository) AddTrack(ctx context.Context, playlistID, callerID uuid.UUID, req model.AddTrackRequest) (*model.PlaylistTrack, error) {
	return scanPlaylistTrack(r.pool.QueryRow(ctx, `
		INSERT INTO playlist_tracks (playlist_id, external_id, title, artist, position, added_by)
		VALUES ($1, $2, $3, $4,
			COALESCE((SELECT MAX(position) FROM playlist_tracks WHERE playlist_id = $1), 0) + 1,
			$5)
		RETURNING id, playlist_id, external_id, title, artist, position, added_by, created_at`,
		playlistID, req.ExternalID, req.Title, req.Artist, callerID,
	))
}

// RemoveTrack deletes the track and recompacts positions so there are no gaps.
func (r *playlistRepository) RemoveTrack(ctx context.Context, playlistID, trackID uuid.UUID) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx,
		`DELETE FROM playlist_tracks WHERE id = $1 AND playlist_id = $2`,
		trackID, playlistID,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}

	// Recompact remaining positions to 1, 2, 3... with no gaps.
	_, err = tx.Exec(ctx, `
		UPDATE playlist_tracks pt
		SET position = sub.rn
		FROM (
			SELECT id, ROW_NUMBER() OVER (ORDER BY position) AS rn
			FROM playlist_tracks
			WHERE playlist_id = $1
		) sub
		WHERE pt.id = sub.id`, playlistID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// maxMoveRetries bounds how many times MoveTrack re-runs its transaction after
// a serialization failure before giving up. moveRetryBaseBackoff is the base of
// the exponential, jittered backoff applied between attempts so that conflicting
// transactions do not immediately re-collide.
const (
	maxMoveRetries       = 5
	moveRetryBaseBackoff = 2 * time.Millisecond
)

// MoveTrack shifts affected rows and places the track at newPos. The work runs
// in a serializable transaction (see moveTrackTx), which Postgres may abort with
// a serialization failure (SQLSTATE 40001) when two moves on the same playlist
// interleave. Those aborts are expected and safe to retry, so we re-run the
// transaction up to maxMoveRetries times instead of surfacing an error.
func (r *playlistRepository) MoveTrack(ctx context.Context, playlistID, trackID uuid.UUID, newPos int) error {
	return retryOnSerialization(maxMoveRetries, time.Sleep, func() error {
		return r.moveTrackTx(ctx, playlistID, trackID, newPos)
	})
}

// retryOnSerialization runs fn, retrying only on a Postgres serialization failure
// (SQLSTATE 40001) up to maxAttempts times. Any other error (including a nil
// result) is returned immediately. Between attempts it waits via sleep using an
// exponential, jittered backoff so conflicting transactions spread out instead
// of re-colliding. When all attempts fail it wraps the last error in
// ErrSerializationFailed. sleep is injected so tests can run without delay.
func retryOnSerialization(maxAttempts int, sleep func(time.Duration), fn func() error) error {
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = fn()
		if err == nil || !isSerializationFailure(err) {
			return err
		}
		if attempt < maxAttempts-1 {
			sleep(serializationBackoff(attempt))
		}
	}
	return fmt.Errorf("%w after %d attempts: %w", ErrSerializationFailed, maxAttempts, err)
}

// serializationBackoff returns the wait before the next retry: an exponentially
// growing base (2ms, 4ms, 8ms, ...) plus a random jitter in [0, base) so that
// transactions that conflicted do not wake up and retry at the same instant.
func serializationBackoff(attempt int) time.Duration {
	base := moveRetryBaseBackoff << attempt
	return base + time.Duration(rand.Int63n(int64(base)))
}

// isSerializationFailure reports whether err is a Postgres serialization failure
// (SQLSTATE 40001), which a serializable transaction may return under contention.
func isSerializationFailure(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40001"
}

// moveTrackTx performs a single attempt: it shifts affected rows and places the
// track at newPos inside a serializable transaction so two concurrent moves on
// the same playlist cannot interleave and corrupt the position order.
func (r *playlistRepository) moveTrackTx(ctx context.Context, playlistID, trackID uuid.UUID, newPos int) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Validate newPos against the actual track count.
	var count int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM playlist_tracks WHERE playlist_id = $1`, playlistID,
	).Scan(&count); err != nil {
		return err
	}
	if newPos > count {
		return ErrPositionOutOfRange
	}

	var currentPos int
	if err := tx.QueryRow(ctx,
		`SELECT position FROM playlist_tracks WHERE id = $1 AND playlist_id = $2`,
		trackID, playlistID,
	).Scan(&currentPos); err != nil {
		return err
	}

	if currentPos == newPos {
		return tx.Commit(ctx)
	}

	if newPos > currentPos {
		_, err = tx.Exec(ctx, `
			UPDATE playlist_tracks
			SET position = position - 1
			WHERE playlist_id = $1 AND position > $2 AND position <= $3`,
			playlistID, currentPos, newPos)
	} else {
		_, err = tx.Exec(ctx, `
			UPDATE playlist_tracks
			SET position = position + 1
			WHERE playlist_id = $1 AND position >= $2 AND position < $3`,
			playlistID, newPos, currentPos)
	}
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx,
		`UPDATE playlist_tracks SET position = $1 WHERE id = $2 AND playlist_id = $3`,
		newPos, trackID, playlistID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ErrPositionOutOfRange is returned by MoveTrack when newPos exceeds the track count.
var ErrPositionOutOfRange = fmt.Errorf("position out of range")

// ErrSerializationFailed is returned by MoveTrack when the transaction still hits
// a serialization failure after exhausting maxMoveRetries retries.
var ErrSerializationFailed = errors.New("serialization failed after retries")
