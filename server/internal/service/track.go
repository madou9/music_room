package service

import (
	"context"
	"errors"
	"math"
	"time"

	"music-room/internal/model"
	"music-room/internal/repository"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrTrackNotFound      = errors.New("track not found")
	ErrTrackAlreadyExists = errors.New("track already in this event")
	ErrAlreadyVoted       = errors.New("already voted on this track")
	ErrNotInvited         = errors.New("NOT_INVITED")
	ErrOutOfRange         = errors.New("OUT_OF_RANGE")
	ErrVotingClosed       = errors.New("VOTING_CLOSED")
	ErrMissingCoords      = errors.New("lat and lng are required to vote on this event")
)

type TrackService interface {
	Suggest(ctx context.Context, eventID, callerID uuid.UUID, req model.SuggestTrackRequest) (*model.Track, error)
	GetQueue(ctx context.Context, eventID, callerID uuid.UUID) ([]model.Track, error)
	Vote(ctx context.Context, eventID, trackID, callerID uuid.UUID, gps model.VoteRequest) error
	DeleteTrack(ctx context.Context, eventID, trackID, callerID uuid.UUID) error
}

type trackService struct {
	eventRepo repository.EventRepository
	trackRepo repository.TrackRepository
}

func NewTrackService(eventRepo repository.EventRepository, trackRepo repository.TrackRepository) TrackService {
	return &trackService{eventRepo: eventRepo, trackRepo: trackRepo}
}

func (s *trackService) Suggest(ctx context.Context, eventID, callerID uuid.UUID, req model.SuggestTrackRequest) (*model.Track, error) {
	event, err := s.eventRepo.GetAccessible(ctx, eventID, callerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrEventNotFound
		}
		return nil, err
	}

	if err := s.enforceLicense(ctx, event, callerID, model.VoteRequest{Lat: req.Lat, Lng: req.Lng}); err != nil {
		return nil, err
	}

	track, err := s.trackRepo.Add(ctx, eventID, callerID, req)
	if err != nil {
		if isPgUniqueViolation(err) {
			return nil, ErrTrackAlreadyExists
		}
		return nil, err
	}
	return track, nil
}

func (s *trackService) GetQueue(ctx context.Context, eventID, callerID uuid.UUID) ([]model.Track, error) {
	if _, err := s.eventRepo.GetAccessible(ctx, eventID, callerID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrEventNotFound
		}
		return nil, err
	}

	return s.trackRepo.GetQueue(ctx, eventID)
}

func (s *trackService) Vote(ctx context.Context, eventID, trackID, callerID uuid.UUID, gps model.VoteRequest) error {
	event, err := s.eventRepo.GetAccessible(ctx, eventID, callerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrEventNotFound
		}
		return err
	}

	if err := s.enforceLicense(ctx, event, callerID, gps); err != nil {
		return err
	}

	if _, err := s.trackRepo.GetByID(ctx, trackID, eventID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTrackNotFound
		}
		return err
	}

	if err := s.trackRepo.Vote(ctx, trackID, callerID); err != nil {
		if isPgUniqueViolation(err) {
			return ErrAlreadyVoted
		}
		return err
	}
	return nil
}

func (s *trackService) enforceLicense(ctx context.Context, event *model.Event, callerID uuid.UUID, gps model.VoteRequest) error {
	if callerID == event.OwnerID {
		return nil
	}
	switch event.License {
	case 1:
		invited, err := s.eventRepo.IsInvited(ctx, event.ID, callerID)
		if err != nil {
			return err
		}
		if !invited {
			return ErrNotInvited
		}
	case 2:
		if gps.Lat == nil || gps.Lng == nil {
			return ErrMissingCoords
		}
		// event.Radius is stored in metres; HaversineKm returns kilometres.
		radiusKm := *event.Radius / 1000.0
		if dist := HaversineKm(*gps.Lat, *gps.Lng, *event.Lat, *event.Lng); dist > radiusKm {
			return ErrOutOfRange
		}
		now := time.Now()
		if now.Before(*event.VoteStart) || now.After(*event.VoteEnd) {
			return ErrVotingClosed
		}
	}
	return nil
}

func (s *trackService) DeleteTrack(ctx context.Context, eventID, trackID, callerID uuid.UUID) error {
	if _, err := s.eventRepo.GetByIDForOwner(ctx, eventID, callerID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrEventNotFound
		}
		return err
	}

	if _, err := s.trackRepo.GetByID(ctx, trackID, eventID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTrackNotFound
		}
		return err
	}

	return s.trackRepo.Delete(ctx, trackID)
}

// HaversineKm returns the great-circle distance in kilometres between two points.
func HaversineKm(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLng := (lng2 - lng1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLng/2)*math.Sin(dLng/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func isPgUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
