package servicefinance

import (
	"context"
	"strings"
	"time"

	domainfinance "family-assistant/internal/domain/finance"
	"family-assistant/internal/dto"
)

// normalizeType lowercases and trims a transaction type and checks it.
func normalizeType(raw string) (string, error) {
	t := strings.ToLower(strings.TrimSpace(raw))
	switch t {
	case domainfinance.TypeIncome, domainfinance.TypeExpense, domainfinance.TypeTransfer:
		return t, nil
	}
	return "", mapDomainError(domainfinance.ErrInvalidType)
}

// applyPatch merges the set fields of patch into tx, resolving account and
// category references inside tx's Space, then validates the merged result.
// Create uses it with every field set on a fresh transaction, update with the
// fields the caller sent on the loaded one. Nothing is written here.
func (s *service) applyPatch(ctx context.Context, tx *domainfinance.Transaction, patch dto.TransactionUpdateInput) error {
	if err := applyScalars(tx, patch); err != nil {
		return err
	}
	if err := s.applyAccounts(ctx, tx, patch); err != nil {
		return err
	}
	if err := s.applyCategory(ctx, tx, patch); err != nil {
		return err
	}
	return mapDomainError(domainfinance.ValidateTransaction(*tx))
}

func applyScalars(tx *domainfinance.Transaction, patch dto.TransactionUpdateInput) (err error) {
	if patch.Type != nil {
		if tx.Type, err = normalizeType(*patch.Type); err != nil {
			return err
		}
	}
	if patch.Amount != nil {
		if err = mapDomainError(domainfinance.ValidateAmount(*patch.Amount)); err != nil {
			return err
		}
		tx.Amount = *patch.Amount
	}
	if patch.Merchant != nil {
		if tx.Merchant, err = normalizeText("merchant", *patch.Merchant, maxMerchantLen); err != nil {
			return err
		}
	}
	if patch.Note != nil {
		if tx.Note, err = normalizeText("note", *patch.Note, maxNoteLen); err != nil {
			return err
		}
	}
	if patch.OccurredAt != nil {
		if patch.OccurredAt.IsZero() {
			return validationError("occurred_at", requiredReason)
		}
		tx.OccurredAt = patch.OccurredAt.UTC()
	}
	return nil
}

// applyAccounts resolves the account and transfer target. An empty string
// clears the target (and leaves the account empty, which validation rejects).
func (s *service) applyAccounts(ctx context.Context, tx *domainfinance.Transaction, patch dto.TransactionUpdateInput) error {
	currentAccount, currentTo := tx.AccountID, ""
	if tx.ToAccountID != nil {
		currentTo = *tx.ToAccountID
	}
	if patch.Account != nil {
		tx.AccountID = ""
		if strings.TrimSpace(*patch.Account) != "" {
			id, err := s.resolveAccount(ctx, tx.SpaceID, *patch.Account, fieldAccount, false, currentAccount)
			if err != nil {
				return err
			}
			tx.AccountID = id
		}
	}
	if patch.ToAccount != nil {
		tx.ToAccountID = nil
		if strings.TrimSpace(*patch.ToAccount) != "" {
			id, err := s.resolveAccount(ctx, tx.SpaceID, *patch.ToAccount, fieldToAccount, false, currentTo)
			if err != nil {
				return err
			}
			tx.ToAccountID = &id
		}
	}
	return nil
}

// applyCategory resolves the category and, whenever the type or category
// changed, checks that the merged category kind matches the type. Transfers
// are left to domain validation, which rejects any category on them.
func (s *service) applyCategory(ctx context.Context, tx *domainfinance.Transaction, patch dto.TransactionUpdateInput) error {
	var category *domainfinance.Category
	currentCategory := ""
	if tx.CategoryID != nil {
		currentCategory = *tx.CategoryID
	}
	if patch.Category != nil {
		tx.CategoryID = nil
		if strings.TrimSpace(*patch.Category) != "" {
			var err error
			if category, err = s.resolveCategory(ctx, tx.SpaceID, *patch.Category, kindHint(tx.Type), false, currentCategory); err != nil {
				return err
			}
			tx.CategoryID = &category.ID
		}
	}
	if tx.CategoryID == nil || tx.Type == domainfinance.TypeTransfer || (patch.Type == nil && patch.Category == nil) {
		return nil
	}
	if category == nil {
		// Unchanged category: it may be archived since, which is fine to keep.
		var err error
		if category, err = s.resolveCategory(ctx, tx.SpaceID, *tx.CategoryID, "", true, ""); err != nil {
			return err
		}
	}
	if category.Kind != tx.Type {
		return validationError(fieldCategory, "kind does not match type")
	}
	return nil
}

// monthRange returns the half-open WIB calendar month containing t as UTC
// instants [from, to).
func monthRange(t time.Time) (from, to time.Time) {
	from = domainfinance.MonthStart(t)
	w := from.In(domainfinance.WIB())
	to = time.Date(w.Year(), w.Month()+1, 1, 0, 0, 0, 0, domainfinance.WIB()).UTC()
	return from, to
}

// budgetStatus reports the category budget of tx's WIB month for an expense,
// with the spending already including tx. It is advisory: any failure or a
// missing budget gives nil and never fails the write that triggered it.
func (s *service) budgetStatus(ctx context.Context, tx domainfinance.Transaction) *domainfinance.BudgetStatus {
	if tx.Type != domainfinance.TypeExpense || tx.CategoryID == nil {
		return nil
	}
	budgets, err := s.repo.ListBudgets(ctx, tx.SpaceID, domainfinance.MonthDate(tx.OccurredAt))
	if err != nil {
		return nil
	}
	for _, budget := range budgets {
		if budget.CategoryID != *tx.CategoryID {
			continue
		}
		from, to := monthRange(tx.OccurredAt)
		spent, err := s.repo.SpentByCategory(ctx, tx.SpaceID, budget.CategoryID, from, to)
		if err != nil {
			return nil
		}
		status := domainfinance.NewBudgetStatus(budget.CategoryID, budget.Amount, spent)
		return &status
	}
	return nil
}

// kindHint is the category kind a transaction type takes: the type itself for
// income and expense, none for a transfer or an unset type.
func kindHint(txType string) string {
	if txType == domainfinance.TypeIncome || txType == domainfinance.TypeExpense {
		return txType
	}
	return ""
}
