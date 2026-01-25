package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"app-go-htmx/internal/portfolio"
	"app-go-htmx/internal/trades"

	"github.com/xuri/excelize/v2"
)

type PortfolioViewModel struct {
	StartingCapital float64
	TradePnLs       string

	FieldErrors map[string]string
	FormErrors  []string

	CanSubmit bool

	portfolio.Result
}

type ImportViewModel struct {
	CSVURL    string
	Portfolio string
	Strategy  string
	StartDate string
	EndDate   string
	Timezone  string

	Result *trades.ImportSummary

	FormErrors []string
}

type BatchImportRow struct {
	CSVURL    string
	Strategy  string
	Portfolio string
	Timezone  string
	StartDate string
	EndDate   string
}

type BatchImportRowResult struct {
	Row        int
	CSVURL     string
	Strategy   string
	Portfolio  string
	Status     string
	Imported   int
	Duplicates int
	Skipped    int
	Error      string
	RowErrors  []trades.RowError
}

type BatchImportViewModel struct {
	Rows        []BatchImportRow
	Results     []BatchImportRowResult
	FileSummary string
}

type MappingRowView struct {
	StrategyKey string
	Enabled     bool
	Weight      float64
	RatioMode   bool
	RatioUnit   float64
	RatioAmount float64
	Notes       string
}

type PortfolioListItem struct {
	Name          string
	BaseName      string
	StrategyCount int
	DateRange     string
}

type PortfolioMergeViewModel struct {
	PortfolioName     string
	PortfolioSelected string
	PortfolioItems    []PortfolioListItem
	Rows              []MappingRowView
	AutoAppendEnabled bool
	FinalNamePreview  string
	SuccessMessage    string
	ErrorMessage      string
	ChartEngine       string
	ChartLinkJS       string
	ChartLinkTV       string
	HasChartData      bool

	AnalysisRangeQuick  string
	AnalysisAutoRefresh bool
	AnalysisN           string
	AnalysisUnit        string
	AnalysisStartDate   string
	AnalysisEndDate     string
	AnalysisBalance     string
	AnalysisPortfolio   string
	AnalysisResult      *AnalysisResult
}

type AnalysisState struct {
	RangeQuick  string
	AutoRefresh bool
	N           string
	Unit        string
	StartDate   string
	EndDate     string
	Balance     string
	Portfolio   string
}

type AnalysisResult struct {
	Portfolio       string
	TradeCount      int
	StartingCapital float64
	EndingCapital   float64
	TotalNetPnL     float64
	RangeLabel      string
	StartDate       string
	EndDate         string
	NetGainPct      float64
	CAGR            float64
	MaxDrawdownPct  float64
	ProfitFactor    float64
	UlcerIndex      float64
	EquityR2        float64
	TimeUnderWater  float64
	Expectancy      float64
	Trades          []AnalysisTradeRow
	ChartData       ChartSeries
}

type ChartSeries struct {
	Labels []string  `json:"labels"`
	PnL    []float64 `json:"pnl"`
	Equity []float64 `json:"equity"`
	DdPct  []float64 `json:"ddPct"`
	DdAmt  []float64 `json:"ddAmt"`
}

type AnalysisTradeRow struct {
	Strategy      string
	Direction     string
	EntryTime     string
	EntryTimeSort time.Time
	EntryPrice    float64
	ExitTime      string
	ExitTimeSort  time.Time
	ExitPrice     float64
	NetPnL        float64
	WeightedPnL   float64
}

type ImportInput struct {
	CSVURL    string
	Strategy  string
	Portfolio string
	Timezone  string
	StartDate string
	EndDate   string
}

type ImportValidation struct {
	Meta      trades.StrategyMetadata
	Loc       *time.Location
	StartDate *time.Time
	EndDate   *time.Time
	Errors    []string
}

const defaultTimezone = "America/Chicago"

func main() {
	tmpl := template.Must(
		template.New("root").Funcs(template.FuncMap{
			"toJSON": toJSON,
		}).ParseFiles(
			"web/templates/portfolio.html",
			"web/templates/imports.html",
			"web/templates/imports_batch.html",
			"web/templates/portfolio_merge.html",
		),
	)

	mux := http.NewServeMux()

	// =================================================
	// Full form submit (recompute portfolio)
	// =================================================
	mux.HandleFunc("/portfolio", func(w http.ResponseWriter, r *http.Request) {
		// Defaults
		startingCapital := 10_000.0
		tradePnLs := "1000,-2000,500"

		if r.Method == http.MethodPost {
			_ = r.ParseForm()

			if v := r.FormValue("starting_capital"); v != "" {
				if parsed, err := strconv.ParseFloat(v, 64); err == nil {
					startingCapital = parsed
				}
			}

			if v := r.FormValue("trade_pnls"); v != "" {
				tradePnLs = v
			}
		}

		// -----------------
		// Validation
		// -----------------
		fieldErrors := make(map[string]string)
		var formErrors []string

		if startingCapital < 0 {
			fieldErrors["starting_capital"] = "Starting capital must be zero or greater."
		}

		tradesList, parseErrors := parseTrades(tradePnLs)
		if len(parseErrors) > 0 {
			fieldErrors["trade_pnls"] = strings.Join(parseErrors, ", ")
		}

		canSubmit := len(fieldErrors) == 0 && len(formErrors) == 0

		var result portfolio.Result
		if canSubmit {
			result = portfolio.Compute(startingCapital, tradesList)
		}

		vm := PortfolioViewModel{
			StartingCapital: startingCapital,
			TradePnLs:       tradePnLs,
			FieldErrors:     fieldErrors,
			FormErrors:      formErrors,
			CanSubmit:       canSubmit,
			Result:          result,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		if r.Header.Get("HX-Request") == "true" {
			_ = tmpl.ExecuteTemplate(w, "portfolio-content", vm)
			return
		}

		_ = tmpl.ExecuteTemplate(w, "portfolio.html", vm)
	})

	// =================================================
	// Per-field validation (HTMX debounced)
	// =================================================

	mux.HandleFunc("/portfolio/validate/starting-capital", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		value := r.FormValue("starting_capital")

		var errMsg string

		if value == "" {
			errMsg = "Starting capital is required."
		} else if v, err := strconv.ParseFloat(value, 64); err != nil {
			errMsg = "Starting capital must be a number."
		} else if v < 0 {
			errMsg = "Starting capital must be zero or greater."
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(errMsg))
	})

	mux.HandleFunc("/portfolio/validate/trade-pnls", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		value := r.FormValue("trade_pnls")

		_, errs := parseTrades(value)

		var errMsg string
		if len(errs) > 0 {
			errMsg = strings.Join(errs, ", ")
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(errMsg))
	})

	// =================================================
	// Trade import UI + handler
	// =================================================
	mux.HandleFunc("/imports/trades", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		if r.Method == http.MethodGet {
			vm := ImportViewModel{Timezone: defaultTimezone}
			_ = tmpl.ExecuteTemplate(w, "imports.html", vm)
			return
		}

		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		const maxUploadBytes = 25 << 20
		if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Invalid multipart form."))
			return
		}

		vm := ImportViewModel{
			CSVURL:    strings.TrimSpace(r.FormValue("csv_url")),
			Portfolio: strings.TrimSpace(r.FormValue("portfolio_name")),
			Strategy:  strings.TrimSpace(r.FormValue("strategy_name")),
			StartDate: strings.TrimSpace(r.FormValue("start_date")),
			EndDate:   strings.TrimSpace(r.FormValue("end_date")),
			Timezone:  strings.TrimSpace(r.FormValue("timezone")),
		}

		input := ImportInput{
			CSVURL:    vm.CSVURL,
			Strategy:  vm.Strategy,
			Portfolio: vm.Portfolio,
			Timezone:  vm.Timezone,
			StartDate: vm.StartDate,
			EndDate:   vm.EndDate,
		}

		files := r.MultipartForm.File["upload_file"]
		hasFile := len(files) > 0
		if !hasFile && vm.CSVURL == "" {
			vm.FormErrors = []string{"Please provide a CSV URL or upload a file."}
			_ = tmpl.ExecuteTemplate(w, "import-result", vm)
			return
		}
		if hasFile && len(files) > 1 {
			vm.FormErrors = []string{"Please upload a single file."}
			_ = tmpl.ExecuteTemplate(w, "import-result", vm)
			return
		}

		requireURL := !hasFile
		validation := validateImportInputWithSource(input, requireURL)
		vm.FormErrors = validation.Errors
		if len(validation.Errors) > 0 {
			vm.Timezone = validation.Loc.String()
			_ = tmpl.ExecuteTemplate(w, "import-result", vm)
			return
		}

		var summary trades.ImportSummary
		var rowErrors []trades.RowError
		var filtered int
		var err error
		if hasFile {
			parsedTrades, uploadErrors, parseErr := parseUploadFile(files[0], validation.Loc)
			if parseErr != nil {
				vm.FormErrors = append(vm.FormErrors, parseErr.Error())
				_ = tmpl.ExecuteTemplate(w, "import-result", vm)
				return
			}
			summary, rowErrors, filtered, err = executeTradeImportFromTrades(r.Context(), input, validation, parsedTrades, uploadErrors)
		} else {
			summary, rowErrors, filtered, err = executeTradeImport(r.Context(), input, validation)
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			vm.FormErrors = append(vm.FormErrors, err.Error())
			_ = tmpl.ExecuteTemplate(w, "import-result", vm)
			return
		}

		summary.Errors = rowErrors
		summary.Skipped = len(rowErrors) + filtered
		vm.Result = &summary
		vm.Timezone = validation.Loc.String()

		_ = tmpl.ExecuteTemplate(w, "import-result", vm)
	})

	// =================================================
	// Batch trade import UI + handler
	// =================================================
	mux.HandleFunc("/import/batch", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		if r.Method == http.MethodGet {
			vm := BatchImportViewModel{
				Rows: defaultBatchRows(3),
			}
			_ = tmpl.ExecuteTemplate(w, "imports_batch.html", vm)
			return
		}

		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		const maxUploadBytes = 25 << 20
		if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("Invalid multipart form."))
			return
		}
		rows := parseBatchRows(r.Form)
		if len(rows) == 0 {
			rows = defaultBatchRows(3)
		}

		files := r.MultipartForm.File["upload_files"]
		if len(files) > 0 {
			results := make([]BatchImportRowResult, 0, len(files))
			failed := 0
			for i, fileHeader := range files {
				row := BatchImportRow{}
				if i < len(rows) {
					row = rows[i]
				}
				input := ImportInput{
					CSVURL:    "",
					Strategy:  strings.TrimSpace(row.Strategy),
					Portfolio: strings.TrimSpace(row.Portfolio),
					Timezone:  strings.TrimSpace(row.Timezone),
					StartDate: strings.TrimSpace(row.StartDate),
					EndDate:   strings.TrimSpace(row.EndDate),
				}

				validation := validateImportInputWithSource(input, false)
				result := BatchImportRowResult{
					Row:       i + 1,
					CSVURL:    fileHeader.Filename,
					Strategy:  input.Strategy,
					Portfolio: input.Portfolio,
				}

				if len(validation.Errors) > 0 {
					result.Status = "Error"
					result.Error = strings.Join(validation.Errors, " ")
					results = append(results, result)
					failed++
					continue
				}

				parsedTrades, uploadErrors, parseErr := parseUploadFile(fileHeader, validation.Loc)
				if parseErr != nil {
					result.Status = "Error"
					result.Error = parseErr.Error()
					results = append(results, result)
					failed++
					continue
				}

				summary, rowErrors, filtered, err := executeTradeImportFromTrades(r.Context(), input, validation, parsedTrades, uploadErrors)
				if err != nil && !errors.Is(err, context.Canceled) {
					result.Status = "Error"
					result.Error = err.Error()
					results = append(results, result)
					failed++
					continue
				}

				summary.Skipped = len(rowErrors) + filtered
				result.Status = "OK"
				result.Imported = summary.Imported
				result.Duplicates = summary.Duplicates
				result.Skipped = summary.Skipped
				result.RowErrors = rowErrors
				if len(rowErrors) > 0 {
					result.Error = "Row errors: " + strconv.Itoa(len(rowErrors))
				}
				results = append(results, result)
			}

			vm := BatchImportViewModel{
				Rows:        rows,
				Results:     results,
				FileSummary: fmt.Sprintf("Imported %d files, %d failed.", len(files)-failed, failed),
			}
			_ = tmpl.ExecuteTemplate(w, "imports_batch.html", vm)
			return
		}

		if allBatchRowsEmpty(rows) {
			vm := BatchImportViewModel{
				Rows:        rows,
				Results:     []BatchImportRowResult{},
				FileSummary: "Please provide CSV URLs or upload files.",
			}
			_ = tmpl.ExecuteTemplate(w, "imports_batch.html", vm)
			return
		}

		results := make([]BatchImportRowResult, 0, len(rows))
		for i, row := range rows {
			input := ImportInput{
				CSVURL:    strings.TrimSpace(row.CSVURL),
				Strategy:  strings.TrimSpace(row.Strategy),
				Portfolio: strings.TrimSpace(row.Portfolio),
				Timezone:  strings.TrimSpace(row.Timezone),
				StartDate: strings.TrimSpace(row.StartDate),
				EndDate:   strings.TrimSpace(row.EndDate),
			}

			validation := validateImportInputWithSource(input, true)
			result := BatchImportRowResult{
				Row:       i + 1,
				CSVURL:    input.CSVURL,
				Strategy:  input.Strategy,
				Portfolio: input.Portfolio,
			}

			if len(validation.Errors) > 0 {
				result.Status = "Error"
				result.Error = strings.Join(validation.Errors, " ")
				results = append(results, result)
				continue
			}

			summary, rowErrors, filtered, err := executeTradeImport(r.Context(), input, validation)
			if err != nil && !errors.Is(err, context.Canceled) {
				result.Status = "Error"
				result.Error = err.Error()
				results = append(results, result)
				continue
			}

			summary.Skipped = len(rowErrors) + filtered
			result.Status = "OK"
			result.Imported = summary.Imported
			result.Duplicates = summary.Duplicates
			result.Skipped = summary.Skipped
			result.RowErrors = rowErrors
			if len(rowErrors) > 0 {
				result.Error = "Row errors: " + strconv.Itoa(len(rowErrors))
			}
			results = append(results, result)
		}

		vm := BatchImportViewModel{
			Rows:    rows,
			Results: results,
		}

		_ = tmpl.ExecuteTemplate(w, "imports_batch.html", vm)
	})

	// =================================================
	// Portfolio merge UI + handler
	// =================================================
	mux.HandleFunc("/portfolios/merge", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		if r.Method == http.MethodGet {
			portfolioName := strings.TrimSpace(r.URL.Query().Get("portfolio"))
			chartEngine := normalizeChartEngine(r.URL.Query().Get("chart"))
			analysis := loadAnalysisState(portfolioName)
			if portfolioName != "" {
				if loaded, err := trades.LoadPortfolioByName(portfolioName); err == nil && loaded != nil {
					analysis = applyPortfolioFilters(analysis, loaded)
				}
				analysis.Portfolio = portfolioName
			}
			result := (*AnalysisResult)(nil)
			if analysis.AutoRefresh || r.URL.Query().Has("chart") {
				if computed, err := computeAnalysisResult(analysis); err == nil {
					result = computed
				}
			}
			vm, err := buildPortfolioMergeViewModel(portfolioName, analysis, result, "", chartEngine, r.URL.Query())
			if err != nil {
				vm.ErrorMessage = err.Error()
			}
			_ = tmpl.ExecuteTemplate(w, "portfolio_merge.html", vm)
			return
		}

		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		_ = r.ParseForm()
		portfolioName := strings.TrimSpace(r.FormValue("portfolio_name"))
		chartEngine := normalizeChartEngine(r.FormValue("chart"))
		inputs := parseMappingRows(r.Form)

		analysis := loadAnalysisState("")
		result := (*AnalysisResult)(nil)
		vm, err := buildPortfolioMergeViewModel(portfolioName, analysis, result, "", chartEngine, buildBaseQuery(portfolioName))
		if err != nil {
			vm.ErrorMessage = err.Error()
			_ = tmpl.ExecuteTemplate(w, "portfolio_merge.html", vm)
			return
		}

		vm.Rows = mappingInputsToView(inputs)
		_ = tmpl.ExecuteTemplate(w, "portfolio_merge.html", vm)
	})

	// =================================================
	// Portfolio analysis handler
	// =================================================
	mux.HandleFunc("/portfolios/merge/analysis", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		_ = r.ParseForm()
		analysis := parseAnalysisState(r.Form)
		portfolioName := strings.TrimSpace(analysis.Portfolio)
		chartEngine := normalizeChartEngine(r.FormValue("chart"))
		result, analysisErr := computeAnalysisResult(analysis)
		if analysisErr != nil {
			vm, err := buildPortfolioMergeViewModel(portfolioName, analysis, result, "", chartEngine, buildBaseQuery(portfolioName))
			if err != nil {
				vm.ErrorMessage = err.Error()
			} else {
				vm.ErrorMessage = analysisErr.Error()
			}
			_ = tmpl.ExecuteTemplate(w, "portfolio_merge.html", vm)
			return
		}

		if saveErr := saveAnalysisState(analysis); saveErr != nil {
			analysisError := saveErr.Error()
			vm, err := buildPortfolioMergeViewModel(portfolioName, analysis, result, "", chartEngine, buildBaseQuery(portfolioName))
			if err != nil {
				vm.ErrorMessage = err.Error()
			} else {
				vm.ErrorMessage = analysisError
			}
			_ = tmpl.ExecuteTemplate(w, "portfolio_merge.html", vm)
			return
		}

		vm, err := buildPortfolioMergeViewModel(portfolioName, analysis, result, "Dashboard updated.", chartEngine, buildBaseQuery(portfolioName))
		if err != nil {
			vm.ErrorMessage = err.Error()
			if len(inputs) > 0 {
				vm.Rows = mappingInputsToView(inputs)
			}
			_ = tmpl.ExecuteTemplate(w, "portfolio_merge.html", vm)
			return
		}
		if len(inputs) > 0 {
			vm.Rows = mappingInputsToView(inputs)
		}
		_ = tmpl.ExecuteTemplate(w, "portfolio_merge.html", vm)
	})

	// =================================================
	// Portfolio API (save/load/delete/list)
	// =================================================
	mux.HandleFunc("/api/portfolios", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		items, err := trades.ListSavedPortfolios()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		list := make([]PortfolioListItem, 0, len(items))
		for _, p := range items {
			list = append(list, PortfolioListItem{
				Name:          p.Name,
				BaseName:      p.BaseName,
				StrategyCount: p.Meta.StrategyCount,
				DateRange:     formatPortfolioDateRange(p.Filters.From, p.Filters.To),
			})
		}

		writeJSON(w, http.StatusOK, list)
	})

	mux.HandleFunc("/api/portfolios/load", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		if name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		portfolio, err := trades.LoadPortfolioByName(name)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if portfolio == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, portfolio)
	})

	mux.HandleFunc("/api/portfolios/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
			return
		}
		if err := trades.DeletePortfolioByName(payload.Name); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/portfolios/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			BaseName   string `json:"base_name"`
			AutoAppend bool   `json:"auto_append"`
			Overwrite  bool   `json:"overwrite"`
			Timestamp  string `json:"timestamp"`
			Analysis   struct {
				RangeQuick string `json:"range_quick"`
				N          string `json:"n"`
				Unit       string `json:"unit"`
				StartDate  string `json:"start_date"`
				EndDate    string `json:"end_date"`
				Balance    string `json:"balance"`
			} `json:"analysis"`
			Mappings []trades.PortfolioMapping `json:"mappings"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
			return
		}

		baseName := strings.TrimSpace(payload.BaseName)
		if baseName == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "portfolio name is required"})
			return
		}

		analysisState := AnalysisState{
			RangeQuick: strings.TrimSpace(payload.Analysis.RangeQuick),
			N:          strings.TrimSpace(payload.Analysis.N),
			Unit:       strings.TrimSpace(payload.Analysis.Unit),
			StartDate:  strings.TrimSpace(payload.Analysis.StartDate),
			EndDate:    strings.TrimSpace(payload.Analysis.EndDate),
			Balance:    strings.TrimSpace(payload.Analysis.Balance),
		}

		enabledCount := 0
		for _, m := range payload.Mappings {
			if m.Enabled {
				enabledCount++
			}
		}

		start, end := resolveNameDates(analysisState)
		finalName := buildPortfolioFinalName(baseName, enabledCount, start, end, payload.AutoAppend, payload.Timestamp)
		if strings.TrimSpace(finalName) == "" {
			finalName = baseName
		}

		balance := 0.0
		if strings.TrimSpace(analysisState.Balance) != "" {
			if v, err := strconv.ParseFloat(strings.TrimSpace(analysisState.Balance), 64); err == nil {
				balance = v
			}
		}

		filters := trades.PortfolioFilters{
			From:      formatDatePtr(start),
			To:        formatDatePtr(end),
			Balance:   balance,
			RangeMode: buildRangeMode(analysisState),
		}

		portfolio := trades.Portfolio{
			Name:     finalName,
			BaseName: baseName,
			Filters:  filters,
			Mappings: payload.Mappings,
			Meta: trades.PortfolioMeta{
				StrategyCount: enabledCount,
			},
		}

		saved, exists, err := trades.SavePortfolio(portfolio, payload.Overwrite)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if exists && !payload.Overwrite {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "exists", "name": finalName})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"name":   saved.Name,
		})
	})

	// =================================================
	// HTTP server + graceful shutdown
	// =================================================
	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go func() {
		log.Println("listening on http://localhost:8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = server.Shutdown(shutdownCtx)
	log.Println("server stopped gracefully")
}

// =================================================
// Helpers
// =================================================
func parseTrades(input string) ([]portfolio.Trade, []string) {
	parts := strings.Split(input, ",")

	var (
		tradesList []portfolio.Trade
		errorsList []string
	)

	now := time.Now()

	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			errorsList = append(errorsList, "Trade PnLs cannot be empty.")
			continue
		}

		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			errorsList = append(errorsList, "Invalid trade PnL: "+p)
			continue
		}

		tradesList = append(tradesList, portfolio.Trade{
			ExitTime: now.AddDate(0, 0, i),
			NetPnL:   v,
		})
	}

	return tradesList, errorsList
}

type chartLinks struct {
	ChartJS string
	TV      string
}

func normalizeChartEngine(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "chartjs", "tv":
		return value
	default:
		return "chartjs"
	}
}

func buildBaseQuery(portfolioName string) url.Values {
	q := url.Values{}
	if strings.TrimSpace(portfolioName) != "" {
		q.Set("portfolio", strings.TrimSpace(portfolioName))
	}
	return q
}

func cloneQuery(values url.Values) url.Values {
	q := url.Values{}
	for key, vals := range values {
		for _, v := range vals {
			q.Add(key, v)
		}
	}
	return q
}

func buildChartLinks(base url.Values) chartLinks {
	build := func(engine string) string {
		q := cloneQuery(base)
		q.Set("chart", engine)
		encoded := q.Encode()
		if encoded == "" {
			return "/portfolios/merge"
		}
		return "/portfolios/merge?" + encoded
	}

	return chartLinks{
		ChartJS: build("chartjs"),
		TV:      build("tv"),
	}
}

func isSafeCSVURL(raw string) bool {
	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "https" {
		return false
	}
	if u.User != nil {
		return false
	}
	if u.Host == "" {
		return false
	}

	host := strings.ToLower(u.Hostname())
	if net.ParseIP(host) != nil {
		return false
	}

	return allowlistedHost(host)
}

func sameHostAllowlist(u *url.URL) bool {
	if u == nil {
		return false
	}
	if u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return allowlistedHost(host)
}

func allowlistedHost(host string) bool {
	if host == "docs.google.com" {
		return true
	}
	if host == "googleusercontent.com" || strings.HasSuffix(host, ".googleusercontent.com") {
		return true
	}
	return false
}

func validateImportInput(input ImportInput) ImportValidation {
	return validateImportInputWithSource(input, true)
}

func validateImportInputWithSource(input ImportInput, requireURL bool) ImportValidation {
	input.CSVURL = strings.TrimSpace(input.CSVURL)
	input.Strategy = strings.TrimSpace(input.Strategy)
	input.Portfolio = strings.TrimSpace(input.Portfolio)
	input.Timezone = strings.TrimSpace(input.Timezone)
	input.StartDate = strings.TrimSpace(input.StartDate)
	input.EndDate = strings.TrimSpace(input.EndDate)

	if input.Timezone == "" {
		input.Timezone = defaultTimezone
	}

	validation := ImportValidation{}
	loc, err := time.LoadLocation(input.Timezone)
	if err != nil {
		validation.Errors = append(validation.Errors, "Timezone must be a valid IANA timezone (e.g., America/Chicago).")
		loc = time.UTC
	}
	validation.Loc = loc

	if requireURL {
		if input.CSVURL == "" {
			validation.Errors = append(validation.Errors, "CSV URL is required.")
		} else if !isSafeCSVURL(input.CSVURL) {
			validation.Errors = append(validation.Errors, "CSV URL must be a valid HTTPS Google Sheets public URL.")
		}
	}

	if input.Strategy == "" {
		validation.Errors = append(validation.Errors, "Strategy name is required (format: NAME-SYMBOL-TIMEFRAME).")
	} else {
		meta, err := trades.ParseStrategyMetadata(input.Strategy)
		if err != nil {
			validation.Errors = append(validation.Errors, err.Error())
		} else {
			validation.Meta = meta
		}
	}

	if input.StartDate != "" {
		if parsed, err := time.ParseInLocation("2006-01-02", input.StartDate, loc); err == nil {
			utc := parsed.UTC()
			validation.StartDate = &utc
		} else {
			validation.Errors = append(validation.Errors, "Start date must be YYYY-MM-DD.")
		}
	}

	if input.EndDate != "" {
		if parsed, err := time.ParseInLocation("2006-01-02", input.EndDate, loc); err == nil {
			end := parsed.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
			utc := end.UTC()
			validation.EndDate = &utc
		} else {
			validation.Errors = append(validation.Errors, "End date must be YYYY-MM-DD.")
		}
	}

	return validation
}

func executeTradeImport(ctx context.Context, input ImportInput, validation ImportValidation) (trades.ImportSummary, []trades.RowError, int, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if !sameHostAllowlist(req.URL) {
				return errors.New("redirect to disallowed host")
			}
			return nil
		},
	}

	resp, err := client.Get(input.CSVURL)
	if err != nil {
		return trades.ImportSummary{}, nil, 0, fmt.Errorf("failed to fetch CSV: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return trades.ImportSummary{}, nil, 0, fmt.Errorf("csv fetch returned status %s", resp.Status)
	}

	const maxCSVBytes = 5 << 20 // 5 MB
	limited := io.LimitReader(resp.Body, maxCSVBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return trades.ImportSummary{}, nil, 0, fmt.Errorf("failed to read CSV body: %w", err)
	}
	if len(body) > maxCSVBytes {
		return trades.ImportSummary{}, nil, 0, fmt.Errorf("csv too large (limit 5 MB)")
	}
	if len(body) == 0 {
		return trades.ImportSummary{}, nil, 0, fmt.Errorf("csv response was empty")
	}

	parsedTrades, rowErrors, err := trades.ParseTVCSV(strings.NewReader(string(body)), validation.Loc)
	if err != nil {
		return trades.ImportSummary{}, rowErrors, 0, err
	}

	return executeTradeImportFromTrades(ctx, input, validation, parsedTrades, rowErrors)
}

func executeTradeImportFromTrades(ctx context.Context, input ImportInput, validation ImportValidation, parsedTrades []trades.Trade, rowErrors []trades.RowError) (trades.ImportSummary, []trades.RowError, int, error) {
	filtered := 0
	finalTrades := make([]trades.Trade, 0, len(parsedTrades))
	for _, t := range parsedTrades {
		t.Strategy = validation.Meta.Strategy
		t.Symbol = validation.Meta.Symbol
		t.Timeframe = validation.Meta.Timeframe
		t.Portfolio = input.Portfolio

		if validation.StartDate != nil && t.ExitDatetime.Before(*validation.StartDate) {
			filtered++
			continue
		}
		if validation.EndDate != nil && t.ExitDatetime.After(*validation.EndDate) {
			filtered++
			continue
		}

		finalTrades = append(finalTrades, t)
	}

	summary, err := trades.InsertTradesDedup(ctx, trades.DefaultStore(), finalTrades)
	if err != nil {
		return trades.ImportSummary{}, rowErrors, filtered, err
	}
	if input.Portfolio != "" {
		_ = trades.EnsureMappingForStrategy(input.Portfolio, validation.Meta.Strategy)
	}

	return summary, rowErrors, filtered, nil
}

func parseBatchRows(form url.Values) []BatchImportRow {
	rowsByIndex := map[int]*BatchImportRow{}
	indices := make([]int, 0)
	seen := make(map[int]struct{})

	for key, values := range form {
		idx, field, ok := parseBatchKey(key)
		if !ok {
			continue
		}
		if len(values) == 0 {
			continue
		}

		row := rowsByIndex[idx]
		if row == nil {
			row = &BatchImportRow{Timezone: defaultTimezone}
			rowsByIndex[idx] = row
			if _, exists := seen[idx]; !exists {
				indices = append(indices, idx)
				seen[idx] = struct{}{}
			}
		}

		value := values[0]
		switch field {
		case "csv_url":
			row.CSVURL = value
		case "strategy_name":
			row.Strategy = value
		case "portfolio_name":
			row.Portfolio = value
		case "timezone":
			row.Timezone = value
		case "start_date":
			row.StartDate = value
		case "end_date":
			row.EndDate = value
		}
	}

	sort.Ints(indices)
	rows := make([]BatchImportRow, 0, len(indices))
	for _, idx := range indices {
		if row := rowsByIndex[idx]; row != nil {
			if strings.TrimSpace(row.Timezone) == "" {
				row.Timezone = defaultTimezone
			}
			rows = append(rows, *row)
		}
	}

	return rows
}

func parseBatchKey(key string) (int, string, bool) {
	if !strings.HasPrefix(key, "rows[") {
		return 0, "", false
	}
	closeIdx := strings.Index(key, "]")
	if closeIdx == -1 {
		return 0, "", false
	}
	idxStr := key[len("rows["):closeIdx]
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		return 0, "", false
	}
	if len(key) <= closeIdx+2 || key[closeIdx+1] != '.' {
		return 0, "", false
	}
	field := key[closeIdx+2:]
	if field == "" {
		return 0, "", false
	}
	return idx, field, true
}

func defaultBatchRows(count int) []BatchImportRow {
	if count < 1 {
		count = 1
	}
	rows := make([]BatchImportRow, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, BatchImportRow{Timezone: defaultTimezone})
	}
	return rows
}

func buildPortfolioMergeViewModel(portfolioName string, analysis AnalysisState, result *AnalysisResult, successMessage string, chartEngine string, baseQuery url.Values) (PortfolioMergeViewModel, error) {
	portfolioName = strings.TrimSpace(portfolioName)
	if portfolioName == "" && strings.TrimSpace(analysis.Portfolio) != "" {
		portfolioName = strings.TrimSpace(analysis.Portfolio)
	}

	savedPortfolios, err := trades.ListSavedPortfolios()
	if err != nil {
		return PortfolioMergeViewModel{}, err
	}

	items := make([]PortfolioListItem, 0, len(savedPortfolios))
	for _, p := range savedPortfolios {
		items = append(items, PortfolioListItem{
			Name:          p.Name,
			BaseName:      p.BaseName,
			StrategyCount: p.Meta.StrategyCount,
			DateRange:     formatPortfolioDateRange(p.Filters.From, p.Filters.To),
		})
	}

	strategies, err := trades.ListStrategies(trades.DefaultStore())
	if err != nil {
		return PortfolioMergeViewModel{}, err
	}

	var saved *trades.Portfolio
	if portfolioName != "" {
		if loaded, err := trades.LoadPortfolioByName(portfolioName); err != nil {
			return PortfolioMergeViewModel{}, err
		} else if loaded != nil {
			saved = loaded
		}
	}

	rows := buildMappingRows(strategies, saved)

	chartEngine = normalizeChartEngine(chartEngine)
	chartLinks := buildChartLinks(baseQuery)
	chartData := ChartSeries{}
	if result != nil {
		chartData = result.ChartData
	}
	hasChartData := len(chartData.Labels) > 0

	baseName := portfolioName
	if saved != nil && strings.TrimSpace(saved.BaseName) != "" {
		baseName = saved.BaseName
	}

	start, end := resolveNameDates(analysis)
	preview := buildPortfolioFinalName(baseName, countEnabled(rows), start, end, true, "")

	return PortfolioMergeViewModel{
		PortfolioName:       baseName,
		PortfolioSelected:   portfolioName,
		PortfolioItems:      items,
		Rows:                rows,
		AutoAppendEnabled:   true,
		FinalNamePreview:    preview,
		SuccessMessage:      successMessage,
		AnalysisRangeQuick:  analysis.RangeQuick,
		AnalysisAutoRefresh: analysis.AutoRefresh,
		AnalysisN:           analysis.N,
		AnalysisUnit:        analysis.Unit,
		AnalysisStartDate:   analysis.StartDate,
		AnalysisEndDate:     analysis.EndDate,
		AnalysisBalance:     analysis.Balance,
		AnalysisPortfolio:   analysis.Portfolio,
		AnalysisResult:      result,
		ChartEngine:         chartEngine,
		ChartLinkJS:         chartLinks.ChartJS,
		ChartLinkTV:         chartLinks.TV,
		HasChartData:        hasChartData,
	}, nil
}

func defaultAnalysisState(portfolioName string) AnalysisState {
	portfolio := strings.TrimSpace(portfolioName)
	return AnalysisState{
		RangeQuick:  "",
		AutoRefresh: false,
		N:           "",
		Unit:        "Years",
		StartDate:   "",
		EndDate:     "",
		Balance:     "",
		Portfolio:   portfolio,
	}
}

func loadAnalysisState(portfolioName string) AnalysisState {
	state := defaultAnalysisState(portfolioName)
	stored, err := trades.LoadAnalysisSettings(portfolioName)
	if err != nil || stored == nil {
		return state
	}

	state.RangeQuick = stored.RangeQuick
	state.AutoRefresh = stored.AutoRefresh
	state.N = stored.N
	state.Unit = stored.Unit
	state.StartDate = stored.StartDate
	state.EndDate = stored.EndDate
	state.Balance = stored.Balance
	state.Portfolio = stored.Portfolio

	return state
}

func saveAnalysisState(state AnalysisState) error {
	if strings.TrimSpace(state.Portfolio) == "" {
		return nil
	}

	return trades.SaveAnalysisSettings(trades.AnalysisSettings{
		Portfolio:   strings.TrimSpace(state.Portfolio),
		RangeQuick:  state.RangeQuick,
		AutoRefresh: state.AutoRefresh,
		N:           state.N,
		Unit:        state.Unit,
		StartDate:   state.StartDate,
		EndDate:     state.EndDate,
		Balance:     state.Balance,
	})
}

func parseAnalysisState(form url.Values) AnalysisState {
	return AnalysisState{
		RangeQuick:  strings.TrimSpace(form.Get("analysis_range_quick")),
		AutoRefresh: form.Get("analysis_auto_refresh") == "on",
		N:           strings.TrimSpace(form.Get("analysis_n")),
		Unit:        strings.TrimSpace(form.Get("analysis_unit")),
		StartDate:   strings.TrimSpace(form.Get("analysis_start_date")),
		EndDate:     strings.TrimSpace(form.Get("analysis_end_date")),
		Balance:     strings.TrimSpace(form.Get("analysis_balance")),
		Portfolio:   strings.TrimSpace(form.Get("analysis_portfolio")),
	}
}

func computeAnalysisResult(state AnalysisState) (*AnalysisResult, error) {
	portfolioName := strings.TrimSpace(state.Portfolio)
	if portfolioName == "" {
		return nil, fmt.Errorf("Portfolio is required.")
	}

	start, end, label := resolveAnalysisRange(state)
	tradesList, err := trades.DefaultStore().Load()
	if err != nil {
		return nil, err
	}

	startingCapital := 10000.0
	if strings.TrimSpace(state.Balance) != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(state.Balance), 64); err == nil {
			startingCapital = v
		}
	}

	mappings, err := trades.ListMappings(portfolioName)
	if err != nil {
		return nil, err
	}
	mapByKey := make(map[string]trades.StrategyPortfolioMapping)
	for _, m := range mappings {
		key := strings.ToLower(strings.TrimSpace(m.StrategyKey))
		if key != "" {
			mapByKey[key] = m
		}
	}

	filtered := make([]portfolio.Trade, 0)
	tradeRows := make([]AnalysisTradeRow, 0)
	type chartPoint struct {
		Time  time.Time
		Label string
		PnL   float64
	}
	chartPoints := make([]chartPoint, 0)
	minExit := (*time.Time)(nil)
	maxExit := (*time.Time)(nil)
	for _, t := range tradesList {
		if !strings.EqualFold(strings.TrimSpace(t.Portfolio), portfolioName) {
			continue
		}
		if start != nil && t.ExitDatetime.Before(*start) {
			continue
		}
		if end != nil && t.ExitDatetime.After(*end) {
			continue
		}

		if minExit == nil || t.ExitDatetime.Before(*minExit) {
			min := t.ExitDatetime
			minExit = &min
		}
		if maxExit == nil || t.ExitDatetime.After(*maxExit) {
			max := t.ExitDatetime
			maxExit = &max
		}

		multiplier, ok := mappingMultiplier(mapByKey, t.Strategy, startingCapital)
		if !ok {
			continue
		}

		weightedPnL := t.NetPnL * multiplier
		filtered = append(filtered, portfolio.Trade{
			ExitTime: t.ExitDatetime,
			NetPnL:   weightedPnL,
		})
		labelTime := t.ExitDatetime
		if labelTime.IsZero() {
			labelTime = t.EntryDatetime
		}
		chartPoints = append(chartPoints, chartPoint{
			Time:  labelTime,
			Label: formatTradeDate(labelTime),
			PnL:   weightedPnL,
		})
		tradeRows = append(tradeRows, AnalysisTradeRow{
			Strategy:      t.Strategy,
			Direction:     t.Direction,
			EntryTime:     formatTradeDate(t.EntryDatetime),
			EntryTimeSort: t.EntryDatetime,
			EntryPrice:    t.EntryPrice,
			ExitTime:      formatTradeDate(t.ExitDatetime),
			ExitTimeSort:  t.ExitDatetime,
			ExitPrice:     t.ExitPrice,
			NetPnL:        t.NetPnL,
			WeightedPnL:   weightedPnL,
		})
	}

	result := portfolio.Compute(startingCapital, filtered)
	if start == nil {
		start = minExit
	}
	if end == nil {
		end = maxExit
	}

	netGainPct := 0.0
	if startingCapital != 0 {
		netGainPct = (result.TotalNetPnL / startingCapital) * 100
	}

	cagr := 0.0
	if startingCapital > 0 && result.EndingCapital > 0 && start != nil && end != nil {
		years := end.Sub(*start).Hours() / 24 / 365.25
		if years > 0 {
			cagr = math.Pow(result.EndingCapital/startingCapital, 1/years) - 1
			cagr *= 100
		}
	}

	profits, losses := sumProfitLoss(filtered)
	profitFactor := 0.0
	if losses > 0 {
		profitFactor = profits / losses
	}

	maxDdPct, ulcer, r2, tuw := equityStats(result.EquityCurve)

	expectancy := 0.0
	if len(filtered) > 0 {
		expectancy = result.TotalNetPnL / float64(len(filtered))
	}

	sort.Slice(tradeRows, func(i, j int) bool {
		return tradeRows[i].EntryTimeSort.Before(tradeRows[j].EntryTimeSort)
	})

	sort.Slice(chartPoints, func(i, j int) bool {
		return chartPoints[i].Time.Before(chartPoints[j].Time)
	})

	chartData := ChartSeries{
		Labels: make([]string, 0, len(chartPoints)),
		PnL:    make([]float64, 0, len(chartPoints)),
		Equity: make([]float64, 0, len(chartPoints)),
		DdPct:  make([]float64, 0, len(chartPoints)),
		DdAmt:  make([]float64, 0, len(chartPoints)),
	}

	equity := 0.0
	peak := 0.0
	for _, point := range chartPoints {
		equity += point.PnL
		if equity > peak {
			peak = equity
		}
		ddAmt := equity - peak
		ddPct := 0.0
		if peak != 0 {
			ddPct = (ddAmt / peak) * 100
		}

		chartData.Labels = append(chartData.Labels, point.Label)
		chartData.PnL = append(chartData.PnL, point.PnL)
		chartData.Equity = append(chartData.Equity, equity)
		chartData.DdPct = append(chartData.DdPct, ddPct)
		chartData.DdAmt = append(chartData.DdAmt, ddAmt)
	}

	return &AnalysisResult{
		Portfolio:       portfolioName,
		TradeCount:      len(filtered),
		StartingCapital: startingCapital,
		EndingCapital:   result.EndingCapital,
		TotalNetPnL:     result.TotalNetPnL,
		RangeLabel:      label,
		StartDate:       formatDatePtr(start),
		EndDate:         formatDatePtr(end),
		NetGainPct:      netGainPct,
		CAGR:            cagr,
		MaxDrawdownPct:  maxDdPct,
		ProfitFactor:    profitFactor,
		UlcerIndex:      ulcer,
		EquityR2:        r2,
		TimeUnderWater:  tuw,
		Expectancy:      expectancy,
		Trades:          tradeRows,
		ChartData:       chartData,
	}, nil
}

func resolveAnalysisRange(state AnalysisState) (*time.Time, *time.Time, string) {
	parseDate := func(value string) *time.Time {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil
		}
		if t, err := time.Parse("2006-01-02", value); err == nil {
			utc := t.UTC()
			return &utc
		}
		return nil
	}

	start := parseDate(state.StartDate)
	end := parseDate(state.EndDate)
	label := "Custom"
	if start != nil || end != nil {
		return start, end, label
	}

	now := time.Now().UTC()
	if state.RangeQuick != "" {
		label = state.RangeQuick
		switch strings.ToUpper(state.RangeQuick) {
		case "YTD":
			start = ptrTime(time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC))
			end = ptrTime(now)
		case "1Y":
			start = ptrTime(now.AddDate(-1, 0, 0))
			end = ptrTime(now)
		case "2Y":
			start = ptrTime(now.AddDate(-2, 0, 0))
			end = ptrTime(now)
		case "3Y":
			start = ptrTime(now.AddDate(-3, 0, 0))
			end = ptrTime(now)
		case "4Y":
			start = ptrTime(now.AddDate(-4, 0, 0))
			end = ptrTime(now)
		case "5Y":
			start = ptrTime(now.AddDate(-5, 0, 0))
			end = ptrTime(now)
		case "6M":
			start = ptrTime(now.AddDate(0, -6, 0))
			end = ptrTime(now)
		case "18M":
			start = ptrTime(now.AddDate(0, -18, 0))
			end = ptrTime(now)
		case "ALL":
			start = nil
			end = nil
			label = "ALL"
		default:
			label = "Custom"
		}
		return start, end, label
	}

	if strings.TrimSpace(state.N) != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(state.N)); err == nil && n > 0 {
			label = state.N + " " + state.Unit
			switch state.Unit {
			case "Days":
				start = ptrTime(now.AddDate(0, 0, -n))
			case "Months":
				start = ptrTime(now.AddDate(0, -n, 0))
			case "Years":
				start = ptrTime(now.AddDate(-n, 0, 0))
			default:
				start = nil
			}
			end = ptrTime(now)
			return start, end, label
		}
	}

	return nil, nil, "ALL"
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

func formatDatePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

func formatTradeDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04")
}

func toJSON(v any) template.JS {
	if v == nil {
		return template.JS("null")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return template.JS("null")
	}
	return template.JS(b)
}

func mappingMultiplier(mapByKey map[string]trades.StrategyPortfolioMapping, strategy string, startingCapital float64) (float64, bool) {
	key := strings.ToLower(strings.TrimSpace(strategy))
	if key == "" {
		return 0, false
	}

	mapping, ok := mapByKey[key]
	if !ok {
		return 1.0, true
	}
	if !mapping.Enabled {
		return 0, false
	}

	weight := mapping.Weight
	if weight == 0 {
		weight = 1.0
	}

	if !mapping.RatioMode {
		return weight, true
	}

	ratioAmount := mapping.RatioAmount
	if ratioAmount <= 0 {
		return weight, true
	}
	ratioUnit := mapping.RatioUnit
	if ratioUnit == 0 {
		ratioUnit = 1.0
	}

	// Scale by capital per ratio amount: (capital / ratioAmount) * ratioUnit
	ratioMultiplier := (startingCapital / ratioAmount) * ratioUnit
	return weight * ratioMultiplier, true
}

func portfolioMultiplier(mapByKey map[string]trades.PortfolioMapping, strategy string, startingCapital float64) (float64, bool) {
	key := strings.ToLower(strings.TrimSpace(strategy))
	if key == "" {
		return 0, false
	}

	mapping, ok := mapByKey[key]
	if !ok {
		return 1.0, true
	}
	if !mapping.Enabled {
		return 0, false
	}

	weight := mapping.Weight
	if weight == 0 {
		weight = 1.0
	}

	if !mapping.RatioMode {
		return weight, true
	}

	ratioAmount := mapping.RatioAmount
	if ratioAmount <= 0 {
		return weight, true
	}
	ratioUnit := mapping.RatioUnit
	if ratioUnit == 0 {
		ratioUnit = 1.0
	}

	ratioMultiplier := (startingCapital / ratioAmount) * ratioUnit
	return weight * ratioMultiplier, true
}

func buildMappingRows(strategies []string, saved *trades.Portfolio) []MappingRowView {
	mapByKey := make(map[string]trades.PortfolioMapping)
	if saved != nil {
		for _, m := range saved.Mappings {
			key := strings.ToLower(strings.TrimSpace(m.Strategy))
			if key != "" {
				mapByKey[key] = m
			}
		}
	}

	rows := make([]MappingRowView, 0, len(strategies))
	seen := make(map[string]struct{})
	for _, strategy := range strategies {
		key := strings.TrimSpace(strategy)
		if key == "" {
			continue
		}
		seen[strings.ToLower(key)] = struct{}{}

		if existing, ok := mapByKey[strings.ToLower(key)]; ok {
			rows = append(rows, MappingRowView{
				StrategyKey: existing.Strategy,
				Enabled:     existing.Enabled,
				Weight:      existing.Weight,
				RatioMode:   existing.RatioMode,
				RatioUnit:   existing.RatioUnit,
				RatioAmount: existing.RatioAmount,
				Notes:       existing.Notes,
			})
			continue
		}

		rows = append(rows, MappingRowView{
			StrategyKey: key,
			Enabled:     true,
			Weight:      1.0,
			RatioMode:   false,
			RatioUnit:   1.0,
			RatioAmount: 10000,
			Notes:       "Auto-added",
		})
	}

	if saved != nil {
		extra := make([]string, 0)
		for _, m := range saved.Mappings {
			key := strings.ToLower(strings.TrimSpace(m.Strategy))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; !ok {
				extra = append(extra, m.Strategy)
			}
		}
		sort.Strings(extra)
		for _, key := range extra {
			m := mapByKey[strings.ToLower(strings.TrimSpace(key))]
			rows = append(rows, MappingRowView{
				StrategyKey: m.Strategy,
				Enabled:     m.Enabled,
				Weight:      m.Weight,
				RatioMode:   m.RatioMode,
				RatioUnit:   m.RatioUnit,
				RatioAmount: m.RatioAmount,
				Notes:       m.Notes,
			})
		}
	}

	return rows
}

func buildPortfolioMappingMap(strategies []string, saved *trades.Portfolio) map[string]trades.PortfolioMapping {
	mapByKey := make(map[string]trades.PortfolioMapping)
	if saved != nil {
		for _, m := range saved.Mappings {
			key := strings.ToLower(strings.TrimSpace(m.Strategy))
			if key != "" {
				mapByKey[key] = m
			}
		}
	}

	if saved == nil {
		for _, strategy := range strategies {
			key := strings.ToLower(strings.TrimSpace(strategy))
			if key == "" {
				continue
			}
			mapByKey[key] = trades.PortfolioMapping{
				Strategy:    strings.TrimSpace(strategy),
				Enabled:     true,
				Weight:      1.0,
				RatioMode:   false,
				RatioUnit:   1.0,
				RatioAmount: 10000,
				Notes:       "Auto-added",
			}
		}
	}

	return mapByKey
}

func countEnabled(rows []MappingRowView) int {
	count := 0
	for _, row := range rows {
		if row.Enabled {
			count++
		}
	}
	return count
}

func resolveNameDates(state AnalysisState) (*time.Time, *time.Time) {
	start, end, _ := resolveAnalysisRange(state)
	if start != nil || end != nil {
		return start, end
	}

	tradesList, err := trades.DefaultStore().Load()
	if err != nil {
		return start, end
	}
	for _, t := range tradesList {
		if t.ExitDatetime.IsZero() {
			continue
		}
		if start == nil || t.ExitDatetime.Before(*start) {
			value := t.ExitDatetime.UTC()
			start = &value
		}
		if end == nil || t.ExitDatetime.After(*end) {
			value := t.ExitDatetime.UTC()
			end = &value
		}
	}

	return start, end
}

func buildPortfolioFinalName(baseName string, enabledCount int, from, to *time.Time, autoAppend bool, timestamp string) string {
	base := strings.TrimSpace(baseName)
	if base == "" {
		return ""
	}
	if !autoAppend {
		return base
	}

	start := from
	end := to
	if start == nil || end == nil {
		now := time.Now().UTC()
		if start == nil {
			start = &now
		}
		if end == nil {
			end = &now
		}
	}

	startStr := formatNameDate(*start)
	endStr := formatNameDate(*end)

	stamp := strings.TrimSpace(timestamp)
	if stamp == "" {
		stamp = formatNameDateTime(time.Now().Local())
	} else if parsed, ok := parseNameDateTime(stamp); ok {
		stamp = formatNameDateTime(parsed)
	} else {
		stamp = formatNameDateTime(time.Now().Local())
	}

	return fmt.Sprintf("%s_%dSystems_%s-%s_%s", base, enabledCount, startStr, endStr, stamp)
}

func formatNameDate(t time.Time) string {
	return t.Format("01-02-2006")
}

func formatNameDateTime(t time.Time) string {
	return t.Format("01-02-2006-15-04")
}

func parseNameDateTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.ParseInLocation("01-02-2006-15-04", value, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func formatPortfolioDateRange(from, to string) string {
	fromFmt := formatNameDateString(from)
	toFmt := formatNameDateString(to)
	if fromFmt == "" && toFmt == "" {
		return "ALL"
	}
	if fromFmt == "" {
		return "- " + toFmt
	}
	if toFmt == "" {
		return fromFmt + " -"
	}
	return fromFmt + " - " + toFmt
}

func formatNameDateString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return formatNameDate(t)
	}
	return ""
}

func buildPortfolioLabel(state AnalysisState) string {
	name := strings.TrimSpace(state.Portfolio)
	if name != "" {
		return name
	}
	return "All Strategies"
}

func applyPortfolioFilters(state AnalysisState, portfolio *trades.Portfolio) AnalysisState {
	if portfolio == nil {
		return state
	}

	state.Portfolio = portfolio.Name
	if portfolio.Filters.Balance > 0 {
		state.Balance = strconv.FormatFloat(portfolio.Filters.Balance, 'f', -1, 64)
	}

	mode := strings.TrimSpace(portfolio.Filters.RangeMode)
	if strings.HasPrefix(strings.ToUpper(mode), "QUICK:") {
		state.RangeQuick = strings.TrimPrefix(mode, "QUICK:")
		state.StartDate = strings.TrimSpace(portfolio.Filters.From)
		state.EndDate = strings.TrimSpace(portfolio.Filters.To)
		return state
	}

	state.RangeQuick = ""
	state.StartDate = strings.TrimSpace(portfolio.Filters.From)
	state.EndDate = strings.TrimSpace(portfolio.Filters.To)
	return state
}

func buildRangeMode(state AnalysisState) string {
	if strings.TrimSpace(state.StartDate) != "" || strings.TrimSpace(state.EndDate) != "" {
		return "CUSTOM"
	}
	if strings.TrimSpace(state.RangeQuick) != "" {
		return "QUICK:" + strings.TrimSpace(state.RangeQuick)
	}
	if strings.TrimSpace(state.N) != "" {
		unit := strings.TrimSpace(state.Unit)
		if unit == "" {
			unit = "Years"
		}
		return "ROLLING:" + unit
	}
	return "ALL"
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func sumProfitLoss(tradesList []portfolio.Trade) (float64, float64) {
	profits := 0.0
	losses := 0.0
	for _, t := range tradesList {
		if t.NetPnL >= 0 {
			profits += t.NetPnL
		} else {
			losses += math.Abs(t.NetPnL)
		}
	}
	return profits, losses
}

func equityStats(points []portfolio.EquityPoint) (maxDdPct float64, ulcer float64, r2 float64, tuw float64) {
	if len(points) == 0 {
		return 0, 0, 0, 0
	}

	equity := make([]float64, 0, len(points))
	for _, p := range points {
		equity = append(equity, p.Equity)
	}

	peak := equity[0]
	ddPcts := make([]float64, 0, len(equity))
	below := 0
	for _, v := range equity {
		if v >= peak {
			peak = v
		} else {
			below++
		}
		ddPct := 0.0
		if peak != 0 {
			ddPct = (v - peak) / peak
		}
		ddPcts = append(ddPcts, ddPct)
		if ddPct < maxDdPct {
			maxDdPct = ddPct
		}
	}

	ulcer = ulcerIndex(ddPcts)
	r2 = r2OfSeries(equity)
	if len(equity) > 0 {
		tuw = float64(below) / float64(len(equity))
	}

	maxDdPct *= 100
	tuw *= 100
	return maxDdPct, ulcer, r2, tuw
}

func ulcerIndex(dds []float64) float64 {
	sumSq := 0.0
	count := 0
	for _, d := range dds {
		if !isFinite(d) {
			continue
		}
		sumSq += d * d
		count++
	}
	if count == 0 {
		return 0
	}
	return math.Sqrt(sumSq/float64(count)) * 100
}

func r2OfSeries(ys []float64) float64 {
	n := len(ys)
	if n < 2 {
		return 0
	}

	sumX := 0.0
	sumY := 0.0
	sumXY := 0.0
	sumXX := 0.0
	sumYY := 0.0

	for i, y := range ys {
		if !isFinite(y) {
			continue
		}
		x := float64(i)
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
		sumYY += y * y
	}

	nf := float64(n)
	num := (nf*sumXY - sumX*sumY)
	den := math.Sqrt((nf*sumXX - sumX*sumX) * (nf*sumYY - sumY*sumY))
	if den == 0 {
		return 0
	}
	return math.Pow(num/den, 2)
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func parseMappingRows(form url.Values) []trades.MappingInput {
	rowsByIndex := map[int]*trades.MappingInput{}
	indices := make([]int, 0)
	seen := make(map[int]struct{})

	for key, values := range form {
		idx, field, ok := parseBatchKey(key)
		if !ok {
			continue
		}
		if len(values) == 0 {
			continue
		}

		row := rowsByIndex[idx]
		if row == nil {
			row = &trades.MappingInput{
				Enabled:     false,
				Weight:      1.0,
				RatioMode:   false,
				RatioUnit:   1.0,
				RatioAmount: 10000,
			}
			rowsByIndex[idx] = row
			if _, exists := seen[idx]; !exists {
				indices = append(indices, idx)
				seen[idx] = struct{}{}
			}
		}

		value := values[0]
		switch field {
		case "strategy_key":
			row.StrategyKey = strings.TrimSpace(value)
		case "enabled":
			row.Enabled = value == "on" || value == "true" || value == "1"
		case "weight":
			if v, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
				row.Weight = v
			}
		case "ratio_mode":
			row.RatioMode = value == "on" || value == "true" || value == "1"
		case "ratio_unit":
			if v, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
				row.RatioUnit = v
			}
		case "ratio_amount":
			if v, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
				row.RatioAmount = v
			}
		case "notes":
			row.Notes = value
		}
	}

	sort.Ints(indices)
	rows := make([]trades.MappingInput, 0, len(indices))
	for _, idx := range indices {
		if row := rowsByIndex[idx]; row != nil {
			rows = append(rows, *row)
		}
	}

	return rows
}

func mappingInputsToView(inputs []trades.MappingInput) []MappingRowView {
	rows := make([]MappingRowView, 0, len(inputs))
	for _, input := range inputs {
		rows = append(rows, MappingRowView{
			StrategyKey: input.StrategyKey,
			Enabled:     input.Enabled,
			Weight:      input.Weight,
			RatioMode:   input.RatioMode,
			RatioUnit:   input.RatioUnit,
			RatioAmount: input.RatioAmount,
			Notes:       input.Notes,
		})
	}
	return rows
}

func parseUploadFile(fileHeader *multipart.FileHeader, loc *time.Location) ([]trades.Trade, []trades.RowError, error) {
	if fileHeader == nil {
		return nil, nil, fmt.Errorf("missing upload file")
	}

	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	if ext == "" {
		return nil, nil, fmt.Errorf("unsupported file type")
	}

	file, err := fileHeader.Open()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open upload: %w", err)
	}
	defer file.Close()

	switch ext {
	case ".csv":
		return trades.ParseTVCSV(file, loc)
	case ".xls", ".xlsx":
		rows, err := readExcelSheetRows(file)
		if err != nil {
			return nil, nil, err
		}
		csvText, err := rowsToCSV(rows)
		if err != nil {
			return nil, nil, err
		}
		return trades.ParseTVCSV(strings.NewReader(csvText), loc)
	default:
		return nil, nil, fmt.Errorf("unsupported file type")
	}
}

func readExcelSheetRows(r io.Reader) ([][]string, error) {
	workbook, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("excel parse failed: %w", err)
	}

	sheets := workbook.GetSheetList()
	if len(sheets) < 4 {
		return nil, fmt.Errorf("Excel file must contain at least 4 sheets; trades are expected on sheet 4.")
	}

	name := sheets[3]
	rows, err := workbook.GetRows(name)
	if err != nil {
		return nil, fmt.Errorf("excel read failed: %w", err)
	}

	return rows, nil
}

func rowsToCSV(rows [][]string) (string, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			return "", err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func allBatchRowsEmpty(rows []BatchImportRow) bool {
	for _, row := range rows {
		if strings.TrimSpace(row.CSVURL) != "" || strings.TrimSpace(row.Strategy) != "" {
			return false
		}
	}
	return true
}
