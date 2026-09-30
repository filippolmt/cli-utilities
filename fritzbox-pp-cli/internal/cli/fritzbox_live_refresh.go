// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

// These hand-authored commands always read the router directly. Leaving them
// in the generated cache-refresh registry performs a redundant sync before the
// live request whenever the local cache is stale.
func init() {
	for _, path := range []string{
		"fritzbox-pp-cli dect list",
		"fritzbox-pp-cli hosts get",
		"fritzbox-pp-cli hosts list",
		"fritzbox-pp-cli tam list",
	} {
		delete(readCommandResources, path)
	}
}
