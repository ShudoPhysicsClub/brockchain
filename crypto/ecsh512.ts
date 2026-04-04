class Ecsh512 {
    private readonly SHA256_K = new Uint32Array([
        0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1,
        0x923f82a4, 0xab1c5ed5, 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
        0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786,
        0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
        0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147,
        0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
        0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b,
        0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
        0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a,
        0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
        0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
    ]);
    private readonly _W = new Uint32Array(64);
    private readonly B =
        4696553609161847937052038347733161511493904362932400438595714524902166517950954342787702028369333676320252689983913848279306011332680415377270051799273732n;
    private readonly P =
        8993676107499813711178921708888238675809847154979649697732348694180432970307063331880010886955582241693580851883689196267674719065903039407289509124576261n;
    private readonly N =
        8993676107499813711178921708888238675809847154979649697732348694180432970307246943191793629927575657800015493680743190744519687410824180990782677655073143n;

    private readonly G: [bigint, bigint] = [
        4743451361895636895739495439656986070167124223519648767913608457993138307613237186699583694502266936424816440302321988866075874618444846383285241868444989n,
        3562637809195735506617822131827424483494791016489737717231873533375631670187155667833017888583892437971062256610397506961047206731101804772111060962144047n,
    ];

    // GLV endomorphism constants derived for this curve/group.
    private readonly BETA =
        3515579910056611758623182072848352381065045366998650234231334343181987947565090424135397442725122203649157144018375998066857304635874341650457679138802323n;
    private readonly LAMBDA =
        2374659027882127318980423184194060980182222403529138393372330335193277480456133372915393837428022834271640736194257766890190515362958282811390275400652474n;
    private readonly GLV_A1 =
        27456575505208260309404568450007990971964881991062530683661732555104187659031n;
    private readonly GLV_B1 =
        -78077368138767355842005768992316903041014797426952907118739925469032171418926n;
    private readonly GLV_A2 =
        105533943643975616151410337442324894012979679418015437802401658024136359077957n;
    private readonly GLV_B2 =
        27456575505208260309404568450007990971964881991062530683661732555104187659031n;

    private readonly BYTE_LEN = 64;
    private readonly _shifts: bigint[] = (() => {
        const shifts: bigint[] = [];
        for (let i = 0; i < this.BYTE_LEN * 8; i += 8) {
            shifts.push(BigInt(i));
        }
        return shifts;
    })();

    // Fixed-base precomputation for G with 8-bit windows.
    // This mirrors secp256k1.ts optimization and speeds up scalarMultG significantly.
    private readonly G_precomp_window: [bigint, bigint, bigint][][] = (() => {
        const table: [bigint, bigint, bigint][][] = [];
        let base: [bigint, bigint, bigint] = [this.G[0], this.G[1], 1n];
        for (let i = 0; i < this.BYTE_LEN; i++) {
            const row: [bigint, bigint, bigint][] = new Array(256);
            row[0] = [0n, 1n, 0n];
            row[1] = base;
            for (let j = 2; j < 256; j++) {
                row[j] = this.addJJ(row[j - 1], base);
            }
            table.push(row);
            for (let j = 0; j < 8; j++) {
                base = this.dblJ(base);
            }
        }
        return table;
    })();

    private m(x: bigint): bigint {
        const r = x % this.P;
        return r < 0n ? r + this.P : r;
    }

    private addMJ(
        p1: [bigint, bigint, bigint],
        x2: bigint,
        y2: bigint,
    ): [bigint, bigint, bigint] {
        const [x1, y1, z1] = p1;
        if (z1 === 0n) return [x2, y2, 1n];

        const p = this.P;
        const z1z1 = (z1 * z1) % p;
        const u2 = (x2 * z1z1) % p;
        const s2 = (y2 * ((z1z1 * z1) % p)) % p;
        const h = this.m(u2 - x1);
        const rr = this.m(s2 - y1);
        if (h === 0n) {
            if (rr === 0n) return this.dblJ(p1);
            return [0n, 1n, 0n];
        }
        const hh = (h * h) % p;
        const hhh = (hh * h) % p;
        const u1hh = (x1 * hh) % p;
        const x3 = this.m(rr * rr - hhh - 2n * u1hh);
        const y3 = this.m(rr * this.m(u1hh - x3) - y1 * hhh);
        const z3 = (h * z1) % p;
        return [x3, y3, z3];
    }

    private addJJ(
        p1: [bigint, bigint, bigint],
        q: [bigint, bigint, bigint],
    ): [bigint, bigint, bigint] {
        const [x1, y1, z1] = p1;
        const [x2, y2, z2] = q;
        if (z1 === 0n) return q;
        if (z2 === 0n) return p1;
        if (z2 === 1n) return this.addMJ(p1, x2, y2);
        if (z1 === 1n) return this.addMJ(q, x1, y1);

        const p = this.P;
        const z1z1 = (z1 * z1) % p;
        const z2z2 = (z2 * z2) % p;
        const u1 = (x1 * z2z2) % p;
        const u2 = (x2 * z1z1) % p;
        const s1 = (y1 * ((z2z2 * z2) % p)) % p;
        const s2 = (y2 * ((z1z1 * z1) % p)) % p;
        const h = this.m(u2 - u1);
        const rr = this.m(s2 - s1);
        if (h === 0n) {
            if (rr === 0n) return this.dblJ(p1);
            return [0n, 1n, 0n];
        }
        const hh = (h * h) % p;
        const hhh = (hh * h) % p;
        const u1hh = (u1 * hh) % p;
        const x3 = this.m(rr * rr - hhh - 2n * u1hh);
        const y3 = this.m(rr * this.m(u1hh - x3) - s1 * hhh);
        const z3 = (((h * z1) % p) * z2) % p;
        return [x3, y3, z3];
    }

    // a=0 の短 Weierstrass 倍算式
    private dblJ(pt: [bigint, bigint, bigint]): [bigint, bigint, bigint] {
        const [x, y, z] = pt;
        if (z === 0n) return pt;
        const p = this.P;
        const yy = (y * y) % p;
        const yyyy = (yy * yy) % p;
        const s = (4n * ((x * yy) % p)) % p;
        const m = (3n * ((x * x) % p)) % p;
        const x3 = this.m(m * m - 2n * s);
        const y3 = this.m(m * this.m(s - x3) - 8n * yyyy);
        return [x3, y3, (2n * ((y * z) % p)) % p];
    }

    private inv(a: bigint, mod: bigint): bigint {
        let aa = a % mod;
        if (aa < 0n) aa += mod;
        if (aa === 0n) throw new Error('inv: a == 0');
        let t = 0n;
        let newT = 1n;
        let r = mod;
        let newR = aa;
        while (newR !== 0n) {
            const q = r / newR;
            [t, newT] = [newT, t - q * newT];
            [r, newR] = [newR, r - q * newR];
        }
        if (r !== 1n) throw new Error('inv: not invertible');
        return t < 0n ? t + mod : t;
    }

    private toAffine(pt: [bigint, bigint, bigint]): [bigint, bigint] {
        if (pt[2] === 0n) return [0n, 0n];
        const zInv = this.inv(pt[2], this.P);
        const zInv2 = this.m(zInv * zInv);
        const zInv3 = this.m(zInv2 * zInv);
        return [this.m(pt[0] * zInv2), this.m(pt[1] * zInv3)];
    }

    private scalarMultGJac(k: bigint): [bigint, bigint, bigint] {
        const shifts = this._shifts;
        const first = Number(k & 0xffn);
        let r: [bigint, bigint, bigint] = [...this.G_precomp_window[0][first]];
        for (let i = 1; i < this.BYTE_LEN; i++) {
            const win = Number((k >> shifts[i]) & 0xffn);
            if (win !== 0) {
                r = this.addJJ(r, this.G_precomp_window[i][win]);
            }
        }
        return r;
    }

    private scalarMultG(k: bigint): [bigint, bigint] {
        return this.toAffine(this.scalarMultGJac(k));
    }

    private readonly BETA2 = this.m(this.BETA * this.BETA);

    private affineEquals(a: [bigint, bigint], b: [bigint, bigint]): boolean {
        return this.m(a[0]) === this.m(b[0]) && this.m(a[1]) === this.m(b[1]);
    }

    private selectGlvBeta(): bigint {
        const lambdaG = this.toAffine(this.scalarMultWNAF5JacPlain(this.G, this.LAMBDA));
        const phiWithBeta: [bigint, bigint] = [(this.BETA * this.G[0]) % this.P, this.G[1]];
        if (this.affineEquals(lambdaG, phiWithBeta)) {
            return this.BETA;
        }
        const phiWithBeta2: [bigint, bigint] = [(this.BETA2 * this.G[0]) % this.P, this.G[1]];
        if (this.affineEquals(lambdaG, phiWithBeta2)) {
            return this.BETA2;
        }
        throw new Error('GLV beta/lambda pairing mismatch');
    }

    private readonly GLV_BETA = this.selectGlvBeta();

    private roundDivNearest(a: bigint, b: bigint): bigint {
        if (b === 0n) throw new Error('roundDivNearest: division by zero');
        let aa = a;
        let bb = b;
        if (bb < 0n) {
            aa = -aa;
            bb = -bb;
        }
        if (aa >= 0n) {
            return (aa + bb / 2n) / bb;
        }
        return -((-aa + bb / 2n) / bb);
    }

    private glvDecompose(k: bigint): [bigint, bigint] {
        const c1 = this.roundDivNearest(k * this.GLV_B2, this.N);
        const c2 = this.roundDivNearest(-k * this.GLV_B1, this.N);

        let k1 = k - c1 * this.GLV_A1 - c2 * this.GLV_A2;
        let k2 = -(c1 * this.GLV_B1 + c2 * this.GLV_B2);

        k1 %= this.N;
        k2 %= this.N;
        if (k1 > (this.N >> 1n)) k1 -= this.N;
        if (k1 < -(this.N >> 1n)) k1 += this.N;
        if (k2 > (this.N >> 1n)) k2 -= this.N;
        if (k2 < -(this.N >> 1n)) k2 += this.N;
        return [k1, k2];
    }

    private toWNAF5(k: bigint): number[] {
        const naf: number[] = [];
        let kk = k < 0n ? -k : k;
        while (kk > 0n) {
            if (kk & 1n) {
                let digit = Number(kk & 0x1fn);
                if (digit >= 16) digit -= 32;
                kk -= BigInt(digit);
                naf.push(digit);
            } else {
                naf.push(0);
            }
            kk >>= 1n;
        }
        return k < 0n ? naf.map((d) => -d) : naf;
    }

    private scalarMultWNAF5JacPlain(
        pt: [bigint, bigint],
        k: bigint,
    ): [bigint, bigint, bigint] {
        if (k === 0n) return [0n, 1n, 0n];
        let point = pt;
        let kk = k;
        if (kk < 0n) {
            point = [point[0], this.m(-point[1])];
            kk = -kk;
        }

        const p = this.P;
        const table: [bigint, bigint, bigint][] = new Array(16);
        table[0] = [point[0], point[1], 1n];
        const p2 = this.dblJ(table[0]);
        for (let i = 1; i < 16; i++) {
            table[i] = this.addJJ(table[i - 1], p2);
        }

        const naf: number[] = [];
        while (kk > 0n) {
            if (kk & 1n) {
                let digit = Number(kk & 0x1fn);
                if (digit >= 16) digit -= 32;
                kk -= BigInt(digit);
                naf.push(digit);
            } else {
                naf.push(0);
            }
            kk >>= 1n;
        }

        let r: [bigint, bigint, bigint] = [0n, 1n, 0n];
        for (let i = naf.length - 1; i >= 0; i--) {
            r = this.dblJ(r);
            const d = naf[i];
            if (d > 0) {
                r = this.addJJ(r, table[(d - 1) >> 1]);
            } else if (d < 0) {
                const [tx, ty, tz] = table[(-d - 1) >> 1];
                r = this.addJJ(r, [tx, (p - ty) % p, tz]);
            }
        }
        return r;
    }

    private scalarMultWNAF5Jac(
        pt: [bigint, bigint],
        k: bigint,
    ): [bigint, bigint, bigint] {
        if (k === 0n) return [0n, 1n, 0n];
        const p = this.P;

        const [k1, k2] = this.glvDecompose(k);
        const phiPt: [bigint, bigint] = [(this.GLV_BETA * pt[0]) % p, pt[1]];

        const table1: [bigint, bigint, bigint][] = new Array(16);
        const table2: [bigint, bigint, bigint][] = new Array(16);
        table1[0] = [pt[0], pt[1], 1n];
        table2[0] = [phiPt[0], phiPt[1], 1n];
        const p2a = this.dblJ(table1[0]);
        const p2b = this.dblJ(table2[0]);
        for (let i = 1; i < 16; i++) {
            table1[i] = this.addJJ(table1[i - 1], p2a);
            table2[i] = this.addJJ(table2[i - 1], p2b);
        }

        const naf1 = this.toWNAF5(k1);
        const naf2 = this.toWNAF5(k2);
        const len = Math.max(naf1.length, naf2.length);

        let r: [bigint, bigint, bigint] = [0n, 1n, 0n];
        for (let i = len - 1; i >= 0; i--) {
            r = this.dblJ(r);

            const d1 = i < naf1.length ? naf1[i] : 0;
            const d2 = i < naf2.length ? naf2[i] : 0;

            if (d1 > 0) {
                r = this.addJJ(r, table1[(d1 - 1) >> 1]);
            } else if (d1 < 0) {
                const [tx, ty, tz] = table1[(-d1 - 1) >> 1];
                r = this.addJJ(r, [tx, (p - ty) % p, tz]);
            }

            if (d2 > 0) {
                r = this.addJJ(r, table2[(d2 - 1) >> 1]);
            } else if (d2 < 0) {
                const [tx, ty, tz] = table2[(-d2 - 1) >> 1];
                r = this.addJJ(r, [tx, (p - ty) % p, tz]);
            }
        }

        return r;
    }

    public isPointOnCurve(pt: [bigint, bigint]): boolean {
        const [x, y] = pt;
        if (x === 0n && y === 0n) return false;
        const p = this.P;
        const x2 = (x * x) % p;
        const rhs = ((x2 * x) % p + this.B) % p;
        return (y * y) % p === rhs;
    }

    public sign(
        message: Uint8Array,
        privKey: Uint8Array,
    ): [Uint8Array, Uint8Array, Uint8Array] {
        const messageBigint = this.bytesToBigInt(message);
        const privKeyBigint = this.bytesToBigInt(privKey);
        const mB = this.bigintToBytes(messageBigint);
        const k = this.generateK(mB, this.bigintToBytes(privKeyBigint));
        const rPoint = this.scalarMultG(k);
        const e =
            this.bytesToBigInt(
                this.sha256(
                    this.concat(
                        this.bigintToBytes(rPoint[0]),
                        this.bigintToBytes(rPoint[1]),
                        mB,
                    ),
                ),
            ) % this.N;
        if (e === 0n) throw new Error('e == 0, retry');
        const s = (k + privKeyBigint * e) % this.N;
        return [
            this.bigintToBytes(rPoint[0]),
            this.bigintToBytes(rPoint[1]),
            this.bigintToBytes(s),
        ];
    }

    public verify(
        message: Uint8Array,
        pubKey: [Uint8Array, Uint8Array],
        signature: [Uint8Array, Uint8Array, Uint8Array],
    ): boolean {
        const messageBigint = this.bytesToBigInt(message);
        const pubKeyBigint: [bigint, bigint] = [
            this.bytesToBigInt(pubKey[0]),
            this.bytesToBigInt(pubKey[1]),
        ];
        const rPoint: [bigint, bigint] = [
            this.bytesToBigInt(signature[0]),
            this.bytesToBigInt(signature[1]),
        ];
        const e =
            this.bytesToBigInt(
                this.sha256(
                    this.concat(
                        this.bigintToBytes(rPoint[0]),
                        this.bigintToBytes(rPoint[1]),
                        this.bigintToBytes(messageBigint),
                    ),
                ),
            ) % this.N;
        if (e === 0n) return false;
        const s = this.bytesToBigInt(signature[2]);
        if (s === 0n) return false;

        const negE = this.N - e;
        const sG = this.scalarMultGJac(s);
        const negEP = this.scalarMultWNAF5Jac(pubKeyBigint, negE);
        const lhs = this.addJJ(sG, negEP);
        if (lhs[2] === 0n) return false;

        const z2 = this.m(lhs[2] * lhs[2]);
        const z3 = this.m(z2 * lhs[2]);
        return (
            this.m(lhs[0]) === this.m(rPoint[0] * z2) &&
            this.m(lhs[1]) === this.m(rPoint[1] * z3)
        );
    }

    public generateKeyPair(): {
        privateKey: Uint8Array;
        publicKey: [Uint8Array, Uint8Array];
    } {
        const priv = this.getRandomBigInt(this.N);
        const pub = this.scalarMultG(priv);
        return {
            privateKey: this.bigintToBytes(priv),
            publicKey: [this.bigintToBytes(pub[0]), this.bigintToBytes(pub[1])],
        };
    }

    public sha256(data: Uint8Array): Uint8Array {
        const K = this.SHA256_K;
        const W = this._W;
        const rotr = (x: number, n: number) => (x >>> n) | (x << (32 - n));

        let h0 = 0x6a09e667;
        let h1 = 0xbb67ae85;
        let h2 = 0x3c6ef372;
        let h3 = 0xa54ff53a;
        let h4 = 0x510e527f;
        let h5 = 0x9b05688c;
        let h6 = 0x1f83d9ab;
        let h7 = 0x5be0cd19;

        const len = data.length;
        const bitLen = len * 8;
        const blockCount = Math.ceil((len + 9) / 64);
        const blocks = new Uint8Array(blockCount * 64);
        blocks.set(data);
        blocks[len] = 0x80;

        const view = new DataView(blocks.buffer);
        view.setUint32(blocks.length - 8, Math.floor(bitLen / 0x100000000), false);
        view.setUint32(blocks.length - 4, bitLen >>> 0, false);

        for (let i = 0; i < blocks.length; i += 64) {
            for (let t = 0; t < 16; t++) W[t] = view.getUint32(i + t * 4, false);
            for (let t = 16; t < 64; t++) {
                const s0 = rotr(W[t - 15], 7) ^ rotr(W[t - 15], 18) ^ (W[t - 15] >>> 3);
                const s1 = rotr(W[t - 2], 17) ^ rotr(W[t - 2], 19) ^ (W[t - 2] >>> 10);
                W[t] = (W[t - 16] + s0 + W[t - 7] + s1) >>> 0;
            }

            let a = h0;
            let b = h1;
            let c = h2;
            let d = h3;
            let e = h4;
            let f = h5;
            let g = h6;
            let h = h7;

            for (let t = 0; t < 64; t++) {
                const S1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
                const ch = (e & f) ^ ((~e >>> 0) & g);
                const temp1 = (h + S1 + ch + K[t] + W[t]) >>> 0;
                const S0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
                const maj = (a & b) ^ (a & c) ^ (b & c);
                const temp2 = (S0 + maj) >>> 0;

                h = g;
                g = f;
                f = e;
                e = (d + temp1) >>> 0;
                d = c;
                c = b;
                b = a;
                a = (temp1 + temp2) >>> 0;
            }

            h0 = (h0 + a) >>> 0;
            h1 = (h1 + b) >>> 0;
            h2 = (h2 + c) >>> 0;
            h3 = (h3 + d) >>> 0;
            h4 = (h4 + e) >>> 0;
            h5 = (h5 + f) >>> 0;
            h6 = (h6 + g) >>> 0;
            h7 = (h7 + h) >>> 0;
        }

        const result = new Uint8Array(32);
        const rv = new DataView(result.buffer);
        rv.setUint32(0, h0, false);
        rv.setUint32(4, h1, false);
        rv.setUint32(8, h2, false);
        rv.setUint32(12, h3, false);
        rv.setUint32(16, h4, false);
        rv.setUint32(20, h5, false);
        rv.setUint32(24, h6, false);
        rv.setUint32(28, h7, false);
        return result;
    }

    private hmacSha256(
        key: Uint8Array,
        data: Uint8Array,
    ): Uint8Array {
        const block = 64;
        const k = key.length > block ? this.sha256(key) : key;
        const kp = new Uint8Array(block);
        kp.set(k);
        const ipad = new Uint8Array(block);
        const opad = new Uint8Array(block);
        for (let i = 0; i < block; i++) {
            ipad[i] = kp[i] ^ 0x36;
            opad[i] = kp[i] ^ 0x5c;
        }
        return this.sha256(this.concat(opad, this.sha256(this.concat(ipad, data))));
    }

    private generateK(message: Uint8Array, privateKey: Uint8Array): bigint {
        const qLen = this.BYTE_LEN;
        const hLen = 32;
        const h1 = this.sha256(message);
        let v: Uint8Array<ArrayBufferLike> = new Uint8Array(hLen).fill(0x01);
        let k: Uint8Array<ArrayBufferLike> = new Uint8Array(hLen).fill(0x00);
        const b0 = new Uint8Array([0x00]);
        const b1 = new Uint8Array([0x01]);

        k = this.hmacSha256(k, this.concat(v, b0, privateKey, h1));
        v = this.hmacSha256(k, v);
        k = this.hmacSha256(k, this.concat(v, b1, privateKey, h1));
        v = this.hmacSha256(k, v);

        while (true) {
            let t = new Uint8Array(0);
            while (t.length < qLen) {
                v = this.hmacSha256(k, v);
                const next = new Uint8Array(t.length + v.length);
                next.set(t);
                next.set(v, t.length);
                t = next;
            }
            const nonce = this.bytesToBigInt(t.subarray(0, qLen));
            if (nonce >= 1n && nonce < this.N) return nonce;
            k = this.hmacSha256(k, this.concat(v, b0));
            v = this.hmacSha256(k, v);
        }
    }

    private concat(...arrays: Uint8Array[]): Uint8Array {
        let total = 0;
        for (const a of arrays) total += a.length;
        const out = new Uint8Array(total);
        let off = 0;
        for (const a of arrays) {
            out.set(a, off);
            off += a.length;
        }
        return out;
    }

    private bigintToBytes(n: bigint): Uint8Array {
        const out = new Uint8Array(this.BYTE_LEN);
        let nn = n;
        for (let i = this.BYTE_LEN - 1; i >= 0; i--) {
            out[i] = Number(nn & 0xffn);
            nn >>= 8n;
        }
        return out;
    }

    private bytesToBigInt(bytes: Uint8Array): bigint {
        let out = 0n;
        for (const b of bytes) {
            out = (out << 8n) + BigInt(b);
        }
        return out;
    }

    private getRandomBigInt(max: bigint): bigint {
        const bytes = this.BYTE_LEN;
        let r: bigint;
        do {
            const b = new Uint8Array(bytes);
            globalThis.crypto.getRandomValues(b);
            r = this.bytesToBigInt(b);
        } while (r >= max || r === 0n);
        return r;
    }
}

const ecsh512 = new Ecsh512();

const enc = new TextEncoder();
const now = () => (typeof performance !== 'undefined' ? performance.now() : Date.now());

function runBasicCheck(): void {
    const message = enc.encode('Hello, world!');
    const { privateKey, publicKey } = ecsh512.generateKeyPair();
    const signature = ecsh512.sign(message, privateKey);
    const isValid = ecsh512.verify(message, publicKey, signature);
    console.log('Basic check valid:', isValid);
}

function runBenchmark(iterations: number): void {
    const fixedMessage = enc.encode('benchmark-message');
    const { privateKey, publicKey } = ecsh512.generateKeyPair();

    const tKeyStart = now();
    for (let i = 0; i < iterations; i++) {
        ecsh512.generateKeyPair();
    }
    const keyMs = now() - tKeyStart;

    const signatures: [Uint8Array, Uint8Array, Uint8Array][] = new Array(iterations);
    const tSignStart = now();
    for (let i = 0; i < iterations; i++) {
        signatures[i] = ecsh512.sign(fixedMessage, privateKey);
    }
    const signMs = now() - tSignStart;

    let verified = 0;
    const tVerifyStart = now();
    for (let i = 0; i < iterations; i++) {
        if (ecsh512.verify(fixedMessage, publicKey, signatures[i])) {
            verified++;
        }
    }
    const verifyMs = now() - tVerifyStart;

    const tAllStart = now();
    let fullOk = 0;
    for (let i = 0; i < iterations; i++) {
        const msg = enc.encode(`bench-${i}`);
        const sig = ecsh512.sign(msg, privateKey);
        if (ecsh512.verify(msg, publicKey, sig)) {
            fullOk++;
        }
    }
    const allMs = now() - tAllStart;

    console.log(`Benchmark iterations: ${iterations}`);
    console.log(`  keygen  : ${keyMs.toFixed(2)} ms (${(keyMs / iterations).toFixed(3)} ms/op)`);
    console.log(`  sign    : ${signMs.toFixed(2)} ms (${(signMs / iterations).toFixed(3)} ms/op)`);
    console.log(`  verify  : ${verifyMs.toFixed(2)} ms (${(verifyMs / iterations).toFixed(3)} ms/op)`);
    console.log(`  sign+verify(mixed msg): ${allMs.toFixed(2)} ms (${(allMs / iterations).toFixed(3)} ms/op)`);
    console.log(`  verify success: ${verified}/${iterations}`);
    console.log(`  end-to-end success: ${fullOk}/${iterations}`);
}

async function runStandardEcdsaBenchmark(iterations: number): Promise<void> {
    const fixedMessage = enc.encode('benchmark-message');
    const keyPair = await globalThis.crypto.subtle.generateKey(
        { name: 'ECDSA', namedCurve: 'P-256' },
        true,
        ['sign', 'verify'],
    );

    const tSignStart = now();
    const signatures: ArrayBuffer[] = new Array(iterations);
    for (let i = 0; i < iterations; i++) {
        signatures[i] = await globalThis.crypto.subtle.sign(
            { name: 'ECDSA', hash: 'SHA-256' },
            keyPair.privateKey,
            fixedMessage,
        );
    }
    const signMs = now() - tSignStart;

    let verified = 0;
    const tVerifyStart = now();
    for (let i = 0; i < iterations; i++) {
        const ok = await globalThis.crypto.subtle.verify(
            { name: 'ECDSA', hash: 'SHA-256' },
            keyPair.publicKey,
            signatures[i],
            fixedMessage,
        );
        if (ok) verified++;
    }
    const verifyMs = now() - tVerifyStart;

    console.log(`Standard ECDSA benchmark iterations: ${iterations}`);
    console.log(`  sign    : ${signMs.toFixed(2)} ms (${(signMs / iterations).toFixed(3)} ms/op)`);
    console.log(`  verify  : ${verifyMs.toFixed(2)} ms (${(verifyMs / iterations).toFixed(3)} ms/op)`);
    console.log(`  verify success: ${verified}/${iterations}`);
}

async function runStandardEcdsaCheck(): Promise<void> {
    const message = enc.encode('Hello, normal ECDSA!');
    const keyPair = await globalThis.crypto.subtle.generateKey(
        { name: 'ECDSA', namedCurve: 'P-256' },
        true,
        ['sign', 'verify'],
    );

    const signature = await globalThis.crypto.subtle.sign(
        { name: 'ECDSA', hash: 'SHA-256' },
        keyPair.privateKey,
        message,
    );

    const valid = await globalThis.crypto.subtle.verify(
        { name: 'ECDSA', hash: 'SHA-256' },
        keyPair.publicKey,
        signature,
        message,
    );

    const tampered = enc.encode('Hello, normal ECDSA?');
    const tamperedValid = await globalThis.crypto.subtle.verify(
        { name: 'ECDSA', hash: 'SHA-256' },
        keyPair.publicKey,
        signature,
        tampered,
    );

    console.log('Standard ECDSA(P-256) valid:', valid);
    console.log('Standard ECDSA(P-256) tampered valid:', tamperedValid);
}

async function main(): Promise<void> {
    const iterations = 1000;
    await runStandardEcdsaCheck();
    await runStandardEcdsaBenchmark(iterations);
    runBasicCheck();
    runBenchmark(iterations);

    console.log('--- 比較（1回あたり） ---');
    console.log('上の出力の ms/op を比較してください。');
    console.log('目安: 小さいほど高速です。');
}

main().catch((err) => {
    console.error('Execution failed:', err);
    process.exitCode = 1;
});