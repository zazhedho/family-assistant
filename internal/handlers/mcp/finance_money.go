package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"

	domainfinance "family-assistant/internal/domain/finance"
	servicefinance "family-assistant/internal/services/finance"

	"github.com/google/jsonschema-go/jsonschema"
)

// MoneyInput is a money tool input: a JSON number or a JSON string holding a
// decimal such as 25000 or "1005422.44". It keeps the text and never goes
// through a float, so parsing (strict grammar, two decimals, bounds) happens
// in the handler where a failure can name the input field.
//
// The MCP SDK decodes tool arguments into float64 before the handler sees
// them, so a number arrives as its shortest float text: exact for every value
// within the bound, and an exponent form such as 1e2 can no longer be told
// apart from 100 (accepted as 100). A string keeps its exact text, so the
// tools tell the model to prefer strings for amounts.
type MoneyInput string

func (m *MoneyInput) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		var text string
		if err := json.Unmarshal(b, &text); err != nil {
			return err
		}
		*m = MoneyInput(text)
		return nil
	}
	*m = MoneyInput(b)
	return nil
}

// moneyOutputPattern is what a Money marshals to: whole amounts without
// decimals, others with exactly two, negatives with a leading minus.
const moneyOutputPattern = `^-?\d+(\.\d{2})?$`

// financeSchemaOptions maps the money types to their schemas: input money is
// a number or a string; output money is an exact decimal string.
func financeSchemaOptions() *jsonschema.ForOptions {
	return &jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[MoneyInput](): {
			Types:       []string{"number", "string"},
			Description: "rupiah with up to two decimals, e.g. 25000 or 1005422.44, as a plain number or a string",
		},
		reflect.TypeFor[domainfinance.Money](): {
			Type:        "string",
			Pattern:     moneyOutputPattern,
			Description: "rupiah as an exact decimal string with up to two decimals, e.g. \"25000\" or \"1005422.44\"; negative values start with '-'",
		},
	}}
}

// moneyError maps a parse or range failure of the amount field to a tool
// error naming that field.
func moneyError(field string, err error) error {
	return MapToolError(servicefinance.MapMoneyError(field, err))
}

// parse reads the text as an amount in sen within the allowed bounds.
func (m MoneyInput) parse(field string, allowNegative bool) (domainfinance.Money, error) {
	value, err := domainfinance.ParseMoney(string(m), allowNegative)
	if err != nil {
		return 0, moneyError(field, err)
	}
	return value, nil
}

// financeAmount parses a required positive amount (transaction, budget,
// recurring); zero and negatives are rejected here, naming the field.
func financeAmount(m MoneyInput, field string) (domainfinance.Money, error) {
	value, err := m.parse(field, true)
	if err != nil {
		return 0, err
	}
	if err := domainfinance.ValidateAmount(value); err != nil {
		return 0, moneyError(field, err)
	}
	return value, nil
}

// financeAmountPtr parses an optional update amount: nil (absent or JSON
// null) stays nil, meaning unchanged.
func financeAmountPtr(m *MoneyInput, field string) (*domainfinance.Money, error) {
	if m == nil {
		return nil, nil
	}
	value, err := financeAmount(*m, field)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

// financeOpeningBalance parses the optional opening balance: blank is zero.
// The sign rule (negative only for credit cards) stays with the service.
func financeOpeningBalance(m MoneyInput) (domainfinance.Money, error) {
	if m == "" {
		return 0, nil
	}
	return m.parse("opening_balance", true)
}

var errNoOutputType = errors.New("mcp: finance tool output type is not a struct")

// financeOutputSchema derives the output schema of Out with the money types
// mapped, dereferencing a pointer so the root stays a plain object. The SDK
// would otherwise infer integer money (it has no hook for named types).
func financeOutputSchema[Out any]() (*jsonschema.Schema, error) {
	rt := reflect.TypeFor[Out]()
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct {
		return nil, errNoOutputType
	}
	return jsonschema.ForType(rt, financeSchemaOptions())
}
