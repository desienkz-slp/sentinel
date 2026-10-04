package billing

import (
	"context"
	"fmt"

	"ainoc/internal/tool"
)

// ToolHistory is retained as a declared tool name so registry/adapter parity
// remains explicit. It is blocked at the adapter surface: the upstream history
// endpoint has no documented server-side pagination or safe projection.
const ToolHistory = "billing.get_history"

func (a *Adapter) getHistory(_ context.Context, args map[string]any) (tool.Output, error) {
	if firstString(args, "identity", "username", "search") == "" {
		return tool.Output{}, fmt.Errorf("billing.get_history butuh identity (username pelanggan)")
	}
	return tool.Output{}, fmt.Errorf("billing.get_history diblokir: upstream history tidak mendukung pagination dan projection server-side yang terikat")
}
