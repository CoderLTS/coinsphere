package ai

import "time"

type AIModelConfig struct {
	ID               int64  `gorm:"primaryKey;autoIncrement"`
	DisplayName      string `gorm:"size:120"`
	BaseURL          string `gorm:"column:base_url;size:1000"`
	ModelName        string `gorm:"size:255"`
	APIKeyCiphertext string `gorm:"column:api_key_ciphertext;type:text"`
	IsEnabled        bool   `gorm:"column:is_enabled"`
	Priority         int
	TimeoutMS        int `gorm:"column:timeout_ms"`
	CreatedBy        int64
	UpdatedBy        int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (AIModelConfig) TableName() string { return "plugin_ai.ai_model_configs" }

type AssistantSession struct {
	ID            int64 `gorm:"primaryKey;autoIncrement"`
	UserID        int64
	ModelConfigID int64
	Title         string `gorm:"size:160"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LastMessageAt time.Time
}

func (AssistantSession) TableName() string { return "plugin_ai.assistant_sessions" }

type AssistantMessage struct {
	ID           int64 `gorm:"primaryKey;autoIncrement"`
	SessionID    int64
	Role         string `gorm:"size:16"`
	Content      string `gorm:"type:text"`
	MetadataJSON string `gorm:"column:metadata_json;type:jsonb"`
	CreatedAt    time.Time
}

func (AssistantMessage) TableName() string { return "plugin_ai.assistant_messages" }
