package controller

import (
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"io"
	"strings"
)

const oneTimeCodeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var oneTimeCodeEncoding = base32.NewEncoding(oneTimeCodeAlphabet).WithPadding(base32.NoPadding)

// newOneTimeCode 从 128-bit 随机值生成适合人工输入的 Crockford Base32 code。
// prefix 用于阻止手机 activation 与 daemon enrollment 在错误入口被交叉消费。
func newOneTimeCode(random io.Reader, prefix string) (string, error) {
	data := make([]byte, 16)
	if _, err := io.ReadFull(random, data); err != nil {
		return "", err
	}
	encoded := oneTimeCodeEncoding.EncodeToString(data)
	return prefix + "-" + encoded[:4] + "-" + encoded[4:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

// normalizeOneTimeCode 只接受指定领域前缀和完整 128-bit code，不做模糊字符替换。
func normalizeOneTimeCode(raw, prefix string) (string, error) {
	compact := strings.ToUpper(strings.TrimSpace(raw))
	compact = strings.ReplaceAll(compact, "-", "")
	wantPrefix := strings.ToUpper(prefix)
	if !strings.HasPrefix(compact, wantPrefix) {
		return "", fmt.Errorf("invalid one-time code prefix")
	}
	body := strings.TrimPrefix(compact, wantPrefix)
	if len(body) != 26 {
		return "", fmt.Errorf("invalid one-time code length")
	}
	if _, err := oneTimeCodeEncoding.DecodeString(body); err != nil {
		return "", fmt.Errorf("invalid one-time code alphabet")
	}
	return wantPrefix + "-" + body[:4] + "-" + body[4:8] + "-" + body[8:12] + "-" + body[12:16] + "-" + body[16:20] + "-" + body[20:], nil
}

// oneTimeCodeDigest 返回持久化和查找使用的固定长度摘要；明文不得写入数据库或日志。
func oneTimeCodeDigest(code string) []byte {
	digest := sha256.Sum256([]byte(code))
	return digest[:]
}
