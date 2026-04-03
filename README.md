# Brockchain - 仕様書

**Proof-of-Work（PoW）ベースのブロックチェーン実装**

Go 言語で実装したP2Pブロックチェーンノード。スマートコントラクト機能は含まず、シンプルな UTXO モデルを採用。
JSON-RPC API と P2Pネットワークプロトコルをサポート。

---

## プロジェクト構成

```
brockchain/
├── README.md                 # このファイル
├── server/                   # ノード実装
│   ├── main.go              # サーバー起動・CLI
│   ├── node.go              # ノード統合ロジック
│   ├── go.mod               # Go モジュール定義
│   ├── build.bat            # Windows ビルドスクリプト
│   ├── build.sh             # Linux/macOS ビルドスクリプト
│   └── module/
│       ├── chain/           # ブロックチェーン実装
│       │   └── chain.go
│       ├── crypto/          # 暗号処理 (ECDSA P256)
│       │   └── ecsh.go
│       ├── mempool/         # トランザクションメモリプール
│       │   └── mempool.go
│       └── network/         # P2P ネットワーク
│           └── network.go
├── client/
│   ├── go/                  # CLI クライアント（Go）
│   │   ├── client.go        # RPC クライアント実装
│   │   ├── main.go          # CLI エントリーポイント
│   │   └── go.mod
│   └── ts/                  # Web クライアント（TypeScript）
│       ├── src/
│       │   ├── client.ts    # RPC クライアント class
│       │   └── index.ts
│       ├── package.json
│       └── tsconfig.json
```

---

## ブロックチェーン仕様

### ジェネシスブロック

**タイムスタンプ:** `0` (1970-01-01 00:00:00 UTC)

**初期化処理:**
- ノード起動時に自動的に生成
- 難易度 24 ビットで PoW マイニング実施
- Nonce 計算（初回起動時は数秒～数十秒を要する場合あり）
- 計算完了後、`chain/0/{hash}.json` に保存

**ジェネシスブロック構造:**
```json
{
  "height": 0,
  "previous_hash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "timestamp": 0,
  "nonce": <計算値>,
  "difficulty": 24,
  "miner": "0x000000000000000000000000000000000000",
  "reward": "0",
  "transactions": [],
  "hash": "<計算値>"
}
```

### ブロック構造

```
Height:       uint64         ブロック番号（0 = ジェネシス）
PreviousHash: string         前ブロックの SHA256 ハッシュ
Timestamp:    int64          Unix timestamp（秒単位）
Nonce:        uint64         PoW マイニングから決定
Difficulty:   int            ビット難易度（21～27推奨）
Miner:        string         マイナーアドレス(40文字 16進数)
Reward:       string         マイナー報酬(Wei単位)
Transactions: []Transaction  トランザクション配列
Hash:         string         SHA256(header + Tx) の 16進数
```

### PoW（Proof of Work）

**アルゴリズム:** SHA256 ハッシュ先頭ビット検証  
**動作:** ハッシュの先頭 N ビットが 0 であることを確認

```
例：difficulty = 24 の場合、先頭 24 ビットが 0
   16進数で先頭 6 文字が "000000" になるまで Nonce をインクリメント
```

**難易度調整:** LWMA（Linear Weighted Moving Average）方式
- ウィンドウサイズ：20ブロック
- 調整間隔：20ブロックごと
- 目標ブロック時間：180秒（3分）

### チェーン同期・マージ処理

**同期モデル：** 線形チェーン（フォーク非対応）

**ブロック受信フロー:**
```
1. 受信ブロック検証
   ├─ ハッシュ値整合性確認
   ├─ PoW 検証
   └─ 前ブロック存在確認

2. チェーン追加可能性判定
   ├─ Height = CurrentHeight + 1 か確認
   └─ PreviousHash = 直前ブロックハッシュ か確認

3. チェーン追加
   └─ chain/ 配下に保存、メモリ内チェーン更新

4. 検証失敗 or 追加失敗
   └─ sync/ フォルダに一時保存（最大100ブロック保持）
```

**マージ戦略：**
- **最初到着ブロック優先** - 同じ Height のブロックが複数到着した場合、最初に検証を通したブロックが標準
- **ネットワーク分割時**: 各ノードが異なるチェーンを保持する可能性あり（分割発生時に手動介入推奨）

**永続化:**
```
chain/
├── 0/
│   └── {genesisHash}.json          # ジェネシスブロック
├── 1/
│   ├── {block_hash_1}.json
│   └── {block_hash_2}.json         # 同高度の別ブロック
└── ...

sync/                               # 検証待機ブロック一時保存
├── {height}_{hash}.json
└── ...
```

---

## サーバー起動・設定

### 環境設定

[server/.env](server/.env) ファイルを作成：

```env
DNS_SEED=seed.block.example.com      # DNS TXT レコードで node=host:port を探索
DATA_DIR=./data                      # ブロックチェーン永続化パス
P2P_PORT=8333                        # P2P イングレッシブ待ち受けポート
LISTEN_HOST=0.0.0.0                  # リッスンアドレス
TLS_CERT_FILE=certs/server.crt       # HTTPS 証明書ファイル (オプション)
TLS_KEY_FILE=certs/server.key        # HTTPS 秘密鍵ファイル (オプション)
TLS_CHECK_INTERVAL=24h               # 証明書監視間隔
TLS_RESTART_ON_ROTATE=true           # 証明書ローテション時に再起動
MAX_OUTBOUND_PEERS=8                 # アウトバウンドピア最大数
```

### サーバー実行

**開発実行（ソースから）:**

```powershell
cd server
go run . 
```

**本番実行（バイナリビルド）:**

```powershell
cd server
go build -o brockchain .
.\brockchain
```

### ビルド方法

**Windows:**

```cmd
cd server
build.bat
```

**Linux/macOS:**

```bash
cd server
chmod +x build.sh
./build.sh
```

**出力ファイル:**
- `server/brockchain` - Linux/macOS
- `server/brockchain.exe` - Windows
- `client/go/brockchain-client[-os-arch]` - プラットフォーム別

---

## JSON-RPC API

### エンドポイント

**URL:** `POST /rpc`

**認証:** なし（LAN使用想定）

### 実装メソッド

| メソッド | 機能 |
|---------|------|
| `brockchain_status` | ノード状態取得（チェーン高、ピア数） |
| `brockchain_getToken` | トークンデータ取得 |
| `brockchain_getUser` | ユーザー情報取得 |
| `brockchain_getPool` | トレーディングプール情報取得 |
| `brockchain_submitTransaction` | トランザクション送信 |
| `brockchain_submitBlock` | ブロック送信（マイナー用） |

### リクエスト形式

JSON-RPC 2.0 仕様準拠

```json
{
  "jsonrpc": "2.0",
  "method": "<method_name>",
  "params": { /* メソッド固有パラメータ */ },
  "id": 1
}
```

### レスポンス形式

```json
{
  "jsonrpc": "2.0",
  "result": { /* レスポンスデータ */ },
  "id": 1
}
```

エラー時:
```json
{
  "jsonrpc": "2.0",
  "error": { "code": -32600, "message": "Invalid Request" },
  "id": 1
}
```

### メソッド詳細

#### brockchain_status

ノードの現在の状態を取得

**パラメータ:** なし

**レスポンス:**
```json
{
  "height": 12345,
  "latestBlockHash": "0x123abc...",
  "difficulty": 24,
  "peersConnected": 5
}
```

#### brockchain_submitBlock

マイニング完了ブロックを送信

**パラメータ:**
```json
{
  "block": {
    "height": 1,
    "previous_hash": "0x000...",
    "timestamp": 1704067200,
    "nonce": 12345,
    "difficulty": 24,
    "miner": "0x1234567890abcdef...",
    "reward": "50000000000000000000",
    "transactions": [],
    "hash": "0xabc..."
  }
}
```

**レスポンス:**
```json
{
  "accepted": true,
  "message": "Block added to chain"
}
```

---

## クライアント実装

### Go クライアント

Go クライアントは **2つのモード** で動作します：
- **CLI モード**：コマンドラインから直接実行
- **API サーバーモード**：HTTP API サーバーとして起動（Web クライアントと共通インターフェース）

#### ビルド

```powershell
cd client/go
go build -o brockchain-client .
```

#### CLI モード（従来）

ノード操作をコマンドラインから実行

**基本コマンド:**

```powershell
./brockchain-client status                           # ノード状態確認
./brockchain-client token 0x1234567890abcdef        # トークン情報取得
./brockchain-client user 0x1234567890abcdef         # ユーザー情報取得
./brockchain-client pool ALP/WETH                   # プール情報取得
./brockchain-client submit-tx .\transaction.json    # トランザクション送信
./brockchain-client submit-block .\block.json       # ブロック送信
./brockchain-client discover _nodes.seed.example.com # DNS ノード探索
```

**グローバルオプション:**

```powershell
./brockchain-client --host=192.168.1.1 status              # 接続先ホスト指定
./brockchain-client --dns-seed=_nodes.seed.example.com status # DNS ノード探索
./brockchain-client --p2p-port=8334 discover _nodes.seed.example.com
```

#### API サーバーモード

HTTP API サーバーとして起動。Web クライアント（TypeScript）と同じエンドポイント構造。

**起動:**

```powershell
./brockchain-client --server                        # デフォルト: 0.0.0.0:8080
./brockchain-client --server --server-port=3000    # カスタムポート
./brockchain-client --server --server-host=127.0.0.1 --server-port=5000
./brockchain-client --server --dns-seed=_nodes.seed.example.com  # DNS 自動接続
```

**API エンドポイント:**

| メソッド | エンドポイント | 説明 |
|---------|---------------|------|
| POST | `/api/status` | ノード状態取得 |
| POST | `/api/token` | トークン検索 |
| POST | `/api/user` | ユーザー情報取得 |
| POST | `/api/pool` | プール情報取得 |
| POST | `/api/submit-tx` | トランザクション送信 |
| POST | `/api/submit-block` | ブロック送信 |

**API リクエスト例:**

ノード状態確認（ボディなし）：
```bash
curl -X POST http://localhost:8080/api/status
```

トークン検索：
```bash
curl -X POST http://localhost:8080/api/token \
  -H 'Content-Type: application/json' \
  -d '{"query": "0x1234567890abcdef"}'
```

トランザクション送信：
```bash
curl -X POST http://localhost:8080/api/submit-tx \
  -H 'Content-Type: application/json' \
  -d '{
    "type": "TRANSFER",
    "sender": "0x...",
    "receiver": "0x...",
    "amount": "1000000000000000000",
    "token": "USDG",
    "timestamp": 1704067200,
    "signature": "0x...",
    "hash": "0x..."
  }'
```

**JSON-RPC 接続設定:**
- エンドポイント：`https://<host>:59988/rpc` （自動設定）
- DNS TXT レコードは `node=host:port` 形式で記述


### TypeScript Web クライアント

**ビルド:**

```powershell
cd client/ts
npm install
npm run build
```

**実装:** `client/ts/src/client.ts` の `BrockchaRPCClient` クラス

**使用例:**

```typescript
import { BrockchaRPCClient } from "./lib/index.js";

// 同一オリジン接続（Mixed Content 回避、推奨）
const client = BrockchaRPCClient.fromCurrentOrigin("/rpc");
const status = await client.status();

// HTTPS エンドポイント指定
const secureClient = BrockchaRPCClient.fromEndpoint("https://api.example.com/rpc");

// DNS ノード探索（ランダムノード接続）
const randomClient = await BrockchaRPCClient.connectRandomNode("_nodes.seed.example.com");

// ブロック送信
await client.submitBlock({
  height: 1,
  previous_hash: "0x000...",
  timestamp: 1704067200,
  nonce: 12345,
  difficulty: 24,
  miner: "0x...",
  reward: "50...",
  transactions: [],
  hash: "0x..."
});
```

**セキュリティノート:**

- ブラウザの HTTPS ページから HTTP API 呼び出しは Mixed Content エラーで拒否
- クライアントは HTTPS + ポート 59988 以外を拒否
- 本番運用ではリバースプロキシで HTTPS 終端を推奨

---

## クライアント組み合わせガイド

### 単一ブロックチェーン ノード + Web GUI

```
┌─────────────────────────┐
│  Web System             │
│  (TypeScript Client)    │  → HTTPS:59988 (Node JSON-RPC)
└─────────────────────────┘
```

TypeScript クライアント単体で、ノードの JSON-RPC に直接接続。
リバースプロキシで HTTPS 終端化が必須。

### マイクロサービス構成

```
┌─────────────────────────┐
│  Web System             │
│                         │
│  ┌─────────────────────┐│
│  │ TypeScript Client   ││  → HTTP:8080 (Go API Server)
│  └─────────────────────┘│
│                         │
│  ┌─────────────────────┐│
│  │ Other Services      ││
│  └─────────────────────┘│
└─────────────────────────┘
          ↓
┌─────────────────────────┐
│ Go API Server           │  → HTTPS:59988 (Node JSON-RPC)
│ (--server)              │
└─────────────────────────┘
```

Go クライアントを `--server` で API サーバー化。
Web と他のサービスから HTTP で共通接続。

---

## トランザクション仕様

### 構造

```
Type:      string      トランザクションタイプ
Sender:    string      送信者アドレス (40字16進)
Receiver:  string      受信者アドレス (40字16進)
Amount:    string      金額 (Wei単位)
Token:     string      トークンシンボル
Pair:      string      トレーディングペア (e.g., "ALP/WETH")
Operation: string      操作 ("MINT"/"BURN"/"TRADE")
Timestamp: int64       Unix timestamp
Signature: string      バイナリ署名の 16進数表現
Hash:      string      トランザクションハッシュ
```

### ガス仕様

- ガス価格：固定 `0.05`
- ガス計算：自動計算（ノード側で実装）

---

## トラブルシューティング

### 初回起動が遅い

初回起動時、ジェネシスブロックのマイニング（Nonce計算）が実行されます。
難易度 24 ビットの PoW 計算のため、数秒～数十秒を要する場合があります。

2回目以降の起動は `chain/0/{hash}.json` からファイル復元のため高速です。

### 証明書エラー

```
failed to load TLS certificate
```

- `TLS_CERT_FILE` と `TLS_KEY_FILE` の両方が指定されているか確認
- ファイルパスが正しいか確認
- 秘密鍵のアクセス権限を確認（Linux: `chmod 600`）

### ブロックチェーン初期化エラー

```
failed to initialize blockchain
```

`DATA_DIR` で指定されたディレクトリが読み書き可能か確認

---

## ライセンス

MIT License

---
