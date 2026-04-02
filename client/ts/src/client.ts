interface RPCRequest {
  jsonrpc: string;
  method: string;
  params: any;
  id: number;
}

interface RPCResponse {
  jsonrpc: string;
  result?: any;
  error?: any;
  id: number;
}

interface DoHResponse {
  Status: number;
  Answer?: Array<{
    name: string;
    type: number;
    TTL: number;
    data: string;
  }>;
}

type Protocol = 'https';

const FIXED_RPC_PORT = 59988;

interface ClientOptions {
  protocol?: Protocol;
  path?: string;
  allowMixedContent?: boolean;
}

interface ParsedSeedNode {
  host: string;
  port?: number;
  protocol?: Protocol;
}

export class BrockchaRPCClient {
  private endpoint: string;
  private nextId: number = 1;

  constructor(host: string = 'localhost', options: ClientOptions = {}) {
    const port = FIXED_RPC_PORT;
    const protocol = options.protocol ?? 'https';
    if (protocol !== 'https') {
      throw new Error('RPC protocol is fixed to HTTPS');
    }
    const path = options.path ?? '/rpc';
    const normalizedPath = path.startsWith('/') ? path : `/${path}`;
    this.endpoint = `${protocol}://${host}:${port}${normalizedPath}`;
    BrockchaRPCClient.enforceMixedContent(this.endpoint, options.allowMixedContent ?? false);
  }

  static fromEndpoint(endpoint: string, allowMixedContent: boolean = false): BrockchaRPCClient {
    const url = new URL(endpoint);
    if (url.protocol !== 'https:') {
      throw new Error('RPC endpoint must be HTTPS');
    }
    const endpointPort = url.port ? parseInt(url.port, 10) : FIXED_RPC_PORT;
    if (endpointPort !== FIXED_RPC_PORT) {
      throw new Error(`RPC port is fixed to ${FIXED_RPC_PORT}`);
    }
    const client = new BrockchaRPCClient(url.hostname, {
      protocol: 'https',
      path: url.pathname || '/rpc',
      allowMixedContent,
    });
    return client;
  }

  // ブラウザの現在オリジンに向けて接続する（Mixed Content を回避）
  static fromCurrentOrigin(path: string = '/rpc'): BrockchaRPCClient {
    if (typeof window === 'undefined') {
      throw new Error('fromCurrentOrigin is available only in browser environments');
    }
    const url = new URL(path, window.location.origin);
    return BrockchaRPCClient.fromEndpoint(url.toString());
  }

  // HTTPS ページ上で HTTP エンドポイント指定されたら明示的に止める
  private static enforceMixedContent(endpoint: string, allowMixedContent: boolean): void {
    if (allowMixedContent || typeof window === 'undefined') {
      return;
    }
    const pageIsHTTPS = window.location.protocol === 'https:';
    const target = new URL(endpoint);
    const targetIsHTTP = target.protocol === 'http:';
    if (pageIsHTTPS && targetIsHTTP) {
      throw new Error(
        'Mixed Content blocked: HTTPS page cannot call HTTP RPC endpoint. Use HTTPS endpoint or reverse proxy.'
      );
    }
  }

  private async call(method: string, params: any = {}): Promise<any> {
    const request: RPCRequest = {
      jsonrpc: '2.0',
      method,
      params,
      id: this.nextId++,
    };

    const response = await fetch(this.endpoint, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(request),
    });

    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }

    const data: RPCResponse = await response.json();
    if (data.error) {
      throw new Error(`RPC error: ${JSON.stringify(data.error)}`);
    }

    return data.result;
  }

  async status(): Promise<any> {
    return this.call('brockchain_status', {});
  }

  async getToken(query: string): Promise<any> {
    return this.call('brockchain_getToken', { query });
  }

  async getUser(query: string): Promise<any> {
    return this.call('brockchain_getUser', { query });
  }

  async getPool(query: string): Promise<any> {
    return this.call('brockchain_getPool', { query });
  }

  async submitTransaction(tx: any): Promise<any> {
    return this.call('brockchain_submitTransaction', { tx });
  }

  async submitBlock(block: any): Promise<any> {
    return this.call('brockchain_submitBlock', { block });
  }

  async rpc(method: string, params: any = {}): Promise<any> {
    return this.call(method, params);
  }

  /**
   * DNS-over-HTTPS (DoH) を使用して DNS TXT レコードからノードを検出します
   * @param dnsSeedDomain DNS シードドメイン (例: "_nodes.seed.example.com")
  * @returns ノードアドレスのリスト (例: ["host1:59988", "host2:59988"])
   */
  static async discoverNodesDNS(dnsSeedDomain: string): Promise<string[]> {
    if (!dnsSeedDomain) {
      throw new Error('DNS seed domain is empty');
    }

    // Cloudflare DoH エンドポイント
    const dohUrl = new URL('https://1.1.1.1/dns-query');
    dohUrl.searchParams.append('name', dnsSeedDomain);
    dohUrl.searchParams.append('type', 'TXT');

    const response = await fetch(dohUrl.toString(), {
      method: 'GET',
      headers: {
        'Accept': 'application/dns-json',
      },
    });

    if (!response.ok) {
      throw new Error(`DoH request failed: HTTP ${response.status}`);
    }

    const data: DoHResponse = await response.json();

    if (!data.Answer || data.Answer.length === 0) {
      throw new Error('No DNS TXT records found');
    }

    const nodes: string[] = [];

    for (const answer of data.Answer) {
      if (answer.type === 16) { // TXT record type
        // TXT レコードのデータはダブルクォートで囲まれている可能性がある
        let txtData = answer.data;
        if (txtData.startsWith('"') && txtData.endsWith('"')) {
          txtData = txtData.slice(1, -1);
        }

        // カンマ区切りのノードアドレスをパース
        const parts = txtData.split(',');
        for (const part of parts) {
          const addr = part.trim();
          if (addr) {
            nodes.push(addr);
          }
        }
      }
    }

    if (nodes.length === 0) {
      throw new Error('No valid nodes found in DNS records');
    }

    return nodes;
  }

  /**
   * DNS シードドメインからランダムにノードを選択して接続します
   * @param dnsSeedDomain DNS シードドメイン (例: "_nodes.seed.example.com")
   * @returns 新しい BrockchaRPCClient インスタンス
   */
  static async connectRandomNode(
    dnsSeedDomain: string,
    protocol?: Protocol
  ): Promise<BrockchaRPCClient> {
    const nodes = await this.discoverNodesDNS(dnsSeedDomain);

    if (nodes.length === 0) {
      throw new Error('No nodes discovered');
    }

    // ランダムにノードを選択
    const randomNode = nodes[Math.floor(Math.random() * nodes.length)];

    const parsed = this.parseSeedNode(randomNode);
    const selectedPort = FIXED_RPC_PORT;
    if (parsed.port != null && parsed.port !== FIXED_RPC_PORT) {
      throw new Error(`Discovered node uses unsupported RPC port ${parsed.port}; expected ${FIXED_RPC_PORT}`);
    }

    return new BrockchaRPCClient(parsed.host, {
      protocol: protocol ?? parsed.protocol ?? 'https',
    });
  }

  private static parseSeedNode(value: string): ParsedSeedNode {
    const text = value.trim();
    if (!text) {
      throw new Error('empty node record');
    }

    if (text.startsWith('http://') || text.startsWith('https://')) {
      const url = new URL(text);
      if (url.protocol !== 'https:') {
        throw new Error('seed node URL must use HTTPS');
      }
      const parsedPort = url.port ? parseInt(url.port, 10) : FIXED_RPC_PORT;
      return {
        host: url.hostname,
        port: parsedPort,
        protocol: 'https',
      };
    }

    // [ipv6]:port
    if (text.startsWith('[')) {
      const match = text.match(/^\[([^\]]+)\](?::(\d+))?$/);
      if (match) {
        return {
          host: match[1],
          port: match[2] ? parseInt(match[2], 10) : undefined,
        };
      }
    }

    // host:port (IPv4/hostname)
    const hostPortMatch = text.match(/^([^:]+):(\d+)$/);
    if (hostPortMatch) {
      return {
        host: hostPortMatch[1],
        port: parseInt(hostPortMatch[2], 10),
      };
    }

    return { host: text };
  }
}
