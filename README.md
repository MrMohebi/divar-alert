
# Divar Alert Bot

## What is this project?

This project is a bot designed to monitor your filters on [Divar](https://divar.ir) and send alerts when new posts matching your filters are published. The bot integrates with both [Telegram](https://telegram.org) and [Bale](https://bale.ai) messaging platforms. You can configure the bot to work with either platform by setting the appropriate API URL and token in the `.env` file.

### Key Features:
- Monitor Divar filters and notify users of new posts.
- **AI-powered evaluation** of rental/buy posts using OpenAI.
- Supports both Telegram and Bale bots.
- Persistent storage using [BadgerDB](https://github.com/dgraph-io/badger).
- Easy-to-use commands for managing alerts.
- Historical post tracking for better AI comparisons.

---

## How to Run

### Prerequisites
- [Go](https://golang.org) (if running natively)
- [Docker](https://www.docker.com) (if running with Docker)
- A `.env` file with the following variables:
  ```env
  TELEGRAM_BOT_TOKEN=<Your Telegram or Bale Bot Token>
  TELEGRAM_API_URL=<Telegram or Bale API URL>
  DB_PATH=<Path to the database directory>
  ```

### Running with Docker
1. Build the Docker image:
   ```bash
   docker build -t divar-alert-bot .
   ```
2. Run the container:
   ```bash
   docker run -d --name divar-alert-bot --env-file .env divar-alert-bot
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
   go run main.go
   ```

---

## Bot Commands

### `/alertSet`
- **Description**: Starts the process of setting a new alert.
- **Usage**: Send `/alertSet` to the bot, and it will guide you through the steps to configure a new alert.

### `/alertList`
- **Description**: Lists all active alerts for the user.
- **Usage**: Send `/alertList` to the bot to see all your active alerts.
- **Features**:
    - Displays the title and interval of each alert.
    - Provides inline buttons to delete specific alerts.

---

## How to Interact with the Bot

1. **Start the Bot**: Add the bot to your Telegram or Bale account and start a chat.
2. **Set Alerts**:
    - Use the `/alertSet` command to configure a new alert.
    - Provide the Divar filter link when prompted.
3. **View Alerts**:
    - Use the `/alertList` command to see all your active alerts.
    - Use the inline "Delete" button to remove an alert.
4. **Receive Notifications**:
    - The bot will automatically notify you when new posts matching your filters are published.
    - Each notification includes an AI evaluation with a score (1-100), decision, and explanation.

---

## AI Evaluation Feature

The bot uses OpenAI to automatically evaluate each new rental/buy post by comparing it with the history of previous posts. This helps you quickly identify the best opportunities.

### How It Works

1. **Get Post Details**: When a new post is detected, the bot fetches the full post details.
2. **Extract Important Data**: The bot extracts key information like price, location, property features, etc.
3. **Load History**: The bot loads the history of all previous posts for the same alert.
4. **AI Evaluation**: The bot sends the new post and history to OpenAI for analysis.
5. **Receive Score & Decision**: The AI returns:
   - A score from 1-100
   - A decision: "Worth Renting", "Worth Buying", or "Not Worth It"
   - A brief explanation (2-3 sentences)
6. **Save to History**: The post is saved to the history for future comparisons.
7. **Notification**: You receive a Telegram message with the post details and AI evaluation.

### History Files

Post history is stored in the `history/` directory as JSON files:
- Each alert has its own history file: `history/posts_history_<alert_id>.json`
- The history includes all important data extracted from each post
- This allows the AI to make better comparisons over time

### Customization

You can customize the AI behavior by:
- Using a different OpenAI model (set `OPENAI_MODEL` in `.env`)
- Using a compatible OpenAI API endpoint (set `OPENAI_BASE_URL` in `.env`)
- The bot will continue to work without AI evaluation if OpenAI credentials are not provided

---

## Environment Variables

| Variable            | Description                                      |
|---------------------|--------------------------------------------------|
| `TELEGRAM_BOT_TOKEN`| The token for your Telegram or Bale bot.         |
| `TELEGRAM_API_URL`  | The API URL for Telegram or Bale.                |
| `DB_PATH`           | The path to the directory where the database is stored. |
| `OPENAI_API_KEY`    | Your OpenAI API key for AI evaluations.          |
| `OPENAI_BASE_URL`   | (Optional) OpenAI API base URL. Default: `https://api.openai.com/v1` |
| `OPENAI_MODEL`      | (Optional) OpenAI model to use. Default: `gpt-4o-mini` |

---

## Example `.env` File
```env
TELEGRAM_BOT_TOKEN=123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11
TELEGRAM_API_URL=https://api.telegram.org
DB_PATH=./db.badger

# OpenAI Configuration (for AI evaluation)
OPENAI_API_KEY=sk-proj-xxxxxxxxxxxxx
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_MODEL=gpt-4o-mini
```

---

## License
This project is licensed under the MIT License. See the `LICENSE` file for details.
