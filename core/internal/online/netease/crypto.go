package netease

// Request encryption for the NetEase Cloud Music web (weapi) and client
// (eapi) endpoints. Written from the publicly documented protocol; the
// constants are the platform's public client parameters (credit: MIT-licensed
// NeteaseCloudMusicApi Enhanced, see NOTICE). This is transport encoding of
// our own requests, not content decryption.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"strings"
)

const (
	weapiIV        = "0102030405060708"
	weapiPresetKey = "0CoJUm6Qyw8W8jud"
	eapiKey        = "e82ckenh8dichen8"
	base62         = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	weapiPubPEM    = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDgtQn2JZ34ZC28NWYpAUd98iZ37BUrX/aKzmFbt7clFSs6sXqHauqKWqdtLkF2KexO40H1YTX8z2lSgBBOAxLsvaklV8k4cBFK9snQXE9/DDaFt6Rr7iVZMldczhC0JNgTz+SHXT6CBHuX3e9SdB1Ua44oncaTWz7OBGLbCiK45wIDAQAB
-----END PUBLIC KEY-----`
)

var weapiPub = func() *rsa.PublicKey {
	b, _ := pem.Decode([]byte(weapiPubPEM))
	k, err := x509.ParsePKIXPublicKey(b.Bytes)
	if err != nil {
		panic(err)
	}
	return k.(*rsa.PublicKey)
}()

func pkcs7(b []byte) []byte {
	n := aes.BlockSize - len(b)%aes.BlockSize
	return append(b, bytes.Repeat([]byte{byte(n)}, n)...)
}

func unpkcs7(b []byte) ([]byte, error) {
	if len(b) == 0 || len(b)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("bad padding")
	}
	n := int(b[len(b)-1])
	if n == 0 || n > aes.BlockSize || n > len(b) {
		return nil, fmt.Errorf("bad padding")
	}
	return b[:len(b)-n], nil
}

func aesCBC(plain []byte, key, iv string) []byte {
	blk, _ := aes.NewCipher([]byte(key))
	src := pkcs7(plain)
	out := make([]byte, len(src))
	cipher.NewCBCEncrypter(blk, []byte(iv)).CryptBlocks(out, src)
	return out
}

func aesCBCDecrypt(ct []byte, key, iv string) ([]byte, error) {
	blk, err := aes.NewCipher([]byte(key))
	if err != nil {
		return nil, err
	}
	if len(ct)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("bad length")
	}
	out := make([]byte, len(ct))
	cipher.NewCBCDecrypter(blk, []byte(iv)).CryptBlocks(out, ct)
	return unpkcs7(out)
}

func aesECB(plain []byte, key string) []byte {
	blk, _ := aes.NewCipher([]byte(key))
	src := pkcs7(plain)
	out := make([]byte, len(src))
	for i := 0; i < len(src); i += aes.BlockSize {
		blk.Encrypt(out[i:], src[i:i+aes.BlockSize])
	}
	return out
}

func aesECBDecrypt(ct []byte, key string) ([]byte, error) {
	blk, _ := aes.NewCipher([]byte(key))
	if len(ct)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("bad length")
	}
	out := make([]byte, len(ct))
	for i := 0; i < len(ct); i += aes.BlockSize {
		blk.Decrypt(out[i:], ct[i:i+aes.BlockSize])
	}
	return unpkcs7(out)
}

// rsaNoPad is textbook RSA (m^e mod n) as the web client does, hex, 256 chars.
func rsaNoPad(msg []byte, pub *rsa.PublicKey) string {
	m := new(big.Int).SetBytes(msg)
	c := new(big.Int).Exp(m, big.NewInt(int64(pub.E)), pub.N)
	return fmt.Sprintf("%0256x", c)
}

var randReader io.Reader = rand.Reader

func randomKey() string {
	b := make([]byte, 16)
	io.ReadFull(randReader, b)
	var sb strings.Builder
	for _, x := range b {
		sb.WriteByte(base62[int(x)%len(base62)])
	}
	return sb.String()
}

// weapiEncrypt returns the form fields params and encSecKey.
func weapiEncrypt(text []byte) (params, encSecKey string) {
	return weapiWithKey(text, randomKey())
}

func weapiWithKey(text []byte, secret string) (string, string) {
	first := base64.StdEncoding.EncodeToString(aesCBC(text, weapiPresetKey, weapiIV))
	params := base64.StdEncoding.EncodeToString(aesCBC([]byte(first), secret, weapiIV))
	rev := []byte(secret)
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return params, rsaNoPad(rev, weapiPub)
}

// eapiEncrypt returns the hex "params" form field for uri (e.g. "/api/song/lyric").
func eapiEncrypt(uri string, text []byte) string {
	digest := md5.Sum([]byte("nobody" + uri + "use" + string(text) + "md5forencrypt"))
	data := uri + "-36cd479b6b5-" + string(text) + "-36cd479b6b5-" + hex.EncodeToString(digest[:])
	return strings.ToUpper(hex.EncodeToString(aesECB([]byte(data), eapiKey)))
}

// eapiDecryptParams reverses eapiEncrypt (used by tests and the fake server).
func eapiDecryptParams(params string) (uri string, text []byte, err error) {
	ct, err := hex.DecodeString(params)
	if err != nil {
		return "", nil, err
	}
	pt, err := aesECBDecrypt(ct, eapiKey)
	if err != nil {
		return "", nil, err
	}
	parts := strings.SplitN(string(pt), "-36cd479b6b5-", 3)
	if len(parts) != 3 {
		return "", nil, fmt.Errorf("bad eapi payload")
	}
	return parts[0], []byte(parts[1]), nil
}

func md5hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}
