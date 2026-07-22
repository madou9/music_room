package service_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"music-room/internal/model"
	"music-room/internal/repository"
	"music-room/internal/service"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// --- mock event repository (only methods used by trackService) ---

type mockEventRepoForTrack struct {
	getAccessibleFn func(ctx context.Context, eventID, callerID uuid.UUID) (*model.Event, error)
	getByIDOwnerFn  func(ctx context.Context, eventID, ownerID uuid.UUID) (*model.Event, error)
	isInvitedFn     func(ctx context.Context, eventID, userID uuid.UUID) (bool, error)
}

func (m *mockEventRepoForTrack) Create(ctx context.Context, ownerID uuid.UUID, req model.CreateEventRequest) (*model.Event, error) {
	panic("not expected")
}
func (m *mockEventRepoForTrack) List(ctx context.Context, callerID uuid.UUID, f model.EventListFilter) ([]model.Event, error) {
	panic("not expected")
}
func (m *mockEventRepoForTrack) GetAccessible(ctx context.Context, eventID, callerID uuid.UUID) (*model.Event, error) {
	return m.getAccessibleFn(ctx, eventID, callerID)
}
func (m *mockEventRepoForTrack) GetByIDForOwner(ctx context.Context, eventID, ownerID uuid.UUID) (*model.Event, error) {
	return m.getByIDOwnerFn(ctx, eventID, ownerID)
}
func (m *mockEventRepoForTrack) Update(ctx context.Context, eventID uuid.UUID, req model.UpdateEventRequest) (*model.Event, error) {
	panic("not expected")
}
func (m *mockEventRepoForTrack) Delete(ctx context.Context, eventID uuid.UUID) error {
	panic("not expected")
}
func (m *mockEventRepoForTrack) AddInvite(ctx context.Context, eventID, userID uuid.UUID) error {
	panic("not expected")
}
func (m *mockEventRepoForTrack) IsInvited(ctx context.Context, eventID, userID uuid.UUID) (bool, error) {
	return m.isInvitedFn(ctx, eventID, userID)
}

// --- mock track repository ---

type mockTrackRepo struct {
	addFn      func(ctx context.Context, eventID, suggestedBy uuid.UUID, req model.SuggestTrackRequest) (*model.Track, error)
	getQueueFn func(ctx context.Context, eventID uuid.UUID) ([]model.Track, error)
	getByIDFn  func(ctx context.Context, trackID, eventID uuid.UUID) (*model.Track, error)
	voteFn     func(ctx context.Context, trackID, userID uuid.UUID) error
	deleteFn   func(ctx context.Context, trackID uuid.UUID) error
}

func (m *mockTrackRepo) Add(ctx context.Context, eventID, suggestedBy uuid.UUID, req model.SuggestTrackRequest) (*model.Track, error) {
	return m.addFn(ctx, eventID, suggestedBy, req)
}
func (m *mockTrackRepo) GetQueue(ctx context.Context, eventID uuid.UUID) ([]model.Track, error) {
	return m.getQueueFn(ctx, eventID)
}
func (m *mockTrackRepo) GetByID(ctx context.Context, trackID, eventID uuid.UUID) (*model.Track, error) {
	return m.getByIDFn(ctx, trackID, eventID)
}
func (m *mockTrackRepo) Vote(ctx context.Context, trackID, userID uuid.UUID) error {
	return m.voteFn(ctx, trackID, userID)
}
func (m *mockTrackRepo) Delete(ctx context.Context, trackID uuid.UUID) error {
	return m.deleteFn(ctx, trackID)
}

// --- helpers ---

func accessibleEvent(eventID uuid.UUID) *model.Event {
	return &model.Event{ID: eventID, Visibility: "public", CreatedAt: time.Now()}
}

func sampleTrack(eventID, trackID uuid.UUID) *model.Track {
	return &model.Track{ID: trackID, EventID: eventID, ExternalID: "dz:123", Title: "Song", Artist: "Artist", VoteCount: 0, CreatedAt: time.Now()}
}

func uniqueViolation() error {
	return &pgconn.PgError{Code: "23505"}
}

// --- tests ---

func TestTrackService_Suggest_Success(t *testing.T) {
	eventID := uuid.New()
	callerID := uuid.New()
	want := sampleTrack(eventID, uuid.New())

	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, cID uuid.UUID) (*model.Event, error) {
			return accessibleEvent(eID), nil
		},
	}
	trackRepo := &mockTrackRepo{
		addFn: func(_ context.Context, eID, sID uuid.UUID, req model.SuggestTrackRequest) (*model.Track, error) {
			if eID != eventID || sID != callerID {
				t.Errorf("unexpected Add params")
			}
			return want, nil
		},
	}

	svc := service.NewTrackService(eventRepo, trackRepo)
	got, err := svc.Suggest(context.Background(), eventID, callerID, model.SuggestTrackRequest{ExternalID: "dz:123", Title: "Song", Artist: "Artist"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("expected track ID %s, got %s", want.ID, got.ID)
	}
}

func TestTrackService_Suggest_EventNotFound(t *testing.T) {
	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, _, _ uuid.UUID) (*model.Event, error) {
			return nil, pgx.ErrNoRows
		},
	}

	svc := service.NewTrackService(eventRepo, &mockTrackRepo{})
	_, err := svc.Suggest(context.Background(), uuid.New(), uuid.New(), model.SuggestTrackRequest{})
	if !errors.Is(err, service.ErrEventNotFound) {
		t.Errorf("expected ErrEventNotFound, got %v", err)
	}
}

func TestTrackService_Suggest_Duplicate_Returns409(t *testing.T) {
	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return accessibleEvent(eID), nil
		},
	}
	trackRepo := &mockTrackRepo{
		addFn: func(_ context.Context, _, _ uuid.UUID, _ model.SuggestTrackRequest) (*model.Track, error) {
			return nil, uniqueViolation()
		},
	}

	svc := service.NewTrackService(eventRepo, trackRepo)
	_, err := svc.Suggest(context.Background(), uuid.New(), uuid.New(), model.SuggestTrackRequest{ExternalID: "dz:123", Title: "Song", Artist: "Artist"})
	if !errors.Is(err, service.ErrTrackAlreadyExists) {
		t.Errorf("expected ErrTrackAlreadyExists, got %v", err)
	}
}

func TestTrackService_GetQueue_Success(t *testing.T) {
	eventID := uuid.New()
	want := []model.Track{*sampleTrack(eventID, uuid.New()), *sampleTrack(eventID, uuid.New())}

	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return accessibleEvent(eID), nil
		},
	}
	trackRepo := &mockTrackRepo{
		getQueueFn: func(_ context.Context, eID uuid.UUID) ([]model.Track, error) {
			if eID != eventID {
				t.Errorf("unexpected eventID: %s", eID)
			}
			return want, nil
		},
	}

	svc := service.NewTrackService(eventRepo, trackRepo)
	got, err := svc.GetQueue(context.Background(), eventID, uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 tracks, got %d", len(got))
	}
}

func TestTrackService_Vote_Success(t *testing.T) {
	eventID := uuid.New()
	trackID := uuid.New()

	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return accessibleEvent(eID), nil
		},
	}
	trackRepo := &mockTrackRepo{
		getByIDFn: func(_ context.Context, tID, eID uuid.UUID) (*model.Track, error) {
			return sampleTrack(eID, tID), nil
		},
		voteFn: func(_ context.Context, _, _ uuid.UUID) error {
			return nil
		},
	}

	svc := service.NewTrackService(eventRepo, trackRepo)
	if err := svc.Vote(context.Background(), eventID, trackID, uuid.New(), model.VoteRequest{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTrackService_Vote_AlreadyVoted_Returns409(t *testing.T) {
	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return accessibleEvent(eID), nil
		},
	}
	trackRepo := &mockTrackRepo{
		getByIDFn: func(_ context.Context, tID, eID uuid.UUID) (*model.Track, error) {
			return sampleTrack(eID, tID), nil
		},
		voteFn: func(_ context.Context, _, _ uuid.UUID) error {
			return uniqueViolation()
		},
	}

	svc := service.NewTrackService(eventRepo, trackRepo)
	err := svc.Vote(context.Background(), uuid.New(), uuid.New(), uuid.New(), model.VoteRequest{})
	if !errors.Is(err, service.ErrAlreadyVoted) {
		t.Errorf("expected ErrAlreadyVoted, got %v", err)
	}
}

func TestTrackService_Vote_TrackNotFound(t *testing.T) {
	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return accessibleEvent(eID), nil
		},
	}
	trackRepo := &mockTrackRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*model.Track, error) {
			return nil, pgx.ErrNoRows
		},
	}

	svc := service.NewTrackService(eventRepo, trackRepo)
	err := svc.Vote(context.Background(), uuid.New(), uuid.New(), uuid.New(), model.VoteRequest{})
	if !errors.Is(err, service.ErrTrackNotFound) {
		t.Errorf("expected ErrTrackNotFound, got %v", err)
	}
}

// TestTrackService_Vote_ServiceLayerConcurrency verifies the service does not
// hold any shared state that would cause a race between two callers. DB-level
// atomicity (vote_count = vote_count + 1) is covered by the integration test
// in repository/track_test.go.
func TestTrackService_Vote_ServiceLayerConcurrency(t *testing.T) {
	eventID := uuid.New()
	trackID := uuid.New()
	var voteCount int32

	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return accessibleEvent(eID), nil
		},
	}
	trackRepo := &mockTrackRepo{
		getByIDFn: func(_ context.Context, tID, eID uuid.UUID) (*model.Track, error) {
			return sampleTrack(eID, tID), nil
		},
		voteFn: func(_ context.Context, _, _ uuid.UUID) error {
			atomic.AddInt32(&voteCount, 1)
			return nil
		},
	}

	svc := service.NewTrackService(eventRepo, trackRepo)

	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			errs <- svc.Vote(context.Background(), eventID, trackID, uuid.New(), model.VoteRequest{})
		}()
	}

	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Errorf("goroutine vote failed: %v", err)
		}
	}

	if got := atomic.LoadInt32(&voteCount); got != 2 {
		t.Errorf("expected voteCount=2, got %d", got)
	}
}

func TestTrackService_DeleteTrack_Owner(t *testing.T) {
	eventID := uuid.New()
	trackID := uuid.New()
	ownerID := uuid.New()
	deleted := false

	eventRepo := &mockEventRepoForTrack{
		getByIDOwnerFn: func(_ context.Context, eID, oID uuid.UUID) (*model.Event, error) {
			if eID != eventID || oID != ownerID {
				return nil, pgx.ErrNoRows
			}
			return accessibleEvent(eID), nil
		},
	}
	trackRepo := &mockTrackRepo{
		getByIDFn: func(_ context.Context, tID, eID uuid.UUID) (*model.Track, error) {
			return sampleTrack(eID, tID), nil
		},
		deleteFn: func(_ context.Context, tID uuid.UUID) error {
			if tID != trackID {
				t.Errorf("unexpected trackID: %s", tID)
			}
			deleted = true
			return nil
		},
	}

	svc := service.NewTrackService(eventRepo, trackRepo)
	if err := svc.DeleteTrack(context.Background(), eventID, trackID, ownerID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleted {
		t.Error("expected Delete to be called on repository")
	}
}

func TestTrackService_DeleteTrack_NotOwner_Returns404(t *testing.T) {
	eventRepo := &mockEventRepoForTrack{
		getByIDOwnerFn: func(_ context.Context, _, _ uuid.UUID) (*model.Event, error) {
			return nil, pgx.ErrNoRows
		},
	}

	svc := service.NewTrackService(eventRepo, &mockTrackRepo{})
	err := svc.DeleteTrack(context.Background(), uuid.New(), uuid.New(), uuid.New())
	if !errors.Is(err, service.ErrEventNotFound) {
		t.Errorf("expected ErrEventNotFound, got %v", err)
	}
}

func TestTrackService_DeleteTrack_TrackNotFound(t *testing.T) {
	eventID := uuid.New()

	eventRepo := &mockEventRepoForTrack{
		getByIDOwnerFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return accessibleEvent(eID), nil
		},
	}
	trackRepo := &mockTrackRepo{
		getByIDFn: func(_ context.Context, _, _ uuid.UUID) (*model.Track, error) {
			return nil, pgx.ErrNoRows
		},
	}

	svc := service.NewTrackService(eventRepo, trackRepo)
	if !errors.Is(svc.DeleteTrack(context.Background(), eventID, uuid.New(), uuid.New()), service.ErrTrackNotFound) {
		t.Error("expected ErrTrackNotFound")
	}
}

func TestTrackService_DeleteTrack_DeleteRepoError(t *testing.T) {
	eventID := uuid.New()
	trackID := uuid.New()
	sentinel := errors.New("db error")

	eventRepo := &mockEventRepoForTrack{
		getByIDOwnerFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return accessibleEvent(eID), nil
		},
	}
	trackRepo := &mockTrackRepo{
		getByIDFn: func(_ context.Context, tID, eID uuid.UUID) (*model.Track, error) {
			return sampleTrack(eID, tID), nil
		},
		deleteFn: func(_ context.Context, _ uuid.UUID) error {
			return sentinel
		},
	}

	svc := service.NewTrackService(eventRepo, trackRepo)
	if !errors.Is(svc.DeleteTrack(context.Background(), eventID, trackID, uuid.New()), sentinel) {
		t.Error("expected sentinel error to propagate from Delete")
	}
}

// --- license enforcement tests ---

func license1Event(eventID uuid.UUID) *model.Event {
	return &model.Event{ID: eventID, License: 1, Visibility: "public"}
}

func license2Event(eventID uuid.UUID, lat, lng, radius float64, start, end time.Time) *model.Event {
	return &model.Event{
		ID: eventID, License: 2, Visibility: "public",
		Lat: &lat, Lng: &lng, Radius: &radius,
		VoteStart: &start, VoteEnd: &end,
	}
}

func TestTrackService_Vote_License1_NotInvited_Returns403(t *testing.T) {
	eventID := uuid.New()
	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return license1Event(eID), nil
		},
		isInvitedFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return false, nil },
	}
	svc := service.NewTrackService(eventRepo, &mockTrackRepo{})
	if !errors.Is(svc.Vote(context.Background(), eventID, uuid.New(), uuid.New(), model.VoteRequest{}), service.ErrNotInvited) {
		t.Error("expected ErrNotInvited")
	}
}

func TestTrackService_Vote_License1_Invited_Succeeds(t *testing.T) {
	eventID, trackID := uuid.New(), uuid.New()
	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return license1Event(eID), nil
		},
		isInvitedFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
	}
	trackRepo := &mockTrackRepo{
		getByIDFn: func(_ context.Context, tID, eID uuid.UUID) (*model.Track, error) { return sampleTrack(eID, tID), nil },
		voteFn:    func(_ context.Context, _, _ uuid.UUID) error { return nil },
	}
	if err := service.NewTrackService(eventRepo, trackRepo).Vote(context.Background(), eventID, trackID, uuid.New(), model.VoteRequest{}); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestTrackService_Vote_License2_MissingCoords_Returns400(t *testing.T) {
	eventID := uuid.New()
	now := time.Now()
	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return license2Event(eID, 48.8566, 2.3522, 10, now.Add(-time.Hour), now.Add(time.Hour)), nil
		},
	}
	if !errors.Is(service.NewTrackService(eventRepo, &mockTrackRepo{}).Vote(context.Background(), eventID, uuid.New(), uuid.New(), model.VoteRequest{}), service.ErrMissingCoords) {
		t.Error("expected ErrMissingCoords")
	}
}

func TestTrackService_Vote_License2_OutOfRange_Returns403(t *testing.T) {
	eventID := uuid.New()
	now := time.Now()
	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return license2Event(eID, 48.8566, 2.3522, 1, now.Add(-time.Hour), now.Add(time.Hour)), nil
		},
	}
	lat, lng := 51.5074, -0.1278 // London, ~340 km from Paris
	if !errors.Is(service.NewTrackService(eventRepo, &mockTrackRepo{}).Vote(context.Background(), eventID, uuid.New(), uuid.New(), model.VoteRequest{Lat: &lat, Lng: &lng}), service.ErrOutOfRange) {
		t.Error("expected ErrOutOfRange")
	}
}

// Radius is stored in metres. A voter 340 km away must be rejected even when
// the numeric radius value (1000) is large, otherwise the metre value is being
// compared against a kilometre distance and geofencing is bypassed.
func TestTrackService_Vote_License2_MetersRadius_OutOfRange(t *testing.T) {
	eventID := uuid.New()
	now := time.Now()
	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return license2Event(eID, 48.8566, 2.3522, 1000, now.Add(-time.Hour), now.Add(time.Hour)), nil
		},
	}
	lat, lng := 51.5074, -0.1278 // London, ~340 km from a 1000 m (1 km) radius Paris event
	if !errors.Is(service.NewTrackService(eventRepo, &mockTrackRepo{}).Vote(context.Background(), eventID, uuid.New(), uuid.New(), model.VoteRequest{Lat: &lat, Lng: &lng}), service.ErrOutOfRange) {
		t.Error("expected ErrOutOfRange for a voter 340 km outside a 1000 m radius")
	}
}

// A voter ~1 km from the event with a 2000 m (2 km) radius is inside the zone
// and must be accepted; guards against an over-aggressive metre conversion.
func TestTrackService_Vote_License2_MetersRadius_InRange_Succeeds(t *testing.T) {
	eventID, trackID := uuid.New(), uuid.New()
	now := time.Now()
	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return license2Event(eID, 48.8566, 2.3522, 2000, now.Add(-time.Hour), now.Add(time.Hour)), nil
		},
	}
	trackRepo := &mockTrackRepo{
		getByIDFn: func(_ context.Context, tID, eID uuid.UUID) (*model.Track, error) { return sampleTrack(eID, tID), nil },
		voteFn:    func(_ context.Context, _, _ uuid.UUID) error { return nil },
	}
	lat, lng := 48.8656, 2.3522 // ~1 km north of Paris, inside the 2 km radius
	if err := service.NewTrackService(eventRepo, trackRepo).Vote(context.Background(), eventID, trackID, uuid.New(), model.VoteRequest{Lat: &lat, Lng: &lng}); err != nil {
		t.Fatalf("expected success for a voter ~1 km inside a 2000 m radius, got %v", err)
	}
}

func TestTrackService_Vote_License2_VotingClosed_Returns403(t *testing.T) {
	eventID := uuid.New()
	past := time.Now().Add(-2 * time.Hour)
	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return license2Event(eID, 48.8566, 2.3522, 10000, past.Add(-time.Hour), past), nil
		},
	}
	lat, lng := 48.8566, 2.3522
	if !errors.Is(service.NewTrackService(eventRepo, &mockTrackRepo{}).Vote(context.Background(), eventID, uuid.New(), uuid.New(), model.VoteRequest{Lat: &lat, Lng: &lng}), service.ErrVotingClosed) {
		t.Error("expected ErrVotingClosed")
	}
}

func TestTrackService_Vote_License2_AllConditionsMet_Succeeds(t *testing.T) {
	eventID, trackID := uuid.New(), uuid.New()
	now := time.Now()
	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return license2Event(eID, 48.8566, 2.3522, 10000, now.Add(-time.Hour), now.Add(time.Hour)), nil
		},
	}
	trackRepo := &mockTrackRepo{
		getByIDFn: func(_ context.Context, tID, eID uuid.UUID) (*model.Track, error) { return sampleTrack(eID, tID), nil },
		voteFn:    func(_ context.Context, _, _ uuid.UUID) error { return nil },
	}
	lat, lng := 48.8566, 2.3522
	if err := service.NewTrackService(eventRepo, trackRepo).Vote(context.Background(), eventID, trackID, uuid.New(), model.VoteRequest{Lat: &lat, Lng: &lng}); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

// --- suggest license enforcement tests ---

func TestTrackService_Suggest_License1_NotInvited_Returns403(t *testing.T) {
	eventID := uuid.New()
	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return license1Event(eID), nil
		},
		isInvitedFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return false, nil },
	}
	svc := service.NewTrackService(eventRepo, &mockTrackRepo{})
	_, err := svc.Suggest(context.Background(), eventID, uuid.New(), model.SuggestTrackRequest{ExternalID: "dz:1", Title: "T", Artist: "A"})
	if !errors.Is(err, service.ErrNotInvited) {
		t.Errorf("expected ErrNotInvited, got %v", err)
	}
}

func TestTrackService_Suggest_License1_Invited_Succeeds(t *testing.T) {
	eventID := uuid.New()
	want := sampleTrack(eventID, uuid.New())
	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return license1Event(eID), nil
		},
		isInvitedFn: func(_ context.Context, _, _ uuid.UUID) (bool, error) { return true, nil },
	}
	trackRepo := &mockTrackRepo{
		addFn: func(_ context.Context, _, _ uuid.UUID, _ model.SuggestTrackRequest) (*model.Track, error) { return want, nil },
	}
	got, err := service.NewTrackService(eventRepo, trackRepo).Suggest(context.Background(), eventID, uuid.New(), model.SuggestTrackRequest{ExternalID: "dz:1", Title: "T", Artist: "A"})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("expected track ID %s, got %s", want.ID, got.ID)
	}
}

func TestTrackService_Suggest_License2_MissingCoords(t *testing.T) {
	eventID := uuid.New()
	now := time.Now()
	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return license2Event(eID, 48.8566, 2.3522, 10, now.Add(-time.Hour), now.Add(time.Hour)), nil
		},
	}
	_, err := service.NewTrackService(eventRepo, &mockTrackRepo{}).Suggest(context.Background(), eventID, uuid.New(), model.SuggestTrackRequest{ExternalID: "dz:1", Title: "T", Artist: "A"})
	if !errors.Is(err, service.ErrMissingCoords) {
		t.Errorf("expected ErrMissingCoords, got %v", err)
	}
}

func TestTrackService_Suggest_License2_OutOfRange(t *testing.T) {
	eventID := uuid.New()
	now := time.Now()
	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return license2Event(eID, 48.8566, 2.3522, 1, now.Add(-time.Hour), now.Add(time.Hour)), nil
		},
	}
	lat, lng := 51.5074, -0.1278 // London
	req := model.SuggestTrackRequest{ExternalID: "dz:1", Title: "T", Artist: "A", Lat: &lat, Lng: &lng}
	_, err := service.NewTrackService(eventRepo, &mockTrackRepo{}).Suggest(context.Background(), eventID, uuid.New(), req)
	if !errors.Is(err, service.ErrOutOfRange) {
		t.Errorf("expected ErrOutOfRange, got %v", err)
	}
}

func TestTrackService_Suggest_License2_VotingClosed(t *testing.T) {
	eventID := uuid.New()
	past := time.Now().Add(-2 * time.Hour)
	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return license2Event(eID, 48.8566, 2.3522, 10000, past.Add(-time.Hour), past), nil
		},
	}
	lat, lng := 48.8566, 2.3522
	req := model.SuggestTrackRequest{ExternalID: "dz:1", Title: "T", Artist: "A", Lat: &lat, Lng: &lng}
	_, err := service.NewTrackService(eventRepo, &mockTrackRepo{}).Suggest(context.Background(), eventID, uuid.New(), req)
	if !errors.Is(err, service.ErrVotingClosed) {
		t.Errorf("expected ErrVotingClosed, got %v", err)
	}
}

func TestTrackService_Suggest_License2_InRange_Succeeds(t *testing.T) {
	eventID := uuid.New()
	now := time.Now()
	want := sampleTrack(eventID, uuid.New())
	eventRepo := &mockEventRepoForTrack{
		getAccessibleFn: func(_ context.Context, eID, _ uuid.UUID) (*model.Event, error) {
			return license2Event(eID, 48.8566, 2.3522, 10000, now.Add(-time.Hour), now.Add(time.Hour)), nil
		},
	}
	trackRepo := &mockTrackRepo{
		addFn: func(_ context.Context, _, _ uuid.UUID, _ model.SuggestTrackRequest) (*model.Track, error) { return want, nil },
	}
	lat, lng := 48.8566, 2.3522
	req := model.SuggestTrackRequest{ExternalID: "dz:1", Title: "T", Artist: "A", Lat: &lat, Lng: &lng}
	got, err := service.NewTrackService(eventRepo, trackRepo).Suggest(context.Background(), eventID, uuid.New(), req)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("expected track ID %s, got %s", want.ID, got.ID)
	}
}

// --- haversine tests ---

func TestHaversineKm_ParisToBerlin(t *testing.T) {
	got := service.HaversineKm(48.8566, 2.3522, 52.5200, 13.4050)
	if got < 870 || got > 890 {
		t.Errorf("Paris-Berlin: expected ~878 km, got %.1f", got)
	}
}

func TestHaversineKm_SamePoint_ReturnsZero(t *testing.T) {
	if got := service.HaversineKm(48.8566, 2.3522, 48.8566, 2.3522); got > 0.001 {
		t.Errorf("same point: expected 0, got %.6f", got)
	}
}

func TestHaversineKm_ParisToLondon(t *testing.T) {
	got := service.HaversineKm(48.8566, 2.3522, 51.5074, -0.1278)
	if got < 330 || got > 350 {
		t.Errorf("Paris-London: expected ~340 km, got %.1f", got)
	}
}

// Ensure mockEventRepoForTrack implements repository.EventRepository at compile time.
var _ repository.EventRepository = (*mockEventRepoForTrack)(nil)

// Ensure mockTrackRepo implements repository.TrackRepository at compile time.
var _ repository.TrackRepository = (*mockTrackRepo)(nil)
