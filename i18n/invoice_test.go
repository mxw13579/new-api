package i18n

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvoiceIssuanceConflictTranslations(t *testing.T) {
	require.NoError(t, Init())
	tests := map[string]string{
		LangEn:   "The invoice number or issuance details conflict with an existing invoice",
		LangZhCN: "发票号码或开票信息与已有发票冲突",
		LangZhTW: "發票號碼或開票資訊與既有發票衝突",
	}
	for lang, expected := range tests {
		assert.Equal(t, expected, Translate(lang, MsgInvoiceIssuanceConflict))
	}
}
