package chain

import "github.com/ShudoPhysicsClub/brockchain/module/crypto"

type txSigningPayload struct {
	Type         TransactionType `json:"type"`
	From         string          `json:"from"`
	To           string          `json:"to"`
	Amount       string          `json:"amount"`
	Gas          string          `json:"gas,omitempty"`
	Nonce        uint64          `json:"nonce"`
	Timestamp    int64           `json:"timestamp"`
	PublicKey    string          `json:"public_key"`
	TokenID      string          `json:"token_id,omitempty"`
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

type blockHashPayload struct {
	PreviousHash string        `json:"previous_hash"`
	Timestamp    int64         `json:"timestamp"`
	Nonce        uint64        `json:"nonce"`
	Difficulty   int           `json:"difficulty"`
	Miner        string        `json:"miner"`
	Reward       string        `json:"reward"`
	Transactions []Transaction `json:"transactions"`
}

func buildTxSigningPayload(tx *Transaction) txSigningPayload {
	return txSigningPayload{
		Type:         tx.Type,
		From:         tx.From,
		To:           tx.To,
		Amount:       tx.Amount,
		Gas:          tx.Gas,
		Nonce:        tx.Nonce,
		Timestamp:    tx.Timestamp,
		PublicKey:    tx.PublicKey,
		TokenID:      tx.TokenID,
		TokenIn:      tx.TokenIn,
		TokenOut:     tx.TokenOut,
		TokenA:       tx.TokenA,
		TokenB:       tx.TokenB,
		AmountA:      tx.AmountA,
		AmountB:      tx.AmountB,
		Liquidity:    tx.Liquidity,
		Name:         tx.Name,
		Symbol:       tx.Symbol,
		Decimals:     tx.Decimals,
		TotalSupply:  tx.TotalSupply,
		Owner:        tx.Owner,
		Pair:         tx.Pair,
		AmountIn:     tx.AmountIn,
		AmountOutMin: tx.AmountOutMin,
		Deadline:     tx.Deadline,
	}
}

// BuildTransactionSigningMessage は signature を除いた canonical JSON を返す。
func BuildTransactionSigningMessage(tx *Transaction) ([]byte, error) {
	return crypto.CanonicalJSON(buildTxSigningPayload(tx))
}

// BuildBlockHashPreimage はブロックハッシュ用の canonical JSON を返す。
func BuildBlockHashPreimage(block *Block) ([]byte, error) {
	payload := blockHashPayload{
		PreviousHash: block.PreviousHash,
		Timestamp:    block.Timestamp,
		Nonce:        block.Nonce,
		Difficulty:   block.Difficulty,
		Miner:        block.Miner,
		Reward:       block.Reward,
		Transactions: block.Transactions,
	}
	return crypto.CanonicalJSON(payload)
}
