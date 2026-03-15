package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config 签名验证配置
type SignatureConfig struct {
	Enabled          bool
	HeaderName       string        // 签名头名称，默认 X-Signature
	TimestampHeader  string        // 时间戳头名称，默认 X-Timestamp
	NonceHeader      string        // Nonce 头名称，默认 X-Nonce
	KeyIDHeader      string        // Key ID 头名称，默认 X-Key-ID
	MaxTimestampSkew time.Duration // 最大时间戳偏差，默认 5 分钟
	NonceCacheSize   int           // Nonce 缓存大小，默认 10000
	Algorithms       []string      // 支持的算法，默认 ["SHA256"]
}

// DefaultConfig 返回默认配置
func DefaultSignatureConfig() SignatureConfig {
	return SignatureConfig{
		Enabled:          false,
		HeaderName:       "X-Signature",
		TimestampHeader:  "X-Timestamp",
		NonceHeader:      "X-Nonce",
		KeyIDHeader:      "X-Key-ID",
		MaxTimestampSkew: 5 * time.Minute,
		NonceCacheSize:   10000,
		Algorithms:       []string{"SHA256"},
	}
}

// SignatureVerifier HMAC 签名验证器
type SignatureVerifier struct {
	config   SignatureConfig
	secrets  map[string]string // keyID -> secret
	nonces   *nonceCache
	mu       sync.RWMutex
}

// nonceCache 用于防止重放攻击的 nonce 缓存
type nonceCache struct {
	entries map[string]int64 // nonce -> timestamp
	maxSize int
	mu       sync.RWMutex
}

// newNonceCache 创建 nonce 缓存
func newNonceCache(maxSize int) *nonceCache {
	return &nonceCache{
		entries: make(map[string]int64),
		maxSize: maxSize,
	}
}

// Add 添加 nonce，返回是否成功（已存在则返回 false）
func (nc *nonceCache) Add(nonce string, timestamp int64) bool {
	nc.mu.Lock()
	defer nc.mu.Unlock()

	if _, exists := nc.entries[nonce]; exists {
		return false
	}

	// 如果缓存已满，清理过期的条目
	if len(nc.entries) >= nc.maxSize {
		nc.cleanup(timestamp)
	}

	nc.entries[nonce] = timestamp
	return true
}

// cleanup 清理过期的 nonce 条目（超过 1 小时的）
func (nc *nonceCache) cleanup(currentTimestamp int64) {
	expireThreshold := currentTimestamp - int64(time.Hour.Seconds())
	for nonce, ts := range nc.entries {
		if ts < expireThreshold {
			delete(nc.entries, nonce)
		}
	}
}

// NewSignatureVerifier 创建签名验证器
func NewSignatureVerifier(config SignatureConfig) *SignatureVerifier {
	if config.HeaderName == "" {
		config.HeaderName = "X-Signature"
	}
	if config.TimestampHeader == "" {
		config.TimestampHeader = "X-Timestamp"
	}
	if config.NonceHeader == "" {
		config.NonceHeader = "X-Nonce"
	}
	if config.KeyIDHeader == "" {
		config.KeyIDHeader = "X-Key-ID"
	}
	if config.MaxTimestampSkew == 0 {
		config.MaxTimestampSkew = 5 * time.Minute
	}
	if config.NonceCacheSize == 0 {
		config.NonceCacheSize = 10000
	}
	if len(config.Algorithms) == 0 {
		config.Algorithms = []string{"SHA256"}
	}

	return &SignatureVerifier{
		config:  config,
		secrets: make(map[string]string),
		nonces:  newNonceCache(config.NonceCacheSize),
	}
}

// AddSecret 添加密钥
func (sv *SignatureVerifier) AddSecret(keyID, secret string) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	sv.secrets[keyID] = secret
}

// RemoveSecret 移除密钥
func (sv *SignatureVerifier) RemoveSecret(keyID string) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	delete(sv.secrets, keyID)
}

// ReloadSecrets 重新加载所有密钥
func (sv *SignatureVerifier) ReloadSecrets(secrets map[string]string) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	sv.secrets = make(map[string]string)
	for keyID, secret := range secrets {
		sv.secrets[keyID] = secret
	}
}

// Sign 生成签名
// 签名格式: HMAC(keyID:timestamp:nonce:method:path:bodyHash, secret)
func (sv *SignatureVerifier) Sign(method, path, body string, timestamp int64, nonce string) string {
	return sv.SignWithAlgorithm(method, path, body, timestamp, nonce, sv.config.Algorithms[0])
}

// SignWithAlgorithm 使用指定算法生成签名
func (sv *SignatureVerifier) SignWithAlgorithm(method, path, body string, timestamp int64, nonce, algorithm string) string {
	// 生成签名数据
	signData := sv.buildSignData(method, path, body, timestamp, nonce)

	// 获取第一个可用的密钥来签名
	sv.mu.RLock()
	var secret string
	for _, s := range sv.secrets {
		secret = s
		break
	}
	sv.mu.RUnlock()

	if secret == "" {
		return ""
	}

	return sv.computeHMAC(signData, secret, algorithm)
}

// SignWithKeyID 使用指定 keyID 生成签名
func (sv *SignatureVerifier) SignWithKeyID(method, path, body string, timestamp int64, nonce, keyID string) string {
	sv.mu.RLock()
	secret, exists := sv.secrets[keyID]
	sv.mu.RUnlock()

	if !exists || secret == "" {
		return ""
	}

	signData := sv.buildSignData(method, path, body, timestamp, nonce)
	return sv.computeHMAC(signData, secret, sv.config.Algorithms[0])
}

// buildSignData 构建待签名数据
func (sv *SignatureVerifier) buildSignData(method, path, body string, timestamp int64, nonce string) string {
	bodyHash := sv.computeBodyHash(body)
	return fmt.Sprintf("%s:%d:%s:%s:%s:%s",
		"", // keyID 在验证时填充
		timestamp,
		nonce,
		strings.ToUpper(method),
		path,
		bodyHash,
	)
}

// computeBodyHash 计算 body 的 SHA256 哈希
func (sv *SignatureVerifier) computeBodyHash(body string) string {
	if body == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(body))
	return hex.EncodeToString(hash[:])
}

// computeHMAC 计算 HMAC 哈希
func (sv *SignatureVerifier) computeHMAC(data, secret, algorithm string) string {
	var h hash.Hash
	switch strings.ToUpper(algorithm) {
	case "SHA256":
		h = hmac.New(sha256.New, []byte(secret))
	case "SHA512":
		h = hmac.New(sha512.New, []byte(secret))
	default:
		h = hmac.New(sha256.New, []byte(secret))
	}
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// Verify 验证 HTTP 请求签名
func (sv *SignatureVerifier) Verify(r *http.Request) (bool, error) {
	if !sv.config.Enabled {
		return true, nil
	}

	// 获取签名头
	signature := r.Header.Get(sv.config.HeaderName)
	if signature == "" {
		return false, errors.New("missing signature header")
	}

	timestampStr := r.Header.Get(sv.config.TimestampHeader)
	if timestampStr == "" {
		return false, errors.New("missing timestamp header")
	}

	nonce := r.Header.Get(sv.config.NonceHeader)
	if nonce == "" {
		return false, errors.New("missing nonce header")
	}

	keyID := r.Header.Get(sv.config.KeyIDHeader)
	if keyID == "" {
		return false, errors.New("missing key ID header")
	}

	// 解析时间戳
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return false, fmt.Errorf("invalid timestamp: %w", err)
	}

	// 检查时间戳偏差
	now := time.Now().Unix()
	skew := now - timestamp
	if skew < 0 {
		skew = -skew
	}
	if time.Duration(skew)*time.Second > sv.config.MaxTimestampSkew {
		return false, errors.New("timestamp skew too large")
	}

	// 检查 nonce（重放攻击防护）
	if !sv.nonces.Add(nonce, timestamp) {
		return false, errors.New("replay attack detected: nonce already used")
	}

	// 获取密钥
	sv.mu.RLock()
	secret, exists := sv.secrets[keyID]
	sv.mu.RUnlock()

	if !exists {
		return false, errors.New("unknown key ID")
	}

	// 读取请求体
	body, err := sv.readBody(r)
	if err != nil {
		return false, fmt.Errorf("failed to read body: %w", err)
	}

	// 构建签名数据
	signData := fmt.Sprintf("%s:%d:%s:%s:%s:%s",
		keyID,
		timestamp,
		nonce,
		strings.ToUpper(r.Method),
		r.URL.Path,
		sv.computeBodyHash(string(body)),
	)

	// 验证签名
	for _, algorithm := range sv.config.Algorithms {
		expectedSig := sv.computeHMAC(signData, secret, algorithm)
		if hmac.Equal([]byte(signature), []byte(expectedSig)) {
			return true, nil
		}
	}

	return false, errors.New("signature mismatch")
}

// readBody 读取请求体并恢复
func (sv *SignatureVerifier) readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return []byte{}, nil
	}
	buf := make([]byte, 0)
	if r.ContentLength > 0 {
		buf = make([]byte, r.ContentLength)
	}
	n, err := r.Body.Read(buf)
	if err != nil && err.Error() != "EOF" {
		return nil, err
	}
	return buf[:n], nil
}

// IsEnabled 返回签名验证是否启用
func (sv *SignatureVerifier) IsEnabled() bool {
	return sv.config.Enabled
}

// GetConfig 返回当前配置
func (sv *SignatureVerifier) GetConfig() SignatureConfig {
	return sv.config
}