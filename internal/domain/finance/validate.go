package domainfinance

import "errors"

// Sentinel errors carry the offending field in the message; the service layer
// maps them to serviceauthorization.ValidationError.
var (
	ErrInvalidType        = errors.New("type must be income, expense or transfer")
	ErrAccountRequired    = errors.New("account is required")
	ErrToAccountRequired  = errors.New("to_account is required for a transfer")
	ErrToAccountForbidden = errors.New("to_account is only allowed for a transfer")
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
	toAccount := ""
	if t.ToAccountID != nil {
		toAccount = *t.ToAccountID
	}
	category := ""
	if t.CategoryID != nil {
		category = *t.CategoryID
	}
	if t.AccountID == "" {
		return ErrAccountRequired
	}
	if t.Type != TypeTransfer {
		if toAccount != "" {
			return ErrToAccountForbidden
		}
		return nil
	}
	if toAccount == "" {
		return ErrToAccountRequired
	}
	if toAccount == t.AccountID {
		return ErrTransferSameAcct
	}
	if category != "" {
		return ErrTransferCategory
	}
	return nil
}
