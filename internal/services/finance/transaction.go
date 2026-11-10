package servicefinance

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	domainaudit "family-assistant/internal/domain/audit"
	domainfinance "family-assistant/internal/domain/finance"
	domainidentity "family-assistant/internal/domain/identity"
	"family-assistant/internal/dto"
	serviceauthorization "family-assistant/internal/services/authorization"
	"family-assistant/utils"
)

const (
	permCreate = "finance:create"
	permUpdate = "finance:update"
	permDelete = "finance:delete"

	resourceTransaction = "finance_transaction"

	// maxMerchantLen is the VARCHAR(255) column; maxNoteLen caps the TEXT column.
	maxMerchantLen = 255
	maxNoteLen     = 1000

	duplicateWindow = 2 * time.Minute
)

// TransactionCreate records an income, expense or transfer. The result carries
// a non-blocking duplicate warning and, for an expense, the budget status.
func (s *service) TransactionCreate(ctx context.Context, actor domainidentity.ActorContext, input dto.TransactionCreateInput) (result *dto.TransactionResult, err error) {
	spaceID := resolveSpace(actor, input.Space)
	defer func() {
		resourceID := ""
		if result != nil {
			resourceID = result.Transaction.ID
		}
		s.auditResult(ctx, actor, domainaudit.ActionCreate, resourceTransaction, spaceID, resourceID, err)
	}()

	if err = s.configured(); err != nil {
		return nil, err
	}
	memberID, err := s.guardMember(ctx, actor, spaceID, permCreate)
	if err != nil {
		return nil, err
	}
	occurredAt := input.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	now := time.Now().UTC()
	tx := domainfinance.Transaction{
		ID: utils.CreateUUID(), SpaceID: spaceID, CreatedByMemberID: memberID, CreatedAt: now, UpdatedAt: now,
	}
	patch := dto.TransactionUpdateInput{
		Type: &input.Type, Amount: &input.Amount, Account: &input.Account, ToAccount: &input.ToAccount,
		Category: &input.Category, Merchant: &input.Merchant, Note: &input.Note, OccurredAt: &occurredAt,
	}
	if err = s.applyPatch(ctx, &tx, patch); err != nil {
		return nil, err
	}
	if err = s.repo.CreateTransaction(ctx, &tx); err != nil {
		return nil, err
	}
	result = &dto.TransactionResult{Transaction: tx, BudgetStatus: s.budgetStatus(ctx, tx)}
	if similar, lookupErr := s.repo.FindRecentSimilar(ctx, spaceID, tx, duplicateWindow); lookupErr == nil {
		result.PossibleDuplicate = similar
	}
	return result, nil
}

// TransactionList returns the newest transactions first. Category and Account
// filters accept an id or a name; an unknown one is a validation error.
func (s *service) TransactionList(ctx context.Context, actor domainidentity.ActorContext, input dto.TransactionListInput) (txs []domainfinance.Transaction, err error) {
	spaceID := resolveSpace(actor, input.Space)
	defer func() { s.auditResult(ctx, actor, actionList, resourceTransaction, spaceID, "", err) }()

	if err = s.configured(); err != nil {
		return nil, err
	}
	limit, err := clampLimit(input.Limit)
	if err != nil {
		return nil, err
	}
	filter := domainfinance.ListFilter{
		SpaceID: spaceID, Search: strings.TrimSpace(input.Search), From: input.From, To: input.To, Limit: limit,
	}
	if filter.Type, err = normalizeFilterType(input.Type); err != nil {
		return nil, err
	}
	if err = s.guard(ctx, actor, spaceID, permList); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Category) != "" {
		category, err := s.resolveCategory(ctx, spaceID, input.Category, kindHint(filter.Type), true, "")
		if err != nil {
			return nil, err
		}
		filter.CategoryID = category.ID
	}
	if strings.TrimSpace(input.Account) != "" {
		if filter.AccountID, err = s.resolveAccount(ctx, spaceID, input.Account, fieldAccount, true, ""); err != nil {
			return nil, err
		}
	}
	return s.repo.ListTransactions(ctx, filter)
}

func normalizeFilterType(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	return normalizeType(raw)
}

// TransactionUpdate changes the fields that are set. The merged result goes
// through the same validation as a create. A member may change only the
// transactions they created; the actor's finance:manage grant (owner and
// admin roles hold it, members do not) lifts that limit. A row outside the
// caller's Space is ErrNotFound, so Forbidden never confirms its existence.
func (s *service) TransactionUpdate(ctx context.Context, actor domainidentity.ActorContext, spaceArg, id string, input dto.TransactionUpdateInput) (result *dto.TransactionResult, err error) {
	spaceID := resolveSpace(actor, spaceArg)
	defer func() { s.auditResult(ctx, actor, domainaudit.ActionUpdate, resourceTransaction, spaceID, id, err) }()

	txID, err := requireID(id)
	if err != nil {
		return nil, err
	}
	if input == (dto.TransactionUpdateInput{}) {
		return nil, validationError("update", "at least one field is required")
	}
	if err = s.configured(); err != nil {
		return nil, err
	}
	memberID, err := s.guardMember(ctx, actor, spaceID, permUpdate)
	if err != nil {
		return nil, err
	}
	tx, err := s.repo.FindTransaction(ctx, spaceID, txID)
	if err != nil {
		return nil, notFound(err)
	}
	if tx.CreatedByMemberID != memberID && !actor.HasPermission(permManage) {
		return nil, serviceauthorization.ErrForbidden
	}
	if err = s.applyPatch(ctx, tx, input); err != nil {
		return nil, err
	}
	if err = s.repo.UpdateTransaction(ctx, tx); err != nil {
		return nil, notFound(err)
	}
	return &dto.TransactionResult{Transaction: *tx, BudgetStatus: s.budgetStatus(ctx, *tx)}, nil
}

// TransactionDelete soft-deletes a transaction; only roles holding
// finance:delete (owner, admin) may.
func (s *service) TransactionDelete(ctx context.Context, actor domainidentity.ActorContext, spaceArg, id string) (err error) {
	spaceID := resolveSpace(actor, spaceArg)
	defer func() { s.auditResult(ctx, actor, domainaudit.ActionDelete, resourceTransaction, spaceID, id, err) }()

	txID, err := requireID(id)
	if err != nil {
		return err
	}
	if err = s.configured(); err != nil {
		return err
	}
	if err = s.guard(ctx, actor, spaceID, permDelete); err != nil {
		return err
	}
	return notFound(s.repo.SoftDeleteTransaction(ctx, spaceID, txID, time.Now().UTC()))
}

// normalizeText trims v and checks it against a column limit in runes.
func normalizeText(field, v string, limit int) (string, error) {
	v = strings.TrimSpace(v)
	if utf8.RuneCountInString(v) > limit {
		return "", validationError(field, "is too long")
	}
	return v, nil
}
