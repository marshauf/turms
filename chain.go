package turms

import (
	"golang.org/x/net/context"
)

// A Chain is a chain of Handlers. It implements a Handler.
// Chains can be nested.
type Chain []Handler

// Handle calls each chain element Handle function subsequently.
func (c *Chain) Handle(ctx context.Context, conn Conn, msg Message) context.Context {
	chainCtx := ctx
	for i := range *c {
		chainCtx = (*c)[i].Handle(chainCtx, conn, msg)
		if chainCtx == nil {
			chainCtx = ctx
		}
		select {
		case <-chainCtx.Done():
			return ctx
		default:
		}
	}
	return ctx
}
