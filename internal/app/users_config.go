package app

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/addodelgrossi/ovm/pkg/onvif/device"
)

// UserDTO is a camera account without its password.
type UserDTO struct {
	Username string `json:"username"`
	Level    string `json:"level"`
}

// UsersView lists the camera accounts.
type UsersView struct {
	Users []UserDTO `json:"users"`
	// CurrentUsername is the account ovm authenticates with.
	CurrentUsername string `json:"current_username"`
	// DefaultCredentialWarning reports that the stored credential matches a
	// widely published factory default. It is a local comparison only; no
	// other password is ever tried against the camera.
	DefaultCredentialWarning bool `json:"default_credential_warning"`
}

// UserChange creates an account or changes its password and level.
type UserChange struct {
	Username string `json:"username"`
	Password string `json:"-"`
	Level    string `json:"level"`
}

// UsersApplyResult is the verified result of an account change.
type UsersApplyResult struct {
	Users  []UserDTO      `json:"users"`
	Report MutationReport `json:"report"`
	// SignInVerified reports that the camera accepted the new password.
	SignInVerified bool `json:"sign_in_verified"`
}

// UsersUseCase manages camera accounts.
type UsersUseCase interface {
	ReadUsers(ctx context.Context, request DeviceRequest) (UsersView, error)
	SetUser(ctx context.Context, request DeviceRequest, change UserChange) (UsersApplyResult, error)
	CreateUser(ctx context.Context, request DeviceRequest, change UserChange) (UsersApplyResult, error)
	DeleteUser(ctx context.Context, request DeviceRequest, username string) (UsersApplyResult, error)
}

// NewProductionUsers returns the production UsersUseCase.
func NewProductionUsers() UsersUseCase { return &productionMedia{} }

// MinimumPasswordLength is the shortest password ovm will set.
const MinimumPasswordLength = 8

// knownDefaultCredentials are widely published factory defaults. They are
// only compared with the credential the technician supplied.
var knownDefaultCredentials = map[string][]string{
	"admin":   {"", "admin", "12345", "123456", "1234", "password", "9999", "888888", "admin123", "fliradmin", "meinsm"},
	"root":    {"root", "pass", "admin", "ikwd"},
	"service": {"service"},
	"ubnt":    {"ubnt"},
	"Admin":   {"1234", "123456"},
	"user":    {"user"},
	"guest":   {"guest"},
}

// IsKnownDefaultCredential reports whether a username and password pair is a
// published factory default.
func IsKnownDefaultCredential(username, password string) bool {
	return slices.Contains(knownDefaultCredentials[username], password)
}

func validateUserChange(change UserChange, requirePassword bool) error {
	if change.Username == "" || !utf8.ValidString(change.Username) || utf8.RuneCountInString(change.Username) > 32 ||
		strings.ContainsAny(change.Username, " <>&\"'\t\r\n") {
		return usageError("the username is not valid")
	}
	if change.Level != "Administrator" && change.Level != "Operator" && change.Level != "User" {
		return usageError("the user level must be Administrator, Operator, or User")
	}
	if requirePassword || change.Password != "" {
		if utf8.RuneCountInString(change.Password) < MinimumPasswordLength || utf8.RuneCountInString(change.Password) > 64 {
			return usageError("the password must have between 8 and 64 characters")
		}
		if IsKnownDefaultCredential(change.Username, change.Password) || strings.EqualFold(change.Password, change.Username) {
			return usageError("the password is a known default and cannot be used")
		}
	}
	return nil
}

func (p *productionMedia) listUsers(ctx context.Context, seam *authDeviceSeam) ([]UserDTO, error) {
	var users []device.User
	err := seam.call(ctx, func(ctx context.Context, opts []device.RequestOptions) error {
		var callErr error
		users, callErr = seam.client.GetUsers(ctx, opts...)
		return callErr
	})
	if err != nil {
		return nil, err
	}
	out := make([]UserDTO, 0, len(users))
	for _, user := range users {
		out = append(out, UserDTO{Username: user.Username, Level: user.UserLevel})
	}
	return out, nil
}

// ReadUsers lists the camera accounts.
func (p *productionMedia) ReadUsers(ctx context.Context, request DeviceRequest) (UsersView, error) {
	seam, opCtx, cancel, err := p.openDevice(ctx, request)
	if err != nil {
		return UsersView{}, err
	}
	defer cancel()
	users, err := p.listUsers(opCtx, seam)
	if err != nil {
		return UsersView{}, mapConfigError(err)
	}
	view := UsersView{Users: users}
	if request.Credentials != nil {
		view.CurrentUsername = request.Credentials.username
		view.DefaultCredentialWarning = IsKnownDefaultCredential(request.Credentials.username, request.Credentials.password)
	}
	return view, nil
}

func userFields(users []UserDTO, username string) fieldValues {
	fields := newFieldValues()
	level := ""
	if index := slices.IndexFunc(users, func(u UserDTO) bool { return u.Username == username }); index >= 0 {
		level = users[index].Level
	}
	fields.set("user:"+username, level)
	return fields
}

// mutateUsers applies one account operation and reads the list back. When
// readAs is set, the read back signs in with those credentials instead, as
// after changing the password of the signed-in account.
func (p *productionMedia) mutateUsers(ctx context.Context, request DeviceRequest, readAs *Credentials, username, wantLevel, rejected string,
	apply func(ctx context.Context, seam *authDeviceSeam, opts []device.RequestOptions) error,
) (UsersApplyResult, error) {
	seam, opCtx, cancel, err := p.openDevice(ctx, request)
	if err != nil {
		return UsersApplyResult{}, err
	}
	defer cancel()
	before, err := p.listUsers(opCtx, seam)
	if err != nil {
		return UsersApplyResult{}, mapConfigError(err)
	}
	err = seam.call(opCtx, func(ctx context.Context, opts []device.RequestOptions) error {
		return apply(ctx, seam, opts)
	})
	if err != nil {
		return UsersApplyResult{}, mapSetError(err, rejected)
	}
	if readAs != nil {
		reader := request
		reader.Credentials = readAs
		readSeam, readCtx, readCancel, err := p.openDevice(ctx, reader)
		if err != nil {
			return UsersApplyResult{Report: MutationReport{Outcome: OutcomeDisconnected, Changes: []FieldChange{}}}, nil
		}
		defer readCancel()
		seam, opCtx = readSeam, readCtx
	}
	requested := newFieldValues()
	requested.set("user:"+username, wantLevel)
	var after []UserDTO
	afterFields, ok := readBack(opCtx, p.readback(), requested, func(ctx context.Context) (fieldValues, error) {
		current, err := p.listUsers(ctx, seam)
		if err != nil {
			return fieldValues{}, err
		}
		after = current
		return userFields(current, username), nil
	})
	if !ok {
		return UsersApplyResult{Report: MutationReport{Outcome: OutcomeDisconnected, Changes: []FieldChange{}}}, nil
	}
	return UsersApplyResult{Users: after, Report: compareMutation(userFields(before, username), requested, afterFields)}, nil
}

// verifySignIn opens a fresh session with the new password.
func (p *productionMedia) verifySignIn(ctx context.Context, request DeviceRequest, username, password string) bool {
	credentials, err := NewCredentialsWithPolicy(username, password, request.Credentials.policy)
	if err != nil {
		return false
	}
	verify := request
	verify.Credentials = credentials
	seam, opCtx, cancel, err := p.openDevice(ctx, verify)
	if err != nil {
		return false
	}
	defer cancel()
	_, err = p.listUsers(opCtx, seam)
	return err == nil
}

// SetUser changes an account's password and level, then verifies sign-in
// with the new password.
func (p *productionMedia) SetUser(ctx context.Context, request DeviceRequest, change UserChange) (UsersApplyResult, error) {
	if err := validateUserChange(change, true); err != nil {
		return UsersApplyResult{}, err
	}
	var readAs *Credentials
	if request.Credentials != nil && request.Credentials.username == change.Username {
		rotated, err := NewCredentialsWithPolicy(change.Username, change.Password, request.Credentials.policy)
		if err != nil {
			return UsersApplyResult{}, usageError("the password is not valid")
		}
		readAs = rotated
	}
	result, err := p.mutateUsers(ctx, request, readAs, change.Username, change.Level, "the camera rejected the password change",
		func(ctx context.Context, seam *authDeviceSeam, opts []device.RequestOptions) error {
			return seam.client.SetUser(ctx, []device.UserChange{{Username: change.Username, Password: change.Password, UserLevel: change.Level}}, opts...)
		})
	if err != nil || request.Credentials == nil {
		return result, err
	}
	if result.Report.Outcome == OutcomeDisconnected && readAs == nil {
		return result, nil
	}
	result.SignInVerified = p.verifySignIn(ctx, request, change.Username, change.Password)
	if !result.SignInVerified {
		result.Report.Outcome = OutcomeNotApplied
	}
	result.Report.Changes = append(result.Report.Changes, FieldChange{
		Field: "password:" + change.Username, Before: "unchanged", Requested: "changed", After: map[bool]string{true: "accepted at sign-in", false: "rejected at sign-in"}[result.SignInVerified],
	})
	return result, nil
}

// CreateUser adds an account.
func (p *productionMedia) CreateUser(ctx context.Context, request DeviceRequest, change UserChange) (UsersApplyResult, error) {
	if err := validateUserChange(change, true); err != nil {
		return UsersApplyResult{}, err
	}
	return p.mutateUsers(ctx, request, nil, change.Username, change.Level, "the camera rejected the new account",
		func(ctx context.Context, seam *authDeviceSeam, opts []device.RequestOptions) error {
			return seam.client.CreateUsers(ctx, []device.UserChange{{Username: change.Username, Password: change.Password, UserLevel: change.Level}}, opts...)
		})
}

// DeleteUser removes an account other than the one ovm signs in with.
func (p *productionMedia) DeleteUser(ctx context.Context, request DeviceRequest, username string) (UsersApplyResult, error) {
	if err := validateUserChange(UserChange{Username: username, Level: "User"}, false); err != nil {
		return UsersApplyResult{}, err
	}
	if request.Credentials != nil && request.Credentials.username == username {
		return UsersApplyResult{}, usageError("the account ovm signs in with cannot be deleted")
	}
	return p.mutateUsers(ctx, request, nil, username, "", "the camera rejected the account removal",
		func(ctx context.Context, seam *authDeviceSeam, opts []device.RequestOptions) error {
			return seam.client.DeleteUsers(ctx, []string{username}, opts...)
		})
}
