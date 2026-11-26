# Neutral Executor API migration note

The neutral `database.Executor` adds two explicit write-returning methods:

- `QueryRowWrite(ctx, sql, args...)` for a write that returns one row.
- `QueryWrite(ctx, sql, args...)` for a write that returns rows.

Migrate neutral repository calls that used `QueryRow` or `Query` with `INSERT`, `UPDATE`, or `DELETE ... RETURNING` to these methods. They run the same UOW write guard as `Exec`, so ReadOnly roots and ReadOnly Required child views are rejected before SQL reaches the backend.

`Query` and `QueryRow` remain read methods by caller contract. The neutral API does not parse or sandbox arbitrary SQL; a caller can misuse a read method with a mutating statement. PostgreSQL can additionally enforce read-only transactions. SQLite cannot enforce that engine guarantee per operation, so API discipline and the explicit write methods are the available UOW guard. Do not claim that the method split classifies arbitrary SQL.

Adapter and mock implementations of `database.Executor` must implement both methods. For a new adapter, the write-returning methods must share the guarded row/cursor lifecycle of their read counterparts while checking write intent before dispatch.
