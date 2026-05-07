package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/misterchenleiya/meeting/internal/auth"
	"github.com/misterchenleiya/meeting/internal/meeting"
	"github.com/misterchenleiya/meeting/internal/turnauth"
)

type iceServersResponse struct {
	IceServers []turnauth.IceServer `json:"iceServers"`
	ExpiresAt  string               `json:"expiresAt,omitempty"`
}

func (s *Server) handleGetICEServers(w http.ResponseWriter, r *http.Request) {
	meetingIdentifier := meetingIdentifierFromPath(r)
	participantID := r.PathValue("participantID")

	meetingValue, found := s.meetings.GetMeeting(meetingIdentifier)
	if !found {
		writeError(w, http.StatusNotFound, "meeting not found")
		return
	}

	participant, found := meetingValue.Participants[participantID]
	if !found {
		writeError(w, http.StatusNotFound, "participant not found")
		return
	}

	if err := s.authorizeICERequest(r, participant); err != nil {
		switch {
		case errors.Is(err, auth.ErrSessionNotFound), errors.Is(err, auth.ErrSessionExpired):
			writeError(w, http.StatusUnauthorized, err.Error())
		case errors.Is(err, meeting.ErrUnauthorized):
			writeError(w, http.StatusForbidden, err.Error())
		default:
			s.logger.Error("authorize ice request failed", "error", err, "meetingId", meetingValue.ID, "meetingNumber", meetingValue.MeetingNumber, "participantId", participantID)
			writeError(w, http.StatusInternalServerError, "failed to authorize ice request")
		}
		return
	}

	iceServers, expiresAt, err := s.buildICEBundle(participant.ID)
	if err != nil {
		s.logger.Error("build runtime ice servers failed", "error", err, "meetingId", meetingValue.ID, "meetingNumber", meetingValue.MeetingNumber, "participantId", participantID)
		writeError(w, http.StatusInternalServerError, "failed to build meeting ice servers")
		return
	}

	writeJSON(w, http.StatusOK, iceServersResponse{
		IceServers: iceServers,
		ExpiresAt:  formatOptionalTimestamp(expiresAt),
	})
}

func (s *Server) authorizeICERequest(r *http.Request, participant *meeting.Participant) error {
	if participant.UserID == "" {
		if s.auth == nil {
			return nil
		}
		if _, _, err := s.currentAuthenticatedUser(r); err != nil && !errors.Is(err, auth.ErrSessionNotFound) && !errors.Is(err, auth.ErrSessionExpired) {
			return err
		}
		return nil
	}

	currentUser, _, err := s.currentAuthenticatedUser(r)
	if err != nil {
		return err
	}
	if currentUser.ID != participant.UserID {
		return meeting.ErrUnauthorized
	}
	return nil
}

func (s *Server) buildICEBundle(participantID string) ([]turnauth.IceServer, time.Time, error) {
	if s.turn == nil {
		return nil, time.Time{}, nil
	}

	bundle, err := s.turn.BuildICEBundle(participantID)
	if err != nil {
		return nil, time.Time{}, err
	}

	return bundle.IceServers, bundle.ExpiresAt, nil
}
