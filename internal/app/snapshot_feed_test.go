package app

import (
	"bytes"
	"context"
	"errors"
	"image/jpeg"
	"strings"
	"testing"
	"time"
)

func TestSnapshotFeedFetchesRepeatedValidatedJPEGs(t *testing.T) {
	t.Parallel()
	secret := "feed-secret-" + t.Name()
	_, host := mediaFakeAuth(t, "admin", secret, true)
	creds, err := NewCredentials("admin", secret, false)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	feed, err := NewProductionSnapshotFeed().OpenSnapshotFeed(context.Background(), SnapshotFeedRequest{
		Host:         host,
		Credentials:  creds,
		ProfileToken: "profile_main",
		Timeout:      10 * time.Second,
	})
	if err != nil {
		t.Fatalf("OpenSnapshotFeed() error = %v", err)
	}
	for i := 0; i < 3; i++ {
		image, err := feed.Fetch(context.Background())
		if err != nil {
			t.Fatalf("Fetch(%d) error = %v", i, err)
		}
		if _, err := jpeg.DecodeConfig(bytes.NewReader(image)); err != nil {
			t.Fatalf("Fetch(%d) returned an invalid JPEG: %v", i, err)
		}
	}
}

func TestSnapshotFeedRejectsBadTokenAndWrongCredentials(t *testing.T) {
	t.Parallel()
	_, host := mediaFakeAuth(t, "admin", "right-secret", true)
	good, err := NewCredentials("admin", "right-secret", false)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	if _, err := NewProductionSnapshotFeed().OpenSnapshotFeed(context.Background(), SnapshotFeedRequest{
		Host: host, Credentials: good, ProfileToken: "",
	}); err == nil {
		t.Fatal("OpenSnapshotFeed() accepted an empty profile token")
	}
	wrong, err := NewCredentials("admin", "wrong-marker", false)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	_, err = NewProductionSnapshotFeed().OpenSnapshotFeed(context.Background(), SnapshotFeedRequest{
		Host: host, Credentials: wrong, ProfileToken: "profile_main", Timeout: 10 * time.Second,
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Category != CategoryAuthentication {
		t.Fatalf("OpenSnapshotFeed(wrong credentials) error = %v, want authentication", err)
	}
	if strings.Contains(err.Error(), "wrong-marker") {
		t.Fatalf("error leaked the password: %v", err)
	}
}
