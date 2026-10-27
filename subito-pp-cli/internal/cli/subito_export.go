// Hand-written: what `export ads` reads (see
// .printing-press-patches/export-ads-path.md).

package cli

import (
	"fmt"
	"io"
)

// exportAdsDefaultLimit bounds `export ads` without --limit: /search/items
// pages through every listing on Subito.
const exportAdsDefaultLimit = 100

// The generator took the list path of ads from detail-recommended, which needs
// one listing's urn, and gave it no pagination. sync already reads /search/items
// with offset paging over start/lim.
func init() {
	resourceReadPaths["ads"] = "/search/items"
	resourceReadConfigs["ads"] = resourceReadConfig{
		responsePath:   "ads",
		paginationType: "offset",
		cursorParam:    "start",
		limitParam:     "lim",
		pageSize:       100,
	}
}

// exportListParams adds the query defaults sync sends for resource (t=s,
// sort=datedesc for ads) to an export page request, without overriding paging.
func exportListParams(resource string, params map[string]string) map[string]string {
	for k, v := range syncResourceParamDefaults(resource) {
		if params == nil {
			params = map[string]string{}
		}
		if _, set := params[k]; !set {
			params[k] = v
		}
	}
	return params
}

// exportLimit returns the record cap for an export: --limit when set, and
// exportAdsDefaultLimit for ads without one, saying so on hint.
func exportLimit(resource string, limit int, hint io.Writer) int {
	if resource != "ads" || limit > 0 {
		return limit
	}
	fmt.Fprintf(hint, "hint: export ads stops at the newest %d listings; pass --limit for more\n", exportAdsDefaultLimit)
	return exportAdsDefaultLimit
}
