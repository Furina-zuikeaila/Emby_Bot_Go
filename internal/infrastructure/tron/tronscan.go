package tron

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrTransferNotFound      = errors.New("trc20 transfer not found")
	ErrTransferNotConfirmed  = errors.New("trc20 transfer not confirmed")
	ErrTransferMismatch      = errors.New("trc20 transfer mismatch")
	ErrInvalidTronScanBase   = errors.New("invalid tronscan api base")
	ErrInvalidAmountDecimals = errors.New("invalid decimals")
)

type Expectation struct {
	ToAddress       string
	ContractAddress string
	Decimals        int
	ExpectedQuant   *big.Int // nil => accept any amount > 0
	RequireConfirm  bool
}

type TRC20Transfer struct {
	TxHash          string
	ToAddress       string
	ContractAddress string
	TokenSymbol     string
	Decimals        int
	Quant           *big.Int
	Confirmed       bool
}

type TronScanClient struct {
	baseURL       string
	httpClient    *http.Client
	searchLimit   int
	searchMaxPage int
}

type TronScanOptions struct {
	APIBase       string
	HTTPTimeout   time.Duration
	SearchLimit   int
	SearchMaxPage int
}

func NewTronScanClient(opts TronScanOptions) (*TronScanClient, error) {
	base := strings.TrimSpace(opts.APIBase)
	if base == "" {
		return nil, ErrInvalidTronScanBase
	}
	if _, err := url.Parse(base); err != nil {
		return nil, ErrInvalidTronScanBase
	}
	timeout := opts.HTTPTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	limit := opts.SearchLimit
	if limit <= 0 {
		limit = 50
	}
	maxPages := opts.SearchMaxPage
	if maxPages <= 0 {
		maxPages = 10
	}
	return &TronScanClient{
		baseURL: base,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		searchLimit:   limit,
		searchMaxPage: maxPages,
	}, nil
}

func (c *TronScanClient) VerifyTRC20Transfer(ctx context.Context, txHash string, expect Expectation) (TRC20Transfer, error) {
	if c == nil {
		return TRC20Transfer{}, fmt.Errorf("nil tronscan client")
	}
	if expect.Decimals < 0 {
		return TRC20Transfer{}, ErrInvalidAmountDecimals
	}
	txHash = strings.TrimSpace(txHash)
	if txHash == "" {
		return TRC20Transfer{}, ErrTransferNotFound
	}
	toAddr := strings.TrimSpace(expect.ToAddress)
	contract := strings.TrimSpace(expect.ContractAddress)
	if toAddr == "" || contract == "" {
		return TRC20Transfer{}, ErrTransferMismatch
	}

	// 主路径：优先调用 transaction-info 接口（更快、单次请求）。
	if tr, ok, err := c.tryTxInfo(ctx, txHash); err == nil && ok {
		return c.validateTransfer(tr, expect)
	}

	// 兜底：扫描 TRC20 转账列表（toAddress + contract），并匹配 transaction_id。
	return c.scanTransfers(ctx, txHash, expect)
}

func (c *TronScanClient) validateTransfer(tr TRC20Transfer, expect Expectation) (TRC20Transfer, error) {
	if tr.Quant == nil {
		return TRC20Transfer{}, ErrTransferMismatch
	}
	if !strings.EqualFold(strings.TrimSpace(tr.ToAddress), strings.TrimSpace(expect.ToAddress)) {
		return TRC20Transfer{}, ErrTransferMismatch
	}
	if !strings.EqualFold(strings.TrimSpace(tr.ContractAddress), strings.TrimSpace(expect.ContractAddress)) {
		return TRC20Transfer{}, ErrTransferMismatch
	}
	if expect.RequireConfirm && !tr.Confirmed {
		return TRC20Transfer{}, ErrTransferNotConfirmed
	}
	if expect.ExpectedQuant != nil {
		if tr.Quant.Cmp(expect.ExpectedQuant) != 0 {
			return TRC20Transfer{}, ErrTransferMismatch
		}
	} else {
		if tr.Quant.Sign() <= 0 {
			return TRC20Transfer{}, ErrTransferMismatch
		}
	}
	return tr, nil
}

func (c *TronScanClient) tryTxInfo(ctx context.Context, txHash string) (TRC20Transfer, bool, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return TRC20Transfer{}, false, ErrInvalidTronScanBase
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/transaction-info"
	q := u.Query()
	q.Set("hash", txHash)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return TRC20Transfer{}, false, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return TRC20Transfer{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return TRC20Transfer{}, false, fmt.Errorf("tronscan txinfo status=%d", resp.StatusCode)
	}

	// TronScan 的 JSON 结构可能会变动，这里用 map 做容错解析，
	// 只提取 TRC20 转账所需的最小字段集合。
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return TRC20Transfer{}, false, err
	}

	// 尝试从若干常见字段中提取 TRC20 transfer 信息。
	// 这些字段在不同版本/节点可能不同，尽量“能用就用”，失败则回退到 transfers 扫描。
	getString := func(m map[string]any, key string) string {
		if m == nil {
			return ""
		}
		v, ok := m[key]
		if !ok || v == nil {
			return ""
		}
		switch x := v.(type) {
		case string:
			return x
		default:
			return ""
		}
	}
	getBool := func(m map[string]any, key string) (bool, bool) {
		if m == nil {
			return false, false
		}
		v, ok := m[key]
		if !ok || v == nil {
			return false, false
		}
		switch x := v.(type) {
		case bool:
			return x, true
		default:
			return false, false
		}
	}

	// 1) tokenTransferInfo (object)
	if v, ok := raw["tokenTransferInfo"]; ok {
		if m, ok := v.(map[string]any); ok {
			to := getString(m, "to_address")
			contract := getString(m, "contract_address")
			symbol := getString(m, "symbol")
			quantStr := getString(m, "amount_str")
			if quantStr == "" {
				quantStr = getString(m, "quant")
			}
			quant, okQuant := new(big.Int).SetString(strings.TrimSpace(quantStr), 10)
			confirmed, _ := getBool(raw, "confirmed")
			if okQuant && to != "" && contract != "" {
				return TRC20Transfer{
					TxHash:          txHash,
					ToAddress:       to,
					ContractAddress: contract,
					TokenSymbol:     symbol,
					Quant:           quant,
					Confirmed:       confirmed,
				}, true, nil
			}
		}
	}

	return TRC20Transfer{}, false, nil
}

func (c *TronScanClient) scanTransfers(ctx context.Context, txHash string, expect Expectation) (TRC20Transfer, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return TRC20Transfer{}, ErrInvalidTronScanBase
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/api/token_trc20/transfers"

	for page := 0; page < c.searchMaxPage; page++ {
		start := page * c.searchLimit
		q := base.Query()
		q.Set("limit", fmt.Sprintf("%d", c.searchLimit))
		q.Set("start", fmt.Sprintf("%d", start))
		q.Set("contract_address", strings.TrimSpace(expect.ContractAddress))
		q.Set("toAddress", strings.TrimSpace(expect.ToAddress))
		// 尽量只看已确认交易（如果 API 支持）。
		q.Set("confirm", "true")
		base.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
		if err != nil {
			return TRC20Transfer{}, err
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return TRC20Transfer{}, err
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return TRC20Transfer{}, err
		}
		if resp.StatusCode != http.StatusOK {
			return TRC20Transfer{}, fmt.Errorf("tronscan transfers status=%d", resp.StatusCode)
		}

		var decoded transfersResp
		if err := json.Unmarshal(body, &decoded); err != nil {
			return TRC20Transfer{}, err
		}
		if len(decoded.TokenTransfers) == 0 {
			break
		}
		for _, t := range decoded.TokenTransfers {
			if !strings.EqualFold(strings.TrimSpace(t.TransactionID), txHash) {
				continue
			}
			quant, ok := new(big.Int).SetString(strings.TrimSpace(t.Quant), 10)
			if !ok {
				return TRC20Transfer{}, ErrTransferMismatch
			}
			decimals := expect.Decimals
			if t.TokenInfo.Decimals > 0 {
				decimals = t.TokenInfo.Decimals
			}
			tr := TRC20Transfer{
				TxHash:          txHash,
				ToAddress:       strings.TrimSpace(t.ToAddress),
				ContractAddress: strings.TrimSpace(t.ContractAddress),
				TokenSymbol:     strings.TrimSpace(firstNonEmpty(t.TokenInfo.Symbol, t.TokenInfo.Abbr, t.Symbol)),
				Decimals:        decimals,
				Quant:           quant,
				Confirmed:       t.Confirmed || strings.EqualFold(t.Status, "confirmed") || strings.EqualFold(t.FinalResult, "SUCCESS") || strings.EqualFold(t.ContractRet, "SUCCESS"),
			}
			return c.validateTransfer(tr, expect)
		}
	}
	return TRC20Transfer{}, ErrTransferNotFound
}

type transfersResp struct {
	TokenTransfers []transferItem `json:"token_transfers"`
}

type transferItem struct {
	TransactionID   string `json:"transaction_id"`
	ToAddress       string `json:"to_address"`
	ContractAddress string `json:"contract_address"`
	Quant           string `json:"quant"`

	Symbol      string `json:"symbol"`
	Status      string `json:"status"`
	Confirmed   bool   `json:"confirmed"`
	FinalResult string `json:"finalResult"`
	ContractRet string `json:"contractRet"`

	TokenInfo struct {
		Symbol   string `json:"symbol"`
		Abbr     string `json:"abbr"`
		Decimals int    `json:"decimals"`
	} `json:"tokenInfo"`
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}
