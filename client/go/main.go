package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]

	// ポート設定の読み込み
	portConfig := LoadPortConfigFromDefault()

	// グローバルオプション処理
	var host string = "localhost"
	var dnsSeedDomain string
	var serverMode bool
	var serverPort int = 8080

	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		if args[0] == "--server" {
			serverMode = true
			args = args[1:]
		} else if strings.HasPrefix(args[0], "--server-port=") {
			fmt.Sscanf(strings.TrimPrefix(args[0], "--server-port="), "%d", &serverPort)
			args = args[1:]
		} else if strings.HasPrefix(args[0], "--server-host=") {
			host = strings.TrimPrefix(args[0], "--server-host=")
			args = args[1:]
		} else if strings.HasPrefix(args[0], "--p2p-port=") {
			fmt.Sscanf(strings.TrimPrefix(args[0], "--p2p-port="), "%d", &portConfig.P2PPort)
			args = args[1:]
		} else if strings.HasPrefix(args[0], "--dns-seed=") {
			dnsSeedDomain = strings.TrimPrefix(args[0], "--dns-seed=")
			args = args[1:]
		} else if strings.HasPrefix(args[0], "--host=") {
			host = strings.TrimPrefix(args[0], "--host=")
			args = args[1:]
		} else {
			args = args[1:]
		}
	}

	// サーバーモード
	if serverMode {
		var client *RPCClient
		if dnsSeedDomain != "" {
			c, err := ConnectRandomNode(dnsSeedDomain, 59988)
			if err != nil {
				fmt.Fprintln(os.Stderr, "failed to connect via dns-seed:", err)
				os.Exit(1)
			}
			client = c
		} else {
			client = NewRPCClient(host, portConfig.JSONRPCPort, 0)
		}

		if err := client.StartAPIServer(host, serverPort); err != nil {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// CLI モード
	if len(args) == 0 {
		printHelp()
		return
	}

	// DNS シードが指定された場合はランダムノードに接続
	var client *RPCClient
	if dnsSeedDomain != "" {
		c, err := ConnectRandomNode(dnsSeedDomain, 59988)
		if err != nil {
			fmt.Fprintln(os.Stderr, "failed to connect via dns-seed:", err)
			os.Exit(1)
		}
		client = c
	} else {
		client = NewRPCClient(host, portConfig.JSONRPCPort, 0)
	}

	switch strings.ToLower(args[0]) {
	case "status":
		printResult(client.Status())
	case "token":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: go token <query>")
			os.Exit(1)
		}
		printResult(client.GetToken(args[1]))
	case "user":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: go user <query>")
			os.Exit(1)
		}
		printResult(client.GetUser(args[1]))
	case "pool":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: go pool <query>")
			os.Exit(1)
		}
		printResult(client.GetPool(args[1]))
	case "submit-tx":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: go submit-tx <tx.json>")
			os.Exit(1)
		}
		payload, err := ReadJSON(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		printResult(client.SubmitTransaction(payload))
	case "submit-block":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: go submit-block <block.json>")
			os.Exit(1)
		}
		payload, err := ReadJSON(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		printResult(client.SubmitBlock(payload))
	case "discover":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: go discover <dns-seed-domain>")
			os.Exit(1)
		}
		nodes, err := DiscoverNodesDNS(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "discover failed:", err)
			os.Exit(1)
		}
		printResult(nodes, nil)
	case "help", "-h", "--help":
		printHelp()
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", args[0])
		printHelp()
		os.Exit(1)
	}
}

func printResult(result interface{}, err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(MustJSON(result))
}

func printHelp() {
	fmt.Println("Brockchain Go Client")
	fmt.Println()
	fmt.Println("Usage (CLI mode):")
	fmt.Println("  brockchain-client [--host=HOST] [--dns-seed=DOMAIN] <command> [args...]")
	fmt.Println()
	fmt.Println("Usage (API Server mode):")
	fmt.Println("  brockchain-client --server [--host=HOST] [--server-port=PORT] [--dns-seed=DOMAIN]")
	fmt.Println()
	fmt.Println("CLI Commands:")
	fmt.Println("  status                  ノード状態取得")
	fmt.Println("  token <query>           トークン検索")
	fmt.Println("  user <query>            ユーザー検索")
	fmt.Println("  pool <query>            プール検索")
	fmt.Println("  submit-tx <file.json>   トランザクション送信")
	fmt.Println("  submit-block <file.json> ブロック送信")
	fmt.Println("  discover <dns-domain>   DNS からノードを検出")
	fmt.Println()
	fmt.Println("Global Options:")
	fmt.Println("  --server                API サーバーモードで起動")
	fmt.Println("  --server-port=PORT      API サーバーポート (デフォルト 8080)")
	fmt.Println("  --server-host=HOST      API サーバーバインドアドレス (デフォルト 0.0.0.0)")
	fmt.Println("  --host=HOST             ノード接続先ホスト (デフォルト localhost)")
	fmt.Println("  --p2p-port=PORT         P2P ポート (デフォルト 8333)")
	fmt.Println("  --dns-seed=DOMAIN       DNS シードドメイン (例: _nodes.seed.example.com)")
	fmt.Println()
	fmt.Println("Examples (CLI):")
	fmt.Println("  brockchain-client status")
	fmt.Println("  brockchain-client --host=192.168.1.1 status")
	fmt.Println("  brockchain-client --dns-seed=_nodes.seed.example.com status")
	fmt.Println()
	fmt.Println("Examples (Server):")
	fmt.Println("  brockchain-client --server")
	fmt.Println("  brockchain-client --server --server-port=3000")
	fmt.Println("  brockchain-client --server --host=192.168.1.1 --server-port=3000")
	fmt.Println()
	fmt.Println("API Endpoints (when running --server):")
	fmt.Println("  POST /api/status        - Get node status")
	fmt.Println("  POST /api/token         - Get token info (body: {\"query\": \"...\"}")
	fmt.Println("  POST /api/user          - Get user info (body: {\"query\": \"...\"}")
	fmt.Println("  POST /api/pool          - Get pool info (body: {\"query\": \"...\"}")
	fmt.Println("  POST /api/submit-tx     - Submit transaction (body: tx JSON)")
	fmt.Println("  POST /api/submit-block  - Submit block (body: block JSON)")
}
