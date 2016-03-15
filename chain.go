package turms

import (
	"golang.org/x/net/context"
)

// A Chain is a chain of Handlers. It implements a Handler.
type Chain []Handler

// Handle calls each chain element Handle function subsequently.
func (c *Chain) Handle(ctx context.Context, conn Conn, msg Message) context.Context {
	for i := range *c {
		ctx = (*c)[i].Handle(ctx, conn, msg)
	}
	return ctx
}
