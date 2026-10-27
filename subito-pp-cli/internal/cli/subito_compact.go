// Hand-written: the listing fields --agent/--compact keep on ads search and
// ads recommended (see .printing-press-patches/listing-compact-keep.md).

package cli

// adCompactKeep is what identifies a listing and its price. The schema-aware
// compaction keeps only gravity names, and a listing's only one is "type".
var adCompactKeep = []string{"urn", "subject", "features", "geo", "dates", "urls"}
