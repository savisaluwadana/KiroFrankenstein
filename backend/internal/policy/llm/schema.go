package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// MaxRationaleRunes is the longest accepted rationale.
const MaxRationaleRunes = 1000

type wireCommand struct {
	Kind        string `json:"kind"`
	SKU         *int64 `json:"sku"`
	Role        string `json:"role"`
	Qty         *int64 `json:"qty"`
	AmountCents *int64 `json:"amountCents"`
	PORef       *int64 `json:"poRef"`
}

type wireProposal struct {
	Commands  []wireCommand `json:"commands"`
	Rationale string        `json:"rationale"`
}

func violation(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrSchemaViolation, fmt.Sprintf(format, a...))
}

// ParseProposal strictly decodes the model output. It returns commands in
// order and the sanitised rationale. Unknown fields or kinds, missing or
// negative parameters, more than budget commands and an over-long rationale
// are ErrSchemaViolation; non-JSON text is ErrInvalidJSON. It runs before the
// deterministic validator, which still checks every command afterwards.
func ParseProposal(content string, budget int) ([]sim.Command, string, error) {
	dec := json.NewDecoder(strings.NewReader(content))
	dec.DisallowUnknownFields()
	var wp wireProposal
	if err := dec.Decode(&wp); err != nil {
		if _, ok := err.(*json.SyntaxError); ok || err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil, "", fmt.Errorf("%w: not a JSON object", ErrInvalidJSON)
		}
		// Unknown field or wrong type: well-formed JSON of the wrong shape.
		return nil, "", violation("proposal does not match the schema")
	}
	if dec.More() {
		return nil, "", fmt.Errorf("%w: trailing data", ErrInvalidJSON)
	}
	if utf8.RuneCountInString(wp.Rationale) > MaxRationaleRunes {
		return nil, "", violation("rationale longer than %d characters", MaxRationaleRunes)
	}
	if len(wp.Commands) > budget {
		return nil, "", violation("%d commands exceed the budget of %d", len(wp.Commands), budget)
	}
	cmds := make([]sim.Command, 0, len(wp.Commands))
	for i, w := range wp.Commands {
		c, err := w.toCommand()
		if err != nil {
			return nil, "", violation("command %d: %v", i, err)
		}
		cmds = append(cmds, c)
	}
	return cmds, SanitizeRationale(wp.Rationale), nil
}

func (w wireCommand) toCommand() (sim.Command, error) {
	for name, p := range map[string]*int64{"sku": w.SKU, "qty": w.Qty, "amountCents": w.AmountCents, "poRef": w.PORef} {
		if p != nil && *p < 0 {
			return sim.Command{}, fmt.Errorf("%s is negative", name)
		}
	}
	need := func(name string, p *int64, minV int64) (int64, error) {
		if p == nil {
			return 0, fmt.Errorf("%s is required for %s", name, w.Kind)
		}
		if *p < minV {
			return 0, fmt.Errorf("%s must be at least %d", name, minV)
		}
		return *p, nil
	}
	var c sim.Command
	var err error
	switch w.Kind {
	case "set_price":
		c.Kind = sim.CmdSetPrice
		if c.SKU, err = need("sku", w.SKU, 0); err != nil {
			return c, err
		}
		var a int64
		if a, err = need("amountCents", w.AmountCents, 1); err != nil {
			return c, err
		}
		c.AmountCents = sim.Cents(a)
	case "place_purchase_order":
		c.Kind = sim.CmdPlacePurchaseOrder
		if c.SKU, err = need("sku", w.SKU, 0); err != nil {
			return c, err
		}
		switch w.Role {
		case "primary":
			c.Role = sim.SupplierPrimary
		case "backup":
			c.Role = sim.SupplierBackup
		default:
			return c, fmt.Errorf(`role must be "primary" or "backup"`)
		}
		if c.Qty, err = need("qty", w.Qty, 1); err != nil {
			return c, err
		}
	case "expedite_purchase_order":
		c.Kind = sim.CmdExpeditePurchaseOrder
		var p int64
		if p, err = need("poRef", w.PORef, 0); err != nil {
			return c, err
		}
		c.PORef = uint64(p)
	case "schedule_overtime":
		c.Kind = sim.CmdScheduleOvertime
		if c.Qty, err = need("qty", w.Qty, 0); err != nil {
			return c, err
		}
	case "hire_temp_workers":
		c.Kind = sim.CmdHireTempWorkers
		if c.Qty, err = need("qty", w.Qty, 1); err != nil {
			return c, err
		}
	case "set_marketing_spend":
		c.Kind = sim.CmdSetMarketingSpend
		if c.SKU, err = need("sku", w.SKU, 0); err != nil {
			return c, err
		}
		var a int64
		if a, err = need("amountCents", w.AmountCents, 0); err != nil {
			return c, err
		}
		c.AmountCents = sim.Cents(a)
	default:
		return c, fmt.Errorf("unknown command kind")
	}
	return c, nil
}

// SanitizeRationale strips control characters (newlines and tabs become
// spaces) and trims. The result is untrusted plain text: display it escaped,
// never as HTML or markdown.
func SanitizeRationale(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteRune(' ')
		case r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			// drop
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
