package turnauth

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

const DefaultTTL = 12 * time.Hour

var defaultSTUNURLs = []string{"stun:stun.l.google.com:19302"}

type Config struct {
	StunURLs     []string
	TurnURLs     []string
	SharedSecret string
	TTL          time.Duration
	Now          func() time.Time
}

type IceServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type Bundle struct {
	IceServers []IceServer
	ExpiresAt  time.Time
}

type Service struct {
	stunURLs     []string
	turnURLs     []string
	sharedSecret string
	ttl          time.Duration
	now          func() time.Time
}

func NewService(cfg Config) (*Service, error) {
	ttl := cfg.TTL
	if ttl == 0 {
		ttl = DefaultTTL
	}
	if ttl < time.Second {
		return nil, fmt.Errorf("turn credential ttl must be at least 1 second")
	}

	stunURLs := normalizeURLList(cfg.StunURLs)
	if len(stunURLs) == 0 {
		stunURLs = append([]string(nil), defaultSTUNURLs...)
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &Service{
		stunURLs:     stunURLs,
		turnURLs:     normalizeURLList(cfg.TurnURLs),
		sharedSecret: strings.TrimSpace(cfg.SharedSecret),
		ttl:          ttl,
		now:          now,
	}, nil
}

func ParseURLList(raw string) []string {
	return normalizeURLList([]string{raw})
}

func (s *Service) HasRelay() bool {
	return len(s.turnURLs) > 0 && s.sharedSecret != ""
}

func (s *Service) BuildICEBundle(participantID string) (Bundle, error) {
	trimmedParticipantID := strings.TrimSpace(participantID)
	if trimmedParticipantID == "" {
		return Bundle{}, fmt.Errorf("participant id is required")
	}

	bundle := Bundle{
		IceServers: []IceServer{
			{
				URLs: append([]string(nil), s.stunURLs...),
			},
		},
	}

	if !s.HasRelay() {
		return bundle, nil
	}

	expiresAt := s.now().UTC().Add(s.ttl)
	username := fmt.Sprintf("%d:%s", expiresAt.Unix(), trimmedParticipantID)
	credential, err := buildCredential(s.sharedSecret, username)
	if err != nil {
		return Bundle{}, err
	}

	bundle.IceServers = append(bundle.IceServers, IceServer{
		URLs:       append([]string(nil), s.turnURLs...),
		Username:   username,
		Credential: credential,
	})
	bundle.ExpiresAt = expiresAt

	return bundle, nil
}

func buildCredential(sharedSecret string, username string) (string, error) {
	mac := hmac.New(sha1.New, []byte(sharedSecret))
	if _, err := mac.Write([]byte(username)); err != nil {
		return "", fmt.Errorf("sign turn credential: %w", err)
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

func normalizeURLList(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		for _, item := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r'
		}) {
			trimmed := strings.TrimSpace(item)
			if trimmed == "" {
				continue
			}
			normalized = append(normalized, trimmed)
		}
	}
	return normalized
}
