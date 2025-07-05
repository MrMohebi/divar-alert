
# Divar Alert Bot

## What is this project?

This project is a bot designed to monitor your filters on [Divar](https://divar.ir) and send alerts when new posts matching your filters are published. The bot integrates with both [Telegram](https://telegram.org) and [Bale](https://bale.ai) messaging platforms. You can configure the bot to work with either platform by setting the appropriate API URL and token in the `.env` file.

### Key Features:
- Monitor Divar filters and notify users of new posts.
- Supports both Telegram and Bale bots.
- Persistent storage using [BadgerDB](https://github.com/dgraph-io/badger).
- Easy-to-use commands for managing alerts.

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

---

## Environment Variables

| Variable            | Description                                      |
|---------------------|--------------------------------------------------|
| `TELEGRAM_BOT_TOKEN`| The token for your Telegram or Bale bot.         |
| `TELEGRAM_API_URL`  | The API URL for Telegram or Bale.                |
| `DB_PATH`           | The path to the directory where the database is stored. |

---

## Example `.env` File
```env
TELEGRAM_BOT_TOKEN=123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11
TELEGRAM_API_URL=https://api.telegram.org
DB_PATH=./db.badger
```

---

## License
This project is licensed under the MIT License. See the `LICENSE` file for details.
