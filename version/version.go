// Package version identifies the running application build. CI sets the
// values at build time:
//
//	go build -ldflags "-X goexpenses/version.Number=58 -X goexpenses/version.Commit=8e503d1"
//
// Local builds without these flags report a development version.
package version

import (
	"strconv"
	"time"
)

var (
	// Number is the CI build number; it increases with every build.
	Number = ""
	// Commit is the short Git commit hash the binary was built from.
	Commit = ""
)

// devAssetTag changes on every start of a development build so browsers
// never reuse stale static files while developing.
var devAssetTag = "dev-" + strconv.FormatInt(time.Now().Unix(), 10)

// Label is the human-readable version, for example "v58 · 8e503d1".
func Label() string {
	if Number == "" {
		return "dev"
	}
	if Commit == "" {
		return "v" + Number
	}
	return "v" + Number + " · " + Commit
}

// AssetTag is appended to static file URLs so every deployment invalidates
// cached CSS and JavaScript.
func AssetTag() string {
	if Number == "" {
		return devAssetTag
	}
	if Commit == "" {
		return Number
	}
	return Number + "-" + Commit
}
