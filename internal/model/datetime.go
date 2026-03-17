package model

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"time"
)

const datetimeFormat = "2006-01-02 15:04:05"

// DateTime 自定义时间类型，JSON序列化格式与Django一致: "Y-m-d H:M:S"
type DateTime struct {
	time.Time
}

// GormDataType implements GORM's GormDataTypeInterface so that
// autoCreateTime / autoUpdateTime use time.Now() instead of unix seconds.
func (DateTime) GormDataType() string {
	return "time"
}

// MarshalJSON 实现 json.Marshaler 接口
func (dt DateTime) MarshalJSON() ([]byte, error) {
	if dt.IsZero() {
		return []byte("null"), nil
	}
	return []byte(fmt.Sprintf(`"%s"`, dt.Time.Format(datetimeFormat))), nil
}

// UnmarshalJSON 实现 json.Unmarshaler 接口
func (dt *DateTime) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "null" || s == "" {
		dt.Time = time.Time{}
		return nil
	}
	// 尝试多种格式解析
	for _, layout := range []string{
		datetimeFormat,
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			dt.Time = t
			return nil
		}
	}
	// 尝试本地时区解析
	t, err := time.ParseInLocation(datetimeFormat, s, time.Local)
	if err != nil {
		return fmt.Errorf("无法解析时间: %s", s)
	}
	dt.Time = t
	return nil
}

// Value 实现 driver.Valuer 接口（写入数据库）
func (dt DateTime) Value() (driver.Value, error) {
	if dt.IsZero() {
		return nil, nil
	}
	return dt.Time, nil
}

// Scan 实现 sql.Scanner 接口（从数据库读取）
func (dt *DateTime) Scan(value interface{}) error {
	if value == nil {
		dt.Time = time.Time{}
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		dt.Time = v
	case int64:
		dt.Time = time.Unix(v, 0)
	case string:
		t, err := time.ParseInLocation(datetimeFormat, v, time.Local)
		if err != nil {
			return err
		}
		dt.Time = t
	case []byte:
		t, err := time.ParseInLocation(datetimeFormat, string(v), time.Local)
		if err != nil {
			return err
		}
		dt.Time = t
	default:
		return fmt.Errorf("无法将 %T 转换为 DateTime", value)
	}
	return nil
}

// Now 返回当前时间的 DateTime
func Now() DateTime {
	return DateTime{Time: time.Now()}
}

// NullDateTime 可空的 DateTime 指针辅助函数
func NullDateTime(t *time.Time) *DateTime {
	if t == nil {
		return nil
	}
	return &DateTime{Time: *t}
}
