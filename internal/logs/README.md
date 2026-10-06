# `internal/logs`

Zap logger factory for the composition root. `app` calls `New` and passes `*zap.Logger` into constructors. Other packages do not import `logs`.

`New` accepts an empty level, `debug`, `production`, or a zap level name (`info`, `warn`, `error`).
