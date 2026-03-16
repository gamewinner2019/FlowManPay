package sign

import (
	"crypto/md5"
	"fmt"
	"sort"
	"strings"
)

// DefaultUseList is the default list of fields used for signing
var DefaultUseList = []string{"mchId", "channelId", "mchOrderNo", "amount", "notifyUrl", "jumpUrl"}

// CombineValues sorts params by key and combines them into a string with the key appended.
// Mirrors Python's combine_values function.
func CombineValues(params map[string]string, key string) string {
	// Remove sign field if present
	delete(params, "sign")

	// Sort keys
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build combined value
	parts := make([]string, 0, len(keys)+1)
	for _, k := range keys {
		if params[k] != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", k, params[k]))
		}
	}
	parts = append(parts, fmt.Sprintf("key=%s", key))

	return strings.Join(parts, "&")
}

// MD5Encryption computes MD5 hash and returns uppercase hex string.
func MD5Encryption(text string) string {
	hash := md5.Sum([]byte(text))
	return strings.ToUpper(fmt.Sprintf("%x", hash))
}

// ToSign generates the sign string and its MD5 hash.
// Returns (combined_value, encrypted_value).
func ToSign(data map[string]string, key string) (string, string) {
	combined := CombineValues(data, key)
	encrypted := MD5Encryption(combined)
	return combined, encrypted
}

// YiSign generates the sign for YiPay compatible mode.
func YiSign(params map[string]string, key string) (string, string) {
	// Filter and sort
	type kv struct {
		Key   string
		Value string
	}
	var sorted []kv
	for k, v := range params {
		if k != "sign" && k != "sign_type" && v != "" {
			sorted = append(sorted, kv{k, v})
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Key < sorted[j].Key
	})

	parts := make([]string, 0, len(sorted))
	for _, item := range sorted {
		parts = append(parts, fmt.Sprintf("%s=%s", item.Key, item.Value))
	}
	urlEncoded := strings.Join(parts, "&")
	signString := urlEncoded + key

	hash := md5.Sum([]byte(signString))
	return signString, fmt.Sprintf("%x", hash)
}

// GetSign generates the sign based on compatible mode.
// compatible=0: standard mode, compatible=1: YiPay mode.
func GetSign(data map[string]string, key string, useList []string, optionalArgs []string, compatible int) (string, string, error) {
	if compatible == 1 {
		raw, hash := YiSign(data, key)
		return raw, hash, nil
	}

	if useList == nil {
		useList = DefaultUseList
	}

	da := make(map[string]string)
	for _, field := range useList {
		val, ok := data[field]
		if !ok {
			return "", "", fmt.Errorf("`%s` not in json body", field)
		}
		da[field] = val
	}
	for _, field := range optionalArgs {
		da[field] = data[field]
	}

	raw, hash := ToSign(da, key)
	return raw, hash, nil
}

// MD5Password computes MD5 hash of password string (lowercase hex).
func MD5Password(password string) string {
	hash := md5.Sum([]byte(password))
	return fmt.Sprintf("%x", hash)
}
