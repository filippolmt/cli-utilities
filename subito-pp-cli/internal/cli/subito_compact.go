// Hand-written: the fields --agent/--compact keep on ads search, ads
// recommended and categories filters (see .printing-press-patches/listing-compact-keep.md).

package cli

// adCompactKeep is what identifies a listing and its price. The schema-aware
// compaction keeps only gravity names, and a listing's only one is "type".
var adCompactKeep = []string{"urn", "subject", "features", "geo", "dates", "urls"}

// filterCompactKeep is what a search filter needs to be used: its label, its
// query-string key and where its values come from. Its only gravity name is
// "type" too.
var filterCompactKeep = []string{"uri", "label", "query_string", "datasource"}
