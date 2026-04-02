package mempool

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/ShudoPhysicsClub/brockchain/module/chain"
)

// Mempool はトランザクション メモリプール
type Mempool struct {
	mu          sync.RWMutex
	txs         map[string]*chain.Transaction // Hash → Transaction
	nonces      map[string]uint64             // Address → next expected nonce
	maxPoolSize int                           // 最大プール サイズ
}

// NewMempool は Mempool を初期化
func NewMempool(maxSize int) *Mempool {
	return &Mempool{
		txs:         make(map[string]*chain.Transaction),
		nonces:      make(map[string]uint64),
		maxPoolSize: maxSize,
	}
}

// calculateTxHash はトランザクションハッシュを計算
func calculateTxHash(tx *chain.Transaction) string {
	data, err := json.Marshal(tx)
	if err != nil {
		data = []byte(fmt.Sprintf("%s:%s:%s:%d:%d", tx.From, tx.To, tx.Amount, tx.Nonce, tx.Timestamp))
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// AddTransaction はトランザクションをプールに追加
func (mp *Mempool) AddTransaction(tx *chain.Transaction) error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if tx == nil {
		return errors.New("transaction is nil")
	}

	// TX ハッシュ計算
	txHash := calculateTxHash(tx)

	// 重複チェック
	if _, exists := mp.txs[txHash]; exists {
		return errors.New("transaction already exists in pool")
	}

	// プール容量チェック
	if len(mp.txs) >= mp.maxPoolSize {
		return errors.New("transaction pool is full")
	}

	// nonce の期待値を取得
	expectedNonce, _ := mp.nonces[tx.From]

	// nonce チェック（期待値と一致するか、または更新可能か）
	if tx.Nonce < expectedNonce {
		return fmt.Errorf("invalid nonce: expected >= %d, got %d", expectedNonce, tx.Nonce)
	}

	// nonce を更新（最大値を記録）
	if tx.Nonce >= expectedNonce {
		mp.nonces[tx.From] = tx.Nonce + 1
	}

	// プールに追加
	mp.txs[txHash] = tx

	return nil
}

// GetPendingTransactions はプール内の全 TX を取得
func (mp *Mempool) GetPendingTransactions() []*chain.Transaction {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	result := make([]*chain.Transaction, 0, len(mp.txs))
	for _, tx := range mp.txs {
		result = append(result, tx)
	}
	return result
}

// SelectValidTransactions は連続 nonce を持つ TX だけを選出
// ブロック作成時に使用
func (mp *Mempool) SelectValidTransactions(accountNonces map[string]uint64, maxTxPerBlock int) []*chain.Transaction {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	// アドレスごとに TX をグループ化
	byAddr := make(map[string][]*chain.Transaction)
	for _, tx := range mp.txs {
		byAddr[tx.From] = append(byAddr[tx.From], tx)
	}

	result := make([]*chain.Transaction, 0, maxTxPerBlock)

	for addr, addrTxs := range byAddr {
		// nonce でソート（簡易）
		for i := 0; i < len(addrTxs); i++ {
			for j := i + 1; j < len(addrTxs); j++ {
				if addrTxs[j].Nonce < addrTxs[i].Nonce {
					addrTxs[i], addrTxs[j] = addrTxs[j], addrTxs[i]
				}
			}
		}

		// 期待 nonce を取得
		expectedNonce := accountNonces[addr]
		seen := make(map[uint64]bool)

		for _, tx := range addrTxs {
			if len(result) >= maxTxPerBlock {
				break
			}

			// 同じ nonce の重複チェック
			if seen[tx.Nonce] {
				continue
			}
			seen[tx.Nonce] = true

			// nonce が連続しているか
			if tx.Nonce == expectedNonce {
				result = append(result, tx)
				expectedNonce++
			} else if tx.Nonce > expectedNonce {
				// 飛びがあったら以降スキップ
				break
			}
		}
	}

	return result
}

// RemoveTransaction はトランザクションをプールから削除
func (mp *Mempool) RemoveTransaction(tx *chain.Transaction) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if tx == nil {
		return
	}
	txHash := calculateTxHash(tx)
	delete(mp.txs, txHash)
}

// RemoveTransactionsByBlock はブロック内の TX をプールから削除
func (mp *Mempool) RemoveTransactionsByBlock(block *chain.Block) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if block == nil {
		return
	}

	for _, tx := range block.Transactions {
		txHash := calculateTxHash(&tx)
		delete(mp.txs, txHash)
	}
}

// Size はプール内の TX 数を返す
func (mp *Mempool) Size() int {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return len(mp.txs)
}

// SetAccountNonce はアカウントの次の nonce を設定
func (mp *Mempool) SetAccountNonce(address string, nonce uint64) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	mp.nonces[address] = nonce
}

// GetAccountNonce はアカウントの次の nonce を取得
func (mp *Mempool) GetAccountNonce(address string) uint64 {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return mp.nonces[address]
}

// Clear はプールをクリア
func (mp *Mempool) Clear() {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	mp.txs = make(map[string]*chain.Transaction)
	mp.nonces = make(map[string]uint64)
}
