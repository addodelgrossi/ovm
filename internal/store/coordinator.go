package store

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"time"
)

const compensationTimeout = 5 * time.Second

// Coordinator combines metadata and secret operations using compensating
// rollbacks. It never holds the repository mutex while calling a provider.
type Coordinator struct {
	repository *Repository
	secrets    SecretProvider
	entropy    io.Reader
}

// NewCoordinator creates cross-resource transaction helpers. Remote credential
// validation must happen in the application before Put is called.
func NewCoordinator(repository *Repository, secrets SecretProvider) (*Coordinator, error) {
	if repository == nil || secrets == nil {
		return nil, ErrInvalidData
	}
	return &Coordinator{repository: repository, secrets: secrets, entropy: rand.Reader}, nil
}

// Put stores a newly referenced secret, then publishes metadata. Updating a
// credential uses a fresh reference. Metadata failure deletes the new secret;
// replacement cleanup failure restores the prior metadata when still safe.
func (coordinator *Coordinator) Put(ctx context.Context, device Device, secret Secret) (Device, error) {
	if coordinator == nil || coordinator.repository == nil || coordinator.secrets == nil {
		return Device{}, ErrInvalidData
	}
	if err := contextError(ctx); err != nil {
		return Device{}, err
	}
	if err := secret.validate(); err != nil {
		return Device{}, err
	}
	reference, err := generateOpaqueID(coordinator.entropy, "secret_", 24)
	if err != nil {
		return Device{}, ErrSecretUnavailable
	}
	if err := safeProviderError(coordinator.secrets.Put(ctx, reference, secret)); err != nil {
		return Device{}, err
	}
	// A provider may complete the write while canceling the request. Do not
	// publish metadata after cancellation, and compensate with an independent
	// bounded context so the new secret is not stranded.
	if err := contextError(ctx); err != nil {
		if cleanupErr := coordinator.cleanupDelete(ctx, reference); cleanupErr != nil {
			return Device{}, newErrorWithCause(CodeRollbackFailed, ErrRollbackFailed.Error(), cleanupErr)
		}
		return Device{}, err
	}

	device.CredentialReference = reference
	device.AuthPolicy = secret.Policy()
	saved, token, metadataErr := coordinator.repository.upsertTransactional(device)
	if metadataErr != nil {
		if token != nil {
			if rollbackErr := token.rollback(); rollbackErr != nil {
				return Device{}, rollbackFailure(rollbackErr, metadataErr)
			}
		}
		if cleanupErr := coordinator.cleanupDelete(ctx, reference); cleanupErr != nil {
			return Device{}, newErrorWithCause(CodeRollbackFailed, ErrRollbackFailed.Error(), cleanupErr)
		}
		return Device{}, newErrorWithCause(CodeTransaction, ErrTransaction.Error(), metadataErr)
	}
	if err := contextError(ctx); err != nil {
		if rollbackErr := token.rollback(); rollbackErr != nil {
			return Device{}, rollbackFailure(rollbackErr, err)
		}
		if cleanupErr := coordinator.cleanupDelete(ctx, reference); cleanupErr != nil {
			return Device{}, newErrorWithCause(CodeRollbackFailed, ErrRollbackFailed.Error(), cleanupErr)
		}
		return Device{}, err
	}

	previous, replaced := token.previousDevice(saved.ID)
	if !replaced || previous.CredentialReference == "" || previous.CredentialReference == reference {
		return saved, nil
	}
	if err := coordinator.deleteIdempotently(ctx, previous.CredentialReference); err != nil {
		if rollbackErr := token.rollback(); rollbackErr != nil {
			// Rollback may have left metadata pointing at the new reference, so
			// preserve that secret rather than turning an error into data loss.
			return Device{}, rollbackFailure(rollbackErr, err)
		}
		if cleanupErr := coordinator.cleanupDelete(ctx, reference); cleanupErr != nil {
			return Device{}, newErrorWithCause(CodeRollbackFailed, ErrRollbackFailed.Error(), cleanupErr)
		}
		return Device{}, newErrorWithCause(CodeTransaction, ErrTransaction.Error(), err)
	}
	return saved, nil
}

// Forget removes metadata first and then deletes its referenced secret. A
// missing secret is an idempotent success. Denied or unavailable deletion rolls
// metadata and the prior selection back when no concurrent metadata mutation
// has occurred; a conflict is reported rather than overwriting newer state.
func (coordinator *Coordinator) Forget(ctx context.Context, id string) error {
	if coordinator == nil || coordinator.repository == nil || coordinator.secrets == nil {
		return ErrInvalidData
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	removed, token, metadataErr := coordinator.repository.removeTransactional(id)
	if metadataErr != nil {
		if token != nil {
			if rollbackErr := token.rollback(); rollbackErr != nil {
				return rollbackFailure(rollbackErr, metadataErr)
			}
		}
		return metadataErr
	}
	if removed.CredentialReference == "" {
		return nil
	}
	if err := coordinator.deleteIdempotently(ctx, removed.CredentialReference); err != nil {
		if rollbackErr := token.rollback(); rollbackErr != nil {
			return rollbackFailure(rollbackErr, err)
		}
		return newErrorWithCause(CodeTransaction, ErrTransaction.Error(), err)
	}
	return nil
}

func (coordinator *Coordinator) deleteIdempotently(ctx context.Context, reference string) error {
	err := safeProviderError(coordinator.secrets.Delete(ctx, reference))
	if errors.Is(err, ErrSecretNotFound) {
		return nil
	}
	return err
}

func (coordinator *Coordinator) cleanupDelete(ctx context.Context, reference string) error {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), compensationTimeout)
	defer cancel()
	return coordinator.deleteIdempotently(cleanupContext, reference)
}

func safeProviderError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	switch SecretErrorKindOf(err) {
	case SecretNotFound:
		return ErrSecretNotFound
	case SecretDenied:
		return ErrSecretDenied
	case SecretUnsupported:
		return ErrSecretUnsupported
	case SecretInvalid:
		return ErrSecretInvalid
	case SecretUnavailable:
		return ErrSecretUnavailable
	default:
		return ErrSecretUnavailable
	}
}

func rollbackFailure(rollbackErr, operationErr error) error {
	if errors.Is(rollbackErr, ErrConflict) {
		return newErrorWithCause(CodeRollbackFailed, ErrRollbackFailed.Error(), errors.Join(ErrConflict, safeTransactionCause(operationErr)))
	}
	return newErrorWithCause(CodeRollbackFailed, ErrRollbackFailed.Error(), safeTransactionCause(operationErr))
}

func safeTransactionCause(err error) error {
	if err == nil {
		return ErrTransaction
	}
	var storeErr *Error
	if errors.As(err, &storeErr) {
		return storeErr
	}
	return safeProviderError(err)
}
