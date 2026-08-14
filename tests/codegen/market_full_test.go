// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package codegen_test

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lemon4ksan/aoni/internal/codegen/analysis"
	"github.com/lemon4ksan/aoni/internal/codegen/emitter"
	"github.com/lemon4ksan/aoni/internal/codegen/optimizer"
	parserpkg "github.com/lemon4ksan/aoni/internal/codegen/parser"
)

func TestFullSteamMarketGeneration(t *testing.T) {
	src := `
package market

import (
	"context"
	"io"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/codec/values"
	"github.com/lemon4ksan/g-man/pkg/steam/id"
)

type CurrencyCode int

// @aoni:service
// @base_url "https://steamcommunity.com/"
// @engine fast
// @header "X-Requested-With: XMLHttpRequest"
// @header "X-Prototype-Version: 1.7"
type SteamMarket interface {
	// @post "market/sellitem"
	// @form
	// @header "Referer: https://steamcommunity.com/profiles/{steam_id}/inventory?modal=1&market=1"
	// @check "success == true"
	CreateSellOrder(ctx context.Context, opts CreateSellOrderOptions, steam_id id.ID, mods ...aoni.RequestModifier) (*CreateSellOrderResponse, error)

	// @post "market/createbuyorder"
	// @form
	// @header "Referer: https://steamcommunity.com/market/listings/{app_id}/{market_hash_name:path_escape}"
	// @check "success == true"
	CreateBuyOrder(ctx context.Context, app_id uint32, market_hash_name string, opts CreateBuyOrderRequest, mods ...aoni.RequestModifier) (*CreateBuyOrderResponse, error)

	// @post "market/cancelbuyorder"
	// @form
	// @check "success == true"
	CancelBuyOrder(ctx context.Context, buy_orderid uint64, mods ...aoni.RequestModifier) error

	// @post "market/removelisting/{listing_id}"
	// @form
	// @check "success == true"
	CancelSellOrder(ctx context.Context, listing_id uint64, mods ...aoni.RequestModifier) error

	// @get "market/search/render"
	// @header "Referer: https://steamcommunity.com/market/search?appid={app_id}"
	Search(ctx context.Context, app_id uint32, opts SearchOptions, mods ...aoni.RequestModifier) (*SearchResponse, error)

	// @get "market/priceoverview"
	GetPriceOverview(ctx context.Context, app_id uint32, currency CurrencyCode, market_hash_name string, mods ...aoni.RequestModifier) (*PriceOverviewResponse, error)

	// @get "market/itemordershistogram"
	// @header "Referer: https://steamcommunity.com/market/listings/{app_id}/{market_hash_name:path_escape}"
	// @check "success == 1"
	GetItemOrdersHistogram(ctx context.Context, app_id uint32, market_hash_name string, item_nameid uint64, country, language string, currency CurrencyCode, two_factor int, mods ...aoni.RequestModifier) (*ItemOrdersHistogramResponse, error)

	// @get "market/mylistings"
	GetMyListings(ctx context.Context, start, count int, norender int, mods ...aoni.RequestModifier) (*MyListingsResponse, error)

	// @get "market/pricehistory"
	// @header "Referer: https://steamcommunity.com/market/listings/{app_id}/{market_hash_name:path_escape}"
	// @check "success == true"
	GetPriceHistory(ctx context.Context, app_id uint32, market_hash_name string, mods ...aoni.RequestModifier) (*PriceHistoryResponse, error)

	// @get "ajaxgetgoovalue"
	// @check "success == 1"
	GetGemValue(ctx context.Context, app_id uint32, contextid int64, assetid uint64, mods ...aoni.RequestModifier) (*GemValueResponse, error)

	// @post "ajaxgrindintogoo"
	// @form
	// @check "success == 1"
	TurnItemIntoGems(ctx context.Context, app_id uint32, contextid int64, assetid uint64, goo_value_expected int, mods ...aoni.RequestModifier) (*GrindGooResponse, error)

	// @post "ajaxunpackbooster"
	// @form
	// @check "success == 1"
	OpenBoosterPack(ctx context.Context, app_id uint32, communityitemid uint64, mods ...aoni.RequestModifier) (*UnpackBoosterResponse, error)

	// @post "tradingcards/ajaxcreatebooster"
	// @form
	// @check "purchase_eresult == 1"
	CreateBoosterPack(ctx context.Context, app_id uint32, series int, tradability_preference int, mods ...aoni.RequestModifier) (*CreateBoosterResponse, error)

	// @post "gifts/{gift_id}/validateunpack"
	// @form
	// @check "success == 1"
	GetGiftDetails(ctx context.Context, gift_id uint64, mods ...aoni.RequestModifier) (*GiftDetailsResponse, error)

	// @post "gifts/{gift_id}/unpack"
	// @form
	// @check "success == 1"
	RedeemGift(ctx context.Context, gift_id uint64, mods ...aoni.RequestModifier) error

	// @post "ajaxexchangegoo"
	// @form
	// @check "success == 1"
	GemExchange(ctx context.Context, app_id uint32, assetid uint64, goo_denomination_in, goo_amount_in int, goo_denomination_out, goo_amount_out_expected int, mods ...aoni.RequestModifier) error

	// @get "market"
	GetMarketPage(ctx context.Context, mods ...aoni.RequestModifier) (io.ReadCloser, error)
}

// @aoni:dto casing=snake_case omitempty=true
type Action struct {
	Link string
	Name string
}

// @aoni:dto casing=snake_case omitempty=true
type Description struct {
	Type  string
	Value string
	Color string
	Label string
}

// @aoni:dto casing=snake_case omitempty=true
type Asset struct {
	AppID           int
	ContextID       values.Int64String
	ID              values.Uint64String
	ClassID         values.Uint64String
	InstanceID      values.Uint64String
	Amount          values.Int64String
	BackgroundColor string
	IconURL         string
	IconURLLarge    string
	Descriptions    []Description
	Tradable        values.BoolInt
	Actions         []Action
	Name            string
	NameColor       string
	Type            string
	MarketName      string
	MarketHashName  string
	Commodity       values.BoolInt
	Marketable      values.BoolInt
}

// @aoni:dto casing=snake_case
type CreateSellOrderOptions struct {
	AppID     uint32
	ContextID int64
	AssetID   uint64
	Price     int
	Amount    int
}

// @aoni:dto casing=snake_case
type CreateSellOrderResponse struct {
	Success                 bool
	RequiresConfirmation    int
	NeedsMobileConfirmation bool
	NeedsEmailConfirmation  bool
	EmailDomain             string
}

// @aoni:dto casing=snake_case
type CreateBuyOrderRequest struct {
	Currency      CurrencyCode
	PriceTotal    string
	Quantity      int
	BillingState  string
	SaveMyAddress string
}

// @aoni:dto casing=snake_case
type CreateBuyOrderResponse struct {
	Success    bool
	BuyOrderID uint64
}

// @aoni:dto casing=snake_case
type ItemOrdersHistogramResponse struct {
	Success          int
	SellOrderTable   string
	SellOrderSummary string
	BuyOrderTable    string
	BuyOrderSummary  string
	HighestBuyOrder  values.Float64String
	LowestSellOrder  values.Float64String
	BuyOrderGraph    []GraphPoint
	SellOrderGraph   []GraphPoint
	GraphMaxY        float64
	GraphMinX        float64
	GraphMaxX        float64
	PricePrefix      string
	PriceSuffix      string
}

// @aoni:tuple
type GraphPoint struct {
	Price       float64
	Volume      int64
	Description string
}

// @aoni:dto casing=snake_case
type PriceOverviewResponse struct {
	Success     bool
	LowestPrice string
	Volume      string
	MedianPrice string
}

// @aoni:dto casing=snake_case
type MyListingsResponse struct {
	Success           bool
	PageSize          int
	TotalCount        int
	Assets            map[string]map[string]map[string]Asset
	Start             int
	NumActiveListings int
	Listings          []ListingResponse
	ListingsOnHold    []ListingResponse
	ListingsToConfirm []ListingResponse
	BuyOrders         []BuyOrderResponse
}

// @aoni:dto casing=snake_case
type ListingResponse struct {
	ListingID           string
	TimeCreated         int64
	Asset               Asset
	SteamIDLister       string
	Price               int
	OriginalPrice       int
	Fee                 int
	CurrencyID          string
	PublisherFeePercent string
	PublisherFeeApp     int
}

// @aoni:dto casing=snake_case
type BuyOrderResponse struct {
	AppID             int
	HashName          string
	WalletCurrency    int
	Price             string
	Quantity          string
	QuantityRemaining string
	BuyOrderID        string
	Description       Asset
}

// @aoni:dto casing=snake_case
type SearchOptions struct {
	Query              string
	Start              int
	Count              int
	SearchDescriptions bool
	SortColumn         string
	SortDir            string
}

// @aoni:dto casing=snake_case
type SearchResponse struct {
	Success    bool
	Start      int
	Pagesize   int
	TotalCount int
	Results    []SearchResultResponse
}

// @aoni:dto casing=snake_case
type SearchResultResponse struct {
	Name             string
	HashName         string
	SellListings     int
	SellPrice        int
	SellPriceText    string
	AppIcon          string
	AppName          string
	AssetDescription Asset
	SalePriceText    string
}

// @aoni:dto casing=snake_case
type PriceHistoryResponse struct {
	Success     bool
	PricePrefix string
	PriceSuffix string
	Prices      []PriceHistoryPoint
}

// @aoni:tuple
type PriceHistoryPoint struct {
	TimeStr string
	Price   float64
	Volume  string
}

// @aoni:dto casing=snake_case
type GemValueResponse struct {
	Success  int
	Message  string
	GooValue values.Int64String
	StrTitle string
}

// @aoni:dto casing=snake_case
type GrindGooResponse struct {
	Success int
	Message string
	// @field "goo_value_received "
	GooValueReceived values.Int64String
	GooValueTotal    values.Int64String
}

// @aoni:dto casing=snake_case
type UnpackBoosterResponse struct {
	Success int
	Message string
	RgItems []any
}

// @aoni:dto casing=snake_case
type CreateBoosterResponse struct {
	PurchaseEResult     int
	GooAmount           values.Int64String
	TradableGooAmount   values.Int64String
	UntradableGooAmount values.Int64String
	PurchaseResult      any
}

// @aoni:dto casing=snake_case
type GiftDetailsResponse struct {
	Success   int
	Message   string
	PackageID values.Int64String
	GiftName  string
	Owned     bool
}
`

	p := parserpkg.NewParser()
	root, err := p.ParseSource("market_full.go", []byte(src))
	require.NoError(t, err)

	an := analysis.NewAnalyzer()
	diags := an.Analyze(root)
	require.False(t, analysis.HasErrors(diags), "Diagnostics: %v", diags)

	opt := optimizer.NewOptimizer()
	opt.Optimize(root)

	em := emitter.NewEmitter()
	code, err := em.Emit(root)
	if err != nil {
		t.Fatalf("Emit error: %v\nSource:\n%s", err, string(code))
	}
	require.NoError(t, err)
	require.NotEmpty(t, code)

	// Validate that the generated Go source parses without any syntax errors
	fset := token.NewFileSet()
	_, parseErr := parser.ParseFile(fset, "market_full.gen.go", code, parser.AllErrors)
	if parseErr != nil {
		t.Fatalf("Generated code syntax error: %v\nCode:\n%s", parseErr, string(code))
	}
	require.NoError(t, parseErr)

	// Verify key generated methods and features
	codeStr := string(code)
	checkList := []string{
		"func (c *steamMarketClient) CreateSellOrder",
		"func (c *steamMarketClient) CreateBuyOrder",
		"func (c *steamMarketClient) CancelBuyOrder",
		"func (c *steamMarketClient) CancelSellOrder",
		"func (c *steamMarketClient) Search",
		"func (c *steamMarketClient) GetPriceOverview",
		"func (c *steamMarketClient) GetItemOrdersHistogram",
		"func (c *steamMarketClient) GetMyListings",
		"func (c *steamMarketClient) GetPriceHistory",
		"func (c *steamMarketClient) GetGemValue",
		"func (c *steamMarketClient) TurnItemIntoGems",
		"func (c *steamMarketClient) OpenBoosterPack",
		"func (c *steamMarketClient) CreateBoosterPack",
		"func (c *steamMarketClient) GetGiftDetails",
		"func (c *steamMarketClient) RedeemGift",
		"func (c *steamMarketClient) GemExchange",
		"func (c *steamMarketClient) GetMarketPage",
		"resp.PurchaseEResult != 1",
		"resp.Success != 1",
		"!resp.Success",
		`json:"goo_value_received ,omitempty"`,
		"func (t *GraphPoint) UnmarshalJSON(data []byte) error",
		"func (t *PriceHistoryPoint) UnmarshalJSON(data []byte) error",
		"allMods := stackMods[:0]",
		"formBytes := opts.AppendFormData(formBuf[:0])",
		"qBytes = opts.AppendQuery(qBytes)",
		"allMods = append(allMods, mod.WithBodyBytes(formBytes))",
	}

	for _, chk := range checkList {
		if !strings.Contains(codeStr, chk) {
			t.Fatalf("Missing expected substring %q in generated code:\n%s", chk, codeStr)
		}
	}
}
