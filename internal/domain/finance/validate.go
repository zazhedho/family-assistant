package domainfinance

import "errors"

// Sentinel errors carry the offending field in the message; the service layer
// maps them to serviceauthorization.ValidationError.
var (
	ErrInvalidType        = errors.New("type must be income, expense or transfer")
	ErrAccountRequired    = errors.New("account_id is required")
	ErrToAccountRequired  = errors.New("to_account_id is required for a transfer")
	ErrToAccountForbidden = errors.New("to_account_id is only allowed for a transfer")
	ErrTransferSameAcct   = errors.New("transfer accounts must differ")
	ErrTransferCategory   = errors.New("a transfer cannot have a category")
)

// ValidateTransaction checks the type, amount, account and transfer rules. It
// does not check Space membership or archived state; the service does.
func ValidateTransaction(t Transaction) error {
	switch t.Type {
	case TypeIncome, TypeExpense, TypeTransfer:
	default:
		return ErrInvalidType
	}
	if err := ValidateAmount(t.Amount); err != nil {
		return err
	}
	if t.AccountID == "" {
		return ErrAccountRequired
	}
	if t.Type != TypeTransfer {
		if t.ToAccountID != "" {
			return ErrToAccountForbidden
		}
		return nil
	}
	if t.ToAccountID == "" {
		return ErrToAccountRequired
	}
	if t.ToAccountID == t.AccountID {
		return ErrTransferSameAcct
	}
	if t.CategoryID != "" {
		return ErrTransferCategory
	}
	return nil
}
