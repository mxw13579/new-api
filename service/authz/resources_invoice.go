package authz

const ResourceInvoicePaymentEvidence = "invoice_payment_evidence"

const (
	// ResourceInvoice scopes authorization decisions for personal-invoice administration.
	ResourceInvoice = "invoice"

	// ActionInvoiceReview permits reviewing and deciding invoice applications.
	ActionInvoiceReview = "review"
	// ActionInvoiceDocumentUpload permits uploading and replacing invoice documents.
	ActionInvoiceDocumentUpload = "document.upload"
	// ActionInvoiceSettings permits reading and updating invoice policy settings.
	ActionInvoiceSettings = "settings"
	// ActionInvoiceSensitiveRead permits viewing unmasked invoice tax identity data.
	ActionInvoiceSensitiveRead = "sensitive.read"
)

var (
	InvoicePaymentEvidenceRead    = Permission{Resource: ResourceInvoicePaymentEvidence, Action: ActionRead}
	InvoicePaymentEvidenceOperate = Permission{Resource: ResourceInvoicePaymentEvidence, Action: ActionOperate}
	// InvoiceReview is the permission required for invoice review workflows.
	InvoiceReview = Permission{Resource: ResourceInvoice, Action: ActionInvoiceReview}
	// InvoiceDocumentUpload is the permission required to upload or replace invoice PDFs.
	InvoiceDocumentUpload = Permission{Resource: ResourceInvoice, Action: ActionInvoiceDocumentUpload}
	// InvoiceSettings is the permission required to manage invoice policy options.
	InvoiceSettings = Permission{Resource: ResourceInvoice, Action: ActionInvoiceSettings}
	// InvoiceSensitiveRead is the permission required to reveal invoice tax identity snapshots.
	InvoiceSensitiveRead = Permission{Resource: ResourceInvoice, Action: ActionInvoiceSensitiveRead}
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
