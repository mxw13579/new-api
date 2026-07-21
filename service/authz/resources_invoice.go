package authz

const ResourceInvoicePaymentEvidence = "invoice_payment_evidence"

var (
	InvoicePaymentEvidenceRead    = Permission{Resource: ResourceInvoicePaymentEvidence, Action: ActionRead}
	InvoicePaymentEvidenceOperate = Permission{Resource: ResourceInvoicePaymentEvidence, Action: ActionOperate}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceInvoicePaymentEvidence,
		LabelKey: "System Tasks",
		Actions: []ActionDefinition{
			{
				Action:         ActionRead,
				LabelKey:       "System Tasks",
				DescriptionKey: "System Tasks",
			},
			{
				Action:         ActionOperate,
				LabelKey:       "System Tasks",
				DescriptionKey: "System Tasks",
			},
		},
	})
}
