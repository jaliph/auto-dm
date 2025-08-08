package wa

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jaliph/auto-dm/utils"
	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

type WaClient struct {
	client *whatsmeow.Client
}

func NewAdminClient(dbPath string, loglevel string) (*WaClient, error) {
	dbLog := waLog.Stdout("Database", strings.ToUpper(loglevel), true)
	ctx := context.Background()
	container, err := sqlstore.New(ctx, "sqlite3", fmt.Sprintf("file:%s/wapp.db?_foreign_keys=on", dbPath), dbLog)
	if err != nil {
		utils.Logger.Error("Failed to create database container", "error", err)
		return nil, err
	}
	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		utils.Logger.Error("Failed to get first device from database", "error", err)
		return nil, err
	}
	client := whatsmeow.NewClient(deviceStore, waLog.Noop)
	if client.Store.ID == nil {
		// No ID stored, new login
		utils.Logger.Info("No ID stored, new login required")
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			return nil, err
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.H, os.Stdout)
				file := fmt.Sprintf("%s/%s.txt", dbPath, "qrcode")
				f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
				if err != nil {
					utils.Logger.Error("Failed to open QR code file", "error", err)
				} else {
					qrterminal.GenerateHalfBlock(evt.Code, qrterminal.H, f)
				}
			} else {
				// Handle other events
				utils.Logger.Info("Event received", "event", evt.Event)
			}
		}
	} else {
		// Already logged in, just connect
		utils.Logger.Debug("Already logged in, connecting")
		err := client.Connect()
		if err != nil {
			return nil, err
		}
	}
	return &WaClient{client}, nil
}

func (w *WaClient) SendMessage(jid string, message string) error {
	utils.Logger.Debug("Trying to sending message", "jid", jid, "message", message)
	resp, err := w.client.SendMessage(context.Background(), types.JID{
		User:   jid,
		Server: types.DefaultUserServer,
	}, &waE2E.Message{
		Conversation: proto.String(message),
	})
	if err != nil {
		utils.Logger.Error("Failed to send message", "jid", jid, "error", err)
		return err
	}
	utils.Logger.Debug("Message sent successfully", "resp", resp)
	return nil
}

func (w *WaClient) Disconnect() {
	if w.client != nil {
		w.client.Disconnect()
		utils.Logger.Info("WhatsApp client disconnected")
	}
}
