// Package owner carries the requesting account through a context so storage
// layers can scope per-user data (playlists, favorites, ratings, history,
// progress) without threading a parameter through every call.
//
// The owner id is the user id of the account a device belongs to. "" is the
// legacy/unowned bucket (installs without accounts). Admin may act on other
// owners' rows by id, but listings always show the caller's own data.
package owner

import "context"

type key struct{}

type Owner struct {
	ID    string
	Admin bool
	Set   bool // false when no request identity is attached (background jobs)
}

// With attaches an owner to ctx.
func With(ctx context.Context, id string, admin bool) context.Context {
	return context.WithValue(ctx, key{}, Owner{ID: id, Admin: admin, Set: true})
}

// From returns the owner on ctx (zero Owner when none).
func From(ctx context.Context) Owner {
	o, _ := ctx.Value(key{}).(Owner)
	return o
}
