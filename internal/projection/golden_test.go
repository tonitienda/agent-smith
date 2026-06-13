package projection

import (
	"flag"
	"time"
)

// update regenerates the golden projection file when set. Run:
//
//	go test ./internal/projection -run TestGolden -update
var update = flag.Bool("update", false, "regenerate golden projection files")

// goldenTS is a fixed timestamp so regenerated golden files are byte-stable.
var goldenTS = time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
