# launchd

Go package (`package launchd`) that renders parameterised macOS launchd agent
definitions from a `Job` spec via the embedded `job.plist.tmpl`, and writes them
to a LaunchAgents directory idempotently. The per-vault `Job` specs are built in
`install` (`VaultJobs`); `install` also bootstraps them via `launchctl`.

See ../ARCHITECTURE.md for how this fits the install flow.
