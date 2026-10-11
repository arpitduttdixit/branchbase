package risk

import (
	"context"
	"testing"
)

func TestRiskAnalyzer(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mockIntrospect := NewMockIntrospector()
	mockIntrospect.Tables["users"] = &TableMetadata{
		TableName: "users",
		Columns:   []string{"id", "email", "name"},
		InboundFKs: []ForeignKeyRef{
			{ConstraintName: "fk_orders_user", FromTable: "orders", FromColumn: "user_email", ToTable: "users", ToColumn: "email"},
		},
	}

	cache, err := NewRiskCache(tmpDir)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}

	analyzer, err := NewRiskAnalyzer(AnalyzeOptions{
		RepoRoot:     tmpDir,
		Engine:       "postgres",
		Offline:      true,
		Classifier:   NewHeuristicClassifier(),
		Introspector: mockIntrospect,
		Cache:        cache,
	})
	if err != nil {
		t.Fatalf("failed to create analyzer: %v", err)
	}

	// 1. Analyze safe migration
	safeSQL := "CREATE TABLE products (id SERIAL PRIMARY KEY, title TEXT);"
	riskSafe, err := analyzer.AnalyzeSQL(ctx, "001_create_products.sql", safeSQL)
	if err != nil {
		t.Fatalf("failed to analyze safe SQL: %v", err)
	}
	if riskSafe.Level != RiskLow {
		t.Errorf("expected LOW for CREATE TABLE, got %v", riskSafe.Level)
	}

	// 2. Analyze destructive FK-referencing migration
	critSQL := "ALTER TABLE users DROP COLUMN email;"
	riskCrit, err := analyzer.AnalyzeSQL(ctx, "002_drop_email.sql", critSQL)
	if err != nil {
		t.Fatalf("failed to analyze critical SQL: %v", err)
	}
	if riskCrit.Level != RiskCritical {
		t.Errorf("expected CRITICAL for dropping FK referenced column, got %v", riskCrit.Level)
	}
	if !riskCrit.TouchesFKs {
		t.Errorf("expected TouchesFKs to be true")
	}

	// 3. Test caching behavior: second call should retrieve from cache
	riskCached, err := analyzer.AnalyzeSQL(ctx, "002_drop_email.sql", critSQL)
	if err != nil {
		t.Fatalf("failed to analyze cached SQL: %v", err)
	}
	if riskCached.Source != "cache" {
		t.Errorf("expected source 'cache', got %s", riskCached.Source)
	}

	// 4. Test aggregated report evaluation
	report := analyzer.EvaluateReport([]MigrationRisk{*riskSafe, *riskCrit}, "feature/drop-email")
	if report.OverallLevel != RiskCritical {
		t.Errorf("expected overall level CRITICAL, got %s", report.OverallLevel)
	}
	if report.OverallAction != ActionBlock || !report.Blocked {
		t.Errorf("expected overall action BLOCK, got %s (blocked=%v)", report.OverallAction, report.Blocked)
	}

	// 5. Test pretty formatting
	pretty := FormatPrettyTerminal(report)
	if len(pretty) == 0 {
		t.Errorf("expected non-empty pretty terminal report")
	}

	// 6. Test github annotations formatting
	annotations := FormatGitHubAnnotations(report)
	if len(annotations) == 0 {
		t.Errorf("expected non-empty github annotations")
	}
}

func TestRiskAnalyzerMultiTableIntrospection(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mockIntrospect := NewMockIntrospector()
	// Table 1 has no inbound FKs
	mockIntrospect.Tables["accounts"] = &TableMetadata{
		TableName: "accounts",
		Columns:   []string{"id", "balance"},
	}
	// Table 2 has inbound FKs
	mockIntrospect.Tables["users"] = &TableMetadata{
		TableName: "users",
		Columns:   []string{"id", "email"},
		InboundFKs: []ForeignKeyRef{
			{ConstraintName: "fk_orders_user", FromTable: "orders", FromColumn: "user_email", ToTable: "users", ToColumn: "email"},
		},
	}

	analyzer, err := NewRiskAnalyzer(AnalyzeOptions{
		RepoRoot:     tmpDir,
		Engine:       "postgres",
		Offline:      true,
		Classifier:   NewHeuristicClassifier(),
		Introspector: mockIntrospect,
	})
	if err != nil {
		t.Fatalf("failed to create analyzer: %v", err)
	}

	// Migration touches accounts first, then users
	multiTableSQL := `
		ALTER TABLE accounts ADD COLUMN currency VARCHAR(3);
		ALTER TABLE users DROP COLUMN email;
	`
	riskRes, err := analyzer.AnalyzeSQL(ctx, "003_multi.sql", multiTableSQL)
	if err != nil {
		t.Fatalf("AnalyzeSQL failed: %v", err)
	}

	// Should introspect both tables and detect that users.email has inbound FKs!
	if riskRes.Level != RiskCritical {
		t.Errorf("expected CRITICAL risk due to second table having FK, got %v", riskRes.Level)
	}
	if !riskRes.TouchesFKs {
		t.Errorf("expected TouchesFKs to be true for second table")
	}
}

type failingClassifier struct{}

func (f *failingClassifier) Name() string { return "failing-jev" }
func (f *failingClassifier) Classify(ctx context.Context, input ClassificationInput) (*MigrationRisk, error) {
	return nil, context.DeadlineExceeded
}

func TestRiskAnalyzerOnJevUnavailableBlock(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	analyzer, err := NewRiskAnalyzer(AnalyzeOptions{
		RepoRoot:   tmpDir,
		Engine:     "postgres",
		Classifier: &failingClassifier{},
	})
	if err != nil {
		t.Fatalf("failed to create analyzer: %v", err)
	}

	// Set policy to block on JEV unavailable
	analyzer.policy.OnJevUnavailable = "block"

	res, err := analyzer.AnalyzeSQL(ctx, "004_test.sql", "CREATE TABLE t (id INT);")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Level != RiskCritical {
		t.Errorf("expected RiskCritical when on_jev_unavailable is block, got %v", res.Level)
	}
	report := analyzer.EvaluateReport([]MigrationRisk{*res}, "test-branch")
	if !report.Blocked {
		t.Errorf("expected report to be blocked")
	}
}
