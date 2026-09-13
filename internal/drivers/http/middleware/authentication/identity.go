package authentication

import "context"

type identityContextKey struct{}

type Identity struct {
	UserID    string
	SessionID string
}

func withIdentity(
	ctx context.Context,
	identity Identity,
) context.Context {
	return context.WithValue(
		ctx,
		identityContextKey{},
		identity,
	)
}

func IdentityFromContext(
	ctx context.Context,
) (Identity, bool) {
	if ctx == nil {
		return Identity{}, false
	}

	identity, found := ctx.Value(
		identityContextKey{},
	).(Identity)

	return identity, found
}
