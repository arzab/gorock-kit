package rockbus

import "os"

// errWriter is the destination for internal rockbus error messages (e.g. OnError panics).
// Overridable in tests.
var errWriter = os.Stderr
