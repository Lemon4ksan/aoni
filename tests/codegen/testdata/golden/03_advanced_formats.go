package golden03

import (
	"context"
	"time"

	"github.com/lemon4ksan/aoni"
)

// @aoni:service
// @base_url "https://api.fintech.com/v1"
// @type_map time.Time -> unix_s
// @type_map []string -> comma
type BillingAPI interface {
	// @get "transactions"
	GetTransactions(
		ctx context.Context,
		from time.Time, // @format unix_ms
		to time.Time, // @format layout="2006-01-02"
		statuses []string, // @format comma
		categories []string, // @format pipe
		accountIDs []int64, // @format comma
		includePending bool, // @format bool_int
		compact bool, // @format flag
		mods ...aoni.RequestModifier,
	) (*TransactionList, error)
}

// @aoni:dto casing=snake_case
type TransactionList struct {
	Total int
}
