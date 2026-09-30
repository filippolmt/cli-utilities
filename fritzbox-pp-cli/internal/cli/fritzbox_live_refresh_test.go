// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import "testing"

func TestLiveCommandsDoNotAutoRefreshCache(t *testing.T) {
	for _, path := range []string{
		"fritzbox-pp-cli dect list",
		"fritzbox-pp-cli hosts get",
		"fritzbox-pp-cli hosts list",
		"fritzbox-pp-cli tam list",
	} {
		if _, ok := readCommandResources[path]; ok {
			t.Errorf("%s still triggers a redundant cache refresh", path)
		}
	}
}
