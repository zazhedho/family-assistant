package servicescheduledmessage

import (
	"context"
	"regexp"
	"strings"
	"time"

	domainaudit "family-assistant/internal/domain/audit"
	domainidentity "family-assistant/internal/domain/identity"
	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
	domainspace "family-assistant/internal/domain/space"
	dto "family-assistant/internal/dto"
	interfacescheduledmessage "family-assistant/internal/interfaces/scheduledmessage"
	"family-assistant/internal/services/authorization"
)

var (
	whatsAppGroupPattern = regexp.MustCompile(`^[0-9]*[1-9][0-9]*@g[.]us$`)
)

func (s *service) resolveCreateTarget(ctx context.Context, spaceID string, input dto.ScheduledMessageCreateInput, members []domainspace.ResolvedMembership) (*string, *string, error) {
	switch input.Target {
	case domainscheduledmessage.TargetDM:
		if input.RecipientMemberID == nil || input.SpaceChannelID != nil {
			return nil, nil, invalid("target", "requires exactly one active recipient")
		}
		recipientID, err := parseUUID(*input.RecipientMemberID, "recipient_member_id")
		if err != nil {
			return nil, nil, err
		}
		if memberByID(members, recipientID) == nil {
			return nil, nil, authorization.ErrNotFound
		}
		targets, err := s.repository.ListTargets(ctx, spaceID)
		if err != nil {
			return nil, nil, err
		}
		for i := range targets {
			target := &targets[i]
			if target.Kind == domainscheduledmessage.TargetDM && target.MemberID != nil && target.SpaceChannelID == nil {
				targetMemberID, targetIDErr := parseUUID(*target.MemberID, "member_id")
				if targetIDErr == nil && targetMemberID == recipientID {
					return &recipientID, nil, nil
				}
			}
		}
		return nil, nil, interfacescheduledmessage.ErrInvalidWhatsAppIdentity
	case domainscheduledmessage.TargetGroup:
		if input.RecipientMemberID != nil || input.SpaceChannelID == nil {
			return nil, nil, invalid("target", "requires exactly one active bound group")
		}
		channelID, err := parseUUID(*input.SpaceChannelID, "space_channel_id")
		if err != nil {
			return nil, nil, err
		}
		targets, err := s.repository.ListTargets(ctx, spaceID)
		if err != nil {
			return nil, nil, err
		}
		for i := range targets {
			target := &targets[i]
			if target.Kind == domainscheduledmessage.TargetGroup && target.SpaceChannelID != nil && target.MemberID == nil {
				targetChannelID, targetIDErr := parseUUID(*target.SpaceChannelID, "space_channel_id")
				if targetIDErr == nil && targetChannelID == channelID {
					return nil, &channelID, nil
				}
			}
		}
		return nil, nil, authorization.ErrNotFound
	default:
		return nil, nil, invalid("target", "must be DM or GROUP")
	}
}

func (s *service) Targets(ctx context.Context, actor domainidentity.ActorContext, rawSpaceID string) (result []dto.ScheduledMessageTargetOutput, err error) {
	spaceID := strings.TrimSpace(actor.SpaceID)
	defer func() {
		if err != nil {
			event := s.auditEvent(ctx, actor, "targets", spaceID, "", actor.MemberID, domainaudit.StatusFailed, nil)
			event.ErrorMessage = failureCategory(err)
			s.writeAudit(ctx, event)
		}
	}()
	var resolveErr error
	spaceID, resolveErr = resolveSpaceID(rawSpaceID, actor.SpaceID)
	if resolveErr != nil {
		return nil, resolveErr
	}
	actorMember, _, err := s.authorizedMember(ctx, actor, spaceID, createPermission)
	if err != nil {
		return nil, err
	}
	result, err = s.repository.ListTargets(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	for i := range result {
		target := &result[i]
		switch target.Kind {
		case domainscheduledmessage.TargetDM:
			if target.MemberID == nil || target.SpaceChannelID != nil {
				return nil, invalid("target", "repository returned an invalid DM target")
			}
			id, parseErr := parseUUID(*target.MemberID, "member_id")
			if parseErr != nil {
				return nil, parseErr
			}
			target.MemberID = &id
		case domainscheduledmessage.TargetGroup:
			if target.SpaceChannelID == nil || target.MemberID != nil {
				return nil, invalid("target", "repository returned an invalid group target")
			}
			id, parseErr := parseUUID(*target.SpaceChannelID, "space_channel_id")
			if parseErr != nil {
				return nil, parseErr
			}
			target.SpaceChannelID = &id
		default:
			return nil, invalid("target", "repository returned an unknown target kind")
		}
	}
	s.writeAudit(ctx, s.auditEvent(ctx, actor, "targets", spaceID, "", actorMember.ID, domainaudit.StatusSuccess, nil))
	return result, nil
}

func (s *service) BindCurrentGroup(ctx context.Context, actor domainidentity.ActorContext, input dto.SpaceChannelBindInput) (bound *domainscheduledmessage.SpaceChannel, err error) {
	spaceID := strings.TrimSpace(actor.SpaceID)
	defer func() {
		if err != nil {
			event := s.auditEvent(ctx, actor, "bind", spaceID, "", actor.MemberID, domainaudit.StatusFailed, nil)
			event.ErrorMessage = failureCategory(err)
			s.writeAudit(ctx, event)
		}
	}()
	var resolveErr error
	spaceID, resolveErr = resolveSpaceID(input.Space, actor.SpaceID)
	if resolveErr != nil {
		return nil, resolveErr
	}
	actorMember, _, err := s.authorizedMember(ctx, actor, spaceID, bindPermission)
	if err != nil {
		return nil, err
	}
	if !isOwnerOrAdmin(actorMember.RoleName) {
		return nil, authorization.ErrForbidden
	}
	channel := strings.ToLower(strings.TrimSpace(input.CurrentChannel))
	chatID := strings.TrimSpace(input.CurrentChatID)
	chatType := strings.ToLower(strings.TrimSpace(input.CurrentChatType))
	if strings.ToLower(strings.TrimSpace(actor.Source)) != "mcp" ||
		channel != "whatsapp" || strings.ToLower(strings.TrimSpace(actor.Channel)) != channel ||
		chatType != "group" || !whatsAppGroupPattern.MatchString(chatID) {
		return nil, interfacescheduledmessage.ErrInvalidWhatsAppIdentity
	}
	now := time.Now().UTC()
	bound, err = s.repository.BindCurrentGroup(ctx, spaceID, actorMember.ID, channel, chatID, chatType, now)
	if err != nil {
		return nil, err
	}
	if bound == nil || bound.SpaceID != spaceID || bound.Channel != channel || bound.BoundByMemberID != actorMember.ID {
		return nil, authorization.ErrNotFound
	}
	s.writeAudit(ctx, s.auditEvent(ctx, actor, "bind", spaceID, bound.ID, actorMember.ID, domainaudit.StatusSuccess, map[string]any{"target": domainscheduledmessage.TargetGroup, "status": bound.Status}))
	return bound, nil
}
