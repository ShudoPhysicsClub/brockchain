type Protocol = 'https';
interface ClientOptions {
    protocol?: Protocol;
    path?: string;
    allowMixedContent?: boolean;
}
export declare class BrockchaRPCClient {
    private endpoint;
    private nextId;
    constructor(host?: string, options?: ClientOptions);
    static fromEndpoint(endpoint: string, allowMixedContent?: boolean): BrockchaRPCClient;
    static fromCurrentOrigin(path?: string): BrockchaRPCClient;
    private static enforceMixedContent;
    private call;
    status(): Promise<any>;
    getToken(query: string): Promise<any>;
    getUser(query: string): Promise<any>;
    getPool(query: string): Promise<any>;
    submitTransaction(tx: any): Promise<any>;
    submitBlock(block: any): Promise<any>;
    rpc(method: string, params?: any): Promise<any>;
    /**
     * DNS-over-HTTPS (DoH) を使用して DNS TXT レコードからノードを検出します
     * @param dnsSeedDomain DNS シードドメイン (例: "_nodes.seed.example.com")
    * @returns ノードアドレスのリスト (例: ["host1:59988", "host2:59988"])
     */
    static discoverNodesDNS(dnsSeedDomain: string): Promise<string[]>;
    /**
     * DNS シードドメインからランダムにノードを選択して接続します
     * @param dnsSeedDomain DNS シードドメイン (例: "_nodes.seed.example.com")
     * @returns 新しい BrockchaRPCClient インスタンス
     */
    static connectRandomNode(dnsSeedDomain: string, protocol?: Protocol): Promise<BrockchaRPCClient>;
    private static parseSeedNode;
}
export {};
//# sourceMappingURL=client.d.ts.map