package autogit

import "embed"

// CanonicalEventSchema is embedded at build time so validation never depends
// on the caller's working directory or a mutable checkout.
//
//go:embed schemas/event-v1.schema.json
var CanonicalEventSchema embed.FS
