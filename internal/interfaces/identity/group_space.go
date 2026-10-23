package interfaceidentity

import "context"

// GroupSpaceLookup resolves the Space a chat is bound to, so group chats can
// default to their bound Space instead of the sender's Personal Space.
type GroupSpaceLookup interface {
	// ActiveSpaceIDForChat returns the Space ID of the ACTIVE binding for the
	// chat, or false when the chat is not bound to an active Space.
	ActiveSpaceIDForChat(ctx context.Context, channel, chatID string) (string, bool, error)
}
