package golden02

import (
	"context"

	"github.com/lemon4ksan/aoni"
)

// @aoni:service
// @base_url "https://api.example.com/v1"
// @engine fast
type PaymentAPI interface {
	// @get "invoices/{invoice_id}"
	// @coalesce
	GetInvoice(ctx context.Context, invoice_id string, mods ...aoni.RequestModifier) (*InvoiceDTO, error)

	// @post "checkout"
	// @idempotent
	CreateCheckout(ctx context.Context, req CheckoutReq, mods ...aoni.RequestModifier) (*CheckoutSession, error)
}

type InvoiceDTO struct {
	ID     string  `json:"id"`
	Amount float64 `json:"amount"`
}

type CheckoutReq struct {
	Items []string `json:"items"`
}

type CheckoutSession struct {
	Token string `json:"token"`
	URL   string `json:"url"`
}
