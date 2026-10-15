package database

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsUniqueViolation(t *testing.T) {
	uniqueErr := &pgconn.PgError{Code: "23505"}
	otherErr := &pgconn.PgError{Code: "23503"}
	stdErr := errors.New("generic error")

	if !IsUniqueViolation(uniqueErr) {
		t.Error("expected IsUniqueViolation to be true for 23505")
	}
	if IsUniqueViolation(otherErr) {
		t.Error("expected IsUniqueViolation to be false for 23503")
	}
	if IsUniqueViolation(stdErr) {
		t.Error("expected IsUniqueViolation to be false for generic error")
	}
	if IsUniqueViolation(nil) {
		t.Error("expected IsUniqueViolation to be false for nil")
	}
}

func TestIsCheckViolation(t *testing.T) {
	checkErr := &pgconn.PgError{Code: "23514"}
	otherErr := &pgconn.PgError{Code: "23505"}

	if !IsCheckViolation(checkErr) {
		t.Error("expected IsCheckViolation to be true for 23514")
	}
	if IsCheckViolation(otherErr) {
		t.Error("expected IsCheckViolation to be false for 23505")
	}
}
