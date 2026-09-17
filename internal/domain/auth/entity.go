package domainauth

import "time"

func (Blacklist) TableName() string {
	return "blacklist"
}

type Blacklist struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	Token     string    `gorm:"not null; unique" json:"token"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `gorm:"not null" json:"expires_at"`
}
