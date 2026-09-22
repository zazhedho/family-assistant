package dto

import "time"

type ActivityCreateInput struct {
	Space      string
	Kind       string
	Note       string
	OccurredAt time.Time
}

type ActivityListInput struct {
	Space string
	Kind  string
	From  *time.Time
	To    *time.Time
	Limit int
}
