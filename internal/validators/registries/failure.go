package registries

import (
	"errors"
	"fmt"
	"net/http"
)

// FailureKind says what a failed package validation means, for a caller that
// has to act on the failure without reading its message.
type FailureKind int

const (
	// FailureOther is anything else: a reference the validator cannot read,
	// or an answer it cannot place.
	FailureOther FailureKind = iota
	// FailureNotFound: the package, or the release of it that was named, is
	// not in its registry.
	FailureNotFound
	// FailureOwnership: the release is there and does not name the server.
	FailureOwnership
	// FailureTransient: the registry gave no answer, and may give one later.
	FailureTransient
)

// failure is a validation error that carries its kind. Its message is the
// wrapped error's own.
type failure struct {
	kind FailureKind
	err  error
}

func (f *failure) Error() string { return f.err.Error() }
func (f *failure) Unwrap() error { return f.err }

// failed returns the error fmt.Errorf would, marked with what it means.
func failed(kind FailureKind, format string, args ...any) error {
	return &failure{kind: kind, err: fmt.Errorf(format, args...)}
}

// KindOf returns what a validation error means. An error that says nothing
// about it is FailureOther.
func KindOf(err error) FailureKind {
	var f *failure
	if errors.As(err, &f) {
		return f.kind
	}
	return FailureOther
}

// statusKind places an HTTP status a registry answered with that the
// validator has no more specific reading of.
func statusKind(status int) FailureKind {
	if status == http.StatusTooManyRequests || (status >= 500 && status < 600) {
		return FailureTransient
	}
	return FailureOther
}
