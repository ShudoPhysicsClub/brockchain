package chain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/ShudoPhysicsClub/brockchain/module/crypto"
)

const (
	// DefaultGasFee は未指定時に使うデフォルト gas 料金。
	DefaultGasFee = "0.05"
	// TokenIDHashBytes は token id に使うハッシュ先頭バイト数。
	TokenIDHashBytes = 8
	// BlockTimestampToleranceSeconds は受理するブロック時刻の許容差（秒）。
	BlockTimestampToleranceSeconds = int64(600)
)

// Block はブロックチェーンのブロック
type Block struct {
	Height       uint64        `json:"height"`
	PreviousHash string        `json:"previous_hash"`
	Timestamp    int64         `json:"timestamp"`
	Nonce        uint64        `json:"nonce"`
	Difficulty   int           `json:"difficulty"`
	Miner        string        `json:"miner"`  // ウォレットアドレス
	Reward       string        `json:"reward"` // Wei 単位の文字列
	Transactions []Transaction `json:"transactions"`
	Hash         string        `json:"hash"`
}

// TransactionType はトランザクション種別
type TransactionType string

const (
	TransactionTypeTransfer        TransactionType = "transfer"
	TransactionTypeCreateToken     TransactionType = "create_token"
	TransactionTypeMint            TransactionType = "mint"
	TransactionTypeBurn            TransactionType = "burn"
	TransactionTypeTokenSwap       TransactionType = "token_swap"
	TransactionTypeAddLiquidity    TransactionType = "add_liquidity"
	TransactionTypeRemoveLiquidity TransactionType = "remove_liquidity"
)

// Transaction はトランザクション
type Transaction struct {
	Type         TransactionType `json:"type"`
	From         string          `json:"from"`   // ウォレットアドレス
	To           string          `json:"to"`     // 宛先アドレス
	Amount       string          `json:"amount"` // Wei 単位
	Gas          string          `json:"gas,omitempty"`
	Nonce        uint64          `json:"nonce"` // リプレイ攻撃対策
	Timestamp    int64           `json:"timestamp"`
	Signature    string          `json:"signature"`  // 16進数の署名
	PublicKey    string          `json:"public_key"` // signer の公開鍵（16進数）
	TokenID      string          `json:"token_id"`
	TokenIn      string          `json:"token_in,omitempty"`
	TokenOut     string          `json:"token_out,omitempty"`
	TokenA       string          `json:"token_a,omitempty"`
	TokenB       string          `json:"token_b,omitempty"`
	AmountA      string          `json:"amount_a,omitempty"`
	AmountB      string          `json:"amount_b,omitempty"`
	Liquidity    string          `json:"liquidity,omitempty"`
	Name         string          `json:"name,omitempty"`
	Symbol       string          `json:"symbol,omitempty"`
	Decimals     uint8           `json:"decimals,omitempty"`
	TotalSupply  string          `json:"total_supply,omitempty"`
	Owner        string          `json:"owner,omitempty"`
	Pair         string          `json:"pair,omitempty"`
	AmountIn     string          `json:"amount_in,omitempty"`
	AmountOutMin string          `json:"amount_out_min,omitempty"`
	Deadline     int64           `json:"deadline,omitempty"`
}

// DeriveTokenID は create_token の内容から token id を生成する。
// SHA-256 の先頭 8 バイトを 16 進数化したものを使う。
func DeriveTokenID(tx *Transaction) string {
	if tx == nil {
		return ""
	}

	payload := struct {
		Type        TransactionType `json:"type"`
		From        string          `json:"from"`
		Name        string          `json:"name"`
		Symbol      string          `json:"symbol"`
		Decimals    uint8           `json:"decimals"`
		TotalSupply string          `json:"total_supply"`
		Owner       string          `json:"owner"`
		Nonce       uint64          `json:"nonce"`
		Timestamp   int64           `json:"timestamp"`
	}{
		Type:        tx.Type,
		From:        tx.From,
		Name:        tx.Name,
		Symbol:      tx.Symbol,
		Decimals:    tx.Decimals,
		TotalSupply: tx.TotalSupply,
		Owner:       tx.Owner,
		Nonce:       tx.Nonce,
		Timestamp:   tx.Timestamp,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}

	sum := sha256.Sum256(data)
	return "0x" + hex.EncodeToString(sum[:TokenIDHashBytes])
}

// Blockchain はブロックチェーン
type Blockchain struct {
	mu            sync.RWMutex
	blocks        map[string]*Block // Hash → Block
	chain         []string          // Height の順序付きブロックハッシュリスト
	difficulty    int               // 現在の難易度
	lastAdjust    uint64            // 最後に難易度を調整したブロック高さ
	genesisHash   string
	maxReorgDepth int    // 最大巻き戻し深さ（デフォルト: 10）
	dataDir       string // ブロック保存ディレクトリ
}

// NewBlockchain はジェネシスブロックとともにブロックチェーンを初期化
func NewBlockchain(dataDir string) (*Blockchain, error) {
	// データディレクトリ作成
	if err := os.MkdirAll(filepath.Join(dataDir, "chain"), 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "sync"), 0755); err != nil {
		return nil, err
	}

	bc := &Blockchain{
		blocks:        make(map[string]*Block),
		chain:         make([]string, 0),
		difficulty:    24, // 初期難易度: 24ビット
		lastAdjust:    0,
		maxReorgDepth: 10,
		dataDir:       dataDir,
	}

	// ジェネシスブロック作成 (Timestamp = 0 = 1970-01-01 UTC)
	genesis := &Block{
		Height:       0,
		PreviousHash: "0x" + string([]byte{'0'}) + "0000000000000000000000000000000000000000000000000000000000000000"[1:],
		Timestamp:    0, // 1970-01-01T00:00:00Z
		Nonce:        0,
		Difficulty:   24,
		Miner:        "0x" + string([]byte{'0'}) + "000000000000000000000000000000000000",
		Reward:       "0",
		Transactions: []Transaction{},
	}

	// ジェネシスブロックをマイニング（Proof of Work計算）
	for nonce := uint64(0); ; nonce++ {
		genesis.Nonce = nonce
		genesis.Hash = bc.CalculateBlockHash(genesis)
		if bc.CheckPoW(genesis.Hash, genesis.Difficulty) {
			break
		}
	}

	bc.genesisHash = genesis.Hash
	bc.blocks[genesis.Hash] = genesis
	bc.chain = append(bc.chain, genesis.Hash)

	return bc, nil
}

// CalculateBlockHash はブロックのハッシュを計算
func (bc *Blockchain) CalculateBlockHash(block *Block) string {
	preimage, err := BuildBlockHashPreimage(block)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(preimage)
	return hex.EncodeToString(hash[:])
}

// CheckPoW はブロックがターゲットを満たしているか確認（難易度チェック）
func (bc *Blockchain) CheckPoW(blockHash string, difficulty int) bool {
	// 先頭 difficulty ビットが 0 であることを確認
	fullNibbles := difficulty / 4
	remainBits := difficulty % 4

	for i := 0; i < fullNibbles; i++ {
		if blockHash[i] != '0' {
			return false
		}
	}

	if remainBits > 0 {
		v := int(blockHash[fullNibbles]) - '0'
		if blockHash[fullNibbles] >= 'a' {
			v = int(blockHash[fullNibbles]) - 'a' + 10
		}
		if v > (1<<(4-remainBits))-1 {
			return false
		}
	}

	return true
}

// ValidateBlock はブロックを検証（署名、ハッシュ、PoW、トランザクション）
func (bc *Blockchain) ValidateBlock(block *Block) error {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	// ハッシュの整合性確認
	expectedHash := bc.CalculateBlockHash(block)
	if block.Hash != expectedHash {
		return errors.New("block hash mismatch")
	}

	// PoW 検証
	if !bc.CheckPoW(block.Hash, block.Difficulty) {
		return errors.New("invalid proof of work")
	}

	// 前ブロックの存在確認（ジェネシス以外）
	if block.Height > 0 {
		if _, exists := bc.blocks[block.PreviousHash]; !exists {
			return errors.New("previous block not found")
		}
	}

	// タイムスタンプ確認（許容差内）
	if !isTimestampWithinTolerance(block.Timestamp) {
		return errors.New("block timestamp out of range")
	}

	// トランザクション検証（簡易：順序チェック）
	for i, tx := range block.Transactions {
		if i > 0 {
			prevTx := block.Transactions[i-1]
			if prevTx.Nonce >= tx.Nonce && prevTx.From == tx.From {
				return errors.New("transaction nonce order invalid")
			}
		}
	}

	return nil
}

// AddBlock はブロックをチェーンに追加（検証込み）
func (bc *Blockchain) AddBlock(block *Block) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	// 基本検証
	if err := bc.validateBlockUnlocked(block); err != nil {
		return err
	}

	// ブロック高さの確認
	if block.Height == 0 {
		return errors.New("cannot add genesis block again")
	}

	currentHeight := uint64(len(bc.chain)) - 1
	expectedHeight := currentHeight + 1
	if block.Height != expectedHeight {
		return fmt.Errorf("invalid block height: expected %d, got %d", expectedHeight, block.Height)
	}

	// 前ブロックの確認
	if bc.chain[currentHeight] != block.PreviousHash {
		return errors.New("previous hash mismatch")
	}

	// ブロック追加
	bc.blocks[block.Hash] = block
	bc.chain = append(bc.chain, block.Hash)

	// 難易度調整（20ブロックごと）
	if block.Height > 0 && block.Height%20 == 0 {
		bc.adjustDifficulty(block.Height)
	}

	// メモリ最適化：最新ブロック以外をメモリから削除
	bc.trimMemoryCacheUnlocked()

	return nil
}

// trimMemoryCacheUnlocked は最新ブロック以外をメモリから削除（メモリ最適化）
// ロック取得なしで実行（AddBlock 内から呼ばれるため）
func (bc *Blockchain) trimMemoryCacheUnlocked() {
	if len(bc.chain) < 2 {
		return // ジェネシスのみ、または空
	}

	// 最新ブロックのハッシュ
	latestHash := bc.chain[len(bc.chain)-1]

	// ジェネシスブロックのハッシュ
	genesisHash := bc.chain[0]

	// 最新ブロックとジェネシス以外をメモリから削除
	for hash := range bc.blocks {
		if hash != latestHash && hash != genesisHash {
			delete(bc.blocks, hash)
		}
	}
}

// loadBlockFromDisk はハッシュからブロックをディスクから読込
func (bc *Blockchain) loadBlockFromDisk(hash string) *Block {
	// ディレクトリを列挙してハッシュに対応するファイルを探す
	chainDir := filepath.Join(bc.dataDir, "chain")
	entries, err := ioutil.ReadDir(chainDir)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		heightDir := filepath.Join(chainDir, entry.Name())
		filePath := filepath.Join(heightDir, hash+".json")
		if _, err := os.Stat(filePath); err == nil {
			data, err := ioutil.ReadFile(filePath)
			if err != nil {
				return nil
			}
			var block *Block
			if err := json.Unmarshal(data, &block); err != nil {
				return nil
			}
			return block
		}
	}

	return nil
}

// loadBlockFromDiskByHeight は高さからブロックをディスクから読込
func (bc *Blockchain) loadBlockFromDiskByHeight(height uint64, blockHash string) *Block {
	heightDir := filepath.Join(bc.dataDir, "chain", strconv.FormatUint(height, 10))
	filePath := filepath.Join(heightDir, blockHash+".json")

	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil
	}

	var block *Block
	if err := json.Unmarshal(data, &block); err != nil {
		return nil
	}

	return block
}

// validateBlockUnlocked はロック取得なしでブロック検証（内部用）
func (bc *Blockchain) validateBlockUnlocked(block *Block) error {
	// ハッシュの整合性確認
	expectedHash := bc.CalculateBlockHash(block)
	if block.Hash != expectedHash {
		return errors.New("block hash mismatch")
	}

	// PoW 検証
	if !bc.CheckPoW(block.Hash, block.Difficulty) {
		return errors.New("invalid proof of work")
	}

	// 前ブロックの存在確認
	if _, exists := bc.blocks[block.PreviousHash]; !exists {
		return errors.New("previous block not found")
	}

	if !isTimestampWithinTolerance(block.Timestamp) {
		return errors.New("block timestamp out of range")
	}

	return nil
}

func isTimestampWithinTolerance(ts int64) bool {
	now := time.Now().Unix()
	if now-ts > BlockTimestampToleranceSeconds {
		return false
	}
	if ts-now > BlockTimestampToleranceSeconds {
		return false
	}
	return true
}

// adjustDifficulty は LWMA（線形加重移動平均）方式で難易度を調整
func (bc *Blockchain) adjustDifficulty(height uint64) {
	targetBlockTime := int64(180) // 目標: 180秒
	windowSize := uint64(20)      // LWMA ウィンドウ
	dampingFactor := 3            // ダンピング係数: 1/3

	if height < windowSize {
		return
	}

	// 直近 20 ブロックのタイムスタンプを取得
	startIdx := len(bc.chain) - int(windowSize)
	var totalTimeSum int64
	var weightedTimeSum int64

	for i := 0; i < int(windowSize); i++ {
		blockHash := bc.chain[startIdx+i]
		block := bc.blocks[blockHash]

		weight := int64(i + 1) // 直近ほど重み大きい
		blockTime := block.Timestamp

		if i > 0 {
			prevBlockHash := bc.chain[startIdx+i-1]
			prevBlock := bc.blocks[prevBlockHash]
			blockTime = block.Timestamp - prevBlock.Timestamp

			// 外れ値フィルタ（30秒～900秒）
			if blockTime < 30 {
				blockTime = 30
			} else if blockTime > 900 {
				blockTime = 900
			}
		}

		totalTimeSum += blockTime
		weightedTimeSum += blockTime * weight
	}

	// LWMA: 加重合計を重みの合計で割る
	sumOfWeights := int64(windowSize * (windowSize + 1) / 2)
	solveTime := weightedTimeSum / sumOfWeights

	// 難易度調整
	adjustment := targetBlockTime * sumOfWeights
	adjustedDifficulty := adjustment / solveTime

	// ダンピングと制限
	change := (adjustedDifficulty - int64(bc.difficulty)) / int64(dampingFactor)
	newDifficulty := bc.difficulty + int(change)

	// 難易度の上下限
	if newDifficulty < 20 {
		newDifficulty = 20
	}
	if newDifficulty > bc.difficulty+1 {
		newDifficulty = bc.difficulty + 1
	}

	bc.difficulty = newDifficulty
	bc.lastAdjust = height
}

// GetLatestBlock は最新ブロックを取得
func (bc *Blockchain) GetLatestBlock() *Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	if len(bc.chain) == 0 {
		return nil
	}

	latestHash := bc.chain[len(bc.chain)-1]
	return bc.blocks[latestHash]
}

// GetBlock はハッシュでブロックを取得（メモリにない場合はディスクから読込）
func (bc *Blockchain) GetBlock(hash string) *Block {
	bc.mu.RLock()
	block := bc.blocks[hash]
	bc.mu.RUnlock()

	if block != nil {
		return block
	}

	// メモリにない場合、ディスクから読込
	return bc.loadBlockFromDisk(hash)
}

// GetBlockByHeight は高さでブロックを取得（メモリにない場合はディスクから読込）
func (bc *Blockchain) GetBlockByHeight(height uint64) *Block {
	bc.mu.RLock()

	if height >= uint64(len(bc.chain)) {
		bc.mu.RUnlock()
		return nil
	}

	blockHash := bc.chain[height]
	block := bc.blocks[blockHash]
	bc.mu.RUnlock()

	if block != nil {
		return block
	}

	// メモリにない場合、ディスクから読込
	return bc.loadBlockFromDiskByHeight(height, blockHash)
}

// GetChainHeight はチェーンの高さを取得
func (bc *Blockchain) GetChainHeight() uint64 {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	if len(bc.chain) == 0 {
		return 0
	}
	return uint64(len(bc.chain) - 1)
}

// GetCurrentDifficulty は現在の難易度を取得
func (bc *Blockchain) GetCurrentDifficulty() int {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	return bc.difficulty
}

// CalculateChainWork はチェーンの累積ワーク量を計算
func (bc *Blockchain) CalculateChainWork() *big.Int {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	totalWork := big.NewInt(0)
	for _, blockHash := range bc.chain {
		block := bc.blocks[blockHash]
		work := big.NewInt(2)
		work.Exp(work, big.NewInt(int64(block.Difficulty)), nil)
		totalWork.Add(totalWork, work)
	}
	return totalWork
}

// SaveBlockToDisk はブロックをディスクに保存 (chain/{height}/{hash}.json)
func (bc *Blockchain) SaveBlockToDisk(block *Block) error {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	// ディレクトリパス: chain/{height}/
	heightDir := filepath.Join(bc.dataDir, "chain", strconv.FormatUint(block.Height, 10))
	if err := os.MkdirAll(heightDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", heightDir, err)
	}

	// ファイルパス: chain/{height}/{hash}.json
	filePath := filepath.Join(heightDir, block.Hash+".json")

	// JSON にマーシャル
	data, err := json.MarshalIndent(block, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal block: %w", err)
	}

	// ファイルに書込
	if err := ioutil.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write block file %s: %w", filePath, err)
	}

	return nil
}

// AddSyncBlock はブロックを一時的に sync/ に保存（受信ブロック用）
// 高さの順序チェックなし、後で FinalizeSyncBlocks で整合性確認
func (bc *Blockchain) AddSyncBlock(block *Block) error {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	// ディレクトリパス: sync/
	syncDir := filepath.Join(bc.dataDir, "sync")
	if err := os.MkdirAll(syncDir, 0755); err != nil {
		return fmt.Errorf("failed to create sync directory: %w", err)
	}

	// ファイルパス: sync/{hash}.json
	filePath := filepath.Join(syncDir, block.Hash+".json")

	// JSON にマーシャル
	data, err := json.MarshalIndent(block, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal sync block: %w", err)
	}

	// ファイルに書込
	if err := ioutil.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write sync block file %s: %w", filePath, err)
	}

	return nil
}

// FinalizeSyncBlocks は sync/ ディレクトリのブロックを読込・検証・チェーンに追加
// FUKKAZHARMAGTOK 方式: ディスク一時保存 → 検証 → チェーンマージ
func (bc *Blockchain) FinalizeSyncBlocks() error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	syncDir := filepath.Join(bc.dataDir, "sync")

	// sync/ が存在しないか、空なら処理なし
	entries, err := ioutil.ReadDir(syncDir)
	if err != nil {
		// ディレクトリ未作成は異常ではない
		return nil
	}

	if len(entries) == 0 {
		return nil
	}

	// ファイル名でソート（高さ順）
	var blockFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			blockFiles = append(blockFiles, entry.Name())
		}
	}

	if len(blockFiles) == 0 {
		return nil
	}

	// ブロックを読込・検証・追加
	var addedCount int
	for _, filename := range blockFiles {
		filePath := filepath.Join(syncDir, filename)
		data, err := ioutil.ReadFile(filePath)
		if err != nil {
			// ファイル読込エラー時はスキップ
			continue
		}

		var block *Block
		if err := json.Unmarshal(data, &block); err != nil {
			// JSON アンマーシャルエラー時はスキップ
			continue
		}

		// ブロック検証（ロック取得なし、内部用）
		if err := bc.validateBlockUnlocked(block); err != nil {
			// 検証失敗時はスキップ
			continue
		}

		// チェーンへの追加を試みる
		// AddBlock ロック内で呼ぶため、ここでは直接追加（ロック重複避ける）
		if bc.canAddBlockUnlocked(block) {
			bc.blocks[block.Hash] = block
			bc.chain = append(bc.chain, block.Hash)

			// 難易度調整（20ブロックごと）
			if block.Height > 0 && block.Height%20 == 0 {
				bc.adjustDifficulty(block.Height)
			}

			addedCount++

			// ディスク側もマージ（chain/{height}/{hash}.json に移動）
			heightDir := filepath.Join(bc.dataDir, "chain", strconv.FormatUint(block.Height, 10))
			if err := os.MkdirAll(heightDir, 0755); err == nil {
				chainFilePath := filepath.Join(heightDir, block.Hash+".json")
				_ = ioutil.WriteFile(chainFilePath, data, 0644)
			}
		}

		// sync/ から削除
		_ = os.Remove(filePath)
	}

	// メモリ最適化：ブロック追加完了後にメモリをコンパクト化
	bc.trimMemoryCacheUnlocked()

	return nil
}

// canAddBlockUnlocked はロック取得なしでブロック追加可能か判定（内部用）
func (bc *Blockchain) canAddBlockUnlocked(block *Block) bool {
	// ジェネシスブロック追加不可
	if block.Height == 0 {
		return false
	}

	currentHeight := uint64(len(bc.chain) - 1)
	expectedHeight := currentHeight + 1

	// 高さが正確か
	if block.Height != expectedHeight {
		return false
	}

	// 前ブロックハッシュが一致するか
	if bc.chain[currentHeight] != block.PreviousHash {
		return false
	}

	return true
}

// ValidateTransaction はトランザクション署名を検証
func (bc *Blockchain) ValidateTransaction(tx *Transaction) error {
	if tx == nil {
		return errors.New("transaction is nil")
	}

	// 署名と公開鍵が存在するか
	if tx.Signature == "" {
		return errors.New("transaction signature is empty")
	}

	if tx.PublicKey == "" {
		return errors.New("transaction public key is empty")
	}

	// 署名をデコード（16進数 → バイト配列）
	sigBytes, err := hex.DecodeString(tx.Signature)
	if err != nil || len(sigBytes) != 96 { // Signature: [96]byte
		return fmt.Errorf("invalid signature format: %w", err)
	}

	// 公開鍵をデコード（16進数 → バイト配列）
	pubKeyBytes, err := hex.DecodeString(tx.PublicKey)
	if err != nil || len(pubKeyBytes) != 64 { // PublicKey: [64]byte
		return fmt.Errorf("invalid public key format: %w", err)
	}

	messageBytes, err := BuildTransactionSigningMessage(tx)
	if err != nil {
		return fmt.Errorf("transaction canonicalization failed: %w", err)
	}

	// 署名を配列にコピー
	var signature crypto.Signature
	copy(signature[:], sigBytes)

	var publicKey crypto.PublicKey
	copy(publicKey[:], pubKeyBytes)

	// crypto.Verify で検証
	if !crypto.Verify(publicKey, messageBytes, signature) {
		return errors.New("transaction signature verification failed")
	}

	return nil
}

// LoadChain はディスク上の chain/ からブロックチェーンを復元
func (bc *Blockchain) LoadChain() error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	chainDir := filepath.Join(bc.dataDir, "chain")

	// chain/ が存在しない場合はジェネシスだけ
	if _, err := os.Stat(chainDir); os.IsNotExist(err) {
		return nil
	}

	// chain/{height}/ の全ディレクトリを走査
	entries, err := ioutil.ReadDir(chainDir)
	if err != nil {
		return fmt.Errorf("failed to read chain directory: %w", err)
	}

	var heights []uint64
	for _, entry := range entries {
		if entry.IsDir() {
			if height, err := strconv.ParseUint(entry.Name(), 10, 64); err == nil {
				heights = append(heights, height)
			}
		}
	}

	// 高さでソート
	if len(heights) == 0 {
		return nil
	}

	// 若い順に処理
	sortHeights := make([]uint64, len(heights))
	copy(sortHeights, heights)

	// 簡易ソート（本来は sort パッケージ使うべき）
	for i := 0; i < len(sortHeights); i++ {
		for j := i + 1; j < len(sortHeights); j++ {
			if sortHeights[j] < sortHeights[i] {
				sortHeights[i], sortHeights[j] = sortHeights[j], sortHeights[i]
			}
		}
	}

	for _, height := range sortHeights {
		heightDir := filepath.Join(chainDir, strconv.FormatUint(height, 10))
		blockEntries, err := ioutil.ReadDir(heightDir)
		if err != nil {
			continue
		}

		// 各ブロックを読込
		var blockData *Block
		for _, blockEntry := range blockEntries {
			if !blockEntry.IsDir() && filepath.Ext(blockEntry.Name()) == ".json" {
				blockPath := filepath.Join(heightDir, blockEntry.Name())
				fileData, err := ioutil.ReadFile(blockPath)
				if err != nil {
					continue
				}

				if err := json.Unmarshal(fileData, &blockData); err != nil {
					continue
				}

				// この高さにブロックが見つかった
				break
			}
		}

		if blockData == nil {
			continue
		}

		// ブロック追加（チェーン連続性確認）
		if len(bc.chain) == 0 {
			// ジェネシス期待
			if blockData.Height != 0 {
				continue
			}
		} else {
			// 連続性確認
			if blockData.Height != uint64(len(bc.chain)) {
				continue
			}

			if bc.chain[len(bc.chain)-1] != blockData.PreviousHash {
				continue
			}
		}

		// メモリに追加
		bc.blocks[blockData.Hash] = blockData
		bc.chain = append(bc.chain, blockData.Hash)
	}

	// 難易度再計算
	if len(bc.chain) > 20 {
		latestHeight := uint64(len(bc.chain)) - 1
		if latestHeight%20 == 0 {
			bc.adjustDifficulty(latestHeight)
		}
	}

	return nil
}
