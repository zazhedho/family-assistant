package repositoryactivity

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"sort"
	"strings"
	"time"

	domainactivity "family-assistant/internal/domain/activity"
	interfaceactivity "family-assistant/internal/interfaces/activity"
	repositorygeneric "family-assistant/internal/repositories/generic"
	"family-assistant/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	DB *gorm.DB
}

func NewRepository(db *gorm.DB) interfaceactivity.RepoActivityInterface {
	return &Repository{DB: db}
}

func (r *Repository) Create(ctx context.Context, activity *domainactivity.Activity) error {
	if activity == nil {
		return domainactivity.ErrActivityRequired
	}
	if strings.TrimSpace(activity.SpaceID) == "" {
		return domainactivity.ErrActivitySpaceID
	}
	if strings.TrimSpace(activity.ID) == "" {
		activity.ID = utils.CreateUUID()
	}
	if activity.Details == nil {
		activity.Details = domainactivity.Details{}
	}
	return r.DB.WithContext(ctx).
		Select("id", "space_id", "created_by_member_id", "kind", "subject", "note", "details", "occurred_at", "created_at", "updated_at").
		Create(activity).Error
}

func (r *Repository) List(ctx context.Context, filter domainactivity.ListFilter) ([]domainactivity.Activity, error) {
	if strings.TrimSpace(filter.SpaceID) == "" {
		return nil, domainactivity.ErrActivitySpaceID
	}
	query := r.DB.WithContext(ctx).Where("space_id = ?", strings.TrimSpace(filter.SpaceID))
	if kind := strings.TrimSpace(filter.Kind); kind != "" {
		query = query.Where("kind = ?", kind)
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		search = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search)
		query = repositorygeneric.BuildSearchFunc("note")(query, search)
	}
	if filter.From != nil {
		query = query.Where("occurred_at >= ?", *filter.From)
	}
	if filter.To != nil {
		query = query.Where("occurred_at <= ?", *filter.To)
	}
	query = query.Order("occurred_at DESC")
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	var activities []domainactivity.Activity
	if err := query.Find(&activities).Error; err != nil {
		return nil, err
	}
	return activities, nil
}

// CountByKind counts a Space's activities of the given kinds that occurred
// within [from, to], keyed by kind. Kinds without activities are absent.
func (r *Repository) CountByKind(ctx context.Context, spaceID string, kinds []string, from, to time.Time) (map[string]int64, error) {
	if strings.TrimSpace(spaceID) == "" {
		return nil, domainactivity.ErrActivitySpaceID
	}
	counts := map[string]int64{}
	if len(kinds) == 0 {
		return counts, nil
	}
	var rows []struct {
		Kind  string
		Total int64
	}
	if err := r.DB.WithContext(ctx).Model(&domainactivity.Activity{}).
		Select("kind, COUNT(*) AS total").
		Where("space_id = ? AND kind IN ? AND occurred_at >= ? AND occurred_at <= ?", strings.TrimSpace(spaceID), kinds, from, to).
		Group("kind").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.Kind] = row.Total
	}
	return counts, nil
}

func (r *Repository) FindByIDInSpace(ctx context.Context, spaceID, activityID string) (*domainactivity.Activity, error) {
	if strings.TrimSpace(spaceID) == "" {
		return nil, domainactivity.ErrActivitySpaceID
	}
	if strings.TrimSpace(activityID) == "" {
		return nil, domainactivity.ErrActivityIDRequired
	}
	var activity domainactivity.Activity
	if err := r.DB.WithContext(ctx).Where("space_id = ? AND id = ?", spaceID, activityID).First(&activity).Error; err != nil {
		return nil, err
	}
	return &activity, nil
}

func (r *Repository) Update(ctx context.Context, spaceID, activityID string, fields domainactivity.UpdateFields, updatedAt time.Time) error {
	if strings.TrimSpace(spaceID) == "" {
		return domainactivity.ErrActivitySpaceID
	}
	if strings.TrimSpace(activityID) == "" {
		return domainactivity.ErrActivityIDRequired
	}
	updates := map[string]any{"updated_at": updatedAt}
	if fields.Kind != nil {
		updates["kind"] = strings.TrimSpace(*fields.Kind)
	}
	if fields.Subject != nil {
		updates["subject"] = strings.TrimSpace(*fields.Subject)
	}
	if fields.Note != nil {
		updates["note"] = strings.TrimSpace(*fields.Note)
	}
	if fields.OccurredAt != nil {
		updates["occurred_at"] = *fields.OccurredAt
	}
	if len(fields.DetailsPatch) > 0 {
		expr, err := detailsPatchExpr(fields.DetailsPatch)
		if err != nil {
			return err
		}
		updates["details"] = expr
	}
	if len(updates) == 1 {
		return domainactivity.ErrActivityUpdateRequired
	}
	result := r.DB.WithContext(ctx).Model(&domainactivity.Activity{}).
		Where("id = ? AND space_id = ?", activityID, spaceID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repository) SoftDelete(ctx context.Context, spaceID, activityID string, deletedAt time.Time) error {
	if strings.TrimSpace(spaceID) == "" {
		return domainactivity.ErrActivitySpaceID
	}
	if strings.TrimSpace(activityID) == "" {
		return domainactivity.ErrActivityIDRequired
	}
	result := r.DB.WithContext(ctx).Model(&domainactivity.Activity{}).
		Where("id = ? AND space_id = ?", activityID, spaceID).
		Updates(map[string]any{"deleted_at": deletedAt, "updated_at": deletedAt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// detailsPatchExpr merges a details patch in SQL so concurrent patches to
// different keys do not overwrite each other. Nil values remove keys.
func detailsPatchExpr(patch domainactivity.Details) (clause.Expr, error) {
	set := map[string]any{}
	var remove pgTextArray
	for key, value := range patch {
		if value == nil {
			remove = append(remove, key)
			continue
		}
		set[key] = value
	}
	sort.Strings(remove)
	raw, err := json.Marshal(set)
	if err != nil {
		return clause.Expr{}, err
	}
	return gorm.Expr("(details || ?::jsonb) - ?::text[]", string(raw), remove), nil
}

// pgTextArray encodes a Postgres text[] literal with every element quoted.
type pgTextArray []string

func (a pgTextArray) Value() (driver.Value, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, s := range a {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		b.WriteString(strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String(), nil
}

// ReportFeeding attaches a feeding report to one feeding in a single
// transaction. Candidate feedings are locked FOR UPDATE (in id order) so
// concurrent reports serialize and always see the latest details.
func (r *Repository) ReportFeeding(ctx context.Context, req domainactivity.FeedingReportRequest) (*domainactivity.FeedingReportResult, error) {
	spaceID := strings.TrimSpace(req.SpaceID)
	if spaceID == "" {
		return nil, domainactivity.ErrActivitySpaceID
	}
	var result domainactivity.FeedingReportResult
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		feedings, err := lockFeedingCandidates(tx, spaceID, req.Selector)
		if err != nil {
			return err
		}
		target, err := domainactivity.SelectFeeding(feedings, req.Selector)
		if err != nil {
			return err
		}
		if req.CanMutate != nil && !req.CanMutate(target) {
			return domainactivity.ErrFeedingForbidden
		}
		previous := cloneDetails(target.Details)
		next, changed, err := domainactivity.ApplyFeedingReport(target.Details, req.Report, req.ReportedAt)
		if err != nil {
			return err
		}
		if !changed {
			result = domainactivity.FeedingReportResult{Activity: *target, Previous: previous}
			return nil
		}
		// Only a backend-generated note follows the report; a note a person
		// wrote (including legacy prose feedings) is kept as is.
		note := target.Note
		if domainactivity.IsGeneratedFeedingNote(target.Subject, target.Details, target.Note) {
			note = domainactivity.FeedingNote(target.Subject, next)
		}
		update := tx.Model(&domainactivity.Activity{}).
			Where("id = ? AND space_id = ?", target.ID, spaceID).
			Updates(map[string]any{"details": next, "note": note, "updated_at": req.Now})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		updated := *target
		updated.Details, updated.Note, updated.UpdatedAt = next, note, req.Now
		result = domainactivity.FeedingReportResult{Activity: updated, Previous: previous, Changed: true}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// lockFeedingCandidates loads and locks the Space's live feedings a selector
// may refer to. SelectFeeding does the actual picking.
func lockFeedingCandidates(tx *gorm.DB, spaceID string, sel domainactivity.FeedingSelector) ([]domainactivity.Activity, error) {
	base := func() *gorm.DB {
		return tx.Model(&domainactivity.Activity{}).Where("space_id = ? AND kind = ?", spaceID, feedingKind)
	}
	if id := strings.TrimSpace(sel.FeedingID); id != "" {
		if _, err := uuid.Parse(id); err != nil {
			return nil, gorm.ErrRecordNotFound
		}
		var feeding domainactivity.Activity
		if err := base().Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&feeding).Error; err != nil {
			return nil, err
		}
		return []domainactivity.Activity{feeding}, nil
	}
	lower, upper := sel.MessageAt.Add(-domainactivity.FeedingOpenLookback), sel.MessageAt
	if sel.FeedingTime != nil {
		if t := sel.FeedingTime.Add(-domainactivity.FeedingTimeTolerance); t.Before(lower) {
			lower = t
		}
		if t := sel.FeedingTime.Add(domainactivity.FeedingTimeTolerance); t.After(upper) {
			upper = t
		}
	}
	recent := base().Select("id").Where("occurred_at <= ?", upper).Order("occurred_at DESC").Limit(recentFeedingCandidates)
	var feedings []domainactivity.Activity
	if err := base().Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("(occurred_at >= ? AND occurred_at <= ?) OR id IN (?)", lower, upper, recent).
		Order("id").
		Find(&feedings).Error; err != nil {
		return nil, err
	}
	return feedings, nil
}

const (
	feedingKind             = "feeding"
	recentFeedingCandidates = 5
)

func cloneDetails(d domainactivity.Details) domainactivity.Details {
	out := make(domainactivity.Details, len(d))
	for k, v := range d {
		out[k] = v
	}
	return out
}

var _ interfaceactivity.RepoActivityInterface = (*Repository)(nil)
