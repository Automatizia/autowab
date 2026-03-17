package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	_ "github.com/mattn/go-sqlite3"
)

type Client struct {
	wac        *whatsmeow.Client
	dbPath     string
	webhookURL string
	log        *zap.Logger
	QRChan     chan string // streams QR codes as base64 PNG
}

type IncomingMessage struct {
	ID        string    `json:"id"`
	From      string    `json:"from"`
	PushName  string    `json:"pushName"`
	Body      string    `json:"body"`
	Timestamp time.Time `json:"timestamp"`
	IsGroup   bool      `json:"isGroup"`
}

func New(dbPath, webhookURL string, log *zap.Logger) (*Client, error) {
	c := &Client{
		dbPath:     dbPath,
		webhookURL: webhookURL,
		log:        log,
		QRChan:     make(chan string, 5),
	}
	return c, nil
}

func (c *Client) Connect(ctx context.Context) error {
	container, err := sqlstore.New("sqlite3", "file:"+c.dbPath+"?_foreign_keys=on", waLog.Noop)
	if err != nil {
		return err
	}

	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		return err
	}

	c.wac = whatsmeow.NewClient(deviceStore, waLog.Noop)
	c.wac.AddEventHandler(c.handleEvent)

	if c.wac.Store.ID == nil {
		// Not logged in — get QR
		qrChan, _ := c.wac.GetQRChannel(ctx)
		if err := c.wac.Connect(); err != nil {
			return err
		}
		go func() {
			for evt := range qrChan {
				if evt.Event == "code" {
					c.log.Info("QR code ready — scan with WhatsApp")
					c.QRChan <- evt.Code
				}
			}
		}()
	} else {
		if err := c.wac.Connect(); err != nil {
			return err
		}
		c.log.Info("autowab connected", zap.String("jid", c.wac.Store.ID.String()))
	}

	return nil
}

func (c *Client) Disconnect() {
	if c.wac != nil {
		c.wac.Disconnect()
	}
}

func (c *Client) IsConnected() bool {
	return c.wac != nil && c.wac.IsConnected()
}

func (c *Client) IsLoggedIn() bool {
	return c.wac != nil && c.wac.Store.ID != nil
}

// SendText sends a plain text message to a phone number (e.g. "521XXXXXXXXXX")
func (c *Client) SendText(to, text string) (string, error) {
	jid, err := types.ParseJID(to + "@s.whatsapp.net")
	if err != nil {
		return "", err
	}

	msg := &waProto.Message{
		Conversation: proto.String(text),
	}

	resp, err := c.wac.SendMessage(context.Background(), jid, msg)
	if err != nil {
		return "", err
	}

	return resp.ID, nil
}

// GetMessages returns recent messages from the local SQLite store
func (c *Client) GetMessages(limit int) ([]IncomingMessage, error) {
	// whatsmeow doesn't have a built-in query API — we read from SQLite directly
	db, err := os.Open(c.dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	// TODO: implement direct SQLite query for message history
	return []IncomingMessage{}, nil
}

// handleEvent processes incoming WhatsApp events
func (c *Client) handleEvent(rawEvt interface{}) {
	switch evt := rawEvt.(type) {
	case *events.Message:
		body := ""
		if evt.Message.GetConversation() != "" {
			body = evt.Message.GetConversation()
		} else if ext := evt.Message.GetExtendedTextMessage(); ext != nil {
			body = ext.GetText()
		}

		if body == "" {
			return
		}

		msg := IncomingMessage{
			ID:        evt.Info.ID,
			From:      evt.Info.Sender.String(),
			PushName:  evt.Info.PushName,
			Body:      body,
			Timestamp: evt.Info.Timestamp,
			IsGroup:   evt.Info.IsGroup,
		}

		c.log.Info("message received",
			zap.String("from", msg.From),
			zap.String("body", body[:min(len(body), 80)]),
		)

		if c.webhookURL != "" {
			go c.fireWebhook(msg)
		}
	}
}

func (c *Client) fireWebhook(msg IncomingMessage) {
	data, _ := json.Marshal(msg)
	resp, err := http.Post(c.webhookURL, "application/json", bytes.NewReader(data))
	if err != nil {
		c.log.Error("webhook failed", zap.Error(err))
		return
	}
	defer resp.Body.Close()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
