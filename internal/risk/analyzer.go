package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// AnalyzeOptions controls the behavior of the risk analyzer.
type AnalyzeOptions struct {
	RepoRoot     string
	Engine       string
	Offline      bool
	Force        bool
	PolicyPath   string
	Classifier   RiskClassifier
	Introspector SchemaIntrospector
	Cache        *RiskCache
}

// AnalysisReport holds aggregated evaluation results across multiple migrations.
type AnalysisReport struct {
	Branch         string          `json:"branch,omitempty"`
	Total          int             `json:"total"`
	OverallLevel   RiskLevel       `json:"overall_level"`
	OverallAction  PolicyAction    `json:"overall_action"`
	Blocked        bool            `json:"blocked"`
	RequiresConfirm bool           `json:"requires_confirm"`
	Migrations     []MigrationRisk `json:"migrations"`
	GeneratedAt    time.Time       `json:"generated_at"`
	PolicyVersion  int             `json:"policy_version"`
}

// RiskAnalyzer orchestrates DDL parsing, catalog introspection, caching, and classification.
type RiskAnalyzer struct {
	opts         AnalyzeOptions
	policy       *RiskPolicy
	parser       *DDLParser
	cache        *RiskCache
	classifier   RiskClassifier
	introspector SchemaIntrospector
}

// NewRiskAnalyzer initializes an analyzer with loaded policies and caches.
func NewRiskAnalyzer(opts AnalyzeOptions) (*RiskAnalyzer, error) {
	if opts.RepoRoot == "" {
		opts.RepoRoot = "."
	}
	if opts.Engine == "" {
		opts.Engine = "postgres"
	}

	policy, err := LoadPolicy(opts.RepoRoot, opts.PolicyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load policy: %w", err)
	}

	cache := opts.Cache
	if cache == nil {
		c, err := NewRiskCache(opts.RepoRoot)
		if err == nil {
			cache = c
		}
	}

	classifier := opts.Classifier
	if classifier == nil || opts.Offline {
		classifier = NewHeuristicClassifier()
	}

	introspector := opts.Introspector
	if introspector == nil {
		introspector = NewMockIntrospector()
	}

	return &RiskAnalyzer{
		opts:         opts,
		policy:       policy,
		parser:       NewDDLParser(opts.Engine),
		cache:        cache,
		classifier:   classifier,
		introspector: introspector,
	}, nil
}

// AnalyzeSQL assesses a single SQL payload against cache, introspector, classifier, and policy.
func (a *RiskAnalyzer) AnalyzeSQL(ctx context.Context, fileName, sqlContent string) (*MigrationRisk, error) {
	// 1. Check path exclusion
	if a.policy.IsPathExcluded(fileName) {
		return &MigrationRisk{
			File:        fileName,
			Level:       RiskLow,
			Reversible:  true,
			Destructive: false,
			Category:    CategoryDocsOrComments,
			Confidence:  1.0,
			Flags:       []string{"EXCLUDED_PATH"},
			AnalyzedAt:  time.Now(),
			Engine:      a.opts.Engine,
			Source:      "policy_exclusion",
		}, nil
	}

	// 2. Parse DDL operations
	ops, err := a.parser.ParseDiff(sqlContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DDL: %w", err)
	}

	// 3. Introspect all affected tables if available
	tablesMeta := make(map[string]*TableMetadata)
	var firstMeta *TableMetadata
	var schemaSignatures []string

	for _, op := range ops {
		if op.TargetTable != "" {
			tbl := op.TargetTable
			if _, exists := tablesMeta[tbl]; !exists && a.introspector != nil {
				if meta, err := a.introspector.IntrospectTable(ctx, tbl); err == nil && meta != nil {
					tablesMeta[tbl] = meta
					if firstMeta == nil {
						firstMeta = meta
					}
					schemaSignatures = append(schemaSignatures, FormatSchemaSignature(meta))
				}
			}
		}
	}

	schemaSignature := "no_schema"
	if len(schemaSignatures) > 0 {
		sort.Strings(schemaSignatures)
		schemaSignature = strings.Join(schemaSignatures, ";")
	}

	// 4. Check cache
	cacheKey := ComputeCacheKey(sqlContent, schemaSignature)
	if a.cache != nil {
		if cached, hit := a.cache.Get(cacheKey); hit {
			return cached, nil
		}
	}

	// 5. Check always_critical rule override
	isCritical, critPattern := a.policy.MatchesAlwaysCriticalForEngine(sqlContent, a.opts.Engine)

	// 6. Execute classification
	tableCtx := FormatPromptContext(firstMeta)
	if len(tablesMeta) > 1 {
		var contexts []string
		for _, meta := range tablesMeta {
			contexts = append(contexts, FormatPromptContext(meta))
		}
		tableCtx = strings.Join(contexts, "\n")
	}

	input := ClassificationInput{
		File:           fileName,
		SQL:            sqlContent,
		Engine:         a.opts.Engine,
		TableContext:   tableCtx,
		TableMetadata:  firstMeta,
		TablesMetadata: tablesMeta,
		Operations:     ops,
	}

	riskResult, err := a.classifier.Classify(ctx, input)
	if err != nil {
		// Fallback to heuristic if primary classifier failed
		if a.classifier.Name() != "heuristic" {
			if strings.EqualFold(a.policy.OnJevUnavailable, string(ActionBlock)) {
				return &MigrationRisk{
					File:        fileName,
					Level:       RiskCritical,
					Reversible:  false,
					Destructive: true,
					Category:    CategorySchemaChange,
					Confidence:  0.0,
					Flags:       []string{"JEV_UNAVAILABLE", "BLOCKED_BY_POLICY"},
					Details:     fmt.Sprintf("Primary classifier %q failed (%v) and policy on_jev_unavailable is set to block", a.classifier.Name(), err),
					AnalyzedAt:  time.Now(),
					Engine:      a.opts.Engine,
					Source:      a.classifier.Name(),
				}, nil
			}
			fallback := NewHeuristicClassifier()
			riskResult, err = fallback.Classify(ctx, input)
			if riskResult != nil {
				riskResult.Flags = append(riskResult.Flags, "JEV_FALLBACK_HEURISTIC")
			}
		}
		if err != nil {
			return nil, fmt.Errorf("classification failed: %w", err)
		}
	}
	for _, op := range ops {
		if op.Type == "DROP_COLUMN" {
			meta := tablesMeta[op.TargetTable]
			if meta == nil || len(meta.Columns) == 0 {
				riskResult.Level = maxLevel(riskResult.Level, RiskHigh)
				hasFKFlag := false
				for _, f := range riskResult.Flags {
					if strings.Contains(f, "FK impact unknown") {
						hasFKFlag = true
						break
					}
				}
				if !hasFKFlag {
					riskResult.Flags = append(riskResult.Flags, "FK impact unknown: catalog metadata unavailable")
				}
			}
		}
	}
	for _, op := range ops {
		if op.Type == "GENERIC_DDL" && !reDataManipulation.MatchString(op.RawSQL) {
			riskResult.Level = maxLevel(riskResult.Level, RiskHigh)
			riskResult.Flags = append(riskResult.Flags, "UNCLASSIFIED SQL statement")
			if riskResult.Details == "" {
				riskResult.Details = "This SQL statement is outside the supported migration parser; review it manually before applying."
			}
		}
	}

	if isCritical {
		riskResult.Level = RiskCritical
		riskResult.Destructive = true
		riskResult.Flags = append(riskResult.Flags, fmt.Sprintf("always_critical rule: %s", critPattern))
	}

	// 7. Save to cache
	if a.cache != nil {
		_ = a.cache.Set(cacheKey, riskResult)
	}

	return riskResult, nil
}

// EvaluateReport computes the aggregated decision and overall action for a set of risks.
func (a *RiskAnalyzer) EvaluateReport(risks []MigrationRisk, branch string) *AnalysisReport {
	overallLevel := RiskLow
	overallAction := ActionAllow
	blocked := false
	requiresConfirm := false

	for _, r := range risks {
		decision := a.policy.Evaluate(&r)
		if decision.EffectiveLevel.Order() > overallLevel.Order() {
			overallLevel = decision.EffectiveLevel
			overallAction = decision.Action
		}
		if decision.Blocked {
			blocked = true
		}
		if decision.RequiresConfirmation {
			requiresConfirm = true
		}
	}

	return &AnalysisReport{
		Branch:          branch,
		Total:           len(risks),
		OverallLevel:    overallLevel,
		OverallAction:   overallAction,
		Blocked:         blocked,
		RequiresConfirm: requiresConfirm,
		Migrations:      risks,
		GeneratedAt:     time.Now(),
		PolicyVersion:   a.policy.Version,
	}
}

// SaveReport writes the analysis report JSON to disk if configured.
func (a *RiskAnalyzer) SaveReport(report *AnalysisReport) error {
	if a.policy.SaveReport == "" {
		return nil
	}

	reportPath := a.policy.SaveReport
	if !filepath.IsAbs(reportPath) {
		reportPath = filepath.Join(a.opts.RepoRoot, reportPath)
	}

	if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(reportPath, data, 0644)
}

// FormatPrettyTerminal renders an ANSI/plain text report for terminal UX.
func FormatPrettyTerminal(report *AnalysisReport) string {
	var sb strings.Builder

	sb.WriteString("┌─────────────────────────────────────────────────┐\n")
	sb.WriteString("│  BranchBase Risk Gate — Migration Analysis       │\n")
	sb.WriteString("└─────────────────────────────────────────────────┘\n\n")

	if report.Branch != "" {
		sb.WriteString(fmt.Sprintf("  Branch:     %s\n", report.Branch))
	}
	sb.WriteString(fmt.Sprintf("  Migrations: %d analyzed\n\n", report.Total))

	for i, m := range report.Migrations {
		badge := "🟢 LOW"
		switch m.Level {
		case RiskMedium:
			badge = "🟡 MEDIUM"
		case RiskHigh:
			badge = "🟠 HIGH"
		case RiskCritical:
			badge = "🔴 CRITICAL"
		}

		sb.WriteString(fmt.Sprintf("  ▸ [%d] %s\n", i+1, m.File))
		sb.WriteString(fmt.Sprintf("    Risk:       %s  (confidence: %.0f%%)\n", badge, m.Confidence*100))
		sb.WriteString(fmt.Sprintf("    Category:   %s\n", m.Category))
		if len(m.Flags) > 0 {
			sb.WriteString(fmt.Sprintf("    Flags:      %s\n", strings.Join(m.Flags, " · ")))
		}
		if m.ExclusiveLock {
			sb.WriteString("    Locks:      ⚠️  Requires ACCESS EXCLUSIVE table lock\n")
		}
		if m.Reversible {
			sb.WriteString("    Reversal:   ✅ Reversible\n")
		} else {
			sb.WriteString("    Reversal:   ❌ Not reversible without backup\n")
		}
		if m.Details != "" {
			sb.WriteString(fmt.Sprintf("    Details:    %s\n", m.Details))
		}
		if m.Suggestion != "" {
			sb.WriteString(fmt.Sprintf("    Suggestion: 💡 %s\n", m.Suggestion))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("  ──────────────────────────────────────────────\n")
	overallBadge := string(report.OverallLevel)
	sb.WriteString(fmt.Sprintf("  Overall Risk: %s — Action: %s\n", overallBadge, strings.ToUpper(string(report.OverallAction))))

	return sb.String()
}

// FormatGitHubAnnotations returns GitHub Action workflow commands for CI annotations.
func FormatGitHubAnnotations(report *AnalysisReport) string {
	var sb strings.Builder
	for _, m := range report.Migrations {
		if m.Level == RiskCritical || m.Level == RiskHigh {
			sb.WriteString(fmt.Sprintf("::error file=%s::[%s Risk] %s - %s\n",
				m.File, m.Level, strings.Join(m.Flags, ", "), m.Details))
		} else if m.Level == RiskMedium {
			sb.WriteString(fmt.Sprintf("::warning file=%s::[%s Risk] %s\n",
				m.File, m.Level, strings.Join(m.Flags, ", ")))
		}
	}
	return sb.String()
}
