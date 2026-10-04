// Package testassert provides checked type assertions for test fixtures.
package testassert

import "testing"

// Type fails the test at the caller when value does not have the required type.
func Type[Value any](test testing.TB, value any) Value {
	test.Helper()
	converted, ok := value.(Value)
	if !ok {
		test.Fatalf("expected %T, got %T", *new(Value), value)
	}
	return converted
}
