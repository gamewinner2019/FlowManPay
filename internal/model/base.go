package model

import (
	"gorm.io/gorm"
)

// CoreModel mirrors Django's CoreModel base class.
// All business models embed this struct.
type CoreModel struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	Description    string         `gorm:"size:255;default:''" json:"description"`
	Creator        *uint          `gorm:"index" json:"creator"`
	Modifier       *uint          `json:"modifier"`
	CreateDatetime DateTime      `gorm:"autoCreateTime;index" json:"create_datetime"`
	UpdateDatetime DateTime      `gorm:"autoUpdateTime" json:"update_datetime"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

// TablePrefix is the global table prefix for all models.
const TablePrefix = "dvadmin_"
