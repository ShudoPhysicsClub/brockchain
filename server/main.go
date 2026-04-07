package main

import (
	"bufio"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ShudoPhysicsClub/brockchain/module/chain"
	"github.com/ShudoPhysicsClub/brockchain/module/network"
)

// ============================================================
// 環境変数読取（.env ファイル）
// ============================================================

// loadEnv は .env ファイルから環境変数を読込
func loadEnv() map[string]string {
	env := make(map[string]string)

	envFile := ".env"
	data, err := ioutil.ReadFile(envFile)
	if err != nil {
		// .env が見つからない場合はデフォルト設定
		fmt.Printf("⚠ .env ファイルが見つかりません: %v\n", err)
		return env
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// コメント行とか空行をスキップ
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// KEY=VALUE をパース
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			env[key] = value
		}
	}

	return env
}

type tlsCertificateState struct {
	Fingerprint string
	NotAfter    time.Time
	CertModTime time.Time
	KeyModTime  time.Time
}

func loadTLSCertificateState(certPath, keyPath string) (*tlsCertificateState, error) {
	certData, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("read cert file: %w", err)
	}

	block, _ := pem.Decode(certData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}

	certInfo, err := os.Stat(certPath)
	if err != nil {
		return nil, fmt.Errorf("stat cert file: %w", err)
	}

	keyInfo, err := os.Stat(keyPath)
	if err != nil {
		return nil, fmt.Errorf("stat key file: %w", err)
	}

	hash := sha256.Sum256(block.Bytes)
	return &tlsCertificateState{
		Fingerprint: hex.EncodeToString(hash[:]),
		NotAfter:    cert.NotAfter,
		CertModTime: certInfo.ModTime(),
		KeyModTime:  keyInfo.ModTime(),
	}, nil
}

func startTLSCertificateMonitor(certPath, keyPath string, interval time.Duration, restartOnRotate bool) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}

	initialState, err := loadTLSCertificateState(certPath, keyPath)
	if err != nil {
		fmt.Printf("⚠ TLS モニタ初期化失敗: %v\n", err)
		return
	}

	fmt.Printf("🔎 TLS 証明書監視開始: interval=%s, expires=%s\n", interval.String(), initialState.NotAfter.Format(time.RFC3339))

	go func(base *tlsCertificateState) {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			state, checkErr := loadTLSCertificateState(certPath, keyPath)
			if checkErr != nil {
				fmt.Printf("⚠ TLS 証明書監視エラー: %v\n", checkErr)
				continue
			}

			now := time.Now()
			if now.After(state.NotAfter) {
				fmt.Printf("❌ TLS 証明書期限切れを検知: %s -> プロセス終了\n", state.NotAfter.Format(time.RFC3339))
				os.Exit(1)
			}

			if restartOnRotate {
				changed := state.Fingerprint != base.Fingerprint || !state.CertModTime.Equal(base.CertModTime) || !state.KeyModTime.Equal(base.KeyModTime)
				if changed {
					fmt.Printf("♻ TLS 証明書/鍵の更新を検知 -> プロセス終了（再起動で反映）\n")
					os.Exit(0)
				}
			}

			remaining := state.NotAfter.Sub(now).Round(time.Hour)
			fmt.Printf("✓ TLS 証明書チェックOK: expires=%s (残り %s)\n", state.NotAfter.Format(time.RFC3339), remaining)
		}
	}(initialState)
}

// ============================================================
// DNS Seed 処理（ノード発見）
// ============================================================

// resolveDNSSeed は DNS Seed から TXTレコードを取得してピア list を返す
func resolveDNSSeed(seedDomain string) []string {
	var peers []string

	if seedDomain == "" {
		return peers
	}

	// net.LookupTXT で TXTレコード取得
	txts, err := net.LookupTXT(seedDomain)
	if err != nil {
		fmt.Printf("⚠ DNS Seed 照会失敗 (%s): %v\n", seedDomain, err)
		return peers
	}

	fmt.Printf("✓ DNS Seed から %d 件のノードを取得\n", len(txts))

	for _, txt := range txts {
		for _, part := range strings.Split(txt, ",") {
			addr := extractNodeAddress(part)
			if addr == "" {
				continue
			}
			peers = append(peers, addr)
			fmt.Printf("  - %s\n", addr)
		}
	}

	return peers
}

func extractNodeAddress(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}

	if strings.HasPrefix(strings.ToLower(text), "node=") {
		return strings.TrimSpace(text[len("node="):])
	}

	return ""
}

// connectToPeer は指定ピアへの接続を試みる
func connectToPeer(peerAddr string) (net.Conn, error) {
	// TCP 接続
	conn, err := net.DialTimeout("tcp", peerAddr, 10*time.Second)
	if err != nil {
		return nil, err
	}

	fmt.Printf("✓ ピア接続: %s\n", peerAddr)
	return conn, nil
}

func getStringField(data map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		value, exists := data[key]
		if !exists || value == nil {
			continue
		}

		switch typed := value.(type) {
		case string:
			return strings.TrimSpace(typed)
		case fmt.Stringer:
			return strings.TrimSpace(typed.String())
		default:
			text := strings.TrimSpace(fmt.Sprintf("%v", value))
			if text != "<nil>" && text != "" {
				return text
			}
		}
	}
	return ""
}

func getUint64Field(data map[string]interface{}, keys ...string) uint64 {
	for _, key := range keys {
		value, exists := data[key]
		if !exists || value == nil {
			continue
		}

		switch typed := value.(type) {
		case float64:
			return uint64(typed)
		case float32:
			return uint64(typed)
		case int:
			return uint64(typed)
		case int64:
			return uint64(typed)
		case uint64:
			return typed
		case json.Number:
			if parsed, err := typed.Int64(); err == nil {
				return uint64(parsed)
			}
		case string:
			if typed == "" {
				continue
			}
			if parsed, err := strconv.ParseUint(typed, 10, 64); err == nil {
				return parsed
			}
		}
	}
	return 0
}

func getInt64Field(data map[string]interface{}, keys ...string) int64 {
	for _, key := range keys {
		value, exists := data[key]
		if !exists || value == nil {
			continue
		}

		switch typed := value.(type) {
		case float64:
			return int64(typed)
		case float32:
			return int64(typed)
		case int:
			return int64(typed)
		case int64:
			return typed
		case uint64:
			return int64(typed)
		case json.Number:
			if parsed, err := typed.Int64(); err == nil {
				return parsed
			}
		case string:
			if typed == "" {
				continue
			}
			if parsed, err := strconv.ParseInt(typed, 10, 64); err == nil {
				return parsed
			}
		}
	}
	return 0
}

// ============================================================
// メッシュ接続管理
// ============================================================

// PeerManager はピア接続を管理
type PeerManager struct {
	mu               sync.RWMutex
	peers            map[string]net.Conn // addr -> connection
	maxOutboundPeers int
}

type peerConnection struct {
	Addr string
	Conn net.Conn
}

type chainSyncPayload struct {
	Type       string        `json:"type"`
	QueryID    string        `json:"query_id,omitempty"`
	FromHeight uint64        `json:"from_height"`
	TipHeight  uint64        `json:"tip_height,omitempty"`
	Limit      uint64        `json:"limit,omitempty"`
	Blocks     []chain.Block `json:"blocks,omitempty"`
}

const (
	chainSyncRequestType  = "sync_request"
	chainSyncResponseType = "sync_response"
	chainTipRequestType   = "sync_tip_request"
	chainTipResponseType  = "sync_tip_response"
	defaultSyncBatchSize  = uint64(128)
	maxSyncBatchSize      = uint64(256)
	syncResponseTimeout   = 12 * time.Second
	periodicSyncInterval  = 24 * time.Hour
	maxSyncRetries        = 2
)

type syncTipResult struct {
	Addr      string
	TipHeight uint64
}

var syncTipQueryBus = struct {
	mu      sync.Mutex
	waiters map[string]chan syncTipResult
}{
	waiters: make(map[string]chan syncTipResult),
}

type syncSessionState struct {
	mu          sync.Mutex
	active      bool
	peer        string
	retries     int
	lastRequest uint64
	timer       *time.Timer
}

var syncSession syncSessionState

// NewPeerManager は PeerManager を初期化
func NewPeerManager(maxPeers int) *PeerManager {
	return &PeerManager{
		peers:            make(map[string]net.Conn),
		maxOutboundPeers: maxPeers,
	}
}

// AddPeer はピア接続を追加
func (pm *PeerManager) AddPeer(addr string, conn net.Conn) bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if len(pm.peers) >= pm.maxOutboundPeers {
		conn.Close()
		return false
	}

	pm.peers[addr] = conn
	fmt.Printf("📡 メッシュピア接続済: %s (計%d)\n", addr, len(pm.peers))
	return true
}

// RemovePeer はピア接続を削除
func (pm *PeerManager) RemovePeer(addr string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if conn, exists := pm.peers[addr]; exists {
		conn.Close()
		delete(pm.peers, addr)
		fmt.Printf("❌ ピア切断: %s\n", addr)
	}
}

// GetPeerCount はピア数を返す
func (pm *PeerManager) GetPeerCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.peers)
}

// SnapshotPeers は現在接続しているピアをスナップショットで返す。
func (pm *PeerManager) SnapshotPeers() []peerConnection {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	peers := make([]peerConnection, 0, len(pm.peers))
	for addr, conn := range pm.peers {
		if conn == nil {
			continue
		}
		peers = append(peers, peerConnection{Addr: addr, Conn: conn})
	}

	return peers
}

// BroadcastMessage はすべてのピアにメッセージをブロードキャスト
func (pm *PeerManager) BroadcastMessage(msg []byte) {
	pm.mu.RLock()
	peers := make([]net.Conn, 0, len(pm.peers))
	for _, conn := range pm.peers {
		peers = append(peers, conn)
	}
	pm.mu.RUnlock()

	for _, conn := range peers {
		go func(c net.Conn) {
			if _, err := c.Write(msg); err != nil {
				// 送信失敗時は切断
			}
		}(conn)
	}
}

// broadcastAcceptedBlock は検証済みブロックをピアへ流す
func broadcastAcceptedBlock(pm *PeerManager, block *chain.Block) {
	if pm == nil || block == nil {
		return
	}

	payload, err := json.Marshal(block)
	if err != nil {
		fmt.Printf("⚠ ブロックのシリアライズ失敗: %v\n", err)
		return
	}

	msg := network.Message{
		Type:      network.MessageTypeBlock,
		Timestamp: time.Now().Unix(),
		Sender:    "",
		To:        "",
		App:       network.AppTypeChain,
		Payload:   payload,
		RequestID: fmt.Sprintf("block-%s-%d", block.Hash, time.Now().UnixNano()),
		TTL:       255,
	}

	encoded, err := json.Marshal(msg)
	if err != nil {
		fmt.Printf("⚠ ネットワークメッセージ作成失敗: %v\n", err)
		return
	}

	pm.BroadcastMessage(encoded)
}

// broadcastAcceptedTransaction は検証済みTXをピアへ流す
func broadcastAcceptedTransaction(pm *PeerManager, tx *chain.Transaction) {
	if pm == nil || tx == nil {
		return
	}

	payload, err := json.Marshal(tx)
	if err != nil {
		fmt.Printf("⚠ トランザクションのシリアライズ失敗: %v\n", err)
		return
	}

	msg := network.Message{
		Type:      network.MessageTypeTransaction,
		Timestamp: time.Now().Unix(),
		Sender:    tx.From,
		To:        tx.To,
		App:       network.AppTypeChain,
		Payload:   payload,
		RequestID: fmt.Sprintf("tx-%s-%d", tx.From, time.Now().UnixNano()),
		TTL:       255,
	}

	encoded, err := json.Marshal(msg)
	if err != nil {
		fmt.Printf("⚠ ネットワークメッセージ作成失敗: %v\n", err)
		return
	}

	pm.BroadcastMessage(encoded)
}

func buildChainMessage(msgType network.MessageType, payload interface{}, requestID string) ([]byte, error) {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	msg := network.Message{
		Type:      msgType,
		Timestamp: time.Now().Unix(),
		Sender:    "",
		To:        "",
		App:       network.AppTypeChain,
		Payload:   rawPayload,
		RequestID: requestID,
		TTL:       255,
	}

	return json.Marshal(msg)
}

func requestDiffSyncToConn(conn net.Conn, fromHeight, limit uint64) error {
	if conn == nil {
		return fmt.Errorf("peer connection is nil")
	}

	if limit == 0 {
		limit = defaultSyncBatchSize
	}
	if limit > maxSyncBatchSize {
		limit = maxSyncBatchSize
	}

	payload := chainSyncPayload{
		Type:       chainSyncRequestType,
		FromHeight: fromHeight,
		Limit:      limit,
	}

	encoded, err := buildChainMessage(network.MessageTypeMessage, payload, fmt.Sprintf("sync-req-%d", time.Now().UnixNano()))
	if err != nil {
		return err
	}

	_, err = conn.Write(encoded)
	return err
}

func registerSyncTipQuery(queryID string) chan syncTipResult {
	syncTipQueryBus.mu.Lock()
	defer syncTipQueryBus.mu.Unlock()

	ch := make(chan syncTipResult, 32)
	syncTipQueryBus.waiters[queryID] = ch
	return ch
}

func unregisterSyncTipQuery(queryID string) {
	syncTipQueryBus.mu.Lock()
	defer syncTipQueryBus.mu.Unlock()

	if ch, exists := syncTipQueryBus.waiters[queryID]; exists {
		delete(syncTipQueryBus.waiters, queryID)
		close(ch)
	}
}

func publishSyncTipResult(queryID, addr string, tipHeight uint64) {
	syncTipQueryBus.mu.Lock()
	ch, exists := syncTipQueryBus.waiters[queryID]
	syncTipQueryBus.mu.Unlock()
	if !exists {
		return
	}

	select {
	case ch <- syncTipResult{Addr: addr, TipHeight: tipHeight}:
	default:
	}
}

func requestTipToConn(conn net.Conn, queryID string) error {
	if conn == nil {
		return fmt.Errorf("peer connection is nil")
	}

	payload := chainSyncPayload{Type: chainTipRequestType, QueryID: queryID}
	encoded, err := buildChainMessage(network.MessageTypeMessage, payload, fmt.Sprintf("tip-req-%d", time.Now().UnixNano()))
	if err != nil {
		return err
	}

	_, err = conn.Write(encoded)
	return err
}

func queryPeerTips(pm *PeerManager, timeout time.Duration) []syncTipResult {
	if pm == nil {
		return nil
	}

	peers := pm.SnapshotPeers()
	if len(peers) == 0 {
		return nil
	}

	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	queryID := fmt.Sprintf("tip-query-%d", time.Now().UnixNano())
	resultsCh := registerSyncTipQuery(queryID)
	defer unregisterSyncTipQuery(queryID)

	sent := 0
	for _, peer := range peers {
		if err := requestTipToConn(peer.Conn, queryID); err != nil {
			fmt.Printf("⚠ ブロック高問い合わせ失敗 (%s): %v\n", peer.Addr, err)
			continue
		}
		sent++
	}

	if sent == 0 {
		return nil
	}

	results := make([]syncTipResult, 0, sent)
	seen := make(map[string]struct{})
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for len(results) < sent {
		select {
		case res, ok := <-resultsCh:
			if !ok {
				return results
			}
			if _, exists := seen[res.Addr]; exists {
				continue
			}
			seen[res.Addr] = struct{}{}
			results = append(results, res)
		case <-timer.C:
			return results
		}
	}

	return results
}

func selectBestTipPeer(pm *PeerManager, localHeight uint64, tips []syncTipResult) (peerConnection, uint64, bool) {
	if pm == nil || len(tips) == 0 {
		return peerConnection{}, localHeight, false
	}

	peerMap := make(map[string]peerConnection)
	for _, peer := range pm.SnapshotPeers() {
		peerMap[peer.Addr] = peer
	}

	bestHeight := localHeight
	bestPeer := peerConnection{}
	found := false
	for _, tip := range tips {
		peer, exists := peerMap[tip.Addr]
		if !exists {
			continue
		}
		if tip.TipHeight > bestHeight {
			bestHeight = tip.TipHeight
			bestPeer = peer
			found = true
		}
	}

	return bestPeer, bestHeight, found
}

func stopSyncWatchdog() {
	syncSession.mu.Lock()
	defer syncSession.mu.Unlock()

	if syncSession.timer != nil {
		syncSession.timer.Stop()
		syncSession.timer = nil
	}
	syncSession.active = false
	syncSession.peer = ""
	syncSession.retries = 0
	syncSession.lastRequest = 0
}

func touchSyncWatchdog(node *Node, pm *PeerManager) {
	if node == nil || pm == nil {
		return
	}

	syncSession.mu.Lock()
	defer syncSession.mu.Unlock()

	if !syncSession.active {
		return
	}

	if syncSession.timer != nil {
		syncSession.timer.Stop()
	}

	syncSession.timer = time.AfterFunc(syncResponseTimeout, func() {
		syncSession.mu.Lock()
		if !syncSession.active {
			syncSession.mu.Unlock()
			return
		}

		retry := syncSession.retries
		lastReq := syncSession.lastRequest
		syncSession.retries++
		syncSession.mu.Unlock()

		if retry >= maxSyncRetries {
			fmt.Printf("⚠ 同期タイムアウト: retry 上限に達したため停止\n")
			node.StopSync()
			stopSyncWatchdog()
			return
		}

		fmt.Printf("⚠ 同期応答タイムアウト: retry=%d from=%d\n", retry+1, lastReq)
		if err := startSyncFromBestPeer(node, pm, lastReq); err != nil {
			fmt.Printf("⚠ 同期再要求失敗: %v\n", err)
		}
	})
}

func startSyncFromBestPeer(node *Node, pm *PeerManager, fromHeight uint64) error {
	if node == nil {
		return fmt.Errorf("node is nil")
	}
	if pm == nil {
		return fmt.Errorf("peer manager is nil")
	}

	localHeight := node.GetChainHeight()
	if fromHeight == 0 {
		fromHeight = localHeight + 1
	}

	tips := queryPeerTips(pm, 2*time.Second)
	bestPeer, bestHeight, found := selectBestTipPeer(pm, localHeight, tips)
	if !found {
		return fmt.Errorf("no higher peer tip found")
	}

	if err := node.StartSync(fromHeight); err != nil {
		return err
	}

	if err := requestDiffSyncToConn(bestPeer.Conn, fromHeight, defaultSyncBatchSize); err != nil {
		node.StopSync()
		return err
	}

	syncSession.mu.Lock()
	syncSession.active = true
	syncSession.peer = bestPeer.Addr
	syncSession.retries = 0
	syncSession.lastRequest = fromHeight
	syncSession.mu.Unlock()
	touchSyncWatchdog(node, pm)

	fmt.Printf("🔄 同期開始: peer=%s from=%d local=%d remote=%d\n", bestPeer.Addr, fromHeight, localHeight, bestHeight)
	return nil
}

func requestDiffSyncFromMesh(node *Node, pm *PeerManager, fromHeight, limit uint64) int {
	if node == nil || pm == nil {
		return 0
	}

	peers := pm.SnapshotPeers()
	sent := 0
	for _, peer := range peers {
		if err := requestDiffSyncToConn(peer.Conn, fromHeight, limit); err != nil {
			fmt.Printf("⚠ 差分同期要求送信失敗 (%s): %v\n", peer.Addr, err)
			continue
		}
		sent++
	}

	return sent
}

func respondDiffSync(conn net.Conn, node *Node, req chainSyncPayload) {
	if conn == nil || node == nil {
		return
	}

	limit := req.Limit
	if limit == 0 {
		limit = defaultSyncBatchSize
	}
	if limit > maxSyncBatchSize {
		limit = maxSyncBatchSize
	}

	tip := node.GetChainHeight()
	blocks := make([]chain.Block, 0, limit)
	for h := req.FromHeight; h <= tip && uint64(len(blocks)) < limit; h++ {
		block := node.GetBlock(h)
		if block == nil {
			continue
		}
		blocks = append(blocks, *block)
	}

	resp := chainSyncPayload{
		Type:       chainSyncResponseType,
		FromHeight: req.FromHeight,
		TipHeight:  tip,
		Limit:      limit,
		Blocks:     blocks,
	}

	encoded, err := buildChainMessage(network.MessageTypeMessage, resp, fmt.Sprintf("sync-resp-%d", time.Now().UnixNano()))
	if err != nil {
		fmt.Printf("⚠ 同期応答の作成失敗: %v\n", err)
		return
	}

	if _, err := conn.Write(encoded); err != nil {
		fmt.Printf("⚠ 同期応答送信失敗: %v\n", err)
	}
}

func handleDiffSyncResponse(conn net.Conn, node *Node, pm *PeerManager, resp chainSyncPayload) {
	if conn == nil || node == nil || pm == nil {
		return
	}

	localHeight := node.GetChainHeight()
	if len(resp.Blocks) > 0 && resp.FromHeight <= localHeight {
		adopted, err := node.TryAdoptFork(resp.Blocks)
		if err != nil {
			fmt.Printf("⚠ フォーク候補評価失敗: %v\n", err)
		} else if adopted {
			fmt.Printf("♻ フォーク採用: from=%d count=%d\n", resp.FromHeight, len(resp.Blocks))
		}

		nextHeight := node.GetChainHeight() + 1
		syncSession.mu.Lock()
		syncSession.lastRequest = nextHeight
		syncSession.mu.Unlock()
		touchSyncWatchdog(node, pm)
		if nextHeight <= resp.TipHeight {
			_ = requestDiffSyncToConn(conn, nextHeight, resp.Limit)
			return
		}

		if err := node.ValidateFullChain(); err != nil {
			fmt.Printf("❌ 同期後チェーン検証失敗: %v\n", err)
		} else {
			fmt.Printf("✓ 同期後チェーン検証OK\n")
		}
		node.StopSync()
		stopSyncWatchdog()
		return
	}

	added := 0
	for i := range resp.Blocks {
		block := resp.Blocks[i]
		if block.Height == 0 {
			continue
		}
		if err := node.AddSyncBlock(&block); err == nil {
			added++
		}
	}

	if added > 0 {
		if err := node.FinalizeSyncBlocks(); err != nil {
			fmt.Printf("⚠ 同期ブロック確定失敗: %v\n", err)
		}
	}

	if len(resp.Blocks) == 0 {
		if err := node.ValidateFullChain(); err != nil {
			fmt.Printf("❌ 同期後チェーン検証失敗: %v\n", err)
		} else {
			fmt.Printf("✓ 同期後チェーン検証OK\n")
		}
		node.StopSync()
		stopSyncWatchdog()
		return
	}

	nextHeight := resp.FromHeight + uint64(len(resp.Blocks))
	syncSession.mu.Lock()
	syncSession.lastRequest = nextHeight
	syncSession.mu.Unlock()
	touchSyncWatchdog(node, pm)
	if nextHeight > resp.TipHeight {
		if err := node.ValidateFullChain(); err != nil {
			fmt.Printf("❌ 同期後チェーン検証失敗: %v\n", err)
		} else {
			fmt.Printf("✓ 同期後チェーン検証OK\n")
		}
		node.StopSync()
		stopSyncWatchdog()
		return
	}

	_ = requestDiffSyncToConn(conn, nextHeight, resp.Limit)
}

func handlePeerStream(conn net.Conn, peerAddr string, pm *PeerManager, node *Node) {
	if conn == nil {
		return
	}

	decoder := json.NewDecoder(conn)
	for {
		var msg network.Message
		if err := decoder.Decode(&msg); err != nil {
			return
		}

		switch msg.Type {
		case network.MessageTypeBlock:
			var block chain.Block
			if err := json.Unmarshal(msg.Payload, &block); err != nil {
				continue
			}
			if err := node.ValidateAndAddBlock(&block); err != nil {
				continue
			}
		case network.MessageTypeMessage:
			if msg.App != network.AppTypeChain {
				continue
			}

			var payload chainSyncPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				continue
			}

			switch payload.Type {
			case chainSyncRequestType:
				respondDiffSync(conn, node, payload)
			case chainSyncResponseType:
				handleDiffSyncResponse(conn, node, pm, payload)
			case chainTipRequestType:
				resp := chainSyncPayload{
					Type:      chainTipResponseType,
					QueryID:   payload.QueryID,
					TipHeight: node.GetChainHeight(),
				}
				encoded, err := buildChainMessage(network.MessageTypeMessage, resp, fmt.Sprintf("tip-resp-%d", time.Now().UnixNano()))
				if err != nil {
					continue
				}
				_, _ = conn.Write(encoded)
			case chainTipResponseType:
				if payload.QueryID != "" {
					publishSyncTipResult(payload.QueryID, peerAddr, payload.TipHeight)
				}
			}
		}
	}
}

// ============================================================
// JSON-RPC API サーバー
// ============================================================

// JSONRPCRequest は JSON-RPC リクエスト
type JSONRPCRequest struct {
	Jsonrpc string                 `json:"jsonrpc"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params"`
	ID      interface{}            `json:"id"`
}

// JSONRPCResponse は JSON-RPC レスポンス
type JSONRPCResponse struct {
	Jsonrpc string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

// TokenRecord はディスク上に保存する token メタデータ
type TokenRecord struct {
	TokenID     string `json:"token_id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Symbol      string `json:"symbol"`
	Decimals    uint8  `json:"decimals"`
	TotalSupply string `json:"total_supply"`
	Owner       string `json:"owner"`
	From        string `json:"from"`
	Gas         string `json:"gas"`
	Status      string `json:"status"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// UserRecord はディスク上に保存する user state
type UserRecord struct {
	Address   string            `json:"address"`
	Nonce     uint64            `json:"nonce"`
	Balance   string            `json:"balance"`
	Tokens    map[string]string `json:"tokens"`
	Liquidity map[string]string `json:"liquidity"`
	CreatedAt int64             `json:"created_at"`
	UpdatedAt int64             `json:"updated_at"`
}

// PoolRecord は AMM pool の状態
type PoolRecord struct {
	PoolID         string            `json:"pool_id"`
	Pair           string            `json:"pair"`
	TokenA         string            `json:"token_a"`
	TokenB         string            `json:"token_b"`
	ReserveA       string            `json:"reserve_a"`
	ReserveB       string            `json:"reserve_b"`
	TotalLiquidity string            `json:"total_liquidity"`
	LiquidityOwner map[string]string `json:"liquidity_owner"`
	CreatedAt      int64             `json:"created_at"`
	UpdatedAt      int64             `json:"updated_at"`
	Status         string            `json:"status"`
}

// JSONRPCHandler は JSON-RPC ハンドラー
type JSONRPCHandler struct {
	node *Node
	mu   sync.RWMutex
}

// NewJSONRPCHandler は JSONRPCHandler を初期化
func NewJSONRPCHandler(node *Node) *JSONRPCHandler {
	return &JSONRPCHandler{
		node: node,
	}
}

func ensureDataDirectories(dataDir string) error {
	directories := []string{
		filepath.Join(dataDir, "chain"),
		filepath.Join(dataDir, "sync"),
		filepath.Join(dataDir, "users"),
		filepath.Join(dataDir, "tokens"),
		filepath.Join(dataDir, "pools"),
	}

	for _, dir := range directories {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	return nil
}

func parseWei(value string) *big.Int {
	result := new(big.Int)
	if strings.TrimSpace(value) == "" {
		return result
	}
	if _, ok := result.SetString(strings.TrimSpace(value), 10); ok {
		return result
	}
	return new(big.Int)
}

func weiString(value *big.Int) string {
	if value == nil {
		return "0"
	}
	return value.String()
}

func minBigInt(values ...*big.Int) *big.Int {
	var best *big.Int
	for _, value := range values {
		if value == nil {
			continue
		}
		if best == nil || value.Cmp(best) < 0 {
			best = new(big.Int).Set(value)
		}
	}
	if best == nil {
		return new(big.Int)
	}
	return best
}

func normalizeHexAddress(value string) string {
	address := strings.TrimSpace(strings.ToLower(value))
	if address == "" {
		return ""
	}
	if !strings.HasPrefix(address, "0x") {
		address = "0x" + address
	}
	return address
}

func addressShardDir(dataDir, address string) string {
	cleanAddress := strings.TrimPrefix(normalizeHexAddress(address), "0x")
	if len(cleanAddress) < 4 {
		cleanAddress = cleanAddress + strings.Repeat("0", 4-len(cleanAddress))
	}
	return filepath.Join(dataDir, "users", cleanAddress[0:2], cleanAddress[2:4])
}

func userRecordPath(dataDir, address string) string {
	normalized := normalizeHexAddress(address)
	return filepath.Join(addressShardDir(dataDir, normalized), normalized+".json")
}

func loadUserRecord(dataDir, address string) (*UserRecord, error) {
	path := userRecordPath(dataDir, address)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var record UserRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}

	if record.Tokens == nil {
		record.Tokens = make(map[string]string)
	}
	if record.Liquidity == nil {
		record.Liquidity = make(map[string]string)
	}

	return &record, nil
}

func saveUserRecord(dataDir string, record *UserRecord) error {
	if record == nil || record.Address == "" {
		return fmt.Errorf("invalid user record")
	}

	record.Address = normalizeHexAddress(record.Address)
	if record.Tokens == nil {
		record.Tokens = make(map[string]string)
	}
	if record.Liquidity == nil {
		record.Liquidity = make(map[string]string)
	}

	dir := addressShardDir(dataDir, record.Address)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}

	return ioutil.WriteFile(userRecordPath(dataDir, record.Address), data, 0644)
}

func ensureUserRecord(dataDir, address string) (*UserRecord, error) {
	normalized := normalizeHexAddress(address)
	if normalized == "" {
		return nil, fmt.Errorf("address is empty")
	}

	record, err := loadUserRecord(dataDir, normalized)
	if err == nil {
		return record, nil
	}

	record = &UserRecord{
		Address:   normalized,
		Nonce:     0,
		Balance:   "0",
		Tokens:    make(map[string]string),
		Liquidity: make(map[string]string),
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
	}
	if err := saveUserRecord(dataDir, record); err != nil {
		return nil, err
	}
	return record, nil
}

func updateUserNonce(dataDir, address string, nonce uint64) error {
	record, err := ensureUserRecord(dataDir, address)
	if err != nil {
		return err
	}
	if nonce+1 > record.Nonce {
		record.Nonce = nonce + 1
	}
	record.UpdatedAt = time.Now().Unix()
	return saveUserRecord(dataDir, record)
}

func addTokenToUser(dataDir, address, tokenID string) error {
	record, err := ensureUserRecord(dataDir, address)
	if err != nil {
		return err
	}
	if record.Tokens == nil {
		record.Tokens = make(map[string]string)
	}
	if current, exists := record.Tokens[tokenID]; exists {
		record.Tokens[tokenID] = current
	} else {
		record.Tokens[tokenID] = "0"
	}
	record.UpdatedAt = time.Now().Unix()
	return saveUserRecord(dataDir, record)
}

func addUserTokenBalance(dataDir, address, tokenID, amount string) error {
	record, err := ensureUserRecord(dataDir, address)
	if err != nil {
		return err
	}
	if record.Tokens == nil {
		record.Tokens = make(map[string]string)
	}
	current := parseWei(record.Tokens[tokenID])
	current.Add(current, parseWei(amount))
	record.Tokens[tokenID] = weiString(current)
	record.UpdatedAt = time.Now().Unix()
	return saveUserRecord(dataDir, record)
}

func subUserTokenBalance(dataDir, address, tokenID, amount string) error {
	record, err := ensureUserRecord(dataDir, address)
	if err != nil {
		return err
	}
	if record.Tokens == nil {
		record.Tokens = make(map[string]string)
	}
	current := parseWei(record.Tokens[tokenID])
	need := parseWei(amount)
	if current.Cmp(need) < 0 {
		return fmt.Errorf("insufficient token balance")
	}
	current.Sub(current, need)
	record.Tokens[tokenID] = weiString(current)
	record.UpdatedAt = time.Now().Unix()
	return saveUserRecord(dataDir, record)
}

func addUserLiquidityBalance(dataDir, address, poolID, amount string) error {
	record, err := ensureUserRecord(dataDir, address)
	if err != nil {
		return err
	}
	if record.Liquidity == nil {
		record.Liquidity = make(map[string]string)
	}
	current := parseWei(record.Liquidity[poolID])
	current.Add(current, parseWei(amount))
	record.Liquidity[poolID] = weiString(current)
	record.UpdatedAt = time.Now().Unix()
	return saveUserRecord(dataDir, record)
}

func subUserLiquidityBalance(dataDir, address, poolID, amount string) error {
	record, err := ensureUserRecord(dataDir, address)
	if err != nil {
		return err
	}
	if record.Liquidity == nil {
		record.Liquidity = make(map[string]string)
	}
	current := parseWei(record.Liquidity[poolID])
	need := parseWei(amount)
	if current.Cmp(need) < 0 {
		return fmt.Errorf("insufficient liquidity balance")
	}
	current.Sub(current, need)
	record.Liquidity[poolID] = weiString(current)
	record.UpdatedAt = time.Now().Unix()
	return saveUserRecord(dataDir, record)
}

func canonicalPair(tokenA, tokenB string) (string, string, string) {
	a := strings.ToLower(strings.TrimSpace(tokenA))
	b := strings.ToLower(strings.TrimSpace(tokenB))
	if a == "" || b == "" {
		return "", a, b
	}
	if a > b {
		a, b = b, a
	}
	return a + "/" + b, a, b
}

func derivePoolID(pair string) string {
	clean := strings.TrimSpace(strings.ToLower(pair))
	if clean == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(clean))
	return "0x" + hex.EncodeToString(sum[:8])
}

func poolShardDir(dataDir, poolID string) string {
	cleanID := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(poolID)), "0x")
	if len(cleanID) < 4 {
		cleanID = cleanID + strings.Repeat("0", 4-len(cleanID))
	}
	return filepath.Join(dataDir, "pools", cleanID[0:2], cleanID[2:4])
}

func poolRecordPath(dataDir, poolID string) string {
	return filepath.Join(poolShardDir(dataDir, poolID), poolID+".json")
}

func loadPoolRecord(dataDir, poolID string) (*PoolRecord, error) {
	path := poolRecordPath(dataDir, poolID)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var record PoolRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	if record.LiquidityOwner == nil {
		record.LiquidityOwner = make(map[string]string)
	}
	return &record, nil
}

func savePoolRecord(dataDir string, record *PoolRecord) error {
	if record == nil || record.PoolID == "" {
		return fmt.Errorf("invalid pool record")
	}
	if record.LiquidityOwner == nil {
		record.LiquidityOwner = make(map[string]string)
	}
	if err := os.MkdirAll(poolShardDir(dataDir, record.PoolID), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(poolRecordPath(dataDir, record.PoolID), data, 0644)
}

func ensurePoolRecord(dataDir, pair string, tokenA string, tokenB string) (*PoolRecord, error) {
	pairKey, canonicalA, canonicalB := canonicalPair(tokenA, tokenB)
	if pairKey == "" && pair != "" {
		pairKey = strings.TrimSpace(strings.ToLower(pair))
	}
	if pairKey == "" {
		return nil, fmt.Errorf("pair is empty")
	}
	poolID := derivePoolID(pairKey)
	record, err := loadPoolRecord(dataDir, poolID)
	if err == nil {
		return record, nil
	}
	record = &PoolRecord{
		PoolID:         poolID,
		Pair:           pairKey,
		TokenA:         canonicalA,
		TokenB:         canonicalB,
		ReserveA:       "0",
		ReserveB:       "0",
		TotalLiquidity: "0",
		LiquidityOwner: make(map[string]string),
		CreatedAt:      time.Now().Unix(),
		UpdatedAt:      time.Now().Unix(),
		Status:         "pending",
	}
	if err := savePoolRecord(dataDir, record); err != nil {
		return nil, err
	}
	return record, nil
}

func findPoolByPair(dataDir, pair string) (*PoolRecord, error) {
	pair = strings.TrimSpace(strings.ToLower(pair))
	if pair == "" {
		return nil, fmt.Errorf("pair is empty")
	}
	poolID := derivePoolID(pair)
	return loadPoolRecord(dataDir, poolID)
}

func applyCreateTokenState(dataDir string, tx *chain.Transaction) error {
	if tx == nil {
		return fmt.Errorf("transaction is nil")
	}
	if tx.Owner == "" {
		tx.Owner = tx.From
	}
	if tx.TokenID == "" {
		tx.TokenID = chain.DeriveTokenID(tx)
	}

	record := &TokenRecord{
		TokenID:     tx.TokenID,
		Type:        string(tx.Type),
		Name:        tx.Name,
		Symbol:      tx.Symbol,
		Decimals:    tx.Decimals,
		TotalSupply: tx.TotalSupply,
		Owner:       tx.Owner,
		From:        tx.From,
		Gas:         chain.DefaultGasFee,
		Status:      "pending",
		CreatedAt:   tx.Timestamp,
		UpdatedAt:   tx.Timestamp,
	}
	if err := saveTokenRecord(dataDir, record); err != nil {
		return err
	}
	if err := addTokenToUser(dataDir, tx.Owner, tx.TokenID); err != nil {
		return err
	}
	return addUserTokenBalance(dataDir, tx.Owner, tx.TokenID, tx.TotalSupply)
}

func applyTokenSwapState(dataDir string, tx *chain.Transaction) (map[string]interface{}, error) {
	if tx == nil {
		return nil, fmt.Errorf("transaction is nil")
	}

	pair := strings.TrimSpace(tx.Pair)
	if pair == "" && tx.TokenIn != "" && tx.TokenOut != "" {
		pair, _, _ = canonicalPair(tx.TokenIn, tx.TokenOut)
	}
	if pair == "" {
		return nil, fmt.Errorf("pair is required")
	}

	pool, err := findPoolByPair(dataDir, pair)
	if err != nil {
		return nil, fmt.Errorf("pool not found: %w", err)
	}

	tokenIn := strings.TrimSpace(tx.TokenIn)
	tokenOut := strings.TrimSpace(tx.TokenOut)
	if tokenIn == "" || tokenOut == "" {
		parts := strings.Split(pair, "/")
		if len(parts) == 2 {
			if tokenIn == "" {
				tokenIn = parts[0]
			}
			if tokenOut == "" {
				tokenOut = parts[1]
			}
		}
	}
	if tokenIn == "" || tokenOut == "" {
		return nil, fmt.Errorf("token_in and token_out are required")
	}

	amountIn := parseWei(tx.AmountIn)
	if amountIn.Sign() <= 0 {
		amountIn = parseWei(tx.Amount)
	}
	if amountIn.Sign() <= 0 {
		return nil, fmt.Errorf("amount_in is required")
	}
	minOut := parseWei(tx.AmountOutMin)

	reserveA := parseWei(pool.ReserveA)
	reserveB := parseWei(pool.ReserveB)

	var reserveIn, reserveOut *big.Int
	var reserveInField string
	if strings.EqualFold(tokenIn, pool.TokenA) && strings.EqualFold(tokenOut, pool.TokenB) {
		reserveIn, reserveOut = reserveA, reserveB
		reserveInField = "A"
	} else if strings.EqualFold(tokenIn, pool.TokenB) && strings.EqualFold(tokenOut, pool.TokenA) {
		reserveIn, reserveOut = reserveB, reserveA
		reserveInField = "B"
	} else {
		return nil, fmt.Errorf("token pair mismatch")
	}

	if reserveIn.Sign() <= 0 || reserveOut.Sign() <= 0 {
		return nil, fmt.Errorf("pool reserves are empty")
	}

	// 0 fee の単純 constant product
	numerator := new(big.Int).Mul(reserveOut, amountIn)
	denominator := new(big.Int).Add(reserveIn, amountIn)
	if denominator.Sign() <= 0 {
		return nil, fmt.Errorf("invalid pool denominator")
	}
	amountOut := new(big.Int).Quo(numerator, denominator)
	if amountOut.Cmp(minOut) < 0 {
		return nil, fmt.Errorf("amount out below minimum")
	}

	if err := subUserTokenBalance(dataDir, tx.From, tokenIn, amountIn.String()); err != nil {
		return nil, err
	}
	if err := addUserTokenBalance(dataDir, tx.From, tokenOut, amountOut.String()); err != nil {
		return nil, err
	}

	if reserveInField == "A" {
		reserveA.Add(reserveA, amountIn)
		reserveB.Sub(reserveB, amountOut)
	} else {
		reserveB.Add(reserveB, amountIn)
		reserveA.Sub(reserveA, amountOut)
	}

	pool.ReserveA = weiString(reserveA)
	pool.ReserveB = weiString(reserveB)
	pool.Status = "active"
	pool.UpdatedAt = time.Now().Unix()
	if err := savePoolRecord(dataDir, pool); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"pair":       pool.Pair,
		"token_in":   tokenIn,
		"token_out":  tokenOut,
		"amount_in":  amountIn.String(),
		"amount_out": amountOut.String(),
		"reserve_a":  pool.ReserveA,
		"reserve_b":  pool.ReserveB,
		"pool_id":    pool.PoolID,
	}, nil
}

func applyAddLiquidityState(dataDir string, tx *chain.Transaction) (map[string]interface{}, error) {
	if tx == nil {
		return nil, fmt.Errorf("transaction is nil")
	}

	tokenA := tx.TokenA
	tokenB := tx.TokenB
	if tokenA == "" || tokenB == "" {
		parts := strings.Split(strings.TrimSpace(tx.Pair), "/")
		if len(parts) == 2 {
			if tokenA == "" {
				tokenA = parts[0]
			}
			if tokenB == "" {
				tokenB = parts[1]
			}
		}
	}
	if tokenA == "" || tokenB == "" {
		return nil, fmt.Errorf("token_a and token_b are required")
	}

	amountA := parseWei(tx.AmountA)
	amountB := parseWei(tx.AmountB)
	if amountA.Sign() <= 0 || amountB.Sign() <= 0 {
		return nil, fmt.Errorf("amount_a and amount_b are required")
	}

	pairKey, canonicalA, canonicalB := canonicalPair(tokenA, tokenB)
	pool, err := ensurePoolRecord(dataDir, pairKey, canonicalA, canonicalB)
	if err != nil {
		return nil, err
	}

	reserveA := parseWei(pool.ReserveA)
	reserveB := parseWei(pool.ReserveB)
	liquidityMinted := new(big.Int)
	if reserveA.Sign() == 0 && reserveB.Sign() == 0 {
		liquidityMinted.Set(amountA)
	} else {
		mintA := new(big.Int).Mul(amountA, parseWei(pool.TotalLiquidity))
		if reserveA.Sign() > 0 {
			mintA.Quo(mintA, reserveA)
		}
		mintB := new(big.Int).Mul(amountB, parseWei(pool.TotalLiquidity))
		if reserveB.Sign() > 0 {
			mintB.Quo(mintB, reserveB)
		}
		liquidityMinted = minBigInt(mintA, mintB)
		if liquidityMinted.Sign() <= 0 {
			liquidityMinted = minBigInt(amountA, amountB)
		}
	}

	if err := subUserTokenBalance(dataDir, tx.From, canonicalA, amountA.String()); err != nil {
		return nil, err
	}
	if err := subUserTokenBalance(dataDir, tx.From, canonicalB, amountB.String()); err != nil {
		return nil, err
	}

	reserveA.Add(reserveA, amountA)
	reserveB.Add(reserveB, amountB)
	pool.ReserveA = weiString(reserveA)
	pool.ReserveB = weiString(reserveB)
	pool.TotalLiquidity = weiString(new(big.Int).Add(parseWei(pool.TotalLiquidity), liquidityMinted))
	pool.Status = "active"
	pool.UpdatedAt = time.Now().Unix()
	if pool.LiquidityOwner == nil {
		pool.LiquidityOwner = make(map[string]string)
	}
	ownerLiquidity := parseWei(pool.LiquidityOwner[tx.From])
	ownerLiquidity.Add(ownerLiquidity, liquidityMinted)
	pool.LiquidityOwner[tx.From] = weiString(ownerLiquidity)
	if err := savePoolRecord(dataDir, pool); err != nil {
		return nil, err
	}
	if err := addUserLiquidityBalance(dataDir, tx.From, pool.PoolID, liquidityMinted.String()); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"pair":             pool.Pair,
		"token_a":          canonicalA,
		"token_b":          canonicalB,
		"amount_a":         amountA.String(),
		"amount_b":         amountB.String(),
		"liquidity_minted": liquidityMinted.String(),
		"reserve_a":        pool.ReserveA,
		"reserve_b":        pool.ReserveB,
		"pool_id":          pool.PoolID,
	}, nil
}

func applyRemoveLiquidityState(dataDir string, tx *chain.Transaction) (map[string]interface{}, error) {
	if tx == nil {
		return nil, fmt.Errorf("transaction is nil")
	}

	tokenA := tx.TokenA
	tokenB := tx.TokenB
	if tokenA == "" || tokenB == "" {
		parts := strings.Split(strings.TrimSpace(tx.Pair), "/")
		if len(parts) == 2 {
			if tokenA == "" {
				tokenA = parts[0]
			}
			if tokenB == "" {
				tokenB = parts[1]
			}
		}
	}
	if tokenA == "" || tokenB == "" {
		return nil, fmt.Errorf("token_a and token_b are required")
	}

	liquidity := parseWei(tx.Liquidity)
	if liquidity.Sign() <= 0 {
		return nil, fmt.Errorf("liquidity is required")
	}

	pairKey, canonicalA, canonicalB := canonicalPair(tokenA, tokenB)
	pool, err := findPoolByPair(dataDir, pairKey)
	if err != nil {
		return nil, err
	}

	owned := parseWei(pool.LiquidityOwner[tx.From])
	if owned.Cmp(liquidity) < 0 {
		return nil, fmt.Errorf("insufficient pool liquidity")
	}
	totalLiquidity := parseWei(pool.TotalLiquidity)
	if totalLiquidity.Sign() <= 0 {
		return nil, fmt.Errorf("pool liquidity is empty")
	}

	reserveA := parseWei(pool.ReserveA)
	reserveB := parseWei(pool.ReserveB)
	amountA := new(big.Int).Mul(reserveA, liquidity)
	amountA.Quo(amountA, totalLiquidity)
	amountB := new(big.Int).Mul(reserveB, liquidity)
	amountB.Quo(amountB, totalLiquidity)

	pool.ReserveA = weiString(new(big.Int).Sub(reserveA, amountA))
	pool.ReserveB = weiString(new(big.Int).Sub(reserveB, amountB))
	pool.TotalLiquidity = weiString(new(big.Int).Sub(totalLiquidity, liquidity))
	owned.Sub(owned, liquidity)
	pool.LiquidityOwner[tx.From] = weiString(owned)
	pool.UpdatedAt = time.Now().Unix()
	if err := savePoolRecord(dataDir, pool); err != nil {
		return nil, err
	}
	if err := subUserLiquidityBalance(dataDir, tx.From, pool.PoolID, liquidity.String()); err != nil {
		return nil, err
	}
	if err := addUserTokenBalance(dataDir, tx.From, canonicalA, amountA.String()); err != nil {
		return nil, err
	}
	if err := addUserTokenBalance(dataDir, tx.From, canonicalB, amountB.String()); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"pair":      pool.Pair,
		"token_a":   canonicalA,
		"token_b":   canonicalB,
		"liquidity": liquidity.String(),
		"amount_a":  amountA.String(),
		"amount_b":  amountB.String(),
		"reserve_a": pool.ReserveA,
		"reserve_b": pool.ReserveB,
		"pool_id":   pool.PoolID,
	}, nil
}

func applyMintState(dataDir string, tx *chain.Transaction) error {
	if tx == nil {
		return fmt.Errorf("transaction is nil")
	}

	tokenID := tx.TokenID
	if tokenID == "" {
		return fmt.Errorf("token_id is required")
	}

	amount := parseWei(tx.Amount)
	if amount.Sign() <= 0 {
		return fmt.Errorf("amount is required and must be positive")
	}

	// トークンレコード取得
	tokenRecord, err := loadTokenRecord(dataDir, tokenID)
	if err != nil {
		return fmt.Errorf("token not found: %w", err)
	}

	// オーナーのみが mint 可能を想定
	if tx.From != tokenRecord.Owner {
		return fmt.Errorf("only token owner can mint")
	}

	// トークン供給量を増加
	currentSupply := parseWei(tokenRecord.TotalSupply)
	currentSupply.Add(currentSupply, amount)
	tokenRecord.TotalSupply = weiString(currentSupply)
	tokenRecord.UpdatedAt = time.Now().Unix()
	if err := saveTokenRecord(dataDir, tokenRecord); err != nil {
		return err
	}

	// ユーザーのトークンバランスを増加
	if err := addUserTokenBalance(dataDir, tx.From, tokenID, amount.String()); err != nil {
		return err
	}

	return nil
}

func applyBurnState(dataDir string, tx *chain.Transaction) error {
	if tx == nil {
		return fmt.Errorf("transaction is nil")
	}

	tokenID := tx.TokenID
	if tokenID == "" {
		return fmt.Errorf("token_id is required")
	}

	amount := parseWei(tx.Amount)
	if amount.Sign() <= 0 {
		return fmt.Errorf("amount is required and must be positive")
	}

	// トークンレコード取得
	tokenRecord, err := loadTokenRecord(dataDir, tokenID)
	if err != nil {
		return fmt.Errorf("token not found: %w", err)
	}

	// ユーザーのトークンバランスをチェック
	userBalance := parseWei("0")
	if userRecord, err := loadUserRecord(dataDir, tx.From); err == nil && userRecord.Tokens != nil {
		userBalance = parseWei(userRecord.Tokens[tokenID])
	}

	if userBalance.Cmp(amount) < 0 {
		return fmt.Errorf("insufficient token balance for burn")
	}

	// トークン供給量を削減
	currentSupply := parseWei(tokenRecord.TotalSupply)
	if currentSupply.Cmp(amount) < 0 {
		return fmt.Errorf("burn amount exceeds total supply")
	}
	currentSupply.Sub(currentSupply, amount)
	tokenRecord.TotalSupply = weiString(currentSupply)
	tokenRecord.UpdatedAt = time.Now().Unix()
	if err := saveTokenRecord(dataDir, tokenRecord); err != nil {
		return err
	}

	// ユーザーのトークンバランスを削減
	if err := subUserTokenBalance(dataDir, tx.From, tokenID, amount.String()); err != nil {
		return err
	}

	return nil
}

func searchUserRecords(dataDir, query string) ([]*UserRecord, error) {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return nil, fmt.Errorf("query is empty")
	}

	baseDir := filepath.Join(dataDir, "users")
	var results []*UserRecord

	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}

		data, readErr := ioutil.ReadFile(path)
		if readErr != nil {
			return nil
		}

		var record UserRecord
		if jsonErr := json.Unmarshal(data, &record); jsonErr != nil {
			return nil
		}

		if strings.Contains(strings.ToLower(record.Address), query) {
			results = append(results, &record)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return results, nil
}

func tokenShardDir(dataDir, tokenID string) string {
	cleanID := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(tokenID)), "0x")
	if len(cleanID) < 4 {
		cleanID = cleanID + strings.Repeat("0", 4-len(cleanID))
	}
	return filepath.Join(dataDir, "tokens", cleanID[0:2], cleanID[2:4])
}

func tokenRecordPath(dataDir, tokenID string) string {
	return filepath.Join(tokenShardDir(dataDir, tokenID), tokenID+".json")
}

func saveTokenRecord(dataDir string, record *TokenRecord) error {
	if record == nil || record.TokenID == "" {
		return fmt.Errorf("invalid token record")
	}

	dir := tokenShardDir(dataDir, record.TokenID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}

	return ioutil.WriteFile(tokenRecordPath(dataDir, record.TokenID), data, 0644)
}

func loadTokenRecord(dataDir, tokenID string) (*TokenRecord, error) {
	path := tokenRecordPath(dataDir, tokenID)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var record TokenRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}

	return &record, nil
}

func searchTokenRecords(dataDir, query string) ([]*TokenRecord, error) {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return nil, fmt.Errorf("query is empty")
	}

	baseDir := filepath.Join(dataDir, "tokens")
	var results []*TokenRecord

	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}

		data, readErr := ioutil.ReadFile(path)
		if readErr != nil {
			return nil
		}

		var record TokenRecord
		if jsonErr := json.Unmarshal(data, &record); jsonErr != nil {
			return nil
		}

		if strings.Contains(strings.ToLower(record.TokenID), query) ||
			strings.Contains(strings.ToLower(record.Name), query) ||
			strings.Contains(strings.ToLower(record.Symbol), query) {
			results = append(results, &record)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return results, nil
}

func updateTokenStatus(dataDir, tokenID, status string) error {
	record, err := loadTokenRecord(dataDir, tokenID)
	if err != nil {
		return err
	}
	record.Status = status
	return saveTokenRecord(dataDir, record)
}

// ServeHTTP は HTTP リクエストを処理
func (h *JSONRPCHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// POST のみ受け入れ
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// リクエスト解析
	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(JSONRPCResponse{
			Jsonrpc: "2.0",
			Error:   "Invalid request",
			ID:      nil,
		})
		return
	}

	// メソッド処理
	var resp JSONRPCResponse
	resp.Jsonrpc = "2.0"
	resp.ID = req.ID

	if h.node.IsSyncing() && req.Method != "brockchain_status" {
		resp.Error = "node is syncing; method temporarily unavailable"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	switch req.Method {
	case "eth_blockNumber":
		// チェーン高を16進数で返す
		height := h.node.GetChainHeight()
		resp.Result = fmt.Sprintf("0x%x", height)

	case "eth_chainId":
		// chain ID（固定）
		resp.Result = "0x1"

	case "eth_gasPrice":
		// ガス価格（返す）
		resp.Result = "0x1"

	case "eth_getBalance":
		// アカウント残高（TBD: state が必要）
		resp.Result = "0x0"

	case "eth_getBlockByNumber":
		// ブロック情報
		if len(req.Params) > 0 {
			// TBD: パースしてブロック取得
			resp.Result = map[string]interface{}{
				"number": "0x1",
			}
		}

	case "eth_sendTransaction":
		// トランザクション送信（TBD）
		resp.Error = "Not implemented"

	case "net_listening":
		// ノードがリッスン中か
		resp.Result = true

	case "web3_clientVersion":
		// バージョン
		resp.Result = "Brockchain/1.0.0"

	case "brockchain_status":
		// カスタム: ノード状態
		status := h.node.GetStatus()
		resp.Result = map[string]interface{}{
			"height":       status.Height,
			"difficulty":   status.Difficulty,
			"mempool_size": status.MempoolSize,
			"is_syncing":   status.IsSyncing,
			"chain_work":   status.ChainWork,
		}

	case "brockchain_getToken":
		// カスタム: token 検索
		query := getStringField(req.Params, "token_id", "tokenId", "query", "name", "symbol")
		if query == "" {
			resp.Error = "missing token query"
			break
		}

		if record, err := loadTokenRecord(h.node.dataDir, query); err == nil {
			resp.Result = map[string]interface{}{
				"found":  true,
				"token":  record,
				"source": "direct",
			}
			break
		}

		matches, err := searchTokenRecords(h.node.dataDir, query)
		if err != nil {
			resp.Error = fmt.Sprintf("token search failed: %v", err)
			break
		}

		resp.Result = map[string]interface{}{
			"found":  len(matches) > 0,
			"count":  len(matches),
			"tokens": matches,
		}

	case "brockchain_getUser":
		// カスタム: user 検索
		query := getStringField(req.Params, "address", "query", "from")
		if query == "" {
			resp.Error = "missing user query"
			break
		}

		if record, err := loadUserRecord(h.node.dataDir, query); err == nil {
			resp.Result = map[string]interface{}{
				"found": true,
				"user":  record,
			}
			break
		}

		matches, err := searchUserRecords(h.node.dataDir, query)
		if err != nil {
			resp.Error = fmt.Sprintf("user search failed: %v", err)
			break
		}

		resp.Result = map[string]interface{}{
			"found": len(matches) > 0,
			"count": len(matches),
			"users": matches,
		}

	case "brockchain_getPool":
		// カスタム: pool 検索
		query := getStringField(req.Params, "pair", "query", "pool_id")
		if query == "" {
			resp.Error = "missing pool query"
			break
		}

		if pool, err := findPoolByPair(h.node.dataDir, query); err == nil {
			resp.Result = map[string]interface{}{
				"found": true,
				"pool":  pool,
			}
			break
		}

		poolID := derivePoolID(query)
		if poolID != "" {
			if pool, err := loadPoolRecord(h.node.dataDir, poolID); err == nil {
				resp.Result = map[string]interface{}{
					"found": true,
					"pool":  pool,
				}
				break
			}
		}

		resp.Error = fmt.Sprintf("pool not found: %s", query)

	case "brockchain_submitTransaction":
		// カスタム: トランザクション送信
		// params: {"tx": {...}} または {"type":..., "from":..., ...}
		txPayload := req.Params
		if nested, exists := req.Params["tx"]; exists {
			if nestedMap, ok := nested.(map[string]interface{}); ok {
				txPayload = nestedMap
			}
		}

		txType := chain.TransactionType(getStringField(txPayload, "type"))
		if txType == "" {
			txType = chain.TransactionTypeTransfer
		}

		tx := &chain.Transaction{
			Type:         txType,
			From:         getStringField(txPayload, "from"),
			To:           getStringField(txPayload, "to", "recipient"),
			Amount:       getStringField(txPayload, "amount"),
			Gas:          chain.DefaultGasFee,
			Nonce:        getUint64Field(txPayload, "nonce"),
			Timestamp:    getInt64Field(txPayload, "timestamp"),
			Signature:    getStringField(txPayload, "signature"),
			PublicKey:    getStringField(txPayload, "public_key", "publicKey"),
			TokenID:      getStringField(txPayload, "token_id", "tokenId"),
			TokenIn:      getStringField(txPayload, "token_in", "tokenIn"),
			TokenOut:     getStringField(txPayload, "token_out", "tokenOut"),
			TokenA:       getStringField(txPayload, "token_a", "tokenA"),
			TokenB:       getStringField(txPayload, "token_b", "tokenB"),
			AmountA:      getStringField(txPayload, "amount_a", "amountA"),
			AmountB:      getStringField(txPayload, "amount_b", "amountB"),
			Liquidity:    getStringField(txPayload, "liquidity"),
			Name:         getStringField(txPayload, "name"),
			Symbol:       getStringField(txPayload, "symbol"),
			Decimals:     uint8(getUint64Field(txPayload, "decimals")),
			TotalSupply:  getStringField(txPayload, "total_supply", "totalSupply"),
			Owner:        getStringField(txPayload, "owner"),
			Pair:         getStringField(txPayload, "pair"),
			AmountIn:     getStringField(txPayload, "amount_in", "amountIn"),
			AmountOutMin: getStringField(txPayload, "amount_out_min", "amountOutMin"),
			Deadline:     getInt64Field(txPayload, "deadline"),
		}

		if tx.Timestamp == 0 {
			tx.Timestamp = time.Now().Unix()
		}
		tx.Gas = chain.DefaultGasFee
		if tx.From == "" {
			tx.From = "0x" + strings.Repeat("0", 40)
		}
		if tx.Type == chain.TransactionTypeCreateToken && tx.Owner == "" {
			tx.Owner = tx.From
		}
		if tx.Type == chain.TransactionTypeCreateToken && tx.TokenID == "" {
			tx.TokenID = chain.DeriveTokenID(tx)
		}
		if tx.Type == chain.TransactionTypeCreateToken && tx.Owner == "" {
			tx.Owner = tx.From
		}

		// 最低限の必須項目を種別ごとに確認
		switch tx.Type {
		case chain.TransactionTypeCreateToken:
			if tx.Name == "" || tx.Symbol == "" || tx.TotalSupply == "" || tx.TokenID == "" {
				resp.Error = "missing token fields: name, symbol, total_supply"
				break
			}
		case chain.TransactionTypeMint:
			if tx.TokenID == "" || tx.Amount == "" {
				resp.Error = "missing mint fields: token_id, amount"
				break
			}
		case chain.TransactionTypeBurn:
			if tx.TokenID == "" || tx.Amount == "" {
				resp.Error = "missing burn fields: token_id, amount"
				break
			}
		case chain.TransactionTypeTokenSwap:
			if tx.Pair == "" && (tx.TokenIn == "" || tx.TokenOut == "") {
				resp.Error = "missing swap fields: pair or token_in/token_out"
				break
			}
			if tx.AmountIn == "" || tx.AmountOutMin == "" {
				resp.Error = "missing swap fields: pair, amount_in, amount_out_min"
				break
			}
		case chain.TransactionTypeAddLiquidity:
			if tx.Pair == "" && (tx.TokenA == "" || tx.TokenB == "") {
				resp.Error = "missing liquidity fields: pair or token_a/token_b"
				break
			}
			if tx.AmountA == "" || tx.AmountB == "" {
				resp.Error = "missing liquidity amounts: amount_a, amount_b"
				break
			}
		case chain.TransactionTypeRemoveLiquidity:
			if tx.Pair == "" && (tx.TokenA == "" || tx.TokenB == "") {
				resp.Error = "missing remove liquidity fields: pair or token_a/token_b"
				break
			}
			if tx.Liquidity == "" {
				resp.Error = "missing liquidity amount"
				break
			}
		default:
			if tx.To == "" || tx.Amount == "" {
				resp.Error = "missing transfer fields: to, amount"
				break
			}
		}

		if resp.Error != nil {
			break
		}

		if tx.Type == chain.TransactionTypeCreateToken {
			if err := applyCreateTokenState(h.node.dataDir, tx); err != nil {
				resp.Error = fmt.Sprintf("failed to create token state: %v", err)
				break
			}
		}

		if tx.Type == chain.TransactionTypeMint {
			if err := applyMintState(h.node.dataDir, tx); err != nil {
				resp.Error = fmt.Sprintf("failed to apply mint: %v", err)
				break
			}
		}

		if tx.Type == chain.TransactionTypeBurn {
			if err := applyBurnState(h.node.dataDir, tx); err != nil {
				resp.Error = fmt.Sprintf("failed to apply burn: %v", err)
				break
			}
		}

		// 署名があるならノード検証、なければ互換用にプール投入
		if tx.Signature != "" && tx.PublicKey != "" {
			if err := h.node.ValidateAndAddTransaction(tx); err != nil {
				resp.Error = fmt.Sprintf("failed to submit transaction: %v", err)
			} else {
				response := map[string]interface{}{
					"status": "accepted",
					"type":   tx.Type,
					"from":   tx.From,
					"to":     tx.To,
				}
				if tx.Type == chain.TransactionTypeTokenSwap {
					if result, err := applyTokenSwapState(h.node.dataDir, tx); err != nil {
						resp.Error = fmt.Sprintf("failed to apply swap: %v", err)
					} else {
						response["swap"] = result
					}
				}
				if tx.Type == chain.TransactionTypeAddLiquidity {
					if result, err := applyAddLiquidityState(h.node.dataDir, tx); err != nil {
						resp.Error = fmt.Sprintf("failed to add liquidity: %v", err)
					} else {
						response["pool"] = result
					}
				}
				if tx.Type == chain.TransactionTypeRemoveLiquidity {
					if result, err := applyRemoveLiquidityState(h.node.dataDir, tx); err != nil {
						resp.Error = fmt.Sprintf("failed to remove liquidity: %v", err)
					} else {
						response["pool"] = result
					}
				}

				if resp.Error != nil {
					break
				}

				resp.Result = response
				_ = updateUserNonce(h.node.dataDir, tx.From, tx.Nonce)
			}
		} else {
			if tx.Type != chain.TransactionTypeTransfer {
				resp.Error = "signed transaction required for this tx type"
				break
			}
			if err := h.node.SubmitTransaction(tx); err != nil {
				resp.Error = fmt.Sprintf("failed to submit transaction: %v", err)
			} else {
				resp.Result = map[string]interface{}{
					"status": "submitted",
					"type":   tx.Type,
					"to":     tx.To,
					"amount": tx.Amount,
				}
			}
		}

	case "brockchain_submitBlock":
		// カスタム: ブロック投げ（マイナー用）
		// params: {"block": {...}}
		blockData, ok := req.Params["block"]
		if !ok {
			resp.Error = "missing 'block' in params"
		} else {
			blockMap, ok := blockData.(map[string]interface{})
			if !ok {
				resp.Error = "invalid block format"
			} else {
				// エラーハンドリング付きでブロック構造体に変換
				height, _ := blockMap["height"].(float64)
				nonce, _ := blockMap["nonce"].(float64)
				difficulty, _ := blockMap["difficulty"].(float64)
				timestamp, _ := blockMap["timestamp"].(float64)

				block := &chain.Block{
					Height:       uint64(height),
					PreviousHash: fmt.Sprintf("%v", blockMap["previous_hash"]),
					Timestamp:    int64(timestamp),
					Nonce:        uint64(nonce),
					Difficulty:   int(difficulty),
					Miner:        fmt.Sprintf("%v", blockMap["miner"]),
					Reward:       fmt.Sprintf("%v", blockMap["reward"]),
					Hash:         fmt.Sprintf("%v", blockMap["hash"]),
					Transactions: []chain.Transaction{},
				}

				// Transactions パース（失敗しても続行）
				if txsData, exists := blockMap["transactions"]; exists {
					if txsArr, ok := txsData.([]interface{}); ok {
						for _, txData := range txsArr {
							if txMap, ok := txData.(map[string]interface{}); ok {
								txNonce, _ := txMap["nonce"].(float64)
								txTimestamp, _ := txMap["timestamp"].(float64)

								tx := chain.Transaction{
									From:      fmt.Sprintf("%v", txMap["from"]),
									To:        fmt.Sprintf("%v", txMap["to"]),
									Amount:    fmt.Sprintf("%v", txMap["amount"]),
									Nonce:     uint64(txNonce),
									Timestamp: int64(txTimestamp),
									Signature: fmt.Sprintf("%v", txMap["signature"]),
									PublicKey: fmt.Sprintf("%v", txMap["public_key"]),
								}
								block.Transactions = append(block.Transactions, tx)
							}
						}
					}
				}

				// ブロック検証 → チェーン追加
				if err := h.node.ValidateAndAddBlock(block); err != nil {
					resp.Error = fmt.Sprintf("block validation failed: %v", err)
				} else {
					resp.Result = map[string]interface{}{
						"status": "accepted",
						"height": block.Height,
						"hash":   block.Hash,
					}
				}
			}
		}

	default:
		resp.Error = "Unknown method"
	}

	// レスポンス返送
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ============================================================
// main
// ============================================================

func main() {
	// .env 読取
	env := loadEnv()
	dnsSeed := env["DNS_SEED"]
	if dnsSeed == "" {
		dnsSeed = "localhost"
	}

	dataDir := "./data"
	if dir := env["DATA_DIR"]; dir != "" {
		dataDir = dir
	}

	p2pPort := 8333
	if env["P2P_PORT"] != "" {
		fmt.Sscanf(env["P2P_PORT"], "%d", &p2pPort)
	}

	jsonRPCPort := 59988
	if raw := strings.TrimSpace(env["JSONRPC_PORT"]); raw != "" && raw != "59988" {
		fmt.Printf("⚠ JSONRPC_PORT は固定 59988 を使用します（指定値 %s は無視）\n", raw)
	}

	listenHost := "127.0.0.1"
	if env["LISTEN_HOST"] != "" {
		listenHost = env["LISTEN_HOST"]
	}

	maxOutboundPeers := 8
	if env["MAX_OUTBOUND_PEERS"] != "" {
		fmt.Sscanf(env["MAX_OUTBOUND_PEERS"], "%d", &maxOutboundPeers)
	}

	tlsCertFile := strings.TrimSpace(env["TLS_CERT_FILE"])
	tlsKeyFile := strings.TrimSpace(env["TLS_KEY_FILE"])
	httpsEnabled := tlsCertFile != "" && tlsKeyFile != ""
	if !httpsEnabled {
		fmt.Printf("❌ HTTPS 必須モードです。TLS_CERT_FILE と TLS_KEY_FILE を設定してください。\n")
		os.Exit(1)
	}

	tlsCheckInterval := 24 * time.Hour
	if raw := strings.TrimSpace(env["TLS_CHECK_INTERVAL"]); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			tlsCheckInterval = parsed
		} else {
			fmt.Printf("⚠ TLS_CHECK_INTERVAL の形式が不正です (%s): %v\n", raw, err)
		}
	}

	tlsRestartOnRotate := true
	if raw := strings.ToLower(strings.TrimSpace(env["TLS_RESTART_ON_ROTATE"])); raw != "" {
		tlsRestartOnRotate = raw == "1" || raw == "true" || raw == "yes" || raw == "on"
	}

	fmt.Printf("🚀 Brockchain ノード起動\n")
	fmt.Printf("  データディレクトリ: %s\n", dataDir)
	fmt.Printf("  DNS Seed: %s\n", dnsSeed)
	fmt.Printf("  リスナーホスト: %s\n", listenHost)
	fmt.Printf("  P2P ポート: %d\n", p2pPort)
	fmt.Printf("  JSON-RPC ポート: %d\n", jsonRPCPort)
	fmt.Printf("  JSON-RPC スキーム: https\n")
	fmt.Printf("  TLS 証明書: %s\n", tlsCertFile)
	fmt.Printf("  TLS 鍵: %s\n", tlsKeyFile)
	fmt.Printf("  TLS 監視間隔: %s\n", tlsCheckInterval.String())
	fmt.Printf("  TLS 差替時再起動: %t\n", tlsRestartOnRotate)

	startTLSCertificateMonitor(tlsCertFile, tlsKeyFile, tlsCheckInterval, tlsRestartOnRotate)

	if err := ensureDataDirectories(dataDir); err != nil {
		fmt.Printf("❌ データディレクトリ作成失敗: %v\n", err)
		os.Exit(1)
	}

	// Node 初期化
	node, err := NewNode(dataDir)
	if err != nil {
		fmt.Printf("❌ ノード初期化失敗: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ ノード初期化完了\n")

	// ピア管理初期化
	pm := NewPeerManager(maxOutboundPeers)
	node.SetBroadcastHandlers(
		func(block *chain.Block) {
			for _, tx := range block.Transactions {
				_ = updateUserNonce(node.dataDir, tx.From, tx.Nonce)
				if tx.Type == chain.TransactionTypeCreateToken && tx.TokenID != "" {
					_ = updateTokenStatus(node.dataDir, tx.TokenID, "confirmed")
					if tx.Owner != "" {
						_ = addTokenToUser(node.dataDir, tx.Owner, tx.TokenID)
					}
				}
			}
			broadcastAcceptedBlock(pm, block)
		},
		func(tx *chain.Transaction) { broadcastAcceptedTransaction(pm, tx) },
	)

	// JSON-RPC サーバー起動（HTTPS 固定）
	go func() {
		handler := NewJSONRPCHandler(node)
		http.Handle("/rpc", handler)
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "Brockchain JSON-RPC Server\nAccess: POST /rpc\nExample: curl -X POST https://%s:%d/rpc -H 'Content-Type: application/json' -d '{\"jsonrpc\":\"2.0\",\"method\":\"brockchain_status\",\"id\":1}'\n", listenHost, jsonRPCPort)
		})

		addr := fmt.Sprintf("%s:%d", listenHost, jsonRPCPort)
		fmt.Printf("🔒 JSON-RPC サーバー起動: https://%s/rpc\n", addr)
		if err := http.ListenAndServeTLS(addr, tlsCertFile, tlsKeyFile, nil); err != nil {
			fmt.Printf("❌ JSON-RPC HTTPS サーバーエラー: %v\n", err)
		}
	}()

	// P2P サーバー起動（インバウンド接続受け入れ）
	go func() {
		listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", listenHost, p2pPort))
		if err != nil {
			fmt.Printf("❌ P2P サーバーエラー: %v\n", err)
			return
		}
		defer listener.Close()

		fmt.Printf("🌐 P2P サーバー起動: tcp://%s:%d\n", listenHost, p2pPort)

		for {
			conn, err := listener.Accept()
			if err != nil {
				fmt.Printf("⚠ P2P 接続受け入れエラー: %v\n", err)
				continue
			}

			// インバウンド接続をピア管理に追加
			peerAddr := conn.RemoteAddr().String()
			pm.AddPeer(peerAddr, conn)

			// ピア通信処理（ブロック伝播/差分同期）
			go func(c net.Conn) {
				defer c.Close()
				defer pm.RemovePeer(c.RemoteAddr().String())
				handlePeerStream(c, c.RemoteAddr().String(), pm, node)
			}(conn)
		}
	}()

	// DNS Seed からピア取得 & アウトバウンド接続
	if dnsSeed != "" {
		go func() {
			time.Sleep(2 * time.Second) // 起動安定化待ち

			fmt.Printf("🔍 DNS Seed 照会中: %s\n", dnsSeed)
			peers := resolveDNSSeed(dnsSeed)

			// 最大アウトバウンド接続数まで接続
			connectCount := 0
			for _, peerAddr := range peers {
				if pm.GetPeerCount() >= maxOutboundPeers {
					break
				}

				conn, err := connectToPeer(peerAddr)
				if err != nil {
					fmt.Printf("⚠ ピア接続失敗 (%s): %v\n", peerAddr, err)
					continue
				}

				if pm.AddPeer(peerAddr, conn) {
					connectCount++

					// ピア通信処理（ブロック伝播/差分同期）
					go func(c net.Conn) {
						defer c.Close()
						defer pm.RemovePeer(c.RemoteAddr().String())
						handlePeerStream(c, c.RemoteAddr().String(), pm, node)
					}(conn)
				}
			}

			fmt.Printf("✓ アウトバウンド接続: %d / %d\n", connectCount, maxOutboundPeers)

			if connectCount > 0 {
				if err := startSyncFromBestPeer(node, pm, 0); err != nil {
					fmt.Printf("⚠ 同期開始スキップ: %v\n", err)
				}
			}
		}()
	}

	// 24時間ごとに接続ピアのtip高を問い合わせて差分同期する。
	go func() {
		ticker := time.NewTicker(periodicSyncInterval)
		defer ticker.Stop()

		for range ticker.C {
			if node.IsSyncing() {
				continue
			}
			if pm.GetPeerCount() == 0 {
				continue
			}
			if err := startSyncFromBestPeer(node, pm, 0); err != nil {
				fmt.Printf("⚠ 定期差分同期スキップ: %v\n", err)
			}
		}
	}()

	// REPL (CLI)
	fmt.Println("\n📝 コマンド入力可能: shutdown, status, peers, help")
	fmt.Println("==================================================")

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		cmd := strings.TrimSpace(scanner.Text())
		if cmd == "" {
			continue
		}

		parts := strings.Fields(cmd)
		command := parts[0]

		if node.IsSyncing() && command != "shutdown" && command != "status" && command != "help" {
			fmt.Println("⏳ 同期中のため、このコマンドは一時的に無効です")
			continue
		}

		switch command {
		case "shutdown":
			fmt.Println("🛑 ノード シャットダウン...")
			node.Close()
			fmt.Println("✓ クローズ完了")
			os.Exit(0)

		case "status":
			status := node.GetStatus()
			fmt.Printf("📊 ノード状態:\n")
			fmt.Printf("  高さ: %d\n", status.Height)
			fmt.Printf("  難易度: %d\n", status.Difficulty)
			fmt.Printf("  Mempool: %d TX\n", status.MempoolSize)
			fmt.Printf("  同期中: %v\n", status.IsSyncing)
			fmt.Printf("  チェーンワーク: %s\n", status.ChainWork)

		case "help":
			fmt.Println("利用可能なコマンド:")
			fmt.Println("  shutdown      - ノードをシャットダウン")
			fmt.Println("  status        - ノード状態を表示")
			fmt.Println("  peers         - ピア接続情報を表示")
			fmt.Println("  sync          - メッシュ差分同期を実行")
			fmt.Println("  mempool       - Mempool トランザクション数")
			fmt.Println("  height        - ブロックチェーン高")
			fmt.Println("  token         - token 検索")
			fmt.Println("  user          - user 検索")
			fmt.Println("  mine [diff]   - ローカルでブロックを採掘して追加")
			fmt.Println("  help          - ヘルプを表示")

		case "peers":
			peerCount := pm.GetPeerCount()
			fmt.Printf("🌐 接続ピア数: %d / %d\n", peerCount, maxOutboundPeers)

		case "sync":
			if err := startSyncFromBestPeer(node, pm, 0); err != nil {
				fmt.Printf("⚠ 同期開始不可: %v\n", err)
			}

		case "mempool":
			fmt.Printf("Mempool 内のトランザクション数: %d\n", node.GetMempoolSize())

		case "height":
			fmt.Printf("チェーン高: %d\n", node.GetChainHeight())

		case "token":
			if len(parts) < 2 {
				fmt.Println("使い方: token <token_id|name|symbol>")
				continue
			}
			query := parts[1]
			record, err := loadTokenRecord(node.dataDir, query)
			if err != nil {
				matches, searchErr := searchTokenRecords(node.dataDir, query)
				if searchErr != nil {
					fmt.Printf("❌ token 検索失敗: %v\n", searchErr)
					continue
				}
				if len(matches) == 0 {
					fmt.Println("token が見つからない")
					continue
				}
				payload, _ := json.MarshalIndent(matches, "", "  ")
				fmt.Println(string(payload))
				continue
			}
			payload, _ := json.MarshalIndent(record, "", "  ")
			fmt.Println(string(payload))

		case "user":
			if len(parts) < 2 {
				fmt.Println("使い方: user <address>")
				continue
			}
			query := parts[1]
			record, err := loadUserRecord(node.dataDir, query)
			if err != nil {
				matches, searchErr := searchUserRecords(node.dataDir, query)
				if searchErr != nil {
					fmt.Printf("❌ user 検索失敗: %v\n", searchErr)
					continue
				}
				if len(matches) == 0 {
					fmt.Println("user が見つからない")
					continue
				}
				payload, _ := json.MarshalIndent(matches, "", "  ")
				fmt.Println(string(payload))
				continue
			}
			payload, _ := json.MarshalIndent(record, "", "  ")
			fmt.Println(string(payload))

		case "mine":
			miner := "0x" + strings.Repeat("0", 40)
			difficulty := 0
			if len(parts) >= 2 {
				if parsed, err := strconv.Atoi(parts[1]); err == nil {
					difficulty = parsed
				}
			}
			if len(parts) >= 3 {
				miner = parts[2]
			}

			fmt.Printf("⛏ 採掘開始: miner=%s difficulty=%d\n", miner, difficulty)
			block, err := node.MineBlock(miner, difficulty)
			if err != nil {
				fmt.Printf("❌ 採掘失敗: %v\n", err)
				continue
			}

			if err := node.ValidateAndAddBlock(block); err != nil {
				fmt.Printf("❌ ブロック追加失敗: %v\n", err)
				continue
			}

			fmt.Printf("✓ ブロック採掘成功: height=%d hash=%s tx=%d\n", block.Height, block.Hash, len(block.Transactions))

		default:
			fmt.Printf("⚠ 不明なコマンド: %s\n", command)
		}
	}
}
