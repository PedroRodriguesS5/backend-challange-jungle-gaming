package database

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	PgErrCodeUniqueViolation = "23505"
	PgErrCodeCheckViolation  = "23514"
)

func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == PgErrCodeUniqueViolation
	}
	return false
}

func IsCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == PgErrCodeCheckViolation
	}
	return false
}
