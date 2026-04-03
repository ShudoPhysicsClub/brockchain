export interface TransactionLike {
  [key: string]: unknown;
}

export interface BlockLike {
  previous_hash?: string;
  previousHash?: string;
  timestamp: number;
  nonce: number;
  difficulty: number;
  miner: string;
  reward: string;
  transactions?: TransactionLike[];
}

function canonicalize(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(canonicalize);
  }

  if (value !== null && typeof value === 'object') {
    const record = value as Record<string, unknown>;
    const keys = Object.keys(record).sort();
    const out: Record<string, unknown> = {};
    for (const key of keys) {
      const normalized = canonicalize(record[key]);
      if (normalized !== undefined) {
        out[key] = normalized;
      }
    }
    return out;
  }

  return value;
}

export function canonicalJSONStringify(value: unknown): string {
  return JSON.stringify(canonicalize(value));
}

export function buildTransactionSigningPayload(tx: TransactionLike): Record<string, unknown> {
  const { signature: _signature, ...rest } = tx as Record<string, unknown>;
  return rest;
}

export function buildTransactionSigningMessage(tx: TransactionLike): Uint8Array {
  const json = canonicalJSONStringify(buildTransactionSigningPayload(tx));
  return new TextEncoder().encode(json);
}

export function buildBlockHashPayload(block: BlockLike): Record<string, unknown> {
  return {
    previous_hash: (block.previous_hash ?? block.previousHash ?? '') as string,
    timestamp: block.timestamp,
    nonce: block.nonce,
    difficulty: block.difficulty,
    miner: block.miner,
    reward: block.reward,
    transactions: block.transactions ?? [],
  };
}

export function buildBlockHashPreimage(block: BlockLike): Uint8Array {
  const json = canonicalJSONStringify(buildBlockHashPayload(block));
  return new TextEncoder().encode(json);
}

export async function sha256Hex(data: Uint8Array): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', data as unknown as BufferSource);
  const bytes = new Uint8Array(digest);
  let hex = '';
  for (const b of bytes) {
    hex += b.toString(16).padStart(2, '0');
  }
  return hex;
}

export async function calculateBlockHash(block: BlockLike): Promise<string> {
  return sha256Hex(buildBlockHashPreimage(block));
}