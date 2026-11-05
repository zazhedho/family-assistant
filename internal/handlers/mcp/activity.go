package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
	"family-assistant/internal/dto"
	interfaceactivity "family-assistant/internal/interfaces/activity"
	interfaceidentity "family-assistant/internal/interfaces/identity"
	interfacereminder "family-assistant/internal/interfaces/reminder"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/utils"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type ActivityCreateInput struct {
	Space      string         `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space in a private chat or the bound Space in a group or other shared chat"`
	Kind       string         `json:"kind" jsonschema:"short event type such as breastfeeding, diaper, sleep, or note"`
	Subject    string         `json:"subject,omitempty" jsonschema:"who or what the event is about, e.g. a child's name or a medicine; optional"`
	Note       string         `json:"note,omitempty" jsonschema:"human-readable details. May be omitted for a feeding created with details.volume_ml: the backend writes the note"`
	Details    map[string]any `json:"details,omitempty" jsonschema:"structured details by kind. feeding: {volume_ml}, optionally leftover_ml/outcome/expired_ml at creation. medication: {name,dose,unit}. vaccination: {vaccine,dose_no,next_due}. temperature: {celsius}. growth_measurement: {weight_kg,height_cm,head_cm}"`
	OccurredAt string         `json:"occurred_at,omitempty" jsonschema:"RFC3339 event time ONLY when the user specifies a time. Omit for now or no stated time: backend uses the trusted WhatsApp message timestamp, not processing time. Never use another record's created_at or updated_at."`
}

type ActivityListInput struct {
	Space  string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space in a private chat or the bound Space in a group or other shared chat"`
	Kind   string `json:"kind,omitempty" jsonschema:"optional event type filter"`
	Search string `json:"search,omitempty" jsonschema:"optional case-insensitive literal substring search in note; combine with kind and time filters"`
	From   string `json:"from,omitempty" jsonschema:"optional RFC3339 lower bound"`
	To     string `json:"to,omitempty" jsonschema:"optional RFC3339 upper bound"`
	Limit  int    `json:"limit,omitempty" jsonschema:"optional maximum results, capped at 100"`
}

type ActivityUpdateInput struct {
	Space      string         `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space in a private chat or the bound Space in a group or other shared chat"`
	ActivityID string         `json:"activity_id" jsonschema:"activity UUID"`
	Kind       *string        `json:"kind,omitempty" jsonschema:"optional replacement event type"`
	Subject    *string        `json:"subject,omitempty" jsonschema:"optional replacement subject"`
	Note       *string        `json:"note,omitempty" jsonschema:"optional replacement details. A note you send for a feeding is kept"`
	Details    map[string]any `json:"details,omitempty" jsonschema:"optional details patch merged into existing details; same per-kind shapes as activity_create. Feeding leftover_ml/outcome/expired_ml cannot be set here: use feeding_leftover_report"`
	OccurredAt string         `json:"occurred_at,omitempty" jsonschema:"optional RFC3339 event time"`
}

type ActivityDeleteInput struct {
	Space      string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space in a private chat or the bound Space in a group or other shared chat"`
	ActivityID string `json:"activity_id" jsonschema:"activity UUID"`
}

type ActivityListOutput struct {
	Activities []domainactivity.Activity `json:"activities"`
}

func ActivityCreate(ctx context.Context, resolver interfaceidentity.Resolver, service interfaceactivity.ServiceActivityInterface, input ActivityCreateInput) (*domainactivity.Activity, error) {
	actor, err := selectSpaceActor(ctx, resolver, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	occurredAt, err := activityOccurredAt(ctx, input.OccurredAt)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("activity service is not configured"))
	}
	created, err := service.Create(ctx, actor, dto.ActivityCreateInput{
		Space: actor.SpaceID, Kind: input.Kind, Subject: input.Subject, Note: input.Note, Details: input.Details, OccurredAt: *occurredAt,
	})
	if err != nil {
		return nil, MapToolError(err)
	}
	return created, nil
}

func activityOccurredAt(ctx context.Context, value string) (*time.Time, error) {
	if strings.TrimSpace(value) != "" {
		return utils.ParseRFC3339(value, "occurred_at", true)
	}
	request, ok := ExternalRequestFromContext(ctx)
	if !ok || request.MessageAt.IsZero() {
		return nil, &serviceauthorization.ValidationError{Field: "occurred_at", Reason: "requires a trusted message timestamp or an explicit event time"}
	}
	return &request.MessageAt, nil
}

func ActivityList(ctx context.Context, resolver interfaceidentity.Resolver, service interfaceactivity.ServiceActivityInterface, input ActivityListInput) ([]domainactivity.Activity, error) {
	actor, err := selectSpaceActor(ctx, resolver, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("activity service is not configured"))
	}
	listInput := dto.ActivityListInput{Space: actor.SpaceID, Kind: input.Kind, Search: input.Search, Limit: input.Limit}
	if listInput.From, err = utils.ParseRFC3339(input.From, "from", false); err != nil {
		return nil, MapToolError(err)
	}
	if listInput.To, err = utils.ParseRFC3339(input.To, "to", false); err != nil {
		return nil, MapToolError(err)
	}
	activities, err := service.List(ctx, actor, listInput)
	if err != nil {
		return nil, MapToolError(err)
	}
	if activities == nil {
		activities = []domainactivity.Activity{}
	}
	return activities, nil
}

func ActivityUpdate(ctx context.Context, resolver interfaceidentity.Resolver, service interfaceactivity.ServiceActivityInterface, input ActivityUpdateInput) (*domainactivity.Activity, error) {
	actor, err := selectSpaceActor(ctx, resolver, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	activityID, err := utils.ParseUUID(input.ActivityID, "activity_id", true)
	if err != nil {
		return nil, MapToolError(err)
	}
	if input.Kind == nil && input.Subject == nil && input.Note == nil && len(input.Details) == 0 && strings.TrimSpace(input.OccurredAt) == "" {
		return nil, MapToolError(&serviceauthorization.ValidationError{Field: "update", Reason: "at least one field is required"})
	}
	occurredAt, err := utils.ParseRFC3339(input.OccurredAt, "occurred_at", false)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("activity service is not configured"))
	}
	activity, err := service.Update(ctx, actor, actor.SpaceID, activityID, dto.ActivityUpdateInput{Kind: input.Kind, Subject: input.Subject, Note: input.Note, Details: input.Details, OccurredAt: occurredAt})
	if err != nil {
		return nil, MapToolError(err)
	}
	return activity, nil
}

type FeedingLeftoverReportInput struct {
	Space       string `json:"space,omitempty" jsonschema:"Space UUID or exact user-facing name; blank selects Personal Space in a private chat or the bound Space in a group or other shared chat"`
	FeedingID   string `json:"feeding_id,omitempty" jsonschema:"feeding activity UUID; use after an ambiguous_feeding or no_feeding_match error once the user has said which feeding"`
	FeedingTime string `json:"feeding_time,omitempty" jsonschema:"RFC3339 time of the feeding being reported. Pass whenever the user or the quoted message gives a time. Omit only to target the open feedings of the last 3 hours"`
	LeftoverML  *int   `json:"leftover_ml,omitempty" jsonschema:"milliliters remaining. Records the amount only; the feeding stays open. Give leftover_ml or outcome, at least one"`
	ExpiredML   *int   `json:"expired_ml,omitempty" jsonschema:"milliliters thrown away as expired. Required with outcome expired unless already stored; only valid with outcome expired (with finished it must be 0 or omitted)"`
	Outcome     string `json:"outcome,omitempty" jsonschema:"finished = everything was drunk (expired_ml becomes 0). expired = milk was thrown away; needs expired_ml"`
}

type FeedingLeftoverReportOutput struct {
	Activity  domainactivity.Activity `json:"activity"`
	Corrected bool                    `json:"corrected" jsonschema:"true when this report overwrote an earlier leftover or outcome"`
}

func FeedingLeftoverReport(ctx context.Context, resolver interfaceidentity.Resolver, service interfaceactivity.ServiceActivityInterface, input FeedingLeftoverReportInput) (FeedingLeftoverReportOutput, error) {
	actor, err := selectSpaceActor(ctx, resolver, input.Space)
	if err != nil {
		return FeedingLeftoverReportOutput{}, MapToolError(err)
	}
	request, ok := ExternalRequestFromContext(ctx)
	if !ok || request.MessageAt.IsZero() {
		return FeedingLeftoverReportOutput{}, MapToolError(&serviceauthorization.ValidationError{Field: "message_at", Reason: "requires a trusted message timestamp"})
	}
	feedingTime, err := utils.ParseRFC3339(input.FeedingTime, "feeding_time", false)
	if err != nil {
		return FeedingLeftoverReportOutput{}, MapToolError(err)
	}
	if service == nil {
		return FeedingLeftoverReportOutput{}, MapToolError(errors.New("activity service is not configured"))
	}
	result, err := service.ReportFeedingLeftover(ctx, actor, dto.FeedingLeftoverInput{
		Space: actor.SpaceID, FeedingID: strings.TrimSpace(input.FeedingID), FeedingTime: feedingTime, MessageAt: request.MessageAt,
		LeftoverML: input.LeftoverML, ExpiredML: input.ExpiredML, Outcome: input.Outcome,
	})
	if err != nil {
		if targetErr, ok := errors.AsType[*domainactivity.FeedingTargetError](err); ok {
			return FeedingLeftoverReportOutput{}, feedingTargetMCPError(targetErr)
		}
		return FeedingLeftoverReportOutput{}, MapToolError(err)
	}
	if result == nil {
		return FeedingLeftoverReportOutput{}, MapToolError(errors.New("activity service returned no result"))
	}
	_, hadLeftover := result.Previous["leftover_ml"]
	_, hadOutcome := result.Previous["outcome"]
	return FeedingLeftoverReportOutput{Activity: result.Activity, Corrected: result.Changed && (hadLeftover || hadOutcome)}, nil
}

func feedingTargetMCPError(e *domainactivity.FeedingTargetError) error {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		loc = time.FixedZone("WIB", 7*60*60)
	}
	code, head := "no_feeding_match", "No feeding matches this report."
	if e.Reason == "ambiguous" {
		code, head = "ambiguous_feeding", "More than one feeding could match this report."
	}
	var b strings.Builder
	b.WriteString(code + ": " + head)
	if len(e.Candidates) > 0 {
		b.WriteString(" Candidates (id, local time dd/mm HH:MM WIB, volume, state):")
		for _, c := range e.Candidates {
			volume, state := "volume unknown", "closed"
			if c.VolumeML != nil {
				volume = fmt.Sprintf("%d ml", *c.VolumeML)
			}
			if c.Open {
				state = "open"
			}
			fmt.Fprintf(&b, "\n- %s, %s, %s, %s", c.ID, c.OccurredAt.In(loc).Format("02/01 15:04 WIB"), volume, state)
		}
	}
	b.WriteString("\nAsk the user which feeding they mean, then call feeding_leftover_report again with feeding_id.")
	return &MCPError{Code: code, Message: b.String()}
}

func ActivityDelete(ctx context.Context, resolver interfaceidentity.Resolver, service interfaceactivity.ServiceActivityInterface, input ActivityDeleteInput) (*domainactivity.Activity, error) {
	actor, err := selectSpaceActor(ctx, resolver, input.Space)
	if err != nil {
		return nil, MapToolError(err)
	}
	activityID, err := utils.ParseUUID(input.ActivityID, "activity_id", true)
	if err != nil {
		return nil, MapToolError(err)
	}
	if service == nil {
		return nil, MapToolError(errors.New("activity service is not configured"))
	}
	activity, err := service.Delete(ctx, actor, actor.SpaceID, activityID)
	if err != nil {
		return nil, MapToolError(err)
	}
	return activity, nil
}

func registerActivityTools(server *mcpsdk.Server, resolver interfaceidentity.Resolver, service interfaceactivity.ServiceActivityInterface, reminderService interfacereminder.ServiceReminderInterface) {
	addTool(server, &mcpsdk.Tool{
		Name: "activity_create_with_reminder", Description: "Create an activity and a reminder together in an authorized Space. Use this when both are requested; use activity_create for activity only. Activity details, subject and note follow the same rules as activity_create (per-kind details shapes; a feeding created with details.volume_ml needs no note). Set reminder.replace_previous for interval-based reminders so older pending reminders of the same activity kind are replaced. Earlier SENT reminders with the same key count as handled: they are marked COMPLETED and listed in completed_reminders. If replace_outcome is kept_existing, no new reminder was created; the returned reminder is the later one already pending.",
	}, func(ctx context.Context, input ActivityCreateWithReminderInput) (ActivityCreateWithReminderOutput, error) {
		return ActivityCreateWithReminder(ctx, resolver, service, reminderService, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "activity_create", Description: "Record a standalone timestamped note or care event in an authorized Space. Use activity_create when only an activity is requested; use activity_create_with_reminder when a reminder is also requested. Put structured data in details by kind: feeding {volume_ml, optionally leftover_ml/outcome/expired_ml}, medication {name,dose,unit}, vaccination {vaccine,dose_no,next_due}, temperature {celsius}, growth_measurement {weight_kg,height_cm,head_cm}. For a feeding with details.volume_ml the backend writes the note; omit note.",
	}, func(ctx context.Context, input ActivityCreateInput) (*domainactivity.Activity, error) {
		return ActivityCreate(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "activity_list", Description: "List or search timestamped notes and care events from an authorized Space. Use search for case-insensitive text in notes; combine with kind and time filters.",
	}, func(ctx context.Context, input ActivityListInput) (ActivityListOutput, error) {
		output, err := ActivityList(ctx, resolver, service, input)
		return ActivityListOutput{Activities: output}, err
	})
	addTool(server, &mcpsdk.Tool{
		Name: "activity_update", Description: "Update a timestamped note or care event in an authorized Space. details is merged into existing details (same per-kind shapes as activity_create). A note you send for a feeding is kept. Leftover, outcome and expired_ml of a feeding cannot be set here: use feeding_leftover_report.",
	}, func(ctx context.Context, input ActivityUpdateInput) (*domainactivity.Activity, error) {
		return ActivityUpdate(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "feeding_leftover_report", Description: "The ONLY way to record leftover, finished or expired milk for an existing feeding; activity_update cannot. Pass feeding_time whenever the user or the quoted message gives a time. Give leftover_ml or outcome (at least one). leftover_ml records the remaining amount and keeps the feeding open; outcome=finished means everything was drunk; outcome=expired needs expired_ml. A repeat report overwrites earlier values. Without feeding_id or feeding_time the open feedings of the last 3 hours are targeted. On ambiguous_feeding or no_feeding_match, ask the user which feeding they mean, then call again with feeding_id. corrected=true means an earlier report was overwritten.",
	}, func(ctx context.Context, input FeedingLeftoverReportInput) (FeedingLeftoverReportOutput, error) {
		return FeedingLeftoverReport(ctx, resolver, service, input)
	})
	addTool(server, &mcpsdk.Tool{
		Name: "activity_delete", Description: "Remove a timestamped note or care event from an authorized Space.",
	}, func(ctx context.Context, input ActivityDeleteInput) (*domainactivity.Activity, error) {
		return ActivityDelete(ctx, resolver, service, input)
	})
}
