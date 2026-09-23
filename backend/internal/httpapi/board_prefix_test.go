package httpapi

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// uniqueViolation decides whether a failed write means "someone took that prefix" or something
// worse. Get it wrong in one direction and a real database error is reported to the user as a
// name clash; wrong in the other and a clash is reported as a server error, or retried three times
// for nothing.

func TestUniqueViolationRecognisesTheDuplicateKeyError(t *testing.T) {
	err := &pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"}
	if !uniqueViolation(err) {
		t.Error("23505 is the duplicate-key code and must be recognised")
	}
}

func TestUniqueViolationSeesThroughWrapping(t *testing.T) {
	// Errors travel wrapped through the query layer; a direct type assertion would miss this.
	err := fmt.Errorf("create board: %w", &pgconn.PgError{Code: "23505"})
	if !uniqueViolation(err) {
		t.Error("a wrapped PgError must still be recognised")
	}
}

func TestUniqueViolationRejectsEverythingElse(t *testing.T) {
	cases := []error{
		nil,
		errors.New("connection refused"),
		&pgconn.PgError{Code: "23503"}, // foreign key: a different problem entirely
		&pgconn.PgError{Code: "42P01"}, // undefined table
	}
	for _, err := range cases {
		if uniqueViolation(err) {
			t.Errorf("%v must not be read as a duplicate key", err)
		}
	}
}

// Retrying forever on a constraint that will never yield turns a bad request into a hung one.
func TestPrefixAttemptsIsBounded(t *testing.T) {
	if prefixAttempts < 2 {
		t.Error("one attempt means no retry, and the race this exists for is real")
	}
	if prefixAttempts > 5 {
		t.Errorf("prefixAttempts = %d: this is a race, not a queue", prefixAttempts)
	}
}
