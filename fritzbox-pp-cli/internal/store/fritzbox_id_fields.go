// Copyright 2026 Filippo Merante Caparrotta and contributors. Licensed under Apache-2.0. See LICENSE.

package store

// FRITZ!OS identifies a device by its UID and lets several devices share a
// display name. The profiler left no IDField for this resource, so the generic
// fallback list reached "name" and collapsed every duplicate-named device into
// a single row: a household with two machines called "Mac" and three called
// "iPhone" silently lost four of its thirty-one devices, with no error raised
// anywhere.
//
// The override lives in this file rather than in the generated map literal so a
// regeneration cannot drop it.
func init() {
	resourceIDFieldOverrides["hosts"] = "UID"
}
