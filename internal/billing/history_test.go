package billing

import (
	"context"
	"strings"
	"testing"
)

func TestHistoryRequiresIdentityBeforeBlockedCapabilityMessage(t *testing.T) {
	_, err := New("http://billing.invalid", "noc-key").Invoke(context.Background(), ToolHistory, nil)
	if err == nil || !strings.Contains(err.Error(), "butuh identity") {
		t.Fatalf("missing identity must fail locally, err=%v", err)
	}
}
