package app

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/savisaluwadana/KiroFrankenstein/backend/internal/sim"
)

// DiffStatement is the observed-differences statement of Req 23.8. It is
// carried verbatim by every DiffResult.
const DiffStatement = "Observed differences do not establish cause. To attribute an effect to one decision, fork and change only that decision."

// DiffCurrency is the ISO 4217 code of every money figure in a DiffResult.
const DiffCurrency = "USD"

// Top-level State component names reported by a diff (machine keys).
const (
	CompClock          = "clock"
	CompInventory      = "inventory"
	CompOrders         = "orders"
	CompOrderSeq       = "order_seq"
	CompPurchaseOrders = "purchase_orders"
	CompPOSeq          = "po_seq"
	CompWorkforce      = "workforce"
	CompLedger         = "ledger"
	CompDisruptions    = "disruptions"
	CompPrices         = "prices"
	CompStreams        = "streams"
	CompPending        = "pending_events"
	CompEventSeq       = "event_seq"
)

// Metric keys reported by a diff.
const (
	MetricNetProfit         = "net_profit"
	MetricCash              = "cash"
	MetricServiceLevel      = "service_level"
	MetricFillRate          = "fill_rate"
	MetricBacklogOrders     = "backlog_orders"
	MetricBacklogUnits      = "backlog_units"
	MetricLateBacklogOrders = "late_backlog_orders"
	MetricCancelledOrders   = "cancelled_orders"
)

// DiffComponent is a top-level State component whose canonical content
// differs at the First_Divergence_Tick. SKUs lists the SKU indexes of the
// per-SKU entries that differ (empty for components without per-SKU entries).
type DiffComponent struct {
	Name string
	SKUs []int64
}

// DiffInput is an Input present in one Branch and absent from the other.
// Side is "A" or "B". Seq is the engine-visible sequence number of the
// Branch's resolved input list (the one ExportInputs returns).
type DiffInput struct {
	Side          string
	Branch        BranchID
	Seq           uint64
	EffectiveTick sim.Tick
	Kind          sim.InputKind
	Command       *sim.Command
	Disruption    *sim.DisruptionInput
	Provenance    Provenance
}

// DiffMetric is one metric at the Comparison_Tick: raw values of both
// Branches and Delta = B - A.
type DiffMetric struct {
	Key   string
	A, B  int64
	Delta int64
}

// DiffSKUStock is the on-hand stock of one SKU in both Branches at the
// Comparison_Tick.
type DiffSKUStock struct {
	SKU        int64
	A, B       int64
	Delta      int64
	ValueA     int64
	ValueB     int64
	ValueDelta int64
}

// DiffResult is the observed difference of two Branches of one World.
type DiffResult struct {
	BranchA, BranchB BranchID
	World            WorldID
	ComparisonTick   sim.Tick
	// FirstDivergenceTick is nil when the State_Hashes are equal at every
	// Tick up to ComparisonTick.
	FirstDivergenceTick *sim.Tick
	// DifferingComponents is set when FirstDivergenceTick is set.
	DifferingComponents []DiffComponent
	// FirstEventA and FirstEventB are the first outcome-journal entries of
	// the divergence Tick that differ between the Branches. Either may be nil
	// when that Branch has no entry at that position.
	FirstEventA, FirstEventB *JournalEntry
	DifferingInputs          []DiffInput
	Metrics                  []DiffMetric
	SKUStock                 []DiffSKUStock
	Statement                string
	Disclaimer               string
	Currency                 string
	Warnings                 []Warning
}

// Diff compares two Branches of one World. It performs reads only and holds
// the service mutex only inside each individual read, never across the scan.
// A nil comparisonTick means the lower of the two Head_Ticks.
func (s *BranchService) Diff(ctx context.Context, branchA, branchB BranchID, comparisonTick *sim.Tick) (DiffResult, error) {
	a, err := s.GetBranch(ctx, branchA)
	if err != nil {
		return DiffResult{}, err
	}
	b, err := s.GetBranch(ctx, branchB)
	if err != nil {
		return DiffResult{}, err
	}
	if a.WorldID != b.WorldID {
		return DiffResult{}, fmt.Errorf("branches %s and %s belong to different worlds: %w", a.ID, b.ID, ErrIncomparableBranches)
	}
	wa, _, err := s.GetWorld(ctx, a.WorldID)
	if err != nil {
		return DiffResult{}, err
	}
	wb, _, err := s.GetWorld(ctx, b.WorldID)
	if err != nil {
		return DiffResult{}, err
	}
	if wa.ScenarioHash != wb.ScenarioHash || wa.EngineVersion != wb.EngineVersion {
		return DiffResult{}, fmt.Errorf("scenario hash or engine version differ: %w", ErrIncomparableBranches)
	}
	headA, err := s.HeadTick(ctx, a.ID)
	if err != nil {
		return DiffResult{}, err
	}
	headB, err := s.HeadTick(ctx, b.ID)
	if err != nil {
		return DiffResult{}, err
	}
	cmp := min(headA, headB)
	if comparisonTick != nil {
		cmp = *comparisonTick
		if cmp < 0 {
			return DiffResult{}, fmt.Errorf("comparison tick %d: %w", cmp, ErrInvalidTick)
		}
		if cmp > headA || cmp > headB {
			return DiffResult{}, fmt.Errorf("comparison tick %d > head %d/%d: %w", cmp, headA, headB, ErrNotYetSimulated)
		}
	}
	warnings, err := s.EngineWarnings(ctx, a.ID)
	if err != nil {
		return DiffResult{}, err
	}
	res := DiffResult{
		BranchA: a.ID, BranchB: b.ID, World: a.WorldID, ComparisonTick: cmp,
		Statement: DiffStatement, Disclaimer: sim.Disclaimer, Currency: DiffCurrency, Warnings: warnings,
	}

	if err := s.diffInputs(ctx, &res, a, b, cmp); err != nil {
		return DiffResult{}, err
	}
	if err := s.diffMetrics(ctx, &res, a.ID, b.ID, cmp); err != nil {
		return DiffResult{}, err
	}
	if a.ID != b.ID {
		if err := s.diffDivergence(ctx, &res, a, b, cmp); err != nil {
			return DiffResult{}, err
		}
	}
	return res, nil
}

// divergenceScanStart returns the lowest Tick at which the two Branches could
// differ: hashes are equal up to the fork Tick of the Branch that leaves the
// common ancestry first (fork fidelity, INV-5).
func (s *BranchService) divergenceScanStart(ctx context.Context, a, b Branch) (sim.Tick, error) {
	chain := func(x Branch) ([]Branch, error) {
		out := []Branch{x}
		for x.ParentBranchID != "" {
			p, err := s.GetBranch(ctx, x.ParentBranchID)
			if err != nil {
				return nil, err
			}
			out = append(out, p)
			x = p
		}
		return out, nil
	}
	ca, err := chain(a)
	if err != nil {
		return 0, err
	}
	cb, err := chain(b)
	if err != nil {
		return 0, err
	}
	idx := make(map[BranchID]int, len(ca))
	for i, x := range ca {
		idx[x.ID] = i
	}
	for j, x := range cb {
		i, ok := idx[x.ID]
		if !ok {
			continue
		}
		start := sim.Tick(-1)
		consider := func(c []Branch, k int) {
			if k == 0 {
				return // this branch is the common ancestor itself
			}
			if ft := c[k-1].ForkTick; start < 0 || ft < start {
				start = ft
			}
		}
		consider(ca, i)
		consider(cb, j)
		if start < 0 {
			start = 0
		}
		return start, nil
	}
	return 0, nil
}

func (s *BranchService) diffDivergence(ctx context.Context, res *DiffResult, a, b Branch, cmp sim.Tick) error {
	start, err := s.divergenceScanStart(ctx, a, b)
	if err != nil {
		return err
	}
	for t := start; t <= cmp; t++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		fa, err := s.FrameAt(ctx, a.ID, t)
		if err != nil {
			return err
		}
		fb, err := s.FrameAt(ctx, b.ID, t)
		if err != nil {
			return err
		}
		if fa.Hash == fb.Hash {
			continue
		}
		tt := t
		res.FirstDivergenceTick = &tt
		sa, err := s.StateAt(ctx, a.ID, t)
		if err != nil {
			return err
		}
		sb, err := s.StateAt(ctx, b.ID, t)
		if err != nil {
			return err
		}
		res.DifferingComponents = diffComponents(sa, sb)
		ja, err := s.JournalRange(ctx, a.ID, t, t)
		if err != nil {
			return err
		}
		jb, err := s.JournalRange(ctx, b.ID, t, t)
		if err != nil {
			return err
		}
		for i := 0; i < len(ja) || i < len(jb); i++ {
			var ea, eb *JournalEntry
			if i < len(ja) {
				ea = &ja[i]
			}
			if i < len(jb) {
				eb = &jb[i]
			}
			if ea == nil || eb == nil || *ea != *eb {
				res.FirstEventA, res.FirstEventB = ea, eb
				break
			}
		}
		return nil
	}
	return nil
}

func diffComponents(a, b sim.State) []DiffComponent {
	var out []DiffComponent
	add := func(name string, equal bool, skus []int64) {
		if !equal {
			out = append(out, DiffComponent{Name: name, SKUs: skus})
		}
	}
	eq := func(x, y any) bool { return reflect.DeepEqual(x, y) }
	add(CompClock, a.Clock == b.Clock, nil)
	add(CompInventory, eq(a.Inventory, b.Inventory), skuDiff(a.Inventory, b.Inventory, func(x sim.SKUInventory) int64 { return x.SKU }))
	add(CompOrders, eq(a.Orders, b.Orders), skuDiff(a.Orders.Open, b.Orders.Open, func(x sim.Order) int64 { return x.SKU }))
	add(CompOrderSeq, a.OrderSeq == b.OrderSeq, nil)
	add(CompPurchaseOrders, eq(a.PurchaseOs, b.PurchaseOs), skuDiff(a.PurchaseOs, b.PurchaseOs, func(x sim.PurchaseOrder) int64 { return x.SKU }))
	add(CompPOSeq, a.POSeq == b.POSeq, nil)
	add(CompWorkforce, eq(a.Workforce, b.Workforce), nil)
	add(CompLedger, eq(a.Ledger, b.Ledger), nil)
	add(CompDisruptions, eq(a.Disruptions, b.Disruptions), nil)
	add(CompPrices, eq(a.Prices, b.Prices), skuDiff(a.Prices, b.Prices, func(x sim.SKUPrice) int64 { return x.SKU }))
	add(CompStreams, eq(a.Streams, b.Streams), nil)
	add(CompPending, eq(a.Pending, b.Pending), nil)
	add(CompEventSeq, a.EventSeq == b.EventSeq, nil)
	return out
}

// skuDiff returns the sorted, de-duplicated SKUs of entries that are not
// matched one-to-one by an equal entry on the other side.
func skuDiff[T any](a, b []T, sku func(T) int64) []int64 {
	type bal struct {
		n   int
		sku int64
	}
	m := map[string]*bal{}
	for _, x := range a {
		k := fmt.Sprintf("%+v", x)
		if m[k] == nil {
			m[k] = &bal{sku: sku(x)}
		}
		m[k].n++
	}
	for _, x := range b {
		k := fmt.Sprintf("%+v", x)
		if m[k] == nil {
			m[k] = &bal{sku: sku(x)}
		}
		m[k].n--
	}
	seen := map[int64]bool{}
	var out []int64
	for _, v := range m {
		if v.n != 0 && !seen[v.sku] {
			seen[v.sku] = true
			out = append(out, v.sku)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (s *BranchService) diffMetrics(ctx context.Context, res *DiffResult, a, b BranchID, cmp sim.Tick) error {
	ma, err := s.MetricsAt(ctx, a, cmp)
	if err != nil {
		return err
	}
	mb, err := s.MetricsAt(ctx, b, cmp)
	if err != nil {
		return err
	}
	sa, err := s.StateAt(ctx, a, cmp)
	if err != nil {
		return err
	}
	sb, err := s.StateAt(ctx, b, cmp)
	if err != nil {
		return err
	}
	m := func(key string, x, y int64) {
		res.Metrics = append(res.Metrics, DiffMetric{Key: key, A: x, B: y, Delta: y - x})
	}
	m(MetricNetProfit, int64(ma.NetProfit), int64(mb.NetProfit))
	m(MetricCash, int64(ma.Cash), int64(mb.Cash))
	m(MetricServiceLevel, int64(ma.ServiceLevel), int64(mb.ServiceLevel))
	m(MetricFillRate, int64(ma.FillRate), int64(mb.FillRate))
	m(MetricBacklogOrders, ma.BacklogOrders, mb.BacklogOrders)
	m(MetricBacklogUnits, ma.BacklogUnits, mb.BacklogUnits)
	m(MetricLateBacklogOrders, ma.LateBacklogOrders, mb.LateBacklogOrders)
	m(MetricCancelledOrders, sa.Orders.Cancelled, sb.Orders.Cancelled)

	n := max(len(sa.Inventory), len(sb.Inventory))
	for i := 0; i < n; i++ {
		var x, y sim.SKUInventory
		if i < len(sa.Inventory) {
			x = sa.Inventory[i]
		}
		if i < len(sb.Inventory) {
			y = sb.Inventory[i]
		}
		skuIdx := x.SKU
		if i >= len(sa.Inventory) {
			skuIdx = y.SKU
		}
		res.SKUStock = append(res.SKUStock, DiffSKUStock{
			SKU: skuIdx, A: x.OnHand, B: y.OnHand, Delta: y.OnHand - x.OnHand,
			ValueA: int64(x.Value), ValueB: int64(y.Value), ValueDelta: int64(y.Value - x.Value),
		})
	}
	return nil
}

func (s *BranchService) diffInputs(ctx context.Context, res *DiffResult, a, b Branch, cmp sim.Tick) error {
	ea, err := s.ExportInputs(ctx, a.ID)
	if err != nil {
		return err
	}
	eb, err := s.ExportInputs(ctx, b.ID)
	if err != nil {
		return err
	}
	key := func(in sim.Input) string {
		return fmt.Sprintf("%d|%d|%+v|%+v", in.EffectiveTick, in.Kind, in.Command, in.Disruption)
	}
	pool := map[string][]int{} // key -> unmatched indexes of B
	for i, in := range eb.Inputs {
		if in.EffectiveTick <= cmp {
			k := key(in)
			pool[k] = append(pool[k], i)
		}
	}
	usedB := map[int]bool{}
	mk := func(side string, id BranchID, ex InputsExport, i int) DiffInput {
		in := ex.Inputs[i]
		d := DiffInput{Side: side, Branch: id, Seq: in.Seq, EffectiveTick: in.EffectiveTick, Kind: in.Kind}
		if i < len(ex.Metadata) {
			d.Provenance = ex.Metadata[i].Provenance
		}
		switch in.Kind {
		case sim.InputCommand:
			c := in.Command
			d.Command = &c
		case sim.InputDisruption:
			x := in.Disruption
			d.Disruption = &x
		}
		return d
	}
	for i, in := range ea.Inputs {
		if in.EffectiveTick > cmp {
			continue
		}
		k := key(in)
		if q := pool[k]; len(q) > 0 {
			usedB[q[0]] = true
			pool[k] = q[1:]
			continue
		}
		res.DifferingInputs = append(res.DifferingInputs, mk("A", a.ID, ea, i))
	}
	for i, in := range eb.Inputs {
		if in.EffectiveTick <= cmp && !usedB[i] {
			res.DifferingInputs = append(res.DifferingInputs, mk("B", b.ID, eb, i))
		}
	}
	sort.SliceStable(res.DifferingInputs, func(i, j int) bool {
		x, y := res.DifferingInputs[i], res.DifferingInputs[j]
		if x.EffectiveTick != y.EffectiveTick {
			return x.EffectiveTick < y.EffectiveTick
		}
		if x.Side != y.Side {
			return x.Side < y.Side
		}
		return x.Seq < y.Seq
	})
	return nil
}
