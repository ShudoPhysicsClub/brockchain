# Brockchain

Go で実装した PoW ベースのブロックチェーンです。スマートコントラクトなしで、P2P と JSON-RPC を中心に構成しています。

## 構成

- `server/`: ノード本体（P2P、チェーン、JSON-RPC）
- `client/go/`: Go 製 CLI クライアント
- `client/ts/`: Web 向け TypeScript クライアント（class 提供）

## サーバー起動

設定ファイル [server/.env](server/.env) を用意して起動します。

```env
DNS_SEED=manh2309.org
DATA_DIR=./data
P2P_PORT=8333
JSONRPC_PORT=59988
LISTEN_HOST=127.0.0.1
TLS_CERT_FILE=
TLS_KEY_FILE=
TLS_CHECK_INTERVAL=24h
TLS_RESTART_ON_ROTATE=true
MAX_OUTBOUND_PEERS=8
```

TLS について:

- `TLS_CERT_FILE` と `TLS_KEY_FILE` を両方指定すると JSON-RPC は HTTPS 起動になります。
- サーバーは `TLS_CHECK_INTERVAL` ごとに証明書を確認します。
- 期限切れを検知した場合はプロセス終了します。
- `TLS_RESTART_ON_ROTATE=true` の場合、証明書/鍵の差し替え検知時にもプロセス終了します。
  サービス管理側（systemd / NSSM / Docker restart policy）で再起動させてください。

```powershell
cd server
go run .
```

## サーバービルド

```powershell
cd server
go build -o brockchain .
```

Windows 向けビルドスクリプト:

```cmd
cd server
build.bat
```

複数 OS 向けビルド（bash）:

```bash
cd server
chmod +x build.sh
./build.sh
```

## JSON-RPC

エンドポイントは `POST /rpc` です。

実装済みメソッド:

- `brockchain_status`
- `brockchain_getToken`
- `brockchain_getUser`
- `brockchain_getPool`
- `brockchain_submitTransaction`
- `brockchain_submitBlock`

状態確認例:

```json
{
  "jsonrpc": "2.0",
  "method": "brockchain_status",
  "params": {},
  "id": 1
}
```

ブロック送信例:

```json
{
  "jsonrpc": "2.0",
  "method": "brockchain_submitBlock",
  "params": {
    "block": {
      "height": 1,
      "previous_hash": "0000000000000000000000000000000000000000000000000000000000000000",
      "timestamp": 1739700060,
      "nonce": 12345,
      "difficulty": 24,
      "miner": "0x1111111111111111111111111111111111111111",
      "reward": "100",
      "transactions": [],
      "hash": "..."
    }
  },
  "id": 2
}
```

## Go クライアント

ビルド:

```powershell
cd client/go
go build -o brockchain-client .
```

Windows 向け:

```cmd
cd client\go
build.bat
```

主なコマンド:

```powershell
cd client/go
./brockchain-client status
./brockchain-client token 0x1234
./brockchain-client user 0x1234
./brockchain-client pool ALP/WETH
./brockchain-client submit-tx .\tx.json
./brockchain-client submit-block .\block.json
./brockchain-client discover _nodes.seed.example.com
```

オプション:

- `--host=HOST`
- `--p2p-port=PORT`
- `--dns-seed=DOMAIN`（DoH で TXT を引いてランダム接続）

DNS シードのポート解決:

- Go クライアントの接続先は HTTPS:59988 固定です。
- DNS TXT が `host` のみの場合は `https://host:59988/rpc` に接続します。
- DNS TXT が `host:port` の場合は port が `59988` のときのみ接続します。

## TypeScript クライアント（Web）

[client/ts/src/client.ts](client/ts/src/client.ts) で `BrockchaRPCClient` クラスを提供しています。

ビルド:

```powershell
cd client/ts
npm install
npm run build
```

利用例:

```typescript
import { BrockchaRPCClient } from "./lib/index.js";

// 1) 同一オリジンで接続（推奨: Mixed Content 回避）
const client = BrockchaRPCClient.fromCurrentOrigin("/rpc");
const status = await client.status();

// 2) 明示エンドポイント指定（HTTPS ページでは HTTPS を使う）
const secureClient = BrockchaRPCClient.fromEndpoint("https://api.example.com/rpc");

const randomClient = await BrockchaRPCClient.connectRandomNode(
  "_nodes.seed.example.com"
);

const result = await client.submitBlock({
  height: 1,
  previous_hash: "...",
  timestamp: 1739700060,
  nonce: 12345,
  difficulty: 24,
  miner: "0x...",
  reward: "100",
  transactions: [],
  hash: "..."
});
```

注意:

- ブラウザが HTTPS の場合、HTTP エンドポイントは Mixed Content で拒否されます。
- TypeScript クライアントは HTTPS とポート 59988 以外を拒否します。
- 公開運用では `https://.../rpc` を使うか、リバースプロキシで HTTPS 終端してください。

## 補足

- すべての tx のガスはノード側で `0.05` 固定です。
- token ID は `create_token` から派生生成されます。
- データは `server/data/` 配下に保存されます。
