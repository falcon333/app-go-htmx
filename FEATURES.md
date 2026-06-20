# Strategy Portfolio Builder & Analyzer — Feature Reference

**Module:** `app-go-htmx` · **Language:** Go 1.25 · **UI:** HTMX (server-rendered HTML)
**Storage:** flat JSON files (no database) · **External dependency:** `github.com/xuri/excelize/v2` (Excel I/O)

A Go + HTMX web application for building, backtesting, and analyzing trading-strategy
portfolios. It ingests trade history (CSV/Excel), combines strategies into weighted
portfolios, and produces deep performance analytics — all rendered server-side with
HTMX for live partial updates (no SPA framework).

---

## 1. Trade Import

- **Batch import** of multiple CSV/Excel files at once (up to 25 MB per request).
- **CSV-URL import** — pull trade data directly from a URL.
- **Auto-derives the strategy name** from the uploaded filename.
- **Per-strategy timezone handling** (default `America/Chicago`).
- **Date-range filtering** (start/end) applied at import time.
- **Duplicate detection** and **row-level error reporting** (per-row error messages).
- **Per-row import summary**: imported / duplicates / skipped counts + status.
- **Strategy metadata parsing** — extracts `Strategy`, `Symbol`, and `Timeframe`
  from a structured strategy ID (e.g. `name-SYMBOL-TF`).
- Rich per-trade record: symbol, timeframe, direction, size, entry/exit times & prices,
  duration (minutes), net P&L, profit, and MAE/MFE (favorable/adverse excursion).

## 2. Portfolio Building & Management

Full CRUD over `data/portfolios.json`:

- **Create / save / load / delete** named portfolios.
- **Overwrite protection** — save detects existing names and requires explicit overwrite.
- **Create/update timestamps** tracked per portfolio.
- Each portfolio holds a set of **strategy mappings**, each with:
  - Enable/disable toggle
  - **Weight** *or* **ratio mode** (units × amount) for position sizing
  - Free-text notes
- **Portfolio filters**: date range (from/to), starting balance, range mode.
- **Merge workflow** — combine multiple strategies into a single portfolio
  (`portfolio_merge.html`), with a live final-name preview and auto-append option.

## 3. Core Equity Computation

`internal/portfolio/compute.go` — pure, dependency-free math:

- Sorts trades by exit time and walks the equity curve.
- Computes **ending capital**, **total net P&L**, peak tracking, and **max drawdown**.
- Returns the full **equity curve** (time/equity points) for charting.

## 4. Performance Analytics

Comprehensive analytics suite (`cmd/server/*.go` modules):

### Risk / Return metrics
- CAGR
- Net Gain %
- Profit Factor
- Expectancy
- Max Drawdown %
- **Ulcer Index**
- Equity R²
- Time Under Water

### Drawdown analysis
- Discrete **drawdown-event detection** with a configurable threshold (default 5%).

### Time-based analysis
- **Monthly returns tables** — both percent (%) and dollar ($) views.
- **Hourly heatmap** (configurable interval, default hourly).
- **Trade-duration buckets**.

### Exposure analysis (`exposure_analysis.go`)
- Pairwise strategy **exposure overlap** (hours) and **Jaccard %**.
- **Drawdown coincidence** analysis: coincidence hours, simultaneous-exposure hours,
  independent-timing hours, and DD-coincidence attribution.
- Exposure matrices + a **derived risk score** matrix.
- **Exposure heatmap spec** with calibrated mid/max thresholds for Jaccard,
  DD-attribution, and risk scales.

### Charts
- JS chart series and **TradingView** chart links.
- Series include equity, P&L, drawdown % and drawdown amount.
- Charts auto-enable above a configurable strategy-count threshold (default 10).

## 5. Portfolio Suggestions (the "builder" engine)

`cmd/server/portfolio_suggestions.go` — auto-generates **three candidate portfolios**
from your strategies' statistics:

| Candidate | Objective |
|-----------|-----------|
| **Return Max** | Highest CAGR subject to risk caps |
| **Low Drawdown** | Lowest drawdown with solid scores |
| **Balanced** | Best overall scorecard balance |

- **Candidate filtering**: trades ≥ 30, profit factor ≥ 1.20, max drawdown ≤ 10%.
- **Graceful fallback** — relaxes the filters if too few strategies qualify.
- **Score-amplified weighting**: normalizes each strategy's scorecard, amplifies by a
  configurable factor (default 2.0), caps per-strategy weight (default max 10),
  then normalizes the weight set.
- Reports per-strategy **drawdown contribution**, expected portfolio CAGR, worst
  drawdown, and a human-readable rationale.

## 6. Scoring Weights

`internal/trades/score_weights.go` — configurable scorecard weighting that drives the
suggestion engine. Persisted to `internal/trades/score_weights.json`. Weighted inputs:

- Equity R²
- Net return
- Drawdown (DD)
- Profit Factor (PF)
- Expectancy

Editable via the **Scoring Weights** UI (`scoring_weights.html`) with success/error feedback.

## 7. Analysis Settings (persistence)

`internal/trades/analysis.go` — per-portfolio analysis settings saved to
`internal/trades/analysis_settings.json`:

- Quick-range presets, auto-refresh toggle, custom N + unit ranges, explicit start/end dates.
- Balance, drawdown threshold, heatmap interval.
- Suggestion params (amplify factor, max weight).
- Chart enable/threshold state.
- `UpdatedAt` timestamp per portfolio.

## 8. Live UI (HTMX)

Server-rendered HTML with HTMX partial updates:

- **Inline / debounced field validation** (e.g. `/portfolio/validate/starting-capital`,
  `/portfolio/validate/trade-pnls`) — validates as you type without full reload.
- Detects the `HX-Request` header to return **partial fragments** vs. full pages.
- Templates: `portfolio.html`, `portfolio_merge.html`, `imports_batch.html`,
  `strat_exposure.html`, `scoring_weights.html`.
- Custom template helpers: `toJSON`, `pct`, `fmtFloat`, `fmtPct`, `dict`, `seq`,
  `mul`, `div`, `float64`, `urlquery`.

---

## Architecture

```
cmd/server/                    HTTP handlers (net/http mux) + analytics modules
  main.go                      entry point, template setup, /portfolio routes (~2900 lines)
  portfolio_suggestions.go     suggestion engine (Return Max / Low DD / Balanced)
  exposure_analysis.go         pairwise exposure & DD-coincidence analysis
  drawdown_events.go           discrete drawdown-event detection
  monthly_tables.go            monthly % and $ return tables
  hourly_heatmap.go            time-of-day heatmap
  trade_duration_analysis.go   duration buckets
  summaries.go                 strategy / pair summaries
internal/
  portfolio/                   pure equity-curve math (Compute) + sort
  trades/                      domain: import, parse, store, mappings, scoring, settings
web/templates/                 HTMX server-rendered HTML
data/portfolios.json           persisted portfolios
```

**Design notes**

- Classic Go stdlib HTTP server (`net/http`), no web framework.
- HTMX drives interactivity; partial vs. full renders keyed off the `HX-Request` header.
- JSON-file persistence — portfolios, scoring weights, and analysis settings each in
  their own file.
- Clean separation between pure-domain packages (`internal/`) and HTTP/view concerns
  (`cmd/server/`).
- Test coverage includes `internal/portfolio/compute_test.go` and
  `cmd/server/exposure_analysis_test.go`.

## Configuration

| Setting | Default | Notes |
|---------|---------|-------|
| Timezone | `America/Chicago` | per-import override |
| Chart threshold | 10 strategies | auto-enable charts above this |
| Drawdown threshold | 5% (`0.05`) | drawdown-event detection |
| Heatmap interval | `hourly` | |
| Trades display limit | 300 | paginated |
| Suggestion amplify | 2.0 | score amplification factor |
| Suggestion max weight | 10.0 | per-strategy weight cap |
| `TRADES_DATA_DIR` | `data` | env var to relocate the data dir |

## Running

```bash
# from app-go-htmx/
go run ./cmd/server      # start the server
go test ./...            # run the test suite
go build ./cmd/server    # build a binary
```
