package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PortConfig は複数ノードのポート設定を管理する
type PortConfig struct {
	P2PPort     uint16
	JSONRPCPort uint16
}

const fixedJSONRPCPort uint16 = 59988

// RPCClient は JSON-RPC クライアント
type RPCClient struct {
	Endpoint string
	HTTP     *http.Client
	nextID   int64
}

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
	ID      int64       `json:"id"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   interface{}     `json:"error,omitempty"`
	ID      int64           `json:"id"`
}

// LoadPortConfig はサーバーの .env から P2P と JSON-RPC ポートを読み込む
func LoadPortConfig(envPath string) (*PortConfig, error) {
	data, err := os.ReadFile(envPath)
	if err != nil {
		return nil, err
	}

	config := &PortConfig{
		P2PPort:     8333,
		JSONRPCPort: 59988,
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "P2P_PORT":
			var p uint16
			fmt.Sscanf(value, "%d", &p)
			if p > 0 {
				config.P2PPort = p
			}
		case "JSONRPC_PORT":
			var p uint16
			fmt.Sscanf(value, "%d", &p)
			if p > 0 {
				config.JSONRPCPort = p
			}
		}
	}

	return config, nil
}

// LoadPortConfigFromDefault は デフォルトパスから ポート設定を読み込む
func LoadPortConfigFromDefault() *PortConfig {
	defaultPaths := []string{
		"../.env",
		"../../server/.env",
		".env",
	}

	for _, path := range defaultPaths {
		if _, err := os.Stat(path); err == nil {
			if config, err := LoadPortConfig(path); err == nil {
				return config
			}
		}
	}

	return &PortConfig{
		P2PPort:     8333,
		JSONRPCPort: 59988,
	}
}

// NewRPCClient は JSON-RPC クライアントを作成する
func NewRPCClient(host string, port uint16, timeout time.Duration) *RPCClient {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if host == "" {
		host = "localhost"
	}
	if port != fixedJSONRPCPort {
		port = fixedJSONRPCPort
	}

	endpoint := fmt.Sprintf("https://%s:%d/rpc", host, port)

	// TLS 検証スキップ（自己署証明書対応）
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}
	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	return &RPCClient{
		Endpoint: endpoint,
		HTTP: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
		nextID: 1,
	}
}

// Call は JSON-RPC を呼び出す
func (c *RPCClient) Call(method string, params interface{}, result interface{}) error {
	if c == nil {
		return errors.New("client is nil")
	}

	request := rpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      c.nextID,
	}
	c.nextID++

	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	response, err := c.HTTP.Post(c.Endpoint, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("post rpc: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return fmt.Errorf("decode rpc response: %w", err)
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("rpc error: %v", rpcResp.Error)
	}
	if result == nil {
		return nil
	}
	if len(rpcResp.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(rpcResp.Result, result); err != nil {
		return fmt.Errorf("decode result: %w", err)
	}
	return nil
}

// Status はノード状態を取得する
func (c *RPCClient) Status() (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.Call("brockchain_status", map[string]interface{}{}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetToken は token を検索する
func (c *RPCClient) GetToken(query string) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.Call("brockchain_getToken", map[string]interface{}{"query": query}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetUser は user を検索する
func (c *RPCClient) GetUser(query string) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.Call("brockchain_getUser", map[string]interface{}{"query": query}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetPool は pool を検索する
func (c *RPCClient) GetPool(query string) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := c.Call("brockchain_getPool", map[string]interface{}{"query": query}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// SubmitTransaction は tx を送信する
func (c *RPCClient) SubmitTransaction(tx map[string]interface{}) (map[string]interface{}, error) {
	var result map[string]interface{}
	params := map[string]interface{}{"tx": tx}
	if err := c.Call("brockchain_submitTransaction", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// SubmitBlock は block を送信する
func (c *RPCClient) SubmitBlock(block map[string]interface{}) (map[string]interface{}, error) {
	var result map[string]interface{}
	params := map[string]interface{}{"block": block}
	if err := c.Call("brockchain_submitBlock", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ReadJSON はファイルから JSON を読み込む
func ReadJSON(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// MustJSON は JSON を整形して返す
func MustJSON(v interface{}) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}

// DiscoverNodesDNS は DNS TXT レコードからノードを発見する
// dnsSeedDomain 例: "_nodes.seed.shudo-physics.com"
// 戻り値: "host1:port1,host2:port2" 形式の文字列のリスト
func DiscoverNodesDNS(dnsSeedDomain string) ([]string, error) {
	if dnsSeedDomain == "" {
		return nil, errors.New("dns seed domain is empty")
	}

	// TXT レコードを検索
	txtRecords, err := net.LookupTXT(dnsSeedDomain)
	if err != nil {
		return nil, fmt.Errorf("dns lookup failed: %w", err)
	}

	if len(txtRecords) == 0 {
		return nil, errors.New("no dns txt records found")
	}

	var nodes []string
	for _, record := range txtRecords {
		// 各 TXT レコードはカンマ区切りのノードアドレスを含む
		// 例: "192.168.1.1:8333,192.168.1.2:8333"
		parts := strings.Split(record, ",")
		for _, part := range parts {
			addr := strings.TrimSpace(part)
			if addr != "" {
				nodes = append(nodes, addr)
			}
		}
	}

	if len(nodes) == 0 {
		return nil, errors.New("no valid nodes found in dns records")
	}

	return nodes, nil
}

// ConnectRandomNode は DNS を使って検出したノードの中からランダムに選択して接続する
func ConnectRandomNode(dnsSeedDomain string, port uint16) (*RPCClient, error) {
	nodes, err := DiscoverNodesDNS(dnsSeedDomain)
	if err != nil {
		return nil, fmt.Errorf("discover nodes: %w", err)
	}

	if len(nodes) == 0 {
		return nil, errors.New("no nodes discovered")
	}

	// ランダムにノードを選択
	randomNode := nodes[rand.Intn(len(nodes))]

	host, discoveredPort, parseErr := parseSeedNodeAddress(randomNode)
	if parseErr != nil {
		return nil, fmt.Errorf("parse discovered node %q: %w", randomNode, parseErr)
	}

	selectedPort := fixedJSONRPCPort
	if port == fixedJSONRPCPort {
		selectedPort = fixedJSONRPCPort
	} else if discoveredPort == fixedJSONRPCPort {
		selectedPort = fixedJSONRPCPort
	}

	return NewRPCClient(host, selectedPort, 10*time.Second), nil
}

func parseSeedNodeAddress(value string) (string, uint16, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return "", 0, errors.New("empty node address")
	}

	// URL 形式 (http://host:port や https://host:port) に対応
	if strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://") {
		u, err := url.Parse(text)
		if err != nil {
			return "", 0, err
		}
		if u.Hostname() == "" {
			return "", 0, errors.New("missing hostname")
		}
		var port uint16
		if u.Port() != "" {
			parsed, convErr := strconv.ParseUint(u.Port(), 10, 16)
			if convErr != nil {
				return "", 0, convErr
			}
			port = uint16(parsed)
		} else if u.Scheme == "https" {
			port = 443
		} else {
			port = 80
		}
		return u.Hostname(), port, nil
	}

	// host:port 形式
	if host, portText, err := net.SplitHostPort(text); err == nil {
		parsed, convErr := strconv.ParseUint(portText, 10, 16)
		if convErr != nil {
			return "", 0, convErr
		}
		return host, uint16(parsed), nil
	}

	// host のみ
	if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
		return strings.Trim(text, "[]"), 0, nil
	}
	return text, 0, nil
}

// ============================================================
// HTTP API サーバー機能
// ============================================================

// StartAPIServer は HTTP API サーバーを起動する
func (c *RPCClient) StartAPIServer(host string, port int) error {
	if host == "" {
		host = "0.0.0.0"
	}
	if port <= 0 {
		port = 8080
	}

	addr := fmt.Sprintf("%s:%d", host, port)

	http.HandleFunc("/api/status", c.handleStatus)
	http.HandleFunc("/api/token", c.handleToken)
	http.HandleFunc("/api/user", c.handleUser)
	http.HandleFunc("/api/pool", c.handlePool)
	http.HandleFunc("/api/submit-tx", c.handleSubmitTx)
	http.HandleFunc("/api/submit-block", c.handleSubmitBlock)

	// ルートエンドポイント
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "Brockchain Go Client API Server\n\nAvailable endpoints:\n")
		fmt.Fprintf(w, "POST /api/status        - Get node status\n")
		fmt.Fprintf(w, "POST /api/token         - Get token info\n")
		fmt.Fprintf(w, "POST /api/user          - Get user info\n")
		fmt.Fprintf(w, "POST /api/pool          - Get pool info\n")
		fmt.Fprintf(w, "POST /api/submit-tx     - Submit transaction (JSON body)\n")
		fmt.Fprintf(w, "POST /api/submit-block  - Submit block (JSON body)\n")
	})

	fmt.Printf("🚀 Brockchain Go Client API Server started on http://%s\n", addr)
	return http.ListenAndServe(addr, nil)
}

// handleStatus は /api/status をハンドル
func (c *RPCClient) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result, err := c.Status()
	if err != nil {
		writeError(w, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleToken は /api/token をハンドル
func (c *RPCClient) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload map[string]string
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, "invalid json: "+err.Error())
		return
	}

	query := payload["query"]
	if query == "" {
		writeError(w, "missing query parameter")
		return
	}

	result, err := c.GetToken(query)
	if err != nil {
		writeError(w, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleUser は /api/user をハンドル
func (c *RPCClient) handleUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload map[string]string
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, "invalid json: "+err.Error())
		return
	}

	query := payload["query"]
	if query == "" {
		writeError(w, "missing query parameter")
		return
	}

	result, err := c.GetUser(query)
	if err != nil {
		writeError(w, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handlePool は /api/pool をハンドル
func (c *RPCClient) handlePool(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload map[string]string
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, "invalid json: "+err.Error())
		return
	}

	query := payload["query"]
	if query == "" {
		writeError(w, "missing query parameter")
		return
	}

	result, err := c.GetPool(query)
	if err != nil {
		writeError(w, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleSubmitTx は /api/submit-tx をハンドル
func (c *RPCClient) handleSubmitTx(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var tx map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		writeError(w, "invalid json: "+err.Error())
		return
	}

	result, err := c.SubmitTransaction(tx)
	if err != nil {
		writeError(w, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleSubmitBlock は /api/submit-block をハンドル
func (c *RPCClient) handleSubmitBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var block map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&block); err != nil {
		writeError(w, "invalid json: "+err.Error())
		return
	}

	result, err := c.SubmitBlock(block)
	if err != nil {
		writeError(w, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// writeError はエラーレスポンスを書き込む
func writeError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": message,
	})
}
