package kroger

import "context"

// orderClient provides Kroger authorization and cart writes.
type orderClient interface {
	authorizationURL(state, banner string) (string, error)
	exchange(context.Context, string) (cartToken, error)
	refresh(context.Context, string) (cartToken, error)
	add(context.Context, string, []cartItem) error
}
