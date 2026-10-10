package app

import (
	"context"
	"errors"
	"testing"
)

func TestUsersCreateChangePasswordAndDelete(t *testing.T) {
	t.Parallel()
	_, host := deviceFakeHost(t, nil)
	request := deviceRequest(t, host)
	view, err := fastEncoder().ReadUsers(context.Background(), request)
	if err != nil || len(view.Users) != 1 || view.CurrentUsername != "admin" || view.DefaultCredentialWarning {
		t.Fatalf("ReadUsers() = %+v, %v", view, err)
	}
	created, err := fastEncoder().CreateUser(context.Background(), request, UserChange{Username: "viewer", Password: "viewer-pass-1", Level: "User"})
	if err != nil || created.Report.Outcome != OutcomeExact || len(created.Users) != 2 {
		t.Fatalf("CreateUser() = %+v, %v", created, err)
	}
	changed, err := fastEncoder().SetUser(context.Background(), request, UserChange{Username: "admin", Password: "rotated-secret-9", Level: "Administrator"})
	if err != nil || changed.Report.Outcome != OutcomeExact || !changed.SignInVerified {
		t.Fatalf("SetUser() = %+v, %v", changed, err)
	}
	if _, err := fastEncoder().ReadUsers(context.Background(), request); err == nil {
		t.Fatal("the old password still signs in after the change")
	}
	rotated, err := NewCredentials("admin", "rotated-secret-9", false)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	request.Credentials = rotated
	deleted, err := fastEncoder().DeleteUser(context.Background(), request, "viewer")
	if err != nil || deleted.Report.Outcome != OutcomeExact || len(deleted.Users) != 1 {
		t.Fatalf("DeleteUser() = %+v, %v", deleted, err)
	}
}

func TestUsersRejectWeakDefaultAndSelfDeletion(t *testing.T) {
	t.Parallel()
	request := DeviceRequest{Host: "192.0.2.1"}
	creds, err := NewCredentials("admin", "admin", false)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	request.Credentials = creds
	for name, call := range map[string]func() error{
		"short": func() error {
			_, err := fastEncoder().SetUser(context.Background(), request, UserChange{Username: "admin", Password: "short", Level: "Administrator"})
			return err
		},
		"default": func() error {
			_, err := fastEncoder().SetUser(context.Background(), request, UserChange{Username: "admin", Password: "password", Level: "Administrator"})
			return err //nolint:gosec // synthetic default probe
		},
		"same name": func() error {
			_, err := fastEncoder().CreateUser(context.Background(), request, UserChange{Username: "operator1", Password: "operator1", Level: "Operator"})
			return err
		},
		"anonymous": func() error {
			_, err := fastEncoder().CreateUser(context.Background(), request, UserChange{Username: "x", Password: "long-enough-1", Level: "Anonymous"})
			return err
		},
		"bad name": func() error {
			_, err := fastEncoder().CreateUser(context.Background(), request, UserChange{Username: "a b", Password: "long-enough-1", Level: "User"})
			return err
		},
		"delete self": func() error { _, err := fastEncoder().DeleteUser(context.Background(), request, "admin"); return err },
	} {
		var appErr *Error
		if err := call(); !errors.As(err, &appErr) || appErr.Category != CategoryUsage {
			t.Errorf("%s: error = %v", name, err)
		}
	}
	if !IsKnownDefaultCredential("admin", "12345") || IsKnownDefaultCredential("admin", "rotated-secret-9") {
		t.Fatal("default credential detection is wrong")
	}
}
