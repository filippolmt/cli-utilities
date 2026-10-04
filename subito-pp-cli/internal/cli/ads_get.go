// Hand-written: ads get, the full detail of one listing.
// pp:data-source live

package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"subito-pp-cli/internal/subito"

	"github.com/spf13/cobra"
)

type adGetView struct {
	ListID  string          `json:"list_id"`
	URL     string          `json:"url"`
	Detail  subito.Detail   `json:"detail"`
	Listing json.RawMessage `json:"listing,omitempty"`
}

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		ads, _, err := root.Find([]string{"ads"})
		if err == nil && ads != nil && ads.Name() == "ads" {
			addNovelCommandIfAbsent(ads, newAdsGetCmd(flags))
		}
	})
}

func newAdsGetCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get [url-or-id]",
		Short: "Full detail of one listing: title, description, price, photos, condition",
		Long: strings.Trim(`
Use this command for the full text and photos of one ad.
Do NOT use it to check scam signals; use 'ads risk' instead.

Accepts the ad URL, its urn, or the list id at the end of the URL. The
detail comes from the ad page; when the ad was seen by an earlier search,
the stored search record (seller, place, features) is included as listing.`, "\n"),
		Example: strings.Trim(`
  subito-pp-cli ads get https://www.subito.it/biciclette/bici-da-corsa-milano-663258568.htm
  subito-pp-cli ads get 663258568 --agent --select detail.description,detail.images`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true", "pp:data-source": "live", "pp:happy-args": "ref=663258568"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "ads get")
			}
			if err := rejectLocalSource(flags, "ads get"); err != nil {
				return err
			}
			if len(args) != 1 {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("pass one ad URL, urn or list id"))
			}
			ref, err := subito.ParseRef(args[0])
			if err != nil {
				return usageErr(err)
			}
			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()
			view := adGetView{ListID: ref.ListID, URL: adPageURL(ref)}
			if db, err := openStoreForRead(ctx, "subito-pp-cli"); err == nil && db != nil {
				if l, _, err := db.SubitoListing(ctx, ref.ListID); err == nil {
					view.Listing = l.Raw
					if ref.URL == "" && l.URL != "" {
						view.URL = l.URL
					}
				}
				_ = db.Close() // read-only lookup; nothing to lose on close
			}
			d, err := fetchAdPage(ctx, flags, view.URL)
			if err != nil {
				return classifyAPIError(cmd.OutOrStdout(), err, flags)
			}
			view.Detail = d
			if d.URL != "" {
				view.URL = d.URL
			}
			if !wantsHumanTable(cmd.OutOrStdout(), flags) {
				return printJSONFiltered(cmd.OutOrStdout(), view, flags)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s\n%.0f %s · %s · %d photos\n%s\n\n%s\n", cell(d.Name), d.Price, d.Currency, d.Condition, len(d.Images), view.URL, d.Description)
			return nil
		},
	}
	return cmd
}
