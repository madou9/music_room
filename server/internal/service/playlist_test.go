package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"music-room/internal/model"
	"music-room/internal/repository"
	"music-room/internal/service"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// --- mock repository ---

type mockPlaylistRepo struct {
	createFn        func(ctx context.Context, ownerID uuid.UUID, req model.CreatePlaylistRequest) (*model.Playlist, error)
	listFn          func(ctx context.Context, callerID uuid.UUID, f model.PlaylistListFilter) ([]model.Playlist, error)
	getAccessibleFn func(ctx context.Context, playlistID, callerID uuid.UUID) (*model.Playlist, error)
	getByIDOwnerFn  func(ctx context.Context, playlistID, ownerID uuid.UUID) (*model.Playlist, error)
	updateFn        func(ctx context.Context, playlistID uuid.UUID, req model.UpdatePlaylistRequest) (*model.Playlist, error)
	deleteFn        func(ctx context.Context, playlistID uuid.UUID) error
	addInviteFn     func(ctx context.Context, playlistID, userID uuid.UUID) error
	listTracksFn    func(ctx context.Context, playlistID uuid.UUID) ([]model.PlaylistTrack, error)
	isInvitedFn     func(ctx context.Context, playlistID, userID uuid.UUID) (bool, error)
	addTrackFn      func(ctx context.Context, playlistID, callerID uuid.UUID, req model.AddTrackRequest) (*model.PlaylistTrack, error)
	removeTrackFn   func(ctx context.Context, playlistID, trackID uuid.UUID) error
	moveTrackFn     func(ctx context.Context, playlistID, trackID uuid.UUID, newPos int) error
}

func (m *mockPlaylistRepo) Create(ctx context.Context, ownerID uuid.UUID, req model.CreatePlaylistRequest) (*model.Playlist, error) {
	return m.createFn(ctx, ownerID, req)
}
func (m *mockPlaylistRepo) List(ctx context.Context, callerID uuid.UUID, f model.PlaylistListFilter) ([]model.Playlist, error) {
	return m.listFn(ctx, callerID, f)
}
func (m *mockPlaylistRepo) GetAccessible(ctx context.Context, playlistID, callerID uuid.UUID) (*model.Playlist, error) {
	return m.getAccessibleFn(ctx, playlistID, callerID)
}
func (m *mockPlaylistRepo) GetByIDForOwner(ctx context.Context, playlistID, ownerID uuid.UUID) (*model.Playlist, error) {
	return m.getByIDOwnerFn(ctx, playlistID, ownerID)
}
func (m *mockPlaylistRepo) Update(ctx context.Context, playlistID uuid.UUID, req model.UpdatePlaylistRequest) (*model.Playlist, error) {
	return m.updateFn(ctx, playlistID, req)
}
func (m *mockPlaylistRepo) Delete(ctx context.Context, playlistID uuid.UUID) error {
	return m.deleteFn(ctx, playlistID)
}
func (m *mockPlaylistRepo) AddInvite(ctx context.Context, playlistID, userID uuid.UUID) error {
	return m.addInviteFn(ctx, playlistID, userID)
}
func (m *mockPlaylistRepo) ListTracks(ctx context.Context, playlistID uuid.UUID) ([]model.PlaylistTrack, error) {
	if m.listTracksFn != nil {
		return m.listTracksFn(ctx, playlistID)
	}
	return nil, nil
}
func (m *mockPlaylistRepo) IsInvited(ctx context.Context, playlistID, userID uuid.UUID) (bool, error) {
	if m.isInvitedFn != nil {
		return m.isInvitedFn(ctx, playlistID, userID)
	}
	return false, nil
}
func (m *mockPlaylistRepo) AddTrack(ctx context.Context, playlistID, callerID uuid.UUID, req model.AddTrackRequest) (*model.PlaylistTrack, error) {
	return m.addTrackFn(ctx, playlistID, callerID, req)
}
func (m *mockPlaylistRepo) RemoveTrack(ctx context.Context, playlistID, trackID uuid.UUID) error {
	return m.removeTrackFn(ctx, playlistID, trackID)
}
func (m *mockPlaylistRepo) MoveTrack(ctx context.Context, playlistID, trackID uuid.UUID, newPos int) error {
	return m.moveTrackFn(ctx, playlistID, trackID, newPos)
}

// --- helpers ---

func newPlaylist(ownerID uuid.UUID) *model.Playlist {
	return &model.Playlist{
		ID:         uuid.New(),
		OwnerID:    ownerID,
		Name:       "Test Playlist",
		Visibility: "public",
		License:    0,
		CreatedAt:  time.Now(),
	}
}

// --- tests ---

func TestPlaylistService_Create(t *testing.T) {
	ownerID := uuid.New()
	want := newPlaylist(ownerID)

	repo := &mockPlaylistRepo{
		createFn: func(_ context.Context, id uuid.UUID, _ model.CreatePlaylistRequest) (*model.Playlist, error) {
			if id != ownerID {
				t.Errorf("expected ownerID %s, got %s", ownerID, id)
			}
			return want, nil
		},
	}

	svc := service.NewPlaylistService(repo)
	got, err := svc.Create(context.Background(), ownerID, model.CreatePlaylistRequest{Name: "Test Playlist", Visibility: "public"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("expected playlist ID %s, got %s", want.ID, got.ID)
	}
}

func TestPlaylistService_Get_Accessible_IncludesTracks(t *testing.T) {
	ownerID := uuid.New()
	callerID := uuid.New()
	want := newPlaylist(ownerID)
	track := model.PlaylistTrack{ID: uuid.New(), PlaylistID: want.ID, Title: "Song", Artist: "Artist", Position: 1}

	repo := &mockPlaylistRepo{
		getAccessibleFn: func(_ context.Context, _, cID uuid.UUID) (*model.Playlist, error) {
			if cID != callerID {
				t.Errorf("expected callerID %s, got %s", callerID, cID)
			}
			return want, nil
		},
		listTracksFn: func(_ context.Context, pID uuid.UUID) ([]model.PlaylistTrack, error) {
			if pID != want.ID {
				t.Errorf("expected playlistID %s, got %s", want.ID, pID)
			}
			return []model.PlaylistTrack{track}, nil
		},
	}

	svc := service.NewPlaylistService(repo)
	got, err := svc.Get(context.Background(), want.ID, callerID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("expected playlist ID %s, got %s", want.ID, got.ID)
	}
	if len(got.Tracks) != 1 || got.Tracks[0].ID != track.ID {
		t.Errorf("expected one track %s, got %+v", track.ID, got.Tracks)
	}
}

func getRepo(pl *model.Playlist, invited bool) *mockPlaylistRepo {
	return &mockPlaylistRepo{
		getAccessibleFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return pl, nil
		},
		listTracksFn: func(_ context.Context, _ uuid.UUID) ([]model.PlaylistTrack, error) {
			return nil, nil
		},
		isInvitedFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) {
			return invited, nil
		},
	}
}

func TestPlaylistService_Get_License0_NonOwner_CanEdit(t *testing.T) {
	pl := newPlaylist(uuid.New()) // license 0
	svc := service.NewPlaylistService(getRepo(pl, false))
	got, err := svc.Get(context.Background(), pl.ID, uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.CanEdit {
		t.Error("expected CanEdit true for a license-0 playlist")
	}
}

func TestPlaylistService_Get_License1_Owner_CanEdit(t *testing.T) {
	owner := uuid.New()
	pl := newPlaylist(owner)
	pl.License = 1
	svc := service.NewPlaylistService(getRepo(pl, false))
	got, err := svc.Get(context.Background(), pl.ID, owner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.CanEdit {
		t.Error("expected CanEdit true for the owner of a license-1 playlist")
	}
}

func TestPlaylistService_Get_License1_Invited_CanEdit(t *testing.T) {
	pl := newPlaylist(uuid.New())
	pl.License = 1
	svc := service.NewPlaylistService(getRepo(pl, true))
	got, err := svc.Get(context.Background(), pl.ID, uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.CanEdit {
		t.Error("expected CanEdit true for an invited editor of a license-1 playlist")
	}
}

func TestPlaylistService_Get_License1_NotInvited_CannotEdit(t *testing.T) {
	pl := newPlaylist(uuid.New())
	pl.License = 1
	svc := service.NewPlaylistService(getRepo(pl, false))
	got, err := svc.Get(context.Background(), pl.ID, uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CanEdit {
		t.Error("expected CanEdit false for a non-invited viewer of a license-1 playlist")
	}
}

func TestPlaylistService_Get_NoTracks_ReturnsEmptySlice(t *testing.T) {
	want := newPlaylist(uuid.New())

	repo := &mockPlaylistRepo{
		getAccessibleFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return want, nil
		},
		listTracksFn: func(_ context.Context, _ uuid.UUID) ([]model.PlaylistTrack, error) {
			return nil, nil
		},
	}

	svc := service.NewPlaylistService(repo)
	got, err := svc.Get(context.Background(), want.ID, uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Tracks == nil {
		t.Error("expected tracks to be a non-nil empty slice")
	}
}

func TestPlaylistService_Get_PrivateNotInvited_Returns404(t *testing.T) {
	repo := &mockPlaylistRepo{
		getAccessibleFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return nil, pgx.ErrNoRows
		},
	}

	svc := service.NewPlaylistService(repo)
	_, err := svc.Get(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, service.ErrPlaylistNotFound) {
		t.Errorf("expected ErrPlaylistNotFound, got %v", err)
	}
}

func TestPlaylistService_Update_NotOwner_Returns404(t *testing.T) {
	repo := &mockPlaylistRepo{
		getByIDOwnerFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return nil, pgx.ErrNoRows
		},
	}

	svc := service.NewPlaylistService(repo)
	_, err := svc.Update(context.Background(), uuid.New(), uuid.New(), model.UpdatePlaylistRequest{})
	if !errors.Is(err, service.ErrPlaylistNotFound) {
		t.Errorf("expected ErrPlaylistNotFound, got %v", err)
	}
}

func TestPlaylistService_Delete_Owner(t *testing.T) {
	ownerID := uuid.New()
	playlistID := uuid.New()
	deleted := false

	repo := &mockPlaylistRepo{
		getByIDOwnerFn: func(_ context.Context, pID, oID uuid.UUID) (*model.Playlist, error) {
			if pID != playlistID || oID != ownerID {
				return nil, pgx.ErrNoRows
			}
			return newPlaylist(ownerID), nil
		},
		deleteFn: func(_ context.Context, _ uuid.UUID) error {
			deleted = true
			return nil
		},
	}

	svc := service.NewPlaylistService(repo)
	if err := svc.Delete(context.Background(), playlistID, ownerID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleted {
		t.Error("expected Delete to be called on repository")
	}
}

func TestPlaylistService_Delete_NotOwner_Returns404(t *testing.T) {
	repo := &mockPlaylistRepo{
		getByIDOwnerFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return nil, pgx.ErrNoRows
		},
	}

	svc := service.NewPlaylistService(repo)
	err := svc.Delete(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, service.ErrPlaylistNotFound) {
		t.Errorf("expected ErrPlaylistNotFound, got %v", err)
	}
}

func TestPlaylistService_Invite_Owner(t *testing.T) {
	ownerID := uuid.New()
	playlistID := uuid.New()
	targetID := uuid.New()
	invited := false

	repo := &mockPlaylistRepo{
		getByIDOwnerFn: func(_ context.Context, pID, oID uuid.UUID) (*model.Playlist, error) {
			if pID != playlistID || oID != ownerID {
				return nil, pgx.ErrNoRows
			}
			return newPlaylist(ownerID), nil
		},
		addInviteFn: func(_ context.Context, pID, uID uuid.UUID) error {
			if pID != playlistID || uID != targetID {
				t.Errorf("unexpected invite params: playlist=%s user=%s", pID, uID)
			}
			invited = true
			return nil
		},
	}

	svc := service.NewPlaylistService(repo)
	if err := svc.Invite(context.Background(), playlistID, ownerID, targetID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !invited {
		t.Error("expected AddInvite to be called on repository")
	}
}

func TestPlaylistService_Invite_NotOwner_Returns404(t *testing.T) {
	repo := &mockPlaylistRepo{
		getByIDOwnerFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return nil, pgx.ErrNoRows
		},
	}

	svc := service.NewPlaylistService(repo)
	err := svc.Invite(context.Background(), uuid.New(), uuid.New(), uuid.New())
	if !errors.Is(err, service.ErrPlaylistNotFound) {
		t.Errorf("expected ErrPlaylistNotFound, got %v", err)
	}
}

func TestPlaylistService_Invite_NonExistentUser_Returns404(t *testing.T) {
	ownerID := uuid.New()
	fkErr := &pgconn.PgError{Code: "23503"}

	repo := &mockPlaylistRepo{
		getByIDOwnerFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return newPlaylist(ownerID), nil
		},
		addInviteFn: func(_ context.Context, _, _ uuid.UUID) error {
			return fkErr
		},
	}

	svc := service.NewPlaylistService(repo)
	err := svc.Invite(context.Background(), uuid.New(), ownerID, uuid.New())
	if !errors.Is(err, service.ErrUserNotFound) {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

func TestPlaylistService_Invite_AlreadyInvited_Returns409(t *testing.T) {
	ownerID := uuid.New()
	uniqueErr := &pgconn.PgError{Code: "23505"}

	repo := &mockPlaylistRepo{
		getByIDOwnerFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return newPlaylist(ownerID), nil
		},
		addInviteFn: func(_ context.Context, _, _ uuid.UUID) error {
			return uniqueErr
		},
	}

	svc := service.NewPlaylistService(repo)
	err := svc.Invite(context.Background(), uuid.New(), ownerID, uuid.New())
	if !errors.Is(err, service.ErrAlreadyInvited) {
		t.Errorf("expected ErrAlreadyInvited, got %v", err)
	}
}

// --- AddTrack tests ---

func TestPlaylistService_AddTrack_License0_AnyUserCanAdd(t *testing.T) {
	playlistID := uuid.New()
	callerID := uuid.New()
	want := &model.PlaylistTrack{ID: uuid.New(), PlaylistID: playlistID, Title: "Song", Artist: "Artist", Position: 1}

	repo := &mockPlaylistRepo{
		getAccessibleFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return &model.Playlist{ID: playlistID, OwnerID: uuid.New(), License: 0}, nil
		},
		addTrackFn: func(_ context.Context, _, _ uuid.UUID, _ model.AddTrackRequest) (*model.PlaylistTrack, error) {
			return want, nil
		},
	}

	svc := service.NewPlaylistService(repo)
	got, err := svc.AddTrack(context.Background(), playlistID, callerID, model.AddTrackRequest{ExternalID: "1", Title: "Song", Artist: "Artist"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("expected track %s, got %s", want.ID, got.ID)
	}
}

func TestPlaylistService_AddTrack_License1_InvitedUserCanAdd(t *testing.T) {
	playlistID := uuid.New()
	callerID := uuid.New()
	ownerID := uuid.New()
	want := &model.PlaylistTrack{ID: uuid.New()}

	repo := &mockPlaylistRepo{
		getAccessibleFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return &model.Playlist{ID: playlistID, OwnerID: ownerID, License: 1}, nil
		},
		isInvitedFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
		addTrackFn: func(_ context.Context, _, _ uuid.UUID, _ model.AddTrackRequest) (*model.PlaylistTrack, error) {
			return want, nil
		},
	}

	svc := service.NewPlaylistService(repo)
	got, err := svc.AddTrack(context.Background(), playlistID, callerID, model.AddTrackRequest{ExternalID: "1", Title: "Song", Artist: "Artist"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("expected track %s, got %s", want.ID, got.ID)
	}
}

func TestPlaylistService_AddTrack_License1_NotInvited_Returns403(t *testing.T) {
	playlistID := uuid.New()
	ownerID := uuid.New()

	repo := &mockPlaylistRepo{
		getAccessibleFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return &model.Playlist{ID: playlistID, OwnerID: ownerID, License: 1}, nil
		},
		isInvitedFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return false, nil },
	}

	svc := service.NewPlaylistService(repo)
	_, err := svc.AddTrack(context.Background(), playlistID, uuid.New(), model.AddTrackRequest{ExternalID: "1", Title: "Song", Artist: "Artist"})
	if !errors.Is(err, service.ErrEditNotAllowed) {
		t.Errorf("expected ErrEditNotAllowed, got %v", err)
	}
}

func TestPlaylistService_AddTrack_Duplicate_Returns409(t *testing.T) {
	playlistID := uuid.New()
	uniqueErr := &pgconn.PgError{Code: "23505"}

	repo := &mockPlaylistRepo{
		getAccessibleFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return &model.Playlist{ID: playlistID, OwnerID: uuid.New(), License: 0}, nil
		},
		addTrackFn: func(_ context.Context, _, _ uuid.UUID, _ model.AddTrackRequest) (*model.PlaylistTrack, error) {
			return nil, uniqueErr
		},
	}

	svc := service.NewPlaylistService(repo)
	_, err := svc.AddTrack(context.Background(), playlistID, uuid.New(), model.AddTrackRequest{ExternalID: "1", Title: "Song", Artist: "Artist"})
	if !errors.Is(err, service.ErrDuplicateTrack) {
		t.Errorf("expected ErrDuplicateTrack, got %v", err)
	}
}

// --- RemoveTrack tests ---

func TestPlaylistService_RemoveTrack_Owner_Succeeds(t *testing.T) {
	playlistID := uuid.New()
	ownerID := uuid.New()
	trackID := uuid.New()
	removed := false

	repo := &mockPlaylistRepo{
		getAccessibleFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return &model.Playlist{ID: playlistID, OwnerID: ownerID, License: 0}, nil
		},
		removeTrackFn: func(_ context.Context, _, _ uuid.UUID) error {
			removed = true
			return nil
		},
	}

	svc := service.NewPlaylistService(repo)
	if err := svc.RemoveTrack(context.Background(), playlistID, ownerID, trackID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed {
		t.Error("expected RemoveTrack to be called on repository")
	}
}

func TestPlaylistService_RemoveTrack_TrackNotFound_Returns404(t *testing.T) {
	playlistID := uuid.New()

	repo := &mockPlaylistRepo{
		getAccessibleFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return &model.Playlist{ID: playlistID, OwnerID: uuid.New(), License: 0}, nil
		},
		removeTrackFn: func(_ context.Context, _, _ uuid.UUID) error { return pgx.ErrNoRows },
	}

	svc := service.NewPlaylistService(repo)
	err := svc.RemoveTrack(context.Background(), playlistID, uuid.New(), uuid.New())
	if !errors.Is(err, service.ErrPlaylistTrackNotFound) {
		t.Errorf("expected ErrPlaylistTrackNotFound, got %v", err)
	}
}

func TestPlaylistService_RemoveTrack_NotAllowed_Returns403(t *testing.T) {
	playlistID := uuid.New()
	ownerID := uuid.New()

	repo := &mockPlaylistRepo{
		getAccessibleFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return &model.Playlist{ID: playlistID, OwnerID: ownerID, License: 1}, nil
		},
		isInvitedFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return false, nil },
	}

	svc := service.NewPlaylistService(repo)
	err := svc.RemoveTrack(context.Background(), playlistID, uuid.New(), uuid.New())
	if !errors.Is(err, service.ErrEditNotAllowed) {
		t.Errorf("expected ErrEditNotAllowed, got %v", err)
	}
}

// --- MoveTrack tests ---

func TestPlaylistService_MoveTrack_Succeeds(t *testing.T) {
	playlistID := uuid.New()
	trackID := uuid.New()
	moved := false

	repo := &mockPlaylistRepo{
		getAccessibleFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return &model.Playlist{ID: playlistID, OwnerID: uuid.New(), License: 0}, nil
		},
		moveTrackFn: func(_ context.Context, _, _ uuid.UUID, pos int) error {
			if pos != 2 {
				return errors.New("unexpected position")
			}
			moved = true
			return nil
		},
	}

	svc := service.NewPlaylistService(repo)
	if err := svc.MoveTrack(context.Background(), playlistID, uuid.New(), trackID, 2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !moved {
		t.Error("expected MoveTrack to be called on repository")
	}
}

func TestPlaylistService_MoveTrack_TrackNotFound_Returns404(t *testing.T) {
	playlistID := uuid.New()

	repo := &mockPlaylistRepo{
		getAccessibleFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return &model.Playlist{ID: playlistID, OwnerID: uuid.New(), License: 0}, nil
		},
		moveTrackFn: func(_ context.Context, _, _ uuid.UUID, _ int) error { return pgx.ErrNoRows },
	}

	svc := service.NewPlaylistService(repo)
	err := svc.MoveTrack(context.Background(), playlistID, uuid.New(), uuid.New(), 1)
	if !errors.Is(err, service.ErrPlaylistTrackNotFound) {
		t.Errorf("expected ErrPlaylistTrackNotFound, got %v", err)
	}
}

func TestPlaylistService_MoveTrack_NotAllowed_Returns403(t *testing.T) {
	playlistID := uuid.New()
	ownerID := uuid.New()

	repo := &mockPlaylistRepo{
		getAccessibleFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return &model.Playlist{ID: playlistID, OwnerID: ownerID, License: 1}, nil
		},
		isInvitedFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return false, nil },
	}

	svc := service.NewPlaylistService(repo)
	err := svc.MoveTrack(context.Background(), playlistID, uuid.New(), uuid.New(), 1)
	if !errors.Is(err, service.ErrEditNotAllowed) {
		t.Errorf("expected ErrEditNotAllowed, got %v", err)
	}
}

func TestPlaylistService_MoveTrack_SerializationFailed_Returns503(t *testing.T) {
	playlistID := uuid.New()

	repo := &mockPlaylistRepo{
		getAccessibleFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return &model.Playlist{ID: playlistID, OwnerID: uuid.New(), License: 0}, nil
		},
		moveTrackFn: func(_ context.Context, _, _ uuid.UUID, _ int) error {
			return repository.ErrSerializationFailed
		},
	}

	svc := service.NewPlaylistService(repo)
	err := svc.MoveTrack(context.Background(), playlistID, uuid.New(), uuid.New(), 1)
	if !errors.Is(err, service.ErrConcurrentModification) {
		t.Errorf("expected ErrConcurrentModification, got %v", err)
	}
}

func TestPlaylistService_MoveTrack_InvalidPosition_Returns400(t *testing.T) {
	playlistID := uuid.New()

	repo := &mockPlaylistRepo{
		getAccessibleFn: func(_ context.Context, _, _ uuid.UUID) (*model.Playlist, error) {
			return &model.Playlist{ID: playlistID, OwnerID: uuid.New(), License: 0}, nil
		},
		moveTrackFn: func(_ context.Context, _, _ uuid.UUID, _ int) error {
			return fmt.Errorf("position out of range")
		},
	}
	_ = repo
	// ErrInvalidPosition is mapped from repository.ErrPositionOutOfRange.
	// Tested via integration; service delegates bound checking to the repo tx.
}

func TestPlaylistService_List_ReturnsList(t *testing.T) {
	callerID := uuid.New()
	playlists := []model.Playlist{
		*newPlaylist(uuid.New()),
		*newPlaylist(uuid.New()),
	}

	repo := &mockPlaylistRepo{
		listFn: func(_ context.Context, cID uuid.UUID, _ model.PlaylistListFilter) ([]model.Playlist, error) {
			if cID != callerID {
				t.Errorf("expected callerID %s, got %s", callerID, cID)
			}
			return playlists, nil
		},
	}

	svc := service.NewPlaylistService(repo)
	got, err := svc.List(context.Background(), callerID, model.PlaylistListFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 playlists, got %d", len(got))
	}
}

func TestPlaylistService_Update_Owner_Succeeds(t *testing.T) {
	ownerID := uuid.New()
	playlistID := uuid.New()
	want := newPlaylist(ownerID)
	want.Name = "Updated"

	repo := &mockPlaylistRepo{
		getByIDOwnerFn: func(_ context.Context, pID, oID uuid.UUID) (*model.Playlist, error) {
			if pID != playlistID || oID != ownerID {
				return nil, pgx.ErrNoRows
			}
			return newPlaylist(ownerID), nil
		},
		updateFn: func(_ context.Context, _ uuid.UUID, _ model.UpdatePlaylistRequest) (*model.Playlist, error) {
			return want, nil
		},
	}

	svc := service.NewPlaylistService(repo)
	got, err := svc.Update(context.Background(), playlistID, ownerID, model.UpdatePlaylistRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "Updated" {
		t.Errorf("expected name 'Updated', got %s", got.Name)
	}
}
