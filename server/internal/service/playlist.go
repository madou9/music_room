package service

import (
	"context"
	"errors"

	"music-room/internal/model"
	"music-room/internal/repository"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrPlaylistNotFound       = errors.New("playlist not found")
	ErrPlaylistTrackNotFound  = errors.New("track not found")
	ErrEditNotAllowed         = errors.New("edit not allowed")
	ErrDuplicateTrack         = errors.New("track already in playlist")
	ErrInvalidPosition        = errors.New("position out of range")
	ErrConcurrentModification = errors.New("the playlist is being edited concurrently, please retry")
)

type PlaylistService interface {
	Create(ctx context.Context, ownerID uuid.UUID, req model.CreatePlaylistRequest) (*model.Playlist, error)
	List(ctx context.Context, callerID uuid.UUID, f model.PlaylistListFilter) ([]model.Playlist, error)
	Get(ctx context.Context, playlistID, callerID uuid.UUID) (*model.PlaylistWithTracks, error)
	Update(ctx context.Context, playlistID, callerID uuid.UUID, req model.UpdatePlaylistRequest) (*model.Playlist, error)
	Delete(ctx context.Context, playlistID, callerID uuid.UUID) error
	Invite(ctx context.Context, playlistID, callerID, targetUserID uuid.UUID) error
	AddTrack(ctx context.Context, playlistID, callerID uuid.UUID, req model.AddTrackRequest) (*model.PlaylistTrack, error)
	RemoveTrack(ctx context.Context, playlistID, callerID, trackID uuid.UUID) error
	MoveTrack(ctx context.Context, playlistID, callerID, trackID uuid.UUID, newPos int) error
}

type playlistService struct {
	repo repository.PlaylistRepository
}

func NewPlaylistService(repo repository.PlaylistRepository) PlaylistService {
	return &playlistService{repo: repo}
}

func (s *playlistService) Create(ctx context.Context, ownerID uuid.UUID, req model.CreatePlaylistRequest) (*model.Playlist, error) {
	return s.repo.Create(ctx, ownerID, req)
}

func (s *playlistService) List(ctx context.Context, callerID uuid.UUID, f model.PlaylistListFilter) ([]model.Playlist, error) {
	return s.repo.List(ctx, callerID, f)
}

func (s *playlistService) Get(ctx context.Context, playlistID, callerID uuid.UUID) (*model.PlaylistWithTracks, error) {
	p, err := s.repo.GetAccessible(ctx, playlistID, callerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPlaylistNotFound
		}
		return nil, err
	}

	tracks, err := s.repo.ListTracks(ctx, playlistID)
	if err != nil {
		return nil, err
	}
	if tracks == nil {
		tracks = []model.PlaylistTrack{}
	}

	// canEdit mirrors requireEditAccess: owner and license-0 playlists are always
	// editable; a license-1 playlist is editable only by the owner or an invited
	// user. The client uses this to render read-only mode on load.
	canEdit := p.OwnerID == callerID || p.License != 1
	if !canEdit {
		invited, err := s.repo.IsInvited(ctx, playlistID, callerID)
		if err != nil {
			return nil, err
		}
		canEdit = invited
	}

	return &model.PlaylistWithTracks{Playlist: *p, Tracks: tracks, CanEdit: canEdit}, nil
}

// Update, Delete and Invite go through GetByIDForOwner first: a non-owner (or a
// missing playlist) yields pgx.ErrNoRows, which maps to ErrPlaylistNotFound so
// the endpoint returns 404 without leaking whether the playlist exists.
func (s *playlistService) Update(ctx context.Context, playlistID, callerID uuid.UUID, req model.UpdatePlaylistRequest) (*model.Playlist, error) {
	if _, err := s.requireOwner(ctx, playlistID, callerID); err != nil {
		return nil, err
	}
	return s.repo.Update(ctx, playlistID, req)
}

func (s *playlistService) Delete(ctx context.Context, playlistID, callerID uuid.UUID) error {
	if _, err := s.requireOwner(ctx, playlistID, callerID); err != nil {
		return err
	}
	return s.repo.Delete(ctx, playlistID)
}

func (s *playlistService) Invite(ctx context.Context, playlistID, callerID, targetUserID uuid.UUID) error {
	if _, err := s.requireOwner(ctx, playlistID, callerID); err != nil {
		return err
	}
	if err := s.repo.AddInvite(ctx, playlistID, targetUserID); err != nil {
		if isPgFKViolation(err) {
			return ErrUserNotFound
		}
		if isPgUniqueViolation(err) {
			return ErrAlreadyInvited
		}
		return err
	}
	return nil
}

func (s *playlistService) requireOwner(ctx context.Context, playlistID, callerID uuid.UUID) (*model.Playlist, error) {
	p, err := s.repo.GetByIDForOwner(ctx, playlistID, callerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPlaylistNotFound
		}
		return nil, err
	}
	return p, nil
}

// requireEditAccess confirms the playlist is accessible to the caller and that
// the caller is allowed to mutate its tracks. For license 1 playlists, only the
// owner and invited users may add, remove, or reorder tracks.
func (s *playlistService) requireEditAccess(ctx context.Context, playlistID, callerID uuid.UUID) (*model.Playlist, error) {
	p, err := s.repo.GetAccessible(ctx, playlistID, callerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPlaylistNotFound
		}
		return nil, err
	}
	if p.License == 1 && p.OwnerID != callerID {
		invited, err := s.repo.IsInvited(ctx, playlistID, callerID)
		if err != nil {
			return nil, err
		}
		if !invited {
			return nil, ErrEditNotAllowed
		}
	}
	return p, nil
}

func (s *playlistService) AddTrack(ctx context.Context, playlistID, callerID uuid.UUID, req model.AddTrackRequest) (*model.PlaylistTrack, error) {
	if _, err := s.requireEditAccess(ctx, playlistID, callerID); err != nil {
		return nil, err
	}
	track, err := s.repo.AddTrack(ctx, playlistID, callerID, req)
	if err != nil {
		if isPgUniqueViolation(err) {
			return nil, ErrDuplicateTrack
		}
		return nil, err
	}
	return track, nil
}

func (s *playlistService) RemoveTrack(ctx context.Context, playlistID, callerID, trackID uuid.UUID) error {
	if _, err := s.requireEditAccess(ctx, playlistID, callerID); err != nil {
		return err
	}
	if err := s.repo.RemoveTrack(ctx, playlistID, trackID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPlaylistTrackNotFound
		}
		return err
	}
	return nil
}

func (s *playlistService) MoveTrack(ctx context.Context, playlistID, callerID, trackID uuid.UUID, newPos int) error {
	if _, err := s.requireEditAccess(ctx, playlistID, callerID); err != nil {
		return err
	}
	if err := s.repo.MoveTrack(ctx, playlistID, trackID, newPos); err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrPlaylistTrackNotFound
		case errors.Is(err, repository.ErrPositionOutOfRange):
			return ErrInvalidPosition
		case errors.Is(err, repository.ErrSerializationFailed):
			return ErrConcurrentModification
		}
		return err
	}
	return nil
}
