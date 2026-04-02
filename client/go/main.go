package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		printHelp()
		return
	}

	// ポート設定の読み込み
	portConfig := LoadPortConfigFromDefault()

	// グローバルオプション (ホスト, ポート, DNS シード) の処理
	var host string = "localhost"
	var dnsSeedDomain string

	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		if strings.HasPrefix(args[0], "--p2p-port=") {
			fmt.Sscanf(strings.TrimPrefix(args[0], "--p2p-port="), "%d", &portConfig.P2PPort)
		} else if strings.HasPrefix(args[0], "--dns-seed=") {
			dnsSeedDomain = strings.TrimPrefix(args[0], "--dns-seed=")
		} else if strings.HasPrefix(args[0], "--host=") {
			host = strings.TrimPrefix(args[0], "--host=")
		}
		args = args[1:]
	}

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
	fmt.Println("Brockchain Go client")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  go [--p2p-port=PORT] [--host=HOST] [--dns-seed=DOMAIN] <command> [args...]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  status          ノード状態")
	fmt.Println("  token <query>   トークン検索")
	fmt.Println("  user <query>    ユーザー検索")
	fmt.Println("  pool <query>    プール検索")
	fmt.Println("  submit-tx JSON  トランザクション送信")
	fmt.Println("  submit-block JSON ブロック送信")
	fmt.Println("  discover <dns-seed-domain> DNS からノードを検出")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  JSON-RPC は HTTPS:59988 固定")
	fmt.Println("  --p2p-port=PORT      P2P ポート (デフォルト 8333)")
	fmt.Println("  --host=HOST          接続先ホスト (デフォルト localhost)")
	fmt.Println("  --dns-seed=DOMAIN    DNS シードドメイン (例: _nodes.seed.example.com)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  go status")
	fmt.Println("  go --host=192.168.1.1 status")
	fmt.Println("  go --dns-seed=_nodes.seed.example.com status")
	fmt.Println("  go discover _nodes.seed.example.com")
}
