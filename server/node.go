package main

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ShudoPhysicsClub/brockchain/module/chain"
	"github.com/ShudoPhysicsClub/brockchain/module/mempool"
	"github.com/ShudoPhysicsClub/brockchain/module/network"
)

// Node はネットワークノード（network + chain + mempool 統合）
type Node struct {
	mu              sync.RWMutex
	bc              *chain.Blockchain
	mp              *mempool.Mempool
	net             *network.Network
	seenBlocks      map[string]struct{}
	onBlockAccepted func(*chain.Block)
	onTxAccepted    func(*chain.Transaction)
	dataDir         string
	isSyncing       bool
	syncTimeout     time.Duration
}

// NewNode は Node を初期化
func NewNode(dataDir string) (*Node, error) {
	// Blockchain 初期化
	bc, err := chain.NewBlockchain(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize blockchain: %w", err)
	}

	// ブロックチェーンをディスクから復元
	if err := bc.LoadChain(); err != nil {
		return nil, fmt.Errorf("failed to load chain from disk: %w", err)
	}

	// Mempool 初期化
	mp := mempool.NewMempool(10000) // 最大 10,000 TX

	node := &Node{
		bc:          bc,
		mp:          mp,
		net:         nil,
		seenBlocks:  make(map[string]struct{}),
		dataDir:     dataDir,
		isSyncing:   false,
		syncTimeout: 30 * time.Second,
	}

	return node, nil
}

// ============================================================
// トランザクション処理
// ============================================================

// SubmitTransaction はトランザクションをプール・ネットワークに提出
func (n *Node) SubmitTransaction(tx *chain.Transaction) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if tx != nil {
		tx.Gas = chain.DefaultGasFee
	}

	// メモリプールに追加
	if err := n.mp.AddTransaction(tx); err != nil {
		return err
	}

	// ネットワーク全体にブロードキャスト（TTL=64 一般 API）
	// TBD: network.BroadcastMessage() 呼び出し

	return nil
}

// ValidateAndAddTransaction はトランザクション検証→追加（受信時）
func (n *Node) ValidateAndAddTransaction(tx *chain.Transaction) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if tx != nil {
		tx.Gas = chain.DefaultGasFee
	}

	// TX 署名検証
	if err := n.bc.ValidateTransaction(tx); err != nil {
		return fmt.Errorf("transaction validation failed: %w", err)
	}

	// メモリプールに追加
	if err := n.mp.AddTransaction(tx); err != nil {
		return fmt.Errorf("failed to add transaction to mempool: %w", err)
	}

	if n.onTxAccepted != nil {
		n.onTxAccepted(tx)
	}

	return nil
}

// ============================================================
// ブロック処理
// ============================================================

// ValidateAndAddBlock はブロック検証→チェーン追加（受信時）
func (n *Node) ValidateAndAddBlock(block *chain.Block) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if block == nil {
		return errors.New("block is nil")
	}

	if _, exists := n.seenBlocks[block.Hash]; exists {
		return errors.New("block already processed")
	}

	// ブロック検証
	if err := n.bc.ValidateBlock(block); err != nil {
		return fmt.Errorf("block validation failed: %w", err)
	}

	// チェーンに追加
	if err := n.bc.AddBlock(block); err != nil {
		return fmt.Errorf("failed to add block to chain: %w", err)
	}

	// ディスク保存
	if err := n.bc.SaveBlockToDisk(block); err != nil {
		return fmt.Errorf("failed to save block to disk: %w", err)
	}

	// ブロック内の TX をメモリプールから削除
	n.mp.RemoveTransactionsByBlock(block)

	// 受信済みブロックとして記録
	n.seenBlocks[block.Hash] = struct{}{}

	if n.onBlockAccepted != nil {
		n.onBlockAccepted(block)
	}

	return nil
}

// AddSyncBlock は受信ブロックを一時保存（同期時）
func (n *Node) AddSyncBlock(block *chain.Block) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if block == nil {
		return errors.New("block is nil")
	}

	if _, exists := n.seenBlocks[block.Hash]; exists {
		return nil
	}

	// sync/ に一時保存
	if err := n.bc.AddSyncBlock(block); err != nil {
		return fmt.Errorf("failed to add sync block: %w", err)
	}

	n.seenBlocks[block.Hash] = struct{}{}

	return nil
}

// FinalizeSyncBlocks は同期完了後、sync/ をマージ
func (n *Node) FinalizeSyncBlocks() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	// sync/ ブロックの読込・検証・追加
	if err := n.bc.FinalizeSyncBlocks(); err != nil {
		return fmt.Errorf("failed to finalize sync blocks: %w", err)
	}

	// 採用済みブロックだけをその後ブロードキャストする流れにする

	return nil
}

// SetBroadcastHandlers は検証済みデータの通知先を設定する
func (n *Node) SetBroadcastHandlers(onBlock func(*chain.Block), onTx func(*chain.Transaction)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onBlockAccepted = onBlock
	n.onTxAccepted = onTx
}

// MineBlock はメモリプールの未処理TXからブロックを組み立てて簡易PoWを実行する
func (n *Node) MineBlock(miner string, difficulty int) (*chain.Block, error) {
	n.mu.RLock()
	latest := n.bc.GetLatestBlock()
	pending := n.mp.GetPendingTransactions()
	currentDifficulty := n.bc.GetCurrentDifficulty()
	n.mu.RUnlock()

	if miner == "" {
		return nil, errors.New("miner address is empty")
	}

	if difficulty <= 0 {
		difficulty = currentDifficulty
		if difficulty > 18 {
			difficulty = 18
		}
	}

	height := uint64(1)
	prevHash := strings.Repeat("0", 64)
	if latest != nil {
		height = latest.Height + 1
		prevHash = latest.Hash
	}

	transactions := make([]chain.Transaction, len(pending))
	for i, tx := range pending {
		if tx != nil {
			transactions[i] = *tx
		}
	}

	block := &chain.Block{
		Height:       height,
		PreviousHash: prevHash,
		Timestamp:    time.Now().Unix(),
		Nonce:        0,
		Difficulty:   difficulty,
		Miner:        miner,
		Reward:       "0",
		Transactions: transactions,
	}

	for nonce := uint64(0); ; nonce++ {
		block.Nonce = nonce
		block.Hash = n.bc.CalculateBlockHash(block)
		if n.bc.CheckPoW(block.Hash, difficulty) {
			return block, nil
		}
	}
}

// ============================================================
// チェーン情報取得
// ============================================================

// GetChainHeight はチェーン高を取得
func (n *Node) GetChainHeight() uint64 {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.bc.GetChainHeight()
}

// GetCurrentDifficulty は現在の難易度を取得
func (n *Node) GetCurrentDifficulty() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.bc.GetCurrentDifficulty()
}

// GetLatestBlock は最新ブロックを取得
func (n *Node) GetLatestBlock() *chain.Block {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.bc.GetLatestBlock()
}

// GetBlock は高さでブロックを取得
func (n *Node) GetBlock(height uint64) *chain.Block {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.bc.GetBlockByHeight(height)
}

// ============================================================
// メモリプール情報
// ============================================================

// GetMempoolSize はメモリプールのサイズを取得
func (n *Node) GetMempoolSize() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.mp.Size()
}

// GetPendingTransactions は未処理 TX を取得（mining 用）
func (n *Node) GetPendingTransactions(maxCount int) []*chain.Transaction {
	n.mu.RLock()
	defer n.mu.RUnlock()

	// メモリプールから連続 nonce を持つ TX を選出
	accountNonces := make(map[string]uint64)
	// TBD: state から account nonce を取得

	return n.mp.SelectValidTransactions(accountNonces, maxCount)
}

// ============================================================
// バリデーション
// ============================================================

// IsValidTransaction はトランザクションが有効か判定（ローカルチェック）
func (n *Node) IsValidTransaction(tx *chain.Transaction) bool {
	if err := n.bc.ValidateTransaction(tx); err != nil {
		return false
	}

	// mempoolに既に存在しないか
	if txs := n.mp.GetPendingTransactions(); len(txs) > 0 {
		// TBD: 重複チェック
	}

	return true
}

// IsValidBlock はブロックが有効か判定（追加前チェック）
func (n *Node) IsValidBlock(block *chain.Block) bool {
	if err := n.bc.ValidateBlock(block); err != nil {
		return false
	}
	return true
}

// ============================================================
// 同期管理
// ============================================================

// StartSync は同期開始
func (n *Node) StartSync(fromHeight uint64) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.isSyncing {
		return errors.New("sync already in progress")
	}

	n.isSyncing = true

	// TBD: ネットワークからブロック要求開始
	// network.RequestChain(fromHeight)

	return nil
}

// StopSync は同期停止
func (n *Node) StopSync() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.isSyncing = false
}

// IsSyncing は同期中か判定
func (n *Node) IsSyncing() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.isSyncing
}

// ============================================================
// ノード状態
// ============================================================

// Status はノード状態を返す
type Status struct {
	Height      uint64
	Difficulty  int
	MempoolSize int
	IsSyncing   bool
	ChainWork   string
}

// GetStatus はノード状態を取得
func (n *Node) GetStatus() Status {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return Status{
		Height:      n.bc.GetChainHeight(),
		Difficulty:  n.bc.GetCurrentDifficulty(),
		MempoolSize: n.mp.Size(),
		IsSyncing:   n.isSyncing,
		ChainWork:   n.bc.CalculateChainWork().String(),
	}
}

// Close はノードをシャットダウン
func (n *Node) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	// リソースクリーンアップ
	// TBD: network.Close()
	// TBD: メモリプール保存

	return nil
}
