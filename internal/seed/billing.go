package seed

import "github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/config"

const billingFixturePoolSize = 4

const (
	createCustomerTableStatement     = "CREATE TABLE IF NOT EXISTS customer (id BIGINT PRIMARY KEY, status VARCHAR(32) NOT NULL)"
	createFolioTableStatement        = "CREATE TABLE IF NOT EXISTS folio (id BIGINT PRIMARY KEY, customer_id BIGINT NOT NULL, last_statement_id BIGINT NULL, CONSTRAINT fk_folio_customer FOREIGN KEY (customer_id) REFERENCES customer(id))"
	createJournalTableStatement      = "CREATE TABLE IF NOT EXISTS journal (id BIGINT PRIMARY KEY, customer_id BIGINT NOT NULL, folio_id BIGINT NOT NULL, member_id BIGINT NULL, reference VARCHAR(255) NOT NULL, amount_cents BIGINT NOT NULL, CONSTRAINT fk_journal_customer FOREIGN KEY (customer_id) REFERENCES customer(id), CONSTRAINT fk_journal_folio FOREIGN KEY (folio_id) REFERENCES folio(id))"
	createPostingTableStatement      = "CREATE TABLE IF NOT EXISTS posting (id BIGINT PRIMARY KEY, journal_id BIGINT NOT NULL, status VARCHAR(32) NOT NULL, amount_cents BIGINT NOT NULL, CONSTRAINT fk_posting_journal FOREIGN KEY (journal_id) REFERENCES journal(id))"
	createBillTableStatement         = "CREATE TABLE IF NOT EXISTS bill (id BIGINT PRIMARY KEY, journal_id BIGINT NOT NULL, folio_id BIGINT NOT NULL, status VARCHAR(32) NOT NULL, total_cents BIGINT NOT NULL, paid_cents BIGINT NOT NULL, version BIGINT NOT NULL DEFAULT 0, CONSTRAINT fk_bill_journal FOREIGN KEY (journal_id) REFERENCES journal(id), CONSTRAINT fk_bill_folio FOREIGN KEY (folio_id) REFERENCES folio(id))"
	createPaymentTableStatement      = "CREATE TABLE IF NOT EXISTS payment (id BIGINT PRIMARY KEY, bill_id BIGINT NOT NULL, journal_id BIGINT NOT NULL, status VARCHAR(32) NOT NULL, amount_cents BIGINT NOT NULL, CONSTRAINT fk_payment_bill FOREIGN KEY (bill_id) REFERENCES bill(id), CONSTRAINT fk_payment_journal FOREIGN KEY (journal_id) REFERENCES journal(id))"
	createFolioBalanceTableStatement = "CREATE TABLE IF NOT EXISTS foliobalance (id BIGINT PRIMARY KEY, customer_id BIGINT NOT NULL, folio_id BIGINT NOT NULL, balance_cents BIGINT NOT NULL, updated_count BIGINT NOT NULL DEFAULT 0, CONSTRAINT fk_foliobalance_customer FOREIGN KEY (customer_id) REFERENCES customer(id), CONSTRAINT fk_foliobalance_folio FOREIGN KEY (folio_id) REFERENCES folio(id))"
	createFeeLogTableStatement       = "CREATE TABLE IF NOT EXISTS feelog (id BIGINT PRIMARY KEY, journal_id BIGINT NOT NULL, fee_bill_id BIGINT NULL, amount_cents BIGINT NOT NULL, CONSTRAINT fk_feelog_journal FOREIGN KEY (journal_id) REFERENCES journal(id), CONSTRAINT fk_feelog_bill FOREIGN KEY (fee_bill_id) REFERENCES bill(id))"
	createStatementTableStatement    = "CREATE TABLE IF NOT EXISTS statement (id BIGINT PRIMARY KEY, customer_id BIGINT NOT NULL, folio_id BIGINT NOT NULL, status VARCHAR(32) NOT NULL, balance_cents BIGINT NOT NULL, CONSTRAINT fk_statement_customer FOREIGN KEY (customer_id) REFERENCES customer(id), CONSTRAINT fk_statement_folio FOREIGN KEY (folio_id) REFERENCES folio(id))"
	createWithdrawalTableStatement   = "CREATE TABLE IF NOT EXISTS withdrawal (id BIGINT PRIMARY KEY, journal_id BIGINT NOT NULL, statement_id BIGINT NULL, status VARCHAR(32) NOT NULL, amount_cents BIGINT NOT NULL, CONSTRAINT fk_withdrawal_journal FOREIGN KEY (journal_id) REFERENCES journal(id), CONSTRAINT fk_withdrawal_statement FOREIGN KEY (statement_id) REFERENCES statement(id) ON DELETE CASCADE)"
)

type JournalPostingBillSlot struct {
	CustomerID int64
	FolioID    int64
	JournalID  int64
	PostingID  int64
	BillID     int64
}

type FolioBalanceUpdateSlot struct {
	CustomerID     int64
	FolioID        int64
	FolioBalanceID int64
}

type FKBackfillSlot struct {
	CustomerID   int64
	FolioID      int64
	JournalID    int64
	BillID       int64
	FeeLogID     int64
	StatementID  int64
	WithdrawalID int64
}

type PaymentMixedReferenceSlot struct {
	CustomerID int64
	FolioID    int64
	BillID     int64
	JournalID  int64
	PaymentID  int64
}

type CascadePathSlot struct {
	CustomerID   int64
	FolioID      int64
	JournalID    int64
	StatementID  int64
	WithdrawalID int64
}

type PaymentBillUpdateProbeSlot struct {
	CustomerID int64
	FolioID    int64
	BillID     int64
	JournalID  int64
	PaymentID  int64
}

type StatementFolioParentUpdateProbeSlot struct {
	CustomerID  int64
	FolioID     int64
	StatementID int64
}

type BillingSeedPlan struct {
	CustomerIDs []int64
	FolioIDs    []int64

	ExistingCustomerID int64
	ExistingFolioID    int64
	ExistingJournalID  int64
	ExistingBillID     int64

	JournalPostingBillSlots              []JournalPostingBillSlot
	FolioBalanceUpdateSlots              []FolioBalanceUpdateSlot
	FKBackfillSlots                      []FKBackfillSlot
	PaymentMixedReferenceSlots           []PaymentMixedReferenceSlot
	CascadePathSlots                     []CascadePathSlot
	PaymentBillUpdateProbeSlots          []PaymentBillUpdateProbeSlot
	StatementFolioParentUpdateProbeSlots []StatementFolioParentUpdateProbeSlot
}

func BillingPlan(cfg config.Config) BillingSeedPlan {
	customerCount := max(cfg.SeedParentRowsPerTable, 0)
	if customerCount == 0 {
		return BillingSeedPlan{}
	}

	customerIDs := make([]int64, 0, customerCount)
	folioIDs := make([]int64, 0, customerCount)
	for i := 0; i < customerCount; i++ {
		customerIDs = append(customerIDs, int64(i+1))
		folioIDs = append(folioIDs, int64(1001+i))
	}

	plan := BillingSeedPlan{
		CustomerIDs:        customerIDs,
		FolioIDs:           folioIDs,
		ExistingCustomerID: customerIDs[0],
		ExistingFolioID:    folioIDs[0],
		ExistingJournalID:  3001,
		ExistingBillID:     4001,
	}

	plan.JournalPostingBillSlots = buildJournalPostingBillSlots(customerIDs, folioIDs, 10001, 11001, 12001)
	plan.FolioBalanceUpdateSlots = buildFolioBalanceUpdateSlots(customerIDs, folioIDs, 13001)
	plan.FKBackfillSlots = buildFKBackfillSlots(customerIDs, folioIDs, 14001, 15001, 16001, 17001, 18001)
	plan.PaymentMixedReferenceSlots = buildPaymentMixedReferenceSlots(
		customerIDs,
		folioIDs,
		plan.ExistingBillID,
		19001,
		20001,
	)
	plan.CascadePathSlots = buildCascadePathSlots(customerIDs, folioIDs, plan.ExistingJournalID, 21001, 22001)
	plan.PaymentBillUpdateProbeSlots = buildPaymentBillUpdateProbeSlots(
		customerIDs,
		folioIDs,
		plan.ExistingBillID,
		plan.ExistingJournalID,
		18001,
	)
	plan.StatementFolioParentUpdateProbeSlots = buildStatementFolioParentUpdateProbeSlots(
		customerIDs,
		folioIDs,
		23001,
	)

	return plan
}

func billingSchemaStatements() []applyStatement {
	return []applyStatement{
		{query: createCustomerTableStatement},
		{query: createFolioTableStatement},
		{query: createJournalTableStatement},
		{query: createPostingTableStatement},
		{query: createBillTableStatement},
		{query: createPaymentTableStatement},
		{query: createFolioBalanceTableStatement},
		{query: createFeeLogTableStatement},
		{query: createStatementTableStatement},
		{query: createWithdrawalTableStatement},
	}
}

func billingFixtureStatements(plan BillingSeedPlan) []applyStatement {
	var statements []applyStatement

	if stmt, ok := newInsertStatement(
		"customer",
		[]string{"id", "status"},
		[]string{"status"},
		billingCustomerRows(plan),
	); ok {
		statements = append(statements, stmt)
	}

	if stmt, ok := newInsertStatement(
		"folio",
		[]string{"id", "customer_id", "last_statement_id"},
		[]string{"customer_id", "last_statement_id"},
		billingFolioRows(plan),
	); ok {
		statements = append(statements, stmt)
	}

	if stmt, ok := newInsertStatement(
		"journal",
		[]string{"id", "customer_id", "folio_id", "member_id", "reference", "amount_cents"},
		[]string{"customer_id", "folio_id", "member_id", "reference", "amount_cents"},
		billingJournalRows(plan),
	); ok {
		statements = append(statements, stmt)
	}

	if stmt, ok := newInsertStatement(
		"posting",
		[]string{"id", "journal_id", "status", "amount_cents"},
		[]string{"journal_id", "status", "amount_cents"},
		billingPostingRows(plan),
	); ok {
		statements = append(statements, stmt)
	}

	if stmt, ok := newInsertStatement(
		"bill",
		[]string{"id", "journal_id", "folio_id", "status", "total_cents", "paid_cents", "version"},
		[]string{"journal_id", "folio_id", "status", "total_cents", "paid_cents", "version"},
		billingBillRows(plan),
	); ok {
		statements = append(statements, stmt)
	}

	if stmt, ok := newInsertStatement(
		"payment",
		[]string{"id", "bill_id", "journal_id", "status", "amount_cents"},
		[]string{"bill_id", "journal_id", "status", "amount_cents"},
		billingPaymentRows(plan),
	); ok {
		statements = append(statements, stmt)
	}

	if stmt, ok := newInsertStatement(
		"foliobalance",
		[]string{"id", "customer_id", "folio_id", "balance_cents"},
		[]string{"customer_id", "folio_id", "balance_cents"},
		billingFolioBalanceRows(plan),
	); ok {
		statements = append(statements, stmt)
	}

	if stmt, ok := newInsertStatement(
		"feelog",
		[]string{"id", "journal_id", "fee_bill_id", "amount_cents"},
		[]string{"journal_id", "fee_bill_id", "amount_cents"},
		billingFeeLogRows(plan),
	); ok {
		statements = append(statements, stmt)
	}

	if stmt, ok := newInsertStatement(
		"statement",
		[]string{"id", "customer_id", "folio_id", "status", "balance_cents"},
		[]string{"customer_id", "folio_id", "status", "balance_cents"},
		billingStatementRows(plan),
	); ok {
		statements = append(statements, stmt)
	}

	if stmt, ok := newInsertStatement(
		"withdrawal",
		[]string{"id", "journal_id", "statement_id", "status", "amount_cents"},
		[]string{"journal_id", "statement_id", "status", "amount_cents"},
		billingWithdrawalRows(plan),
	); ok {
		statements = append(statements, stmt)
	}

	return statements
}

func billingCustomerRows(plan BillingSeedPlan) [][]any {
	rows := make([][]any, 0, len(plan.CustomerIDs))
	for _, customerID := range plan.CustomerIDs {
		rows = append(rows, []any{customerID, "active"})
	}
	return rows
}

func billingFolioRows(plan BillingSeedPlan) [][]any {
	rows := make([][]any, 0, len(plan.FolioIDs))
	for index, folioID := range plan.FolioIDs {
		rows = append(rows, []any{folioID, plan.CustomerIDs[index], nil})
	}
	return rows
}

func billingJournalRows(plan BillingSeedPlan) [][]any {
	if len(plan.CustomerIDs) == 0 {
		return nil
	}

	rows := make([][]any, 0, 1+len(plan.JournalPostingBillSlots)+len(plan.FKBackfillSlots)+len(plan.PaymentMixedReferenceSlots))
	rows = append(rows, []any{
		plan.ExistingJournalID,
		plan.ExistingCustomerID,
		plan.ExistingFolioID,
		nil,
		"seed-existing-journal",
		int64(500),
	})
	for _, slot := range plan.JournalPostingBillSlots {
		rows = append(rows, []any{slot.JournalID, slot.CustomerID, slot.FolioID, nil, "seed-journal-posting", int64(1500)})
	}
	for _, slot := range plan.FKBackfillSlots {
		rows = append(rows, []any{slot.JournalID, slot.CustomerID, slot.FolioID, nil, "seed-fk-backfill", int64(525)})
	}
	for _, slot := range plan.PaymentMixedReferenceSlots {
		rows = append(rows, []any{slot.JournalID, slot.CustomerID, slot.FolioID, nil, "seed-payment-mixed", int64(875)})
	}
	return rows
}

func billingPostingRows(plan BillingSeedPlan) [][]any {
	rows := make([][]any, 0, len(plan.JournalPostingBillSlots))
	for _, slot := range plan.JournalPostingBillSlots {
		rows = append(rows, []any{slot.PostingID, slot.JournalID, "posted", int64(1500)})
	}
	return rows
}

func billingBillRows(plan BillingSeedPlan) [][]any {
	if len(plan.CustomerIDs) == 0 {
		return nil
	}

	rows := make([][]any, 0, 1+len(plan.JournalPostingBillSlots)+len(plan.FKBackfillSlots))
	rows = append(rows, []any{
		plan.ExistingBillID,
		plan.ExistingJournalID,
		plan.ExistingFolioID,
		"open",
		int64(2000),
		int64(0),
		int64(0),
	})
	for _, slot := range plan.JournalPostingBillSlots {
		rows = append(rows, []any{slot.BillID, slot.JournalID, slot.FolioID, "open", int64(1500), int64(0), int64(0)})
	}
	for _, slot := range plan.FKBackfillSlots {
		rows = append(rows, []any{slot.BillID, slot.JournalID, slot.FolioID, "open", int64(225), int64(0), int64(0)})
	}
	return rows
}

func billingPaymentRows(plan BillingSeedPlan) [][]any {
	rows := make([][]any, 0, len(plan.PaymentMixedReferenceSlots)+len(plan.PaymentBillUpdateProbeSlots))
	for _, slot := range plan.PaymentMixedReferenceSlots {
		rows = append(rows, []any{slot.PaymentID, slot.BillID, slot.JournalID, "applied", int64(875)})
	}
	for _, slot := range plan.PaymentBillUpdateProbeSlots {
		rows = append(rows, []any{slot.PaymentID, slot.BillID, slot.JournalID, "probe", int64(100)})
	}
	return rows
}

func billingFolioBalanceRows(plan BillingSeedPlan) [][]any {
	rows := make([][]any, 0, len(plan.FolioBalanceUpdateSlots))
	for _, slot := range plan.FolioBalanceUpdateSlots {
		rows = append(rows, []any{slot.FolioBalanceID, slot.CustomerID, slot.FolioID, int64(0)})
	}
	return rows
}

func billingFeeLogRows(plan BillingSeedPlan) [][]any {
	rows := make([][]any, 0, len(plan.FKBackfillSlots))
	for _, slot := range plan.FKBackfillSlots {
		rows = append(rows, []any{slot.FeeLogID, slot.JournalID, slot.BillID, int64(225)})
	}
	return rows
}

func billingStatementRows(plan BillingSeedPlan) [][]any {
	rows := make([][]any, 0, len(plan.FKBackfillSlots)+len(plan.CascadePathSlots))
	for _, slot := range plan.FKBackfillSlots {
		rows = append(rows, []any{slot.StatementID, slot.CustomerID, slot.FolioID, "issued", int64(525)})
	}
	for _, slot := range plan.CascadePathSlots {
		rows = append(rows, []any{slot.StatementID, slot.CustomerID, slot.FolioID, "issued", int64(640)})
	}
	return rows
}

func billingWithdrawalRows(plan BillingSeedPlan) [][]any {
	rows := make([][]any, 0, len(plan.FKBackfillSlots)+len(plan.CascadePathSlots))
	for _, slot := range plan.FKBackfillSlots {
		rows = append(rows, []any{slot.WithdrawalID, slot.JournalID, slot.StatementID, "pending", int64(300)})
	}
	for _, slot := range plan.CascadePathSlots {
		rows = append(rows, []any{slot.WithdrawalID, slot.JournalID, slot.StatementID, "pending", int64(640)})
	}
	return rows
}

func buildJournalPostingBillSlots(
	customerIDs, folioIDs []int64,
	firstJournalID, firstPostingID, firstBillID int64,
) []JournalPostingBillSlot {
	slots := make([]JournalPostingBillSlot, 0, billingFixturePoolSize)
	for i := 0; i < billingFixturePoolSize; i++ {
		slots = append(slots, JournalPostingBillSlot{
			CustomerID: customerIDs[i%len(customerIDs)],
			FolioID:    folioIDs[i%len(folioIDs)],
			JournalID:  firstJournalID + int64(i),
			PostingID:  firstPostingID + int64(i),
			BillID:     firstBillID + int64(i),
		})
	}
	return slots
}

func buildFolioBalanceUpdateSlots(customerIDs, folioIDs []int64, firstBalanceID int64) []FolioBalanceUpdateSlot {
	slots := make([]FolioBalanceUpdateSlot, 0, billingFixturePoolSize)
	for i := 0; i < billingFixturePoolSize; i++ {
		slots = append(slots, FolioBalanceUpdateSlot{
			CustomerID:     customerIDs[i%len(customerIDs)],
			FolioID:        folioIDs[i%len(folioIDs)],
			FolioBalanceID: firstBalanceID + int64(i),
		})
	}
	return slots
}

func buildFKBackfillSlots(
	customerIDs, folioIDs []int64,
	firstJournalID, firstBillID, firstFeeLogID, firstStatementID, firstWithdrawalID int64,
) []FKBackfillSlot {
	slots := make([]FKBackfillSlot, 0, billingFixturePoolSize)
	for i := 0; i < billingFixturePoolSize; i++ {
		slots = append(slots, FKBackfillSlot{
			CustomerID:   customerIDs[i%len(customerIDs)],
			FolioID:      folioIDs[i%len(folioIDs)],
			JournalID:    firstJournalID + int64(i),
			BillID:       firstBillID + int64(i),
			FeeLogID:     firstFeeLogID + int64(i),
			StatementID:  firstStatementID + int64(i),
			WithdrawalID: firstWithdrawalID + int64(i),
		})
	}
	return slots
}

func buildPaymentMixedReferenceSlots(
	customerIDs, folioIDs []int64,
	billID, firstJournalID, firstPaymentID int64,
) []PaymentMixedReferenceSlot {
	slots := make([]PaymentMixedReferenceSlot, 0, billingFixturePoolSize)
	for i := 0; i < billingFixturePoolSize; i++ {
		slots = append(slots, PaymentMixedReferenceSlot{
			CustomerID: customerIDs[i%len(customerIDs)],
			FolioID:    folioIDs[i%len(folioIDs)],
			BillID:     billID,
			JournalID:  firstJournalID + int64(i),
			PaymentID:  firstPaymentID + int64(i),
		})
	}
	return slots
}

func buildCascadePathSlots(
	customerIDs, folioIDs []int64,
	journalID, firstStatementID, firstWithdrawalID int64,
) []CascadePathSlot {
	slots := make([]CascadePathSlot, 0, billingFixturePoolSize)
	for i := 0; i < billingFixturePoolSize; i++ {
		slots = append(slots, CascadePathSlot{
			CustomerID:   customerIDs[i%len(customerIDs)],
			FolioID:      folioIDs[i%len(folioIDs)],
			JournalID:    journalID,
			StatementID:  firstStatementID + int64(i),
			WithdrawalID: firstWithdrawalID + int64(i),
		})
	}
	return slots
}

func buildPaymentBillUpdateProbeSlots(
	customerIDs, folioIDs []int64,
	billID, journalID, firstPaymentID int64,
) []PaymentBillUpdateProbeSlot {
	slots := make([]PaymentBillUpdateProbeSlot, 0, billingFixturePoolSize)
	for i := 0; i < billingFixturePoolSize; i++ {
		slots = append(slots, PaymentBillUpdateProbeSlot{
			CustomerID: customerIDs[i%len(customerIDs)],
			FolioID:    folioIDs[i%len(folioIDs)],
			BillID:     billID,
			JournalID:  journalID,
			PaymentID:  firstPaymentID + int64(i),
		})
	}
	return slots
}

func buildStatementFolioParentUpdateProbeSlots(
	customerIDs, folioIDs []int64,
	firstStatementID int64,
) []StatementFolioParentUpdateProbeSlot {
	slots := make([]StatementFolioParentUpdateProbeSlot, 0, billingFixturePoolSize)
	for i := 0; i < billingFixturePoolSize; i++ {
		slots = append(slots, StatementFolioParentUpdateProbeSlot{
			CustomerID:  customerIDs[i%len(customerIDs)],
			FolioID:     folioIDs[i%len(folioIDs)],
			StatementID: firstStatementID + int64(i),
		})
	}
	return slots
}
