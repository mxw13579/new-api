package authz

const ResourceInvoicePaymentEvidence = "invoice_payment_evidence"

const (
	ResourceInvoice = "invoice"

	ActionInvoiceReview         = "review"
	ActionInvoiceDocumentUpload = "document.upload"
	ActionInvoiceSettings       = "settings"
	ActionInvoiceSensitiveRead  = "sensitive.read"
)

var (
	InvoicePaymentEvidenceRead    = Permission{Resource: ResourceInvoicePaymentEvidence, Action: ActionRead}
	InvoicePaymentEvidenceOperate = Permission{Resource: ResourceInvoicePaymentEvidence, Action: ActionOperate}
	InvoiceReview                 = Permission{Resource: ResourceInvoice, Action: ActionInvoiceReview}
	InvoiceDocumentUpload         = Permission{Resource: ResourceInvoice, Action: ActionInvoiceDocumentUpload}
	InvoiceSettings               = Permission{Resource: ResourceInvoice, Action: ActionInvoiceSettings}
	InvoiceSensitiveRead          = Permission{Resource: ResourceInvoice, Action: ActionInvoiceSensitiveRead}
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
	RegisterResource(ResourceDefinition{
		Resource: ResourceInvoice,
		LabelKey: "Invoice Management",
		Actions: []ActionDefinition{
			{Action: ActionInvoiceReview, LabelKey: "Review invoices", DescriptionKey: "Review and decide invoice applications.", DefaultRoles: []string{BuiltInRoleAdmin}},
			{Action: ActionInvoiceDocumentUpload, LabelKey: "Upload invoice documents", DescriptionKey: "Upload and replace issued invoice documents.", DefaultRoles: []string{BuiltInRoleAdmin}},
			{Action: ActionInvoiceSettings, LabelKey: "Manage invoice settings", DescriptionKey: "View and update invoice policy settings.", DefaultRoles: []string{BuiltInRoleAdmin}},
			{Action: ActionInvoiceSensitiveRead, LabelKey: "View sensitive invoice data", DescriptionKey: "View unmasked invoice profile tax data."},
		},
	})
}
