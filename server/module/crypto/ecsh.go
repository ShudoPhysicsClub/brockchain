package crypto

import (
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"math/big"
)

var curve = elliptic.P256()
var params = curve.Params()

// --- 型定義 ---

type PrivateKey [32]byte
type PublicKey [64]byte // X(32) + Y(32)
type Signature [96]byte // Rx(32) + Ry(32) + S(32)

// --- 変換ユーティリティ ---

func bigToBytes32(n *big.Int) [32]byte {
	var b [32]byte
	nb := n.Bytes()
	copy(b[32-len(nb):], nb)
	return b
}

func bytesToBig(b []byte) *big.Int {
	return new(big.Int).SetBytes(b)
}

func sha256Hash(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

func hmacSha256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func concat(arrays ...[]byte) []byte {
	var total int
	for _, a := range arrays {
		total += len(a)
	}
	out := make([]byte, 0, total)
	for _, a := range arrays {
		out = append(out, a...)
	}
	return out
}

// --- RFC6979 決定論的ノンス生成 ---

func generateK(priv PrivateKey, msg []byte) *big.Int {
	qLen := 32
	h1 := sha256Hash(msg)

	V := make([]byte, qLen)
	for i := range V {
		V[i] = 0x01
	}
	K := make([]byte, qLen) // 0x00で初期化済み

	b0 := []byte{0x00}
	b1 := []byte{0x01}

	K = hmacSha256(K, concat(V, b0, priv[:], h1))
	V = hmacSha256(K, V)
	K = hmacSha256(K, concat(V, b1, priv[:], h1))
	V = hmacSha256(K, V)

	for {
		var T []byte
		for len(T) < qLen {
			V = hmacSha256(K, V)
			T = append(T, V...)
		}

		k := new(big.Int).SetBytes(T[:qLen])
		if k.Sign() >= 1 && k.Cmp(params.N) < 0 {
			return k
		}

		K = hmacSha256(K, concat(V, b0))
		V = hmacSha256(K, V)
	}
}

// --- 公開鍵導出 ---

func DerivePublicKey(priv PrivateKey) (PublicKey, error) {
	d := bytesToBig(priv[:])
	if d.Sign() <= 0 || d.Cmp(params.N) >= 0 {
		return PublicKey{}, errors.New("ecsh: invalid private key")
	}

	x, y := curve.ScalarBaseMult(priv[:])

	var pub PublicKey
	xb := bigToBytes32(x)
	yb := bigToBytes32(y)
	copy(pub[:32], xb[:])
	copy(pub[32:], yb[:])
	return pub, nil
}

// --- 署名 ---

func Sign(priv PrivateKey, msg []byte) (Signature, error) {
	d := bytesToBig(priv[:])
	if d.Sign() <= 0 || d.Cmp(params.N) >= 0 {
		return Signature{}, errors.New("ecsh: invalid private key")
	}

	pub, err := DerivePublicKey(priv)
	if err != nil {
		return Signature{}, err
	}

	// RFC6979で決定論的にkを生成
	k := generateK(priv, msg)

	Rx, Ry := curve.ScalarBaseMult(k.Bytes())

	h := sha256.New()
	h.Write(Rx.Bytes())
	h.Write(Ry.Bytes())
	h.Write(pub[:32]) // Yx
	h.Write(pub[32:]) // Yy
	h.Write(msg)
	e := new(big.Int).SetBytes(h.Sum(nil))
	e.Mod(e, params.N)

	s := new(big.Int).Mul(e, d)
	s.Add(s, k)
	s.Mod(s, params.N)

	var sig Signature
	rxb := bigToBytes32(Rx)
	ryb := bigToBytes32(Ry)
	sb := bigToBytes32(s)
	copy(sig[:32], rxb[:])
	copy(sig[32:64], ryb[:])
	copy(sig[64:], sb[:])
	return sig, nil
}

// --- 検証 ---

func Verify(pub PublicKey, msg []byte, sig Signature) bool {
	Yx := bytesToBig(pub[:32])
	Yy := bytesToBig(pub[32:])
	if !curve.IsOnCurve(Yx, Yy) {
		return false
	}

	Rx := bytesToBig(sig[:32])
	Ry := bytesToBig(sig[32:64])
	s := bytesToBig(sig[64:])
	if s.Sign() <= 0 || s.Cmp(params.N) >= 0 {
		return false
	}

	h := sha256.New()
	h.Write(sig[:32])  // Rx
	h.Write(sig[32:64]) // Ry
	h.Write(pub[:32])  // Yx
	h.Write(pub[32:])  // Yy
	h.Write(msg)
	e := new(big.Int).SetBytes(h.Sum(nil))
	e.Mod(e, params.N)

	sGx, sGy := curve.ScalarBaseMult(s.Bytes())
	eYx, eYy := curve.ScalarMult(Yx, Yy, e.Bytes())
	checkX, checkY := curve.Add(Rx, Ry, eYx, eYy)

	return sGx.Cmp(checkX) == 0 && sGy.Cmp(checkY) == 0
}