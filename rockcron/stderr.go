package rockcron

import (
	"io"
	"os"
)

// errWriter is where internal panics (e.g. inside OnError) are written.
// Override in tests to capture output.
var errWriter io.Writer = os.Stderr
