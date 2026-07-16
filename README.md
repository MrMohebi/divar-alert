
# Divar Alert Bot

## What is this project?

This project is a bot designed to monitor your filters on [Divar](https://divar.ir) and send alerts when new posts matching your filters are published. The bot integrates with both [Telegram](https://telegram.org) and [Bale](https://bale.ai) messaging platforms. You can configure the bot to work with either platform by setting the appropriate API URL and token in the `.env` file.

### Key Features:
- Monitor Divar filters and notify users of new posts.
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

### Running with Docker
1. Build the Docker image:
   ```bash
   docker build -t divar-alert-bot .
   ```
2. Run the container:
   ```bash
   mkdir -p data
   docker run -d --name divar-alert-bot --env-file .env \
     -v "$PWD/data:/root/app/data" divar-alert-bot
   ```

You can also set the token in `docker-compose.yml` and run `docker compose up -d`.

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

## Bot Commands

### `/start` and `/help`
- Show the main menu with buttons for creating and managing alerts.

### `/alert_set`
- **Description**: Starts the process of setting a new alert.
- **Usage**: The bot guides you through the title, Divar curl command, optional AI analysis, and check interval.
- **AI analysis**: Choose enabled or disabled with buttons. When enabled, describe what makes a post relevant to you.

### `/alerts`
- **Description**: Lists all active alerts for the user.
- **Usage**: Send `/alerts` to manage your active alerts.
- **Features**:
    - Displays the title and interval of each alert.
    - Provides inline buttons to enable or disable AI analysis per alert.
    - Provides a confirmed fresh-start action that clears delivered-post and AI history.
    - Provides a confirmed delete action for each alert.

### `/cancel`
- Cancels the current setup or AI prompt operation and returns to the main menu.

The legacy `/alertSet` and `/alertList` commands remain supported.

---

## How to Interact with the Bot

1. **Start the Bot**: Send `/start` and select an action from the main menu.
2. **Set Alerts**:
    - Select **New Alert** or use `/alert_set`.
    - Provide the Divar search API curl command copied from your browser's developer tools when prompted.
    - Choose whether AI analysis should be enabled.
    - If enabled, describe how AI should analyze posts, such as `Recommend only accident-free automatic cars with a fair price`.
    - Select a common check interval button or enter a custom value in seconds.
3. **View Alerts**:
    - Select **My Alerts** or use `/alerts`.
    - Toggle AI analysis, start an alert fresh, or delete it using the inline buttons.
4. **Receive Notifications**:
    - The bot will automatically notify you when new posts matching your filters are published.
    - When AI is enabled, notifications include a score, decision, and explanation.

---

## AI Evaluation Feature

AI analysis is optional for each alert. When enabled, the bot uses OpenAI to analyze each new post against the prompt saved with that alert and the history of previous posts. Prompts can target any category, such as vehicles, electronics, jobs, or property.

### How It Works

1. **Check AI State**: Disabled alerts skip OpenAI requests and detailed AI data extraction entirely.
2. **Get Post Details**: For enabled alerts, the bot fetches the full post details.
3. **Extract Important Data**: The bot extracts general listing data. Rental price, location, and property fields remain available when present.
4. **Load History**: The bot loads the recent history for the same alert.
5. **AI Evaluation**: The bot sends the alert's analysis prompt, the new post, and its history to OpenAI.
6. **Receive Score & Decision**: The AI returns:
   - A score from 1-100
   - A short decision based on the alert's prompt
   - A brief explanation (2-3 sentences)
   - Price, location, and feature scores when rental-specific analysis is relevant
7. **Save to History**: The analyzed post is saved for future comparisons.
8. **Notification**: You receive a message with the post and, when enabled, its AI evaluation.

### History Storage

Post history is stored in the SQLite database configured by `DB_PATH`. History is restored after restart and deleted automatically with its alert. The **Fresh Start** button clears both AI history and delivered-post tokens, resets the last-check time, and causes current matching posts to be eligible for notification again.

### Migrating From DuckDB

SQLite and DuckDB files are not interchangeable. Set `DB_PATH` to a new file such as `./data/divar-alert.db`; do not point it at the previous `db.badger` file. Keep the old file as a backup if you need its data for a separate one-time migration.

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
| `DB_PATH`           | The path to the SQLite database file.            |
| `POLL_INTERVAL_SECONDS` | Scheduler scan interval from 1 to 3600 seconds. Default: `1` |
| `OPENAI_API_KEY`    | Your OpenAI API key for AI evaluations.          |
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
