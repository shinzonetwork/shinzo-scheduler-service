package dto

type ErrorResponse struct {
	Error string `json:"error"`
}

type StatsResponse struct {
	ActiveIndexers int `json:"active_indexers"`
	ActiveHosts    int `json:"active_hosts"`
	Subscriptions  int `json:"subscriptions"`
}

type QuoteResponse struct {
	IndexerID   string  `json:"indexer_id"`
	SubType     string  `json:"sub_type"`
	BlockFrom   int     `json:"block_from,omitempty"`
	BlockTo     int     `json:"block_to,omitempty"`
	PriceTokens float64 `json:"price_tokens"`
	Currency    string  `json:"currency"`
	ValidUntil  string  `json:"valid_until"`
}
