package mdpp

// Version is the Markdown++ engine version.
const Version = "0.1.10"

// SpecVersion is the Markdown++ format version implemented by this package.
const SpecVersion = "0.1"

// BuildCommit and BuildTime may be injected by release builds with -ldflags.
var (
	BuildCommit = "dev"
	BuildTime   = "dev"
)
