package sim

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// CommandKind identifies a member of the closed Command_Set (Req 17.1). Values
// are stored in the Input_Log; append only.
type CommandKind uint8

// The Command_Set. All kinds are implemented; ReasonNotImplemented remains for
// kinds a future engine version adds.
const (
	CmdSetPrice CommandKind = iota
	CmdPlacePurchaseOrder
	CmdExpeditePurchaseOrder
	CmdScheduleOvertime
	CmdHireTempWorkers
	CmdSetMarketingSpend
	numCommandKinds
)

// Name returns the wire name of the kind ("" for an unknown kind).
func (k CommandKind) Name() string {
	switch k {
	case CmdSetPrice:
		return "set_price"
	case CmdPlacePurchaseOrder:
		return "place_purchase_order"
	case CmdExpeditePurchaseOrder:
		return "expedite_purchase_order"
	case CmdScheduleOvertime:
		return "schedule_overtime"
	case CmdHireTempWorkers:
		return "hire_temp_workers"
	case CmdSetMarketingSpend:
		return "set_marketing_spend"
	}
	return ""
}

// Command is one manager decision, a tagged struct of integers so it can be
// canonically encoded into the Input_Log. Only the fields named for the Kind
// are read; the others should be zero.
//
//	set_price             SKU, AmountCents (new unit price)
//	place_purchase_order  SKU, Role (SupplierPrimary or SupplierBackup), Qty
//	expedite_purchase_order  PORef
//	schedule_overtime     Qty (overtime hours per regular worker per Tick, from the
//	                      next Tick until changed; 0 cancels)
//	hire_temp_workers     Qty (workers; paid from the effective Tick, capacity after onboarding)
//	set_marketing_spend   SKU, AmountCents (spend per Tick, in force from the effective
//	                      Tick until changed; 0 stops it)
//
// SKU is an index into Scenario.SKUs.
type Command struct {
	Kind        CommandKind
	SKU         int64
	Role        uint8
	Qty         int64
	AmountCents Cents
	PORef       uint64
}

// Command errors returned by the engine when it applies an input that did not
// pass Validate.
var (
	ErrCommandNotImplemented = errors.New("sim: command kind is not implemented yet")
	ErrBadCommand            = errors.New("sim: command cannot be applied")
)

// commandEncodedLen is the fixed size of EncodeCommand output.
const commandEncodedLen = 1 + 8 + 1 + 8 + 8 + 8

// EncodeCommand returns the canonical fixed-width big-endian encoding of c:
// kind, SKU, role, qty, amount, PO reference.
func EncodeCommand(c Command) []byte {
	e := encoder{b: make([]byte, 0, commandEncodedLen)}
	e.u8(uint8(c.Kind))
	e.i64(c.SKU)
	e.u8(c.Role)
	e.i64(c.Qty)
	e.i64(int64(c.AmountCents))
	e.u64(c.PORef)
	return e.b
}

// DecodeCommand is the inverse of EncodeCommand.
func DecodeCommand(b []byte) (Command, error) {
	if len(b) != commandEncodedLen {
		return Command{}, fmt.Errorf("%w: encoded length %d, want %d", ErrBadCommand, len(b), commandEncodedLen)
	}
	return Command{
		Kind:        CommandKind(b[0]),
		SKU:         int64(binary.BigEndian.Uint64(b[1:9])),
		Role:        b[9],
		Qty:         int64(binary.BigEndian.Uint64(b[10:18])),
		AmountCents: Cents(binary.BigEndian.Uint64(b[18:26])),
		PORef:       binary.BigEndian.Uint64(b[26:34]),
	}, nil
}

// commandEvent builds the EvCommandApply event of inp at sim minute at. The
// payload carries Ref = input sequence, SKU, Qty (quantity, or the price for
// set_price) and Aux = kind<<8 | role.
func commandEvent(at SimMinute, inp Input) Event {
	c := inp.Command
	qty := c.Qty
	switch c.Kind {
	case CmdSetPrice, CmdSetMarketingSpend:
		qty = int64(c.AmountCents)
	case CmdExpeditePurchaseOrder:
		qty = int64(c.PORef) // the order to expedite
	}
	return Event{
		At:       at,
		Priority: PriorityCommandApply,
		Kind:     EvCommandApply,
		Payload:  EventPayload{Ref: int64(inp.Seq), SKU: c.SKU, Qty: qty, Aux: int64(c.Kind)<<8 | int64(c.Role)},
	}
}

// applyCommand handles EvCommandApply. It reads no random stream. On error the
// State and queue are left as they were before the command.
func (e *stepper) applyCommand(ev Event) error {
	kind := CommandKind(ev.Payload.Aux >> 8)
	role := uint8(ev.Payload.Aux & 0xff)
	switch kind {
	case CmdSetPrice:
		for i := range e.st.Prices {
			if e.st.Prices[i].SKU == ev.Payload.SKU {
				if ev.Payload.Qty <= 0 {
					return fmt.Errorf("%w: set_price needs a positive price", ErrBadCommand)
				}
				e.st.Prices[i].PriceCents = Cents(ev.Payload.Qty)
				return nil
			}
		}
		return fmt.Errorf("%w: set_price: %w: %d", ErrBadCommand, ErrUnknownSKU, ev.Payload.SKU)
	case CmdPlacePurchaseOrder:
		if _, err := PlacePurchaseOrder(e.sc, &e.st, e.q, ev.Payload.SKU, role, ev.Payload.Qty, ev.At); err != nil {
			return fmt.Errorf("%w: place_purchase_order: %w", ErrBadCommand, err)
		}
		return nil
	case CmdExpeditePurchaseOrder:
		if err := ExpeditePurchaseOrder(e.sc, &e.st, e.q, uint64(ev.Payload.Qty), ev.At); err != nil {
			return fmt.Errorf("%w: expedite_purchase_order: %w", ErrBadCommand, err)
		}
		return nil
	case CmdScheduleOvertime:
		if err := ScheduleOvertime(e.sc, &e.st, ev.Payload.Qty); err != nil {
			return fmt.Errorf("%w: schedule_overtime: %w", ErrBadCommand, err)
		}
		return nil
	case CmdHireTempWorkers:
		if err := HireTempWorkers(e.sc, &e.st, ev.Payload.Qty, ev.At); err != nil {
			return fmt.Errorf("%w: hire_temp_workers: %w", ErrBadCommand, err)
		}
		return nil
	case CmdSetMarketingSpend:
		if err := SetMarketingSpend(e.sc, &e.st, ev.Payload.SKU, Cents(ev.Payload.Qty)); err != nil {
			return fmt.Errorf("%w: set_marketing_spend: %w", ErrBadCommand, err)
		}
		return nil
	}
	return fmt.Errorf("%w: unknown kind %d", ErrBadCommand, kind)
}
