# internal/repository — SQL rules

- Only this package and `migrations/` may contain SQL.
- Use parameterized queries; never interpolate values.
- Wrap multi-statement changes in a transaction.
- Prefer explicit column lists in `SELECT`.
- Scan into typed structs; no `map[string]any` in repositories.
- Errors are wrapped with `%w` and mapped to `domain` sentinels at the boundary.
- Every new query used by a hot path gets an index in the next migration and
  a note in `ARCHITECTURE.md §4`.