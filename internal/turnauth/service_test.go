package turnauth

import (
	"testing"
	"time"
)

func TestBuildICEBundleWithRelay(t *testing.T) {
	now := time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)
	service, err := NewService(Config{
		StunURLs:     []string{"stun:stun.example.com:3478"},
		TurnURLs:     []string{"turn:turn.example.com:3478?transport=udp", "turns:turn.example.com:5349?transport=tcp"},
		SharedSecret: "shared-secret",
		TTL:          2 * time.Hour,
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	bundle, err := service.BuildICEBundle("participant-123")
	if err != nil {
		t.Fatalf("BuildICEBundle() error = %v", err)
	}

	if len(bundle.IceServers) != 2 {
		t.Fatalf("len(bundle.IceServers) = %d, want 2", len(bundle.IceServers))
	}

	if bundle.IceServers[0].URLs[0] != "stun:stun.example.com:3478" {
		t.Fatalf("stun urls = %v", bundle.IceServers[0].URLs)
	}

	expectedUsername := "1777975200:participant-123"
	if bundle.IceServers[1].Username != expectedUsername {
		t.Fatalf("username = %q, want %q", bundle.IceServers[1].Username, expectedUsername)
	}

	expectedCredential := "NBr+i2phd2zu1bX2c2J4bnYJYvs="
	if bundle.IceServers[1].Credential != expectedCredential {
		t.Fatalf("credential = %q, want %q", bundle.IceServers[1].Credential, expectedCredential)
	}

	expectedExpiresAt := now.Add(2 * time.Hour)
	if !bundle.ExpiresAt.Equal(expectedExpiresAt) {
		t.Fatalf("expiresAt = %s, want %s", bundle.ExpiresAt, expectedExpiresAt)
	}
}

func TestBuildICEBundleWithoutRelaySecretFallsBackToSTUNOnly(t *testing.T) {
	service, err := NewService(Config{
		StunURLs: []string{"stun:stun.example.com:3478"},
		TurnURLs: []string{"turn:turn.example.com:3478?transport=udp"},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	bundle, err := service.BuildICEBundle("participant-123")
	if err != nil {
		t.Fatalf("BuildICEBundle() error = %v", err)
	}

	if len(bundle.IceServers) != 1 {
		t.Fatalf("len(bundle.IceServers) = %d, want 1", len(bundle.IceServers))
	}

	if !bundle.ExpiresAt.IsZero() {
		t.Fatalf("expiresAt = %s, want zero", bundle.ExpiresAt)
	}
}

func TestNewServiceRejectsTooShortTTL(t *testing.T) {
	if _, err := NewService(Config{TTL: 500 * time.Millisecond}); err == nil {
		t.Fatal("NewService() error = nil, want error")
	}
}
