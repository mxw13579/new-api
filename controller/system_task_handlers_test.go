package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
)

func TestScheduledSystemTaskHandlersIncludeInvoiceCleanup(t *testing.T) {
	types := map[string]bool{}
	for _, handler := range scheduledSystemTaskHandlers() {
		types[handler.Type()] = true
	}
	assert.True(t, types[model.InvoiceDocumentCleanupTaskType])
}
