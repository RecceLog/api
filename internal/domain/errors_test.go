package domain_test

import (
	"errors"
	"testing"

	"github.com/RecceLog/api/internal/domain"
)

// assertFields checks that err contains a *ValidationError for each expected
// field name (in any order). Reports missing/extra fields via t.Errorf.
func assertFields(t *testing.T, err error, want []string) {
	t.Helper()
	got := make(map[string]int)
	for _, v := range collectValidationErrors(err) {
		got[v.Field]++
	}
	for _, f := range want {
		if got[f] == 0 {
			t.Errorf("missing validation error for field %q (got fields: %v)", f, got)
		}
		got[f]--
	}
	for f, n := range got {
		if n > 0 {
			t.Errorf("unexpected validation error for field %q (count=%d)", f, n)
		}
	}
}

// collectValidationErrors walks a (possibly errors.Join'd / fmt.Errorf-wrapped)
// tree and returns every *ValidationError leaf. Test-only helper that lets
// individual cases assert on the *set* of fields that failed.
func collectValidationErrors(err error) []*domain.ValidationError {
	if err == nil {
		return nil
	}
	var out []*domain.ValidationError
	if v, ok := err.(*domain.ValidationError); ok {
		out = append(out, v)
	}
	switch e := err.(type) {
	case interface{ Unwrap() []error }:
		for _, sub := range e.Unwrap() {
			out = append(out, collectValidationErrors(sub)...)
		}
	case interface{ Unwrap() error }:
		// don't recurse if we already captured this node as a ValidationError —
		// its Unwrap returns the ErrValidation sentinel which isn't a leaf.
		if len(out) == 0 {
			out = append(out, collectValidationErrors(e.Unwrap())...)
		}
	}
	return out
}

func TestValidationErrorImplementsError(t *testing.T) {
	verr := &domain.ValidationError{Field: "name", Message: "can't be empty"}

	if got, want := verr.Error(), "name: can't be empty"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(verr, domain.ErrValidation) {
		t.Error("errors.Is(verr, ErrValidation) = false, want true")
	}

	var target *domain.ValidationError
	if !errors.As(verr, &target) {
		t.Fatal("errors.As did not extract *ValidationError")
	}
	if target.Field != "name" {
		t.Errorf("Field = %q, want %q", target.Field, "name")
	}
}

func TestErrValidationDistinctFromErrNotFound(t *testing.T) {
	verr := &domain.ValidationError{Field: "f", Message: "m"}
	if errors.Is(verr, domain.ErrNotFound) {
		t.Error("ValidationError should not satisfy errors.Is(ErrNotFound)")
	}
}
