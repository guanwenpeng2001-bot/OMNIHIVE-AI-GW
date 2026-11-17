package repository

import "strings"

// accountExtraSQL binds intermediate values inside a correlated SET subquery.
// Inlining an earlier JSONB expression at each use makes the SQL (and its plan)
// grow multiplicatively as revision, fingerprint and cleanup steps compose.
// OFFSET 0 prevents PostgreSQL from pulling up and expanding those expressions.
//
// The first stage references the UPDATE target's columns, never a second scan
// of accounts. That correlation is essential: after waiting for a concurrent
// writer, READ COMMITTED must recompute the merge from the updated target row.
// A materialized CTE (even inside SET) can retain its tuplestore across that
// recheck and lose concurrent JSONB keys. These parameterized subqueries rescan
// instead. No schema objects, extensions or migrations are needed.
type accountExtraSQL struct {
	stages []string
}

// bind accepts only repository-generated SQL and identifiers. Account values
// remain bound parameters. Each stage has exactly one row; LATERAL exposes
// earlier bindings so extra and quota columns share the same calculations.
func (s *accountExtraSQL) bind(name, expression string) string {
	stage := "account_extra_step_" + itoa(len(s.stages))
	s.stages = append(s.stages, "(SELECT "+expression+" AS "+name+" OFFSET 0) AS "+stage)
	return name
}

func (s *accountExtraSQL) selectSQL(expressions ...string) string {
	return "(SELECT " + strings.Join(expressions, ", ") + " FROM " + strings.Join(s.stages, " CROSS JOIN LATERAL ") + ")"
}
