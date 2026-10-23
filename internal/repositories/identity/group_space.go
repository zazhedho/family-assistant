package repositoryidentity

import (
	"context"
	"strings"

	interfaceidentity "family-assistant/internal/interfaces/identity"

	"gorm.io/gorm"
)

const (
	groupSpaceChannelActive = "ACTIVE"
	groupSpaceActive        = "ACTIVE"
)

type GroupSpaceRepository struct {
	DB *gorm.DB
}

func NewGroupSpaceRepository(db *gorm.DB) interfaceidentity.GroupSpaceLookup {
	return &GroupSpaceRepository{DB: db}
}

var _ interfaceidentity.GroupSpaceLookup = (*GroupSpaceRepository)(nil)

// ActiveSpaceIDForChat reads the single ACTIVE space_channels binding for a chat.
// The partial unique index ux_space_channels_active_chat guarantees at most one row.
func (r *GroupSpaceRepository) ActiveSpaceIDForChat(ctx context.Context, channel, chatID string) (string, bool, error) {
	channel = strings.ToLower(strings.TrimSpace(channel))
	chatID = strings.TrimSpace(chatID)
	if channel == "" || chatID == "" {
		return "", false, nil
	}
	var spaceIDs []string
	err := r.DB.WithContext(ctx).Table("space_channels").
		Joins("JOIN spaces ON spaces.id = space_channels.space_id").
		Where("space_channels.channel = ? AND space_channels.external_chat_id = ? AND space_channels.status = ? AND spaces.status = ? AND spaces.deleted_at IS NULL",
			channel, chatID, groupSpaceChannelActive, groupSpaceActive).
		Limit(1).
		Pluck("space_channels.space_id", &spaceIDs).Error
	if err != nil {
		return "", false, err
	}
	if len(spaceIDs) == 0 {
		return "", false, nil
	}
	return spaceIDs[0], true, nil
}
