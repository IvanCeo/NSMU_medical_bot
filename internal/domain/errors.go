package domain

import "errors"

var (
	ErrInvalidConfig        = errors.New("invalid configuration")
	ErrInvalidUser          = errors.New("invalid user")
	ErrStorage              = errors.New("storage failure")
	ErrTelegramUnavailable  = errors.New("telegram unavailable")
	ErrTelegramUnauthorized = errors.New("telegram unauthorized")
	ErrTelegramConflict     = errors.New("telegram polling conflict")
)
