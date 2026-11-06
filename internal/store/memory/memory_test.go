package memory_test

import (
	"testing"

	"github.com/rojinebrahimi/langdeck/internal/store"
	"github.com/rojinebrahimi/langdeck/internal/store/memory"
	"github.com/rojinebrahimi/langdeck/internal/store/storetest"
)

func TestContract(t *testing.T) {
	storetest.Run(t, func(*testing.T) store.Store { return memory.New() })
}
