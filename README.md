
# Divar Alert Bot

## What is this project?

This project is a bot designed to monitor your filters on [Divar](https://divar.ir) and send alerts when new posts matching your filters are published. The bot integrates with both [Telegram](https://telegram.org) and [Bale](https://bale.ai) messaging platforms. You can configure the bot to work with either platform by setting the appropriate API URL and token in the `.env` file.

### Key Features:
- Describe an alert in Persian; the bot resolves cities, categories, and live Divar filters.
- Answer up to five dynamic clarification questions, review the summary, and confirm.
- Monitor every 60 seconds by default, with up to three current listings sent initially.
- **Prompt-based AI analysis** for any Divar category using OpenAI.
- Supports both Telegram and Bale bots.
- Persistent storage using SQLite, including alerts, bot state, delivered posts, and AI history.
- Easy-to-use commands for managing alerts.
- Historical post tracking for better AI comparisons.

---

## How to Run

### Prerequisites
- [Go](https://golang.org) 1.25 or newer (if running natively)
- [Docker](https://www.docker.com) (if running with Docker)
- [Air](https://github.com/air-verse/air) (optional, for live reload during development)
- A `.env` file with the following variables:
  ```env
  TELEGRAM_BOT_TOKEN=<Your Telegram or Bale Bot Token>
  TELEGRAM_API_URL=<Telegram or Bale API URL>
  DB_PATH=<Path to the SQLite database file>
  POLL_INTERVAL_SECONDS=<Seconds between scheduler scans; default 1>
  ```

### Deploying a published Docker image (Linux x86-64)

1. Publish this version through **Actions → Publish image to GHCR → Run workflow**,
   selecting the branch containing your changes (for example, `fully-ai`). A
   manual run publishes `ghcr.io/mrmohebi/divar-alert:sha-<full-commit>` without
   changing `latest`. To promote an already verified image without rebuilding,
   run the same workflow with its `promote_digest` input set to the complete
   `sha256:...` digest; the workflow verifies that `latest` resolves to that digest.
   Publishing a GitHub release also publishes its release tag;
   only stable releases update `latest`. The workflow tests the code before
   publishing and records the resulting tags and digest in its run summary.
   GitHub requires the workflow file to exist on the default branch for manual
   dispatch; if GitHub does not offer this workflow, merge the workflow change
   into the default branch first, then select the desired branch when running it.

2. Put `docker-compose.yml` and a private `.env` file on the server. Start from
   `.env.example` and configure:
   ```env
   DIVAR_IMAGE_TAG=sha-<full-commit-from-the-successful-workflow>
   TELEGRAM_BOT_TOKEN=<your-token>
   TELEGRAM_API_URL=https://tapi.bale.ai
   POLL_INTERVAL_SECONDS=1
   OPENAI_API_KEY=<your-key>
   OPENAI_BASE_URL=https://api.openai.com/v1
   OPENAI_MODEL=gpt-4o-mini
   ```
   Use `https://api.telegram.org` for Telegram. Keep a custom AI base URL/model if
   your provider requires it. Conversational alert setup needs the AI key.
   Compose loads `.env` into the container and sets
   `DB_PATH=/root/app/data/divar-alert.db`, overriding any native path in `.env`.
   Do not paste resolved `docker compose config` output into logs or tickets;
   it includes credentials. No inbound ports are needed.

3. Stop any other instance using the same bot token before starting this one.
   Create `data/` and preserve any existing SQLite database there. Before the
   first migration or an upgrade, stop the bot and back up the whole directory,
   including any SQLite WAL files:
   ```bash
   mkdir -p data
   docker compose stop
   tar -czf "divar-data-$(date -u +%Y%m%dT%H%M%SZ).tar.gz" data
   ```
   For an initial deployment, omit `docker compose stop` if no container exists.
   Native instances must also be stopped before backing up their database.

4. Pull and verify the image against the digest recorded by the workflow:
   ```bash
   docker compose pull
   docker image inspect "ghcr.io/mrmohebi/divar-alert:$DIVAR_IMAGE_TAG" \
     --format '{{json .RepoDigests}} {{index .Config.Labels "org.opencontainers.image.revision"}}'
   docker compose up -d --no-build
   docker compose ps
   docker compose logs --tail=100 -f
   ```
   In the inspection command, set the shell variable `DIVAR_IMAGE_TAG` to the
   same published tag you put in `.env`. Public packages can be pulled without
   login; private packages require `docker login ghcr.io` with read access.

For upgrades, stop and back up first, change `DIVAR_IMAGE_TAG` to the tested
version, then pull and recreate with the same commands. Keep one running bot
instance and one writable database. Existing SQLite alerts migrate automatically;
older DuckDB files are not compatible. To roll back a schema-changing upgrade,
restore its pre-upgrade backup with the bot stopped and use the previous image.

### Building and checking the image locally

```bash
docker build --platform linux/amd64 -t divar-alert:deployment-check .
python3 scripts/docker-smoke.py divar-alert:deployment-check
```

The Linux smoke test uses a local mock bot API, disposable data, and fake
credentials. It checks Compose configuration, startup, embedded catalogs, CA
certificates, legacy SQLite migration, persistence across container recreation,
and graceful shutdown. It sends no real chat messages.

To run a local build yourself, use the same persistent database path:
```bash
docker run -d --name divar-alert-bot --env-file .env \
  -e DB_PATH=/root/app/data/divar-alert.db \
  -v "$PWD/data:/root/app/data" divar-alert:deployment-check
```

### Running Natively
1. Clone the repository:
   ```bash
   git clone https://github.com/mrmohebi/divar-alert.git
   cd divar-alert
   ```
2. Install dependencies:
   ```bash
   go mod tidy
   ```
3. Run the bot:
   ```bash
   go run .
   ```

### Running with Air

Air rebuilds and restarts the bot whenever a Go source file changes. The repository already includes an `.air.toml` configuration.

1. Install Air using Go 1.25 or newer:
   ```bash
   go install github.com/air-verse/air@latest
   ```
2. Ensure the Go binary directory is available in your `PATH`:
   ```bash
   export PATH="$PATH:$(go env GOPATH)/bin"
   ```
3. Create your local environment file if needed:
   ```bash
   cp .env.example .env
   ```
4. Start the bot with live reload from the repository root:
   ```bash
   air
   ```

Air uses `.air.toml` to build the complete application into `tmp/main`. The application loads `.env` at startup, and Air restarts it after each successful rebuild.

---

## Category Catalog

The complete nested tree from the **دسته‌ها** sidebar on
[Divar Kerman](https://divar.ir/s/kerman) is saved in `divar/categories.json`.
It includes the UTC `generated_at` timestamp, source page, refresh request,
category count (excluding `ROOT`), and the full tree with Persian names,
API slugs, parent slugs, icons, and children. Leaves have `children: []`.
`second_name` gives a more specific label, such as `اجارهٔ آپارتمان`.
API slugs can differ from website URL aliases.

Refresh the file without starting the bot or configuring credentials:

```bash
go run ./cmd/update-categories
# Optional output path:
go run ./cmd/update-categories -output ./data/categories.json
```

Call the refresh function from Go:

```go
catalog, err := divar.UpdateCategories(ctx, "divar/categories.json")
```

`divar.GetCategories(ctx)` fetches without saving, and
`divar.NewCategoriesRequest(ctx)` builds the underlying HTTP request.
The public endpoint used by Divar supplies every nested category in a single
response and does not require a city parameter, cookies, or authentication:

```bash
curl 'https://api.divar.ir/v8/postlist/w/categories' \
  -H 'Accept: application/json'
```

Refresh validates the response before replacing the JSON file atomically.
Failed requests leave the previous file intact. Refresh is explicit; it does
not run automatically when the bot starts.

---

## City Catalog

`divar/places.json` saves all cities grouped by province under the country root,
including Divar's string IDs, Persian names, parent IDs, subtitles and selection
labels when provided, API version, counts, UTC `generated_at`, and refresh request.
Leaves have `children: []`. The API's `tabs` repeats the same root; the catalog
stores the hierarchy once.

Refresh without bot credentials:

```bash
go run ./cmd/update-places
# Optional output path:
go run ./cmd/update-places -output ./data/places.json
```

From Go, call `divar.UpdatePlaces(ctx, "divar/places.json")` to refresh the file,
`divar.GetPlaces(ctx)` to fetch without saving, or `divar.NewPlacesRequest(ctx)`
to build the request. Failed refreshes leave the previous file intact.

The request retains POST but sends no custom headers, cookies, or body:

```bash
curl -X POST 'https://api.divar.ir/v8/postlist/w/places'
```

The endpoint rejects GET, so POST must be explicit after removing `--data-raw`.

---

## Dynamic Filters

Filters are fetched on demand and are never saved to a JSON file:

```go
filters, err := divar.GetFilters(ctx, divar.FiltersParams{
    Category: "partnership", // API slug from the category catalog
    CityIDs:  []string{"13"}, // IDs from the city catalog
})
if err != nil {
    return err
}

form, err := filters.BuildFormData(map[string]any{
    "has-photo":     true,
    "business-type": []string{"personal"},
    "recent_ads":    "1d",
})
if err != nil {
    return err
}
searchData := filters.SearchData
searchData.FormData.Data = form
// Use searchData in a search request's search_data, keeping the same city IDs.
// Or pass form as FiltersParams.FormData to refresh dependent filters.
```

`FiltersResult` returns the category, city IDs, page title, current search data,
and a list of `FilterDefinition` structs containing exact field keys, value types,
Persian labels, sections, units, options, default/selected values, validators,
range suggestions, and lazy-loading metadata. Styling and analytics are omitted.
`FiltersResponse`, `FiltersPage`, and `FilterWidget` describe the upstream shape;
`CleanFilters` is the conversion point for later return-format changes.
Each definition retains its original `RawData` in memory (excluded from JSON).

`BuildFormData` preserves the category and existing selections. Use `nil` to clear
a filter. Selections use `string` for `str`, `bool` for `boolean`, `[]string` for
`repeated_string`, or `divar.FilterNumberRange{Minimum: "0", Maximum: "1000000"}`
for `number_range`. Range bounds are integer strings, preserving zero and large
prices. The helper checks types and range ordering; callers still need to apply
the returned validators and available options. Individual definitions also expose
`EncodeValue` for encoding a single selection.

Brand/model filters can have `LazyPayload` and `OnlineSearch` instead of inline
options. These are preserved for subsequent requests; this function does not load
those options. Suggested minimum/maximum values are suggestions, not limits.

`NewFiltersRequest` builds the POST request with only `Content-Type` and the
required dynamic body. No cookies, authentication, or tracing headers are sent.
`FiltersParams.ServerPayload` can carry current search state; otherwise the request
uses Divar's default date sort. The filters page's UI server payload is kept
separate from the search server payload.

---

## Bot Commands

### `/start` and `/help`
- Show the main menu with buttons for creating and managing alerts.

### `/alert_set`
- **Description**: Starts or resumes conversational alert creation.
- **Usage**: Describe what you want in Persian, including the city if known. The bot asks for a missing or ambiguous city separately, then asks at most five category/filter questions with three choices each. Typed answers and corrections are supported.
- **Confirmation**: Review the applied search conditions and additional scoring preferences, then confirm, edit, or cancel.
- **AI analysis**: Preferences that Divar cannot express become optional AI commentary. Every search match is sent, regardless of score or whether those preferences are met.

### `/alerts`
- **Description**: Lists all active alerts for the user.
- **Usage**: Send `/alerts` to manage your active alerts.
- **Features**:
    - Displays the title and interval of each alert.
    - Provides inline buttons to enable or disable AI analysis per alert.
    - Provides a confirmed fresh-start action that resets delivery deduplication and AI history while retaining delivery audit records.
    - Provides a confirmed delete action for each alert.

### `/cancel`
- Cancels the current setup or AI prompt operation and returns to the main menu.

The legacy `/alertSet` and `/alertList` commands remain supported.

---

## How to Interact with the Bot

1. **Start the Bot**: Send `/start`, select **New Alert**, or simply write your request.
2. **Set Alerts**:
    - Describe the item, city, budget, and preferences in natural Persian.
    - Answer only the clarifications needed; choose a button or type your reply.
    - Review and confirm the summary. Checking defaults to 60 seconds; request a different interval in your message if needed (10–86400 seconds).
    - After five category/filter questions, unresolved details are disclosed for your review. The bot never invents an unspecified budget or city.
3. **View Alerts**:
    - Select **My Alerts** or use `/alerts`.
    - Toggle AI analysis, start an alert fresh, or delete it using the inline buttons.
4. **Receive Notifications**:
    - New conversational alerts initially send up to three current listings, then only newly appearing search matches.
    - When AI is enabled, notifications include a Persian score, recommendation, and explanation. Low scores and unmet wishes never hide a listing. If AI or listing-detail extraction fails, the listing is sent without commentary.
    - Failed sends retry. Pagination and scan checkpoints persist across restarts.

---

## AI Evaluation Feature

AI analysis is optional for each alert. When enabled, the bot uses OpenAI to analyze each new post against the prompt saved with that alert and the history of previous posts. Prompts can target any category, such as vehicles, electronics, jobs, or property.

### How It Works

1. **Check AI State**: Disabled alerts skip OpenAI requests and detailed AI data extraction entirely.
2. **Get Post Details**: For enabled alerts, the bot fetches the full post details.
3. **Extract Important Data**: The bot extracts general listing data. Rental price, location, and property fields remain available when present.
4. **Load History**: The bot loads the recent history for the same alert.
5. **AI Evaluation**: The bot sends the alert's analysis prompt, the new post, and its history to OpenAI.
6. **Receive Score & Decision**: The AI returns commentary (never a delivery gate):
   - A score from 1-100
   - A short decision based on the alert's prompt
   - A brief explanation (2-3 sentences)
   - Price, location, and feature scores when rental-specific analysis is relevant
7. **Save to History**: Keep only the latest 50 posts per alert for future comparisons, trimming older history on startup and after each append. Each AI evaluation uses at most the latest 20 previous posts.
8. **Notification**: You receive a message with the post and, when enabled, its AI evaluation.

### History Storage

Post history is stored in the SQLite database configured by `DB_PATH`. History is restored after restart and limited to the latest 50 ads per alert; each AI evaluation uses at most 20 previous ads. Deleting an alert soft-deletes its configuration and stops monitoring while preserving its stored data. The **Fresh Start** button clears AI history and scan state, and retires active delivery deduplication records without erasing them. Conversational alerts start again with up to three current listings; legacy curl alerts retain their existing fresh-start behavior.

### Migrating From DuckDB

Successful delivery records retain their row ID, alert ID, listing token, payload, and timestamp. Fresh Start marks old delivery records with `deleted_at`, allowing a resend to create a new audit row. Deleted alerts retain their owning chat ID, so delivery records remain attributable to users. No charging, quota enforcement, or AI-token accounting is implemented. These records preserve the current at-least-once delivery behavior, not an exactly-once billing ledger. Temporary conversation state and capped AI history still use physical cleanup.

SQLite and DuckDB files are not interchangeable. Set `DB_PATH` to a new file such as `./data/divar-alert.db`; do not point it at the previous `db.badger` file. Keep the old file as a backup if you need its data for a separate one-time migration.

### Conversation implementation

`conversation.Service` owns the typed `AlertDraft` and runs catalog-grounded AI turns through `ai.PlanAlertTurn`. Draft JSON is stored in the existing SQLite processes table. Category/filter questions are capped at five per draft (edits do not reset the budget); city questions are separate. Confirmation atomically consumes the saved revision, preventing duplicate creation. `/start` resumes an unfinished draft and `/cancel` cancels even an in-flight interpretation.

`divar.SearchWithQuery` uses a typed request, date sorting, disabled recommendations, and opaque pagination cursors. The scheduler allows up to four independent structured alert scans without overlapping scans of the same alert. A scan processes at most ten pages per scheduling pass, saving its cursor for continuation. External-service delays can extend an individual scan beyond the configured interval.

Category and city snapshots are embedded in the binary; refresh them with the existing commands and rebuild to deploy updated snapshots. Dynamic filters and lazy brand/model options are fetched during interpretation, not written to disk. Additive SQLite migrations retain legacy curl alerts and in-progress legacy setup sessions. Runtime and conversation error messages are Persian; credentials and technical errors stay out of chat replies.

Tests use mocked AI, Divar, and Telegram/Bale HTTP endpoints; no live chat messages are sent. Run `go test ./...` and `go test -race ./...` for the conversation, persistence, and watcher checks.

### Customization

You can customize the AI behavior by:
- Enabling or disabling AI independently for every alert
- Setting a different analysis prompt for each alert
- Using a different OpenAI model (set `OPENAI_MODEL` in `.env`)
- Using a compatible OpenAI API endpoint (set `OPENAI_BASE_URL` in `.env`)
- The bot will continue to work without AI evaluation if OpenAI credentials are not provided

---

## Environment Variables

| Variable            | Description                                      |
|---------------------|--------------------------------------------------|
| `TELEGRAM_BOT_TOKEN`| The token for your Telegram or Bale bot.         |
| `TELEGRAM_API_URL`  | The API URL for Telegram or Bale.                |
| `DIVAR_IMAGE_TAG`  | Required by Docker Compose: a published release tag or `sha-<full-commit>`. |
| `DB_PATH`           | The path to the SQLite database file.            |
| `POLL_INTERVAL_SECONDS` | Scheduler scan interval from 1 to 3600 seconds. Default: `1` |
| `OPENAI_API_KEY`    | Required for conversational setup; also used for optional per-listing commentary. Existing alerts continue monitoring if AI is unavailable. |
| `OPENAI_BASE_URL`   | (Optional) OpenAI API base URL. Default: `https://api.openai.com/v1` |
| `OPENAI_MODEL`      | (Optional) OpenAI model to use. Default: `gpt-4o-mini` |

---

## Example `.env` File
```env
TELEGRAM_BOT_TOKEN=123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11
TELEGRAM_API_URL=https://api.telegram.org
DB_PATH=./data/divar-alert.db
POLL_INTERVAL_SECONDS=1

# OpenAI Configuration (for AI evaluation)
OPENAI_API_KEY=sk-proj-xxxxxxxxxxxxx
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_MODEL=gpt-4o-mini
```

---

## License
This project is licensed under the MIT License. See the `LICENSE` file for details.
