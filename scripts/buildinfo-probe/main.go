// Command buildinfo-probe prints only the build version string. It exists so
// build-stamp tests can verify the ldflags target wires through to
// buildinfo.Get(); it is not a shipped fleet binary.
package main

import (
	"fmt"

	"github.com/rsned/spacemolt/pkg/buildinfo"
)

func main() {
	fmt.Println(buildinfo.Get().Version)
}
