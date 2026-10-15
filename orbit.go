// Package orbit defines the lifecycle contract shared by framework components.
package orbit

import "context"

// Component is started in dependency order and stopped in reverse order.
// Start must finish initialization before returning. Stop must be idempotent.
// A failed Start must clean up its own partially initialized resources.
// Components are single-use unless explicitly documented otherwise.
type Component interface {
	Start(context.Context) error
	Stop(context.Context) error
}
