package network

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// AppType はアプリケーションタイプを表す
type AppType string

const (
	AppTypeChain   AppType = "chain" // ブロックチェーン（TTL=255）
	AppTypeGeneral AppType = "mail"  // 汎用 API（TTL=64）
)

// MessageType はネットワークメッセージの種類を表す
type MessageType string

const (
	MessageTypeHandshake   MessageType = "handshake"
	MessageTypeBlock       MessageType = "block"
	MessageTypeTransaction MessageType = "transaction"
	MessageTypeMessage     MessageType = "message" // 汎用メッセージ
	MessageTypeAppRegister MessageType = "app_register"
)

// Message は P2P ネットワーク上で送受信される基本メッセージ
type Message struct {
	Type      MessageType     `json:"type"`
	Timestamp int64           `json:"timestamp"`
	Sender    string          `json:"sender"` // ウォレットアドレス
	To        string          `json:"to"`     // 宛先アドレス（""=ブロードキャスト）
	App       AppType         `json:"app"`    // アプリケーション名
	Payload   json.RawMessage `json:"payload"`
	RequestID string          `json:"request_id"` // ループ防止用 UUID
	TTL       int             `json:"ttl"`        // 残りホップ数
}

// AppRegistration はアプリケーション登録時のペイロード
type AppRegistration struct {
	AppName   string `json:"app_name"`
	Challenge string `json:"challenge"` // ランダムチャレンジ
	Signature string `json:"signature"` // チャレンジへの署名
}

// ClientInfo は接続中のクライアント情報
type ClientInfo struct {
	Address   string    `json:"address"`
	AppName   string    `json:"app_name"`
	Connected time.Time `json:"connected"`
}

// Peer はネットワーク上の他のノード情報
type Peer struct {
	ID        string    `json:"id"`
	Address   string    `json:"address"`
	Port      int       `json:"port"`
	LastSeen  time.Time `json:"last_seen"`
	Connected bool      `json:"connected"`
}

// HandshakePayload はハンドシェイク時に送信される情報
type HandshakePayload struct {
	NodeID    string `json:"node_id"`
	Version   string `json:"version"`
	Port      int    `json:"port"`
	Timestamp int64  `json:"timestamp"`
}

// MessageHandler はメッセージ受信時に呼ばれるコールバック
type MessageHandler func(msg *Message, peer *Peer) error

// Network はノードのP2P通信を管理する
type Network struct {
	mu              sync.RWMutex
	nodeID          string
	port            int
	peers           map[string]*Peer
	messageHandlers map[MessageType]MessageHandler
	appHandlers     map[AppType]MessageHandler
	localClients    map[string]*ClientInfo // アドレス → クライアント情報
	seenMessages    map[string]time.Time   // RequestID → 最後に見た時刻（ループ防止）
	listener        net.Listener
	connChan        chan net.Conn
	stopChan        chan struct{}
	isRunning       bool
}

// NewNetwork は新しいネットワークインスタンスを作成
func NewNetwork(nodeID string, port int) *Network {
	return &Network{
		nodeID:          nodeID,
		port:            port,
		peers:           make(map[string]*Peer),
		messageHandlers: make(map[MessageType]MessageHandler),
		appHandlers:     make(map[AppType]MessageHandler),
		localClients:    make(map[string]*ClientInfo),
		seenMessages:    make(map[string]time.Time),
		connChan:        make(chan net.Conn, 100),
		stopChan:        make(chan struct{}),
		isRunning:       false,
	}
}

// RegisterMessageHandler はメッセージタイプと対応するハンドラーを登録
func (n *Network) RegisterMessageHandler(msgType MessageType, handler MessageHandler) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.messageHandlers[msgType] = handler
}

// RegisterAppHandler はアプリケーションタイプと対応するハンドラーを登録
func (n *Network) RegisterAppHandler(appType AppType, handler MessageHandler) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.appHandlers[appType] = handler
}

// getInitialTTL はアプリケーション種別に応じた初期 TTL を返す
func getInitialTTL(app AppType) int {
	switch app {
	case AppTypeChain:
		return 255 // ブロックチェーン：全ネットワーク
	case AppTypeGeneral:
		return 64 // 汎用 API：負荷軽減
	default:
		return 64
	}
}

// generateRequestID はループ防止用のランダムな RequestID を生成
func generateRequestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Start はリスナーを開始してP2P通信を開始
func (n *Network) Start() error {
	n.mu.Lock()
	if n.isRunning {
		n.mu.Unlock()
		return errors.New("network already running")
	}
	n.mu.Unlock()

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", n.port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", n.port, err)
	}

	n.mu.Lock()
	n.listener = listener
	n.isRunning = true
	n.mu.Unlock()

	go n.acceptConnections()
	go n.handleConnections()

	return nil
}

// Stop はネットワークをシャットダウン
func (n *Network) Stop() error {
	n.mu.Lock()
	if !n.isRunning {
		n.mu.Unlock()
		return errors.New("network not running")
	}

	close(n.stopChan)
	listener := n.listener
	n.isRunning = false
	n.mu.Unlock()

	if listener != nil {
		return listener.Close()
	}
	return nil
}

// acceptConnections は新しい接続を受け付ける
func (n *Network) acceptConnections() {
	for {
		select {
		case <-n.stopChan:
			return
		default:
		}

		conn, err := n.listener.Accept()
		if err != nil {
			return
		}

		select {
		case n.connChan <- conn:
		case <-n.stopChan:
			conn.Close()
			return
		}
	}
}

// handleConnections は接続されたコネクションでメッセージを受信
func (n *Network) handleConnections() {
	for {
		select {
		case conn := <-n.connChan:
			go n.handleConnection(conn)
		case <-n.stopChan:
			return
		}
	}
}

// handleConnection は単一の接続でメッセージを処理
func (n *Network) handleConnection(conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	var msg Message

	err := decoder.Decode(&msg)
	if err != nil {
		return
	}

	// ハンドシェイクまたはメッセージハンドラーを実行
	if msg.Type == MessageTypeHandshake {
		n.handleHandshake(&msg)
	} else {
		n.mu.RLock()
		handler, exists := n.messageHandlers[msg.Type]
		n.mu.RUnlock()

		if exists {
			peer, _ := n.GetPeer(msg.Sender)
			handler(&msg, peer)
		}
	}
}

// handleHandshake はハンドシェイクメッセージを処理
func (n *Network) handleHandshake(msg *Message) {
	var payload HandshakePayload
	json.Unmarshal(msg.Payload, &payload)

	peer := &Peer{
		ID:        payload.NodeID,
		Address:   "127.0.0.1", // TODO: 実際の接続元から取得
		Port:      payload.Port,
		LastSeen:  time.Now(),
		Connected: true,
	}

	n.AddPeer(peer)
}

// AddPeer はピアをネットワークに追加
func (n *Network) AddPeer(peer *Peer) {
	n.mu.Lock()
	defer n.mu.Unlock()
	peer.LastSeen = time.Now()
	peer.Connected = true
	n.peers[peer.ID] = peer
}

// RemovePeer はピアをネットワークから削除
func (n *Network) RemovePeer(peerID string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if _, exists := n.peers[peerID]; !exists {
		return fmt.Errorf("peer %s not found", peerID)
	}

	delete(n.peers, peerID)
	return nil
}

// GetPeer はピア情報を取得
func (n *Network) GetPeer(peerID string) (*Peer, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	peer, exists := n.peers[peerID]
	if !exists {
		return nil, fmt.Errorf("peer %s not found", peerID)
	}

	return peer, nil
}

// GetAllPeers はすべてのピア情報を取得
func (n *Network) GetAllPeers() []*Peer {
	n.mu.RLock()
	defer n.mu.RUnlock()

	peers := make([]*Peer, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, p)
	}
	return peers
}

// Broadcast はメッセージをすべてのピアに送信
func (n *Network) Broadcast(msg *Message) error {
	if msg.Sender == "" {
		msg.Sender = n.nodeID
	}
	if msg.Timestamp == 0 {
		msg.Timestamp = time.Now().Unix()
	}

	n.mu.RLock()
	peers := make([]*Peer, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, p)
	}
	n.mu.RUnlock()

	for _, peer := range peers {
		go n.SendMessageToPeer(peer, msg)
	}

	return nil
}

// SendMessageToPeer はメッセージを特定のピアに送信
func (n *Network) SendMessageToPeer(peer *Peer, msg *Message) error {
	if msg.Sender == "" {
		msg.Sender = n.nodeID
	}
	if msg.Timestamp == 0 {
		msg.Timestamp = time.Now().Unix()
	}

	addr := net.JoinHostPort(peer.Address, fmt.Sprintf("%d", peer.Port))
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		peer.Connected = false
		return fmt.Errorf("failed to connect to peer %s: %w", peer.ID, err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	err = encoder.Encode(msg)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	peer.LastSeen = time.Now()
	peer.Connected = true

	return nil
}

// ConnectToPeer は指定されたアドレスのピアに接続
func (n *Network) ConnectToPeer(peerID, address string, port int) error {
	peer := &Peer{
		ID:        peerID,
		Address:   address,
		Port:      port,
		LastSeen:  time.Now(),
		Connected: false,
	}

	// ハンドシェイク送信
	handshake := HandshakePayload{
		NodeID:    n.nodeID,
		Version:   "1.0",
		Port:      n.port,
		Timestamp: time.Now().Unix(),
	}

	payload, _ := json.Marshal(handshake)
	msg := &Message{
		Type:    MessageTypeHandshake,
		Sender:  n.nodeID,
		Payload: payload,
	}

	err := n.SendMessageToPeer(peer, msg)
	if err != nil {
		return err
	}

	n.AddPeer(peer)
	return nil
}

// GetNodeID はこのノードのIDを返す
func (n *Network) GetNodeID() string {
	return n.nodeID
}

// PeerCount は接続されているピアの数を返す
func (n *Network) PeerCount() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.peers)
}

// BroadcastMessage はメッセージをブロードキャストする（TTL付き）
func (n *Network) BroadcastMessage(app AppType, sender, payload string) error {
	requestID := generateRequestID()
	ttl := getInitialTTL(app)

	msg := &Message{
		Type:      MessageTypeMessage,
		Timestamp: time.Now().Unix(),
		Sender:    sender,
		To:        "", // ブロードキャスト
		App:       app,
		Payload:   json.RawMessage(payload),
		RequestID: requestID,
		TTL:       ttl,
	}

	return n.RelayMessage(msg)
}

// SendToAddress はアドレス宛のメッセージを送信
func (n *Network) SendToAddress(app AppType, sender, to, payload string) error {
	requestID := generateRequestID()
	ttl := getInitialTTL(app)

	msg := &Message{
		Type:      MessageTypeMessage,
		Timestamp: time.Now().Unix(),
		Sender:    sender,
		To:        to,
		App:       app,
		Payload:   json.RawMessage(payload),
		RequestID: requestID,
		TTL:       ttl,
	}

	// 自分のクライアント一覧をチェック
	n.mu.RLock()
	client, exists := n.localClients[to]
	n.mu.RUnlock()

	if exists {
		// ローカルクライアントに転送 → 実装は後で
		_ = client
	} else {
		// TTL > 0 ならリレー
		return n.RelayMessage(msg)
	}

	return nil
}

// RelayMessage はメッセージをピアにリレー（TTL管理、ループ防止）
func (n *Network) RelayMessage(msg *Message) error {
	n.mu.Lock()

	// タイムスタンプ検証（汎用メッセージのみ）
	if msg.Type == MessageTypeMessage {
		now := time.Now().Unix()
		if now-msg.Timestamp > 30 || msg.Timestamp-now > 30 {
			// 30秒以上古い or 未来 → ドロップ
			n.mu.Unlock()
			return nil
		}
	}
	// ブロック・トランザクションはタイムスタンプチェック不要

	// ループ防止：同じ RequestID を見たか確認
	if _, seen := n.seenMessages[msg.RequestID]; seen {
		n.mu.Unlock()
		return nil // ドロップ
	}

	// RequestID を記録（30秒後に自動削除）
	n.seenMessages[msg.RequestID] = time.Now()
	go func() {
		time.Sleep(30 * time.Second)
		n.mu.Lock()
		delete(n.seenMessages, msg.RequestID)
		n.mu.Unlock()
	}()

	// TTL デクリメント
	msg.TTL--
	if msg.TTL <= 0 {
		n.mu.Unlock()
		return nil // TTL 切れ
	}

	peers := make([]*Peer, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, p)
	}
	n.mu.Unlock()

	// 全ピアにリレー
	for _, peer := range peers {
		go n.SendMessageToPeer(peer, msg)
	}

	return nil
}

// RegisterLocalClient はローカルクライアントを登録
func (n *Network) RegisterLocalClient(address, appName string) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if _, exists := n.localClients[address]; exists {
		return "", fmt.Errorf("client %s already registered", address)
	}

	n.localClients[address] = &ClientInfo{
		Address:   address,
		AppName:   appName,
		Connected: time.Now(),
	}

	return "registered", nil
}

// UnregisterLocalClient はローカルクライアントを登録解除
func (n *Network) UnregisterLocalClient(address string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if _, exists := n.localClients[address]; !exists {
		return fmt.Errorf("client %s not found", address)
	}

	delete(n.localClients, address)
	return nil
}
