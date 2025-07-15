package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// TradeExecutor interface abstracts trade execution
type TradeExecutor interface {
	ExecuteTriangularArbitrage(path [3]string, amount float64) error
}

// SimulatedExecutor just logs trades, no real orders
type SimulatedExecutor struct{}

func (se *SimulatedExecutor) ExecuteTriangularArbitrage(path [3]string, amount float64) error {
	fmt.Printf("[SIMULATION] Executing triangular arbitrage: %v with %.6f amount\n", path, amount)
	return nil
}

// BinanceExecutor executes live trades via Binance REST API
type BinanceExecutor struct {
	APIKey    string
	APISecret string
	Client    *http.Client
}

func NewBinanceExecutor(apiKey, apiSecret string) *BinanceExecutor {
	return &BinanceExecutor{
		APIKey:    apiKey,
		APISecret: apiSecret,
		Client:    &http.Client{Timeout: 10 * time.Second},
	}
}

// ExecuteTriangularArbitrage executes all three trades sequentially (simplified IOC orders)
func (be *BinanceExecutor) ExecuteTriangularArbitrage(path [3]string, amount float64) error {
	fmt.Printf("[LIVE] Executing triangular arbitrage: %v with %.6f amount\n", path, amount)

	// For simplicity, divide amount equally per trade; real logic should calculate proper sizes and order types
	for _, symbol := range path {
		err := be.placeOrder(symbol, amount)
		if err != nil {
			return fmt.Errorf("error placing order for %s: %v", symbol, err)
		}
	}

	fmt.Println("[LIVE] Arbitrage executed successfully")
	return nil
}

func (be *BinanceExecutor) placeOrder(symbol string, quantity float64) error {
	// Place a MARKET order (for simplicity) - you may want LIMIT or IOC orders in real bot

	endpoint := "https://api.binance.com/api/v3/order"

	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("side", "BUY") // TODO: determine BUY or SELL based on trade direction
	params.Set("type", "MARKET")
	params.Set("quantity", strconv.FormatFloat(quantity, 'f', 6, 64))
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))

	query := params.Encode()
	signature := be.sign(query)
	query += "&signature=" + signature

	req, err := http.NewRequest("POST", endpoint+"?"+query, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-MBX-APIKEY", be.APIKey)

	resp, err := be.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("binance order error: %s", string(body))
	}

	fmt.Printf("Order placed for %s: %s\n", symbol, string(body))
	return nil
}

func (be *BinanceExecutor) sign(data string) string {
	mac := hmac.New(sha256.New, []byte(be.APISecret))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}
