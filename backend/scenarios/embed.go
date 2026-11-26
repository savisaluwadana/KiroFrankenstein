// Package scenarios embeds the versioned scenario files so binaries can serve
// them without reading the working directory.
package scenarios

import _ "embed"

// BlackFriday is the flagship scenario document (Black Friday Meltdown).
//
//go:embed black-friday.json
var BlackFriday []byte
