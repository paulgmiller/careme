package kroger

import "context"

// orderClient provides Kroger authorization and cart writes.
type orderClient interface {
	AuthorizationURL(state, banner string) (string, error)
	Exchange(context.Context, string) (CartToken, error)
	Refresh(context.Context, string) (CartToken, error)
	Add(context.Context, string, []CartItem) error
}
