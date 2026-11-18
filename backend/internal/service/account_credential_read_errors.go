package service

import (
	"database/sql"
	"errors"

	dbent "github.com/Wei-Shaw/sub2api/ent"
)

// Only a single error chain ending in a known reference error permits recovery.
// Joined errors and reference errors with an infrastructure cause fail closed.
// Return the canonical error, never wrapper text or credential-bearing metadata.
func accountCredentialReferenceError(err error) error {
	cause := accountReadErrorCause(err)
	for _, referenceErr := range []error{ErrCredentialSecretNotFound, ErrCredentialSecretInvalid, ErrCredentialSecretRevoked, ErrCredentialSecretConflict} {
		if errors.Is(cause, referenceErr) {
			return referenceErr
		}
	}
	return nil
}

func accountReadErrorCause(err error) error {
	for err != nil {
		if _, joined := err.(interface{ Unwrap() []error }); joined {
			return nil
		}
		cause := errors.Unwrap(err)
		if cause == nil {
			return err
		}
		err = cause
	}
	return nil
}

func accountReadMissing(err error) bool {
	if !errors.Is(err, ErrAccountNotFound) {
		return false
	}
	// Repository translations carry Ent/SQL's no-row cause. Do not hide an
	// unrelated infrastructure cause merely because the outer error is not-found.
	cause := accountReadErrorCause(err)
	return errors.Is(cause, ErrAccountNotFound) || errors.Is(cause, sql.ErrNoRows) || dbent.IsNotFound(cause)
}
