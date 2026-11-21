// Package app runs the Divar alert bot on every configured messaging
// platform: it serves chats, watches saved searches, and delivers listings.
package app

import (
	"context"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/mrmohebi/divar-alert/ai"
	"github.com/mrmohebi/divar-alert/config"
	"github.com/mrmohebi/divar-alert/conversation"
	"github.com/mrmohebi/divar-alert/db"
	"github.com/mrmohebi/divar-alert/divar"
	"github.com/mrmohebi/divar-alert/platform"
	"go.uber.org/zap"
)

// maxConcurrentPolls bounds how many alerts are scanned at the same time.
const maxConcurrentPolls = 4

// Options are the dependencies of an App.
type Options struct {
	Store        *db.DB
	Conversation *conversation.Service
	Logger       *zap.SugaredLogger
	PollInterval time.Duration
}

// App is the bot. Every chat is addressed as a platform.Chat, and every
// message to a chat goes through the bot of that chat's platform.
type App struct {
	store        *db.DB
	conv         *conversation.Service
	log          *zap.SugaredLogger
	pollInterval time.Duration

	// Filled by AddBot before Run and read-only afterwards.
	bots      map[platform.Platform]*bot.Bot
	platforms map[*bot.Bot]platform.Platform

	chatLocks   sync.Map // platform.Chat -> *sync.Mutex
	activeTurns sync.Map // platform.Chat -> context.CancelFunc

	pollSlots   chan struct{}
	pollLocks   sync.Map // alert ID -> *sync.Mutex
	pollJobs    sync.WaitGroup
	unservedLog sync.Map // platform.Platform -> struct{}

	// External services, replaced in tests.
	search   func(context.Context, divar.SearchQuery) (divar.SearchPage, error)
	getPost  func(context.Context, string) (divar.PostDetailRes, error)
	evaluate func(context.Context, any, []any, string) (*ai.EvaluationResult, error)
}

// New creates an App without bots; add them with AddBot.
func New(opts Options) *App {
	logger := opts.Logger
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	interval := opts.PollInterval
	if interval <= 0 {
		interval = time.Second
	}
	return &App{
		store:        opts.Store,
		conv:         opts.Conversation,
		log:          logger,
		pollInterval: interval,
		bots:         map[platform.Platform]*bot.Bot{},
		platforms:    map[*bot.Bot]platform.Platform{},
		pollSlots:    make(chan struct{}, maxConcurrentPolls),
		search:       divar.SearchWithQuery,
		getPost:      divar.GetPostContext,
		evaluate: func(ctx context.Context, post any, history []any, goal string) (*ai.EvaluationResult, error) {
			return ai.EvaluatePost(ctx, post, history, goal)
		},
	}
}

// AddBot connects the bot of one platform. A failed token check is logged,
// not fatal, so one unreachable platform does not stop the others.
func (a *App) AddBot(ctx context.Context, cfg config.Bot) error {
	opts := []bot.Option{
		bot.WithServerURL(cfg.APIURL),
		bot.WithSkipGetMe(),
		bot.WithErrorsHandler(func(err error) {
			a.log.Errorw("Bot update error", "platform", cfg.Platform, "error", err)
		}),
		bot.WithDefaultHandler(a.serialized(a.handleText)),
	}
	for _, route := range a.callbackRoutes() {
		opts = append(opts, bot.WithCallbackQueryDataHandler(route.prefix, route.match, a.serialized(route.handler)))
	}
	b, err := bot.New(cfg.Token, opts...)
	if err != nil {
		return err
	}
	for _, command := range a.commands() {
		for _, name := range command.names {
			b.RegisterHandler(bot.HandlerTypeMessageText, "/"+name, bot.MatchTypeExact, a.serialized(command.handler))
		}
	}
	if me, err := b.GetMe(ctx); err != nil {
		a.log.Errorw("Bot token check failed; updates will be retried", "platform", cfg.Platform, "error", err)
	} else {
		a.log.Infow("Bot connected", "platform", cfg.Platform, "username", me.Username)
	}
	a.publishCommands(ctx, b)
	a.registerBot(cfg.Platform, b)
	return nil
}

func (a *App) registerBot(p platform.Platform, b *bot.Bot) {
	a.bots[p] = b
	a.platforms[b] = p
}

// Run serves every bot and watches alerts until ctx is canceled, then waits
// for in-flight deliveries to finish.
func (a *App) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	schedulerDone := make(chan struct{})
	go func() { defer close(schedulerDone); a.runScheduler(ctx) }()

	var bots sync.WaitGroup
	for _, b := range a.bots {
		bots.Add(1)
		go func() { defer bots.Done(); b.Start(ctx) }()
	}
	bots.Wait()
	cancel()
	<-schedulerDone
	a.pollJobs.Wait()
}

// handlerFunc handles an update from a known chat.
type handlerFunc func(ctx context.Context, b *bot.Bot, chat platform.Chat, update *models.Update)

// serialized resolves the update's chat and runs handlers of the same chat
// one at a time. /cancel first interrupts the chat's in-flight AI turn so it
// does not wait behind it.
func (a *App) serialized(handler handlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		id, ok := updateChatID(update)
		if !ok {
			a.answerCallback(ctx, b, update, "این پیام دیگر قابل استفاده نیست.", true)
			return
		}
		chat := platform.Chat{Platform: a.platforms[b], ID: id}
		if update.Message != nil && update.Message.Text == "/cancel" {
			if cancel, ok := a.activeTurns.Load(chat); ok {
				cancel.(context.CancelFunc)()
			}
		}
		value, _ := a.chatLocks.LoadOrStore(chat, &sync.Mutex{})
		lock := value.(*sync.Mutex)
		lock.Lock()
		defer lock.Unlock()
		handler(ctx, b, chat, update)
	}
}

func updateChatID(update *models.Update) (int64, bool) {
	switch {
	case update == nil:
		return 0, false
	case update.Message != nil:
		return update.Message.Chat.ID, true
	case update.CallbackQuery != nil && update.CallbackQuery.Message.Message != nil:
		return update.CallbackQuery.Message.Message.Chat.ID, true
	}
	return 0, false
}

// callbackMessageID returns the message whose button was pressed.
func callbackMessageID(update *models.Update) int {
	if update == nil || update.CallbackQuery == nil || update.CallbackQuery.Message.Message == nil {
		return 0
	}
	return update.CallbackQuery.Message.Message.ID
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
