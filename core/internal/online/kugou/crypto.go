package kugou

// Request signing and the small amount of transport crypto Kugou's client
// protocol requires (device registration, login payloads). Protocol knowledge
// from KuGouMusicApi (MIT, MakcRe); implementation is our own. None of this
// touches audio: there is no decryption of protected formats here.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	mrand "math/rand/v2"
	"sort"
	"strings"
)

const (
	appID      = "1005"
	clientVer  = "20489"
	srcAppID   = "2919"
	saltAndr   = "OIlwieks28dk2k092lksi2UIkp"
	saltWeb    = "NVPh5oo715z5DIWAeQlhMDsWXXQV4hwt"
	saltKey    = "57ae12eb6890223e355ccfcb74edf70d"
	userAgent  = "Android15-1070-11083-46-0-DiscoveryDRADProtocol-wifi"
	tokenKey   = "90b8382a1bb4ccdcf063102053fd75b8"
	tokenIV    = "f063102053fd75b8"
	pubKeyPEM  = "-----BEGIN PUBLIC KEY-----\nMIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDIAG7QOELSYoIJvTFJhMpe1s/gbjDJX51HBNnEl5HXqTW6lQ7LC8jr9fWZTwusknp+sVGzwd40MwP6U5yDE27M/X1+UR4tvOGOqp94TJtQ1EPnWGWXngpeIW5GxoQGao1rmYWAu6oi1z9XkChrsUdC6DJE5E221wf/4WLFxwAtRQIDAQAB\n-----END PUBLIC KEY-----"
	randomPool = "1234567890ABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

func md5hex(s string) string { h := md5.Sum([]byte(s)); return hex.EncodeToString(h[:]) }

func sortedKV(p map[string]string, sep string) string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + sep + p[k])
	}
	return b.String()
}

// signAndroid: md5(salt + sorted "k=v" + body + salt).
func signAndroid(p map[string]string, body string) string {
	return md5hex(saltAndr + sortedKV(p, "=") + body + saltAndr)
}

// signWeb: md5(salt + sorted("k=v" strings) + body + salt). Sorting whole
// "k=v" strings differs from sorting keys only when a key prefixes another.
func signWeb(p map[string]string, body string) string {
	kv := make([]string, 0, len(p))
	for k, v := range p {
		kv = append(kv, k+"="+v)
	}
	sort.Strings(kv)
	return md5hex(saltWeb + strings.Join(kv, "") + body + saltWeb)
}

// signKey authorises song_url: md5(hash + salt + appid + mid + userid).
func signKey(hash, mid, userid string) string {
	if userid == "" {
		userid = "0"
	}
	return md5hex(hash + saltKey + appID + mid + userid)
}

// signParamsKey: md5(appid + salt + clientver + data).
func signParamsKey(data string) string { return md5hex(appID + saltAndr + clientVer + data) }

// calcMid turns md5(guid) into its decimal value (the "mid" device id).
func calcMid(guid string) string {
	n, _ := new(big.Int).SetString(md5hex(guid), 16)
	return n.String()
}

func randomString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = randomPool[mrand.IntN(len(randomPool))]
	}
	return string(b)
}

func newGUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func pad7(b []byte) []byte {
	p := aes.BlockSize - len(b)%aes.BlockSize
	out := append([]byte{}, b...)
	for i := 0; i < p; i++ {
		out = append(out, byte(p))
	}
	return out
}

func unpad7(b []byte) ([]byte, error) {
	if len(b) == 0 || len(b)%aes.BlockSize != 0 {
		return nil, errors.New("kugou: bad cipher length")
	}
	p := int(b[len(b)-1])
	if p == 0 || p > aes.BlockSize {
		return nil, errors.New("kugou: bad padding")
	}
	return b[:len(b)-p], nil
}

func aesCBC(plain []byte, key, iv string) []byte {
	blk, _ := aes.NewCipher([]byte(key))
	src := pad7(plain)
	out := make([]byte, len(src))
	cipher.NewCBCEncrypter(blk, []byte(iv)).CryptBlocks(out, src)
	return out
}

func aesCBCDecrypt(ct []byte, key, iv string) ([]byte, error) {
	blk, err := aes.NewCipher([]byte(key))
	if err != nil {
		return nil, err
	}
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return nil, errors.New("kugou: bad cipher length")
	}
	out := make([]byte, len(ct))
	cipher.NewCBCDecrypter(blk, []byte(iv)).CryptBlocks(out, ct)
	return unpad7(out)
}

// aesTemp encrypts with a random temp key: key = md5(temp)[:32], iv = key[16:].
// Returns hex ciphertext and the temp key (sent RSA-wrapped).
func aesTemp(plain []byte) (hexCT, temp string) {
	temp = strings.ToLower(randomString(16))
	k := md5hex(temp)
	return hex.EncodeToString(aesCBC(plain, k, k[16:])), temp
}

func aesTempDecrypt(hexCT, temp string) ([]byte, error) {
	ct, err := hex.DecodeString(hexCT)
	if err != nil {
		return nil, err
	}
	k := md5hex(temp)
	return aesCBCDecrypt(ct, k, k[16:])
}

// devAES is the register_dev body cipher: 6-char key, AES key/iv = halves of md5(key), base64.
func devAES(plain []byte) (b64, key string) {
	key = strings.ToLower(randomString(6))
	m := md5hex(key)
	return base64.StdEncoding.EncodeToString(aesCBC(plain, m[:16], m[16:])), key
}

func devAESDecrypt(ct []byte, key string) ([]byte, error) {
	m := md5hex(key)
	return aesCBCDecrypt(ct, m[:16], m[16:])
}

var pubKey = func() *rsa.PublicKey {
	blk, _ := pem.Decode([]byte(pubKeyPEM))
	k, err := x509.ParsePKIXPublicKey(blk.Bytes)
	if err != nil {
		panic(err)
	}
	return k.(*rsa.PublicKey)
}()

// rsaRaw: textbook RSA of the message right-padded with zeros to key size (hex).
func rsaRaw(msg []byte) string {
	n := pubKey.Size()
	buf := make([]byte, n)
	copy(buf, msg)
	c := new(big.Int).Exp(new(big.Int).SetBytes(buf), big.NewInt(int64(pubKey.E)), pubKey.N)
	out := c.Text(16)
	return strings.Repeat("0", n*2-len(out)) + out
}

// rsaPKCS1 is RSAES-PKCS1-v1_5 (hex).
func rsaPKCS1(msg []byte) string {
	//lint:ignore SA1019 the Kugou protocol mandates PKCS1 v1.5 (interop only)
	ct, err := rsa.EncryptPKCS1v15(rand.Reader, pubKey, msg)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(ct)
}
