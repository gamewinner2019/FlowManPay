package preoccupy

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// PreOccupy implements Redis-based preoccupation with optimistic locking.
// Mirrors Python's pre_occupy function.
func PreOccupy(rdb *redis.Client, key string, amount int64, couldNegative bool) error {
	ctx := context.Background()
	maxRetries := 20000

	for i := 0; i < maxRetries; i++ {
		err := rdb.Watch(ctx, func(tx *redis.Tx) error {
			// Get current value
			val, err := tx.Get(ctx, key).Result()
			pre := int64(0)
			if err == nil {
				pre, _ = strconv.ParseInt(val, 10, 64)
			} else if err != redis.Nil {
				return err
			}

			// Calculate new value
			t := pre + amount
			if !couldNegative && t < 0 {
				t = 0
			}

			// Execute in transaction
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, key, strconv.FormatInt(t, 10), 600*time.Second)
				return nil
			})
			return err
		}, key)

		if err == nil {
			return nil
		}
		if err != redis.TxFailedErr {
			return err
		}
		// WatchError - retry
	}
	return nil
}

// GetPreOccupy returns the current preoccupied amount.
func GetPreOccupy(rdb *redis.Client, key string) (int64, error) {
	ctx := context.Background()
	val, err := rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	pre, _ := strconv.ParseInt(val, 10, 64)
	return pre, nil
}

// TenantPreKey returns the Redis key for tenant preoccupation.
func TenantPreKey(tenantID uint) string {
	return "pre:occ_tenant_" + strconv.FormatUint(uint64(tenantID), 10)
}

// WriteoffPreKey returns the Redis key for writeoff preoccupation.
func WriteoffPreKey(writeoffID uint) string {
	return "pre:occ_" + strconv.FormatUint(uint64(writeoffID), 10)
}
