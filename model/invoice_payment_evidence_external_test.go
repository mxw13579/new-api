package model_test

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestInvoicePaymentSourceExternalConstruction(t *testing.T) {
	var source model.InvoicePaymentSource = model.NewTopUpInvoicePaymentSource()
	require.NotNil(t, source)
}
