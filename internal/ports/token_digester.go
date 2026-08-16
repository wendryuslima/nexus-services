package ports

import "context"

type TokenDigester interface {
	Digest(ctx context.Context, rawToken string) (string, error)
}
